package db_test

import (
	"context"
	"fmt"
	"path/filepath"
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

// legacyDepartmentURL mimics the shape DepartmentURL had earlier today (case
// metadata columns still on department_urls, before it moved to urls) to
// simulate an already-migrated dev database's schema before this newer
// migration runs.
type legacyDepartmentURL struct {
	DepartmentID    uint `gorm:"primaryKey;autoIncrement:false"`
	URLID           uint `gorm:"primaryKey;autoIncrement:false"`
	Enabled         bool `gorm:"not null;default:true"`
	DueDate         *time.Time
	Agency          string
	ReferenceNumber string
	RequestingDept  string
	Status          string
	RequestedAt     *time.Time
	CreatedAt       time.Time
}

func (legacyDepartmentURL) TableName() string { return "department_urls" }

// TestConnect_DropsObsoleteDepartmentURLCaseColumns simulates a dev database
// still on the earlier-today shape: department_urls carrying the six
// case-metadata columns. db.Connect must drop them (this feature was never
// deployed with real data, so this is a plain drop, not a data-preserving
// migration); those fields now live on cases, not urls (see
// BackfillURLCaseMetadataIntoCases), so urls should carry none of them.
func TestConnect_DropsObsoleteDepartmentURLCaseColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate.db")

	// Build the old schema directly (bypassing db.Connect, which only knows
	// about the current — already-moved — struct) and seed a row.
	oldDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open old schema db: %v", err)
	}
	if err := oldDB.AutoMigrate(&legacyDepartmentURL{}); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	seeded := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if err := oldDB.Create(&legacyDepartmentURL{DepartmentID: 1, URLID: 1, Enabled: true, DueDate: &seeded, Status: "requested"}).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	oldSQLDB, err := oldDB.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	if err := oldSQLDB.Close(); err != nil {
		t.Fatalf("close old connection: %v", err)
	}

	// Reopen through the real db.Connect, which must drop the six obsolete
	// columns from department_urls and AutoMigrate the current schema
	// (including the new urls case-metadata columns and the agencies table).
	newDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}

	for _, col := range []string{"due_date", "agency", "reference_number", "requesting_dept", "status", "requested_at"} {
		if newDB.Migrator().HasColumn(&db.DepartmentURL{}, col) {
			t.Fatalf("expected department_urls.%s to be dropped", col)
		}
	}
	for _, col := range []string{"due_date", "agency_id", "status", "requested_at"} {
		if newDB.Migrator().HasColumn(&db.URL{}, col) {
			t.Fatalf("expected urls.%s to be dropped, superseded by cases owning it now", col)
		}
	}
	for _, col := range []string{"reference_number", "requesting_dept_id"} {
		if newDB.Migrator().HasColumn(&db.URL{}, col) {
			t.Fatalf("expected urls.%s to be dropped, superseded by cases", col)
		}
	}
	if !newDB.Migrator().HasTable(&db.Agency{}) {
		t.Fatal("expected agencies table to exist")
	}
}

// TestConnect_DropIsIdempotent runs db.Connect twice against the same
// already-migrated database (the normal case for every restart after the
// first) and confirms it doesn't error the second time, and that a Case's
// fields (which replaced the urls.due_date column this test used to seed
// directly) survive the second run untouched.
func TestConnect_DropIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate_idempotent.db")

	firstDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("first db.Connect: %v", err)
	}
	dept := db.Department{Name: "IdempotentDept"}
	if err := firstDB.Create(&dept).Error; err != nil {
		t.Fatalf("seed department: %v", err)
	}
	due := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	c := db.Case{DepartmentID: dept.ID, DueDate: &due}
	if err := firstDB.Create(&c).Error; err != nil {
		t.Fatalf("seed case: %v", err)
	}
	firstSQLDB, err := firstDB.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	if err := firstSQLDB.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	secondDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("second db.Connect: %v", err)
	}
	var got db.Case
	if err := secondDB.First(&got, c.ID).Error; err != nil {
		t.Fatalf("reload case: %v", err)
	}
	if got.DueDate == nil || !got.DueDate.Equal(due) {
		t.Fatalf("expected case fields to survive a second Connect call, got %+v", got)
	}
}

