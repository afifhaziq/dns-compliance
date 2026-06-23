package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	store       db.Store
	scanner     *Scanner
	broadcaster *Broadcaster
}

func NewHandlers(store db.Store, scanner *Scanner, broadcaster *Broadcaster) *Handlers {
	return &Handlers{store: store, scanner: scanner, broadcaster: broadcaster}
}

func buildProgressPayload(ctx context.Context, store db.Store) ([]byte, error) {
	run, err := store.LastScanRun(ctx)
	if err != nil || run == nil {
		return nil, err
	}
	progress, err := store.ScanProgress(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	urls, err := store.ListURLs(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(scanProgressResponse{
		ScanRun:   run,
		TotalURLs: len(urls),
		PerDNS:    progress,
	})
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
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}

	since := time.Now().AddDate(0, 0, -7)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			since = parsed
		}
	}

	var until time.Time
	if raw := r.URL.Query().Get("until"); raw != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			until = parsed
		}
	}

	results, err := h.store.ResultsByURL(r.Context(), urlValue, since, until)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (h *Handlers) HeatmapByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}

	since := time.Now().AddDate(0, 0, -7)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			since = parsed
		}
	}

	var until time.Time
	if raw := r.URL.Query().Get("until"); raw != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			until = parsed
		}
	}

	stats, err := h.store.DailyComplianceByURL(r.Context(), urlValue, since, until)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// Scan progress

type scanProgressResponse struct {
	ScanRun   *db.ScanRun        `json:"scan_run"`
	TotalURLs int                `json:"total_urls"`
	PerDNS    []db.ProgressEntry `json:"per_dns"`
}

func (h *Handlers) ScanProgress(w http.ResponseWriter, r *http.Request) {
	run, err := h.store.LastScanRun(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "no scan run found")
		return
	}
	progress, err := h.store.ScanProgress(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	urls, err := h.store.ListURLs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scanProgressResponse{
		ScanRun:   run,
		TotalURLs: len(urls),
		PerDNS:    progress,
	})
}

func (h *Handlers) ScanProgressStream(w http.ResponseWriter, r *http.Request) {
	if h.broadcaster == nil {
		http.Error(w, "streaming not configured", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send current state immediately so the client doesn't wait for the first event.
	if data, err := buildProgressPayload(r.Context(), h.store); err == nil && data != nil {
		fmt.Fprintf(w, "data: %s\n\n", data) //nolint:errcheck
		flusher.Flush()
	}

	ch := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data) //nolint:errcheck
			flusher.Flush()
		}
	}
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
