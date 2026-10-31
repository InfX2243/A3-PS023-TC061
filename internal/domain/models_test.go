package domain

import (
	"errors"
	"testing"
	"time"
)

func TestValidVMTransitions(t *testing.T) {
	tests := []struct {
		from VMState
		to   VMState
	}{
		{VMDefined, VMStopped},
		{VMStopped, VMStarting},
		{VMStarting, VMRunning},
		{VMRunning, VMStopping},
		{VMStopping, VMStopped},
		{VMStarting, VMUnknown},
		{VMUnknown, VMRunning},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			vm := &VM{ID: "vm-test", Observed: tt.from}
			now := time.Date(2026, time.October, 29, 12, 0, 0, 0, time.FixedZone("test", 3600))
			if err := TransitionVM(vm, tt.to, now); err != nil {
				t.Fatalf("expected transition to be allowed: %v", err)
			}
			if vm.Observed != tt.to {
				t.Fatalf("state = %q, want %q", vm.Observed, tt.to)
			}
			if vm.UpdatedAt.Location() != time.UTC {
				t.Fatalf("updated time location = %v, want UTC", vm.UpdatedAt.Location())
			}
		})
	}
}

func TestInvalidVMTransitions(t *testing.T) {
	tests := []struct {
		from VMState
		to   VMState
	}{
		{VMDefined, VMRunning},
		{VMStopped, VMStopping},
		{VMRunning, VMStarting},
		{VMFailed, VMRunning},
		{VMUnknown, VMStarting},
		{VMRunning, VMDefined},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			vm := &VM{ID: "vm-test", Observed: tt.from}
			if err := TransitionVM(vm, tt.to, time.Now()); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("expected ErrInvalidTransition, got %v", err)
			}
			if vm.Observed != tt.from {
				t.Fatalf("invalid transition mutated state to %q", vm.Observed)
			}
		})
	}
}

func TestOperationTransitions(t *testing.T) {
	op := &Operation{ID: "op-test", State: OperationPending}
	if err := TransitionOperation(op, OperationDispatched, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := TransitionOperation(op, OperationUnknown, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := TransitionOperation(op, OperationSucceeded, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := TransitionOperation(op, OperationDispatched, time.Now()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected terminal operation transition to fail, got %v", err)
	}
}

func TestNilTransitionTargets(t *testing.T) {
	if err := TransitionVM(nil, VMStopped, time.Now()); err == nil {
		t.Fatal("expected nil VM error")
	}
	if err := TransitionOperation(nil, OperationFailed, time.Now()); err == nil {
		t.Fatal("expected nil operation error")
	}
}
