package blockimport

import (
	"context"
	"errors"
	"strings"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/urlnorm"
	"gorm.io/gorm"
)

// errDryRunRollback forces gorm.Transaction to roll back a dry run whose
// body otherwise completed without error.
var errDryRunRollback = errors.New("blockimport: dry run rollback")

// ImportSummary is what a run (dry or real) reports. Shared by both the CRD
// and CMOD importers.
type ImportSummary struct {
	CasesCreated              int
	CasesSkippedExist         int            // already imported (idempotency)
	URLsSkippedBadURL         int            // failed urlnorm.Normalize
	CategoriesObserved        map[string]int // raw Category/Offence value -> row count, for visibility only
	URLOffencesCreated        int            // URLOffence rows created (CRD only, see WriteCRDCases)
	OffencesSkippedNoCitation int            // cases whose CitationText has no confirmed entry in the classification CSV
}

// mapCRDStatus maps the spreadsheet's Status values onto case_urls.status's
// vocabulary (requested | blocked | uplift | suspended | not_blocked |
// internal) — resolved per stakeholder sign-off, 2026-09-13, see
// docs/blocking-list-migration-clarifications.md Question 1. Only the empty
// cell (21 rows) falls through to "requested", the model's default start
// state.
func mapCRDStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "blocked":
		return "blocked"
	case "uplift":
		return "uplift"
	case "suspended":
		return "suspended"
	case "not blocked":
		return "not_blocked"
	default:
		return "requested"
	}
}

