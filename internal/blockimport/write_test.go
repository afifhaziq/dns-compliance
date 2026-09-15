package blockimport

import (
	"context"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestGormDB(t *testing.T) *gorm.DB {
	t.Helper()
	gormDB, err := db.Connect(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	return gormDB
}

func mustSeedDepartment(t *testing.T, gdb *gorm.DB, name string) db.Department {
	t.Helper()
	dept := db.Department{Name: name}
	if err := gdb.Create(&dept).Error; err != nil {
		t.Fatalf("seeding department %q: %v", name, err)
	}
	return dept
}

func TestWriteCRDCases_CreatesOneCasePerReference(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: "example.com", Status: "Blocked"}},
		Categories:      []string{"Judi"},
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1", summary.CasesCreated)
	}
	var count int64
	gdb.Model(&db.Case{}).Count(&count)
	if count != 1 {
		t.Fatalf("cases table has %d rows, want 1", count)
	}

	var letter db.CaseLetter
	if err := gdb.First(&letter).Error; err != nil {
		t.Fatalf("expected a CaseLetter row: %v", err)
	}
	if letter.ReferenceNumberExternal != "REF-1" || letter.Type != "Notice" {
		t.Fatalf("got letter %+v", letter)
	}

	var caseURL db.CaseURL
	if err := gdb.First(&caseURL).Error; err != nil {
		t.Fatalf("expected a CaseURL row: %v", err)
	}
	if caseURL.Status != "blocked" {
		t.Fatalf("caseURL.Status = %q, want blocked", caseURL.Status)
	}
	if caseURL.OriginalURL != "example.com" {
		t.Fatalf("caseURL.OriginalURL = %q, want example.com", caseURL.OriginalURL)
	}

	var u db.URL
	if err := gdb.First(&u, caseURL.URLID).Error; err != nil {
		t.Fatalf("expected a URL row: %v", err)
	}
	if u.URL != "example.com" {
		t.Fatalf("u.URL = %q, want example.com", u.URL)
	}
}

func TestWriteCRDCases_IsIdempotent(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: "example.com", Status: "Blocked"}},
		Categories:      []string{"Judi"},
	}}

	if _, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false); err != nil {
		t.Fatalf("first WriteCRDCases: %v", err)
	}
	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false)
	if err != nil {
		t.Fatalf("second WriteCRDCases: %v", err)
	}
	if summary.CasesSkippedExist != 1 || summary.CasesCreated != 0 {
		t.Fatalf("got %+v, want CasesSkippedExist=1 CasesCreated=0", summary)
	}
	var count int64
	gdb.Model(&db.Case{}).Count(&count)
	if count != 1 {
		t.Fatalf("cases table has %d rows, want 1", count)
	}
}

func TestWriteCRDCases_SkipsEmptyDomain(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains: []CollapsedDomain{
			{RawDomain: "example.com", Status: "Blocked"},
			{RawDomain: "   ", Status: "Blocked"},
		},
		Categories: []string{"Judi"},
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.URLsSkippedBadURL != 1 {
		t.Fatalf("URLsSkippedBadURL = %d, want 1", summary.URLsSkippedBadURL)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1 (case still created for the valid domain)", summary.CasesCreated)
	}
	var caseURLCount int64
	gdb.Model(&db.CaseURL{}).Count(&caseURLCount)
	if caseURLCount != 1 {
		t.Fatalf("case_urls has %d rows, want 1", caseURLCount)
	}
}

