package workeragent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/cloudhypervisor"
)


type customRuntime struct { hv Hypervisor; proc Process }
func (r customRuntime) Launch(context.Context, string, string, time.Duration, time.Duration) (Hypervisor, Process, error) { return r.hv, r.proc, nil }

type failedRuntime struct{ err error }
func (r failedRuntime) Launch(context.Context, string, string, time.Duration, time.Duration) (Hypervisor, Process, error) {
	return nil, nil, r.err
}

type blockingHypervisor struct{ fakeHypervisor }
func (h *blockingHypervisor) Start(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

type failedStatusHypervisor struct{ fakeHypervisor }
func (h *failedStatusHypervisor) Status(context.Context) (cloudhypervisor.StatusResponse, error) {
	return cloudhypervisor.StatusResponse{}, errors.New("simulated VMM process exit")
}

func TestPrepareReturnsActionableErrorWhenVMMUnavailable(t *testing.T) {
	cfg := Config{
		WorkerID: "worker-a", RuntimeDir: filepath.Join(t.TempDir(), "runtime"),
		CloudHypervisor: "/usr/bin/cloud-hypervisor", MaxVCPUs: 8, MaxMemoryBytes: 8 << 30,
		Images: []Image{{ID: "ubuntu-demo", KernelPath: "/images/vmlinuz"}},
		RequestTimeout: time.Second, StartupTimeout: time.Second,
	}
	manager, err := NewManagerWithRuntime(cfg, failedRuntime{err: errors.New("binary unavailable")})
	if err != nil { t.Fatal(err) }
	defer manager.Close()
	if _, err := manager.Prepare(context.Background(), validCreate()); err == nil || !strings.Contains(err.Error(), "launch VMM") {
		t.Fatalf("Prepare error = %v, want VMM launch failure", err)
	}
}

func TestStartTimeoutIsReturnedWithoutClaimingRunning(t *testing.T) {
	hv := &blockingHypervisor{}
	runtime := customRuntime{hv: hv, proc: &fakeProcess{}}
	cfg := Config{
		WorkerID: "worker-a", RuntimeDir: filepath.Join(t.TempDir(), "runtime"),
		CloudHypervisor: "/usr/bin/cloud-hypervisor", MaxVCPUs: 8, MaxMemoryBytes: 8 << 30,
		Images: []Image{{ID: "ubuntu-demo", KernelPath: "/images/vmlinuz"}},
		RequestTimeout: 10 * time.Millisecond, StartupTimeout: time.Second,
	}
	manager, err := NewManagerWithRuntime(cfg, runtime)
	if err != nil { t.Fatal(err) }
	defer manager.Close()
	req := validCreate()
	if _, err := manager.Prepare(context.Background(), req); err != nil { t.Fatal(err) }
	if _, err := manager.Start(context.Background(), "op-start", req.VMID); err == nil {
		t.Fatal("Start unexpectedly succeeded after its deadline")
	}
	report, err := manager.Status(context.Background(), req.VMID)
	if err != nil { t.Fatal(err) }
	if report.State == string(cloudhypervisor.StatusRunning) {
		t.Fatalf("timed-out start incorrectly reported running: %#v", report)
	}
}

func TestStatusSurfacesVMMProcessExit(t *testing.T) {
	hv := &failedStatusHypervisor{}
	runtime := customRuntime{hv: hv, proc: &fakeProcess{}}
	manager, err := NewManagerWithRuntime(Config{
		WorkerID: "worker-a", RuntimeDir: filepath.Join(t.TempDir(), "runtime"),
		CloudHypervisor: "/usr/bin/cloud-hypervisor", MaxVCPUs: 8, MaxMemoryBytes: 8 << 30,
		Images: []Image{{ID: "ubuntu-demo", KernelPath: "/images/vmlinuz"}},
		RequestTimeout: time.Second, StartupTimeout: time.Second,
	}, runtime)
	if err != nil { t.Fatal(err) }
	defer manager.Close()
	if _, err := manager.Prepare(context.Background(), validCreate()); err != nil { t.Fatal(err) }
	if _, err := manager.Status(context.Background(), "vm-1"); err == nil || !strings.Contains(err.Error(), "query VM status") {
		t.Fatalf("Status error = %v, want VMM process-exit error", err)
	}
}

func TestHTTPRejectsInvalidImageBeforeLaunchingVMM(t *testing.T) {
	manager, runtime := testManager(t)
	defer manager.Close()
	mux := http.NewServeMux()
	NewHandler(manager, "secret").Register(mux)
	req := validCreate()
	req.ImageID = "/tmp/untrusted.img"
	body, err := json.Marshal(req)
	if err != nil { t.Fatal(err) }
	request := httptest.NewRequest(http.MethodPost, "/api/v1/worker/vms", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity { t.Fatalf("status = %d, want 422; body=%s", response.Code, response.Body.String()) }
	if runtime.launches != 0 { t.Fatalf("invalid image launched %d VMMs", runtime.launches) }
}

func TestManagerCloseKillsAndReapsOwnedVMMForWorkerRestart(t *testing.T) {
	manager, runtime := testManager(t)
	if _, err := manager.Prepare(context.Background(), validCreate()); err != nil { t.Fatal(err) }
	if err := manager.Close(); err != nil { t.Fatalf("Close: %v", err) }
	runtime.proc.mu.Lock()
	killed, waited := runtime.proc.killed, runtime.proc.waited
	runtime.proc.mu.Unlock()
	if !killed || !waited { t.Fatalf("worker shutdown did not clean up child process: killed=%t waited=%t", killed, waited) }
	// The next worker process starts with an empty in-memory registry. Controller
	// reconciliation must treat old metadata as unconfirmed rather than assume a VM
	// is running; durable worker inventory/re-attachment is intentionally a later task.
	nextManager, _ := testManager(t)
	defer nextManager.Close()
	if _, err := nextManager.Status(context.Background(), "vm-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fresh worker status error = %v, want ErrNotFound until reconciled", err)
	}
}
