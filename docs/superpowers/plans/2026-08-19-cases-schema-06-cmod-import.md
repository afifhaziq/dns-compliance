# CMOD Blocklist Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A one-off, re-runnable CLI (`cmd/import-cmod/`) that imports the
`BLK` sheet of `Masterlist Blocking CMOD.xlsx` (repo-root, gitignored)
into `cases`/`case_letters`/`case_urls`, per the mapping decided in
`docs/cmod-blocking-list-migration-clarifications.md`.

**Prerequisite:** `2026-08-19-cases-schema-01-foundation.md` must already
be merged to `main`. Like `2026-08-19-cases-schema-05-crd-import.md`, this
plan does **not** depend on Task 02/03/04 (store/API/frontend) — it's a
standalone CLI writing straight to the database via `internal/db.Connect`.
It is also **independent of Task 05** (the CRD importer) — see "Why CRD
and CMOD import independently" below. Do not share code with
`internal/blockimport/crd.go` beyond what both naturally already get from
`internal/db`/`internal/urlnorm` — two ~150-line parsers is not enough
callers to justify a forced-shared abstraction between them (see this
repo's ponytail-mode conventions: ponytail-marked or not, ponytail's rule
against premature abstraction still applies).

**Architecture:** New fourth binary, `cmd/import-cmod/main.go`. Same
`excelize`-based read approach as the CRD importer (Task 05 already added
the dependency to `go.mod` — if that task hasn't merged yet when this one
starts, add it here instead; don't block on it). Groups the sheet's 499
correspondence rows (one row per **letter**, not per blocking event) by
base reference number into one `Case` per group, with **up to 4**
`CaseLetter` rows per case (Memo/Notice/Memo (Uplift)/Notice (Uplift)) —
this is the key structural difference from CRD's importer, which always
produces exactly one letter per case.

**Tech Stack:** Go, `github.com/xuri/excelize/v2`, GORM (via
`internal/db`).

**Spec:** `docs/cmod-blocking-list-migration-clarifications.md` and
`docs/db-schema.dbml`'s `cases`/`case_letters`/`case_urls` tables.

## Why CRD and CMOD import independently

`cases.department_id` scopes ownership per department by design (see the
schema doc's `cases` table note). 95% of CMOD's 242 normalizable domains
already appear somewhere in CRD's 34,312-domain history (per the EDA
doc's §7), and CMOD's Notice ref matches CRD's `No. Rujukan NMD` 77% of
the time where they overlap — but that overlap is a **reconciliation**
concern, not an import-time one: each department's own paperwork becomes
its own `Case` row, same as if two departments manually opened separate
cases against the same live domain today. This plan does not attempt to
detect or merge against Task 05's CRD-imported cases. A cross-department
"these two cases probably describe the same real event" view is listed as
explicit follow-up work in the orchestration doc
(`2026-08-19-cases-schema-00-orchestration.md`), not part of either
importer.

## Global Constraints

- Same sign-off gate as Task 05: **do not run a real (`--dry-run=false`)
  import against production** from this plan — the status-mapping and
  category-taxonomy questions in
  `docs/cmod-blocking-list-migration-clarifications.md` are still open
  per `CLAUDE.md`'s TODO entry. Build/test against a local database only;
  treat a reviewed dry-run as this plan's deliverable.
- Same out-of-scope carve-out as Task 05: **no `Category`/`Citation`/
  `URLOffence` rows are created.** `CMOD Offence` values are captured for
  dry-run visibility only (`ImportSummary.CategoriesObserved`), for the
  same reason Task 05 excludes them — see that plan's Global Constraints
  for the full rationale, unchanged here.
- Every row's `Link` value must pass `internal/urlnorm.Normalize` after
  splitting newline-joined multi-URL cells (see Task 1) — a row that
  still fails after splitting is skipped and counted, never silently
  dropped.
- Idempotent, same rule as Task 05: re-running against the same file and
  database must not duplicate `Case` rows. Key the existence check on
  `CaseLetter.ReferenceNumber` + `Type` within department CMOD's cases
  (CMOD's per-letter refs, e.g. the Notice ref, are the stable identity —
  see Task 1).

---

### Task 1: Column mapping + row-collapsing (pure function, no I/O)

**Files:**
- Create: `internal/blockimport/cmod.go`
- Test: `internal/blockimport/cmod_test.go`

**Before writing this task's code:** open the actual
`Masterlist Blocking CMOD.xlsx` (repo root), confirm the `BLK` sheet's
header row (row 2 per the EDA doc — row 1 may be a title/merged-cell row,
verify this against the live file rather than assuming) and exact column
headers: `No, Letter Date, Recipient, Type, Subject, Reference No, OIC,
Requestor, Offence, Link (One Link Per Row), Remarks, Agency, Status,
Received, Submission`. Resolve columns by header name (same reasoning as
Task 05's Task 1 — survives reordering), and only read the `BLK` sheet —
`FAKE NEWS`/`EVENT`/`xCRR` are confirmed out of scope by the EDA doc.

**Interfaces (produced, relied on by Task 2):**
```go
package blockimport

// CMODRow is one raw BLK-sheet row after column-name resolution.
type CMODRow struct {
	LetterDate      string // raw cell text -- mixed formats per the EDA doc (plain strings and real Excel datetimes); parse to *time.Time in Task 2, not here, so this stays a pure text-mapping layer
	Recipient       string
	Type            string // "Memo" | "Notice" | "Memo (Uplift)" | "Notice (Uplift)"
	Subject         string
	ReferenceNumber string // includes the -1/-2/-3/-4 suffix at this stage
	OIC             string
	Requestor       string
	Offence         string
	Links           []string // already split from the raw "Link (One Link Per Row)" cell -- see splitLinks below
	Remarks         string
	Agency          string
	Status          string // CMOD's workflow status: Draft | Pending Legal | Pending TSC | Submitted
	Received        string
	Submission      string
}

// ParseCMODRows reads every BLK-sheet data row from path.
func ParseCMODRows(path string) ([]CMODRow, error) { /* ... */ }

// splitLinks splits a "Link (One Link Per Row)" cell on newlines and
// trims each -- handles the 2 rows in the EDA doc that violate the
// sheet's own one-link-per-row rule (one 2-URL cell, one 13-URL cell).
// A normal single-URL cell returns a length-1 slice.
func splitLinks(raw string) []string { /* ... */ }

// baseReferenceNumber strips CMOD's -1/-2/-3/-4 suffix (Memo/Notice/
// Memo (Uplift)/Notice (Uplift)) to recover the real case identifier --
// e.g. "MCMC(S)CMOD/BLK/2026(19-1)" -> "MCMC(S)CMOD/BLK/2026(19)". Per
// the EDA doc, the suffix always lines up with Type, so this is a plain
// string trim on the known pattern, not inference from Type.
func baseReferenceNumber(ref string) string { /* ... */ }

// CollapsedCMODCase is one base-reference-number group -- the unit that
// becomes one Case with (up to 4) CaseLetter rows.
type CollapsedCMODCase struct {
	BaseReferenceNumber string
	Letters             []CMODRow // 1-4 rows, one per Type present for this base ref
	URLs                []string  // union of every Links entry across all of this case's rows, deduplicated
	Offence             string    // from whichever row has it set (should agree across the group -- log if it doesn't, don't silently pick one)
}

// CollapseCMODRows groups rows by baseReferenceNumber. Pure in-memory
// transform, no I/O -- mirrors CollapseCRDRows in crd.go.
func CollapseCMODRows(rows []CMODRow) []CollapsedCMODCase { /* ... */ }
```

**Decisions this task's implementation must apply** (resolved by the EDA
doc's own findings, not new open questions):
- The 1 malformed `Reference No` cell that crams 3 case numbers into one
  value (`"MCMC(S)CMOD/BLK/2026(19-1, 20-1, 21-1)"`) — split on `,`,
  treat as 3 separate rows each with one of the 3 refs, same domain(s)
  attached to all 3 (the EDA doc flags this as likely a data-entry error
  needing correction, and splitting is the doc's own suggested fix — do
  this in `ParseCMODRows`, before collapsing).
- The 4 rows using the longer `MCMC(S)NSS/CPMD/CMOD/BLK/2026(NN-N)` prefix
  — treat as the same series as the standard
  `MCMC(S)CMOD/BLK/2026(NN-N)` prefix (per the EDA doc, presumably just
  inconsistent typing) — `baseReferenceNumber` should strip the suffix
  regardless of which prefix variant precedes it, not assume a fixed
  prefix string.
- The 2 `Offence` compound rows (`"Palsu, Jelik Melampau"`) — same
  handling as CRD's compound categories: not written to any table per
  this plan's Global Constraints, but `splitOffence` (small helper, same
  shape as `strings.Split` + trim) should still exist so
  `ImportSummary.CategoriesObserved` (Task 2) counts each split value
  separately rather than the raw compound string as one bucket.

- [ ] **Step 1: Write the failing tests** in `internal/blockimport/cmod_test.go`
  (reuse `writeTestXLSX` from `crd_test.go` if that task already merged —
  it's in the same package; if not yet merged, define a local copy here
  and let the eventual merge dedupe it, don't block on merge order):
```go
func TestParseCMODRows_ResolvesColumnsByHeaderName(t *testing.T) {
	path := writeTestXLSX(t, "BLK", [][]string{
		{"title row placeholder"}, // row 1, per the EDA doc's "header on row 2"
		{"No", "Letter Date", "Recipient", "Type", "Subject", "Reference No", "OIC", "Requestor", "Offence", "Link (One Link Per Row)", "Remarks", "Agency", "Status", "Received", "Submission"},
		{"1", "1-Jan-2026", "ISP X", "Notice", "Blocking", "MCMC(S)CMOD/BLK/2026(1-2)", "Atiqah", "Someone", "Judi dalam talian", "example.com", "", "PDRM", "Submitted", "", ""},
	})
	rows, err := ParseCMODRows(path)
	if err != nil {
		t.Fatalf("ParseCMODRows: %v", err)
	}
	if len(rows) != 1 || rows[0].ReferenceNumber != "MCMC(S)CMOD/BLK/2026(1-2)" || rows[0].Type != "Notice" {
		t.Fatalf("got %+v", rows)
	}
}

func TestSplitLinks_HandlesNewlineJoinedCell(t *testing.T) {
	got := splitLinks("edisisiasat4.wordpress.com\nedisisiasat5.wordpress.com")
	want := []string{"edisisiasat4.wordpress.com", "edisisiasat5.wordpress.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBaseReferenceNumber_StripsSuffix(t *testing.T) {
	cases := map[string]string{
		"MCMC(S)CMOD/BLK/2026(19-1)":              "MCMC(S)CMOD/BLK/2026(19)",
		"MCMC(S)CMOD/BLK/2026(19-2)":              "MCMC(S)CMOD/BLK/2026(19)",
		"MCMC(S)NSS/CPMD/CMOD/BLK/2026(64-1)":     "MCMC(S)NSS/CPMD/CMOD/BLK/2026(64)",
	}
	for in, want := range cases {
		if got := baseReferenceNumber(in); got != want {
			t.Errorf("baseReferenceNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollapseCMODRows_GroupsMemoAndNoticeUnderOneCase(t *testing.T) {
	rows := []CMODRow{
		{ReferenceNumber: "REF(1-1)", Type: "Memo", Links: []string{"a.com"}},
		{ReferenceNumber: "REF(1-2)", Type: "Notice", Links: []string{"a.com"}},
	}
	cases := CollapseCMODRows(rows)
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(cases))
	}
	if len(cases[0].Letters) != 2 {
		t.Fatalf("got %d letters, want 2 (Memo + Notice)", len(cases[0].Letters))
	}
	if len(cases[0].URLs) != 1 {
		t.Fatalf("got %d URLs, want 1 deduplicated URL", len(cases[0].URLs))
	}
}

func TestCollapseCMODRows_SplitsThreeCaseNumbersInOneCell(t *testing.T) {
	// input row with ReferenceNumber "MCMC(S)CMOD/BLK/2026(19-1, 20-1, 21-1)"
	// (post-ParseCMODRows split into 3 rows, per the parsing rule above) --
	// assert 3 distinct CollapsedCMODCase results, each with its own base ref.
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/blockimport/... -run 'TestParseCMODRows|TestSplitLinks|TestBaseReferenceNumber|TestCollapseCMODRows' -v`
  Expected: FAIL
- [ ] **Step 3: Implement `ParseCMODRows`/`splitLinks`/`baseReferenceNumber`/
  `CollapseCMODRows`/`CMODRow`/`CollapsedCMODCase`** in
  `internal/blockimport/cmod.go`, per the rules above.
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/blockimport/... -v`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/blockimport/cmod.go internal/blockimport/cmod_test.go
git commit -m "blockimport: parse and collapse CMOD BLK-sheet rows"
```

---

### Task 2: Write collapsed CMOD cases to the database

**Files:**
- Create: `internal/blockimport/write_cmod.go`
- Test: `internal/blockimport/write_cmod_test.go`

**Interfaces:**
```go
// WriteCMODCases creates one Case (DepartmentID = the CMOD department's
// ID) per CollapsedCMODCase, one CaseLetter per Letters entry (Type/
// ReferenceNumber/WorkflowStatus/LetterDate/SubmittedAt/Subject/
// Requestor/Remarks mapped directly from the CMODRow; OICUserID left nil
// -- see the OIC caveat below), and one CaseURL per URL in URLs (Phase:
// "uplift" if any letter's Type contains "Uplift", else "requested" --
// CMOD's Status column is a workflow-approval state, not a case phase,
// see mapCMODPhase below). dryRun mirrors WriteCRDCases' rollback-always
// transaction approach exactly (internal/blockimport/write.go).
func WriteCMODCases(ctx context.Context, db *gorm.DB, cmodDeptID uint, cases []CollapsedCMODCase, dryRun bool) (ImportSummary, error) { /* ... */ }

// mapCMODPhase derives case_urls.phase from a case's letters -- "uplift"
// if any letter Type contains "Uplift" (Memo (Uplift) or Notice
// (Uplift)), else "requested". CMOD's own Status column (Draft/Pending
// Legal/Pending TSC/Submitted) is NOT this function's input -- it's
// letter-approval-workflow state, stored as-is on CaseLetter.WorkflowStatus,
// a different axis entirely (see the EDA doc's §1).
func mapCMODPhase(letters []CMODRow) string { /* ... */ }
```

**OIC caveat (do not silently guess):** `docs/cmod-blocking-list-migration-clarifications.md`
explicitly flags `oic_user_id` as "NOT YET VERIFIED that OIC names in the
source sheet (Atiqah, Arishah, ...) actually match real `users.username`
values." This task does **not** attempt a name-matching heuristic —
`CaseLetter.OICUserID` stays `nil` for every imported row, and the raw
`OIC` text is preserved by folding it into `Remarks` (e.g. prefix the
existing remarks with `"OIC (unmatched): <name> — "` if non-empty) so the
information isn't lost, just not linked. Leave a `ponytail:` comment on
`WriteCMODCases` at the point `OICUserID` is set to nil, naming this
caveat and citing the EDA doc, so it's easy to find once someone verifies
the name-matching.

**`ReferenceNumber` field on `CaseLetter`** for a CMOD-imported letter is
the row's own full reference (including suffix) — e.g. `Type: "Memo",
ReferenceNumber: "MCMC(S)CMOD/BLK/2026(1-1)"` — not the collapsed base
reference (that's `Case`'s grouping key, not a stored column; if a
"case-level reference" display is ever needed, it's derived the same way
`docs/db-schema.dbml`'s `case_urls` note already describes: most-recent
`case_letters` row by `letter_date`, joined through `case_urls` — nothing
new to build here, `ListCasesForURL`/`ListDepartmentURLs` from
`2026-08-19-cases-schema-02-store-layer.md` already do this once both
plans have merged).

- [ ] **Step 1: Write the failing tests** in
  `internal/blockimport/write_cmod_test.go` (same `newTestGormDB`/
  `mustSeedDepartment` helpers as `write_test.go` — reuse, don't
  redefine, they're in the same package):
```go
func TestWriteCMODCases_CreatesFourLettersForFullLifecycle(t *testing.T) {
	gdb := newTestGormDB(t)
	cmod := mustSeedDepartment(t, gdb, "CMOD")
	cases := []CollapsedCMODCase{{
		BaseReferenceNumber: "REF(1)",
		Letters: []CMODRow{
			{ReferenceNumber: "REF(1-1)", Type: "Memo"},
			{ReferenceNumber: "REF(1-2)", Type: "Notice"},
			{ReferenceNumber: "REF(1-3)", Type: "Memo (Uplift)"},
			{ReferenceNumber: "REF(1-4)", Type: "Notice (Uplift)"},
		},
		URLs: []string{"example.com"},
	}}
	summary, err := WriteCMODCases(context.Background(), gdb, cmod.ID, cases, false)
	if err != nil {
		t.Fatalf("WriteCMODCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1", summary.CasesCreated)
	}
	var letterCount int64
	gdb.Model(&db.CaseLetter{}).Count(&letterCount)
	if letterCount != 4 {
		t.Fatalf("case_letters count = %d, want 4", letterCount)
	}
	var cu db.CaseURL
	if err := gdb.First(&cu).Error; err != nil {
		t.Fatalf("CaseURL: %v", err)
	}
	if cu.Phase != "uplift" {
		t.Errorf("Phase = %q, want uplift (case has an Uplift letter)", cu.Phase)
	}
}

func TestWriteCMODCases_PreservesUnmatchedOICInRemarks(t *testing.T) {
	// a CMODRow with OIC: "Atiqah", Remarks: "" -- assert the resulting
	// CaseLetter has OICUserID == nil and Remarks containing "Atiqah".
}

func TestWriteCMODCases_IsIdempotent(t *testing.T) {
	// mirror TestWriteCRDCases_IsIdempotent from write_test.go.
}

func TestMapCMODPhase(t *testing.T) {
	onlyBlock := []CMODRow{{Type: "Memo"}, {Type: "Notice"}}
	withUplift := []CMODRow{{Type: "Memo"}, {Type: "Notice"}, {Type: "Notice (Uplift)"}}
	if got := mapCMODPhase(onlyBlock); got != "requested" {
		t.Errorf("mapCMODPhase(onlyBlock) = %q, want requested", got)
	}
	if got := mapCMODPhase(withUplift); got != "uplift" {
		t.Errorf("mapCMODPhase(withUplift) = %q, want uplift", got)
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/blockimport/... -run 'TestWriteCMODCases|TestMapCMODPhase' -v`
  Expected: FAIL
- [ ] **Step 3: Implement `WriteCMODCases`/`mapCMODPhase`** in
  `internal/blockimport/write_cmod.go`, reusing the same get-or-create
  URL helper `write.go` (Task 05) already established — if Task 05 hasn't
  merged yet, write a local equivalent here and let the merge dedupe it
  later; don't block this task on Task 05's merge order (they're
  independent per this plan's header).
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/blockimport/... -v`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/blockimport/write_cmod.go internal/blockimport/write_cmod_test.go
git commit -m "blockimport: write CMOD cases to the database, idempotent, dry-run capable"
```

---

### Task 3: `cmd/import-cmod/main.go` CLI

**Files:**
- Create: `cmd/import-cmod/main.go`

Same shape as `cmd/import-crd/main.go`
(`2026-08-19-cases-schema-05-crd-import.md`'s Task 3) — `--file`,
`--db-url` (required), `--dry-run` (default `true`). Looks up the `CMOD`
department by name instead of `CRD`. Copy that CLI's structure exactly
(flag parsing, `db.Connect`, department lookup, summary printing);
substitute `blockimport.ParseCMODRows`/`CollapseCMODRows`/`WriteCMODCases`
for the CRD equivalents.

- [ ] **Step 1:** Implement `cmd/import-cmod/main.go`.
- [ ] **Step 2: `go build ./cmd/import-cmod/`**
  Expected: builds clean.
- [ ] **Step 3: `go build ./...`** — confirm nothing else broke.
- [ ] **Step 4:** `go run ./cmd/import-cmod/ --help` — confirm every flag
  is documented.
- [ ] **Step 5 (manual, not automated):** Against a local/throwaway
  database only (never production — see Global Constraints), run
  `go run ./cmd/import-cmod/ --file "Masterlist Blocking CMOD.xlsx" --db-url "<local dsn>"`
  and review the dry-run summary against the EDA doc's known counts (499
  rows → 65 distinct base cases → 254 distinct case/URL pairs → 244
  distinct URLs). A materially different count means the column/suffix
  assumptions in Task 1 need revisiting against the live file. Do not
  pass `--dry-run=false` as part of this plan's execution.
- [ ] **Step 6: Commit**
```bash
git add cmd/import-cmod/main.go
git commit -m "cmd/import-cmod: CLI for the CMOD blocklist import"
```

## Verification for this plan as a whole

```bash
go build ./...
go test ./internal/blockimport/...
```
Both must pass. Step 5 above (manual dry-run against the real file) is
the actual acceptance check, same reasoning as Task 05's equivalent step.
