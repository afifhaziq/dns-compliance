package blockimport

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// writeTestXLSX builds a minimal .xlsx with the given sheet name and rows
// (first row is the header) and returns its path.
func writeTestXLSX(t *testing.T, sheetName string, rows [][]string) string {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	const defaultSheet = "Sheet1"
	if err := f.SetSheetName(defaultSheet, sheetName); err != nil {
		t.Fatalf("SetSheetName: %v", err)
	}
	for r, row := range rows {
		for c, val := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				t.Fatalf("CoordinatesToCellName: %v", err)
			}
			if err := f.SetCellValue(sheetName, cell, val); err != nil {
				t.Fatalf("SetCellValue: %v", err)
			}
		}
	}
	path := filepath.Join(t.TempDir(), "test.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func TestParseCRDRows_ResolvesColumnsByHeaderName(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"No. Rujukan NMD", "No. Rujukan NMSMD", "Alamat Laman Web", "Status", "Kategori", "Elemen", "Butiran Kesalahan", "Agensi", "Tahun"},
		{"REF-1", "", "http://example.com", "Blocked", "Judi", "", "Seksyen 233", "PDRM", "2023"},
	})
	rows, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	if len(rows) != 1 || rows[0].ReferenceNumber != "REF-1" || rows[0].Domain != "http://example.com" {
		t.Fatalf("got %+v", rows)
	}
	if rows[0].Status != "Blocked" || rows[0].Category != "Judi" || rows[0].CitationText != "Seksyen 233" || rows[0].Agency != "PDRM" || rows[0].Year != 2023 {
		t.Fatalf("got %+v", rows[0])
	}
}

func TestParseCRDRows_FixesColumnShiftBug(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"No. Rujukan NMD", "No. Rujukan NMSMD", "Alamat Laman Web", "Status", "Kategori", "Elemen", "Butiran Kesalahan", "Agensi", "Tahun"},
		{"REF-2", "", "http://shift.example.com", "Blocked", "", "Kepentingan Negara", "", "", "2020"},
	})
	rows, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Category != "Kepentingan Negara" || rows[0].Element != "" {
		t.Fatalf("got Category=%q Element=%q, want Category=Kepentingan Negara Element=empty", rows[0].Category, rows[0].Element)
	}
}

func TestParseCRDRows_StripsNumberedListPrefix(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"No. Rujukan NMD", "No. Rujukan NMSMD", "Alamat Laman Web", "Status", "Kategori", "Elemen", "Butiran Kesalahan", "Agensi", "Tahun"},
		{"REF-3", "", "19. http://www.example.net", "Blocked", "Judi", "", "", "", "2021"},
	})
	rows, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	if len(rows) != 1 || rows[0].Domain != "http://www.example.net" {
		t.Fatalf("got Domain=%q, want http://www.example.net", rows[0].Domain)
	}
}

func TestParseCRDRows_SkipsPreambleRowsBeforeHeader(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"", "Tahun", "", "", "Blocked"},
		{"", "2011", "", "", "5"},
		{"", "Tahun", "Alamat Laman Web", "Butiran Kesalahan", "Agensi", "Status", "No. Rujukan NMD", "Kategori", "Elemen"},
		{"", "2023", "http://example.com", "Seksyen 233", "PDRM", "Blocked", "REF-1", "Judi", ""},
	})
	rows, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	if len(rows) != 1 || rows[0].ReferenceNumber != "REF-1" || rows[0].Domain != "http://example.com" || rows[0].Year != 2023 {
		t.Fatalf("got %+v", rows)
	}
}

func TestParseCRDRows_StripsStraySpaceAfterScheme(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"No. Rujukan NMD", "No. Rujukan NMSMD", "Alamat Laman Web", "Status", "Kategori", "Elemen", "Butiran Kesalahan", "Agensi", "Tahun"},
		{"REF-4", "", "http:// www.foo.com", "Blocked", "Judi", "", "", "", "2021"},
	})
	rows, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	if len(rows) != 1 || rows[0].Domain != "http://www.foo.com" {
		t.Fatalf("got Domain=%q, want http://www.foo.com", rows[0].Domain)
	}
}

