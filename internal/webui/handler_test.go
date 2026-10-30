package webui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/controllerapi"
	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
	"github.com/InfX2243/A3-PS023-TC061/internal/ec2"
)

type fakeUIService struct { registeredID string; registeredName string; registeredEndpoint string; registeredMemory uint64; deletedID string; createCalls int; lastKey string; startCalls int; stopCalls int; vms []domain.VM; latest domain.Operation; latestErr error }
func (s *fakeUIService) RegisterWorker(_ context.Context, id, name, endpoint string, memory uint64) error {
	s.registeredID, s.registeredName, s.registeredEndpoint, s.registeredMemory = id, name, endpoint, memory
	return nil
}
func (s *fakeUIService) DeleteWorker(_ context.Context, id string) error { s.deletedID = id; return nil }

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
	testUI().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/console", nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" { t.Fatalf("unauthenticated dashboard status/location = %d/%q", response.Code, response.Header().Get("Location")) }

	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	testUI().ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusSeeOther { t.Fatalf("login status = %d", loginResponse.Code) }
	cookie := loginResponse.Result().Cookies()[0]
	dashboard := httptest.NewRequest(http.MethodGet, "/console", nil)
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
	cookieRequest := httptest.NewRequest(http.MethodGet, "/console", nil)
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
		vms: []domain.VM{{ID:"vm-1", Name:"demo", WorkerID:"worker-a", VCPUs:1, MemoryBytes:512*1024*1024, ImageID:"ubuntu-demo", Observed:domain.VMUnknown}},
		latest: domain.Operation{ID:"op-1", Type:domain.OperationStart, State:domain.OperationUnknown},
	}
	mux := http.NewServeMux()
	h := NewHandler(service, "operator-secret", Limits{MaxVCPUs:8, MaxMemoryMiB:8192, AllowedImages:[]string{"ubuntu-demo"}}, false)
	h.Register(mux)
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	mux.ServeHTTP(loginResponse, login)
	request := httptest.NewRequest(http.MethodGet, "/console", nil)
	request.AddCookie(loginResponse.Result().Cookies()[0])
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK { t.Fatalf("dashboard status=%d", response.Code) }
	if !strings.Contains(response.Body.String(), "http-equiv=\"refresh\" content=\"5\"") { t.Fatal("dashboard did not refresh for unknown operation") }
	for _, expected := range []string{"demo", "worker-a", "ubuntu-demo", "start: unknown"} { if !strings.Contains(response.Body.String(), expected) { t.Fatalf("dashboard omitted %q", expected) } }
	if strings.Contains(response.Body.String(), ">Start</button>") || strings.Contains(response.Body.String(), ">Stop</button>") { t.Fatal("dashboard offered lifecycle action for unknown state") }
}

func TestDocumentationIsPublicAndLinksToOperatorConsole(t *testing.T) {
	response := httptest.NewRecorder()
	testUI().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("documentation status = %d; body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{"Platform overview", "Quick start", "Architecture and request flow", "Lifecycle and operation states", "Security model", "Current limitations", "href=\"/login\"", "id=\"main-content\"", "Skip to main content", "/console", "aria-current=\"location\"", "font-size:16px", "/assets/ui.js", "docs-reading-progress", "docs-search", "Find a topic", "docs-back-top"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("documentation page omitted %q", expected)
		}
	}
	if strings.Contains(body, "name=\"token\"") {
		t.Fatal("public documentation should not render the operator login form")
	}
}

func TestDashboardNavigationIncludesDocumentationAndAvoidsDuplicateHero(t *testing.T) {
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	testUI().ServeHTTP(loginResponse, login)
	request := httptest.NewRequest(http.MethodGet, "/console", nil)
	request.AddCookie(loginResponse.Result().Cookies()[0])
	response := httptest.NewRecorder()
	testUI().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, "href=\"/docs\"") {
		t.Fatal("dashboard navigation omitted documentation link")
	}
	if strings.Contains(body, "Your compute fleet, at a glance.") {
		t.Fatal("dashboard retained the duplicate hero message")
	}
	if strings.Contains(body, "Operational signals") {
		t.Fatal("dashboard retained a redundant inventory summary")
	}
}

