# urls.tsx Cases View + Case Edit Form Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Cases view" to `/urls` that groups domains under their case (one expandable row per case, subrows per domain), plus a case edit form reusing `AddUrlDialog` in add/edit-dual-purpose mode.

**Architecture:** New read-only `db.CaseSummary` aggregate (`GET /api/case-summaries`) joins each case to its Notice/Memo letters and covered domains, mirroring `ListDepartmentURLs`'s "derive display fields via correlated subquery" pattern. A new `PATCH /api/cases/{id}/letters/{letter_id}` lets the edit form update an existing `CaseLetter` in place. Frontend: a `?view=domains|cases` search param drives a `ToggleGroup`; Cases view is a real tree grid (`getSubRows`/`getExpandedRowModel`, `CaseRow`/`DomainSubRow` discriminated union) mirroring `results.index.tsx`'s `DomainRow`/`ServerRow`; `AddUrlDialog` gains an `editing: CaseSummary | null` prop mirroring `legal-citations.tsx`'s `InstrumentFormDialog` add/edit pattern.

**Tech Stack:** Go 1.26 + GORM + chi (backend), React 19 + TypeScript + TanStack Router/Table (frontend). No frontend test framework exists in this repo — frontend tasks verify via `npm run build` (runs `tsc --noEmit`) plus a manual `dev.sh` click-through, not automated tests.

**Spec:** `docs/superpowers/specs/2026-08-21-urls-cases-view-design.md`

## Global Constraints

