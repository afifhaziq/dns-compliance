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
	ISP  string
	Rows []ISPUnblockedRow // one per (domain, DNS server)
}

// WriteAllISPUnblockedWorkbook writes the all-ISP export: a Summary sheet
// (one row per domain × DNS server it's not blocked on), a DNS Servers
// sheet with each server's details, then one sheet per ISP in the per-ISP export's
// exact layout so it can be copied out and sent to that ISP as-is.
func WriteAllISPUnblockedWorkbook(servers []db.DNSServer, isps []ISPExport, w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()
	st, err := newExportStyles(f)
	if err != nil {
		return err
	}
	servers = append([]db.DNSServer(nil), servers...)
	sort.SliceStable(servers, func(i, j int) bool {
		if servers[i].ISP != servers[j].ISP {
			return servers[i].ISP < servers[j].ISP
		}
		return servers[i].Name < servers[j].Name
	})
	if err := f.SetSheetName("Sheet1", "Summary"); err != nil {
		return err
	}
	if err := writeSummarySheet(f, isps, st); err != nil {
		return err
	}
	total := 0
	for _, e := range isps {
		total += len(e.Rows)
	}
	if err := addTable(f, "Summary", 1, 7, total); err != nil {
		return err
	}
	if _, err := f.NewSheet("DNS Servers"); err != nil {
		return err
	}
	if err := writeServersSheet(f, servers); err != nil {
		return err
	}
	if err := addTable(f, "DNS Servers", 2, 5, len(servers)); err != nil {
		return err
	}
	used := map[string]bool{"summary": true, "dns servers": true}
	for i, e := range isps {
		name := sheetName(e.ISP, used)
		if _, err := f.NewSheet(name); err != nil {
			return err
		}
		if err := writeISPUnblockedSheet(f, name, e.Rows, st); err != nil {
			return err
		}
		if err := addTable(f, name, i+3, len(ispUnblockedHeaders), len(e.Rows)); err != nil {
			return err
		}
	}
	return f.Write(w)
}

func writeServersSheet(f *excelize.File, servers []db.DNSServer) error {
	const sheet = "DNS Servers"
	headers := []any{"DNS Server", "ISP", "Address", "Protocol", "Enabled"}
	if err := applyHeaderStyle(f, sheet, len(headers)); err != nil {
		return err
	}
	if err := f.SetSheetRow(sheet, "A1", &headers); err != nil {
		return err
	}
	for i, s := range servers {
		enabled := "No"
		if s.Enabled {
			enabled = "Yes"
		}
		row := []any{s.Name, s.ISP, s.Address, s.Protocol, enabled}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			return err
		}
	}
	return nil
}

// writeSummarySheet is one row per (domain, DNS server) the domain is not
// blocked on, sorted by domain, then ISP, then server name.
func writeSummarySheet(f *excelize.File, isps []ISPExport, st exportStyles) error {
	const sheet = "Summary"
	var rows []ISPUnblockedRow
	for _, e := range isps {
		rows = append(rows, e.Rows...)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Row.URL != b.Row.URL {
			return a.Row.URL < b.Row.URL
		}
		if a.ISP != b.ISP {
			return a.ISP < b.ISP
		}
		return a.Row.DNSServerName < b.Row.DNSServerName
	})

	headers := []any{"Domain", "ISP", "DNS Server", "Scan Date", "Notice Date", "Due Date", "Reference No."}
	if err := applyHeaderStyle(f, sheet, len(headers)); err != nil {
		return err
	}
	if err := f.SetSheetRow(sheet, "A1", &headers); err != nil {
		return err
	}
	for i, x := range rows {
		r := x.Row
		row := []any{r.URL, x.ISP, r.DNSServerName, r.ScannedAt.In(myt), optTime(r.NoticeDate), optTime(r.DueDate), r.CurrentReferenceNumber}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			return err
		}
		for col, style := range map[int]int{4: st.dateTime, 5: st.date, 6: st.date} {
			c, _ := excelize.CoordinatesToCellName(col, i+2)
			if err := f.SetCellStyle(sheet, c, c, style); err != nil {
				return err
			}
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
