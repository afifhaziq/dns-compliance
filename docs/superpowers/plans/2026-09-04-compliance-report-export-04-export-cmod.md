# CMOD Compliance Export — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pure, unit-testable functions that flatten `db.CaseLetterEntry` rows (the Docs page's data shape) into the CMOD department's legacy Excel column layout, and write them to a real `.xlsx` workbook — the data-transform half of the CMOD export. No DB, no HTTP; this plan produces the `internal/blockexport/cmod.go` half of the package Plan 03 starts, wired up to an HTTP endpoint by `2026-09-04-compliance-report-export-05-export-routes.md`.

**Prerequisite:** None — independent of `2026-09-04-compliance-report-export-03-export-crd.md` (disjoint new files, `cmod.go`/`cmod_test.go` vs `crd.go`/`crd_test.go`, same package). Both plans will independently run `go mod tidy` to promote `github.com/xuri/excelize/v2` from `// indirect` to direct — if merging both produces a trivial `go.mod` conflict, keep either side, it's the same promotion.

**Architecture:** New file in the `internal/blockexport` package (created by Plan 03, or create it yourself if Plan 03 hasn't merged yet in your worktree — `package blockexport` at the top of `cmod.go` is all that's structurally required; this plan does not call anything Plan 03 defines).

**Tech Stack:** Go, `github.com/xuri/excelize/v2`.

**Spec:** `docs/superpowers/specs/2026-09-04-compliance-report-export-design.md`, "CMOD export — `FlattenCMODRows`" section.

## Global Constraints

- Every step must contain real code — no "add appropriate error handling" placeholders.
- `go test ./internal/blockexport/...` must pass at the end of this plan. Zero DB/HTTP dependency in the tests.
- Dates render as plain `"YYYY-MM-DD"` text cells, not native Excel date cells — same deliberate simplification as Plan 03.
- `FlattenCMODRows` does **not** call the database or any `db.Store` method itself — every lookup it needs (OIC username, offences-by-domain, agency name) is passed in as a plain Go map, built by its caller (the HTTP handler in Plan 05). This keeps the function pure and independently testable without a database.
- Column order below is taken verbatim from the design spec — do not reorder, rename, or drop a column.

---

### Task 1: `CMODRow` type + `FlattenCMODRows`

**Files:**
- Create: `internal/blockexport/cmod.go`
- Test: `internal/blockexport/cmod_test.go`

**Context — `db.CaseLetterEntry`/`db.CaseLetter` (internal/db/models.go), the primary input type:**
```go
type CaseLetter struct {
	ID                      uint       `json:"id"`
	CaseID                  uint       `json:"case_id"`
	Type                    string     `json:"type"` // Memo | Notice | Memo (Uplift) | Notice (Uplift)
	ReferenceNumberExternal string     `json:"reference_number_external,omitempty"`
	ReferenceNumberInternal string     `json:"reference_number_internal,omitempty"`
	WorkflowStatus          string     `json:"workflow_status,omitempty"` // Draft | Pending Legal | Pending TSC | Submitted
	Recipient               string     `json:"recipient,omitempty"`
	LetterDate              *time.Time `json:"letter_date,omitempty"`
	ReceivedAt              *time.Time `json:"received_at,omitempty"`
	SubmittedAt             *time.Time `json:"submitted_at,omitempty"`
	Subject                 string     `json:"subject,omitempty"`
	OICUserID               *uint      `json:"oic_user_id,omitempty"`
	OICUser                 *User      `json:"oic_user,omitempty"` // may be nil even when OICUserID is set — don't rely on preload
	Requestor               string     `json:"requestor,omitempty"`
	Remarks                 string     `json:"remarks,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
}

