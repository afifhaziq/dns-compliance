package blockimport

import (
	"context"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"gorm.io/gorm"
)

// newTestGormDB and mustSeedDepartment are defined once in write_test.go and
// shared by every test in this package.

func TestWriteCMODCases_CreatesFourLettersForFullLifecycle(t *testing.T) {
	gdb := newTestGormDB(t)
	cmod := mustSeedDepartment(t, gdb, "CMOD")
	cases := []CollapsedCMODCase{{
		BaseReferenceNumber: "REF(1)",
		Letters: []CMODRow{
			{ReferenceNumber: "REF(1-1)", Type: "Memo"},
			{ReferenceNumber: "REF(1-2)", Type: "Notice"},
			{ReferenceNumber: "REF(1-3)", Type: "Memo (Uplift)"},
			{ReferenceNumber: "REF(1-4)", Type: "Notice (Uplift)"},
		},
		URLs: []string{"example.com"},
	}}
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, 0, cases, "", false)
	if err != nil {
		t.Fatalf("WriteCMODCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1", summary.CasesCreated)
	}
	var letterCount int64
	gdb.Model(&db.CaseLetter{}).Count(&letterCount)
	if letterCount != 4 {
		t.Fatalf("case_letters count = %d, want 4", letterCount)
	}
	var cu db.CaseURL
	if err := gdb.First(&cu).Error; err != nil {
		t.Fatalf("CaseURL: %v", err)
	}
	if cu.Status != "" {
		t.Errorf("Status = %q, want \"\" (CMOD domains carry no block status)", cu.Status)
	}
}

func TestWriteCMODCases_CreatesAndLinksOICUsers(t *testing.T) {
	gdb := newTestGormDB(t)
	cmod := mustSeedDepartment(t, gdb, "CMOD")
	cases := []CollapsedCMODCase{{
		BaseReferenceNumber: "REF(1)",
		Letters: []CMODRow{
			{ReferenceNumber: "REF(1-1)", Type: "Memo", OIC: "Mas Atika"},
			{ReferenceNumber: "REF(1-2)", Type: "Notice", OIC: "Mas Atika; Jamal", Remarks: "urgent"},
		},
		URLs: []string{"example.com"},
	}}
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, 0, cases, "pw", false)
	if err != nil {
		t.Fatalf("WriteCMODCases: %v", err)
	}
	if summary.UsersCreated != 2 {
		t.Fatalf("UsersCreated = %d, want 2", summary.UsersCreated)
	}
	var u db.User
	if err := gdb.Where("username = ?", "mas_atika").First(&u).Error; err != nil {
		t.Fatalf("user mas_atika: %v", err)
	}
	if !u.MustChangePassword || u.DepartmentID == nil || *u.DepartmentID != cmod.ID || !db.CheckPassword(u.PasswordHash, "pw") {
		t.Errorf("user = %+v, want CMOD, forced change, password pw", u)
	}
	var memo, notice db.CaseLetter
	gdb.Where("type = ?", "Memo").First(&memo)
	gdb.Where("type = ?", "Notice").First(&notice)
	if memo.OICUserID == nil || *memo.OICUserID != u.ID || memo.Remarks != "" {
		t.Errorf("memo OIC/remarks = %v/%q, want %d/\"\"", memo.OICUserID, memo.Remarks, u.ID)
	}
	if want := "OIC: Mas Atika; Jamal — urgent"; notice.Remarks != want {
		t.Errorf("notice remarks = %q, want %q", notice.Remarks, want)
	}
}