// TestWriteCRDCases_RetainsUnnormalizableURLViaFallback guards item 13
// (docs/blocking-list-open-questions.md): a row too garbled for
// urlnorm.Normalize to extract any hostname from (e.g. invalid port syntax)
// must not be silently dropped -- it falls back to a raw storage key so the
// case/citation record and the exact cited text (OriginalURL) both survive.
func TestWriteCRDCases_RetainsUnnormalizableURLViaFallback(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	const garbled = "https://solar123movies.cB33:B69om/"
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: garbled, Status: "Blocked"}},
		Categories:      []string{"Judi"},
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.URLsSkippedBadURL != 0 {
		t.Fatalf("URLsSkippedBadURL = %d, want 0 (retained via fallback, not skipped)", summary.URLsSkippedBadURL)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1", summary.CasesCreated)
	}

	var caseURL db.CaseURL
	if err := gdb.First(&caseURL).Error; err != nil {
		t.Fatalf("expected a CaseURL row: %v", err)
	}
	if caseURL.OriginalURL != garbled {
		t.Fatalf("caseURL.OriginalURL = %q, want %q", caseURL.OriginalURL, garbled)
	}
}

func TestWriteCRDCases_DedupesDomainsThatNormalizeToTheSameURL(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains: []CollapsedDomain{
			{RawDomain: "http://example.com", Status: "Blocked"},
			{RawDomain: "https://example.com/", Status: "Uplift"},
		},
		Categories: []string{"Judi"},
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1", summary.CasesCreated)
	}
	var caseURLCount int64
	gdb.Model(&db.CaseURL{}).Count(&caseURLCount)
	if caseURLCount != 1 {
		t.Fatalf("case_urls has %d rows, want 1 (deduped by normalized URL)", caseURLCount)
	}
}

func TestWriteCRDCases_DryRunWritesNothing(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: "example.com", Status: "Blocked"}},
		Categories:      []string{"Judi"},
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, true)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1 (dry run still reports what would happen)", summary.CasesCreated)
	}
	var count int64
	gdb.Model(&db.Case{}).Count(&count)
	if count != 0 {
		t.Fatalf("cases table has %d rows, want 0 after dry run", count)
	}
	gdb.Model(&db.URL{}).Count(&count)
	if count != 0 {
		t.Fatalf("urls table has %d rows, want 0 after dry run", count)
	}
}

func TestWriteCRDCases_TracksCategoriesObserved(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{
		{ReferenceNumber: "REF-1", Domains: []CollapsedDomain{{RawDomain: "a.com", Status: "Blocked"}}, Categories: []string{"Judi"}},
		{ReferenceNumber: "REF-2", Domains: []CollapsedDomain{{RawDomain: "b.com", Status: "Blocked"}}, Categories: []string{"Judi", "Palsu"}},
	}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, true)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CategoriesObserved["Judi"] != 2 || summary.CategoriesObserved["Palsu"] != 1 {
		t.Fatalf("got %+v", summary.CategoriesObserved)
	}
}

// TestWriteCRDCases_PreservesOriginalURLPath guards against OriginalURL
// silently degrading to the same bare-hostname value URL.URL stores — the
// whole point of the field is to keep what urlnorm.Normalize strips.
func TestWriteCRDCases_PreservesOriginalURLPath(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: "https://t.me/SomeChannel123", Status: "Blocked"}},
		Categories:      []string{"Lucah"},
	}}

	if _, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false); err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}

	var caseURL db.CaseURL
	if err := gdb.First(&caseURL).Error; err != nil {
		t.Fatalf("expected a CaseURL row: %v", err)
	}
	if caseURL.OriginalURL != "https://t.me/SomeChannel123" {
		t.Fatalf("caseURL.OriginalURL = %q, want https://t.me/SomeChannel123", caseURL.OriginalURL)
	}

	var u db.URL
	if err := gdb.First(&u, caseURL.URLID).Error; err != nil {
		t.Fatalf("expected a URL row: %v", err)
	}
	if u.URL != "t.me" {
		t.Fatalf("u.URL = %q, want t.me (bare hostname, unlike OriginalURL)", u.URL)
	}
}

