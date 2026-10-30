// Package observability receives host telemetry from enrolled EC2 workers.
// Samples are retained in memory for the lifetime of the controller process.
package observability

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var instanceIDPattern = regexp.MustCompile(`^i-[a-fA-F0-9]{8,32}$`)

type Sample struct {
	InstanceID string    `json:"instance_id"`
	Timestamp  time.Time `json:"timestamp"`
	CPUPercent float64   `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	DiskPercent float64  `json:"disk_percent"`
	NetworkRxBytes uint64 `json:"network_rx_bytes"`
	NetworkTxBytes uint64 `json:"network_tx_bytes"`
}

type Store struct {
	mu sync.RWMutex
	samples map[string][]Sample
	maxPerInstance int
	retention time.Duration
	now func() time.Time
}

func NewStore() *Store {
	return &Store{samples: make(map[string][]Sample), maxPerInstance: 1440, retention: 24*time.Hour, now: time.Now}
}

func (s *Store) Add(sample Sample) error {
	if !instanceIDPattern.MatchString(sample.InstanceID) { return errors.New("invalid EC2 instance ID") }
	if sample.CPUPercent < 0 || sample.CPUPercent > 100 || sample.MemoryPercent < 0 || sample.MemoryPercent > 100 || sample.DiskPercent < 0 || sample.DiskPercent > 100 {
		return errors.New("CPU, memory, and disk percentages must be between 0 and 100")
	}
	now := s.now().UTC()
	if sample.Timestamp.IsZero() { sample.Timestamp = now }
	if sample.Timestamp.After(now.Add(2*time.Minute)) || sample.Timestamp.Before(now.Add(-s.retention)) { return errors.New("sample timestamp is outside the accepted time window") }
	sample.Timestamp = sample.Timestamp.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	items := append(s.samples[sample.InstanceID], sample)
	cutoff := now.Add(-s.retention)
	kept := items[:0]
	for _, item := range items { if item.Timestamp.After(cutoff) { kept = append(kept, item) } }
	if len(kept) > s.maxPerInstance { kept = kept[len(kept)-s.maxPerInstance:] }
	s.samples[sample.InstanceID] = append([]Sample(nil), kept...)
	return nil
}

func (s *Store) List(instanceID string, since time.Time) []Sample {
	s.mu.RLock()
	items := append([]Sample(nil), s.samples[instanceID]...)
	s.mu.RUnlock()
	result := items[:0]
	for _, item := range items { if since.IsZero() || !item.Timestamp.Before(since) { result = append(result, item) } }
	sort.Slice(result, func(i,j int) bool { return result[i].Timestamp.Before(result[j].Timestamp) })
	return result
}

type Handler struct { Store *Store; WorkerSecret string; OperatorToken string }

func (h Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/worker/metrics", h.ingest)
	mux.HandleFunc("GET /api/v1/instances/{instanceID}/metrics", h.query)
}

func bearer(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(value, "Bearer ") { return strings.TrimSpace(strings.TrimPrefix(value, "Bearer ")) }
	return ""
}
func equalSecret(got, want string) bool {
	if want == "" || len(got) != len(want) { return false }
	var diff byte
	for i := range got { diff |= got[i] ^ want[i] }
	return diff == 0
}
func (h Handler) ingest(w http.ResponseWriter, r *http.Request) {
	if !equalSecret(bearer(r), h.WorkerSecret) { http.Error(w, "unauthorized", http.StatusUnauthorized); return }
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var sample Sample
	dec := json.NewDecoder(r.Body); dec.DisallowUnknownFields()
	if err := dec.Decode(&sample); err != nil { http.Error(w, "invalid metrics payload", http.StatusBadRequest); return }
	if err := h.Store.Add(sample); err != nil { http.Error(w, err.Error(), http.StatusBadRequest); return }
	w.WriteHeader(http.StatusAccepted)
}
func (h Handler) query(w http.ResponseWriter, r *http.Request) {
	if !equalSecret(bearer(r), h.OperatorToken) { http.Error(w, "unauthorized", http.StatusUnauthorized); return }
	id := r.PathValue("instanceID")
	if !instanceIDPattern.MatchString(id) { http.Error(w, "invalid EC2 instance ID", http.StatusBadRequest); return }
	var since time.Time
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil { since = parsed } else { http.Error(w, "since must be RFC3339", http.StatusBadRequest); return }
	}
	items := h.Store.List(id, since)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"instance_id": id, "samples": items})
}
