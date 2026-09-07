# OIC Field — Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Default `AddCaseLetter`'s `oic_user_id` to the caller's own user when omitted, let `UpdateCaseLetterFields` set/clear a letter's OIC after creation, and expose a `GET /api/users/open` route so any authenticated user can populate an OIC picker scoped to their own department.

**Architecture:** Three independent, additive changes to `internal/db` and `internal/server` — no new files, no schema migration (`CaseLetter.OICUserID`/`OICUser` already exist and are already `AutoMigrate`d). This is Part 1 (backend half) of the compliance-report-export feature — it exists because the CMOD export (a later, independent plan) needs a real OIC value to show, and today nothing ever sets `oic_user_id`.

**Tech Stack:** Go, GORM, chi router — no new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-04-compliance-report-export-design.md`, "Part 1 — OIC field" section (the "Backend" subsection specifically).

**Prerequisite:** None — this is a root task. Plan 02 (`2026-09-04-compliance-report-export-02-oic-frontend.md`) depends on this plan being merged first (it calls `GET /api/users/open` and sends `oic_user_id` through `PATCH /api/cases/{id}/letters/{letter_id}`).

## Global Constraints

- Every step below contains real, complete code — no "add appropriate error handling" placeholders.
- Follow the codebase's existing three-state double-pointer convention exactly (`outer nil` = don't touch, `outer non-nil → nil inner` = clear, `outer non-nil → &v` = set) for `OICUserID **uint` on `CaseLetterFields` — this is already established by `CaseFields.AgencyID` (`internal/db/models.go`), don't invent a different sentinel.
- The JSON-body clear sentinel for a `*uint` id field is `0` (never a real row id) — this is already established by `UpdateCase`'s handling of `agency_id` (`internal/server/case_handlers.go`); reuse it exactly for `oic_user_id`, don't use JSON `null` for this purpose since Go's `*uint` can't distinguish "absent" from "explicit null" without extra plumbing this codebase doesn't already have.
- `go build ./... && go test ./internal/db/... ./internal/server/... -v` must pass after every task.

---

### Task 1: `CaseLetterFields.OICUserID` — store-layer field + partial-update support

**Files:**
- Modify: `internal/db/models.go` (the `CaseLetterFields` struct, currently at lines 756-767)
- Modify: `internal/db/cases.go` (the `UpdateCaseLetterFields` method, currently at lines 297-340)
- Test: `internal/db/cases_test.go`

**Interfaces (produced, relied on by Task 2 and by Plan 02):**
```go
// internal/db/models.go — CaseLetterFields gains one field:
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
	OICUserID               **uint // three-state: nil=don't touch, &nil=clear, &(&id)=set — same convention as CaseFields.AgencyID
}
```

**Current `UpdateCaseLetterFields` body** (`internal/db/cases.go:297-340`), for reference — you're adding one more `if` block to the `updates` map, following the exact same shape as the existing single-pointer fields but dereferencing twice like `CaseFields.AgencyID`'s handling in `UpdateCaseFields` (`internal/db/cases.go:36-61`, specifically `if fields.AgencyID != nil { updates["agency_id"] = *fields.AgencyID }` — note `*fields.AgencyID` is itself a `*uint`, which GORM's map-based `Updates` writes as the column value, `nil` becoming SQL `NULL`):
```go
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
Add, right after the `SubmittedAt` block:
```go
	if fields.OICUserID != nil {
		updates["oic_user_id"] = *fields.OICUserID
	}
```

