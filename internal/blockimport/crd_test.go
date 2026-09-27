package blockimport

import (
	"path/filepath"
	"strings"
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

// TestCollapseCRDRows_AgencyIsPerDomainNotCollapsed guards the real-world
// case this package exists to model correctly: a single internal reference
// can legitimately cover domains requested by two different agencies (see
// docs/blocking-list-migration-clarifications.md's Agency section) — each
// domain must keep its own row's Agency, not a case-wide "most common"
// winner that would silently overwrite the minority domains' true agency.
func TestCollapseCRDRows_AgencyIsPerDomainNotCollapsed(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "SKMM(T)REF-1", Domain: "bet.example.com", Status: "Blocked", Category: "Judi", Agency: "PDRM"},
		{ReferenceNumber: "SKMM(T)REF-1", Domain: "adult.example.com", Status: "Blocked", Category: "Lucah", Agency: "MCMC"},
	}
	cases := CollapseCRDRows(rows)
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1 (one internal reference)", len(cases))
	}
	agencyByDomain := map[string]string{}
	for _, d := range cases[0].Domains {
		agencyByDomain[d.RawDomain] = d.Agency
	}
	if agencyByDomain["bet.example.com"] != "PDRM" || agencyByDomain["adult.example.com"] != "MCMC" {
		t.Fatalf("got %+v, want bet.example.com=PDRM, adult.example.com=MCMC (each domain keeps its own agency)", agencyByDomain)
	}
}

// TestCollapseCRDRows_LastWriteWinsOnRepeatedDomainAgency mirrors the
// existing Status last-write-wins test: a repeated (reference, domain) pair
// with a different Agency on its second occurrence keeps the later value,
// same as Status already does.
func TestCollapseCRDRows_LastWriteWinsOnRepeatedDomainAgency(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "SKMM(T)REF-1", Domain: "a.com", Status: "Blocked", Category: "Judi", Agency: "PDRM"},
		{ReferenceNumber: "SKMM(T)REF-1", Domain: "a.com", Status: "Blocked", Category: "Judi", Agency: "MCMC"},
	}
	cases := CollapseCRDRows(rows)
	if len(cases) != 1 || len(cases[0].Domains) != 1 {
		t.Fatalf("got %+v, want one case with one domain", cases)
	}
	if cases[0].Domains[0].Agency != "MCMC" {
		t.Fatalf("Domains[0].Agency = %q, want MCMC (last write wins)", cases[0].Domains[0].Agency)
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

func TestCollapseCRDRows_TieBreakIsDeterministic(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "KKMM/1", Domain: "a.com", Category: "Judi", CitationText: "Seksyen 4"},
		{ReferenceNumber: "KKMM/1", Domain: "b.com", Category: "Lucah", CitationText: "Seksyen 211"},
	}
	first := CollapseCRDRows(rows)[0]
	for i := 0; i < 50; i++ {
		got := CollapseCRDRows(rows)[0]
		if got.CitationText != first.CitationText || got.Categories[0] != first.Categories[0] {
			t.Fatalf("run %d: got %q/%v, first run was %q/%v", i, got.CitationText, got.Categories, first.CitationText, first.Categories)
		}
	}
}

func TestCollapseCRDRows_KeepsPerDomainClassificationTogether(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "MCMC(S)CMOD/1", Domain: "a.com", Category: "Judi", CitationText: "Seksyen 4"},
		{ReferenceNumber: "MCMC(S)CMOD/1", Domain: "b.com", Category: "Lucah", CitationText: "Seksyen 211"},
	}
	c := CollapseCRDRows(rows)[0]
	a, b := c.Domains[0].Offences, c.Domains[1].Offences
	if len(a) != 1 || a[0].CitationText != "Seksyen 4" || a[0].Categories[0] != "Judi" ||
		len(b) != 1 || b[0].CitationText != "Seksyen 211" || b[0].Categories[0] != "Lucah" {
		t.Fatalf("got a=%+v b=%+v", a, b)
	}
}

