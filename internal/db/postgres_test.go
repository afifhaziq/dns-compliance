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
	if len(urls) != 1 || urls[0].URL != "https://example.com" {
		t.Fatalf("expected 1 url, got %v", urls)
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
