package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/config"
	"github.com/InfX2243/A3-PS023-TC061/internal/controller"
	"github.com/InfX2243/A3-PS023-TC061/internal/ec2"
	"github.com/InfX2243/A3-PS023-TC061/internal/controllerapi"
	"github.com/InfX2243/A3-PS023-TC061/internal/store"
	"github.com/InfX2243/A3-PS023-TC061/internal/workerhealth"
	"github.com/InfX2243/A3-PS023-TC061/internal/observability"
	"github.com/InfX2243/A3-PS023-TC061/internal/webui"
)

func main() {
	configPath := flag.String("config", "config.json", "path to controller JSON configuration (optional)")
	flag.Parse()

	if err := config.LoadDotEnv(".env"); err != nil {
		slog.Error("load environment file", "error", err)
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("configuration invalid", "error", err)
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	db, err := store.Open(context.Background(), cfg.DatabasePath)
	if err != nil {
		slog.Error("open controller database", "error", err)
		os.Exit(2)
	}
	defer func() {
		if err := db.Close(); err != nil { slog.Error("close controller database", "error", err) }
	}()
	service, err := controller.NewService(context.Background(), db, cfg)
	if err != nil {
		slog.Error("initialize controller service", "error", err)
		os.Exit(2)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	allowedImages := make(map[string]struct{}, len(cfg.AllowedImages))
	for _, image := range cfg.AllowedImages { allowedImages[image] = struct{}{} }
	controllerapi.NewHandler(service, cfg.OperatorToken, controllerapi.Limits{
		MaxVCPUs: cfg.MaxVCPUs,
		MaxMemoryMiB: cfg.MaxMemoryMiB,
		AllowedImages: allowedImages,
	}).Register(mux)
	ui := webui.NewHandlerWithCredentials(service, cfg.OperatorToken, cfg.OperatorUsername, cfg.OperatorPassword, webui.Limits{
		MaxVCPUs: cfg.MaxVCPUs,
		MaxMemoryMiB: cfg.MaxMemoryMiB,
		AllowedImages: cfg.AllowedImages,
	}, cfg.SecureCookies)
	awsRegion := os.Getenv("AWS_REGION")
	if awsRegion == "" { awsRegion = os.Getenv("AWS_DEFAULT_REGION") }
	ui.SetEC2Provisioner(ec2.Client{Region: awsRegion})
	ui.SetEC2Bootstrap(ec2.BootstrapConfigFromEnv())
	metricsStore := observability.NewStore()
	ui.SetMetricsStore(metricsStore)
	observability.Handler{Store: metricsStore, WorkerSecret: cfg.WorkerSharedSecret, OperatorToken: cfg.OperatorToken}.Register(mux)
	ui.Register(mux)
	registry := workerhealth.NewRegistry(workerhealth.DefaultStaleAfter)
	workerhealth.NewHandlerWithRecorder(registry, cfg.WorkerSharedSecret, cfg.OperatorToken, service).RegisterHeartbeatOnly(mux)

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
	go reconciliationLoop(ctx, service)
	errCh := make(chan error, 1)
	go func() {
		slog.Info("controller starting", "listen_addr", cfg.ListenAddr)
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
			_ = server.Close()
			os.Exit(1)
		}
		slog.Info("controller stopped")
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("controller server failed", "error", err)
			os.Exit(1)
		}
	}
}

func reconciliationLoop(ctx context.Context, service *controller.Service) {
	run := func() {
		reconcileCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := service.Reconcile(reconcileCtx); err != nil {
			slog.Warn("one or more operations remain unresolved after reconciliation", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done(): return
		case <-ticker.C: run()
		}
	}
}
