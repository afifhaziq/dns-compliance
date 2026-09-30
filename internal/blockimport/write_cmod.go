package blockimport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/afif/dns-tracking/internal/db"
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

// cmodOffences maps the sheet's Offence values onto the legal catalog. CMOD
// only records a category, so the citation is the one CRD already used for
// the same notices (docs/cmod-blocking-list-migration-clarifications.md §2).
var cmodOffences = map[string]struct {
	target   citationTarget
	category string
}{
	"Judi dalam talian": {citationTarget{"Akta Rumah Judi Terbuka 1953 (Akta 289)", "Seksyen 4(1)"}, "Judi"},
	"Palsu":             {citationTarget{"Akta Komunikasi dan Multimedia 1998 (Akta 588)", "Seksyen 233"}, "Palsu"},
	"Lucah":             {citationTarget{"Akta Komunikasi dan Multimedia 1998 (Akta 588)", "Seksyen 233"}, "Lucah"},
	"Jelik Melampau":    {citationTarget{"Akta Komunikasi dan Multimedia 1998 (Akta 588)", "Seksyen 233"}, "Jelik Melampau"},
}

// OICUsername turns a sheet OIC name into a login: "Mas Atika" -> "mas_atika".
func OICUsername(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), "_"))
}

// splitOIC splits collapseLetterGroup's "; "-joined OIC value.
func splitOIC(oic string) []string {
	var out []string
	for _, n := range strings.Split(oic, ";") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// letterOIC links a letter to its first OIC's account. A letter covering
// domains handled by several OICs keeps the full list in Remarks, since
// OICUserID holds only one.
func letterOIC(oic, remarks string, users map[string]uint) (*uint, string) {
	names := splitOIC(oic)
	if len(names) == 0 {
		return nil, remarks
	}
	var id *uint
	if uid, ok := users[names[0]]; ok {
		id = &uid
	}
	if len(names) == 1 && id != nil {
		return id, remarks
	}
	note := "OIC: " + strings.Join(names, "; ")
	if remarks == "" {
		return id, note
	}
	return id, note + " — " + remarks
}

// ensureOICUsers get-or-creates one CMOD account per distinct OIC name,
// all with the same initial password and a forced change on first login.
// An empty password skips account creation (OICs then stay text-only).
func ensureOICUsers(ctx context.Context, tx *gorm.DB, cmodDeptID uint, cases []CollapsedCMODCase, password string, summary *ImportSummary) (map[string]uint, error) {
	users := map[string]uint{}
	if password == "" {
		return users, nil
	}
	hash, err := db.HashPassword(password)
	if err != nil {
		return nil, err
	}
	for _, c := range cases {
		for _, l := range c.Letters {
			for _, name := range splitOIC(l.OIC) {
				if _, ok := users[name]; ok {
					continue
				}
				var u db.User
				res := tx.WithContext(ctx).Where("username = ?", OICUsername(name)).
					Attrs(db.User{Username: OICUsername(name), PasswordHash: hash, DepartmentID: &cmodDeptID, MustChangePassword: true}).
					FirstOrCreate(&u)
				if res.Error != nil {
					return nil, res.Error
				}
				if res.RowsAffected > 0 {
					summary.UsersCreated++
				}
				users[name] = u.ID
			}
		}
	}
	return users, nil
}

// getOrCreateURL is the same normalize + FirstOrCreate get-or-create
// pattern internal/db.postgresStore.CreateURL uses, replicated here (that
// method is unexported and this package writes through a raw *gorm.DB, not
// a db.Store) so re-running the import against already-imported domains is
// idempotent instead of erroring on a unique-constraint violation.
// normalizeOrFallback (write.go) covers rows too garbled for
// urlnorm.Normalize to extract a hostname from.
func getOrCreateURL(tx *gorm.DB, raw string) (db.URL, error) {
	normalized := normalizeOrFallback(raw)
	if normalized == "" {
		return db.URL{}, fmt.Errorf("blockimport: empty domain, nothing to store")
	}
	var u db.URL
	err := tx.Where("url = ?", normalized).Attrs(db.URL{URL: normalized}).FirstOrCreate(&u).Error
	return u, err
}

// WriteCMODCases writes each CollapsedCMODCase as a CMOD case. A case whose
// Memo/Notice reference already sits on a CRD case (CRD's sheet transcribes
// CMOD's Notice ref) is linked instead of duplicated: that case moves to
// CMOD, CMOD's letters merge into it by type, and its domains lose their
// CRD status. Everything else becomes a new case. CMOD domains carry no
// status (""), only their letters' WorkflowStatus. crdDeptID 0 disables
// linking. dryRun rolls everything back after computing the summary.
func WriteCMODCases(ctx context.Context, gormDB *gorm.DB, cmodDeptID, crdDeptID uint, cases []CollapsedCMODCase, oicPassword string, dryRun bool) (ImportSummary, error) {
	summary := ImportSummary{CategoriesObserved: map[string]int{}}

	err := gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		users, err := ensureOICUsers(ctx, tx, cmodDeptID, cases, oicPassword, &summary)
		if err != nil {
			return err
		}
		for _, c := range cases {
			if len(c.Letters) == 0 {
				continue
			}
			for _, off := range splitOffence(c.Offence) {
				summary.CategoriesObserved[off]++
			}

			refs := make([]string, 0, len(c.Letters))
			for _, l := range c.Letters {
				refs = append(refs, l.ReferenceNumber)
			}
			var matches []db.Case
			if err := tx.Where(`id IN (SELECT case_id FROM case_letters
				WHERE reference_number_external IN ? OR reference_number_internal IN ?)`, refs, refs).
				Order("id").Find(&matches).Error; err != nil {
				return err
			}
			var target *db.Case
			skip := false
			for i := range matches {
				switch matches[i].DepartmentID {
				case cmodDeptID:
					skip = true
				case crdDeptID:
					if target == nil && crdDeptID != 0 {
						target = &matches[i]
					}
				}
			}
			if skip {
				summary.CasesSkippedExist++
				continue
			}

			if target != nil {
				if err := tx.Model(&db.Case{}).Where("id = ?", target.ID).Update("department_id", cmodDeptID).Error; err != nil {
					return err
				}
				// CRD's block status doesn't exist for CMOD.
				if err := tx.Model(&db.CaseURL{}).Where("case_id = ?", target.ID).Update("status", "").Error; err != nil {
					return err
				}
				// Its domains leave CRD's list unless another CRD case still
				// covers them. Only auto-linked (disabled) rows: a domain CRD
				// switched on for scanning stays.
				if err := tx.Exec(`DELETE FROM department_urls
					WHERE department_id = ? AND enabled = ?
					AND url_id IN (SELECT url_id FROM case_urls WHERE case_id = ?)
					AND NOT EXISTS (SELECT 1 FROM case_urls cu JOIN cases c ON c.id = cu.case_id
						WHERE cu.url_id = department_urls.url_id AND c.department_id = ?)`,
					crdDeptID, false, target.ID, crdDeptID).Error; err != nil {
					return err
				}
				summary.CasesLinked++
			} else {
				target = &db.Case{DepartmentID: cmodDeptID}
				if err := tx.Create(target).Error; err != nil {
					return err
				}
				summary.CasesCreated++
			}

			if err := mergeCMODLetters(tx, target.ID, c.Letters, users); err != nil {
				return err
			}
			if err := writeCMODDomains(ctx, tx, target.ID, c, &summary); err != nil {
				return err
			}
		}
		if dryRun {
			return errDryRunRollback
		}
		return nil
	})
	if errors.Is(err, errDryRunRollback) {
		return summary, nil
	}
	return summary, err
}

