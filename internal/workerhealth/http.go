package workerhealth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

type HeartbeatRecorder interface { RecordWorkerHeartbeat(context.Context, Heartbeat) error }

type Handler struct {
	registry *Registry
	workerSecret string
	operatorToken string
	recorder HeartbeatRecorder
}

func NewHandler(registry *Registry, workerSecret, operatorToken string) *Handler {
	return &Handler{registry: registry, workerSecret: workerSecret, operatorToken: operatorToken}
}

func NewHandlerWithRecorder(registry *Registry, workerSecret, operatorToken string, recorder HeartbeatRecorder) *Handler {
	return &Handler{registry: registry, workerSecret: workerSecret, operatorToken: operatorToken, recorder: recorder}
}

func (h *Handler) Register(mux *http.ServeMux) {
	h.RegisterHeartbeatOnly(mux)
	mux.HandleFunc("GET /api/v1/workers", h.list)
}

func (h *Handler) RegisterHeartbeatOnly(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/worker/heartbeat", h.heartbeat)
}

func bearerToken(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}

func authorized(r *http.Request, expected string) bool {
	if expected == "" {
		return false
	}
	got := bearerToken(r)
	if got == "" || len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) heartbeat(w http.ResponseWriter, r *http.Request) {
	if !authorized(r, h.workerSecret) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var hb Heartbeat
	if err := dec.Decode(&hb); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid heartbeat payload"})
		return
	}
	report, err := h.registry.Heartbeat(hb)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if h.recorder != nil {
		if err := h.recorder.RecordWorkerHeartbeat(r.Context(), hb); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "worker heartbeat could not be persisted"})
			return
		}
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !authorized(r, h.operatorToken) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workers": h.registry.List()})
}
