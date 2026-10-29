package cloudhypervisor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeSocket(t *testing.T, handler http.Handler) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ch.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen on unix socket: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	return path
}

func TestLifecycleEndpointsUseExpectedMethodsAndPaths(t *testing.T) {
	var got []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case apiPrefix + "/vm.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"state\":\"Running\"}"))
		case apiPrefix + "/vmm.ping":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"version\":\"test\"}"))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	client, err := NewClient(fakeSocket(t, handler), time.Second)
	if err != nil { t.Fatal(err) }
	ctx := context.Background()
	cfg := VMConfig{CPUs: CPUConfig{BootVCPUs: 2, MaxVCPUs: 2}, Payload: PayloadConfig{Kernel: "/images/vmlinuz"}}
	if err := client.Prepare(ctx, cfg); err != nil { t.Fatalf("prepare: %v", err) }
	if err := client.Start(ctx); err != nil { t.Fatalf("start: %v", err) }
	if err := client.Stop(ctx); err != nil { t.Fatalf("stop: %v", err) }
	status, err := client.Status(ctx)
	if err != nil { t.Fatalf("status: %v", err) }
	if status.State != StatusRunning { t.Fatalf("state = %q, want Running", status.State) }
	if err := client.Ping(ctx); err != nil { t.Fatalf("ping: %v", err) }
	want := []string{
		"PUT /api/v1/vm.create",
		"PUT /api/v1/vm.boot",
		"PUT /api/v1/vm.shutdown",
		"GET /api/v1/vm.info",
		"GET /api/v1/vmm.ping",
	}
	if len(got) != len(want) { t.Fatalf("requests = %#v, want %#v", got, want) }
	for i := range want {
		if got[i] != want[i] { t.Errorf("request[%d] = %q, want %q", i, got[i], want[i]) }
	}
}

func TestCreateValidatesConfigBeforeCallingAPI(t *testing.T) {
	client, err := NewClientWithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request for invalid config")
		return nil, errors.New("unexpected request")
	})})
	if err != nil { t.Fatal(err) }
	err = client.Create(context.Background(), VMConfig{CPUs: CPUConfig{BootVCPUs: 2, MaxVCPUs: 1}})
	if !errors.Is(err, ErrInvalidConfig) { t.Fatalf("error = %v, want ErrInvalidConfig", err) }
}

func TestHTTPErrorIsStructured(t *testing.T) {
	client, err := NewClientWithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusConflict, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{\"error\":\"already created\"}"))}, nil
	})})
	if err != nil { t.Fatal(err) }
	err = client.Start(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) { t.Fatalf("error type = %T, want *APIError", err) }
	if apiErr.Status != http.StatusConflict || apiErr.Code != "http_error" { t.Fatalf("unexpected structured error: %#v", apiErr) }
}

func TestStatusRejectsMalformedOrUnknownState(t *testing.T) {
	for _, response := range []string{"not-json", "{\"state\":\"BrandNew\"}"} {
		client, err := NewClientWithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
		})})
		if err != nil { t.Fatal(err) }
		if _, err := client.Status(context.Background()); err == nil { t.Fatalf("Status(%q) unexpectedly succeeded", response) }
	}
}

func TestRequestTimeoutIsStructured(t *testing.T) {
	client, err := NewClientWithHTTPClient(&http.Client{Timeout: 10 * time.Millisecond, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})})
	if err != nil { t.Fatal(err) }
	err = client.Start(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "timeout" { t.Fatalf("error = %#v, want timeout APIError", apiErr) }
}

type roundTripFunc func(*http.Request) (*http.Response, error)
func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
