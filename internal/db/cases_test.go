package db_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCreateCase_LinksURL(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, err := store.CreateDepartment(ctx, "CRD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	u, err := store.CreateURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if c.DepartmentID != dept.ID {
		t.Errorf("DepartmentID = %d, want %d", c.DepartmentID, dept.ID)
	}

	cases, err := store.ListCasesForURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListCasesForURL: %v", err)
	}
	if len(cases) != 1 || cases[0].Status != "requested" {
		t.Fatalf("got %+v, want one case with status=requested", cases)
	}
}

func TestAddCaseLetter_AppearsInListCasesForURL(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, err := store.CreateDepartment(ctx, "CMOD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	u, err := store.CreateURL(ctx, "example2.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}

	letter, err := store.AddCaseLetter(ctx, db.CaseLetter{
		CaseID: c.ID,
		Type:   "Memo",
	})
	if err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}
	if letter.ID == 0 {
		t.Fatal("expected a generated ID")
	}

	cases, err := store.ListCasesForURL(ctx, "example2.com")
	if err != nil {
		t.Fatalf("ListCasesForURL: %v", err)
	}
	if len(cases) != 1 || len(cases[0].Letters) != 1 || cases[0].Letters[0].Type != "Memo" {
		t.Fatalf("got %+v, want one case with one Memo letter", cases)
	}
}

func TestAddURLToCase_CoversMultipleURLs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, err := store.CreateDepartment(ctx, "CMOD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	u1, err := store.CreateURL(ctx, "batch1.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	u2, err := store.CreateURL(ctx, "batch2.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	c, err := store.CreateCase(ctx, dept.ID, u1.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddURLToCase(ctx, c.ID, u2.ID, "requested", "", nil); err != nil {
		t.Fatalf("AddURLToCase: %v", err)
	}

	for _, urlValue := range []string{"batch1.com", "batch2.com"} {
		cases, err := store.ListCasesForURL(ctx, urlValue)
		if err != nil {
			t.Fatalf("ListCasesForURL(%s): %v", urlValue, err)
		}
		if len(cases) != 1 || cases[0].ID != c.ID {
			t.Fatalf("ListCasesForURL(%s) = %+v, want the shared case %d", urlValue, cases, c.ID)
		}
	}
}

func TestListCaseLetters_ScopesByDepartmentAndCarriesURLs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	crd, err := store.CreateDepartment(ctx, "CRD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	cmod, err := store.CreateDepartment(ctx, "CMOD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	u, err := store.CreateURL(ctx, "docs-page.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	crdCase, err := store.CreateCase(ctx, crd.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: crdCase.ID, Type: "Notice"}); err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}
	cmodCase, err := store.CreateCase(ctx, cmod.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: cmodCase.ID, Type: "Memo"}); err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}

	all, total, err := store.ListCaseLetters(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListCaseLetters: %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Fatalf("ListCaseLetters total=%d len=%d, want 2 and 2", total, len(all))
	}
	for _, e := range all {
		if len(e.URLs) != 1 || e.URLs[0] != "docs-page.com" {
			t.Errorf("entry %+v: URLs = %v, want [docs-page.com]", e.Type, e.URLs)
		}
	}

	crdOnly, crdTotal, err := store.ListCaseLettersForDepartment(ctx, 1, 10, crd.ID)
	if err != nil {
		t.Fatalf("ListCaseLettersForDepartment: %v", err)
	}
	if crdTotal != 1 || len(crdOnly) != 1 || crdOnly[0].Type != "Notice" || crdOnly[0].DepartmentName != "CRD" {
		t.Fatalf("ListCaseLettersForDepartment(CRD) = %+v, want one Notice letter from CRD", crdOnly)
	}
}

// TestCreateCase_SetsAgencyStatusDueDate covers Case now owning the shared
// case-level fields formerly on URL: CreateCase's phase becomes Case.Status,
// and opts.AgencyID/DueDate persist and read back correctly via
// ListDepartmentURLs' derived-from-latest-case fields.
func TestCreateCase_SetsAgencyStatusDueDate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, err := store.CreateDepartment(ctx, "AgencyStatusDept")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	agency, err := store.CreateAgency(ctx, "MCMC")
	if err != nil {
		t.Fatalf("CreateAgency: %v", err)
	}
	u, err := store.AddURLToWatchlist(ctx, dept.ID, "case-fields.com")
	if err != nil {
		t.Fatalf("AddURLToWatchlist: %v", err)
	}

	due := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "uplift", db.CaseCreateOptions{AgencyID: &agency.ID, DueDate: &due})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if c.DueDate == nil || !c.DueDate.Equal(due) {
		t.Fatalf("Case.DueDate = %v, want %v", c.DueDate, due)
	}
	// AgencyID is now on CaseURL (not Case), and is set during CreateCase
	// TestConnect_BackfillsCaseAgencyIntoCaseURLs verifies the backfill path
	cases, err := store.ListCasesForURL(ctx, "case-fields.com")
	if err != nil {
		t.Fatalf("ListCasesForURL: %v", err)
	}
	if len(cases) != 1 || cases[0].Status != "uplift" {
		t.Fatalf("got %+v, want one case with status=uplift", cases)
	}

	entries, err := store.ListDepartmentURLs(ctx, dept.ID)
	if err != nil {
		t.Fatalf("ListDepartmentURLs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Status != "uplift" || e.AgencyName != "MCMC" || e.DueDate == nil || !e.DueDate.Equal(due) {
		t.Fatalf("expected URLEntry to derive case-level fields from the latest case, got %+v", e)
	}
}

