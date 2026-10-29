package domain

import (
	"errors"
	"fmt"
	"time"
)

type VMState string

const (
	VMDefined  VMState = "defined"
	VMStopped  VMState = "stopped"
	VMStarting VMState = "starting"
	VMRunning  VMState = "running"
	VMStopping VMState = "stopping"
	VMFailed   VMState = "failed"
	VMUnknown  VMState = "unknown"
)

type WorkerState string

const (
	WorkerHealthy     WorkerState = "healthy"
	WorkerUnavailable WorkerState = "unavailable"
	WorkerStale       WorkerState = "stale"
)

type OperationType string

const (
	OperationPrepare OperationType = "prepare"
	OperationStart   OperationType = "start"
	OperationStop    OperationType = "stop"
)

type OperationState string

const (
	OperationPending    OperationState = "pending"
	OperationDispatched OperationState = "dispatched"
	OperationSucceeded  OperationState = "succeeded"
	OperationFailed     OperationState = "failed"
	OperationUnknown    OperationState = "unknown"
)

type VM struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	WorkerID    string    `json:"worker_id,omitempty"`
	VCPUs       int       `json:"vcpus"`
	MemoryBytes uint64    `json:"memory_bytes"`
	ImageID     string    `json:"image_id"`
	Desired     VMState   `json:"desired_state"`
	Observed    VMState   `json:"observed_state"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Worker struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	Endpoint             string      `json:"endpoint"`
	State                WorkerState `json:"state"`
	LastHeartbeat        time.Time   `json:"last_heartbeat"`
	AvailableMemoryBytes uint64      `json:"available_memory_bytes"`
	MemoryLimitBytes     uint64      `json:"memory_limit_bytes"`
}

type ReservationState string

const (
	ReservationPending  ReservationState = "pending"
	ReservationActive   ReservationState = "active"
	ReservationReleased ReservationState = "released"
)

type Reservation struct {
	VMID        string           `json:"vm_id"`
	WorkerID    string           `json:"worker_id"`
	MemoryBytes uint64           `json:"memory_bytes"`
	State       ReservationState `json:"state"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

type Operation struct {
	ID             string         `json:"id"`
	VMID           string         `json:"vm_id"`
	Type           OperationType  `json:"type"`
	IdempotencyKey string         `json:"-"`
	State          OperationState `json:"state"`
	Attempt        uint32         `json:"attempt"`
	ErrorCode      string         `json:"error_code,omitempty"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

var ErrInvalidTransition = errors.New("invalid state transition")

// CanTransition reports whether a VM lifecycle transition is allowed.
// Unknown and failed states can only be moved by explicit reconciliation/reset
// logic, rather than ordinary start/stop requests.
func CanTransition(from, to VMState) bool {
	switch from {
	case VMDefined:
		return to == VMStopped || to == VMFailed || to == VMUnknown
	case VMStopped:
		return to == VMStarting || to == VMFailed || to == VMUnknown
	case VMStarting:
		return to == VMRunning || to == VMStopped || to == VMFailed || to == VMUnknown
	case VMRunning:
		return to == VMStopping || to == VMFailed || to == VMUnknown
	case VMStopping:
		return to == VMStopped || to == VMRunning || to == VMFailed || to == VMUnknown
	case VMFailed:
		return to == VMUnknown
	case VMUnknown:
		return to == VMDefined || to == VMStopped || to == VMRunning || to == VMFailed
	default:
		return false
	}
}

func TransitionVM(vm *VM, next VMState, now time.Time) error {
	if vm == nil {
		return errors.New("VM must not be nil")
	}
	if !CanTransition(vm.Observed, next) {
		return fmt.Errorf("%w: VM %q cannot move from %q to %q", ErrInvalidTransition, vm.ID, vm.Observed, next)
	}
	vm.Observed = next
	vm.UpdatedAt = now.UTC()
	return nil
}

func CanTransitionOperation(from, to OperationState) bool {
	switch from {
	case OperationPending:
		return to == OperationDispatched || to == OperationFailed
	case OperationDispatched:
		return to == OperationSucceeded || to == OperationFailed || to == OperationUnknown
	case OperationUnknown:
		return to == OperationSucceeded || to == OperationFailed
	default:
		return false
	}
}

func TransitionOperation(op *Operation, next OperationState, now time.Time) error {
	if op == nil {
		return errors.New("operation must not be nil")
	}
	if !CanTransitionOperation(op.State, next) {
		return fmt.Errorf("%w: operation %q cannot move from %q to %q", ErrInvalidTransition, op.ID, op.State, next)
	}
	op.State = next
	op.UpdatedAt = now.UTC()
	return nil
}