- Follow existing conventions exactly: department-ownership checks are 404-not-403 (`GetCase` → `DepartmentID` match), same pattern as `AddCaseLetter`/`UpdateCase` in `internal/server/case_handlers.go`.
- `CaseSummary`/`CaseSummaryDomain` are computed read structs (`gorm:"-"` on the slice field), not persisted tables — same as `CaseLetterEntry`.
- Every new list-returning frontend fetcher must guard `Array.isArray(data) ? data : []` (Go nil slice → JSON `null`, see `web/CLAUDE.md`).
- Reuse `STATUS_OPTIONS`/`CASE_PHASE_OPTIONS` (`@/lib/case-options`'s `PHASE_OPTIONS`) already defined/imported in `urls.tsx` — do not redefine them.
- Do not touch `docs.tsx`, `CaseHistoryDialog`, or the offence-editing `EditUrlDialog`/`FileText` icon already wired in Domain view's action column — out of scope, unrelated concern (offences, not case fields).

---

## Task 1: `CaseSummary`/`CaseSummaryDomain`/`CaseLetterFields` structs

**Files:**
- Modify: `internal/db/models.go` (insert after the `CaseLetterEntry` struct, ~line 702, before `BuildProvisionSortKey`)

**Interfaces:**
- Produces: `db.CaseSummary`, `db.CaseSummaryDomain`, `db.CaseLetterFields` — used by Task 2 (store interface), Task 3 (implementation), Task 5/6 (handlers).

- [ ] **Step 1: Add the structs**

```go
// CaseSummary is one row for the Cases view (GET /api/case-summaries): a
// Case's own fields plus its Notice letter's fields (Notice chosen over
// Memo when both exist, same convention as current_reference_number, see
// ListDepartmentURLs) and every domain it covers. Not a persisted table.
type CaseSummary struct {
	ID                             uint                `json:"id"`
	AgencyID                       *uint               `json:"agency_id,omitempty"`
	AgencyName                     string              `json:"agency_name,omitempty"`
	Status                         string              `json:"status,omitempty"`
	DueDate                        *time.Time          `json:"due_date,omitempty"`
	RequestedAt                    *time.Time          `json:"requested_at,omitempty"`
	CreatedAt                      time.Time           `json:"created_at"`
	NoticeLetterID                 *uint               `json:"notice_letter_id,omitempty"`
	NoticeSubject                  string              `json:"notice_subject,omitempty"`
	NoticeWorkflowStatus           string              `json:"notice_workflow_status,omitempty"`
	NoticeReferenceNumberExternal  string              `json:"notice_reference_number_external,omitempty"`
	NoticeReferenceNumberInternal  string              `json:"notice_reference_number_internal,omitempty"`
	NoticeRecipient                string              `json:"notice_recipient,omitempty"`
	NoticeRequestor                string              `json:"notice_requestor,omitempty"`
	NoticeLetterDate               *time.Time          `json:"notice_letter_date,omitempty"`
	NoticeReceivedAt               *time.Time          `json:"notice_received_at,omitempty"`
	NoticeSubmittedAt              *time.Time          `json:"notice_submitted_at,omitempty"`
	NoticeRemarks                  string              `json:"notice_remarks,omitempty"`
	MemoLetterID                   *uint               `json:"memo_letter_id,omitempty"`
	MemoSubject                    string              `json:"memo_subject,omitempty"`
	MemoReferenceNumberInternal    string              `json:"memo_reference_number_internal,omitempty"`
	Domains                        []CaseSummaryDomain `gorm:"-" json:"domains"`
}

// CaseSummaryDomain is one domain a CaseSummary covers, via CaseURL.
type CaseSummaryDomain struct {
	URLID uint   `json:"url_id"`
	URL   string `json:"url"`
	Phase string `json:"phase"`
}

// CaseLetterFields is a partial update to a CaseLetter's fields (PATCH
// /api/cases/{id}/letters/{letter_id}). String fields use the same
// present-but-empty-clears convention as CaseFields.Status (nil = don't
// touch, non-nil "" = clear, non-nil non-"" = set); the three date fields
// have no natural empty sentinel so they use CaseFields.DueDate's
// double-pointer convention instead (outer nil = don't touch, outer
// non-nil -> nil inner = clear, outer non-nil -> &v = set).
type CaseLetterFields struct {
	Subject                 *string
	WorkflowStatus          *string
	ReferenceNumberExternal *string
	ReferenceNumberInternal *string
	Recipient               *string
	Requestor               *string
	Remarks                 *string
	LetterDate              **time.Time
	ReceivedAt              **time.Time
	SubmittedAt             **time.Time
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/db/...`
Expected: success (no test yet — pure type addition).

- [ ] **Step 3: Commit**

```bash
git add internal/db/models.go
git commit -m "db: add CaseSummary/CaseSummaryDomain/CaseLetterFields types"
```

---

## Task 2: `ListCases`/`ListCasesForDepartment` — interface, implementation, DB test

**Files:**
- Modify: `internal/db/store.go` (`CaseStore` interface, after `ListCaseLettersForDepartment`)
- Modify: `internal/db/cases.go` (implementation)
- Test: `internal/db/cases_test.go`

**Interfaces:**
- Consumes: `db.CaseSummary{}`, `db.CaseSummaryDomain{}` (Task 1); `s.db *gorm.DB` (postgresStore field).
- Produces: `Store.ListCases(ctx) ([]CaseSummary, error)`, `Store.ListCasesForDepartment(ctx, departmentID) ([]CaseSummary, error)` — consumed by Task 5's handler.

- [ ] **Step 1: Write the failing test**

Add to `internal/db/cases_test.go`:

```go
// TestListCasesForDepartment_PicksNoticeOverMemoAndListsDomains covers the
// Cases view's core aggregate: a case with both a Notice and a Memo letter
// surfaces the Notice's fields (not the Memo's), and every domain the case
// covers (via CaseURL, including one added later via AddURLToCase) appears
// in Domains.
func TestListCasesForDepartment_PicksNoticeOverMemoAndListsDomains(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, err := store.CreateDepartment(ctx, "SummaryDept")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	agency, err := store.CreateAgency(ctx, "MCMC")
	if err != nil {
		t.Fatalf("CreateAgency: %v", err)
	}
	u1, err := store.CreateURL(ctx, "summary-a.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	u2, err := store.CreateURL(ctx, "summary-b.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	c, err := store.CreateCase(ctx, dept.ID, u1.ID, "requested", db.CaseCreateOptions{AgencyID: &agency.ID})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddURLToCase(ctx, c.ID, u2.ID, "requested"); err != nil {
		t.Fatalf("AddURLToCase: %v", err)
	}

	earlier := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Memo", Subject: "memo-subject", LetterDate: &earlier}); err != nil {
		t.Fatalf("AddCaseLetter(Memo): %v", err)
	}
	if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Notice", Subject: "notice-subject", LetterDate: &later}); err != nil {
		t.Fatalf("AddCaseLetter(Notice): %v", err)
	}

	summaries, err := store.ListCasesForDepartment(ctx, dept.ID)
	if err != nil {
		t.Fatalf("ListCasesForDepartment: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("want 1 summary, got %d", len(summaries))
	}
	s := summaries[0]
	if s.NoticeSubject != "notice-subject" {
		t.Fatalf("NoticeSubject = %q, want notice-subject", s.NoticeSubject)
	}
	if s.MemoSubject != "memo-subject" {
		t.Fatalf("MemoSubject = %q, want memo-subject", s.MemoSubject)
	}
	if s.AgencyName != "MCMC" {
		t.Fatalf("AgencyName = %q, want MCMC", s.AgencyName)
	}
	if len(s.Domains) != 2 {
		t.Fatalf("want 2 domains, got %+v", s.Domains)
	}
	gotURLs := map[string]bool{}
	for _, d := range s.Domains {
		gotURLs[d.URL] = true
	}
	if !gotURLs["summary-a.com"] || !gotURLs["summary-b.com"] {
		t.Fatalf("expected both domains, got %+v", s.Domains)
	}
}

// TestListCases_GlobalAcrossDepartments covers the admin/global variant —
// same split as ListCaseLetters/ListCaseLettersForDepartment.
func TestListCases_GlobalAcrossDepartments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	deptA, _ := store.CreateDepartment(ctx, "GlobalDeptA")
	deptB, _ := store.CreateDepartment(ctx, "GlobalDeptB")
	uA, _ := store.CreateURL(ctx, "global-a.com")
	uB, _ := store.CreateURL(ctx, "global-b.com")
	if _, err := store.CreateCase(ctx, deptA.ID, uA.ID, "requested", db.CaseCreateOptions{}); err != nil {
		t.Fatalf("CreateCase A: %v", err)
	}
	if _, err := store.CreateCase(ctx, deptB.ID, uB.ID, "requested", db.CaseCreateOptions{}); err != nil {
		t.Fatalf("CreateCase B: %v", err)
	}

	all, err := store.ListCases(ctx)
	if err != nil {
		t.Fatalf("ListCases: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 summaries globally, got %d", len(all))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/... -run 'TestListCasesForDepartment_PicksNoticeOverMemoAndListsDomains|TestListCases_GlobalAcrossDepartments' -v`
Expected: FAIL — `store.ListCasesForDepartment`/`store.ListCases` undefined (not yet on the interface).

- [ ] **Step 3: Add to the `CaseStore` interface**

In `internal/db/store.go`, inside the `CaseStore` interface, after `ListCaseLettersForDepartment`'s line:

```go
	// ListCases/ListCasesForDepartment back the Cases view (GET
	// /api/case-summaries) — one row per case with its own fields, its
	// Notice letter's fields, and every domain it covers. Same admin-global
	// vs department-scoped split as ListCaseLetters/ForDepartment.
	ListCases(ctx context.Context) ([]CaseSummary, error)
	ListCasesForDepartment(ctx context.Context, departmentID uint) ([]CaseSummary, error)
```

- [ ] **Step 4: Implement in `internal/db/cases.go`**

Append to the file:

```go
// caseSummaryQuery is the shared Select/Joins for the Cases view's per-case
// row, optionally scoped to one department — used by both ListCases and
// ListCasesForDepartment (mirrors caseLetterQuery's department-scoping
// pattern above, and postgresStore.ListDepartmentURLs's latest-Notice/
// latest-Memo correlated-subquery pattern in postgres.go).
func (s *postgresStore) caseSummaryQuery(ctx context.Context, departmentID *uint) *gorm.DB {
	q := s.db.WithContext(ctx).
		Table("cases").
		Select(`cases.id, cases.agency_id, agencies.name as agency_name,
			cases.status, cases.due_date, cases.requested_at, cases.created_at,
			notice.id as notice_letter_id, notice.subject as notice_subject,
			notice.workflow_status as notice_workflow_status,
			notice.reference_number_external as notice_reference_number_external,
			notice.reference_number_internal as notice_reference_number_internal,
			notice.recipient as notice_recipient, notice.requestor as notice_requestor,
			notice.letter_date as notice_letter_date, notice.received_at as notice_received_at,
			notice.submitted_at as notice_submitted_at, notice.remarks as notice_remarks,
			memo.id as memo_letter_id, memo.subject as memo_subject,
			memo.reference_number_internal as memo_reference_number_internal`).
		Joins("LEFT JOIN agencies ON agencies.id = cases.agency_id").
		Joins(`LEFT JOIN case_letters notice ON notice.id = (
			SELECT cl.id FROM case_letters cl
			WHERE cl.case_id = cases.id AND cl.type IN ('Notice', 'Notice (Uplift)')
			ORDER BY cl.letter_date DESC LIMIT 1)`).
		Joins(`LEFT JOIN case_letters memo ON memo.id = (
			SELECT cl.id FROM case_letters cl
			WHERE cl.case_id = cases.id AND cl.type IN ('Memo', 'Memo (Uplift)')
			ORDER BY cl.letter_date DESC LIMIT 1)`)
	if departmentID != nil {
		q = q.Where("cases.department_id = ?", *departmentID)
	}
	return q
}

func (s *postgresStore) listCaseSummaries(ctx context.Context, departmentID *uint) ([]CaseSummary, error) {
	var summaries []CaseSummary
	if err := s.caseSummaryQuery(ctx, departmentID).Order("cases.created_at desc").Scan(&summaries).Error; err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return summaries, nil
	}

	caseIDs := make([]uint, len(summaries))
	idxByCaseID := make(map[uint]int, len(summaries))
	for i, c := range summaries {
		caseIDs[i] = c.ID
		idxByCaseID[c.ID] = i
	}
	type domainRow struct {
		CaseID uint
		URLID  uint
		URL    string
		Phase  string
	}
	var rows []domainRow
	if err := s.db.WithContext(ctx).
		Table("case_urls").
		Select("case_urls.case_id as case_id, case_urls.url_id as url_id, urls.url as url, case_urls.phase as phase").
		Joins("JOIN urls ON urls.id = case_urls.url_id").
		Where("case_urls.case_id IN ?", caseIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		i := idxByCaseID[r.CaseID]
		summaries[i].Domains = append(summaries[i].Domains, CaseSummaryDomain{URLID: r.URLID, URL: r.URL, Phase: r.Phase})
	}
	return summaries, nil
}

func (s *postgresStore) ListCases(ctx context.Context) ([]CaseSummary, error) {
	return s.listCaseSummaries(ctx, nil)
}

func (s *postgresStore) ListCasesForDepartment(ctx context.Context, departmentID uint) ([]CaseSummary, error) {
	return s.listCaseSummaries(ctx, &departmentID)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/db/... -run 'TestListCasesForDepartment_PicksNoticeOverMemoAndListsDomains|TestListCases_GlobalAcrossDepartments' -v`
Expected: PASS. (`go build ./...` will still fail — `fullMockStore` in `internal/server` doesn't yet implement the two new interface methods; that's fixed in Task 5.)

- [ ] **Step 6: Commit**

```bash
git add internal/db/store.go internal/db/cases.go internal/db/cases_test.go
git commit -m "db: add ListCases/ListCasesForDepartment for the Cases view"
```

---

## Task 3: `UpdateCaseLetterFields` — interface, implementation, DB test

**Files:**
- Modify: `internal/db/store.go` (`CaseStore` interface)
- Modify: `internal/db/cases.go`
- Test: `internal/db/cases_test.go`

**Interfaces:**
- Consumes: `db.CaseLetterFields` (Task 1).
- Produces: `Store.UpdateCaseLetterFields(ctx, caseID, letterID, fields) (bool, error)` — consumed by Task 6's handler.

- [ ] **Step 1: Write the failing test**

Add to `internal/db/cases_test.go`:

```go
// TestUpdateCaseLetterFields_PartialUpdateAndScoping covers the partial-
// update contract (touching one field leaves the others alone) and that
// the update is scoped by (caseID, letterID) — a letter can't be edited
// through the wrong case id, same defense-in-depth UpdateCaseURLPhase uses.
func TestUpdateCaseLetterFields_PartialUpdateAndScoping(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, _ := store.CreateDepartment(ctx, "LetterFieldsDept")
	u, _ := store.CreateURL(ctx, "letter-fields.com")
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	letter, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Notice", Subject: "orig-subject", WorkflowStatus: "Draft"})
	if err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}

	newSubject := "updated-subject"
	found, err := store.UpdateCaseLetterFields(ctx, c.ID, letter.ID, db.CaseLetterFields{Subject: &newSubject})
	if err != nil || !found {
		t.Fatalf("UpdateCaseLetterFields: found=%v err=%v", found, err)
	}

	cases, err := store.ListCasesForURL(ctx, "letter-fields.com")
	if err != nil {
		t.Fatalf("ListCasesForURL: %v", err)
	}
	if len(cases) != 1 || len(cases[0].Letters) != 1 {
		t.Fatalf("got %+v", cases)
	}
	got := cases[0].Letters[0]
	if got.Subject != "updated-subject" {
		t.Fatalf("Subject = %q, want updated-subject", got.Subject)
	}
	if got.WorkflowStatus != "Draft" {
		t.Fatalf("WorkflowStatus = %q, want unchanged Draft", got.WorkflowStatus)
	}

	// Wrong case id — same letter id, different (wrong) case: no row matches.
	found, err = store.UpdateCaseLetterFields(ctx, c.ID+999, letter.ID, db.CaseLetterFields{Subject: &newSubject})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false when case id doesn't match the letter's own case")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/... -run TestUpdateCaseLetterFields_PartialUpdateAndScoping -v`
Expected: FAIL — `store.UpdateCaseLetterFields` undefined.

- [ ] **Step 3: Add to the `CaseStore` interface**

In `internal/db/store.go`, right after the `ListCasesForDepartment` line added in Task 2:

```go
	// UpdateCaseLetterFields applies a partial update to one CaseLetter's
	// fields, scoped by (caseID, letterID) so a letter can't be edited
	// through a case it doesn't belong to — same defense-in-depth scoping
	// UpdateCaseURLPhase uses. Ownership (departmentID owns caseID) is
	// checked by the caller (handler layer), same as UpdateCaseFields.
	// False if no such (case, letter) pair exists.
	UpdateCaseLetterFields(ctx context.Context, caseID, letterID uint, fields CaseLetterFields) (bool, error)
```

- [ ] **Step 4: Implement in `internal/db/cases.go`**

```go
// UpdateCaseLetterFields applies a partial update to one CaseLetter's
// fields, scoped by (caseID, letterID) — see CaseStore's doc comment.
func (s *postgresStore) UpdateCaseLetterFields(ctx context.Context, caseID, letterID uint, fields CaseLetterFields) (bool, error) {
	updates := map[string]interface{}{}
	if fields.Subject != nil {
		updates["subject"] = *fields.Subject
	}
	if fields.WorkflowStatus != nil {
		updates["workflow_status"] = *fields.WorkflowStatus
	}
	if fields.ReferenceNumberExternal != nil {
		updates["reference_number_external"] = *fields.ReferenceNumberExternal
	}
	if fields.ReferenceNumberInternal != nil {
		updates["reference_number_internal"] = *fields.ReferenceNumberInternal
	}
	if fields.Recipient != nil {
		updates["recipient"] = *fields.Recipient
	}
	if fields.Requestor != nil {
		updates["requestor"] = *fields.Requestor
	}
	if fields.Remarks != nil {
		updates["remarks"] = *fields.Remarks
	}
	if fields.LetterDate != nil {
		updates["letter_date"] = *fields.LetterDate
	}
	if fields.ReceivedAt != nil {
		updates["received_at"] = *fields.ReceivedAt
	}
	if fields.SubmittedAt != nil {
		updates["submitted_at"] = *fields.SubmittedAt
	}
	if len(updates) == 0 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&CaseLetter{}).Where("id = ? AND case_id = ?", letterID, caseID).Count(&count).Error; err != nil {
			return false, err
		}
		return count > 0, nil
	}
	res := s.db.WithContext(ctx).Model(&CaseLetter{}).Where("id = ? AND case_id = ?", letterID, caseID).Updates(updates)
	return res.RowsAffected > 0, res.Error
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/db/... -run TestUpdateCaseLetterFields_PartialUpdateAndScoping -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/db/store.go internal/db/cases.go internal/db/cases_test.go
git commit -m "db: add UpdateCaseLetterFields, scoped by (case, letter)"
```

---

## Task 4: Wire the new `CaseStore` methods into `fullMockStore`

Doing this as its own task (before the handler tasks) restores `go build ./...`/`go test ./...` to a fully green state, since `internal/server/handlers_test.go`'s `var _ db.Store = (*fullMockStore)(nil)` currently fails to compile after Tasks 2–3.

**Files:**
- Modify: `internal/server/handlers_test.go` (append near the other `Case*` mock methods, ~line 1494, before `var _ db.Store = (*fullMockStore)(nil)`)

**Interfaces:**
- Consumes: `m.cases []db.Case`, `m.caseURLs []db.CaseURL`, `m.caseLetters []db.CaseLetter`, `m.urls []db.URL` (existing `fullMockStore` fields).
- Produces: mock `ListCases`/`ListCasesForDepartment`/`UpdateCaseLetterFields` — consumed by Task 5/6's handler tests.

- [ ] **Step 1: Add the mock implementations**

```go
func (m *fullMockStore) listCaseSummaries(departmentID *uint) []db.CaseSummary {
	var out []db.CaseSummary
	for _, c := range m.cases {
		if departmentID != nil && c.DepartmentID != *departmentID {
			continue
		}
		cs := db.CaseSummary{ID: c.ID, AgencyID: c.AgencyID, Status: c.Status, DueDate: c.DueDate, RequestedAt: c.RequestedAt, CreatedAt: c.CreatedAt}
		for _, l := range m.caseLetters {
			if l.CaseID != c.ID {
				continue
			}
			id := l.ID
			switch l.Type {
			case "Notice", "Notice (Uplift)":
				cs.NoticeLetterID = &id
				cs.NoticeSubject = l.Subject
				cs.NoticeWorkflowStatus = l.WorkflowStatus
				cs.NoticeReferenceNumberExternal = l.ReferenceNumberExternal
				cs.NoticeReferenceNumberInternal = l.ReferenceNumberInternal
			case "Memo", "Memo (Uplift)":
				cs.MemoLetterID = &id
				cs.MemoSubject = l.Subject
				cs.MemoReferenceNumberInternal = l.ReferenceNumberInternal
			}
		}
		for _, cu := range m.caseURLs {
			if cu.CaseID != c.ID {
				continue
			}
			for _, u := range m.urls {
				if u.ID == cu.URLID {
					cs.Domains = append(cs.Domains, db.CaseSummaryDomain{URLID: u.ID, URL: u.URL, Phase: cu.Phase})
				}
			}
		}
		out = append(out, cs)
	}
	return out
}

func (m *fullMockStore) ListCases(_ context.Context) ([]db.CaseSummary, error) {
	return m.listCaseSummaries(nil), nil
}

func (m *fullMockStore) ListCasesForDepartment(_ context.Context, departmentID uint) ([]db.CaseSummary, error) {
	return m.listCaseSummaries(&departmentID), nil
}

func (m *fullMockStore) UpdateCaseLetterFields(_ context.Context, caseID, letterID uint, fields db.CaseLetterFields) (bool, error) {
	for i, l := range m.caseLetters {
		if l.ID != letterID || l.CaseID != caseID {
			continue
		}
		if fields.Subject != nil {
			m.caseLetters[i].Subject = *fields.Subject
		}
		if fields.WorkflowStatus != nil {
			m.caseLetters[i].WorkflowStatus = *fields.WorkflowStatus
		}
		if fields.ReferenceNumberExternal != nil {
			m.caseLetters[i].ReferenceNumberExternal = *fields.ReferenceNumberExternal
		}
		if fields.ReferenceNumberInternal != nil {
			m.caseLetters[i].ReferenceNumberInternal = *fields.ReferenceNumberInternal
		}
		if fields.Recipient != nil {
			m.caseLetters[i].Recipient = *fields.Recipient
		}
		if fields.Requestor != nil {
			m.caseLetters[i].Requestor = *fields.Requestor
		}
		if fields.Remarks != nil {
			m.caseLetters[i].Remarks = *fields.Remarks
		}
		if fields.LetterDate != nil {
			m.caseLetters[i].LetterDate = *fields.LetterDate
		}
		if fields.ReceivedAt != nil {
			m.caseLetters[i].ReceivedAt = *fields.ReceivedAt
		}
		if fields.SubmittedAt != nil {
			m.caseLetters[i].SubmittedAt = *fields.SubmittedAt
		}
		return true, nil
	}
	return false, nil
}
```

- [ ] **Step 2: Verify the whole repo builds and tests pass again**

Run: `go build ./... && go test ./...`
Expected: PASS (network-dependent `internal/dns` tests aside, per CLAUDE.md).

- [ ] **Step 3: Commit**

```bash
git add internal/server/handlers_test.go
git commit -m "test: implement new CaseStore methods on fullMockStore"
```

---

## Task 5: `GET /api/case-summaries` handler, route, handler test

**Files:**
- Modify: `internal/server/case_handlers.go`
- Modify: `internal/server/router.go` (inside the existing `/cases`/`/case-letters` block, ~line 138)
- Test: `internal/server/case_handlers_test.go`

**Interfaces:**
- Consumes: `h.store.ListCases`/`ListCasesForDepartment` (Task 2); `userFromContext`, `writeJSON`, `writeInternalError`, `writeError` (existing `handlers.go` helpers).
- Produces: `Handlers.ListCaseSummaries` — routed at `GET /api/case-summaries`, consumed by Task 8's `fetchCaseSummaries`.

- [ ] **Step 1: Write the failing test**

Add to `internal/server/case_handlers_test.go`:

```go
func TestListCaseSummaries_NonAdminScopedToOwnDepartment(t *testing.T) {
	store := &fullMockStore{}
	uA := db.URL{ID: 1, URL: "summaries-a.com"}
	uB := db.URL{ID: 2, URL: "summaries-b.com"}
	store.urls = append(store.urls, uA, uB)
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1, Status: "requested"},
		db.Case{ID: 2, DepartmentID: 2, Status: "requested"},
	)
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: uA.ID, Phase: "requested"},
		db.CaseURL{CaseID: 2, URLID: uB.ID, Phase: "requested"},
	)
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var summaries []db.CaseSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != 1 {
		t.Fatalf("expected only dept 1's case, got %+v", summaries)
	}
}

func TestListCaseSummaries_AdminSeesGlobal(t *testing.T) {
	store := &fullMockStore{}
	uA := db.URL{ID: 1, URL: "summaries-a.com"}
	uB := db.URL{ID: 2, URL: "summaries-b.com"}
	store.urls = append(store.urls, uA, uB)
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1, Status: "requested"},
		db.Case{ID: 2, DepartmentID: 2, Status: "requested"},
	)
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: uA.ID, Phase: "requested"},
		db.CaseURL{CaseID: 2, URLID: uB.ID, Phase: "requested"},
	)
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var summaries []db.CaseSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected both cases for admin, got %+v", summaries)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run 'TestListCaseSummaries_NonAdminScopedToOwnDepartment|TestListCaseSummaries_AdminSeesGlobal' -v`
Expected: FAIL — 404 (no such route yet).

- [ ] **Step 3: Add the handler**

Append to `internal/server/case_handlers.go`:

```go
// ListCaseSummaries is the Cases view's data source (GET
// /api/case-summaries) — one row per case with its own fields, its Notice
// letter's fields, and every domain it covers. Same admin-global/non-admin-
// department-scoped split as ListCaseLetters.
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

- [ ] **Step 4: Wire the route**

In `internal/server/router.go`, right after the `r.Get("/case-letters", h.ListCaseLetters)` line (~138):

```go
			// Cases view's data source — see ListCaseSummaries.
			r.Get("/case-summaries", h.ListCaseSummaries)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/server/... -run 'TestListCaseSummaries_NonAdminScopedToOwnDepartment|TestListCaseSummaries_AdminSeesGlobal' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/case_handlers.go internal/server/router.go internal/server/case_handlers_test.go
git commit -m "server: add GET /api/case-summaries for the Cases view"
```

---

## Task 6: `PATCH /api/cases/{id}/letters/{letter_id}` handler, route, handler test

**Files:**
- Modify: `internal/server/case_handlers.go`
- Modify: `internal/server/router.go` (same block as Task 5)
- Test: `internal/server/case_handlers_test.go`

**Interfaces:**
- Consumes: `h.store.GetCase`, `h.store.UpdateCaseLetterFields` (Task 3); `parseOptionalRFC3339` (`handlers.go:248`).
- Produces: `Handlers.UpdateCaseLetter` — routed at `PATCH /api/cases/{id}/letters/{letter_id}`, consumed by Task 8's `updateCaseLetter`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/server/case_handlers_test.go`:

```go
func TestUpdateCaseLetter_PartialUpdate(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "update-letter.com"}
	store.urls = append(store.urls, u)
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID, Phase: "requested"})
	store.caseLetters = append(store.caseLetters, db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice", Subject: "orig", WorkflowStatus: "Draft"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"subject": "updated"})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1/letters/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.caseLetters[0].Subject != "updated" {
		t.Fatalf("Subject = %q, want updated", store.caseLetters[0].Subject)
	}
	if store.caseLetters[0].WorkflowStatus != "Draft" {
		t.Fatalf("WorkflowStatus = %q, want unchanged Draft", store.caseLetters[0].WorkflowStatus)
	}
}

func TestUpdateCaseLetter_NonOwningDepartment404(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "update-letter-2.com"}
	store.urls = append(store.urls, u)
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID, Phase: "requested"})
	store.caseLetters = append(store.caseLetters, db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice"})
	cookie := deptCookie(store, 2)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"subject": "updated"})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1/letters/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run 'TestUpdateCaseLetter_PartialUpdate|TestUpdateCaseLetter_NonOwningDepartment404' -v`
Expected: FAIL — 404 (no such route yet).

- [ ] **Step 3: Add the handler**

Append to `internal/server/case_handlers.go`:

```go
// UpdateCaseLetter applies a partial update to one CaseLetter's fields —
// the Cases view's edit form writes Notice/Memo subject, workflow status,
// reference numbers, recipient/requestor, dates, and remarks through this.
// Ownership: same case-DepartmentID check as AddCaseLetter/UpdateCase; the
// store call further scopes by (case, letter) so a letter can't be edited
// through the wrong case id (see UpdateCaseLetterFields).
func (h *Handlers) UpdateCaseLetter(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	letterID, err := strconv.ParseUint(chi.URLParam(r, "letter_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid letter_id")
		return
	}

	c, err := h.store.GetCase(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeInternalError(w, err)
		return
	}
	if !user.IsAdmin && (user.DepartmentID == nil || *user.DepartmentID != c.DepartmentID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var body struct {
		Subject                 *string `json:"subject"`
		WorkflowStatus          *string `json:"workflow_status"`
		ReferenceNumberExternal *string `json:"reference_number_external"`
		ReferenceNumberInternal *string `json:"reference_number_internal"`
		Recipient               *string `json:"recipient"`
		Requestor               *string `json:"requestor"`
		Remarks                 *string `json:"remarks"`
		LetterDate              *string `json:"letter_date"`
		ReceivedAt              *string `json:"received_at"`
		SubmittedAt             *string `json:"submitted_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	fields := db.CaseLetterFields{
		Subject: body.Subject, WorkflowStatus: body.WorkflowStatus,
		ReferenceNumberExternal: body.ReferenceNumberExternal, ReferenceNumberInternal: body.ReferenceNumberInternal,
		Recipient: body.Recipient, Requestor: body.Requestor, Remarks: body.Remarks,
	}
	if body.LetterDate != nil {
		t, err := parseOptionalRFC3339(*body.LetterDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid letter_date, expected RFC3339")
			return
		}
		fields.LetterDate = &t
	}
	if body.ReceivedAt != nil {
		t, err := parseOptionalRFC3339(*body.ReceivedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid received_at, expected RFC3339")
			return
		}
		fields.ReceivedAt = &t
	}
	if body.SubmittedAt != nil {
		t, err := parseOptionalRFC3339(*body.SubmittedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid submitted_at, expected RFC3339")
			return
		}
		fields.SubmittedAt = &t
	}

	found, err := h.store.UpdateCaseLetterFields(r.Context(), uint(id), uint(letterID), fields)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Wire the route**

