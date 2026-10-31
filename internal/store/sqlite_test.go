package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMigrationAndRestartPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "controller.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO workers
		(id,name,endpoint,state,last_heartbeat,available_memory_bytes,memory_limit_bytes,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"worker-a", "worker-a", "http://127.0.0.1:9000", "healthy", now, 4096, 8192, now, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var name string
	if err := s.DB().QueryRowContext(ctx, "SELECT name FROM workers WHERE id = ?", "worker-a").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "worker-a" {
		t.Fatalf("persisted worker name = %q", name)
	}
	var version int
	if err := s.DB().QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
}

func TestUniqueIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO workers
		(id,name,endpoint,state,available_memory_bytes,memory_limit_bytes,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`, "w1", "w1", "http://localhost:1", "healthy", 1024, 2048, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO vms
		(id,name,worker_id,vcpus,memory_bytes,image_id,desired_state,observed_state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, "vm1", "vm1", "w1", 1, 128, "ubuntu-demo", "stopped", "stopped", now, now); err != nil {
		t.Fatal(err)
	}
	insert := func(id string) error {
		_, err := s.DB().ExecContext(ctx, `INSERT INTO operations
			(id,vm_id,type,idempotency_key,state,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?)`, id, "vm1", "prepare", "same-key", "pending", now, now)
		return err
	}
	if err := insert("op1"); err != nil {
		t.Fatal(err)
	}
	if err := insert("op2"); err == nil {
		t.Fatal("expected duplicate idempotency key to violate unique constraint")
	}
}

func TestTransactionRollsBackOnError(t *testing.T) {
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sentinel := errors.New("force rollback")
	err = s.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO workers
			(id,name,endpoint,state,available_memory_bytes,memory_limit_bytes,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?)`, "w1", "w1", "http://localhost:1", "healthy", 1024, 2048, "now", "now")
		if err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v, want sentinel", err)
	}
	var count int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM workers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("workers after rollback = %d, want 0", count)
	}
}

func TestConcurrentTransactionsSerialize(t *testing.T) {
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.WithTx(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.ExecContext(context.Background(), "SELECT 1")
				return err
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("transaction failed: %v", err)
		}
	}
}
