// Package workeragent implements the authenticated worker-side VM lifecycle.
package workeragent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/cloudhypervisor"
)

var (
	ErrNotFound = errors.New("worker VM not found")
	ErrConflict = errors.New("worker VM operation conflicts with current state")
	ErrInvalid  = errors.New("invalid worker VM request")
	idPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
)

type Image struct {
	ID          string
	KernelPath  string
	DiskPath    string
	FirmwarePath string
	Cmdline     string
}

type Config struct {
	WorkerID       string
	RuntimeDir     string
	CloudHypervisor string
	MaxVCPUs       int
	MaxMemoryBytes uint64
	Images         []Image
	RequestTimeout time.Duration
	StartupTimeout time.Duration
}

type CreateRequest struct {
	OperationID string `json:"operation_id"`
	VMID        string `json:"vm_id"`
	Name        string `json:"name"`
	VCPUs       int    `json:"vcpus"`
	MemoryBytes uint64 `json:"memory_bytes"`
	ImageID     string `json:"image_id"`
}

type VMReport struct {
	WorkerID    string `json:"worker_id"`
	VMID        string `json:"vm_id"`
	OperationID string `json:"operation_id"`
	ImageID     string `json:"image_id"`
	VCPUs       int    `json:"vcpus"`
	MemoryBytes uint64 `json:"memory_bytes"`
	State       string `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Hypervisor interface {
	Create(context.Context, cloudhypervisor.VMConfig) error
	Start(context.Context) error
	Stop(context.Context) error
	Status(context.Context) (cloudhypervisor.StatusResponse, error)
	Ping(context.Context) error
}

type Process interface {
	Wait() error
	Kill() error
}

type Runtime interface {
	Launch(context.Context, string, string, time.Duration, time.Duration) (Hypervisor, Process, error)
}

type execRuntime struct{}

type childProcess struct { cmd *exec.Cmd }

func (p *childProcess) Wait() error { return p.cmd.Wait() }
func (p *childProcess) Kill() error {
	if p.cmd.Process == nil { return nil }
	err := p.cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) { return nil }
	return err
}

func (execRuntime) Launch(ctx context.Context, binary, socket string, requestTimeout, startupTimeout time.Duration) (Hypervisor, Process, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		return nil, nil, fmt.Errorf("create VM runtime directory: %w", err)
	}
	// A stale socket may remain after an unclean worker shutdown. The manager
	// serializes launches and owns this VM-specific socket path.
	_ = os.Remove(socket)
	cmd := exec.Command(binary, "--api-socket", socket)
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start Cloud Hypervisor: %w", err)
	}
	proc := &childProcess{cmd: cmd}
	client, err := cloudhypervisor.NewClient(socket, requestTimeout)
	if err != nil {
		_ = proc.Kill()
		_ = proc.Wait()
		return nil, nil, err
	}
	deadline := time.NewTimer(startupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := client.Ping(ctx); err == nil {
			return client, proc, nil
		}
		select {
		case <-ctx.Done():
			_ = proc.Kill()
			_ = proc.Wait()
			return nil, nil, fmt.Errorf("wait for Cloud Hypervisor API: %w", ctx.Err())
		case <-deadline.C:
			_ = proc.Kill()
			_ = proc.Wait()
			return nil, nil, errors.New("Cloud Hypervisor API did not become ready before timeout")
		case <-ticker.C:
		}
	}
}

type vmEntry struct {
	report VMReport
	client Hypervisor
	process Process
	socket string
	vmConfig cloudhypervisor.VMConfig
}

type Manager struct {
	mu      sync.Mutex
	cfg     Config
	runtime Runtime
	images  map[string]Image
	vms     map[string]*vmEntry
	ops     map[string]string
	now     func() time.Time
}

func NewManager(cfg Config) (*Manager, error) {
	return NewManagerWithRuntime(cfg, execRuntime{})
}

func NewManagerWithRuntime(cfg Config, runtime Runtime) (*Manager, error) {
	if strings.TrimSpace(cfg.WorkerID) == "" || len(cfg.WorkerID) > 128 {
		return nil, errors.New("worker_id must be between 1 and 128 characters")
	}
	if !filepath.IsAbs(cfg.RuntimeDir) {
		return nil, errors.New("runtime_dir must be an absolute path")
	}
	if strings.TrimSpace(cfg.CloudHypervisor) == "" {
		return nil, errors.New("cloud_hypervisor binary path must not be empty")
	}
	if cfg.MaxVCPUs < 1 || cfg.MaxVCPUs > 256 || cfg.MaxMemoryBytes == 0 {
		return nil, errors.New("worker resource limits must be positive")
	}
	if cfg.RequestTimeout <= 0 { cfg.RequestTimeout = 2 * time.Second }
	if cfg.StartupTimeout <= 0 { cfg.StartupTimeout = 5 * time.Second }
	if runtime == nil { return nil, errors.New("worker runtime must not be nil") }
	images := make(map[string]Image, len(cfg.Images))
	for _, image := range cfg.Images {
		if !idPattern.MatchString(image.ID) {
			return nil, fmt.Errorf("invalid configured image ID %q", image.ID)
		}
		if _, exists := images[image.ID]; exists { return nil, fmt.Errorf("duplicate configured image ID %q", image.ID) }
		if image.KernelPath == "" && image.FirmwarePath == "" {
			return nil, fmt.Errorf("image %q requires a kernel or firmware path", image.ID)
		}
		if image.KernelPath != "" && !filepath.IsAbs(image.KernelPath) { return nil, fmt.Errorf("image %q kernel path must be absolute", image.ID) }
		if image.FirmwarePath != "" && !filepath.IsAbs(image.FirmwarePath) { return nil, fmt.Errorf("image %q firmware path must be absolute", image.ID) }
		if image.DiskPath != "" && !filepath.IsAbs(image.DiskPath) { return nil, fmt.Errorf("image %q disk path must be absolute", image.ID) }
		images[image.ID] = image
	}
	if len(images) == 0 { return nil, errors.New("at least one approved image must be configured") }
	return &Manager{cfg: cfg, runtime: runtime, images: images, vms: make(map[string]*vmEntry), ops: make(map[string]string), now: time.Now}, nil
}

func (m *Manager) Prepare(ctx context.Context, req CreateRequest) (VMReport, error) {
	if !idPattern.MatchString(req.OperationID) || !idPattern.MatchString(req.VMID) {
		return VMReport{}, fmt.Errorf("%w: operation_id and vm_id must be safe identifiers", ErrInvalid)
	}
	if strings.TrimSpace(req.Name) == "" || len(req.Name) > 128 {
		return VMReport{}, fmt.Errorf("%w: name must be between 1 and 128 characters", ErrInvalid)
	}
	if req.VCPUs < 1 || req.VCPUs > m.cfg.MaxVCPUs || req.MemoryBytes == 0 || req.MemoryBytes > m.cfg.MaxMemoryBytes || req.MemoryBytes > uint64(1<<63-1) {
		return VMReport{}, fmt.Errorf("%w: requested resources exceed worker limits", ErrInvalid)
	}
	image, ok := m.images[req.ImageID]
	if !ok { return VMReport{}, fmt.Errorf("%w: image_id is not allowlisted", ErrInvalid) }

	m.mu.Lock()
	defer m.mu.Unlock()
	if existingVMID, ok := m.ops[req.OperationID]; ok {
		if existingVMID != req.VMID { return VMReport{}, fmt.Errorf("%w: operation_id was already used for another VM", ErrConflict) }
		if entry := m.vms[existingVMID]; entry != nil { return entry.report, nil }
	}
	if entry := m.vms[req.VMID]; entry != nil {
		if entry.report.OperationID == req.OperationID && entry.report.ImageID == req.ImageID && entry.report.VCPUs == req.VCPUs && entry.report.MemoryBytes == req.MemoryBytes {
			return entry.report, nil
		}
		return VMReport{}, fmt.Errorf("%w: VM already exists", ErrConflict)
	}

	socket := filepath.Join(m.cfg.RuntimeDir, req.VMID, "api.sock")
	hv, proc, err := m.runtime.Launch(ctx, m.cfg.CloudHypervisor, socket, m.cfg.RequestTimeout, m.cfg.StartupTimeout)
	if err != nil { return VMReport{}, fmt.Errorf("launch VMM: %w", err) }
	vmConfig := cloudhypervisor.VMConfig{
		CPUs: cloudhypervisor.CPUConfig{BootVCPUs: req.VCPUs, MaxVCPUs: req.VCPUs},
		Memory: &cloudhypervisor.MemoryConfig{Size: int64(req.MemoryBytes)},
		Payload: cloudhypervisor.PayloadConfig{Kernel: image.KernelPath, Firmware: image.FirmwarePath, Cmdline: image.Cmdline},
	}
	if image.DiskPath != "" { vmConfig.Disks = []cloudhypervisor.DiskConfig{{Path: image.DiskPath, ImageType: "Raw"}} }
	callCtx, cancel := context.WithTimeout(ctx, m.cfg.RequestTimeout)
	err = hv.Create(callCtx, vmConfig)
	cancel()
	if err != nil {
		_ = proc.Kill()
		_ = proc.Wait()
		_ = os.Remove(socket)
		return VMReport{}, fmt.Errorf("create VM in Cloud Hypervisor: %w", err)
	}
	now := m.now().UTC()
	report := VMReport{WorkerID: m.cfg.WorkerID, VMID: req.VMID, OperationID: req.OperationID, ImageID: req.ImageID, VCPUs: req.VCPUs, MemoryBytes: req.MemoryBytes, State: string(cloudhypervisor.StatusCreated), CreatedAt: now, UpdatedAt: now}
	m.vms[req.VMID] = &vmEntry{report: report, client: hv, process: proc, socket: socket, vmConfig: vmConfig}
	m.ops[req.OperationID] = req.VMID
	return report, nil
}

func (m *Manager) Start(ctx context.Context, operationID, vmID string) (VMReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.vms[vmID]
	if !ok { return VMReport{}, ErrNotFound }
	if operationID == "" { return VMReport{}, fmt.Errorf("%w: operation_id is required", ErrInvalid) }
	if entry.report.State == string(cloudhypervisor.StatusRunning) { return entry.report, nil }
	if entry.report.State != string(cloudhypervisor.StatusCreated) && entry.report.State != string(cloudhypervisor.StatusShutdown) {
		return VMReport{}, fmt.Errorf("%w: cannot start VM in state %s", ErrConflict, entry.report.State)
	}
	if entry.client == nil {
		hv, proc, err := m.runtime.Launch(ctx, m.cfg.CloudHypervisor, entry.socket, m.cfg.RequestTimeout, m.cfg.StartupTimeout)
		if err != nil { return VMReport{}, fmt.Errorf("relaunch VMM for stopped VM: %w", err) }
		createCtx, cancel := context.WithTimeout(ctx, m.cfg.RequestTimeout)
		err = hv.Create(createCtx, entry.vmConfig)
		cancel()
		if err != nil {
			_ = proc.Kill()
			_ = proc.Wait()
			_ = os.Remove(entry.socket)
			return VMReport{}, fmt.Errorf("recreate stopped VM: %w", err)
		}
		entry.client, entry.process = hv, proc
	}
	ctxCall, cancel := context.WithTimeout(ctx, m.cfg.RequestTimeout)
	err := entry.client.Start(ctxCall)
	cancel()
	if err != nil { return VMReport{}, fmt.Errorf("start VM: %w", err) }
	entry.report.State = string(cloudhypervisor.StatusRunning)
	entry.report.OperationID = operationID
	entry.report.UpdatedAt = m.now().UTC()
	return entry.report, nil
}

func (m *Manager) Stop(ctx context.Context, operationID, vmID string) (VMReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.vms[vmID]
	if !ok { return VMReport{}, ErrNotFound }
	if operationID == "" { return VMReport{}, fmt.Errorf("%w: operation_id is required", ErrInvalid) }
	if entry.report.State == string(cloudhypervisor.StatusShutdown) { return entry.report, nil }
	if entry.report.State != string(cloudhypervisor.StatusRunning) && entry.report.State != string(cloudhypervisor.StatusPaused) {
		return VMReport{}, fmt.Errorf("%w: cannot stop VM in state %s", ErrConflict, entry.report.State)
	}
	ctxCall, cancel := context.WithTimeout(ctx, m.cfg.RequestTimeout)
	err := entry.client.Stop(ctxCall)
	cancel()
	if err != nil { return VMReport{}, fmt.Errorf("stop VM: %w", err) }
	ctxCall, cancel = context.WithTimeout(ctx, m.cfg.RequestTimeout)
	status, err := entry.client.Status(ctxCall)
	cancel()
	if err != nil { return VMReport{}, fmt.Errorf("confirm VM stopped: %w", err) }
	if status.State != cloudhypervisor.StatusShutdown {
		return VMReport{}, fmt.Errorf("%w: Cloud Hypervisor reports %s after stop", ErrConflict, status.State)
	}
	if err := entry.process.Kill(); err != nil { return VMReport{}, fmt.Errorf("terminate Cloud Hypervisor process: %w", err) }
	_ = entry.process.Wait()
	_ = os.Remove(entry.socket)
	entry.report.State = string(cloudhypervisor.StatusShutdown)
	entry.report.OperationID = operationID
	entry.report.UpdatedAt = m.now().UTC()
	entry.client = nil
	entry.process = nil
	return entry.report, nil
}

func (m *Manager) Status(ctx context.Context, vmID string) (VMReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.vms[vmID]
	if !ok { return VMReport{}, ErrNotFound }
	if entry.client != nil {
		callCtx, cancel := context.WithTimeout(ctx, m.cfg.RequestTimeout)
		status, err := entry.client.Status(callCtx)
		cancel()
		if err != nil { return VMReport{}, fmt.Errorf("query VM status: %w", err) }
		entry.report.State = string(status.State)
		entry.report.UpdatedAt = m.now().UTC()
	}
	return entry.report, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []string
	for id, entry := range m.vms {
		if entry.process != nil {
			if err := entry.process.Kill(); err != nil { errs = append(errs, fmt.Sprintf("%s: %v", id, err)) }
			if err := entry.process.Wait(); err != nil { /* process exit status is expected after Kill */ }
		}
		_ = os.Remove(entry.socket)
	}
	if len(errs) != 0 { return errors.New(strings.Join(errs, "; ")) }
	return nil
}
