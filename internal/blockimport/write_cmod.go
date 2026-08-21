package blockimport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/urlnorm"
	"gorm.io/gorm"
)

// ImportSummary is defined once in write.go and shared by both importers.

// cmodDateLayouts are the raw text formats seen in the sheet's date-ish
// columns -- plain strings mixing abbreviated/full month names, with and
// without a time-of-day, plus real Excel datetime cells that excelize
// already renders as one of these.
var cmodDateLayouts = []string{
	"2-Jan-2006 15:04",
	"2-Jan-2006",
	"2-January-2006 15:04",
	"2-January-2006",
}

// parseCMODDate tries every known layout, returning nil for blank cells and
// for values that don't match any of them (e.g. "Withdrawn (26 June 2026 @
// 11:19 AM)") rather than erroring -- a single unparsable date must not
// abort the import.
func parseCMODDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, layout := range cmodDateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t
		}
	}
	return nil
}

// mapCMODPhase derives case_urls.phase from a case's letters -- "uplift" if
// any letter Type contains "Uplift" (Memo (Uplift) or Notice (Uplift)),
// else "requested". CMOD's own Status column (Draft/Pending Legal/Pending
// TSC/Submitted) is NOT this function's input -- it's letter-approval-
// workflow state, stored as-is on CaseLetter.WorkflowStatus, a different
// axis entirely (see the EDA doc's §1).
func mapCMODPhase(letters []CMODRow) string {
	for _, l := range letters {
		if strings.Contains(l.Type, "Uplift") {
			return "uplift"
		}
	}
	return "requested"
}

// foldOICIntoRemarks preserves the raw OIC name(s) as text on Remarks
// without linking them to a real user -- see the OIC caveat on
// WriteCMODCases below.
func foldOICIntoRemarks(oic, remarks string) string {
	if oic == "" {
		return remarks
	}
	if remarks == "" {
		return fmt.Sprintf("OIC (unmatched): %s", oic)
	}
	return fmt.Sprintf("OIC (unmatched): %s — %s", oic, remarks)
}

// getOrCreateURL is the same normalize + FirstOrCreate get-or-create
// pattern internal/db.postgresStore.CreateURL uses, replicated here (that
// method is unexported and this package writes through a raw *gorm.DB, not
// a db.Store) so re-running the import against already-imported domains is
// idempotent instead of erroring on a unique-constraint violation.
func getOrCreateURL(tx *gorm.DB, raw string) (db.URL, error) {
	normalized, err := urlnorm.Normalize(raw)
	if err != nil {
		return db.URL{}, err
	}
	var u db.URL
	err = tx.Where("url = ?", normalized).Attrs(db.URL{URL: normalized}).FirstOrCreate(&u).Error
	return u, err
}

// WriteCMODCases creates one Case (DepartmentID = the CMOD department's ID)
// per CollapsedCMODCase, one CaseLetter per Letters entry, and one CaseURL
// per URL in URLs (Phase via mapCMODPhase). dryRun wraps every write in a
// transaction that's always rolled back, so the returned ImportSummary
// reflects exactly what a real run would do without persisting anything.
func WriteCMODCases(ctx context.Context, gormDB *gorm.DB, cmodDeptID uint, cases []CollapsedCMODCase, dryRun bool) (ImportSummary, error) {
	summary := ImportSummary{CategoriesObserved: map[string]int{}}

	err := gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, c := range cases {
			if len(c.Letters) == 0 {
				continue
			}
			for _, off := range splitOffence(c.Offence) {
				summary.CategoriesObserved[off]++
			}

			key := c.Letters[0]
			var existing db.CaseLetter
			err := tx.Joins("JOIN cases ON cases.id = case_letters.case_id").
				Where("cases.department_id = ? AND case_letters.reference_number_external = ? AND case_letters.type = ?", cmodDeptID, key.ReferenceNumber, key.Type).
				First(&existing).Error
			if err == nil {
				summary.CasesSkippedExist++
				continue
			} else if err != gorm.ErrRecordNotFound {
				return err
			}

			newCase := db.Case{DepartmentID: cmodDeptID}
			if err := tx.Create(&newCase).Error; err != nil {
				return err
			}

			for _, letter := range c.Letters {
				cl := db.CaseLetter{
					CaseID:                  newCase.ID,
					Type:                    letter.Type,
					ReferenceNumberExternal: letter.ReferenceNumber,
					WorkflowStatus:          letter.Status,
					Recipient:               letter.Recipient,
					LetterDate:              parseCMODDate(letter.LetterDate),
					ReceivedAt:              parseCMODDate(letter.Received),
					SubmittedAt:             parseCMODDate(letter.Submission),
					Subject:                 letter.Subject,
					// ponytail: OICUserID left nil -- the CMOD EDA doc
					// (docs/cmod-blocking-list-migration-clarifications.md)
					// flags that OIC free-text names (Atiqah, Arishah, ...)
					// are NOT YET VERIFIED to match real users.username
					// values, so this import never guesses a link. The raw
					// name is preserved via foldOICIntoRemarks instead.
					// Revisit once someone confirms the name-matching.
					OICUserID: nil,
					Requestor: letter.Requestor,
					Remarks:   foldOICIntoRemarks(letter.OIC, letter.Remarks),
				}
				if err := tx.Create(&cl).Error; err != nil {
					return err
				}
			}

			phase := mapCMODPhase(c.Letters)
			for _, raw := range c.URLs {
				u, err := getOrCreateURL(tx, raw)
				if err != nil {
					summary.URLsSkippedBadURL++
					continue
				}
				cu := db.CaseURL{CaseID: newCase.ID, URLID: u.ID, Phase: phase}
				if err := tx.Create(&cu).Error; err != nil {
					return err
				}
			}

			summary.CasesCreated++
		}
		if dryRun {
			return fmt.Errorf("blockimport: dry run, rolling back")
		}
		return nil
	})
	if err != nil {
		if dryRun && err.Error() == "blockimport: dry run, rolling back" {
			return summary, nil
		}
		return summary, err
	}
	return summary, nil
}
