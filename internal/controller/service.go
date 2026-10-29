package controller

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/config"
	"github.com/InfX2243/A3-PS023-TC061/internal/controllerapi"
	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
	"github.com/InfX2243/A3-PS023-TC061/internal/placement"
	"github.com/InfX2243/A3-PS023-TC061/internal/store"
	"github.com/InfX2243/A3-PS023-TC061/internal/workerhealth"
)

type Service struct {
	store *store.Store
	cfg config.Config
	workers *workerClient
	now func() time.Time
}

func NewService(ctx context.Context, st *store.Store, cfg config.Config) (*Service, error) {
	if st == nil { return nil, errors.New("controller store must not be nil") }
	if len(cfg.Workers) > 0 && cfg.WorkerSharedSecret == "" { return nil, errors.New("VM_CONTROLLER_WORKER_SHARED_SECRET is required when workers are configured") }
	s := &Service{store: st, cfg: cfg, workers: newWorkerClient(cfg.WorkerSharedSecret, 5*time.Second), now: time.Now}
	if err := s.ensureConfiguredWorkers(ctx); err != nil { return nil, err }
	return s, nil
}

func (s *Service) ensureConfiguredWorkers(ctx context.Context) error {
	now := s.now().UTC().Format(time.RFC3339Nano)
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		for _, worker := range s.cfg.Workers {
			limit := worker.MemoryLimitMiB * 1024 * 1024
			_, err := tx.ExecContext(ctx, `INSERT INTO workers(id, name, endpoint, state, last_heartbeat, available_memory_bytes, memory_limit_bytes, created_at, updated_at)
				VALUES (?, ?, ?, 'unreachable', NULL, 0, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET name=excluded.name, endpoint=excluded.endpoint,
					memory_limit_bytes=excluded.memory_limit_bytes, updated_at=excluded.updated_at`,
				worker.ID, worker.Name, strings.TrimRight(worker.Endpoint, "/"), int64(limit), now, now)
			if err != nil { return fmt.Errorf("register configured worker %q: %w", worker.ID, err) }
		}
		return nil
	})
}

func (s *Service) ListWorkers(ctx context.Context) ([]domain.Worker, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT id, name, endpoint, state, last_heartbeat,
		available_memory_bytes, memory_limit_bytes FROM workers ORDER BY id`)
	if err != nil { return nil, fmt.Errorf("list workers: %w", err) }
	defer rows.Close()
	var result []domain.Worker
	for rows.Next() {
		var w domain.Worker
		var state string
		var heartbeat sql.NullString
		var available, limit int64
		if err := rows.Scan(&w.ID, &w.Name, &w.Endpoint, &state, &heartbeat, &available, &limit); err != nil { return nil, fmt.Errorf("scan worker: %w", err) }
		w.State = workerState(state)
		if heartbeat.Valid {
			w.LastHeartbeat, _ = time.Parse(time.RFC3339Nano, heartbeat.String)
			age := s.now().UTC().Sub(w.LastHeartbeat)
			if age < 0 { w.State = domain.WorkerUnavailable } else if age > 30*time.Second && w.State == domain.WorkerHealthy { w.State = domain.WorkerStale }
		}
		if available > 0 { w.AvailableMemoryBytes = uint64(available) }
		if limit > 0 { w.MemoryLimitBytes = uint64(limit) }
		result = append(result, w)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("iterate workers: %w", err) }
	if result == nil { result = []domain.Worker{} }
	return result, nil
}

func workerState(value string) domain.WorkerState {
	switch value {
	case "healthy": return domain.WorkerHealthy
	case "stale": return domain.WorkerStale
	default: return domain.WorkerUnavailable
	}
}


func (s *Service) RecordWorkerHeartbeat(ctx context.Context, heartbeat workerhealth.Heartbeat) error {
	now := s.now().UTC()
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		var configuredLimit int64
		err := tx.QueryRowContext(ctx, "SELECT memory_limit_bytes FROM workers WHERE id = ?", heartbeat.WorkerID).Scan(&configuredLimit)
		if errors.Is(err, sql.ErrNoRows) { return fmt.Errorf("unconfigured worker %q", heartbeat.WorkerID) }
		if err != nil { return fmt.Errorf("load configured worker limit: %w", err) }
		if configuredLimit <= 0 || heartbeat.ConfiguredMemoryCapBytes == 0 || heartbeat.ConfiguredMemoryCapBytes != uint64(configuredLimit) { return fmt.Errorf("worker %q reported a configured memory cap that does not match controller configuration", heartbeat.WorkerID) }
		if heartbeat.AvailableMemoryBytes > uint64(configuredLimit) { return fmt.Errorf("worker %q reported available memory above its controller-configured limit", heartbeat.WorkerID) }
		state := "unreachable"
		if heartbeat.CloudHypervisorHealthy { state = "healthy" }
		_, err = tx.ExecContext(ctx, "UPDATE workers SET state=?, last_heartbeat=?, available_memory_bytes=?, updated_at=? WHERE id=?", state, now.Format(time.RFC3339Nano), int64(heartbeat.AvailableMemoryBytes), now.Format(time.RFC3339Nano), heartbeat.WorkerID)
		if err != nil { return fmt.Errorf("persist worker heartbeat: %w", err) }
		return nil
	})
}

func (s *Service) ListVMs(ctx context.Context) ([]domain.VM, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT id, name, worker_id, vcpus, memory_bytes, image_id,
		desired_state, observed_state, created_at, updated_at FROM vms ORDER BY created_at, id`)
	if err != nil { return nil, fmt.Errorf("list VMs: %w", err) }
	defer rows.Close()
	var result []domain.VM
	for rows.Next() {
		vm, err := scanVM(rows)
		if err != nil { return nil, err }
		result = append(result, vm)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("iterate VMs: %w", err) }
	if result == nil { result = []domain.VM{} }
	return result, nil
}

