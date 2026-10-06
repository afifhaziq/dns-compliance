package blockexport

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/afif/dns-tracking/internal/db"
)

func TestWriteBlockingRegisterWorkbook(t *testing.T) {
	rows := []db.BlockingStatRow{
		{Year: 2021, Agency: "MCMC", Offence: "Lucah", Count: 99}, // before RegisterFirstYear: dropped
		{Year: 2022, Agency: "MCMC", Offence: "Lucah", Count: 3},
		{Year: 2023, Agency: "MCMC", Offence: "Palsu", Count: 1},
		{Year: 2023, Agency: "PDRM", Offence: "Judi", Count: 5},
		{Year: 2022, Agency: "KPKT", Offence: "Pinjaman", Count: 1},
	}
	var buf bytes.Buffer
	asOf := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC)
	if err := WriteBlockingRegisterWorkbook(rows, asOf, &buf); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.GetSheetList(); len(got) != 3 || got[0] != "A. Jumlah Sekatan MCMC" || got[2] != "C. Perbandingan" {
		t.Fatalf("unexpected sheets: %q", got)
	}
	a, _ := f.GetRows("A. Jumlah Sekatan MCMC")
	if a[1][1] != "A. JUMLAH SEKATAN LAMAN SESAWANG OLEH MCMC (Setakat 6 Oktober 2026)" {
		t.Fatalf("title: %q", a[1][1])
	}
	if a[3][1] != "Jumlah Keseluruhan Sekatan Laman Sesawang oleh MCMC (2022 - 2023)" {
		t.Fatalf("heading: %q", a[3])
	}
	// Header on row 6, then the five categories in workbook order, then total.
	if a[5][1] != "Elemen/Kesalahan" || a[6][1] != "Lucah" || a[6][2] != "3" || a[7][1] != "Sumbang" || a[7][2] != "0" || a[11][1] != "Jumlah Keseluruhan" || a[11][2] != "4" || a[6][3] != "75.00%" {
		t.Fatalf("overall section: %q", a[5:12])
	}
	// Next section is the newest year.
	if a[14][1] != "Jumlah Sekatan Laman Sesawang oleh MCMC bagi tahun 2023" {
		t.Fatalf("first year section: %q", a[14])
	}
	b, _ := f.GetRows("B. Jumlah Sekatan Agensi")
	// PDRM (5) ranks above KPKT (1).
	if b[6][1] != "PDRM" || b[6][3] != "5" || b[7][1] != "KPKT" || b[8][1] != "Jumlah Keseluruhan" || b[8][3] != "6" {
		t.Fatalf("B overall: %q", b[5:9])
	}
	c, _ := f.GetRows("C. Perbandingan")
	// Two header rows, then MCMC / Agensi Lain / total by year.
	if c[5][2] != "Tahun" || c[6][2] != "2022" || c[6][3] != "2023" || c[7][1] != "MCMC" || c[7][2] != "3" || c[7][3] != "1" || c[7][4] != "4" || c[8][1] != "Agensi Lain" || c[9][4] != "10" {
		t.Fatalf("C section 1: %q", c[5:10])
	}
}
