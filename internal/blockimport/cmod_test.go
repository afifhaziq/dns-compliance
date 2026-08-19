package blockimport

import (
	"reflect"
	"testing"
)

// writeTestXLSX is defined once in crd_test.go and shared by every test in
// this package.

func TestParseCMODRows_ResolvesColumnsByHeaderName(t *testing.T) {
	path := writeTestXLSX(t, "BLK", [][]string{
		{"title row placeholder"}, // row 1, per the EDA doc's "header on row 2"
		{"No", "Letter Date", "Recipient", "Type", "Subject", "Reference No", "OIC", "Requestor", "Offence", "Link (One Link Per Row)", "Remarks", "Agency", "Status", "Received", "Submission"},
		{"1", "1-Jan-2026", "ISP X", "Notice", "Blocking", "MCMC(S)CMOD/BLK/2026(1-2)", "Atiqah", "Someone", "Judi dalam talian", "example.com", "", "PDRM", "Submitted", "", ""},
	})
	rows, err := ParseCMODRows(path)
	if err != nil {
		t.Fatalf("ParseCMODRows: %v", err)
	}
	if len(rows) != 1 || rows[0].ReferenceNumber != "MCMC(S)CMOD/BLK/2026(1-2)" || rows[0].Type != "Notice" {
		t.Fatalf("got %+v", rows)
	}
}

func TestParseCMODRows_HandlesNewlineSuffixedHeader(t *testing.T) {
	path := writeTestXLSX(t, "BLK", [][]string{
		{"title row placeholder"},
		{"No", "Letter Date", "Recipient", "Type", "Subject", "Reference No", "OIC", "Requestor", "Offence", "Link (One Link Per Row)", "Remarks", "Agency", "Status", "Received\n(isi ikut format datetime 24H, jangan tukar format)", "Submission\n(isi ikut format)"},
		{"1", "1-Jan-2026", "ISP X", "Memo", "Blocking", "REF(1-1)", "Atiqah", "Someone", "Judi dalam talian", "example.com", "", "PDRM", "Draft", "1-Jan-2026 10:00", "2-Jan-2026"},
	})
	rows, err := ParseCMODRows(path)
	if err != nil {
		t.Fatalf("ParseCMODRows: %v", err)
	}
	if len(rows) != 1 || rows[0].Received != "1-Jan-2026 10:00" || rows[0].Submission != "2-Jan-2026" {
		t.Fatalf("got %+v", rows)
	}
}

