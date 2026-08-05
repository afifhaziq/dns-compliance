# Legal Sub-Element Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a fifth level, `SubElement`, to the legal citation catalog (`Instrument → Citation → Category → Element → SubElement`), mirroring `Element` exactly at every layer (model, store, HTTP, frontend), manageable on `/legal-citations` and optionally selectable in `MultiOffencePicker`.

**Architecture:** `SubElement` is a plain optional child of `Element` — same shape as `Element` is to `Category`. `URLOffence` gains an optional `SubElementID *uint` / `SubElement *SubElement`, mirroring its existing `ElementID`/`Element` fields. Every layer (Go model → `db.Store` interface → `postgresStore` impl → HTTP handler → chi route → TS type → API client → React tree UI → offence picker) gets the same one-level-deeper addition that `Element` already has under `Category`.

**Tech Stack:** Go + GORM (backend), React + TypeScript + TanStack Router (frontend), chi router, SQLite in-memory for Go tests.

## Global Constraints

- Mirror `Element`'s existing pattern exactly at every layer — do not invent new conventions (spec: "Decision" section).
- `SubElement` is optional on `URLOffence`, same nullable-FK pattern as `ElementID` (nullable because `NULL != NULL` breaks composite-PK uniqueness — not relevant here since `URLOffence` already uses a surrogate ID PK, but the nullability itself must match).
- No nesting beyond `SubElement` — five levels is the ceiling (spec: "Out of Scope").
- Do not change existing `Element`/`Category` behavior or their existing tests.
- Read routes for the catalog stay open to any authenticated role; mutation routes stay admin-or-dept-admin (`requireAnyAdmin`), matching every other catalog level.
- `go test ./...` and `cd web && npm run build && npm run lint` must pass before this is considered done (per repo's CLAUDE.md).

---

## Task 1: Backend data layer — model, migration, store interface, store implementation, DB tests

**Files:**
- Modify: `internal/db/models.go` (add `SubElement` struct after `Element`, extend `URLOffence`)
- Modify: `internal/db/db.go` (add `&SubElement{}` to `AutoMigrate`)
- Modify: `internal/db/store.go` (add `SubElement` methods to `LegalCitationStore`, extend `AttachOffenceToURL` signature)
- Modify: `internal/db/legalcite.go` (add `SubElement` CRUD, update `AttachOffenceToURL`/`ListOffencesByURL`)
- Modify: `internal/db/legalcite_test.go` (new tests mirroring the existing `Element` tests)

**Interfaces:**
- Produces: `db.SubElement{ID, ElementID, Element, Name, CreatedAt}`; `db.Store.ListSubElementsByElement(ctx, elementID uint) ([]SubElement, error)`; `CreateSubElement(ctx, se SubElement) (SubElement, error)`; `UpdateSubElement(ctx, id uint, name string) (SubElement, error)`; `DeleteSubElement(ctx, id uint) error`; `AttachOffenceToURL(ctx, urlValue string, categoryID uint, elementID *uint, subElementID *uint) (URLOffence, error)` (signature change — 5th param added, all callers updated in this task and Task 2).
- Consumes: nothing new from other tasks (this is the base layer).

- [ ] **Step 1: Add the `SubElement` model and extend `URLOffence`**

In `internal/db/models.go`, insert immediately after the `Element` struct (after line 424, before the `URLOffence` doc comment):

```go
// SubElement is an optional sub-category of an Element — not every element
// has one, mirroring how not every Category has an Element.
type SubElement struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ElementID uint      `gorm:"not null;index" json:"element_id"`
	Element   Element   `gorm:"foreignKey:ElementID;constraint:OnDelete:CASCADE" json:"-"`
	Name      string    `gorm:"not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}
```

Then update the `URLOffence` struct (currently lines 430-439) to add the optional sub-element fields, mirroring `ElementID`/`Element`:

```go
// URLOffence links a URL to the specific offence it committed, at Category
// granularity with an optional Element and, one level deeper, an optional
// SubElement. Uses a surrogate ID PK rather than a composite one (unlike
// DepartmentURL) because ElementID/SubElementID are nullable and SQL
// NULL != NULL breaks composite-PK uniqueness semantics.
type URLOffence struct {
	ID           uint        `gorm:"primaryKey" json:"id"`
	URLID        uint        `gorm:"not null;index" json:"url_id"`
	URL          URL         `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	CategoryID   uint        `gorm:"not null;index" json:"category_id"`
	Category     Category    `gorm:"foreignKey:CategoryID;constraint:OnDelete:CASCADE" json:"category"`
	ElementID    *uint       `gorm:"index" json:"element_id,omitempty"`
	Element      *Element    `gorm:"foreignKey:ElementID;constraint:OnDelete:CASCADE" json:"element,omitempty"`
	SubElementID *uint       `gorm:"index" json:"sub_element_id,omitempty"`
	SubElement   *SubElement `gorm:"foreignKey:SubElementID;constraint:OnDelete:CASCADE" json:"sub_element,omitempty"`
	RecordedAt   time.Time   `gorm:"not null" json:"recorded_at"`
}
```

- [ ] **Step 2: Register `SubElement` in `AutoMigrate`**

In `internal/db/db.go`, change:

```go
	if err := database.AutoMigrate(
		&Department{}, &User{}, &Session{}, &DNSServer{}, &URL{}, &DepartmentURL{}, &ScanRun{}, &ScanResult{}, &CompliantIP{}, &DomainWhois{}, &IPInfo{}, &Favicon{}, &ScanSettings{}, &SubdomainScan{}, &ISPLogo{},
		&Instrument{}, &Citation{}, &Category{}, &Element{}, &URLOffence{},
	); err != nil {
```

to:

```go
	if err := database.AutoMigrate(
		&Department{}, &User{}, &Session{}, &DNSServer{}, &URL{}, &DepartmentURL{}, &ScanRun{}, &ScanResult{}, &CompliantIP{}, &DomainWhois{}, &IPInfo{}, &Favicon{}, &ScanSettings{}, &SubdomainScan{}, &ISPLogo{},
		&Instrument{}, &Citation{}, &Category{}, &Element{}, &SubElement{}, &URLOffence{},
	); err != nil {
```

- [ ] **Step 3: Add `SubElement` methods + extend `AttachOffenceToURL` in `db.Store`'s `LegalCitationStore`**

In `internal/db/store.go`, change the `DeleteElement` comment and insert a new block right after it (currently lines 202-205):

```go
	ListElementsByCategory(ctx context.Context, categoryID uint) ([]Element, error)
	CreateElement(ctx context.Context, el Element) (Element, error)
	UpdateElement(ctx context.Context, id uint, name string) (Element, error)
	DeleteElement(ctx context.Context, id uint) error // cascades to SubElement/URLOffence

	ListSubElementsByElement(ctx context.Context, elementID uint) ([]SubElement, error)
	CreateSubElement(ctx context.Context, se SubElement) (SubElement, error)
	UpdateSubElement(ctx context.Context, id uint, name string) (SubElement, error)
	DeleteSubElement(ctx context.Context, id uint) error // cascades to URLOffence
```

Then update the `AttachOffenceToURL` line (currently line 216) from:

```go
	AttachOffenceToURL(ctx context.Context, urlValue string, categoryID uint, elementID *uint) (URLOffence, error)
```

to:

```go
	AttachOffenceToURL(ctx context.Context, urlValue string, categoryID uint, elementID *uint, subElementID *uint) (URLOffence, error)
```

- [ ] **Step 4: Implement `SubElement` CRUD + update `AttachOffenceToURL`/`ListOffencesByURL` in `internal/db/legalcite.go`**

Insert immediately after `DeleteElement` (currently lines 121-123), before `ListOffencesByURL`:

```go
func (s *postgresStore) ListSubElementsByElement(ctx context.Context, elementID uint) ([]SubElement, error) {
	var subElements []SubElement
	return subElements, s.db.WithContext(ctx).Where("element_id = ?", elementID).Order("name asc").Find(&subElements).Error
}

func (s *postgresStore) CreateSubElement(ctx context.Context, se SubElement) (SubElement, error) {
	se.ID = 0
	return se, s.db.WithContext(ctx).Create(&se).Error
}

func (s *postgresStore) UpdateSubElement(ctx context.Context, id uint, name string) (SubElement, error) {
	err := s.db.WithContext(ctx).Model(&SubElement{}).Where("id = ?", id).Update("name", name).Error
	if err != nil {
		return SubElement{}, err
	}
	var se SubElement
	return se, s.db.WithContext(ctx).First(&se, id).Error
}

func (s *postgresStore) DeleteSubElement(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&SubElement{}, id).Error
}
```

Update `ListOffencesByURL` to also preload `SubElement` (add one line to the existing chain):

```go
func (s *postgresStore) ListOffencesByURL(ctx context.Context, urlValue string) ([]URLOffence, error) {
	var offences []URLOffence
	err := s.db.WithContext(ctx).
		Joins("JOIN urls ON urls.id = url_offences.url_id").
		Where("urls.url = ?", urlValue).
		Preload("Category.Citation.Instrument").
		Preload("Element").
		Preload("SubElement").
		Order("url_offences.recorded_at desc").
		Find(&offences).Error
	return offences, err
}
```

Update `AttachOffenceToURL` to accept and set `SubElementID`:

```go
func (s *postgresStore) AttachOffenceToURL(ctx context.Context, urlValue string, categoryID uint, elementID *uint, subElementID *uint) (URLOffence, error) {
	u, err := s.GetURLByValue(ctx, urlValue)
	if err != nil {
		return URLOffence{}, err
	}
	if u == nil {
		return URLOffence{}, gorm.ErrRecordNotFound
	}
	o := URLOffence{URLID: u.ID, CategoryID: categoryID, ElementID: elementID, SubElementID: subElementID, RecordedAt: time.Now()}
	return o, s.db.WithContext(ctx).Create(&o).Error
}
```

- [ ] **Step 5: Write DB-layer tests mirroring the existing `Element` tests**

Append to `internal/db/legalcite_test.go` (after `TestCategoryAndElementCRUD`, i.e. at the end of the file):

```go
// seedCMA233WithSubElement extends seedCMA233 with one SubElement under the
// Element it creates — a separate helper (not a signature change to
// seedCMA233 itself) since seedCMA233 is already called by six existing
// tests that destructure exactly 4 return values.
func seedCMA233WithSubElement(t *testing.T, s db.Store, ctx context.Context) (db.Instrument, db.Citation, db.Category, db.Element, db.SubElement) {
	t.Helper()
	instrument, citation, category, element := seedCMA233(t, s, ctx)
	subElement, err := s.CreateSubElement(ctx, db.SubElement{ElementID: element.ID, Name: "Direct Threat"})
	if err != nil {
		t.Fatalf("CreateSubElement: %v", err)
	}
	return instrument, citation, category, element, subElement
}

func TestAttachOffenceToURL_WithSubElement(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _, category, element, subElement := seedCMA233WithSubElement(t, s, ctx)

	if _, err := s.CreateURL(ctx, "https://example.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID, &subElement.ID)
	if err != nil {
		t.Fatalf("AttachOffenceToURL: %v", err)
	}
	if offence.SubElementID == nil || *offence.SubElementID != subElement.ID {
		t.Fatalf("expected SubElementID %d, got %v", subElement.ID, offence.SubElementID)
	}

	offences, err := s.ListOffencesByURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListOffencesByURL: %v", err)
	}
	if len(offences) != 1 {
		t.Fatalf("expected 1 offence, got %d", len(offences))
	}
	if offences[0].SubElement == nil || offences[0].SubElement.Name != "Direct Threat" {
		t.Fatalf("expected preloaded SubElement, got %+v", offences[0].SubElement)
	}
}

func TestAttachOffenceToURL_NilSubElementAllowed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _, category, element, _ := seedCMA233WithSubElement(t, s, ctx)

	if _, err := s.CreateURL(ctx, "https://example.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID, nil)
	if err != nil {
		t.Fatalf("AttachOffenceToURL: %v", err)
	}
	if offence.SubElementID != nil {
		t.Fatalf("expected nil SubElementID, got %v", offence.SubElementID)
	}

	offences, err := s.ListOffencesByURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListOffencesByURL: %v", err)
	}
	if len(offences) != 1 || offences[0].SubElement != nil {
		t.Fatalf("expected 1 offence with nil SubElement, got %+v", offences)
	}
}

func TestSubElementCRUD(t *testing.T) {
	s := newCascadeTestStore(t)
	ctx := context.Background()
	_, _, _, element, subElement := seedCMA233WithSubElement(t, s, ctx)

	updated, err := s.UpdateSubElement(ctx, subElement.ID, "Menacing Threat")
	if err != nil {
		t.Fatalf("UpdateSubElement: %v", err)
	}
	if updated.Name != "Menacing Threat" {
		t.Fatalf("expected updated name, got %q", updated.Name)
	}

	second, err := s.CreateSubElement(ctx, db.SubElement{ElementID: element.ID, Name: "Implied Threat"})
	if err != nil {
		t.Fatalf("CreateSubElement: %v", err)
	}

	subElements, err := s.ListSubElementsByElement(ctx, element.ID)
	if err != nil {
		t.Fatalf("ListSubElementsByElement: %v", err)
	}
	if len(subElements) != 2 {
		t.Fatalf("expected 2 sub-elements, got %d", len(subElements))
	}

	if err := s.DeleteSubElement(ctx, second.ID); err != nil {
		t.Fatalf("DeleteSubElement: %v", err)
	}
	subElements, err = s.ListSubElementsByElement(ctx, element.ID)
	if err != nil {
		t.Fatalf("ListSubElementsByElement: %v", err)
	}
	if len(subElements) != 1 {
		t.Fatalf("expected 1 sub-element after delete, got %d", len(subElements))
	}
}

func TestDeleteElement_CascadesToSubElementAndOffence(t *testing.T) {
	s := newCascadeTestStore(t)
	ctx := context.Background()
	_, _, category, element, subElement := seedCMA233WithSubElement(t, s, ctx)
	if _, err := s.CreateURL(ctx, "https://example.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	if _, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID, &subElement.ID); err != nil {
		t.Fatalf("AttachOffenceToURL: %v", err)
	}

	if err := s.DeleteElement(ctx, element.ID); err != nil {
		t.Fatalf("DeleteElement: %v", err)
	}

	subElements, err := s.ListSubElementsByElement(ctx, element.ID)
	if err != nil {
		t.Fatalf("ListSubElementsByElement: %v", err)
	}
	if len(subElements) != 0 {
		t.Fatalf("expected sub-elements to cascade-delete with element, got %d", len(subElements))
	}
	offences, err := s.ListOffencesByURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListOffencesByURL: %v", err)
	}
	if len(offences) != 0 {
		t.Fatalf("expected url_offence to cascade-delete, got %d", len(offences))
	}
}

func TestDeleteInstrument_CascadesThroughSubElement(t *testing.T) {
	s := newCascadeTestStore(t)
	ctx := context.Background()
	instrument, _, category, element, subElement := seedCMA233WithSubElement(t, s, ctx)
	if _, err := s.CreateURL(ctx, "https://example.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	if _, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID, &subElement.ID); err != nil {
		t.Fatalf("AttachOffenceToURL: %v", err)
	}

	if err := s.DeleteInstrument(ctx, instrument.ID); err != nil {
		t.Fatalf("DeleteInstrument: %v", err)
	}

	subElements, err := s.ListSubElementsByElement(ctx, element.ID)
	if err != nil {
		t.Fatalf("ListSubElementsByElement: %v", err)
	}
	if len(subElements) != 0 {
		t.Fatalf("expected sub-elements to cascade-delete through the whole chain, got %d", len(subElements))
	}
}
```

Also update the 5 existing call sites of `AttachOffenceToURL` elsewhere in this same test file to pass a trailing `nil` for the new `subElementID` parameter (run `grep -n "AttachOffenceToURL(ctx" internal/db/legalcite_test.go` to relocate them if line numbers have shifted):

In `TestAttachAndListOffencesByURL`, change `offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID)` to `offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID, nil)`.

In `TestAttachOffenceToURL_NilElementAllowed`, change `offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, nil)` to `offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, nil, nil)`.

In `TestDetachOffenceFromURL`, change `offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID)` to `offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID, nil)`.

In `TestDeleteInstrument_CascadesThroughWholeChain`, change `if _, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID); err != nil {` to `if _, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID, nil); err != nil {`.

In `TestGetOffence_PreloadsURL`, change `offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID)` to `offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID, nil)`.

- [ ] **Step 6: Run the DB package tests**

Run: `go test ./internal/db/...`
Expected: PASS (all existing + new tests). If it fails to compile, the most likely cause is a stale `AttachOffenceToURL` call site missing the new 5th argument — fix each one.

- [ ] **Step 7: Commit**

```bash
git add internal/db/models.go internal/db/db.go internal/db/store.go internal/db/legalcite.go internal/db/legalcite_test.go
git commit -m "feat: add SubElement level to legal citation catalog (data layer)"
```

---

## Task 2: HTTP handlers, router wiring, mock store, handler tests

**Files:**
- Modify: `internal/server/legal_handlers.go` (add `SubElement` handlers, extend `AttachOffence` body)
- Modify: `internal/server/router.go` (wire new routes)
- Modify: `internal/server/handlers_test.go` (extend `fullMockStore` with `SubElement` support)
- Modify: `internal/server/legal_handlers_test.go` (new tests mirroring the `Element`/offence tests)

**Interfaces:**
- Consumes: `db.SubElement`, `db.Store.ListSubElementsByElement/CreateSubElement/UpdateSubElement/DeleteSubElement`, the 5-arg `AttachOffenceToURL` (all from Task 1).
- Produces: `Handlers.ListSubElementsByElement/CreateSubElement/UpdateSubElement/DeleteSubElement` HTTP handlers; routes `GET /api/legal/elements/{id}/subelements` (open), `POST/PATCH/DELETE /api/legal/subelements[/{id}]` (admin-or-dept-admin); `AttachOffence` accepts `sub_element_id` in its request body.

- [ ] **Step 1: Add `SubElement` HTTP handlers**

In `internal/server/legal_handlers.go`, insert a new `// SubElements` section immediately after the `Elements` section ends (after `DeleteElement`, currently ending at line 348, before the `// URL <-> offence linking` comment):

```go
// SubElements

func (h *Handlers) ListSubElementsByElement(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	subElements, err := h.store.ListSubElementsByElement(r.Context(), uint(id))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, subElements)
}

func (h *Handlers) CreateSubElement(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ElementID uint   `json:"element_id"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ElementID == 0 || body.Name == "" {
		writeError(w, http.StatusBadRequest, "element_id and name are required")
		return
	}
	subElement, err := h.store.CreateSubElement(r.Context(), db.SubElement{ElementID: body.ElementID, Name: body.Name})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, subElement)
}

