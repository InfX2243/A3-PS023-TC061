package workeragent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/cloudhypervisor"
)

type fakeRuntime struct {
	mu sync.Mutex
	hv *fakeHypervisor
	proc *fakeProcess
	launches int
}

func (r *fakeRuntime) Launch(context.Context, string, string, time.Duration, time.Duration) (Hypervisor, Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.launches++
	return r.hv, r.proc, nil
}

type fakeHypervisor struct {
	mu sync.Mutex
	state cloudhypervisor.VMStatus
	created cloudhypervisor.VMConfig
	createErr error
	startErr error
	stopErr error
}

func (h *fakeHypervisor) Create(_ context.Context, cfg cloudhypervisor.VMConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.createErr != nil { return h.createErr }
	h.created = cfg
	h.state = cloudhypervisor.StatusCreated
	return nil
}
func (h *fakeHypervisor) Start(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.startErr != nil { return h.startErr }
	h.state = cloudhypervisor.StatusRunning
	return nil
}
func (h *fakeHypervisor) Stop(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopErr != nil { return h.stopErr }
	h.state = cloudhypervisor.StatusShutdown
	return nil
}
func (h *fakeHypervisor) Status(context.Context) (cloudhypervisor.StatusResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return cloudhypervisor.StatusResponse{State: h.state}, nil
}
func (*fakeHypervisor) Ping(context.Context) error { return nil }

type fakeProcess struct {
	mu sync.Mutex
	killed bool
	waited bool
}
func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killed = true
	return nil
}
func (p *fakeProcess) Wait() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.waited = true
	return nil
}

func testManager(t *testing.T) (*Manager, *fakeRuntime) {
	t.Helper()
	root := t.TempDir()
	baseDisk := filepath.Join(root, "ubuntu-base.img")
	if err := os.WriteFile(baseDisk, []byte("test base image"), 0600); err != nil { t.Fatal(err) }
	runtime := &fakeRuntime{hv: &fakeHypervisor{}, proc: &fakeProcess{}}
	manager, err := NewManagerWithRuntime(Config{
		WorkerID: "worker-a",
		RuntimeDir: filepath.Join(root, "runtime"),
		CloudHypervisor: "/usr/bin/cloud-hypervisor",
		MaxVCPUs: 8,
		MaxMemoryBytes: 8 * 1024 * 1024 * 1024,
		Images: []Image{{ID: "ubuntu-demo", KernelPath: "/var/lib/vm-images/vmlinuz", DiskPath: baseDisk, Cmdline: "console=ttyS0"}},
		RequestTimeout: time.Second,
		StartupTimeout: time.Second,
	}, runtime)
	if err != nil { t.Fatalf("NewManagerWithRuntime: %v", err) }
	return manager, runtime
}

func validCreate() CreateRequest {
	return CreateRequest{OperationID: "op-1", VMID: "vm-1", Name: "demo", VCPUs: 2, MemoryBytes: 1024 * 1024 * 1024, ImageID: "ubuntu-demo"}
}

func TestPrepareUsesConfiguredImageAndIsIdempotent(t *testing.T) {
	manager, runtime := testManager(t)
	defer manager.Close()
	req := validCreate()
	first, err := manager.Prepare(context.Background(), req)
	if err != nil { t.Fatalf("Prepare: %v", err) }
	second, err := manager.Prepare(context.Background(), req)
	if err != nil { t.Fatalf("repeat Prepare: %v", err) }
	if first.VMID != second.VMID || runtime.launches != 1 {
		t.Fatalf("repeat create launched %d VMMs; reports %q and %q", runtime.launches, first.VMID, second.VMID)
	}
	runtime.hv.mu.Lock()
	defer runtime.hv.mu.Unlock()
	if runtime.hv.created.Payload.Kernel != "/var/lib/vm-images/vmlinuz" || len(runtime.hv.created.Disks) != 1 {
		t.Fatalf("Cloud Hypervisor received unexpected image configuration: %#v", runtime.hv.created)
	}
	perVMDisk := runtime.hv.created.Disks[0].Path
	if perVMDisk == "" || filepath.Base(perVMDisk) != "root.img" { t.Fatalf("disk path %q is not a per-VM disk", perVMDisk) }
	diskContents, err := os.ReadFile(perVMDisk)
	if err != nil { t.Fatalf("read staged per-VM disk: %v", err) }
	if string(diskContents) != "test base image" { t.Fatalf("per-VM disk content = %q", diskContents) }
}

func TestPrepareRejectsUnapprovedImageAndUnsafeID(t *testing.T) {
	manager, runtime := testManager(t)
	defer manager.Close()
	req := validCreate()
	req.ImageID = "../../etc/passwd"
	if _, err := manager.Prepare(context.Background(), req); !errors.Is(err, ErrInvalid) { t.Fatalf("image error = %v, want ErrInvalid", err) }
	req = validCreate()
	req.VMID = "../escape"
	if _, err := manager.Prepare(context.Background(), req); !errors.Is(err, ErrInvalid) { t.Fatalf("VM ID error = %v, want ErrInvalid", err) }
	if runtime.launches != 0 { t.Fatalf("launched %d VMMs for invalid requests", runtime.launches) }
}

func TestStartStopAndProcessCleanup(t *testing.T) {
	manager, runtime := testManager(t)
	defer manager.Close()
	req := validCreate()
	if _, err := manager.Prepare(context.Background(), req); err != nil { t.Fatal(err) }
	started, err := manager.Start(context.Background(), "op-start", req.VMID)
	if err != nil { t.Fatalf("Start: %v", err) }
	if started.State != string(cloudhypervisor.StatusRunning) { t.Fatalf("start state = %q", started.State) }
	// Repeating start is safe and does not issue another VMM launch.
	if _, err := manager.Start(context.Background(), "op-start", req.VMID); err != nil { t.Fatalf("repeat Start: %v", err) }
	stopped, err := manager.Stop(context.Background(), "op-stop", req.VMID)
	if err != nil { t.Fatalf("Stop: %v", err) }
	if stopped.State != string(cloudhypervisor.StatusShutdown) { t.Fatalf("stop state = %q", stopped.State) }
	runtime.proc.mu.Lock()
	defer runtime.proc.mu.Unlock()
	if !runtime.proc.killed || !runtime.proc.waited { t.Fatalf("VMM process not killed and reaped: %#v", runtime.proc) }
}

func TestLifecycleHandlerRequiresWorkerBearerToken(t *testing.T) {
	manager, runtime := testManager(t)
	defer manager.Close()
	mux := http.NewServeMux()
	NewHandler(manager, "secret").Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/worker/vms", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized { t.Fatalf("status = %d, want 401", response.Code) }
	if runtime.launches != 0 { t.Fatalf("unauthorized request launched %d VMMs", runtime.launches) }
}

func TestOperationIDCannotBeReusedAcrossLifecycleKinds(t *testing.T) {
	manager, _ := testManager(t)
	defer manager.Close()
	req := validCreate()
	if _, err := manager.Prepare(context.Background(), req); err != nil { t.Fatal(err) }
	if _, err := manager.Start(context.Background(), req.OperationID, req.VMID); !errors.Is(err, ErrConflict) {
		t.Fatalf("start reused prepare operation ID: err=%v, want conflict", err)
	}
	if _, err := manager.Start(context.Background(), "op-start", req.VMID); err != nil { t.Fatal(err) }
	if _, err := manager.Stop(context.Background(), "op-start", req.VMID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stop reused start operation ID: err=%v, want conflict", err)
	}
}
