package webui

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/controllerapi"
	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
	"github.com/InfX2243/A3-PS023-TC061/internal/ec2"
)

type Service interface {
	ListWorkers(context.Context) ([]domain.Worker, error)
	ListVMs(context.Context) ([]domain.VM, error)
	LatestOperation(context.Context, string) (domain.Operation, error)
	CreateVM(context.Context, controllerapi.CreateVMRequest, string) (domain.VM, domain.Operation, error)
	StartVM(context.Context, string) (domain.VM, domain.Operation, error)
	StopVM(context.Context, string) (domain.VM, domain.Operation, error)
}

type Limits struct {
	MaxVCPUs int
	MaxMemoryMiB uint64
	AllowedImages []string
}

type Handler struct {
	service Service
	token string
	loginUsername string
	loginPassword string
	limits Limits
	secureCookie bool
	ec2Client ec2Provisioner
	ec2Bootstrap ec2.BootstrapConfig
	templates *template.Template
}

type vmRow struct { VM domain.VM; Operation domain.Operation; HasOperation bool }
type dashboardData struct { Workers []domain.Worker; VMs []vmRow; Error string; Notice string; Refreshing bool; CSRFToken string }
type formData struct { Limits Limits; Error string; IdempotencyKey string; CSRFToken string }
type workerPageData struct { Workers []domain.Worker; Error string; Notice string; CSRFToken string }
type vmInventoryPageData struct { VMs []vmRow; Error string; Notice string; Refreshing bool; CSRFToken string }
type ec2Provisioner interface { Launch(context.Context, ec2.LaunchInput) (ec2.Instance,error); Describe(context.Context,string,string) (ec2.Instance,error); Action(context.Context,string,string,string) error }
type ec2InventoryProvisioner interface { ListManaged(context.Context, string) ([]ec2.Instance, error) }
type ec2IngressProvisioner interface { AllowControllerIngress(context.Context, string, []string, string, int) error }
type workerManagementService interface {
	RegisterWorker(context.Context, string, string, string, uint64) error
	DeleteWorker(context.Context, string) error
}

func NewHandler(service Service, operatorToken string, limits Limits, secureCookie bool) *Handler {
	return NewHandlerWithCredentials(service, operatorToken, "", "", limits, secureCookie)
}

func NewHandlerWithCredentials(service Service, operatorToken, username, password string, limits Limits, secureCookie bool) *Handler {
	return &Handler{service: service, token: operatorToken, loginUsername: username, loginPassword: password, limits: limits, secureCookie: secureCookie, templates: template.Must(template.New("pages").Funcs(template.FuncMap{"divMiB": func(v uint64) uint64 { return v / (1024 * 1024) }, "stateIs": func(state domain.VMState, expected string) bool { return string(state) == expected }, "countWorkerState": func(workers []domain.Worker, expected string) int { count := 0; for _, worker := range workers { if string(worker.State) == expected { count++ } }; return count }, "countVMState": func(vms []vmRow, expected string) int { count := 0; for _, vm := range vms { if string(vm.VM.Observed) == expected { count++ } }; return count }, "memoryPercent": func(available, limit uint64) int { if limit == 0 { return 0 }; percent := int(available * 100 / limit); if percent > 100 { return 100 }; if percent < 0 { return 0 }; return percent }, "percentOf": func(value, total int) int { if total <= 0 || value <= 0 { return 0 }; percent := value * 100 / total; if percent > 100 { return 100 }; return percent }, "countNonHealthyWorkers": func(workers []domain.Worker) int { count := 0; for _, worker := range workers { if string(worker.State) != "healthy" { count++ } }; return count }, "countActiveOperations": func(vms []vmRow) int { count := 0; for _, vm := range vms { if vm.HasOperation && (vm.Operation.State == domain.OperationPending || vm.Operation.State == domain.OperationDispatched || vm.Operation.State == domain.OperationUnknown) { count++ } }; return count }}).Parse(pageTemplates + docsPageTemplate + workersPageTemplate + awsPageTemplate + sharedNavTemplate + workerInventoryTemplate + vmInventoryTemplate))}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /docs", h.docsPage)
	mux.HandleFunc("GET /docs/", h.docsPage)
	mux.HandleFunc("GET /assets/ui.js", h.uiScript)
	mux.HandleFunc("GET /assets/ui.css", h.uiStylesheet)
	mux.HandleFunc("GET /login", h.loginPage)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("POST /logout", h.logout)
	mux.HandleFunc("GET /", h.landingPage)
	mux.HandleFunc("GET /console", h.dashboard)
	mux.HandleFunc("GET /console/", h.dashboard)
	mux.HandleFunc("GET /workers", h.workersPage)
	mux.HandleFunc("GET /vms", h.vmInventoryPage)
	mux.HandleFunc("GET /aws", h.awsPage)
	mux.HandleFunc("POST /aws/instances", h.launchEC2)
	mux.HandleFunc("POST /aws/instances/{instanceID}/{action}", h.ec2Action)
	mux.HandleFunc("POST /workers", h.registerWorker)
	mux.HandleFunc("POST /workers/{workerID}/delete", h.deleteWorker)
	mux.HandleFunc("GET /vms/new", h.newVMPage)
	mux.HandleFunc("POST /vms", h.createVM)
	mux.HandleFunc("POST /vms/{vmID}/start", h.startVM)
	mux.HandleFunc("POST /vms/{vmID}/stop", h.stopVM)
}

func (h *Handler) landingPage(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	h.render(w, "landing", nil)
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	h.render(w, "login", map[string]any{"Error": "", "UseCredentials": h.loginUsername != "" && h.loginPassword != ""})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if h.token == "" {
		h.renderStatus(w, http.StatusServiceUnavailable, "login", map[string]any{"Error": "Operator authentication is not configured. Set VM_CONTROLLER_OPERATOR_TOKEN.", "UseCredentials": h.loginUsername != "" && h.loginPassword != ""})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := r.ParseForm(); err != nil {
		h.renderStatus(w, http.StatusBadRequest, "login", map[string]any{"Error": "Invalid login form.", "UseCredentials": h.loginUsername != "" && h.loginPassword != ""})
		return
	}
	valid := false
	if h.loginUsername != "" && h.loginPassword != "" {
		gotUser, gotPassword := r.FormValue("username"), r.FormValue("password")
		userOK := len(gotUser) == len(h.loginUsername) && subtle.ConstantTimeCompare([]byte(gotUser), []byte(h.loginUsername)) == 1
		passwordOK := len(gotPassword) == len(h.loginPassword) && subtle.ConstantTimeCompare([]byte(gotPassword), []byte(h.loginPassword)) == 1
		valid = userOK && passwordOK
	} else {
		got := r.FormValue("token")
		valid = len(got) == len(h.token) && subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) == 1
	}
	if !valid {
		h.renderStatus(w, http.StatusUnauthorized, "login", map[string]any{"Error": "Invalid credentials.", "UseCredentials": h.loginUsername != "" && h.loginPassword != ""})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: h.newSession(), Path: "/", HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: int((12*time.Hour).Seconds())})
	http.Redirect(w, r, "/console", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := r.ParseForm(); err != nil || !h.verifyCSRF(r) { http.Error(w, "invalid request token", http.StatusForbidden); return }
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	if h.service == nil { h.renderStatus(w, http.StatusServiceUnavailable, "dashboard", dashboardData{Error:"Controller service is not initialized.", CSRFToken:h.csrfToken(r)}); return }
	workers, err := h.service.ListWorkers(r.Context())
	if err != nil { h.renderStatus(w, http.StatusInternalServerError, "dashboard", dashboardData{Error:"Could not load worker inventory.", CSRFToken:h.csrfToken(r)}); return }
	vms, err := h.service.ListVMs(r.Context())
	if err != nil { h.renderStatus(w, http.StatusInternalServerError, "dashboard", dashboardData{Workers:workers, Error:"Could not load VM inventory.", CSRFToken:h.csrfToken(r)}); return }
	rows := make([]vmRow, 0, len(vms))
	refreshing := false
	for _, vm := range vms {
		op, err := h.service.LatestOperation(r.Context(), vm.ID)
		if err == nil && (op.State == domain.OperationPending || op.State == domain.OperationDispatched || op.State == domain.OperationUnknown) { refreshing = true }
		rows = append(rows, vmRow{VM:vm, Operation:op, HasOperation:err == nil})
	}
	h.render(w, "dashboard", dashboardData{Workers:workers, VMs:rows, Notice:r.URL.Query().Get("notice"), Error:r.URL.Query().Get("error"), Refreshing:refreshing, CSRFToken:h.csrfToken(r)})
}

func (h *Handler) SetEC2Provisioner(client ec2Provisioner) { h.ec2Client = client }
func (h *Handler) SetEC2Bootstrap(config ec2.BootstrapConfig) { h.ec2Bootstrap = config }
type ec2InventoryRow struct { Instance ec2.Instance; Worker domain.Worker; HasWorker bool }
type awsPageData struct { Instance ec2.Instance; HasInstance bool; Agent domain.Worker; HasAgent bool; Instances []ec2InventoryRow; InventoryError string; BootstrapEnabled bool; UserData string; Region, Error, Notice, CSRFToken, IdempotencyKey string }

