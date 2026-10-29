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
	"os"
	"os/signal"
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
	WorkerID        string        `json:"worker_id"`
	ListenAddr      string        `json:"listen_addr"`
	RuntimeDir      string        `json:"runtime_dir"`
	CloudHypervisor string        `json:"cloud_hypervisor_path"`
	MaxVCPUs        int           `json:"max_vcpus"`
	MaxMemoryBytes  uint64        `json:"max_memory_bytes"`
	RequestTimeoutMS int          `json:"request_timeout_ms"`
	StartupTimeoutMS int          `json:"startup_timeout_ms"`
	Images          []imageConfig `json:"images"`
}

func main() {
	configPath := flag.String("config", "worker.json", "path to worker JSON configuration")
	flag.Parse()
	cfg, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("worker configuration invalid", "error", err)
		os.Exit(2)
	}
	secret := os.Getenv("VM_CONTROLLER_WORKER_SHARED_SECRET")
	if secret == "" {
		slog.Error("worker shared secret is required", "environment_variable", "VM_CONTROLLER_WORKER_SHARED_SECRET")
		os.Exit(2)
	}
	images := make([]workeragent.Image, 0, len(cfg.Images))
	for _, image := range cfg.Images {
		images = append(images, workeragent.Image{ID: image.ID, KernelPath: image.KernelPath, DiskPath: image.DiskPath, FirmwarePath: image.FirmwarePath, Cmdline: image.Cmdline})
	}
	manager, err := workeragent.NewManager(workeragent.Config{
		WorkerID: cfg.WorkerID,
		RuntimeDir: cfg.RuntimeDir,
		CloudHypervisor: cfg.CloudHypervisor,
		MaxVCPUs: cfg.MaxVCPUs,
		MaxMemoryBytes: cfg.MaxMemoryBytes,
		Images: images,
		RequestTimeout: time.Duration(cfg.RequestTimeoutMS) * time.Millisecond,
		StartupTimeout: time.Duration(cfg.StartupTimeoutMS) * time.Millisecond,
	})
	if err != nil {
		slog.Error("initialize worker manager", "error", err)
		os.Exit(2)
	}
	defer func() {
		if err := manager.Close(); err != nil { slog.Error("worker process cleanup failed", "error", err) }
	}()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	mux := http.NewServeMux()
	workeragent.NewHandler(manager, secret).Register(mux)
	server := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
			slog.Error("worker HTTP shutdown failed", "error", err)
			_ = server.Close()
			os.Exit(1)
		}
		slog.Info("worker agent stopped", "worker_id", cfg.WorkerID)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("worker HTTP server failed", "error", err)
			os.Exit(1)
		}
	}
}

func loadConfig(path string) (fileConfig, error) {
	f, err := os.Open(path)
	if err != nil { return fileConfig{}, fmt.Errorf("open worker config: %w", err) }
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var cfg fileConfig
	if err := dec.Decode(&cfg); err != nil { return fileConfig{}, fmt.Errorf("decode worker config: %w", err) }
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) { return fileConfig{}, errors.New("worker config must contain exactly one JSON object") }
	if cfg.ListenAddr == "" { return fileConfig{}, errors.New("listen_addr must not be empty") }
	if cfg.RequestTimeoutMS == 0 { cfg.RequestTimeoutMS = 2000 }
	if cfg.StartupTimeoutMS == 0 { cfg.StartupTimeoutMS = 5000 }
	if cfg.RequestTimeoutMS < 1 || cfg.RequestTimeoutMS > 60000 { return fileConfig{}, errors.New("request_timeout_ms must be between 1 and 60000") }
	if cfg.StartupTimeoutMS < 1 || cfg.StartupTimeoutMS > 120000 { return fileConfig{}, errors.New("startup_timeout_ms must be between 1 and 120000") }
	return cfg, nil
}
