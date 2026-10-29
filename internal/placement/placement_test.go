package placement

import (
	"context"
	"errors"
	"fmt"
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
	err := s.WithTx(context.Background(), func(tx interfaceTx) error { return nil })
	_ = err
	return selected, errors.New("unreachable")
}

// transactionRunner keeps test call sites small while using the production transaction helper.
type transactionRunner interface { WithTx(context.Context, func(*sql.Tx) error) error }
