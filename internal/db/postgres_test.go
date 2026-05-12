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