In `internal/server/router.go`, right after `r.Patch("/cases/{id}/urls/{url_id}", h.UpdateCaseURLPhase)` (~134):

```go
			r.Patch("/cases/{id}/letters/{letter_id}", h.UpdateCaseLetter)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/server/... -run 'TestUpdateCaseLetter_PartialUpdate|TestUpdateCaseLetter_NonOwningDepartment404' -v`
Expected: PASS.

- [ ] **Step 6: Run the full backend suite**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/server/case_handlers.go internal/server/router.go internal/server/case_handlers_test.go
git commit -m "server: add PATCH /api/cases/{id}/letters/{letter_id}"
```

---

## Task 7: Frontend types + API fetchers

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/cases.ts`

**Interfaces:**
- Consumes: `api.get`/`api.patch` (`client.ts`).
- Produces: `CaseSummary`, `CaseSummaryDomain` types; `fetchCaseSummaries()`, `updateCaseLetter(caseId, letterId, fields)` — consumed by Task 9/10.

- [ ] **Step 1: Add types to `web/src/api/types.ts`**

Insert after the `CaseLettersResponse` type (~line 113):

```ts
// One row of GET /api/case-summaries — the Cases view's per-case row: a
// Case's own fields plus its Notice letter's fields (Notice chosen over
// Memo when both exist, same convention as current_reference_number) and
// every domain it covers.
export type CaseSummaryDomain = { url_id: number; url: string; phase: string }

export type CaseSummary = {
  id: number
  agency_id?: number
  agency_name?: string
  status?: string
  due_date?: string
  requested_at?: string
  created_at: string
  notice_letter_id?: number
  notice_subject?: string
  notice_workflow_status?: string
  notice_reference_number_external?: string
  notice_reference_number_internal?: string
  notice_recipient?: string
  notice_requestor?: string
  notice_letter_date?: string
  notice_received_at?: string
  notice_submitted_at?: string
  notice_remarks?: string
  memo_letter_id?: number
  memo_subject?: string
  memo_reference_number_internal?: string
  domains: CaseSummaryDomain[]
}
```

