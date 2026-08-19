package blockimport

import (
	"context"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestGormDB(t *testing.T) *gorm.DB {
	t.Helper()
	gormDB, err := db.Connect(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	return gormDB
}

func mustSeedDepartment(t *testing.T, gdb *gorm.DB, name string) db.Department {
	t.Helper()
	dept := db.Department{Name: name}
	if err := gdb.Create(&dept).Error; err != nil {
		t.Fatalf("seeding department %q: %v", name, err)
	}
	return dept
}

func TestWriteCRDCases_CreatesOneCasePerReference(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: "example.com", Status: "Blocked"}},
		Categories:      []string{"Judi"},
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1", summary.CasesCreated)
	}
	var count int64
	gdb.Model(&db.Case{}).Count(&count)
	if count != 1 {
		t.Fatalf("cases table has %d rows, want 1", count)
	}

	var letter db.CaseLetter
	if err := gdb.First(&letter).Error; err != nil {
		t.Fatalf("expected a CaseLetter row: %v", err)
	}
	if letter.ReferenceNumber != "REF-1" || letter.Type != "Notice" {
		t.Fatalf("got letter %+v", letter)
	}

	var caseURL db.CaseURL
	if err := gdb.First(&caseURL).Error; err != nil {
		t.Fatalf("expected a CaseURL row: %v", err)
	}
	if caseURL.Phase != "requested" {
		t.Fatalf("caseURL.Phase = %q, want requested", caseURL.Phase)
	}

	var u db.URL
	if err := gdb.First(&u, caseURL.URLID).Error; err != nil {
		t.Fatalf("expected a URL row: %v", err)
	}
	if u.URL != "example.com" {
		t.Fatalf("u.URL = %q, want example.com", u.URL)
	}
}

func TestWriteCRDCases_IsIdempotent(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: "example.com", Status: "Blocked"}},
		Categories:      []string{"Judi"},
	}}

	if _, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, false); err != nil {
		t.Fatalf("first WriteCRDCases: %v", err)
	}
	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, false)
	if err != nil {
		t.Fatalf("second WriteCRDCases: %v", err)
	}
	if summary.CasesSkippedExist != 1 || summary.CasesCreated != 0 {
		t.Fatalf("got %+v, want CasesSkippedExist=1 CasesCreated=0", summary)
	}
	var count int64
	gdb.Model(&db.Case{}).Count(&count)
	if count != 1 {
		t.Fatalf("cases table has %d rows, want 1", count)
	}
}

func TestWriteCRDCases_SkipsUnnormalizableURL(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains: []CollapsedDomain{
			{RawDomain: "example.com", Status: "Blocked"},
			{RawDomain: "https://solar123movies.cB33:B69om/", Status: "Blocked"},
		},
		Categories: []string{"Judi"},
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.URLsSkippedBadURL != 1 {
		t.Fatalf("URLsSkippedBadURL = %d, want 1", summary.URLsSkippedBadURL)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1 (case still created for the valid domain)", summary.CasesCreated)
	}
	var caseURLCount int64
	gdb.Model(&db.CaseURL{}).Count(&caseURLCount)
	if caseURLCount != 1 {
		t.Fatalf("case_urls has %d rows, want 1", caseURLCount)
	}
}

func TestWriteCRDCases_DedupesDomainsThatNormalizeToTheSameURL(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains: []CollapsedDomain{
			{RawDomain: "http://example.com", Status: "Blocked"},
			{RawDomain: "https://example.com/", Status: "Uplift"},
		},
		Categories: []string{"Judi"},
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1", summary.CasesCreated)
	}
	var caseURLCount int64
	gdb.Model(&db.CaseURL{}).Count(&caseURLCount)
	if caseURLCount != 1 {
		t.Fatalf("case_urls has %d rows, want 1 (deduped by normalized URL)", caseURLCount)
	}
}

func TestWriteCRDCases_DryRunWritesNothing(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: "example.com", Status: "Blocked"}},
		Categories:      []string{"Judi"},
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, true)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1 (dry run still reports what would happen)", summary.CasesCreated)
	}
	var count int64
	gdb.Model(&db.Case{}).Count(&count)
	if count != 0 {
		t.Fatalf("cases table has %d rows, want 0 after dry run", count)
	}
	gdb.Model(&db.URL{}).Count(&count)
	if count != 0 {
		t.Fatalf("urls table has %d rows, want 0 after dry run", count)
	}
}

func TestWriteCRDCases_TracksCategoriesObserved(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{
		{ReferenceNumber: "REF-1", Domains: []CollapsedDomain{{RawDomain: "a.com", Status: "Blocked"}}, Categories: []string{"Judi"}},
		{ReferenceNumber: "REF-2", Domains: []CollapsedDomain{{RawDomain: "b.com", Status: "Blocked"}}, Categories: []string{"Judi", "Palsu"}},
	}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, true)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CategoriesObserved["Judi"] != 2 || summary.CategoriesObserved["Palsu"] != 1 {
		t.Fatalf("got %+v", summary.CategoriesObserved)
	}
}

func TestMapCRDStatus(t *testing.T) {
	cases := map[string]string{
		"Blocked": "requested", "blocked": "requested",
		"Uplift": "uplift", "Suspended": "suspended",
		"Not Blocked": "requested", "Not blocked": "requested", "": "requested",
	}
	for in, want := range cases {
		if got := mapCRDStatus(in); got != want {
			t.Errorf("mapCRDStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
