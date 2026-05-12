package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	store   db.Store
	scanner *Scanner
}

func NewHandlers(store db.Store, scanner *Scanner) *Handlers {
	return &Handlers{store: store, scanner: scanner}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// URLs

func (h *Handlers) ListURLs(w http.ResponseWriter, r *http.Request) {
	urls, err := h.store.ListURLs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, urls)
}

func (h *Handlers) CreateURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	u, err := h.store.CreateURL(r.Context(), body.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (h *Handlers) DeleteURL(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteURL(r.Context(), uint(id)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DNS Servers

func (h *Handlers) ListDNSServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.store.ListDNSServers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, servers)
}

func (h *Handlers) CreateDNSServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Address  string `json:"address"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Address == "" {
		writeError(w, http.StatusBadRequest, "address is required")
		return
	}
	if body.Protocol == "" {
		body.Protocol = "udp"
	}
	s, err := h.store.CreateDNSServer(r.Context(), db.DNSServer{
		Name:     body.Name,
		Address:  body.Address,
		Protocol: body.Protocol,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

func (h *Handlers) DeleteDNSServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteDNSServer(r.Context(), uint(id)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Scan control

func (h *Handlers) TriggerScan(w http.ResponseWriter, r *http.Request) {
	if h.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "scanner not configured")
		return
	}
	if err := h.scanner.Trigger(r.Context(), "manual"); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "scan triggered"})
}

func (h *Handlers) ScanStatus(w http.ResponseWriter, r *http.Request) {
	run, err := h.store.ActiveScanRun(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// Results

func (h *Handlers) LatestResults(w http.ResponseWriter, r *http.Request) {
	results, err := h.store.LatestResults(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (h *Handlers) ResultsByURL(w http.ResponseWriter, r *http.Request) {
	urlValue := chi.URLParam(r, "*")
	results, err := h.store.ResultsByURL(r.Context(), urlValue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// Screenshot

func (h *Handlers) TriggerScreenshot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL         string `json:"url"`
		DNSServerID uint   `json:"dns_server_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		writeError(w, http.StatusBadRequest, "url and dns_server_id are required")
		return
	}
	if h.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "scanner not configured")
		return
	}
	if err := h.scanner.TriggerScreenshot(r.Context(), body.URL, body.DNSServerID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "screenshot requested"})
}