- [ ] **Step 2: Add fetchers to `web/src/api/cases.ts`**

Change the import line at the top:

```ts
import type { Case, CaseLetter, CaseLettersResponse, CaseSummary } from './types'
```

Append to the file:

```ts
export async function fetchCaseSummaries(): Promise<CaseSummary[]> {
  const data = await api.get<CaseSummary[]>('/case-summaries')
  return Array.isArray(data) ? data : []
}

export type CaseLetterFieldsUpdate = Partial<{
  subject: string
  workflowStatus: string
  referenceNumberExternal: string
  referenceNumberInternal: string
  recipient: string
  requestor: string
  remarks: string
  letterDate: string | null
  receivedAt: string | null
  submittedAt: string | null
}>

// Partial update of one CaseLetter's fields (PATCH
// /api/cases/{caseId}/letters/{letterId}) — only keys present in `fields`
// are sent. Date fields clear via null -> "" (same sentinel convention as
// updateCase's dueDate/requestedAt above); string fields clear via "".
export async function updateCaseLetter(caseId: number, letterId: number, fields: CaseLetterFieldsUpdate): Promise<void> {
  const body: Record<string, string> = {}
  if (fields.subject !== undefined) body.subject = fields.subject
  if (fields.workflowStatus !== undefined) body.workflow_status = fields.workflowStatus
  if (fields.referenceNumberExternal !== undefined) body.reference_number_external = fields.referenceNumberExternal
  if (fields.referenceNumberInternal !== undefined) body.reference_number_internal = fields.referenceNumberInternal
  if (fields.recipient !== undefined) body.recipient = fields.recipient
  if (fields.requestor !== undefined) body.requestor = fields.requestor
  if (fields.remarks !== undefined) body.remarks = fields.remarks
  if (fields.letterDate !== undefined) body.letter_date = fields.letterDate ?? ''
  if (fields.receivedAt !== undefined) body.received_at = fields.receivedAt ?? ''
  if (fields.submittedAt !== undefined) body.submitted_at = fields.submittedAt ?? ''
  await api.patch<void>(`/cases/${caseId}/letters/${letterId}`, body)
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd web && npm run build`
Expected: success (`tsc --noEmit` passes; these are pure additions, nothing consumes them yet).

