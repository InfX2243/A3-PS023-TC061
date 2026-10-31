package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/workeragent"
)

type imageConfig struct {
	ID           string `json:"id"`
	KernelPath   string `json:"kernel_path"`
	DiskPath     string `json:"disk_path"`
	FirmwarePath string `json:"firmware_path"`
	Cmdline      string `json:"cmdline"`
}

type fileConfig struct {
	WorkerID                  string        `json:"worker_id"`
	WorkerName                string        `json:"worker_name"`
	ListenAddr                string        `json:"listen_addr"`
	RuntimeDir                string        `json:"runtime_dir"`
	CloudHypervisor            string        `json:"cloud_hypervisor_path"`
	MaxVCPUs                  int           `json:"max_vcpus"`
	MaxMemoryBytes            uint64        `json:"max_memory_bytes"`
	RequestTimeoutMS          int           `json:"request_timeout_ms"`
	StartupTimeoutMS          int           `json:"startup_timeout_ms"`
	ControllerHeartbeatURL    string        `json:"controller_heartbeat_url"`
	WorkerEndpoint             string        `json:"worker_endpoint"`
	HeartbeatIntervalSeconds  int           `json:"heartbeat_interval_seconds"`
	Images                    []imageConfig `json:"images"`
}

type heartbeatPayload struct {
	WorkerID string `json:"worker_id"`
	Hostname string `json:"hostname"`
	Endpoint string `json:"endpoint"`
	AvailableMemoryBytes uint64 `json:"available_memory_bytes"`
	ConfiguredMemoryCapBytes uint64 `json:"configured_memory_cap_bytes"`
	CloudHypervisorHealthy bool `json:"cloud_hypervisor_healthy"`
}

func main() {
	if err := run(); err != nil {
		slog.Error("worker agent stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "worker.json", "path to worker JSON configuration")
	flag.Parse()
	cfg, err := loadConfig(*configPath)
	if err != nil { return fmt.Errorf("worker configuration invalid: %w", err) }
	secret := os.Getenv("VM_CONTROLLER_WORKER_SHARED_SECRET")
	if secret == "" { return errors.New("VM_CONTROLLER_WORKER_SHARED_SECRET is required") }

	images := make([]workeragent.Image, 0, len(cfg.Images))
	for _, image := range cfg.Images {
		images = append(images, workeragent.Image{ID: image.ID, KernelPath: image.KernelPath, DiskPath: image.DiskPath, FirmwarePath: image.FirmwarePath, Cmdline: image.Cmdline})
	}
	manager, err := workeragent.NewManager(workeragent.Config{
		WorkerID: cfg.WorkerID, RuntimeDir: cfg.RuntimeDir, CloudHypervisor: cfg.CloudHypervisor,
		MaxVCPUs: cfg.MaxVCPUs, MaxMemoryBytes: cfg.MaxMemoryBytes, Images: images,
		RequestTimeout: time.Duration(cfg.RequestTimeoutMS) * time.Millisecond,
		StartupTimeout: time.Duration(cfg.StartupTimeoutMS) * time.Millisecond,
	})
	if err != nil { return fmt.Errorf("initialize worker manager: %w", err) }
	defer func() {
		if err := manager.Close(); err != nil { slog.Error("worker process cleanup failed", "error", err) }
	}()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	mux := http.NewServeMux()
	workeragent.NewHandler(manager, secret).Register(mux)
	server := &http.Server{
		Addr: cfg.ListenAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.ControllerHeartbeatURL != "" {
		go heartbeatLoop(ctx, cfg, secret)
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("worker agent starting", "worker_id", cfg.WorkerID, "listen_addr", cfg.ListenAddr)
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("worker HTTP shutdown failed: %w", err)
		}
		slog.Info("worker agent stopped", "worker_id", cfg.WorkerID)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) { return fmt.Errorf("worker HTTP server failed: %w", err) }
	}
	return nil
}

func loadConfig(path string) (fileConfig, error) {
	f, err := os.Open(path)
	if err != nil { return fileConfig{}, fmt.Errorf("open worker config: %w", err) }
	defer f.Close()
	info, err := f.Stat()
	if err != nil { return fileConfig{}, fmt.Errorf("stat worker config: %w", err) }
	if info.Mode().Perm()&0022 != 0 { return fileConfig{}, errors.New("worker config file must not be group- or world-writable") }
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var cfg fileConfig
	if err := dec.Decode(&cfg); err != nil { return fileConfig{}, fmt.Errorf("decode worker config: %w", err) }
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) { return fileConfig{}, errors.New("worker config must contain exactly one JSON object") }
	if strings.TrimSpace(cfg.WorkerID) == "" || strings.TrimSpace(cfg.ListenAddr) == "" { return fileConfig{}, errors.New("worker_id and listen_addr are required") }
	if cfg.RequestTimeoutMS == 0 { cfg.RequestTimeoutMS = 2000 }
	if cfg.StartupTimeoutMS == 0 { cfg.StartupTimeoutMS = 5000 }
	if cfg.HeartbeatIntervalSeconds == 0 { cfg.HeartbeatIntervalSeconds = 10 }
	if cfg.RequestTimeoutMS < 1 || cfg.RequestTimeoutMS > 60000 { return fileConfig{}, errors.New("request_timeout_ms must be between 1 and 60000") }
	if cfg.StartupTimeoutMS < 1 || cfg.StartupTimeoutMS > 120000 { return fileConfig{}, errors.New("startup_timeout_ms must be between 1 and 120000") }
	if cfg.HeartbeatIntervalSeconds < 1 || cfg.HeartbeatIntervalSeconds > 300 { return fileConfig{}, errors.New("heartbeat_interval_seconds must be between 1 and 300") }
	if cfg.ControllerHeartbeatURL != "" {
		u, err := url.Parse(cfg.ControllerHeartbeatURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fileConfig{}, errors.New("controller_heartbeat_url must be an absolute http(s) URL without credentials, query, or fragment")
		}
	}
	if cfg.ControllerHeartbeatURL != "" && strings.TrimSpace(cfg.WorkerEndpoint) == "" {
		return fileConfig{}, errors.New("worker_endpoint is required when controller_heartbeat_url is configured")
	}
	if cfg.WorkerEndpoint != "" {
		u, err := url.Parse(cfg.WorkerEndpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fileConfig{}, errors.New("worker_endpoint must be an absolute http(s) URL without credentials, query, or fragment")
		}
	}
	return cfg, nil
}

