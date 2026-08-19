# Cases Store Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `db.Store` CRUD + query methods over `Case`/`CaseLetter`/
`CaseURL`, and fix `ListDepartmentURLs`/`UpdateURLCaseFields` (which still
reference the now-removed `urls.reference_number`/`requesting_dept_id`
columns after `2026-08-19-cases-schema-01-foundation.md` merged).

**Prerequisite:** Task 01 (`2026-08-19-cases-schema-01-foundation.md`) must
already be merged to `main` — this plan assumes `Case`/`CaseLetter`/
`CaseURL` exist in `internal/db/models.go` and that
`URL.ReferenceNumber`/`RequestingDeptID` and their `URLCaseFields`/
`URLEntry` counterparts are already gone. Start this worktree from `main`
*after* that merge, not before.

**Architecture:** One new `CaseStore` sub-interface (`internal/db/store.go`)
implemented by `postgresStore` (`internal/db/postgres.go`), following this
codebase's existing "narrow sub-interface per aggregate" convention (see
`internal/db/CLAUDE.md`'s `db.Store` bullet — `URLStore`, `AgencyStore`,
etc. are the precedent). The two broken call sites get a single-query fix
using a correlated subquery, matching the existing portable-across-Postgres-
and-SQLite pattern `resurfacedDomains`/`SLAActiveURLs` already use (not a
Postgres-only `LATERAL` join, and not an N+1 loop).

**Tech Stack:** Go, GORM, PostgreSQL/SQLite (tests).

**Spec:** `docs/db-schema-proposed.dbml` (now merged into
`docs/db-schema.dbml`) — the `cases`/`case_letters`/`case_urls` table
definitions and their resolved notes.

## Global Constraints

- Every new store method takes `context.Context` first, matching every
  existing method in `internal/db/postgres.go`.
- `CaseStore` methods that create/mutate must go on the narrowest
  sub-interface that makes sense — don't add case methods to the giant
  `URLStore` interface.
- No N+1 queries against `cases`/`case_letters` from the watchlist grid
  path (`ListDepartmentURLs`) — it's called on every `/urls` page load
  and must stay a single query, same constraint that already shaped
  `idx_scan_results_url_server_time`.

---

### Task 1: `CaseStore` interface + `CreateCase`/`AddCaseLetter`/`SetCaseURL`