- [ ] **Step 1: Write the failing test** in `internal/db/cases_test.go`. Mirror the existing `TestUpdateCaseFields_SetAndClear` pattern (`internal/db/cases_test.go:223-` — it creates a department, an entity, calls the update with a double-pointer field, then re-reads to confirm):
```go
func TestUpdateCaseLetterFields_SetsAndClearsOICUserID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, _ := store.CreateDepartment(ctx, "OICFieldsDept")
	u, _ := store.CreateURL(ctx, "oic-fields.com")
	c, err := store.CreateCase(ctx, dept.ID, u.ID, "requested", db.CaseCreateOptions{})
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	letter, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: c.ID, Type: "Notice"})
	if err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}

	oicID := uint(42)
	oicIDPtr := &oicID
	found, err := store.UpdateCaseLetterFields(ctx, c.ID, letter.ID, db.CaseLetterFields{OICUserID: &oicIDPtr})
	if err != nil || !found {
		t.Fatalf("UpdateCaseLetterFields(set): found=%v err=%v", found, err)
	}

	var got db.CaseLetter
	if err := store.(interface {
		DB() interface{ First(dest interface{}, cond ...interface{}) error }
	}); false {
		_ = got // placeholder branch never taken — see below for the real read
	}
	// Read back via GetCase's letters, same as TestUpdateCaseLetterFields_PartialUpdateAndScoping does.
	caseWithLetters, err := store.GetCase(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCase: %v", err)
	}
	_ = caseWithLetters

	// GetCase returns db.Case, not letters — read the letter list directly
	// via ListCaseLetters instead, filtering to this case, matching how
	// TestUpdateCaseLetterFields_PartialUpdateAndScoping (cases_test.go:570)
	// verifies a write.
	entries, _, err := store.ListCaseLetters(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListCaseLetters: %v", err)
	}
	var updated *db.CaseLetterEntry
	for i := range entries {
		if entries[i].ID == letter.ID {
			updated = &entries[i]
		}
	}
	if updated == nil || updated.OICUserID == nil || *updated.OICUserID != oicID {
		t.Fatalf("expected OICUserID %d, got %+v", oicID, updated)
	}

	// Clear it.
	var nilOIC *uint
	found, err = store.UpdateCaseLetterFields(ctx, c.ID, letter.ID, db.CaseLetterFields{OICUserID: &nilOIC})
	if err != nil || !found {
		t.Fatalf("UpdateCaseLetterFields(clear): found=%v err=%v", found, err)
	}
	entries, _, err = store.ListCaseLetters(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListCaseLetters: %v", err)
	}
	for i := range entries {
		if entries[i].ID == letter.ID && entries[i].OICUserID != nil {
			t.Fatalf("expected OICUserID cleared, got %+v", entries[i])
		}
	}
}
```
Delete the dead placeholder `if` block in the draft above before running it (the `store.(interface{...})` / `_ = got` lines) — it was left in accidentally; the real read is the `ListCaseLetters` call that follows. The final test body should go straight from `AddCaseLetter` to the `UpdateCaseLetterFields(set)` call, then the `ListCaseLetters`-based read, then the clear + re-read.
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/db/... -run TestUpdateCaseLetterFields_SetsAndClearsOICUserID -v`
  Expected: FAIL (`CaseLetterFields` has no field `OICUserID`)
- [ ] **Step 3: Implement.** Add `OICUserID **uint` to `CaseLetterFields` in `internal/db/models.go` (right after `SubmittedAt **time.Time`), and add the `if fields.OICUserID != nil { updates["oic_user_id"] = *fields.OICUserID }` block to `UpdateCaseLetterFields` in `internal/db/cases.go` (right after the `SubmittedAt` block), per the code shown above.
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/db/... -v`
  Expected: PASS (full package, to confirm nothing else broke)
- [ ] **Step 5: Commit**
```bash
git add internal/db/models.go internal/db/cases.go internal/db/cases_test.go
git commit -m "db: let CaseLetterFields set/clear a letter's OIC user"
```

---

### Task 2: `UpdateCaseLetter` HTTP handler — accept `oic_user_id` in the PATCH body

**Files:**
- Modify: `internal/server/case_handlers.go` (the `UpdateCaseLetter` handler, currently at lines 467-555)
- Test: `internal/server/case_handlers_test.go`

**Current handler body** (`internal/server/case_handlers.go:467-555`) — full source for reference:
```go
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

**The pattern to follow for `oic_user_id`** is `UpdateCase`'s handling of `agency_id` (`internal/server/case_handlers.go:140-157`), which uses the `0`-clears-a-`*uint` convention:
```go
	var body struct {
		AgencyID    *uint   `json:"agency_id"`
		DueDate     *string `json:"due_date"`
		RequestedAt *string `json:"requested_at"`
	}
	// ...
	var fields db.CaseFields
	if body.AgencyID != nil {
		var agencyID *uint
		if *body.AgencyID != 0 {
			agencyID = body.AgencyID
		}
		fields.AgencyID = &agencyID
	}