func TestParseNoticeDate(t *testing.T) {
	for raw, want := range map[string]string{"30-May-11": "2011-05-30", "7-Jul-12": "2012-07-07", " 13-Jan-26 ": "2026-01-13"} {
		if got := parseNoticeDate(raw); got == nil || got.Format("2006-01-02") != want {
			t.Errorf("parseNoticeDate(%q) = %v, want %s", raw, got, want)
		}
	}
	for _, raw := range []string{"", "NA", "Oct/Nov"} {
		if got := parseNoticeDate(raw); got != nil {
			t.Errorf("parseNoticeDate(%q) = %v, want nil", raw, got)
		}
	}
}

func TestCollapseCRDRows_UpliftDateIsEarliestInGroup(t *testing.T) {
	d1, d2 := parseNoticeDate("15-Nov-21"), parseNoticeDate("3-Jul-23")
	got := CollapseCRDRows([]CRDRow{
		{ReferenceNumber: "MCMC(S)X/2", Domain: "a.com", UpliftDate: d2},
		{ReferenceNumber: "MCMC(S)X/2", Domain: "b.com", UpliftDate: d1},
	})
	if len(got) != 1 || got[0].UpliftDate == nil || !got[0].UpliftDate.Equal(*d1) {
		t.Fatalf("got %+v, want earliest uplift date %v", got, d1)
	}
}

func TestCollapseCRDRows_NoticeDateIsEarliestInGroup(t *testing.T) {
	d1, d2 := parseNoticeDate("13-Jan-26"), parseNoticeDate("2-Jan-26")
	got := CollapseCRDRows([]CRDRow{
		{ReferenceNumber: "MCMC(S)X/1", Domain: "a.com", NoticeDate: d1},
		{ReferenceNumber: "MCMC(S)X/1", Domain: "b.com", NoticeDate: d2},
		{ReferenceNumber: "MCMC(S)X/1", Domain: "c.com"},
	})
	if len(got) != 1 || got[0].NoticeDate == nil || !got[0].NoticeDate.Equal(*d2) {
		t.Fatalf("got %+v, want the earliest date %v", got, d2)
	}
}

func TestParseCRDRows_PromotesOrphanSubElementToElement(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"No. Rujukan NMD", "No. Rujukan NMSMD", "Alamat Laman Web", "Status", "Kategori", "Elemen", "Sub-Elemen", "Butiran Kesalahan", "Agensi", "Tahun"},
		{"REF-3", "", "http://a.example.com", "Blocked", "Jelik", "", "Ngeri / Grafik keterlaluan", "", "", "2017"},
		{"REF-4", "", "http://b.example.com", "Blocked", "Jelik", "Dewasa", "Sub X", "", "", "2017"},
	})
	rows, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	if rows[0].Element != "Ngeri / Grafik Keterlaluan" || rows[0].SubElement != "" {
		t.Fatalf("orphan sub-element: got Element=%q SubElement=%q", rows[0].Element, rows[0].SubElement)
	}
	if rows[1].Element != "Dewasa" || rows[1].SubElement != "Sub X" {
		t.Fatalf("normal row changed: got Element=%q SubElement=%q", rows[1].Element, rows[1].SubElement)
	}
}

func TestParseCRDRows_CanonicalizesCategoryCasingToTitleCase(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"No. Rujukan NMD", "No. Rujukan NMSMD", "Alamat Laman Web", "Status", "Kategori", "Elemen", "Butiran Kesalahan", "Agensi", "Tahun"},
		{"R1", "", "http://a.example.com", "Blocked", "Tidak Berdaftar", "", "", "", "2020"},
		{"R2", "", "http://b.example.com", "Blocked", "Tidak berdaftar", "", "", "", "2020"},
		{"R3", "", "http://c.example.com", "Blocked", "Tidak berdaftar", "", "", "", "2020"},
		{"R4", "", "http://d.example.com", "Blocked", "Jelik, tidak berdaftar", "", "", "", "2020"},
	})
	rows, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	for i, want := range []string{"Tidak Berdaftar", "Tidak Berdaftar", "Tidak Berdaftar", "Jelik,Tidak Berdaftar"} {
		if rows[i].Category != want {
			t.Errorf("row %d: got %q, want %q", i, rows[i].Category, want)
		}
	}
}

