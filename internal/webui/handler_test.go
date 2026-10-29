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

type fakeUIService struct { createCalls int; lastKey string; startCalls int; stopCalls int; vms []domain.VM; latest domain.Operation; latestErr error }
func (*fakeUIService) ListWorkers(context.Context) ([]domain.Worker, error) {
	return []domain.Worker{{ID:"worker-a", Name:"Worker A", State:domain.WorkerHealthy, AvailableMemoryBytes:2*1024*1024*1024, MemoryLimitBytes:4*1024*1024*1024}}, nil
}
func (s *fakeUIService) ListVMs(context.Context) ([]domain.VM, error) { return s.vms, nil }
func (s *fakeUIService) LatestOperation(context.Context, string) (domain.Operation, error) { if s.latestErr != nil { return domain.Operation{}, s.latestErr }; return s.latest, nil }
func (s *fakeUIService) CreateVM(_ context.Context, _ controllerapi.CreateVMRequest, key string) (domain.VM, domain.Operation, error) {
	s.createCalls++
	s.lastKey = key
	now := time.Now()
	return domain.VM{ID:"vm-1", Name:"demo", Observed:domain.VMStopped, CreatedAt:now, UpdatedAt:now}, domain.Operation{ID:"op-1", State:domain.OperationSucceeded}, nil
}
func (s *fakeUIService) StartVM(context.Context, string) (domain.VM, domain.Operation, error) { s.startCalls++; return domain.VM{}, domain.Operation{ID:"op-start", State:domain.OperationSucceeded}, nil }
func (s *fakeUIService) StopVM(context.Context, string) (domain.VM, domain.Operation, error) { s.stopCalls++; return domain.VM{}, domain.Operation{ID:"op-stop", State:domain.OperationSucceeded}, nil }

func testUI() http.Handler {
	_, handler := newTestUI()
	return handler
}

func newTestUI() (*Handler, http.Handler) {
	mux := http.NewServeMux()
	h := NewHandler(&fakeUIService{}, "operator-secret", Limits{MaxVCPUs:8, MaxMemoryMiB:8192, AllowedImages:[]string{"ubuntu-demo"}}, false)
	h.Register(mux)
	return h, mux
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

func TestCreateFormRequiresCSRFAndPassesStableIdempotencyKey(t *testing.T) {
	h, handler := newTestUI()
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	cookie := loginResponse.Result().Cookies()[0]
	values := url.Values{"name":{"demo"}, "vcpus":{"1"}, "memory_mib":{"512"}, "image_id":{"ubuntu-demo"}, "idempotency_key":{"form-key-1"}}
	withoutCSRF := httptest.NewRequest(http.MethodPost, "/vms", strings.NewReader(values.Encode()))
	withoutCSRF.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withoutCSRF.AddCookie(cookie)
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, withoutCSRF)
	if rejected.Code != http.StatusForbidden { t.Fatalf("missing CSRF status=%d, want 403", rejected.Code) }
	cookieRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	cookieRequest.AddCookie(cookie)
	values.Set("csrf_token", h.csrfToken(cookieRequest))
	request := httptest.NewRequest(http.MethodPost, "/vms", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther { t.Fatalf("valid create status=%d; body=%s", response.Code, response.Body.String()) }
	service := h.service.(*fakeUIService)
	if service.createCalls != 1 || service.lastKey != "form-key-1" { t.Fatalf("create calls/key=%d/%q", service.createCalls, service.lastKey) }
}

func TestLifecycleActionsRequireCSRF(t *testing.T) {
	h, handler := newTestUI()
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	cookie := loginResponse.Result().Cookies()[0]
	request := httptest.NewRequest(http.MethodPost, "/vms/vm-1/start", strings.NewReader(""))
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden { t.Fatalf("missing CSRF status=%d, want 403", response.Code) }
	service := h.service.(*fakeUIService)
	if service.startCalls != 0 { t.Fatalf("start called %d times without CSRF", service.startCalls) }
	cookieRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	cookieRequest.AddCookie(cookie)
	values := url.Values{"csrf_token":{h.csrfToken(cookieRequest)}}
	request = httptest.NewRequest(http.MethodPost, "/vms/vm-1/start", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther { t.Fatalf("valid start status=%d", response.Code) }
	if service.startCalls != 1 { t.Fatalf("start calls=%d, want 1", service.startCalls) }
}

func TestDashboardRefreshesWhileOperationIsUnknown(t *testing.T) {
	service := &fakeUIService{
		vms: []domain.VM{{ID:"vm-1", Name:"demo", Observed:domain.VMUnknown, MemoryBytes:512*1024*1024}},
		latest: domain.Operation{ID:"op-1", Type:domain.OperationStart, State:domain.OperationUnknown},
	}
	mux := http.NewServeMux()
	h := NewHandler(service, "operator-secret", Limits{MaxVCPUs:8, MaxMemoryMiB:8192, AllowedImages:[]string{"ubuntu-demo"}}, false)
	h.Register(mux)
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	mux.ServeHTTP(loginResponse, login)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(loginResponse.Result().Cookies()[0])
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK { t.Fatalf("dashboard status=%d", response.Code) }
	if !strings.Contains(response.Body.String(), "http-equiv=\"refresh\" content=\"5\"") { t.Fatal("dashboard did not refresh for unknown operation") }
	if strings.Contains(response.Body.String(), ">Start</button>") || strings.Contains(response.Body.String(), ">Stop</button>") { t.Fatal("dashboard offered lifecycle action for unknown state") }
}
