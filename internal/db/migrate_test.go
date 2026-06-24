package db_test

import (
	"context"
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
