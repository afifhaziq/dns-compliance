package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
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
	urls, err := store.ListWatchedURLs(ctx)
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

// URLs (department watchlist scope — admin sees the global pool, everyone
// else sees only their own department's watchlist)

func (h *Handlers) ListURLs(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var urls []db.URL
	var err error
	switch {
	case user.IsAdmin:
		urls, err = h.store.ListURLs(r.Context())
	case user.DepartmentID != nil:
		urls, err = h.store.ListDepartmentURLs(r.Context(), *user.DepartmentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, urls)
}

// AddToWatchlist gets-or-creates the URL by normalized value and links it to
// a department's watchlist. Non-admins are scoped to their own department;
// admins have no department of their own, so they must specify one.
func (h *Handlers) AddToWatchlist(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var body struct {
		URL          string `json:"url"`
		DepartmentID *uint  `json:"department_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	var departmentID uint
	if user.IsAdmin {
		if body.DepartmentID == nil {
			writeError(w, http.StatusBadRequest, "department_id is required")
			return
		}
		departmentID = *body.DepartmentID
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		departmentID = *user.DepartmentID
	}

	u, err := h.store.AddURLToWatchlist(r.Context(), departmentID, body.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

// RemoveFromWatchlist unlinks a URL from a department's watchlist only —
// the URL row and its scan history are untouched, so other departments
// watching the same domain (or anyone re-adding it later) keep full
// history. 404s if the URL wasn't actually on that department's watchlist.
func (h *Handlers) RemoveFromWatchlist(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var departmentID uint
	if user.IsAdmin {
		raw := r.URL.Query().Get("department_id")
		parsed, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "department_id query param is required")
			return
		}
		departmentID = uint(parsed)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		departmentID = *user.DepartmentID
	}

	removed, err := h.store.RemoveURLFromWatchlist(r.Context(), departmentID, uint(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "url not on this department's watchlist")
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
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var results []db.ScanResult
	var err error
	switch {
	case user.IsAdmin:
		results, err = h.store.LatestResults(r.Context())
	case user.DepartmentID != nil:
		results, err = h.store.LatestResultsForDepartment(r.Context(), *user.DepartmentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// requireDomainOwnership 404s non-admins who request a domain not on their
// own department's watchlist — a 404 rather than 403 so the response
// doesn't confirm the domain even exists to a department that shouldn't
// know about it.
func requireDomainOwnership(h *Handlers, w http.ResponseWriter, r *http.Request, urlValue string) (ok bool) {
	user, authed := userFromContext(r.Context())
	if !authed {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return false
	}
	if user.IsAdmin {
		return true
	}
	if user.DepartmentID == nil {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	owned, err := h.store.URLOwnedByDepartment(r.Context(), *user.DepartmentID, urlValue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !owned {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	return true
}

func (h *Handlers) ResultsByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
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
	if !requireDomainOwnership(h, w, r, urlValue) {
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

// DNS Records (live lookup, independent of the compliance-scan pipeline)

type dnsRecordSet struct {
	A     []string `json:"a"`
	AAAA  []string `json:"aaaa"`
	CNAME []string `json:"cname"`
	MX    []string `json:"mx"`
	TXT   []string `json:"txt"`
	NS    []string `json:"ns"`
}

type dnsRecordsResponse struct {
	Hostname   string        `json:"hostname"`
	Resolved   bool          `json:"resolved"`
	ResolverIP string        `json:"resolver_ip,omitempty"`
	Records    *dnsRecordSet `json:"records,omitempty"`
}

// resolverAddrCapture wraps net.Resolver's Dial hook to record which
// nameserver address actually answered a lookup — Go's net.Resolver itself
// doesn't expose this, so the only way to learn it is to intercept the dial.
type resolverAddrCapture struct {
	mu   sync.Mutex
	addr string
}

func (c *resolverAddrCapture) dial(ctx context.Context, network, address string) (net.Conn, error) {
	c.mu.Lock()
	c.addr = address
	c.mu.Unlock()
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

// host returns the captured address with its port stripped, e.g.
// "127.0.0.11:53" -> "127.0.0.11". Empty if no dial was observed.
func (c *resolverAddrCapture) host() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(c.addr); err == nil {
		return host
	}
	return c.addr
}

func (h *Handlers) DNSRecordsByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}

	hostname := urlValue
	if parsed, parseErr := url.Parse(urlValue); parseErr == nil && parsed.Hostname() != "" {
		hostname = parsed.Hostname()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	capture := &resolverAddrCapture{}
	resolver := &net.Resolver{PreferGo: true, Dial: capture.dial}

	addrs, err := resolver.LookupHost(ctx, hostname)
	if err != nil {
		writeJSON(w, http.StatusOK, dnsRecordsResponse{Hostname: hostname, Resolved: false, ResolverIP: capture.host()})
		return
	}

	records := lookupDNSRecordSet(ctx, resolver, hostname, addrs)
	writeJSON(w, http.StatusOK, dnsRecordsResponse{
		Hostname:   hostname,
		Resolved:   true,
		ResolverIP: capture.host(),
		Records:    &records,
	})
}

func lookupDNSRecordSet(ctx context.Context, resolver *net.Resolver, hostname string, addrs []string) dnsRecordSet {
	set := dnsRecordSet{A: aFromAddrs(addrs), AAAA: aaaaFromAddrs(addrs)}

	var wg sync.WaitGroup
	wg.Add(4)

	go func() { defer wg.Done(); set.CNAME = lookupCNAME(ctx, resolver, hostname) }()
	go func() { defer wg.Done(); set.MX = lookupMX(ctx, resolver, hostname) }()
	go func() { defer wg.Done(); set.TXT = lookupTXT(ctx, resolver, hostname) }()
	go func() { defer wg.Done(); set.NS = lookupNS(ctx, resolver, hostname) }()

	wg.Wait()
	return set
}

func aFromAddrs(addrs []string) []string {
	out := make([]string, 0)
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
			out = append(out, a)
		}
	}
	return out
}

func aaaaFromAddrs(addrs []string) []string {
	out := make([]string, 0)
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() == nil {
			out = append(out, a)
		}
	}
	return out
}

func lookupCNAME(ctx context.Context, resolver *net.Resolver, hostname string) []string {
	cname, err := resolver.LookupCNAME(ctx, hostname)
	if err != nil {
		return []string{}
	}
	trimmed := strings.TrimSuffix(cname, ".")
	if trimmed == "" || strings.EqualFold(trimmed, strings.TrimSuffix(hostname, ".")) {
		return []string{}
	}
	return []string{trimmed}
}

func lookupMX(ctx context.Context, resolver *net.Resolver, hostname string) []string {
	records, err := resolver.LookupMX(ctx, hostname)
	out := make([]string, 0, len(records))
	if err != nil {
		return out
	}
	for _, mx := range records {
		out = append(out, fmt.Sprintf("%d %s", mx.Pref, strings.TrimSuffix(mx.Host, ".")))
	}
	return out
}

func lookupTXT(ctx context.Context, resolver *net.Resolver, hostname string) []string {
	records, err := resolver.LookupTXT(ctx, hostname)
	if err != nil {
		return []string{}
	}
	return records
}

func lookupNS(ctx context.Context, resolver *net.Resolver, hostname string) []string {
	records, err := resolver.LookupNS(ctx, hostname)
	out := make([]string, 0, len(records))
	if err != nil {
		return out
	}
	for _, ns := range records {
		out = append(out, strings.TrimSuffix(ns.Host, "."))
	}
	return out
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
	urls, err := h.store.ListWatchedURLs(r.Context())
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
