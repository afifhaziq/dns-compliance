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

func TestLatestResults_OnlyLatestScanRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})

	firstRun, _ := s.CreateScanRun(ctx, "manual")
	_ = s.InsertResult(ctx, db.ScanResult{
		ScanRunID: firstRun.ID, URLValue: "https://a.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now(),
	})

	time.Sleep(2 * time.Millisecond)
	secondRun, _ := s.CreateScanRun(ctx, "manual")
	_ = s.InsertResult(ctx, db.ScanResult{
		ScanRunID: secondRun.ID, URLValue: "https://b.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now(),
	})

	results, err := s.LatestResults(ctx)
	if err != nil {
		t.Fatalf("LatestResults: %v", err)
	}
	if len(results) != 1 || results[0].URLValue != "https://b.com" {
		t.Fatalf("expected only https://b.com from the latest run, got %v", results)
	}
}

func TestLatestResults_IgnoresScreenshotRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})

	sweepRun, _ := s.CreateScanRun(ctx, "manual")
	_ = s.InsertResult(ctx, db.ScanResult{
		ScanRunID: sweepRun.ID, URLValue: "https://a.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now(),
	})
	_ = s.InsertResult(ctx, db.ScanResult{
		ScanRunID: sweepRun.ID, URLValue: "https://b.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now(),
	})

	time.Sleep(2 * time.Millisecond)
	screenshotRun, _ := s.CreateScanRun(ctx, "screenshot")
	_ = s.InsertResult(ctx, db.ScanResult{
		ScanRunID: screenshotRun.ID, URLValue: "https://a.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now(),
	})

	results, err := s.LatestResults(ctx)
	if err != nil {
		t.Fatalf("LatestResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected both sweep results still visible after a screenshot run, got %v", results)
	}
}

func TestLastScanRun_ExcludesScreenshotRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	manual, _ := s.CreateScanRun(ctx, "manual")
	_ = s.CompleteScanRun(ctx, manual.ID, "completed", time.Now())

	time.Sleep(2 * time.Millisecond)
	_, _ = s.CreateScanRun(ctx, "screenshot")

	got, err := s.LastScanRun(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID != manual.ID {
		t.Fatalf("expected the manual run (id=%d), got %+v", manual.ID, got)
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

func TestSetURLOrderedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept4")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "example.com")

	orderedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	found, err := s.SetURLOrderedAt(ctx, dept.ID, u.ID, &orderedAt)
	if err != nil || !found {
		t.Fatalf("SetURLOrderedAt(set): found=%v err=%v", found, err)
	}

	entries, _ := s.ListDepartmentURLs(ctx, dept.ID)
	if len(entries) != 1 || entries[0].OrderedAt == nil || !entries[0].OrderedAt.Equal(orderedAt) {
		t.Fatalf("expected ordered_at to be set, got %+v", entries)
	}

	// Clear it
	found, err = s.SetURLOrderedAt(ctx, dept.ID, u.ID, nil)
	if err != nil || !found {
		t.Fatalf("SetURLOrderedAt(clear): found=%v err=%v", found, err)
	}
	entries, _ = s.ListDepartmentURLs(ctx, dept.ID)
	if len(entries) != 1 || entries[0].OrderedAt != nil {
		t.Fatalf("expected ordered_at to be cleared, got %+v", entries)
	}
}

func TestSetURLOrderedAtNotOnWatchlist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept5")
	u, _ := s.CreateURL(ctx, "notlinked2.com")

	orderedAt := time.Now()
	found, err := s.SetURLOrderedAt(ctx, dept.ID, u.ID, &orderedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for URL not on watchlist")
	}
}