- [ ] **Step 4: Commit**

```bash
git add web/src/api/types.ts web/src/api/cases.ts
git commit -m "web: add CaseSummary types and fetchCaseSummaries/updateCaseLetter"
```

---

## Task 8: `AddUrlDialog` add/edit-dual-purpose mode

**Files:**
- Modify: `web/src/routes/urls.tsx` (the `AddUrlDialog` function, ~line 321-715)

**Interfaces:**
- Consumes: `CaseSummary` (Task 7); `updateCase` (existing import), `updateCaseLetter`, `addUrlToCase`, `addCaseLetter` (existing/Task 7 imports).
- Produces: `AddUrlDialog` accepting `editing: CaseSummary | null` — consumed by Task 9's case-row edit action and the page's `+ Create Case` button.

No automated test framework exists for this file — verify via `npm run build` after each step and a manual dev.sh check at the end of Task 10.

- [ ] **Step 1: Add the `editing` prop and import `updateCaseLetter`**

In `web/src/routes/urls.tsx`, update the import line:

```tsx
import { createCase, addCaseLetter, addUrlToCase, updateCase, updateCaseLetter } from '../api/cases'
```

Add `CaseSummary` to the existing `types` import line (already imports `URLEntry`, `Agency`, etc.):

```tsx
import type { URLEntry, Agency, Department, DueDatePreset, Instrument, Citation, LegalCategory, LegalElement, LegalSubElement, URLOffence, Recipient, Requestor, CaseSummary } from '../api/types'
```

Change `AddUrlDialog`'s signature to accept `editing`:

```tsx
function AddUrlDialog({
  open,
  onClose,
  onAdded,
  agencies,
  duePresets,
  recipients,
  requestors,
  editing,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  agencies: Agency[]
  duePresets: DueDatePreset[]
  recipients: Recipient[]
  requestors: Requestor[]
  editing: CaseSummary | null
}) {
```

- [ ] **Step 2: Add the prefill effect**

Add a `useEffect` right after the `reset` function definition (import `useEffect` — already imported at the top of the file):

```tsx
  useEffect(() => {
    if (!open) return
    if (editing) {
      setValue(editing.domains.map(d => d.url).join('\n'))
      setOffences([])
      setAgencyId(editing.agency_id ?? '')
      setDueDurationMinutes('') // existing due date shown read-only; picking a duration replaces it (see the read-only line in the form below)
      setPhase(editing.status || 'requested')
      setCreateLetter(true)
      setReferenceNumberExternal(editing.notice_reference_number_external ?? '')
      setReferenceNumberInternal(editing.notice_reference_number_internal ?? '')
      setRecipient(editing.notice_recipient ?? '')
      setNoticeSubject(editing.notice_subject ?? '')
      setMemoSubject(editing.memo_subject ?? '')
      setMemoReferenceNumberInternal(editing.memo_reference_number_internal ?? '')
      setRequestor(editing.notice_requestor ?? '')
      setWorkflowStatus(editing.notice_workflow_status ?? '')
      setLetterDate(editing.notice_letter_date ? editing.notice_letter_date.slice(0, 10) : '')
      setReceivedAt(editing.notice_received_at ? editing.notice_received_at.slice(0, 10) : '')
      setSubmittedAt(editing.notice_submitted_at ? editing.notice_submitted_at.slice(0, 10) : '')
      setRemarks(editing.notice_remarks ?? '')
      setError(null)
    } else {
      reset()
    }
  }, [open, editing])
```

- [ ] **Step 3: Show the existing due date read-only, above the duration picker**

In the "Time to Block" `form-field` block, add a read-only line when editing:

```tsx
            <div className="form-field">
              <label className="form-label" id="add-due-date-label">Time to Block</label>
              {editing && (
                <p className="text-xs text-stone-muted" style={{ marginTop: 0, marginBottom: 4 }}>
                  Current deadline: {editing.due_date ? DUE_DATE_FMT.format(new Date(editing.due_date)) : '—'} — pick a duration below to replace it
                </p>
              )}
              <Select value={dueDurationMinutes} onValueChange={setDueDurationMinutes} disabled={loading}>
```

(`DUE_DATE_FMT` is already defined module-level in this file, above `PAGE_SIZE`.)

- [ ] **Step 4: Branch `handleSubmit` for the edit path**

Replace the body of `handleSubmit` — keep the existing `domains`/`pending`/`allOffences`/`caseOpts` computation at the top unchanged, then branch:

