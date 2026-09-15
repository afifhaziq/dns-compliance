package blockexport

import (
	"bytes"
	"strconv"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/xuri/excelize/v2"
)

func ptrTime(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestFlattenCRDRows_MultiOffenceExpandsToMultipleRows(t *testing.T) {
	cases := []db.CaseSummary{{
		ID: 1,
		Domains: []db.CaseSummaryDomain{{
			URL:        "example.com",
			Status:     "requested",
			AgencyName: "PDRM",
			Offences: []db.OffenceEntry{
				{Citation: "s.233", Category: "Judi"},
				{Citation: "s.234", Category: "Lucah"},
			},
		}},
	}}
	rows := FlattenCRDRows(cases, nil)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].AlamatLamanWeb != "example.com" || rows[1].AlamatLamanWeb != "example.com" {
		t.Fatalf("both rows should carry the same domain: %+v", rows)
	}
	if rows[0].Kategori != "Judi" || rows[1].Kategori != "Lucah" {
		t.Fatalf("expected distinct categories per row, got %+v", rows)
	}
	if rows[0].CaseID != 1 || rows[1].CaseID != 1 {
		t.Fatalf("expected CaseID 1 on both rows: %+v", rows)
	}
}

func TestFlattenCRDRows_ZeroOffenceFallsBackToOneRow(t *testing.T) {
	cases := []db.CaseSummary{{
		ID: 1,
		Domains: []db.CaseSummaryDomain{{URL: "example.com", Status: "requested"}},
	}}
	rows := FlattenCRDRows(cases, nil)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(rows), rows)
	}
	if rows[0].Kategori != "" || rows[0].ButiranKesalahan != "" {
		t.Fatalf("expected blank offence columns, got %+v", rows[0])
	}
}

func TestFlattenCRDRows_SplitsNoticeAndUpliftDatesIndependently(t *testing.T) {
	cases := []db.CaseSummary{{
		ID:      5,
		Domains: []db.CaseSummaryDomain{{URL: "a.com", Status: "uplift"}},
	}}
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 10, CaseID: 5, Type: "Notice", LetterDate: ptrTime(2024, 1, 10), ReferenceNumberExternal: "REF-EXT"}, DepartmentName: "CRD"},
		{CaseLetter: db.CaseLetter{ID: 11, CaseID: 5, Type: "Notice (Uplift)", LetterDate: ptrTime(2024, 6, 1)}, DepartmentName: "CRD"},
	}
	rows := FlattenCRDRows(cases, letters)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].TarikhBlocked != "2024-01-10" {
		t.Fatalf("TarikhBlocked = %q, want 2024-01-10", rows[0].TarikhBlocked)
	}
	if rows[0].TarikhUplift != "2024-06-01" {
		t.Fatalf("TarikhUplift = %q, want 2024-06-01", rows[0].TarikhUplift)
	}
	if rows[0].NoRujukanNMD != "REF-EXT" {
		t.Fatalf("NoRujukanNMD = %q, want REF-EXT", rows[0].NoRujukanNMD)
	}
	if rows[0].Department != "CRD" {
		t.Fatalf("Department = %q, want CRD", rows[0].Department)
	}
}

func TestFlattenCRDRows_DotMYSuffix(t *testing.T) {
	cases := []db.CaseSummary{{
		ID: 1,
		Domains: []db.CaseSummaryDomain{
			{URL: "example.com.my", Status: "requested"},
			{URL: "example.com", Status: "requested"},
		},
	}}
	rows := FlattenCRDRows(cases, nil)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	got := map[string]string{rows[0].AlamatLamanWeb: rows[0].DotMY, rows[1].AlamatLamanWeb: rows[1].DotMY}
	if got["example.com.my"] != "Yes" {
		t.Fatalf("example.com.my DotMY = %q, want Yes", got["example.com.my"])
	}
	if got["example.com"] != "No" {
		t.Fatalf("example.com DotMY = %q, want No", got["example.com"])
	}
}

func TestFlattenCRDRows_StatusTitleCased(t *testing.T) {
	cases := []db.CaseSummary{{
		ID:      1,
		Domains: []db.CaseSummaryDomain{{URL: "a.com", Status: "requested"}},
	}}
	rows := FlattenCRDRows(cases, nil)
	if rows[0].Status != "Requested" {
		t.Fatalf("Status = %q, want Requested", rows[0].Status)
	}
}

