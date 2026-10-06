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

func letterIDColumn(t *testing.T, body []byte) []string {
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
	for _, row := range rows[1:] {
		if len(row) > 15 {
			caseIDs = append(caseIDs, row[15]) // "Case ID" is column 16 (0-indexed 15)
		}
	}
	return caseIDs
}

func TestExportCaseLetters_NonAdminCannotWidenScopeViaLetterIDs(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1}, db.Case{ID: 2, DepartmentID: 2})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 2, URLID: 2, Status: "requested"})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	store.caseLetters = append(store.caseLetters,
		db.CaseLetter{ID: 10, CaseID: 1, Type: "Notice"}, db.CaseLetter{ID: 20, CaseID: 2, Type: "Notice"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-letters/export?letter_ids=10,20", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ids := letterIDColumn(t, w.Body.Bytes())
	if len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("expected only case 1's letter even though letter_ids=10,20 was requested, got %v", ids)
	}
}

func TestExportCaseLetters_LoopsBeyondOneHundredLetters(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"})
	for i := uint(1); i <= 150; i++ { // > the 100-per-page cap
		store.caseLetters = append(store.caseLetters, db.CaseLetter{ID: i, CaseID: 1, Type: "Notice"})
	}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-letters/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows)-1 != 150 { // minus header
		t.Fatalf("got %d data rows, want 150 (must not truncate at the pagination cap)", len(rows)-1)
	}
}

// Finding 3(b): ExportCaseLetters must resolve oic_user_id only against
// users in the export's own department scope, not every user globally —
// defense in depth in case a cross-department id ever ends up stored on a
// letter (e.g. legacy data from before AddCaseLetter/UpdateCaseLetter
// validated it). A department-1 export must never surface a department-2
// user's username, even when that id is sitting right there on the row.
func TestExportCaseLetters_OICUsernameScopedToOwnDepartment(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"})
	store.caseURLs = append(store.caseURLs, db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"})
	otherDeptUserID := uint(50)
	otherDeptID := uint(2)
	store.users = append(store.users, db.User{ID: otherDeptUserID, Username: "dept2-secret-username", DepartmentID: &otherDeptID})
	store.caseLetters = append(store.caseLetters,
		db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice", OICUserID: &otherDeptUserID})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-letters/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want header + 1 data row", len(rows))
	}
	if oic := rows[1][6]; oic != "" { // "OIC" is cmodHeaders[6]
		t.Fatalf("OIC = %q, must not leak department 2's username to a department-1 export", oic)
	}
}

func TestExportCaseLetters_OmittedIDsExportsEverythingInScope(t *testing.T) {
	store := &fullMockStore{}
	store.cases = append(store.cases, db.Case{ID: 1, DepartmentID: 1})
	store.urls = append(store.urls, db.URL{ID: 1, URL: "a.com"}, db.URL{ID: 2, URL: "b.com"})
	store.caseURLs = append(store.caseURLs,
		db.CaseURL{CaseID: 1, URLID: 1, Status: "requested"}, db.CaseURL{CaseID: 1, URLID: 2, Status: "requested"})
	store.caseLetters = append(store.caseLetters, db.CaseLetter{ID: 1, CaseID: 1, Type: "Notice"})
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/case-letters/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows)-1 != 2 { // one letter x 2 urls
		t.Fatalf("got %d data rows, want 2", len(rows)-1)
	}
}

func TestExportAllISPUnblocked_OneSheetPerISP(t *testing.T) {
	store := &fullMockStore{dnsServers: []db.DNSServer{
		{ID: 1, ISP: "TM"}, {ID: 2, ISP: "Google"}, {ID: 3, ISP: "Google"},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/unblocked/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	if got := f.GetSheetList(); len(got) != 4 || got[0] != "Summary" || got[1] != "Matrix" || got[2] != "Google" || got[3] != "TM" {
		t.Fatalf("unexpected sheets: %q", got)
	}
	summary, _ := f.GetRows("Summary")
	if g := summary[4]; g[0] != "Google" || g[1] != "2" {
		t.Fatalf("expected Google with 2 servers, got %q", g)
	}
}