// TestUpdateCaseFields_SetAndClear exercises UpdateCaseFields' double-pointer
// clear-vs-untouched contract for each field, and that updating one field
// doesn't clobber the others.
func TestUpdateCaseFields_SetAndClear(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, _ := store.CreateDepartment(ctx, "UpdateCaseDept")
	u, _ := store.CreateURL(ctx, "update-case.com")
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}

	due := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	duePtr := &due
	found, err := store.UpdateCaseFields(ctx, dept.ID, c.ID, db.CaseFields{
		DueDate: &duePtr,
	})
	if err != nil || !found {
		t.Fatalf("UpdateCaseFields(set): found=%v err=%v", found, err)
	}

	got, err := store.GetCase(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCase: %v", err)
	}
	if got.DueDate == nil || !got.DueDate.Equal(due) {
		t.Fatalf("expected DueDate to be set, got %+v", got)
	}

	// Clear DueDate (outer non-nil, inner nil).
	var nilDueDate *time.Time
	found, err = store.UpdateCaseFields(ctx, dept.ID, c.ID, db.CaseFields{DueDate: &nilDueDate})
	if err != nil || !found {
		t.Fatalf("UpdateCaseFields(clear): found=%v err=%v", found, err)
	}
	got, _ = store.GetCase(ctx, c.ID)
	if got.DueDate != nil {
		t.Fatalf("expected due_date to be cleared, got %+v", got)
	}
}

