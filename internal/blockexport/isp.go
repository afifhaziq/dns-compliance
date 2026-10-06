package blockexport

import (
	"io"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/afif/dns-tracking/internal/db"
)

// ISPUnblockedRow is one export line: a domain still resolving on one of
// the ISP's DNS servers as of that server's latest scan in the period.
type ISPUnblockedRow struct {
	ISP      string
	Row      db.ISPUnblockedRow
	DaysOpen *int
}

var ispUnblockedHeaders = []string{
	"ISP", "DNS Server", "DNS Server Address", "Protocol", "Domain", "Original URL",
	"Resolved IP", "Resolved Org", "ASN", "Scan Date", "Notice Date", "Due Date",
	"Reference No.", "Department", "Status", "Days Open", "Screenshot URL",
}

func WriteISPUnblockedWorkbook(rows []ISPUnblockedRow, w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Sheet1"

	if err := applyHeaderStyle(f, sheet, len(ispUnblockedHeaders)); err != nil {
		return err
	}
	dateStyle, err := dateCellStyle(f)
	if err != nil {
		return err
	}
	dtFmt := "yyyy-mm-dd hh:mm"
	dateTimeStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &dtFmt})
	if err != nil {
		return err
	}
	if err := f.SetSheetRow(sheet, "A1", &ispUnblockedHeaders); err != nil {
		return err
	}
	for i, x := range rows {
		r := x.Row
		var days any = ""
		if x.DaysOpen != nil {
			days = *x.DaysOpen
		}
		var asn any = ""
		if r.ResolvedASN != 0 {
			asn = r.ResolvedASN
		}
		vals := []any{
			x.ISP, r.DNSServerName, r.DNSServerAddress, r.DNSServerProtocol, r.URL, r.OriginalURL,
			r.ResolvedIP, r.ResolvedOrg, asn, r.ScannedAt.In(myt), optTime(r.NoticeDate), optTime(r.DueDate),
			r.CurrentReferenceNumber, r.DepartmentName, r.Status, days, r.ScreenshotURL,
		}
		cell, err := excelize.CoordinatesToCellName(1, i+2)
		if err != nil {
			return err
		}
		if err := f.SetSheetRow(sheet, cell, &vals); err != nil {
			return err
		}
		for col, style := range map[int]int{10: dateTimeStyle, 11: dateStyle, 12: dateStyle} {
			c, _ := excelize.CoordinatesToCellName(col, i+2)
			if err := f.SetCellStyle(sheet, c, c, style); err != nil {
				return err
			}
		}
	}
	return f.Write(w)
}

// myt pins exported timestamps to Malaysia time: Excel dates carry no zone,
// and the server container usually runs in UTC.
var myt = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Kuala_Lumpur"); err == nil {
		return loc
	}
	return time.FixedZone("MYT", 8*60*60)
}()

func optTime(t *time.Time) any {
	if t == nil {
		return ""
	}
	return t.In(myt)
}