// mergeCMODLetters writes CMOD's letters onto caseID, updating an existing
// letter of the same Type (a linked CRD case's Notice / Notice (Uplift))
// rather than adding a second one. The existing letter keeps its internal
// reference and, when CMOD's date is blank, its letter date.
func mergeCMODLetters(tx *gorm.DB, caseID uint, letters []CMODRow, users map[string]uint) error {
	for _, letter := range letters {
		oicID, remarks := letterOIC(letter.OIC, letter.Remarks, users)
		cl := db.CaseLetter{
			CaseID:                  caseID,
			Type:                    letter.Type,
			ReferenceNumberExternal: letter.ReferenceNumber,
			WorkflowStatus:          letter.Status,
			Recipient:               letter.Recipient,
			LetterDate:              parseCMODDate(letter.LetterDate),
			ReceivedAt:              parseCMODDate(letter.Received),
			SubmittedAt:             parseCMODDate(letter.Submission),
			Subject:                 letter.Subject,
			OICUserID:               oicID,
			Requestor:               letter.Requestor,
			Remarks:                 remarks,
		}
		var existing db.CaseLetter
		err := tx.Where("case_id = ? AND type = ?", caseID, letter.Type).Order("id").First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&cl).Error; err != nil {
				return err
			}
			continue
		} else if err != nil {
			return err
		}
		cl.ID, cl.ReferenceNumberInternal, cl.CreatedAt = existing.ID, existing.ReferenceNumberInternal, existing.CreatedAt
		if cl.LetterDate == nil {
			cl.LetterDate = existing.LetterDate
		}
		if err := tx.Save(&cl).Error; err != nil {
			return err
		}
	}
	return nil
}