func TestParseCMODRows_SplitsMalformedMultiRefCell(t *testing.T) {
	path := writeTestXLSX(t, "BLK", [][]string{
		{"title row placeholder"},
		{"No", "Letter Date", "Recipient", "Type", "Subject", "Reference No", "OIC", "Requestor", "Offence", "Link (One Link Per Row)", "Remarks", "Agency", "Status", "Received", "Submission"},
		{"1", "1-Jan-2026", "ISP X", "Memo", "Blocking", "MCMC(S)CMOD/BLK/2026(19-1, 20-1, 21-1)", "Atiqah", "Someone", "Judi dalam talian", "example.com", "", "PDRM", "Draft", "", ""},
	})
	rows, err := ParseCMODRows(path)
	if err != nil {
		t.Fatalf("ParseCMODRows: %v", err)
	}
	want := []string{
		"MCMC(S)CMOD/BLK/2026(19-1)",
		"MCMC(S)CMOD/BLK/2026(20-1)",
		"MCMC(S)CMOD/BLK/2026(21-1)",
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for i, r := range rows {
		if r.ReferenceNumber != want[i] {
			t.Errorf("rows[%d].ReferenceNumber = %q, want %q", i, r.ReferenceNumber, want[i])
		}
		if r.Links[0] != "example.com" {
			t.Errorf("rows[%d].Links = %v, want [example.com] (same domain attached to all 3)", i, r.Links)
		}
	}
}

func TestSplitLinks_HandlesNewlineJoinedCell(t *testing.T) {
	got := splitLinks("edisisiasat4.wordpress.com\nedisisiasat5.wordpress.com")
	want := []string{"edisisiasat4.wordpress.com", "edisisiasat5.wordpress.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplitLinks_SingleURL(t *testing.T) {
	got := splitLinks("example.com")
	want := []string{"example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBaseReferenceNumber_StripsSuffix(t *testing.T) {
	cases := map[string]string{
		"MCMC(S)CMOD/BLK/2026(19-1)":          "MCMC(S)CMOD/BLK/2026(19)",
		"MCMC(S)CMOD/BLK/2026(19-2)":          "MCMC(S)CMOD/BLK/2026(19)",
		"MCMC(S)NSS/CPMD/CMOD/BLK/2026(64-1)": "MCMC(S)NSS/CPMD/CMOD/BLK/2026(64)",
	}
	for in, want := range cases {
		if got := baseReferenceNumber(in); got != want {
			t.Errorf("baseReferenceNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitOffence_SplitsCompoundValue(t *testing.T) {
	got := splitOffence("Palsu, Jelik Melampau")
	want := []string{"Palsu", "Jelik Melampau"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplitOffence_SingleValue(t *testing.T) {
	got := splitOffence("Judi dalam talian")
	want := []string{"Judi dalam talian"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollapseCMODRows_GroupsMemoAndNoticeUnderOneCase(t *testing.T) {
	rows := []CMODRow{
		{ReferenceNumber: "REF(1-1)", Type: "Memo", Links: []string{"a.com"}},
		{ReferenceNumber: "REF(1-2)", Type: "Notice", Links: []string{"a.com"}},
	}
	cases := CollapseCMODRows(rows)
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(cases))
	}
	if len(cases[0].Letters) != 2 {
		t.Fatalf("got %d letters, want 2 (Memo + Notice)", len(cases[0].Letters))
	}
	if len(cases[0].URLs) != 1 {
		t.Fatalf("got %d URLs, want 1 deduplicated URL", len(cases[0].URLs))
	}
}

func TestCollapseCMODRows_CollapsesMultiDomainLetterToOneLetterRow(t *testing.T) {
	// A real Memo letter covers many domains -- one raw row per domain, all
	// sharing the same (base ref, Type) -- collapses to one CaseLetter, with
	// URLs unioned across the group.
	rows := []CMODRow{
		{ReferenceNumber: "REF(1-1)", Type: "Memo", OIC: "Jamal", Links: []string{"a.com"}},
		{ReferenceNumber: "REF(1-1)", Type: "Memo", OIC: "Jamal", Links: []string{"b.com"}},
		{ReferenceNumber: "REF(1-1)", Type: "Memo", OIC: "Arishah", Links: []string{"c.com"}},
	}
	cases := CollapseCMODRows(rows)
	if len(cases) != 1 || len(cases[0].Letters) != 1 {
		t.Fatalf("got %+v", cases)
	}
	if len(cases[0].URLs) != 3 {
		t.Fatalf("got %d URLs, want 3", len(cases[0].URLs))
	}
	if cases[0].Letters[0].OIC != "Jamal; Arishah" {
		t.Errorf("Letters[0].OIC = %q, want %q (distinct OIC values joined)", cases[0].Letters[0].OIC, "Jamal; Arishah")
	}
}

func TestCollapseCMODRows_SplitsThreeCaseNumbersInOneCell(t *testing.T) {
	rows, err := ParseCMODRows(writeTestXLSX(t, "BLK", [][]string{
		{"title row placeholder"},
		{"No", "Letter Date", "Recipient", "Type", "Subject", "Reference No", "OIC", "Requestor", "Offence", "Link (One Link Per Row)", "Remarks", "Agency", "Status", "Received", "Submission"},
		{"1", "1-Jan-2026", "ISP X", "Memo", "Blocking", "MCMC(S)CMOD/BLK/2026(19-1, 20-1, 21-1)", "Atiqah", "Someone", "Judi dalam talian", "example.com", "", "PDRM", "Draft", "", ""},
	}))
	if err != nil {
		t.Fatalf("ParseCMODRows: %v", err)
	}
	cases := CollapseCMODRows(rows)
	if len(cases) != 3 {
		t.Fatalf("got %d cases, want 3", len(cases))
	}
	seen := map[string]bool{}
	for _, c := range cases {
		seen[c.BaseReferenceNumber] = true
	}
	for _, want := range []string{"MCMC(S)CMOD/BLK/2026(19)", "MCMC(S)CMOD/BLK/2026(20)", "MCMC(S)CMOD/BLK/2026(21)"} {
		if !seen[want] {
			t.Errorf("missing case with base ref %q", want)
		}
	}
}
