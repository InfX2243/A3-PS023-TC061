package workerhealth

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

const DefaultStaleAfter = 30 * time.Second

type Status string

const (
	StatusHealthy     Status = "healthy"
	StatusStale       Status = "stale"
	StatusUnreachable Status = "unreachable"
)

type Report struct {
	WorkerID                 string    `json:"worker_id"`
	Hostname                 string    `json:"hostname"`
	ReportedAt               time.Time `json:"reported_at"`
	LastHeartbeat            time.Time `json:"last_heartbeat"`
	AvailableMemoryBytes     uint64    `json:"available_memory_bytes"`
	ConfiguredMemoryCapBytes uint64    `json:"configured_memory_cap_bytes"`
	CloudHypervisorHealthy   bool      `json:"cloud_hypervisor_healthy"`
	Status                   Status    `json:"status"`
}

type Heartbeat struct {
	WorkerID                 string `json:"worker_id"`
	Hostname                 string `json:"hostname"`
	AvailableMemoryBytes     uint64 `json:"available_memory_bytes"`
	ConfiguredMemoryCapBytes uint64 `json:"configured_memory_cap_bytes"`
	CloudHypervisorHealthy   bool   `json:"cloud_hypervisor_healthy"`
}

type Registry struct {
	mu        sync.RWMutex
	workers   map[string]Report
	now       func() time.Time
	staleAfter time.Duration
}

func NewRegistry(staleAfter time.Duration) *Registry {
	if staleAfter <= 0 {
		staleAfter = DefaultStaleAfter
	}
	return &Registry{workers: make(map[string]Report), now: time.Now, staleAfter: staleAfter}
}

func (r *Registry) Heartbeat(h Heartbeat) (Report, error) {
	if h.WorkerID == "" || len(h.WorkerID) > 128 {
		return Report{}, errors.New("worker_id must be between 1 and 128 characters")
	}
	if h.Hostname == "" || len(h.Hostname) > 255 {
		return Report{}, errors.New("hostname must be between 1 and 255 characters")
	}
	if h.ConfiguredMemoryCapBytes == 0 {
		return Report{}, errors.New("configured_memory_cap_bytes must be positive")
	}
	if h.AvailableMemoryBytes > h.ConfiguredMemoryCapBytes {
		return Report{}, errors.New("available_memory_bytes cannot exceed configured memory cap")
	}
	now := r.now().UTC()
	report := Report{
		WorkerID: h.WorkerID, Hostname: h.Hostname, ReportedAt: now, LastHeartbeat: now,
		AvailableMemoryBytes: h.AvailableMemoryBytes, ConfiguredMemoryCapBytes: h.ConfiguredMemoryCapBytes,
		CloudHypervisorHealthy: h.CloudHypervisorHealthy, Status: StatusHealthy,
	}
	if !h.CloudHypervisorHealthy {
		report.Status = StatusUnreachable
	}
	r.mu.Lock()
	r.workers[h.WorkerID] = report
	r.mu.Unlock()
	return report, nil
}

func (r *Registry) List() []Report {
	now := r.now().UTC()
	r.mu.RLock()
	reports := make([]Report, 0, len(r.workers))
	for _, report := range r.workers {
		age := now.Sub(report.LastHeartbeat)
		switch {
		case age < 0:
			report.Status = StatusUnreachable
		case age > r.staleAfter:
			report.Status = StatusStale
		case !report.CloudHypervisorHealthy:
			report.Status = StatusUnreachable
		default:
			report.Status = StatusHealthy
		}
		reports = append(reports, report)
	}
	r.mu.RUnlock()
	sort.Slice(reports, func(i, j int) bool { return reports[i].WorkerID < reports[j].WorkerID })
	return reports
}

func (r *Registry) Get(id string) (Report, bool) {
	for _, report := range r.List() {
		if report.WorkerID == id {
			return report, true
		}
	}
	return Report{}, false
}

func (r *Registry) Validate() error {
	if r == nil {
		return fmt.Errorf("worker registry is nil")
	}
	return nil
}
