package server

import (
	"context"
	"net/url"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
)

// fakeCaseLetterStore is a minimal db.Store double for testing the
// pagination loop in isolation — only the two methods it calls are wired,
// everything else panics if called (proving the test doesn't accidentally
// depend on unrelated store behavior). It embeds db.Store so it satisfies
// the interface without implementing every method.
type fakeCaseLetterStore struct {
	db.Store
	all []db.CaseLetterEntry
}

func (f *fakeCaseLetterStore) paginate(page, pageSize int) ([]db.CaseLetterEntry, int, error) {
	total := len(f.all)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return f.all[start:end], total, nil
}

func (f *fakeCaseLetterStore) ListCaseLetters(_ context.Context, page, pageSize int) ([]db.CaseLetterEntry, int, error) {
	return f.paginate(page, pageSize)
}

func (f *fakeCaseLetterStore) ListCaseLettersForDepartment(_ context.Context, page, pageSize int, _ uint) ([]db.CaseLetterEntry, int, error) {
	return f.paginate(page, pageSize)
}

func TestFetchAllCaseLetterEntries_LoopsBeyondOnePage(t *testing.T) {
	var all []db.CaseLetterEntry
	for i := uint(1); i <= 250; i++ { // > 2 full pages at pageSize 100
		all = append(all, db.CaseLetterEntry{CaseLetter: db.CaseLetter{ID: i}})
	}
	store := &fakeCaseLetterStore{all: all}

	got, err := fetchAllCaseLetterEntries(context.Background(), store, nil)
	if err != nil {
		t.Fatalf("fetchAllCaseLetterEntries: %v", err)
	}
	if len(got) != 250 {
		t.Fatalf("got %d letters, want 250 (must not truncate at the 100-per-page cap)", len(got))
	}
}

func TestFetchAllCaseLetterEntries_DepartmentScoped(t *testing.T) {
	store := &fakeCaseLetterStore{all: []db.CaseLetterEntry{
		{CaseLetter: db.CaseLetter{ID: 1}}, {CaseLetter: db.CaseLetter{ID: 2}},
	}}
	deptID := uint(5)
	got, err := fetchAllCaseLetterEntries(context.Background(), store, &deptID)
	if err != nil {
		t.Fatalf("fetchAllCaseLetterEntries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d letters, want 2", len(got))
	}
}

func TestParseIDSet_AbsentMeansNoFilter(t *testing.T) {
	ids, ok := parseIDSet(url.Values{}, "case_ids")
	if ok {
		t.Fatalf("ok = true for absent param, want false (no filter)")
	}
	if ids != nil {
		t.Fatalf("ids = %v, want nil", ids)
	}
}

func TestParseIDSet_PresentButEmptyMeansZeroMatches(t *testing.T) {
	// "?case_ids=" — an empty "current view" selection (Finding 2) — must
	// filter to zero ids, not fall through to "no filter".
	ids, ok := parseIDSet(url.Values{"case_ids": {""}}, "case_ids")
	if !ok {
		t.Fatalf("ok = false for present-but-empty param, want true (filter to zero matches)")
	}
	if len(ids) != 0 {
		t.Fatalf("ids = %v, want empty", ids)
	}
}

func TestParseIDSet_ParsesAndSkipsInvalid(t *testing.T) {
	ids, ok := parseIDSet(url.Values{"case_ids": {"1,2,notanumber,3"}}, "case_ids")
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	want := map[uint]bool{1: true, 2: true, 3: true}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for id := range want {
		if !ids[id] {
			t.Fatalf("missing id %d in %v", id, ids)
		}
	}
}
