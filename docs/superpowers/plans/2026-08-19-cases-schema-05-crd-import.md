# CRD Blocklist Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A one-off, re-runnable CLI (`cmd/import-crd/`) that imports
`Blocking Full List_1.xlsx` (repo-root, gitignored — the user has it
locally) into `cases`/`case_letters`/`case_urls`, per the mapping decided
in `docs/blocking-list-migration-clarifications.md`. `url_offences`
(Category/Element/citation linking) is deliberately **out of scope** — see
the Global Constraints note below.

**Prerequisite:** `2026-08-19-cases-schema-01-foundation.md` must already
be merged to `main` — this plan only needs `Case`/`CaseLetter`/`CaseURL`
(and the pre-existing `URL`/`URLOffence`/`Category`/`Agency` models) to
exist. It does **not** depend on Task 02/03/04 (the store/API/frontend
layers) — this importer talks to the database directly via
`internal/db.Connect`, the same way `cmd/server`'s `main.go` does, not
through the HTTP API.

**Architecture:** New third binary, `cmd/import-crd/main.go`. Reads the
sheet with `github.com/xuri/excelize/v2` (new dependency — MIT-licensed,
actively maintained, the standard choice for this in Go; add via
`go get`). Builds one `Case`+`CaseLetter`+`CaseURL` (+ `URLOffence`) set
per collapsed (base reference number, url) group, using
`internal/urlnorm.Normalize` for every URL and `internal/db.CreateURL`'s
get-or-create semantics so re-running the import is idempotent against
already-imported domains. Ships with a `--dry-run` flag that prints a
summary (rows read, cases that would be created, rows skipped and why)
without writing anything — given the open data-quality caveats below, a
dry run must be reviewed before a real run against production.

**Tech Stack:** Go, `github.com/xuri/excelize/v2`, GORM (via
`internal/db`).

**Spec:** `docs/blocking-list-migration-clarifications.md` (the EDA this
plan implements the decisions from) and `docs/db-schema.dbml`'s
`cases`/`case_letters`/`case_urls` tables.

## Global Constraints

- **This plan does not decide the four open questions in
  `docs/blocking-list-migration-clarifications.md`** (status mapping,
  history-vs-collapse, compound-category splitting, the 3 unrecoverable
  garbled URLs) — those need product sign-off per that doc's closing
  paragraph and `CLAUDE.md`'s TODO list entry ("CRD + CMOD blocking-list
  migration... Nothing imported yet — pending sign-off"). **Do not run a
  real (non-`--dry-run`) import against a production database from this
  plan without that sign-off having actually happened** — build and test
  the tool against a local/throwaway database only, and treat `--dry-run`
  output review as the deliverable, not a live import. If sign-off has
  landed by the time this task executes, the decisions below (marked
  RESOLVED) already reflect it; anything still marked OPEN in the source
  doc must be resolved before flipping off `--dry-run` for a real run.
