package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/afif/dns-tracking/internal/blockexport"
	"github.com/afif/dns-tracking/internal/db"
)

// fetchAllCaseLetterEntries loops every page of ListCaseLetters/
// ListCaseLettersForDepartment (capped at maxDomainSummaryPageSize per
// page) until every letter in scope has been collected — the JSON list
// handler intentionally returns one page at a time for the Docs page's
// grid (the frontend loops itself, see web/src/api/cases.ts's
// fetchAllCaseLetters), but a server-side export must never silently
// truncate at the page cap.
func fetchAllCaseLetterEntries(ctx context.Context, store db.Store, departmentID *uint) ([]db.CaseLetterEntry, error) {
	var all []db.CaseLetterEntry
	page := 1
	for {
		var (
			batch []db.CaseLetterEntry
			total int
			err   error
		)
		if departmentID != nil {
			batch, total, err = store.ListCaseLettersForDepartment(ctx, page, maxDomainSummaryPageSize, *departmentID)
		} else {
			batch, total, err = store.ListCaseLetters(ctx, page, maxDomainSummaryPageSize)
		}
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) == 0 || len(all) >= total {
			break
		}
		page++
	}
	return all, nil
}

// parseIDSet parses a comma-separated list of uints from a query param
// value. ok is false when raw is empty, meaning "no filter". Non-numeric
// or empty segments are silently skipped.
func parseIDSet(raw string) (map[uint]bool, bool) {
	if raw == "" {
		return nil, false
	}
	ids := make(map[uint]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if v, err := strconv.ParseUint(part, 10, 64); err == nil {
			ids[uint(v)] = true
		}
	}
	return ids, true
}

func (h *Handlers) ExportCaseSummaries(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var cases []db.CaseSummary
	var letters []db.CaseLetterEntry
	var err error
	var deptID *uint
	if !user.IsAdmin {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		deptID = user.DepartmentID
	}
	if deptID != nil {
		cases, err = h.store.ListCasesForDepartment(r.Context(), *deptID)
	} else {
		cases, err = h.store.ListCases(r.Context())
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	letters, err = fetchAllCaseLetterEntries(r.Context(), h.store, deptID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	if ids, filter := parseIDSet(r.URL.Query().Get("case_ids")); filter {
		filtered := make([]db.CaseSummary, 0, len(cases))
		for _, c := range cases {
			if ids[c.ID] {
				filtered = append(filtered, c)
			}
		}
		cases = filtered
	}

	rows := blockexport.FlattenCRDRows(cases, letters)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="blocking-list-export-%s.xlsx"`, time.Now().UTC().Format("2006-01-02")))
	if err := blockexport.WriteCRDWorkbook(rows, w); err != nil {
		writeInternalError(w, err)
		return
	}
}

func (h *Handlers) ExportCaseLetters(w http.ResponseWriter, r *http.Request) {
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

	letters, err := fetchAllCaseLetterEntries(r.Context(), h.store, deptID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var cases []db.CaseSummary
	if deptID != nil {
		cases, err = h.store.ListCasesForDepartment(r.Context(), *deptID)
	} else {
		cases, err = h.store.ListCases(r.Context())
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	if ids, filter := parseIDSet(r.URL.Query().Get("letter_ids")); filter {
		filtered := make([]db.CaseLetterEntry, 0, len(letters))
		for _, l := range letters {
			if ids[l.ID] {
				filtered = append(filtered, l)
			}
		}
		letters = filtered
	}

	oicUsernames := make(map[uint]string, len(users))
	for _, u := range users {
		oicUsernames[u.ID] = u.Username
	}
	agencyNameByCaseID := make(map[uint]string, len(cases))
	offencesByURL := make(map[string][]db.OffenceEntry)
	for _, c := range cases {
		agencyNameByCaseID[c.ID] = c.AgencyName
		for _, d := range c.Domains {
			offencesByURL[d.URL] = d.Offences
		}
	}

	rows := blockexport.FlattenCMODRows(letters, oicUsernames, offencesByURL, agencyNameByCaseID)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cmod-blocking-export-%s.xlsx"`, time.Now().UTC().Format("2006-01-02")))
	if err := blockexport.WriteCMODWorkbook(rows, w); err != nil {
		writeInternalError(w, err)
		return
	}
}
