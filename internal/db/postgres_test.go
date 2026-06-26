package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/glebarez/sqlite"
)

func newTestStore(t *testing.T) db.Store {
	t.Helper()
	gormDB, err := db.Connect(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return db.NewStore(gormDB)
}

func TestCreateAndListURLs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateURL(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	urls, err := s.ListURLs(ctx)
	if err != nil {
		t.Fatalf("ListURLs: %v", err)
	}
	if len(urls) != 1 || urls[0].URL != "example.com" {
		t.Fatalf("expected 1 normalized url, got %v", urls)
	}
}

func TestCreateURL_NormalizesAndDedupes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.CreateURL(ctx, "https://Example.com/")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	b, err := s.CreateURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	if a.ID != b.ID {
		t.Fatalf("expected both inputs to resolve to the same URL row, got ids %d and %d", a.ID, b.ID)
	}
	urls, _ := s.ListURLs(ctx)
	if len(urls) != 1 {
		t.Fatalf("expected exactly 1 url row, got %d", len(urls))
	}
}

func TestDeleteURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateURL(ctx, "https://example.com")
	if err := s.DeleteURL(ctx, u.ID); err != nil {
		t.Fatalf("DeleteURL: %v", err)
	}
	urls, _ := s.ListURLs(ctx)
	if len(urls) != 0 {
		t.Fatal("expected 0 urls after delete")
	}
}

func TestCreateAndListDNSServers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, err := s.CreateDNSServer(ctx, db.DNSServer{Name: "Google", Address: "8.8.8.8:53", Protocol: "udp"})
	if err != nil {
		t.Fatalf("CreateDNSServer: %v", err)
	}
	if srv.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	servers, _ := s.ListDNSServers(ctx)
	if len(servers) != 1 || servers[0].Name != "Google" {
		t.Fatalf("expected 1 server, got %v", servers)
	}
}

func TestScanRunLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	active, _ := s.ActiveScanRun(ctx)
	if active != nil {
		t.Fatal("expected no active scan run initially")
	}

	run, err := s.CreateScanRun(ctx, "manual")
	if err != nil {
		t.Fatalf("CreateScanRun: %v", err)
	}
	if run.Status != "running" {
		t.Fatalf("expected status running, got %s", run.Status)
	}

	active, _ = s.ActiveScanRun(ctx)
	if active == nil || active.ID != run.ID {
		t.Fatal("expected active scan run")
	}

	if err := s.CompleteScanRun(ctx, run.ID, "completed", time.Now()); err != nil {
		t.Fatalf("CompleteScanRun: %v", err)
	}

	active, _ = s.ActiveScanRun(ctx)
	if active != nil {
		t.Fatal("expected no active scan run after completion")
	}
}

func TestInsertResultAndLatest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	r := db.ScanResult{
		ScanRunID:   run.ID,
		URLValue:    "https://example.com",
		DNSServerID: srv.ID,
		Compliant:   false,
		ResolvedIP:  "93.184.216.34",
		ScannedAt:   time.Now(),
	}
	if err := s.InsertResult(ctx, r); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	results, err := s.LatestResults(ctx)
	if err != nil {
		t.Fatalf("LatestResults: %v", err)
	}
	if len(results) != 1 || results[0].URLValue != "https://example.com" {
		t.Fatalf("expected 1 result, got %v", results)
	}
}