func TestFlattenCRDRows_YearFallbackChain(t *testing.T) {
	requestedAt := ptrTime(2022, 3, 1)
	cases := []db.CaseSummary{{
		ID:          1,
		RequestedAt: requestedAt,
		CreatedAt:   time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
		Domains:     []db.CaseSummaryDomain{{URL: "a.com", Status: "requested"}},
	}}
	rows := FlattenCRDRows(cases, nil) // no Notice letter -> falls back to RequestedAt
	if rows[0].Tahun != 2022 {
		t.Fatalf("Tahun = %d, want 2022 (RequestedAt fallback)", rows[0].Tahun)
	}
}

func TestWriteCRDWorkbook_RoundTrips(t *testing.T) {
	rows := []CRDRow{
		{Tahun: 2024, AlamatLamanWeb: "example.com", ButiranKesalahan: "s.233", Agensi: "PDRM",
			TarikhBlocked: "2024-01-10", NoRujukanNMD: "REF-1", Kategori: "Judi", Elemen: "E1",
			SubElemen: "SE1", Status: "Requested", TarikhUplift: "", NoRujukanNMSMD: "INT-1",
			Remarks: "note", DotMY: "No", CaseID: 1, Department: "CRD", DueDate: "2024-02-01"},
	}
	var buf bytes.Buffer
	if err := WriteCRDWorkbook(rows, &buf); err != nil {
		t.Fatalf("WriteCRDWorkbook: %v", err)
	}

	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()

	got, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(got) != 2 { // header + 1 data row
		t.Fatalf("got %d rows, want 2: %+v", len(got), got)
	}
	wantHeader := []string{
		"Tahun", "Alamat Laman Web", "Butiran Kesalahan", "Agensi",
		"Tarikh Maklum IASP (Blocked)", "No. Rujukan NMD", "Kategori", "Elemen",
		"Sub-Elemen", "Status", "Tarikh Maklum ISP (Uplift)", "No. Rujukan NMSMD",
		"Remarks", ".my", "Case ID", "Department", "Due Date",
	}
	for i, h := range wantHeader {
		if got[0][i] != h {
			t.Fatalf("header col %d = %q, want %q", i, got[0][i], h)
		}
	}
	if got[1][1] != "example.com" || got[1][5] != "REF-1" || got[1][14] != "1" {
		t.Fatalf("data row mismatch: %+v", got[1])
	}
}

func TestWriteCRDWorkbook_HeaderBoldAndDateColumnsAreRealDates(t *testing.T) {
	rows := []CRDRow{
		{Tahun: 2024, AlamatLamanWeb: "example.com", TarikhBlocked: "2024-01-10", DueDate: "2024-02-01"},
	}
	var buf bytes.Buffer
	if err := WriteCRDWorkbook(rows, &buf); err != nil {
		t.Fatalf("WriteCRDWorkbook: %v", err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()

	styleID, err := f.GetCellStyle("Sheet1", "A1")
	if err != nil {
		t.Fatalf("GetCellStyle: %v", err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil {
		t.Fatalf("GetStyle: %v", err)
	}
	if style.Font == nil || !style.Font.Bold {
		t.Fatalf("header cell A1 not bold: %+v", style.Font)
	}

	// TarikhBlocked is column E (index 4); its data row is row 2. A real
	// Excel date is stored as a numeric serial value, not the "2024-01-10"
	// text — but still displays as "2024-01-10" once the date style is applied.
	raw, err := f.GetCellValue("Sheet1", "E2", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("GetCellValue (raw): %v", err)
	}
	if _, err := strconv.ParseFloat(raw, 64); err != nil {
		t.Fatalf("TarikhBlocked raw value = %q, want a numeric date serial, got parse error: %v", raw, err)
	}
	if got, _ := f.GetCellValue("Sheet1", "E2"); got != "2024-01-10" {
		t.Fatalf("TarikhBlocked displayed value = %q, want 2024-01-10", got)
	}

	dateStyleID, err := f.GetCellStyle("Sheet1", "E2")
	if err != nil {
		t.Fatalf("GetCellStyle: %v", err)
	}
	dateStyle, err := f.GetStyle(dateStyleID)
	if err != nil {
		t.Fatalf("GetStyle: %v", err)
	}
	if dateStyle.CustomNumFmt == nil || *dateStyle.CustomNumFmt != dateNumFmt {
		t.Fatalf("TarikhBlocked cell CustomNumFmt = %v, want %q", dateStyle.CustomNumFmt, dateNumFmt)
	}
}
