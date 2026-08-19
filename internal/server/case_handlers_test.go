package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
)

func TestCasesByURL_OwningDepartment(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID, Phase: "requested"})
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
	if len(cases) != 1 || cases[0].Phase != "requested" {
		t.Fatalf("expected 1 case with phase requested, got %+v", cases)
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

	body, _ := json.Marshal(map[string]string{"phase": "requested"})
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

	body, _ := json.Marshal(map[string]string{"phase": "requested"})
	req := httptest.NewRequest(http.MethodPost, "/api/cases/example.com", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateCaseForURL_InvalidPhaseRejected(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"phase": "bogus"})
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
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID, Phase: "requested"})
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
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID, Phase: "requested"})
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
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: u.ID, Phase: "requested"})
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