// CaseLetterEntry is one row for the Docs page: a CaseLetter plus its
// case's department and every URL the case covers.
type CaseLetterEntry struct {
	CaseLetter
	DepartmentID   uint     `json:"department_id"`
	DepartmentName string   `json:"department_name"`
	URLs           []string `json:"urls"`
}
```
`OICUserID` may be nil for historical rows imported before the separate "OIC field" work (a different, independent plan pair — `01-oic-backend.md`/`02-oic-frontend.md`) lands — `FlattenCMODRows` must render a blank OIC column for those, never panic on a nil pointer or a missing map entry.

**Interfaces (produced, relied on by Task 2 and by Plan 05):**
```go
package blockexport

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
) []CMODRow
```

**Column table (from the design spec, copy exactly):**

| # | Column | Source |
|---|---|---|
| 1 | `No` | Sequential row number in the output (1-indexed) |
| 2 | `Letter Date` | `CaseLetter.LetterDate` |
| 3 | `Recipient` | `CaseLetter.Recipient` |
| 4 | `Type` | `CaseLetter.Type` |
| 5 | `Subject` | `CaseLetter.Subject` |
| 6 | `Reference No` | `CaseLetter.ReferenceNumberExternal` |
| 7 | `OIC` | `oicUsernames[*letter.OICUserID]` if `OICUserID` non-nil, else blank |
| 8 | `Requestor` | `CaseLetter.Requestor` |
| 9 | `Offence` | This row's domain's `OffenceEntry.Category`, from `offencesByURL` |
| 10 | `Link` | The one domain for this row |
| 11 | `Remarks` | `CaseLetter.Remarks` |
| 12 | `Agency` | `agencyNameByCaseID[letter.CaseID]` |
| 13 | `Status` | `CaseLetter.WorkflowStatus` (letter-approval workflow, not the domain's block/uplift/suspended status) |
| 14 | `Received` | `CaseLetter.ReceivedAt` |
| 15 | `Submission` | `CaseLetter.SubmittedAt` |
| 16 | `Case ID` | `CaseLetter.CaseID` |
| 17 | `Internal Ref (No. Rujukan NMSMD)` | `CaseLetter.ReferenceNumberInternal` |

**Grain**: one row per `CaseLetterEntry` x its `URLs` x its per-domain `Offences` (zero offences on a domain still yields one row for that domain). A letter with zero `URLs` contributes zero rows — structurally shouldn't happen (every case is created with at least one linked URL, see `CreateCase` in `internal/db/cases.go`) but is not treated as an error if it does; it's simply skipped.

- [ ] **Step 1: Write the failing tests** in `internal/blockexport/cmod_test.go`:
```go
package blockexport

import (
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
)

func cmodPtrTime(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestFlattenCMODRows_ExpandsPerURL(t *testing.T) {
	letters := []db.CaseLetterEntry{
		{
			CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice", Subject: "Blocking notice"},
			URLs:       []string{"a.com", "b.com", "c.com"},
		},
	}
	rows := FlattenCMODRows(letters, nil, nil, nil)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %+v", len(rows), rows)
	}
	links := map[string]bool{rows[0].Link: true, rows[1].Link: true, rows[2].Link: true}
	for _, want := range []string{"a.com", "b.com", "c.com"} {
		if !links[want] {
			t.Fatalf("missing link %q in rows %+v", want, rows)
		}
	}
	for _, r := range rows {
		if r.Subject != "Blocking notice" {
			t.Fatalf("Subject should repeat across expanded rows, got %+v", r)
		}
	}
}

func TestFlattenCMODRows_ExpandsPerOffenceWithinDomain(t *testing.T) {
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice"}, URLs: []string{"a.com"}},
	}
	offences := map[string][]db.OffenceEntry{
		"a.com": {{Category: "Judi"}, {Category: "Lucah"}},
	}
	rows := FlattenCMODRows(letters, nil, offences, nil)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].Offence != "Judi" || rows[1].Offence != "Lucah" {
		t.Fatalf("expected distinct offence categories per row: %+v", rows)
	}
}

