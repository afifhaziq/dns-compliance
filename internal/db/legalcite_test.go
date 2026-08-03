package db_test

import (
	"context"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/glebarez/sqlite"
)

func intPtr(n int) *int { return &n }

// newCascadeTestStore is like newTestStore but with FK enforcement turned
// on. glebarez/sqlite doesn't enforce foreign keys (including ON DELETE
// CASCADE) by default — Postgres, the production driver, always does — so
// plain newTestStore silently no-ops cascade deletes instead of exercising
// them. Kept as a separate helper (not a change to the shared
// newTestStore) because several existing tests elsewhere in this package
// insert rows with intentionally dangling FKs (e.g. ScanResult.URLID: 0)
// that only this new legal-citation hierarchy's tests need to avoid.
func newCascadeTestStore(t *testing.T) db.Store {
	t.Helper()
	gormDB, err := db.Connect(sqlite.Open(":memory:?_pragma=foreign_keys(1)"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return db.NewStore(gormDB)
}

func seedCMA233(t *testing.T, s db.Store, ctx context.Context) (db.Instrument, db.Citation, db.Category, db.Element) {
	t.Helper()
	instrument, err := s.GetOrCreateInstrument(ctx, db.Instrument{
		Type: "ACT", Jurisdiction: "FEDERAL", Number: "588", Year: intPtr(1998),
		ShortTitle: "Communications and Multimedia Act 1998",
	})
	if err != nil {
		t.Fatalf("GetOrCreateInstrument: %v", err)
	}
	citation, err := s.CreateCitation(ctx, db.Citation{
		InstrumentID: instrument.ID,
		RawText:      "Seksyen 233(1)(a)",
		Parsed: db.LegalCitationParsed{
			ProvisionNum: intPtr(233), SubProvision: intPtr(1), Paragraph: "a",
		},
		ParseConfidence: "OK",
	})
	if err != nil {
		t.Fatalf("CreateCitation: %v", err)
	}
	category, err := s.CreateCategory(ctx, db.Category{CitationID: citation.ID, Name: "Harassment"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	element, err := s.CreateElement(ctx, db.Element{CategoryID: category.ID, Name: "Menacing"})
	if err != nil {
		t.Fatalf("CreateElement: %v", err)
	}
	return instrument, citation, category, element
}

func TestGetOrCreateInstrument_DedupesByNaturalKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.GetOrCreateInstrument(ctx, db.Instrument{
		Type: "ACT", Jurisdiction: "FEDERAL", Number: "588", Year: intPtr(1998),
		ShortTitle: "Communications and Multimedia Act 1998",
	})
	if err != nil {
		t.Fatalf("GetOrCreateInstrument: %v", err)
	}
	b, err := s.GetOrCreateInstrument(ctx, db.Instrument{
		Type: "ACT", Jurisdiction: "FEDERAL", Number: "588", Year: intPtr(1998),
		ShortTitle: "Communications and Multimedia Act 1998",
	})
	if err != nil {
		t.Fatalf("GetOrCreateInstrument: %v", err)
	}
	if a.ID != b.ID {
		t.Fatalf("expected same instrument row, got ids %d and %d", a.ID, b.ID)
	}

	instruments, err := s.ListInstruments(ctx)
	if err != nil {
		t.Fatalf("ListInstruments: %v", err)
	}
	if len(instruments) != 1 {
		t.Fatalf("expected exactly 1 instrument, got %d", len(instruments))
	}
}

func TestGetOrCreateInstrument_NilYearIsDistinctFromSetYear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	withYear, err := s.GetOrCreateInstrument(ctx, db.Instrument{
		Type: "ACT", Jurisdiction: "FEDERAL", Number: "1", Year: intPtr(2000), ShortTitle: "Test Act 2000",
	})
	if err != nil {
		t.Fatalf("GetOrCreateInstrument (with year): %v", err)
	}
	noYear, err := s.GetOrCreateInstrument(ctx, db.Instrument{
		Type: "ACT", Jurisdiction: "FEDERAL", Number: "1", Year: nil, ShortTitle: "Test Act (undated)",
	})
	if err != nil {
		t.Fatalf("GetOrCreateInstrument (no year): %v", err)
	}
	if withYear.ID == noYear.ID {
		t.Fatalf("expected distinct rows for nil vs set year, got same id %d", withYear.ID)
	}
}

func TestCitation_SortKeyOrdersSuffixCorrectly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	instrument, err := s.GetOrCreateInstrument(ctx, db.Instrument{
		Type: "ACT", Jurisdiction: "FEDERAL", Number: "1", ShortTitle: "Test Act",
	})
	if err != nil {
		t.Fatalf("GetOrCreateInstrument: %v", err)
	}

	for _, raw := range []struct {
		text   string
		num    int
		suffix string
	}{
		{"s 4", 4, ""},
		{"s 4A", 4, "A"},
		{"s 40", 40, ""},
	} {
		if _, err := s.CreateCitation(ctx, db.Citation{
			InstrumentID: instrument.ID, RawText: raw.text,
			Parsed:          db.LegalCitationParsed{ProvisionNum: intPtr(raw.num), ProvisionSuffix: raw.suffix},
			ParseConfidence: "OK",
		}); err != nil {
			t.Fatalf("CreateCitation(%s): %v", raw.text, err)
		}
	}

	citations, err := s.ListCitationsByInstrument(ctx, instrument.ID)
	if err != nil {
		t.Fatalf("ListCitationsByInstrument: %v", err)
	}
	if len(citations) != 3 {
		t.Fatalf("expected 3 citations, got %d", len(citations))
	}
	got := []string{citations[0].RawText, citations[1].RawText, citations[2].RawText}
	want := []string{"s 4", "s 4A", "s 40"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected sort order %v, got %v", want, got)
		}
	}
}

