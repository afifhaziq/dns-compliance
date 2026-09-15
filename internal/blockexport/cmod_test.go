package blockexport

import (
	"bytes"
	"strconv"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/xuri/excelize/v2"
)

func cmodPtrTime(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestFlattenCMODRows_ExpandsPerURL(t *testing.T) {
	letters := []db.CaseLetterEntry{
		{
			CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice", Subject: "Blocking notice"},
			URLs:       []string{"a.com", "b.com", "c.com"},
		},
	}
	rows := FlattenCMODRows(letters, nil, nil, nil)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %+v", len(rows), rows)
	}
	links := map[string]bool{rows[0].Link: true, rows[1].Link: true, rows[2].Link: true}
	for _, want := range []string{"a.com", "b.com", "c.com"} {
		if !links[want] {
			t.Fatalf("missing link %q in rows %+v", want, rows)
		}
	}
	for _, r := range rows {
		if r.Subject != "Blocking notice" {
			t.Fatalf("Subject should repeat across expanded rows, got %+v", r)
		}
	}
}

func TestFlattenCMODRows_ExpandsPerOffenceWithinDomain(t *testing.T) {
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice"}, URLs: []string{"a.com"}},
	}
	offences := map[string][]db.OffenceEntry{
		"a.com": {{Category: "Judi"}, {Category: "Lucah"}},
	}
	rows := FlattenCMODRows(letters, nil, offences, nil)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].Offence != "Judi" || rows[1].Offence != "Lucah" {
		t.Fatalf("expected distinct offence categories per row: %+v", rows)
	}
}

func TestFlattenCMODRows_MissingOICUserRendersBlankNotPanic(t *testing.T) {
	oicID := uint(99) // not present in oicUsernames
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice", OICUserID: &oicID}, URLs: []string{"a.com"}},
	}
	rows := FlattenCMODRows(letters, map[uint]string{}, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].OIC != "" {
		t.Fatalf("OIC = %q, want blank for an unresolvable user id", rows[0].OIC)
	}

	// nil OICUserID must also render blank, not panic.
	letters2 := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 2, CaseID: 1, Type: "Notice"}, URLs: []string{"a.com"}},
	}
	rows2 := FlattenCMODRows(letters2, map[uint]string{1: "alice"}, nil, nil)
	if rows2[0].OIC != "" {
		t.Fatalf("OIC = %q, want blank for nil OICUserID", rows2[0].OIC)
	}
}

func TestFlattenCMODRows_ResolvesOICUsername(t *testing.T) {
	oicID := uint(7)
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice", OICUserID: &oicID}, URLs: []string{"a.com"}},
	}
	rows := FlattenCMODRows(letters, map[uint]string{7: "alice"}, nil, nil)
	if rows[0].OIC != "alice" {
		t.Fatalf("OIC = %q, want alice", rows[0].OIC)
	}
}

func TestFlattenCMODRows_SequentialNumberingAcrossLetters(t *testing.T) {
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice"}, URLs: []string{"a.com", "b.com"}},
		{CaseLetter: db.CaseLetter{ID: 2, CaseID: 2, Type: "Memo"}, URLs: []string{"c.com"}},
	}
	rows := FlattenCMODRows(letters, nil, nil, nil)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for i, r := range rows {
		if r.No != i+1 {
			t.Fatalf("row %d: No = %d, want %d", i, r.No, i+1)
		}
	}
}

func TestFlattenCMODRows_AgencyAndDatesResolved(t *testing.T) {
	letters := []db.CaseLetterEntry{
		{
			CaseLetter: db.CaseLetter{
				ID: 1, CaseID: 9, Type: "Notice",
				LetterDate: cmodPtrTime(2024, 5, 1), ReceivedAt: cmodPtrTime(2024, 5, 2),
				SubmittedAt: cmodPtrTime(2024, 5, 3),
			},
			URLs: []string{"a.com"},
		},
	}
	rows := FlattenCMODRows(letters, nil, nil, map[CaseURLKey]string{{CaseID: 9, URL: "a.com"}: "PDRM"})
	if rows[0].Agency != "PDRM" {
		t.Fatalf("Agency = %q, want PDRM", rows[0].Agency)
	}
	if rows[0].LetterDate != "2024-05-01" || rows[0].Received != "2024-05-02" || rows[0].Submission != "2024-05-03" {
		t.Fatalf("date columns mismatch: %+v", rows[0])
	}
}

func TestWriteCMODWorkbook_RoundTrips(t *testing.T) {
	rows := []CMODRow{
		{No: 1, LetterDate: "2024-05-01", Recipient: "ISP A", Type: "Notice", Subject: "Blocking",
			ReferenceNo: "REF-1", OIC: "alice", Requestor: "bob", Offence: "Judi", Link: "a.com",
			Remarks: "note", Agency: "PDRM", Status: "Submitted", Received: "2024-05-02",
			Submission: "2024-05-03", CaseID: 9, InternalRef: "INT-1"},
	}
	var buf bytes.Buffer
	if err := WriteCMODWorkbook(rows, &buf); err != nil {
		t.Fatalf("WriteCMODWorkbook: %v", err)
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
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(got), got)
	}
	wantHeader := []string{
		"No", "Letter Date", "Recipient", "Type", "Subject", "Reference No", "OIC",
		"Requestor", "Offence", "Link", "Remarks", "Agency", "Status", "Received",
		"Submission", "Case ID", "Internal Ref (No. Rujukan NMSMD)",
	}
	for i, h := range wantHeader {
		if got[0][i] != h {
			t.Fatalf("header col %d = %q, want %q", i, got[0][i], h)
		}
	}
	if got[1][9] != "a.com" || got[1][6] != "alice" || got[1][15] != "9" {
		t.Fatalf("data row mismatch: %+v", got[1])
	}
}

func TestWriteCMODWorkbook_HeaderBoldAndDateColumnsAreRealDates(t *testing.T) {
	rows := []CMODRow{
		{No: 1, LetterDate: "2024-05-01", Received: "2024-05-02", Submission: "2024-05-03"},
	}
	var buf bytes.Buffer
	if err := WriteCMODWorkbook(rows, &buf); err != nil {
		t.Fatalf("WriteCMODWorkbook: %v", err)
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

	// LetterDate is column B (index 1); its data row is row 2. A real Excel
	// date is stored as a numeric serial value, not the "2024-05-01" text —
	// but still displays as "2024-05-01" once the date style is applied.
	raw, err := f.GetCellValue("Sheet1", "B2", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("GetCellValue (raw): %v", err)
	}
	if _, err := strconv.ParseFloat(raw, 64); err != nil {
		t.Fatalf("LetterDate raw value = %q, want a numeric date serial, got parse error: %v", raw, err)
	}
	if got, _ := f.GetCellValue("Sheet1", "B2"); got != "2024-05-01" {
		t.Fatalf("LetterDate displayed value = %q, want 2024-05-01", got)
	}

	dateStyleID, err := f.GetCellStyle("Sheet1", "B2")
	if err != nil {
		t.Fatalf("GetCellStyle: %v", err)
	}
	dateStyle, err := f.GetStyle(dateStyleID)
	if err != nil {
		t.Fatalf("GetStyle: %v", err)
	}
	if dateStyle.CustomNumFmt == nil || *dateStyle.CustomNumFmt != dateNumFmt {
		t.Fatalf("LetterDate cell CustomNumFmt = %v, want %q", dateStyle.CustomNumFmt, dateNumFmt)
	}
}