func TestLandingPageIsPublicAndHasDistinctDocumentationAndLoginRoutes(t *testing.T) {
	response := httptest.NewRecorder()
	testUI().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("landing status = %d", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{"Infrastructure,", "href=\"/docs\"", "href=\"/login\"", "id=\"platform\"", "id=\"how-it-works\"", "data-preview-tab=\"fleet\"", "data-preview-panel=\"capacity\"", "Illustrative interface preview"} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(expected)) {
			t.Fatalf("landing page omitted %q", expected)
		}
	}
	if strings.Contains(body, "name=\"token\"") {
		t.Fatal("public landing page should not contain the operator login form")
	}
}

func TestLoginPageHasAccessiblePremiumAuthExperience(t *testing.T) {
	_, handler := newTestUI()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/login", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("login page status = %d", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{"auth-page", "auth-showcase", "auth-content", "SECURE OPERATOR ACCESS", "data-password-toggle=\"token\"", "auth-assurance", "href=\"/docs\"", "Skip to main content", "src=\"/assets/ui.js\""} {
		if !strings.Contains(body, expected) {
			t.Errorf("login page omitted %q", expected)
		}
	}
	if strings.Contains(body, "name=\"username\"") || strings.Contains(body, "name=\"password\"") {
		t.Fatal("token-auth login should not render username/password fields")
	}
}

func TestCredentialLoginPageRendersUsernamePasswordAndToggle(t *testing.T) {
	mux := http.NewServeMux()
	h := NewHandlerWithCredentials(&fakeUIService{}, "session-signing-secret", "operator", "correct-horse-battery-staple-2026", Limits{}, false)
	h.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/login", nil))
	body := response.Body.String()
	for _, expected := range []string{"name=\"username\"", "name=\"password\"", "autocomplete=\"username\"", "autocomplete=\"current-password\"", "data-password-toggle=\"password\""} {
		if !strings.Contains(body, expected) {
			t.Errorf("credential login page omitted %q", expected)
		}
	}
}

func TestLoginRedirectsToOperatorConsole(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	testUI().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/console" {
		t.Fatalf("login status/location = %d/%q, want 303 /console", response.Code, response.Header().Get("Location"))
	}
}


func TestUIEnhancementAssetAndFormLoadingStates(t *testing.T) {
	_, handler := newTestUI()
	assetResponse := httptest.NewRecorder()
	handler.ServeHTTP(assetResponse, httptest.NewRequest(http.MethodGet, "/assets/ui.js", nil))
	if assetResponse.Code != http.StatusOK {
		t.Fatalf("asset status = %d", assetResponse.Code)
	}
	if !strings.Contains(assetResponse.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("asset content type = %q", assetResponse.Header().Get("Content-Type"))
	}
	asset := assetResponse.Body.String()
	if strings.Contains(asset, "\\n") {
		t.Fatal("enhancement asset contains escaped line breaks instead of JavaScript source lines")
	}
	if !strings.HasPrefix(asset, "// Progressive enhancements for the server-rendered operator UI.\n(function () {") {
		t.Fatal("enhancement asset is not emitted as a valid source file")
	}
	for _, expected := range []string{"IntersectionObserver", "aria-current", "Signing in…", "Signing out…", "prefers-reduced-motion", "initDocsInteractions", "docs-code-copy", "clipboard.writeText"} {
		if !strings.Contains(asset, expected) {
			t.Fatalf("enhancement asset omitted %q", expected)
		}
	}

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodGet, "/login", nil))
	if !strings.Contains(loginResponse.Body.String(), "src=\"/assets/ui.js\"") {
		t.Fatal("login page does not load progressive form feedback")
	}
	if !strings.Contains(loginResponse.Body.String(), "ui-button-spinner") {
		t.Fatal("login page does not include visible loading indicator styles")
	}
	vmLogin := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	vmLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	vmLoginResponse := httptest.NewRecorder()
	handler.ServeHTTP(vmLoginResponse, vmLogin)
	vmRequest := httptest.NewRequest(http.MethodGet, "/vms/new", nil)
	vmRequest.AddCookie(vmLoginResponse.Result().Cookies()[0])
	vmResponse := httptest.NewRecorder()
	handler.ServeHTTP(vmResponse, vmRequest)
	if vmResponse.Code != http.StatusOK || !strings.Contains(vmResponse.Body.String(), "src=\"/assets/ui.js\"") {
		t.Fatalf("VM form did not load form feedback: status=%d", vmResponse.Code)
	}

}

