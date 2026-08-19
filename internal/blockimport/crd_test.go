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

func TestCollapseCRDRows_GroupsByReferenceNumber(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "REF-1", Domain: "a.com", Status: "Blocked", Category: "Judi"},
		{ReferenceNumber: "REF-1", Domain: "b.com", Status: "Uplift", Category: "Judi"},
		{ReferenceNumber: "REF-2", Domain: "c.com", Status: "Blocked", Category: "Palsu"},
	}
	cases := CollapseCRDRows(rows)
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	ref1 := findCase(t, cases, "REF-1")
	if len(ref1.Domains) != 2 {
		t.Fatalf("REF-1 domains = %+v, want 2", ref1.Domains)
	}
}

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
