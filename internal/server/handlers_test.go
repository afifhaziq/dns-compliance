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
	domainWhois    []db.DomainWhois
	ipInfo         []db.IPInfo
	favicons       []db.Favicon
	subdomainScans []db.SubdomainScan
	scanInterval   int
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
		dnsServerName       string
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

func (m *fullMockStore) ListDepartmentURLs(_ context.Context, departmentID uint) ([]db.URLEntry, error) {
	var out []db.URLEntry
	for _, du := range m.departmentURLs {
		if du.DepartmentID != departmentID {
			continue
		}
		for _, u := range m.urls {
			if u.ID == du.URLID {
				out = append(out, db.URLEntry{ID: u.ID, URL: u.URL, Enabled: du.Enabled, OrderedAt: du.OrderedAt, CreatedAt: u.CreatedAt})
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
	m.departmentURLs = append(m.departmentURLs, db.DepartmentURL{DepartmentID: departmentID, URLID: u.ID, Enabled: true, CreatedAt: time.Now()})
	return u, nil
}

func (m *fullMockStore) SetURLEnabled(_ context.Context, departmentID, urlID uint, enabled bool) (bool, error) {
	for i, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == urlID {
			m.departmentURLs[i].Enabled = enabled
			return true, nil
		}
	}
	return false, nil
}

func (m *fullMockStore) SetURLOrderedAt(_ context.Context, departmentID, urlID uint, orderedAt *time.Time) (bool, error) {
	for i, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == urlID {
			m.departmentURLs[i].OrderedAt = orderedAt
			return true, nil
		}
	}
	return false, nil
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
		if du.Enabled {
			watchedIDs[du.URLID] = true
		}
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

func (m *fullMockStore) CountDepartmentURLsSince(_ context.Context, since time.Time) (int, error) {
	count := 0
	for _, du := range m.departmentURLs {
		if !du.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (m *fullMockStore) CountDepartmentURLsSinceForDepartment(_ context.Context, since time.Time, departmentID uint) (int, error) {
	count := 0
	for _, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && !du.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (m *fullMockStore) ListCompliantIPs(_ context.Context) ([]db.CompliantIP, error) {
	return nil, nil
}
func (m *fullMockStore) CreateCompliantIP(_ context.Context, address, note string) (db.CompliantIP, error) {
	return db.CompliantIP{Address: address, Note: note}, nil
}
func (m *fullMockStore) DeleteCompliantIP(_ context.Context, _ uint) error { return nil }

func (m *fullMockStore) GetScanInterval(_ context.Context) (int, error) { return m.scanInterval, nil }
func (m *fullMockStore) SetScanInterval(_ context.Context, minutes int) error {
	m.scanInterval = minutes
	return nil
}

func (m *fullMockStore) ISPStats(_ context.Context, _ string) (db.ISPStatsResult, error) {
	return db.ISPStatsResult{}, nil
}

func (m *fullMockStore) ISPStatsForDepartment(_ context.Context, _ string, _ uint) (db.ISPStatsResult, error) {
	return db.ISPStatsResult{}, nil
}

func (m *fullMockStore) ISPTrend(_ context.Context, _ string, _, _ time.Time) ([]db.ISPTrendStat, error) {
	return nil, nil
}
func (m *fullMockStore) ISPTrendForDepartment(_ context.Context, _ string, _, _ time.Time, _ uint) ([]db.ISPTrendStat, error) {
	return nil, nil
}

func (m *fullMockStore) ISPComplianceTiming(_ context.Context, isp string) (db.ISPTimingResult, error) {
	return db.ISPTimingResult{ISP: isp}, nil
}
func (m *fullMockStore) ISPComplianceTimingForDepartment(_ context.Context, isp string, _ uint) (db.ISPTimingResult, error) {
	return db.ISPTimingResult{ISP: isp}, nil
}

func (m *fullMockStore) NationalTrend(_ context.Context, _, _ time.Time) ([]db.ISPTrendStat, error) {
	return nil, nil
}
func (m *fullMockStore) NationalTrendForDepartment(_ context.Context, _, _ time.Time, _ uint) ([]db.ISPTrendStat, error) {
	return nil, nil
}

func (m *fullMockStore) UpsertDomainWhois(_ context.Context, w db.DomainWhois) error {
	for i, existing := range m.domainWhois {
		if existing.URLID == w.URLID {
			m.domainWhois[i] = w
			return nil
		}
	}
	m.domainWhois = append(m.domainWhois, w)
	return nil
}

func (m *fullMockStore) GetURLByValue(_ context.Context, urlValue string) (*db.URL, error) {
	normalized, err := urlnorm.Normalize(urlValue)
	if err != nil {
		return nil, err
	}
	for _, u := range m.urls {
		if u.URL == normalized {
			uCopy := u
			return &uCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) GetDomainWhois(ctx context.Context, urlValue string) (*db.DomainWhois, error) {
	u, err := m.GetURLByValue(ctx, urlValue)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	urlID := u.ID
	for _, w := range m.domainWhois {
		if w.URLID == urlID {
			wCopy := w
			return &wCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) ListStaleDomains(_ context.Context, olderThan time.Time, limit int) ([]db.URL, error) {
	fetchedAt := make(map[uint]time.Time, len(m.domainWhois))
	for _, w := range m.domainWhois {
		fetchedAt[w.URLID] = w.LastFetchedAt
	}
	watchedIDs := make(map[uint]bool)
	for _, du := range m.departmentURLs {
		if du.Enabled {
			watchedIDs[du.URLID] = true
		}
	}
	var out []db.URL
	for _, u := range m.urls {
		if !watchedIDs[u.ID] {
			continue
		}
		last, fetched := fetchedAt[u.ID]
		if fetched && !last.Before(olderThan) {
			continue
		}
		out = append(out, u)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *fullMockStore) GetIPInfo(_ context.Context, ip string) (*db.IPInfo, error) {
	for _, info := range m.ipInfo {
		if info.IP == ip {
			infoCopy := info
			return &infoCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) UpsertIPInfo(_ context.Context, info db.IPInfo) error {
	for i, existing := range m.ipInfo {
		if existing.IP == info.IP {
			m.ipInfo[i] = info
			return nil
		}
	}
	m.ipInfo = append(m.ipInfo, info)
	return nil
}

func (m *fullMockStore) GetFavicon(_ context.Context, domain string) (*db.Favicon, error) {
	for _, fav := range m.favicons {
		if fav.Domain == domain {
			favCopy := fav
			return &favCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) UpsertFavicon(_ context.Context, fav db.Favicon) error {
	for i, existing := range m.favicons {
		if existing.Domain == fav.Domain {
			m.favicons[i] = fav
			return nil
		}
	}
	m.favicons = append(m.favicons, fav)
	return nil
}

func (m *fullMockStore) GetSubdomainScan(ctx context.Context, urlValue string) (*db.SubdomainScan, error) {
	u, err := m.GetURLByValue(ctx, urlValue)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	for _, s := range m.subdomainScans {
		if s.URLID == u.ID {
			sCopy := s
			return &sCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) UpsertSubdomainScan(_ context.Context, s db.SubdomainScan) error {
	for i, existing := range m.subdomainScans {
		if existing.URLID == s.URLID {
			m.subdomainScans[i] = s
			return nil
		}
	}
	m.subdomainScans = append(m.subdomainScans, s)
	return nil
}

var _ db.Store = (*fullMockStore)(nil)

func setupRouter(store db.Store, sc *server.Scanner) http.Handler {
	r := chi.NewRouter()
	// whoisFetch/subfinderFetch/ipFetch/netnameFetch are nil — the lazy
	// on-add fetch goroutines never run in tests, so no test hits the
	// network or shells out.
	server.RegisterRoutes(r, store, sc, nil, false, nil, nil, nil, nil, nil)
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
	adminDeptID := uint(999)
	return loginAs(store, db.User{Username: "admin", PasswordHash: "x", IsAdmin: true, DepartmentID: &adminDeptID})
}

func deptCookie(store *fullMockStore, departmentID uint) *http.Cookie {
	return loginAs(store, db.User{
		Username:     fmt.Sprintf("dept-user-%d", departmentID),
		PasswordHash: "x",
		DepartmentID: &departmentID,
	})
}

func deptAdminCookie(store *fullMockStore, departmentID uint) *http.Cookie {
	return loginAs(store, db.User{
		Username:     fmt.Sprintf("dept-admin-%d", departmentID),
		PasswordHash: "x",
		IsDeptAdmin:  true,
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
	var entries []db.URLEntry
	json.NewDecoder(w.Body).Decode(&entries)
	if len(entries) != 0 {
		t.Fatalf("expected empty list, got %v", entries)
	}
}

func TestAddToWatchlist_AdminUsesOwnDepartment(t *testing.T) {
	// Admin is now assigned to the "Admin" department — no body department_id needed.
	store := &fullMockStore{}
	cookie := adminCookie(store) // sets DepartmentID=999
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin adding URL to own department, got %d: %s", w.Code, w.Body.String())
	}
	var entry db.URLEntry
	json.NewDecoder(w.Body).Decode(&entry)
	if entry.URL != "example.com" {
		t.Fatalf("unexpected URL: %s", entry.URL)
	}
	if !entry.Enabled {
		t.Fatalf("expected newly added URL to be enabled")
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
	var entries []db.URLEntry
	json.NewDecoder(listW.Body).Decode(&entries)
	if len(entries) != 1 || entries[0].URL != "example.com" {
		t.Fatalf("expected the new url on the caller's own watchlist, got %v", entries)
	}
	if !entries[0].Enabled {
		t.Fatalf("expected newly added URL to be enabled")
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
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
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
	body, _ := json.Marshal(map[string]string{"isp": "Google", "name": "Google UDP", "address": "8.8.8.8:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListDNSServers_AllowedForNonAdmin(t *testing.T) {
	store := &fullMockStore{dnsServers: []db.DNSServer{{ID: 1, ISP: "Google", Name: "Google UDP", Address: "8.8.8.8:53", Protocol: "udp"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/dns-servers", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for a non-admin reading the DNS server list, got %d: %s", w.Code, w.Body.String())
	}
	var servers []db.DNSServer
	json.NewDecoder(w.Body).Decode(&servers)
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
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

func TestCreateDNSServer_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"isp": "Google", "name": "Google UDP", "address": "8.8.8.8:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected a department admin to be able to add a DNS server (shared catalog), got %d: %s", w.Code, w.Body.String())
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
			{DepartmentID: 1, URLID: 1, Enabled: true},
			{DepartmentID: 1, URLID: 2, Enabled: true},
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
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
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
		{ID: 1, URLValue: "https://example.com", DNSServerID: 1, DNSServer: db.DNSServer{ID: 1, ISP: "Google", Name: "Google UDP"}, Compliant: true, ScannedAt: day.Add(1 * time.Hour)},
		{ID: 2, URLValue: "https://example.com", DNSServerID: 1, DNSServer: db.DNSServer{ID: 1, ISP: "Google", Name: "Google UDP"}, Compliant: false, ScannedAt: day.Add(2 * time.Hour)},
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

// Admin: departments and users

func TestCreateDepartment_AdminOnly(t *testing.T) {
	store := &fullMockStore{}
	nonAdmin := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"name": "Legal"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/departments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(nonAdmin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", w.Code)
	}

	admin := adminCookie(store)
	req2 := httptest.NewRequest(http.MethodPost, "/api/admin/departments", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(admin)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin, got %d: %s", w2.Code, w2.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/departments", nil)
	listReq.AddCookie(admin)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	var departments []db.Department
	json.NewDecoder(listW.Body).Decode(&departments)
	if len(departments) != 1 || departments[0].Name != "Legal" {
		t.Fatalf("expected the new department to be listed, got %v", departments)
	}
}

func TestCreateUser_RequiresDepartmentForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{"username": "bob", "password": "pw12345", "is_admin": false})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when department_id is omitted for a non-admin user, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateUser_Success(t *testing.T) {
	store := &fullMockStore{departments: []db.Department{{ID: 1, Name: "CMOD"}}}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{
		"username": "bob", "password": "pw12345", "is_admin": false, "department_id": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var u db.User
	json.NewDecoder(w.Body).Decode(&u)
	if u.Username != "bob" || u.DepartmentID == nil || *u.DepartmentID != 1 {
		t.Fatalf("unexpected created user: %+v", u)
	}
	if u.PasswordHash != "" {
		t.Fatalf("expected PasswordHash to never be serialized in the API response, got %q", u.PasswordHash)
	}
	stored := store.users[len(store.users)-1]
	if stored.PasswordHash == "" || stored.PasswordHash == "pw12345" {
		t.Fatalf("expected the stored password to be hashed, not stored in plaintext")
	}

	// The new user can actually log in with the password they were given.
	loginBody, _ := json.Marshal(map[string]string{"username": "bob", "password": "pw12345"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("expected the newly created user to be able to log in, got %d: %s", loginW.Code, loginW.Body.String())
	}
}

func TestListUsers_DeptAdminSeesOnlyOwnDepartment(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "alice", DepartmentID: &dept1},
		{ID: 2, Username: "carol", DepartmentID: &dept2},
	}}
	cookie := deptAdminCookie(store, dept1) // becomes user ID 3
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var users []db.User
	json.NewDecoder(w.Body).Decode(&users)
	if len(users) != 2 {
		t.Fatalf("expected 2 department-1 users (alice + the dept admin itself), got %d: %+v", len(users), users)
	}
	for _, u := range users {
		if u.DepartmentID == nil || *u.DepartmentID != dept1 {
			t.Fatalf("expected only department-1 users, got %+v", u)
		}
	}
}

func TestListUsers_SuperAdminSeesAll(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "alice", DepartmentID: &dept1},
		{ID: 2, Username: "carol", DepartmentID: &dept2},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var users []db.User
	json.NewDecoder(w.Body).Decode(&users)
	if len(users) != 3 { // alice, carol, and the admin itself
		t.Fatalf("expected a super admin to see every user, got %d: %+v", len(users), users)
	}
}

func TestCreateUser_DeptAdminForcesOwnDepartmentAndPlainRole(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	store := &fullMockStore{}
	cookie := deptAdminCookie(store, dept1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{
		"username": "eve", "password": "pw12345",
		"is_admin": true, "is_dept_admin": true, "department_id": dept2,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var u db.User
	json.NewDecoder(w.Body).Decode(&u)
	if u.IsAdmin || u.IsDeptAdmin {
		t.Fatalf("expected a department admin to never grant admin/dept-admin, got %+v", u)
	}
	if u.DepartmentID == nil || *u.DepartmentID != dept1 {
		t.Fatalf("expected the created user pinned to the caller's own department (1), got %+v", u)
	}
}

func TestDeleteUser_DeptAdminScoping(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "same-dept-member", DepartmentID: &dept1},
		{ID: 2, Username: "other-dept-member", DepartmentID: &dept2},
		{ID: 3, Username: "same-dept-admin", IsDeptAdmin: true, DepartmentID: &dept1},
	}}
	cookie := deptAdminCookie(store, dept1)
	r := setupRouter(store, nil)

	del := func(id uint) int {
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", id), nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := del(2); code != http.StatusForbidden {
		t.Fatalf("expected 403 deleting a user in a different department, got %d", code)
	}
	if code := del(3); code != http.StatusForbidden {
		t.Fatalf("expected 403 deleting another department admin, got %d", code)
	}
	if code := del(1); code != http.StatusNoContent {
		t.Fatalf("expected 204 deleting a plain member of the caller's own department, got %d", code)
	}
}

func TestToggleURL_DisableAndReenable(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.departmentURLs[0].Enabled {
		t.Fatal("expected Enabled=false after toggle")
	}

	body2, _ := json.Marshal(map[string]bool{"enabled": true})
	req2 := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("want 204 on re-enable, got %d: %s", w2.Code, w2.Body.String())
	}
	if !store.departmentURLs[0].Enabled {
		t.Fatal("expected Enabled=true after re-enable")
	}
}

func TestToggleURL_NotOnWatchlistReturns404(t *testing.T) {
	store := &fullMockStore{urls: []db.URL{{ID: 1, URL: "example.com"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for URL not on watchlist, got %d", w.Code)
	}
}

func TestToggleURL_SetsOrderedAtWithoutTouchingEnabled(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"ordered_at": "2026-01-15T00:00:00Z"})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if !store.departmentURLs[0].Enabled {
		t.Fatal("expected Enabled to remain untouched by an ordered_at-only body")
	}
	if store.departmentURLs[0].OrderedAt == nil {
		t.Fatal("expected ordered_at to be set")
	}

	// Clearing with an empty string
	clearBody, _ := json.Marshal(map[string]string{"ordered_at": ""})
	req2 := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(clearBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("want 204 on clear, got %d: %s", w2.Code, w2.Body.String())
	}
	if store.departmentURLs[0].OrderedAt != nil {
		t.Fatal("expected ordered_at to be cleared by an empty string")
	}
}

func TestToggleURL_InvalidOrderedAtReturns400(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"ordered_at": "not-a-date"})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid ordered_at, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteUser_AdminOnly(t *testing.T) {
	store := &fullMockStore{users: []db.User{{ID: 5, Username: "bob"}}}
	nonAdmin := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/5", nil)
	req.AddCookie(nonAdmin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", w.Code)
	}

	admin := adminCookie(store)
	req2 := httptest.NewRequest(http.MethodDelete, "/api/admin/users/5", nil)
	req2.AddCookie(admin)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestURLsRequestedThisMonth_CountsOnlyThisCalendarMonth(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	now := time.Now().UTC()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastMonth := startOfMonth.AddDate(0, 0, -1) // last day of the previous month — outside the window even if within 30 days
	store := &fullMockStore{departmentURLs: []db.DepartmentURL{
		{DepartmentID: dept1, URLID: 1, CreatedAt: startOfMonth.Add(time.Hour)}, // this month, dept1
		{DepartmentID: dept2, URLID: 2, CreatedAt: now},                         // this month, dept2
		{DepartmentID: dept1, URLID: 3, CreatedAt: lastMonth},                   // previous month — must not count
	}}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/urls/requested-count", nil)
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]int
	json.NewDecoder(w.Body).Decode(&got)
	if got["count"] != 2 {
		t.Fatalf("expected admin to see 2 requests this calendar month (excluding last month's), got %+v", got)
	}
}

func TestURLsRequestedThisMonth_ScopedForDepartment(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	now := time.Now().UTC()
	store := &fullMockStore{departmentURLs: []db.DepartmentURL{
		{DepartmentID: dept1, URLID: 1, CreatedAt: now},
		{DepartmentID: dept2, URLID: 2, CreatedAt: now},
		{DepartmentID: dept2, URLID: 3, CreatedAt: now},
	}}
	cookie := deptCookie(store, dept1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/urls/requested-count", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]int
	json.NewDecoder(w.Body).Decode(&got)
	if got["count"] != 1 {
		t.Fatalf("expected department 1 to see only its own request, got %+v", got)
	}
}

func TestScanInterval_GetAndSet_AdminOnly(t *testing.T) {
	store := &fullMockStore{scanInterval: 60}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/scan-interval", nil)
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]int
	json.NewDecoder(w.Body).Decode(&got)
	if got["interval_minutes"] != 60 {
		t.Fatalf("expected interval_minutes=60, got %+v", got)
	}

	body, _ := json.Marshal(map[string]int{"interval_minutes": 15})
	req2 := httptest.NewRequest(http.MethodPatch, "/api/admin/scan-interval", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(admin)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w2.Code, w2.Body.String())
	}
	if store.scanInterval != 15 {
		t.Fatalf("expected the stored interval to update to 15, got %d", store.scanInterval)
	}
}

func TestScanInterval_ForbiddenForDeptAdmin(t *testing.T) {
	store := &fullMockStore{scanInterval: 60}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/scan-interval", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected scan interval to stay super-admin-only, got %d: %s", w.Code, w.Body.String())
	}
}

func TestScanInterval_RejectsNonPositive(t *testing.T) {
	store := &fullMockStore{}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]int{"interval_minutes": 0})
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/scan-interval", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-positive interval, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSubdomainsByURL_FetchedFalseWhenNeverScanned(t *testing.T) {
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/subdomains/example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Fetched bool `json:"fetched"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Fetched {
		t.Fatalf("expected fetched=false with no cached SubdomainScan row, got true")
	}
}

func TestSubdomainsByURL_404ForUnownedDomain(t *testing.T) {
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}, {ID: 2, URL: "other.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/subdomains/other.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a domain not on the caller's watchlist, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefreshSubdomains_503WhenDisabled(t *testing.T) {
	// setupRouter always wires a nil subfinderFetch — mirrors production
	// running with --subfinder-path "" to disable enumeration entirely.
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/subdomains/example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when subfinderFetch is nil, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefreshHostingInfo_503WhenDisabled(t *testing.T) {
	// setupRouter always wires a nil ipFetch — mirrors production running
	// with hosting lookups disabled.
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/hosting/1.2.3.4", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when ipFetch is nil, got %d: %s", w.Code, w.Body.String())
	}
}