**Files:**
- Modify: `internal/db/store.go`
- Modify: `internal/db/postgres.go`
- Test: `internal/db/postgres_test.go` (or a new `internal/db/cases_test.go`
  if `postgres_test.go` is already large — check its current line count
  first; this codebase's convention is one togo test file per aggregate
  once a file gets unwieldy, e.g. `legalcite.go`/`legalcite_test.go` are
  already split out from `postgres.go`/`postgres_test.go` this way, so
  prefer creating `internal/db/cases.go`/`internal/db/cases_test.go` for
  this task's implementation rather than growing `postgres.go` further)

**Interfaces (produced, relied on by Task 03):**
```go
// CaseStore is the cases/case_letters/case_urls aggregate.
type CaseStore interface {
	// CreateCase creates a Case for departmentID and links it to urlID
	// with the given phase (requested | uplift | suspended) via CaseURL.
	// Returns the created Case (zero letters — AddCaseLetter is separate).
	CreateCase(ctx context.Context, departmentID, urlID uint, phase string) (Case, error)
	// AddCaseLetter appends one CaseLetter row to an existing case.
	AddCaseLetter(ctx context.Context, letter CaseLetter) (CaseLetter, error)
	// ListCasesForURL returns every case covering urlValue, each with its
	// letters (newest LetterDate first) and this url's own Phase from
	// case_urls — ownership scoping happens at the handler layer
	// (requireDomainOwnership), same split as ListOffencesByURL/
	// AttachOffenceToURL already use.
	ListCasesForURL(ctx context.Context, urlValue string) ([]CaseWithLetters, error)
}

// CaseWithLetters is Case plus its Letters and this url's Phase — the read
// shape ListCasesForURL returns. Not a persisted table.
type CaseWithLetters struct {
	Case
	Phase   string       `json:"phase"`
	Letters []CaseLetter `json:"letters"`
}
```
Add `CaseStore` to `db.Store`'s embedded interface list in
`internal/db/store.go` alongside the existing fourteen (`URLStore`,
`DNSServerStore`, ..., `LegalCitationStore`) — find that embed list first
(`grep -n "type Store interface" internal/db/store.go`) and follow its
exact formatting.

- [ ] **Step 1: Write the failing test** (in the new `internal/db/cases_test.go`
  — check `internal/db/legalcite_test.go` for this codebase's test-setup
  helper, likely something like `newTestStore(t)` or `setupTestDB(t)`; use
  whatever `postgres_test.go`/`legalcite_test.go` already use rather than
  inventing a new helper):
```go
func TestCreateCase_LinksURLWithPhase(t *testing.T) {
	store := newTestStore(t) // reuse existing test helper
	ctx := context.Background()
	dept := mustCreateDepartment(t, store, "CRD")
	u := mustCreateURL(t, store, "example.com")

	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested")
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if c.DepartmentID != dept.ID {
		t.Errorf("DepartmentID = %d, want %d", c.DepartmentID, dept.ID)
	}

	cases, err := store.ListCasesForURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListCasesForURL: %v", err)
	}
	if len(cases) != 1 || cases[0].Phase != "requested" {
		t.Fatalf("got %+v, want one case with phase=requested", cases)
	}
}

func TestAddCaseLetter_AppearsInListCasesForURL(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept := mustCreateDepartment(t, store, "CMOD")
	u := mustCreateURL(t, store, "example2.com")
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested")
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}

	letter, err := store.AddCaseLetter(ctx, db.CaseLetter{
		CaseID: c.ID,
		Type:   "Memo",
	})
	if err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}
	if letter.ID == 0 {
		t.Fatal("expected a generated ID")
	}

	cases, err := store.ListCasesForURL(ctx, "example2.com")
	if err != nil {
		t.Fatalf("ListCasesForURL: %v", err)
	}
	if len(cases) != 1 || len(cases[0].Letters) != 1 || cases[0].Letters[0].Type != "Memo" {
		t.Fatalf("got %+v, want one case with one Memo letter", cases)
	}
}
```
(Adjust the helper function names/signatures to whatever
`mustCreateDepartment`/`mustCreateURL`-equivalent helpers already exist in
this test file's neighbors — grep `postgres_test.go`/`legalcite_test.go`
for `func must` or similar before inventing new ones.)

- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/db/... -run 'TestCreateCase_LinksURLWithPhase|TestAddCaseLetter_AppearsInListCasesForURL' -v`
  Expected: FAIL (methods don't exist — compile error)
- [ ] **Step 3: Implement `CreateCase`/`AddCaseLetter`/`ListCasesForURL`**
  in `internal/db/cases.go` (new file):
```go
func (s *postgresStore) CreateCase(ctx context.Context, departmentID, urlID uint, phase string) (Case, error) {
	c := Case{DepartmentID: departmentID}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		return tx.Create(&CaseURL{CaseID: c.ID, URLID: urlID, Phase: phase}).Error
	})
	return c, err
}

func (s *postgresStore) AddCaseLetter(ctx context.Context, letter CaseLetter) (CaseLetter, error) {
	err := s.db.WithContext(ctx).Create(&letter).Error
	return letter, err
}

