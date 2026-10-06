package blockexport

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/afif/dns-tracking/internal/db"
)

func TestWriteISPUnblockedWorkbook(t *testing.T) {
	days := 3
	var buf bytes.Buffer
	err := WriteISPUnblockedWorkbook([]ISPUnblockedRow{{
		ISP: "Google", DaysOpen: &days,
		Row: db.ISPUnblockedRow{URL: "bad.com", DNSServerName: "Google DNS", DNSServerAddress: "8.8.8.8:53", ResolvedIP: "1.2.3.4", ScannedAt: time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC)},
	}}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := f.GetRows("Sheet1")
	if len(rows) != 2 || rows[1][0] != "Google" || rows[1][2] != "8.8.8.8:53" || rows[1][4] != "bad.com" || rows[1][15] != "3" {
		t.Fatalf("unexpected rows: %q", rows)
	}
	if rows[1][9] != "2026-10-06 02:00" { // 18:00 UTC = 02:00 MYT next day
		t.Fatalf("scan date not in MYT: %q", rows[1][9])
	}
}

func TestWriteAllISPUnblockedWorkbook(t *testing.T) {
	notice := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	row := func(isp string, urlID uint, url, server string) ISPUnblockedRow {
		return ISPUnblockedRow{ISP: isp, Row: db.ISPUnblockedRow{URLID: urlID, URL: url, DNSServerName: server, NoticeDate: &notice}}
	}
	isps := []ISPExport{
		{ISP: "Google", ServerCount: 2, PreviousCount: 3, Rows: []ISPUnblockedRow{
			row("Google", 1, "a.com", "G1"), row("Google", 1, "a.com", "G2"), row("Google", 2, "b.com", "G1"),
		}},
		{ISP: "TM/Unifi: [x]", ServerCount: 1, Rows: []ISPUnblockedRow{row("TM/Unifi: [x]", 1, "a.com", "T1")}},
	}
	var buf bytes.Buffer
	since := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	if err := WriteAllISPUnblockedWorkbook(isps, since, since.AddDate(0, 0, 7), &buf); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.GetSheetList(); len(got) != 4 || got[0] != "Summary" || got[1] != "Matrix" || got[2] != "Google" || got[3] != "TM_Unifi_ _x_" {
		t.Fatalf("unexpected sheets: %q", got)
	}
	summary, _ := f.GetRows("Summary")
	// Google: 2 domains, 3 rows, previous 3 -> change -1.
	if g := summary[4]; g[0] != "Google" || g[1] != "2" || g[2] != "2" || g[3] != "3" || g[4] != "3" || g[5] != "-1" {
		t.Fatalf("unexpected Google summary row: %q", g)
	}
	matrix, _ := f.GetRows("Matrix")
	// a.com is not blocked on both ISPs, b.com only on Google.
	if len(matrix) != 3 || matrix[1][0] != "a.com" || matrix[1][2] != "2" || matrix[1][3] != "Not blocked" || matrix[1][4] != "Not blocked" {
		t.Fatalf("unexpected matrix: %q", matrix)
	}
	if b := matrix[2]; b[0] != "b.com" || b[2] != "1" || b[3] != "Not blocked" || (len(b) > 4 && b[4] != "") {
		t.Fatalf("unexpected b.com matrix row: %q", b)
	}
	if rows, _ := f.GetRows("Google"); len(rows) != 4 || rows[0][0] != "ISP" {
		t.Fatalf("expected Google sheet in per-ISP layout, got %q", rows)
	}
}

func TestSheetNameUnique(t *testing.T) {
	used := map[string]bool{"summary": true}
	if got := sheetName("Summary", used); got != "Summary (2)" {
		t.Fatalf("got %q", got)
	}
	long := "An ISP Name That Is Far Longer Than Excel Allows"
	a, b := sheetName(long, used), sheetName(long, used)
	if len([]rune(a)) > 31 || len([]rune(b)) > 31 || a == b {
		t.Fatalf("got %q / %q", a, b)
	}
}