func TestISPComplianceTiming_BlockedAndStillOpen(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "TelCo", Name: "TelCo DNS", Address: "1.2.3.4:53", Protocol: "udp"})
	dept, _ := s.CreateDepartment(ctx, "TimingDept")
	run, _ := s.CreateScanRun(ctx, "manual")

	blocked, _ := s.AddURLToWatchlist(ctx, dept.ID, "blocked.com")
	stillOpen, _ := s.AddURLToWatchlist(ctx, dept.ID, "open.com")
	noOrderDate, _ := s.AddURLToWatchlist(ctx, dept.ID, "noorder.com")

	blockedOrder := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	openOrder := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.SetURLOrderedAt(ctx, dept.ID, blocked.ID, &blockedOrder); err != nil {
		t.Fatalf("SetURLOrderedAt blocked: %v", err)
	}
	if _, err := s.SetURLOrderedAt(ctx, dept.ID, stillOpen.ID, &openOrder); err != nil {
		t.Fatalf("SetURLOrderedAt open: %v", err)
	}
	_ = noOrderDate

	// blocked.com: compliant 3 days after order
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: blocked.ID, URLValue: blocked.URL, DNSServerID: srv.ID,
		Compliant: true, ScannedAt: blockedOrder.AddDate(0, 0, 3),
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}
	// open.com: never compliant (still open) — no result inserted, or a non-compliant one
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: stillOpen.ID, URLValue: stillOpen.URL, DNSServerID: srv.ID,
		Compliant: false, ScannedAt: openOrder.AddDate(0, 0, 1),
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	timing, err := s.ISPComplianceTiming(ctx, "TelCo")
	if err != nil {
		t.Fatalf("ISPComplianceTiming: %v", err)
	}
	if timing.BlockedCount != 1 {
		t.Fatalf("expected 1 blocked domain, got %d", timing.BlockedCount)
	}
	if timing.StillOpenCount != 1 {
		t.Fatalf("expected 1 still-open domain, got %d", timing.StillOpenCount)
	}
	if timing.WithOrderDateCount != 2 {
		t.Fatalf("expected 2 domains with an order date, got %d", timing.WithOrderDateCount)
	}
	if timing.TotalDomains != 3 {
		t.Fatalf("expected 3 total monitored domains, got %d", timing.TotalDomains)
	}
	if timing.MedianDaysToBlock != 3 {
		t.Fatalf("expected median 3 days, got %v", timing.MedianDaysToBlock)
	}

	deptTiming, err := s.ISPComplianceTimingForDepartment(ctx, "TelCo", dept.ID)
	if err != nil {
		t.Fatalf("ISPComplianceTimingForDepartment: %v", err)
	}
	if deptTiming.BlockedCount != 1 || deptTiming.StillOpenCount != 1 {
		t.Fatalf("expected department-scoped timing to match global scope here, got %+v", deptTiming)
	}
}

func TestISPComplianceTiming_NegativeClampedToZero(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "TelCo2", Name: "TelCo2 DNS", Address: "1.2.3.5:53", Protocol: "udp"})
	dept, _ := s.CreateDepartment(ctx, "TimingDept2")
	run, _ := s.CreateScanRun(ctx, "manual")

	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "alreadyblocked.com")
	// Compliant scan happened BEFORE the recorded order date.
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	orderedAt := early.AddDate(0, 0, 5)
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: true, ScannedAt: early,
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}
	if _, err := s.SetURLOrderedAt(ctx, dept.ID, u.ID, &orderedAt); err != nil {
		t.Fatalf("SetURLOrderedAt: %v", err)
	}

	timing, err := s.ISPComplianceTiming(ctx, "TelCo2")
	if err != nil {
		t.Fatalf("ISPComplianceTiming: %v", err)
	}
	// The only compliant scan predates the order date, so it's still "open"
	// from the order's perspective (no valid block event recorded after it).
	if timing.StillOpenCount != 1 || timing.BlockedCount != 0 {
		t.Fatalf("expected still-open (pre-order compliant scan doesn't count), got blocked=%d open=%d", timing.BlockedCount, timing.StillOpenCount)
	}
}