func TestWriteCRDCases_AttachesOffencesViaCitationMap(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	citationMap := map[string][]citationTarget{
		"Seksyen 233 Akta Komunikasi dan Multimedia 1998": {
			{Instrument: "Akta Komunikasi dan Multimedia 1998", Provision: "Seksyen 233"},
		},
	}
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: "example.com", Status: "Blocked"}},
		Categories:      []string{"Jelik", "Palsu"},
		Element:         "Politik",
		CitationText:    "Seksyen 233 Akta Komunikasi dan Multimedia 1998",
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, citationMap, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	// 2 categories x 1 url = 2 URLOffence rows, sharing one Instrument/Citation.
	if summary.URLOffencesCreated != 2 {
		t.Fatalf("URLOffencesCreated = %d, want 2", summary.URLOffencesCreated)
	}
	if summary.OffencesSkippedNoCitation != 0 {
		t.Fatalf("OffencesSkippedNoCitation = %d, want 0", summary.OffencesSkippedNoCitation)
	}

	var instrumentCount, citationCount, categoryCount, elementCount, offenceCount int64
	gdb.Model(&db.Instrument{}).Count(&instrumentCount)
	gdb.Model(&db.Citation{}).Count(&citationCount)
	gdb.Model(&db.Category{}).Count(&categoryCount)
	gdb.Model(&db.Element{}).Count(&elementCount)
	gdb.Model(&db.URLOffence{}).Count(&offenceCount)
	if instrumentCount != 1 || citationCount != 1 || categoryCount != 2 || elementCount != 2 || offenceCount != 2 {
		t.Fatalf("got instruments=%d citations=%d categories=%d elements=%d offences=%d, want 1,1,2,2,2",
			instrumentCount, citationCount, categoryCount, elementCount, offenceCount)
	}

	var instrument db.Instrument
	if err := gdb.First(&instrument).Error; err != nil {
		t.Fatalf("expected an Instrument row: %v", err)
	}
	if instrument.Type != "ACT" || instrument.ShortTitle != "Akta Komunikasi dan Multimedia 1998" {
		t.Fatalf("got instrument %+v", instrument)
	}
}

func TestWriteCRDCases_SharesInstrumentAndCategoryAcrossCases(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	citationMap := map[string][]citationTarget{
		"Seksyen 233 AKM 1998": {{Instrument: "Akta Komunikasi dan Multimedia 1998", Provision: "Seksyen 233"}},
	}
	cases := []CollapsedCase{
		{ReferenceNumber: "REF-1", Domains: []CollapsedDomain{{RawDomain: "a.com", Status: "Blocked"}}, Categories: []string{"Judi"}, CitationText: "Seksyen 233 AKM 1998"},
		{ReferenceNumber: "REF-2", Domains: []CollapsedDomain{{RawDomain: "b.com", Status: "Blocked"}}, Categories: []string{"Judi"}, CitationText: "Seksyen 233 AKM 1998"},
	}

	if _, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, citationMap, false); err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}

	var instrumentCount, citationCount, categoryCount, offenceCount int64
	gdb.Model(&db.Instrument{}).Count(&instrumentCount)
	gdb.Model(&db.Citation{}).Count(&citationCount)
	gdb.Model(&db.Category{}).Count(&categoryCount)
	gdb.Model(&db.URLOffence{}).Count(&offenceCount)
	if instrumentCount != 1 || citationCount != 1 || categoryCount != 1 || offenceCount != 2 {
		t.Fatalf("got instruments=%d citations=%d categories=%d offences=%d, want 1,1,1,2 (shared catalog rows, one offence per url)",
			instrumentCount, citationCount, categoryCount, offenceCount)
	}
}

func TestWriteCRDCases_SkipsOffencesWhenCitationUnclassified(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{{
		ReferenceNumber: "REF-1",
		Domains:         []CollapsedDomain{{RawDomain: "example.com", Status: "Blocked"}},
		Categories:      []string{"Judi"},
		CitationText:    "Some Citation Not In The Classification CSV",
	}}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, map[string][]citationTarget{}, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.OffencesSkippedNoCitation != 1 {
		t.Fatalf("OffencesSkippedNoCitation = %d, want 1", summary.OffencesSkippedNoCitation)
	}
	if summary.CasesCreated != 1 {
		t.Fatalf("CasesCreated = %d, want 1 (case still created without offence data)", summary.CasesCreated)
	}
	var offenceCount int64
	gdb.Model(&db.URLOffence{}).Count(&offenceCount)
	if offenceCount != 0 {
		t.Fatalf("url_offences has %d rows, want 0", offenceCount)
	}
}

