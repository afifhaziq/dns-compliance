package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCreateCase_LinksURLWithPhase(t *testing.T) {
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
	if len(cases) != 1 || cases[0].Phase != "requested" {
		t.Fatalf("got %+v, want one case with phase=requested", cases)
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
	if _, err := store.AddURLToCase(ctx, c.ID, u2.ID, "requested"); err != nil {
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
	if c.Status != "uplift" {
		t.Fatalf("Case.Status = %q, want uplift", c.Status)
	}
	if c.AgencyID == nil || *c.AgencyID != agency.ID {
		t.Fatalf("Case.AgencyID = %v, want %d", c.AgencyID, agency.ID)
	}
	if c.DueDate == nil || !c.DueDate.Equal(due) {
		t.Fatalf("Case.DueDate = %v, want %v", c.DueDate, due)
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
	agency, _ := store.CreateAgency(ctx, "MCMC")
	u, _ := store.CreateURL(ctx, "update-case.com")
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}

	due := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	duePtr := &due
	agencyIDPtr := &agency.ID
	newStatus := "uplift"
	found, err := store.UpdateCaseFields(ctx, dept.ID, c.ID, db.CaseFields{
		AgencyID: &agencyIDPtr, Status: &newStatus, DueDate: &duePtr,
	})
	if err != nil || !found {
		t.Fatalf("UpdateCaseFields(set): found=%v err=%v", found, err)
	}

	got, err := store.GetCase(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCase: %v", err)
	}
	if got.Status != "uplift" || got.AgencyID == nil || *got.AgencyID != agency.ID || got.DueDate == nil || !got.DueDate.Equal(due) {
		t.Fatalf("expected fields to be set, got %+v", got)
	}

	// Updating only RequestedAt must not clobber the fields set above.
	requestedAt := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	requestedAtPtr := &requestedAt
	found, err = store.UpdateCaseFields(ctx, dept.ID, c.ID, db.CaseFields{RequestedAt: &requestedAtPtr})
	if err != nil || !found {
		t.Fatalf("UpdateCaseFields(requested_at only): found=%v err=%v", found, err)
	}
	got, _ = store.GetCase(ctx, c.ID)
	if got.RequestedAt == nil || !got.RequestedAt.Equal(requestedAt) {
		t.Fatalf("expected requested_at to be set, got %+v", got)
	}
	if got.Status != "uplift" || got.AgencyID == nil {
		t.Fatalf("expected status/agency_id to remain untouched, got %+v", got)
	}

	// Clear AgencyID and DueDate (outer non-nil, inner nil).
	var nilAgencyID *uint
	var nilDueDate *time.Time
	found, err = store.UpdateCaseFields(ctx, dept.ID, c.ID, db.CaseFields{AgencyID: &nilAgencyID, DueDate: &nilDueDate})
	if err != nil || !found {
		t.Fatalf("UpdateCaseFields(clear): found=%v err=%v", found, err)
	}
	got, _ = store.GetCase(ctx, c.ID)
	if got.AgencyID != nil || got.DueDate != nil {
		t.Fatalf("expected agency_id/due_date to be cleared, got %+v", got)
	}
}

// TestUpdateCaseFields_UnknownCase covers the false-not-error result for a
// case id that doesn't exist.
func TestUpdateCaseFields_UnknownCase(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	status := "uplift"
	found, err := store.UpdateCaseFields(ctx, 1, 999, db.CaseFields{Status: &status})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for an unknown case id")
	}
}

// TestUpdateCaseURLPhase covers the per-domain override: Case.Status is the
// default every url in the case starts with, but UpdateCaseURLPhase can
// diverge one specific url's CaseURL.Phase without touching the case's own
// Status or any other url's Phase.
func TestUpdateCaseURLPhase(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, _ := store.CreateDepartment(ctx, "PhaseOverrideDept")
	u1, _ := store.CreateURL(ctx, "phase-a.com")
	u2, _ := store.CreateURL(ctx, "phase-b.com")

	c, err := store.CreateCase(ctx, dept.ID, u1.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddURLToCase(ctx, c.ID, u2.ID, "requested"); err != nil {
		t.Fatalf("AddURLToCase: %v", err)
	}

	found, err := store.UpdateCaseURLPhase(ctx, c.ID, u2.ID, "uplift")
	if err != nil || !found {
		t.Fatalf("UpdateCaseURLPhase: found=%v err=%v", found, err)
	}

	casesU1, _ := store.ListCasesForURL(ctx, "phase-a.com")
	casesU2, _ := store.ListCasesForURL(ctx, "phase-b.com")
	if len(casesU1) != 1 || casesU1[0].Phase != "requested" {
		t.Fatalf("expected phase-a.com's phase to stay requested, got %+v", casesU1)
	}
	if len(casesU2) != 1 || casesU2[0].Phase != "uplift" {
		t.Fatalf("expected phase-b.com's phase to be overridden to uplift, got %+v", casesU2)
	}
	if casesU2[0].Status != "requested" {
		t.Fatalf("expected Case.Status to remain the original default (requested), got %q", casesU2[0].Status)
	}

	// No such (case, url) pair.
	found, err = store.UpdateCaseURLPhase(ctx, c.ID, 999999, "uplift")
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
// a CaseURL with Phase = Status), and skip the second entirely (logged, not
// fatal) since there's no department to attribute a Case to.
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
		if c.Status != "uplift" || c.DueDate == nil || !c.DueDate.Equal(due) {
			t.Fatalf("expected backfilled case to carry the legacy status/due_date forward, got %+v", c)
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
		if cu.Phase != "uplift" {
			t.Fatalf("expected case_url.phase to match the legacy status, got %q", cu.Phase)
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
	if _, err := store.AddURLToCase(ctx, c.ID, u2.ID, "requested"); err != nil {
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
	if s.AgencyName != "MCMC" {
		t.Fatalf("AgencyName = %q, want MCMC", s.AgencyName)
	}
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

// TestUpdateCaseLetterFields_PartialUpdateAndScoping covers the partial-
// update contract (touching one field leaves the others alone) and that
// the update is scoped by (caseID, letterID) — a letter can't be edited
// through the wrong case id, same defense-in-depth UpdateCaseURLPhase uses.
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