func (h *Handlers) UpdateSubElement(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	subElement, err := h.store.UpdateSubElement(r.Context(), uint(id), body.Name)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, subElement)
}

func (h *Handlers) DeleteSubElement(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteSubElement(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 2: Extend `AttachOffence`'s request body with `sub_element_id`**

In `internal/server/legal_handlers.go`, in `AttachOffence` (currently lines 371-394), change:

```go
	var body struct {
		CategoryID uint  `json:"category_id"`
		ElementID  *uint `json:"element_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CategoryID == 0 {
		writeError(w, http.StatusBadRequest, "category_id is required")
		return
	}
	offence, err := h.store.AttachOffenceToURL(r.Context(), urlValue, body.CategoryID, body.ElementID)
```

to:

```go
	var body struct {
		CategoryID   uint  `json:"category_id"`
		ElementID    *uint `json:"element_id"`
		SubElementID *uint `json:"sub_element_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CategoryID == 0 {
		writeError(w, http.StatusBadRequest, "category_id is required")
		return
	}
	offence, err := h.store.AttachOffenceToURL(r.Context(), urlValue, body.CategoryID, body.ElementID, body.SubElementID)
```

- [ ] **Step 3: Wire the new routes**

In `internal/server/router.go`, add the open read route right after `r.Get("/legal/categories/{id}/elements", h.ListElementsByCategory)` (currently line 90):

```go
			r.Get("/legal/categories/{id}/elements", h.ListElementsByCategory)
			r.Get("/legal/elements/{id}/subelements", h.ListSubElementsByElement)
```

And add the admin-gated mutation routes right after `r.Delete("/legal/elements/{id}", h.DeleteElement)` (currently line 125), inside the same `requireAnyAdmin` group:

```go
				r.Post("/legal/elements", h.CreateElement)
				r.Patch("/legal/elements/{id}", h.UpdateElement)
				r.Delete("/legal/elements/{id}", h.DeleteElement)
				r.Post("/legal/subelements", h.CreateSubElement)
				r.Patch("/legal/subelements/{id}", h.UpdateSubElement)
				r.Delete("/legal/subelements/{id}", h.DeleteSubElement)
```

- [ ] **Step 4: Extend `fullMockStore` with `SubElement` support**

In `internal/server/handlers_test.go`:

Add a field to the mock store struct (right after the `elements []db.Element` field, currently line 41):

```go
	elements       []db.Element
	subElements    []db.SubElement
```

Add the mock CRUD methods right after `DeleteElement`/`deleteElementCascade` (currently ending at line 625, before `hydrateOffence`):

```go
func (m *fullMockStore) ListSubElementsByElement(_ context.Context, elementID uint) ([]db.SubElement, error) {
	var out []db.SubElement
	for _, se := range m.subElements {
		if se.ElementID == elementID {
			out = append(out, se)
		}
	}
	return out, nil
}
func (m *fullMockStore) CreateSubElement(_ context.Context, se db.SubElement) (db.SubElement, error) {
	se.ID = uint(len(m.subElements) + 1)
	m.subElements = append(m.subElements, se)
	return se, nil
}
func (m *fullMockStore) UpdateSubElement(_ context.Context, id uint, name string) (db.SubElement, error) {
	for i, se := range m.subElements {
		if se.ID == id {
			m.subElements[i].Name = name
			return m.subElements[i], nil
		}
	}
	return db.SubElement{}, nil
}
func (m *fullMockStore) DeleteSubElement(_ context.Context, id uint) error {
	m.deleteSubElementCascade(id)
	return nil
}
func (m *fullMockStore) deleteSubElementCascade(id uint) {
	for i, se := range m.subElements {
		if se.ID == id {
			m.subElements = append(m.subElements[:i], m.subElements[i+1:]...)
		}
	}
	for i := 0; i < len(m.urlOffences); i++ {
		if m.urlOffences[i].SubElementID != nil && *m.urlOffences[i].SubElementID == id {
			m.urlOffences = append(m.urlOffences[:i], m.urlOffences[i+1:]...)
			i--
		}
	}
}
```

Update `deleteElementCascade` (currently lines 613-625) to also cascade into `subElements`, by adding a loop before the existing `urlOffences` loop:

```go
func (m *fullMockStore) deleteElementCascade(id uint) {
	for i, el := range m.elements {
		if el.ID == id {
			m.elements = append(m.elements[:i], m.elements[i+1:]...)
		}
	}
	for _, se := range m.subElements {
		if se.ElementID == id {
			m.deleteSubElementCascade(se.ID)
		}
	}
	for i := 0; i < len(m.urlOffences); i++ {
		if m.urlOffences[i].ElementID != nil && *m.urlOffences[i].ElementID == id {
			m.urlOffences = append(m.urlOffences[:i], m.urlOffences[i+1:]...)
			i--
		}
	}
}
```

Update `hydrateOffence` (currently lines 630-660) to also hydrate `SubElement`, by adding a block after the existing `ElementID` block:

```go
	if o.ElementID != nil {
		for _, el := range m.elements {
			if el.ID == *o.ElementID {
				elCopy := el
				o.Element = &elCopy
			}
		}
	}
	if o.SubElementID != nil {
		for _, se := range m.subElements {
			if se.ID == *o.SubElementID {
				seCopy := se
				o.SubElement = &seCopy
			}
		}
	}
```

Update `AttachOffenceToURL`'s mock signature and body (currently lines 696-718) to accept and store the 5th param:

```go
func (m *fullMockStore) AttachOffenceToURL(_ context.Context, urlValue string, categoryID uint, elementID *uint, subElementID *uint) (db.URLOffence, error) {
	normalized, err := urlnorm.Normalize(urlValue)
	if err != nil {
		return db.URLOffence{}, err
	}
	var urlID uint
	found := false
	for _, u := range m.urls {
		if u.URL == normalized {
			urlID = u.ID
			found = true
			break
		}
	}
	if !found {
		return db.URLOffence{}, fmt.Errorf("url not found: %s", urlValue)
	}
	o := db.URLOffence{
		ID: uint(len(m.urlOffences) + 1), URLID: urlID, CategoryID: categoryID, ElementID: elementID, SubElementID: subElementID, RecordedAt: time.Now(),
	}
	m.urlOffences = append(m.urlOffences, o)
	return o, nil
}
```

- [ ] **Step 5: Run the server package to check for compile errors**

Run: `go build ./...`
Expected: succeeds. (This surfaces any remaining `AttachOffenceToURL(...)` call sites in non-test code that still use the old 4-arg signature — there should be none outside `internal/db/legalcite.go` and `internal/server/legal_handlers.go`, both already updated.)

- [ ] **Step 6: Write handler tests mirroring the existing instrument/offence tests**

Append to `internal/server/legal_handlers_test.go` (at the end of the file):

```go
func TestCreateSubElement_ForbiddenForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	store.categories = append(store.categories, db.Category{ID: 1, CitationID: 1, Name: "Harassment"})
	store.elements = append(store.elements, db.Element{ID: 1, CategoryID: 1, Name: "Menacing"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]any{"element_id": 1, "name": "Direct Threat"})
	req := httptest.NewRequest(http.MethodPost, "/api/legal/subelements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSubElementCRUD_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{}
	store.categories = append(store.categories, db.Category{ID: 1, CitationID: 1, Name: "Harassment"})
	store.elements = append(store.elements, db.Element{ID: 1, CategoryID: 1, Name: "Menacing"})
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	createBody, _ := json.Marshal(map[string]any{"element_id": 1, "name": "Direct Threat"})
	req := httptest.NewRequest(http.MethodPost, "/api/legal/subelements", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created db.SubElement
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/legal/elements/1/subelements", nil)
	listReq.AddCookie(cookie)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listed []db.SubElement
	if err := json.Unmarshal(listW.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "Direct Threat" {
		t.Fatalf("expected 1 sub-element named Direct Threat, got %+v", listed)
	}

	patchBody, _ := json.Marshal(map[string]string{"name": "Explicit Threat"})
	patchReq := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/legal/subelements/%d", created.ID), bytes.NewReader(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.AddCookie(cookie)
	patchW := httptest.NewRecorder()
	r.ServeHTTP(patchW, patchReq)
	if patchW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", patchW.Code, patchW.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/legal/subelements/%d", created.ID), nil)
	delReq.AddCookie(cookie)
	delW := httptest.NewRecorder()
	r.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", delW.Code, delW.Body.String())
	}
}

// seedOffenceWithSubElement is like seedOffence but also wires a SubElement
// under the seeded Element and attaches the offence with it — a separate
// helper (not a change to seedOffence) since seedOffence is already reused
// by several tests above that don't need a sub-element.
func seedOffenceWithSubElement(store *fullMockStore, departmentID uint) (urlValue string, offenceID uint) {
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: departmentID, URLID: u.ID, Enabled: true})

	instrument := db.Instrument{ID: 1, Type: "ACT", Jurisdiction: "FEDERAL", Number: "588", ShortTitle: "CMA 1998"}
	store.instruments = append(store.instruments, instrument)
	citation := db.Citation{ID: 1, InstrumentID: 1, RawText: "Seksyen 233(1)(a)", ParseConfidence: "OK"}
	store.citations = append(store.citations, citation)
	category := db.Category{ID: 1, CitationID: 1, Name: "Harassment"}
	store.categories = append(store.categories, category)
	element := db.Element{ID: 1, CategoryID: 1, Name: "Menacing"}
	store.elements = append(store.elements, element)
	subElement := db.SubElement{ID: 1, ElementID: 1, Name: "Direct Threat"}
	store.subElements = append(store.subElements, subElement)

	offence := db.URLOffence{ID: 1, URLID: u.ID, CategoryID: 1, ElementID: &element.ID, SubElementID: &subElement.ID}
	store.urlOffences = append(store.urlOffences, offence)
	return u.URL, offence.ID
}

func TestOffencesByURL_HydratesSubElement(t *testing.T) {
	store := &fullMockStore{}
	urlValue, _ := seedOffenceWithSubElement(store, 1)
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/legal/offences/"+urlValue, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var offences []db.URLOffence
	if err := json.Unmarshal(w.Body.Bytes(), &offences); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(offences) != 1 || offences[0].SubElement == nil || offences[0].SubElement.Name != "Direct Threat" {
		t.Fatalf("expected hydrated SubElement, got %+v", offences)
	}
}

func TestAttachOffence_WithSubElementID(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	store.categories = append(store.categories, db.Category{ID: 1, CitationID: 1, Name: "Harassment"})
	store.elements = append(store.elements, db.Element{ID: 1, CategoryID: 1, Name: "Menacing"})
	store.subElements = append(store.subElements, db.SubElement{ID: 1, ElementID: 1, Name: "Direct Threat"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]uint{"category_id": 1, "element_id": 1, "sub_element_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/legal/offences/example.com", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var offence db.URLOffence
	if err := json.Unmarshal(w.Body.Bytes(), &offence); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if offence.SubElementID == nil || *offence.SubElementID != 1 {
		t.Fatalf("expected SubElementID 1, got %v", offence.SubElementID)
	}
}
```

`internal/server/legal_handlers_test.go` does not currently import `"fmt"` — add it to the import block (needed for `fmt.Sprintf` in `TestSubElementCRUD_AllowedForDeptAdmin`):

```go
import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
)
```

- [ ] **Step 7: Run the server package tests**

Run: `go test ./internal/server/...`
Expected: PASS (all existing + new tests).

- [ ] **Step 8: Commit**

```bash
git add internal/server/legal_handlers.go internal/server/router.go internal/server/handlers_test.go internal/server/legal_handlers_test.go
git commit -m "feat: add SubElement HTTP routes and handler tests"
```

---

## Task 3: Frontend types + API client

**Files:**
- Modify: `web/src/api/types.ts` (add `LegalSubElement`, extend `URLOffence`)
- Modify: `web/src/api/legal.ts` (add `fetchSubElements`/`createSubElement`/`updateSubElement`/`deleteSubElement`, extend `attachOffence`)

**Interfaces:**
- Consumes: nothing (pure frontend types/API mirroring Task 1/2's wire shapes).
- Produces: `LegalSubElement`; `URLOffence.sub_element_id?`/`sub_element?`; `fetchSubElements(elementId: number): Promise<LegalSubElement[]>`; `createSubElement(elementId: number, name: string): Promise<LegalSubElement>`; `updateSubElement(id: number, name: string): Promise<LegalSubElement>`; `deleteSubElement(id: number): Promise<void>`; `attachOffence(url: string, categoryId: number, elementId?: number, subElementId?: number): Promise<URLOffence>` (signature change — 4th param added; both call sites updated in Task 5).

- [ ] **Step 1: Add `LegalSubElement` type and extend `URLOffence`**

In `web/src/api/types.ts`, insert right after the `LegalElement` type (currently lines 205-211):

```ts
// Named LegalSubElement (not SubElement) for the same DOM-shadowing reason
// LegalElement avoids Element.
export type LegalSubElement = {
  id: number
  element_id: number
  name: string
  created_at: string
}
```

Then update `URLOffence` (currently lines 215-223):

```ts
// One row of GET /api/legal/offences/*url — a domain tagged with a specific
// (Category, optional Element, optional SubElement) offence.
export type URLOffence = {
  id: number
  url_id: number
  category_id: number
  category: LegalCategory
  element_id?: number
  element?: LegalElement
  sub_element_id?: number
  sub_element?: LegalSubElement
  recorded_at: string
}
```

- [ ] **Step 2: Add `SubElement` API functions and extend `attachOffence`**

In `web/src/api/legal.ts`, update the type import at the top:

```ts
import type { Instrument, Citation, LegalCategory, LegalElement, LegalSubElement, URLOffence, LegalCitationParsed } from './types'
```

Insert right after `deleteElement` (currently lines 98-100), before the `// URL <-> offence linking` comment:

```ts
export async function fetchSubElements(elementId: number): Promise<LegalSubElement[]> {
  const data = await api.get<LegalSubElement[]>(`/legal/elements/${elementId}/subelements`)
  return Array.isArray(data) ? data : []
}

export function createSubElement(elementId: number, name: string): Promise<LegalSubElement> {
  return api.post<LegalSubElement>('/legal/subelements', { element_id: elementId, name })
}

export function updateSubElement(id: number, name: string): Promise<LegalSubElement> {
  return api.patch<LegalSubElement>(`/legal/subelements/${id}`, { name })
}

export function deleteSubElement(id: number): Promise<void> {
  return api.delete<void>(`/legal/subelements/${id}`)
}
```

Update `attachOffence` (currently lines 109-114):

```ts
export function attachOffence(url: string, categoryId: number, elementId?: number, subElementId?: number): Promise<URLOffence> {
  return api.post<URLOffence>(`/legal/offences/${encodeURIComponent(url)}`, {
    category_id: categoryId,
    element_id: elementId,
    sub_element_id: subElementId,
  })
}
```

- [ ] **Step 3: Type-check**

Run: `cd web && npx tsc -b --noEmit`
Expected: fails at this point with errors in `urls.tsx`/`legal-citations.tsx` about the changed `attachOffence` call sites still being source-compatible (they are, since the new param is optional and unused call sites just omit it) — actually expect this to succeed since existing 3-arg calls remain valid against the new 4-arg-with-optional-4th signature. If it fails, the error will point at real call sites; note them for Task 5 but do not fix here (this task deliberately doesn't touch the picker yet).

- [ ] **Step 4: Commit**

```bash
git add web/src/api/types.ts web/src/api/legal.ts
git commit -m "feat: add SubElement types and API client functions"
```

---

## Task 4: `/legal-citations` tree UI

**Files:**
- Modify: `web/src/routes/legal-citations.tsx`

**Interfaces:**
- Consumes: `fetchSubElements`/`createSubElement`/`updateSubElement`/`deleteSubElement` (Task 3), `LegalSubElement` type (Task 3).
- Produces: `SubElementFormDialog` component; extended `LegalTreeRow`/`LegalKind` supporting `'subelement'`.

- [ ] **Step 1: Import the new API functions and type**

Update the imports at the top of `web/src/routes/legal-citations.tsx`:

```ts
import {
  fetchInstruments, createInstrument, updateInstrument, deleteInstrument,
  fetchCitations, parseCitationPreview, createCitation, updateCitation, deleteCitation,
  fetchCategories, createCategory, updateCategory, deleteCategory,
  fetchElements, createElement, updateElement, deleteElement,
  fetchSubElements, createSubElement, updateSubElement, deleteSubElement,
  formatParsedCitation,
} from '@/api/legal'
import type { Instrument, Citation, LegalCategory, LegalElement, LegalSubElement, LegalCitationParsed } from '@/api/types'
```

- [ ] **Step 2: Extend `LegalKind` and `LegalTreeRow`**

Change:

```ts
type LegalKind = 'instrument' | 'citation' | 'category' | 'element'

type LegalTreeRow = {
  id: string
  kind: LegalKind
  refId: number
  label: string
  instrument?: Instrument
  citation?: Citation
  category?: LegalCategory
  element?: LegalElement
  children?: LegalTreeRow[]
}
```

to:

```ts
type LegalKind = 'instrument' | 'citation' | 'category' | 'element' | 'subelement'

type LegalTreeRow = {
  id: string
  kind: LegalKind
  refId: number
  label: string
  instrument?: Instrument
  citation?: Citation
  category?: LegalCategory
  element?: LegalElement
  subElement?: LegalSubElement
  children?: LegalTreeRow[]
}
```

- [ ] **Step 3: Extend `loadTree` one level deeper**

Change the `categoryRows` mapping inside `loadTree` from:

```ts
      const categoryRows = await Promise.all(categories.map(async (cat): Promise<LegalTreeRow> => {
        const elements = await fetchElements(cat.id)
        const elementRows: LegalTreeRow[] = elements.map(el => ({
          id: `element-${el.id}`, kind: 'element', refId: el.id, label: el.name, element: el,
        }))
        return {
          id: `category-${cat.id}`, kind: 'category', refId: cat.id, label: cat.name, category: cat,
          children: elementRows.length > 0 ? elementRows : undefined,
        }
      }))
```

to:

```ts
      const categoryRows = await Promise.all(categories.map(async (cat): Promise<LegalTreeRow> => {
        const elements = await fetchElements(cat.id)
        const elementRows: LegalTreeRow[] = await Promise.all(elements.map(async (el): Promise<LegalTreeRow> => {
          const subElements = await fetchSubElements(el.id)
          const subElementRows: LegalTreeRow[] = subElements.map(se => ({
            id: `subelement-${se.id}`, kind: 'subelement', refId: se.id, label: se.name, subElement: se,
          }))
          return {
            id: `element-${el.id}`, kind: 'element', refId: el.id, label: el.name, element: el,
            children: subElementRows.length > 0 ? subElementRows : undefined,
          }
        }))
        return {
          id: `category-${cat.id}`, kind: 'category', refId: cat.id, label: cat.name, category: cat,
          children: elementRows.length > 0 ? elementRows : undefined,
        }
      }))
```

- [ ] **Step 4: Extend `DELETE_DESCRIPTIONS`**

Change:

```ts
const DELETE_DESCRIPTIONS: Record<LegalKind, string> = {
  instrument: 'Cascades to every citation, category, element, and recorded offence under this law.',
  citation: 'Cascades to every category, element, and recorded offence under this citation.',
  category: 'Cascades to every element and recorded offence under this category.',
  element: 'Removes this element from any domain currently tagged with it.',
}
```

to:

```ts
const DELETE_DESCRIPTIONS: Record<LegalKind, string> = {
  instrument: 'Cascades to every citation, category, element, sub-element, and recorded offence under this law.',
  citation: 'Cascades to every category, element, sub-element, and recorded offence under this citation.',
  category: 'Cascades to every element, sub-element, and recorded offence under this category.',
  element: 'Cascades to every sub-element and recorded offence under this element.',
  subelement: 'Removes this sub-element from any domain currently tagged with it.',
}
```

- [ ] **Step 5: Add `SubElementFormDialog`, mirroring `ElementFormDialog` exactly**

Insert immediately after `ElementFormDialog` (currently ending at line 522), before the `/* ─── Page ─── */` comment:

```tsx
function SubElementFormDialog({
  open, onClose, onSaved, element, editing,
}: { open: boolean; onClose: () => void; onSaved: () => void; element: LegalElement | null; editing: LegalSubElement | null }) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setName(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  useEffect(() => {
    if (!open) return
    if (editing) { setName(editing.name); setError(null) } else { reset() }
  }, [open, editing])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!element) return
    if (!name.trim()) { setError('Name is required'); return }
    setLoading(true)
    setError(null)
    try {
      if (editing) {
        await updateSubElement(editing.id, name.trim())
      } else {
        await createSubElement(element.id, name.trim())
      }
      reset()
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${editing ? 'save' : 'add'} sub-element`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit Sub-Element' : 'Add Sub-Element'}</DialogTitle>
          <DialogDescription>
            {element ? `Sub-category of "${element.name}".` : ''} Optional finer-grained qualifier one level below Element.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="subelement-name-input">Name</label>
            <input
              id="subelement-name-input"
              className="form-input"
              type="text"
              placeholder="e.g. Direct Threat"
              value={name}
              onChange={e => setName(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {editing ? (loading ? 'Saving…' : 'Save Changes') : (loading ? 'Adding…' : 'Add Sub-Element')}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 6: Wire dialog state, delete handling, columns, and dialog mounts in `LegalCitationsPage`**

Add state (right after `const [addElementFor, setAddElementFor] = useState<LegalCategory | null>(null)` and its edit-target sibling — currently lines 537, 541):

```ts
  const [addElementFor, setAddElementFor] = useState<LegalCategory | null>(null)
  const [addSubElementFor, setAddSubElementFor] = useState<LegalElement | null>(null)
  const [editInstrumentTarget, setEditInstrumentTarget] = useState<Instrument | null>(null)
  const [editCitationTarget, setEditCitationTarget] = useState<Citation | null>(null)
  const [editCategoryTarget, setEditCategoryTarget] = useState<LegalCategory | null>(null)
  const [editElementTarget, setEditElementTarget] = useState<LegalElement | null>(null)
  const [editSubElementTarget, setEditSubElementTarget] = useState<LegalSubElement | null>(null)
```

Update `handleDelete`'s switch:

```ts
  const handleDelete = async () => {
    if (!deleteTarget) return
    switch (deleteTarget.kind) {
      case 'instrument': await deleteInstrument(deleteTarget.id); break
      case 'citation': await deleteCitation(deleteTarget.id); break
      case 'category': await deleteCategory(deleteTarget.id); break
      case 'element': await deleteElement(deleteTarget.id); break
      case 'subelement': await deleteSubElement(deleteTarget.id); break
    }
    setDeleteTarget(null)
    load()
  }
```

Update the `name` column's cell renderer — change:

```tsx
            {(r.kind === 'category' || r.kind === 'element') && (
              <span>{r.label}</span>
            )}
```

to:

```tsx
            {(r.kind === 'category' || r.kind === 'element' || r.kind === 'subelement') && (
              <span>{r.label}</span>
            )}
```

Update the `actions` column's cell renderer — add a "+ Sub-Element" button for `element` rows (right after the existing `category` block that renders "+ Element"):

```tsx
            {r.kind === 'category' && r.category && (
              <button type="button" className="btn-ghost" style={{ backgroundColor: 'var(--stone-panel)' }} onClick={() => setAddElementFor(r.category!)}>
                + Element
              </button>
            )}
            {r.kind === 'element' && r.element && (
              <button type="button" className="btn-ghost" style={{ backgroundColor: 'var(--stone-panel)' }} onClick={() => setAddSubElementFor(r.element!)}>
                + Sub-Element
              </button>
            )}
```

Update the edit-button `onClick` switch:

```tsx
              onClick={() => {
                if (r.kind === 'instrument' && r.instrument) setEditInstrumentTarget(r.instrument)
                else if (r.kind === 'citation' && r.citation) setEditCitationTarget(r.citation)
                else if (r.kind === 'category' && r.category) setEditCategoryTarget(r.category)
                else if (r.kind === 'element' && r.element) setEditElementTarget(r.element)
                else if (r.kind === 'subelement' && r.subElement) setEditSubElementTarget(r.subElement)
              }}
```

Finally, mount the new dialogs right after the existing `ElementFormDialog` mounts (currently lines 743-750), before `<DeleteConfirmDialog`:

```tsx
      <ElementFormDialog open={addElementFor !== null} onClose={() => setAddElementFor(null)} onSaved={load} category={addElementFor} editing={null} />
      <ElementFormDialog open={editElementTarget !== null} onClose={() => setEditElementTarget(null)} onSaved={load} category={null} editing={editElementTarget} />

      <SubElementFormDialog open={addSubElementFor !== null} onClose={() => setAddSubElementFor(null)} onSaved={load} element={addSubElementFor} editing={null} />
      <SubElementFormDialog open={editSubElementTarget !== null} onClose={() => setEditSubElementTarget(null)} onSaved={load} element={null} editing={editSubElementTarget} />
```

- [ ] **Step 7: Type-check**

Run: `cd web && npx tsc -b --noEmit`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add web/src/routes/legal-citations.tsx
git commit -m "feat: add SubElement management to /legal-citations tree UI"
```

---

## Task 5: `MultiOffencePicker` sub-element step + `urls.tsx` wiring

**Files:**
- Modify: `web/src/routes/urls.tsx`

**Interfaces:**
- Consumes: `fetchSubElements`, `LegalSubElement`, the 4-arg `attachOffence` (Task 3).

- [ ] **Step 1: Import the new API function and type**

Update the imports at the top of `web/src/routes/urls.tsx`:

```ts
import type { URLEntry, Instrument, Citation, LegalCategory, LegalElement, LegalSubElement, URLOffence } from '../api/types'
import { fetchInstruments, fetchCitations, fetchCategories, fetchElements, fetchSubElements, attachOffence, fetchOffencesByUrl, detachOffence, formatParsedCitation } from '../api/legal'
```

- [ ] **Step 2: Extend `StagedOffence` with `subElementId`**

Change:

```ts
export type StagedOffence = {
  instrumentId: number
  citationId: number
  categoryId: number
  elementId?: number
  label: string
}
```

to:

```ts
export type StagedOffence = {
  instrumentId: number
  citationId: number
  categoryId: number
  elementId?: number
  subElementId?: number
  label: string
}
```

- [ ] **Step 3: Add sub-element state, fetching, and the cascading select to `MultiOffencePicker`**

Add state (right after `const [elementId, setElementId] = useState<number | ''>('')`, currently line 111):

```ts
  const [instruments, setInstruments] = useState<Instrument[]>([])
  const [citations, setCitations] = useState<Citation[]>([])
  const [categories, setCategories] = useState<LegalCategory[]>([])
  const [elements, setElements] = useState<LegalElement[]>([])
  const [subElements, setSubElements] = useState<LegalSubElement[]>([])

  const [instrumentId, setInstrumentId] = useState<number | ''>('')
  const [citationId, setCitationId] = useState<number | ''>('')
  const [categoryId, setCategoryId] = useState<number | ''>('')
  const [elementId, setElementId] = useState<number | ''>('')
  const [subElementId, setSubElementId] = useState<number | ''>('')
```

Add the fetch effect right after the existing `elementId`-driven `elements` effect (currently lines 122-125):

```ts
  useEffect(() => {
    if (categoryId === '') { setElements([]); return }
    fetchElements(categoryId).then(setElements)
  }, [categoryId])
  useEffect(() => {
    if (elementId === '') { setSubElements([]); return }
    fetchSubElements(elementId).then(setSubElements)
  }, [elementId])
```

Update `resetStaging`:

```ts
  const resetStaging = () => {
    setInstrumentId(''); setCitationId(''); setCategoryId(''); setElementId(''); setSubElementId('')
  }
```

Update `computePending`:

```ts
  const computePending = (): StagedOffence | null => {
    if (instrumentId === '' || citationId === '' || categoryId === '') return null
    const citation = citations.find(c => c.id === citationId)
    const category = categories.find(c => c.id === categoryId)
    const element = elementId === '' ? undefined : elements.find(e => e.id === elementId)
    const subElement = subElementId === '' ? undefined : subElements.find(se => se.id === subElementId)
    if (!citation || !category) return null
    const label = `${formatParsedCitation(citation.parsed)} — ${category.name}${element ? ` (${element.name})` : ''}${subElement ? ` › ${subElement.name}` : ''}`
    return {
      instrumentId, citationId, categoryId,
      elementId: elementId === '' ? undefined : elementId,
      subElementId: subElementId === '' ? undefined : subElementId,
      label,
    }
  }
```

Update the `instrumentId`/`citationId`/`categoryId` `onValueChange` handlers to also clear `subElementId` downstream (mirroring how they already clear every field below them):

```tsx
        <Select
          value={String(instrumentId)}
          onValueChange={v => { setInstrumentId(v === '' ? '' : Number(v)); setCitationId(''); setCategoryId(''); setElementId(''); setSubElementId('') }}
          disabled={disabled}
        >
```

```tsx
          <Select
            value={String(citationId)}
            onValueChange={v => { setCitationId(v === '' ? '' : Number(v)); setCategoryId(''); setElementId(''); setSubElementId('') }}
            disabled={disabled}
          >
```

```tsx
          <Select
            value={String(categoryId)}
            onValueChange={v => { setCategoryId(v === '' ? '' : Number(v)); setElementId(''); setSubElementId('') }}
            disabled={disabled}
          >
```

Update the element `Select`'s `onValueChange` to clear `subElementId`, and add the sub-element `Select` right after it — change:

```tsx
        {categoryId !== '' && elements.length > 0 && (
          <Select
            value={String(elementId)}
            onValueChange={v => setElementId(v === '' ? '' : Number(v))}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Element" placeholder="Element (optional)…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No element</SelectItem>
              {elements.map((el, i) => (
                <SelectItem key={el.id} index={i + 1} value={String(el.id)}>{el.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
```

to:

```tsx
        {categoryId !== '' && elements.length > 0 && (
          <Select
            value={String(elementId)}
            onValueChange={v => { setElementId(v === '' ? '' : Number(v)); setSubElementId('') }}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Element" placeholder="Element (optional)…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No element</SelectItem>
              {elements.map((el, i) => (
                <SelectItem key={el.id} index={i + 1} value={String(el.id)}>{el.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {elementId !== '' && subElements.length > 0 && (
          <Select
            value={String(subElementId)}
            onValueChange={v => setSubElementId(v === '' ? '' : Number(v))}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Sub-Element" placeholder="Sub-Element (optional)…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No sub-element</SelectItem>
              {subElements.map((se, i) => (
                <SelectItem key={se.id} index={i + 1} value={String(se.id)}>{se.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
```

- [ ] **Step 4: Update `attachOffence` call sites**

In `AddUrlDialog`'s `handleSubmit` (currently line 286):

```ts
      await Promise.all(
        domains.flatMap(d => allOffences.map(o => attachOffence(d, o.categoryId, o.elementId, o.subElementId)))
      )
```

In `EditOffencesDialog`'s `handleAddStaged` (currently line 396):

```ts
      await attachOffence(url, added.categoryId, added.elementId, added.subElementId)
```

In `EditOffencesDialog`'s `handleDone` (currently line 414):

```ts
        await attachOffence(url, pending.categoryId, pending.elementId, pending.subElementId)
```

- [ ] **Step 5: Show the sub-element in the existing-offences chip list**

In `EditOffencesDialog`'s render (currently line 436), change:

```tsx
                <span>{formatParsedCitation(o.category.citation.parsed)} — {o.category.name}{o.element ? ` (${o.element.name})` : ''}</span>
```

to:

```tsx
                <span>{formatParsedCitation(o.category.citation.parsed)} — {o.category.name}{o.element ? ` (${o.element.name})` : ''}{o.sub_element ? ` › ${o.sub_element.name}` : ''}</span>
```

- [ ] **Step 6: Type-check and lint**

Run: `cd web && npx tsc -b --noEmit && npm run lint`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/routes/urls.tsx
git commit -m "feat: add optional SubElement step to MultiOffencePicker"
```

---

## Task 6: CLAUDE.md documentation + final verification

**Files:**
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: nothing (documentation only).

- [ ] **Step 1: Update the "Legal citation catalog" section of `CLAUDE.md`**

In the first paragraph of that section, change:

```
Four-level hierarchy, each level FK'd to its parent with `OnDelete:CASCADE`: `Instrument` (a Malaysian law — Act/Ordinance/Enactment/Subsidiary/Constitution — get-or-created via `GetOrCreateInstrument` on `(type, jurisdiction, number, year)` so the same Act isn't re-entered per citation; `Number` is optional, since plenty of instruments — older pre-1968-revision Acts, most state Enactments — have no commonly cited official number) → `Citation` (one specific provision, e.g. "Seksyen 233(1)(a)") → `Category` (an offence category scoped to *that* citation, not a shared global lookup — the same name under two different citations is deliberately two separate rows, since category vocabulary is specific to the provision's wording) → `Element` (an optional sub-category, e.g. "Harassment" splitting into "Menacing"/"Obscene"; not every `Category` has one). `URLOffence` is a separate join linking a `URL` to a `Category` + optional `Element`, with a surrogate ID PK rather than a composite one (unlike `DepartmentURL`) because `ElementID` is nullable and SQL `NULL != NULL` breaks composite-PK uniqueness. `db.Store`'s `LegalCitationStore` sub-interface bundles all five aggregates together rather than splitting per-table — every real consumer walks the whole chain (rendering a URL's offences means resolving Element→Category→Citation→Instrument), the same reasoning `EnrichmentStore` already uses.
```

to:

```
Five-level hierarchy, each level FK'd to its parent with `OnDelete:CASCADE`: `Instrument` (a Malaysian law — Act/Ordinance/Enactment/Subsidiary/Constitution — get-or-created via `GetOrCreateInstrument` on `(type, jurisdiction, number, year)` so the same Act isn't re-entered per citation; `Number` is optional, since plenty of instruments — older pre-1968-revision Acts, most state Enactments — have no commonly cited official number) → `Citation` (one specific provision, e.g. "Seksyen 233(1)(a)") → `Category` (an offence category scoped to *that* citation, not a shared global lookup — the same name under two different citations is deliberately two separate rows, since category vocabulary is specific to the provision's wording) → `Element` (an optional sub-category, e.g. "Harassment" splitting into "Menacing"/"Obscene"; not every `Category` has one) → `SubElement` (an optional sub-category of `Element`, mirroring `Element`'s own relationship to `Category` one level down; not every `Element` has one — five levels is the ceiling, no nesting deeper than `SubElement`). `URLOffence` is a separate join linking a `URL` to a `Category` + optional `Element` + optional `SubElement`, with a surrogate ID PK rather than a composite one (unlike `DepartmentURL`) because `ElementID`/`SubElementID` are nullable and SQL `NULL != NULL` breaks composite-PK uniqueness. `db.Store`'s `LegalCitationStore` sub-interface bundles all six aggregates together rather than splitting per-table — every real consumer walks the whole chain (rendering a URL's offences means resolving SubElement→Element→Category→Citation→Instrument), the same reasoning `EnrichmentStore` already uses.
```

In the second paragraph, change:

```
- `internal/legalcite.Parse(raw string) Result` turns free-text Malay citation shorthand ("Seksyen 233(1)(a)", "Perkara 121(1A)", "Bahagian IX") into `Citation.Parsed`'s structured fields (part/chapter/provision+suffix/sub-provision/paragraph/subparagraph/sub-subparagraph/schedule) — Malay-only by design, matching MCMC's national blocking-list spreadsheet ("Seksyen"/"Peraturan", never the English "s"/"section" shorthand). A standalone package with no `internal/db` import, matching the `internal/whois`/`internal/subfinder` convention of returning a plain struct the caller converts. Unparseable or ambiguous input — including a joined list like "Seksyen 211 dan 233 Akta..." — sets `ParseConfidence` to `NEEDS_REVIEW` rather than guessing or silently dropping data, while keeping whatever did parse; `POST /api/legal/citations/parse-preview` exposes this for a live preview before submitting. `db.BuildProvisionSortKey` (recomputed server-side on every create/update, never client-supplied) zero-pads `ProvisionNum`+`ProvisionSuffix` into a sortable `SortKey`, since a plain `ORDER BY provision_num` would put "4A" after "40". Section/Article and Subsection/Clause share the same `ProvisionNum`/`SubProvision` fields — which label applies is a display-only switch on `Instrument.Type == "CONSTITUTION"`, not separate columns.
```

Leave this paragraph as-is (it's about `legalcite.Parse`, unaffected by `SubElement`).

In the third paragraph (routes/read-write gating), change:

```
- Read routes for the catalog (instruments/citations/categories/elements) are open to any authenticated role — shared/global reference data, same shape as DNS servers/ISP logos. Mutations are **admin or department-admin**. `URLOffence` attach/detach is different: department-ownership-scoped via `requireDomainOwnership` (the shared 404-not-403 helper also used by `/api/results`, `/api/domain`, etc.), since it's tied to a specific department's watchlist domain rather than global catalog data.
- Frontend: `legal-citations.tsx` and the `MultiOffencePicker`/`EditOffencesDialog` in `urls.tsx` — see the route bullets above. `Instrument.Type`/`Jurisdiction` enum values display Malay labels (Akta/Persekutuan/etc.) while keeping English wire values, matching every other enum in this codebase.
```

to:

```
- Read routes for the catalog (instruments/citations/categories/elements/sub-elements) are open to any authenticated role — shared/global reference data, same shape as DNS servers/ISP logos. Mutations are **admin or department-admin**. `URLOffence` attach/detach is different: department-ownership-scoped via `requireDomainOwnership` (the shared 404-not-403 helper also used by `/api/results`, `/api/domain`, etc.), since it's tied to a specific department's watchlist domain rather than global catalog data.
- `GET /api/legal/elements/{id}/subelements` and `POST/PATCH/DELETE /api/legal/subelements[/{id}]` follow the exact same open-read/admin-write shape as the `Element` routes one level up; `AttachOffence`'s body accepts an optional `sub_element_id` alongside `category_id`/`element_id`.
- Frontend: `legal-citations.tsx` renders `SubElement` as a fifth indented tree level nested under each `Element` row (via `SubElementFormDialog`, mirroring `ElementFormDialog`); the `MultiOffencePicker`/`EditOffencesDialog` in `urls.tsx` show an optional cascading Sub-Element select once the currently-selected Element has sub-elements, same conditional pattern as the Element step itself. `Instrument.Type`/`Jurisdiction` enum values display Malay labels (Akta/Persekutuan/etc.) while keeping English wire values, matching every other enum in this codebase.
```

- [ ] **Step 2: Run the full backend test suite**

Run: `go test ./...`
Expected: PASS. (`internal/dns` tests make real network calls and may fail offline — that's expected per CLAUDE.md's "Test dependency summary" and unrelated to this change; every other package must pass.)

- [ ] **Step 3: Run the frontend build and lint**

Run: `cd web && npm run build && npm run lint`
Expected: both succeed with no errors.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document SubElement level in legal citation catalog"
```

---

## Manual Verification (post-implementation, human-driven per repo convention)

Not automatable in this environment — leave for the human reviewer running `dev.sh`:
- As admin: add a sub-element under an existing element on `/legal-citations`, edit its name, delete it, confirm the parent element's row updates.
- As a regular department member: open Add Domain / Edit Offences, confirm the Sub-Element select appears only when the selected Element has sub-elements, and that submitting without picking one still succeeds (optional field).