// TestUpdateCaseFields_UnknownCase covers the false-not-error result for a
// case id that doesn't exist.
func TestUpdateCaseFields_UnknownCase(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	due := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	duePtr := &due
	found, err := store.UpdateCaseFields(ctx, 1, 999, db.CaseFields{DueDate: &duePtr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for an unknown case id")
	}
}

// TestUpdateCaseURLStatus covers the per-domain field: two urls sharing one
// case can carry different Status values, and updating one doesn't touch
// the other.
func TestUpdateCaseURLStatus(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, _ := store.CreateDepartment(ctx, "StatusOverrideDept")
	u1, _ := store.CreateURL(ctx, "status-a.com")
	u2, _ := store.CreateURL(ctx, "status-b.com")

	c, err := store.CreateCase(ctx, dept.ID, u1.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddURLToCase(ctx, c.ID, u2.ID, "requested", "", nil); err != nil {
		t.Fatalf("AddURLToCase: %v", err)
	}

	found, err := store.UpdateCaseURLStatus(ctx, c.ID, u2.ID, "uplift")
	if err != nil || !found {
		t.Fatalf("UpdateCaseURLStatus: found=%v err=%v", found, err)
	}

	casesU1, _ := store.ListCasesForURL(ctx, "status-a.com")
	casesU2, _ := store.ListCasesForURL(ctx, "status-b.com")
	if len(casesU1) != 1 || casesU1[0].Status != "requested" {
		t.Fatalf("expected status-a.com's status to stay requested, got %+v", casesU1)
	}
	if len(casesU2) != 1 || casesU2[0].Status != "uplift" {
		t.Fatalf("expected status-b.com's status to be updated to uplift, got %+v", casesU2)
	}

	// No such (case, url) pair.
	found, err = store.UpdateCaseURLStatus(ctx, c.ID, 999999, "uplift")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a url not in this case")
	}
}

// legacyCaseMetadataURL mirrors the urls table shape before Case took over
// Agency/Status/DueDate/RequestedAt — those four columns still directly on
// urls (db.URL no longer declares them) — to simulate a pre-migration
// database. ReferenceNumber/RequestingDeptID are already-migrated-away
// columns from an earlier change and are irrelevant here, but included so
// AutoMigrate produces a schema db.Connect's HasColumn guards recognize the
// same way legacyURL (migrate_test.go) does.
type legacyCaseMetadataURL struct {
	ID          uint   `gorm:"primaryKey"`
	URL         string `gorm:"uniqueIndex;not null"`
	CreatedAt   time.Time
	DueDate     *time.Time
	AgencyID    *uint
	Status      string
	RequestedAt *time.Time
}

func (legacyCaseMetadataURL) TableName() string { return "urls" }

// TestConnect_BackfillsURLCaseMetadataIntoCases simulates a pre-migration
// database: a urls row with Status/DueDate set, watched by two departments,
// and a second urls row with the same case metadata but zero watching
// departments. db.Connect must create one Case per distinct watching
// department for the first (each carrying the metadata forward, linked via
// a CaseURL), and skip the second entirely (logged, not fatal) since
// there's no department to attribute a Case to.
func TestConnect_BackfillsURLCaseMetadataIntoCases(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backfill_case_metadata.db")

	oldDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open old schema db: %v", err)
	}
	if err := oldDB.AutoMigrate(&db.Department{}, &db.DepartmentURL{}, &legacyCaseMetadataURL{}); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}

	deptA := db.Department{Name: "DeptA"}
	deptB := db.Department{Name: "DeptB"}
	if err := oldDB.Create(&deptA).Error; err != nil {
		t.Fatalf("seed deptA: %v", err)
	}
	if err := oldDB.Create(&deptB).Error; err != nil {
		t.Fatalf("seed deptB: %v", err)
	}

	due := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	watched := legacyCaseMetadataURL{URL: "watched-legacy.com", DueDate: &due, Status: "uplift"}
	if err := oldDB.Create(&watched).Error; err != nil {
		t.Fatalf("seed watched legacy url: %v", err)
	}
	if err := oldDB.Create(&db.DepartmentURL{DepartmentID: deptA.ID, URLID: watched.ID, Enabled: true}).Error; err != nil {
		t.Fatalf("seed department_url A: %v", err)
	}
	if err := oldDB.Create(&db.DepartmentURL{DepartmentID: deptB.ID, URLID: watched.ID, Enabled: true}).Error; err != nil {
		t.Fatalf("seed department_url B: %v", err)
	}

	unwatched := legacyCaseMetadataURL{URL: "unwatched-legacy.com", Status: "requested"}
	if err := oldDB.Create(&unwatched).Error; err != nil {
		t.Fatalf("seed unwatched legacy url: %v", err)
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

	for _, col := range []string{"due_date", "agency_id", "status", "requested_at"} {
		if newDB.Migrator().HasColumn(&db.URL{}, col) {
			t.Fatalf("expected urls.%s to be dropped after backfill", col)
		}
	}

	var watchedRow db.URL
	if err := newDB.Where("url = ?", "watched-legacy.com").First(&watchedRow).Error; err != nil {
		t.Fatalf("load watched url: %v", err)
	}
	var watchedCases []db.Case
	if err := newDB.Where(
		"id IN (SELECT case_id FROM case_urls WHERE url_id = ?)", watchedRow.ID,
	).Find(&watchedCases).Error; err != nil {
		t.Fatalf("load cases for watched url: %v", err)
	}
	if len(watchedCases) != 2 {
		t.Fatalf("expected one case per distinct watching department (2), got %d: %+v", len(watchedCases), watchedCases)
	}
	seenDepts := map[uint]bool{}
	for _, c := range watchedCases {
		seenDepts[c.DepartmentID] = true
		if c.DueDate == nil || !c.DueDate.Equal(due) {
			t.Fatalf("expected backfilled case to carry the legacy due_date forward, got %+v", c)
		}
	}
	if !seenDepts[deptA.ID] || !seenDepts[deptB.ID] {
		t.Fatalf("expected one case for each of deptA/deptB, got departments %v", seenDepts)
	}

	var watchedCaseURLs []db.CaseURL
	newDB.Where("url_id = ?", watchedRow.ID).Find(&watchedCaseURLs)
	if len(watchedCaseURLs) != 2 {
		t.Fatalf("expected 2 case_url rows for the watched url, got %d", len(watchedCaseURLs))
	}
	for _, cu := range watchedCaseURLs {
		if cu.Status != "uplift" {
			t.Fatalf("expected case_url.status to match the legacy status, got %q", cu.Status)
		}
	}

	var unwatchedRow db.URL
	if err := newDB.Where("url = ?", "unwatched-legacy.com").First(&unwatchedRow).Error; err != nil {
		t.Fatalf("load unwatched url: %v", err)
	}
	var unwatchedCaseCount int64
	newDB.Table("case_urls").Where("url_id = ?", unwatchedRow.ID).Count(&unwatchedCaseCount)
	if unwatchedCaseCount != 0 {
		t.Fatalf("expected no case created for a url with no watching department, got %d", unwatchedCaseCount)
	}
}

// TestListCasesForDepartment_PicksNoticeOverMemoAndListsDomains covers the
// Cases view's core aggregate: a case with both a Notice and a Memo letter
// surfaces the Notice's fields (not the Memo's), and every domain the case
// covers (via CaseURL, including one added later via AddURLToCase) appears
// in Domains.
func TestListCasesForDepartment_PicksNoticeOverMemoAndListsDomains(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, err := store.CreateDepartment(ctx, "SummaryDept")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	agency, err := store.CreateAgency(ctx, "MCMC")
	if err != nil {
		t.Fatalf("CreateAgency: %v", err)
	}
	u1, err := store.CreateURL(ctx, "summary-a.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	u2, err := store.CreateURL(ctx, "summary-b.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	c, err := store.CreateCase(ctx, dept.ID, u1.ID, "requested", db.CaseCreateOptions{AgencyID: &agency.ID})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddURLToCase(ctx, c.ID, u2.ID, "requested", "", nil); err != nil {
		t.Fatalf("AddURLToCase: %v", err)
	}

	earlier := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Memo", Subject: "memo-subject", LetterDate: &earlier}); err != nil {
		t.Fatalf("AddCaseLetter(Memo): %v", err)
	}
	if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Notice", Subject: "notice-subject", LetterDate: &later}); err != nil {
		t.Fatalf("AddCaseLetter(Notice): %v", err)
	}

	summaries, err := store.ListCasesForDepartment(ctx, dept.ID)
	if err != nil {
		t.Fatalf("ListCasesForDepartment: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("want 1 summary, got %d", len(summaries))
	}
	s := summaries[0]
	if s.NoticeSubject != "notice-subject" {
		t.Fatalf("NoticeSubject = %q, want notice-subject", s.NoticeSubject)
	}
	if s.MemoSubject != "memo-subject" {
		t.Fatalf("MemoSubject = %q, want memo-subject", s.MemoSubject)
	}
	// AgencyID/AgencyName are now on CaseURL (per-domain), not Case
	// (case-level) — moved 2026-09-15, so they're no longer populated here
	if len(s.Domains) != 2 {
		t.Fatalf("want 2 domains, got %+v", s.Domains)
	}
	gotURLs := map[string]bool{}
	for _, d := range s.Domains {
		gotURLs[d.URL] = true
	}
	if !gotURLs["summary-a.com"] || !gotURLs["summary-b.com"] {
		t.Fatalf("expected both domains, got %+v", s.Domains)
	}
}