func findCase(t *testing.T, cases []CollapsedCase, ref string) CollapsedCase {
	t.Helper()
	for _, c := range cases {
		if c.ReferenceNumber == ref {
			return c
		}
	}
	t.Fatalf("no case found for reference %q", ref)
	return CollapsedCase{}
}

func TestCollapseCRDRows_GroupsByInternalReferenceNumber(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "SKMM(T)REF-1", Domain: "a.com", Status: "Blocked", Category: "Judi"},
		{ReferenceNumber: "SKMM(T)REF-1", Domain: "b.com", Status: "Uplift", Category: "Judi"},
		{ReferenceNumber: "SKMM(T)REF-2", Domain: "c.com", Status: "Blocked", Category: "Palsu"},
	}
	cases := CollapseCRDRows(rows)
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	ref1 := findCase(t, cases, "SKMM(T)REF-1")
	if len(ref1.Domains) != 2 {
		t.Fatalf("SKMM(T)REF-1 domains = %+v, want 2", ref1.Domains)
	}
}

// TestCollapseCRDRows_DoesNotGroupByExternalReference guards the fix for the
// blanket-reference problem: a non-internal reference like PDRM's real
// "JK KPN(PR) 168/6" is reused across 9,206 unrelated rows / 7,652 distinct
// domains in the real file, so trusting it as a shared grouping key would
// merge all of them into one case and collapse their Category/CitationText
// down to a single most-common winner, mislabeling every minority row. Two
// different domains citing the same external reference must NOT collapse
// into one case.
func TestCollapseCRDRows_DoesNotGroupByExternalReference(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "JK KPN(PR) 168/6", Domain: "a.com", Status: "Blocked", Category: "Judi"},
		{ReferenceNumber: "JK KPN(PR) 168/6", Domain: "b.com", Status: "Blocked", Category: "Pelacuran"},
	}
	cases := CollapseCRDRows(rows)
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2 (one per domain, not merged by shared external reference)", len(cases))
	}
	for _, c := range cases {
		if len(c.Domains) != 1 {
			t.Fatalf("case %+v has %d domains, want 1", c, len(c.Domains))
		}
		if c.ReferenceNumber != "JK KPN(PR) 168/6" {
			t.Fatalf("case %+v: ReferenceNumber = %q, want the original external text preserved", c, c.ReferenceNumber)
		}
	}
}

// TestCollapseCRDRows_GroupsRepeatedExternalReferenceDomainPair guards the
// other half: the *same* domain repeating under the *same* external
// reference (an exact-duplicate row, or a genuine re-block in a later year)
// must still collapse together, exactly like the internal-reference case
// already does -- only *different* domains sharing an external reference
// should stay split.
func TestCollapseCRDRows_GroupsRepeatedExternalReferenceDomainPair(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "JK KPN(PR) 168/6", Domain: "a.com", Status: "Blocked", Category: "Judi"},
		{ReferenceNumber: "JK KPN(PR) 168/6", Domain: "a.com", Status: "Uplift", Category: "Judi"},
	}
	cases := CollapseCRDRows(rows)
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1 (same reference + same domain still collapse)", len(cases))
	}
	if len(cases[0].Domains) != 1 || cases[0].Domains[0].Status != "Uplift" {
		t.Fatalf("got %+v, want one domain with last-write-wins status Uplift", cases[0])
	}
}

// TestCollapseCRDRows_GroupsBlankReferenceByDomain guards the other loose
// end from the same root cause: rows with no reference number at all (empty
// NMD, no NMSMD fallback either) previously all shared the same "" grouping
// key and piled into one giant case (up to 179 rows in the real file) --
// they now split per domain exactly like any other non-internal reference.
func TestCollapseCRDRows_GroupsBlankReferenceByDomain(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "", Domain: "a.com", Status: "Blocked", Category: "Judi"},
		{ReferenceNumber: "", Domain: "b.com", Status: "Blocked", Category: "Palsu"},
	}
	cases := CollapseCRDRows(rows)
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
}