func TestLastScanRun_None(t *testing.T) {
	s := newTestStore(t)
	run, err := s.LastScanRun(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run != nil {
		t.Fatalf("expected nil, got %+v", run)
	}
}

func TestLastScanRun_ReturnsLatest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, _ := s.CreateScanRun(ctx, "scheduled")
	_ = s.CompleteScanRun(ctx, first.ID, "completed", time.Now())
	time.Sleep(2 * time.Millisecond)
	second, _ := s.CreateScanRun(ctx, "manual")

	got, err := s.LastScanRun(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID != second.ID {
		t.Fatalf("expected run id=%d, got %+v", second.ID, got)
	}
}

func TestScanProgress_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.CreateDNSServer(ctx, db.DNSServer{Name: "CF", Address: "1.1.1.1:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	entries, err := s.ScanProgress(ctx, run.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Completed != 0 {
		t.Fatalf("expected 0 completed, got %d", entries[0].Completed)
	}
}

func TestScanProgress_WithResults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv1, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "CF", Address: "1.1.1.1:53", Protocol: "udp"})
	srv2, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "Google", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	for _, url := range []string{"https://a.com", "https://b.com"} {
		_ = s.InsertResult(ctx, db.ScanResult{
			ScanRunID: run.ID, URLValue: url, DNSServerID: srv1.ID,
			Compliant: false, ScannedAt: time.Now(),
		})
	}
	_ = s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://a.com", DNSServerID: srv2.ID,
		Compliant: false, ScannedAt: time.Now(),
	})

	entries, err := s.ScanProgress(ctx, run.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byName := make(map[string]int)
	for _, e := range entries {
		byName[e.Name] = e.Completed
	}
	if byName["CF"] != 2 {
		t.Fatalf("expected CF=2, got %d", byName["CF"])
	}
	if byName["Google"] != 1 {
		t.Fatalf("expected Google=1, got %d", byName["Google"])
	}
}

func TestUpdateScreenshot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	r := db.ScanResult{
		ScanRunID:   run.ID,
		URLValue:    "https://x.com",
		DNSServerID: srv.ID,
		Compliant:   false,
		ScannedAt:   time.Now(),
	}
	_ = s.InsertResult(ctx, r)

	results, _ := s.LatestResults(ctx)
	if err := s.UpdateScreenshot(ctx, results[0].ID, "http://minio/screenshots/abc.png"); err != nil {
		t.Fatalf("UpdateScreenshot: %v", err)
	}

	results, _ = s.LatestResults(ctx)
	if results[0].ScreenshotURL != "http://minio/screenshots/abc.png" {
		t.Fatalf("expected screenshot URL, got %q", results[0].ScreenshotURL)
	}
}

func TestResultsByURL_FiltersSince(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	old := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now().AddDate(0, 0, -10),
	}
	recent := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: true, ScannedAt: time.Now(),
	}
	if err := s.InsertResult(ctx, old); err != nil {
		t.Fatalf("InsertResult old: %v", err)
	}
	if err := s.InsertResult(ctx, recent); err != nil {
		t.Fatalf("InsertResult recent: %v", err)
	}

	since := time.Now().AddDate(0, 0, -7)
	results, err := s.ResultsByURL(ctx, "https://example.com", since, time.Time{})
	if err != nil {
		t.Fatalf("ResultsByURL: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after since filter, got %d", len(results))
	}
	if !results[0].Compliant {
		t.Fatalf("expected the recent compliant result, got %+v", results[0])
	}
}

func TestResultsByURL_ZeroTimeReturnsAll(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	old := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now().AddDate(-1, 0, 0),
	}
	if err := s.InsertResult(ctx, old); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	results, err := s.ResultsByURL(ctx, "https://example.com", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("ResultsByURL: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with zero-time (unbounded), got %d", len(results))
	}
}

func TestResultsByURL_FiltersUntil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	inWindow := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: true, ScannedAt: time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
	}
	afterWindow := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	if err := s.InsertResult(ctx, inWindow); err != nil {
		t.Fatalf("InsertResult inWindow: %v", err)
	}
	if err := s.InsertResult(ctx, afterWindow); err != nil {
		t.Fatalf("InsertResult afterWindow: %v", err)
	}

	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	results, err := s.ResultsByURL(ctx, "https://example.com", since, until)
	if err != nil {
		t.Fatalf("ResultsByURL: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result within 2025 window, got %d", len(results))
	}
	if !results[0].Compliant {
		t.Fatalf("expected the in-window 2025 result, got %+v", results[0])
	}
}