func TestWriteCMODCases_LinksExistingCRDCase(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cmod := mustSeedDepartment(t, gdb, "CMOD")
	// What the CRD importer leaves for a CMOD notice: the ref on the
	// internal column, a CRD block status, a finer offence.
	crdCase := db.Case{DepartmentID: crd.ID}
	gdb.Create(&crdCase)
	gdb.Create(&db.CaseLetter{CaseID: crdCase.ID, Type: "Notice", ReferenceNumberInternal: "REF(1-2)"})
	a := db.URL{URL: "a.com"}
	gdb.Create(&a)
	gdb.Create(&db.CaseURL{CaseID: crdCase.ID, URLID: a.ID, Status: "blocked"})
	gdb.Create(&db.DepartmentURL{DepartmentID: crd.ID, URLID: a.ID})
	gdb.Model(&db.DepartmentURL{}).Where("url_id = ?", a.ID).Update("enabled", false)
	cat := mustSeedCategory(t, gdb)
	gdb.Create(&db.URLOffence{URLID: a.ID, CaseID: &crdCase.ID, CategoryID: cat, RecordedAt: time.Now()})

	cases := []CollapsedCMODCase{{
		BaseReferenceNumber: "REF(1)",
		Letters: []CMODRow{
			{ReferenceNumber: "REF(1-1)", Type: "Memo", Status: "Submitted"},
			{ReferenceNumber: "REF(1-2)", Type: "Notice", Status: "Submitted", Recipient: "ISP"},
		},
		URLs:         []string{"a.com", "b.com"},
		AgencyByURL:  map[string]string{"a.com": "PDRM", "b.com": "MCMC"},
		OffenceByURL: map[string]string{"a.com": "Judi dalam talian", "b.com": "Palsu, Jelik Melampau"},
	}}
	for run := 0; run < 2; run++ {
		summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, crd.ID, cases, "", false)
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if run == 0 && (summary.CasesLinked != 1 || summary.CasesCreated != 0 || summary.URLOffencesCreated != 2) {
			t.Fatalf("first run = %+v, want 1 linked, 0 created, 2 offences (b.com only)", summary)
		}
		if run == 1 && summary.CasesSkippedExist != 1 {
			t.Fatalf("second run = %+v, want 1 skipped", summary)
		}
	}

	var got db.Case
	gdb.First(&got, crdCase.ID)
	if got.DepartmentID != cmod.ID {
		t.Errorf("case department = %d, want CMOD %d", got.DepartmentID, cmod.ID)
	}
	var n int64
	gdb.Model(&db.Case{}).Count(&n)
	if n != 1 {
		t.Errorf("cases = %d, want 1 (linked, not duplicated)", n)
	}
	var notices []db.CaseLetter
	gdb.Where("type = ?", "Notice").Find(&notices)
	if len(notices) != 1 || notices[0].ReferenceNumberExternal != "REF(1-2)" || notices[0].ReferenceNumberInternal != "REF(1-2)" || notices[0].WorkflowStatus != "Submitted" || notices[0].Recipient != "ISP" {
		t.Errorf("notices = %+v, want one merged Notice", notices)
	}
	var cus []db.CaseURL
	gdb.Order("url_id").Find(&cus)
	if len(cus) != 2 {
		t.Fatalf("case_urls = %d, want 2", len(cus))
	}
	for _, cu := range cus {
		if cu.Status != "" || cu.AgencyID == nil {
			t.Errorf("case_url %+v, want status \"\" and an agency", cu)
		}
	}
	var crdListed int64
	gdb.Model(&db.DepartmentURL{}).Where("department_id = ?", crd.ID).Count(&crdListed)
	if crdListed != 0 {
		t.Errorf("CRD watchlist rows = %d, want 0 (its only CRD case moved to CMOD)", crdListed)
	}
	var aOffences int64
	gdb.Model(&db.URLOffence{}).Where("url_id = ?", a.ID).Count(&aOffences)
	if aOffences != 1 {
		t.Errorf("a.com offences = %d, want 1 (CRD's kept, CMOD's not added)", aOffences)
	}
}

func mustSeedCategory(t *testing.T, gdb *gorm.DB) uint {
	t.Helper()
	in := db.Instrument{Type: "ACT", Jurisdiction: "FEDERAL", ShortTitle: "X"}
	gdb.Create(&in)
	ci := db.Citation{InstrumentID: in.ID, RawText: "Seksyen 1"}
	gdb.Create(&ci)
	cat := db.Category{CitationID: ci.ID, Name: "X"}
	if err := gdb.Create(&cat).Error; err != nil {
		t.Fatalf("seeding category: %v", err)
	}
	return cat.ID
}

