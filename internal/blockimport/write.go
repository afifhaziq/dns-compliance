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
	CasesCreated       int
	CasesSkippedExist  int            // already imported (idempotency)
	URLsSkippedBadURL  int            // failed urlnorm.Normalize
	CategoriesObserved map[string]int // raw Category/Offence value -> row count, for visibility only
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

// WriteCRDCases creates one Case (DepartmentID = the CRD department's ID)
// per CollapsedCase, one CaseLetter (Type: "Notice") carrying the reference
// number, and one CaseURL per domain (Status mapped from the domain's Status
// via mapCRDStatus). Does NOT create Category/Citation/URLOffence rows — see
// the plan's Global Constraints. dryRun=true does every lookup/validation
// but wraps all writes in a transaction that's always rolled back, so
// ImportSummary reflects exactly what a real run would do.
func WriteCRDCases(ctx context.Context, gdb *gorm.DB, crdDeptID uint, cases []CollapsedCase, dryRun bool) (ImportSummary, error) {
	summary := ImportSummary{CategoriesObserved: make(map[string]int)}

	err := gdb.Transaction(func(tx *gorm.DB) error {
		for _, cc := range cases {
			for _, cat := range cc.Categories {
				summary.CategoriesObserved[cat]++
			}

			var existing db.CaseLetter
			err := tx.WithContext(ctx).
				Joins("JOIN cases ON cases.id = case_letters.case_id").
				Where("case_letters.reference_number_external = ? AND case_letters.type = ? AND cases.department_id = ?", cc.ReferenceNumber, "Notice", crdDeptID).
				First(&existing).Error
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
			letter := db.CaseLetter{
				CaseID:                  c.ID,
				Type:                    "Notice",
				ReferenceNumberExternal: cc.ReferenceNumber,
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
				if existing, dup := caseURLByID[u.ID]; dup {
					existing.Status = mapCRDStatus(d.Status)
					existing.OriginalURL = d.RawDomain
					continue
				}
				caseURLByID[u.ID] = &db.CaseURL{
					CaseID:      c.ID,
					URLID:       u.ID,
					Status:      mapCRDStatus(d.Status),
					OriginalURL: d.RawDomain,
				}
			}
			for _, caseURL := range caseURLByID {
				if err := tx.WithContext(ctx).Create(caseURL).Error; err != nil {
					return err
				}
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
