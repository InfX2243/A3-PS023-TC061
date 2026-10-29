package webui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/controllerapi"
	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
)

type fakeUIService struct{}
func (*fakeUIService) ListWorkers(context.Context) ([]domain.Worker, error) {
	return []domain.Worker{{ID:"worker-a", Name:"Worker A", State:domain.WorkerHealthy, AvailableMemoryBytes:2*1024*1024*1024, MemoryLimitBytes:4*1024*1024*1024}}, nil
}
func (*fakeUIService) ListVMs(context.Context) ([]domain.VM, error) { return []domain.VM{}, nil }
func (*fakeUIService) LatestOperation(context.Context, string) (domain.Operation, error) { return domain.Operation{}, controllerapi.ErrNotFound }
func (*fakeUIService) CreateVM(context.Context, controllerapi.CreateVMRequest, string) (domain.VM, domain.Operation, error) {
	now := time.Now()
	return domain.VM{ID:"vm-1", Name:"demo", Observed:domain.VMStopped, CreatedAt:now, UpdatedAt:now}, domain.Operation{ID:"op-1", State:domain.OperationSucceeded}, nil
}
func (*fakeUIService) StartVM(context.Context, string) (domain.VM, domain.Operation, error) { return domain.VM{}, domain.Operation{}, nil }
func (*fakeUIService) StopVM(context.Context, string) (domain.VM, domain.Operation, error) { return domain.VM{}, domain.Operation{}, nil }

func testUI() http.Handler {
	mux := http.NewServeMux()
	NewHandler(&fakeUIService{}, "operator-secret", Limits{MaxVCPUs:8, MaxMemoryMiB:8192, AllowedImages:[]string{"ubuntu-demo"}}, false).Register(mux)
	return mux
}

func TestDashboardRequiresLoginAndSetsSecurityHeaders(t *testing.T) {
	response := httptest.NewRecorder()
	testUI().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" { t.Fatalf("unauthenticated dashboard status/location = %d/%q", response.Code, response.Header().Get("Location")) }

	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	testUI().ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusSeeOther { t.Fatalf("login status = %d", loginResponse.Code) }
	cookie := loginResponse.Result().Cookies()[0]
	dashboard := httptest.NewRequest(http.MethodGet, "/", nil)
	dashboard.AddCookie(cookie)
	dashboardResponse := httptest.NewRecorder()
	testUI().ServeHTTP(dashboardResponse, dashboard)
	if dashboardResponse.Code != http.StatusOK { t.Fatalf("dashboard status = %d; body=%s", dashboardResponse.Code, dashboardResponse.Body.String()) }
	if dashboardResponse.Header().Get("Content-Security-Policy") == "" || dashboardResponse.Header().Get("X-Frame-Options") != "DENY" { t.Fatal("expected UI security headers") }
	if !strings.Contains(dashboardResponse.Body.String(), "Worker A") { t.Fatal("dashboard omitted worker inventory") }
}

func TestLoginRejectsInvalidToken(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"token":{"wrong"}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	testUI().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized { t.Fatalf("status = %d, want 401", response.Code) }
	if len(response.Result().Cookies()) != 0 { t.Fatal("invalid login issued a session cookie") }
}

func TestNewVMFormHasStableIdempotencyKey(t *testing.T) {
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	testUI().ServeHTTP(loginResponse, login)
	cookie := loginResponse.Result().Cookies()[0]
	request := httptest.NewRequest(http.MethodGet, "/vms/new", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	testUI().ServeHTTP(response, request)
	if response.Code != http.StatusOK { t.Fatalf("status = %d", response.Code) }
	if !strings.Contains(response.Body.String(), "name=\"idempotency_key\"") { t.Fatal("VM form missing idempotency key") }
}