func TestDailyComplianceByURL_GroupsAndComputesLevel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "Google", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	day := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	results := []db.ScanResult{
		{ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID, Compliant: true, ScannedAt: day.Add(1 * time.Hour)},
		{ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID, Compliant: false, ScannedAt: day.Add(2 * time.Hour)},
		{ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID, Compliant: false, ScannedAt: day.Add(3 * time.Hour)},
		// outside the requested window entirely
		{ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID, Compliant: true, ScannedAt: day.AddDate(0, 0, -30)},
	}
	for _, r := range results {
		if err := s.InsertResult(ctx, r); err != nil {
			t.Fatalf("InsertResult: %v", err)
		}
	}

	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 6, 30, 23, 59, 59, 0, time.UTC)
	stats, err := s.DailyComplianceByURL(ctx, "https://example.com", since, until)
	if err != nil {
		t.Fatalf("DailyComplianceByURL: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 day bucket within the window, got %d: %+v", len(stats), stats)
	}

	stat := stats[0]
	if stat.DNSServerID != srv.ID {
		t.Fatalf("expected dns_server_id %d, got %d", srv.ID, stat.DNSServerID)
	}
	if stat.DNSServerName != "Google" {
		t.Fatalf("expected dns_server_name %q, got %q", "Google", stat.DNSServerName)
	}
	if stat.Total != 3 || stat.Compliant != 1 {
		t.Fatalf("expected total=3 compliant=1, got total=%d compliant=%d", stat.Total, stat.Compliant)
	}
	// 2 of 3 violations -> rate 0.667, falls in the ">2/3" bucket only if >2/3;
	// 2/3 exactly is <= 2/3 -> level 3 (medium), per db.DailyComplianceLevel.
	if stat.Level != 3 {
		t.Fatalf("expected level 3, got %d", stat.Level)
	}
}

func TestAddURLToWatchlist_LinksExistingURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cmod, _ := s.CreateDepartment(ctx, "CMOD")
	crd, _ := s.CreateDepartment(ctx, "CRD")

	a, err := s.AddURLToWatchlist(ctx, cmod.ID, "https://Example.com/")
	if err != nil {
		t.Fatalf("AddURLToWatchlist (cmod): %v", err)
	}
	b, err := s.AddURLToWatchlist(ctx, crd.ID, "example.com")
	if err != nil {
		t.Fatalf("AddURLToWatchlist (crd): %v", err)
	}
	if a.ID != b.ID {
		t.Fatalf("expected both departments to link the same URL row, got ids %d and %d", a.ID, b.ID)
	}

	urls, _ := s.ListURLs(ctx)
	if len(urls) != 1 {
		t.Fatalf("expected exactly 1 url row, got %d", len(urls))
	}

	cmodURLs, _ := s.ListDepartmentURLs(ctx, cmod.ID)
	if len(cmodURLs) != 1 {
		t.Fatalf("expected CMOD to see 1 watched url, got %d", len(cmodURLs))
	}
	crdURLs, _ := s.ListDepartmentURLs(ctx, crd.ID)
	if len(crdURLs) != 1 {
		t.Fatalf("expected CRD to see 1 watched url, got %d", len(crdURLs))
	}
}

func TestAddURLToWatchlist_ReAddIsNoOp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cmod, _ := s.CreateDepartment(ctx, "CMOD")
	if _, err := s.AddURLToWatchlist(ctx, cmod.ID, "example.com"); err != nil {
		t.Fatalf("AddURLToWatchlist: %v", err)
	}
	if _, err := s.AddURLToWatchlist(ctx, cmod.ID, "example.com"); err != nil {
		t.Fatalf("AddURLToWatchlist (re-add): %v", err)
	}
	urls, _ := s.ListDepartmentURLs(ctx, cmod.ID)
	if len(urls) != 1 {
		t.Fatalf("expected re-adding the same domain to be a no-op, got %d watched urls", len(urls))
	}
}

