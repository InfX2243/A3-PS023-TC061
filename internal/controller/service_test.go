package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/config"
	"github.com/InfX2243/A3-PS023-TC061/internal/controllerapi"
	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
	"github.com/InfX2243/A3-PS023-TC061/internal/store"
	"github.com/InfX2243/A3-PS023-TC061/internal/workerhealth"
)

func testService(t *testing.T, endpoint string) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:")
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.Defaults()
	cfg.WorkerSharedSecret = "worker-secret"
	cfg.MemorySafetyReserveMiB = 1024
	cfg.Workers = []config.WorkerEndpoint{{ID: "worker-a", Name: "Worker A", Endpoint: endpoint, MemoryLimitMiB: 8192}}
	svc, err := NewService(context.Background(), st, cfg)
	if err != nil { t.Fatal(err) }
	if err := svc.RecordWorkerHeartbeat(context.Background(), workerhealth.Heartbeat{
		WorkerID: "worker-a", Hostname: "host-a", AvailableMemoryBytes: 8 * 1024 * 1024 * 1024,
		ConfiguredMemoryCapBytes: 8 * 1024 * 1024 * 1024, CloudHypervisorHealthy: true,
	}); err != nil { t.Fatal(err) }
	return svc, st
}

func fakeWorker(t *testing.T, delay time.Duration) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer worker-secret" { w.WriteHeader(http.StatusUnauthorized); return }
		if r.URL.Path == "/api/v1/worker/vms" {
			creates.Add(1)
			if delay > 0 { time.Sleep(delay) }
			var req struct { OperationID string `json:"operation_id"`; VMID string `json:"vm_id"`; ImageID string `json:"image_id"`; VCPUs int `json:"vcpus"`; MemoryBytes uint64 `json:"memory_bytes"` }
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"worker_id":"worker-a","vm_id":req.VMID,"operation_id":req.OperationID,"image_id":req.ImageID,"vcpus":req.VCPUs,"memory_bytes":req.MemoryBytes,"state":"Created"})
			return
		}
		state := "Running"
		if r.Method == http.MethodGet { state = "Created" }
		if strings.HasSuffix(r.URL.Path, "/stop") { state = "Shutdown" }
		vmID := strings.TrimPrefix(r.URL.Path, "/api/v1/worker/vms/")
		vmID = strings.TrimSuffix(strings.TrimSuffix(vmID, "/start"), "/stop")
		var operation struct { OperationID string `json:"operation_id"` }
		if r.Method == http.MethodPost { _ = json.NewDecoder(r.Body).Decode(&operation) }
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"worker_id":"worker-a","vm_id":vmID,"operation_id":operation.OperationID,"state":state})
	}))
	return server, &creates
}

func TestCreateVMIsIdempotentAndPersistsConfirmedOutcome(t *testing.T) {
	server, creates := fakeWorker(t, 0)
	defer server.Close()
	svc, st := testService(t, server.URL)
	req := controllerapi.CreateVMRequest{Name:"demo", VCPUs:2, MemoryMiB:1024, ImageID:"ubuntu-demo"}
	vm1, op1, err := svc.CreateVM(context.Background(), req, "idempotency-1")
	if err != nil { t.Fatalf("CreateVM: %v", err) }
	vm2, op2, err := svc.CreateVM(context.Background(), req, "idempotency-1")
	if err != nil { t.Fatalf("repeat CreateVM: %v", err) }
	if vm1.ID != vm2.ID || op1.ID != op2.ID { t.Fatalf("idempotent records differ: %q/%q and %q/%q", vm1.ID, op1.ID, vm2.ID, op2.ID) }
	if creates.Load() != 1 { t.Fatalf("worker create calls = %d, want 1", creates.Load()) }
	if vm1.Observed != domain.VMStopped || op1.State != domain.OperationSucceeded { t.Fatalf("confirmed outcome not persisted: vm=%#v op=%#v", vm1, op1) }
	var reservationState string
	if err := st.DB().QueryRow("SELECT state FROM reservations WHERE vm_id=?", vm1.ID).Scan(&reservationState); err != nil { t.Fatal(err) }
	if reservationState != "active" { t.Fatalf("reservation state = %q, want active", reservationState) }
}

