# CRD Compliance Export — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pure, unit-testable functions that flatten `db.CaseSummary` rows (the Cases view's data shape) into the CRD department's legacy Excel column layout, and write them to a real `.xlsx` workbook — the data-transform half of the CRD export. No DB, no HTTP; this plan produces a self-contained `internal/blockexport` package that `2026-09-04-compliance-report-export-05-export-routes.md` wires up to an HTTP endpoint.

**Prerequisite:** None. This plan only depends on `db.CaseSummary`/`db.CaseSummaryDomain`/`db.OffenceEntry`/`db.CaseLetterEntry`, which already exist in `internal/db/models.go` on `main` today.

**Architecture:** New package `internal/blockexport/crd.go` mirrors `internal/blockimport`'s existing shape (pure flatten functions separated from thin I/O code) but writes instead of reads. Uses `github.com/xuri/excelize/v2` (already a `go.mod` dependency, currently `// indirect` — this plan's `go build`/`go mod tidy` will promote it to a direct entry, since this is the first production code in the repo to *call* its writer API rather than only its reader API).

**Tech Stack:** Go, `github.com/xuri/excelize/v2`.

**Spec:** `docs/superpowers/specs/2026-09-04-compliance-report-export-design.md`, "CRD export — `FlattenCRDRows`" section.

## Global Constraints

- Every step must contain real code — no "add appropriate error handling" placeholders.
- `go test ./internal/blockexport/...` must pass at the end of this plan. Zero DB/HTTP dependency in the tests — everything here is pure data transformation plus file I/O to a `bytes.Buffer` or temp file.
- Dates render as plain `"YYYY-MM-DD"` text cells (`time.Time.Format("2006-01-02")`), not native Excel date-serial cells with number formatting — the simplest correct approach for a first version; upgrading to native date cells + column formatting is a cosmetic follow-up, not required by the design spec.
- This plan does not touch `internal/blockimport` — that package is read-only reference for excelize usage patterns, not a dependency of `internal/blockexport`.
- Column order below is taken verbatim from the design spec — do not reorder, rename, or drop a column.

---

### Task 1: `CRDRow` type + `FlattenCRDRows`

**Files:**
- Create: `internal/blockexport/crd.go`
- Test: `internal/blockexport/crd_test.go`

**Interfaces (produced, relied on by Task 2 and by Plan 05):**
```go
package blockexport

// CRDRow is one flattened output row, one field per export column, in
// declaration order matching the column table below.
type CRDRow struct {
	Tahun            int    // col 1
	AlamatLamanWeb   string // col 2
	ButiranKesalahan string // col 3
	Agensi           string // col 4
	TarikhBlocked    string // col 5, "YYYY-MM-DD" or ""
	NoRujukanNMD     string // col 6
	Kategori         string // col 7
	Elemen           string // col 8
	SubElemen        string // col 9
	Status           string // col 10
	TarikhUplift     string // col 11, "YYYY-MM-DD" or ""
	NoRujukanNMSMD   string // col 12
	Remarks          string // col 13
	DotMY            string // col 14, "Yes"/"No"
	CaseID           uint   // col 15
	Department       string // col 16
	DueDate          string // col 17, "YYYY-MM-DD" or ""
}

// FlattenCRDRows expands cases into export rows — one row per
// CaseSummaryDomain x its Offences (a domain with zero offences still gets
// exactly one row, with Kategori/Elemen/SubElemen/ButiranKesalahan blank).
// letters must contain every db.CaseLetterEntry for the same case IDs as
// cases (both "Notice" and "Notice (Uplift)" types) — CaseSummary's own
// Notice* fields collapse to whichever letter is most recent once a case
// has been uplifted, losing the original block date, so this function
// cross-references the raw letter list to recover both independently. Pure
// function, no DB/HTTP.
func FlattenCRDRows(cases []db.CaseSummary, letters []db.CaseLetterEntry) []CRDRow
```

**Column table (from the design spec, copy exactly):**

