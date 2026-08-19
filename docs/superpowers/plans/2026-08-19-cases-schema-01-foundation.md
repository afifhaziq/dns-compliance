# Cases Schema Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `cases`/`case_letters`/`case_urls` tables to the live
schema, remove `urls.reference_number` and `urls.requesting_dept_id` (both
replaced by data that now lives on `cases`), and migrate any existing data
losslessly. This is the foundation every other task in this migration
builds on — it must merge before any of them start real work.

**Architecture:** Three new GORM models mirroring the shape already
designed in `docs/db-schema-proposed.dbml`. A `db.Connect`-time migration
(same idiom as the existing `department_urls` column drop and
`due_date_presets.hours→minutes` rename in `internal/db/db.go`) backfills
any populated `urls.reference_number`/`requesting_dept_id` values into the
new tables, then drops both columns. No HTTP or store-interface changes —
those are Tasks 02/03.

**Tech Stack:** Go, GORM (`gorm.io/gorm`), PostgreSQL (production) /
glebarez/sqlite (tests).

**Spec:** `docs/db-schema-proposed.dbml` (see its header comment for the
finalized design decisions this plan implements — the `requesting_dept_id`
removal and `case_letters.type` default are both resolved there, dated
2026-08-19).

## Global Constraints

- `urls.url` stays the unique, pre-normalized bare hostname — nothing here
  touches that column.
- Every new table's `Ref:` / FK follows the existing `OnDelete` conventions
  in `docs/db-schema-proposed.dbml` exactly: `case_letters.case_id` and
  `case_urls.case_id`/`case_urls.url_id` cascade; `case_letters.oic_user_id`
  sets null.
- No AutoMigrate-only column drops — GORM's `AutoMigrate` never removes a
  column, so `reference_number`/`requesting_dept_id` must be dropped
  explicitly via `Migrator().DropColumn`, after backfilling, exactly like
  the existing `department_urls` six-column drop in `internal/db/db.go`.
- This migration is **not reversible** once the columns are dropped — the
  backfill step must run and be verified in the same `Connect` call, never
  as a separate manual step.

---

### Task 1: Add Case/CaseLetter/CaseURL models

**Files:**
- Modify: `internal/db/models.go`

**Interfaces (produced, relied on by every later task in this migration):**
```go
type Case struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	DepartmentID uint       `gorm:"not null;index" json:"department_id"`
	Department   Department `gorm:"foreignKey:DepartmentID" json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
}

type CaseLetter struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	CaseID          uint       `gorm:"not null;index" json:"case_id"`
	Type            string     `gorm:"not null" json:"type"` // Memo | Notice | Memo (Uplift) | Notice (Uplift)
	ReferenceNumber string     `json:"reference_number,omitempty"`
	WorkflowStatus  string     `json:"workflow_status,omitempty"` // CMOD-only: Draft | Pending Legal | Pending TSC | Submitted
	LetterDate      *time.Time `json:"letter_date,omitempty"`
	SubmittedAt     *time.Time `json:"submitted_at,omitempty"`
	Subject         string     `json:"subject,omitempty"`
	OICUserID       *uint      `gorm:"index" json:"oic_user_id,omitempty"`
	OICUser         *User      `gorm:"foreignKey:OICUserID;constraint:OnDelete:SET NULL" json:"oic_user,omitempty"`
	Requestor       string     `json:"requestor,omitempty"`
	Remarks         string     `json:"remarks,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type CaseURL struct {
	CaseID uint   `gorm:"primaryKey;autoIncrement:false" json:"case_id"`
	URLID  uint   `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
	URL    URL    `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	Phase  string `gorm:"not null" json:"phase"` // requested | uplift | suspended
}
```

Place these directly after the existing `URLOffence` struct (end of the
legal-citation-catalog block, around `internal/db/models.go:598`), with a
doc comment on `Case` summarizing the three-table shape (copy the rationale
from `docs/db-schema-proposed.dbml`'s `cases`/`case_letters`/`case_urls`
table `Note:` blocks — don't re-derive it from scratch).

`CaseURL`'s `Ref: case_urls.case_id > cases.id [delete: cascade]` needs a
`Case` FK too, not just `URL` — add it to the struct:
```go
	Case Case `gorm:"foreignKey:CaseID;constraint:OnDelete:CASCADE" json:"-"`
```
(add this field to `CaseURL` above the `URL` field).

- [ ] **Step 1: Add the three structs to `internal/db/models.go`** per the
  interfaces above, with doc comments.
- [ ] **Step 2: `go build ./...`** — confirms the new structs compile and
  GORM tags are well-formed (no runtime check yet, AutoMigrate wiring is
  Task 2).
- [ ] **Step 3: Commit**
```bash
git add internal/db/models.go
git commit -m "db: add Case/CaseLetter/CaseURL models"
```

---

### Task 2: Remove URL.ReferenceNumber and URL.RequestingDeptID

**Files:**
- Modify: `internal/db/models.go`

**Interfaces (consumed by Task 02/03/04 — these fields no longer exist
after this task):**
- `db.URL` loses `ReferenceNumber string` and `RequestingDeptID *uint` /
  `RequestingDept *Department`.
- `db.URLCaseFields` loses `ReferenceNumber *string` and
  `RequestingDeptID **uint`.
- `db.URLEntry` loses `ReferenceNumber string`, `RequestingDeptID *uint`,
  `RequestingDeptName string`.

- [ ] **Step 1: Remove from `URL` struct** (`internal/db/models.go:42-46`)
  — delete the `ReferenceNumber`, `RequestingDeptID`, `RequestingDept`
  fields and their comments.
- [ ] **Step 2: Remove from `URLCaseFields`** (`internal/db/models.go:133-140`)
  — delete the `ReferenceNumber *string` and `RequestingDeptID **uint`
  fields; update the struct's doc comment (it currently calls out
  `AgencyID`/`RequestingDeptID` needing the double-pointer three-state
  contract — rewrite that sentence to only mention `AgencyID` now).
