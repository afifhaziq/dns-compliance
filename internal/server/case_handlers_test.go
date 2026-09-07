package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/server"
	"github.com/go-chi/chi/v5"
)

func TestCasesByURL_OwningDepartment(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cases/example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var cases []db.CaseWithLetters
	if err := json.Unmarshal(w.Body.Bytes(), &cases); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cases) != 1 || cases[0].ID != 1 {
		t.Fatalf("expected 1 case, got %+v", cases)
	}
}

func TestCasesByURL_NonOwningDepartment404(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	cookie := deptCookie(store, 2)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cases/example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// CreateCaseForURL must always use the caller's own department, never a
// client-supplied one — the body has no department_id field to trust.
func TestCreateCaseForURL_UsesCallersOwnDepartment(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "requested"})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/example.com", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var c db.Case
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.DepartmentID != 1 {
		t.Fatalf("expected department_id 1, got %d", c.DepartmentID)
	}
}

func TestCreateCaseForURL_NonOwningDepartment404(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	cookie := deptCookie(store, 2)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "requested"})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/example.com", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateCaseForURL_InvalidStatusRejected(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "bogus"})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/example.com", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddCaseLetter_OwningDepartmentSucceeds(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"type": "Memo"})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/1/letters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.caseLetters) != 1 || store.caseLetters[0].Type != "Memo" {
		t.Fatalf("expected 1 letter of type Memo, got %+v", store.caseLetters)
	}
}

func TestAddCaseLetter_NonOwningDepartment404(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID})
	cookie := deptCookie(store, 2)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"type": "Memo"})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/1/letters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.caseLetters) != 0 {
		t.Fatalf("expected no letter to be added, got %d", len(store.caseLetters))
	}
}

func TestAddCaseLetter_WatchesURLButDoesNotOwnCase404(t *testing.T) {
	// Regression test: a department that merely watches the same domain
	// (department_urls) must not be able to add a letter to another
	// department's case — ownership is the case's own DepartmentID, not
	// URL watchlist membership, since domains are shared across
	// departments' watchlists by design.
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs,
		db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true},
		db.DepartmentURL{DepartmentID: 2, URLID: u.ID, Enabled: true},
	)
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID})
	cookie := deptCookie(store, 2) // watches the URL, but doesn't own the case
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"type": "Memo"})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/1/letters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.caseLetters) != 0 {
		t.Fatalf("expected no letter to be added, got %d", len(store.caseLetters))
	}
}

func TestAddCaseLetter_UnknownCase404(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"type": "Memo"})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/999/letters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// PATCH /api/cases/{id} — ownership + clear-sentinel behavior, mirroring
// the old PATCH /api/urls/{id} case-field tests (handlers_test.go) now that
// those fields live on Case.

func TestUpdateCase_OwningDepartmentSetsFields(t *testing.T) {
	store := &fullMockStore{
		cases:    []db.Case{{ID: 1, DepartmentID: 1}},
		agencies: []db.Agency{{ID: 7, Name: "MCMC"}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"agency_id": 7, "due_date": "2026-01-15T00:00:00Z", "requested_at": "2026-01-01T00:00:00Z",
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	c := store.cases[0]
	if c.AgencyID == nil || *c.AgencyID != 7 {
		t.Fatalf("expected agency_id to be set, got %+v", c)
	}
	if c.DueDate == nil || c.RequestedAt == nil {
		t.Fatalf("expected due_date/requested_at to be set, got %+v", c)
	}
}

// UpdateCase is the only remaining trigger point for the due-date-reached
// notification task now that DueDate lives on Case, not URL — ToggleURL
// (PATCH /api/urls/{id}) was narrowed to {enabled} only and no longer
// touches it. Confirms the fan-out reaches every url the case covers.
func TestUpdateCase_ReschedulesDueDateForEveryCaseURL(t *testing.T) {
	store := &fullMockStore{
		cases: []db.Case{{ID: 1, DepartmentID: 1}},
		caseURLs: []db.CaseURL{
			{CaseID: 1, URLID: 10},
			{CaseID: 1, URLID: 11},
		},
	}
	cookie := deptCookie(store, 1)
	notifier := &fakeNotifier{}
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, nil, nil, false, nil, nil, nil, nil, nil, notifier)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Header.Set("X-Requested-With", "fetch")
		r.ServeHTTP(w, req)
	})

	body, _ := json.Marshal(map[string]interface{}{"due_date": "2026-01-15T00:00:00Z"})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.calls) != 2 {
		t.Fatalf("expected 2 RescheduleDueDate calls (one per case url), got %d: %+v", len(notifier.calls), notifier.calls)
	}
	seen := map[uint]bool{}
	for _, c := range notifier.calls {
		if c.departmentID != 1 {
			t.Fatalf("expected department 1, got %d", c.departmentID)
		}
		if c.dueDate == nil {
			t.Fatal("expected a non-nil due date")
		}
		seen[c.urlID] = true
	}
	if !seen[10] || !seen[11] {
		t.Fatalf("expected calls for both case urls, got %+v", notifier.calls)
	}
}