func TestCreateStartStopLifecyclePersistsOutcomes(t *testing.T) {
	server, _ := fakeWorker(t, 0)
	defer server.Close()
	svc, _ := testService(t, server.URL)
	vm, _, err := svc.CreateVM(context.Background(), controllerapi.CreateVMRequest{Name:"demo", VCPUs:1, MemoryMiB:512, ImageID:"ubuntu-demo"}, "lifecycle-key")
	if err != nil { t.Fatal(err) }
	started, startOp, err := svc.StartVM(context.Background(), vm.ID)
	if err != nil { t.Fatalf("StartVM: %v", err) }
	if started.Observed != domain.VMRunning || startOp.State != domain.OperationSucceeded { t.Fatalf("start result: vm=%#v op=%#v", started, startOp) }
	stopped, stopOp, err := svc.StopVM(context.Background(), vm.ID)
	if err != nil { t.Fatalf("StopVM: %v", err) }
	if stopped.Observed != domain.VMStopped || stopOp.State != domain.OperationSucceeded { t.Fatalf("stop result: vm=%#v op=%#v", stopped, stopOp) }
	var reservation string
	if err := svc.store.DB().QueryRow("SELECT state FROM reservations WHERE vm_id=?", vm.ID).Scan(&reservation); err != nil { t.Fatal(err) }
	if reservation != "released" { t.Fatalf("stopped VM reservation=%q, want released", reservation) }
	restarted, restartOp, err := svc.StartVM(context.Background(), vm.ID)
	if err != nil { t.Fatalf("restart VM: %v", err) }
	if restarted.WorkerID != vm.WorkerID || restarted.Observed != domain.VMRunning || restartOp.State != domain.OperationSucceeded { t.Fatalf("restart result: vm=%#v op=%#v", restarted, restartOp) }
}

func TestWorkerTimeoutLeavesUnknownOperationAndReservation(t *testing.T) {
	server, _ := fakeWorker(t, 100*time.Millisecond)
	defer server.Close()
	svc, st := testService(t, server.URL)
	svc.workers = newWorkerClient("worker-secret", 10*time.Millisecond)
	vm, op, err := svc.CreateVM(context.Background(), controllerapi.CreateVMRequest{Name:"demo", VCPUs:1, MemoryMiB:512, ImageID:"ubuntu-demo"}, "timeout-key")
	if err != nil { t.Fatal(err) }
	if op.State != domain.OperationUnknown || vm.Observed != domain.VMUnknown { t.Fatalf("timeout not represented as unknown: vm=%#v op=%#v", vm, op) }
	var state string
	if err := st.DB().QueryRow("SELECT state FROM reservations WHERE vm_id=?", vm.ID).Scan(&state); err != nil { t.Fatal(err) }
	if state != "pending" { t.Fatalf("reservation state = %q, want pending while outcome is unknown", state) }
}

func TestHeartbeatRejectsUnknownWorkerAndExcessiveMemory(t *testing.T) {
	server, _ := fakeWorker(t, 0)
	defer server.Close()
	svc, _ := testService(t, server.URL)
	err := svc.RecordWorkerHeartbeat(context.Background(), workerhealth.Heartbeat{WorkerID:"not-configured", Hostname:"host", ConfiguredMemoryCapBytes:1, CloudHypervisorHealthy:true})
	if err == nil { t.Fatal("unknown worker heartbeat unexpectedly accepted") }
	err = svc.RecordWorkerHeartbeat(context.Background(), workerhealth.Heartbeat{WorkerID:"worker-a", Hostname:"host", ConfiguredMemoryCapBytes:9*1024*1024*1024, AvailableMemoryBytes:1, CloudHypervisorHealthy:true})
	if err == nil { t.Fatal("heartbeat above controller-configured memory cap unexpectedly accepted") }
}

func TestWorkerUnavailableErrorIsClassifiedAsUncertain(t *testing.T) {
	if !isUnknownWorkerOutcome(&workerCallError{Unknown:true, cause:errors.New("timeout")}) { t.Fatal("timeout should be uncertain") }
	if isUnknownWorkerOutcome(&workerCallError{Status:http.StatusBadRequest}) { t.Fatal("worker 400 should be a definitive rejection") }
}

func TestIdempotencyKeyCannotBeReusedForDifferentRequest(t *testing.T) {
	server, creates := fakeWorker(t, 0)
	defer server.Close()
	svc, _ := testService(t, server.URL)
	first := controllerapi.CreateVMRequest{Name:"demo", VCPUs:1, MemoryMiB:512, ImageID:"ubuntu-demo"}
	if _, _, err := svc.CreateVM(context.Background(), first, "same-key"); err != nil { t.Fatal(err) }
	first.MemoryMiB = 1024
	if _, _, err := svc.CreateVM(context.Background(), first, "same-key"); !errors.Is(err, controllerapi.ErrConflict) { t.Fatalf("different request with same idempotency key error = %v, want conflict", err) }
	if creates.Load() != 1 { t.Fatalf("worker create calls = %d, want 1", creates.Load()) }
}