func (s *Service) GetVM(ctx context.Context, id string) (domain.VM, error) {
	vm, err := scanVM(s.store.DB().QueryRowContext(ctx, `SELECT id, name, worker_id, vcpus, memory_bytes, image_id,
		desired_state, observed_state, created_at, updated_at FROM vms WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) { return domain.VM{}, controllerapi.ErrNotFound }
	return vm, err
}

type rowScanner interface { Scan(...any) error }

func scanVM(row rowScanner) (domain.VM, error) {
	var vm domain.VM
	var workerID sql.NullString
	var memory int64
	var desired, observed, created, updated string
	if err := row.Scan(&vm.ID, &vm.Name, &workerID, &vm.VCPUs, &memory, &vm.ImageID, &desired, &observed, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) { return domain.VM{}, sql.ErrNoRows }
		return domain.VM{}, fmt.Errorf("scan VM record: %w", err)
	}
	if workerID.Valid { vm.WorkerID = workerID.String }
	if memory > 0 { vm.MemoryBytes = uint64(memory) }
	vm.Desired, vm.Observed = domain.VMState(desired), domain.VMState(observed)
	vm.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	vm.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return vm, nil
}

func scanOperation(row rowScanner) (domain.Operation, error) {
	var op domain.Operation
	var key, code, message sql.NullString
	var state, typ, created, updated string
	var attempt int64
	if err := row.Scan(&op.ID, &op.VMID, &typ, &key, &state, &attempt, &code, &message, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) { return domain.Operation{}, sql.ErrNoRows }
		return domain.Operation{}, fmt.Errorf("scan operation record: %w", err)
	}
	op.Type, op.State, op.Attempt = domain.OperationType(typ), domain.OperationState(state), uint32(max(attempt, 0))
	if key.Valid { op.IdempotencyKey = key.String }
	if code.Valid { op.ErrorCode = code.String }
	if message.Valid { op.ErrorMessage = message.String }
	op.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	op.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return op, nil
}

const operationColumns = "id, vm_id, type, idempotency_key, state, attempt, error_code, error_message, created_at, updated_at"


func (s *Service) LatestOperation(ctx context.Context, vmID string) (domain.Operation, error) {
	op, err := scanOperation(s.store.DB().QueryRowContext(ctx, "SELECT "+operationColumns+" FROM operations WHERE vm_id=? ORDER BY created_at DESC, id DESC LIMIT 1", vmID))
	if errors.Is(err, sql.ErrNoRows) { return domain.Operation{}, controllerapi.ErrNotFound }
	return op, err
}

func (s *Service) GetOperation(ctx context.Context, id string) (domain.Operation, error) {
	op, err := scanOperation(s.store.DB().QueryRowContext(ctx, "SELECT "+operationColumns+" FROM operations WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) { return domain.Operation{}, controllerapi.ErrNotFound }
	return op, err
}

func (s *Service) CreateVM(ctx context.Context, req controllerapi.CreateVMRequest, key string) (domain.VM, domain.Operation, error) {
	if req.MemoryMiB > uint64(1<<63-1)/(1024*1024) { return domain.VM{}, domain.Operation{}, controllerapi.ErrInvalid }
	memoryBytes := req.MemoryMiB * 1024 * 1024
	vmID, opID := newID("vm"), newID("op")
	now := s.now().UTC()
	var vm domain.VM
	var op domain.Operation
	var endpoint string
	existing := false
	err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
		var existingVMID string
		err := tx.QueryRowContext(ctx, "SELECT vm_id FROM operations WHERE idempotency_key = ?", key).Scan(&existingVMID)
		if err == nil {
			existing = true
			var err error
			vm, err = scanVM(tx.QueryRowContext(ctx, `SELECT id, name, worker_id, vcpus, memory_bytes, image_id, desired_state, observed_state, created_at, updated_at FROM vms WHERE id = ?`, existingVMID))
			if err != nil { return err }
			op, err = scanOperation(tx.QueryRowContext(ctx, "SELECT "+operationColumns+" FROM operations WHERE idempotency_key = ?", key))
			if err != nil { return err }
			if vm.Name != req.Name || vm.VCPUs != req.VCPUs || vm.MemoryBytes != memoryBytes || vm.ImageID != req.ImageID { return controllerapi.ErrConflict }
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) { return fmt.Errorf("check idempotency key: %w", err) }
		timestamp := now.Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO vms(id, name, worker_id, vcpus, memory_bytes, image_id, desired_state, observed_state, created_at, updated_at)
			VALUES (?, ?, NULL, ?, ?, ?, 'stopped', 'defined', ?, ?)`, vmID, req.Name, req.VCPUs, int64(memoryBytes), req.ImageID, timestamp, timestamp); err != nil {
			return fmt.Errorf("create VM record: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id, vm_id, type, idempotency_key, state, attempt, created_at, updated_at)
			VALUES (?, ?, 'prepare', ?, 'pending', 0, ?, ?)`, opID, vmID, key, timestamp, timestamp); err != nil {
			return fmt.Errorf("create operation record: %w", err)
		}
		workerID, err := placement.PlaceAndReserve(ctx, tx, vmID, memoryBytes, placement.Policy{SafetyReserveBytes: s.cfg.MemorySafetyReserveMiB * 1024 * 1024, StaleAfter: 30*time.Second}, now)
		if errors.Is(err, placement.ErrNoCapacity) { return controllerapi.ErrUnavailable }
		if err != nil { return err }
		if err := tx.QueryRowContext(ctx, "SELECT endpoint FROM workers WHERE id = ?", workerID).Scan(&endpoint); err != nil { return fmt.Errorf("load selected worker endpoint: %w", err) }
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='dispatched', attempt=1, updated_at=? WHERE id=?", timestamp, opID); err != nil { return fmt.Errorf("persist operation before dispatch: %w", err) }
		vm, err = scanVM(tx.QueryRowContext(ctx, `SELECT id, name, worker_id, vcpus, memory_bytes, image_id, desired_state, observed_state, created_at, updated_at FROM vms WHERE id = ?`, vmID))
		if err != nil { return err }
		op, err = scanOperation(tx.QueryRowContext(ctx, "SELECT "+operationColumns+" FROM operations WHERE id = ?", opID))
		return err
	})
	if err != nil { return domain.VM{}, domain.Operation{}, err }
	if existing { return vm, op, nil }

	report, callErr := s.workers.prepare(ctx, endpoint, workerVMRequest{OperationID: opID, VMID: vm.ID, Name: vm.Name, VCPUs: vm.VCPUs, MemoryBytes: vm.MemoryBytes, ImageID: vm.ImageID})
	switch {
	case callErr == nil && report.State == "Created":
		err = s.finishOperation(ctx, vm.ID, op.ID, domain.OperationSucceeded, domain.VMStopped, domain.VMStopped, "active", "", "")
	case isUnknownWorkerOutcome(callErr) || (callErr == nil && report.State != "Created"):
		err = s.finishOperation(ctx, vm.ID, op.ID, domain.OperationUnknown, domain.VMStopped, domain.VMUnknown, "pending", "outcome_unknown", "worker outcome requires reconciliation")
	default:
		err = s.finishOperation(ctx, vm.ID, op.ID, domain.OperationFailed, domain.VMStopped, domain.VMFailed, "released", "worker_rejected", "worker did not create the VM")
	}
	if err != nil { return domain.VM{}, domain.Operation{}, err }
	vm, err = s.GetVM(ctx, vm.ID)
	if err != nil { return domain.VM{}, domain.Operation{}, err }
	op, err = s.GetOperation(ctx, op.ID)
	return vm, op, err
}

func (s *Service) StartVM(ctx context.Context, vmID string) (domain.VM, domain.Operation, error) {
	opID := newID("op")
	now := s.now().UTC()
	var vm domain.VM
	var op domain.Operation
	var endpoint string
	err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		vm, err = scanVM(tx.QueryRowContext(ctx, `SELECT id, name, worker_id, vcpus, memory_bytes, image_id, desired_state, observed_state, created_at, updated_at FROM vms WHERE id = ?`, vmID))
		if errors.Is(err, sql.ErrNoRows) { return controllerapi.ErrNotFound }
		if err != nil { return err }
		if vm.Observed == domain.VMRunning {
			op, err = latestOperationTx(ctx, tx, vmID)
			return err
		}
		if vm.Observed != domain.VMStopped && vm.Observed != domain.VMDefined { return controllerapi.ErrConflict }
		var reservationState sql.NullString
		err = tx.QueryRowContext(ctx, "SELECT state FROM reservations WHERE vm_id = ?", vmID).Scan(&reservationState)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && reservationState.String == "released") {
			workerID, reserveErr := placement.PlaceAndReserve(ctx, tx, vmID, vm.MemoryBytes, placement.Policy{SafetyReserveBytes: s.cfg.MemorySafetyReserveMiB * 1024 * 1024, StaleAfter: 30*time.Second}, now)
			if errors.Is(reserveErr, placement.ErrNoCapacity) { return controllerapi.ErrUnavailable }
			if reserveErr != nil { return reserveErr }
			vm.WorkerID = workerID
		} else if err != nil { return fmt.Errorf("read VM reservation: %w", err) }
		if vm.WorkerID == "" { return controllerapi.ErrUnavailable }
		if err := tx.QueryRowContext(ctx, "SELECT endpoint FROM workers WHERE id = ?", vm.WorkerID).Scan(&endpoint); err != nil { return controllerapi.ErrUnavailable }
		timestamp := now.Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id, vm_id, type, state, attempt, created_at, updated_at)
			VALUES (?, ?, 'start', 'dispatched', 1, ?, ?)`, opID, vmID, timestamp, timestamp); err != nil { return fmt.Errorf("create start operation: %w", err) }
		if _, err := tx.ExecContext(ctx, "UPDATE vms SET desired_state='starting', observed_state='starting', worker_id=?, updated_at=? WHERE id=?", vm.WorkerID, timestamp, vmID); err != nil { return err }
		vm, err = scanVM(tx.QueryRowContext(ctx, `SELECT id, name, worker_id, vcpus, memory_bytes, image_id, desired_state, observed_state, created_at, updated_at FROM vms WHERE id = ?`, vmID))
		if err != nil { return err }
		op, err = scanOperation(tx.QueryRowContext(ctx, "SELECT "+operationColumns+" FROM operations WHERE id = ?", opID))
		return err
	})
	if err != nil { return domain.VM{}, domain.Operation{}, err }
	if op.ID != opID { return vm, op, nil }
	report, callErr := s.workers.start(ctx, endpoint, vmID, opID)
	switch {
	case callErr == nil && report.State == "Running":
		err = s.finishOperation(ctx, vmID, opID, domain.OperationSucceeded, domain.VMRunning, domain.VMRunning, "active", "", "")
	case isUnknownWorkerOutcome(callErr) || (callErr == nil && report.State != "Running"):
		err = s.finishOperation(ctx, vmID, opID, domain.OperationUnknown, domain.VMRunning, domain.VMUnknown, "pending", "outcome_unknown", "worker outcome requires reconciliation")
	default:
		err = s.finishOperation(ctx, vmID, opID, domain.OperationFailed, domain.VMStopped, domain.VMStopped, "active", "worker_rejected", "worker did not confirm VM start")
	}
	if err != nil { return domain.VM{}, domain.Operation{}, err }
	vm, err = s.GetVM(ctx, vmID); if err != nil { return domain.VM{}, domain.Operation{}, err }
	op, err = s.GetOperation(ctx, opID)
	return vm, op, err
}