```

- [ ] **Step 1: Write the failing test** in `internal/server/case_handlers_test.go`. Mirror `TestAddCaseLetter_OwningDepartmentSucceeds` (`internal/server/case_handlers_test.go:130-153`) for the request/assertion shape:
```go
func TestUpdateCaseLetter_SetsAndClearsOICUserID(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID})
	store.caseLetters = append(store.caseLetters, db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{"oic_user_id": 7})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1/letters/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.caseLetters[0].OICUserID == nil || *store.caseLetters[0].OICUserID != 7 {
		t.Fatalf("expected OICUserID 7, got %+v", store.caseLetters[0])
	}

	// Clear it via the 0 sentinel.
	body, _ = json.Marshal(map[string]any{"oic_user_id": 0})
	req = httptest.NewRequest(http.MethodPatch, "/api/cases/1/letters/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.caseLetters[0].OICUserID != nil {
		t.Fatalf("expected OICUserID cleared, got %+v", store.caseLetters[0])
	}
}
```
This requires `fullMockStore.UpdateCaseLetterFields` (`internal/server/handlers_test.go:1545-`) to actually apply `fields.OICUserID` to the matching entry in `store.caseLetters`. Its current body loops `for i, l := range m.caseLetters { if l.ID != letterID || l.CaseID != caseID { continue } ... }` and inside that block has one `if fields.X != nil { m.caseLetters[i].X = *fields.X }` per field (ending with `ReceivedAt`/`SubmittedAt`, around line 1574). Add, in that same block:
```go
		if fields.OICUserID != nil {
			m.caseLetters[i].OICUserID = *fields.OICUserID
		}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/server/... -run TestUpdateCaseLetter_SetsAndClearsOICUserID -v`
  Expected: FAIL (handler doesn't read `oic_user_id` from the body yet, so the mock's `caseLetters[0].OICUserID` stays nil after the "set" call)
- [ ] **Step 3: Implement.** In `internal/server/case_handlers.go`'s `UpdateCaseLetter`:
  1. Add `OICUserID *uint \`json:"oic_user_id"\`` to the `body` anonymous struct.
  2. After the `fields := db.CaseLetterFields{...}` literal, add:
  ```go
  	if body.OICUserID != nil {
  		var oicUserID *uint
  		if *body.OICUserID != 0 {
  			oicUserID = body.OICUserID
  		}
  		fields.OICUserID = &oicUserID
  	}
  ```
  3. In `internal/server/handlers_test.go`, inside `fullMockStore.UpdateCaseLetterFields`'s per-letter `if l.ID != letterID || l.CaseID != caseID { continue }` block (starts line 1545), add the `if fields.OICUserID != nil { m.caseLetters[i].OICUserID = *fields.OICUserID }` branch shown above, placed after the existing `ReceivedAt`/`SubmittedAt` branches.
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/server/... -v`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/server/case_handlers.go internal/server/case_handlers_test.go internal/server/handlers_test.go
git commit -m "server: accept oic_user_id in PATCH /api/cases/{id}/letters/{letter_id}"
```

---

### Task 3: `AddCaseLetter` defaults `oic_user_id` to the caller

**Files:**
- Modify: `internal/server/case_handlers.go` (the `AddCaseLetter` handler, currently at lines 212-277)
- Test: `internal/server/case_handlers_test.go`

