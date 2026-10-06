package server

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/afif/dns-tracking/internal/blockexport"
	"github.com/afif/dns-tracking/internal/db"
)

// UnblockedDomain is one domain on the ISP page's Unblocked table: the
// latest-case fields once, plus every server of the ISP still resolving it.
type UnblockedDomain struct {
	URLID                  uint                 `json:"url_id"`
	URL                    string               `json:"url"`
	CaseID                 *uint                `json:"case_id,omitempty"`
	Status                 string               `json:"status,omitempty"`
	NoticeDate             *time.Time           `json:"notice_date,omitempty"`
	DueDate                *time.Time           `json:"due_date,omitempty"`
	DaysOpen               *int                 `json:"days_open,omitempty"` // see daysOpen
	CurrentReferenceNumber string               `json:"current_reference_number,omitempty"`
	LastScannedAt          time.Time            `json:"last_scanned_at"`
	Servers                []db.ISPUnblockedRow `json:"servers"`
}

type unblockedResponse struct {
	Items []UnblockedDomain `json:"items"`
	Total int               `json:"total"`
}

// ispUnblockedRows parses {isp} + since/until (RFC3339, default last 7
// days) and runs the RBAC-scoped store query. ok=false means a response was
// already written.
func (h *Handlers) ispUnblockedRows(w http.ResponseWriter, r *http.Request) (string, []db.ISPUnblockedRow, bool) {
	isp, err := url.PathUnescape(chi.URLParam(r, "isp"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ISP")
		return "", nil, false
	}
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return "", nil, false
	}
	var deptID *uint
	if !user.IsAdmin {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return "", nil, false
		}
		deptID = user.DepartmentID
	}
	until := time.Now()
	since := until.AddDate(0, 0, -7)
	if t, err := time.Parse(time.RFC3339, r.URL.Query().Get("since")); err == nil {
		since = t
	}
	if t, err := time.Parse(time.RFC3339, r.URL.Query().Get("until")); err == nil {
		until = t
	}
	rows, err := h.store.ISPUnblocked(r.Context(), isp, since, until, deptID)
	if err != nil {
		writeInternalError(w, err)
		return "", nil, false
	}
	return isp, rows, true
}

// daysOpen is whole days since the due date, or since the Notice date when
// there's no due date (every imported case: the spreadsheets carry none).
// nil when neither is set.
func daysOpen(due, notice *time.Time, now time.Time) *int {
	from := due
	if from == nil {
		from = notice
	}
	if from == nil {
		return nil
	}
	d := int(now.Sub(*from).Hours() / 24)
	if d < 0 {
		d = 0
	}
	return &d
}

// groupUnblocked folds per-(domain, server) rows (ordered by url) into one
// entry per domain.
func groupUnblocked(rows []db.ISPUnblockedRow, now time.Time) []UnblockedDomain {
	var out []UnblockedDomain
	for _, row := range rows {
		if n := len(out); n == 0 || out[n-1].URLID != row.URLID {
			out = append(out, UnblockedDomain{
				URLID: row.URLID, URL: row.URL, CaseID: row.CaseID, Status: row.Status,
				NoticeDate: row.NoticeDate, DueDate: row.DueDate, DaysOpen: daysOpen(row.DueDate, row.NoticeDate, now),
				CurrentReferenceNumber: row.CurrentReferenceNumber,
			})
		}
		d := &out[len(out)-1]
		d.Servers = append(d.Servers, row)
		if row.ScannedAt.After(d.LastScannedAt) {
			d.LastScannedAt = row.ScannedAt
		}
	}
	return out
}

// ISPUnblocked — GET /api/isps/{isp}/unblocked?since=&until=&q=&sort=&dir=&page=&page_size=
// sort: "days_open" (default, longest open first), "url", "notice_date".
// Grouped, filtered and paged in Go: the row count is bounded by one ISP's
// violating domains in one period (low thousands), not scan history.
func (h *Handlers) ISPUnblocked(w http.ResponseWriter, r *http.Request) {
	_, rows, ok := h.ispUnblockedRows(w, r)
	if !ok {
		return
	}
	qs := r.URL.Query()
	items := groupUnblocked(rows, time.Now())

	if q := strings.ToLower(strings.TrimSpace(qs.Get("q"))); q != "" {
		filtered := items[:0]
		for _, d := range items {
			if strings.Contains(d.URL, q) || strings.Contains(strings.ToLower(d.CurrentReferenceNumber), q) {
				filtered = append(filtered, d)
			}
		}
		items = filtered
	}

	desc := qs.Get("dir") != "asc"
	less := func(i, j int) bool { return items[i].URL < items[j].URL }
	switch qs.Get("sort") {
	case "url":
		desc = qs.Get("dir") == "desc"
	case "notice_date":
		less = func(i, j int) bool { return timeLess(items[i].NoticeDate, items[j].NoticeDate, desc) }
		desc = false // timeLess already applied direction (empties last)
	default:
		less = func(i, j int) bool {
			a, b := items[i].DaysOpen, items[j].DaysOpen
			if (a == nil) != (b == nil) {
				return a != nil // no due date always last
			}
			if a == nil || *a == *b {
				return items[i].URL < items[j].URL
			}
			if desc {
				return *a > *b
			}
			return *a < *b
		}
		desc = false
	}
	sort.SliceStable(items, func(i, j int) bool {
		if desc {
			return less(j, i)
		}
		return less(i, j)
	})

	page, pageSize := 1, 50
	if n, err := strconv.Atoi(qs.Get("page")); err == nil && n > 0 {
		page = n
	}
	if n, err := strconv.Atoi(qs.Get("page_size")); err == nil && n > 0 && n <= maxDomainSummaryPageSize {
		pageSize = n
	}
	total := len(items)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	writeJSON(w, http.StatusOK, unblockedResponse{Items: items[start:end], Total: total})
}

// timeLess orders by t (desc or asc) with nil always last.
func timeLess(a, b *time.Time, desc bool) bool {
	if (a == nil) != (b == nil) {
		return a != nil
	}
	if a == nil || a.Equal(*b) {
		return false
	}
	if desc {
		return a.After(*b)
	}
	return a.Before(*b)
}

// ExportISPUnblocked — GET /api/isps/{isp}/unblocked/export?since=&until=
// — every row in scope, one per (domain, DNS server), unpaged.
func (h *Handlers) ExportISPUnblocked(w http.ResponseWriter, r *http.Request) {
	isp, rows, ok := h.ispUnblockedRows(w, r)
	if !ok {
		return
	}
	now := time.Now()
	out := make([]blockexport.ISPUnblockedRow, len(rows))
	for i, row := range rows {
		out[i] = blockexport.ISPUnblockedRow{ISP: isp, Row: row, DaysOpen: daysOpen(row.DueDate, row.NoticeDate, now)}
	}
	safe := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '/' || r < 0x20 {
			return '_'
		}
		return r
	}, isp)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="unblocked-%s-%s.xlsx"`, safe, now.UTC().Format("2006-01-02")))
	if err := blockexport.WriteISPUnblockedWorkbook(out, w); err != nil {
		writeInternalError(w, err)
	}
}