```tsx
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const domains = value.split('\n').map(s => s.trim()).filter(Boolean)
    if (domains.length === 0) { setError('At least one domain is required'); return }
    const pending = pickerRef.current?.flush()
    const allOffences = pending ? [...offences, pending] : offences

    const caseOpts: { agencyId?: number; dueDate?: string } = {}
    if (agencyId !== '') caseOpts.agencyId = agencyId
    if (dueDurationMinutes) caseOpts.dueDate = dueDateFromDurationMinutes(Number(dueDurationMinutes))

    setLoading(true)
    setError(null)
    try {
      if (editing) {
        await updateCase(editing.id, {
          status: phase,
          agencyId: agencyId === '' ? null : agencyId,
          ...(caseOpts.dueDate ? { dueDate: caseOpts.dueDate } : {}),
        })

        const existingURLs = new Set(editing.domains.map(d => d.url))
        const newDomains = domains.filter(d => !existingURLs.has(d))
        // Removing a line from the textarea is a deliberate no-op — there's
        // no "unlink domain from case" endpoint (see the spec's Out of
        // Scope section); only additions are applied.
        await Promise.all(newDomains.map(d => addUrlToCase(editing.id, d, phase)))

        const richFields = {
          recipient: recipient.trim() || undefined,
          requestor: requestor.trim() || undefined,
          workflowStatus: workflowStatus || undefined,
          letterDate: isoFromDateInput(letterDate),
          receivedAt: isoFromDateInput(receivedAt),
          submittedAt: isoFromDateInput(submittedAt),
          remarks: remarks.trim() || undefined,
        }
        const letterWork: Promise<unknown>[] = []
        if (editing.notice_letter_id) {
          letterWork.push(updateCaseLetter(editing.id, editing.notice_letter_id, {
            ...richFields,
            subject: noticeSubject.trim() || undefined,
            referenceNumberExternal: referenceNumberExternal.trim() || undefined,
            referenceNumberInternal: referenceNumberInternal.trim() || undefined,
          }))
        } else {
          letterWork.push(addCaseLetter(editing.id, {
            type: 'Notice',
            recipient: richFields.recipient, requestor: richFields.requestor,
            workflow_status: richFields.workflowStatus, letter_date: richFields.letterDate,
            received_at: richFields.receivedAt, submitted_at: richFields.submittedAt, remarks: richFields.remarks,
            subject: noticeSubject.trim() || undefined,
            reference_number_external: referenceNumberExternal.trim() || undefined,
            reference_number_internal: referenceNumberInternal.trim() || undefined,
          }))
        }
        if (editing.memo_letter_id) {
          letterWork.push(updateCaseLetter(editing.id, editing.memo_letter_id, {
            ...richFields,
            subject: memoSubject.trim() || undefined,
            referenceNumberInternal: memoReferenceNumberInternal.trim() || undefined,
          }))
        } else if (memoSubject.trim() || memoReferenceNumberInternal.trim()) {
          letterWork.push(addCaseLetter(editing.id, {
            type: 'Memo',
            recipient: richFields.recipient, requestor: richFields.requestor,
            workflow_status: richFields.workflowStatus, letter_date: richFields.letterDate,
            received_at: richFields.receivedAt, submitted_at: richFields.submittedAt, remarks: richFields.remarks,
            subject: memoSubject.trim() || undefined,
            reference_number_external: referenceNumberExternal.trim() || undefined,
            reference_number_internal: memoReferenceNumberInternal.trim() || undefined,
          }))
        }
        await Promise.all([
          ...letterWork,
          ...newDomains.flatMap(d => allOffences.map(o => attachOffence(d, o.categoryId, o.elementId, o.subElementId))),
        ])
        reset()
        onAdded()
        onClose()
        return
      }

      const created = await Promise.all(domains.map(d => createUrl(d)))
      const caseWork = (async () => {
        const c = await createCase(created[0].url, phase, caseOpts)
        await Promise.all(created.slice(1).map(u => addUrlToCase(c.id, u.url, phase)))
        const richFields = createLetter ? {
          recipient: recipient.trim() || undefined,
          requestor: requestor.trim() || undefined,
          workflow_status: workflowStatus || undefined,
          letter_date: isoFromDateInput(letterDate),
          received_at: isoFromDateInput(receivedAt),
          submitted_at: isoFromDateInput(submittedAt),
          remarks: remarks.trim() || undefined,
        } : {}
        const letters = [{
          ...richFields,
          type: 'Notice',
          subject: createLetter ? (noticeSubject.trim() || undefined) : undefined,
          reference_number_external: referenceNumberExternal.trim() || undefined,
          reference_number_internal: referenceNumberInternal.trim() || undefined,
        }]
        if (createLetter && (memoSubject.trim() || memoReferenceNumberInternal.trim())) {
          letters.push({
            ...richFields,
            type: 'Memo',
            subject: memoSubject.trim() || undefined,
            reference_number_external: referenceNumberExternal.trim() || undefined,
            reference_number_internal: memoReferenceNumberInternal.trim() || undefined,
          })
        }
        await Promise.all(letters.map(l => addCaseLetter(c.id, l)))
      })()
      await Promise.all([
        ...created.flatMap(u => allOffences.map(o => attachOffence(u.url, o.categoryId, o.elementId, o.subElementId))),
        caseWork,
      ])
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${editing ? 'save' : 'create'} case`)
    } finally {
      setLoading(false)
    }
  }
```

- [ ] **Step 5: Swap dialog title/description and submit button label, add the removal hint**

```tsx
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit Case' : 'Create Case'}</DialogTitle>
          <DialogDescription>
            {editing
              ? 'Update this case\'s shared fields. Adding a domain line links it to this case; removing a line here does not unlink it — remove a domain from Cases view instead.'
              : <>Enter one or more domains or full URLs to monitor for DNS compliance — full URLs will have their domain automatically extracted, and multiple entries (one per line) share the case opened below. A case is required to add {domainCount > 1 ? 'these domains' : 'a domain'}; if a later batch covers a different offence, open a new case for it instead of reusing this one.</>}
          </DialogDescription>
        </DialogHeader>
```

And the submit button:

```tsx
            <button type="submit" className="btn-primary" disabled={loading}>
              {editing ? (loading ? 'Saving…' : 'Save Changes') : (loading ? 'Creating…' : 'Create Case')}
            </button>
```

- [ ] **Step 6: Update the two call sites (temporary — both pass `editing={null}` until Task 9 wires the real edit trigger)**

At the bottom of `URLsPage`'s JSX, update the existing `<AddUrlDialog>` call to pass `editing={null}` (Task 9 replaces this with the shared open/editing state):

```tsx
      <AddUrlDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
        agencies={agencies}
        duePresets={duePresets}
        recipients={recipients}
        requestors={requestors}
        editing={null}
      />
```

- [ ] **Step 7: Verify it compiles**

Run: `cd web && npm run build`
Expected: success.

- [ ] **Step 8: Commit**

```bash
git add web/src/routes/urls.tsx
git commit -m "web: AddUrlDialog add/edit-dual-purpose mode"
```

---

## Task 9: Cases view — route search param, ToggleGroup, tree grid

**Files:**
- Modify: `web/src/routes/urls.tsx`

**Interfaces:**
- Consumes: `CaseSummary`, `CaseSummaryDomain` (Task 7); `fetchCaseSummaries`, `updateCaseURLPhase`, `updateCase` (existing/Task 7 imports); `AddUrlDialog` with `editing` prop (Task 8).
- Produces: Cases view rendering, `caseSummaries` state, `editingCase` state — consumed by Task 10 (filters).

- [ ] **Step 1: Add imports**

```tsx
import { useNavigate } from '@tanstack/react-router'
import {
  type ColumnDef,
  type SortingState,
  type PaginationState,
  type VisibilityState,
  type ExpandedState,
  getCoreRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  getExpandedRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { ChevronRight } from '@/components/ui/chevron-right'
import { SquarePenIcon } from '@/components/ui/square-pen'
import { DataGridTableRowExpand } from '@/components/reui/data-grid/data-grid-table'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { fetchCaseSummaries, updateCaseURLPhase } from '../api/cases'
```

(`createCase, addCaseLetter, addUrlToCase, updateCase, updateCaseLetter` stays from Task 8's edit to that import line — add `fetchCaseSummaries, updateCaseURLPhase` there instead of a separate line if preferred.)

- [ ] **Step 2: Add `validateSearch` to the route**

```tsx
export const Route = createFileRoute('/urls')({
  validateSearch: (search: Record<string, unknown>): { view: 'domains' | 'cases' } => ({
    view: search.view === 'cases' ? 'cases' : 'domains',
  }),
  component: URLsPage,
})
```

- [ ] **Step 3: Add Cases-view state to `URLsPage`**

```tsx
  const { view } = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  const [caseSummaries, setCaseSummaries] = useState<CaseSummary[]>([])
  const [addOpen, setAddOpen] = useState(false)
  const [editingCase, setEditingCase] = useState<CaseSummary | null>(null)

  const [casesSorting, setCasesSorting] = useState<SortingState>([])
  const [casesPagination, setCasesPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  const [casesColumnVisibility, setCasesColumnVisibility] = useState<VisibilityState>({})
  const [casesExpanded, setCasesExpanded] = useState<ExpandedState>({})

  const { ready: casesGridPrefReady } = useGridPreference(
    'urls-cases',
    { sorting: casesSorting, columnVisibility: casesColumnVisibility, pageSize: casesPagination.pageSize },
    {
      setSorting: setCasesSorting,
      setColumnVisibility: setCasesColumnVisibility,
      setPageSize: pageSize => setCasesPagination(p => ({ ...p, pageSize })),
    }
  )
```

Remove the now-duplicate `const [addOpen, setAddOpen] = useState(false)` further down in the existing state block (keep only the one added here).

- [ ] **Step 4: Load case summaries alongside everything else**

Update `load`'s `Promise.all` to also fetch case summaries:

```tsx
  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      const [u, a, d, p, rc, rq, cs] = await Promise.all([
        fetchUrls(), fetchAgencies(), fetchDepartmentsOpen(), fetchDueDatePresets(), fetchRecipients(), fetchRequestors(), fetchCaseSummaries(),
      ])
      setUrls(u)
      setAgencies(a)
      setDepartments(d)
      setDuePresets(p)
      setRecipients(rc)
      setRequestors(rq)
      setCaseSummaries(cs)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load domains')
    } finally {
      setLoading(false)
    }
  }, [])
