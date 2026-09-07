package server_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/xuri/excelize/v2"
)

func caseIDColumn(t *testing.T, body []byte) []string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	var caseIDs []string
	for _, row := range rows[1:] { // skip header
		if len(row) > 14 {
			caseIDs = append(caseIDs, row[14]) // "Case ID" is column 15 (0-indexed 14)
		}
	}
	return caseIDs
}

func TestExportCaseSummaries_AdminGetsEverythingInScope(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 2})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("Content-Type = %q", ct)
	}
	ids := caseIDColumn(t, w.Body.Bytes())
	if len(ids) != 2 {
		t.Fatalf("got %d rows, want 2 (both departments' cases): %v", len(ids), ids)
	}
}

func TestExportCaseSummaries_NonAdminScopedToOwnDepartment(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 2})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ids := caseIDColumn(t, w.Body.Bytes())
	if len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("expected only case 1 (own department), got %v", ids)
	}
}

// A non-admin passing another department's case_id must not see it — the
// server always filters its own already-scoped fetch, never the client's
// list.
func TestExportCaseSummaries_NonAdminCannotWidenScopeViaCaseIDs(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 2})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries/export?case_ids=1,2", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ids := caseIDColumn(t, w.Body.Bytes())
	if len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("expected only case 1 even though case_ids=1,2 was requested, got %v", ids)
	}
}

func TestExportCaseSummaries_CaseIDsFilterNarrowsWithinScope(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases,
		db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 1})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-summaries/export?case_ids=2", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ids := caseIDColumn(t, w.Body.Bytes())
	if len(ids) != 1 || ids[0] != "2" {
		t.Fatalf("expected only case 2, got %v", ids)
	}
}