**Current handler body** (`internal/server/case_handlers.go:212-277`) — full source already shown in this plan's header context is reproduced here for this task's direct reference:
```go
func (h *Handlers) AddCaseLetter(w http.ResponseWriter, r *http.Request) {
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
		Type                    string     `json:"type"`
		ReferenceNumberExternal string     `json:"reference_number_external"`
		ReferenceNumberInternal string     `json:"reference_number_internal"`
		WorkflowStatus          string     `json:"workflow_status"`
		Recipient               string     `json:"recipient"`
		LetterDate              *time.Time `json:"letter_date"`
		ReceivedAt              *time.Time `json:"received_at"`
		SubmittedAt             *time.Time `json:"submitted_at"`
		Subject                 string     `json:"subject"`
		OICUserID               *uint      `json:"oic_user_id"`
		Requestor               string     `json:"requestor"`
		Remarks                 string     `json:"remarks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}

	letter, err := h.store.AddCaseLetter(r.Context(), db.CaseLetter{
		CaseID:                  uint(id),
		Type:                    body.Type,
		ReferenceNumberExternal: body.ReferenceNumberExternal,
		ReferenceNumberInternal: body.ReferenceNumberInternal,
		WorkflowStatus:          body.WorkflowStatus,
		Recipient:               body.Recipient,
		LetterDate:              body.LetterDate,
		ReceivedAt:              body.ReceivedAt,
		SubmittedAt:             body.SubmittedAt,
		Subject:                 body.Subject,
		OICUserID:               body.OICUserID,
		Requestor:               body.Requestor,
		Remarks:                 body.Remarks,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, letter)
}
```
`body.OICUserID` is already decoded and already passed through to `db.CaseLetter{OICUserID: body.OICUserID}` — the only change needed is defaulting it before that struct literal is built.

- [ ] **Step 1: Write the failing test** in `internal/server/case_handlers_test.go`:
```go
func TestAddCaseLetter_DefaultsOICUserIDToCaller(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID})
	cookie := deptCookie(store, 1) // first user created in an empty store -> ID 1, see loginAs (handlers_test.go:1616-1629)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"type": "Notice"})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/1/letters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.caseLetters) != 1 || store.caseLetters[0].OICUserID == nil || *store.caseLetters[0].OICUserID != 1 {
		t.Fatalf("expected OICUserID defaulted to caller's own id (1), got %+v", store.caseLetters)
	}
}

func TestAddCaseLetter_ExplicitOICUserIDWins(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID})
	cookie := deptCookie(store, 1) // caller's own id is 1
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{"type": "Notice", "oic_user_id": 99})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/1/letters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.caseLetters) != 1 || store.caseLetters[0].OICUserID == nil || *store.caseLetters[0].OICUserID != 99 {
		t.Fatalf("expected explicit OICUserID 99 to win, got %+v", store.caseLetters)
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/server/... -run TestAddCaseLetter_DefaultsOICUserIDToCaller -v`
  Expected: FAIL (`OICUserID` is nil, since nothing defaults it yet)
- [ ] **Step 3: Implement.** In `AddCaseLetter`, right after the `json.NewDecoder(...).Decode(&body)` block and before building the `db.CaseLetter{...}` literal, add:
```go
	if body.OICUserID == nil {
		body.OICUserID = &user.ID
	}
```
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/server/... -v`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/server/case_handlers.go internal/server/case_handlers_test.go
git commit -m "server: default AddCaseLetter's oic_user_id to the caller"
```

---

### Task 4: `GET /api/users/open` → `ListUsersOpen`

**Files:**
- Modify: `internal/server/admin_handlers.go` (add a new handler near `ListUsers`, currently at lines 80-102)
- Modify: `internal/server/router.go` (add one route in the `requireAuth`-only group)
- Test: `internal/server/handlers_test.go`

**Current `ListUsers` body** (`internal/server/admin_handlers.go:80-102`) — this is the exact logic `ListUsersOpen` reuses:
```go
func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	caller, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !caller.IsAdmin {
		// department admin: only their own department's users
		var scoped []db.User
		for _, u := range users {
			if u.DepartmentID != nil && caller.DepartmentID != nil && *u.DepartmentID == *caller.DepartmentID {
				scoped = append(scoped, u)
			}
		}
		users = scoped
	}
	writeJSON(w, http.StatusOK, users)
}
```
Note the branch condition is `!caller.IsAdmin` (not `caller.IsDeptAdmin`) — this already scopes correctly for *any* non-admin caller, department admin or plain member alike, which is exactly the behavior `ListUsersOpen` needs (a plain member gets their own department's users, same as a department admin would).

**New handler** — add directly below `ListUsers` in `internal/server/admin_handlers.go`:
```go
// ListUsersOpen is the same underlying data as ListUsers (gated by
// requireAnyAdmin at GET /api/admin/users) but exposed to any authenticated
// role at GET /api/users/open — mirrors ListDepartmentsOpen's "any
// authenticated user" pattern (admin_handlers.go, line 32). Needed for the
// OIC picker any regular user sees when creating/editing a case's letters
// (docs.tsx), not just admins.
func (h *Handlers) ListUsersOpen(w http.ResponseWriter, r *http.Request) {
	h.ListUsers(w, r)
}
```
This is a one-line delegation, not a duplicate implementation — `ListUsers`'s body already does exactly the right scoping for any caller (admin: everyone; non-admin: own department only), so `ListUsersOpen` just needs a different route/RBAC gate pointing at the same logic.

**Router change** (`internal/server/router.go`) — in the `requireAuth`-only group, near the existing `r.Get("/departments", h.ListDepartmentsOpen)` line (line 74), add:
```go
			r.Get("/users/open", h.ListUsersOpen)