// normalizeOrFallback wraps urlnorm.Normalize with a last-resort fallback for
// the handful of historical import rows too garbled for it to extract any
// hostname at all (e.g. invalid port syntax) -- see item 13,
// docs/blocking-list-open-questions.md. Rather than dropping the case/
// citation record entirely, these fall back to a lowercased, trimmed copy of
// the raw cited text as the URL row's storage key -- unscannable, but no
// data is lost either way since CaseURL.OriginalURL always keeps the raw
// text verbatim regardless of which path produced the key. Only a
// genuinely-empty raw string (after trimming) returns "".
func normalizeOrFallback(raw string) string {
	if normalized, err := urlnorm.Normalize(raw); err == nil {
		return normalized
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

// createURL replicates internal/db.postgresStore.CreateURL's normalize +
// get-or-create semantics against a raw *gorm.DB (that method is unexported
// and scoped to db.Store, which this importer doesn't otherwise need).
func createURL(ctx context.Context, gdb *gorm.DB, rawURL string) (db.URL, error) {
	normalized := normalizeOrFallback(rawURL)
	if normalized == "" {
		return db.URL{}, errors.New("blockimport: empty domain, nothing to store")
	}
	var u db.URL
	err := gdb.WithContext(ctx).
		Where("url = ?", normalized).
		Attrs(db.URL{URL: normalized}).
		FirstOrCreate(&u).Error
	return u, err
}

// getOrCreateAgency get-or-creates an Agency row by exact (already-trimmed,
// see CRDRow.Agency/cellAt) name match, same FirstOrCreate pattern as
// createURL. "" is not a valid agency and never reaches here -- callers
// check d.Agency != "" first.
func getOrCreateAgency(ctx context.Context, gdb *gorm.DB, name string) (db.Agency, error) {
	var a db.Agency
	err := gdb.WithContext(ctx).
		Where("name = ?", name).
		Attrs(db.Agency{Name: name}).
		FirstOrCreate(&a).Error
	return a, err
}

// WriteCRDCases creates one Case (DepartmentID = the CRD department's ID)
// per CollapsedCase, one CaseLetter (Type: "Notice") carrying the reference
// number, one CaseURL per domain (Status mapped from the domain's Status via
// mapCRDStatus, AgencyID get-or-created by name from that domain's own
// CollapsedDomain.Agency — "Agensi" — via getOrCreateAgency, left nil when
// the domain has none), and —
// via citationMap, see LoadCitationClassification — one
// URLOffence per (resolved Citation x split Category) combination for every
// domain in the case. A case whose CitationText has no confirmed entry in
// citationMap still gets its Case/CaseURL rows; it just carries no offence
// data (OffencesSkippedNoCitation counts these, for visibility). Pass a nil
// citationMap to skip offence attachment entirely. dryRun=true does every
// lookup/validation but wraps all writes in a transaction that's always
// rolled back, so ImportSummary reflects exactly what a real run would do.
func WriteCRDCases(ctx context.Context, gdb *gorm.DB, crdDeptID uint, cases []CollapsedCase, citationMap map[string][]citationTarget, dryRun bool) (ImportSummary, error) {
	summary := ImportSummary{CategoriesObserved: make(map[string]int)}

	err := gdb.Transaction(func(tx *gorm.DB) error {
		for _, cc := range cases {
			for _, cat := range cc.Categories {
				summary.CategoriesObserved[cat]++
			}

			isInternal := isInternalReference(cc.ReferenceNumber)

			// An internal reference is trusted as a case identity on its
			// own (see groupingKey). A non-internal one isn't -- a blanket
			// reference like PDRM's "JK KPN(PR) 168/6" is shared by
			// thousands of unrelated domains, so groupingKey already folds
			// the domain into the grouping key for these, meaning every
			// such CollapsedCase has exactly one domain -- and the rerun
			// check here has to match on that same (reference, domain) pair
			// too, not the reference text alone, or the second distinct
			// domain sharing a blanket reference would wrongly read as
			// "already imported" once the first one exists.
			var existing db.CaseLetter
			var err error
			if isInternal {
				err = tx.WithContext(ctx).
					Joins("JOIN cases ON cases.id = case_letters.case_id").
					Where("case_letters.reference_number_internal = ? AND case_letters.type = ? AND cases.department_id = ?",
						cc.ReferenceNumber, "Notice", crdDeptID).
					First(&existing).Error
			} else {
				normalizedDomain := ""
				if len(cc.Domains) > 0 {
					normalizedDomain = normalizeOrFallback(cc.Domains[0].RawDomain)
				}
				// reference_number_internal = '' guards against a domain
				// that appears twice in the sheet -- once under a real
				// internal case (whose own reference_number_external is
				// empty, since it never had an NMSMD) and once with no
				// reference at all elsewhere. Without this, the blank/
				// external row's lookup (matching on the same empty
				// external text + shared domain) would find that unrelated
				// internal case's letter and wrongly skip creating its own.
				err = tx.WithContext(ctx).
					Joins("JOIN cases ON cases.id = case_letters.case_id").
					Joins("JOIN case_urls ON case_urls.case_id = cases.id").
					Joins("JOIN urls ON urls.id = case_urls.url_id").
					Where("case_letters.reference_number_external = ? AND case_letters.reference_number_internal = '' AND case_letters.type = ? AND cases.department_id = ? AND urls.url = ?",
						cc.ReferenceNumber, "Notice", crdDeptID, normalizedDomain).
					First(&existing).Error
			}
			if err == nil {
				summary.CasesSkippedExist++
				continue
			}
			if err != gorm.ErrRecordNotFound {
				return err
			}

			c := db.Case{DepartmentID: crdDeptID}
			if err := tx.WithContext(ctx).Create(&c).Error; err != nil {
				return err
			}
			letter := db.CaseLetter{CaseID: c.ID, Type: "Notice"}
			if isInternal {
				letter.ReferenceNumberInternal = cc.ReferenceNumber
			} else {
				letter.ReferenceNumberExternal = cc.ReferenceNumber
			}
			// NMSMD (a secondary MCMC reference, e.g. a re-block's
			// follow-up case number -- see CRDRow.NMSMD) always lands on
			// the external slot regardless of the primary reference's own
			// routing; the two never collide in the real data (verified:
			// every non-internal NMD has an empty NMSMD).
			if cc.NMSMD != "" {
				letter.ReferenceNumberExternal = cc.NMSMD
			}
			if err := tx.WithContext(ctx).Create(&letter).Error; err != nil {
				return err
			}

			// Two raw domain spellings within the same reference can
			// normalize to the same URL row (e.g. "http://foo.com" and
			// "https://foo.com") even though CollapseCRDRows only dedupes
			// on exact raw string — track by URLID here too, last-write-wins
			// on Status, to avoid a duplicate (case_id, url_id) insert.
			caseURLByID := make(map[uint]*db.CaseURL)
			for _, d := range cc.Domains {
				u, err := createURL(ctx, tx, d.RawDomain)
				if err != nil {
					summary.URLsSkippedBadURL++
					continue
				}
				var agencyID *uint
				if d.Agency != "" {
					agency, err := getOrCreateAgency(ctx, tx, d.Agency)
					if err != nil {
						return err
					}
					agencyID = &agency.ID
				}
				if existing, dup := caseURLByID[u.ID]; dup {
					existing.Status = mapCRDStatus(d.Status)
					existing.OriginalURL = d.RawDomain
					existing.AgencyID = agencyID
					continue
				}
				caseURLByID[u.ID] = &db.CaseURL{
					CaseID:      c.ID,
					URLID:       u.ID,
					Status:      mapCRDStatus(d.Status),
					OriginalURL: d.RawDomain,
					AgencyID:    agencyID,
				}
			}
			for _, caseURL := range caseURLByID {
				if err := tx.WithContext(ctx).Create(caseURL).Error; err != nil {
					return err
				}
			}

			if targets := citationMap[cc.CitationText]; len(targets) > 0 {
				urlIDs := make([]uint, 0, len(caseURLByID))
				for urlID := range caseURLByID {
					urlIDs = append(urlIDs, urlID)
				}
				created, err := attachOffences(ctx, tx, urlIDs, targets, cc.Categories, cc.Element, cc.SubElement)
				if err != nil {
					return err
				}
				summary.URLOffencesCreated += created
			} else if cc.CitationText != "" {
				summary.OffencesSkippedNoCitation++
			}

			summary.CasesCreated++
		}
		if dryRun {
			return errDryRunRollback
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRunRollback) {
		return summary, err
	}
	return summary, nil
}