// TestListCases_GlobalAcrossDepartments covers the admin/global variant —
// same split as ListCaseLetters/ListCaseLettersForDepartment.
func TestListCases_GlobalAcrossDepartments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	deptA, _ := store.CreateDepartment(ctx, "GlobalDeptA")
	deptB, _ := store.CreateDepartment(ctx, "GlobalDeptB")
	uA, _ := store.CreateURL(ctx, "global-a.com")
	uB, _ := store.CreateURL(ctx, "global-b.com")
	if _, err := store.CreateCase(ctx, deptA.ID, uA.ID, "requested", db.CaseCreateOptions{}); err != nil {
		t.Fatalf("CreateCase A: %v", err)
	}
	if _, err := store.CreateCase(ctx, deptB.ID, uB.ID, "requested", db.CaseCreateOptions{}); err != nil {
		t.Fatalf("CreateCase B: %v", err)
	}

	all, err := store.ListCases(ctx)
	if err != nil {
		t.Fatalf("ListCases: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 summaries globally, got %d", len(all))
	}
}

// TestListCases_DomainsCarryTheirOwnAgency proves the real-world shape this
// migration exists to support: two domains in the same case, each opened
// against a different Agency (CaseURL.AgencyID, not the old case-level
// Case.AgencyID) — each domain must report its own agency, independently.
func TestListCases_DomainsCarryTheirOwnAgency(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, err := store.CreateDepartment(ctx, "CRD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	pdrm, err := store.CreateAgency(ctx, "PDRM")
	if err != nil {
		t.Fatalf("CreateAgency(PDRM): %v", err)
	}
	mcmc, err := store.CreateAgency(ctx, "MCMC")
	if err != nil {
		t.Fatalf("CreateAgency(MCMC): %v", err)
	}
	uGambling, err := store.CreateURL(ctx, "bet.example.com")
	if err != nil {
		t.Fatalf("CreateURL(gambling): %v", err)
	}
	uPorn, err := store.CreateURL(ctx, "adult.example.com")
	if err != nil {
		t.Fatalf("CreateURL(porn): %v", err)
	}

	c, err := store.CreateCase(ctx, dept.ID, uGambling.ID, "blocked", db.CaseCreateOptions{AgencyID: &pdrm.ID})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddURLToCase(ctx, c.ID, uPorn.ID, "blocked", "", &mcmc.ID); err != nil {
		t.Fatalf("AddURLToCase: %v", err)
	}

	summaries, err := store.ListCasesForDepartment(ctx, dept.ID)
	if err != nil {
		t.Fatalf("ListCasesForDepartment: %v", err)
	}
	if len(summaries) != 1 || len(summaries[0].Domains) != 2 {
		t.Fatalf("got %+v, want one case with two domains", summaries)
	}
	agencyByURL := map[string]string{}
	for _, d := range summaries[0].Domains {
		agencyByURL[d.URL] = d.AgencyName
	}
	if agencyByURL["bet.example.com"] != "PDRM" || agencyByURL["adult.example.com"] != "MCMC" {
		t.Fatalf("got %+v, want bet.example.com=PDRM, adult.example.com=MCMC", agencyByURL)
	}
}

// TestUpdateCaseLetterFields_PartialUpdateAndScoping covers the partial-
// update contract (touching one field leaves the others alone) and that
// the update is scoped by (caseID, letterID) — a letter can't be edited
// through the wrong case id.
func TestUpdateCaseLetterFields_PartialUpdateAndScoping(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, _ := store.CreateDepartment(ctx, "LetterFieldsDept")
	u, _ := store.CreateURL(ctx, "letter-fields.com")
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	letter, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Notice", Subject: "orig-subject", WorkflowStatus: "Draft"})
	if err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}

	newSubject := "updated-subject"
	found, err := store.UpdateCaseLetterFields(ctx, c.ID, letter.ID, db.CaseLetterFields{Subject: &newSubject})
	if err != nil || !found {
		t.Fatalf("UpdateCaseLetterFields: found=%v err=%v", found, err)
	}

	cases, err := store.ListCasesForURL(ctx, "letter-fields.com")
	if err != nil {
		t.Fatalf("ListCasesForURL: %v", err)
	}
	if len(cases) != 1 || len(cases[0].Letters) != 1 {
		t.Fatalf("got %+v", cases)
	}
	got := cases[0].Letters[0]
	if got.Subject != "updated-subject" {
		t.Fatalf("Subject = %q, want updated-subject", got.Subject)
	}
	if got.WorkflowStatus != "Draft" {
		t.Fatalf("WorkflowStatus = %q, want unchanged Draft", got.WorkflowStatus)
	}

	// Wrong case id — same letter id, different (wrong) case: no row matches.
	found, err = store.UpdateCaseLetterFields(ctx, c.ID+999, letter.ID, db.CaseLetterFields{Subject: &newSubject})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false when case id doesn't match the letter's own case")
	}
}