func TestReconcileResolvesUnknownCreateFromWorkerStatus(t *testing.T) {
	server, _ := fakeWorker(t, 100*time.Millisecond)
	defer server.Close()
	svc, _ := testService(t, server.URL)
	svc.workers = newWorkerClient("worker-secret", 10*time.Millisecond)
	vm, op, err := svc.CreateVM(context.Background(), controllerapi.CreateVMRequest{Name:"demo", VCPUs:1, MemoryMiB:512, ImageID:"ubuntu-demo"}, "reconcile-key")
	if err != nil { t.Fatal(err) }
	if op.State != domain.OperationUnknown { t.Fatalf("initial operation state = %q, want unknown", op.State) }
	svc.workers = newWorkerClient("worker-secret", time.Second)
	if err := svc.Reconcile(context.Background()); err != nil { t.Fatalf("Reconcile: %v", err) }
	vm, err = svc.GetVM(context.Background(), vm.ID)
	if err != nil { t.Fatal(err) }
	op, err = svc.GetOperation(context.Background(), op.ID)
	if err != nil { t.Fatal(err) }
	if vm.Observed != domain.VMStopped || op.State != domain.OperationSucceeded { t.Fatalf("reconciled state not confirmed: vm=%#v op=%#v", vm, op) }
}

func TestReconcilePreservesUnknownWhenWorkerCannotConfirmState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer worker-secret" { w.WriteHeader(http.StatusUnauthorized); return }
		if r.Method == http.MethodGet { w.WriteHeader(http.StatusNotFound); return }
		var req struct { OperationID string `json:"operation_id"`; VMID string `json:"vm_id"`; ImageID string `json:"image_id"`; VCPUs int `json:"vcpus"`; MemoryBytes uint64 `json:"memory_bytes"` }
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"worker_id":"worker-a","vm_id":req.VMID,"operation_id":req.OperationID,"image_id":req.ImageID,"vcpus":req.VCPUs,"memory_bytes":req.MemoryBytes,"state":"Created"})
	}))
	defer server.Close()
	svc, st := testService(t, server.URL)
	vm, op, err := svc.CreateVM(context.Background(), controllerapi.CreateVMRequest{Name:"demo", VCPUs:1, MemoryMiB:512, ImageID:"ubuntu-demo"}, "unknown-worker-key")
	if err != nil { t.Fatal(err) }
	if err := st.WithTx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE operations SET state='unknown' WHERE id=?", op.ID); err != nil { return err }
		if _, err := tx.Exec("UPDATE vms SET observed_state='unknown' WHERE id=?", vm.ID); err != nil { return err }
		_, err := tx.Exec("UPDATE reservations SET state='pending' WHERE vm_id=?", vm.ID)
		return err
	}); err != nil { t.Fatal(err) }
	if err := svc.Reconcile(context.Background()); err == nil { t.Fatal("expected unresolved worker status to be reported") }
	vm, err = svc.GetVM(context.Background(), vm.ID)
	if err != nil { t.Fatal(err) }
	op, err = svc.GetOperation(context.Background(), op.ID)
	if err != nil { t.Fatal(err) }
	var reservation string
	if err := st.DB().QueryRow("SELECT state FROM reservations WHERE vm_id=?", vm.ID).Scan(&reservation); err != nil { t.Fatal(err) }
	if vm.Observed != domain.VMUnknown || op.State != domain.OperationUnknown || reservation != "pending" {
		t.Fatalf("unconfirmed outcome was altered: vm=%#v op=%#v reservation=%q", vm, op, reservation)
	}
}

