package blockexport

import (
	"io"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/xuri/excelize/v2"
)

// formatDate and exportDateLayout are defined in crd.go — this file reuses them.

// CMODRow is one flattened output row, one field per export column, in
// declaration order matching the column table below.
type CMODRow struct {
	No          int    // col 1, sequential across the whole output, 1-indexed
	LetterDate  string // col 2, "YYYY-MM-DD" or ""
	Recipient   string // col 3
	Type        string // col 4
	Subject     string // col 5
	ReferenceNo string // col 6
	OIC         string // col 7
	Requestor   string // col 8
	Offence     string // col 9
	Link        string // col 10
	Remarks     string // col 11
	Agency      string // col 12
	Status      string // col 13
	Received    string // col 14, "YYYY-MM-DD" or ""
	Submission  string // col 15, "YYYY-MM-DD" or ""
	CaseID      uint   // col 16
	InternalRef string // col 17
}

// FlattenCMODRows expands letters into export rows — one row per
// CaseLetterEntry x its URLs (a letter covering 3 domains produces 3 rows,
// Link varies, everything else repeats), further expanded per-domain-
// offence the same way CRD does (so full expansion is letter x domain x
// offence; a domain with zero offences still emits one row for that
// domain, Offence column blank).
//
// oicUsernames maps User.ID -> Username (built by the caller from a full
// user list — see internal/db.Store.ListUsers — not fetched here).
// offencesByURL maps a domain string -> its []db.OffenceEntry (built by the
// caller, keyed by the domains already present on CaseLetterEntry.URLs).
// agencyNameByCaseID maps CaseLetter.CaseID -> the case's Agency.Name
// (built by the caller — CaseLetterEntry does not carry Agency directly;
// see Plan 05 for exactly how the caller builds this map by reusing
// db.CaseSummary.AgencyName, since internal/db.cases.go's case_letters
// query does not join agencies at all).
// Pure function, no DB/HTTP.
func FlattenCMODRows(
	letters []db.CaseLetterEntry,
	oicUsernames map[uint]string,
	offencesByURL map[string][]db.OffenceEntry,
	agencyNameByCaseID map[uint]string,
) []CMODRow {
	var out []CMODRow
	no := 0
	for _, l := range letters {
		oic := ""
		if l.OICUserID != nil {
			oic = oicUsernames[*l.OICUserID]
		}
		agency := agencyNameByCaseID[l.CaseID]

		for _, url := range l.URLs {
			offences := offencesByURL[url]
			base := CMODRow{
				LetterDate: formatDate(l.LetterDate), Recipient: l.Recipient, Type: l.Type,
				Subject: l.Subject, ReferenceNo: l.ReferenceNumberExternal, OIC: oic,
				Requestor: l.Requestor, Link: url, Remarks: l.Remarks, Agency: agency,
				Status: l.WorkflowStatus, Received: formatDate(l.ReceivedAt),
				Submission: formatDate(l.SubmittedAt), CaseID: l.CaseID,
				InternalRef: l.ReferenceNumberInternal,
			}
			if len(offences) == 0 {
				no++
				row := base
				row.No = no
				out = append(out, row)
				continue
			}
			for _, off := range offences {
				no++
				row := base
				row.No = no
				row.Offence = off.Category
				out = append(out, row)
			}
		}
	}
	return out
}

var cmodHeaders = []string{
	"No", "Letter Date", "Recipient", "Type", "Subject", "Reference No", "OIC",
	"Requestor", "Offence", "Link", "Remarks", "Agency", "Status", "Received",
	"Submission", "Case ID", "Internal Ref (No. Rujukan NMSMD)",
}

// WriteCMODWorkbook writes rows as a single-sheet .xlsx to w — a header row
// (the 17 column titles, in FlattenCMODRows' declared order) followed by
// one data row per CMODRow.
// cmodDateCols holds the 0-indexed column positions (matching cmodHeaders)
// that hold dates and must be written as real Excel date values.
var cmodDateCols = map[int]bool{1: true, 13: true, 14: true} // LetterDate, Received, Submission

func WriteCMODWorkbook(rows []CMODRow, w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Sheet1"

	if err := applyHeaderStyle(f, sheet, len(cmodHeaders)); err != nil {
		return err
	}
	dateStyle, err := dateCellStyle(f)
	if err != nil {
		return err
	}

	for i, h := range cmodHeaders {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return err
		}
	}
	for r, row := range rows {
		vals := []interface{}{
			row.No, row.LetterDate, row.Recipient, row.Type, row.Subject, row.ReferenceNo,
			row.OIC, row.Requestor, row.Offence, row.Link, row.Remarks, row.Agency,
			row.Status, row.Received, row.Submission, row.CaseID, row.InternalRef,
		}
		for c, v := range vals {
			cell, err := excelize.CoordinatesToCellName(c+1, r+2)
			if err != nil {
				return err
			}
			if cmodDateCols[c] {
				if err := setDateCell(f, sheet, cell, v.(string), dateStyle); err != nil {
					return err
				}
				continue
			}
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				return err
			}
		}
	}
	return f.Write(w)
}