| # | Column | Source |
|---|---|---|
| 1 | `Tahun` | Case's earliest `"Notice"`-type letter's `LetterDate.Year()`; falls back to `CaseSummary.RequestedAt.Year()`, then `CaseSummary.CreatedAt.Year()` |
| 2 | `Alamat Laman Web` | `CaseSummaryDomain.URL` |
| 3 | `Butiran Kesalahan` | `OffenceEntry.Citation` |
| 4 | `Agensi` | `CaseSummary.AgencyName` |
| 5 | `Tarikh Maklum IASP (Blocked)` | That case's earliest `"Notice"` (NOT `"Notice (Uplift)"`) letter's `LetterDate` |
| 6 | `No. Rujukan NMD` | That Notice letter's `ReferenceNumberExternal` |
| 7 | `Kategori` | `OffenceEntry.Category` |
| 8 | `Elemen` | `OffenceEntry.Element` |
| 9 | `Sub-Elemen` | `OffenceEntry.SubElement` |
| 10 | `Status` | `CaseSummaryDomain.Status`, title-cased (first letter upper, rest as-is) — app's own vocabulary (`requested`/`uplift`/`suspended`), never translated to legacy wording |
| 11 | `Tarikh Maklum ISP (Uplift)` | That case's earliest `"Notice (Uplift)"` letter's `LetterDate`, blank if none exists |
| 12 | `No. Rujukan NMSMD` | The Notice letter's (not Uplift's) `ReferenceNumberInternal` |
| 13 | `Remarks` | The Notice letter's `Remarks` |
| 14 | `.my` | `"Yes"`/`"No"` via `strings.HasSuffix(url, ".my")` |
| 15 | `Case ID` | `CaseSummary.ID` |
| 16 | `Department` | `CaseLetterEntry.DepartmentName` — any letter belonging to the case carries the same value; take the first one found |
| 17 | `Due Date` | `CaseSummary.DueDate`, blank if nil |

**Grain**: one row per `CaseSummaryDomain` x its `Offences`. A domain with zero attached offences still emits exactly one row (columns 3, 7-9 blank).