- [ ] **Step 3: Remove from `URLEntry`** (`internal/db/models.go:148-161`)
  — delete `ReferenceNumber`, `RequestingDeptID`, `RequestingDeptName`
  fields; update the struct's doc comment (currently says
  "AgencyID/RequestingDeptID are included too" — becomes just "AgencyID").
- [ ] **Step 4: `go build ./...`** — this will now show compile errors in
  `internal/db/postgres.go` and `internal/server/handlers.go` referencing
  the removed fields. That's expected and out of scope for this task —
  Tasks 02/03 (separate worktrees) fix those call sites. **Do not** edit
  `postgres.go` or `handlers.go` in this task/worktree.
- [ ] **Step 5: Commit**
```bash
git add internal/db/models.go
git commit -m "db: remove URL.ReferenceNumber and URL.RequestingDeptID, superseded by cases"
```

*(This plan's own build-verification step, below, builds only the `db`
package in isolation for this reason — see Task 4.)*

---

### Task 3: Migration — backfill then drop, wire AutoMigrate

**Files:**
- Modify: `internal/db/db.go`
- Test: `internal/db/migrate_test.go`

**Interfaces:**
- Consumes: `Case`, `CaseLetter`, `CaseURL` (Task 1); the now-still-present
  raw columns `urls.reference_number`/`urls.requesting_dept_id` (Task 2
  only removed them from the Go struct, the DB columns still exist on any
  already-migrated database until this task's `DropColumn` calls run).
- Produces: `db.Connect(dialector)` behavior — after this task, a database
  with populated `reference_number`/`requesting_dept_id` data gets it
  losslessly moved into `cases`/`case_letters`/`case_urls` before the old
  columns are dropped.

**Backfill policy** (apply per `urls` row where `reference_number != ''`
OR `requesting_dept_id IS NOT NULL`):
- Create one `Case{DepartmentID: <that row's requesting_dept_id>}` — only
  when `requesting_dept_id` is set. A row with a `reference_number` but no
  `requesting_dept_id` has no department to attribute the case to; log it
  (`log.Printf`) and skip creating a case for it — same "log and skip,
  non-fatal" philosophy `db.BackfillURLValues` already uses for imperfect
  backfills (see `internal/db/CLAUDE.md`'s "Domain normalization &
  watchlists" section for the precedent), not a hard migration failure.
- If a case was created: add one `CaseLetter{CaseID: <case>, Type:
  "Notice", ReferenceNumber: <the url's reference_number>}` — `"Notice"`
  per the schema doc's resolved default (a pre-existing single reference
  number is functionally the externally-citable one).
- Add one `CaseURL{CaseID: <case>, URLID: <url>, Phase: <that url's
  status, or "requested" if status is empty>}` — reuses `urls.status`
  (untouched by this migration) as the best-known phase at backfill time.
- This all runs inside one `database.Transaction(...)` per `Connect` call,
  batched the same way `db.BackfillURLValues` batches
  (`id IN (SELECT id ... LIMIT n)` looping until empty) if the affected-row
  count could realistically be large — check with a `COUNT(*)` first; if
  it is under a few thousand rows (check against your target deployment's
  actual `urls` table size before assuming this — the CRD/CMOD import
  tasks haven't run yet at this point in the sequence, so pre-migration
  `urls` data is only ever what departments entered by hand through the
  existing `PATCH /api/urls/{id}` field, not the Excel imports), a single
  unbatched pass inside the transaction is fine — this is a one-time,
  one-row-at-a-time-in-Go loop (not a giant `UPDATE`), so Postgres's
  `statement_timeout` risk that motivated `BackfillURLValues`'s batching
  doesn't apply the same way; still wrap in a transaction so a mid-loop
  error rolls back cleanly instead of leaving `cases` half-populated with
  the old columns still present.
- After every row is processed successfully, drop both columns via
  `Migrator().DropColumn(&URL{}, "reference_number")` and
  `Migrator().DropColumn(&URL{}, "requesting_dept_id")`, guarded by
  `HasColumn` first (idempotent — a no-op on a fresh DB or a DB that's
  already been migrated), exactly like the existing
  `department_urls` six-column drop block (`internal/db/db.go:30-41`).

- [ ] **Step 1: Write the failing test** in `internal/db/migrate_test.go`
  (follow the existing test style in that file — check its imports/setup
  helpers first, e.g. how it spins up a throwaway sqlite `*gorm.DB` for
  `Connect`):
```go
func TestConnect_BackfillsReferenceNumberIntoCases(t *testing.T) {
	// Arrange: open a DB, AutoMigrate the OLD shape by hand (URL still
	// has reference_number/requesting_dept_id columns — simulate a
	// pre-migration database), insert a Department + a URL row with
	// both fields populated and status="uplift".
	// Act: call db.Connect on that same DB.
	// Assert:
	//   - urls.reference_number and urls.requesting_dept_id columns no
	//     longer exist (Migrator().HasColumn == false)
	//   - exactly one Case row exists with the right DepartmentID
	//   - exactly one CaseLetter row exists, Type == "Notice",
	//     ReferenceNumber == the original value
	//   - exactly one CaseURL row exists, Phase == "uplift"
}

func TestConnect_SkipsReferenceNumberWithNoDepartment(t *testing.T) {
	// Arrange: a URL row with reference_number set but
	// requesting_dept_id NULL.
	// Act: db.Connect.
	// Assert: no Case/CaseLetter/CaseURL rows created for that URL, no
	// error returned, and the two columns are still dropped.
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/db/... -run TestConnect_Backfills -v`
  Expected: FAIL (columns still exist / backfill logic doesn't exist yet)
- [ ] **Step 3: Implement the backfill+drop in `internal/db/db.go`**,
  inserted after the existing `department_urls`/`due_date_presets` migration
  blocks (`internal/db/db.go:41-64`) and before the `AutoMigrate(...)` call
  (`internal/db/db.go:65`) — it must run **before** `AutoMigrate` for the
  same reason the `due_date_presets.hours` rename does: `AutoMigrate` adds
  `Case`/`CaseLetter`/`CaseURL` as new tables (fine, order-independent for
  those), but the `reference_number`/`requesting_dept_id` columns need to
  be read here while they still exist, before anything downstream assumes
  they're gone.
- [ ] **Step 4: Add `&Case{}, &CaseLetter{}, &CaseURL{}` to the
  `AutoMigrate(...)` call** (`internal/db/db.go:66-69`).
- [ ] **Step 5: Run the tests to verify they pass**
  Run: `go test ./internal/db/... -run TestConnect_Backfills -v`
  Expected: PASS
- [ ] **Step 6: Run the full existing `internal/db` test suite** to confirm
  nothing else broke:
  Run: `go test ./internal/db/...`
  Expected: PASS (any pre-existing failures unrelated to this change are
  out of scope — note them, don't fix them here)
- [ ] **Step 7: Commit**
```bash
git add internal/db/db.go internal/db/migrate_test.go
git commit -m "db: migrate reference_number/requesting_dept_id into cases, drop columns"
```

---

### Task 4: Sync `docs/db-schema.dbml` and retire the proposed doc

**Files:**
- Modify: `docs/db-schema.dbml`
- Modify: `docs/db-schema-proposed.dbml`

**Steps:**
- [ ] **Step 1:** Apply the four structural changes from
  `docs/db-schema-proposed.dbml`'s header comment to `docs/db-schema.dbml`
  directly: remove `urls.reference_number` and `urls.requesting_dept_id`
  (and their `Ref:` line for the latter), add the `cases`/`case_letters`/
  `case_urls` tables verbatim (copy the finalized table definitions —
  including the RESOLVED notes — from the proposed file).
- [ ] **Step 2:** At the top of `docs/db-schema-proposed.dbml`, replace the
  file header with a short note: `// SHIPPED — see docs/db-schema.dbml.
  Kept for history; do not edit further.` Leave the rest of the file
  content as-is (it's now a historical record of the design discussion).
- [ ] **Step 3: `go build ./...`** one more time across the whole module
  (not just `internal/db`) — this is the first point in this task where a
  full-module build is expected to pass again, since Tasks 2+3 together
  removed the fields and finished the migration that replaces them. This
  is a **verification-only** step: if it fails anywhere outside
  `internal/db`, that's Task 02/03's job (they haven't run yet in a fresh
  worktree sequencing), not something to fix here — but if you're running
  these four tasks in one combined worktree pass rather than strictly
  respecting the cross-plan boundary, this is where you'd notice
  `postgres.go`/`handlers.go` still reference the removed fields. Leave
  those failures for the Task 02/03 plans; don't fix them in this
  worktree.
- [ ] **Step 4: Commit**
```bash
git add docs/db-schema.dbml docs/db-schema-proposed.dbml
git commit -m "docs: sync db-schema.dbml with cases/case_letters/case_urls, retire proposed doc"
```

## Verification for this plan as a whole

This plan's `db` package must build and its tests must pass in isolation,
even though the wider module won't build until Task 02 lands (that's
expected and fine — Task 02's worktree branches from this one merged, not
from a green full-module build):

```bash
go build ./internal/db/...
go test ./internal/db/...
```
Both must pass before this task is considered done and ready to merge.