func TestWriteCRDCases_RoutesReferenceByPrefix(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{
		{ReferenceNumber: "SKMM(T)09-NMD/800/2013/Jld.1(011)", Domains: []CollapsedDomain{{RawDomain: "a.com", Status: "Blocked"}}},
		{ReferenceNumber: "MCMC(S)CMOD/BLK/2025(62-2)", Domains: []CollapsedDomain{{RawDomain: "b.com", Status: "Blocked"}}},
		{ReferenceNumber: "JK KPN(PR) 168/6", Domains: []CollapsedDomain{{RawDomain: "c.com", Status: "Blocked"}}},
		{ReferenceNumber: "SB-2021-0070-HQR", Domains: []CollapsedDomain{{RawDomain: "d.com", Status: "Blocked"}}},
	}

	if _, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false); err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}

	var letters []db.CaseLetter
	if err := gdb.Find(&letters).Error; err != nil {
		t.Fatalf("listing letters: %v", err)
	}
	got := map[string]string{} // reference -> "internal" or "external"
	for _, l := range letters {
		if l.ReferenceNumberInternal != "" {
			got[l.ReferenceNumberInternal] = "internal"
		}
		if l.ReferenceNumberExternal != "" {
			got[l.ReferenceNumberExternal] = "external"
		}
	}
	want := map[string]string{
		"SKMM(T)09-NMD/800/2013/Jld.1(011)": "internal",
		"MCMC(S)CMOD/BLK/2025(62-2)":        "internal",
		"JK KPN(PR) 168/6":                  "external",
		"SB-2021-0070-HQR":                  "external",
	}
	for ref, wantField := range want {
		if got[ref] != wantField {
			t.Errorf("reference %q routed to %q, want %q", ref, got[ref], wantField)
		}
	}
}

func TestWriteCRDCases_IdempotentAcrossInternalAndExternalReference(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{
		{ReferenceNumber: "SKMM(T)09-NMD/800/2013/Jld.1(011)", Domains: []CollapsedDomain{{RawDomain: "a.com", Status: "Blocked"}}},
	}

	if _, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false); err != nil {
		t.Fatalf("first WriteCRDCases: %v", err)
	}
	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false)
	if err != nil {
		t.Fatalf("second WriteCRDCases: %v", err)
	}
	if summary.CasesSkippedExist != 1 || summary.CasesCreated != 0 {
		t.Fatalf("got %+v, want CasesSkippedExist=1 CasesCreated=0 (rerun must find the internal-routed reference)", summary)
	}
}