- [ ] **Step 1: Write the failing tests** in `internal/blockexport/crd_test.go`:
```go
package blockexport

import (
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
)

func ptrTime(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestFlattenCRDRows_MultiOffenceExpandsToMultipleRows(t *testing.T) {
	cases := []db.CaseSummary{{
		ID:         1,
		AgencyName: "PDRM",
		Domains: []db.CaseSummaryDomain{{
			URL:    "example.com",
			Status: "requested",
			Offences: []db.OffenceEntry{
				{Citation: "s.233", Category: "Judi"},
				{Citation: "s.234", Category: "Lucah"},
			},
		}},
	}}
	rows := FlattenCRDRows(cases, nil)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].AlamatLamanWeb != "example.com" || rows[1].AlamatLamanWeb != "example.com" {
		t.Fatalf("both rows should carry the same domain: %+v", rows)
	}
	if rows[0].Kategori != "Judi" || rows[1].Kategori != "Lucah" {
		t.Fatalf("expected distinct categories per row, got %+v", rows)
	}
	if rows[0].CaseID != 1 || rows[1].CaseID != 1 {
		t.Fatalf("expected CaseID 1 on both rows: %+v", rows)
	}
}

func TestFlattenCRDRows_ZeroOffenceFallsBackToOneRow(t *testing.T) {
	cases := []db.CaseSummary{{
		ID: 1,
		Domains: []db.CaseSummaryDomain{{URL: "example.com", Status: "requested"}},
	}}
	rows := FlattenCRDRows(cases, nil)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(rows), rows)
	}
	if rows[0].Kategori != "" || rows[0].ButiranKesalahan != "" {
		t.Fatalf("expected blank offence columns, got %+v", rows[0])
	}
}

func TestFlattenCRDRows_SplitsNoticeAndUpliftDatesIndependently(t *testing.T) {
	cases := []db.CaseSummary{{
		ID:      5,
		Domains: []db.CaseSummaryDomain{{URL: "a.com", Status: "uplift"}},
	}}
	letters := []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 10, CaseID: 5, Type: "Notice", LetterDate: ptrTime(2024, 1, 10), ReferenceNumberExternal: "REF-EXT"}, DepartmentName: "CRD"},
		{CaseLetter: db.CaseLetter{ID: 11, CaseID: 5, Type: "Notice (Uplift)", LetterDate: ptrTime(2024, 6, 1)}, DepartmentName: "CRD"},
	}
	rows := FlattenCRDRows(cases, letters)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].TarikhBlocked != "2024-01-10" {
		t.Fatalf("TarikhBlocked = %q, want 2024-01-10", rows[0].TarikhBlocked)
	}
	if rows[0].TarikhUplift != "2024-06-01" {
		t.Fatalf("TarikhUplift = %q, want 2024-06-01", rows[0].TarikhUplift)
	}
	if rows[0].NoRujukanNMD != "REF-EXT" {
		t.Fatalf("NoRujukanNMD = %q, want REF-EXT", rows[0].NoRujukanNMD)
	}
	if rows[0].Department != "CRD" {
		t.Fatalf("Department = %q, want CRD", rows[0].Department)
	}
}

func TestFlattenCRDRows_DotMYSuffix(t *testing.T) {
	cases := []db.CaseSummary{{
		ID: 1,
		Domains: []db.CaseSummaryDomain{
			{URL: "example.com.my", Status: "requested"},
			{URL: "example.com", Status: "requested"},
		},
	}}
	rows := FlattenCRDRows(cases, nil)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	got := map[string]string{rows[0].AlamatLamanWeb: rows[0].DotMY, rows[1].AlamatLamanWeb: rows[1].DotMY}
	if got["example.com.my"] != "Yes" {
		t.Fatalf("example.com.my DotMY = %q, want Yes", got["example.com.my"])
	}
	if got["example.com"] != "No" {
		t.Fatalf("example.com DotMY = %q, want No", got["example.com"])
	}
}

func TestFlattenCRDRows_StatusTitleCased(t *testing.T) {
	cases := []db.CaseSummary{{
		ID:      1,
		Domains: []db.CaseSummaryDomain{{URL: "a.com", Status: "requested"}},
	}}
	rows := FlattenCRDRows(cases, nil)
	if rows[0].Status != "Requested" {
		t.Fatalf("Status = %q, want Requested", rows[0].Status)
	}
}

func TestFlattenCRDRows_YearFallbackChain(t *testing.T) {
	requestedAt := ptrTime(2022, 3, 1)
	cases := []db.CaseSummary{{
		ID:          1,
		RequestedAt: requestedAt,
		CreatedAt:   time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
		Domains:     []db.CaseSummaryDomain{{URL: "a.com", Status: "requested"}},
	}}
	rows := FlattenCRDRows(cases, nil) // no Notice letter -> falls back to RequestedAt
	if rows[0].Tahun != 2022 {
		t.Fatalf("Tahun = %d, want 2022 (RequestedAt fallback)", rows[0].Tahun)
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/blockexport/... -v`
  Expected: FAIL (package/functions don't exist)
- [ ] **Step 3: Implement `internal/blockexport/crd.go`:**
```go
// Package blockexport flattens the app's case/case-letter data into the
// legacy CRD/CMOD Excel column layouts and writes them as real .xlsx
// workbooks — the export half of internal/blockimport's import.
package blockexport

import (
	"strings"
	"time"

	"github.com/afif/dns-tracking/internal/db"
)

const exportDateLayout = "2006-01-02"

func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(exportDateLayout)
}

func titleCaseFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type CRDRow struct {
	Tahun            int
	AlamatLamanWeb   string
	ButiranKesalahan string
	Agensi           string
	TarikhBlocked    string
	NoRujukanNMD     string
	Kategori         string
	Elemen           string
	SubElemen        string
	Status           string
	TarikhUplift     string
	NoRujukanNMSMD   string
	Remarks          string
	DotMY            string
	CaseID           uint
	Department       string
	DueDate          string
}

// caseLetterInfo holds the earliest Notice and earliest Notice (Uplift)
// letter for one case, plus its department name.
type caseLetterInfo struct {
	notice     *db.CaseLetterEntry
	uplift     *db.CaseLetterEntry
	department string
}

func earlierLetter(cur, candidate *db.CaseLetterEntry) *db.CaseLetterEntry {
	if cur == nil {
		return candidate
	}
	if candidate.LetterDate == nil {
		return cur
	}
	if cur.LetterDate == nil || candidate.LetterDate.Before(*cur.LetterDate) {
		return candidate
	}
	return cur
}

func indexCRDLettersByCase(letters []db.CaseLetterEntry) map[uint]*caseLetterInfo {
	byCase := make(map[uint]*caseLetterInfo)
	for i := range letters {
		l := &letters[i]
		info, ok := byCase[l.CaseID]
		if !ok {
			info = &caseLetterInfo{}
			byCase[l.CaseID] = info
		}
		if info.department == "" {
			info.department = l.DepartmentName
		}
		switch l.Type {
		case "Notice":
			info.notice = earlierLetter(info.notice, l)
		case "Notice (Uplift)":
			info.uplift = earlierLetter(info.uplift, l)
		}
	}
	return byCase
}

func FlattenCRDRows(cases []db.CaseSummary, letters []db.CaseLetterEntry) []CRDRow {
	infoByCase := indexCRDLettersByCase(letters)

	var out []CRDRow
	for _, c := range cases {
		info := infoByCase[c.ID]
		var notice, uplift *db.CaseLetterEntry
		department := ""
		if info != nil {
			notice, uplift, department = info.notice, info.uplift, info.department
		}

		year := c.CreatedAt.Year()
		if c.RequestedAt != nil {
			year = c.RequestedAt.Year()
		}
		if notice != nil && notice.LetterDate != nil {
			year = notice.LetterDate.Year()
		}

		blocked, refExternal, refInternal, remarks := "", "", "", ""
		if notice != nil {
			blocked = formatDate(notice.LetterDate)
			refExternal = notice.ReferenceNumberExternal
			refInternal = notice.ReferenceNumberInternal
			remarks = notice.Remarks
		}
		upliftDate := ""
		if uplift != nil {
			upliftDate = formatDate(uplift.LetterDate)
		}

		for _, d := range c.Domains {
			my := "No"
			if strings.HasSuffix(d.URL, ".my") {
				my = "Yes"
			}
			base := CRDRow{
				Tahun: year, AlamatLamanWeb: d.URL, Agensi: c.AgencyName,
				TarikhBlocked: blocked, NoRujukanNMD: refExternal,
				Status: titleCaseFirst(d.Status), TarikhUplift: upliftDate,
				NoRujukanNMSMD: refInternal, Remarks: remarks, DotMY: my,
				CaseID: c.ID, Department: department, DueDate: formatDate(c.DueDate),
			}
			if len(d.Offences) == 0 {
				out = append(out, base)
				continue
			}
			for _, off := range d.Offences {
				row := base
				row.ButiranKesalahan = off.Citation
				row.Kategori = off.Category
				row.Elemen = off.Element
				row.SubElemen = off.SubElement
				out = append(out, row)
			}
		}
	}
	return out
}
```
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/blockexport/... -v -run TestFlattenCRDRows`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/blockexport/crd.go internal/blockexport/crd_test.go
git commit -m "blockexport: flatten CRD case summaries into export rows"
```

---

### Task 2: `WriteCRDWorkbook`

**Files:**
- Modify: `internal/blockexport/crd.go`
- Modify: `internal/blockexport/crd_test.go`

**Interfaces (produced, relied on by Plan 05):**
```go
// WriteCRDWorkbook writes rows as a single-sheet .xlsx to w — a header row
// (the 17 column titles, in FlattenCRDRows' declared order) followed by one
// data row per CRDRow.
func WriteCRDWorkbook(rows []CRDRow, w io.Writer) error
```

- [ ] **Step 1: Write the failing test** — append to `internal/blockexport/crd_test.go`:
```go
func TestWriteCRDWorkbook_RoundTrips(t *testing.T) {
	rows := []CRDRow{
		{Tahun: 2024, AlamatLamanWeb: "example.com", ButiranKesalahan: "s.233", Agensi: "PDRM",
			TarikhBlocked: "2024-01-10", NoRujukanNMD: "REF-1", Kategori: "Judi", Elemen: "E1",
			SubElemen: "SE1", Status: "Requested", TarikhUplift: "", NoRujukanNMSMD: "INT-1",
			Remarks: "note", DotMY: "No", CaseID: 1, Department: "CRD", DueDate: "2024-02-01"},
	}
	var buf bytes.Buffer
	if err := WriteCRDWorkbook(rows, &buf); err != nil {
		t.Fatalf("WriteCRDWorkbook: %v", err)
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
	if len(got) != 2 { // header + 1 data row
		t.Fatalf("got %d rows, want 2: %+v", len(got), got)
	}
	wantHeader := []string{
		"Tahun", "Alamat Laman Web", "Butiran Kesalahan", "Agensi",
		"Tarikh Maklum IASP (Blocked)", "No. Rujukan NMD", "Kategori", "Elemen",
		"Sub-Elemen", "Status", "Tarikh Maklum ISP (Uplift)", "No. Rujukan NMSMD",
		"Remarks", ".my", "Case ID", "Department", "Due Date",
	}
	for i, h := range wantHeader {
		if got[0][i] != h {
			t.Fatalf("header col %d = %q, want %q", i, got[0][i], h)
		}
	}
	if got[1][1] != "example.com" || got[1][5] != "REF-1" || got[1][14] != "1" {
		t.Fatalf("data row mismatch: %+v", got[1])
	}
}
```
Add `"bytes"` and `"github.com/xuri/excelize/v2"` to the test file's imports.
- [ ] **Step 2: Run the test to verify it fails**
  Run: `go test ./internal/blockexport/... -run TestWriteCRDWorkbook -v`
  Expected: FAIL (`WriteCRDWorkbook` undefined)
- [ ] **Step 3: Implement `WriteCRDWorkbook`** — append to `internal/blockexport/crd.go` (add `"io"` and `"github.com/xuri/excelize/v2"` to its imports):
```go
var crdHeaders = []string{
	"Tahun", "Alamat Laman Web", "Butiran Kesalahan", "Agensi",
	"Tarikh Maklum IASP (Blocked)", "No. Rujukan NMD", "Kategori", "Elemen",
	"Sub-Elemen", "Status", "Tarikh Maklum ISP (Uplift)", "No. Rujukan NMSMD",
	"Remarks", ".my", "Case ID", "Department", "Due Date",
}

func WriteCRDWorkbook(rows []CRDRow, w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Sheet1"

	for i, h := range crdHeaders {
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
			row.Tahun, row.AlamatLamanWeb, row.ButiranKesalahan, row.Agensi,
			row.TarikhBlocked, row.NoRujukanNMD, row.Kategori, row.Elemen,
			row.SubElemen, row.Status, row.TarikhUplift, row.NoRujukanNMSMD,
			row.Remarks, row.DotMY, row.CaseID, row.Department, row.DueDate,
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
  Expected: PASS (all `TestFlattenCRDRows_*` + `TestWriteCRDWorkbook_RoundTrips`)
- [ ] **Step 5: `go mod tidy`** — promotes `github.com/xuri/excelize/v2` from `// indirect` to a direct `go.mod` entry, since this is the first production (non-test) code calling into it for writing.
- [ ] **Step 6: Commit**
```bash
git add internal/blockexport/crd.go internal/blockexport/crd_test.go go.mod go.sum
git commit -m "blockexport: write CRD export rows to a real .xlsx workbook"
```

## Verification for this plan as a whole

```bash
go build ./...
go test ./internal/blockexport/... -v
```
Both must pass.
