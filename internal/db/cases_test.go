package db_test

import (
	"context"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
)

func TestCreateCase_LinksURLWithPhase(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, err := store.CreateDepartment(ctx, "CRD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	u, err := store.CreateURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

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
	dept, err := store.CreateDepartment(ctx, "CMOD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	u, err := store.CreateURL(ctx, "example2.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
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

func TestAddURLToCase_CoversMultipleURLs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, err := store.CreateDepartment(ctx, "CMOD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	u1, err := store.CreateURL(ctx, "batch1.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	u2, err := store.CreateURL(ctx, "batch2.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	c, err := store.CreateCase(ctx, dept.ID, u1.ID, "requested")
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddURLToCase(ctx, c.ID, u2.ID, "requested"); err != nil {
		t.Fatalf("AddURLToCase: %v", err)
	}

	for _, urlValue := range []string{"batch1.com", "batch2.com"} {
		cases, err := store.ListCasesForURL(ctx, urlValue)
		if err != nil {
			t.Fatalf("ListCasesForURL(%s): %v", urlValue, err)
		}
		if len(cases) != 1 || cases[0].ID != c.ID {
			t.Fatalf("ListCasesForURL(%s) = %+v, want the shared case %d", urlValue, cases, c.ID)
		}
	}
}

func TestListCaseLetters_ScopesByDepartmentAndCarriesURLs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	crd, err := store.CreateDepartment(ctx, "CRD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	cmod, err := store.CreateDepartment(ctx, "CMOD")
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}
	u, err := store.CreateURL(ctx, "docs-page.com")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	crdCase, err := store.CreateCase(ctx, crd.ID, u.ID, "requested")
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: crdCase.ID, Type: "Notice"}); err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}
	cmodCase, err := store.CreateCase(ctx, cmod.ID, u.ID, "requested")
	if err != nil {
		t.Fatalf("CreateCase: %v", err)
	}
	if _, err := store.AddCaseLetter(ctx, db.CaseLetter{CaseID: cmodCase.ID, Type: "Memo"}); err != nil {
		t.Fatalf("AddCaseLetter: %v", err)
	}

	all, total, err := store.ListCaseLetters(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListCaseLetters: %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Fatalf("ListCaseLetters total=%d len=%d, want 2 and 2", total, len(all))
	}
	for _, e := range all {
		if len(e.URLs) != 1 || e.URLs[0] != "docs-page.com" {
			t.Errorf("entry %+v: URLs = %v, want [docs-page.com]", e.Type, e.URLs)
		}
	}

	crdOnly, crdTotal, err := store.ListCaseLettersForDepartment(ctx, 1, 10, crd.ID)
	if err != nil {
		t.Fatalf("ListCaseLettersForDepartment: %v", err)
	}
	if crdTotal != 1 || len(crdOnly) != 1 || crdOnly[0].Type != "Notice" || crdOnly[0].DepartmentName != "CRD" {
		t.Fatalf("ListCaseLettersForDepartment(CRD) = %+v, want one Notice letter from CRD", crdOnly)
	}
}