// TestUpdateCaseURLAgency_SetsAndClears covers UpdateCaseURLAgency setting
// and clearing one (case, url) pair's own AgencyID, read back via
// ListCasesForURL's new AgencyID/AgencyName fields.
func TestUpdateCaseURLAgency_SetsAndClears(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, err := store.CreateDepartment(ctx, "CRD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	agency, err := store.CreateAgency(ctx, "PDRM")
	if err != nil {
		t.Fatalf("CreateAgency: %v", err)
	}
	u, err := store.CreateURL(ctx, "case-url-agency.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}

	ok, err := store.UpdateCaseURLAgency(ctx, c.ID, u.ID, &agency.ID)
	if err != nil {
		t.Fatalf("UpdateCaseURLAgency: %v", err)
	}
	if !ok {
		t.Fatal("UpdateCaseURLAgency returned false, want true")
	}

	cases, err := store.ListCasesForURL(ctx, "case-url-agency.com")
	if err != nil {
		t.Fatalf("ListCasesForURL: %v", err)
	}
	if len(cases) != 1 || cases[0].AgencyID == nil || *cases[0].AgencyID != agency.ID || cases[0].AgencyName != "PDRM" {
		t.Fatalf("got %+v, want one case with AgencyID=%d AgencyName=PDRM", cases, agency.ID)
	}

	ok, err = store.UpdateCaseURLAgency(ctx, c.ID, u.ID, nil)
	if err != nil {
		t.Fatalf("UpdateCaseURLAgency (clear): %v", err)
	}
	if !ok {
		t.Fatal("UpdateCaseURLAgency (clear) returned false, want true")
	}

	cases, err = store.ListCasesForURL(ctx, "case-url-agency.com")
	if err != nil {
		t.Fatalf("ListCasesForURL: %v", err)
	}
	if len(cases) != 1 || cases[0].AgencyID != nil || cases[0].AgencyName != "" {
		t.Fatalf("got %+v, want AgencyID/AgencyName cleared after UpdateCaseURLAgency(nil)", cases)
	}
}

// TestUpdateCaseURLAgency_FalseWhenNoSuchCaseURL covers the false-not-error
// result for a (case, url) pair that doesn't exist.
func TestUpdateCaseURLAgency_FalseWhenNoSuchCaseURL(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	ok, err := store.UpdateCaseURLAgency(ctx, 9999, 9999, nil)
	if err != nil {
		t.Fatalf("UpdateCaseURLAgency: %v", err)
	}
	if ok {
		t.Fatal("expected false for a nonexistent case_url pair")
	}
}

func TestUpdateCaseLetterFields_SetsAndClearsOICUserID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, _ := store.CreateDepartment(ctx, "OICFieldsDept")
	u, _ := store.CreateURL(ctx, "oic-fields.com")
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	letter, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Notice"})
	if err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}

	oicID := uint(42)
	oicIDPtr := &oicID
	found, err := store.UpdateCaseLetterFields(ctx, c.ID, letter.ID, db.CaseLetterFields{OICUserID: &oicIDPtr})
	if err != nil || !found {
		t.Fatalf("UpdateCaseLetterFields(set): found=%v err=%v", found, err)
	}

	// Read back via ListCaseLetters, same as TestUpdateCaseLetterFields_PartialUpdateAndScoping does.
	entries, _, err := store.ListCaseLetters(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListCaseLetters: %v", err)
	}
	var updated *db.CaseLetterEntry
	for i := range entries {
		if entries[i].ID == letter.ID {
			updated = &entries[i]
		}
	}
	if updated == nil || updated.OICUserID == nil || *updated.OICUserID != oicID {
		t.Fatalf("expected OICUserID %d, got %+v", oicID, updated)
	}

	// Clear it.
	var nilOIC *uint
	found, err = store.UpdateCaseLetterFields(ctx, c.ID, letter.ID, db.CaseLetterFields{OICUserID: &nilOIC})
	if err != nil || !found {
		t.Fatalf("UpdateCaseLetterFields(clear): found=%v err=%v", found, err)
	}
	entries, _, err = store.ListCaseLetters(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListCaseLetters: %v", err)
	}
	for i := range entries {
		if entries[i].ID == letter.ID && entries[i].OICUserID != nil {
			t.Fatalf("expected OICUserID cleared, got %+v", entries[i])
		}
	}
}

