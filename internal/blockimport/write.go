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
// vocabulary (requested | uplift | suspended).
//
// ponytail: "Not Blocked"/"Not blocked"/empty map to "requested" as a
// placeholder — case_urls.status is NOT NULL and there's no clean mapping for
// these per docs/blocking-list-migration-clarifications.md Question 1
// (open, pending product sign-off). Revisit once that lands.
func mapCRDStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "uplift":
		return "uplift"
	case "suspended":
		return "suspended"
	default:
		return "requested"
	}
}

// createURL replicates internal/db.postgresStore.CreateURL's normalize +
// get-or-create semantics against a raw *gorm.DB (that method is unexported
// and scoped to db.Store, which this importer doesn't otherwise need).
func createURL(ctx context.Context, gdb *gorm.DB, rawURL string) (db.URL, error) {
	normalized, err := urlnorm.Normalize(rawURL)
	if err != nil {
		return db.URL{}, err
	}
	var u db.URL
	err = gdb.WithContext(ctx).
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
					continue
				}
				caseURLByID[u.ID] = &db.CaseURL{
					CaseID: c.ID,
					URLID:  u.ID,
					Status: mapCRDStatus(d.Status),
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