- The source file is gitignored and not present in a fresh checkout —
  this plan's tests use a small synthetic `.xlsx` fixture built in the
  test itself (via `excelize`'s writer API), not the real file. Verifying
  against the real file is a manual step (Step 5 of Task 3), not part of
  the automated test suite.
- Every row this importer touches must first pass
  `internal/urlnorm.Normalize` — a row whose URL fails to normalize is
  skipped and counted, never silently dropped without a log line (mirrors
  `db.NormalizeAndDedupeURLs`'s "log and skip, non-fatal" philosophy).
- Idempotent: running the importer twice against the same file and the
  same database must not create duplicate `Case` rows for the same
  (department, base reference number) — key the "does this case already
  exist" check on `CaseLetter.ReferenceNumber` within department CRD's
  cases.
- **`url_offences`/`Category`/`Citation` linking is out of scope for this
  plan.** `URLOffence.CategoryID` requires a real `Category` row, which
  requires a `Citation` scoped to an `Instrument` — inferring an
  Instrument from the sheet's free-text `Butiran Kesalahan` column isn't
  a mapping decision this migration has settled (the EDA doc leaves both
  "same taxonomy as CMOD's `Offence`?" and "which Instrument?" as open
  questions, and `internal/legalcite.Parse` parses citation *text* within
  an already-chosen Instrument, it doesn't pick the Instrument for you).
  This task captures `Category`/`Element`/`CitationText`/`Agency` per row
  for visibility in the dry-run summary only — it does not create
  `Category`/`Citation`/`URLOffence` rows. Wiring that up is a follow-up
  task once the taxonomy question has product sign-off, not part of this
  plan.

---

### Task 1: Column mapping + row-collapsing (pure function, no I/O)

**Files:**
- Create: `internal/blockimport/crd.go`
- Test: `internal/blockimport/crd_test.go`

**Before writing this task's code:** open the actual
`Blocking Full List_1.xlsx` (repo root) and confirm the sheet name
(`"2011-2026"` per the EDA doc) and column headers match what's assumed
below — the EDA doc's findings are the ground truth for *mapping rules*,
but column order/exact header spelling should be re-verified against the
live file before writing the header-index-resolution code, since a header
row can be edited between the EDA pass and now. If anything's drifted,
follow what the actual file shows and note the discrepancy in this task's
commit message.

**Known columns** (from the EDA doc, `docs/blocking-list-migration-clarifications.md`):
`No. Rujukan NMD`, `No. Rujukan NMSMD`, a domain/URL column, `Status`,
`Kategori`, `Elemen`, `Butiran Kesalahan` (citation text), `Agensi`, and a
year column. Resolve exact header strings from the live file's row 1
rather than hardcoding positional indices — `excelize`'s `GetRows` gives
you the header row; build a `map[string]int` from header text to column
index once, then look up by name for every data row. This survives column
reordering; a positional-index approach doesn't.

**Interfaces (produced, relied on by Task 2):**
```go
package blockimport

// CRDRow is one raw spreadsheet row after column-name resolution, before
// any collapsing/validation.
type CRDRow struct {
	ReferenceNumber string // "No. Rujukan NMD" — the base ref before any suffix handling (CRD's sheet doesn't use CMOD's -1/-2/-3/-4 suffix convention at all, per the EDA doc)
	Domain          string // raw, not yet normalized
	Status          string
	Category        string // "Kategori"
	Element         string // "Elemen"
	CitationText    string // "Butiran Kesalahan"
	Agency          string // "Agensi"
	Year            int
}

// ParseCRDRows reads every data row from the sheet at path, resolving
// columns by header name. Returns an error only for a file-level failure
// (can't open, sheet not found) — a single malformed row is not a parse
// error, it's reflected in the returned rows (empty Domain, etc.) for the
// caller to validate downstream.
func ParseCRDRows(path string) ([]CRDRow, error) { /* ... */ }

// CollapsedCase is one (base reference number) group after collapsing —
// the unit that becomes one Case. Domains holds every (url, status) pair
// under this reference, since a reference legitimately covers many urls.
type CollapsedCase struct {
	ReferenceNumber string
	Domains         []CollapsedDomain
	Category        string // most-common Category across the group's rows — see Task 2's decision note
	Element         string
	CitationText    string
	Agency          string
}

type CollapsedDomain struct {
	RawDomain string
	Status    string // last-write-wins across the group if it repeats — see Task 2
}

// CollapseCRDRows groups rows by ReferenceNumber into one CollapsedCase
// per distinct reference. Does NOT normalize URLs or hit the database —
// pure in-memory transform, testable without I/O.
func CollapseCRDRows(rows []CRDRow) []CollapsedCase { /* ... */ }
```

**Decisions this task's implementation must apply** (from the EDA doc,
already resolved by the doc's own findings — not new open questions):
- 8 comma-joined compound `Kategori` values (e.g. `"Jelik, Palsu, Lucah"`)
  — split on `,`, trim each, keep all as separate entries in
  `CollapsedCase.Category` becoming multiple `URLOffence` rows in Task 2
  (one row per split category) rather than picking one primary. This
  mirrors how the schema already supports a url having multiple
  `url_offences` rows.
- 12 column-shift rows (`Kategori` blank, `Elemen` holds a category name)
  — detect via `Category == "" && Element != ""` and swap them during
  parsing (`ParseCRDRows`, not `CollapseCRDRows` — it's a per-row data
  fix, not a collapsing concern).
- 26 unnormalizable URLs (20 stray-space, 3 garbled, 1 numbered-list
  prefix) — the 20 stray-space and 1 numbered-list-prefix cases get fixed
  by a trim/regex pass in `ParseCRDRows` (strip a leading `^\d+\.\s*`
  prefix; strip whitespace immediately after `://`). The 3 garbled rows
  are left as-is here — Task 2 is where they get counted and skipped via
  `urlnorm.Normalize`'s error return, not silently fixed with a guess.

- [ ] **Step 1: Write the failing tests** in `internal/blockimport/crd_test.go`:
```go
func TestParseCRDRows_ResolvesColumnsByHeaderName(t *testing.T) {
	path := writeTestXLSX(t, "2011-2026", [][]string{
		{"No. Rujukan NMD", "No. Rujukan NMSMD", "URL", "Status", "Kategori", "Elemen", "Butiran Kesalahan", "Agensi", "Year"},
		{"REF-1", "", "http://example.com", "Blocked", "Judi", "", "Seksyen 233", "PDRM", "2023"},
	})
	rows, err := ParseCRDRows(path)
	if err != nil {
		t.Fatalf("ParseCRDRows: %v", err)
	}
	if len(rows) != 1 || rows[0].ReferenceNumber != "REF-1" || rows[0].Domain != "http://example.com" {
		t.Fatalf("got %+v", rows)
	}
}

func TestParseCRDRows_FixesColumnShiftBug(t *testing.T) {
	// Kategori blank, Elemen holds "Kepentingan Negara" -- assert the
	// parsed row has Category == "Kepentingan Negara", Element == "".
}

func TestParseCRDRows_StripsNumberedListPrefix(t *testing.T) {
	// Domain cell "19. http://www.example.net" -> Domain == "http://www.example.net"
}

func TestCollapseCRDRows_GroupsByReferenceNumber(t *testing.T) {
	rows := []CRDRow{
		{ReferenceNumber: "REF-1", Domain: "a.com", Status: "Blocked", Category: "Judi"},
		{ReferenceNumber: "REF-1", Domain: "b.com", Status: "Uplift", Category: "Judi"},
		{ReferenceNumber: "REF-2", Domain: "c.com", Status: "Blocked", Category: "Palsu"},
	}
	cases := CollapseCRDRows(rows)
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	ref1 := findCase(t, cases, "REF-1")
	if len(ref1.Domains) != 2 {
		t.Fatalf("REF-1 domains = %+v, want 2", ref1.Domains)
	}
}

func TestCollapseCRDRows_SplitsCompoundCategory(t *testing.T) {
	rows := []CRDRow{{ReferenceNumber: "REF-1", Domain: "a.com", Category: "Jelik, Palsu, Lucah"}}
	cases := CollapseCRDRows(rows)
	// however Category is represented post-split -- e.g. a []string field,
	// or verify downstream in Task 2's URLOffence-creation test instead if
	// you decide to keep CollapsedCase.Category as a raw string and split
	// at URLOffence-creation time. Pick one and be consistent with the
	// CollapsedCase struct actually shipped in Step 3.
}
```
`writeTestXLSX(t, sheetName string, rows [][]string) string` is a small
test helper you write once in `crd_test.go` using `excelize.NewFile()` +
`SetSheetName`/`SetCellValue`/`SaveAs` to a `t.TempDir()` path — reused by
every test in this file.
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/blockimport/... -v`
  Expected: FAIL (package/functions don't exist)
- [ ] **Step 3: `go get github.com/xuri/excelize/v2`**, then implement
  `ParseCRDRows`/`CollapseCRDRows`/`CRDRow`/`CollapsedCase`/
  `CollapsedDomain` in `internal/blockimport/crd.go` per the rules above.
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/blockimport/... -v`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add go.mod go.sum internal/blockimport/crd.go internal/blockimport/crd_test.go
git commit -m "blockimport: parse and collapse CRD blocklist rows"
```

---

### Task 2: Write collapsed cases to the database

**Files:**
- Create: `internal/blockimport/write.go`
- Test: `internal/blockimport/write_test.go`

**Interfaces:**
```go
// ImportSummary is what a run (dry or real) reports.
type ImportSummary struct {
	CasesCreated       int
	CasesSkippedExist  int            // already imported (idempotency)
	URLsSkippedBadURL  int            // failed urlnorm.Normalize
	CategoriesObserved map[string]int // raw Category value -> row count, for visibility only (see Global Constraints -- no Category/URLOffence rows are created)
}

// WriteCRDCases creates one Case (DepartmentID = the CRD department's ID)
// per CollapsedCase, one CaseLetter (Type: "Notice" -- see the schema
// doc's resolved default for CRD-sourced letters) carrying the reference
// number, and one CaseURL per domain (Phase mapped from CRDRow.Status via
// mapCRDStatus, see below). Does NOT create Category/Citation/URLOffence
// rows -- see Global Constraints. dryRun=true does every lookup/validation
// but wraps all writes in a transaction that's always rolled back, so
// ImportSummary reflects exactly what a real run would do.
func WriteCRDCases(ctx context.Context, db *gorm.DB, crdDeptID uint, cases []CollapsedCase, dryRun bool) (ImportSummary, error) { /* ... */ }

// mapCRDStatus maps the spreadsheet's Status values onto case_urls.phase's
// vocabulary (requested | uplift | suspended). "Blocked"/"blocked" ->
// "requested" (a blocked domain has an active request), "Uplift" ->
// "uplift", "Suspended" -> "suspended", "Not Blocked"/"Not blocked" and
// empty -> "" (case_urls.phase is NOT NULL in the schema -- see the note
// below on what "" means here before shipping this).
func mapCRDStatus(raw string) string { /* ... */ }
```

**Open item this task must not silently paper over:** `case_urls.phase`
is `not null` in the schema, but the EDA doc's Question 1 flags 268+4
"Not Blocked" rows and 21 empty-Status rows with no clean mapping onto
`requested | uplift | suspended`. This plan does **not** resolve that —
per `docs/blocking-list-migration-clarictions.md`'s open question, it
needs product sign-off. Implement `mapCRDStatus` to return `"requested"`
for "Not Blocked"/empty as a placeholder default (matching "the row still
represents a real request even if not currently enforced," the safest of
the doc's own candidate answers), but **leave a `ponytail:` comment on
`mapCRDStatus` naming this exact assumption and citing the open question**
so it's easy to find and revisit once sign-off lands — don't let this
default get mistaken for a resolved decision.

- [ ] **Step 1: Write the failing tests** in `internal/blockimport/write_test.go`
  (use an in-memory sqlite `*gorm.DB` via `internal/db.Connect` with the
  sqlite dialector, same as `internal/db`'s own tests):
```go
func TestWriteCRDCases_CreatesOneCasePerReference(t *testing.T) {
	gdb := newTestGormDB(t) // mirror internal/db test setup
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains: []CollapsedDomain{{RawDomain: "example.com", Status: "Blocked"}},
		Category: "Judi",
	}}
	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1", summary.CasesCreated)
	}
	var count int64
	gdb.Model(&db.Case{}).Count(&count)
	if count != 1 {
		t.Fatalf("cases table has %d rows, want 1", count)
	}
}

