package webui

import (
	"embed"
	"io"
	"net/http"
)

//go:embed ui.js ui.css
var uiAssets embed.FS

func (h *Handler) uiScript(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	file, err := uiAssets.Open("ui.js")
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	if _, err := io.Copy(w, file); err != nil {
		return
	}
}

func (h *Handler) uiStylesheet(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	file, err := uiAssets.Open("ui.css")
	if err != nil { http.Error(w, "stylesheet unavailable", http.StatusInternalServerError); return }
	defer file.Close()
	if _, err := io.Copy(w, file); err != nil { return }
}