// TestConnect_FreshDBSkipsDropEntirely covers a brand-new database that
// never had the legacy department_urls columns — the HasColumn guards
// should all be false and no DropColumn call attempted.
func TestConnect_FreshDBSkipsDropEntirely(t *testing.T) {
	newDB, err := db.Connect(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	for _, col := range []string{"due_date", "agency", "reference_number", "requesting_dept", "status", "requested_at"} {
		if newDB.Migrator().HasColumn(&db.DepartmentURL{}, col) {
			t.Fatalf("fresh DB should never have department_urls.%s", col)
		}
	}
}

// legacyURL mirrors the urls table shape before this migration —
// reference_number/requesting_dept_id still present as plain columns — to
// simulate a pre-migration database (db.URL itself no longer declares
// them).
type legacyURL struct {
	ID               uint   `gorm:"primaryKey"`
	URL              string `gorm:"uniqueIndex;not null"`
	CreatedAt        time.Time
	DueDate          *time.Time
	AgencyID         *uint
	ReferenceNumber  string
	RequestingDeptID *uint
	Status           string
	RequestedAt      *time.Time
}

func (legacyURL) TableName() string { return "urls" }

// legacyCase mirrors the pre-2026-09-15 Case shape (AgencyID still a
// scalar on the case itself) for TestConnect_BackfillsCaseAgencyIntoCaseURLs
// to seed directly, bypassing the current (already-moved) db.Case struct.
type legacyCase struct {
	ID           uint `gorm:"primaryKey"`
	DepartmentID uint `gorm:"not null;index"`
	CreatedAt    time.Time
	DueDate      *time.Time
	AgencyID     *uint
	RequestedAt  *time.Time
}

func (legacyCase) TableName() string { return "cases" }

// TestConnect_BackfillsCaseAgencyIntoCaseURLs guards the 2026-09-15 move of
// Agency off Case onto CaseURL (see Case's doc comment in models.go) — a
// pre-existing cases.agency_id value must survive onto every CaseURL row
// under that case before the column is dropped.
func TestConnect_BackfillsCaseAgencyIntoCaseURLs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backfill_case_agency.db")

	oldDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open old schema db: %v", err)
	}
	if err := oldDB.AutoMigrate(&db.Department{}, &db.Agency{}, &legacyCase{}, &db.CaseURL{}, &db.URL{}); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	dept := db.Department{Name: "CRD"}
	if err := oldDB.Create(&dept).Error; err != nil {
		t.Fatalf("seed department: %v", err)
	}
	agency := db.Agency{Name: "PDRM"}
	if err := oldDB.Create(&agency).Error; err != nil {
		t.Fatalf("seed agency: %v", err)
	}
	u := db.URL{URL: "example.com"}
	if err := oldDB.Create(&u).Error; err != nil {
		t.Fatalf("seed url: %v", err)
	}
	legacy := legacyCase{DepartmentID: dept.ID, AgencyID: &agency.ID}
	if err := oldDB.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy case: %v", err)
	}
	if err := oldDB.Create(&db.CaseURL{CaseID: legacy.ID, URLID: u.ID, Status: "blocked"}).Error; err != nil {
		t.Fatalf("seed case_url: %v", err)
	}
	oldSQLDB, err := oldDB.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	if err := oldSQLDB.Close(); err != nil {
		t.Fatalf("close old connection: %v", err)
	}

	newDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}

	if newDB.Migrator().HasColumn(&db.Case{}, "agency_id") {
		t.Fatal("expected cases.agency_id to be dropped")
	}

	var cu db.CaseURL
	if err := newDB.Where("case_id = ? AND url_id = ?", legacy.ID, u.ID).First(&cu).Error; err != nil {
		t.Fatalf("load case_url: %v", err)
	}
	if cu.AgencyID == nil || *cu.AgencyID != agency.ID {
		t.Fatalf("case_url.AgencyID = %v, want %d", cu.AgencyID, agency.ID)
	}
}