func TestWriteCRDCases_IsIdempotent(t *testing.T) {
	// run WriteCRDCases twice with the same input; assert the second run's
	// CasesSkippedExist == 1, CasesCreated == 0, and the cases table still
	// has exactly 1 row.
}

func TestWriteCRDCases_SkipsUnnormalizableURL(t *testing.T) {
	// a CollapsedDomain with RawDomain "https://solar123movies.cB33:B69om/"
	// (one of the 3 known-garbled rows) -- assert URLsSkippedBadURL == 1
	// and no CaseURL row is created for it, but the Case/CaseLetter still
	// get created if the group has at least one other valid domain.
}

func TestWriteCRDCases_DryRunWritesNothing(t *testing.T) {
	// dryRun=true -- assert the returned summary is non-zero (reflects
	// what would happen) but the cases/case_letters/case_urls tables are
	// still empty afterward.
}

func TestMapCRDStatus(t *testing.T) {
	cases := map[string]string{
		"Blocked": "requested", "blocked": "requested",
		"Uplift": "uplift", "Suspended": "suspended",
		"Not Blocked": "requested", "Not blocked": "requested", "": "requested",
	}
	for in, want := range cases {
		if got := mapCRDStatus(in); got != want {
			t.Errorf("mapCRDStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/blockimport/... -run TestWriteCRDCases -v`
  Expected: FAIL
- [ ] **Step 3: Implement `WriteCRDCases`/`mapCRDStatus`** in
  `internal/blockimport/write.go`. Use `db.CreateURL`-equivalent
  get-or-create semantics for each domain (call through to the same
  normalize+`FirstOrCreate` logic `internal/db.postgresStore.CreateURL`
  uses — either import and call it directly if it's exported, or, if it's
  unexported, replicate the two-line `urlnorm.Normalize` +
  `clause.OnConflict`-based `FirstOrCreate` pattern inline; check
  `internal/db/postgres.go`'s `CreateURL` implementation before choosing).
  Idempotency check: before creating a `Case`, query for an existing
  `CaseLetter` with `ReferenceNumber = <this ref> AND Type = 'Notice'`
  joined to a `Case` with `DepartmentID = crdDeptID` — if found, skip
  (increment `CasesSkippedExist`) rather than creating a duplicate.
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/blockimport/... -v`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/blockimport/write.go internal/blockimport/write_test.go
git commit -m "blockimport: write CRD cases to the database, idempotent, dry-run capable"
```

---

### Task 3: `cmd/import-crd/main.go` CLI

**Files:**
- Create: `cmd/import-crd/main.go`

**Flags** (mirror `cmd/crawler`/`cmd/server`'s self-documenting `--help`
convention — every flag needs a description, per `CLAUDE.md`'s "Both
binaries are self-documenting" line, now three binaries):
- `--file` (required) — path to the `.xlsx`
- `--db-url` (required) — same PostgreSQL DSN format as `cmd/server`'s
  `--db-url`
- `--dry-run` (default `true` — **opt out, not opt in**, given the open
  data-quality questions this plan's Global Constraints section flags)

```go
func main() {
	file := flag.String("file", "", "path to Blocking Full List_1.xlsx")
	dbURL := flag.String("db-url", "", "PostgreSQL DSN (key=value pairs)")
	dryRun := flag.Bool("dry-run", true, "print what would be imported without writing (default true — pass --dry-run=false for a real run)")
	flag.Parse()
	if *file == "" || *dbURL == "" {
		fmt.Fprintln(os.Stderr, "--file and --db-url are required")
		os.Exit(1)
	}

	rows, err := blockimport.ParseCRDRows(*file)
	if err != nil {
		log.Fatalf("parsing %s: %v", *file, err)
	}
	cases := blockimport.CollapseCRDRows(rows)

	gormDB, err := db.Connect(postgres.Open(*dbURL))
	if err != nil {
		log.Fatalf("connecting to db: %v", err)
	}
	var crdDept db.Department
	if err := gormDB.Where("name = ?", "CRD").First(&crdDept).Error; err != nil {
		log.Fatalf("looking up CRD department (run db.SeedDepartments first): %v", err)
	}

	summary, err := blockimport.WriteCRDCases(context.Background(), gormDB, crdDept.ID, cases, *dryRun)
	if err != nil {
		log.Fatalf("importing: %v", err)
	}
	mode := "DRY RUN"
	if !*dryRun {
		mode = "LIVE"
	}
	fmt.Printf("[%s] %d rows parsed, %d cases collapsed\n", mode, len(rows), len(cases))
	fmt.Printf("  cases created:        %d\n", summary.CasesCreated)
	fmt.Printf("  cases already existed: %d\n", summary.CasesSkippedExist)
	fmt.Printf("  urls skipped (bad url): %d\n", summary.URLsSkippedBadURL)
	fmt.Printf("  distinct categories observed (not imported, see plan's Global Constraints): %d\n", len(summary.CategoriesObserved))
}
```

- [ ] **Step 1:** Implement `cmd/import-crd/main.go` per above (adjust
  import paths/`postgres.Open` usage to match exactly how `cmd/server`'s
  `main.go` opens its GORM dialector — copy that, don't reinvent it).
- [ ] **Step 2: `go build ./cmd/import-crd/`**
  Expected: builds clean.
- [ ] **Step 3: `go vet ./...` and `go build ./...`** — confirm this new
  binary doesn't break anything else in the module.
- [ ] **Step 4:** Run `go run ./cmd/import-crd/ --help` and confirm every
  flag has a description (per the self-documenting-binary convention).
- [ ] **Step 5 (manual, not automated):** Against a local/throwaway
  Postgres (e.g. the `docker-compose.dev.yml` dev database — never
  production), run
  `go run ./cmd/import-crd/ --file "Blocking Full List_1.xlsx" --db-url "<local dsn>"`
  (dry-run stays on by default) and review the printed summary against
  the EDA doc's known counts (1,840 distinct references, 38,156 rows, 26
  unnormalizable URLs, etc. — the dry-run numbers should be in the same
  ballpark; a wildly different count means the header-mapping assumption
  from Task 1 drifted from the real file and needs fixing before this
  ships). Do not pass `--dry-run=false` as part of this plan's execution —
  that's the sign-off-gated step called out in Global Constraints.
- [ ] **Step 6: Commit**
```bash
git add cmd/import-crd/main.go
git commit -m "cmd/import-crd: CLI for the CRD blocklist import"
```

## Verification for this plan as a whole

```bash
go build ./...
go test ./internal/blockimport/...
```
Both must pass. Step 5 above (the manual dry-run against real data) is
the actual acceptance check for this plan — a clean build alone doesn't
confirm the column mapping survived contact with the real file.