func (s *postgresStore) ListCasesForURL(ctx context.Context, urlValue string) ([]CaseWithLetters, error) {
	u, err := s.GetURLByValue(ctx, urlValue)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, gorm.ErrRecordNotFound
	}

	var caseURLs []CaseURL
	if err := s.db.WithContext(ctx).Where("url_id = ?", u.ID).Find(&caseURLs).Error; err != nil {
		return nil, err
	}
	phaseByCaseID := make(map[uint]string, len(caseURLs))
	caseIDs := make([]uint, 0, len(caseURLs))
	for _, cu := range caseURLs {
		phaseByCaseID[cu.CaseID] = cu.Phase
		caseIDs = append(caseIDs, cu.CaseID)
	}
	if len(caseIDs) == 0 {
		return []CaseWithLetters{}, nil
	}

	var cases []Case
	if err := s.db.WithContext(ctx).Where("id IN ?", caseIDs).Find(&cases).Error; err != nil {
		return nil, err
	}
	var letters []CaseLetter
	if err := s.db.WithContext(ctx).Where("case_id IN ?", caseIDs).
		Order("letter_date desc").Find(&letters).Error; err != nil {
		return nil, err
	}
	lettersByCaseID := make(map[uint][]CaseLetter, len(cases))
	for _, l := range letters {
		lettersByCaseID[l.CaseID] = append(lettersByCaseID[l.CaseID], l)
	}

	result := make([]CaseWithLetters, 0, len(cases))
	for _, c := range cases {
		result = append(result, CaseWithLetters{
			Case:    c,
			Phase:   phaseByCaseID[c.ID],
			Letters: lettersByCaseID[c.ID],
		})
	}
	return result, nil
}
```
- [ ] **Step 4: Add `CaseStore` to the `Store` embed in `internal/db/store.go`**.
- [ ] **Step 5: Run the tests to verify they pass**
  Run: `go test ./internal/db/... -run 'TestCreateCase_LinksURLWithPhase|TestAddCaseLetter_AppearsInListCasesForURL' -v`
  Expected: PASS
- [ ] **Step 6: Add `CaseStore` (or a purpose-built fake) to
  `internal/server/handlers_test.go`'s `fullMockStore`** so the server
  package still compiles after Task 03 adds handlers that take
  `db.CaseStore` or `db.Store` — check whether this task's worktree even
  builds `internal/server` (it may not need to yet, since Task 03 hasn't
  landed); if `go build ./...` at the repo root fails here purely because
  `fullMockStore` doesn't implement the three new interface methods,
  add no-op/fake implementations now so downstream doesn't inherit a
  broken mock — one property test worth adding directly:
```go
func (m *fullMockStore) CreateCase(_ context.Context, departmentID, urlID uint, phase string) (db.Case, error) {
	return db.Case{ID: 1, DepartmentID: departmentID}, nil
}
func (m *fullMockStore) AddCaseLetter(_ context.Context, letter db.CaseLetter) (db.CaseLetter, error) {
	letter.ID = 1
	return letter, nil
}
func (m *fullMockStore) ListCasesForURL(_ context.Context, urlValue string) ([]db.CaseWithLetters, error) {
	return nil, nil
}
```
- [ ] **Step 7: Commit**
```bash
git add internal/db/store.go internal/db/cases.go internal/db/cases_test.go internal/server/handlers_test.go
git commit -m "db: add CaseStore (CreateCase/AddCaseLetter/ListCasesForURL)"
```

---

### Task 2: Fix `ListDepartmentURLs` and `UpdateURLCaseFields`

**Files:**
- Modify: `internal/db/postgres.go`

**Interfaces:**
- `URLEntry` (already trimmed by Task 01) gains two new derived,
  read-only fields — add these now:
```go
	CurrentReferenceNumber string   `json:"current_reference_number,omitempty"`
	RequestingDepartments  []string `json:"requesting_departments,omitempty"`
