package workeragent

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type Handler struct {
	manager *Manager
	secret  string
}

func NewHandler(manager *Manager, sharedSecret string) *Handler {
	return &Handler{manager: manager, secret: sharedSecret}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/worker/vms", h.prepare)
	mux.HandleFunc("POST /api/v1/worker/vms/{vmID}/start", h.start)
	mux.HandleFunc("POST /api/v1/worker/vms/{vmID}/stop", h.stop)
	mux.HandleFunc("GET /api/v1/worker/vms/{vmID}", h.status)
}

type operationRequest struct {
	OperationID string `json:"operation_id"`
}

func (h *Handler) authorized(r *http.Request) bool {
	if h.secret == "" {
		return false
	}
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	if got == "" || len(got) != len(h.secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.secret)) == 1
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) bool {
	if h.manager == nil || !h.authorized(r) {
		writeWorkerJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}
	return true
}

func (h *Handler) prepare(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(w, r) { return }
	var req CreateRequest
	if !decodeWorkerJSON(w, r, &req) { return }
	report, err := h.manager.Prepare(r.Context(), req)
	if err != nil {
		writeWorkerError(w, err)
		return
	}
	writeWorkerJSON(w, http.StatusOK, report)
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(w, r) { return }
	var req operationRequest
	if !decodeWorkerJSON(w, r, &req) { return }
	report, err := h.manager.Start(r.Context(), req.OperationID, r.PathValue("vmID"))
	if err != nil {
		writeWorkerError(w, err)
		return
	}
	writeWorkerJSON(w, http.StatusOK, report)
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(w, r) { return }
	var req operationRequest
	if !decodeWorkerJSON(w, r, &req) { return }
	report, err := h.manager.Stop(r.Context(), req.OperationID, r.PathValue("vmID"))
	if err != nil {
		writeWorkerError(w, err)
		return
	}
	writeWorkerJSON(w, http.StatusOK, report)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(w, r) { return }
	report, err := h.manager.Status(r.Context(), r.PathValue("vmID"))
	if err != nil {
		writeWorkerError(w, err)
		return
	}
	writeWorkerJSON(w, http.StatusOK, report)
}

func decodeWorkerJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		writeWorkerJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request"})
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeWorkerJSON(w, http.StatusBadRequest, map[string]string{"error": "request must contain exactly one JSON value"})
		return false
	}
	return true
}

func writeWorkerJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeWorkerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeWorkerJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_request", "message": err.Error()})
	case errors.Is(err, ErrNotFound):
		writeWorkerJSON(w, http.StatusNotFound, map[string]string{"error": "not_found", "message": "VM does not exist on this worker"})
	case errors.Is(err, ErrConflict):
		writeWorkerJSON(w, http.StatusConflict, map[string]string{"error": "state_conflict", "message": err.Error()})
	default:
		writeWorkerJSON(w, http.StatusBadGateway, map[string]string{"error": "worker_operation_failed", "message": "worker could not confirm the requested VM operation"})
	}
}