func TestRemoveURLFromCase(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, _ := store.CreateDepartment(ctx, "CRD")
	u1, _ := store.CreateURL(ctx, "rm-one.com")
	u2, _ := store.CreateURL(ctx, "rm-two.com")
	c, err := store.CreateCase(ctx, dept.ID, u1.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddURLToCase(ctx, c.ID, u2.ID, "requested", "", nil); err != nil {
		t.Fatalf("AddURLToCase: %v", err)
	}
	if ok, err := store.RemoveURLFromCase(ctx, c.ID, u2.ID); err != nil || !ok {
		t.Fatalf("remove u2: ok=%v err=%v", ok, err)
	}
	if _, err := store.RemoveURLFromCase(ctx, c.ID, u1.ID); !errors.Is(err, db.ErrLastCaseURL) {
		t.Fatalf("remove last: err=%v, want ErrLastCaseURL", err)
	}
	ids, _ := store.ListCaseURLIDs(ctx, c.ID)
	if len(ids) != 1 || ids[0] != u1.ID {
		t.Fatalf("case urls = %v, want just u1", ids)
	}
}

func TestListCaseSummariesPage_PagesFiltersAndSorts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	deptA, _ := store.CreateDepartment(ctx, "CRD")
	deptB, _ := store.CreateDepartment(ctx, "CMOD")
	agency, _ := store.CreateAgency(ctx, "PDRM")
	mk := func(dept uint, host, status string, due *time.Time) db.Case {
		u, err := store.CreateURL(ctx, host)
		if err != nil {
			t.Fatalf("CreateURL: %v", err)
		}
		c, err := store.CreateCase(ctx, dept, u.ID, status, db.CaseCreateOptions{DueDate: due})
		if err != nil {
			t.Fatalf("CreateCase: %v", err)
		}
		return c
	}
	d1 := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC)
	c1 := mk(deptA.ID, "page-one.com", "blocked", &d1)
	c2 := mk(deptA.ID, "page-two.com", "requested", &d2)
	c3 := mk(deptB.ID, "other-three.com", "blocked", nil)
	if _, err := store.UpdateCaseURLAgency(ctx, c2.ID, 2, &agency.ID); err != nil {
		t.Fatalf("agency: %v", err)
	}

	list := func(p db.CaseListParams) ([]db.CaseSummary, int) {
		if p.Page == 0 {
			p.Page = 1
		}
		if p.PageSize == 0 {
			p.PageSize = 10
		}
		got, total, err := store.ListCaseSummariesPage(ctx, p)
		if err != nil {
			t.Fatalf("ListCaseSummariesPage(%+v): %v", p, err)
		}
		return got, total
	}
	ids := func(cs []db.CaseSummary) []uint {
		out := make([]uint, len(cs))
		for i, c := range cs {
			out[i] = c.ID
		}
		return out
	}

	if got, total := list(db.CaseListParams{PageSize: 2}); len(got) != 2 || total != 3 {
		t.Fatalf("page 1 of 3 cases: got %d rows, total %d", len(got), total)
	}
	if got, _ := list(db.CaseListParams{Page: 2, PageSize: 2}); len(got) != 1 {
		t.Fatalf("page 2: got %d rows, want 1", len(got))
	}
	if got, total := list(db.CaseListParams{DepartmentID: &deptA.ID}); total != 2 || len(got) != 2 {
		t.Fatalf("dept scope: %v total %d", ids(got), total)
	}
	if got, total := list(db.CaseListParams{Query: fmt.Sprintf("#%d", c2.ID)}); total != 1 || got[0].ID != c2.ID {
		t.Fatalf("search by case id: %v total %d", ids(got), total)
	}
	if got, total := list(db.CaseListParams{Query: "OTHER-thr"}); total != 1 || got[0].ID != c3.ID || len(got[0].Domains) != 1 {
		t.Fatalf("search by domain: %v total %d", ids(got), total)
	}
	if _, total := list(db.CaseListParams{Status: "blocked"}); total != 2 {
		t.Fatalf("status filter: total %d, want 2", total)
	}
	if got, _ := list(db.CaseListParams{AgencyID: &agency.ID}); len(got) != 1 || got[0].ID != c2.ID {
		t.Fatalf("agency filter: %v", ids(got))
	}
	if got, _ := list(db.CaseListParams{Due: db.DateFilter{Op: "on", From: "2026-01-10"}}); len(got) != 1 || got[0].ID != c1.ID {
		t.Fatalf("due on: %v", ids(got))
	}
	if got, _ := list(db.CaseListParams{Due: db.DateFilter{Op: "between", From: "2026-01-01", To: "2026-03-05"}}); len(got) != 2 {
		t.Fatalf("due between (inclusive of To): %v", ids(got))
	}
	if got, _ := list(db.CaseListParams{Due: db.DateFilter{Op: "after", From: "2026-01-10"}}); len(got) != 1 || got[0].ID != c2.ID {
		t.Fatalf("due after: %v", ids(got))
	}
	if got, _ := list(db.CaseListParams{SortBy: "id"}); got[0].ID != c1.ID {
		t.Fatalf("sort id asc: %v", ids(got))
	}
	if got, _ := list(db.CaseListParams{SortBy: "id", SortDesc: true}); got[0].ID != c3.ID {
		t.Fatalf("sort id desc: %v", ids(got))
	}
	if got, _ := list(db.CaseListParams{RequestingDept: &deptB.ID}); len(got) != 1 || got[0].ID != c3.ID {
		t.Fatalf("requesting dept: %v", ids(got))
	}
}