func TestRemoveURLFromWatchlist_KeepsURLAndHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cmod, _ := s.CreateDepartment(ctx, "CMOD")
	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "Google", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	u, err := s.AddURLToWatchlist(ctx, cmod.ID, "example.com")
	if err != nil {
		t.Fatalf("AddURLToWatchlist: %v", err)
	}
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: true, ScannedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	removed, err := s.RemoveURLFromWatchlist(ctx, cmod.ID, u.ID)
	if err != nil {
		t.Fatalf("RemoveURLFromWatchlist: %v", err)
	}
	if !removed {
		t.Fatal("expected RemoveURLFromWatchlist to report a row was removed")
	}

	// URL row and its scan history must survive removal from the watchlist.
	urls, _ := s.ListURLs(ctx)
	if len(urls) != 1 {
		t.Fatalf("expected the URL row to survive watchlist removal, got %d urls", len(urls))
	}
	results, _ := s.ResultsByURL(ctx, "example.com", time.Time{}, time.Time{})
	if len(results) != 1 {
		t.Fatalf("expected scan history to survive watchlist removal, got %d results", len(results))
	}

	// But it should no longer be on anyone's watchlist or in the active scan set.
	deptURLs, _ := s.ListDepartmentURLs(ctx, cmod.ID)
	if len(deptURLs) != 0 {
		t.Fatalf("expected 0 watched urls for cmod after removal, got %d", len(deptURLs))
	}
	watched, _ := s.ListWatchedURLs(ctx)
	if len(watched) != 0 {
		t.Fatalf("expected ListWatchedURLs to exclude the removed domain, got %d", len(watched))
	}
	unassigned, _ := s.ListUnassignedURLs(ctx)
	if len(unassigned) != 1 {
		t.Fatalf("expected the orphaned domain to show up in ListUnassignedURLs, got %d", len(unassigned))
	}
}

func TestRemoveURLFromWatchlist_NotOwnedReturnsFalse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cmod, _ := s.CreateDepartment(ctx, "CMOD")
	crd, _ := s.CreateDepartment(ctx, "CRD")
	u, _ := s.AddURLToWatchlist(ctx, cmod.ID, "example.com")

	removed, err := s.RemoveURLFromWatchlist(ctx, crd.ID, u.ID)
	if err != nil {
		t.Fatalf("RemoveURLFromWatchlist: %v", err)
	}
	if removed {
		t.Fatal("expected false — CRD never watched this domain")
	}
	// CMOD's link must be untouched.
	cmodURLs, _ := s.ListDepartmentURLs(ctx, cmod.ID)
	if len(cmodURLs) != 1 {
		t.Fatalf("expected CMOD's watchlist link to be untouched, got %d", len(cmodURLs))
	}
}

func TestListWatchedURLs_ExcludesOrphaned(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// A pre-existing URL with no department watching it (e.g. a pre-migration row).
	if _, err := s.CreateURL(ctx, "orphan.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	cmod, _ := s.CreateDepartment(ctx, "CMOD")
	if _, err := s.AddURLToWatchlist(ctx, cmod.ID, "watched.com"); err != nil {
		t.Fatalf("AddURLToWatchlist: %v", err)
	}

	watched, err := s.ListWatchedURLs(ctx)
	if err != nil {
		t.Fatalf("ListWatchedURLs: %v", err)
	}
	if len(watched) != 1 || watched[0].URL != "watched.com" {
		t.Fatalf("expected only watched.com, got %v", watched)
	}
}

func TestURLOwnedByDepartment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cmod, _ := s.CreateDepartment(ctx, "CMOD")
	crd, _ := s.CreateDepartment(ctx, "CRD")
	if _, err := s.AddURLToWatchlist(ctx, cmod.ID, "example.com"); err != nil {
		t.Fatalf("AddURLToWatchlist: %v", err)
	}

	owned, err := s.URLOwnedByDepartment(ctx, cmod.ID, "https://example.com/")
	if err != nil {
		t.Fatalf("URLOwnedByDepartment: %v", err)
	}
	if !owned {
		t.Fatal("expected CMOD to own example.com")
	}

	owned, err = s.URLOwnedByDepartment(ctx, crd.ID, "example.com")
	if err != nil {
		t.Fatalf("URLOwnedByDepartment: %v", err)
	}
	if owned {
		t.Fatal("expected CRD to NOT own example.com")
	}
}