func TestNationalTrend_AggregatesAcrossISPs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv1, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "ISP1", Name: "S1", Address: "1.1.1.1:53", Protocol: "udp"})
	srv2, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "ISP2", Name: "S2", Address: "2.2.2.2:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	day := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	results := []db.ScanResult{
		{ScanRunID: run.ID, URLValue: "https://a.com", DNSServerID: srv1.ID, Compliant: true, ScannedAt: day},
		{ScanRunID: run.ID, URLValue: "https://b.com", DNSServerID: srv2.ID, Compliant: false, ScannedAt: day},
	}
	for _, r := range results {
		if err := s.InsertResult(ctx, r); err != nil {
			t.Fatalf("InsertResult: %v", err)
		}
	}

	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 6, 30, 23, 59, 59, 0, time.UTC)
	stats, err := s.NationalTrend(ctx, since, until)
	if err != nil {
		t.Fatalf("NationalTrend: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 day bucket, got %d: %+v", len(stats), stats)
	}
	if stats[0].Total != 2 || stats[0].Compliant != 1 {
		t.Fatalf("expected total=2 compliant=1 across both ISPs, got total=%d compliant=%d", stats[0].Total, stats[0].Compliant)
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

// The four tests below cover LatestResults, ISPStats, ISPTrend, and
// CountDepartmentURLsSince after collapsing each *ForDepartment variant into
// a shared private helper (see postgres.go) — asserting the department scope
// actually still filters correctly, not just that the code compiles.

func TestLatestResultsForDepartment_ScopesToWatchlist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	d1, _ := s.CreateDepartment(ctx, "ScopeD1")
	d2, _ := s.CreateDepartment(ctx, "ScopeD2")
	u, _ := s.AddURLToWatchlist(ctx, d1.ID, "scoped.com")

	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	d1Results, err := s.LatestResultsForDepartment(ctx, d1.ID)
	if err != nil {
		t.Fatalf("LatestResultsForDepartment(d1): %v", err)
	}
	if len(d1Results) != 1 {
		t.Fatalf("expected 1 result for d1 (owns the watchlist entry), got %d", len(d1Results))
	}

	d2Results, err := s.LatestResultsForDepartment(ctx, d2.ID)
	if err != nil {
		t.Fatalf("LatestResultsForDepartment(d2): %v", err)
	}
	if len(d2Results) != 0 {
		t.Fatalf("expected 0 results for d2 (no watchlist entry), got %d", len(d2Results))
	}
}

func TestISPStats_ScopesToDepartmentWatchlist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "ScopeISP", Name: "ScopeISP DNS", Address: "9.9.9.9:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	owner, _ := s.CreateDepartment(ctx, "StatsOwner")
	other, _ := s.CreateDepartment(ctx, "StatsOther")
	u, _ := s.AddURLToWatchlist(ctx, owner.ID, "isp-scope.com")

	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	global, err := s.ISPStats(ctx, "ScopeISP")
	if err != nil {
		t.Fatalf("ISPStats: %v", err)
	}
	if len(global.Servers) != 1 || global.Servers[0].Total != 1 {
		t.Fatalf("expected 1 server with 1 total scan globally, got %+v", global)
	}

	ownerStats, err := s.ISPStatsForDepartment(ctx, "ScopeISP", owner.ID)
	if err != nil {
		t.Fatalf("ISPStatsForDepartment(owner): %v", err)
	}
	if len(ownerStats.Servers) != 1 || ownerStats.Servers[0].Total != 1 {
		t.Fatalf("expected owning department to see the scan, got %+v", ownerStats)
	}

	otherStats, err := s.ISPStatsForDepartment(ctx, "ScopeISP", other.ID)
	if err != nil {
		t.Fatalf("ISPStatsForDepartment(other): %v", err)
	}
	if len(otherStats.Servers) != 0 {
		t.Fatalf("expected non-owning department to see no servers, got %+v", otherStats)
	}
}

