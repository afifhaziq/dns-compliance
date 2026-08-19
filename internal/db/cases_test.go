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