func TestCreateUserAndGetByUsernameAndID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "CMOD")
	deptID := dept.ID
	created, err := s.CreateUser(ctx, db.User{Username: "alice", PasswordHash: "hash", DepartmentID: &deptID})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	byUsername, err := s.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if byUsername == nil || byUsername.ID != created.ID {
		t.Fatalf("expected to find the created user by username, got %v", byUsername)
	}
	if byUsername.Department == nil || byUsername.Department.Name != "CMOD" {
		t.Fatalf("expected Department to be preloaded, got %v", byUsername.Department)
	}

	byID, err := s.GetUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if byID == nil || byID.Username != "alice" {
		t.Fatalf("expected to find the created user by id, got %v", byID)
	}

	missing, err := s.GetUserByID(ctx, 99999)
	if err != nil {
		t.Fatalf("GetUserByID for missing id should not error: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for a non-existent user id, got %v", missing)
	}
}

func TestSetURLEnabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "example.com")

	// disable
	found, err := s.SetURLEnabled(ctx, dept.ID, u.ID, false)
	if err != nil || !found {
		t.Fatalf("SetURLEnabled(false): found=%v err=%v", found, err)
	}

	// ListWatchedURLs must exclude it
	urls, err := s.ListWatchedURLs(ctx)
	if err != nil {
		t.Fatalf("ListWatchedURLs: %v", err)
	}
	for _, wu := range urls {
		if wu.ID == u.ID {
			t.Fatal("disabled URL should not appear in ListWatchedURLs")
		}
	}

	// re-enable
	found, err = s.SetURLEnabled(ctx, dept.ID, u.ID, true)
	if err != nil || !found {
		t.Fatalf("SetURLEnabled(true): found=%v err=%v", found, err)
	}
	urls, _ = s.ListWatchedURLs(ctx)
	var seen bool
	for _, wu := range urls {
		if wu.ID == u.ID {
			seen = true
		}
	}
	if !seen {
		t.Fatal("re-enabled URL should appear in ListWatchedURLs")
	}
}

func TestSetURLEnabledNotOnWatchlist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept2")
	u, _ := s.CreateURL(ctx, "notlinked.com")

	found, err := s.SetURLEnabled(ctx, dept.ID, u.ID, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for URL not on watchlist")
	}
}

func TestListDepartmentURLsReturnsEnabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept3")
	_, _ = s.AddURLToWatchlist(ctx, dept.ID, "alpha.com")
	u2, _ := s.AddURLToWatchlist(ctx, dept.ID, "beta.com")
	_, _ = s.SetURLEnabled(ctx, dept.ID, u2.ID, false)

	entries, err := s.ListDepartmentURLs(ctx, dept.ID)
	if err != nil {
		t.Fatalf("ListDepartmentURLs: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.URL == "alpha.com" && !e.Enabled {
			t.Error("alpha.com should be enabled")
		}
		if e.URL == "beta.com" && e.Enabled {
			t.Error("beta.com should be disabled")
		}
	}
}

func TestListWatchedURLsDeduplicatesAcrossDepartments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d1, _ := s.CreateDepartment(ctx, "D1")
	d2, _ := s.CreateDepartment(ctx, "D2")
	_, _ = s.AddURLToWatchlist(ctx, d1.ID, "shared.com")
	_, _ = s.AddURLToWatchlist(ctx, d2.ID, "shared.com")

	urls, err := s.ListWatchedURLs(ctx)
	if err != nil {
		t.Fatalf("ListWatchedURLs: %v", err)
	}
	count := 0
	for _, u := range urls {
		if u.URL == "shared.com" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want shared.com once, got %d times", count)
	}
}

func TestListWatchedURLsEnabledByAnyDept(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d1, _ := s.CreateDepartment(ctx, "E1")
	d2, _ := s.CreateDepartment(ctx, "E2")
	u, _ := s.AddURLToWatchlist(ctx, d1.ID, "shared.com")
	_, _ = s.AddURLToWatchlist(ctx, d2.ID, "shared.com")
	// d1 disables it, d2 has it enabled — should still appear
	_, _ = s.SetURLEnabled(ctx, d1.ID, u.ID, false)

	urls, err := s.ListWatchedURLs(ctx)
	if err != nil {
		t.Fatalf("ListWatchedURLs: %v", err)
	}
	var seen bool
	for _, wu := range urls {
		if wu.ID == u.ID {
			seen = true
		}
	}
	if !seen {
		t.Fatal("URL enabled by any department should still appear in ListWatchedURLs")
	}
}
