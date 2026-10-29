package controllerapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
)

type fakeService struct {
	createCalls int
	lastKey string
	createErr error
}

func (*fakeService) ListWorkers(context.Context) ([]domain.Worker, error) { return []domain.Worker{}, nil }
func (*fakeService) ListVMs(context.Context) ([]domain.VM, error) { return []domain.VM{}, nil }
func (*fakeService) GetVM(context.Context, string) (domain.VM, error) { return domain.VM{}, ErrNotFound }
func (s *fakeService) CreateVM(_ context.Context, req CreateVMRequest, key string) (domain.VM, domain.Operation, error) {
	s.createCalls++
	s.lastKey = key
	if s.createErr != nil { return domain.VM{}, domain.Operation{}, s.createErr }
	now := time.Now().UTC()
	vm := domain.VM{ID: "vm-1", Name: req.Name, VCPUs: req.VCPUs, MemoryBytes: req.MemoryMiB * 1024 * 1024, ImageID: req.ImageID, Desired: domain.VMStopped, Observed: domain.VMDefined, CreatedAt: now, UpdatedAt: now}
	op := domain.Operation{ID: "op-1", VMID: vm.ID, Type: domain.OperationPrepare, State: domain.OperationPending, CreatedAt: now, UpdatedAt: now}
	return vm, op, nil
}
func (*fakeService) StartVM(context.Context, string) (domain.VM, domain.Operation, error) { return domain.VM{}, domain.Operation{}, ErrNotFound }
func (*fakeService) StopVM(context.Context, string) (domain.VM, domain.Operation, error) { return domain.VM{}, domain.Operation{}, ErrNotFound }
func (*fakeService) GetOperation(context.Context, string) (domain.Operation, error) { return domain.Operation{}, ErrNotFound }

func newTestHandler(service Service) http.Handler {
	mux := http.NewServeMux()
	NewHandler(service, "operator-secret", Limits{MaxVCPUs: 8, MaxMemoryMiB: 8192, AllowedImages: map[string]struct{}{"ubuntu-demo": {}}}).Register(mux)
	return mux
}

func TestOperatorEndpointsRequireAuthentication(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/vms", nil)
	newTestHandler(&fakeService{}).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized { t.Fatalf("status = %d, want 401", response.Code) }
	if response.Header().Get("X-Request-ID") == "" { t.Fatal("missing generated request ID") }
}

func TestCreateRequiresIdempotencyAndServerSideImageAllowlist(t *testing.T) {
	for _, tc := range []struct{name, key, image string; want int}{
		{name: "missing key", image: "ubuntu-demo", want: http.StatusBadRequest},
		{name: "unapproved image", key: "request-1", image: "/tmp/attacker.img", want: http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakeService{}
			body, _ := json.Marshal(CreateVMRequest{Name: "demo", VCPUs: 1, MemoryMiB: 512, ImageID: tc.image})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/vms", strings.NewReader(string(body)))
			request.Header.Set("Authorization", "Bearer operator-secret")
			if tc.key != "" { request.Header.Set("Idempotency-Key", tc.key) }
			response := httptest.NewRecorder()
			newTestHandler(service).ServeHTTP(response, request)
			if response.Code != tc.want { t.Fatalf("status = %d, want %d; body=%s", response.Code, tc.want, response.Body.String()) }
			if service.createCalls != 0 { t.Fatalf("service called %d times for invalid request", service.createCalls) }
		})
	}
}

func TestCreatePassesIdempotencyKeyAndReturnsAccepted(t *testing.T) {
	service := &fakeService{}
	body := `{"name":"demo","vcpus":2,"memory_mib":1024,"image_id":"ubuntu-demo"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vms", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer operator-secret")
	request.Header.Set("Idempotency-Key", "create-demo-1")
	request.Header.Set("X-Request-ID", "req-test-1")
	response := httptest.NewRecorder()
	newTestHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusAccepted { t.Fatalf("status = %d, want 202; body=%s", response.Code, response.Body.String()) }
	if service.createCalls != 1 || service.lastKey != "create-demo-1" { t.Fatalf("create call/key = %d/%q", service.createCalls, service.lastKey) }
	if response.Header().Get("X-Request-ID") != "req-test-1" { t.Fatalf("request ID = %q", response.Header().Get("X-Request-ID")) }
}

func TestServiceErrorsUseStableStatusAndCode(t *testing.T) {
	service := &fakeService{createErr: errors.Join(ErrConflict, errors.New("duplicate state"))}
	body := `{"name":"demo","vcpus":1,"memory_mib":512,"image_id":"ubuntu-demo"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vms", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer operator-secret")
	request.Header.Set("Idempotency-Key", "create-demo-1")
	response := httptest.NewRecorder()
	newTestHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusConflict { t.Fatalf("status = %d, want 409", response.Code) }
	if !strings.Contains(response.Body.String(), `"code":"state_conflict"`) { t.Fatalf("missing stable error code: %s", response.Body.String()) }
}
