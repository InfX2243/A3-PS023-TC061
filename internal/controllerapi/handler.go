// Package controllerapi exposes the versioned operator API. Business rules and
// persistence are injected through Service so HTTP contracts can be tested alone.
package controllerapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
)

var (
	ErrNotFound   = errors.New("resource not found")
	ErrConflict   = errors.New("request conflicts with current state")
	ErrUnavailable = errors.New("required service unavailable")
	ErrInvalid    = errors.New("invalid request")
	requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)
)

type Limits struct {
	MaxVCPUs      int
	MaxMemoryMiB  uint64
	AllowedImages map[string]struct{}
}

type CreateVMRequest struct {
	Name      string `json:"name"`
	VCPUs     int    `json:"vcpus"`
	MemoryMiB uint64 `json:"memory_mib"`
	ImageID   string `json:"image_id"`
}

type Service interface {
	ListWorkers(context.Context) ([]domain.Worker, error)
	ListVMs(context.Context) ([]domain.VM, error)
	GetVM(context.Context, string) (domain.VM, error)
	CreateVM(context.Context, CreateVMRequest, string) (domain.VM, domain.Operation, error)
	StartVM(context.Context, string) (domain.VM, domain.Operation, error)
	StopVM(context.Context, string) (domain.VM, domain.Operation, error)
	GetOperation(context.Context, string) (domain.Operation, error)
}

type Handler struct {
	service Service
	token   string
	limits  Limits
}

func NewHandler(service Service, operatorToken string, limits Limits) *Handler {
	return &Handler{service: service, token: operatorToken, limits: limits}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workers", h.listWorkers)
	mux.HandleFunc("GET /api/v1/vms", h.listVMs)
	mux.HandleFunc("POST /api/v1/vms", h.createVM)
	mux.HandleFunc("GET /api/v1/vms/{vmID}", h.getVM)
	mux.HandleFunc("POST /api/v1/vms/{vmID}/start", h.startVM)
	mux.HandleFunc("POST /api/v1/vms/{vmID}/stop", h.stopVM)
	mux.HandleFunc("GET /api/v1/operations/{operationID}", h.getOperation)
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	requestID := r.Header.Get("X-Request-ID")
	if !requestIDPattern.MatchString(requestID) {
		requestID = newRequestID()
	}
	w.Header().Set("X-Request-ID", requestID)
	if h.token == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]string{"code": "auth_not_configured", "message": "operator authentication is not configured"}, "request_id": requestID})
		return false
	}
	value := r.Header.Get("Authorization")
	got := ""
	if strings.HasPrefix(value, "Bearer ") {
		got = strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	}
	if len(got) != len(h.token) || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "valid bearer authentication is required"}, "request_id": requestID})
		return false
	}
	return true
}

func (h *Handler) ready(w http.ResponseWriter) bool {
	if h.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorPayload("service_unavailable", "controller service is not initialized", ""))
		return false
	}
	return true
}

func (h *Handler) listWorkers(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) || !h.ready(w) { return }
	items, err := h.service.ListWorkers(r.Context())
	if err != nil { writeServiceError(w, err); return }
	writeJSON(w, http.StatusOK, map[string]any{"workers": items})
}

func (h *Handler) listVMs(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) || !h.ready(w) { return }
	items, err := h.service.ListVMs(r.Context())
	if err != nil { writeServiceError(w, err); return }
	writeJSON(w, http.StatusOK, map[string]any{"vms": items})
}

