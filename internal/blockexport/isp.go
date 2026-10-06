package blockexport

import (
	"fmt"
	"io"
	"sort"
	"strings"
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
	st, err := newExportStyles(f)
	if err != nil {
		return err
	}
	if err := writeISPUnblockedSheet(f, "Sheet1", rows, st); err != nil {
		return err
	}
	return f.Write(w)
}

type exportStyles struct{ date, dateTime int }

func newExportStyles(f *excelize.File) (exportStyles, error) {
	date, err := dateCellStyle(f)
	if err != nil {
		return exportStyles{}, err
	}
	dtFmt := "yyyy-mm-dd hh:mm"
	dateTime, err := f.NewStyle(&excelize.Style{CustomNumFmt: &dtFmt})
	return exportStyles{date: date, dateTime: dateTime}, err
}

// writeISPUnblockedSheet fills an existing sheet with the per-(domain, DNS
// server) table — the same layout whether it's the only sheet (per-ISP
// export) or one of several (all-ISP export).
func writeISPUnblockedSheet(f *excelize.File, sheet string, rows []ISPUnblockedRow, st exportStyles) error {
	if err := applyHeaderStyle(f, sheet, len(ispUnblockedHeaders)); err != nil {
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
		for col, style := range map[int]int{10: st.dateTime, 11: st.date, 12: st.date} {
			c, _ := excelize.CoordinatesToCellName(col, i+2)
			if err := f.SetCellStyle(sheet, c, c, style); err != nil {
				return err
			}
		}
	}
	return nil
}

// ISPExport is one ISP's slice of the all-ISP export.
type ISPExport struct {
	ISP           string
	ServerCount   int
	Rows          []ISPUnblockedRow // one per (domain, DNS server)
	PreviousCount int               // domains not blocked in the same-length period before
}

func (e ISPExport) domainCount() int {
	seen := map[uint]bool{}
	for _, r := range e.Rows {
		seen[r.Row.URLID] = true
	}
	return len(seen)
}

// WriteAllISPUnblockedWorkbook writes the all-ISP export: a Summary sheet
// (one row per ISP), a Matrix sheet (one row per domain, one column per
// ISP), then one sheet per ISP in the per-ISP export's exact layout so it
// can be copied out and sent to that ISP as-is.
func WriteAllISPUnblockedWorkbook(isps []ISPExport, since, until time.Time, w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()
	st, err := newExportStyles(f)
	if err != nil {
		return err
	}
	if err := f.SetSheetName("Sheet1", "Summary"); err != nil {
		return err
	}
	if err := writeSummarySheet(f, isps, since, until, st); err != nil {
		return err
	}
	if _, err := f.NewSheet("Matrix"); err != nil {
		return err
	}
	if err := writeMatrixSheet(f, isps, st); err != nil {
		return err
	}
	used := map[string]bool{"summary": true, "matrix": true}
	for _, e := range isps {
		name := sheetName(e.ISP, used)
		if _, err := f.NewSheet(name); err != nil {
			return err
		}
		if err := writeISPUnblockedSheet(f, name, e.Rows, st); err != nil {
			return err
		}
	}
	return f.Write(w)
}

func writeSummarySheet(f *excelize.File, isps []ISPExport, since, until time.Time, st exportStyles) error {
	const sheet = "Summary"
	// Period on its own rows above the table: the table header is row 4.
	meta := [][]any{{"Period from", since.In(myt)}, {"Period to", until.In(myt)}}
	for i, m := range meta {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow(sheet, cell, &m); err != nil {
			return err
		}
		c, _ := excelize.CoordinatesToCellName(2, i+1)
		if err := f.SetCellStyle(sheet, c, c, st.dateTime); err != nil {
			return err
		}
	}
	headers := []any{"ISP", "DNS Servers", "Domains Not Blocked", "Domain × Server Rows", "Previous Period", "Change", "Oldest Notice Date"}
	if err := f.SetSheetRow(sheet, "A4", &headers); err != nil {
		return err
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, "A1", "A2", bold); err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, "A4", "G4", bold); err != nil {
		return err
	}
	for i, e := range isps {
		var oldest *time.Time
		for _, r := range e.Rows {
			if n := r.Row.NoticeDate; n != nil && (oldest == nil || n.Before(*oldest)) {
				oldest = n
			}
		}
		domains := e.domainCount()
		row := []any{e.ISP, e.ServerCount, domains, len(e.Rows), e.PreviousCount, domains - e.PreviousCount, optTime(oldest)}
		cell, _ := excelize.CoordinatesToCellName(1, i+5)
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			return err
		}
		c, _ := excelize.CoordinatesToCellName(7, i+5)
		if err := f.SetCellStyle(sheet, c, c, st.date); err != nil {
			return err
		}
	}
	return nil
}

func writeMatrixSheet(f *excelize.File, isps []ISPExport, st exportStyles) error {
	const sheet = "Matrix"
	type domain struct {
		url    string
		notice *time.Time
		on     map[string]bool
	}
	byURL := map[string]*domain{}
	var order []string
	for _, e := range isps {
		for _, r := range e.Rows {
			d, ok := byURL[r.Row.URL]
			if !ok {
				d = &domain{url: r.Row.URL, notice: r.Row.NoticeDate, on: map[string]bool{}}
				byURL[r.Row.URL] = d
				order = append(order, r.Row.URL)
			}
			d.on[e.ISP] = true
		}
	}
	sort.Strings(order)

	headers := []any{"Domain", "Notice Date", "ISPs Not Blocking"}
	for _, e := range isps {
		headers = append(headers, e.ISP)
	}
	if err := applyHeaderStyle(f, sheet, len(headers)); err != nil {
		return err
	}
	if err := f.SetSheetRow(sheet, "A1", &headers); err != nil {
		return err
	}
	for i, url := range order {
		d := byURL[url]
		row := []any{d.url, optTime(d.notice), len(d.on)}
		for _, e := range isps {
			if d.on[e.ISP] {
				row = append(row, "Not blocked")
			} else {
				row = append(row, "")
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			return err
		}
		c, _ := excelize.CoordinatesToCellName(2, i+2)
		if err := f.SetCellStyle(sheet, c, c, st.date); err != nil {
			return err
		}
	}
	return nil
}

// sheetName makes an ISP name a valid, unique Excel sheet name: no
// []:*?/\ characters, at most 31 characters, case-insensitively unique.
func sheetName(isp string, used map[string]bool) string {
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(isp))
	if name == "" {
		name = "ISP"
	}
	if r := []rune(name); len(r) > 31 {
		name = string(r[:31])
	}
	base := name
	for n := 2; used[strings.ToLower(name)]; n++ {
		suffix := fmt.Sprintf(" (%d)", n)
		r := []rune(base)
		if len(r)+len([]rune(suffix)) > 31 {
			r = r[:31-len([]rune(suffix))]
		}
		name = string(r) + suffix
	}
	used[strings.ToLower(name)] = true
	return name
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
