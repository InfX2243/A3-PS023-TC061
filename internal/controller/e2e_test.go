package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/InfX2243/A3-PS023-TC061/internal/config"
	"github.com/InfX2243/A3-PS023-TC061/internal/controllerapi"
	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
	"github.com/InfX2243/A3-PS023-TC061/internal/store"
	"github.com/InfX2243/A3-PS023-TC061/internal/workerhealth"
)

type simulatedWorker struct {
	id string
	mu sync.Mutex
	states map[string]string
	createCount int
}

func newSimulatedWorker(id string) (*simulatedWorker, *httptest.Server) {
	w := &simulatedWorker{id:id, states:make(map[string]string)}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer worker-secret" { rw.WriteHeader(http.StatusUnauthorized); return }
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/worker/vms" {
			var req struct { OperationID string `json:"operation_id"`; VMID string `json:"vm_id"`; Name string `json:"name"`; ImageID string `json:"image_id"`; VCPUs int `json:"vcpus"`; MemoryBytes uint64 `json:"memory_bytes"` }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil { rw.WriteHeader(http.StatusBadRequest); return }
			w.mu.Lock()
			if _, exists := w.states[req.VMID]; !exists { w.createCount++ }
			w.states[req.VMID] = "Created"
			w.mu.Unlock()
			w.write(rw, req.VMID, req.OperationID, req.Name, req.ImageID, req.VCPUs, req.MemoryBytes, "Created")
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 { rw.WriteHeader(http.StatusNotFound); return }
		vmID := parts[3]
		w.mu.Lock()
		state, exists := w.states[vmID]
		if r.Method == http.MethodGet && exists {
			w.mu.Unlock()
			w.write(rw, vmID, "status", "", "ubuntu-demo", 1, 512*1024*1024, state)
			return
		}
		if !exists { w.mu.Unlock(); rw.WriteHeader(http.StatusNotFound); return }
		if strings.HasSuffix(r.URL.Path, "/start") { state = "Running"; w.states[vmID] = state }
		if strings.HasSuffix(r.URL.Path, "/stop") { state = "Shutdown"; w.states[vmID] = state }
		w.mu.Unlock()
		var req struct { OperationID string `json:"operation_id"` }
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.write(rw, vmID, req.OperationID, "", "ubuntu-demo", 1, 512*1024*1024, state)
	}))
	return w, server
}

func (w *simulatedWorker) write(rw http.ResponseWriter, vmID, opID, name, image string, vcpus int, memory uint64, state string) {
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{"worker_id":w.id,"vm_id":vmID,"operation_id":opID,"name":name,"image_id":image,"vcpus":vcpus,"memory_bytes":memory,"state":state})
}

func TestTwoWorkerAPIProvisioningJourney(t *testing.T) {
	workerA, serverA := newSimulatedWorker("worker-a")
	defer serverA.Close()
	workerB, serverB := newSimulatedWorker("worker-b")
	defer serverB.Close()
	st, err := store.Open(context.Background(), ":memory:")
	if err != nil { t.Fatal(err) }
	defer st.Close()
	cfg := config.Defaults()
	cfg.OperatorToken = "operator-secret"
	cfg.WorkerSharedSecret = "worker-secret"
	cfg.Workers = []config.WorkerEndpoint{
		{ID:"worker-a", Name:"Worker A", Endpoint:serverA.URL, MemoryLimitMiB:8192},
		{ID:"worker-b", Name:"Worker B", Endpoint:serverB.URL, MemoryLimitMiB:8192},
	}
	svc, err := NewService(context.Background(), st, cfg)
	if err != nil { t.Fatal(err) }
	for _, hb := range []workerhealth.Heartbeat{
		{WorkerID:"worker-a", Hostname:"host-a", AvailableMemoryBytes:3*1024*1024*1024, ConfiguredMemoryCapBytes:8*1024*1024*1024, CloudHypervisorHealthy:true},
		{WorkerID:"worker-b", Hostname:"host-b", AvailableMemoryBytes:7*1024*1024*1024, ConfiguredMemoryCapBytes:8*1024*1024*1024, CloudHypervisorHealthy:true},
	} {
		if err := svc.RecordWorkerHeartbeat(context.Background(), hb); err != nil { t.Fatal(err) }
	}
	mux := http.NewServeMux()
	controllerapi.NewHandler(svc, cfg.OperatorToken, controllerapi.Limits{MaxVCPUs:cfg.MaxVCPUs, MaxMemoryMiB:cfg.MaxMemoryMiB, AllowedImages:map[string]struct{}{"ubuntu-demo":{}}}).Register(mux)

	create := httptest.NewRequest(http.MethodPost, "/api/v1/vms", strings.NewReader(`{"name":"integration-vm","vcpus":1,"memory_mib":2048,"image_id":"ubuntu-demo"}`))
	create.Header.Set("Authorization", "Bearer operator-secret")
	create.Header.Set("Idempotency-Key", "integration-create-1")
	createResponse := httptest.NewRecorder()
	mux.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusAccepted { t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String()) }
	var created struct { VM domain.VM `json:"vm"`; Operation domain.Operation `json:"operation"` }
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil { t.Fatal(err) }
	if created.VM.WorkerID != "worker-b" { t.Fatalf("selected worker=%q, want worker-b", created.VM.WorkerID) }
	workerA.mu.Lock(); aCreates := workerA.createCount; workerA.mu.Unlock()
	workerB.mu.Lock(); bCreates := workerB.createCount; workerB.mu.Unlock()
	if aCreates != 0 || bCreates != 1 { t.Fatalf("worker create counts A/B=%d/%d", aCreates, bCreates) }

	for _, action := range []string{"start", "stop"} {
		request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/vms/%s/%s", created.VM.ID, action), nil)
		request.Header.Set("Authorization", "Bearer operator-secret")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted { t.Fatalf("%s status=%d body=%s", action, response.Code, response.Body.String()) }
	}
	finalVM, err := svc.GetVM(context.Background(), created.VM.ID)
	if err != nil { t.Fatal(err) }
	if finalVM.Observed != domain.VMStopped { t.Fatalf("final VM state=%q, want stopped", finalVM.Observed) }
}