func (h *Handler) createVM(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) || !h.ready(w) { return }
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !requestIDPattern.MatchString(key) {
		writeJSON(w, http.StatusBadRequest, errorPayload("idempotency_key_required", "Idempotency-Key must be a 1-128 character identifier", requestIDFrom(w)))
		return
	}
	var req CreateVMRequest
	if !decodeJSON(w, r, &req) { return }
	if strings.TrimSpace(req.Name) == "" || len(req.Name) > 128 {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid_name", "name must be between 1 and 128 characters", requestIDFrom(w)))
		return
	}
	if req.VCPUs < 1 || req.VCPUs > h.limits.MaxVCPUs {
		writeJSON(w, http.StatusUnprocessableEntity, errorPayload("invalid_vcpus", "vcpus is outside the configured limit", requestIDFrom(w)))
		return
	}
	if req.MemoryMiB < 1 || req.MemoryMiB > h.limits.MaxMemoryMiB {
		writeJSON(w, http.StatusUnprocessableEntity, errorPayload("invalid_memory", "memory_mib is outside the configured limit", requestIDFrom(w)))
		return
	}
	if _, ok := h.limits.AllowedImages[req.ImageID]; !ok {
		writeJSON(w, http.StatusUnprocessableEntity, errorPayload("image_not_allowed", "image_id is not in the server-side allowlist", requestIDFrom(w)))
		return
	}
	vm, op, err := h.service.CreateVM(r.Context(), req, key)
	if err != nil { writeServiceError(w, err); return }
	writeJSON(w, http.StatusAccepted, map[string]any{"vm": vm, "operation": op})
}

func (h *Handler) getVM(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) || !h.ready(w) { return }
	vm, err := h.service.GetVM(r.Context(), r.PathValue("vmID"))
	if err != nil { writeServiceError(w, err); return }
	writeJSON(w, http.StatusOK, map[string]any{"vm": vm})
}

func (h *Handler) startVM(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) || !h.ready(w) { return }
	if !emptyBody(w, r) { return }
	vm, op, err := h.service.StartVM(r.Context(), r.PathValue("vmID"))
	if err != nil { writeServiceError(w, err); return }
	writeJSON(w, http.StatusAccepted, map[string]any{"vm": vm, "operation": op})
}

func (h *Handler) stopVM(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) || !h.ready(w) { return }
	if !emptyBody(w, r) { return }
	vm, op, err := h.service.StopVM(r.Context(), r.PathValue("vmID"))
	if err != nil { writeServiceError(w, err); return }
	writeJSON(w, http.StatusAccepted, map[string]any{"vm": vm, "operation": op})
}

func (h *Handler) getOperation(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) || !h.ready(w) { return }
	op, err := h.service.GetOperation(r.Context(), r.PathValue("operationID"))
	if err != nil { writeServiceError(w, err); return }
	writeJSON(w, http.StatusOK, map[string]any{"operation": op})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid_json", "request body must be one valid JSON object", requestIDFrom(w)))
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid_json", "request body must contain exactly one JSON value", requestIDFrom(w)))
		return false
	}
	return true
}

func emptyBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil || r.ContentLength == 0 { return true }
	var extra [1]byte
	n, err := r.Body.Read(extra[:])
	if n != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		writeJSON(w, http.StatusBadRequest, errorPayload("unexpected_body", "start/stop requests do not accept a body", requestIDFrom(w)))
		return false
	}
	return true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeJSON(w, http.StatusUnprocessableEntity, errorPayload("invalid_request", err.Error(), requestIDFrom(w)))
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorPayload("not_found", "requested resource was not found", requestIDFrom(w)))
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorPayload("state_conflict", err.Error(), requestIDFrom(w)))
	case errors.Is(err, ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, errorPayload("service_unavailable", "required worker or capacity is unavailable", requestIDFrom(w)))
	default:
		writeJSON(w, http.StatusInternalServerError, errorPayload("internal_error", "request could not be completed", requestIDFrom(w)))
	}
}

func errorPayload(code, message, requestID string) map[string]any {
	return map[string]any{"error": map[string]string{"code": code, "message": message}, "request_id": requestID}
}

func requestIDFrom(w http.ResponseWriter) string { return w.Header().Get("X-Request-ID") }

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil { return fmt.Sprintf("req-%d", len(b)) }
	return hex.EncodeToString(b[:])
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