func TestCitation_ParsedJSONBRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	instrument, err := s.GetOrCreateInstrument(ctx, db.Instrument{
		Type: "CONSTITUTION", Jurisdiction: "FEDERAL", Number: "n/a", ShortTitle: "Federal Constitution",
	})
	if err != nil {
		t.Fatalf("GetOrCreateInstrument: %v", err)
	}
	created, err := s.CreateCitation(ctx, db.Citation{
		InstrumentID: instrument.ID,
		RawText:      "Perkara 121(1A)",
		Parsed: db.LegalCitationParsed{
			ProvisionNum: intPtr(121), SubProvision: intPtr(1), SubProvisionSuffix: "A",
		},
		ParseConfidence: "OK",
	})
	if err != nil {
		t.Fatalf("CreateCitation: %v", err)
	}

	citations, err := s.ListCitationsByInstrument(ctx, instrument.ID)
	if err != nil {
		t.Fatalf("ListCitationsByInstrument: %v", err)
	}
	if len(citations) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(citations))
	}
	got := citations[0].Parsed
	if got.ProvisionNum == nil || *got.ProvisionNum != 121 {
		t.Fatalf("expected ProvisionNum 121, got %+v", got)
	}
	if got.SubProvision == nil || *got.SubProvision != 1 || got.SubProvisionSuffix != "A" {
		t.Fatalf("expected SubProvision 1A, got %+v", got)
	}

	// UpdateCitation must also round-trip Parsed (exercises the struct-based
	// Save path, not a map Updates, since jsonb needs the serializer to run).
	updated, err := s.UpdateCitation(ctx, created.ID, db.Citation{
		InstrumentID:    instrument.ID,
		RawText:         "Perkara 121(1B)",
		Parsed:          db.LegalCitationParsed{ProvisionNum: intPtr(121), SubProvision: intPtr(1), SubProvisionSuffix: "B"},
		ParseConfidence: "OK",
	})
	if err != nil {
		t.Fatalf("UpdateCitation: %v", err)
	}
	if updated.Parsed.SubProvisionSuffix != "B" {
		t.Fatalf("expected updated suffix B, got %+v", updated.Parsed)
	}
	citations, _ = s.ListCitationsByInstrument(ctx, instrument.ID)
	if citations[0].Parsed.SubProvisionSuffix != "B" {
		t.Fatalf("expected persisted suffix B, got %+v", citations[0].Parsed)
	}
}

func TestAttachAndListOffencesByURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _, category, element := seedCMA233(t, s, ctx)

	if _, err := s.CreateURL(ctx, "https://example.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID)
	if err != nil {
		t.Fatalf("AttachOffenceToURL: %v", err)
	}
	if offence.ID == 0 {
		t.Fatal("expected non-zero offence ID")
	}

	offences, err := s.ListOffencesByURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListOffencesByURL: %v", err)
	}
	if len(offences) != 1 {
		t.Fatalf("expected 1 offence, got %d", len(offences))
	}
	got := offences[0]
	if got.Category.Name != "Harassment" || got.Category.Citation.RawText != "Seksyen 233(1)(a)" {
		t.Fatalf("expected preloaded Category->Citation chain, got %+v", got.Category)
	}
	if got.Category.Citation.Instrument.ShortTitle != "Communications and Multimedia Act 1998" {
		t.Fatalf("expected preloaded Instrument, got %+v", got.Category.Citation.Instrument)
	}
	if got.Element == nil || got.Element.Name != "Menacing" {
		t.Fatalf("expected preloaded Element, got %+v", got.Element)
	}
}

func TestAttachOffenceToURL_NilElementAllowed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _, category, _ := seedCMA233(t, s, ctx)

	if _, err := s.CreateURL(ctx, "https://example.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}

	offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, nil)
	if err != nil {
		t.Fatalf("AttachOffenceToURL: %v", err)
	}
	if offence.ElementID != nil {
		t.Fatalf("expected nil ElementID, got %v", offence.ElementID)
	}

	offences, err := s.ListOffencesByURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListOffencesByURL: %v", err)
	}
	if len(offences) != 1 || offences[0].Element != nil {
		t.Fatalf("expected 1 offence with nil Element, got %+v", offences)
	}
}

func TestDetachOffenceFromURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _, category, element := seedCMA233(t, s, ctx)
	if _, err := s.CreateURL(ctx, "https://example.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID)
	if err != nil {
		t.Fatalf("AttachOffenceToURL: %v", err)
	}

	if err := s.DetachOffenceFromURL(ctx, offence.ID); err != nil {
		t.Fatalf("DetachOffenceFromURL: %v", err)
	}

	offences, err := s.ListOffencesByURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListOffencesByURL: %v", err)
	}
	if len(offences) != 0 {
		t.Fatalf("expected 0 offences after detach, got %d", len(offences))
	}
}

func TestDeleteInstrument_CascadesThroughWholeChain(t *testing.T) {
	s := newCascadeTestStore(t)
	ctx := context.Background()
	instrument, _, category, element := seedCMA233(t, s, ctx)
	if _, err := s.CreateURL(ctx, "https://example.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	if _, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID); err != nil {
		t.Fatalf("AttachOffenceToURL: %v", err)
	}

	if err := s.DeleteInstrument(ctx, instrument.ID); err != nil {
		t.Fatalf("DeleteInstrument: %v", err)
	}

	citations, err := s.ListCitationsByInstrument(ctx, instrument.ID)
	if err != nil {
		t.Fatalf("ListCitationsByInstrument: %v", err)
	}
	if len(citations) != 0 {
		t.Fatalf("expected citations to cascade-delete, got %d", len(citations))
	}
	categories, err := s.ListCategoriesByCitation(ctx, category.CitationID)
	if err != nil {
		t.Fatalf("ListCategoriesByCitation: %v", err)
	}
	if len(categories) != 0 {
		t.Fatalf("expected categories to cascade-delete, got %d", len(categories))
	}
	offences, err := s.ListOffencesByURL(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListOffencesByURL: %v", err)
	}
	if len(offences) != 0 {
		t.Fatalf("expected url_offence to cascade-delete, got %d", len(offences))
	}
}

func TestGetOffence_PreloadsURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _, category, element := seedCMA233(t, s, ctx)
	if _, err := s.CreateURL(ctx, "https://example.com"); err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	offence, err := s.AttachOffenceToURL(ctx, "example.com", category.ID, &element.ID)
	if err != nil {
		t.Fatalf("AttachOffenceToURL: %v", err)
	}

	got, err := s.GetOffence(ctx, offence.ID)
	if err != nil {
		t.Fatalf("GetOffence: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil offence")
	}
	if got.URL.URL != "example.com" {
		t.Fatalf("expected preloaded URL, got %+v", got.URL)
	}

	missing, err := s.GetOffence(ctx, 999999)
	if err != nil {
		t.Fatalf("GetOffence (missing): %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing offence, got %+v", missing)
	}
}

func TestCategoryAndElementCRUD(t *testing.T) {
	s := newCascadeTestStore(t)
	ctx := context.Background()
	_, citation, category, _ := seedCMA233(t, s, ctx)

	updatedCat, err := s.UpdateCategory(ctx, category.ID, "Bullying or Harassment")
	if err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	if updatedCat.Name != "Bullying or Harassment" {
		t.Fatalf("expected updated name, got %q", updatedCat.Name)
	}

	el, err := s.CreateElement(ctx, db.Element{CategoryID: category.ID, Name: "Obscene"})
	if err != nil {
		t.Fatalf("CreateElement: %v", err)
	}
	updatedEl, err := s.UpdateElement(ctx, el.ID, "Obscene Content")
	if err != nil {
		t.Fatalf("UpdateElement: %v", err)
	}
	if updatedEl.Name != "Obscene Content" {
		t.Fatalf("expected updated element name, got %q", updatedEl.Name)
	}

	elements, err := s.ListElementsByCategory(ctx, category.ID)
	if err != nil {
		t.Fatalf("ListElementsByCategory: %v", err)
	}
	if len(elements) != 2 {
		t.Fatalf("expected 2 elements (Menacing + Obscene Content), got %d", len(elements))
	}

	if err := s.DeleteCategory(ctx, category.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	elements, err = s.ListElementsByCategory(ctx, category.ID)
	if err != nil {
		t.Fatalf("ListElementsByCategory: %v", err)
	}
	if len(elements) != 0 {
		t.Fatalf("expected elements to cascade-delete with category, got %d", len(elements))
	}

	categories, err := s.ListCategoriesByCitation(ctx, citation.ID)
	if err != nil {
		t.Fatalf("ListCategoriesByCitation: %v", err)
	}
	if len(categories) != 0 {
		t.Fatalf("expected category to be gone, got %d", len(categories))
	}
}
