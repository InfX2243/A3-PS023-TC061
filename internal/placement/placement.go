// Package placement implements byte-based safe-capacity selection and atomic
// memory reservations. Call PlaceAndReserve inside the same transaction that
// creates the VM and operation records.
package placement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

var ErrNoCapacity = errors.New("no healthy worker has sufficient safe memory capacity")

type Policy struct {
	SafetyReserveBytes uint64
	StaleAfter         time.Duration
}

type candidate struct {
	id            string
	available     uint64
	limit         uint64
	reserved      uint64
	safeCapacity  uint64
}

func PlaceAndReserve(ctx context.Context, tx *sql.Tx, vmID string, memoryBytes uint64, policy Policy, now time.Time) (string, error) {
	if tx == nil { return "", errors.New("placement transaction must not be nil") }
	if vmID == "" || memoryBytes == 0 || memoryBytes > math.MaxInt64 {
		return "", errors.New("VM ID and a positive SQLite-compatible memory size are required")
	}
	if policy.StaleAfter <= 0 { return "", errors.New("worker stale threshold must be positive") }
	now = now.UTC()

	rows, err := tx.QueryContext(ctx, `SELECT id, state, last_heartbeat, available_memory_bytes, memory_limit_bytes
		FROM workers ORDER BY id`)
	if err != nil { return "", fmt.Errorf("query workers for placement: %w", err) }
	defer rows.Close()
	var candidates []candidate
	for rows.Next() {
		var id, state string
		var heartbeat sql.NullString
		var available, limit int64
		if err := rows.Scan(&id, &state, &heartbeat, &available, &limit); err != nil {
			return "", fmt.Errorf("scan worker for placement: %w", err)
		}
		if state != "healthy" || !heartbeat.Valid || available < 0 || limit <= 0 { continue }
		last, err := time.Parse(time.RFC3339Nano, heartbeat.String)
		if err != nil { continue }
		age := now.Sub(last)
		if age < 0 || age > policy.StaleAfter { continue }
		candidates = append(candidates, candidate{id: id, available: uint64(available), limit: uint64(limit)})
	}
	if err := rows.Err(); err != nil { return "", fmt.Errorf("iterate workers for placement: %w", err) }
	if err := rows.Close(); err != nil { return "", fmt.Errorf("close worker placement rows: %w", err) }

	reservedRows, err := tx.QueryContext(ctx, `SELECT worker_id, COALESCE(SUM(memory_bytes), 0)
		FROM reservations WHERE state IN ('pending', 'active') GROUP BY worker_id`)
	if err != nil { return "", fmt.Errorf("query existing reservations: %w", err) }
	reservedByWorker := make(map[string]uint64)
	for reservedRows.Next() {
		var id string
		var amount int64
		if err := reservedRows.Scan(&id, &amount); err != nil { _ = reservedRows.Close(); return "", fmt.Errorf("scan reservation total: %w", err) }
		if amount > 0 { reservedByWorker[id] = uint64(amount) }
	}
	if err := reservedRows.Err(); err != nil { _ = reservedRows.Close(); return "", fmt.Errorf("iterate reservation totals: %w", err) }
	if err := reservedRows.Close(); err != nil { return "", fmt.Errorf("close reservation rows: %w", err) }

	var best *candidate
	for i := range candidates {
		c := &candidates[i]
		c.reserved = reservedByWorker[c.id]
		availableCapacity := uint64(0)
		if c.available > policy.SafetyReserveBytes { availableCapacity = c.available - policy.SafetyReserveBytes }
		limitCapacity := uint64(0)
		if c.limit > c.reserved { limitCapacity = c.limit - c.reserved }
		c.safeCapacity = availableCapacity
		if limitCapacity < c.safeCapacity { c.safeCapacity = limitCapacity }
		if c.safeCapacity < memoryBytes { continue }
		if best == nil || c.safeCapacity > best.safeCapacity || (c.safeCapacity == best.safeCapacity && c.id < best.id) {
			best = c
		}
	}
	if best == nil { return "", ErrNoCapacity }

	if _, err := tx.ExecContext(ctx, `INSERT INTO reservations(vm_id, worker_id, memory_bytes, state, created_at, updated_at)
		VALUES (?, ?, ?, 'pending', ?, ?)`, vmID, best.id, int64(memoryBytes), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return "", fmt.Errorf("reserve memory for VM %q: %w", vmID, err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE vms SET worker_id = ?, updated_at = ? WHERE id = ?", best.id, now.Format(time.RFC3339Nano), vmID); err != nil {
		return "", fmt.Errorf("assign VM to selected worker: %w", err)
	}
	return best.id, nil
}