func TestISPTrend_ScopesToDepartmentWatchlist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "TrendISP", Name: "TrendISP DNS", Address: "9.9.9.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	owner, _ := s.CreateDepartment(ctx, "TrendOwner")
	other, _ := s.CreateDepartment(ctx, "TrendOther")
	u, _ := s.AddURLToWatchlist(ctx, owner.ID, "trend-scope.com")

	scanTime := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: true, ScannedAt: scanTime,
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 6, 30, 23, 59, 59, 0, time.UTC)

	global, err := s.ISPTrend(ctx, "TrendISP", since, until)
	if err != nil {
		t.Fatalf("ISPTrend: %v", err)
	}
	if len(global) != 1 || global[0].Total != 1 || global[0].Compliant != 1 {
		t.Fatalf("expected 1 day bucket total=1 compliant=1 globally, got %+v", global)
	}

	ownerTrend, err := s.ISPTrendForDepartment(ctx, "TrendISP", since, until, owner.ID)
	if err != nil {
		t.Fatalf("ISPTrendForDepartment(owner): %v", err)
	}
	if len(ownerTrend) != 1 || ownerTrend[0].Total != 1 {
		t.Fatalf("expected owning department to see the scan, got %+v", ownerTrend)
	}

	otherTrend, err := s.ISPTrendForDepartment(ctx, "TrendISP", since, until, other.ID)
	if err != nil {
		t.Fatalf("ISPTrendForDepartment(other): %v", err)
	}
	if len(otherTrend) != 0 {
		t.Fatalf("expected non-owning department to see no trend data, got %+v", otherTrend)
	}
}

func TestCountDepartmentURLsSince_ScopesToDepartment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d1, _ := s.CreateDepartment(ctx, "CountD1")
	d2, _ := s.CreateDepartment(ctx, "CountD2")
	if _, err := s.AddURLToWatchlist(ctx, d1.ID, "count-a.com"); err != nil {
		t.Fatalf("AddURLToWatchlist: %v", err)
	}
	if _, err := s.AddURLToWatchlist(ctx, d1.ID, "count-b.com"); err != nil {
		t.Fatalf("AddURLToWatchlist: %v", err)
	}
	if _, err := s.AddURLToWatchlist(ctx, d2.ID, "count-c.com"); err != nil {
		t.Fatalf("AddURLToWatchlist: %v", err)
	}

	since := time.Now().Add(-1 * time.Hour)

	total, err := s.CountDepartmentURLsSince(ctx, since)
	if err != nil {
		t.Fatalf("CountDepartmentURLsSince: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 watchlist additions globally, got %d", total)
	}

	d1Count, err := s.CountDepartmentURLsSinceForDepartment(ctx, since, d1.ID)
	if err != nil {
		t.Fatalf("CountDepartmentURLsSinceForDepartment(d1): %v", err)
	}
	if d1Count != 2 {
		t.Fatalf("expected 2 watchlist additions for d1, got %d", d1Count)
	}

	d2Count, err := s.CountDepartmentURLsSinceForDepartment(ctx, since, d2.ID)
	if err != nil {
		t.Fatalf("CountDepartmentURLsSinceForDepartment(d2): %v", err)
	}
	if d2Count != 1 {
		t.Fatalf("expected 1 watchlist addition for d2, got %d", d2Count)
	}
}