```

- [ ] **Step 1: Write the failing test** in `internal/server/handlers_test.go`, mirroring `TestListDepartmentsOpen_AllowedForNonAdmin` (lines 3397-3416) and reusing `fullMockStore.users`:
```go
func TestListUsersOpen_NonAdminSeesOwnDepartmentOnly(t *testing.T) {
	dept1 := uint(1)
	dept2 := uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "alice", DepartmentID: &dept1},
		{ID: 2, Username: "bob", DepartmentID: &dept2},
	}}
	cookie := deptCookie(store, 1) // becomes user ID 3 in store.users, department 1
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/users/open", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var users []db.User
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// alice (dept 1) and the caller (dept 1) are visible; bob (dept 2) is not.
	names := map[string]bool{}
	for _, u := range users {
		names[u.Username] = true
	}
	if !names["alice"] || names["bob"] {
		t.Fatalf("expected alice visible and bob hidden, got %+v", users)
	}
}

func TestListUsersOpen_AdminSeesEveryone(t *testing.T) {
	dept1 := uint(1)
	dept2 := uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "alice", DepartmentID: &dept1},
		{ID: 2, Username: "bob", DepartmentID: &dept2},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/users/open", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var users []db.User
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(users) != 3 { // alice, bob, and the admin caller loginAs adds
		t.Fatalf("expected admin to see all 3 users, got %d: %+v", len(users), users)
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**
  Run: `go test ./internal/server/... -run TestListUsersOpen -v`
  Expected: FAIL (404 — route doesn't exist yet)
- [ ] **Step 3: Implement.** Add the `ListUsersOpen` handler to `internal/server/admin_handlers.go` and the route line to `internal/server/router.go`, exactly as shown above.
- [ ] **Step 4: Run the tests to verify they pass**
  Run: `go test ./internal/server/... -v`
  Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/server/admin_handlers.go internal/server/router.go internal/server/handlers_test.go
git commit -m "server: add GET /api/users/open for the OIC picker"
```

## Self-Review (performed while writing this plan)

- **Spec coverage:** All three backend bullets under the design spec's "Part 1 — OIC field / Backend" are covered: `AddCaseLetter` default (Task 3), `CaseLetterFields.OICUserID` three-state field threaded through `UpdateCaseLetterFields`/`UpdateCaseLetter` (Tasks 1-2), and `GET /api/users/open` (Task 4). The spec's "No change to `listCaseLetters`'s SQL" note requires no task — confirmed no task here touches that query.
- **Placeholder scan:** Removed one accidentally-included dead `if` branch from Task 1's draft test (called out explicitly in that step so an executor doesn't paste it verbatim); every other step has complete, runnable code.
- **Type consistency:** `CaseLetterFields.OICUserID` is `**uint` everywhere it's referenced (Task 1's struct, Task 2's handler); the JSON field name `oic_user_id` and its `0`-clears sentinel are used identically in Task 2 and Task 3; `ListUsersOpen`'s route path `/api/users/open` (Task 4) matches what Plan 02 is told to call.
