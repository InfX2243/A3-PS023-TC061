package placement

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/store"
)

const gib = uint64(1024 * 1024 * 1024)

func newPlacementStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), ":memory:")
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func addWorker(t *testing.T, s *store.Store, id, state string, available, limit uint64, heartbeat time.Time) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB().Exec(`INSERT INTO workers(id, name, endpoint, state, last_heartbeat, available_memory_bytes, memory_limit_bytes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, id, "http://127.0.0.1:9000", state, heartbeat.UTC().Format(time.RFC3339Nano), int64(available), int64(limit), now, now)
	if err != nil { t.Fatalf("insert worker %s: %v", id, err) }
}

func addVM(t *testing.T, s *store.Store, id string, memory uint64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB().Exec(`INSERT INTO vms(id, name, worker_id, vcpus, memory_bytes, image_id, desired_state, observed_state, created_at, updated_at)
		VALUES (?, ?, NULL, 1, ?, 'ubuntu-demo', 'stopped', 'defined', ?, ?)`, id, id, int64(memory), now, now)
	if err != nil { t.Fatalf("insert VM %s: %v", id, err) }
}

func reserve(t *testing.T, s *store.Store, vmID string, memory uint64, policy Policy, now time.Time) (string, error) {
	t.Helper()
	var selected string
	err := s.WithTx(context.Background(), func(tx *sql.Tx) error {
		var err error
		selected, err = PlaceAndReserve(context.Background(), tx, vmID, memory, policy, now)
		return err
	})
	return selected, err
}

func TestPlaceAndReserveChoosesGreatestSafeCapacity(t *testing.T) {
	s := newPlacementStore(t)
	now := time.Now().UTC()
	addWorker(t, s, "worker-a", "healthy", 6*gib, 8*gib, now)
	addWorker(t, s, "worker-b", "healthy", 7*gib, 8*gib, now)
	addVM(t, s, "vm-1", 4*gib)
	selected, err := reserve(t, s, "vm-1", 4*gib, Policy{SafetyReserveBytes: gib, StaleAfter: 30*time.Second}, now)
	if err != nil { t.Fatalf("PlaceAndReserve: %v", err) }
	if selected != "worker-b" { t.Fatalf("selected %q, want worker-b", selected) }
	var assigned string
	if err := s.DB().QueryRow("SELECT worker_id FROM vms WHERE id = 'vm-1'").Scan(&assigned); err != nil { t.Fatal(err) }
	if assigned != selected { t.Fatalf("VM assigned to %q, want %q", assigned, selected) }
}

func TestPlaceAndReserveRejectsStaleUnhealthyAndInsufficientWorkers(t *testing.T) {
	s := newPlacementStore(t)
	now := time.Now().UTC()
	addWorker(t, s, "stale", "healthy", 32*gib, 32*gib, now.Add(-time.Minute))
	addWorker(t, s, "unhealthy", "unreachable", 32*gib, 32*gib, now)
	addWorker(t, s, "small", "healthy", gib, 2*gib, now)
	addVM(t, s, "vm-1", 2*gib)
	_, err := reserve(t, s, "vm-1", 2*gib, Policy{SafetyReserveBytes: gib / 2, StaleAfter: 30*time.Second}, now)
	if !errors.Is(err, ErrNoCapacity) { t.Fatalf("error = %v, want ErrNoCapacity", err) }
}

func TestPlaceAndReserveUsesDeterministicTieBreak(t *testing.T) {
	s := newPlacementStore(t)
	now := time.Now().UTC()
	addWorker(t, s, "worker-b", "healthy", 8*gib, 8*gib, now)
	addWorker(t, s, "worker-a", "healthy", 8*gib, 8*gib, now)
	addVM(t, s, "vm-1", gib)
	selected, err := reserve(t, s, "vm-1", gib, Policy{SafetyReserveBytes: gib, StaleAfter: 30*time.Second}, now)
	if err != nil { t.Fatal(err) }
	if selected != "worker-a" { t.Fatalf("selected %q, want lexicographic worker-a tie-break", selected) }
}

func TestConcurrentReservationsCannotExceedTrackedSafeCapacity(t *testing.T) {
	s := newPlacementStore(t)
	now := time.Now().UTC()
	addWorker(t, s, "worker-a", "healthy", 7*gib, 8*gib, now)
	addVM(t, s, "vm-1", 4*gib)
	addVM(t, s, "vm-2", 4*gib)
	policy := Policy{SafetyReserveBytes: gib, StaleAfter: 30*time.Second}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	var failures []error
	for _, id := range []string{"vm-1", "vm-2"} {
		wg.Add(1)
		go func(vmID string) {
			defer wg.Done()
			_, err := reserve(t, s, vmID, 4*gib, policy, now)
			mu.Lock()
			defer mu.Unlock()
			if err == nil { successes++ } else { failures = append(failures, err) }
		}(id)
	}
	wg.Wait()
	if successes != 1 { t.Fatalf("successful reservations = %d, want exactly 1; failures=%v", successes, failures) }
	var total int64
	if err := s.DB().QueryRow("SELECT COALESCE(SUM(memory_bytes),0) FROM reservations WHERE state IN ('pending','active')").Scan(&total); err != nil { t.Fatal(err) }
	if uint64(total) > 6*gib { t.Fatalf("reserved %d bytes exceeds safe capacity %d", total, 6*gib) }
}