func TestPrimaryPagesExposeKeyboardSkipNavigation(t *testing.T) {
	_, handler := newTestUI()
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	cookie := loginResponse.Result().Cookies()[0]

	pages := []struct {
		path string
		cookie bool
	}{
		{path: "/", cookie: false},
		{path: "/docs", cookie: false},
		{path: "/login", cookie: false},
		{path: "/console", cookie: true},
		{path: "/vms/new", cookie: true},
	}
	for _, page := range pages {
		t.Run(page.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, page.path, nil)
			if page.cookie {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d", page.path, response.Code)
			}
			body := response.Body.String()
			if !strings.Contains(body, "href=\"#main-content\"") || !strings.Contains(body, "id=\"main-content\"") {
				t.Fatalf("GET %s is missing a working main-content skip link", page.path)
			}
		})
	}
}

func TestWorkerInventoryRequiresAuthenticationAndRendersRegistrationForm(t *testing.T) {
	_, handler := newTestUI()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/workers", nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
		t.Fatalf("unauthenticated worker inventory = %d, location %q", response.Code, response.Header().Get("Location"))
	}
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	cookie := loginResponse.Result().Cookies()[0]
	request := httptest.NewRequest(http.MethodGet, "/workers", nil)
	request.AddCookie(cookie)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, request)
	if page.Code != http.StatusOK { t.Fatalf("worker inventory status = %d; body=%s", page.Code, page.Body.String()) }
	for _, want := range []string{"Register an existing worker manually", "memory_limit_mib", "Agent endpoint"} {
		if !strings.Contains(page.Body.String(), want) { t.Errorf("worker page missing %q", want) }
	}
}

func TestRegisterWorkerRequiresCSRFAndPersistsThroughService(t *testing.T) {
	h, handler := newTestUI()
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	cookie := loginResponse.Result().Cookies()[0]
	get := httptest.NewRequest(http.MethodGet, "/workers", nil)
	get.AddCookie(cookie)
	csrf := h.csrfToken(get)
	form := url.Values{"csrf_token": {csrf}, "id": {"worker-test"}, "name": {"Test Worker"}, "endpoint": {"https://worker.example.internal:9090"}, "memory_limit_mib": {"7168"}}
	request := httptest.NewRequest(http.MethodPost, "/workers", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || !strings.HasPrefix(response.Header().Get("Location"), "/workers?notice=") {
		t.Fatalf("registration response = %d, location=%q, body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	fake := h.service.(*fakeUIService)
	if fake.registeredID != "worker-test" || fake.registeredName != "Test Worker" || fake.registeredEndpoint != "https://worker.example.internal:9090" || fake.registeredMemory != 7168 {
		t.Fatalf("unexpected worker registration: %#v", fake)
	}
}

func TestRegisterWorkerRejectsMissingCSRF(t *testing.T) {
	h, handler := newTestUI()
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	cookie := loginResponse.Result().Cookies()[0]
	form := url.Values{"id": {"worker-test"}, "name": {"Test Worker"}, "endpoint": {"file:///etc/passwd"}, "memory_limit_mib": {"1024"}}
	request := httptest.NewRequest(http.MethodPost, "/workers", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden { t.Fatalf("missing CSRF response = %d, want 403", response.Code) }
	if h.service.(*fakeUIService).registeredID != "" { t.Fatal("worker registration happened without CSRF token") }
}

func TestUsernamePasswordLogin(t *testing.T) {
	mux := http.NewServeMux()
	h := NewHandlerWithCredentials(&fakeUIService{}, "session-signing-secret", "operator", "correct-horse-battery-staple-2026", Limits{MaxVCPUs:8, MaxMemoryMiB:8192, AllowedImages:[]string{"ubuntu-demo"}}, true)
	h.Register(mux)
	wrong := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"username":{"operator"}, "password":{"wrong-password"}}.Encode()))
	wrong.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wrongResponse := httptest.NewRecorder()
	mux.ServeHTTP(wrongResponse, wrong)
	if wrongResponse.Code != http.StatusUnauthorized { t.Fatalf("wrong password status = %d, want 401", wrongResponse.Code) }
	if len(wrongResponse.Result().Cookies()) != 0 { t.Fatal("wrong password issued a session cookie") }

	good := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"username":{"operator"}, "password":{"correct-horse-battery-staple-2026"}}.Encode()))
	good.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	goodResponse := httptest.NewRecorder()
	mux.ServeHTTP(goodResponse, good)
	if goodResponse.Code != http.StatusSeeOther || goodResponse.Header().Get("Location") != "/console" { t.Fatalf("valid credentials response = %d, location %q", goodResponse.Code, goodResponse.Header().Get("Location")) }
	cookies := goodResponse.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie attributes not secure: %#v", cookies)
	}
}

