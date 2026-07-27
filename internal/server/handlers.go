package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/favicon"
	"github.com/afif/dns-tracking/internal/ipinfo"
	"github.com/afif/dns-tracking/internal/subfinder"
	"github.com/afif/dns-tracking/internal/whois"
	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	store          db.Store
	scanner        *Scanner
	broadcaster    *Broadcaster
	whoisFetch     whois.Fetcher     // nil disables the lazy on-add fetch (e.g. in tests)
	faviconFetch   favicon.Fetcher   // nil disables on-demand favicon fetching (e.g. in tests)
	subfinderFetch subfinder.Fetcher // nil disables the lazy on-add + refresh subdomain enumeration (e.g. in tests)
	ipFetch        ipinfo.Fetcher    // nil disables the on-demand hosting-info refresh (e.g. in tests)
	netnameFetch   whois.IPFetcher   // nil disables the NetName/abuse-email half of a hosting-info refresh
}

func NewHandlers(store db.Store, scanner *Scanner, broadcaster *Broadcaster, whoisFetch whois.Fetcher, faviconFetch favicon.Fetcher, subfinderFetch subfinder.Fetcher, ipFetch ipinfo.Fetcher, netnameFetch whois.IPFetcher) *Handlers {
	return &Handlers{store: store, scanner: scanner, broadcaster: broadcaster, whoisFetch: whoisFetch, faviconFetch: faviconFetch, subfinderFetch: subfinderFetch, ipFetch: ipFetch, netnameFetch: netnameFetch}
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

// writeInternalError logs the real error server-side and returns a generic
// 500 to the client — raw DB/driver errors (constraint names, SQLSTATE
// codes) must never reach the response body.
func writeInternalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// URLs (department watchlist scope — every user including admin is scoped to
// their own department's watchlist; admin's department is "Admin")

func (h *Handlers) ListURLs(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if user.DepartmentID == nil {
		writeError(w, http.StatusInternalServerError, "user has no department")
		return
	}
	urls, err := h.store.ListDepartmentURLs(r.Context(), *user.DepartmentID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, urls)
}

// AddToWatchlist gets-or-creates the URL by normalized value and links it to
// the caller's department watchlist (admin's own "Admin" department included).
func (h *Handlers) AddToWatchlist(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if user.DepartmentID == nil {
		writeError(w, http.StatusForbidden, "user has no department")
		return
	}

	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if isPrivateHost(body.URL) {
		writeError(w, http.StatusBadRequest, "url resolves to a private or reserved address")
		return
	}

	u, err := h.store.AddURLToWatchlist(r.Context(), *user.DepartmentID, body.URL)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	// Lazy WHOIS fetch — detached from the request, own timeout context, so
	// a slow/unreachable RDAP server never delays or fails the add.
	if h.whoisFetch != nil {
		go fetchAndStoreWhois(h.store, h.whoisFetch, u.ID, u.URL)
	}

	// Lazy subdomain enumeration — same detached-goroutine shape as WHOIS
	// above; subfinder can take much longer than an RDAP lookup, so it gets
	// its own (longer) timeout in fetchAndStoreSubdomains.
	if h.subfinderFetch != nil {
		go fetchAndStoreSubdomains(h.store, h.subfinderFetch, u.ID, u.URL)
	}

	writeJSON(w, http.StatusCreated, db.URLEntry{ID: u.ID, URL: u.URL, Enabled: true, CreatedAt: u.CreatedAt})
}

// fetchAndStoreWhois runs a RDAP lookup and caches the result (or the
// failure) in DomainWhois. Called from a detached goroutine on watchlist-add
// and, with a paced caller loop, from the periodic refresher.
func fetchAndStoreWhois(store db.EnrichmentStore, fetch whois.Fetcher, urlID uint, domain string) {
	fetchCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	res, err := fetch(fetchCtx, domain)
	cancel()

	w := db.DomainWhois{URLID: urlID, LastFetchedAt: time.Now()}
	if err != nil {
		w.FetchError = err.Error()
	} else {
		w.Registrar, w.DomainCreated, w.DomainExpires = res.Registrar, res.DomainCreated, res.DomainExpires
		w.RegistrarURL, w.RegistrarAbuseEmail, w.RegistrarAbusePhone = res.RegistrarURL, res.RegistrarAbuseEmail, res.RegistrarAbusePhone
	}

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dbCancel()
	if err := store.UpsertDomainWhois(dbCtx, w); err != nil {
		log.Printf("whois: upsert for %s: %v", domain, err)
	}
}

// fetchAndStoreSubdomains runs subfinder and caches the result (or the
// failure) in SubdomainScan. Called from a detached goroutine on
// watchlist-add and from RefreshSubdomains — there is no periodic sweep, so
// this is the only place a cached row ever gets (re)written.
func fetchAndStoreSubdomains(store db.EnrichmentStore, fetch subfinder.Fetcher, urlID uint, domain string) {
	fetchCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	subs, err := fetch(fetchCtx, domain)
	cancel()

	scan := db.SubdomainScan{URLID: urlID, FetchedAt: time.Now()}
	if err != nil {
		scan.FetchError = err.Error()
	} else {
		scan.Subdomains = subs
	}

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dbCancel()
	if err := store.UpsertSubdomainScan(dbCtx, scan); err != nil {
		log.Printf("subfinder: upsert for %s: %v", domain, err)
	}
}

// RemoveFromWatchlist unlinks a URL from the caller's department watchlist
// only — URL row and its scan history are untouched. 404s if the URL wasn't
// actually on that department's watchlist.
func (h *Handlers) RemoveFromWatchlist(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if user.DepartmentID == nil {
		writeError(w, http.StatusForbidden, "user has no department")
		return
	}

	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	removed, err := h.store.RemoveURLFromWatchlist(r.Context(), *user.DepartmentID, uint(id))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "url not on this department's watchlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ToggleURL updates a URL in the caller's department watchlist: the enabled
// flag and/or the optional order date. Does not affect other departments
// watching the same domain. Only fields present in the body are touched —
// omit "enabled" to change only "ordered_at" and vice versa.
func (h *Handlers) ToggleURL(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if user.DepartmentID == nil {
		writeError(w, http.StatusForbidden, "user has no department")
		return
	}

	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var body struct {
		Enabled *bool `json:"enabled"`
		// OrderedAt is RFC3339 when setting a date, or "" to clear it.
		// Omit the key entirely to leave the order date untouched.
		OrderedAt *string `json:"ordered_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	found := false
	if body.Enabled != nil {
		f, err := h.store.SetURLEnabled(r.Context(), *user.DepartmentID, uint(id), *body.Enabled)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		found = found || f
	}
	if body.OrderedAt != nil {
		var orderedAt *time.Time
		if *body.OrderedAt != "" {
			t, err := time.Parse(time.RFC3339, *body.OrderedAt)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid ordered_at, expected RFC3339")
				return
			}
			orderedAt = &t
		}
		f, err := h.store.SetURLOrderedAt(r.Context(), *user.DepartmentID, uint(id), orderedAt)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		found = found || f
	}
	if !found {
		writeError(w, http.StatusNotFound, "url not on this department's watchlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DNS Servers

func (h *Handlers) ListDNSServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.store.ListDNSServers(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, servers)
}

func (h *Handlers) CreateDNSServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ISP      string `json:"isp"`
		Name     string `json:"name"`
		Address  string `json:"address"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.ISP == "" {
		writeError(w, http.StatusBadRequest, "isp is required")
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
		ISP:      body.ISP,
		Name:     body.Name,
		Address:  body.Address,
		Protocol: body.Protocol,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

func (h *Handlers) UpdateDNSServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		ISP      string `json:"isp"`
		Name     string `json:"name"`
		Address  string `json:"address"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.ISP == "" {
		writeError(w, http.StatusBadRequest, "isp is required")
		return
	}
	if body.Address == "" {
		writeError(w, http.StatusBadRequest, "address is required")
		return
	}
	if body.Protocol == "" {
		body.Protocol = "udp"
	}
	s, err := h.store.UpdateDNSServer(r.Context(), uint(id), db.DNSServer{
		ISP:      body.ISP,
		Name:     body.Name,
		Address:  body.Address,
		Protocol: body.Protocol,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *Handlers) DeleteDNSServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteDNSServer(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
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
	var body struct {
		URLs []string `json:"urls"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck — body is optional
	if err := h.scanner.Trigger(r.Context(), "manual", body.URLs); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "scan triggered"})
}

func (h *Handlers) ScanStatus(w http.ResponseWriter, r *http.Request) {
	run, err := h.store.ActiveScanRun(r.Context())
	if err != nil {
		writeInternalError(w, err)
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
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// URLsRequestedThisMonth counts watchlist additions since the start of the
// current calendar month (not a rolling 30-day window).
func (h *Handlers) URLsRequestedThisMonth(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	now := time.Now().UTC()
	since := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var count int
	var err error
	switch {
	case user.IsAdmin:
		count, err = h.store.CountDepartmentURLsSince(r.Context(), since)
	case user.DepartmentID != nil:
		count, err = h.store.CountDepartmentURLsSinceForDepartment(r.Context(), since, *user.DepartmentID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
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
		writeInternalError(w, err)
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
		writeInternalError(w, err)
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
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

type domainInfoResponse struct {
	Fetched bool `json:"fetched"`
	*db.DomainWhois
}

// DomainInfoByURL returns the cached RDAP registrar/expiry info for a
// domain, scoped the same way as /api/results (404 rather than 403 for a
// non-owning department, so the response doesn't confirm the domain
// exists). Returns {"fetched":false} — not a 404 — when the domain is owned
// but has no cached WHOIS row yet (never fetched).
func (h *Handlers) DomainInfoByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
		return
	}

	info, err := h.store.GetDomainWhois(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if info == nil {
		writeJSON(w, http.StatusOK, domainInfoResponse{Fetched: false})
		return
	}
	writeJSON(w, http.StatusOK, domainInfoResponse{Fetched: true, DomainWhois: info})
}

// RefreshDomainInfo triggers an on-demand RDAP re-fetch for a domain,
// bypassing the periodic refresher's staleness check. Scoped like
// DomainInfoByURL; blocks until the fetch completes so the response
// reflects the fresh result (or fetch error) immediately.
func (h *Handlers) RefreshDomainInfo(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
		return
	}
	if h.whoisFetch == nil {
		writeError(w, http.StatusServiceUnavailable, "whois lookups are disabled")
		return
	}

	u, err := h.store.GetURLByValue(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if u == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	fetchAndStoreWhois(h.store, h.whoisFetch, u.ID, u.URL)

	info, err := h.store.GetDomainWhois(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, domainInfoResponse{Fetched: true, DomainWhois: info})
}

type subdomainScanResponse struct {
	Fetched bool `json:"fetched"`
	*db.SubdomainScan
}

// SubdomainsByURL returns the cached subfinder result for a domain, scoped
// the same way as /api/results (404 rather than 403 for a non-owning
// department). Returns {"fetched":false} — not a 404 — when the domain is
// owned but has no cached row yet (never fetched, or subfinder is disabled).
func (h *Handlers) SubdomainsByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
		return
	}

	scan, err := h.store.GetSubdomainScan(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if scan == nil {
		writeJSON(w, http.StatusOK, subdomainScanResponse{Fetched: false})
		return
	}
	writeJSON(w, http.StatusOK, subdomainScanResponse{Fetched: true, SubdomainScan: scan})
}

// RefreshSubdomains triggers an on-demand subfinder re-run for a domain —
// the only way a cached row is ever updated after the initial on-add fetch,
// since there's no periodic sweep. Scoped like SubdomainsByURL; blocks
// until the run completes so the response reflects the fresh result (or
// fetch error) immediately.
func (h *Handlers) RefreshSubdomains(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
		return
	}
	if h.subfinderFetch == nil {
		writeError(w, http.StatusServiceUnavailable, "subdomain enumeration is disabled")
		return
	}

	u, err := h.store.GetURLByValue(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if u == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	fetchAndStoreSubdomains(h.store, h.subfinderFetch, u.ID, u.URL)

	scan, err := h.store.GetSubdomainScan(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, subdomainScanResponse{Fetched: true, SubdomainScan: scan})
}

// RefreshHostingInfo triggers an on-demand ASN/org/NetName/abuse-email
// re-fetch for a resolved IP, bypassing IPInfo's fetch-once-ever cache —
// the only way an already-cached IP's row is ever updated. Unscoped for any
// authenticated role: IPInfo is keyed by IP, not department-owned watchlist
// data, same rationale as DNSRecordsByURL/FaviconByURL.
func (h *Handlers) RefreshHostingInfo(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if ip == "" {
		writeError(w, http.StatusBadRequest, "ip is required")
		return
	}
	if h.ipFetch == nil {
		writeError(w, http.StatusServiceUnavailable, "hosting lookups are disabled")
		return
	}
	info := fetchAndCacheIPInfo(h.store, h.ipFetch, h.netnameFetch, ip)
	writeJSON(w, http.StatusOK, info)
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

// FaviconByURL serves a domain's favicon, fetching and caching it server-side
// on first request so the browser never has to contact the domain (or
// Google's favicon proxy) directly — see db.Favicon's comment for why that
// matters for this app.
func (h *Handlers) FaviconByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}

	domain := urlValue
	if parsed, parseErr := url.Parse(urlValue); parseErr == nil && parsed.Hostname() != "" {
		domain = parsed.Hostname()
	}

	cached, err := h.store.GetFavicon(r.Context(), domain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favicon")
		return
	}

	if cached == nil {
		if h.faviconFetch == nil {
			writeError(w, http.StatusServiceUnavailable, "favicon lookups are disabled")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		res, fetchErr := h.faviconFetch(ctx, domain)
		fav := db.Favicon{Domain: domain, FetchedAt: time.Now()}
		if fetchErr != nil {
			fav.FetchError = fetchErr.Error()
		} else {
			fav.ContentType = res.ContentType
			fav.Data = res.Data
		}
		if err := h.store.UpsertFavicon(r.Context(), fav); err != nil {
			log.Printf("upsert favicon for %s: %v", domain, err)
		}
		cached = &fav
	}

	if cached.FetchError != "" || len(cached.Data) == 0 {
		writeError(w, http.StatusNotFound, "no favicon available")
		return
	}

	w.Header().Set("Content-Type", cached.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=604800") // favicons rarely change; server already caches forever
	w.Write(cached.Data)
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
		writeInternalError(w, err)
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "no scan run found")
		return
	}
	progress, err := h.store.ScanProgress(r.Context(), run.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	urls, err := h.store.ListWatchedURLs(r.Context())
	if err != nil {
		writeInternalError(w, err)
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

// ISP Stats

func (h *Handlers) ISPStats(w http.ResponseWriter, r *http.Request) {
	isp, err := url.PathUnescape(chi.URLParam(r, "isp"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ISP")
		return
	}
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var stats db.ISPStatsResult
	if user.IsAdmin {
		stats, err = h.store.ISPStats(r.Context(), isp)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		stats, err = h.store.ISPStatsForDepartment(r.Context(), isp, *user.DepartmentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ISP stats")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// ISP Trend

func (h *Handlers) ISPTrend(w http.ResponseWriter, r *http.Request) {
	isp, err := url.PathUnescape(chi.URLParam(r, "isp"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ISP")
		return
	}
	now := time.Now()
	since := now.AddDate(0, 0, -30)
	until := now
	if s := r.URL.Query().Get("since"); s != "" {
		if t, err2 := time.Parse(time.RFC3339, s); err2 == nil {
			since = t
		}
	}
	if u := r.URL.Query().Get("until"); u != "" {
		if t, err2 := time.Parse(time.RFC3339, u); err2 == nil {
			until = t
		}
	}
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var stats []db.ISPTrendStat
	if user.IsAdmin {
		stats, err = h.store.ISPTrend(r.Context(), isp, since, until)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		stats, err = h.store.ISPTrendForDepartment(r.Context(), isp, since, until, *user.DepartmentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load trend data")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// ISP Compliance Timing

func (h *Handlers) ISPTiming(w http.ResponseWriter, r *http.Request) {
	isp, err := url.PathUnescape(chi.URLParam(r, "isp"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ISP")
		return
	}
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var timing db.ISPTimingResult
	if user.IsAdmin {
		timing, err = h.store.ISPComplianceTiming(r.Context(), isp)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		timing, err = h.store.ISPComplianceTimingForDepartment(r.Context(), isp, *user.DepartmentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load timing data")
		return
	}
	writeJSON(w, http.StatusOK, timing)
}

// National Trend

func (h *Handlers) NationalTrend(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	since := now.AddDate(0, 0, -30)
	until := now
	if s := r.URL.Query().Get("since"); s != "" {
		if t, err2 := time.Parse(time.RFC3339, s); err2 == nil {
			since = t
		}
	}
	if u := r.URL.Query().Get("until"); u != "" {
		if t, err2 := time.Parse(time.RFC3339, u); err2 == nil {
			until = t
		}
	}
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var stats []db.ISPTrendStat
	var err error
	if user.IsAdmin {
		stats, err = h.store.NationalTrend(r.Context(), since, until)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		stats, err = h.store.NationalTrendForDepartment(r.Context(), since, until, *user.DepartmentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load trend data")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// Resurfaced Domains

func (h *Handlers) ResurfacedDomains(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var domains []db.ResurfacedDomain
	var err error
	if user.IsAdmin {
		domains, err = h.store.ResurfacedDomains(r.Context())
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		domains, err = h.store.ResurfacedDomainsForDepartment(r.Context(), *user.DepartmentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load resurfaced domains")
		return
	}
	writeJSON(w, http.StatusOK, domains)
}

// Domain Summaries — GET /api/domains, backs the Domain page's browseable
// "any domain with scan history" table (distinct from /api/results, which is
// only the latest scan run).

const (
	defaultDomainSummaryPageSize = 25
	maxDomainSummaryPageSize     = 100
)

func (h *Handlers) DomainSummaries(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	pageSize := defaultDomainSummaryPageSize
	if ps, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && ps > 0 && ps <= maxDomainSummaryPageSize {
		pageSize = ps
	}

	filter := db.DomainSummaryFilter{Search: r.URL.Query().Get("q")}
	if status := r.URL.Query().Get("status"); status == "compliant" || status == "violations" {
		filter.Status = status
	}
	if id, err := strconv.Atoi(r.URL.Query().Get("dns_server_id")); err == nil && id > 0 {
		filter.DNSServerID = uint(id)
	}

	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var domains []db.DomainSummary
	var total int
	var err error
	if user.IsAdmin {
		domains, total, err = h.store.ListDomainSummaries(r.Context(), page, pageSize, filter)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		domains, total, err = h.store.ListDomainSummariesForDepartment(r.Context(), page, pageSize, *user.DepartmentID, filter)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load domain summaries")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains, "total": total})
}

// Domain Server Summaries — GET /api/domains/*url, the expanded-row detail
// under the Domain page's history table: one lifetime aggregate row per DNS
// server that has ever scanned this one domain.
func (h *Handlers) DomainServerSummaries(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
		return
	}
	summaries, err := h.store.DomainServerSummaries(r.Context(), urlValue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load domain server summaries")
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

// Screenshot

func (h *Handlers) TriggerScreenshot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL          string `json:"url"`
		DNSServerIDs []uint `json:"dns_server_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" || len(body.DNSServerIDs) == 0 {
		writeError(w, http.StatusBadRequest, "url and dns_server_ids are required")
		return
	}
	if isPrivateHost(body.URL) {
		writeError(w, http.StatusBadRequest, "url resolves to a private or reserved address")
		return
	}
	if h.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "scanner not configured")
		return
	}
	if err := h.scanner.TriggerScreenshot(r.Context(), body.URL, body.DNSServerIDs); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "screenshot requested"})
}