func TestListCaseSummariesPage_LetterDatesAndSort(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, _ := store.CreateDepartment(ctx, "CRD")
	day := func(m time.Month, d int) *time.Time { t := time.Date(2026, m, d, 0, 0, 0, 0, time.UTC); return &t }
	mk := func(host string, notice, uplift *time.Time) uint {
		u, _ := store.CreateURL(ctx, host)
		c, err := store.CreateCase(ctx, dept.ID, u.ID, "blocked", db.CaseCreateOptions{})
		if err != nil {
			t.Fatalf("CreateCase: %v", err)
		}
		if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Notice", ReferenceNumberInternal: host, LetterDate: notice}); err != nil {
			t.Fatalf("notice: %v", err)
		}
		if uplift != nil {
			if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Notice (Uplift)", LetterDate: uplift}); err != nil {
				t.Fatalf("uplift: %v", err)
			}
		}
		return c.ID
	}
	early := mk("early.com", day(time.January, 5), day(time.March, 1))
	late := mk("late.com", day(time.February, 9), nil)
	none := mk("none.com", nil, nil)

	list := func(sort string, desc bool) []db.CaseSummary {
		got, _, err := store.ListCaseSummariesPage(ctx, db.CaseListParams{Page: 1, PageSize: 10, SortBy: sort, SortDesc: desc})
		if err != nil {
			t.Fatalf("list %s: %v", sort, err)
		}
		return got
	}
	order := func(cs []db.CaseSummary) []uint {
		out := make([]uint, len(cs))
		for i, c := range cs {
			out[i] = c.ID
		}
		return out
	}
	eq := func(got []db.CaseSummary, want ...uint) {
		t.Helper()
		g := order(got)
		for i := range want {
			if g[i] != want[i] {
				t.Fatalf("order = %v, want %v", g, want)
			}
		}
	}
	// Empty dates sort last in both directions.
	eq(list("notice_letter_date", false), early, late, none)
	eq(list("notice_letter_date", true), late, early, none)
	eq(list("uplift_letter_date", false), early, none, late) // only one has an uplift date; ties fall back to id desc

	// The uplift letter doesn't displace the Notice's own refs/date, and its date is exposed.
	for _, c := range list("", false) {
		if c.ID == early {
			if c.NoticeReferenceNumberInternal != "early.com" || c.NoticeLetterDate == nil || !c.NoticeLetterDate.Equal(*day(time.January, 5)) {
				t.Fatalf("notice fields taken from the wrong letter: %+v", c)
			}
			if c.UpliftLetterDate == nil || !c.UpliftLetterDate.Equal(*day(time.March, 1)) {
				t.Fatalf("UpliftLetterDate = %v", c.UpliftLetterDate)
			}
		}
	}
}

