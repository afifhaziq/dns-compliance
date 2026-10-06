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
	Resurfaced             bool                 `json:"resurfaced"` // flipped blocked→resolving in its latest scan on one of this ISP's servers
	Servers                []db.ISPUnblockedRow `json:"servers"`
}

type unblockedResponse struct {
	Items []UnblockedDomain `json:"items"`
	Total int               `json:"total"`
}

// ispScope parses {isp} and the caller's RBAC scope (deptID nil = admin).
// ok=false means a response was already written.
func ispScope(w http.ResponseWriter, r *http.Request) (isp string, deptID *uint, ok bool) {
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
	if !user.IsAdmin {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return "", nil, false
		}
		deptID = user.DepartmentID
	}
	return isp, deptID, true
}

// parsePeriod reads since/until (RFC3339), defaulting to the last 7 days.
func parsePeriod(r *http.Request) (since, until time.Time) {
	until = time.Now()
	since = until.AddDate(0, 0, -7)
	if t, err := time.Parse(time.RFC3339, r.URL.Query().Get("since")); err == nil {
		since = t
	}
	if t, err := time.Parse(time.RFC3339, r.URL.Query().Get("until")); err == nil {
		until = t
	}
	return since, until
}

// pageParams reads page/page_size (default 1/50, capped like the other
// paged endpoints) and returns the [start, end) slice bounds for total rows.
func pageParams(r *http.Request, total int) (start, end int) {
	page, pageSize := 1, 50
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n > 0 {
		page = n
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && n > 0 && n <= maxDomainSummaryPageSize {
		pageSize = n
	}
	start = min((page-1)*pageSize, total)
	return start, min(start+pageSize, total)
}

// ispUnblockedRows parses {isp} + since/until (RFC3339, default last 7
// days) and runs the RBAC-scoped store query. ok=false means a response was
// already written.
func (h *Handlers) ispUnblockedRows(w http.ResponseWriter, r *http.Request) (string, *uint, []db.ISPUnblockedRow, bool) {
	isp, deptID, ok := ispScope(w, r)
	if !ok {
		return "", nil, nil, false
	}
	since, until := parsePeriod(r)
	rows, err := h.store.ISPUnblocked(r.Context(), isp, since, until, deptID)
	if err != nil {
		writeInternalError(w, err)
		return "", nil, nil, false
	}
	return isp, deptID, rows, true
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
	isp, deptID, rows, ok := h.ispUnblockedRows(w, r)
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

	start, end := pageParams(r, len(items))
	page := items[start:end]

	// Resurfaced flag for this page only: the unfiltered resurfaced query
	// scans all of scan_results (~2s on CRD-sized data).
	urls := make([]string, len(page))
	for i, d := range page {
		urls[i] = d.URL
	}
	resurfaced, err := h.store.ISPResurfaced(r.Context(), isp, deptID, urls)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	isResurfaced := make(map[string]bool, len(resurfaced))
	for _, d := range resurfaced {
		isResurfaced[d.URLValue] = true
	}
	for i := range page {
		page[i].Resurfaced = isResurfaced[page[i].URL]
	}
	writeJSON(w, http.StatusOK, unblockedResponse{Items: page, Total: len(items)})
}

// ISPResurfaced — GET /api/isps/{isp}/resurfaced?page=&page_size= — the
// ISP page's Resurfaced table: domains whose latest scan on one of this
// ISP's servers flipped blocked→resolving, newest flip first, paged.
// /api/resurfaced stays unpaged for the Overview count and scan-results.
func (h *Handlers) ISPResurfaced(w http.ResponseWriter, r *http.Request) {
	isp, deptID, ok := ispScope(w, r)
	if !ok {
		return
	}
	domains, err := h.store.ISPResurfaced(r.Context(), isp, deptID, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	sort.Slice(domains, func(i, j int) bool {
		if !domains[i].ResurfacedAt.Equal(domains[j].ResurfacedAt) {
			return domains[i].ResurfacedAt.After(domains[j].ResurfacedAt)
		}
		return domains[i].URLValue < domains[j].URLValue
	})
	start, end := pageParams(r, len(domains))
	writeJSON(w, http.StatusOK, struct {
		Items []db.ResurfacedDomain `json:"items"`
		Total int                   `json:"total"`
	}{domains[start:end], len(domains)})
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
	isp, _, rows, ok := h.ispUnblockedRows(w, r)
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
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="unblocked-%s-%s.xlsx"`, safe, now.UTC().Format("2006-01-02_1504")))
	if err := blockexport.WriteISPUnblockedWorkbook(out, w); err != nil {
		writeInternalError(w, err)
	}
}

// ExportAllISPUnblocked — GET /api/unblocked/export?since=&until= — every
// ISP's unblocked domains in one workbook: Summary (domain × DNS server),
// DNS Servers, then one sheet per ISP in the per-ISP export's layout. Same
// scoping as the per-ISP routes.
func (h *Handlers) ExportAllISPUnblocked(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var deptID *uint
	if !user.IsAdmin {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		deptID = user.DepartmentID
	}
	since, until := parsePeriod(r)

	servers, err := h.store.ListDNSServers(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	seen := map[string]bool{}
	var isps []string
	for _, s := range servers {
		if !seen[s.ISP] {
			seen[s.ISP] = true
			isps = append(isps, s.ISP)
		}
	}
	sort.Strings(isps)

	now := time.Now()
	out := make([]blockexport.ISPExport, 0, len(isps))
	for _, isp := range isps {
		rows, err := h.store.ISPUnblocked(r.Context(), isp, since, until, deptID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		e := blockexport.ISPExport{ISP: isp}
		for _, row := range rows {
			e.Rows = append(e.Rows, blockexport.ISPUnblockedRow{ISP: isp, Row: row, DaysOpen: daysOpen(row.DueDate, row.NoticeDate, now)})
		}
		out = append(out, e)
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="isp_weekly_report-%s.xlsx"`, now.In(time.FixedZone("MYT", 8*60*60)).Format("2006-01-02_1504")))
	if err := blockexport.WriteAllISPUnblockedWorkbook(servers, out, w); err != nil {
		writeInternalError(w, err)
	}
}