```
  (Add to `internal/db/models.go`'s `URLEntry` struct — this is a small,
  additive change to a struct Task 01 already touched; safe to make here
  since Task 01 is already merged and this doesn't conflict with anything
  else in flight.)

**`ListDepartmentURLs` fix** (`internal/db/postgres.go:554-568`) — the
`Select(...)` currently references the dropped `urls.reference_number` and
joins `departments` via the dropped `urls.requesting_dept_id`
(`internal/db/postgres.go:560,564`). Replace with two correlated
subqueries — the same portability-first pattern (works on both Postgres
and the SQLite test driver, no `LATERAL`/window functions) already used by
`resurfacedDomains`/`SLAActiveURLs`:

```go
func (s *postgresStore) ListDepartmentURLs(ctx context.Context, departmentID uint) ([]URLEntry, error) {
	var entries []URLEntry
	err := s.db.WithContext(ctx).
		Table("urls").
		Select(`urls.id, urls.url, urls.created_at, du.enabled,
			urls.due_date, urls.agency_id, agencies.name as agency_name,
			urls.status, urls.requested_at,
			(SELECT cl.reference_number FROM case_letters cl
			 JOIN cases c ON c.id = cl.case_id
			 JOIN case_urls cu ON cu.case_id = c.id
			 WHERE cu.url_id = urls.id
			 ORDER BY cl.letter_date DESC LIMIT 1) AS current_reference_number`).
		Joins("JOIN department_urls du ON du.url_id = urls.id AND du.department_id = ?", departmentID).
		Joins("LEFT JOIN agencies ON agencies.id = urls.agency_id").
		Order("urls.created_at asc").
		Scan(&entries).Error
	if err != nil {
		return nil, err
	}

	// RequestingDepartments can't be a scalar subquery (it's genuinely
	// multi-valued) — fetch separately and merge in Go rather than a
	// database-specific array_agg, keeping this portable across Postgres
	// and the SQLite test driver.
	if len(entries) > 0 {
		urlIDs := make([]uint, len(entries))
		idxByURLID := make(map[uint]int, len(entries))
		for i, e := range entries {
			urlIDs[i] = e.ID
			idxByURLID[e.ID] = i
		}
		type deptRow struct {
			URLID uint
			Name  string
		}
		var rows []deptRow
		if err := s.db.WithContext(ctx).
			Table("case_urls").
			Select("DISTINCT case_urls.url_id as url_id, departments.name as name").
			Joins("JOIN cases ON cases.id = case_urls.case_id").
			Joins("JOIN departments ON departments.id = cases.department_id").
			Where("case_urls.url_id IN ?", urlIDs).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			i := idxByURLID[r.URLID]
			entries[i].RequestingDepartments = append(entries[i].RequestingDepartments, r.Name)
		}
	}
	return entries, nil
}
```

**`UpdateURLCaseFields` fix** (`internal/db/postgres.go:602-638`) — delete
the two `if fields.ReferenceNumber != nil` / `if fields.RequestingDeptID
!= nil` blocks (`internal/db/postgres.go:626-631`); everything else in
that function (`DueDate`/`AgencyID`/`Status`/`RequestedAt`) is unchanged.

- [ ] **Step 1: Write the failing test** (in `internal/db/postgres_test.go`,
  find the existing `TestListDepartmentURLs`-style test to match its setup
  style):
```go
func TestListDepartmentURLs_DerivesCurrentReferenceAndRequestingDepartments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	crd := mustCreateDepartment(t, store, "CRD")
	cmod := mustCreateDepartment(t, store, "CMOD")
	u := mustCreateURL(t, store, "example.com")
	mustWatch(t, store, crd.ID, u.ID) // however this test file already adds a URL to a watchlist

	crdCase, err := store.CreateCase(ctx, crd.ID, u.ID, "requested")
	if err != nil {
		t.Fatalf("CreateCase (crd): %v", err)
	}
	if _, err := store.AddCaseLetter(ctx, db.CaseLetter{
		CaseID: crdCase.ID, Type: "Notice", ReferenceNumber: "REF-1",
		LetterDate: ptrTime(time.Now().Add(-time.Hour)),
	}); err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}
	if _, err := store.CreateCase(ctx, cmod.ID, u.ID, "requested"); err != nil {
		t.Fatalf("CreateCase (cmod): %v", err)
	}

	entries, err := store.ListDepartmentURLs(ctx, crd.ID)
	if err != nil {
		t.Fatalf("ListDepartmentURLs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].CurrentReferenceNumber != "REF-1" {
		t.Errorf("CurrentReferenceNumber = %q, want REF-1", entries[0].CurrentReferenceNumber)
	}
	got := append([]string{}, entries[0].RequestingDepartments...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"CMOD", "CRD"}) {
		t.Errorf("RequestingDepartments = %v, want [CMOD CRD]", got)
	}
}
```
- [ ] **Step 2: Run the test to verify it fails**
  Run: `go test ./internal/db/... -run TestListDepartmentURLs_DerivesCurrentReferenceAndRequestingDepartments -v`
  Expected: FAIL (compile error — `reference_number` column doesn't exist
  under the old query; `CurrentReferenceNumber`/`RequestingDepartments`
  don't exist on `URLEntry` yet)
- [ ] **Step 3: Add the two fields to `URLEntry`** in
  `internal/db/models.go`, per the Interfaces section above.
- [ ] **Step 4: Replace `ListDepartmentURLs`'s body** with the version
  above.
- [ ] **Step 5: Delete the two dead blocks in `UpdateURLCaseFields`**.
- [ ] **Step 6: Run the test to verify it passes**
  Run: `go test ./internal/db/... -run TestListDepartmentURLs_DerivesCurrentReferenceAndRequestingDepartments -v`
  Expected: PASS
- [ ] **Step 7: Run the full `internal/db` suite**
  Run: `go test ./internal/db/...`
  Expected: PASS
- [ ] **Step 8: Commit**
```bash
git add internal/db/postgres.go internal/db/models.go internal/db/postgres_test.go
git commit -m "db: derive current_reference_number/requesting_departments in ListDepartmentURLs"
```

## Verification for this plan as a whole

```bash
go build ./internal/db/... ./internal/server/...
go test ./internal/db/...
```
`internal/server` should build (Task 01's `fullMockStore` update from Task
1/Step 6 above keeps it compiling) even though no handler calls the new
`CaseStore` methods yet — that wiring is Task 03.