func TestListCaseSummariesPage_LatestScanOutcomePerDomain(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, _ := store.CreateDepartment(ctx, "CRD")
	scanned, _ := store.CreateURL(ctx, "scanned.com")
	unscanned, _ := store.CreateURL(ctx, "unscanned.com")
	c, _ := store.CreateCase(ctx, dept.ID, scanned.ID, "blocked", db.CaseCreateOptions{})
	if _, err := store.AddURLToCase(ctx, c.ID, unscanned.ID, "blocked", "", nil); err != nil {
		t.Fatalf("AddURLToCase: %v", err)
	}
	s1, _ := store.CreateDNSServer(ctx, db.DNSServer{Name: "A", Address: "1.1.1.1:53", Protocol: "udp"})
	s2, _ := store.CreateDNSServer(ctx, db.DNSServer{Name: "B", Address: "8.8.8.8:53", Protocol: "udp"})
	old, _ := store.CreateScanRun(ctx, "manual")
	// The older run says everything is compliant; only the latest run should count.
	if err := store.InsertResult(ctx, db.ScanResult{ScanRunID: old.ID, URLID: scanned.ID, URLValue: scanned.URL, DNSServerID: s1.ID, Compliant: true, ScannedAt: time.Now().Add(-48 * time.Hour)}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	run, _ := store.CreateScanRun(ctx, "manual")
	if err := store.InsertResult(ctx, db.ScanResult{ScanRunID: run.ID, URLID: scanned.ID, URLValue: scanned.URL, DNSServerID: s1.ID, Compliant: true, ScannedAt: time.Now()}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}
	if err := store.InsertResult(ctx, db.ScanResult{ScanRunID: run.ID, URLID: scanned.ID, URLValue: scanned.URL, DNSServerID: s2.ID, Compliant: false, ScannedAt: time.Now()}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	got, _, err := store.ListCaseSummariesPage(ctx, db.CaseListParams{Page: 1, PageSize: 10})
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %v %+v", err, got)
	}
	for _, d := range got[0].Domains {
		switch d.URL {
		case "scanned.com":
			if d.ScanTotal != 2 || d.ScanCompliant != 1 || d.ScannedAt == nil {
				t.Fatalf("scanned.com: %+v", d)
			}
		case "unscanned.com":
			if d.ScanTotal != 0 || d.ScannedAt != nil {
				t.Fatalf("unscanned.com: %+v", d)
			}
		}
	}
}

// TestMergeOffenceCasing covers the "Tidak Berdaftar" vs "Tidak berdaftar"
// bug: categories rows that differ only in casing must collapse into one
// BlockingStatRow per (year, agency) with counts summed, and — modelled on
// the real data for "Penjualan (T|t)anpa Kebenaran"/agency MOTAC, where the
// lowercase variant actually wins some individual years and the uppercase
// variant wins others — the winning casing must be picked once globally so
// the same offence never re-splits across years in the frontend's
// (agency, offence)-string grouping.
func TestMergeOffenceCasing(t *testing.T) {
	in := []db.BlockingStatRow{
		{Year: 2024, Agency: "MCMC", Offence: "Tidak Berdaftar", Count: 5},
		{Year: 2024, Agency: "MCMC", Offence: "Tidak berdaftar", Count: 1},
		// Different year/agency buckets must stay separate.
		{Year: 2023, Agency: "MCMC", Offence: "Tidak Berdaftar", Count: 2},
		{Year: 2024, Agency: "KPDNHEP", Offence: "tidak berdaftar", Count: 3},
		// MOTAC: lowercase wins 2016+2017, uppercase wins 2018+2021 — the
		// global total (13+64=77 lower vs 8+1=9 upper) must still pick one
		// consistent name for every MOTAC row, not flip year to year.
		{Year: 2016, Agency: "MOTAC", Offence: "Penjualan Tanpa Kebenaran", Count: 3},
		{Year: 2016, Agency: "MOTAC", Offence: "Penjualan tanpa kebenaran", Count: 10},
		{Year: 2017, Agency: "MOTAC", Offence: "Penjualan Tanpa Kebenaran", Count: 19},
		{Year: 2017, Agency: "MOTAC", Offence: "Penjualan tanpa kebenaran", Count: 45},
		{Year: 2018, Agency: "MOTAC", Offence: "Penjualan Tanpa Kebenaran", Count: 8},
		{Year: 2021, Agency: "MOTAC", Offence: "Penjualan Tanpa Kebenaran", Count: 1},
	}
	got := db.MergeOffenceCasing(in)
	if len(got) != 7 {
		t.Fatalf("len(got) = %d, want 7: %+v", len(got), got)
	}
	byKey := map[string]db.BlockingStatRow{}
	for _, r := range got {
		byKey[fmt.Sprintf("%d/%s/%s", r.Year, r.Agency, strings.ToLower(r.Offence))] = r
	}
	tb := byKey["2024/MCMC/tidak berdaftar"]
	if tb.Count != 6 || tb.Offence != "Tidak Berdaftar" {
		t.Fatalf("tidak berdaftar 2024/MCMC = %+v, want count=6 offence=Tidak Berdaftar", tb)
	}
	if r := byKey["2023/MCMC/tidak berdaftar"]; r.Count != 2 {
		t.Fatalf("2023/MCMC bucket should stay separate: %+v", r)
	}
	if r := byKey["2024/KPDNHEP/tidak berdaftar"]; r.Count != 3 {
		t.Fatalf("2024/KPDNHEP bucket should stay separate: %+v", r)
	}

	// Every MOTAC row must share one display name — the global-plurality
	// lowercase variant — even the 2018/2021 rows that only ever appeared
	// with the uppercase spelling in their own bucket.
	wantCounts := map[int]int{2016: 13, 2017: 64, 2018: 8, 2021: 1}
	names := map[string]bool{}
	for year, wantCount := range wantCounts {
		r := byKey[fmt.Sprintf("%d/MOTAC/penjualan tanpa kebenaran", year)]
		if r.Count != wantCount {
			t.Fatalf("MOTAC %d count = %d, want %d: %+v", year, r.Count, wantCount, r)
		}
		names[r.Offence] = true
	}
	if len(names) != 1 {
		t.Fatalf("MOTAC rows use %d distinct display names, want 1: %v", len(names), names)
	}
	if !names["Penjualan tanpa kebenaran"] {
		t.Fatalf("MOTAC display name = %v, want the global-plurality lowercase variant", names)
	}
}

// MCMC's five categories count as MCMC whoever handled them; gambling MCMC
// handled counts as PDRM; and re-attributed rows merge into their bucket.
func TestMergeOffenceCasingAttributesAgency(t *testing.T) {
	got := db.MergeOffenceCasing([]db.BlockingStatRow{
		{Year: 2025, Agency: "PDRM", Offence: "lucah", Count: 2},
		{Year: 2025, Agency: "MCMC", Offence: "Lucah", Count: 3},
		{Year: 2025, Agency: "MCMC", Offence: "Judi", Count: 4},
		{Year: 2025, Agency: "PDRM", Offence: "Judi", Count: 1},
		{Year: 2025, Agency: "KPKT", Offence: "Judi", Count: 7},
	})
	byKey := map[string]int{}
	for _, r := range got {
		byKey[r.Agency+"/"+r.Offence] = r.Count
	}
	want := map[string]int{"MCMC/Lucah": 5, "PDRM/Judi": 5, "KPKT/Judi": 7}
	if fmt.Sprint(byKey) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", byKey, want)
	}
}
