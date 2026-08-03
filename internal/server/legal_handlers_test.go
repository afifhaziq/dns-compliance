package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
)

func TestCreateInstrument_ForbiddenForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]any{
		"type": "ACT", "jurisdiction": "FEDERAL", "number": "588", "year": 1998, "short_title": "CMA 1998",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/legal/instruments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateInstrument_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]any{
		"type": "ACT", "jurisdiction": "FEDERAL", "number": "588", "year": 1998, "short_title": "CMA 1998",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/legal/instruments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Re-posting the same natural key must not create a duplicate row.
	req2 := httptest.NewRequest(http.MethodPost, "/api/legal/instruments", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if len(store.instruments) != 1 {
		t.Fatalf("expected instrument to be deduped, got %d rows", len(store.instruments))
	}
}

func TestParseCitationPreview(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"raw_text": "Seksyen 233(1)(a)"})
	req := httptest.NewRequest(http.MethodPost, "/api/legal/citations/parse-preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Parsed          db.LegalCitationParsed `json:"parsed"`
		ParseConfidence string                 `json:"parse_confidence"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ParseConfidence != "OK" {
		t.Fatalf("expected OK confidence, got %q", resp.ParseConfidence)
	}
	if resp.Parsed.ProvisionNum == nil || *resp.Parsed.ProvisionNum != 233 {
		t.Fatalf("expected provision_num 233, got %+v", resp.Parsed)
	}
	if resp.Parsed.Paragraph != "a" {
		t.Fatalf("expected paragraph a, got %+v", resp.Parsed)
	}
}

// seedOffence wires up a full Instrument->Citation->Category->Element chain
// plus a watchlisted URL owned by departmentID, and attaches one offence to
// it — the fixture every ownership-scoping test below builds on.
func seedOffence(store *fullMockStore, departmentID uint) (urlValue string, offenceID uint) {
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

	offence := db.URLOffence{ID: 1, URLID: u.ID, CategoryID: 1, ElementID: &element.ID}
	store.urlOffences = append(store.urlOffences, offence)
	return u.URL, offence.ID
}

func TestOffencesByURL_OwningDepartment(t *testing.T) {
	store := &fullMockStore{}
	urlValue, _ := seedOffence(store, 1)
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
	if len(offences) != 1 {
		t.Fatalf("expected 1 offence, got %d", len(offences))
	}
	if offences[0].Category.Name != "Harassment" || offences[0].Category.Citation.RawText != "Seksyen 233(1)(a)" {
		t.Fatalf("expected hydrated Category->Citation, got %+v", offences[0].Category)
	}
	if offences[0].Category.Citation.Instrument.ShortTitle != "CMA 1998" {
		t.Fatalf("expected hydrated Instrument, got %+v", offences[0].Category.Citation.Instrument)
	}
}

func TestOffencesByURL_NonOwningDepartment404(t *testing.T) {
	store := &fullMockStore{}
	urlValue, _ := seedOffence(store, 1)
	cookie := deptCookie(store, 2) // different department, doesn't own this URL
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/legal/offences/"+urlValue, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a non-owning department, got %d: %s", w.Code, w.Body.String())
	}
}

// AttachOffence is routine per-domain bookkeeping, open to any authenticated
// role within the owning department — not admin-gated, matching PATCH
// /api/urls/{id}'s ordered_at.
func TestAttachOffence_PlainMemberOfOwningDepartmentAllowed(t *testing.T) {
	store := &fullMockStore{}
	u := db.URL{ID: 1, URL: "example.com"}
	store.urls = append(store.urls, u)
	store.departmentURLs = append(store.departmentURLs, db.DepartmentURL{DepartmentID: 1, URLID: u.ID, Enabled: true})
	store.categories = append(store.categories, db.Category{ID: 1, CitationID: 1, Name: "Harassment"})
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]uint{"category_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/legal/offences/example.com", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDetachOffence_NonOwningDepartment404(t *testing.T) {
	store := &fullMockStore{}
	seedOffence(store, 1)
	cookie := deptCookie(store, 2)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/legal/offences/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.urlOffences) != 1 {
		t.Fatalf("expected offence to remain untouched, got %d rows", len(store.urlOffences))
	}
}

func TestDetachOffence_OwningDepartmentSucceeds(t *testing.T) {
	store := &fullMockStore{}
	seedOffence(store, 1)
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/legal/offences/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.urlOffences) != 0 {
		t.Fatalf("expected offence to be removed, got %d rows", len(store.urlOffences))
	}
}
