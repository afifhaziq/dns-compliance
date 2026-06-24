package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/server"
	"github.com/afif/dns-tracking/internal/urlnorm"
	"github.com/go-chi/chi/v5"
)

// fullMockStore implements db.Store completely for handler tests.
type fullMockStore struct {
	urls           []db.URL
	dnsServers     []db.DNSServer
	results        []db.ScanResult
	activeRun      *db.ScanRun
	lastRun        *db.ScanRun
	progress       []db.ProgressEntry
	departments    []db.Department
	users          []db.User
	sessions       []db.Session
	departmentURLs []db.DepartmentURL
}

func (m *fullMockStore) ListURLs(_ context.Context) ([]db.URL, error) { return m.urls, nil }

// CreateURL mirrors postgresStore's get-or-create-by-normalized-value
// behavior so mock-store-backed tests exercise the same dedup semantics.
func (m *fullMockStore) CreateURL(_ context.Context, rawURL string) (db.URL, error) {
	normalized, err := urlnorm.Normalize(rawURL)
	if err != nil {
		return db.URL{}, err
	}
	for _, u := range m.urls {
		if u.URL == normalized {
			return u, nil
		}
	}
	u := db.URL{ID: uint(len(m.urls) + 1), URL: normalized, CreatedAt: time.Now()}
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

func (m *fullMockStore) LatestResultsForDepartment(ctx context.Context, departmentID uint) ([]db.ScanResult, error) {
	watchedIDs := make(map[uint]bool)
	for _, du := range m.departmentURLs {
		if du.DepartmentID == departmentID {
			watchedIDs[du.URLID] = true
		}
	}
	var out []db.ScanResult
	for _, r := range m.results {
		if watchedIDs[r.URLID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *fullMockStore) ListDepartments(_ context.Context) ([]db.Department, error) {
	return m.departments, nil
}
func (m *fullMockStore) CreateDepartment(_ context.Context, name string) (db.Department, error) {
	d := db.Department{ID: uint(len(m.departments) + 1), Name: name, CreatedAt: time.Now()}
	m.departments = append(m.departments, d)
	return d, nil
}

func (m *fullMockStore) ListUsers(_ context.Context) ([]db.User, error) { return m.users, nil }
func (m *fullMockStore) CreateUser(_ context.Context, u db.User) (db.User, error) {
	u.ID = uint(len(m.users) + 1)
	u.CreatedAt = time.Now()
	m.users = append(m.users, u)
	return u, nil
}
func (m *fullMockStore) GetUserByUsername(_ context.Context, username string) (*db.User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return &u, nil
		}
	}
	return nil, nil
}
func (m *fullMockStore) GetUserByID(_ context.Context, id uint) (*db.User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return &u, nil
		}
	}
	return nil, nil
}
func (m *fullMockStore) DeleteUser(_ context.Context, id uint) error {
	for i, u := range m.users {
		if u.ID == id {
			m.users = append(m.users[:i], m.users[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) CreateSession(_ context.Context, s db.Session) error {
	m.sessions = append(m.sessions, s)
	return nil
}
func (m *fullMockStore) GetSession(_ context.Context, token string) (*db.Session, error) {
	for _, s := range m.sessions {
		if s.Token == token && s.ExpiresAt.After(time.Now()) {
			return &s, nil
		}
	}
	return nil, nil
}
func (m *fullMockStore) DeleteSession(_ context.Context, token string) error {
	for i, s := range m.sessions {
		if s.Token == token {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) ListDepartmentURLs(_ context.Context, departmentID uint) ([]db.URL, error) {
	var out []db.URL
	for _, du := range m.departmentURLs {
		if du.DepartmentID != departmentID {
			continue
		}
		for _, u := range m.urls {
			if u.ID == du.URLID {
				out = append(out, u)
			}
		}
	}
	return out, nil
}

func (m *fullMockStore) AddURLToWatchlist(ctx context.Context, departmentID uint, rawURL string) (db.URL, error) {
	u, err := m.CreateURL(ctx, rawURL)
	if err != nil {
		return db.URL{}, err
	}
	for _, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == u.ID {
			return u, nil // already linked — no-op
		}
	}
	m.departmentURLs = append(m.departmentURLs, db.DepartmentURL{DepartmentID: departmentID, URLID: u.ID, CreatedAt: time.Now()})
	return u, nil
}

func (m *fullMockStore) RemoveURLFromWatchlist(_ context.Context, departmentID, urlID uint) (bool, error) {
	for i, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == urlID {
			m.departmentURLs = append(m.departmentURLs[:i], m.departmentURLs[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *fullMockStore) ListWatchedURLs(_ context.Context) ([]db.URL, error) {
	watchedIDs := make(map[uint]bool)
	for _, du := range m.departmentURLs {
		watchedIDs[du.URLID] = true
	}
	var out []db.URL
	for _, u := range m.urls {
		if watchedIDs[u.ID] {
			out = append(out, u)
		}
	}
	return out, nil
}

func (m *fullMockStore) ListUnassignedURLs(_ context.Context) ([]db.URL, error) {
	watchedIDs := make(map[uint]bool)
	for _, du := range m.departmentURLs {
		watchedIDs[du.URLID] = true
	}
	var out []db.URL
	for _, u := range m.urls {
		if !watchedIDs[u.ID] {
			out = append(out, u)
		}
	}
	return out, nil
}

func (m *fullMockStore) URLOwnedByDepartment(_ context.Context, departmentID uint, urlValue string) (bool, error) {
	normalized, err := urlnorm.Normalize(urlValue)
	if err != nil {
		return false, err
	}
	var urlID uint
	found := false
	for _, u := range m.urls {
		if u.URL == normalized {
			urlID = u.ID
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	for _, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == urlID {
			return true, nil
		}
	}
	return false, nil
}

var _ db.Store = (*fullMockStore)(nil)

func setupRouter(store db.Store, sc *server.Scanner) http.Handler {
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, sc, nil, false)
	return r
}

// loginAs seeds a user + session directly into the mock store and returns
// a cookie a test request can attach via req.AddCookie — bypassing the
// /api/auth/login flow for tests that aren't specifically about login.
func loginAs(store *fullMockStore, user db.User) *http.Cookie {
	user.ID = uint(len(store.users) + 1)
	user.CreatedAt = time.Now()
	store.users = append(store.users, user)

	token := fmt.Sprintf("test-session-token-%d", len(store.sessions)+1)
	store.sessions = append(store.sessions, db.Session{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	})
	return &http.Cookie{Name: "session_token", Value: token}
}

func adminCookie(store *fullMockStore) *http.Cookie {
	return loginAs(store, db.User{Username: "admin", PasswordHash: "x", IsAdmin: true})
}

func deptCookie(store *fullMockStore, departmentID uint) *http.Cookie {
	return loginAs(store, db.User{
		Username:     fmt.Sprintf("dept-user-%d", departmentID),
		PasswordHash: "x",
		DepartmentID: &departmentID,
	})
}

func TestListURLsEmpty(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	req.AddCookie(cookie)
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

func TestAddToWatchlist_AdminRequiresDepartment(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when admin omits department_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddToWatchlist_AdminWithDepartment(t *testing.T) {
	store := &fullMockStore{departments: []db.Department{{ID: 1, Name: "CMOD"}}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]any{"url": "https://example.com", "department_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var u db.URL
	json.NewDecoder(w.Body).Decode(&u)
	if u.URL != "example.com" {
		t.Fatalf("unexpected URL: %s", u.URL)
	}
}

func TestAddToWatchlist_NonAdminUsesOwnDepartment(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	listReq.AddCookie(cookie)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	var urls []db.URL
	json.NewDecoder(listW.Body).Decode(&urls)
	if len(urls) != 1 || urls[0].URL != "example.com" {
		t.Fatalf("expected the new url on the caller's own watchlist, got %v", urls)
	}
}

func TestAddToWatchlistMissingField(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"url": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty url, got %d", w.Code)
	}
}

func TestRemoveFromWatchlist_PreservesURLAndHistory(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1}},
		results:        []db.ScanResult{{ID: 1, URLID: 1, URLValue: "example.com", ScannedAt: time.Now()}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/urls/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.urls) != 1 {
		t.Fatalf("expected the url row to survive removal from the watchlist, got %v", store.urls)
	}
	if len(store.results) != 1 {
		t.Fatalf("expected scan history to survive removal from the watchlist, got %v", store.results)
	}

	watched, _ := store.ListWatchedURLs(context.Background())
	if len(watched) != 0 {
		t.Fatalf("expected the url to no longer be actively watched, got %v", watched)
	}
}

func TestRemoveFromWatchlist_NotOnWatchlistReturns404(t *testing.T) {
	store := &fullMockStore{urls: []db.URL{{ID: 1, URL: "example.com"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/urls/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPurgeURL_AdminOnly(t *testing.T) {
	store := &fullMockStore{urls: []db.URL{{ID: 1, URL: "example.com"}}}
	nonAdmin := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/urls/1", nil)
	req.AddCookie(nonAdmin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", w.Code)
	}

	admin := adminCookie(store)
	req2 := httptest.NewRequest(http.MethodDelete, "/api/admin/urls/1", nil)
	req2.AddCookie(admin)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin, got %d: %s", w2.Code, w2.Body.String())
	}
	if len(store.urls) != 0 {
		t.Fatalf("expected the url to be hard-deleted, got %v", store.urls)
	}
}

func TestCreateDNSServer(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"name": "Google", "address": "8.8.8.8:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateDNSServer_ForbiddenForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"name": "Google", "address": "8.8.8.8:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetScanStatusIdle(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/status", nil)
	req.AddCookie(cookie)
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
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results", nil)
	req.AddCookie(cookie)
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
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/progress", nil)
	req.AddCookie(cookie)
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
		departmentURLs: []db.DepartmentURL{
			{DepartmentID: 1, URLID: 1},
			{DepartmentID: 1, URLID: 2},
		},
		lastRun: &db.ScanRun{ID: 3, Status: "completed", StartedAt: now},
		progress: []db.ProgressEntry{
			{DNSServerID: 1, Name: "CF", Completed: 2},
			{DNSServerID: 2, Name: "Google", Completed: 1},
		},
	}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/progress", nil)
	req.AddCookie(cookie)
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

func TestResultsByURL_DefaultWindow(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", ScannedAt: time.Now().AddDate(0, 0, -10)},
		{ID: 2, URLValue: "https://example.com", ScannedAt: time.Now()},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com", nil)
	req.AddCookie(cookie)
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
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com?since="+since, nil)
	req.AddCookie(cookie)
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
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Now().AddDate(-2, 0, 0).Format(time.RFC3339))
	until := url.QueryEscape(time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com?since="+since+"&until="+until, nil)
	req.AddCookie(cookie)
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

func TestResultsByURL_404ForUnownedDomain(t *testing.T) {
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}, {ID: 2, URL: "other.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1}},
		results:        []db.ScanResult{{ID: 1, URLValue: "other.com", ScannedAt: time.Now()}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/results/other.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a domain not on the caller's watchlist, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHeatmapByURL_GroupsByDay(t *testing.T) {
	day := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", DNSServerID: 1, DNSServer: db.DNSServer{ID: 1, Name: "Google"}, Compliant: true, ScannedAt: day.Add(1 * time.Hour)},
		{ID: 2, URLValue: "https://example.com", DNSServerID: 1, DNSServer: db.DNSServer{ID: 1, Name: "Google"}, Compliant: false, ScannedAt: day.Add(2 * time.Hour)},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/heatmap/https://example.com?since="+since, nil)
	req.AddCookie(cookie)
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

func TestDNSRecordsByURL_ResolvesKnownHost(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/dns-records/google.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Hostname   string `json:"hostname"`
		Resolved   bool   `json:"resolved"`
		ResolverIP string `json:"resolver_ip"`
		Records    *struct {
			A     []string `json:"a"`
			AAAA  []string `json:"aaaa"`
			CNAME []string `json:"cname"`
			MX    []string `json:"mx"`
			TXT   []string `json:"txt"`
			NS    []string `json:"ns"`
		} `json:"records"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Hostname != "google.com" {
		t.Fatalf("expected hostname google.com, got %q", resp.Hostname)
	}
	if !resp.Resolved {
		t.Fatalf("expected resolved=true for google.com")
	}
	if resp.ResolverIP == "" {
		t.Fatalf("expected a non-empty resolver_ip")
	}
	if resp.Records == nil || len(resp.Records.A) == 0 {
		t.Fatalf("expected at least one A record for google.com, got %+v", resp.Records)
	}
	if resp.Records == nil || len(resp.Records.NS) == 0 {
		t.Fatalf("expected at least one NS record for google.com, got %+v", resp.Records)
	}
}

func TestDNSRecordsByURL_NXDOMAIN(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/dns-records/this-host-should-not-exist-zzqxv12345.invalid", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Hostname string `json:"hostname"`
		Resolved bool   `json:"resolved"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Resolved {
		t.Fatalf("expected resolved=false for a non-existent host")
	}
}

func TestResultsByURL_PercentEncodedSlashes(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com/", ScannedAt: time.Now()},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results/https%3A%2F%2Fexample.com%2F", nil)
	req.AddCookie(cookie)
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

// Auth

func TestLogin_Success(t *testing.T) {
	hash, _ := db.HashPassword("s3cret")
	store := &fullMockStore{users: []db.User{{ID: 1, Username: "alice", PasswordHash: hash, IsAdmin: true}}}
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "s3cret"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) == 0 {
		t.Fatalf("expected a session cookie to be set")
	}
	if len(store.sessions) != 1 {
		t.Fatalf("expected a session to be created, got %d", len(store.sessions))
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	hash, _ := db.HashPassword("s3cret")
	store := &fullMockStore{users: []db.User{{ID: 1, Username: "alice", PasswordHash: hash, IsAdmin: true}}}
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_RejectsMissingCookie(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLogout_ClearsSession(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if len(store.sessions) != 0 {
		t.Fatalf("expected the session to be deleted, got %d remaining", len(store.sessions))
	}

	// The same cookie must no longer work.
	req2 := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", w2.Code)
	}
}
