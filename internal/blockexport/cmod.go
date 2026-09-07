package blockexport

import (
	"time"

	"github.com/afif/dns-tracking/internal/db"
)

const exportDateLayout = "2006-01-02"

// formatDate returns a plain text date string "YYYY-MM-DD" or "" if t is nil.
func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(exportDateLayout)
}

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