func (h *Handler) awsPage(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	region := r.URL.Query().Get("region")
	if region == "" { region = os.Getenv("AWS_REGION"); if region == "" { region = os.Getenv("AWS_DEFAULT_REGION") } }
	data := awsPageData{Region: region, Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"), CSRFToken: h.csrfToken(r), IdempotencyKey: newFormID(), Instances: []ec2InventoryRow{}, BootstrapEnabled: h.ec2Bootstrap.Enabled()}
	workersByID := make(map[string]domain.Worker)
	if h.service != nil {
		workers, err := h.service.ListWorkers(r.Context())
		if err != nil { data.InventoryError = "Could not load worker-agent health: " + err.Error() } else {
			for _, worker := range workers { workersByID[worker.ID] = worker }
		}
	}
	if h.ec2Client == nil {
		if data.Error == "" { data.Error = "EC2 provisioning is unavailable. Configure AWS_REGION and the AWS SDK credential chain on the controller." }
	} else if inventory, ok := h.ec2Client.(ec2InventoryProvisioner); ok {
		instances, err := inventory.ListManaged(r.Context(), data.Region)
		if err != nil { data.InventoryError = "Could not load managed EC2 inventory: " + err.Error() } else {
			for _, instance := range instances {
				worker, enrolled := workersByID[instance.ID]
				data.Instances = append(data.Instances, ec2InventoryRow{Instance: instance, Worker: worker, HasWorker: enrolled})
			}
		}
	} else {
		data.InventoryError = "The configured EC2 provider does not support instance inventory."
	}
	if id := r.URL.Query().Get("instance_id"); id != "" {
		if h.ec2Client == nil { data.Error = "EC2 provisioning is not configured." } else if instance, err := h.ec2Client.Describe(r.Context(), data.Region, id); err != nil { data.Error = "Could not read EC2 lifecycle state: " + err.Error() } else {
			data.Instance = instance
			data.HasInstance = true
			data.Agent, data.HasAgent = workersByID[instance.ID]
		}
	}
	if data.BootstrapEnabled {
		bootstrap := h.ec2Bootstrap
		bootstrap.NodeName = strings.TrimSpace(r.URL.Query().Get("name"))
		if data.HasInstance && data.Instance.Name != "" { bootstrap.NodeName = data.Instance.Name }
		if bootstrap.NodeName == "" { bootstrap.NodeName = "aiori-worker-test" }
		if script, err := bootstrap.BuildUserData(); err == nil { data.UserData = script } else { data.InventoryError = "Worker bootstrap configuration is invalid: " + err.Error() }
	}
	h.render(w, "aws", data)
}
func (h *Handler) launchEC2(w http.ResponseWriter,r *http.Request) {
 h.headers(w); if !h.authenticated(r) { http.Redirect(w,r,"/login",http.StatusSeeOther); return }
 r.Body=http.MaxBytesReader(w,r.Body,16*1024); if err:=r.ParseForm(); err!=nil || !h.verifyCSRF(r) { http.Error(w,"invalid request token or form",http.StatusForbidden); return }
 if h.ec2Client==nil { http.Error(w,"EC2 provisioning unavailable",http.StatusServiceUnavailable); return }
 if !safeFormID(r.FormValue("client_token")) { http.Error(w,"invalid launch token; reload the form",http.StatusBadRequest); return }
 groups:=strings.Fields(strings.ReplaceAll(r.FormValue("security_group_ids"),","," "))
 in:=ec2.LaunchInput{Name:strings.TrimSpace(r.FormValue("name")),Region:strings.TrimSpace(r.FormValue("region")),ImageID:strings.TrimSpace(r.FormValue("image_id")),InstanceType:r.FormValue("instance_type"),SubnetID:strings.TrimSpace(r.FormValue("subnet_id")),SecurityGroupIDs:groups,KeyName:strings.TrimSpace(r.FormValue("key_name")),InstanceProfile:strings.TrimSpace(r.FormValue("instance_profile")),ClientToken:r.FormValue("client_token")}
 if h.ec2Bootstrap.Enabled() { if in.InstanceProfile == "" { http.Redirect(w,r,"/aws?error="+url.QueryEscape("Worker bootstrap requires an EC2 instance profile with ssm:GetParameter permission for the configured SecureString parameter.")+"&region="+url.QueryEscape(in.Region),http.StatusSeeOther); return }; bootstrap := h.ec2Bootstrap; bootstrap.NodeName = in.Name; userData, err := bootstrap.BuildUserData(); if err != nil { http.Redirect(w,r,"/aws?error="+url.QueryEscape("Worker bootstrap configuration is invalid: "+err.Error())+"&region="+url.QueryEscape(in.Region),http.StatusSeeOther); return }; in.UserData = userData }
 instance,err:=h.ec2Client.Launch(r.Context(),in); if err!=nil { http.Redirect(w,r,"/aws?error="+url.QueryEscape(err.Error())+"&region="+url.QueryEscape(in.Region),http.StatusSeeOther); return }
 notice := "EC2 launch accepted without worker-agent bootstrap; this instance is not an enrolled worker."
 if h.ec2Bootstrap.Enabled() {
   ingress, ok := h.ec2Client.(ec2IngressProvisioner)
   if !ok { http.Redirect(w,r,"/aws?instance_id="+url.QueryEscape(instance.ID)+"&region="+url.QueryEscape(in.Region)+"&error="+url.QueryEscape("Instance launched, but the provider cannot configure private worker-agent ingress."),http.StatusSeeOther); return }
   if err := ingress.AllowControllerIngress(r.Context(), in.Region, instance.SecurityGroups, h.ec2Bootstrap.ControllerSecurityGroupID, h.ec2Bootstrap.AgentPort); err != nil {
     http.Redirect(w,r,"/aws?instance_id="+url.QueryEscape(instance.ID)+"&region="+url.QueryEscape(in.Region)+"&error="+url.QueryEscape("Instance launched with user data, but controller-to-worker network access could not be configured: "+err.Error()),http.StatusSeeOther); return
   }
   notice = "EC2 launch accepted with checksum-verified worker bootstrap and private controller-only agent ingress. Wait for cloud-init and the first authenticated heartbeat; agent health is shown separately in Worker inventory."
 }
 http.Redirect(w,r,"/aws?instance_id="+url.QueryEscape(instance.ID)+"&region="+url.QueryEscape(in.Region)+"&notice="+url.QueryEscape(notice)+"&name="+url.QueryEscape(in.Name),http.StatusSeeOther)
}

func (h *Handler) ec2Action(w http.ResponseWriter,r *http.Request) {
 h.headers(w); if !h.authenticated(r) { http.Redirect(w,r,"/login",http.StatusSeeOther); return }
 r.Body=http.MaxBytesReader(w,r.Body,16*1024); if err:=r.ParseForm(); err!=nil || !h.verifyCSRF(r) { http.Error(w,"invalid request token",http.StatusForbidden); return }
 action,id:=r.PathValue("action"),r.PathValue("instanceID"); if action!="start" && action!="stop" && action!="terminate" { http.Error(w,"unsupported EC2 action",http.StatusBadRequest); return }
 if h.ec2Client==nil { http.Error(w,"EC2 provisioning unavailable",http.StatusServiceUnavailable); return }
 region:=strings.TrimSpace(r.FormValue("region")); if err:=h.ec2Client.Action(r.Context(),region,id,action); err!=nil { http.Redirect(w,r,"/aws?instance_id="+url.QueryEscape(id)+"&region="+url.QueryEscape(region)+"&error="+url.QueryEscape(err.Error()),http.StatusSeeOther); return }
 http.Redirect(w,r,"/aws?instance_id="+url.QueryEscape(id)+"&region="+url.QueryEscape(region)+"&notice="+url.QueryEscape("EC2 "+action+" request accepted; refresh to confirm state."),http.StatusSeeOther)
}
func (h *Handler) workersPage(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	admin, ok := h.service.(workerManagementService)
	if !ok || admin == nil {
		h.renderStatus(w, http.StatusServiceUnavailable, "workers", workerPageData{Error:"Worker registration is unavailable in this controller instance.", CSRFToken:h.csrfToken(r)})
		return
	}
	workers, err := h.service.ListWorkers(r.Context())
	if err != nil {
		h.renderStatus(w, http.StatusInternalServerError, "workers", workerPageData{Error:"Could not load worker inventory.", CSRFToken:h.csrfToken(r)})
		return
	}
	h.render(w, "workers", workerPageData{Workers:workers, Notice:r.URL.Query().Get("notice"), Error:r.URL.Query().Get("error"), CSRFToken:h.csrfToken(r)})
}

func (h *Handler) registerWorker(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := r.ParseForm(); err != nil || !h.verifyCSRF(r) {
		http.Error(w, "invalid request token or form", http.StatusForbidden)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	endpoint := strings.TrimRight(strings.TrimSpace(r.FormValue("endpoint")), "/")
	memoryMiB, parseErr := strconv.ParseUint(strings.TrimSpace(r.FormValue("memory_limit_mib")), 10, 64)
	u, urlErr := url.Parse(endpoint)
	if !safeFormID(id) || name == "" || len(name) > 128 || parseErr != nil || memoryMiB == 0 ||
		urlErr != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		h.redirectWorkerResult(w, r, "error", "Enter a valid ID, name, absolute HTTP(S) agent URL, and positive memory cap.")
		return
	}
	admin, ok := h.service.(workerManagementService)
	if !ok || admin == nil { http.Error(w, "worker registration unavailable", http.StatusServiceUnavailable); return }
	if err := admin.RegisterWorker(r.Context(), id, name, endpoint, memoryMiB); err != nil {
		h.redirectWorkerResult(w, r, "error", err.Error())
		return
	}
	h.redirectWorkerResult(w, r, "notice", "Worker registration saved. It will show healthy only after a valid agent heartbeat.")
}

func (h *Handler) deleteWorker(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := r.ParseForm(); err != nil || !h.verifyCSRF(r) { http.Error(w, "invalid request token or form", http.StatusForbidden); return }
	admin, ok := h.service.(workerManagementService)
	if !ok || admin == nil { http.Error(w, "worker registration unavailable", http.StatusServiceUnavailable); return }
	id := r.PathValue("workerID")
	if !safeFormID(id) { h.redirectWorkerResult(w, r, "error", "Invalid worker ID."); return }
	if err := admin.DeleteWorker(r.Context(), id); err != nil {
		h.redirectWorkerResult(w, r, "error", err.Error())
		return
	}
	h.redirectWorkerResult(w, r, "notice", "Worker registration removed. No cloud instance was stopped or terminated.")
}

func (h *Handler) redirectWorkerResult(w http.ResponseWriter, r *http.Request, key, value string) {
	location := "/workers?" + key + "=" + url.QueryEscape(value)
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func (h *Handler) vmInventoryPage(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	data := vmInventoryPageData{VMs: []vmRow{}, Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"), CSRFToken: h.csrfToken(r)}
	if h.service == nil {
		h.renderStatus(w, http.StatusServiceUnavailable, "vm-inventory", vmInventoryPageData{Error: "Controller service is not initialized.", CSRFToken: data.CSRFToken})
		return
	}
	vms, err := h.service.ListVMs(r.Context())
	if err != nil {
		h.renderStatus(w, http.StatusInternalServerError, "vm-inventory", vmInventoryPageData{Error: "Could not load virtual machine inventory.", CSRFToken: data.CSRFToken})
		return
	}
	for _, vm := range vms {
		op, opErr := h.service.LatestOperation(r.Context(), vm.ID)
		hasOperation := opErr == nil
		if hasOperation && (op.State == domain.OperationPending || op.State == domain.OperationDispatched || op.State == domain.OperationUnknown) { data.Refreshing = true }
		data.VMs = append(data.VMs, vmRow{VM: vm, Operation: op, HasOperation: hasOperation})
	}
	h.render(w, "vm-inventory", data)
}

func (h *Handler) newVMPage(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	h.render(w, "new-vm", formData{Limits:h.limits, CSRFToken:h.csrfToken(r), IdempotencyKey:newFormID()})
}

func (h *Handler) createVM(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := r.ParseForm(); err != nil { h.renderStatus(w, http.StatusBadRequest, "new-vm", formData{Limits:h.limits, Error:"Invalid form submission.", IdempotencyKey:newFormID(), CSRFToken:h.csrfToken(r)}); return }
	if !h.verifyCSRF(r) { h.renderStatus(w, http.StatusForbidden, "new-vm", formData{Limits:h.limits, Error:"Invalid request token.", IdempotencyKey:r.FormValue("idempotency_key"), CSRFToken:h.csrfToken(r)}); return }
	name := strings.TrimSpace(r.FormValue("name"))
	vcpus, cpuErr := strconv.Atoi(r.FormValue("vcpus"))
	memory, memErr := strconv.ParseUint(r.FormValue("memory_mib"), 10, 64)
	imageID := r.FormValue("image_id")
	if name == "" || len(name) > 128 || cpuErr != nil || vcpus < 1 || vcpus > h.limits.MaxVCPUs || memErr != nil || memory < 1 || memory > h.limits.MaxMemoryMiB || !h.allowedImage(imageID) {
		h.renderStatus(w, http.StatusUnprocessableEntity, "new-vm", formData{Limits:h.limits, Error:"Check the VM name, vCPU, memory, and approved image values.", IdempotencyKey:r.FormValue("idempotency_key"), CSRFToken:h.csrfToken(r)})
		return
	}
	key := r.FormValue("idempotency_key")
	if !safeFormID(key) { h.renderStatus(w, http.StatusBadRequest, "new-vm", formData{Limits:h.limits, Error:"The form's idempotency key is invalid. Reload the form and retry.", IdempotencyKey:newFormID(), CSRFToken:h.csrfToken(r)}); return }
	_, op, err := h.service.CreateVM(r.Context(), controllerapi.CreateVMRequest{Name:name, VCPUs:vcpus, MemoryMiB:memory, ImageID:imageID}, key)
	if err != nil {
		h.renderStatus(w, http.StatusServiceUnavailable, "new-vm", formData{Limits:h.limits, Error:"Could not submit the VM request. Check worker health and capacity, then retry with the same form.", IdempotencyKey:key, CSRFToken:h.csrfToken(r)})
		return
	}
	http.Redirect(w, r, "/console?notice="+template.URLQueryEscaper("VM request recorded; operation "+string(op.State)), http.StatusSeeOther)
}

func (h *Handler) startVM(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, true)
}
func (h *Handler) stopVM(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, false)
}
func (h *Handler) lifecycleAction(w http.ResponseWriter, r *http.Request, start bool) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := r.ParseForm(); err != nil || !h.verifyCSRF(r) { http.Error(w, "invalid request token", http.StatusForbidden); return }
	var op domain.Operation
	var err error
	if start { _, op, err = h.service.StartVM(r.Context(), r.PathValue("vmID")) } else { _, op, err = h.service.StopVM(r.Context(), r.PathValue("vmID")) }
	if err != nil {
		http.Redirect(w, r, "/console?error="+template.URLQueryEscaper("Operation could not be confirmed. Refresh the dashboard before retrying."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/console?notice="+template.URLQueryEscaper("Operation "+op.ID+" is "+string(op.State)), http.StatusSeeOther)
}

func (h *Handler) allowedImage(id string) bool {
	for _, allowed := range h.limits.AllowedImages { if id == allowed { return true } }
	return false
}

const sessionCookie = "vm_controller_session"

func (h *Handler) newSession() string {
	expiry := strconv.FormatInt(time.Now().Add(12*time.Hour).Unix(), 10)
	mac := hmac.New(sha256.New, []byte(h.token))
	_, _ = mac.Write([]byte(expiry))
	return base64.RawURLEncoding.EncodeToString([]byte(expiry)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (h *Handler) authenticated(r *http.Request) bool {
	if h.token == "" { return false }
	cookie, err := r.Cookie(sessionCookie)
	if err != nil { return false }
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 { return false }
	expiryBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil { return false }
	expiry, err := strconv.ParseInt(string(expiryBytes), 10, 64)
	if err != nil || time.Now().Unix() >= expiry { return false }
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { return false }
	mac := hmac.New(sha256.New, []byte(h.token))
	_, _ = mac.Write(expiryBytes)
	return hmac.Equal(got, mac.Sum(nil))
}


func (h *Handler) csrfToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || h.token == "" { return "" }
	mac := hmac.New(sha256.New, []byte(h.token))
	_, _ = mac.Write([]byte("csrf:" + cookie.Value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (h *Handler) verifyCSRF(r *http.Request) bool {
	expected := h.csrfToken(r)
	got := r.FormValue("csrf_token")
	return expected != "" && len(got) == len(expected) && subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func (h *Handler) headers(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
}

func (h *Handler) render(w http.ResponseWriter, page string, data any) { h.renderStatus(w, http.StatusOK, page, data) }
func (h *Handler) renderStatus(w http.ResponseWriter, status int, page string, data any) {
	var output strings.Builder
	if err := h.templates.ExecuteTemplate(&output, page, data); err != nil {
		http.Error(w, "page rendering failed", http.StatusInternalServerError)
		return
	}
	// Keep the existing server-rendered templates intact while applying one
	// shared design system to every public and authenticated page.
	html := strings.Replace(output.String(), "</head>", `<link rel="stylesheet" href="/assets/ui.css"></head>`, 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(html))
}

func newFormID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil { return fmt.Sprintf("form-%d", time.Now().UnixNano()) }
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func safeFormID(value string) bool {
	if value == "" || len(value) > 128 { return false }
	for _, r := range value { if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') { return false } }
	return true
}



const pageTemplates = `
{{define "landing"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#080f20"><meta name="description" content="Aiori Compute is a focused control plane for capacity-aware virtual machine operations, worker health, and infrastructure lifecycle management."><title>Aiori Compute — Infrastructure, in control.</title>{{template "styles" .}}<script src="/assets/ui.js" defer></script></head>
<body class="marketing-page landing-v2"><a class="skip-link" href="#main-content">Skip to main content</a><div class="landing-progress" aria-hidden="true"><span></span></div>
<header class="marketing-header landing-header"><a class="marketing-brand" href="/" aria-label="Aiori Compute home"><span class="brand-mark" aria-hidden="true"><svg viewBox="0 0 32 32" fill="none"><path d="M7 23.5 15.9 6l9.1 17.5h-5.1l-4-8.1-4 8.1H7Z" fill="currentColor"/><path d="M12.2 20h7.5" stroke="#C8C3FF" stroke-width="2" stroke-linecap="round"/></svg></span><span class="landing-wordmark"><span class="marketing-brand-name">Aiori<span>Compute</span></span><span class="marketing-brand-sub">INFRASTRUCTURE CONTROL PLANE</span></span></a>
<button class="landing-menu-toggle" type="button" aria-expanded="false" aria-controls="landing-navigation" aria-label="Open navigation"><span></span><span></span></button><nav class="marketing-nav landing-nav" id="landing-navigation" aria-label="Main navigation"><a href="#platform">Platform</a><a href="#how-it-works">How it works</a><a href="/docs">Documentation</a><a class="landing-nav-login" href="/login">Operator sign in <span aria-hidden="true">↗</span></a></nav></header>
<main id="main-content">
<section class="landing-hero"><div class="landing-hero-glow" aria-hidden="true"></div><div class="landing-grid-texture" aria-hidden="true"></div><div class="landing-hero-inner">
<div class="landing-copy" data-reveal><div class="landing-eyebrow"><span class="landing-eyebrow-mark"><i></i><i></i><i></i></span> COMPUTE INFRASTRUCTURE, MADE OPERABLE</div><h1>Infrastructure,<br><span>in control.</span></h1><p class="landing-lede">A considered control plane for virtual machine operations. Understand your worker fleet, place workloads with capacity in mind, and manage lifecycle state with confidence.</p><div class="landing-hero-actions"><a class="landing-btn landing-btn-primary" href="/docs#getting-started">Explore the platform <span aria-hidden="true">↗</span></a><a class="landing-btn landing-btn-quiet" href="#platform">Discover capabilities <span aria-hidden="true">↓</span></a></div><div class="landing-assurance"><span class="landing-assurance-icon">✓</span><span>Built around observable state, deliberate actions, and clear operational feedback.</span></div></div>
<div class="landing-visual" data-reveal data-reveal-delay="140"><div class="landing-visual-orbit orbit-a" aria-hidden="true"></div><div class="landing-visual-orbit orbit-b" aria-hidden="true"></div><div class="landing-console">
<div class="landing-console-top"><div class="landing-window-controls"><i></i><i></i><i></i></div><div class="landing-console-path"><span class="landing-console-lock">⌑</span> aiori.compute <span>/</span> workspace</div><span class="landing-preview-label">PRODUCT PREVIEW</span></div>
<div class="landing-console-layout"><aside class="landing-console-sidebar"><div class="landing-mini-brand"><span>A</span></div><div class="landing-mini-nav active"><b>◫</b></div><div class="landing-mini-nav"><b>▦</b></div><div class="landing-mini-nav"><b>⌘</b></div><div class="landing-mini-nav"><b>◇</b></div><div class="landing-mini-sidebar-line"></div><div class="landing-mini-avatar">OP</div></aside>
<div class="landing-console-main"><div class="landing-console-heading"><div><small>WORKSPACE / OVERVIEW</small><strong>Fleet overview</strong></div><span class="landing-live-chip"><i></i> SYSTEM VIEW</span></div>
<div class="landing-preview-tabs" role="tablist" aria-label="Preview dashboard views"><button type="button" class="is-active" role="tab" aria-selected="true" id="preview-tab-fleet" aria-controls="preview-panel-fleet" data-preview-tab="fleet">Fleet</button><button type="button" role="tab" aria-selected="false" id="preview-tab-workloads" aria-controls="preview-panel-workloads" data-preview-tab="workloads">Workloads</button><button type="button" role="tab" aria-selected="false" id="preview-tab-capacity" aria-controls="preview-panel-capacity" data-preview-tab="capacity">Capacity</button></div>
<div class="landing-preview-panel" id="preview-panel-fleet" aria-labelledby="preview-tab-fleet" tabindex="0" data-preview-panel="fleet" role="tabpanel"><div class="landing-preview-metrics"><div><small>WORKERS</small><strong>04</strong><em><i></i> Fleet inventory</em></div><div><small>VIRTUAL MACHINES</small><strong>12</strong><em>Tracked resources</em></div><div><small>CAPACITY</small><strong>68<span>%</span></strong><div class="landing-meter"><i></i></div></div></div><div class="landing-preview-caption">WORKER HEALTH <span>LAST HEARTBEAT</span></div><div class="landing-preview-row"><span class="landing-node-icon">01</span><span class="landing-node-name"><b>compute-east-01</b><small>Worker agent</small></span><span class="landing-node-state"><i></i> Healthy</span></div><div class="landing-preview-row"><span class="landing-node-icon">02</span><span class="landing-node-name"><b>compute-east-02</b><small>Worker agent</small></span><span class="landing-node-state"><i></i> Healthy</span></div><div class="landing-preview-row"><span class="landing-node-icon muted">03</span><span class="landing-node-name"><b>compute-west-01</b><small>Reconciliation in progress</small></span><span class="landing-node-state pending"><i></i> Pending</span></div></div>
<div class="landing-preview-panel" id="preview-panel-workloads" aria-labelledby="preview-tab-workloads" tabindex="0" data-preview-panel="workloads" role="tabpanel" hidden><div class="landing-preview-metrics"><div><small>RUNNING</small><strong>09</strong><em><i></i> Observed state</em></div><div><small>IN PROGRESS</small><strong>02</strong><em>Lifecycle actions</em></div><div><small>DEFINED</small><strong>01</strong><em>Awaiting start</em></div></div><div class="landing-preview-caption">RECENT WORKLOADS <span>STATE</span></div><div class="landing-preview-row"><span class="landing-node-icon">VM</span><span class="landing-node-name"><b>api-production-01</b><small>2 vCPU · 4 GiB</small></span><span class="landing-node-state"><i></i> Running</span></div><div class="landing-preview-row"><span class="landing-node-icon">VM</span><span class="landing-node-name"><b>worker-batch-02</b><small>4 vCPU · 8 GiB</small></span><span class="landing-node-state pending"><i></i> Starting</span></div><div class="landing-preview-row"><span class="landing-node-icon">VM</span><span class="landing-node-name"><b>staging-web-01</b><small>2 vCPU · 2 GiB</small></span><span class="landing-node-state"><i></i> Running</span></div></div>
<div class="landing-preview-panel" id="preview-panel-capacity" aria-labelledby="preview-tab-capacity" tabindex="0" data-preview-panel="capacity" role="tabpanel" hidden><div class="landing-capacity-summary"><div><small>ALLOCATED MEMORY</small><strong>43.5 <span>GiB</span></strong></div><div><small>AVAILABLE</small><strong>20.5 <span>GiB</span></strong></div></div><div class="landing-capacity-bars"><div><span>compute-east-01</span><b><i style="width:72%"></i></b><small>72%</small></div><div><span>compute-east-02</span><b><i style="width:58%"></i></b><small>58%</small></div><div><span>compute-west-01</span><b><i style="width:39%"></i></b><small>39%</small></div></div><div class="landing-capacity-note"><i></i> Capacity is evaluated alongside worker health and heartbeat freshness.</div></div>
<div class="landing-console-foot"><span><i></i> Illustrative interface preview</span><span>NOT LIVE DATA</span></div></div></div></div><div class="landing-float-card"><span class="landing-float-icon">✓</span><span><b>State, made clear</b><small>Observed separately from intent</small></span></div></div></div><div class="landing-hero-bottom"><span>BUILT FOR INFRASTRUCTURE TEAMS</span><span>OBSERVE <i>·</i> DECIDE <i>·</i> OPERATE</span></div></section>
<section class="landing-principles" aria-label="Platform principles"><div class="landing-principle" data-reveal><span>01</span><div><b>Know what is happening</b><p>Keep observed state distinct from requested actions.</p></div></div><div class="landing-principle" data-reveal><span>02</span><div><b>Place with context</b><p>Consider worker health, heartbeat freshness, and memory.</p></div></div><div class="landing-principle" data-reveal><span>03</span><div><b>Operate deliberately</b><p>Use authenticated workflows and explicit feedback.</p></div></div></section>
<section class="landing-platform" id="platform"><div class="landing-section-heading" data-reveal><div><div class="landing-section-kicker">THE PLATFORM</div><h2>Everything important.<br><span>Nothing in the way.</span></h2></div><p>A focused operational surface that brings the signals and controls for your compute fleet into one coherent workflow.</p></div>
<div class="landing-capability-grid"><article class="landing-capability-card" data-reveal><div class="landing-capability-top"><span class="landing-capability-icon violet"><svg viewBox="0 0 24 24" fill="none"><rect x="3" y="4" width="18" height="6" rx="1.5"/><rect x="3" y="14" width="18" height="6" rx="1.5"/><path d="M7 7h.01M7 17h.01M11 7h6M11 17h6"/></svg></span><span>01 / OBSERVE</span></div><h3>A clear view of your fleet</h3><p>Review registered workers, health signals, memory availability, and tracked virtual machines without losing operational context.</p><a href="/docs#architecture">Explore fleet visibility <span>↗</span></a></article>
<article class="landing-capability-card" data-reveal data-reveal-delay="80"><div class="landing-capability-top"><span class="landing-capability-icon blue"><svg viewBox="0 0 24 24" fill="none"><path d="M12 3v18M3 12h18M5.6 5.6l12.8 12.8M18.4 5.6 5.6 18.4"/></svg></span><span>02 / PROVISION</span></div><h3>Capacity-aware VM requests</h3><p>Define workload requirements against configured limits and approved images, then let the controller select a suitable worker.</p><a href="/docs#virtual-machines">Understand VM provisioning <span>↗</span></a></article>
<article class="landing-capability-card" data-reveal data-reveal-delay="160"><div class="landing-capability-top"><span class="landing-capability-icon green"><svg viewBox="0 0 24 24" fill="none"><path d="M20 7v5h-5M4 17v-5h5"/><path d="M5.7 9a7 7 0 0 1 11.6-2L20 12M4 12l2.7 5a7 7 0 0 0 11.6-2"/></svg></span><span>03 / RECONCILE</span></div><h3>Lifecycle state you can trust</h3><p>Track pending and uncertain outcomes without treating a timeout as success or encouraging blind retries.</p><a href="/docs#lifecycle">Explore operation states <span>↗</span></a></article></div></section>
<section class="landing-workflow" id="how-it-works"><div class="landing-workflow-inner"><div class="landing-workflow-copy" data-reveal><div class="landing-section-kicker">A DELIBERATE WORKFLOW</div><h2>From request<br>to <span>running workload.</span></h2><p>Every operation follows a clear path. The controller validates the request, evaluates available capacity, and coordinates work through registered agents.</p><a class="landing-inline-link" href="/docs#getting-started">See how the system works <span>↗</span></a></div><div class="landing-workflow-steps"><div class="landing-workflow-step" data-reveal><span class="landing-step-index">01</span><div class="landing-step-symbol">⌘</div><div><b>Describe the workload</b><p>Specify the resources and image your VM needs.</p></div><span class="landing-step-arrow">→</span></div><div class="landing-workflow-step" data-reveal data-reveal-delay="80"><span class="landing-step-index">02</span><div class="landing-step-symbol">◎</div><div><b>Place with capacity in mind</b><p>Evaluate worker availability and health signals.</p></div><span class="landing-step-arrow">→</span></div><div class="landing-workflow-step" data-reveal data-reveal-delay="160"><span class="landing-step-index">03</span><div class="landing-step-symbol">↗</div><div><b>Track the actual outcome</b><p>Follow operation progress and observed VM state.</p></div><span class="landing-step-arrow">✓</span></div></div></div></section>
<section class="landing-cta"><div class="landing-cta-orb" aria-hidden="true"></div><div class="landing-cta-content" data-reveal><div class="landing-section-kicker">START WITH THE FUNDAMENTALS</div><h2>Know the system.<br><span>Then take control.</span></h2><p>Explore the architecture, setup requirements, security model, and operational workflows in the platform documentation.</p><div class="landing-cta-actions"><a class="landing-btn landing-btn-primary" href="/docs">Read the documentation <span>↗</span></a><a class="landing-cta-login" href="/login">Already an operator? Sign in</a></div></div><div class="landing-cta-aside" data-reveal data-reveal-delay="120"><div class="landing-cta-mark">A</div><b>Designed for clarity.<br>Engineered for control.</b><span>AIORI COMPUTE / ENGINEERING PREVIEW</span></div></section></main>
<footer class="landing-footer"><a class="marketing-brand" href="/" aria-label="Aiori Compute home"><span class="brand-mark" aria-hidden="true">A</span><span class="landing-wordmark"><span class="marketing-brand-name">Aiori<span>Compute</span></span><span class="marketing-brand-sub">INFRASTRUCTURE CONTROL PLANE</span></span></a><p>AIORI Hackathon · Engineering preview</p><nav aria-label="Footer navigation"><a href="/docs">Documentation</a><a href="/login">Operator sign in</a><a href="https://github.com/InfX2243/A3-PS023-TC061">GitHub <span>↗</span></a></nav></footer></body></html>{{end}}
{{define "login"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#091326"><meta name="description" content="Secure operator access to the Aiori Compute infrastructure control plane."><title>Operator sign in · Aiori Compute</title>{{template "styles" .}}<script src="/assets/ui.js" defer></script></head><body class="auth-body"><a class="skip-link" href="#main-content">Skip to main content</a><main class="auth-page"><section class="auth-showcase" aria-label="Aiori Compute platform overview"><div class="auth-showcase-grid" aria-hidden="true"></div><div class="auth-glow auth-glow-one" aria-hidden="true"></div><div class="auth-glow auth-glow-two" aria-hidden="true"></div><div class="auth-showcase-inner"><a class="auth-brand" href="/" aria-label="Aiori Compute home"><span class="auth-brand-mark" aria-hidden="true">A</span><span><strong>Aiori <em>Compute</em></strong><small>INFRASTRUCTURE CONTROL PLANE</small></span></a><div class="auth-showcase-copy"><div class="auth-kicker"><span></span> OPERATOR WORKSPACE</div><h1>Your infrastructure.<br><span>In clear view.</span></h1><p>One control plane to prepare virtual machines, orchestrate worker capacity, and understand every lifecycle operation.</p><div class="auth-feature-list"><div><span aria-hidden="true">✓</span><span><strong>Capacity-aware placement</strong><small>Make informed decisions across your worker fleet</small></span></div><div><span aria-hidden="true">✓</span><span><strong>Predictable operations</strong><small>Track lifecycle actions with clear state transitions</small></span></div><div><span aria-hidden="true">✓</span><span><strong>Operational visibility</strong><small>Understand worker health and available resources</small></span></div></div></div><div class="auth-showcase-bottom"><div class="auth-status"><i aria-hidden="true"></i><span>CONTROL PLANE</span><strong>READY FOR OPERATORS</strong></div><span>AIORI HACKATHON · ENGINEERING PREVIEW</span></div></div><div class="auth-orbit-card" aria-hidden="true"><span class="auth-orbit-icon">⌘</span><span><strong>Compute, coordinated.</strong><small>One place to operate your fleet</small></span><i></i></div></section><section class="auth-panel"><div class="auth-panel-top"><a class="auth-mobile-brand" href="/" aria-label="Aiori Compute home"><span class="auth-brand-mark" aria-hidden="true">A</span><strong>Aiori <em>Compute</em></strong></a><a class="auth-help-link" href="/docs">Documentation <span aria-hidden="true">↗</span></a></div><div class="auth-content" id="main-content"><div class="auth-form-heading"><div class="auth-form-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><path d="M12 3.5 19 6v5.1c0 4.3-2.8 7.6-7 9.4-4.2-1.8-7-5.1-7-9.4V6l7-2.5Z" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/><path d="m9 12 2 2 4-4" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg></div><div class="auth-form-eyebrow">SECURE OPERATOR ACCESS</div><h2>Welcome back<span>.</span></h2><p>Sign in to continue to your compute workspace.</p></div>{{if .Error}}<div class="auth-error" role="alert"><span aria-hidden="true">!</span><div><strong>We couldn't sign you in</strong><p>{{.Error}}</p></div></div>{{end}}<form class="auth-form" method="post" action="/login">{{if .UseCredentials}}<div class="auth-field"><label for="username">Operator username</label><div class="auth-input-wrap"><span class="auth-input-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><circle cx="12" cy="8" r="3.3" stroke="currentColor" stroke-width="1.6"/><path d="M5.5 20c.5-3.4 2.7-5.2 6.5-5.2s6 1.8 6.5 5.2" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg></span><input id="username" type="text" name="username" autocomplete="username" placeholder="Enter your username" required autofocus></div></div><div class="auth-field"><div class="auth-label-row"><label for="password">Password</label></div><div class="auth-input-wrap"><span class="auth-input-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><rect x="5" y="10" width="14" height="10" rx="2" stroke="currentColor" stroke-width="1.6"/><path d="M8 10V7a4 4 0 0 1 8 0v3" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg></span><input id="password" type="password" name="password" autocomplete="current-password" placeholder="Enter your password" required><button class="auth-password-toggle" type="button" data-password-toggle="password" aria-label="Show password" aria-pressed="false">Show</button></div></div>{{else}}<div class="auth-field"><label for="token">Operator access token</label><div class="auth-input-wrap"><span class="auth-input-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><rect x="5" y="10" width="14" height="10" rx="2" stroke="currentColor" stroke-width="1.6"/><path d="M8 10V7a4 4 0 0 1 8 0v3" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg></span><input id="token" type="password" name="token" autocomplete="current-password" placeholder="Paste your operator token" required autofocus><button class="auth-password-toggle" type="button" data-password-toggle="token" aria-label="Show access token" aria-pressed="false">Show</button></div><p class="auth-field-hint">Use the operator token configured for this controller instance.</p></div>{{end}}<button class="auth-submit" type="submit"><span>Sign in to workspace</span><span class="auth-submit-arrow" aria-hidden="true">→</span></button></form><div class="auth-assurance"><span class="auth-assurance-icon" aria-hidden="true">◇</span><span><strong>Protected operator session</strong><small>Secure, HTTP-only session cookie · 12-hour expiry</small></span></div><div class="auth-panel-footer"><span>New to Aiori Compute?</span><a href="/docs">Explore the platform guide <span aria-hidden="true">↗</span></a></div></div><div class="auth-legal"><span>© AIORI HACKATHON · ENGINEERING PREVIEW</span><a href="/">Back to home</a></div></section></main></body></html>{{end}}
{{define "dashboard"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#f5f7fb"><title>Overview · Aiori Compute</title>{{if .Refreshing}}<meta http-equiv="refresh" content="5">{{end}}{{template "styles" .}}<script src="/assets/ui.js" defer></script></head><body class="dashboard-body"><a class="skip-link" href="#main-content">Skip to main content</a><div class="app-shell dashboard-shell">{{template "sidebar" .}}<div class="main-area dashboard-main"><header class="topbar dashboard-topbar"><div class="breadcrumbs"><span>Control plane</span><span aria-hidden="true">/</span><strong>Overview</strong></div><div class="top-actions"><span class="env-pill"><span class="env-dot"></span> Controller workspace</span><a class="btn btn-secondary btn-small dashboard-refresh" href="/console" aria-label="Refresh dashboard">↻ Refresh</a><div class="avatar" aria-label="Operator">OP</div><form method="post" action="/logout" class="action-form"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><button class="btn btn-secondary btn-small" type="submit">Sign out</button></form></div></header><main class="content dashboard-content" id="main-content"><div class="page-heading"><div><div class="eyebrow">Infrastructure overview</div><h1>Good to see you, operator.</h1><p>Monitor your worker fleet and manage virtual machines from one place.</p></div><a class="btn btn-primary" href="/vms/new"><span aria-hidden="true">＋</span> Prepare a VM</a></div>{{if .Notice}}<div class="notice" role="status"><span class="notice-icon">✓</span><span>{{.Notice}}</span></div>{{end}}{{if .Error}}<div class="notice error" role="alert"><span class="notice-icon">!</span><span>{{.Error}}</span></div>{{end}}<section class="metric-grid" aria-label="Infrastructure summary"><article class="metric"><div class="metric-top"><span>Registered workers</span><span class="metric-icon">▤</span></div><div class="metric-value">{{len .Workers}}</div><div class="metric-foot">Worker agents in inventory</div></article><article class="metric"><div class="metric-top"><span>Virtual machines</span><span class="metric-icon">▣</span></div><div class="metric-value">{{len .VMs}}</div><div class="metric-foot">Tracked VM records</div></article><article class="metric"><div class="metric-top"><span>Healthy workers</span><span class="metric-icon">⌁</span></div><div class="metric-value">{{countWorkerState .Workers "healthy"}}</div><div class="metric-foot">Reporting healthy status</div></article><article class="metric"><div class="metric-top"><span>Running VMs</span><span class="metric-icon">▶</span></div><div class="metric-value">{{countVMState .VMs "running"}}</div><div class="metric-foot">Currently observed as running</div></article></section>
<section class="dashboard-insights" aria-label="Fleet analytics">
  <article class="section-card dashboard-chart-card">
    <div class="section-head"><div><div class="section-title">Workload distribution</div><div class="section-subtitle">Observed VM states across the controller inventory</div></div><span class="chart-live-label"><i></i> INVENTORY SNAPSHOT</span></div>
    <div class="dashboard-chart-body">
      <div class="state-chart" role="img" aria-label="Virtual machine counts by observed state">
        <div class="state-chart-row"><span class="state-chart-label"><i class="state-dot state-dot-running"></i>running</span><span class="state-chart-track"><i class="state-chart-bar state-bar-running" style="width:{{percentOf (countVMState .VMs "running") (len .VMs)}}%"></i></span><strong>{{countVMState .VMs "running"}}</strong></div>
        <div class="state-chart-row"><span class="state-chart-label"><i class="state-dot state-dot-stopped"></i>stopped</span><span class="state-chart-track"><i class="state-chart-bar state-bar-stopped" style="width:{{percentOf (countVMState .VMs "stopped") (len .VMs)}}%"></i></span><strong>{{countVMState .VMs "stopped"}}</strong></div>
        <div class="state-chart-row"><span class="state-chart-label"><i class="state-dot state-dot-starting"></i>starting</span><span class="state-chart-track"><i class="state-chart-bar state-bar-starting" style="width:{{percentOf (countVMState .VMs "starting") (len .VMs)}}%"></i></span><strong>{{countVMState .VMs "starting"}}</strong></div>
        <div class="state-chart-row"><span class="state-chart-label"><i class="state-dot state-dot-stopping"></i>stopping</span><span class="state-chart-track"><i class="state-chart-bar state-bar-stopping" style="width:{{percentOf (countVMState .VMs "stopping") (len .VMs)}}%"></i></span><strong>{{countVMState .VMs "stopping"}}</strong></div>
        <div class="state-chart-row"><span class="state-chart-label"><i class="state-dot state-dot-unknown"></i>unknown</span><span class="state-chart-track"><i class="state-chart-bar state-bar-unknown" style="width:{{percentOf (countVMState .VMs "unknown") (len .VMs)}}%"></i></span><strong>{{countVMState .VMs "unknown"}}</strong></div>
        <div class="state-chart-row"><span class="state-chart-label"><i class="state-dot state-dot-failed"></i>failed</span><span class="state-chart-track"><i class="state-chart-bar state-bar-failed" style="width:{{percentOf (countVMState .VMs "failed") (len .VMs)}}%"></i></span><strong>{{countVMState .VMs "failed"}}</strong></div>
        <div class="state-chart-row"><span class="state-chart-label"><i class="state-dot state-dot-defined"></i>defined</span><span class="state-chart-track"><i class="state-chart-bar state-bar-defined" style="width:{{percentOf (countVMState .VMs "defined") (len .VMs)}}%"></i></span><strong>{{countVMState .VMs "defined"}}</strong></div>
      </div>
      <div class="chart-summary"><strong>{{len .VMs}}</strong><span>tracked workloads</span><small>Counts reflect observed state, not requested intent.</small></div>
    </div>
  </article>
  <article class="section-card dashboard-chart-card">
    <div class="section-head"><div><div class="section-title">Worker memory headroom</div><div class="section-subtitle">Reported available memory against each worker limit</div></div><a class="subtle-link" href="/workers">Fleet details ↗</a></div>
    <div class="dashboard-chart-body capacity-chart-body">
      {{if .Workers}}{{range .Workers}}
      <div class="capacity-chart-row"><div class="capacity-chart-meta"><span class="capacity-worker-name">{{.Name}}</span><strong>{{divMiB .AvailableMemoryBytes}} <small>MiB free</small></strong></div><div class="capacity-chart-track"><i style="width:{{memoryPercent .AvailableMemoryBytes .MemoryLimitBytes}}%"></i></div><div class="capacity-chart-foot"><span>{{.State}}</span><span>{{memoryPercent .AvailableMemoryBytes .MemoryLimitBytes}}% available</span></div></div>
      {{end}}{{else}}<div class="chart-empty"><span>⌁</span><strong>No worker capacity to chart</strong><p>Register a worker and wait for its first heartbeat to see memory headroom.</p><a href="/workers">Open worker inventory ↗</a></div>{{end}}
    </div>
  </article>
</section>
<section class="dashboard-alerts section-card" aria-label="Operational alerts" aria-live="polite">
  <div class="section-head"><div><div class="section-title">Operational alerts</div><div class="section-subtitle">Signals that may need an operator's attention</div></div><span class="alert-count">{{if gt (countNonHealthyWorkers .Workers) 0}}{{countNonHealthyWorkers .Workers}} attention{{else if gt (countActiveOperations .VMs) 0}}{{countActiveOperations .VMs}} in progress{{else}}0 active{{end}}</span></div>
  <div class="dashboard-alert-list">
    {{if gt (countNonHealthyWorkers .Workers) 0}}
    <article class="dashboard-alert dashboard-alert-warning" data-dashboard-alert><span class="dashboard-alert-symbol">!</span><div class="dashboard-alert-copy"><strong>Worker health needs attention</strong><p>{{countNonHealthyWorkers .Workers}} worker(s) are not currently reporting a healthy state. Review heartbeat freshness and agent connectivity.</p></div><a href="/workers">Review workers <span>↗</span></a><button type="button" class="alert-dismiss" data-alert-dismiss aria-label="Dismiss alert">×</button></article>
    {{else if gt (countActiveOperations .VMs) 0}}
    <article class="dashboard-alert dashboard-alert-info" data-dashboard-alert><span class="dashboard-alert-symbol">↻</span><div class="dashboard-alert-copy"><strong>Operations are being reconciled</strong><p>{{countActiveOperations .VMs}} VM operation(s) are pending, dispatched, or awaiting confirmation. Their observed state may still change.</p></div><a href="/vms">View operations <span>↗</span></a><button type="button" class="alert-dismiss" data-alert-dismiss aria-label="Dismiss alert">×</button></article>
    {{else}}
    <article class="dashboard-alert dashboard-alert-clear"><span class="dashboard-alert-symbol">✓</span><div class="dashboard-alert-copy"><strong>No active fleet alerts</strong><p>All registered workers are reporting healthy status and there are no operations awaiting reconciliation.</p></div><span class="alert-clear-label">SYSTEM NOMINAL</span></article>
    {{end}}
  </div>
</section>
<section class="workspace-shortcuts" aria-label="Workspace destinations">
  <a class="workspace-shortcut" href="/workers"><span class="workspace-shortcut-icon">▤</span><span class="workspace-shortcut-copy"><strong>Worker inventory</strong><small>{{len .Workers}} registered worker(s) · {{countWorkerState .Workers "healthy"}} healthy</small></span><span class="workspace-shortcut-arrow" aria-hidden="true">↗</span></a>
  <a class="workspace-shortcut" href="/vms"><span class="workspace-shortcut-icon workspace-shortcut-vm">▣</span><span class="workspace-shortcut-copy"><strong>Virtual machines</strong><small>{{len .VMs}} tracked VM(s) · {{countVMState .VMs "running"}} running</small></span><span class="workspace-shortcut-arrow" aria-hidden="true">↗</span></a>
</section><div class="dashboard-bottom"><section class="section-card info-card"><h3>How the platform works</h3><p>The controller validates requests and selects a worker with available capacity. The worker agent manages each VM through Cloud Hypervisor. Reconciliation records confirmed outcomes and keeps uncertain operations visible rather than assuming success.</p><div class="architecture-flow"><span class="flow-node">Operator UI</span><span class="flow-arrow">→</span><span class="flow-node">Controller</span><span class="flow-arrow">→</span><span class="flow-node">Worker agent</span><span class="flow-arrow">→</span><span class="flow-node">Cloud Hypervisor</span></div></section><section class="section-card info-card"><h3>Need a hand?</h3><p>The operator guide explains setup, VM creation, lifecycle states, worker health, security boundaries, and troubleshooting without requiring an operator session.</p><div class="architecture-flow"><a class="btn btn-secondary btn-small" href="/docs">Open platform guide →</a><a class="btn btn-secondary btn-small" href="/docs#getting-started">Quick start</a></div></section></div><footer class="page-footer"><span>Aiori Compute · AIORI Hackathon engineering preview</span><span>States reflect controller inventory, not a live hypervisor console.</span></footer></main></div></div></body></html>{{end}}
{{define "new-vm"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#f5f7fb"><title>Prepare a VM · Aiori Compute</title>{{template "styles" .}}<script src="/assets/ui.js" defer></script></head><body><a class="skip-link" href="#main-content">Skip to main content</a><div class="app-shell">{{template "sidebar" .}}<div class="main-area"><header class="topbar"><div class="breadcrumbs"><a href="/console">Workspace</a><span aria-hidden="true">/</span><strong>Prepare a VM</strong></div><div class="top-actions"><span class="env-pill"><span class="env-dot"></span> Engineering environment</span><a class="btn btn-secondary btn-small" href="/console">← Dashboard</a></div></header><main class="content" id="main-content"><div class="page-heading"><div><div class="eyebrow">Compute provisioning</div><h1>Prepare a virtual machine</h1><p>Define the guest's resources and approved image. The controller will select a suitable worker.</p></div></div>{{if .Error}}<div class="notice error" role="alert"><span class="notice-icon">!</span><span>{{.Error}}</span></div>{{end}}<div class="form-layout"><section class="section-card form-card"><h2>VM configuration</h2><p class="intro">Choose a clear name and request only the compute resources your workload needs. Limits reflect the controller's current configuration.</p><form method="post" action="/vms"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><input type="hidden" name="idempotency_key" value="{{.IdempotencyKey}}"><div class="field"><label for="name">Virtual machine name</label><input id="name" name="name" maxlength="128" placeholder="e.g. api-staging-01" required autocomplete="off"><span class="field-hint">Use a name that identifies the workload or environment.</span></div><div class="field-grid"><div class="field"><label for="vcpus">Virtual CPUs</label><input id="vcpus" type="number" name="vcpus" min="1" max="{{.Limits.MaxVCPUs}}" value="1" required><span class="field-hint">Allowed range: 1–{{.Limits.MaxVCPUs}} vCPU</span></div><div class="field"><label for="memory_mib">Memory allocation <span class="field-hint">(MiB)</span></label><input id="memory_mib" type="number" name="memory_mib" min="1" max="{{.Limits.MaxMemoryMiB}}" value="1024" required><span class="field-hint">Maximum configured: {{.Limits.MaxMemoryMiB}} MiB</span></div></div><div class="field"><label for="image_id">Approved guest image</label><select id="image_id" name="image_id" required>{{range .Limits.AllowedImages}}<option value="{{.}}">{{.}}</option>{{else}}<option value="" disabled selected>No approved images configured</option>{{end}}</select><span class="field-hint">Image choices are restricted to the controller's allowlist.</span></div><div class="field-actions"><button class="btn btn-primary" type="submit">Submit VM request <span aria-hidden="true">→</span></button><a class="btn btn-secondary" href="/console">Cancel</a></div></form></section><aside><section class="section-card side-panel"><h3>What happens next?</h3><div class="side-step"><span class="step-number">01</span><div><strong>Validate the request</strong><p>The controller checks the name, CPU, memory, approved image, and request idempotency.</p></div></div><div class="side-step"><span class="step-number">02</span><div><strong>Select a worker</strong><p>Placement considers reported worker health, available memory, and configured limits.</p></div></div><div class="side-step"><span class="step-number">03</span><div><strong>Prepare and reconcile</strong><p>The worker performs the requested action. If the outcome is uncertain, the controller keeps it visible for reconciliation.</p></div></div><div class="callout"><strong>Important:</strong> submitting a request does not guarantee that a guest is already running. Check the operation and observed state on the dashboard.</div></section></aside></div><footer class="page-footer"><span>Aiori Compute · AIORI Hackathon engineering preview</span><span>Request fields are validated server-side.</span></footer></main></div></div></body></html>{{end}}
{{define "styles"}}<style>
:root{color-scheme:light;--ink:#172033;--muted:#667085;--line:#e5e9f1;--surface:#fff;--canvas:#f5f7fb;--nav:#10182b;--nav-muted:#a5b0c7;--brand:#5b5ce2;--brand-dark:#4647c8;--brand-soft:#efefff;--green:#13845b;--green-bg:#e8f7f0;--amber:#a85b08;--amber-bg:#fff4df;--red:#c43d4d;--red-bg:#fff0f1;--shadow:0 10px 30px rgba(25,35,60,.045)}
*{box-sizing:border-box}html{min-height:100%;background:var(--canvas)}body{margin:0;color:var(--ink);font:14px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;-webkit-font-smoothing:antialiased}a{color:inherit;text-decoration:none}button,input,select{font:inherit}button,a,.field input,.field select{transition:background .16s ease,border-color .16s ease,box-shadow .16s ease,transform .16s ease}button{cursor:pointer}button:focus-visible,a:focus-visible,input:focus-visible,select:focus-visible{outline:3px solid rgba(91,92,226,.28);outline-offset:2px}.app-shell{min-height:100vh}.sidebar{position:fixed;inset:0 auto 0 0;width:246px;padding:25px 16px;background:var(--nav);color:#fff;display:flex;flex-direction:column;z-index:3}.brand{display:flex;align-items:center;gap:11px;padding:3px 11px 31px}.brand-mark{display:grid;place-items:center;width:36px;height:36px;border-radius:11px;background:linear-gradient(145deg,#8586ff,#5455dd);box-shadow:0 5px 16px #5b5ce244;font-size:18px;font-weight:800}.brand-name{font-size:15px;font-weight:750;letter-spacing:-.35px}.brand-sub{font-size:10px;color:#9da9c4;letter-spacing:1.4px;text-transform:uppercase;margin-top:1px}.nav-label{padding:0 12px;margin:8px 0 9px;color:#7885a1;font-size:10px;font-weight:750;letter-spacing:1.5px;text-transform:uppercase}.nav-link{display:flex;align-items:center;gap:12px;margin:2px 0;padding:11px 12px;border-radius:9px;color:var(--nav-muted);font-weight:550}.nav-link .nav-icon{width:20px;text-align:center;font-size:16px}.nav-link.active{color:#fff;background:#272f49;box-shadow:inset 3px 0 #8586ff}.nav-link:hover{background:#202941;color:#fff}.sidebar-spacer{flex:1}.sidebar-note{margin:15px 5px 0;padding:14px;border:1px solid #303a54;border-radius:12px;background:#171f34}.note-top{display:flex;align-items:center;gap:8px;font-size:12px;font-weight:650}.pulse{width:7px;height:7px;border-radius:50%;background:#45d5a0;box-shadow:0 0 0 4px #45d5a01c}.sidebar-note p{margin:7px 0 0;color:#98a5bf;font-size:11px;line-height:1.55}.sidebar-bottom{padding:18px 11px 0;color:#7d8aa5;font-size:11px}.main-area{margin-left:246px;min-height:100vh}.topbar{height:72px;padding:0 38px;border-bottom:1px solid var(--line);background:#ffffffd9;display:flex;align-items:center;justify-content:space-between;gap:20px}.breadcrumbs{display:flex;align-items:center;gap:9px;color:var(--muted);font-size:12px}.breadcrumbs strong{color:var(--ink);font-weight:650}.top-actions{display:flex;align-items:center;gap:13px}.env-pill{display:inline-flex;align-items:center;gap:7px;border:1px solid #dce4ec;border-radius:20px;padding:6px 10px;background:#fff;color:#566277;font-size:11px;font-weight:650}.env-dot{width:6px;height:6px;border-radius:50%;background:#9ba6b7}.avatar{width:33px;height:33px;border-radius:11px;display:grid;place-items:center;background:#ececff;color:#4c4dbe;font-weight:800;font-size:12px}.content{max-width:1500px;margin:0 auto;padding:32px 38px 48px}.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:25px}.eyebrow{font-size:10px;letter-spacing:1.7px;text-transform:uppercase;font-weight:800;color:var(--brand);margin-bottom:7px}.page-heading h1{font-size:27px;line-height:1.2;letter-spacing:-.9px;margin:0;font-weight:760}.page-heading p{margin:9px 0 0;color:var(--muted);font-size:13px}.btn{display:inline-flex;justify-content:center;align-items:center;gap:8px;border:1px solid transparent;border-radius:9px;padding:10px 15px;font-weight:700;font-size:12px;white-space:nowrap}.btn-primary{background:var(--brand);color:#fff;box-shadow:0 4px 10px #5b5ce225}.btn-primary:hover{background:var(--brand-dark);transform:translateY(-1px)}.btn-secondary{background:#fff;border-color:#dce2ec;color:#344054}.btn-secondary:hover{background:#f8f9fc;border-color:#bfc8d8}.btn-danger{background:#fff;border-color:#f0c9cd;color:var(--red);padding:7px 10px;font-size:11px}.btn-danger:hover{background:var(--red-bg)}.btn-small{padding:7px 10px;font-size:11px}.notice{display:flex;gap:10px;align-items:flex-start;margin:0 0 18px;padding:12px 15px;border-radius:10px;border:1px solid #cdebdc;background:#effaf4;color:#176846;font-size:12px}.notice.error{border-color:#f2cdd1;background:var(--red-bg);color:#a62e3c}.notice-icon{font-weight:900}.hero{position:relative;overflow:hidden;border-radius:17px;padding:25px 28px;margin-bottom:22px;background:linear-gradient(110deg,#202b4b 0%,#343c72 60%,#5554b8 100%);color:#fff;box-shadow:0 12px 26px #27345b18}.hero:after{content:"";position:absolute;right:8%;top:-115px;width:280px;height:280px;border:1px solid #ffffff20;border-radius:50%;box-shadow:0 0 0 32px #ffffff08,0 0 0 64px #ffffff06}.hero-inner{position:relative;z-index:1;max-width:700px}.hero-kicker{display:flex;align-items:center;gap:8px;color:#c4c8ff;font-size:10px;font-weight:800;letter-spacing:1.5px;text-transform:uppercase}.hero h2{font-size:22px;letter-spacing:-.55px;margin:9px 0 7px}.hero p{max-width:600px;color:#d5daf0;margin:0;font-size:12px;line-height:1.7}.hero-meta{display:flex;flex-wrap:wrap;gap:18px;margin-top:19px;color:#d6dcf5;font-size:11px}.hero-meta span{display:flex;align-items:center;gap:7px}.hero-meta b{color:#fff;font-weight:650}.metric-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:15px;margin-bottom:24px}.metric{background:var(--surface);border:1px solid var(--line);border-radius:13px;padding:17px 18px;box-shadow:var(--shadow);min-width:0}.metric-top{display:flex;align-items:center;justify-content:space-between;gap:10px;color:var(--muted);font-size:11px;font-weight:650}.metric-icon{display:grid;place-items:center;width:32px;height:32px;border-radius:9px;background:#f0f1ff;color:var(--brand);font-size:16px}.metric-value{font-size:27px;font-weight:760;letter-spacing:-1px;margin-top:10px;line-height:1.2}.metric-foot{font-size:10px;color:var(--muted);margin-top:6px}.metric-foot .positive{color:var(--green);font-weight:700}.section-card{background:#fff;border:1px solid var(--line);border-radius:13px;box-shadow:var(--shadow);margin-bottom:21px;overflow:hidden}.section-head{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:18px 20px;border-bottom:1px solid #edf0f5}.section-title{font-size:13px;font-weight:760;letter-spacing:-.2px}.section-subtitle{font-size:11px;color:var(--muted);margin-top:3px}.section-head-right{display:flex;align-items:center;gap:9px}.count-pill{display:inline-flex;align-items:center;justify-content:center;min-width:25px;height:23px;padding:0 7px;border-radius:7px;background:#f0f1f7;color:#626d80;font-size:10px;font-weight:750}.table-wrap{overflow-x:auto}table{border-collapse:collapse;width:100%;white-space:nowrap;font-size:11px}thead{background:#fafbfe}th{text-align:left;padding:11px 17px;color:#7b8496;font-size:9px;text-transform:uppercase;letter-spacing:.9px;font-weight:800;border-bottom:1px solid #edf0f5}td{padding:14px 17px;border-bottom:1px solid #f0f2f6;color:#465167}tbody tr:last-child td{border-bottom:0}tbody tr:hover{background:#fafbff}.primary-cell{color:var(--ink);font-weight:700}.sub-cell{font-size:10px;color:#8b94a6;margin-top:3px}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:10px;color:#68738a}.status{display:inline-flex;align-items:center;gap:6px;padding:5px 8px;border-radius:7px;font-size:10px;font-weight:750;text-transform:capitalize}.status:before{content:"";width:5px;height:5px;border-radius:50%;background:currentColor}.status-healthy,.status-running,.status-succeeded{background:var(--green-bg);color:var(--green)}.status-degraded,.status-pending,.status-dispatched,.status-unknown{background:var(--amber-bg);color:var(--amber)}.status-unhealthy,.status-failed,.status-stopped{background:#eef1f5;color:#687386}.status-defined{background:var(--brand-soft);color:var(--brand)}.status-offline,.status-error{background:var(--red-bg);color:var(--red)}.resource-cell{min-width:135px}.resource-label{display:flex;justify-content:space-between;gap:10px;font-size:10px;color:#667085;margin-bottom:7px}.resource-label b{color:#344054}.progress{height:5px;background:#edf0f6;border-radius:8px;overflow:hidden}.progress span{display:block;height:100%;border-radius:8px;background:linear-gradient(90deg,#7778ef,#5556d5)}.empty-state{padding:35px 20px;text-align:center}.empty-icon{margin:0 auto 12px;width:43px;height:43px;display:grid;place-items:center;border-radius:13px;background:#f0f1ff;color:var(--brand);font-size:20px}.empty-state h3{margin:0;font-size:13px}.empty-state p{margin:6px auto 15px;max-width:350px;font-size:11px;color:var(--muted);line-height:1.7}.empty-muted{color:#8791a4;font-size:11px;padding:20px!important;text-align:center}.action-row{display:flex;align-items:center;gap:7px}.action-form{display:inline}.dashboard-bottom{display:grid;grid-template-columns:minmax(0,1.5fr) minmax(250px,1fr);gap:18px}.info-card{padding:20px}.info-card h3{font-size:12px;margin:0 0 5px}.info-card>p{margin:0;color:var(--muted);font-size:11px;line-height:1.7}.architecture-flow{display:flex;align-items:center;gap:8px;margin-top:17px;flex-wrap:wrap}.flow-node{padding:8px 10px;background:#f7f8fc;border:1px solid #e6e9f2;border-radius:8px;font-size:10px;font-weight:700}.flow-arrow{color:#a0a8b8}.health-list{margin-top:15px;display:grid;gap:12px}.health-item{display:flex;align-items:center;justify-content:space-between;gap:10px;font-size:11px}.health-item span:first-child{color:#667085}.health-value{font-weight:750;color:#344054}.form-layout{display:grid;grid-template-columns:minmax(0,1.6fr) minmax(250px,1fr);gap:20px;align-items:start}.form-card{padding:23px}.form-card h2{font-size:15px;margin:0}.form-card .intro{font-size:11px;color:var(--muted);margin:6px 0 23px;line-height:1.7}.field-grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}.field{display:flex;flex-direction:column;gap:7px;margin-bottom:18px;min-width:0}.field label{font-size:11px;font-weight:750;color:#344054}.field-hint{font-size:10px;color:#8992a4;font-weight:400}.field input,.field select{width:100%;min-width:0;border:1px solid #dce2ec;background:#fff;border-radius:8px;padding:11px 12px;color:var(--ink);font-size:12px}.field input:hover,.field select:hover{border-color:#b7c0d1}.field input:focus,.field select:focus{outline:none;border-color:#8586ef;box-shadow:0 0 0 3px #5b5ce21a}.field-actions{display:flex;gap:10px;align-items:center;padding-top:5px}.side-panel{padding:19px}.side-panel h3{font-size:12px;margin:0 0 13px}.side-step{display:flex;gap:11px;margin:14px 0}.step-number{flex:none;width:25px;height:25px;display:grid;place-items:center;border-radius:8px;background:var(--brand-soft);color:var(--brand);font-size:10px;font-weight:800}.side-step strong{display:block;font-size:11px}.side-step p{font-size:10px;color:var(--muted);margin:3px 0 0;line-height:1.6}.callout{padding:12px;border:1px solid #e4e6fb;background:#f8f8ff;border-radius:9px;color:#5354a4;font-size:10px;line-height:1.7;margin-top:18px}.page-footer{display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;padding-top:14px;color:#8a93a5;font-size:10px}.login-page{min-height:100vh;display:grid;grid-template-columns:minmax(0,1.05fr) minmax(360px,.95fr);background:#fff}.login-art{position:relative;overflow:hidden;display:flex;flex-direction:column;justify-content:space-between;padding:44px 8%;color:#fff;background:radial-gradient(ellipse at 10% 10%,#424b81 0%,transparent 48%),linear-gradient(145deg,#11192e,#252f56 65%,#41428c)}.login-art:before{content:"";position:absolute;width:470px;height:470px;right:-140px;top:17%;border:1px solid #ffffff1c;border-radius:50%;box-shadow:0 0 0 45px #ffffff08,0 0 0 90px #ffffff06}.login-art>*{position:relative;z-index:1}.login-art .brand{padding:0}.login-art-copy{max-width:510px;padding:45px 0}.login-art-copy .eyebrow{color:#aeb2ff}.login-art-copy h1{font-size:clamp(30px,4vw,48px);line-height:1.1;letter-spacing:-1.7px;margin:14px 0}.login-art-copy p{max-width:430px;color:#c5cde5;font-size:13px;line-height:1.85}.login-feature{display:flex;align-items:center;gap:10px;margin-top:26px;color:#e2e7f7;font-size:11px}.feature-check{display:grid;place-items:center;width:23px;height:23px;border-radius:7px;background:#ffffff17;color:#a6f1d4}.login-foot{color:#8f9bbc;font-size:10px}.login-foot a{color:#c9d1ff;text-decoration:underline;text-underline-offset:3px}.login-doc-link{text-align:center;color:#8b94a5;font-size:10px;margin-top:15px}.login-doc-link a{color:var(--brand);font-weight:700;text-decoration:none}.login-doc-link a:hover{text-decoration:underline}.login-main{display:flex;align-items:center;justify-content:center;padding:35px}.login-box{width:100%;max-width:385px}.login-box .brand{display:none}.login-box h2{font-size:25px;letter-spacing:-.7px;margin:0}.login-box .login-sub{font-size:12px;color:var(--muted);margin:8px 0 28px;line-height:1.7}.login-box .field{margin-bottom:19px}.login-box .btn{width:100%;padding:12px;margin-top:5px}.login-security{display:flex;align-items:center;justify-content:center;gap:7px;color:#8b94a5;font-size:10px;margin-top:20px}.alert-error{margin:0 0 18px;padding:11px 12px;border-radius:8px;border:1px solid #f0c8cd;background:#fff1f2;color:#ac3140;font-size:11px}.muted{color:var(--muted)}.ui-button-spinner{display:inline-block;width:14px;height:14px;border:2px solid currentColor;border-right-color:transparent;border-radius:50%;vertical-align:-2px;animation:ui-spin .7s linear infinite;margin-right:8px}.is-loading{opacity:.86;cursor:wait}.btn:focus-visible,a:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible{outline:3px solid #7377f5;outline-offset:3px}@keyframes ui-spin{to{transform:rotate(360deg)}}@media(prefers-reduced-motion:reduce){.ui-button-spinner{animation:none;border-right-color:currentColor}}@media(max-width:1150px){.sidebar{width:210px}.main-area{margin-left:210px}.content{padding:27px 24px 40px}.topbar{padding:0 24px}.metric-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.dashboard-bottom{grid-template-columns:1fr}}@media(max-width:760px){.sidebar{position:static;width:auto;padding:13px 16px;flex-direction:row;align-items:center;gap:12px}.brand{padding:0;flex:1}.brand-sub,.nav-label,.sidebar-note,.sidebar-bottom{display:none}.sidebar nav{display:flex;gap:4px}.nav-link{margin:0;padding:9px}.nav-link .nav-icon{width:auto}.nav-link .nav-text{display:none}.sidebar-spacer{display:none}.main-area{margin-left:0}.topbar{height:57px;padding:0 17px}.content{padding:23px 16px 32px}.page-heading{align-items:flex-start}.page-heading h1{font-size:23px}.page-heading p{max-width:420px}.hero{padding:21px}.hero h2{font-size:19px}.hero:after{right:-150px}.metric-grid{gap:10px}.metric{padding:14px}.metric-value{font-size:24px}.section-head{padding:15px}.section-head-right .btn{padding:8px 10px}.form-layout{grid-template-columns:1fr}.login-page{grid-template-columns:1fr}.login-art{display:none}.login-main{min-height:100vh;padding:28px 22px}.login-box .brand{display:flex;padding:0 0 35px}.field-grid{grid-template-columns:1fr 1fr}}@media(max-width:430px){.env-pill{display:none}.metric-grid{grid-template-columns:1fr 1fr}.metric-top{font-size:10px}.metric-icon{width:28px;height:28px}.field-grid{grid-template-columns:1fr}.page-heading{gap:10px}.page-heading>.btn{padding:9px 10px;font-size:11px}.hero-meta{gap:10px}.breadcrumbs{font-size:11px}}
.skip-link{position:absolute;left:12px;top:8px;z-index:20;transform:translateY(-160%);padding:10px 14px;border-radius:8px;background:#fff;color:#10182b;font-weight:750;box-shadow:0 3px 16px #0003}.skip-link:focus{transform:translateY(0)}.marketing-page{background:#fff;color:#18223a}.marketing-header{height:78px;padding:0 max(5.5vw,24px);display:flex;align-items:center;justify-content:space-between;gap:24px;background:#10182b;color:#fff;border-bottom:1px solid #ffffff17}.marketing-brand{display:flex;align-items:center;gap:12px}.marketing-brand .brand-mark{width:39px;height:39px}.marketing-brand-name{display:block;font-size:15px;font-weight:760;letter-spacing:-.3px}.marketing-brand-sub{display:block;margin-top:2px;color:#a8b2c9;font-size:10px;letter-spacing:1.3px;text-transform:uppercase}.marketing-nav{display:flex;align-items:center;gap:30px;color:#c3cadd;font-size:12px;font-weight:650}.marketing-nav a:not(.marketing-nav-login):hover{color:#fff}.marketing-nav-login{padding:10px 14px;border:1px solid #59627a;border-radius:8px;color:#fff}.marketing-nav-login:hover{background:#27324d;border-color:#929bb2}.marketing-hero{position:relative;isolation:isolate;overflow:hidden;display:grid;grid-template-columns:minmax(0,1fr) minmax(360px,.9fr);gap:44px;align-items:center;min-height:565px;padding:70px max(7vw,32px) 76px;background:radial-gradient(ellipse at 90% 45%,#343e6b 0%,#1c2745 35%,#10182b 72%);color:#fff}.marketing-hero-grid{position:absolute;inset:0;z-index:-1;opacity:.18;background-image:linear-gradient(#a8b5db20 1px,transparent 1px),linear-gradient(90deg,#a8b5db20 1px,transparent 1px);background-size:42px 42px;mask-image:linear-gradient(90deg,transparent,#000 65%)}.marketing-hero-content{max-width:670px;animation:rise-in .65s ease-out both}.marketing-kicker{display:inline-flex;align-items:center;gap:9px;padding:8px 12px;border:1px solid #ffffff24;border-radius:24px;background:#ffffff08;color:#c5cce0;font-size:10px;letter-spacing:.5px}.marketing-kicker-dot{width:7px;height:7px;border-radius:50%;background:#70e0b1;box-shadow:0 0 0 4px #70e0b11b}.marketing-hero h1{margin:25px 0 20px;font-size:clamp(38px,4.5vw,62px);line-height:1.08;letter-spacing:-2.7px;font-weight:780}.marketing-hero h1 span{color:#a8adff}.marketing-lede{max-width:610px;color:#bac4da;font-size:15px;line-height:1.8}.marketing-actions{display:flex;flex-wrap:wrap;gap:12px;margin-top:29px}.btn-light{background:#fff;color:#19223b;box-shadow:0 8px 25px #0002}.btn-light:hover{background:#eef0ff;transform:translateY(-2px);box-shadow:0 12px 28px #0003}.btn-outline-light{border-color:#65708b;color:#fff;background:#ffffff05}.btn-outline-light:hover{background:#ffffff12;border-color:#a6b0ca;transform:translateY(-2px)}.marketing-trust-row{display:flex;flex-wrap:wrap;gap:18px;margin-top:27px;color:#aeb9d1;font-size:10px}.marketing-trust-row span{display:flex;align-items:center;gap:6px}.marketing-trust-row span span{color:#7fe0b4;font-size:13px}.marketing-console-art{position:relative;z-index:1;max-width:570px;width:100%;justify-self:end;animation:rise-in .75s .1s ease-out both}.console-window{position:relative;overflow:hidden;border:1px solid #aeb8dd42;border-radius:14px;background:#f8f9fd;color:#202a43;box-shadow:0 28px 80px #0005,0 0 0 8px #ffffff05;transform:perspective(1100px) rotateY(-4deg) rotateX(1deg)}.console-titlebar{height:45px;display:flex;align-items:center;justify-content:space-between;gap:10px;padding:0 15px;background:#fff;border-bottom:1px solid #e7eaf2;color:#8590a6;font-size:9px}.console-dots{display:flex;gap:5px}.console-dots i{width:6px;height:6px;border-radius:50%;background:#d2d8e5}.console-live{display:flex;align-items:center;gap:5px;font-size:8px;letter-spacing:.8px}.console-live i,.console-state i{width:6px;height:6px;border-radius:50%;background:#1aaf79}.console-body{padding:22px 20px 15px}.console-welcome{display:flex;justify-content:space-between;align-items:center;margin-bottom:17px}.console-welcome strong{display:block;margin-top:4px;font-size:17px;letter-spacing:-.4px}.console-overline{color:#838da3;font-size:8px;letter-spacing:1px}.console-demo-pill{padding:5px 7px;border-radius:5px;background:#eeefff;color:#5456c9;font-size:8px;font-weight:800;letter-spacing:.7px}.console-stats{display:grid;grid-template-columns:repeat(3,1fr);gap:9px}.console-stats>div{min-width:0;padding:13px;border:1px solid #e6e9f2;border-radius:8px;background:#fff}.console-stats>div>span{display:block;color:#7c879d;font-size:8px;font-weight:700;letter-spacing:.6px}.console-stats strong{display:block;margin:8px 0 5px;font-size:25px;letter-spacing:-1px}.console-stats strong span{font-size:13px;color:#7c879d}.console-stats small{display:block;color:#8a94a7;font-size:8px}.console-stats small i{display:inline-block;width:5px;height:5px;border-radius:50%;background:#18ad7b;margin-right:4px}.console-meter{height:4px;border-radius:4px;background:#e9eaf8;overflow:hidden}.console-meter i{display:block;width:68%;height:100%;border-radius:4px;background:#6768e8}.console-section-label{display:flex;justify-content:space-between;margin:21px 0 9px;color:#7b879e;font-size:8px;font-weight:800;letter-spacing:.8px}.console-section-label span{color:#6265d9}.console-worker{display:flex;align-items:center;gap:10px;padding:10px 0;border-top:1px solid #eaedf4}.console-worker-icon{display:grid;place-items:center;width:31px;height:31px;flex-shrink:0;border-radius:8px;background:#efefff;color:#5d5ed2;font-size:9px;font-weight:800}.console-worker-icon.muted{background:#f0f2f7;color:#8992a5}.console-worker>span:nth-child(2){flex:1;min-width:0}.console-worker strong,.console-worker small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.console-worker strong{font-size:10px}.console-worker small{margin-top:3px;color:#8993a7;font-size:8px}.console-state{display:flex;align-items:center;gap:5px;color:#17845d;font-size:8px;font-weight:750}.console-state.pending{color:#a46919}.console-state.pending i{background:#d99a39}.console-footnote{display:flex;gap:6px;align-items:center;margin-top:12px;padding-top:12px;border-top:1px solid #e8ebf3;color:#858fa3;font-size:8px}.hero-orbit{position:absolute;z-index:-1;border:1px solid #b8c2ff14;border-radius:50%;pointer-events:none}.orbit-one{width:620px;height:620px;right:-190px;top:-90px}.orbit-two{width:460px;height:460px;right:-110px;top:-10px}.marketing-proof{display:grid;grid-template-columns:repeat(3,1fr);gap:0;padding:31px max(7vw,32px);border-bottom:1px solid #e8ebf2;background:#fbfcff}.marketing-proof>div{padding:6px 25px;border-right:1px solid #e6e9f1}.marketing-proof>div:first-child{padding-left:0}.marketing-proof>div:last-child{border-right:0}.proof-index{display:block;margin-bottom:10px;color:#6869df;font-size:10px;font-weight:800;letter-spacing:1px}.marketing-proof strong{font-size:13px}.marketing-proof p{margin:6px 0 0;color:#6b758a;font-size:11px;line-height:1.7}.marketing-platform{padding:88px max(7vw,32px) 96px;background:#fff}.marketing-section-intro{max-width:650px;margin-bottom:37px}.marketing-section-intro h2,.marketing-docs-cta h2{margin:0;color:#18223a;font-size:clamp(29px,3vw,40px);line-height:1.2;letter-spacing:-1.3px}.marketing-section-intro p{max-width:510px;margin:14px 0 0;color:#68738a;font-size:13px;line-height:1.8}.marketing-feature-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:17px}.marketing-feature-card{padding:25px 24px 24px;border:1px solid #e5e9f2;border-radius:13px;background:#fff;box-shadow:0 8px 28px #17213a06;transition:transform .2s ease,border-color .2s ease,box-shadow .2s ease}.marketing-feature-card:hover{transform:translateY(-4px);border-color:#c8c9fa;box-shadow:0 16px 35px #17213a0e}.marketing-feature-icon{display:grid;place-items:center;width:42px;height:42px;border-radius:11px;font-size:20px}.marketing-feature-icon.purple{background:#efefff;color:#5c5dd4}.marketing-feature-icon.blue{background:#eaf4ff;color:#3277c9}.marketing-feature-icon.green{background:#e8f8f0;color:#16875f}.feature-label{margin-top:22px;color:#777f95;font-size:9px;font-weight:800;letter-spacing:1px}.marketing-feature-card h3{margin:9px 0;color:#202a42;font-size:18px;letter-spacing:-.4px}.marketing-feature-card p{min-height:68px;margin:0 0 20px;color:#68738a;font-size:12px;line-height:1.8}.marketing-feature-card>a{display:inline-flex;align-items:center;gap:7px;color:#5557cc;font-size:11px;font-weight:750}.marketing-feature-card>a:hover{color:#3335a9;gap:11px}.marketing-docs-cta{display:flex;align-items:center;justify-content:space-between;gap:35px;padding:47px max(7vw,32px);background:#f5f6fc;border-top:1px solid #e9ebf4;border-bottom:1px solid #e9ebf4}.marketing-docs-cta>div:first-child{max-width:670px}.marketing-docs-cta h2{font-size:clamp(25px,2.7vw,34px)}.marketing-docs-cta p{max-width:620px;margin:12px 0 0;color:#68738a;font-size:12px;line-height:1.8}.marketing-cta-actions{display:flex;flex-direction:column;align-items:flex-start;gap:13px;flex-shrink:0}.marketing-text-link{color:#5557cc;font-size:11px;font-weight:700}.marketing-text-link:hover{text-decoration:underline}.marketing-footer{display:flex;align-items:center;justify-content:space-between;gap:24px;padding:27px max(5.5vw,24px);background:#10182b;color:#a8b2c9}.marketing-footer .brand-mark{width:31px;height:31px;border-radius:9px;font-size:15px}.marketing-footer .marketing-brand-name{font-size:12px}.marketing-footer .marketing-brand-sub{font-size:8px}.marketing-footer p{max-width:350px;margin:0;font-size:10px;line-height:1.7}.marketing-footer>div{display:flex;flex-wrap:wrap;gap:17px;font-size:10px}.marketing-footer a:hover{color:#fff}.login-page .login-doc-link a:hover{text-decoration:underline}.login-page .login-art-foot a:hover{text-decoration:underline}@keyframes rise-in{from{opacity:0;transform:translateY(12px)}to{opacity:1;transform:translateY(0)}}@media(max-width:1100px){.marketing-hero{grid-template-columns:minmax(0,1fr) minmax(300px,.8fr);gap:25px;padding-left:5vw;padding-right:5vw}.marketing-hero h1{font-size:clamp(37px,4.2vw,51px)}.marketing-trust-row{gap:11px}.marketing-proof,.marketing-platform,.marketing-docs-cta{padding-left:5vw;padding-right:5vw}.marketing-feature-card{padding:21px 18px}.marketing-footer{flex-wrap:wrap}.marketing-footer p{order:3;flex-basis:100%}}@media(max-width:760px){.marketing-header{height:68px;padding:0 20px}.marketing-nav{gap:14px}.marketing-nav>a:first-child{display:none}.marketing-nav-login{padding:9px 10px}.marketing-hero{grid-template-columns:1fr;gap:38px;padding:57px 22px 52px}.marketing-hero h1{font-size:clamp(38px,9vw,53px);letter-spacing:-1.8px}.marketing-lede{font-size:14px}.marketing-console-art{justify-self:center;max-width:540px}.console-window{transform:none}.marketing-proof{grid-template-columns:1fr;gap:0;padding:14px 22px}.marketing-proof>div,.marketing-proof>div:first-child{padding:17px 0;border-right:0;border-bottom:1px solid #e6e9f1}.marketing-proof>div:last-child{border-bottom:0}.marketing-proof p{margin-top:4px}.marketing-platform{padding:64px 22px}.marketing-section-intro h2{font-size:32px}.marketing-feature-grid{grid-template-columns:1fr;gap:12px}.marketing-feature-card p{min-height:0}.marketing-docs-cta{align-items:flex-start;flex-direction:column;padding:39px 22px}.marketing-cta-actions{align-items:flex-start}.marketing-footer{align-items:flex-start;flex-direction:column;padding:25px 22px}.marketing-footer p{order:0}.marketing-footer>div{gap:16px}.console-body{padding:17px 13px 13px}.console-stats{gap:6px}.console-stats>div{padding:10px 8px}.console-stats strong{font-size:21px}.console-worker{gap:7px}.console-state{font-size:7px}.marketing-trust-row{gap:10px 15px}}@media(max-width:400px){.marketing-brand-name{font-size:13px}.marketing-nav{gap:9px;font-size:11px}.marketing-nav-login{padding:8px}.console-stats>div>span{font-size:7px}.console-stats strong{font-size:18px}.console-state{font-size:0}.console-state i{width:7px;height:7px}.console-worker small{font-size:7px}}@media(prefers-reduced-motion:reduce){*,*::before,*::after{scroll-behavior:auto!important;animation-duration:.01ms!important;animation-iteration-count:1!important;transition-duration:.01ms!important}}.app-shell{font-size:15px;color:#20283a}.nav-label{font-size:12px;color:#b5bfd5}.nav-link{font-size:14px;min-height:42px}.sidebar-note p{font-size:13px;line-height:1.7;color:#c2cbe0}.sidebar-bottom{font-size:11px;letter-spacing:.6px}.topbar{font-size:14px}.breadcrumbs{font-size:14px}.env-pill{font-size:12px}.content{font-size:15px}.page-heading h1{font-size:clamp(27px,2.5vw,34px);line-height:1.2}.page-heading p{font-size:15px;line-height:1.7;color:#5a657b}.metric-top{font-size:14px}.metric-value{font-size:30px}.metric-foot{font-size:13px;line-height:1.6}.section-title{font-size:17px}.section-subtitle{font-size:13px;line-height:1.6}.section-card table{font-size:14px}.section-card th{font-size:12px;letter-spacing:.3px}.section-card td{font-size:14px;line-height:1.6}.primary-cell{font-size:14px}.sub-cell{font-size:12px;line-height:1.5}.status,.count-pill,.env-pill{min-height:26px;font-size:12px}.btn{font-size:14px;min-height:42px}.btn-small{font-size:13px;min-height:36px}.field label{font-size:14px}.field input,.field select,.field textarea{font-size:16px;min-height:44px}.field .help,.field-help{font-size:13px;line-height:1.6}.login-box h2{font-size:30px;line-height:1.2}.login-box .login-sub{font-size:16px;line-height:1.7;color:#59647a}.login-doc-link{font-size:13px;color:#59647a}.login-security{font-size:13px;color:#59647a}.alert-error{font-size:14px;line-height:1.6}.skip-link:focus{transform:translateY(0)}.form-card h2{font-size:23px;line-height:1.3}.form-card .intro{font-size:15px;line-height:1.75;color:#58647a}.field-hint{font-size:13px;line-height:1.55;color:#536078}.side-panel h3{font-size:18px}.side-step strong{font-size:15px}.side-step p{font-size:14px;line-height:1.7;color:#536078}.callout{font-size:14px;line-height:1.7}.field-actions{gap:12px}.field-actions .btn{min-height:46px;padding:12px 17px}@media(max-width:760px){.page-heading h1{font-size:25px}.metric-top{font-size:12px}.metric-foot{font-size:12px}.section-card th{font-size:11px}.section-card td{font-size:13px}}
/* Console layout refinement: consistent rhythm, alignment, and responsive behavior. */
:root{
 --space-1:4px;--space-2:8px;--space-3:12px;--space-4:16px;--space-5:20px;
 --space-6:24px;--space-8:32px;--space-10:40px;--space-12:48px;
 --content-max:1480px;--sidebar-width:264px;
}
html{scroll-behavior:smooth}
body{min-width:320px;background:#f4f6fb}
.app-shell{display:grid;grid-template-columns:var(--sidebar-width) minmax(0,1fr);min-height:100vh;font-size:14px}
.sidebar{
 position:sticky;top:0;inset:auto;width:auto;height:100vh;min-height:620px;
 grid-column:1;grid-row:1;padding:28px 18px 22px;overflow-y:auto;
 border-right:1px solid #28324a;box-shadow:4px 0 24px rgba(16,24,43,.06)
}
.brand{gap:12px;padding:2px 10px 34px}
.brand-mark{width:40px;height:40px;flex:0 0 40px;border-radius:12px}
.brand strong{display:block;font-size:15px;line-height:1.35;letter-spacing:-.25px}
.brand small{display:block;margin-top:3px;color:#a5b0c7;font-size:10px;letter-spacing:1.35px;text-transform:uppercase}
.sidebar-nav{display:flex;flex-direction:column;gap:5px}
.nav-label{margin:7px 0 10px;padding:0 12px;font-size:10px;letter-spacing:1.35px}
.nav-link{min-height:46px;margin:0;padding:11px 13px;gap:13px;border:1px solid transparent;border-radius:10px;font-size:13px;line-height:1.3}
.nav-link .nav-icon{width:21px;flex:0 0 21px;font-size:17px}
.nav-link:hover{background:#202a42;border-color:#303c59}
.sidebar-spacer{min-height:24px}
.sidebar-logout{margin-top:auto;padding:16px 4px 0;border-top:1px solid #303a54}
.sidebar-logout .btn{width:100%;justify-content:center}
.main-area{grid-column:2;grid-row:1;min-width:0;margin:0;min-height:100vh}
.topbar{
 position:sticky;top:0;z-index:2;min-height:68px;height:auto;padding:13px clamp(22px,3vw,42px);
 background:rgba(255,255,255,.94);backdrop-filter:blur(12px);
}
.breadcrumbs{gap:10px;font-size:12px;line-height:1.4}
.top-actions{gap:12px}
.content{width:100%;max-width:var(--content-max);margin:0 auto;padding:34px clamp(22px,3vw,44px) 52px}
.page-heading{align-items:center;gap:24px;margin:0 0 28px}
.page-heading>div{min-width:0}
.eyebrow{margin-bottom:9px;font-size:10px;letter-spacing:1.5px}
.page-heading h1{font-size:clamp(27px,2.4vw,35px);line-height:1.18;letter-spacing:-.9px}
.page-heading p{max-width:760px;margin:10px 0 0;font-size:14px;line-height:1.7;color:#667085}
.metric-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px;margin:0 0 24px}
.metric,.section-card,.info-card{
 min-width:0;border:1px solid #e4e8f0;border-radius:14px;background:#fff;
 box-shadow:0 5px 20px rgba(23,32,51,.035)
}
.metric{padding:20px 21px}
.metric-top{display:flex;align-items:center;justify-content:space-between;gap:12px;color:#596579;font-size:12px;font-weight:650}
.metric-icon{display:grid;place-items:center;width:34px;height:34px;border-radius:10px;background:#f0f1ff;color:#5b5ce2;font-size:16px}
.metric-value{margin:15px 0 4px;font-size:32px;line-height:1;font-weight:760;letter-spacing:-1.2px}
.metric-foot{color:#8791a3;font-size:11px;line-height:1.5}
.section-card{margin:0 0 22px;padding:24px}
.section-head{display:flex;align-items:center;justify-content:space-between;gap:18px;margin:0 0 19px}
.section-head>div:first-child{min-width:0}
.section-title{margin:0;color:#202a3e;font-size:16px;line-height:1.35;font-weight:750;letter-spacing:-.2px}
.section-subtitle{margin-top:5px;color:#7b8597;font-size:12px;line-height:1.55}
.section-head-right{display:flex;align-items:center;justify-content:flex-end;flex-wrap:wrap;gap:10px}
.count-pill{display:inline-flex;align-items:center;justify-content:center;min-height:29px;padding:5px 10px;border:1px solid #e3e7ef;border-radius:20px;background:#fafbfe;color:#667085;font-size:11px;font-weight:650;white-space:nowrap}
.table-wrap{width:100%;overflow-x:auto;border:1px solid #edf0f5;border-radius:10px}
.section-card table{width:100%;min-width:700px;border-collapse:separate;border-spacing:0;font-size:12px}
.section-card th{padding:13px 15px;background:#f8f9fc;color:#7a8497;text-align:left;font-size:10px;font-weight:750;letter-spacing:.55px;text-transform:uppercase;white-space:nowrap}
.section-card td{padding:14px 15px;border-top:1px solid #edf0f5;color:#465166;vertical-align:middle;font-size:12px;line-height:1.5}
.section-card tbody tr:hover{background:#fbfcff}
.primary-cell{color:#253047;font-size:12px;font-weight:700;overflow-wrap:anywhere}
.sub-cell{margin-top:3px;color:#8992a3;font-size:10px;overflow-wrap:anywhere}
.mono,code{font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-size:.92em;overflow-wrap:anywhere}
.resource-cell{min-width:115px}
.resource-label{display:flex;justify-content:space-between;gap:8px;margin-bottom:7px;font-size:10px}
.progress{height:5px;overflow:hidden;border-radius:10px;background:#edf0f6}
.progress span{display:block;height:100%;border-radius:inherit;background:#696be6}
.status{display:inline-flex;align-items:center;min-height:25px;padding:4px 9px;border:1px solid #e5e8ef;border-radius:20px;background:#f7f8fb;color:#657086;font-size:10px;font-weight:700;white-space:nowrap}
.status-healthy,.status-running{border-color:#ccebdd;background:#eaf8f1;color:#147a55}
.status-unhealthy,.status-failed,.status-error{border-color:#f2d5d9;background:#fff0f1;color:#b33a4a}
.status-pending,.status-starting,.status-stopping,.status-unknown{border-color:#f0e1c4;background:#fff7e9;color:#98600e}
.empty-state{display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:210px;padding:34px 20px;text-align:center}
.empty-icon{display:grid;place-items:center;width:48px;height:48px;margin-bottom:13px;border-radius:14px;background:#f0f1ff;color:#5b5ce2;font-size:22px}
.empty-state h3{margin:0;color:#273249;font-size:16px}
.empty-state p{max-width:480px;margin:8px auto 17px;color:#788397;font-size:12px;line-height:1.7}
.dashboard-bottom{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px;margin-top:4px}
.info-card{margin:0;padding:22px}
.info-card h3{margin:0 0 9px;font-size:15px}
.info-card p{margin:0;color:#6c778b;font-size:12px;line-height:1.75}
.architecture-flow{display:flex;align-items:center;flex-wrap:wrap;gap:8px;margin-top:16px}
.flow-node{padding:7px 9px;border:1px solid #e4e7f0;border-radius:7px;background:#f8f9fc;color:#566176;font-size:10px;font-weight:650}
.flow-arrow{color:#a0a8b7;font-size:12px}
.page-footer{display:flex;justify-content:space-between;flex-wrap:wrap;gap:8px;margin-top:28px;padding-top:17px;border-top:1px solid #e7eaf1;color:#9aa2b1;font-size:10px}
.notice{display:flex;align-items:flex-start;gap:10px;margin:0 0 20px;padding:13px 16px;border:1px solid #cfe9dc;border-radius:10px;background:#eff9f4;color:#176d4d;font-size:12px;line-height:1.6}
.notice.error{border-color:#f0d5d9;background:#fff3f4;color:#a93848}
.notice-icon{font-weight:800}
.dashboard-grid{display:grid;grid-template-columns:minmax(0,1.15fr) minmax(300px,.85fr);align-items:start;gap:20px}
.dashboard-grid>.section-card{margin:0}
.form-layout{display:grid;grid-template-columns:minmax(0,1.5fr) minmax(280px,.8fr);align-items:start;gap:22px}
.form-card{padding:28px}
.form-card h2{margin:0 0 8px;font-size:20px}
.form-card .intro{margin:0 0 25px;color:#6d788c;font-size:13px;line-height:1.7}
.form-stack{display:flex;flex-direction:column;gap:19px}
.field{display:flex;flex-direction:column;gap:7px;min-width:0;margin:0 0 17px}
.form-stack .field{margin:0}
.field label{color:#344054;font-size:12px;font-weight:700}
.field input,.field select,.field textarea{width:100%;min-width:0;min-height:45px;padding:10px 12px;border:1px solid #dce2eb;border-radius:8px;background:#fff;color:#263248;font-size:13px;line-height:1.4}
.field input::placeholder{color:#a3abba}
.field input:hover,.field select:hover,.field textarea:hover{border-color:#b9c2d2}
.field input:focus,.field select:focus,.field textarea:focus{border-color:#7778e8;outline:0;box-shadow:0 0 0 3px rgba(91,92,226,.12)}
.field-hint,.field-help{color:#8992a3;font-size:11px;line-height:1.55}
.field-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}
.field-actions,.action-row{display:flex;align-items:center;flex-wrap:wrap;gap:10px;margin-top:20px}
.btn{min-height:39px;padding:9px 14px;border-radius:8px;font-size:11px;font-weight:700;line-height:1.2}
.btn-small{min-height:32px;padding:7px 10px;font-size:10px}
.btn-primary{background:#5758d8;box-shadow:0 3px 8px rgba(91,92,226,.16)}
.btn-primary:hover{background:#4546c4;transform:translateY(-1px)}
.btn-secondary{border-color:#dfe4ed;background:#fff;color:#475467}
.btn-secondary:hover{border-color:#c7cde0;background:#f8f9fd}
.btn-danger{background:#fff0f1;color:#b63b4b;border-color:#f2d4d8}
.btn-danger:hover{background:#ffe4e7}
.side-panel{padding:24px}
.side-panel h3{margin:0 0 20px}
.side-step{display:grid;grid-template-columns:30px minmax(0,1fr);gap:12px;margin:0 0 18px}
.step-number{display:grid;place-items:center;width:28px;height:28px;border-radius:8px;background:#f0f1ff;color:#5b5ce2;font-size:10px;font-weight:800}
.side-step strong{font-size:12px}
.side-step p{margin:4px 0 0;color:#778296;font-size:11px;line-height:1.65}
.callout{margin-top:18px;padding:14px 15px;border:1px solid #e5e8f1;border-radius:9px;background:#f8f9fc;color:#657086;font-size:11px;line-height:1.7}
.action-form{display:inline-flex;margin:0}
.sidebar-logout .btn{min-height:38px}
.skip-link{z-index:10}
@media(max-width:1180px){
 :root{--sidebar-width:235px}
 .metric-grid{grid-template-columns:repeat(2,minmax(0,1fr))}
 .dashboard-grid{grid-template-columns:1fr}
 .form-layout{grid-template-columns:minmax(0,1.25fr) minmax(260px,.75fr)}
 .content{padding-left:26px;padding-right:26px}
}
@media(max-width:850px){
 .app-shell{display:flex;flex-direction:column}
 .sidebar{position:relative;top:auto;width:100%;height:auto;min-height:0;padding:14px 18px 13px;overflow:visible;border-right:0;border-bottom:1px solid #29334c}
 .brand{padding:2px 4px 13px}
 .brand-mark{width:34px;height:34px;flex-basis:34px}
 .sidebar-nav{display:flex;flex-direction:row;flex-wrap:wrap;gap:5px}
 .nav-label{display:none}
 .nav-link{min-height:36px;padding:8px 10px;gap:7px;font-size:11px}
 .nav-link .nav-icon{width:auto;font-size:13px}
 .sidebar-spacer{display:none}
 .sidebar-logout{position:absolute;right:18px;top:13px;margin:0;padding:0;border:0}
 .sidebar-logout .btn{min-height:34px;padding:7px 10px;font-size:10px}
 .main-area{width:100%;min-height:0}
 .topbar{position:relative;min-height:56px;padding:10px 22px}
 .content{padding:27px 22px 40px}
}
@media(max-width:620px){
 .topbar{align-items:flex-start;flex-direction:column;gap:9px}
 .top-actions{width:100%;justify-content:space-between}
 .page-heading{align-items:flex-start;flex-direction:column;gap:15px;margin-bottom:22px}
 .page-heading h1{font-size:26px}
 .metric-grid{grid-template-columns:repeat(2,minmax(0,1fr));gap:11px}
 .metric{padding:15px}
 .metric-value{font-size:27px}
 .section-card{padding:17px;margin-bottom:16px}
 .section-head{align-items:flex-start;flex-direction:column;gap:12px}
 .section-head-right{justify-content:flex-start}
 .dashboard-bottom,.form-layout{grid-template-columns:1fr;gap:14px}
 .form-card{padding:19px}
 .field-grid{grid-template-columns:1fr;gap:0}
 .content{padding:22px 15px 34px}
 .sidebar{padding:12px 13px}
 .sidebar-logout{right:13px}
 .nav-link{padding:7px 8px;font-size:10px}
 .nav-link .nav-icon{display:none}
 .page-footer{flex-direction:column}
}
@media(max-width:380px){
 .metric-grid{grid-template-columns:1fr}
 .env-pill{font-size:10px}
 .section-card{padding:14px}
}
</style>{{end}}`;


const workersPageTemplate = ""