type fakeEC2Inventory struct { instances []ec2.Instance }
func (f *fakeEC2Inventory) Launch(context.Context, ec2.LaunchInput) (ec2.Instance, error) { return ec2.Instance{}, nil }
func (f *fakeEC2Inventory) Describe(_ context.Context, _ string, id string) (ec2.Instance, error) {
	for _, instance := range f.instances { if instance.ID == id { return instance, nil } }
	return ec2.Instance{}, errors.New("not found")
}
func (f *fakeEC2Inventory) Action(context.Context, string, string, string) error { return nil }
func (f *fakeEC2Inventory) ListManaged(context.Context, string) ([]ec2.Instance, error) { return f.instances, nil }

func TestAWSPageShowsManagedInstanceInventoryAndIndependentScrollRegion(t *testing.T) {
	h, handler := newTestUI()
	h.SetEC2Provisioner(&fakeEC2Inventory{instances: []ec2.Instance{{
		ID: "i-node123", Name: "aiori-worker", Type: "t3.micro", PublicIP: "203.0.113.8", PrivateIP: "10.0.0.8",
		State: struct{ Name string }{Name: "running"},
	}, {ID: "worker-a", Name: "Registered worker", Type: "t3.small", State: struct{ Name string }{Name: "running"}}}})
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	request := httptest.NewRequest(http.MethodGet, "/aws?region=ap-south-1", nil)
	request.AddCookie(loginResponse.Result().Cookies()[0])
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK { t.Fatalf("AWS page status=%d body=%s", response.Code, response.Body.String()) }
	body := response.Body.String()
	for _, expected := range []string{"Managed EC2 inventory", "aiori-worker", "i-node123", "203.0.113.8", "Registered worker", "healthy", "aws-table-scroll", "Refresh inventory from AWS"} {
		if !strings.Contains(body, expected) { t.Fatalf("AWS page omitted %q", expected) }
	}
}

func TestSharedDesignSystemStylesheetIsServedAndApplied(t *testing.T) {
	_, handler := newTestUI()

	stylesheet := httptest.NewRecorder()
	handler.ServeHTTP(stylesheet, httptest.NewRequest(http.MethodGet, "/assets/ui.css", nil))
	if stylesheet.Code != http.StatusOK {
		t.Fatalf("stylesheet status = %d", stylesheet.Code)
	}
	if !strings.Contains(stylesheet.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("stylesheet content type = %q", stylesheet.Header().Get("Content-Type"))
	}
	for _, token := range []string{"--ai-purple: #5145e5", ".sidebar-nav", ".marketing-hero", "@media (max-width:620px)"} {
		if !strings.Contains(stylesheet.Body.String(), token) {
			t.Fatalf("design system stylesheet omitted %q", token)
		}
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token=operator-secret"))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("test login status = %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	pages := []string{"/", "/docs", "/login", "/console", "/workers", "/aws", "/vms/new"}
	for _, path := range pages {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if path != "/" && path != "/docs" && path != "/login" {
			request.AddCookie(cookie)
		}
		page := httptest.NewRecorder()
		handler.ServeHTTP(page, request)
		if page.Code != http.StatusOK {
			t.Fatalf("%s returned status %d", path, page.Code)
		}
		if !strings.Contains(page.Body.String(), `<link rel="stylesheet" href="/assets/ui.css">`) {
			t.Fatalf("%s did not receive the shared design system stylesheet", path)
		}
	}
}
