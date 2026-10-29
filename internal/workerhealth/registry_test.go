package workerhealth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHeartbeatHealthAndStaleness(t *testing.T) {
	registry := NewRegistry(30 * time.Second)
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }

	report, err := registry.Heartbeat(Heartbeat{
		WorkerID: "worker-b", Hostname: "node-b", AvailableMemoryBytes: 6 << 30,
		ConfiguredMemoryCapBytes: 8 << 30, CloudHypervisorHealthy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusHealthy {
		t.Fatalf("status = %q, want healthy", report.Status)
	}
	now = now.Add(31 * time.Second)
	reports := registry.List()
	if len(reports) != 1 || reports[0].Status != StatusStale {
		t.Fatalf("stale inventory = %#v", reports)
	}
}

func TestVMMUnhealthyAndInvalidCapacity(t *testing.T) {
	registry := NewRegistry(time.Minute)
	if _, err := registry.Heartbeat(Heartbeat{WorkerID: "w", Hostname: "host", ConfiguredMemoryCapBytes: 100, AvailableMemoryBytes: 101}); err == nil {
		t.Fatal("expected capacity validation error")
	}
	report, err := registry.Heartbeat(Heartbeat{WorkerID: "w", Hostname: "host", ConfiguredMemoryCapBytes: 100, AvailableMemoryBytes: 50, CloudHypervisorHealthy: false})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusUnreachable {
		t.Fatalf("status = %q, want unreachable", report.Status)
	}
}

func TestHTTPAuthenticationAndHeartbeat(t *testing.T) {
	registry := NewRegistry(time.Minute)
	handler := NewHandler(registry, "worker-secret", "operator-secret")
	mux := http.NewServeMux()
	handler.Register(mux)

	payload, _ := json.Marshal(Heartbeat{WorkerID: "w1", Hostname: "host-1", AvailableMemoryBytes: 512, ConfiguredMemoryCapBytes: 1024, CloudHypervisorHealthy: true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/worker/heartbeat", bytes.NewReader(payload))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", res.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/worker/heartbeat", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer worker-secret")
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated heartbeat status = %d, body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated inventory status = %d, body=%s", res.Code, res.Body.String())
	}
}

func TestNoSecretMeansClosed(t *testing.T) {
	registry := NewRegistry(time.Minute)
	handler := NewHandler(registry, "", "")
	mux := http.NewServeMux()
	handler.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
	req.Header.Set("Authorization", "Bearer anything")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want unauthorized", res.Code)
	}
}

func TestExplicitUnreachableStatusPersistsUntilHeartbeat(t *testing.T) {
	registry := NewRegistry(time.Minute)
	if _, err := registry.Heartbeat(Heartbeat{WorkerID: "w", Hostname: "host", AvailableMemoryBytes: 10, ConfiguredMemoryCapBytes: 100, CloudHypervisorHealthy: true}); err != nil {
		t.Fatal(err)
	}
	if !registry.MarkUnreachable("w") {
		t.Fatal("expected existing worker to be marked unreachable")
	}
	report, ok := registry.Get("w")
	if !ok || report.Status != StatusUnreachable {
		t.Fatalf("report = %#v, found=%v; want unreachable", report, ok)
	}
	if _, err := registry.Heartbeat(Heartbeat{WorkerID: "w", Hostname: "host", AvailableMemoryBytes: 10, ConfiguredMemoryCapBytes: 100, CloudHypervisorHealthy: true}); err != nil {
		t.Fatal(err)
	}
	report, _ = registry.Get("w")
	if report.Status != StatusHealthy {
		t.Fatalf("status after valid heartbeat = %q, want healthy", report.Status)
	}
}