// TestConnect_CaseAgencyBackfillSurvivesSecondConnect runs db.Connect twice
// against the same already-migrated database (the normal case for every
// restart after the first) and confirms a CaseURL's own AgencyID isn't
// clobbered the second time — same "already-moved data survives a repeat
// Connect" shape as TestConnect_DropIsIdempotent.
func TestConnect_CaseAgencyBackfillSurvivesSecondConnect(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backfill_case_agency_idempotent.db")

	firstDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("first db.Connect: %v", err)
	}
	dept := db.Department{Name: "IdempotentDept"}
	if err := firstDB.Create(&dept).Error; err != nil {
		t.Fatalf("seed department: %v", err)
	}
	agency := db.Agency{Name: "AgencyB"}
	if err := firstDB.Create(&agency).Error; err != nil {
		t.Fatalf("seed agency: %v", err)
	}
	u := db.URL{URL: "example.com"}
	if err := firstDB.Create(&u).Error; err != nil {
		t.Fatalf("seed url: %v", err)
	}
	c := db.Case{DepartmentID: dept.ID}
	if err := firstDB.Create(&c).Error; err != nil {
		t.Fatalf("seed case: %v", err)
	}
	if err := firstDB.Create(&db.CaseURL{CaseID: c.ID, URLID: u.ID, Status: "blocked", AgencyID: &agency.ID}).Error; err != nil {
		t.Fatalf("seed case_url: %v", err)
	}
	firstSQLDB, err := firstDB.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	if err := firstSQLDB.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	secondDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("second db.Connect: %v", err)
	}
	var cu db.CaseURL
	if err := secondDB.Where("case_id = ? AND url_id = ?", c.ID, u.ID).First(&cu).Error; err != nil {
		t.Fatalf("reload case_url: %v", err)
	}
	if cu.AgencyID == nil || *cu.AgencyID != agency.ID {
		t.Fatalf("expected case_url.AgencyID to survive a second Connect call, got %v", cu.AgencyID)
	}
}

// TestConnect_BackfillsReferenceNumberIntoCases simulates a pre-migration
// database with a urls row carrying both reference_number and
// requesting_dept_id, and confirms db.Connect losslessly moves that data
// into cases/case_letters/case_urls before dropping the old columns.
func TestConnect_BackfillsReferenceNumberIntoCases(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backfill.db")

	oldDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open old schema db: %v", err)
	}
	if err := oldDB.AutoMigrate(&db.Department{}, &legacyURL{}); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	dept := db.Department{Name: "CRD"}
	if err := oldDB.Create(&dept).Error; err != nil {
		t.Fatalf("seed department: %v", err)
	}
	legacy := legacyURL{
		URL:              "example.com",
		ReferenceNumber:  "JK KPN(PR) 168/6",
		RequestingDeptID: &dept.ID,
		Status:           "uplift",
	}
	if err := oldDB.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy url: %v", err)
	}
	oldSQLDB, err := oldDB.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	if err := oldSQLDB.Close(); err != nil {
		t.Fatalf("close old connection: %v", err)
	}

	newDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}

	if newDB.Migrator().HasColumn(&db.URL{}, "reference_number") {
		t.Fatal("expected urls.reference_number to be dropped")
	}
	if newDB.Migrator().HasColumn(&db.URL{}, "requesting_dept_id") {
		t.Fatal("expected urls.requesting_dept_id to be dropped")
	}

	var cases []db.Case
	if err := newDB.Find(&cases).Error; err != nil {
		t.Fatalf("load cases: %v", err)
	}
	if len(cases) != 1 || cases[0].DepartmentID != dept.ID {
		t.Fatalf("expected exactly one case for department %d, got %+v", dept.ID, cases)
	}

	var letters []db.CaseLetter
	if err := newDB.Find(&letters).Error; err != nil {
		t.Fatalf("load case_letters: %v", err)
	}
	if len(letters) != 1 || letters[0].CaseID != cases[0].ID || letters[0].Type != "Notice" || letters[0].ReferenceNumberExternal != "JK KPN(PR) 168/6" {
		t.Fatalf("expected exactly one Notice case_letter carrying the reference number, got %+v", letters)
	}

	var urlRow db.URL
	if err := newDB.Where("url = ?", "example.com").First(&urlRow).Error; err != nil {
		t.Fatalf("load url: %v", err)
	}

	var caseURLs []db.CaseURL
	if err := newDB.Find(&caseURLs).Error; err != nil {
		t.Fatalf("load case_urls: %v", err)
	}
	if len(caseURLs) != 1 || caseURLs[0].CaseID != cases[0].ID || caseURLs[0].URLID != urlRow.ID || caseURLs[0].Status != "uplift" {
		t.Fatalf("expected exactly one case_url with status=uplift, got %+v", caseURLs)
	}
}