// TestWriteCRDCases_ExternalIdempotencyIsPerDomain guards the fix that made
// the rerun check for a non-internal reference match on (reference, domain)
// together instead of the reference text alone -- otherwise two different
// domains sharing a blanket external reference (like PDRM's real
// "JK KPN(PR) 168/6") would wrongly read the second one as "already
// imported" once the first exists, silently dropping it.
func TestWriteCRDCases_ExternalIdempotencyIsPerDomain(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	first := []CollapsedCase{
		{ReferenceNumber: "JK KPN(PR) 168/6", Domains: []CollapsedDomain{{RawDomain: "a.com", Status: "Blocked"}}},
	}
	if _, err := WriteCRDCases(context.Background(), gdb, crd.ID, first, nil, false); err != nil {
		t.Fatalf("first WriteCRDCases: %v", err)
	}

	// A different domain under the same blanket external reference must
	// still be created, not skipped as "already exists".
	second := []CollapsedCase{
		{ReferenceNumber: "JK KPN(PR) 168/6", Domains: []CollapsedDomain{{RawDomain: "b.com", Status: "Blocked"}}},
	}
	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, second, nil, false)
	if err != nil {
		t.Fatalf("second WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 1 || summary.CasesSkippedExist != 0 {
		t.Fatalf("got %+v, want CasesCreated=1 CasesSkippedExist=0 (different domain, same blanket reference)", summary)
	}

	// Re-running the exact same (reference, domain) pair must be skipped.
	summary, err = WriteCRDCases(context.Background(), gdb, crd.ID, second, nil, false)
	if err != nil {
		t.Fatalf("third WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 0 || summary.CasesSkippedExist != 1 {
		t.Fatalf("got %+v, want CasesCreated=0 CasesSkippedExist=1 (same reference and domain rerun)", summary)
	}

	var caseCount int64
	gdb.Model(&db.Case{}).Count(&caseCount)
	if caseCount != 2 {
		t.Fatalf("cases table has %d rows, want 2 (a.com and b.com stay separate cases)", caseCount)
	}
}

// TestWriteCRDCases_NMSMDGoesOnExternalRef guards the general NMSMD ->
// ReferenceNumberExternal transfer -- independent of whether the case's own
// primary reference routed internal or external.
func TestWriteCRDCases_NMSMDGoesOnExternalRef(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{
		{
			ReferenceNumber: "SKMM(T)09-NMD/800/2014 (023)",
			NMSMD:           "SKMM(T)09-NMD/800/2021 (095)",
			Domains:         []CollapsedDomain{{RawDomain: "a.com", Status: "Blocked"}},
		},
	}
	if _, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false); err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}

	var letter db.CaseLetter
	if err := gdb.First(&letter).Error; err != nil {
		t.Fatalf("expected a CaseLetter row: %v", err)
	}
	if letter.ReferenceNumberInternal != "SKMM(T)09-NMD/800/2014 (023)" {
		t.Fatalf("ReferenceNumberInternal = %q, want the primary NMD", letter.ReferenceNumberInternal)
	}
	if letter.ReferenceNumberExternal != "SKMM(T)09-NMD/800/2021 (095)" {
		t.Fatalf("ReferenceNumberExternal = %q, want the NMSMD value", letter.ReferenceNumberExternal)
	}
}

// TestWriteCRDCases_BlankRefDoesNotMatchInternalCaseSharingADomain guards a
// second idempotency false-positive: a domain that appears twice in the
// sheet, once under a real internal case (whose own reference_number_external
// is empty, since it never had an NMSMD) and once with no reference at all,
// must not have its blank-reference occurrence silently skipped as "already
// exists" just because it happens to match that internal case's empty
// external slot plus shared domain.
func TestWriteCRDCases_BlankRefDoesNotMatchInternalCaseSharingADomain(t *testing.T) {
	gdb := newTestGormDB(t)
	crd := mustSeedDepartment(t, gdb, "CRD")
	cases := []CollapsedCase{
		{ReferenceNumber: "SKMM(T)09-NMD/800/2020 (059)", Domains: []CollapsedDomain{{RawDomain: "shared.com", Status: "Blocked"}}},
		{ReferenceNumber: "", Domains: []CollapsedDomain{{RawDomain: "shared.com", Status: "Blocked"}}},
	}

	summary, err := WriteCRDCases(context.Background(), gdb, crd.ID, cases, nil, false)
	if err != nil {
		t.Fatalf("WriteCRDCases: %v", err)
	}
	if summary.CasesCreated != 2 || summary.CasesSkippedExist != 0 {
		t.Fatalf("got %+v, want CasesCreated=2 CasesSkippedExist=0 (the blank-ref case must not be mistaken for the internal one)", summary)
	}
	var caseCount int64
	gdb.Model(&db.Case{}).Count(&caseCount)
	if caseCount != 2 {
		t.Fatalf("cases table has %d rows, want 2", caseCount)
	}
}

func TestMapCRDStatus(t *testing.T) {
	cases := map[string]string{
		"Blocked": "blocked", "blocked": "blocked",
		"Uplift": "uplift", "Suspended": "suspended",
		"Not Blocked": "not_blocked", "Not blocked": "not_blocked", "": "requested",
	}
	for in, want := range cases {
		if got := mapCRDStatus(in); got != want {
			t.Errorf("mapCRDStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