func (s *Service) StopVM(ctx context.Context, vmID string) (domain.VM, domain.Operation, error) {
	opID := newID("op")
	now := s.now().UTC()
	var vm domain.VM
	var op domain.Operation
	var endpoint string
	err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		vm, err = scanVM(tx.QueryRowContext(ctx, `SELECT id, name, worker_id, vcpus, memory_bytes, image_id, desired_state, observed_state, created_at, updated_at FROM vms WHERE id = ?`, vmID))
		if errors.Is(err, sql.ErrNoRows) { return controllerapi.ErrNotFound }
		if err != nil { return err }
		if vm.Observed == domain.VMStopped {
			op, err = latestOperationTx(ctx, tx, vmID)
			return err
		}
		if vm.Observed != domain.VMRunning { return controllerapi.ErrConflict }
		if vm.WorkerID == "" { return controllerapi.ErrUnavailable }
		if err := tx.QueryRowContext(ctx, "SELECT endpoint FROM workers WHERE id = ?", vm.WorkerID).Scan(&endpoint); err != nil { return controllerapi.ErrUnavailable }
		timestamp := now.Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id, vm_id, type, state, attempt, created_at, updated_at)
			VALUES (?, ?, 'stop', 'dispatched', 1, ?, ?)`, opID, vmID, timestamp, timestamp); err != nil { return fmt.Errorf("create stop operation: %w", err) }
		if _, err := tx.ExecContext(ctx, "UPDATE vms SET desired_state='stopped', observed_state='stopping', updated_at=? WHERE id=?", timestamp, vmID); err != nil { return err }
		vm, err = scanVM(tx.QueryRowContext(ctx, `SELECT id, name, worker_id, vcpus, memory_bytes, image_id, desired_state, observed_state, created_at, updated_at FROM vms WHERE id = ?`, vmID))
		if err != nil { return err }
		op, err = scanOperation(tx.QueryRowContext(ctx, "SELECT "+operationColumns+" FROM operations WHERE id = ?", opID))
		return err
	})
	if err != nil { return domain.VM{}, domain.Operation{}, err }
	if op.ID != opID { return vm, op, nil }
	report, callErr := s.workers.stop(ctx, endpoint, vmID, opID)
	switch {
	case callErr == nil && report.State == "Shutdown":
		err = s.finishOperation(ctx, vmID, opID, domain.OperationSucceeded, domain.VMStopped, domain.VMStopped, "active", "", "")
	case isUnknownWorkerOutcome(callErr) || (callErr == nil && report.State != "Shutdown"):
		err = s.finishOperation(ctx, vmID, opID, domain.OperationUnknown, domain.VMStopped, domain.VMUnknown, "active", "outcome_unknown", "worker outcome requires reconciliation")
	default:
		err = s.finishOperation(ctx, vmID, opID, domain.OperationFailed, domain.VMRunning, domain.VMRunning, "active", "worker_rejected", "worker did not confirm VM stop")
	}
	if err != nil { return domain.VM{}, domain.Operation{}, err }
	vm, err = s.GetVM(ctx, vmID); if err != nil { return domain.VM{}, domain.Operation{}, err }
	op, err = s.GetOperation(ctx, opID)
	return vm, op, err
}

func latestOperationTx(ctx context.Context, tx *sql.Tx, vmID string) (domain.Operation, error) {
	op, err := scanOperation(tx.QueryRowContext(ctx, "SELECT "+operationColumns+" FROM operations WHERE vm_id=? ORDER BY created_at DESC, id DESC LIMIT 1", vmID))
	if errors.Is(err, sql.ErrNoRows) { return domain.Operation{}, controllerapi.ErrConflict }
	return op, err
}

func (s *Service) finishOperation(ctx context.Context, vmID, opID string, opState domain.OperationState, desired, observed domain.VMState, reservationState, code, message string) error {
	now := s.now().UTC().Format(time.RFC3339Nano)
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state=?, error_code=NULLIF(?,''), error_message=NULLIF(?,''), updated_at=? WHERE id=?", string(opState), code, message, now, opID); err != nil { return fmt.Errorf("persist operation outcome: %w", err) }
		if _, err := tx.ExecContext(ctx, "UPDATE vms SET desired_state=?, observed_state=?, updated_at=? WHERE id=?", string(desired), string(observed), now, vmID); err != nil { return fmt.Errorf("persist VM outcome: %w", err) }
		if reservationState != "" {
			if _, err := tx.ExecContext(ctx, "UPDATE reservations SET state=?, updated_at=? WHERE vm_id=?", reservationState, now, vmID); err != nil { return fmt.Errorf("persist reservation outcome: %w", err) }
		}
		return nil
	})
}

func isUnknownWorkerOutcome(err error) bool {
	if err == nil { return false }
	var callErr *workerCallError
	if errors.As(err, &callErr) { return callErr.Unknown }
	return errors.Is(err, controllerapi.ErrUnavailable)
}

func newID(prefix string) string {
	const hexChars = "0123456789abcdef"
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UTC().UnixNano())
	}
	out := make([]byte, len(raw)*2)
	for i, b := range raw { out[2*i] = hexChars[b>>4]; out[2*i+1] = hexChars[b&0x0f] }
	return prefix + "-" + string(out)
}