// TestCollapseCRDRows_GroupsExternalReferenceByNormalizedDomain guards a
// grouping-key/idempotency-check mismatch: the key must normalize the
// domain, not compare it raw, or two spellings of the same site sharing a
// blanket external reference become two separate CollapsedCases here while
// WriteCRDCases's rerun check (which can only compare against the
// normalized urls.url column) would silently treat the second as "already
// imported" and never write it.
func TestCollapseCRDRows_GroupsExternalReferenceByNormalizedDomain(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "JK KPN(PR) 168/6", Domain: "http://foo.com", Status: "Blocked", Category: "Judi"},
		{ReferenceNumber: "JK KPN(PR) 168/6", Domain: "https://foo.com/", Status: "Uplift", Category: "Judi"},
	}
	cases := CollapseCRDRows(rows)
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1 (both spellings normalize to the same domain)", len(cases))
	}
	if len(cases[0].Domains) != 2 {
		t.Fatalf("got %d raw domain spellings, want 2 (both kept, WriteCRDCases dedupes by normalized URL ID)", len(cases[0].Domains))
	}
}

func TestIsInternalReference(t *testing.T) {
	cases := map[string]bool{
		"SKMM(T)09-NMD/800/2013/Jld.1(011)": true,
		"skmm(t)09-nmd/800/2013":            true, // case-insensitive
		"MCMC(S)CMOD/BLK/2025(62-2)":        true,
		"  MCMC(S)CMOD/BLK/2025(62-2)":      true, // leading whitespace
		"JK KPN(PR) 168/6":                  false,
		"SB-2021-0070-HQR":                  false,
		"EP(SIFU)-2020-0004-HQR":            false,
		"":                                  false,
	}
	for ref, want := range cases {
		if got := isInternalReference(ref); got != want {
			t.Errorf("isInternalReference(%q) = %v, want %v", ref, got, want)
		}
	}
}

// TestParseCRDRows_FallsBackToNMSMDWhenNMDBlank guards the 3 real rows where
// NMD itself is blank but NMSMD is populated -- rather than lose the only
// reference the row has, NMSMD stands in for it and isn't also carried
// separately (see CRDRow.NMSMD's doc comment).
func TestParseCRDRows_FallsBackToNMSMDWhenNMDBlank(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"No. Rujukan NMD", "No. Rujukan NMSMD", "Alamat Laman Web", "Status", "Kategori", "Elemen", "Butiran Kesalahan", "Agensi", "Tahun"},
		{"", "SKMM(T)09-NMMD/800/2017 (015)", "http://www.dropship.com", "Blocked", "Judi", "", "Seksyen 4", "PDRM", "2017"},
	})
	parsed, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("got %d rows, want 1", len(parsed))
	}
	if parsed[0].ReferenceNumber != "SKMM(T)09-NMMD/800/2017 (015)" {
		t.Fatalf("ReferenceNumber = %q, want the NMSMD fallback", parsed[0].ReferenceNumber)
	}
	if parsed[0].NMSMD != "" {
		t.Fatalf("NMSMD = %q, want empty once consumed as the ReferenceNumber fallback", parsed[0].NMSMD)
	}
}

// Also covers item 15 (docs/blocking-list-open-questions.md): the 474
// exact-duplicate rows share both reference number and domain, so they
// collapse to a single CollapsedDomain here without any dedicated dedup
// step -- no separate handling needed for that item.
func TestCollapseCRDRows_LastWriteWinsOnRepeatedDomainStatus(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "REF-1", Domain: "a.com", Status: "Blocked", Category: "Judi"},
		{ReferenceNumber: "REF-1", Domain: "a.com", Status: "Uplift", Category: "Judi"},
	}
	cases := CollapseCRDRows(rows)
	ref1 := findCase(t, cases, "REF-1")
	if len(ref1.Domains) != 1 || ref1.Domains[0].Status != "Uplift" {
		t.Fatalf("got %+v, want single domain with Status=Uplift", ref1.Domains)
	}
}

func TestCollapseCRDRows_SplitsCompoundCategory(t *testing.T) {
	rows := []CRDRow{{ReferenceNumber: "REF-1", Domain: "a.com", Category: "Jelik, Palsu, Lucah"}}
	cases := CollapseCRDRows(rows)
	ref1 := findCase(t, cases, "REF-1")
	want := []string{"Jelik", "Palsu", "Lucah"}
	if len(ref1.Categories) != len(want) {
		t.Fatalf("got Categories=%+v, want %+v", ref1.Categories, want)
	}
	for i, c := range want {
		if ref1.Categories[i] != c {
			t.Fatalf("got Categories=%+v, want %+v", ref1.Categories, want)
		}
	}
}