func heartbeatLoop(ctx context.Context, cfg fileConfig, secret string) {
	ticker := time.NewTicker(time.Duration(cfg.HeartbeatIntervalSeconds) * time.Second)
	defer ticker.Stop()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }}
	send := func() {
		hostname := strings.TrimSpace(cfg.WorkerName)
		if hostname == "" { hostname, _ = os.Hostname() }
		available := availableMemoryBytes()
		if available > cfg.MaxMemoryBytes { available = cfg.MaxMemoryBytes }
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		healthy := probeKVMAndVMM(probeCtx, cfg.CloudHypervisor)
		cancel()
		payload := heartbeatPayload{WorkerID: cfg.WorkerID, Hostname: hostname, Endpoint: cfg.WorkerEndpoint, AvailableMemoryBytes: available, ConfiguredMemoryCapBytes: cfg.MaxMemoryBytes, CloudHypervisorHealthy: healthy}
		data, err := json.Marshal(payload)
		if err != nil { slog.Warn("encode worker heartbeat", "error", err); return }
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ControllerHeartbeatURL, strings.NewReader(string(data)))
		if err != nil { slog.Warn("build worker heartbeat", "error", err); return }
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("ngrok-skip-browser-warning", "true")
		resp, err := client.Do(req)
		if err != nil { slog.Warn("send worker heartbeat failed", "worker_id", cfg.WorkerID, "error", err); return }
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 { slog.Warn("controller rejected worker heartbeat", "worker_id", cfg.WorkerID, "status", resp.StatusCode) }
	}
	send()
	for {
		select {
		case <-ctx.Done(): return
		case <-ticker.C: send()
		}
	}
}

func availableMemoryBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil { return 0 }
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kib, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil && kib <= ^uint64(0)/1024 { return kib * 1024 }
			}
		}
	}
	return 0
}

func probeKVMAndVMM(ctx context.Context, binary string) bool {
	kvm, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil { return false }
	_ = kvm.Close()
	cmd := exec.CommandContext(ctx, binary, "--version")
	if err := cmd.Run(); err != nil { return false }
	return true
}