func TestFlattenCMODRows_MissingOICUserRendersBlankNotPanic(t *testing.T) {
	oicID := uint(99) // not present in oicUsernames
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice", OICUserID: &oicID}, URLs: []string{"a.com"}},
	}
	rows := FlattenCMODRows(letters, map[uint]string{}, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].OIC != "" {
		t.Fatalf("OIC = %q, want blank for an unresolvable user id", rows[0].OIC)
	}

	// nil OICUserID must also render blank, not panic.
	letters2 := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 2, CaseID: 1, Type: "Notice"}, URLs: []string{"a.com"}},
	}
	rows2 := FlattenCMODRows(letters2, map[uint]string{1: "alice"}, nil, nil)
	if rows2[0].OIC != "" {
		t.Fatalf("OIC = %q, want blank for nil OICUserID", rows2[0].OIC)
	}
}

func TestFlattenCMODRows_ResolvesOICUsername(t *testing.T) {
	oicID := uint(7)
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice", OICUserID: &oicID}, URLs: []string{"a.com"}},
	}
	rows := FlattenCMODRows(letters, map[uint]string{7: "alice"}, nil, nil)
	if rows[0].OIC != "alice" {
		t.Fatalf("OIC = %q, want alice", rows[0].OIC)
	}
}

func TestFlattenCMODRows_SequentialNumberingAcrossLetters(t *testing.T) {
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice"}, URLs: []string{"a.com", "b.com"}},
		{CaseLetter: db.CaseLetter{ID: 2, CaseID: 2, Type: "Memo"}, URLs: []string{"c.com"}},
	}
	rows := FlattenCMODRows(letters, nil, nil, nil)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for i, r := range rows {
		if r.No != i+1 {
			t.Fatalf("row %d: No = %d, want %d", i, r.No, i+1)
		}
	}
}