func TestResurfacedDomains_DetectsComplianceToViolationFlip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "ResurfaceISP", Name: "Resurface DNS", Address: "9.9.9.7:53", Protocol: "udp"})
	run1, _ := s.CreateScanRun(ctx, "manual")
	run2, _ := s.CreateScanRun(ctx, "manual")
	dept, _ := s.CreateDepartment(ctx, "ResurfaceDept")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "resurface-flip.com")

	t1 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run1.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: true, ScannedAt: t1,
	}); err != nil {
		t.Fatalf("InsertResult (compliant): %v", err)
	}
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run2.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: false, ScannedAt: t2,
	}); err != nil {
		t.Fatalf("InsertResult (violation): %v", err)
	}

	domains, err := s.ResurfacedDomains(ctx)
	if err != nil {
		t.Fatalf("ResurfacedDomains: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("expected 1 resurfaced domain, got %d: %+v", len(domains), domains)
	}
	d := domains[0]
	if d.URLValue != "resurface-flip.com" {
		t.Fatalf("expected resurface-flip.com, got %q", d.URLValue)
	}
	if len(d.AffectedServers) != 1 || d.AffectedServers[0].DNSServerID != srv.ID {
		t.Fatalf("expected 1 affected server matching srv.ID, got %+v", d.AffectedServers)
	}
	if !d.AffectedServers[0].LastCompliantAt.Equal(t1) {
		t.Fatalf("expected last_compliant_at %v, got %v", t1, d.AffectedServers[0].LastCompliantAt)
	}
	if !d.AffectedServers[0].ResurfacedAt.Equal(t2) {
		t.Fatalf("expected resurfaced_at %v, got %v", t2, d.AffectedServers[0].ResurfacedAt)
	}
}

func TestResurfacedDomains_IgnoresNonRegressions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "NonRegressISP", Name: "NonRegress DNS", Address: "9.9.9.6:53", Protocol: "udp"})
	dept, _ := s.CreateDepartment(ctx, "NonRegressDept")

	t1 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	insert := func(hostname string, c1, c2 bool) {
		run1, _ := s.CreateScanRun(ctx, "manual")
		run2, _ := s.CreateScanRun(ctx, "manual")
		u, _ := s.AddURLToWatchlist(ctx, dept.ID, hostname)
		if err := s.InsertResult(ctx, db.ScanResult{
			ScanRunID: run1.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
			Compliant: c1, ScannedAt: t1,
		}); err != nil {
			t.Fatalf("InsertResult 1 for %s: %v", hostname, err)
		}
		if err := s.InsertResult(ctx, db.ScanResult{
			ScanRunID: run2.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
			Compliant: c2, ScannedAt: t2,
		}); err != nil {
			t.Fatalf("InsertResult 2 for %s: %v", hostname, err)
		}
	}

	insert("still-compliant.com", true, true)   // still blocked — not a regression
	insert("still-violating.com", false, false) // already known-bad — not a new regression
	insert("just-reblocked.com", false, true)   // got re-blocked — the opposite of resurfacing

	domains, err := s.ResurfacedDomains(ctx)
	if err != nil {
		t.Fatalf("ResurfacedDomains: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("expected 0 resurfaced domains for non-regression cases, got %d: %+v", len(domains), domains)
	}
}

func TestResurfacedDomains_RollsUpMultipleServersIntoOneDomainRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srvA, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "RollupISP", Name: "Rollup DNS A", Address: "9.9.9.5:53", Protocol: "udp"})
	srvB, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "RollupISP", Name: "Rollup DNS B", Address: "9.9.9.4:53", Protocol: "udp"})
	dept, _ := s.CreateDepartment(ctx, "RollupDept")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "rollup-multi.com")

	t1 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	for _, srv := range []db.DNSServer{srvA, srvB} {
		run1, _ := s.CreateScanRun(ctx, "manual")
		run2, _ := s.CreateScanRun(ctx, "manual")
		if err := s.InsertResult(ctx, db.ScanResult{
			ScanRunID: run1.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
			Compliant: true, ScannedAt: t1,
		}); err != nil {
			t.Fatalf("InsertResult 1 for server %d: %v", srv.ID, err)
		}
		if err := s.InsertResult(ctx, db.ScanResult{
			ScanRunID: run2.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
			Compliant: false, ScannedAt: t2,
		}); err != nil {
			t.Fatalf("InsertResult 2 for server %d: %v", srv.ID, err)
		}
	}

	domains, err := s.ResurfacedDomains(ctx)
	if err != nil {
		t.Fatalf("ResurfacedDomains: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("expected 1 resurfaced domain row (rolled up), got %d: %+v", len(domains), domains)
	}
	if len(domains[0].AffectedServers) != 2 {
		t.Fatalf("expected 2 affected servers on the rolled-up domain, got %d: %+v", len(domains[0].AffectedServers), domains[0].AffectedServers)
	}
}

