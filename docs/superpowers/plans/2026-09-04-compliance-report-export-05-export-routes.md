# Compliance Export Routes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two new authenticated HTTP endpoints — `GET /api/case-summaries/export` (CRD) and `GET /api/case-letters/export` (CMOD) — that stream a `.xlsx` workbook built from `internal/blockexport`'s flatten/write functions, RBAC-scoped exactly like their JSON-list counterparts, with an optional `case_ids`/`letter_ids` query filter.

**Prerequisite:** `2026-09-04-compliance-report-export-03-export-crd.md` AND `2026-09-04-compliance-report-export-04-export-cmod.md` must both be merged to `main` first — this plan calls `blockexport.FlattenCRDRows`/`WriteCRDWorkbook`/`FlattenCMODRows`/`WriteCMODWorkbook`, all defined there.

**Architecture:** New file `internal/server/export_handlers.go` (kept separate from `case_handlers.go` rather than appended to it, since it's a distinct concern — HTTP streaming of a generated file, not case CRUD — and `case_handlers.go` is already 600+ lines). Two handlers plus a handful of small package-private helpers (ID-list parsing, server-side "fetch every page" pagination loop, RBAC-scope filtering). Two new routes registered in `internal/server/router.go`.

**Tech Stack:** Go, `github.com/afif/dns-tracking/internal/blockexport` (this repo, Plans 03/04), `github.com/go-chi/chi/v5`.

**Spec:** `docs/superpowers/specs/2026-09-04-compliance-report-export-design.md`, "Routes" and "Response" subsections of Part 2.

## Global Constraints

- Every step must contain real code — no "add appropriate error handling"/"add tests for the above" placeholders.
- `go build ./... && go test ./internal/server/... -v` must pass at the end of this plan.
- **Non-admin scope-widening via a foreign ID must be structurally impossible, not merely untested.** Both handlers always call the same admin/non-admin store method their JSON-list counterparts use first (fetching every case/letter the caller's RBAC scope allows), and only *then* filter that already-scoped Go slice down to the requested `case_ids`/`letter_ids` — the client-supplied ID list is never passed into a SQL `WHERE` clause or trusted as a substitute for the authoritative fetch.
- **Resolved design decision — Agency name + per-domain offences for the CMOD export**: `internal/db/cases.go`'s `case_letters` query (`caseLetterQuery`/`listCaseLetters`) does **not** join `agencies` at all, and there is no existing store method for "offences by domain, batched." Rather than adding a new store method, this plan's CMOD handler reuses the **same** `ListCases`/`ListCasesForDepartment` call the CRD handler already makes (`db.CaseSummary` already carries `AgencyName` per case, and `CaseSummaryDomain.Offences` per domain) to build both lookup maps `blockexport.FlattenCMODRows` needs. This avoids a new DB method entirely — see Task 2 below.
- **Resolved design decision — pagination.** `ListCaseLetters`/`ListCaseLettersForDepartment` (`internal/db/cases.go`) are paginated, capped at `maxDomainSummaryPageSize = 100` per page (`internal/server/handlers.go:1403-1404`). The existing JSON handler (`ListCaseLetters`, `internal/server/case_handlers.go:399-431`) only ever returns one page — the frontend loops client-side (`web/src/api/cases.ts`'s `fetchAllCaseLetters`). An export handler runs entirely server-side and must not silently truncate at 100 letters (`ListCases`/`ListCasesForDepartment`, used for CRD, are **not** paginated — no analogous loop needed there). Task 1 below adds a server-side page-looping helper mirroring the frontend's existing pattern.

---

### Task 1: `fetchAllCaseLetterEntries` + `parseIDSet` helpers

**Files:**
- Create: `internal/server/export_handlers.go`
- Test: `internal/server/export_handlers_test.go` — **`package server`** (the internal test package, NOT `server_test`), because it tests two unexported helpers directly. This is a deliberate, one-off departure from this codebase's usual `package server_test`-only convention (see `internal/server/handlers_test.go`/`case_handlers_test.go`) — those two files only ever exercise exported handlers over HTTP, so they never needed internal-package access before. Go allows both an internal (`server`) and external (`server_test`) test package to coexist in the same directory as separate files in the same test binary, but symbols declared in one are **not** visible to the other. Task 2/3's tests need `server_test`'s existing `setupRouter`/`fullMockStore`/`adminCookie`/`deptCookie` helpers (defined in `handlers_test.go`), so they go in a **different, new file** — see Task 2.

**Interfaces (produced, relied on by Task 2):**
```go
package server

// fetchAllCaseLetterEntries loops every page of ListCaseLetters/
// ListCaseLettersForDepartment (pageSize capped at
// maxDomainSummaryPageSize) until every letter in scope has been
// collected. departmentID nil means admin/unscoped (ListCaseLetters);
// non-nil means department-scoped (ListCaseLettersForDepartment).
func fetchAllCaseLetterEntries(ctx context.Context, store db.Store, departmentID *uint) ([]db.CaseLetterEntry, error)

// parseIDSet parses a comma-separated list of uints from a query param
// value. ok is false when raw is empty, meaning "no filter — export
// everything in scope" (the caller must not filter in that case).
// Non-numeric or empty segments are silently skipped rather than
// rejecting the whole request — a malformed id in an otherwise-valid list
// shouldn't 400 the export, it just won't match anything.
func parseIDSet(raw string) (ids map[uint]bool, ok bool)
```

- [ ] **Step 1: Write the failing tests** in `internal/server/export_handlers_test.go`:
```go
package server

import (
	"context"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
)

// fakeCaseLetterStore is a minimal db.Store double for testing the
// pagination loop in isolation — only the two methods it calls are wired,
// everything else panics if called (proving the test doesn't accidentally
// depend on unrelated store behavior). It embeds db.Store so it satisfies
// the interface without implementing every method.
type fakeCaseLetterStore struct {
	db.Store
	all []db.CaseLetterEntry
}

func (f *fakeCaseLetterStore) paginate(page, pageSize int) ([]db.CaseLetterEntry, int, error) {
	total := len(f.all)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return f.all[start:end], total, nil
}

func (f *fakeCaseLetterStore) ListCaseLetters(_ context.Context, page, pageSize int) ([]db.CaseLetterEntry, int, error) {
	return f.paginate(page, pageSize)
}

func (f *fakeCaseLetterStore) ListCaseLettersForDepartment(_ context.Context, page, pageSize int, _ uint) ([]db.CaseLetterEntry, int, error) {
	return f.paginate(page, pageSize)
}

func TestFetchAllCaseLetterEntries_LoopsBeyondOnePage(t *testing.T) {
	var all []db.CaseLetterEntry
	for i := uint(1); i <= 250; i++ { // > 2 full pages at pageSize 100
		all = append(all, db.CaseLetterEntry{CaseLetter: db.CaseLetter{ID: i}})
	}
	store := &fakeCaseLetterStore{all: all}

	got, err := fetchAllCaseLetterEntries(context.Background(), store, nil)
	if err != nil {
		t.Fatalf("fetchAllCaseLetterEntries: %v", err)
	}
	if len(got) != 250 {
		t.Fatalf("got %d letters, want 250 (must not truncate at the 100-per-page cap)", len(got))
	}
}

func TestFetchAllCaseLetterEntries_DepartmentScoped(t *testing.T) {
	store := &fakeCaseLetterStore{all: []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1}}, {CaseLetter: db.CaseLetter{ID: 2}},
	}}
	deptID := uint(5)
	got, err := fetchAllCaseLetterEntries(context.Background(), store, &deptID)
	if err != nil {
		t.Fatalf("fetchAllCaseLetterEntries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d letters, want 2", len(got))
	}
}

func TestParseIDSet_EmptyMeansNoFilter(t *testing.T) {
	ids, ok := parseIDSet("")
	if ok {
		t.Fatalf("ok = true for empty input, want false (no filter)")
	}
	if ids != nil {
		t.Fatalf("ids = %v, want nil", ids)
	}
}

func TestParseIDSet_ParsesAndSkipsInvalid(t *testing.T) {
	ids, ok := parseIDSet("1,2,notanumber,3")
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	want := map[uint]bool{1: true, 2: true, 3: true}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for id := range want {
		if !ids[id] {
			t.Fatalf("missing id %d in %v", id, ids)
		}
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/server/... -run 'TestFetchAllCaseLetterEntries|TestParseIDSet' -v`
  Expected: FAIL (functions don't exist)
- [ ] **Step 3: Implement in `internal/server/export_handlers.go`:**
```go
package server

import (
	"context"
	"strconv"
	"strings"

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
```
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/server/... -run 'TestFetchAllCaseLetterEntries|TestParseIDSet' -v`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/server/export_handlers.go internal/server/export_handlers_test.go
git commit -m "server: add pagination-looping and id-filter helpers for compliance exports"
```

---

### Task 2: `ExportCaseSummaries` (CRD) handler + route

**Files:**
- Modify: `internal/server/export_handlers.go`
- Modify: `internal/server/router.go`
- Create: `internal/server/export_routes_test.go` — **`package server_test`** (the external test package `handlers_test.go`/`case_handlers_test.go` already use), a deliberately separate file from Task 1's `export_handlers_test.go` (see Task 1's note on why the two can't share a file). Being in the same `server_test` package as `handlers_test.go` means `setupRouter`/`fullMockStore`/`adminCookie`/`deptCookie` are automatically in scope here with no import needed, exactly as `case_handlers_test.go` already relies on today.

**Context — the existing `ListCaseSummaries` handler this mirrors** (`internal/server/case_handlers.go:437-459`):
```go
func (h *Handlers) ListCaseSummaries(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var summaries []db.CaseSummary
	var err error
	if user.IsAdmin {
		summaries, err = h.store.ListCases(r.Context())
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		summaries, err = h.store.ListCasesForDepartment(r.Context(), *user.DepartmentID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}
```

**Router context** (`internal/server/router.go`) — read the file yourself to confirm exact current line numbers before editing, but as of this plan's writing the relevant lines inside the `requireAuth`-only group are:
```go
			// Docs page data source — every CaseLetter, admin: global,
			// non-admin: own department's cases only. See ListCaseLetters.
			r.Get("/case-letters", h.ListCaseLetters)

			// Cases view's data source — see ListCaseSummaries.
			r.Get("/case-summaries", h.ListCaseSummaries)
```

**Interfaces (produced, relied on by Plan 07/Plan 06's frontend consumer):**
```go
// GET /api/case-summaries/export?case_ids=1,2,3 (case_ids optional)
func (h *Handlers) ExportCaseSummaries(w http.ResponseWriter, r *http.Request)
```

- [ ] **Step 1: Write the failing tests** in a new file `internal/server/export_routes_test.go`:
```go
package server_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/xuri/excelize/v2"
)

func caseIDColumn(t *testing.T, body []byte) []string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	var caseIDs []string
	for _, row := range rows[1:] { // skip header
		if len(row) > 14 {
			caseIDs = append(caseIDs, row[14]) // "Case ID" is column 15 (0-indexed 14)
		}
	}
	return caseIDs
}

func TestExportCaseSummaries_AdminGetsEverythingInScope(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 2})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("Content-Type = %q", ct)
	}
	ids := caseIDColumn(t, w.Body.Bytes())
	if len(ids) != 2 {
		t.Fatalf("got %d rows, want 2 (both departments' cases): %v", len(ids), ids)
	}
}

func TestExportCaseSummaries_NonAdminScopedToOwnDepartment(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 2})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ids := caseIDColumn(t, w.Body.Bytes())
	if len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("expected only case 1 (own department), got %v", ids)
	}
}

// A non-admin passing another department's case_id must not see it — the
// server always filters its own already-scoped fetch, never the client's
// list.
func TestExportCaseSummaries_NonAdminCannotWidenScopeViaCaseIDs(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 2})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries/export?case_ids=1,2", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ids := caseIDColumn(t, w.Body.Bytes())
	if len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("expected only case 1 even though case_ids=1,2 was requested, got %v", ids)
	}
}

func TestExportCaseSummaries_CaseIDsFilterNarrowsWithinScope(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries/export?case_ids=2", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ids := caseIDColumn(t, w.Body.Bytes())
	if len(ids) != 1 || ids[0] != "2" {
		t.Fatalf("expected only case 2, got %v", ids)
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/server/... -run TestExportCaseSummaries -v`
  Expected: FAIL (`ExportCaseSummaries` / route undefined)
- [ ] **Step 3: Implement `ExportCaseSummaries`** — append to `internal/server/export_handlers.go` (add imports `fmt`, `net/http`, `time`, `github.com/afif/dns-tracking/internal/blockexport`):
```go
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
```
- [ ] **Step 4: Register the route** in `internal/server/router.go` — add immediately after the existing `r.Get("/case-summaries", h.ListCaseSummaries)` line (same `requireAuth`-only group):
```go
			r.Get("/case-summaries", h.ListCaseSummaries)
			r.Get("/case-summaries/export", h.ExportCaseSummaries)
```
- [ ] **Step 5: Run the tests to verify they pass**
  Run: `go test ./internal/server/... -run TestExportCaseSummaries -v`
  Expected: PASS
- [ ] **Step 6: Commit**
```bash
git add internal/server/export_handlers.go internal/server/export_routes_test.go internal/server/router.go
git commit -m "server: add GET /api/case-summaries/export (CRD compliance export)"
```

---

### Task 3: `ExportCaseLetters` (CMOD) handler + route

**Files:**
- Modify: `internal/server/export_handlers.go`
- Modify: `internal/server/router.go`
- Modify: `internal/server/export_routes_test.go` (created in Task 2, `package server_test`)

**Interfaces (produced, relied on by Plan 06/07's frontend consumer):**
```go
// GET /api/case-letters/export?letter_ids=1,2,3 (letter_ids optional)
func (h *Handlers) ExportCaseLetters(w http.ResponseWriter, r *http.Request)
```

- [ ] **Step 1: Write the failing tests** — append to `internal/server/export_routes_test.go` (Task 2's file, `package server_test` — no new imports needed):
```go
func letterIDColumn(t *testing.T, body []byte) []string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	var caseIDs []string
	for _, row := range rows[1:] {
		if len(row) > 15 {
			caseIDs = append(caseIDs, row[15]) // "Case ID" is column 16 (0-indexed 15)
		}
	}
	return caseIDs
}

func TestExportCaseLetters_NonAdminCannotWidenScopeViaLetterIDs(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 2})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	store.caseLetters = append(store.caseLetters,
		db.CaseLetter{ID: 10, CaseID: 1, Type: "Notice"}, db.CaseLetter{ID: 20, CaseID: 2, Type: "Notice"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-letters/export?letter_ids=10,20", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ids := letterIDColumn(t, w.Body.Bytes())
	if len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("expected only case 1's letter even though letter_ids=10,20 was requested, got %v", ids)
	}
}

func TestExportCaseLetters_LoopsBeyondOneHundredLetters(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"})
	for i := uint(1); i <= 150; i++ { // > the 100-per-page cap
		store.caseLetters = append(store.caseLetters, db.CaseLetter{ID: i, CaseID: 1, Type: "Notice"})
	}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-letters/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows)-1 != 150 { // minus header
		t.Fatalf("got %d data rows, want 150 (must not truncate at the pagination cap)", len(rows)-1)
	}
}

func TestExportCaseLetters_OmittedIDsExportsEverythingInScope(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 1, URLID: 2, Status: "requested"})
	store.caseLetters = append(store.caseLetters, db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice"})
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-letters/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows)-1 != 2 { // one letter x 2 urls
		t.Fatalf("got %d data rows, want 2", len(rows)-1)
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/server/... -run TestExportCaseLetters -v`
  Expected: FAIL (`ExportCaseLetters` / route undefined)
- [ ] **Step 3: Implement `ExportCaseLetters`** — append to `internal/server/export_handlers.go`:
```go
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
```
- [ ] **Step 4: Register the route** in `internal/server/router.go` — add immediately after the existing `r.Get("/case-letters", h.ListCaseLetters)` line:
```go
			r.Get("/case-letters", h.ListCaseLetters)
			r.Get("/case-letters/export", h.ExportCaseLetters)
```
- [ ] **Step 5: Run the tests to verify they pass**
  Run: `go test ./internal/server/... -run TestExportCaseLetters -v`
  Expected: PASS
- [ ] **Step 6: Commit**
```bash
git add internal/server/export_handlers.go internal/server/export_routes_test.go internal/server/router.go
git commit -m "server: add GET /api/case-letters/export (CMOD compliance export)"
```

## Verification for this plan as a whole

```bash
go build ./...
go test ./internal/server/... -v
```
Both must pass. Also run `go vet ./...` once, since this plan adds a new file with several new imports.
