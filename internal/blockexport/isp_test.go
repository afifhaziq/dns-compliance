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