func TestRegisterWorkerPersistsAndDeregistrationIsSafe(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	if err != nil { t.Fatal(err) }
	defer st.Close()
	cfg := config.Defaults()
	cfg.WorkerSharedSecret = "worker-secret"
	cfg.Workers = nil
	svc, err := NewService(ctx, st, cfg)
	if err != nil { t.Fatal(err) }
	if err := svc.RegisterWorker(ctx, "bring-your-own-1", "BYO Worker", "https://worker.example.internal:9090", 4096); err != nil {
		t.Fatalf("register worker: %v", err)
	}
	workers, err := svc.ListWorkers(ctx)
	if err != nil { t.Fatal(err) }
	if len(workers) != 1 || workers[0].ID != "bring-your-own-1" || workers[0].Name != "BYO Worker" || workers[0].State != domain.WorkerUnavailable {
		t.Fatalf("unexpected registered worker inventory: %#v", workers)
	}
	if err := svc.RegisterWorker(ctx, "bring-your-own-1", "Duplicate", "https://worker.example.internal:9090", 4096); err == nil {
		t.Fatal("expected duplicate worker ID to be rejected")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO vms
		(id,name,worker_id,vcpus,memory_bytes,image_id,desired_state,observed_state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, "vm-bound", "VM bound", "bring-your-own-1", 1, 512*1024*1024, "ubuntu-demo", "stopped", "stopped", now, now); err != nil { t.Fatal(err) }
	if err := svc.DeleteWorker(ctx, "bring-your-own-1"); err == nil {
		t.Fatal("worker with assigned VMs must not be deregistered")
	}
	if _, err := st.DB().ExecContext(ctx, "DELETE FROM vms WHERE id = ?", "vm-bound"); err != nil { t.Fatal(err) }
	if err := svc.DeleteWorker(ctx, "bring-your-own-1"); err != nil { t.Fatalf("delete unused worker registration: %v", err) }
	workers, err = svc.ListWorkers(ctx)
	if err != nil { t.Fatal(err) }
	if len(workers) != 0 { t.Fatalf("workers after deletion = %#v, want empty", workers) }
}

func TestConfiguredWorkerStartupDoesNotResetPersistedState(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	if err != nil { t.Fatal(err) }
	defer st.Close()
	cfg := config.Defaults()
	cfg.WorkerSharedSecret = "worker-secret"
	cfg.Workers = []config.WorkerEndpoint{{ID:"worker-a", Name:"Worker A", Endpoint:"http://worker-a.internal:9090", MemoryLimitMiB:4096}}
	first, err := NewService(ctx, st, cfg)
	if err != nil { t.Fatal(err) }
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.DB().ExecContext(ctx, "UPDATE workers SET state='healthy', last_heartbeat=?, available_memory_bytes=? WHERE id='worker-a'", now, 1024); err != nil { t.Fatal(err) }
	if _, err := NewService(ctx, st, cfg); err != nil { t.Fatal(err) }
	workers, err := first.ListWorkers(ctx)
	if err != nil { t.Fatal(err) }
	if len(workers) != 1 || workers[0].State != domain.WorkerHealthy || workers[0].AvailableMemoryBytes != 1024 {
		t.Fatalf("startup reset persisted worker state: %#v", workers)
	}
}

func TestHeartbeatAutoEnrollsWorkerWithAdvertisedEndpoint(t *testing.T) {
	server, _ := fakeWorker(t, 0)
	defer server.Close()
	svc, _ := testService(t, server.URL)
	heartbeat := workerhealth.Heartbeat{
		WorkerID: "i-worker123", Hostname: "ip-10-0-1-12",
		Endpoint: "http://10.0.1.12:9090",
		AvailableMemoryBytes: 2 * 1024 * 1024 * 1024,
		ConfiguredMemoryCapBytes: 4 * 1024 * 1024 * 1024,
		CloudHypervisorHealthy: true,
	}
	if err := svc.RecordWorkerHeartbeat(context.Background(), heartbeat); err != nil { t.Fatalf("auto-enroll heartbeat: %v", err) }
	workers, err := svc.ListWorkers(context.Background())
	if err != nil { t.Fatal(err) }
	var found *domain.Worker
	for i := range workers { if workers[i].ID == heartbeat.WorkerID { found = &workers[i]; break } }
	if found == nil { t.Fatal("auto-enrolled worker missing from durable inventory") }
	if found.Name != heartbeat.Hostname || found.Endpoint != heartbeat.Endpoint || found.State != domain.WorkerHealthy { t.Fatalf("auto-enrolled worker metadata = %#v", found) }
	if found.AvailableMemoryBytes != heartbeat.AvailableMemoryBytes || found.MemoryLimitBytes != heartbeat.ConfiguredMemoryCapBytes { t.Fatalf("auto-enrolled capacity = %#v", found) }
}

func TestAutoEnrollmentRejectsMissingOrUnsafeEndpoint(t *testing.T) {
	server, _ := fakeWorker(t, 0)
	defer server.Close()
	svc, _ := testService(t, server.URL)
	base := workerhealth.Heartbeat{WorkerID: "i-worker456", Hostname: "node", AvailableMemoryBytes: 1024, ConfiguredMemoryCapBytes: 2048, CloudHypervisorHealthy: true}
	if err := svc.RecordWorkerHeartbeat(context.Background(), base); err == nil { t.Fatal("expected missing endpoint to be rejected") }
	base.Endpoint = "http://user:password@10.0.1.15:9090"
	if err := svc.RecordWorkerHeartbeat(context.Background(), base); err == nil { t.Fatal("expected credential-bearing endpoint to be rejected") }
	base.Endpoint = "http://198.51.100.15:9090"
	if err := svc.RecordWorkerHeartbeat(context.Background(), base); err == nil { t.Fatal("expected public endpoint to be rejected") }
}