```

- [ ] **Step 5: Generalize `deleteTarget`'s type**

Change:

```tsx
  const [deleteTarget, setDeleteTarget] = useState<URLEntry | null>(null)
```

to:

```tsx
  const [deleteTarget, setDeleteTarget] = useState<{ id: number; url: string } | null>(null)
```

(`URLEntry` values already satisfy this shape structurally — no other change needed at the existing `setDeleteTarget(u)` call site in Domain view's action column.)

- [ ] **Step 6: Add case-status/phase optimistic-update handlers**

```tsx
  const handleCaseStatusChange = useCallback(async (caseId: number, status: string) => {
    const prev = caseSummaries.find(c => c.id === caseId)?.status
    setCaseSummaries(prevList => prevList.map(c => c.id === caseId ? { ...c, status } : c))
    try {
      await updateCase(caseId, { status })
    } catch {
      setCaseSummaries(prevList => prevList.map(c => c.id === caseId ? { ...c, status: prev } : c))
    }
  }, [caseSummaries])

  const handlePhaseChange = useCallback(async (caseId: number, domain: CaseSummaryDomain, phase: string) => {
    const prevPhase = domain.phase
    setCaseSummaries(prevList => prevList.map(c => c.id === caseId
      ? { ...c, domains: c.domains.map(d => d.url_id === domain.url_id ? { ...d, phase } : d) }
      : c))
    try {
      await updateCaseURLPhase(caseId, domain.url_id, phase)
    } catch {
      setCaseSummaries(prevList => prevList.map(c => c.id === caseId
        ? { ...c, domains: c.domains.map(d => d.url_id === domain.url_id ? { ...d, phase: prevPhase } : d) }
        : c))
    }
  }, [])