func TestFlattenCMODRows_AgencyAndDatesResolved(t *testing.T) {
	letters := []db.CaseLetterEntry{
		{
			CaseLetter: db.CaseLetter{
				ID: 1, CaseID: 9, Type: "Notice",
				LetterDate: cmodPtrTime(2024, 5, 1), ReceivedAt: cmodPtrTime(2024, 5, 2),
				SubmittedAt: cmodPtrTime(2024, 5, 3),
			},
			URLs: []string{"a.com"},
		},
	}
	rows := FlattenCMODRows(letters, nil, nil, map[uint]string{9: "PDRM"})
	if rows[0].Agency != "PDRM" {
		t.Fatalf("Agency = %q, want PDRM", rows[0].Agency)
	}
	if rows[0].LetterDate != "2024-05-01" || rows[0].Received != "2024-05-02" || rows[0].Submission != "2024-05-03" {
		t.Fatalf("date columns mismatch: %+v", rows[0])
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/blockexport/... -v -run TestFlattenCMODRows`
  Expected: FAIL (package/functions don't exist, or `crd.go`'s `formatDate`/`titleCaseFirst` helpers don't exist yet if Plan 03 hasn't merged in your worktree — see the note below)
- [ ] **Step 3: Implement `internal/blockexport/cmod.go`.** If `internal/blockexport/crd.go` already exists in your worktree (Plan 03 merged first), it already declares `package blockexport` and a `formatDate(*time.Time) string` helper — reuse that helper rather than redefining it. If it does **not** exist yet, add a local `formatDate` identical to Plan 03's (shown below) so this plan is independently buildable; if both plans' `formatDate` end up defined when merged together, delete the duplicate during merge (they're byte-identical, so keep either):
```go
package blockexport

import (
	"github.com/afif/dns-tracking/internal/db"
)

// formatDate — only add this function if internal/blockexport/crd.go does
// not already define it (see note above).
// func formatDate(t *time.Time) string {
// 	if t == nil {
// 		return ""
// 	}
// 	return t.Format(exportDateLayout) // exportDateLayout = "2006-01-02"
// }

type CMODRow struct {
	No          int
	LetterDate  string
	Recipient   string
	Type        string
	Subject     string
	ReferenceNo string
	OIC         string
	Requestor   string
	Offence     string
	Link        string
	Remarks     string
	Agency      string
	Status      string
	Received    string
	Submission  string
	CaseID      uint
	InternalRef string
}

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
```
Delete the commented-out `formatDate` block above once you've confirmed whether to keep it (only keep it if `crd.go` isn't present yet).
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/blockexport/... -v -run TestFlattenCMODRows`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/blockexport/cmod.go internal/blockexport/cmod_test.go
git commit -m "blockexport: flatten CMOD case letters into export rows"
```

---

### Task 2: `WriteCMODWorkbook`

**Files:**
- Modify: `internal/blockexport/cmod.go`
- Modify: `internal/blockexport/cmod_test.go`

**Interfaces (produced, relied on by Plan 05):**
```go
// WriteCMODWorkbook writes rows as a single-sheet .xlsx to w — a header row
// (the 17 column titles, in FlattenCMODRows' declared order) followed by
// one data row per CMODRow.
func WriteCMODWorkbook(rows []CMODRow, w io.Writer) error
```

- [ ] **Step 1: Write the failing test** — append to `internal/blockexport/cmod_test.go` (add `"bytes"` and `"github.com/xuri/excelize/v2"` to its imports):
```go
func TestWriteCMODWorkbook_RoundTrips(t *testing.T) {
	rows := []CMODRow{
		{No: 1, LetterDate: "2024-05-01", Recipient: "ISP A", Type: "Notice", Subject: "Blocking",
			ReferenceNo: "REF-1", OIC: "alice", Requestor: "bob", Offence: "Judi", Link: "a.com",
			Remarks: "note", Agency: "PDRM", Status: "Submitted", Received: "2024-05-02",
			Submission: "2024-05-03", CaseID: 9, InternalRef: "INT-1"},
	}
	var buf bytes.Buffer
	if err := WriteCMODWorkbook(rows, &buf); err != nil {
		t.Fatalf("WriteCMODWorkbook: %v", err)
	}

	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()

	got, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(got), got)
	}
	wantHeader := []string{
		"No", "Letter Date", "Recipient", "Type", "Subject", "Reference No", "OIC",
		"Requestor", "Offence", "Link", "Remarks", "Agency", "Status", "Received",
		"Submission", "Case ID", "Internal Ref (No. Rujukan NMSMD)",
	}
	for i, h := range wantHeader {
		if got[0][i] != h {
			t.Fatalf("header col %d = %q, want %q", i, got[0][i], h)
		}
	}
	if got[1][9] != "a.com" || got[1][6] != "alice" || got[1][15] != "9" {
		t.Fatalf("data row mismatch: %+v", got[1])
	}
}
```
- [ ] **Step 2: Run the test to verify it fails**
  Run: `go test ./internal/blockexport/... -run TestWriteCMODWorkbook -v`
  Expected: FAIL (`WriteCMODWorkbook` undefined)
- [ ] **Step 3: Implement `WriteCMODWorkbook`** — append to `internal/blockexport/cmod.go` (add `"io"` and `"github.com/xuri/excelize/v2"` to its imports):
```go
var cmodHeaders = []string{
	"No", "Letter Date", "Recipient", "Type", "Subject", "Reference No", "OIC",
	"Requestor", "Offence", "Link", "Remarks", "Agency", "Status", "Received",
	"Submission", "Case ID", "Internal Ref (No. Rujukan NMSMD)",
}

func WriteCMODWorkbook(rows []CMODRow, w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Sheet1"

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
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				return err
			}
		}
	}
	return f.Write(w)
}
```
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/blockexport/... -v`
  Expected: PASS (all `TestFlattenCMODRows_*` + `TestWriteCMODWorkbook_RoundTrips`, plus Plan 03's CRD tests if that plan has already merged into this worktree)
- [ ] **Step 5: `go mod tidy`** — same `excelize` promotion note as Plan 03; a no-op if Plan 03 already did it.
- [ ] **Step 6: Commit**
```bash
git add internal/blockexport/cmod.go internal/blockexport/cmod_test.go go.mod go.sum
git commit -m "blockexport: write CMOD export rows to a real .xlsx workbook"
```

## Verification for this plan as a whole

```bash
go build ./...
go test ./internal/blockexport/... -v
```
Both must pass.