func TestResurfacedDomains_ScopesToDepartmentWatchlist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "ScopeResurfaceISP", Name: "ScopeResurface DNS", Address: "9.9.9.3:53", Protocol: "udp"})
	owner, _ := s.CreateDepartment(ctx, "ResurfaceOwner")
	other, _ := s.CreateDepartment(ctx, "ResurfaceOther")
	u, _ := s.AddURLToWatchlist(ctx, owner.ID, "resurface-scope.com")

	t1 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	run1, _ := s.CreateScanRun(ctx, "manual")
	run2, _ := s.CreateScanRun(ctx, "manual")
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run1.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: true, ScannedAt: t1,
	}); err != nil {
		t.Fatalf("InsertResult 1: %v", err)
	}
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run2.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: false, ScannedAt: t2,
	}); err != nil {
		t.Fatalf("InsertResult 2: %v", err)
	}

	global, err := s.ResurfacedDomains(ctx)
	if err != nil {
		t.Fatalf("ResurfacedDomains: %v", err)
	}
	if len(global) != 1 {
		t.Fatalf("expected 1 resurfaced domain globally, got %d", len(global))
	}

	ownerDomains, err := s.ResurfacedDomainsForDepartment(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ResurfacedDomainsForDepartment(owner): %v", err)
	}
	if len(ownerDomains) != 1 {
		t.Fatalf("expected owning department to see the resurfaced domain, got %d", len(ownerDomains))
	}

	otherDomains, err := s.ResurfacedDomainsForDepartment(ctx, other.ID)
	if err != nil {
		t.Fatalf("ResurfacedDomainsForDepartment(other): %v", err)
	}
	if len(otherDomains) != 0 {
		t.Fatalf("expected non-owning department to see no resurfaced domains, got %d", len(otherDomains))
	}
}

func TestListDomainSummaries_AggregatesLifetimeScans(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "SummarySrv", Address: "9.9.9.5:53", Protocol: "udp"})
	d, _ := s.CreateDepartment(ctx, "SummaryDept")
	u, _ := s.AddURLToWatchlist(ctx, d.ID, "summary.com")

	t1 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	run1, _ := s.CreateScanRun(ctx, "manual")
	run2, _ := s.CreateScanRun(ctx, "manual")
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run1.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: true, ScannedAt: t1,
	}); err != nil {
		t.Fatalf("InsertResult 1: %v", err)
	}
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run2.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: false, ScannedAt: t2,
	}); err != nil {
		t.Fatalf("InsertResult 2: %v", err)
	}

	summaries, total, err := s.ListDomainSummaries(ctx, 1, 25)
	if err != nil {
		t.Fatalf("ListDomainSummaries: %v", err)
	}
	if total != 1 || len(summaries) != 1 {
		t.Fatalf("expected 1 domain, got total=%d len=%d", total, len(summaries))
	}
	got := summaries[0]
	if got.URLValue != "summary.com" || got.TotalScans != 2 || got.CompliantScans != 1 {
		t.Fatalf("unexpected summary: %+v", got)
	}
	if !got.LastScannedAt.Equal(t2) {
		t.Fatalf("expected LastScannedAt %v, got %v", t2, got.LastScannedAt)
	}
}