// A status-only update must not touch the notification queue at all — only
// an actual due_date change in the request body should fan out.
func TestUpdateCase_StatusOnlyUpdateDoesNotRescheduleDueDate(t *testing.T) {
	store := &fullMockStore{
		cases:    []db.Case{{ID: 1, DepartmentID: 1}},
		caseURLs: []db.CaseURL{{CaseID: 1, URLID: 10}},
	}
	cookie := deptCookie(store, 1)
	notifier := &fakeNotifier{}
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, nil, nil, false, nil, nil, nil, nil, nil, notifier)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Header.Set("X-Requested-With", "fetch")
		r.ServeHTTP(w, req)
	})

	body, _ := json.Marshal(map[string]interface{}{"status": "uplift"})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.calls) != 0 {
		t.Fatalf("expected 0 RescheduleDueDate calls for a status-only update, got %d", len(notifier.calls))
	}
}

func TestUpdateCase_ClearsAgencyAndDueDateWithSentinels(t *testing.T) {
	due := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	agencyID := uint(7)
	store := &fullMockStore{
		cases: []db.Case{{ID: 1, DepartmentID: 1, AgencyID: &agencyID, DueDate: &due, RequestedAt: &due}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	// 0 clears agency_id, "" clears due_date/requested_at — same sentinel
	// convention the old PATCH /api/urls/{id} used.
	body, _ := json.Marshal(map[string]interface{}{
		"agency_id": 0, "due_date": "", "requested_at": "",
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	c := store.cases[0]
	if c.AgencyID != nil || c.DueDate != nil || c.RequestedAt != nil {
		t.Fatalf("expected agency_id/due_date/requested_at to be cleared, got %+v", c)
	}
}

func TestUpdateCase_NonOwningDepartment404(t *testing.T) {
	store := &fullMockStore{cases: []db.Case{{ID: 1, DepartmentID: 1}}}
	cookie := deptCookie(store, 2)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]interface{}{"agency_id": 7})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if store.cases[0].AgencyID != nil {
		t.Fatalf("expected case to remain untouched, got %+v", store.cases[0])
	}
}

func TestUpdateCase_UnknownCase404(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "uplift"})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// PATCH /api/cases/{id}/urls/{url_id} — ownership identical to AddCaseURL's,
// plus status validation.

func TestUpdateCaseURLStatus_OwningDepartmentSucceeds(t *testing.T) {
	u := db.URL{ID: 1, URL: "example.com"}
	store := &fullMockStore{
		urls:     []db.URL{u},
		cases:    []db.Case{{ID: 1, DepartmentID: 1}},
		caseURLs: []db.CaseURL{{CaseID: 1, URLID: u.ID, Status: "requested"}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "uplift"})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.caseURLs[0].Status != "uplift" {
		t.Fatalf("expected status to be updated, got %q", store.caseURLs[0].Status)
	}
}

func TestUpdateCaseURLStatus_NonOwningDepartment404(t *testing.T) {
	u := db.URL{ID: 1, URL: "example.com"}
	store := &fullMockStore{
		urls:     []db.URL{u},
		cases:    []db.Case{{ID: 1, DepartmentID: 1}},
		caseURLs: []db.CaseURL{{CaseID: 1, URLID: u.ID, Status: "requested"}},
	}
	cookie := deptCookie(store, 2)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "uplift"})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if store.caseURLs[0].Status != "requested" {
		t.Fatalf("expected status to remain untouched, got %q", store.caseURLs[0].Status)
	}
}

func TestUpdateCaseURLStatus_InvalidStatusReturns400(t *testing.T) {
	u := db.URL{ID: 1, URL: "example.com"}
	store := &fullMockStore{
		urls:     []db.URL{u},
		cases:    []db.Case{{ID: 1, DepartmentID: 1}},
		caseURLs: []db.CaseURL{{CaseID: 1, URLID: u.ID, Status: "requested"}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "bogus"})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateCaseURLStatus_UnknownPairReturns404(t *testing.T) {
	store := &fullMockStore{
		cases: []db.Case{{ID: 1, DepartmentID: 1}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "uplift"})
	req := httptest.NewRequest(http.MethodPatch, "/api/cases/1/urls/999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListCaseSummaries_NonAdminScopedToOwnDepartment(t *testing.T) {
	store := &fullMockStore{}
	uA := db.URL{ID: 1, URL: "summaries-a.com"}
	uB := db.URL{ID: 2, URL: "summaries-b.com"}
	store.urls = append(store.urls, uA, uB)
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1},
		db.Case{ID: 2, DepartmentID: 2},
	)
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: uA.ID, Status: "requested"},
		db.CaseURL{CaseID: 2, URLID: uB.ID, Status: "requested"},
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
		db.Case{ID: 1, DepartmentID: 1},
		db.Case{ID: 2, DepartmentID: 2},
	)
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: uA.ID, Status: "requested"},
		db.CaseURL{CaseID: 2, URLID: uB.ID, Status: "requested"},
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

func TestUpdateCaseLetter_PartialUpdate(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "update-letter.com"}
	store.urls = append(store.urls, u)
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID})
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
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID})
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