func TestNormalizeLabel(t *testing.T) {
	for in, want := range map[string]string{
		"Kanak - kanak":              "Kanak-kanak",
		"Kanak-kanak":                "Kanak-kanak",
		"Keganasan/ Militan":         "Keganasan / Militan",
		"Keganasan / Militan":        "Keganasan / Militan",
		"  Dewasa  /Kanak - kanak ":  "Dewasa / Kanak-kanak",
		"Aktivit Perakaunan":         "Aktiviti Perakaunan",
		"Aktiviti Pasaran Model":     "Aktiviti Pasaran Modal",
		"Iklan & Penjualan Ubat":     "Iklan dan Penjualan Ubat",
		"Dadah Merbahaya":            "Dadah Berbahaya",
		"Keganasan/ Grafik Melampau": "Ngeri / Grafik Keterlaluan",
		"Ngeri / Grafik keterlaluan": "Ngeri / Grafik Keterlaluan",
	} {
		if got := normalizeLabel(in); got != want {
			t.Errorf("normalizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitCategories_SlashAndDan(t *testing.T) {
	for in, want := range map[string]string{
		"Mengancam/ Palsu": "Mengancam|Palsu",
		"Lucah dan Palsu":  "Lucah|Palsu",
		"Jelik, Palsu":     "Jelik|Palsu",
		"Pendidikan":       "Pendidikan",
		"Palsu (Phishing)": "Palsu (Phishing)",
	} {
		if got := strings.Join(splitCategories(in), "|"); got != want {
			t.Errorf("splitCategories(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitElement(t *testing.T) {
	for in, want := range map[string]string{
		"Dewasa / Kanak-kanak": "Dewasa|Kanak-kanak",
		"Keganasan / Militan":  "Keganasan / Militan",
		"":                     "",
	} {
		if got := strings.Join(splitElement(in), "|"); got != want {
			t.Errorf("splitElement(%q) = %q, want %q", in, got, want)
		}
	}
}

// A domain re-blocked later under the same blanket external reference is a
// separate event (its own case and year), while a same-date repeat still
// collapses.
func TestCollapseCRDRows_SplitsExternalReblockByNoticeDate(t *testing.T) {
	d23, d25 := parseNoticeDate("5-Mar-23"), parseNoticeDate("9-Jan-25")
	rows := []CRDRow{
		{ReferenceNumber: "JK KPN(PR) 168/6", Domain: "a.com", Status: "Blocked", NoticeDate: d23},
		{ReferenceNumber: "JK KPN(PR) 168/6", Domain: "a.com", Status: "Blocked", NoticeDate: d25},
		{ReferenceNumber: "JK KPN(PR) 168/6", Domain: "a.com", Status: "Blocked", NoticeDate: d25},
	}
	if got := len(CollapseCRDRows(rows)); got != 2 {
		t.Fatalf("got %d cases, want 2", got)
	}
}

func TestParseCRDRows_FoldsPhishingIntoPalsu(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"No. Rujukan NMD", "No. Rujukan NMSMD", "Alamat Laman Web", "Status", "Kategori", "Elemen", "Butiran Kesalahan", "Agensi", "Tahun"},
		{"R1", "", "http://a.example.com", "Blocked", "Palsu (Phishing)", "", "", "MCMC", "2025"},
		{"R2", "", "http://b.example.com", "Blocked", "Phishing", "", "", "MCMC", "2021"},
	})
	rows, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	for i, r := range rows {
		if r.Category != "Palsu" || r.Element != "Phishing" {
			t.Errorf("row %d: got %q › %q, want Palsu › Phishing", i, r.Category, r.Element)
		}
	}
}
