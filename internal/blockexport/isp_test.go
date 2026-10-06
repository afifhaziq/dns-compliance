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
	row := func(isp string, urlID uint, url string, serverID uint, server string, day int) ISPUnblockedRow {
		return ISPUnblockedRow{ISP: isp, Row: db.ISPUnblockedRow{
			URLID: urlID, URL: url, DNSServerID: serverID, DNSServerName: server, NoticeDate: &notice, CurrentReferenceNumber: "REF-" + url,
			ScannedAt: time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC),
		}}
	}
	servers := []db.DNSServer{
		{ID: 3, ISP: "TM/Unifi: [x]", Name: "T1", Address: "10.0.0.3", Protocol: "udp", Enabled: true},
		{ID: 2, ISP: "Google", Name: "G2", Address: "8.8.4.4", Protocol: "udp"},
		{ID: 1, ISP: "Google", Name: "G1", Address: "8.8.8.8", Protocol: "dot", Enabled: true},
	}
	isps := []ISPExport{
		{ISP: "Google", Rows: []ISPUnblockedRow{row("Google", 1, "a.com", 2, "G2", 29), row("Google", 1, "a.com", 1, "G1", 28), row("Google", 2, "b.com", 1, "G1", 28)}},
		{ISP: "TM/Unifi: [x]", Rows: []ISPUnblockedRow{row("TM/Unifi: [x]", 1, "a.com", 3, "T1", 30)}},
	}
	var buf bytes.Buffer
	if err := WriteAllISPUnblockedWorkbook(servers, isps, &buf); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.GetSheetList(); len(got) != 4 || got[0] != "Summary" || got[1] != "DNS Servers" || got[2] != "Google" || got[3] != "TM_Unifi_ _x_" {
		t.Fatalf("unexpected sheets: %q", got)
	}
	summary, _ := f.GetRows("Summary")
	// One row per (domain, server), sorted domain, ISP, server name.
	if len(summary) != 5 {
		t.Fatalf("want header + 4 rows, got %q", summary)
	}
	if a := summary[1]; a[0] != "a.com" || a[1] != "Google" || a[2] != "G1" || a[3] != "2026-09-28 08:00" || a[6] != "REF-a.com" {
		t.Fatalf("unexpected first row: %q", a)
	}
	if got := []string{summary[2][2], summary[3][1], summary[4][0]}; got[0] != "G2" || got[1] != "TM/Unifi: [x]" || got[2] != "b.com" {
		t.Fatalf("unexpected order: %q", summary)
	}
	for _, sheet := range f.GetSheetList() {
		if tables, _ := f.GetTables(sheet); len(tables) != 1 {
			t.Fatalf("sheet %q: want 1 table, got %d", sheet, len(tables))
		}
	}
	srv, _ := f.GetRows("DNS Servers")
	if len(srv) != 4 || srv[1][0] != "G1" || srv[1][1] != "Google" || srv[1][2] != "8.8.8.8" || srv[2][4] != "No" {
		t.Fatalf("unexpected DNS Servers sheet: %q", srv)
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