```

- [ ] **Step 7: Build the tree row types, tree data, and columns**

Add near the other module-level types (`StagedOffence` etc.), or just above `URLsPage`:

```tsx
type DomainSubRow = { kind: 'domain'; caseId: number; status: string; domain: CaseSummaryDomain }
type CaseRow = { kind: 'case'; summary: CaseSummary; subRows: DomainSubRow[] }
type CaseTreeRow = CaseRow | DomainSubRow
```

Inside `URLsPage`, after `filteredCases` is defined (Task 10 adds it — for this task, build `caseTreeData` off `caseSummaries` directly and Task 10 will swap the source to `filteredCases`):

```tsx
  const caseTreeData = useMemo<CaseRow[]>(() => caseSummaries.map(summary => ({
    kind: 'case',
    summary,
    subRows: summary.domains.map(domain => ({ kind: 'domain', caseId: summary.id, status: summary.status ?? '', domain })),
  })), [caseSummaries])

  const caseColumns = useMemo<ColumnDef<CaseTreeRow>[]>(() => [
    {
      id: 'case',
      accessorFn: r => r.kind === 'case' ? r.summary.id : r.domain.url,
      header: ({ column }) => <SortableHeader column={column} title="Case #" />,
      enableHiding: false,
      size: 220,
      meta: { headerTitle: 'Case #', headerClassName: 'col-domain th-left', cellClassName: 'col-domain' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'domain') {
          return <span className="dns-name">{original.domain.url}</span>
        }
        const expandControl = original.subRows.length > 0 ? (
          <DataGridTableRowExpand row={row}>
            <ChevronRight className={`expand-icon${row.getIsExpanded() ? ' expanded' : ''}`} />
          </DataGridTableRowExpand>
        ) : null
        return <span className="flex items-center gap-[2px]">{expandControl}<span className="hostname">#{original.summary.id}</span></span>
      },
    },
    {
      id: 'agency',
      header: 'Agency',
      accessorFn: r => r.kind === 'case' ? (r.summary.agency_name ?? '') : '',
      meta: { headerTitle: 'Agency', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.agency_name ?? '—'}</span> : null,
    },
    {
      id: 'status',
      header: 'Status',
      accessorFn: r => r.kind === 'case' ? (r.summary.status ?? '') : r.status,
      meta: { headerTitle: 'Status', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'case') {
          return <span className="dns-name">{STATUS_OPTIONS.find(o => o.value === (original.summary.status ?? ''))?.label ?? '—'}</span>
        }
        return (
          <Select value={original.status} onValueChange={v => handleCaseStatusChange(original.caseId, v)}>
            <SelectTrigger aria-label={`Status for ${original.domain.url}`} placeholder="—" className="w-full" />
            <SelectContent>
              {STATUS_OPTIONS.map((opt, i) => (
                <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )
      },
    },
    {
      id: 'phase',
      header: 'Phase',
      accessorFn: r => r.kind === 'domain' ? r.domain.phase : '',
      meta: { headerTitle: 'Phase', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind !== 'domain') return null
        return (
          <Select value={original.domain.phase} onValueChange={v => handlePhaseChange(original.caseId, original.domain, v)}>
            <SelectTrigger aria-label={`Phase for ${original.domain.url}`} placeholder="—" className="w-full" />
            <SelectContent>
              {CASE_PHASE_OPTIONS.map((opt, i) => (
                <SelectItem key={opt.value} index={i} value={opt.value}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )
      },
    },
    {
      id: 'due_date',
      accessorFn: r => r.kind === 'case' ? (r.summary.due_date ?? '') : '',
      header: ({ column }) => <SortableHeader column={column} title="Due Date" />,
      meta: { headerTitle: 'Due Date', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case'
        ? <span className="dns-name">{row.original.summary.due_date ? DUE_DATE_FMT.format(new Date(row.original.summary.due_date)) : '—'}</span>
        : null,
    },
    {
      id: 'reference_number',
      header: 'Ref No.',
      accessorFn: r => r.kind === 'case' ? (r.summary.notice_reference_number_external ?? '') : '',
      meta: { headerTitle: 'Ref No.', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.notice_reference_number_external || '—'}</span> : null,
    },
    {
      id: 'domain_count',
      header: 'Domains',
      accessorFn: r => r.kind === 'case' ? r.summary.domains.length : '',
      meta: { headerTitle: 'Domains', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.domains.length}</span> : null,
    },
    {
      id: 'subject',
      header: 'Subject',
      accessorFn: r => r.kind === 'case' ? (r.summary.notice_subject ?? '') : '',
      meta: { headerTitle: 'Subject', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.notice_subject || '—'}</span> : null,
    },
    {
      id: 'workflow_status',
      header: 'Workflow Status',
      accessorFn: r => r.kind === 'case' ? (r.summary.notice_workflow_status ?? '') : '',
      meta: { headerTitle: 'Workflow Status', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.notice_workflow_status || '—'}</span> : null,
    },
    {
      id: 'action',
      header: 'Action',
      enableHiding: false,
      size: 140,
      meta: { headerClassName: 'th-center', cellClassName: 'text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'case') {
          return (
            <button type="button" className="screenshot-icon-btn" onClick={() => setEditingCase(original.summary)} aria-label={`Edit case #${original.summary.id}`} title="Edit">
              <SquarePenIcon size={16} />
            </button>
          )
        }
        return (
          <div className="flex items-center justify-center gap-1">
            <Link to="/domain/$url" params={{ url: original.domain.url }} search={{ tab: 'overview' }} className="screenshot-icon-btn" aria-label={`View details for ${original.domain.url}`} title="View details">
              <GripIcon size={16} />
            </Link>
            <button type="button" className="screenshot-icon-btn" onClick={() => setDeleteTarget({ id: original.domain.url_id, url: original.domain.url })} aria-label={`Delete ${original.domain.url}`} title="Delete">
              <XIcon size={16} />
            </button>
          </div>
        )
      },
    },
  ], [handleCaseStatusChange, handlePhaseChange])

  const casesTable = useReactTable({
    data: caseTreeData,
    columns: caseColumns,
    initialState: { columnPinning: { left: ['case'], right: ['action'] } },
    state: { sorting: casesSorting, pagination: casesPagination, columnVisibility: casesColumnVisibility, expanded: casesExpanded },
    onSortingChange: setCasesSorting,
    onPaginationChange: setCasesPagination,
    onColumnVisibilityChange: setCasesColumnVisibility,
    onExpandedChange: setCasesExpanded,
    getRowId: r => r.kind === 'case' ? `case:${r.summary.id}` : `case-domain:${r.caseId}:${r.domain.url_id}`,
    getSubRows: r => r.kind === 'case' ? r.subRows : undefined,
    getRowCanExpand: row => row.original.kind === 'case' && row.original.subRows.length > 0,
    paginateExpandedRows: false,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  })

  const casesGridLoading = loading || !casesGridPrefReady
```

- [ ] **Step 8: Add the ToggleGroup and render Cases view**

In the page header, right after the `<h1>`/`<p>` and before the `+ Create Case` button's wrapping `div`:

```tsx
        <ToggleGroup
          type="single"
          value={view}
          onValueChange={v => { if (v) navigate({ to: '/urls', search: { view: v as 'domains' | 'cases' }, replace: true }) }}
          variant="outline"
          aria-label="View"
        >
          <ToggleGroupItem value="domains">Domain</ToggleGroupItem>
          <ToggleGroupItem value="cases">Cases</ToggleGroupItem>
        </ToggleGroup>
```

Wrap the existing table/empty-state JSX block (the `error ? ... : !gridLoading && urls.length === 0 ? ... : (...)` ternary) so it only renders for `view === 'domains'`, and add a parallel block for `view === 'cases'` using `casesTable` the same way the Domain-view block uses `table`:

```tsx
      {view === 'domains' ? (
        error ? (
          /* unchanged existing error-state JSX */
        ) : !gridLoading && urls.length === 0 ? (
          /* unchanged existing empty-state JSX */
        ) : (
          /* unchanged existing filter-bar + DataGrid JSX using `table` */
        )
      ) : (
        error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !casesGridLoading && caseSummaries.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No cases yet</p>
            <p className="empty-body">Create a case to start monitoring a domain for DNS compliance.</p>
            <button className="btn-primary" onClick={() => setAddOpen(true)}>Create Case</button>
          </div>
        ) : (
          <div className="flex flex-col items-stretch w-full gap-4 mt-4">
            <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full">
              <Input
                type="search"
                placeholder="Search case ref. or domain..."
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="max-w-64"
                aria-label="Search cases"
              />
              <Filters filters={filters} fields={filterFields} onChange={setFilters} />
              <div style={{ marginLeft: 'auto' }}>
                <DataGridColumnVisibility table={casesTable} trigger={<Button variant="outline">Columns</Button>} />
              </div>
            </div>
            <div className="results-wrap w-full">
              <DataGrid
                table={casesTable}
                recordCount={caseTreeData.length}
                isLoading={casesGridLoading}
                tableClassNames={{ base: 'results-table results-table--pinned' }}
                tableLayout={{ columnsPinnable: true }}
              >
                <DataGridContainer className="overflow-x-auto overflow-y-visible mb-5">
                  <DataGridTable />
                </DataGridContainer>
                <DataGridPagination sizes={[10, 25, 50, 100]} />
              </DataGrid>
            </div>
          </div>
        )
      )}
```

- [ ] **Step 9: Wire the shared `AddUrlDialog` instance for both add and edit**

Replace the `<AddUrlDialog>` call from Task 8 (Step 6) with:

```tsx
      <AddUrlDialog
        open={addOpen || editingCase !== null}
        onClose={() => { setAddOpen(false); setEditingCase(null) }}
        onAdded={load}
        agencies={agencies}
        duePresets={duePresets}
        recipients={recipients}
        requestors={requestors}
        editing={editingCase}
      />
```

- [ ] **Step 10: Verify it compiles**

Run: `cd web && npm run build`
Expected: success.

- [ ] **Step 11: Commit**

```bash
git add web/src/routes/urls.tsx
git commit -m "web: Cases view tree grid with case-row edit and domain-subrow actions"
```

---

## Task 10: Filters apply to Cases view

**Files:**
- Modify: `web/src/routes/urls.tsx`

**Interfaces:**
- Consumes: `caseSummaries`, `urls`, existing `search`/`filters` state (Task 9 / pre-existing).
- Produces: `filteredCases` — swaps into `caseTreeData`'s source (Task 9's `caseSummaries.map(...)` becomes `filteredCases.map(...)`).

- [ ] **Step 1: Add the department-lookup map and `filteredCases` memo**

Add near the existing `filtered` memo (Domain view's):

```tsx
  const urlDeptMap = useMemo(() => {
    const m = new Map<string, string[]>()
    urls.forEach(u => m.set(u.url, u.requesting_departments ?? []))
    return m
  }, [urls])

  const filteredCases = useMemo(() => {
    const query = search.trim().toLowerCase()
    return caseSummaries.filter(c => {
      const matchesSearch = !query
        || (c.notice_reference_number_external ?? '').toLowerCase().includes(query)
        || c.domains.some(d => d.url.toLowerCase().includes(query))
      const matchesStatus = !statusFilter || (c.status ?? '') === statusFilter
      const matchesDept = !deptFilterName || c.domains.some(d => (urlDeptMap.get(d.url) ?? []).includes(deptFilterName))
      const matchesAgency = !agencyFilter || String(c.agency_id ?? '') === agencyFilter
      return matchesSearch && matchesStatus && matchesDept && matchesAgency
        && matchesDateFilter(c.created_at, createdAtFilter)
        && matchesDateFilter(c.due_date, dueDateFilter)
    })
  }, [caseSummaries, search, statusFilter, deptFilterName, agencyFilter, createdAtFilter, dueDateFilter, urlDeptMap])
```

- [ ] **Step 2: Swap `caseTreeData`'s source from `caseSummaries` to `filteredCases`**

In the `caseTreeData` memo added in Task 9, change:

```tsx
  const caseTreeData = useMemo<CaseRow[]>(() => filteredCases.map(summary => ({
```

(dependency array becomes `[filteredCases]`).

- [ ] **Step 3: Reset Cases-view pagination when filters change**

Extend the existing `useEffect(() => { setPagination(p => ({ ...p, pageIndex: 0 })) }, [...])` to also reset `casesPagination`:

```tsx
  useEffect(() => {
    setPagination(p => ({ ...p, pageIndex: 0 }))
    setCasesPagination(p => ({ ...p, pageIndex: 0 }))
  }, [search, statusFilter, deptFilter, agencyFilter, createdAtFilter, dueDateFilter])
```

- [ ] **Step 4: Use `filteredCases.length === 0` (not `caseSummaries.length === 0`) for the Cases-view "no cases match filters" branch**

In Task 9 Step 8's Cases-view JSX, the empty-state check for "no domains yet" should stay against `caseSummaries.length === 0` (nothing exists at all), but add a second, filtered-empty branch matching Domain view's `filtered.length === 0` pattern — inside the `DataGrid`-rendering branch, before rendering `DataGrid`:

```tsx
            <div className="results-wrap w-full">
              {!casesGridLoading && filteredCases.length === 0 ? (
                <div className="empty-state" style={{ padding: '3rem 0' }}>
                  <p className="empty-heading">No cases match the current filters</p>
                </div>
              ) : (
                <DataGrid
                  table={casesTable}
                  recordCount={caseTreeData.length}
                  isLoading={casesGridLoading}
                  tableClassNames={{ base: 'results-table results-table--pinned' }}
                  tableLayout={{ columnsPinnable: true }}
                >
                  <DataGridContainer className="overflow-x-auto overflow-y-visible mb-5">
                    <DataGridTable />
                  </DataGridContainer>
                  <DataGridPagination sizes={[10, 25, 50, 100]} />
                </DataGrid>
              )}
            </div>
```

- [ ] **Step 5: Verify it compiles**

Run: `cd web && npm run build`
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add web/src/routes/urls.tsx
git commit -m "web: apply Domain view's filters/search to Cases view"
```

---

## Task 11: Manual verification

No further code changes — this is the spec's own Testing section, run by hand against `dev.sh`.

- [ ] **Step 1: Start the stack**

Run: `./dev.sh`

- [ ] **Step 2: Walk the spec's manual checklist**

- Create a multi-domain case via `+ Create Case` (multiple lines in the domain textarea).
- Switch to Cases view — confirm it groups correctly with the right domain count.
- Expand the case row — confirm Phase edits per domain persist (change one domain's Phase, reload, confirm it stuck).
- Edit the case via the pencil icon — confirm every field prefills (domains, agency, phase/status, ref numbers, Notice/Memo subjects, recipient/requestor/workflow status/dates/remarks).
- Change agency + Notice subject, save — confirm both persisted (reload Cases view).
- Add a new domain line during edit — confirm it's linked to the case afterward (appears in the expanded subrows).
- Add a bogus domain line then remove it before saving — confirm nothing broke (no orphan row, no error).
- Confirm Domain view is visually unchanged (still just Grip link + Status select + delete, no new edit icon).
- Confirm `?view=cases` in the URL survives a page reload and lands on Cases view.

- [ ] **Step 3: Report results to the user**

No commit for this task — it's verification only. If anything in the checklist fails, return to the relevant task above and fix before considering the feature done.