// TestConnect_SkipsReferenceNumberWithNoDepartment covers a urls row that
// carries a reference_number but no requesting_dept_id — there's no
// department to attribute a case to, so db.Connect must log and skip
// creating a case, still drop the old columns, and not error.
func TestConnect_SkipsReferenceNumberWithNoDepartment(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backfill_skip.db")

	oldDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open old schema db: %v", err)
	}
	if err := oldDB.AutoMigrate(&legacyURL{}); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	legacy := legacyURL{URL: "noref-dept.example.com", ReferenceNumber: "SOME-REF"}
	if err := oldDB.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy url: %v", err)
	}
	oldSQLDB, err := oldDB.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	if err := oldSQLDB.Close(); err != nil {
		t.Fatalf("close old connection: %v", err)
	}

	newDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}

	var caseCount int64
	if err := newDB.Model(&db.Case{}).Count(&caseCount).Error; err != nil {
		t.Fatalf("count cases: %v", err)
	}
	if caseCount != 0 {
		t.Fatalf("expected no case created without a requesting department, got %d", caseCount)
	}
	var letterCount, caseURLCount int64
	newDB.Model(&db.CaseLetter{}).Count(&letterCount)
	newDB.Model(&db.CaseURL{}).Count(&caseURLCount)
	if letterCount != 0 || caseURLCount != 0 {
		t.Fatalf("expected no case_letters/case_urls without a case, got %d/%d", letterCount, caseURLCount)
	}

	if newDB.Migrator().HasColumn(&db.URL{}, "reference_number") {
		t.Fatal("expected urls.reference_number to be dropped even when skipped")
	}
	if newDB.Migrator().HasColumn(&db.URL{}, "requesting_dept_id") {
		t.Fatal("expected urls.requesting_dept_id to be dropped even when skipped")
	}
}

func TestBackfillErrorClass_ClassifiesExistingRows(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	u, _ := s.CreateURL(ctx, "example.com")
	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	seed := []db.ScanResult{
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(), Error: "no such host"},
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(), Error: "context deadline exceeded"},
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(), Error: "no A records for example.com"},
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: false, ScannedAt: time.Now()}, // violation row, no error — must stay untouched
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(), Error: "invalid URL: not-a-url"},
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(), Error: "server misbehaving"},
		// screenshot-failure-shaped row: DNS resolved (ResolvedIP set), only the
		// capture step errored — must stay unclassified even though the error
		// text would otherwise match the "timeout" LIKE pattern.
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: false, ScannedAt: time.Now(), ResolvedIP: "1.2.3.4", Error: "context deadline exceeded"},
	}
	for i := range seed {
		if err := gormDB.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	if err := db.BackfillErrorClass(ctx, gormDB); err != nil {
		t.Fatalf("BackfillErrorClass: %v", err)
	}

	var got []db.ScanResult
	if err := gormDB.Order("id asc").Find(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	want := []string{"nxdomain", "timeout", "other", "", "invalid_url", "servfail", ""}
	for i, w := range want {
		if got[i].ErrorClass != w {
			t.Errorf("row %d: ErrorClass = %q, want %q", i, got[i].ErrorClass, w)
		}
	}
}

func TestBackfillErrorClass_IsIdempotent(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	u, _ := s.CreateURL(ctx, "example.com")
	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	if err := gormDB.Create(&db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true,
		ScannedAt: time.Now(), Error: "no such host",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	// screenshot-failure-shaped row: must stay unclassified across repeated runs too.
	if err := gormDB.Create(&db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: false,
		ScannedAt: time.Now(), ResolvedIP: "1.2.3.4", Error: "context deadline exceeded",
	}).Error; err != nil {
		t.Fatalf("seed screenshot-failure row: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := db.BackfillErrorClass(ctx, gormDB); err != nil {
			t.Fatalf("BackfillErrorClass run %d: %v", i, err)
		}
	}

	var got []db.ScanResult
	if err := gormDB.Order("id asc").Find(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got[0].ErrorClass != "nxdomain" {
		t.Fatalf("expected ErrorClass unchanged at nxdomain, got %q", got[0].ErrorClass)
	}
	if got[1].ErrorClass != "" {
		t.Fatalf("expected screenshot-failure row to stay unclassified, got %q", got[1].ErrorClass)
	}
}
