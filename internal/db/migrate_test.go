package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// rawConnect mirrors newTestStore but hands back the raw *gorm.DB, needed to
// set up legacy "unnormalized data" fixtures that bypass CreateURL's
// normalization (CreateURL itself can no longer produce such rows).
func rawConnect(t *testing.T) (*gorm.DB, db.Store) {
	t.Helper()
	gormDB, err := db.Connect(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return gormDB, db.NewStore(gormDB)
}

func TestNormalizeAndDedupeURLs_NormalizesSingleRow(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	if err := gormDB.Create(&db.URL{URL: "https://Example.com/"}).Error; err != nil {
		t.Fatalf("seed legacy url: %v", err)
	}

	if err := db.NormalizeAndDedupeURLs(ctx, gormDB); err != nil {
		t.Fatalf("NormalizeAndDedupeURLs: %v", err)
	}

	urls, _ := s.ListURLs(ctx)
	if len(urls) != 1 || urls[0].URL != "example.com" {
		t.Fatalf("expected 1 normalized url, got %v", urls)
	}
}

func TestNormalizeAndDedupeURLs_MergesDuplicatesAndReassignsResults(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	older := db.URL{URL: "https://Example.com/", CreatedAt: time.Now()}
	if err := gormDB.Create(&older).Error; err != nil {
		t.Fatalf("seed older: %v", err)
	}
	newer := db.URL{URL: "example.com", CreatedAt: time.Now().Add(time.Hour)}
	if err := gormDB.Create(&newer).Error; err != nil {
		t.Fatalf("seed newer: %v", err)
	}

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: newer.ID, URLValue: newer.URL, DNSServerID: srv.ID,
		Compliant: true, ScannedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	cmod, _ := s.CreateDepartment(ctx, "CMOD")
	if err := gormDB.Create(&db.DepartmentURL{DepartmentID: cmod.ID, URLID: newer.ID}).Error; err != nil {
		t.Fatalf("seed department_url: %v", err)
	}

	if err := db.NormalizeAndDedupeURLs(ctx, gormDB); err != nil {
		t.Fatalf("NormalizeAndDedupeURLs: %v", err)
	}

	urls, _ := s.ListURLs(ctx)
	if len(urls) != 1 {
		t.Fatalf("expected duplicates merged into 1 url row, got %d: %v", len(urls), urls)
	}
	if urls[0].ID != older.ID {
		t.Fatalf("expected the older row (lowest ID) to survive as canonical, got id=%d", urls[0].ID)
	}

	results, _ := s.ResultsByURL(ctx, "example.com", time.Time{}, time.Time{})
	if len(results) != 1 {
		t.Fatalf("expected scan history to be reassigned to the canonical url, got %d results", len(results))
	}

	deptURLs, _ := s.ListDepartmentURLs(ctx, cmod.ID)
	if len(deptURLs) != 1 || deptURLs[0].ID != older.ID {
		t.Fatalf("expected the department's watchlist link to be remapped to the canonical url, got %v", deptURLs)
	}
}

func TestNormalizeAndDedupeURLs_Idempotent(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	if err := gormDB.Create(&db.URL{URL: "https://Example.com/"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.NormalizeAndDedupeURLs(ctx, gormDB); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := db.NormalizeAndDedupeURLs(ctx, gormDB); err != nil {
		t.Fatalf("second run: %v", err)
	}

	urls, _ := s.ListURLs(ctx)
	if len(urls) != 1 || urls[0].URL != "example.com" {
		t.Fatalf("expected idempotent result, got %v", urls)
	}
}

func TestBackfillURLValues_RewritesDivergedRows(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	u, err := s.CreateURL(ctx, "https://Example.com/")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	if u.URL != "example.com" {
		t.Fatalf("expected normalized url row, got %q", u.URL)
	}

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	// A legacy row whose url_value kept the raw pre-normalization string.
	if err := gormDB.Create(&db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: "https://Example.com/",
		DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed diverged result: %v", err)
	}

	if err := db.BackfillURLValues(ctx, gormDB); err != nil {
		t.Fatalf("BackfillURLValues: %v", err)
	}

	var got db.ScanResult
	if err := gormDB.First(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.URLValue != "example.com" {
		t.Fatalf("expected url_value backfilled to example.com, got %q", got.URLValue)
	}
}

func TestBackfillURLValues_IsIdempotent(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	u, _ := s.CreateURL(ctx, "example.com")
	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	if err := gormDB.Create(&db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: "example.com",
		DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := db.BackfillURLValues(ctx, gormDB); err != nil {
			t.Fatalf("BackfillURLValues run %d: %v", i, err)
		}
	}

	var got db.ScanResult
	gormDB.First(&got)
	if got.URLValue != "example.com" {
		t.Fatalf("expected url_value unchanged, got %q", got.URLValue)
	}
}

// TestBackfillURLValues_HandlesMultipleBatches seeds more diverged rows than
// one BackfillURLValuesBatchSize chunk and confirms a single call still
// repairs every row — the loop inside BackfillURLValues must keep batching
// until nothing diverged remains, not just fix the first chunk.
func TestBackfillURLValues_HandlesMultipleBatches(t *testing.T) {
	gormDB, _ := rawConnect(t)
	ctx := context.Background()

	srv := db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp", ISP: "Test"}
	if err := gormDB.Create(&srv).Error; err != nil {
		t.Fatalf("seed dns server: %v", err)
	}
	run := db.ScanRun{TriggeredBy: "manual", StartedAt: time.Now()}
	if err := gormDB.Create(&run).Error; err != nil {
		t.Fatalf("seed scan run: %v", err)
	}

	// More rows than one batch, but far short of a realistic production
	// table — BackfillURLValuesBatchSize is small enough that this stays cheap.
	n := db.BackfillURLValuesBatchSize*2 + 5
	urls := make([]db.URL, n)
	for i := range urls {
		urls[i] = db.URL{URL: fmt.Sprintf("host%d.example.com", i)}
	}
	if err := gormDB.CreateInBatches(urls, 500).Error; err != nil {
		t.Fatalf("seed urls: %v", err)
	}

	results := make([]db.ScanResult, n)
	for i, u := range urls {
		results[i] = db.ScanResult{
			ScanRunID: run.ID, URLID: u.ID, URLValue: "stale-value",
			DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(),
		}
	}
	if err := gormDB.CreateInBatches(results, 500).Error; err != nil {
		t.Fatalf("seed diverged results: %v", err)
	}

	if err := db.BackfillURLValues(ctx, gormDB); err != nil {
		t.Fatalf("BackfillURLValues: %v", err)
	}

	var stale int64
	if err := gormDB.Model(&db.ScanResult{}).Where("url_value = ?", "stale-value").Count(&stale).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if stale != 0 {
		t.Fatalf("expected all %d rows backfilled across multiple batches, %d still stale", n, stale)
	}
}
