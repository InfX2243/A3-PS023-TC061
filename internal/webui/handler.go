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
	"strconv"
	"strings"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/controllerapi"
	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
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
	limits Limits
	secureCookie bool
	templates *template.Template
}

type vmRow struct { VM domain.VM; Operation domain.Operation; HasOperation bool }
type dashboardData struct { Workers []domain.Worker; VMs []vmRow; Error string; Notice string; Refreshing bool; CSRFToken string }
type formData struct { Limits Limits; Error string; IdempotencyKey string; CSRFToken string }

func NewHandler(service Service, operatorToken string, limits Limits, secureCookie bool) *Handler {
	return &Handler{service: service, token: operatorToken, limits: limits, secureCookie: secureCookie, templates: template.Must(template.New("pages").Funcs(template.FuncMap{"divMiB": func(v uint64) uint64 { return v / (1024 * 1024) }, "stateIs": func(state domain.VMState, expected string) bool { return string(state) == expected }, "countWorkerState": func(workers []domain.Worker, expected string) int { count := 0; for _, worker := range workers { if string(worker.State) == expected { count++ } }; return count }, "countVMState": func(vms []vmRow, expected string) int { count := 0; for _, vm := range vms { if string(vm.VM.Observed) == expected { count++ } }; return count }, "memoryPercent": func(available, limit uint64) int { if limit == 0 { return 0 }; percent := int(available * 100 / limit); if percent > 100 { return 100 }; if percent < 0 { return 0 }; return percent }}).Parse(pageTemplates))}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", h.loginPage)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("POST /logout", h.logout)
	mux.HandleFunc("GET /", h.dashboard)
	mux.HandleFunc("GET /vms/new", h.newVMPage)
	mux.HandleFunc("POST /vms", h.createVM)
	mux.HandleFunc("POST /vms/{vmID}/start", h.startVM)
	mux.HandleFunc("POST /vms/{vmID}/stop", h.stopVM)
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	h.render(w, "login", map[string]string{"Error": ""})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if h.token == "" {
		h.renderStatus(w, http.StatusServiceUnavailable, "login", map[string]string{"Error": "Operator authentication is not configured. Set VM_CONTROLLER_OPERATOR_TOKEN."})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := r.ParseForm(); err != nil {
		h.renderStatus(w, http.StatusBadRequest, "login", map[string]string{"Error": "Invalid login form."})
		return
	}
	got := r.FormValue("token")
	if len(got) != len(h.token) || subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) != 1 {
		h.renderStatus(w, http.StatusUnauthorized, "login", map[string]string{"Error": "Invalid credentials."})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: h.newSession(), Path: "/", HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: int((12*time.Hour).Seconds())})
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
	http.Redirect(w, r, "/?notice="+template.URLQueryEscaper("VM request recorded; operation "+string(op.State)), http.StatusSeeOther)
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
		http.Redirect(w, r, "/?error="+template.URLQueryEscaper("Operation could not be confirmed. Refresh the dashboard before retrying."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?notice="+template.URLQueryEscaper("Operation "+op.ID+" is "+string(op.State)), http.StatusSeeOther)
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
	w.Header().Set("Content-Security-Policy", "default-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
}

func (h *Handler) render(w http.ResponseWriter, page string, data any) { h.renderStatus(w, http.StatusOK, page, data) }
func (h *Handler) renderStatus(w http.ResponseWriter, status int, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.templates.ExecuteTemplate(w, page, data); err != nil { return }
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
{{define "login"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#11192e"><title>Sign in · Aiori Compute</title>{{template "styles" .}}</head><body><div class="login-page"><section class="login-art"><div class="brand"><div class="brand-mark">A</div><div><div class="brand-name">Aiori Compute</div><div class="brand-sub">Virtual infrastructure</div></div></div><div class="login-art-copy"><div class="eyebrow">Engineering control plane</div><h1>Compute infrastructure.<br>Under your control.</h1><p>A focused control plane for preparing, placing, and operating virtual machines across your worker fleet. Clear state, predictable operations, no guesswork.</p><div class="login-feature"><span class="feature-check">✓</span> Capacity-aware VM placement</div><div class="login-feature"><span class="feature-check">✓</span> Auditable lifecycle operations</div><div class="login-feature"><span class="feature-check">✓</span> Worker health at a glance</div></div><div class="login-foot">Aiori Compute · Engineering preview</div></section><main class="login-main"><div class="login-box"><div class="brand"><div class="brand-mark">A</div><div><div class="brand-name">Aiori Compute</div><div class="brand-sub">Virtual infrastructure</div></div></div><div class="eyebrow">Operator access</div><h2>Welcome back</h2><p class="login-sub">Sign in with your operator token to access the infrastructure dashboard.</p>{{if .Error}}<div class="alert-error" role="alert">{{.Error}}</div>{{end}}<form method="post" action="/login"><div class="field"><label for="token">Operator token</label><input id="token" type="password" name="token" autocomplete="current-password" placeholder="Enter your access token" required autofocus></div><button class="btn btn-primary" type="submit">Sign in <span aria-hidden="true">→</span></button></form><div class="login-security"><span aria-hidden="true">◈</span> Protected operator session · 12-hour expiry</div></div></main></div></body></html>{{end}}
{{define "dashboard"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#f5f7fb"><title>Overview · Aiori Compute</title>{{if .Refreshing}}<meta http-equiv="refresh" content="5">{{end}}{{template "styles" .}}</head><body><div class="app-shell"><aside class="sidebar"><a class="brand" href="/"><span class="brand-mark">A</span><span><span class="brand-name">Aiori Compute</span><span class="brand-sub" style="display:block">Control plane</span></span></a><div class="nav-label">Workspace</div><nav aria-label="Main navigation"><a class="nav-link active" href="/"><span class="nav-icon">▦</span><span class="nav-text">Overview</span></a><a class="nav-link" href="/#workers"><span class="nav-icon">▤</span><span class="nav-text">Worker fleet</span></a><a class="nav-link" href="/#vms"><span class="nav-icon">▣</span><span class="nav-text">Virtual machines</span></a><a class="nav-link" href="/vms/new"><span class="nav-icon">＋</span><span class="nav-text">Prepare a VM</span></a></nav><div class="sidebar-spacer"></div><div class="sidebar-note"><div class="note-top"><span class="pulse"></span> Control plane online</div><p>Dashboard data is read from the controller's current inventory. Worker and VM states may be pending reconciliation.</p></div><div class="sidebar-bottom">AIORI HACKATHON · ENGINEERING BUILD</div></aside><div class="main-area"><header class="topbar"><div class="breadcrumbs"><span>Workspace</span><span aria-hidden="true">/</span><strong>Overview</strong></div><div class="top-actions"><span class="env-pill"><span class="env-dot"></span> Engineering environment</span><div class="avatar" aria-label="Operator">OP</div><form method="post" action="/logout" class="action-form"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><button class="btn btn-secondary btn-small" type="submit">Sign out</button></form></div></header><main class="content"><div class="page-heading"><div><div class="eyebrow">Infrastructure overview</div><h1>Good to see you, operator.</h1><p>Monitor your worker fleet and manage virtual machines from one place.</p></div><a class="btn btn-primary" href="/vms/new"><span aria-hidden="true">＋</span> Prepare a VM</a></div>{{if .Notice}}<div class="notice" role="status"><span class="notice-icon">✓</span><span>{{.Notice}}</span></div>{{end}}{{if .Error}}<div class="notice error" role="alert"><span class="notice-icon">!</span><span>{{.Error}}</span></div>{{end}}<section class="hero"><div class="hero-inner"><div class="hero-kicker"><span class="pulse"></span> Infrastructure control plane</div><h2>Your compute fleet, at a glance.</h2><p>See where capacity lives, understand the current state of each machine, and trigger lifecycle operations with clear feedback when the controller is still reconciling an outcome.</p><div class="hero-meta"><span><b>●</b> Worker-aware placement</span><span><b>↻</b> Operation reconciliation</span><span><b>⌁</b> Controlled lifecycle</span></div></div></section><section class="metric-grid" aria-label="Infrastructure summary"><article class="metric"><div class="metric-top"><span>Registered workers</span><span class="metric-icon">▤</span></div><div class="metric-value">{{len .Workers}}</div><div class="metric-foot">Worker agents in inventory</div></article><article class="metric"><div class="metric-top"><span>Virtual machines</span><span class="metric-icon">▣</span></div><div class="metric-value">{{len .VMs}}</div><div class="metric-foot">Tracked VM records</div></article><article class="metric"><div class="metric-top"><span>Healthy workers</span><span class="metric-icon">⌁</span></div><div class="metric-value">{{countWorkerState .Workers "healthy"}}</div><div class="metric-foot">Reporting healthy status</div></article><article class="metric"><div class="metric-top"><span>Running VMs</span><span class="metric-icon">▶</span></div><div class="metric-value">{{countVMState .VMs "running"}}</div><div class="metric-foot">Currently observed as running</div></article></section><section class="section-card" id="workers"><div class="section-head"><div><div class="section-title">Worker fleet</div><div class="section-subtitle">Compute nodes registered with the controller</div></div><div class="section-head-right"><span class="count-pill">{{len .Workers}} total</span></div></div>{{if .Workers}}<div class="table-wrap"><table><thead><tr><th>Worker</th><th>Health</th><th>Available memory</th><th>Memory limit</th><th>Last heartbeat</th></tr></thead><tbody>{{range .Workers}}<tr><td><div class="primary-cell">{{.Name}}</div><div class="sub-cell mono">{{.ID}}</div></td><td><span class="status status-{{.State}}">{{.State}}</span></td><td><div class="resource-cell"><div class="resource-label"><b>{{divMiB .AvailableMemoryBytes}} MiB</b><span>available</span></div><div class="progress"><span style="width:{{memoryPercent .AvailableMemoryBytes .MemoryLimitBytes}}%"></span></div></div></td><td>{{divMiB .MemoryLimitBytes}} MiB</td><td>{{if .LastHeartbeat.IsZero}}<span class="muted">No heartbeat yet</span>{{else}}{{.LastHeartbeat.Format "02 Jan 2006, 15:04:05 MST"}}{{end}}</td></tr>{{end}}</tbody></table></div>{{else}}<div class="empty-state"><div class="empty-icon">▤</div><h3>No workers registered yet</h3><p>The controller has no worker inventory to schedule against. Start a configured worker agent to make compute capacity available.</p></div>{{end}}</section><section class="section-card" id="vms"><div class="section-head"><div><div class="section-title">Virtual machines</div><div class="section-subtitle">VM inventory, observed state, and latest lifecycle operation</div></div><div class="section-head-right"><span class="count-pill">{{len .VMs}} total</span><a class="btn btn-secondary btn-small" href="/vms/new">＋ New VM</a></div></div>{{if .VMs}}<div class="table-wrap"><table><thead><tr><th>Virtual machine</th><th>Placement</th><th>Resources</th><th>Image</th><th>Observed state</th><th>Latest operation</th><th>Actions</th></tr></thead><tbody>{{range .VMs}}<tr><td><div class="primary-cell">{{.VM.Name}}</div><div class="sub-cell mono">{{.VM.ID}}</div></td><td><div class="primary-cell">{{if .VM.WorkerID}}{{.VM.WorkerID}}{{else}}Unassigned{{end}}</div><div class="sub-cell">Worker node</div></td><td><div class="primary-cell">{{.VM.VCPUs}} vCPU</div><div class="sub-cell">{{divMiB .VM.MemoryBytes}} MiB memory</div></td><td><span class="mono">{{.VM.ImageID}}</span></td><td><span class="status status-{{.VM.Observed}}">{{.VM.Observed}}</span></td><td>{{if .HasOperation}}<div class="primary-cell">{{.Operation.Type}}</div><div class="sub-cell"><span class="status status-{{.Operation.State}}">{{.Operation.State}}</span></div>{{else}}<span class="muted">No operation</span>{{end}}</td><td><div class="action-row">{{if or (stateIs .VM.Observed "stopped") (stateIs .VM.Observed "defined")}}<form class="action-form" method="post" action="/vms/{{.VM.ID}}/start"><input type="hidden" name="csrf_token" value="{{$.CSRFToken}}"><button class="btn btn-secondary btn-small" type="submit">▶ Start</button></form>{{end}}{{if stateIs .VM.Observed "running"}}<form class="action-form" method="post" action="/vms/{{.VM.ID}}/stop"><input type="hidden" name="csrf_token" value="{{$.CSRFToken}}"><button class="btn btn-danger" type="submit">■ Stop</button></form>{{end}}{{if or (stateIs .VM.Observed "unknown") (stateIs .VM.Observed "starting") (stateIs .VM.Observed "stopping")}}<span class="muted">Reconciling</span>{{end}}</div></td></tr>{{end}}</tbody></table></div>{{else}}<div class="empty-state"><div class="empty-icon">▣</div><h3>Your VM inventory is ready</h3><p>Create your first virtual machine to see its placement, resource allocation, image, and lifecycle state here.</p><a class="btn btn-primary" href="/vms/new">＋ Prepare your first VM</a></div>{{end}}</section><div class="dashboard-bottom"><section class="section-card info-card"><h3>How the platform works</h3><p>The controller accepts requests and selects a suitable worker. The worker agent manages the Cloud Hypervisor process, while reconciliation keeps reported state aligned with observed outcomes.</p><div class="architecture-flow"><span class="flow-node">Operator UI</span><span class="flow-arrow">→</span><span class="flow-node">Controller</span><span class="flow-arrow">→</span><span class="flow-node">Worker agent</span><span class="flow-arrow">→</span><span class="flow-node">Cloud Hypervisor</span></div></section><section class="section-card info-card"><h3>Operational signals</h3><p>Use these signals to understand what the dashboard can currently confirm.</p><div class="health-list"><div class="health-item"><span>Worker inventory</span><span class="health-value">{{len .Workers}} registered</span></div><div class="health-item"><span>VM inventory</span><span class="health-value">{{len .VMs}} tracked</span></div><div class="health-item"><span>Pending operations</span><span class="health-value">{{if .Refreshing}}Reconciliation active{{else}}No unresolved operation detected{{end}}</span></div></div></section></div><footer class="page-footer"><span>Aiori Compute · AIORI Hackathon engineering preview</span><span>States reflect controller inventory, not a live hypervisor console.</span></footer></main></div></div></body></html>{{end}}
{{define "new-vm"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#f5f7fb"><title>Prepare a VM · Aiori Compute</title>{{template "styles" .}}</head><body><div class="app-shell"><aside class="sidebar"><a class="brand" href="/"><span class="brand-mark">A</span><span><span class="brand-name">Aiori Compute</span><span class="brand-sub" style="display:block">Control plane</span></span></a><div class="nav-label">Workspace</div><nav aria-label="Main navigation"><a class="nav-link" href="/"><span class="nav-icon">▦</span><span class="nav-text">Overview</span></a><a class="nav-link" href="/#workers"><span class="nav-icon">▤</span><span class="nav-text">Worker fleet</span></a><a class="nav-link active" href="/vms/new"><span class="nav-icon">＋</span><span class="nav-text">Prepare a VM</span></a></nav><div class="sidebar-spacer"></div><div class="sidebar-note"><div class="note-top"><span class="pulse"></span> Safe-by-default workflow</div><p>Only configured, approved image IDs can be selected. Resource requests are validated before submission.</p></div><div class="sidebar-bottom">AIORI HACKATHON · ENGINEERING BUILD</div></aside><div class="main-area"><header class="topbar"><div class="breadcrumbs"><a href="/">Workspace</a><span aria-hidden="true">/</span><strong>Prepare a VM</strong></div><div class="top-actions"><span class="env-pill"><span class="env-dot"></span> Engineering environment</span><a class="btn btn-secondary btn-small" href="/">← Dashboard</a></div></header><main class="content"><div class="page-heading"><div><div class="eyebrow">Compute provisioning</div><h1>Prepare a virtual machine</h1><p>Define the guest's resources and approved image. The controller will select a suitable worker.</p></div></div>{{if .Error}}<div class="notice error" role="alert"><span class="notice-icon">!</span><span>{{.Error}}</span></div>{{end}}<div class="form-layout"><section class="section-card form-card"><h2>VM configuration</h2><p class="intro">Choose a clear name and request only the compute resources your workload needs. Limits reflect the controller's current configuration.</p><form method="post" action="/vms"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><input type="hidden" name="idempotency_key" value="{{.IdempotencyKey}}"><div class="field"><label for="name">Virtual machine name</label><input id="name" name="name" maxlength="128" placeholder="e.g. api-staging-01" required autocomplete="off"><span class="field-hint">Use a name that identifies the workload or environment.</span></div><div class="field-grid"><div class="field"><label for="vcpus">Virtual CPUs</label><input id="vcpus" type="number" name="vcpus" min="1" max="{{.Limits.MaxVCPUs}}" value="1" required><span class="field-hint">Allowed range: 1–{{.Limits.MaxVCPUs}} vCPU</span></div><div class="field"><label for="memory_mib">Memory allocation <span class="field-hint">(MiB)</span></label><input id="memory_mib" type="number" name="memory_mib" min="1" max="{{.Limits.MaxMemoryMiB}}" value="1024" required><span class="field-hint">Maximum configured: {{.Limits.MaxMemoryMiB}} MiB</span></div></div><div class="field"><label for="image_id">Approved guest image</label><select id="image_id" name="image_id" required>{{range .Limits.AllowedImages}}<option value="{{.}}">{{.}}</option>{{else}}<option value="" disabled selected>No approved images configured</option>{{end}}</select><span class="field-hint">Image choices are restricted to the controller's allowlist.</span></div><div class="field-actions"><button class="btn btn-primary" type="submit">Submit VM request <span aria-hidden="true">→</span></button><a class="btn btn-secondary" href="/">Cancel</a></div></form></section><aside><section class="section-card side-panel"><h3>What happens next?</h3><div class="side-step"><span class="step-number">01</span><div><strong>Validate the request</strong><p>The controller checks the name, CPU, memory, approved image, and request idempotency.</p></div></div><div class="side-step"><span class="step-number">02</span><div><strong>Select a worker</strong><p>Placement considers reported worker health, available memory, and configured limits.</p></div></div><div class="side-step"><span class="step-number">03</span><div><strong>Prepare and reconcile</strong><p>The worker performs the requested action. If the outcome is uncertain, the controller keeps it visible for reconciliation.</p></div></div><div class="callout"><strong>Important:</strong> submitting a request does not guarantee that a guest is already running. Check the operation and observed state on the dashboard.</div></section><section class="section-card side-panel"><h3>Configured request limits</h3><div class="health-list"><div class="health-item"><span>Maximum vCPUs</span><span class="health-value">{{.Limits.MaxVCPUs}}</span></div><div class="health-item"><span>Maximum memory</span><span class="health-value">{{.Limits.MaxMemoryMiB}} MiB</span></div><div class="health-item"><span>Approved images</span><span class="health-value">{{len .Limits.AllowedImages}}</span></div></div></section></aside></div><footer class="page-footer"><span>Aiori Compute · AIORI Hackathon engineering preview</span><span>Request fields are validated server-side.</span></footer></main></div></div></body></html>{{end}}
{{define "styles"}}<style>
:root{color-scheme:light;--ink:#172033;--muted:#667085;--line:#e5e9f1;--surface:#fff;--canvas:#f5f7fb;--nav:#10182b;--nav-muted:#a5b0c7;--brand:#5b5ce2;--brand-dark:#4647c8;--brand-soft:#efefff;--green:#13845b;--green-bg:#e8f7f0;--amber:#a85b08;--amber-bg:#fff4df;--red:#c43d4d;--red-bg:#fff0f1;--shadow:0 10px 30px rgba(25,35,60,.045)}
*{box-sizing:border-box}html{min-height:100%;background:var(--canvas)}body{margin:0;color:var(--ink);font:14px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;-webkit-font-smoothing:antialiased}a{color:inherit;text-decoration:none}button,input,select{font:inherit}button,a,.field input,.field select{transition:background .16s ease,border-color .16s ease,box-shadow .16s ease,transform .16s ease}button{cursor:pointer}button:focus-visible,a:focus-visible,input:focus-visible,select:focus-visible{outline:3px solid rgba(91,92,226,.28);outline-offset:2px}.app-shell{min-height:100vh}.sidebar{position:fixed;inset:0 auto 0 0;width:246px;padding:25px 16px;background:var(--nav);color:#fff;display:flex;flex-direction:column;z-index:3}.brand{display:flex;align-items:center;gap:11px;padding:3px 11px 31px}.brand-mark{display:grid;place-items:center;width:36px;height:36px;border-radius:11px;background:linear-gradient(145deg,#8586ff,#5455dd);box-shadow:0 5px 16px #5b5ce244;font-size:18px;font-weight:800}.brand-name{font-size:15px;font-weight:750;letter-spacing:-.35px}.brand-sub{font-size:10px;color:#9da9c4;letter-spacing:1.4px;text-transform:uppercase;margin-top:1px}.nav-label{padding:0 12px;margin:8px 0 9px;color:#7885a1;font-size:10px;font-weight:750;letter-spacing:1.5px;text-transform:uppercase}.nav-link{display:flex;align-items:center;gap:12px;margin:2px 0;padding:11px 12px;border-radius:9px;color:var(--nav-muted);font-weight:550}.nav-link .nav-icon{width:20px;text-align:center;font-size:16px}.nav-link.active{color:#fff;background:#272f49;box-shadow:inset 3px 0 #8586ff}.nav-link:hover{background:#202941;color:#fff}.sidebar-spacer{flex:1}.sidebar-note{margin:15px 5px 0;padding:14px;border:1px solid #303a54;border-radius:12px;background:#171f34}.note-top{display:flex;align-items:center;gap:8px;font-size:12px;font-weight:650}.pulse{width:7px;height:7px;border-radius:50%;background:#45d5a0;box-shadow:0 0 0 4px #45d5a01c}.sidebar-note p{margin:7px 0 0;color:#98a5bf;font-size:11px;line-height:1.55}.sidebar-bottom{padding:18px 11px 0;color:#7d8aa5;font-size:11px}.main-area{margin-left:246px;min-height:100vh}.topbar{height:72px;padding:0 38px;border-bottom:1px solid var(--line);background:#ffffffd9;display:flex;align-items:center;justify-content:space-between;gap:20px}.breadcrumbs{display:flex;align-items:center;gap:9px;color:var(--muted);font-size:12px}.breadcrumbs strong{color:var(--ink);font-weight:650}.top-actions{display:flex;align-items:center;gap:13px}.env-pill{display:inline-flex;align-items:center;gap:7px;border:1px solid #dce4ec;border-radius:20px;padding:6px 10px;background:#fff;color:#566277;font-size:11px;font-weight:650}.env-dot{width:6px;height:6px;border-radius:50%;background:#9ba6b7}.avatar{width:33px;height:33px;border-radius:11px;display:grid;place-items:center;background:#ececff;color:#4c4dbe;font-weight:800;font-size:12px}.content{max-width:1500px;margin:0 auto;padding:32px 38px 48px}.page-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:25px}.eyebrow{font-size:10px;letter-spacing:1.7px;text-transform:uppercase;font-weight:800;color:var(--brand);margin-bottom:7px}.page-heading h1{font-size:27px;line-height:1.2;letter-spacing:-.9px;margin:0;font-weight:760}.page-heading p{margin:9px 0 0;color:var(--muted);font-size:13px}.btn{display:inline-flex;justify-content:center;align-items:center;gap:8px;border:1px solid transparent;border-radius:9px;padding:10px 15px;font-weight:700;font-size:12px;white-space:nowrap}.btn-primary{background:var(--brand);color:#fff;box-shadow:0 4px 10px #5b5ce225}.btn-primary:hover{background:var(--brand-dark);transform:translateY(-1px)}.btn-secondary{background:#fff;border-color:#dce2ec;color:#344054}.btn-secondary:hover{background:#f8f9fc;border-color:#bfc8d8}.btn-danger{background:#fff;border-color:#f0c9cd;color:var(--red);padding:7px 10px;font-size:11px}.btn-danger:hover{background:var(--red-bg)}.btn-small{padding:7px 10px;font-size:11px}.notice{display:flex;gap:10px;align-items:flex-start;margin:0 0 18px;padding:12px 15px;border-radius:10px;border:1px solid #cdebdc;background:#effaf4;color:#176846;font-size:12px}.notice.error{border-color:#f2cdd1;background:var(--red-bg);color:#a62e3c}.notice-icon{font-weight:900}.hero{position:relative;overflow:hidden;border-radius:17px;padding:25px 28px;margin-bottom:22px;background:linear-gradient(110deg,#202b4b 0%,#343c72 60%,#5554b8 100%);color:#fff;box-shadow:0 12px 26px #27345b18}.hero:after{content:"";position:absolute;right:8%;top:-115px;width:280px;height:280px;border:1px solid #ffffff20;border-radius:50%;box-shadow:0 0 0 32px #ffffff08,0 0 0 64px #ffffff06}.hero-inner{position:relative;z-index:1;max-width:700px}.hero-kicker{display:flex;align-items:center;gap:8px;color:#c4c8ff;font-size:10px;font-weight:800;letter-spacing:1.5px;text-transform:uppercase}.hero h2{font-size:22px;letter-spacing:-.55px;margin:9px 0 7px}.hero p{max-width:600px;color:#d5daf0;margin:0;font-size:12px;line-height:1.7}.hero-meta{display:flex;flex-wrap:wrap;gap:18px;margin-top:19px;color:#d6dcf5;font-size:11px}.hero-meta span{display:flex;align-items:center;gap:7px}.hero-meta b{color:#fff;font-weight:650}.metric-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:15px;margin-bottom:24px}.metric{background:var(--surface);border:1px solid var(--line);border-radius:13px;padding:17px 18px;box-shadow:var(--shadow);min-width:0}.metric-top{display:flex;align-items:center;justify-content:space-between;gap:10px;color:var(--muted);font-size:11px;font-weight:650}.metric-icon{display:grid;place-items:center;width:32px;height:32px;border-radius:9px;background:#f0f1ff;color:var(--brand);font-size:16px}.metric-value{font-size:27px;font-weight:760;letter-spacing:-1px;margin-top:10px;line-height:1.2}.metric-foot{font-size:10px;color:var(--muted);margin-top:6px}.metric-foot .positive{color:var(--green);font-weight:700}.section-card{background:#fff;border:1px solid var(--line);border-radius:13px;box-shadow:var(--shadow);margin-bottom:21px;overflow:hidden}.section-head{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:18px 20px;border-bottom:1px solid #edf0f5}.section-title{font-size:13px;font-weight:760;letter-spacing:-.2px}.section-subtitle{font-size:11px;color:var(--muted);margin-top:3px}.section-head-right{display:flex;align-items:center;gap:9px}.count-pill{display:inline-flex;align-items:center;justify-content:center;min-width:25px;height:23px;padding:0 7px;border-radius:7px;background:#f0f1f7;color:#626d80;font-size:10px;font-weight:750}.table-wrap{overflow-x:auto}table{border-collapse:collapse;width:100%;white-space:nowrap;font-size:11px}thead{background:#fafbfe}th{text-align:left;padding:11px 17px;color:#7b8496;font-size:9px;text-transform:uppercase;letter-spacing:.9px;font-weight:800;border-bottom:1px solid #edf0f5}td{padding:14px 17px;border-bottom:1px solid #f0f2f6;color:#465167}tbody tr:last-child td{border-bottom:0}tbody tr:hover{background:#fafbff}.primary-cell{color:var(--ink);font-weight:700}.sub-cell{font-size:10px;color:#8b94a6;margin-top:3px}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:10px;color:#68738a}.status{display:inline-flex;align-items:center;gap:6px;padding:5px 8px;border-radius:7px;font-size:10px;font-weight:750;text-transform:capitalize}.status:before{content:"";width:5px;height:5px;border-radius:50%;background:currentColor}.status-healthy,.status-running,.status-succeeded{background:var(--green-bg);color:var(--green)}.status-degraded,.status-pending,.status-dispatched,.status-unknown{background:var(--amber-bg);color:var(--amber)}.status-unhealthy,.status-failed,.status-stopped{background:#eef1f5;color:#687386}.status-defined{background:var(--brand-soft);color:var(--brand)}.status-offline,.status-error{background:var(--red-bg);color:var(--red)}.resource-cell{min-width:135px}.resource-label{display:flex;justify-content:space-between;gap:10px;font-size:10px;color:#667085;margin-bottom:7px}.resource-label b{color:#344054}.progress{height:5px;background:#edf0f6;border-radius:8px;overflow:hidden}.progress span{display:block;height:100%;border-radius:8px;background:linear-gradient(90deg,#7778ef,#5556d5)}.empty-state{padding:35px 20px;text-align:center}.empty-icon{margin:0 auto 12px;width:43px;height:43px;display:grid;place-items:center;border-radius:13px;background:#f0f1ff;color:var(--brand);font-size:20px}.empty-state h3{margin:0;font-size:13px}.empty-state p{margin:6px auto 15px;max-width:350px;font-size:11px;color:var(--muted);line-height:1.7}.empty-muted{color:#8791a4;font-size:11px;padding:20px!important;text-align:center}.action-row{display:flex;align-items:center;gap:7px}.action-form{display:inline}.dashboard-bottom{display:grid;grid-template-columns:minmax(0,1.5fr) minmax(250px,1fr);gap:18px}.info-card{padding:20px}.info-card h3{font-size:12px;margin:0 0 5px}.info-card>p{margin:0;color:var(--muted);font-size:11px;line-height:1.7}.architecture-flow{display:flex;align-items:center;gap:8px;margin-top:17px;flex-wrap:wrap}.flow-node{padding:8px 10px;background:#f7f8fc;border:1px solid #e6e9f2;border-radius:8px;font-size:10px;font-weight:700}.flow-arrow{color:#a0a8b8}.health-list{margin-top:15px;display:grid;gap:12px}.health-item{display:flex;align-items:center;justify-content:space-between;gap:10px;font-size:11px}.health-item span:first-child{color:#667085}.health-value{font-weight:750;color:#344054}.form-layout{display:grid;grid-template-columns:minmax(0,1.6fr) minmax(250px,1fr);gap:20px;align-items:start}.form-card{padding:23px}.form-card h2{font-size:15px;margin:0}.form-card .intro{font-size:11px;color:var(--muted);margin:6px 0 23px;line-height:1.7}.field-grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}.field{display:flex;flex-direction:column;gap:7px;margin-bottom:18px;min-width:0}.field label{font-size:11px;font-weight:750;color:#344054}.field-hint{font-size:10px;color:#8992a4;font-weight:400}.field input,.field select{width:100%;min-width:0;border:1px solid #dce2ec;background:#fff;border-radius:8px;padding:11px 12px;color:var(--ink);font-size:12px}.field input:hover,.field select:hover{border-color:#b7c0d1}.field input:focus,.field select:focus{outline:none;border-color:#8586ef;box-shadow:0 0 0 3px #5b5ce21a}.field-actions{display:flex;gap:10px;align-items:center;padding-top:5px}.side-panel{padding:19px}.side-panel h3{font-size:12px;margin:0 0 13px}.side-step{display:flex;gap:11px;margin:14px 0}.step-number{flex:none;width:25px;height:25px;display:grid;place-items:center;border-radius:8px;background:var(--brand-soft);color:var(--brand);font-size:10px;font-weight:800}.side-step strong{display:block;font-size:11px}.side-step p{font-size:10px;color:var(--muted);margin:3px 0 0;line-height:1.6}.callout{padding:12px;border:1px solid #e4e6fb;background:#f8f8ff;border-radius:9px;color:#5354a4;font-size:10px;line-height:1.7;margin-top:18px}.page-footer{display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;padding-top:14px;color:#8a93a5;font-size:10px}.login-page{min-height:100vh;display:grid;grid-template-columns:minmax(0,1.05fr) minmax(360px,.95fr);background:#fff}.login-art{position:relative;overflow:hidden;display:flex;flex-direction:column;justify-content:space-between;padding:44px 8%;color:#fff;background:radial-gradient(ellipse at 10% 10%,#424b81 0%,transparent 48%),linear-gradient(145deg,#11192e,#252f56 65%,#41428c)}.login-art:before{content:"";position:absolute;width:470px;height:470px;right:-140px;top:17%;border:1px solid #ffffff1c;border-radius:50%;box-shadow:0 0 0 45px #ffffff08,0 0 0 90px #ffffff06}.login-art>*{position:relative;z-index:1}.login-art .brand{padding:0}.login-art-copy{max-width:510px;padding:45px 0}.login-art-copy .eyebrow{color:#aeb2ff}.login-art-copy h1{font-size:clamp(30px,4vw,48px);line-height:1.1;letter-spacing:-1.7px;margin:14px 0}.login-art-copy p{max-width:430px;color:#c5cde5;font-size:13px;line-height:1.85}.login-feature{display:flex;align-items:center;gap:10px;margin-top:26px;color:#e2e7f7;font-size:11px}.feature-check{display:grid;place-items:center;width:23px;height:23px;border-radius:7px;background:#ffffff17;color:#a6f1d4}.login-foot{color:#8f9bbc;font-size:10px}.login-main{display:flex;align-items:center;justify-content:center;padding:35px}.login-box{width:100%;max-width:385px}.login-box .brand{display:none}.login-box h2{font-size:25px;letter-spacing:-.7px;margin:0}.login-box .login-sub{font-size:12px;color:var(--muted);margin:8px 0 28px;line-height:1.7}.login-box .field{margin-bottom:19px}.login-box .btn{width:100%;padding:12px;margin-top:5px}.login-security{display:flex;align-items:center;justify-content:center;gap:7px;color:#8b94a5;font-size:10px;margin-top:20px}.alert-error{margin:0 0 18px;padding:11px 12px;border-radius:8px;border:1px solid #f0c8cd;background:#fff1f2;color:#ac3140;font-size:11px}.muted{color:var(--muted)}@media(max-width:1150px){.sidebar{width:210px}.main-area{margin-left:210px}.content{padding:27px 24px 40px}.topbar{padding:0 24px}.metric-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.dashboard-bottom{grid-template-columns:1fr}}@media(max-width:760px){.sidebar{position:static;width:auto;padding:13px 16px;flex-direction:row;align-items:center;gap:12px}.brand{padding:0;flex:1}.brand-sub,.nav-label,.sidebar-note,.sidebar-bottom{display:none}.sidebar nav{display:flex;gap:4px}.nav-link{margin:0;padding:9px}.nav-link .nav-icon{width:auto}.nav-link .nav-text{display:none}.sidebar-spacer{display:none}.main-area{margin-left:0}.topbar{height:57px;padding:0 17px}.content{padding:23px 16px 32px}.page-heading{align-items:flex-start}.page-heading h1{font-size:23px}.page-heading p{max-width:420px}.hero{padding:21px}.hero h2{font-size:19px}.hero:after{right:-150px}.metric-grid{gap:10px}.metric{padding:14px}.metric-value{font-size:24px}.section-head{padding:15px}.section-head-right .btn{padding:8px 10px}.form-layout{grid-template-columns:1fr}.login-page{grid-template-columns:1fr}.login-art{display:none}.login-main{min-height:100vh;padding:28px 22px}.login-box .brand{display:flex;padding:0 0 35px}.field-grid{grid-template-columns:1fr 1fr}}@media(max-width:430px){.env-pill{display:none}.metric-grid{grid-template-columns:1fr 1fr}.metric-top{font-size:10px}.metric-icon{width:28px;height:28px}.field-grid{grid-template-columns:1fr}.page-heading{gap:10px}.page-heading>.btn{padding:9px 10px;font-size:11px}.hero-meta{gap:10px}.breadcrumbs{font-size:11px}}
</style>{{end}}`;