// writeCMODDomains links c's domains to caseID with CMOD's per-domain agency,
// and attaches CMOD's offence to any domain the case has none for yet (a
// linked CRD case keeps its own, finer-grained classification).
func writeCMODDomains(ctx context.Context, tx *gorm.DB, caseID uint, c CollapsedCMODCase, summary *ImportSummary) error {
	seen := map[uint]bool{}
	for _, raw := range c.URLs {
		u, err := getOrCreateURL(tx, raw)
		if err != nil {
			summary.URLsSkippedBadURL++
			continue
		}
		var agencyID *uint
		if name := c.AgencyByURL[raw]; name != "" {
			a, err := getOrCreateAgency(ctx, tx, name)
			if err != nil {
				return err
			}
			agencyID = &a.ID
		}

		var cu db.CaseURL
		err = tx.Where("case_id = ? AND url_id = ?", caseID, u.ID).First(&cu).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			cu = db.CaseURL{CaseID: caseID, URLID: u.ID, OriginalURL: raw, AgencyID: agencyID}
			if err := db.CreateCaseURL(tx, &cu); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if !seen[u.ID] {
				if agencyID != nil {
					if err := tx.Model(&db.CaseURL{}).Where("case_id = ? AND url_id = ?", caseID, u.ID).Update("agency_id", agencyID).Error; err != nil {
						return err
					}
				}
			} else if err := tx.Model(&db.CaseURL{}).Where("case_id = ? AND url_id = ?", caseID, u.ID).
				Update("original_url", db.AppendOriginalURL(cu.OriginalURL, raw)).Error; err != nil {
				return err
			}
		}
		if seen[u.ID] {
			continue
		}
		seen[u.ID] = true

		var n int64
		if err := tx.Model(&db.URLOffence{}).Where("case_id = ? AND url_id = ?", caseID, u.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		for _, off := range splitOffence(c.OffenceByURL[raw]) {
			m, ok := cmodOffences[off]
			if !ok {
				summary.OffencesSkippedNoCitation++
				continue
			}
			created, err := attachOffences(ctx, tx, caseID, []uint{u.ID}, []citationTarget{m.target}, []string{m.category}, "", "")
			if err != nil {
				return err
			}
			summary.URLOffencesCreated += created
		}
	}
	return nil
}