func TestListDomainSummariesForDepartment_IncludesDisabledDomain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "DisabledSrv", Address: "9.9.9.6:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	owner, _ := s.CreateDepartment(ctx, "DisabledOwner")
	other, _ := s.CreateDepartment(ctx, "DisabledOther")
	u, _ := s.AddURLToWatchlist(ctx, owner.ID, "disabled-history.com")

	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	// Disable the URL on the owning department's watchlist — it still has
	// history, so it must remain visible in the lookup table (unlike
	// ListWatchedURLs, which is enabled-only and feeds the scan sweep).
	if ok, err := s.SetURLEnabled(ctx, owner.ID, u.ID, false); err != nil || !ok {
		t.Fatalf("SetURLEnabled: ok=%v err=%v", ok, err)
	}

	ownerSummaries, total, err := s.ListDomainSummariesForDepartment(ctx, 1, 25, owner.ID)
	if err != nil {
		t.Fatalf("ListDomainSummariesForDepartment(owner): %v", err)
	}
	if total != 1 || len(ownerSummaries) != 1 {
		t.Fatalf("expected disabled-but-historied domain to still be listed, got total=%d len=%d", total, len(ownerSummaries))
	}

	otherSummaries, total, err := s.ListDomainSummariesForDepartment(ctx, 1, 25, other.ID)
	if err != nil {
		t.Fatalf("ListDomainSummariesForDepartment(other): %v", err)
	}
	if total != 0 || len(otherSummaries) != 0 {
		t.Fatalf("expected non-owning department to see nothing, got total=%d len=%d", total, len(otherSummaries))
	}
}

func TestDomainServerSummaries_AggregatesPerServer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srvA, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "ISP-A", Name: "ServerA", Address: "9.9.9.7:53", Protocol: "udp"})
	srvB, _ := s.CreateDNSServer(ctx, db.DNSServer{ISP: "ISP-B", Name: "ServerB", Address: "9.9.9.8:53", Protocol: "udp"})
	d, _ := s.CreateDepartment(ctx, "PerServerDept")
	u, _ := s.AddURLToWatchlist(ctx, d.ID, "per-server.com")
	other, _ := s.AddURLToWatchlist(ctx, d.ID, "unrelated.com")

	t1 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	run1, _ := s.CreateScanRun(ctx, "manual")
	run2, _ := s.CreateScanRun(ctx, "manual")

	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run1.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srvA.ID,
		Compliant: true, ScannedAt: t1,
	}); err != nil {
		t.Fatalf("InsertResult A1: %v", err)
	}
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run2.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srvA.ID,
		Compliant: false, ScannedAt: t2,
	}); err != nil {
		t.Fatalf("InsertResult A2: %v", err)
	}
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run1.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srvB.ID,
		Compliant: false, ScannedAt: t1,
	}); err != nil {
		t.Fatalf("InsertResult B1: %v", err)
	}
	// A result for a different domain must not leak into per-server rows.
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run1.ID, URLID: other.ID, URLValue: other.URL, DNSServerID: srvA.ID,
		Compliant: true, ScannedAt: t1,
	}); err != nil {
		t.Fatalf("InsertResult other-domain: %v", err)
	}

	summaries, err := s.DomainServerSummaries(ctx, "per-server.com")
	if err != nil {
		t.Fatalf("DomainServerSummaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 servers, got %d: %+v", len(summaries), summaries)
	}

	byServer := make(map[uint]db.DomainServerSummary, len(summaries))
	for _, sm := range summaries {
		byServer[sm.DNSServerID] = sm
	}

	a, ok := byServer[srvA.ID]
	if !ok {
		t.Fatalf("expected a row for ServerA, got %+v", summaries)
	}
	if a.ISP != "ISP-A" || a.Address != "9.9.9.7:53" || a.TotalScans != 2 || a.CompliantScans != 1 || !a.LastScannedAt.Equal(t2) {
		t.Fatalf("unexpected ServerA summary: %+v", a)
	}

	b, ok := byServer[srvB.ID]
	if !ok {
		t.Fatalf("expected a row for ServerB, got %+v", summaries)
	}
	if b.ISP != "ISP-B" || b.Address != "9.9.9.8:53" || b.TotalScans != 1 || b.CompliantScans != 0 || !b.LastScannedAt.Equal(t1) {
		t.Fatalf("unexpected ServerB summary: %+v", b)
	}
}