func TestWriteCMODCases_IsIdempotent(t *testing.T) {
	gdb := newTestGormDB(t)
	cmod := mustSeedDepartment(t, gdb, "CMOD")
	cases := []CollapsedCMODCase{{
		BaseReferenceNumber: "REF(1)",
		Letters: []CMODRow{
			{ReferenceNumber: "REF(1-1)", Type: "Memo"},
			{ReferenceNumber: "REF(1-2)", Type: "Notice"},
		},
		URLs: []string{"example.com"},
	}}
	if _, err := WriteCMODCases(context.Background(), gdb, cmod.ID, 0, cases, "", false); err != nil {
		t.Fatalf("first WriteCMODCases: %v", err)
	}
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, 0, cases, "", false)
	if err != nil {
		t.Fatalf("second WriteCMODCases: %v", err)
	}
	if summary.CasesCreated != 0 || summary.CasesSkippedExist != 1 {
		t.Fatalf("got CasesCreated=%d CasesSkippedExist=%d, want 0/1", summary.CasesCreated, summary.CasesSkippedExist)
	}
	var count int64
	gdb.Model(&db.Case{}).Count(&count)
	if count != 1 {
		t.Fatalf("cases table has %d rows, want 1", count)
	}
}

func TestWriteCMODCases_SkipsUnnormalizableURL(t *testing.T) {
	gdb := newTestGormDB(t)
	cmod := mustSeedDepartment(t, gdb, "CMOD")
	cases := []CollapsedCMODCase{{
		BaseReferenceNumber: "REF(1)",
		Letters: []CMODRow{
			{ReferenceNumber: "REF(1-1)", Type: "Memo"},
		},
		URLs: []string{"", "example.com"},
	}}
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, 0, cases, "", false)
	if err != nil {
		t.Fatalf("WriteCMODCases: %v", err)
	}
	if summary.URLsSkippedBadURL != 1 {
		t.Fatalf("URLsSkippedBadURL = %d, want 1", summary.URLsSkippedBadURL)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1 (case still created despite one bad URL)", summary.CasesCreated)
	}
	var cuCount int64
	gdb.Model(&db.CaseURL{}).Count(&cuCount)
	if cuCount != 1 {
		t.Fatalf("case_urls count = %d, want 1", cuCount)
	}
}

func TestWriteCMODCases_DryRunWritesNothing(t *testing.T) {
	gdb := newTestGormDB(t)
	cmod := mustSeedDepartment(t, gdb, "CMOD")
	cases := []CollapsedCMODCase{{
		BaseReferenceNumber: "REF(1)",
		Letters: []CMODRow{
			{ReferenceNumber: "REF(1-1)", Type: "Memo"},
		},
		URLs: []string{"example.com"},
	}}
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, 0, cases, "", true)
	if err != nil {
		t.Fatalf("WriteCMODCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1 (summary reflects what would happen)", summary.CasesCreated)
	}
	var count int64
	gdb.Model(&db.Case{}).Count(&count)
	if count != 0 {
		t.Fatalf("cases table has %d rows, want 0 (dry run must write nothing)", count)
	}
}

func TestOICUsername(t *testing.T) {
	for in, want := range map[string]string{"Mas Atika": "mas_atika", "Hazim  R": "hazim_r", "Adli": "adli"} {
		if got := OICUsername(in); got != want {
			t.Errorf("OICUsername(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseCMODDate(t *testing.T) {
	cases := map[string]bool{ // input -> expect non-nil
		"1-Jan-2026":                          true,
		"13-August-2026":                      true,
		"05-Jan-2026 18:37":                   true,
		"":                                    false,
		"Withdrawn (26 June 2026 @ 11:19 AM)": false,
	}
	for in, wantNonNil := range cases {
		got := parseCMODDate(in)
		if (got != nil) != wantNonNil {
			t.Errorf("parseCMODDate(%q) = %v, want non-nil=%v", in, got, wantNonNil)
		}
	}
}
