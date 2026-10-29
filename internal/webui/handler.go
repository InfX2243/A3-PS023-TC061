package webui

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
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
type dashboardData struct { Workers []domain.Worker; VMs []vmRow; Error string; Notice string }
type formData struct { Limits Limits; Error string; IdempotencyKey string }

func NewHandler(service Service, operatorToken string, limits Limits, secureCookie bool) *Handler {
	return &Handler{service: service, token: operatorToken, limits: limits, secureCookie: secureCookie, templates: template.Must(template.New("pages").Parse(pageTemplates))}
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
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	if h.service == nil { h.renderStatus(w, http.StatusServiceUnavailable, "dashboard", dashboardData{Error:"Controller service is not initialized."}); return }
	workers, err := h.service.ListWorkers(r.Context())
	if err != nil { h.renderStatus(w, http.StatusInternalServerError, "dashboard", dashboardData{Error:"Could not load worker inventory."}); return }
	vms, err := h.service.ListVMs(r.Context())
	if err != nil { h.renderStatus(w, http.StatusInternalServerError, "dashboard", dashboardData{Workers:workers, Error:"Could not load VM inventory."}); return }
	rows := make([]vmRow, 0, len(vms))
	for _, vm := range vms {
		op, err := h.service.LatestOperation(r.Context(), vm.ID)
		rows = append(rows, vmRow{VM:vm, Operation:op, HasOperation:err == nil})
	}
	h.render(w, "dashboard", dashboardData{Workers:workers, VMs:rows, Notice:r.URL.Query().Get("notice"), Error:r.URL.Query().Get("error")})
}

func (h *Handler) newVMPage(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	h.render(w, "new-vm", formData{Limits:h.limits, IdempotencyKey:newFormID()})
}

func (h *Handler) createVM(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	if !h.authenticated(r) { http.Redirect(w, r, "/login", http.StatusSeeOther); return }
	if err := r.ParseForm(); err != nil { h.renderStatus(w, http.StatusBadRequest, "new-vm", formData{Limits:h.limits, Error:"Invalid form submission."}); return }
	name := strings.TrimSpace(r.FormValue("name"))
	vcpus, cpuErr := strconv.Atoi(r.FormValue("vcpus"))
	memory, memErr := strconv.ParseUint(r.FormValue("memory_mib"), 10, 64)
	imageID := r.FormValue("image_id")
	if name == "" || len(name) > 128 || cpuErr != nil || vcpus < 1 || vcpus > h.limits.MaxVCPUs || memErr != nil || memory < 1 || memory > h.limits.MaxMemoryMiB || !h.allowedImage(imageID) {
		h.renderStatus(w, http.StatusUnprocessableEntity, "new-vm", formData{Limits:h.limits, Error:"Check the VM name, vCPU, memory, and approved image values.", IdempotencyKey:r.FormValue("idempotency_key")})
		return
	}
	key := r.FormValue("idempotency_key")
	if !safeFormID(key) { h.renderStatus(w, http.StatusBadRequest, "new-vm", formData{Limits:h.limits, Error:"The form's idempotency key is invalid. Reload the form and retry."}); return }
	_, op, err := h.service.CreateVM(r.Context(), controllerapi.CreateVMRequest{Name:name, VCPUs:vcpus, MemoryMiB:memory, ImageID:imageID}, key)
	if err != nil {
		h.renderStatus(w, http.StatusServiceUnavailable, "new-vm", formData{Limits:h.limits, Error:err.Error(), IdempotencyKey:key})
		return
	}
	http.Redirect(w, r, "/?notice="+template.URLQueryEscaper("VM request recorded; operation "+op.State.String()), http.StatusSeeOther)
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
	var op domain.Operation
	var err error
	if start { _, op, err = h.service.StartVM(r.Context(), r.PathValue("vmID")) } else { _, op, err = h.service.StopVM(r.Context(), r.PathValue("vmID")) }
	if err != nil {
		http.Redirect(w, r, "/?error="+template.URLQueryEscaper("Operation failed: "+err.Error()), http.StatusSeeOther)
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

var _ = errors.New

const pageTemplates = `
{{define "login"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>VM Controller — Sign in</title></head><body><main><h1>VM Controller</h1><h2>Sign in</h2>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}<form method="post" action="/login"><label>Operator token <input type="password" name="token" autocomplete="current-password" required></label><button type="submit">Sign in</button></form></main></body></html>{{end}}
{{define "dashboard"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>VM Controller</title></head><body><header><h1>VM Controller</h1><form method="post" action="/logout"><button type="submit">Sign out</button></form></header><main>{{if .Notice}}<p role="status">{{.Notice}}</p>{{end}}{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}<h2>Workers</h2><table><thead><tr><th>ID</th><th>Name</th><th>Status</th><th>Heartbeat</th><th>Available memory (MiB)</th><th>Cap (MiB)</th></tr></thead><tbody>{{range .Workers}}<tr><td>{{.ID}}</td><td>{{.Name}}</td><td>{{.State}}</td><td>{{if .LastHeartbeat.IsZero}}Never{{else}}{{.LastHeartbeat}}{{end}}</td><td>{{divMiB .AvailableMemoryBytes}}</td><td>{{divMiB .MemoryLimitBytes}}</td></tr>{{else}}<tr><td colspan="6">No workers configured.</td></tr>{{end}}</tbody></table><h2>Virtual machines</h2><p><a href="/vms/new">Prepare a VM</a></p><table><thead><tr><th>ID</th><th>Name</th><th>Worker</th><th>vCPU</th><th>Memory (MiB)</th><th>Image</th><th>State</th><th>Latest operation</th><th>Actions</th></tr></thead><tbody>{{range .VMs}}<tr><td>{{.VM.ID}}</td><td>{{.VM.Name}}</td><td>{{.VM.WorkerID}}</td><td>{{.VM.VCPUs}}</td><td>{{divMiB .VM.MemoryBytes}}</td><td>{{.VM.ImageID}}</td><td>{{.VM.Observed}}</td><td>{{if .HasOperation}}{{.Operation.Type}}: {{.Operation.State}}{{else}}—{{end}}</td><td>{{if or (eq .VM.Observed "stopped") (eq .VM.Observed "defined")}}<form method="post" action="/vms/{{.VM.ID}}/start"><button type="submit">Start</button></form>{{end}}{{if eq .VM.Observed "running"}}<form method="post" action="/vms/{{.VM.ID}}/stop"><button type="submit">Stop</button></form>{{end}}</td></tr>{{else}}<tr><td colspan="9">No VMs yet.</td></tr>{{end}}</tbody></table></main></body></html>{{end}}
{{define "new-vm"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Prepare VM</title></head><body><main><h1>Prepare a VM</h1>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}<form method="post" action="/vms"><input type="hidden" name="idempotency_key" value="{{.IdempotencyKey}}"><label>Name <input name="name" maxlength="128" required></label><label>vCPUs <input type="number" name="vcpus" min="1" max="{{.Limits.MaxVCPUs}}" value="1" required></label><label>Memory (MiB) <input type="number" name="memory_mib" min="1" max="{{.Limits.MaxMemoryMiB}}" value="1024" required></label><label>Approved image <select name="image_id" required>{{range .Limits.AllowedImages}}<option value="{{.}}">{{.}}</option>{{end}}</select></label><button type="submit">Prepare VM</button></form><p><a href="/">Back to dashboard</a></p></main></body></html>{{end}}
`;
