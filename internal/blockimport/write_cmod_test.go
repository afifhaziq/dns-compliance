package blockimport

import (
	"context"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
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
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, cases, false)
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
	if cu.Status != "uplift" {
		t.Errorf("Status = %q, want uplift (case has an Uplift letter)", cu.Status)
	}
}

func TestWriteCMODCases_PreservesUnmatchedOICInRemarks(t *testing.T) {
	gdb := newTestGormDB(t)
	cmod := mustSeedDepartment(t, gdb, "CMOD")
	cases := []CollapsedCMODCase{{
		BaseReferenceNumber: "REF(1)",
		Letters: []CMODRow{
			{ReferenceNumber: "REF(1-1)", Type: "Memo", OIC: "Atiqah", Remarks: ""},
		},
		URLs: []string{"example.com"},
	}}
	if _, err := WriteCMODCases(context.Background(), gdb, cmod.ID, cases, false); err != nil {
		t.Fatalf("WriteCMODCases: %v", err)
	}
	var cl db.CaseLetter
	if err := gdb.First(&cl).Error; err != nil {
		t.Fatalf("CaseLetter: %v", err)
	}
	if cl.OICUserID != nil {
		t.Errorf("OICUserID = %v, want nil (never linked, per the EDA doc caveat)", cl.OICUserID)
	}
	if want := "OIC (unmatched): Atiqah"; cl.Remarks != want {
		t.Errorf("Remarks = %q, want %q", cl.Remarks, want)
	}
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
	if _, err := WriteCMODCases(context.Background(), gdb, cmod.ID, cases, false); err != nil {
		t.Fatalf("first WriteCMODCases: %v", err)
	}
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, cases, false)
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
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, cases, false)
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
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, cases, true)
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

func TestMapCMODPhase(t *testing.T) {
	onlyBlock := []CMODRow{{Type: "Memo"}, {Type: "Notice"}}
	withUplift := []CMODRow{{Type: "Memo"}, {Type: "Notice"}, {Type: "Notice (Uplift)"}}
	if got := mapCMODPhase(onlyBlock); got != "requested" {
		t.Errorf("mapCMODPhase(onlyBlock) = %q, want requested", got)
	}
	if got := mapCMODPhase(withUplift); got != "uplift" {
		t.Errorf("mapCMODPhase(withUplift) = %q, want uplift", got)
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
