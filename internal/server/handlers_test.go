package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/server"
	"github.com/go-chi/chi/v5"
)

// fullMockStore implements db.Store completely for handler tests.
type fullMockStore struct {
	urls       []db.URL
	dnsServers []db.DNSServer
	results    []db.ScanResult
	activeRun  *db.ScanRun
	lastRun    *db.ScanRun
	progress   []db.ProgressEntry
}

func (m *fullMockStore) ListURLs(_ context.Context) ([]db.URL, error) { return m.urls, nil }
func (m *fullMockStore) CreateURL(_ context.Context, rawURL string) (db.URL, error) {
	u := db.URL{ID: uint(len(m.urls) + 1), URL: rawURL, CreatedAt: time.Now()}
	m.urls = append(m.urls, u)
	return u, nil
}
func (m *fullMockStore) DeleteURL(_ context.Context, id uint) error {
	for i, u := range m.urls {
		if u.ID == id {
			m.urls = append(m.urls[:i], m.urls[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *fullMockStore) ListDNSServers(_ context.Context) ([]db.DNSServer, error) {
	return m.dnsServers, nil
}
func (m *fullMockStore) CreateDNSServer(_ context.Context, s db.DNSServer) (db.DNSServer, error) {
	s.ID = uint(len(m.dnsServers) + 1)
	m.dnsServers = append(m.dnsServers, s)
	return s, nil
}
func (m *fullMockStore) DeleteDNSServer(_ context.Context, id uint) error {
	for i, s := range m.dnsServers {
		if s.ID == id {
			m.dnsServers = append(m.dnsServers[:i], m.dnsServers[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *fullMockStore) CreateScanRun(_ context.Context, by string) (db.ScanRun, error) {
	return db.ScanRun{ID: 1, TriggeredBy: by, Status: "running", StartedAt: time.Now()}, nil
}
func (m *fullMockStore) CompleteScanRun(_ context.Context, _ uint, _ string, _ time.Time) error {
	return nil
}
func (m *fullMockStore) ActiveScanRun(_ context.Context) (*db.ScanRun, error) {
	return m.activeRun, nil
}
func (m *fullMockStore) LastScanRun(_ context.Context) (*db.ScanRun, error) {
	return m.lastRun, nil
}
func (m *fullMockStore) ScanProgress(_ context.Context, _ uint) ([]db.ProgressEntry, error) {
	return m.progress, nil
}
func (m *fullMockStore) LatestResults(_ context.Context) ([]db.ScanResult, error) {
	return m.results, nil
}
func (m *fullMockStore) ResultsByURL(_ context.Context, u string, since, until time.Time) ([]db.ScanResult, error) {
	var out []db.ScanResult
	for _, r := range m.results {
		if r.URLValue != u || r.ScannedAt.Before(since) {
			continue
		}
		if !until.IsZero() && r.ScannedAt.After(until) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
func (m *fullMockStore) DailyComplianceByURL(_ context.Context, u string, since, until time.Time) ([]db.DailyComplianceStat, error) {
	type bucketKey struct {
		dnsServerID uint
		day         string
	}
	type bucket struct {
		dnsServerName        string
		total, compliantSum int
	}
	buckets := make(map[bucketKey]*bucket)
	order := make([]bucketKey, 0)
	for _, r := range m.results {
		if r.URLValue != u || r.ScannedAt.Before(since) {
			continue
		}
		if !until.IsZero() && r.ScannedAt.After(until) {
			continue
		}
		k := bucketKey{dnsServerID: r.DNSServerID, day: r.ScannedAt.Format("2006-01-02")}
		b, ok := buckets[k]
		if !ok {
			b = &bucket{dnsServerName: r.DNSServer.Name}
			buckets[k] = b
			order = append(order, k)
		}
		b.total++
		if r.Compliant {
			b.compliantSum++
		}
	}
	out := make([]db.DailyComplianceStat, 0, len(order))
	for _, k := range order {
		b := buckets[k]
		out = append(out, db.DailyComplianceStat{
			DNSServerID:   k.dnsServerID,
			DNSServerName: b.dnsServerName,
			Day:           k.day,
			Total:         b.total,
			Compliant:     b.compliantSum,
			Level:         db.DailyComplianceLevel(b.total, b.compliantSum),
		})
	}
	return out, nil
}

func (m *fullMockStore) InsertResult(_ context.Context, r db.ScanResult) error {
	m.results = append(m.results, r)
	return nil
}
func (m *fullMockStore) UpdateScreenshot(_ context.Context, _ uint, _ string) error { return nil }

var _ db.Store = (*fullMockStore)(nil)

func setupRouter(store db.Store, sc *server.Scanner) http.Handler {
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, sc, nil)
	return r
}

func TestListURLsEmpty(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var urls []db.URL
	json.NewDecoder(w.Body).Decode(&urls)
	if len(urls) != 0 {
		t.Fatalf("expected empty list, got %v", urls)
	}
}

func TestCreateURL(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	body, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var u db.URL
	json.NewDecoder(w.Body).Decode(&u)
	if u.URL != "https://example.com" {
		t.Fatalf("unexpected URL: %s", u.URL)
	}
}

func TestDeleteURL(t *testing.T) {
	store := &fullMockStore{urls: []db.URL{{ID: 1, URL: "https://example.com"}}}
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/urls/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestCreateDNSServer(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	body, _ := json.Marshal(map[string]string{"name": "Google", "address": "8.8.8.8:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetScanStatusIdle(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "idle" {
		t.Fatalf("expected idle, got %v", resp["status"])
	}
}

func TestGetLatestResults(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", Compliant: false, ScannedAt: time.Now()},
	}}
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestScanProgressNotFound(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/progress", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestScanProgressWithRun(t *testing.T) {
	now := time.Now()
	store := &fullMockStore{
		urls: []db.URL{
			{ID: 1, URL: "https://a.com"},
			{ID: 2, URL: "https://b.com"},
		},
		lastRun: &db.ScanRun{ID: 3, Status: "completed", StartedAt: now},
		progress: []db.ProgressEntry{
			{DNSServerID: 1, Name: "CF", Completed: 2},
			{DNSServerID: 2, Name: "Google", Completed: 1},
		},
	}
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/progress", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ScanRun   map[string]any   `json:"scan_run"`
		TotalURLs int              `json:"total_urls"`
		PerDNS    []map[string]any `json:"per_dns"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalURLs != 2 {
		t.Fatalf("expected total_urls=2, got %d", resp.TotalURLs)
	}
	if len(resp.PerDNS) != 2 {
		t.Fatalf("expected 2 per_dns entries, got %d", len(resp.PerDNS))
	}
}

func TestCreateURLMissingField(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	body, _ := json.Marshal(map[string]string{"url": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty url, got %d", w.Code)
	}
}

func TestResultsByURL_DefaultWindow(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", ScannedAt: time.Now().AddDate(0, 0, -10)},
		{ID: 2, URLValue: "https://example.com", ScannedAt: time.Now()},
	}}
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result within default 7-day window, got %d", len(results))
	}
	if results[0].ID != 2 {
		t.Fatalf("expected the recent result (id=2), got id=%d", results[0].ID)
	}
}

func TestResultsByURL_ExplicitSince(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", ScannedAt: time.Now().AddDate(0, 0, -10)},
		{ID: 2, URLValue: "https://example.com", ScannedAt: time.Now()},
	}}
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com?since="+since, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 2 {
		t.Fatalf("expected 2 results within 30-day window, got %d", len(results))
	}
}

func TestResultsByURL_ExplicitUntil(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", ScannedAt: time.Now().AddDate(-1, 0, 0)},
		{ID: 2, URLValue: "https://example.com", ScannedAt: time.Now()},
	}}
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Now().AddDate(-2, 0, 0).Format(time.RFC3339))
	until := url.QueryEscape(time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com?since="+since+"&until="+until, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result within since/until window, got %d", len(results))
	}
	if results[0].ID != 1 {
		t.Fatalf("expected the older result (id=1), got id=%d", results[0].ID)
	}
}

func TestHeatmapByURL_GroupsByDay(t *testing.T) {
	day := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", DNSServerID: 1, DNSServer: db.DNSServer{ID: 1, Name: "Google"}, Compliant: true, ScannedAt: day.Add(1 * time.Hour)},
		{ID: 2, URLValue: "https://example.com", DNSServerID: 1, DNSServer: db.DNSServer{ID: 1, Name: "Google"}, Compliant: false, ScannedAt: day.Add(2 * time.Hour)},
	}}
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/heatmap/https://example.com?since="+since, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stats []db.DailyComplianceStat
	json.NewDecoder(w.Body).Decode(&stats)
	if len(stats) != 1 {
		t.Fatalf("expected 1 day bucket, got %d", len(stats))
	}
	if stats[0].Total != 2 || stats[0].Compliant != 1 {
		t.Fatalf("expected total=2 compliant=1, got %+v", stats[0])
	}
	if stats[0].Level != 3 {
		t.Fatalf("expected level 3 (1 of 2 violations, rate 0.5 is >1/3 and <=2/3), got %d", stats[0].Level)
	}
}

func TestResultsByURL_PercentEncodedSlashes(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com/", ScannedAt: time.Now()},
	}}
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results/https%3A%2F%2Fexample.com%2F", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}
