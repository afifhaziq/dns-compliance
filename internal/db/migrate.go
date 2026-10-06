package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/afif/dns-tracking/internal/urlnorm"
	"gorm.io/gorm"
)

// NormalizeAndDedupeURLs normalizes every existing urls.url value; for
// duplicates that collide after normalization, merges them onto one
// canonical row (lowest ID), reassigning ScanResult.URLID and merging any
// DepartmentURL links, then deletes the duplicate URL rows. Runs inside a
// transaction. Idempotent — safe to call on every startup.
func NormalizeAndDedupeURLs(ctx context.Context, database *gorm.DB) error {
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var urls []URL
		if err := tx.Order("id asc").Find(&urls).Error; err != nil {
			return fmt.Errorf("loading urls: %w", err)
		}

		canonicalID := make(map[string]uint)
		canonicalNorm := make(map[uint]string)
		var duplicateIDs []uint
		duplicateOf := make(map[uint]uint)

		for _, u := range urls {
			norm, err := urlnorm.Normalize(u.URL)
			if err != nil {
				continue // leave unparseable legacy rows for manual review
			}
			if id, ok := canonicalID[norm]; ok {
				duplicateIDs = append(duplicateIDs, u.ID)
				duplicateOf[u.ID] = id
				continue
			}
			canonicalID[norm] = u.ID
			canonicalNorm[u.ID] = norm
		}

		// Reassign everything linked to a duplicate and delete duplicate rows
		// *before* renaming canonical rows — a duplicate may already hold the
		// exact normalized string the canonical row is about to be renamed to,
		// which would otherwise collide with URL's unique index.
		for _, dupID := range duplicateIDs {
			canonID := duplicateOf[dupID]

			if err := tx.Model(&ScanResult{}).Where("url_id = ?", dupID).Update("url_id", canonID).Error; err != nil {
				return fmt.Errorf("reassigning scan_results from url id=%d to %d: %w", dupID, canonID, err)
			}

			var dupLinks []DepartmentURL
			if err := tx.Where("url_id = ?", dupID).Find(&dupLinks).Error; err != nil {
				return fmt.Errorf("loading department_urls for url id=%d: %w", dupID, err)
			}
			for _, link := range dupLinks {
				var existing int64
				tx.Model(&DepartmentURL{}).
					Where("department_id = ? AND url_id = ?", link.DepartmentID, canonID).
					Count(&existing)
				if existing == 0 {
					if err := tx.Model(&DepartmentURL{}).
						Where("department_id = ? AND url_id = ?", link.DepartmentID, dupID).
						Update("url_id", canonID).Error; err != nil {
						return fmt.Errorf("merging department_urls from %d to %d: %w", dupID, canonID, err)
					}
				}
			}
			if err := tx.Where("url_id = ?", dupID).Delete(&DepartmentURL{}).Error; err != nil {
				return fmt.Errorf("cleaning leftover department_urls for url id=%d: %w", dupID, err)
			}

			// Everything else FK'd to urls cascades on delete, so it must move
			// too. case_urls is keyed (case_id, url_id): a case already linked
			// to the canonical row keeps that link and drops the duplicate's.
			if err := tx.Model(&CaseURL{}).
				Where("url_id = ? AND case_id NOT IN (?)", dupID,
					tx.Model(&CaseURL{}).Select("case_id").Where("url_id = ?", canonID)).
				Update("url_id", canonID).Error; err != nil {
				return fmt.Errorf("merging case_urls from %d to %d: %w", dupID, canonID, err)
			}
			// Keep the dropped links' cited text on the surviving link.
			var leftover []CaseURL
			if err := tx.Where("url_id = ?", dupID).Find(&leftover).Error; err != nil {
				return fmt.Errorf("loading leftover case_urls for url id=%d: %w", dupID, err)
			}
			for _, cu := range leftover {
				var keep CaseURL
				if err := tx.Where("case_id = ? AND url_id = ?", cu.CaseID, canonID).First(&keep).Error; err != nil {
					return fmt.Errorf("loading case_url (%d,%d): %w", cu.CaseID, canonID, err)
				}
				if merged := AppendOriginalURL(keep.OriginalURL, cu.OriginalURL); merged != keep.OriginalURL {
					if err := tx.Model(&CaseURL{}).Where("case_id = ? AND url_id = ?", cu.CaseID, canonID).
						Update("original_url", merged).Error; err != nil {
						return fmt.Errorf("merging original_url into case_url (%d,%d): %w", cu.CaseID, canonID, err)
					}
				}
			}
			if err := tx.Where("url_id = ?", dupID).Delete(&CaseURL{}).Error; err != nil {
				return fmt.Errorf("cleaning leftover case_urls for url id=%d: %w", dupID, err)
			}
			if err := tx.Model(&URLOffence{}).Where("url_id = ?", dupID).Update("url_id", canonID).Error; err != nil {
				return fmt.Errorf("reassigning url_offences from url id=%d to %d: %w", dupID, canonID, err)
			}
			if err := tx.Model(&Notification{}).Where("url_id = ?", dupID).
				Updates(map[string]any{"url_id": canonID, "url_value": canonicalNorm[canonID]}).Error; err != nil {
				return fmt.Errorf("reassigning notifications from url id=%d to %d: %w", dupID, canonID, err)
			}
			if err := tx.Delete(&URL{}, dupID).Error; err != nil {
				return fmt.Errorf("deleting duplicate url id=%d: %w", dupID, err)
			}
		}

		for id, norm := range canonicalNorm {
			if err := tx.Model(&URL{}).Where("id = ?", id).Update("url", norm).Error; err != nil {
				return fmt.Errorf("normalizing url id=%d: %w", id, err)
			}
		}
		return nil
	})
}

// BackfillCaseURLsToDepartmentListsBatchSize bounds how many case_urls rows
// are read and linked per iteration, same statement_timeout-avoidance
// rationale as BackfillURLValuesBatchSize.
const BackfillCaseURLsToDepartmentListsBatchSize = 1000

// BackfillCaseURLsToDepartmentLists ensures every url already linked to a
// department via a Case (case_urls -> cases.department_id) also has a
// DepartmentURL row for that department — the same thing CreateCase and
// AddCaseURL now do for a case's urls going forward (see ensureDepartmentURL
// in postgres.go), needed here because a bulk import (internal/blockimport)
// writes case_urls directly and never went through either of those. Without
// this, a department's Domain tab (GET /api/urls, department-scoped) stays
// empty for every domain that only ever arrived via an imported case.
// New rows default Enabled: false — this backfill's whole point is to make
// an already-known domain visible and toggleable, not to silently start
// scanning tens of thousands of domains no one has reviewed yet. Idempotent:
// only touches (department, url) pairs with no DepartmentURL row at all, so
// a pair already linked (via this backfill, AddURLToWatchlist, or the
// normal case-creation flow) is left alone regardless of its Enabled value.
// Runs as one batched INSERT...SELECT per iteration (not a per-row Go loop
// calling ensureDepartmentURL — CRD-import scale here is ~36k case_urls
// rows, and a round trip per row measurably slowed startup), repeating
// until a batch inserts zero rows — same convergence shape as
// BackfillURLValues, and just as non-fatal on error (see its call site in
// cmd/server/main.go). The `false` literal lands as a real `false`, not
// department_urls.enabled's `default:true`, because this is a raw INSERT
// rather than a GORM struct Create (see ensureDepartmentURL's doc comment
// for why that distinction matters).
func BackfillCaseURLsToDepartmentLists(ctx context.Context, database *gorm.DB) error {
	// CURRENT_TIMESTAMP, not a bound time.Now() parameter — pgx can't infer
	// a bound parameter's type here (no column context to match against in
	// a bare SELECT list next to a DISTINCT), so it arrives as text and
	// Postgres rejects it against created_at's timestamptz column. The
	// keyword form is standard SQL and sidesteps that entirely, and both
	// engines this runs against (Postgres, SQLite in tests) support it.
	stmt := `INSERT INTO department_urls (department_id, url_id, enabled, created_at)
		SELECT DISTINCT cases.department_id, case_urls.url_id, false, CURRENT_TIMESTAMP
		FROM case_urls
		JOIN cases ON cases.id = case_urls.case_id
		WHERE NOT EXISTS (
			SELECT 1 FROM department_urls du
			WHERE du.department_id = cases.department_id AND du.url_id = case_urls.url_id
		)
		LIMIT ?
		ON CONFLICT DO NOTHING`

	for {
		res := database.WithContext(ctx).Exec(stmt, BackfillCaseURLsToDepartmentListsBatchSize)
		if res.Error != nil {
			return fmt.Errorf("linking case_urls to department_urls: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return nil
		}
	}
}

// BackfillURLValuesBatchSize caps each UPDATE to a bounded chunk of rows —
// on a first deploy (pre-branch, every url_value diverges) this would
// otherwise be a single full-table rewrite, risking a managed-Postgres
// statement_timeout. Exported so tests can seed a multi-batch scenario
// without needing tens of thousands of rows.
const BackfillURLValuesBatchSize = 1000

// backfillURLValuesMaxIterations guards against spinning forever if some
// row's url_value can never converge (shouldn't happen — canonical is keyed
// off the row's own url_id — but a stuck loop should error out, not hang the
// startup goroutine indefinitely).
const backfillURLValuesMaxIterations = 100000

// BackfillURLValues repairs scan_results rows whose denormalized url_value
// drifted from the canonical urls.url it points at via url_id — the state
// NormalizeAndDedupeURLs used to leave behind (it reassigned url_id and
// renamed urls.url, but never rewrote url_value), and that a pre-fix
// grpcServer.Submit wrote directly by storing the raw crawler string.
//
// url_value is the GROUP BY / join key for nearly every aggregate in
// internal/db/postgres.go, so a diverged row silently reads as a separate
// domain — or as no domain at all, when a handler looks it up by its
// normalized name. Idempotent: rows already in sync match nothing.
//
// Runs in bounded batches (BackfillURLValuesBatchSize rows per statement)
// rather than one unbatched UPDATE, repeating until no diverged rows remain.
// Written as a correlated subquery rather than Postgres's UPDATE ... FROM so
// the same statement also runs on the SQLite backend used by tests; the
// batch is selected via `id IN (SELECT id ... LIMIT n)`, which both engines
// support even where a direct `UPDATE ... LIMIT` doesn't exist (SQLite).
func BackfillURLValues(ctx context.Context, database *gorm.DB) error {
	const canonical = "(SELECT url FROM urls WHERE urls.id = scan_results.url_id)"
	stmt := "UPDATE scan_results SET url_value = " + canonical +
		" WHERE id IN (SELECT id FROM scan_results" +
		" WHERE url_id IS NOT NULL AND url_value <> " + canonical +
		" LIMIT ?)"

	for i := 0; i < backfillURLValuesMaxIterations; i++ {
		res := database.WithContext(ctx).Exec(stmt, BackfillURLValuesBatchSize)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
	}
	return fmt.Errorf("backfilling scan_results.url_value: did not converge after %d iterations", backfillURLValuesMaxIterations)
}

// ErrorClassBatchSize caps each UPDATE to a bounded chunk of rows, same
// rationale as BackfillURLValuesBatchSize: a first-deploy run rewrites
// every pre-existing scan_results row that has an error, and an unbatched
// statement risks tripping a managed-Postgres statement_timeout.
const ErrorClassBatchSize = 1000

// errorClassMaxIterations guards against spinning forever; the CASE below
// always assigns "other" as a last resort, so a row can never fail to
// converge, but the guard matches BackfillURLValues's defensive shape.
const errorClassMaxIterations = 100000

// BackfillErrorClass assigns error_class to any pre-existing scan_results
// row that represents a DNS failure (no resolved IP) with a raw error
// string but no classification yet — rows inserted before internal/dns
// started preserving RCode via RCodeError. This can only approximate the
// categories internal/dns.Classify computes going forward, matching on the
// raw error text the same way web/src/lib/dns-error.ts's classifyDNSError
// used to (client-side, made redundant by this backfill) — the original
// RCode isn't recoverable after the fact. Rows with a non-empty resolved_ip
// are screenshot failures, not DNS failures (internal/pipeline.checkDNS's
// failure branches never set ResolvedIP; only a successful DNS step
// followed by a failed takeScreenshot does) — those are deliberately left
// unclassified here, consistent with takeScreenshot leaving ErrorClass
// empty itself. Idempotent: only rows with error_class = '', a non-empty
// error, and no resolved_ip are touched, so a row already classified never
// changes again.
func BackfillErrorClass(ctx context.Context, database *gorm.DB) error {
	const stmt = `
		UPDATE scan_results SET error_class = CASE
			WHEN LOWER(error) LIKE 'invalid url:%' THEN 'invalid_url'
			WHEN LOWER(error) LIKE '%no such host%' OR LOWER(error) LIKE '%nxdomain%' THEN 'nxdomain'
			WHEN LOWER(error) LIKE '%timeout%' OR LOWER(error) LIKE '%deadline exceeded%' THEN 'timeout'
			WHEN LOWER(error) LIKE '%server misbehaving%' OR LOWER(error) LIKE '%connection refused%' OR LOWER(error) LIKE '%servfail%' THEN 'servfail'
			ELSE 'other'
		END
		WHERE id IN (SELECT id FROM scan_results WHERE error <> '' AND error_class = '' AND resolved_ip = '' LIMIT ?)`

	for i := 0; i < errorClassMaxIterations; i++ {
		res := database.WithContext(ctx).Exec(stmt, ErrorClassBatchSize)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
	}
	return fmt.Errorf("backfilling scan_results.error_class: did not converge after %d iterations", errorClassMaxIterations)
}

// BackfillURLCaseMetadataBatchSize caps each read batch. Unlike
// BackfillURLValues (one bulk UPDATE per batch), this backfill does
// per-row Go logic — one Case+CaseURL per distinct department watching a
// URL — so batching here bounds how many URL rows are loaded and processed
// per iteration rather than bounding a single SQL statement.
const BackfillURLCaseMetadataBatchSize = 500

// BackfillURLCaseMetadataIntoCases moves the legacy case-metadata still on
// urls (due_date/agency_id/status — superseded by Case owning
// these fields, see URL's doc comment in models.go) into one Case per URL
// per distinct watching department, before those columns are dropped (see
// db.Connect). Only touches URL rows with at least one of the four fields
// set AND zero existing CaseURL rows — a URL that already has a case (via
// the normal CreateCaseForURL flow, or a prior run of this backfill) is
// left alone, making this idempotent and safe to call on every startup. A
// URL with zero DepartmentURL rows has no department to attribute a Case
// to and is skipped with a logged warning, non-fatal — same "log and skip"
// philosophy BackfillURLValues/backfillURLReferenceNumbersIntoCases (db.go)
// already use. Must run after AutoMigrate (Case needs its new
// AgencyID/Status/DueDate columns already added) and before the
// old urls columns are dropped.
func BackfillURLCaseMetadataIntoCases(ctx context.Context, database *gorm.DB) error {
	type legacyURLRow struct {
		ID       uint
		DueDate  *time.Time
		AgencyID *uint
		Status   string
	}

	lastID := uint(0)
	for {
		var rows []legacyURLRow
		err := database.WithContext(ctx).
			Table("urls").
			Select("urls.id, urls.due_date, urls.agency_id, urls.status").
			Where("urls.id > ?", lastID).
			Where("urls.due_date IS NOT NULL OR urls.agency_id IS NOT NULL OR urls.status <> ''").
			Where("NOT EXISTS (SELECT 1 FROM case_urls WHERE case_urls.url_id = urls.id)").
			Order("urls.id asc").
			Limit(BackfillURLCaseMetadataBatchSize).
			Find(&rows).Error
		if err != nil {
			return fmt.Errorf("loading legacy url case metadata: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}

		for _, row := range rows {
			lastID = row.ID

			var deptIDs []uint
			if err := database.WithContext(ctx).Model(&DepartmentURL{}).
				Where("url_id = ?", row.ID).
				Pluck("department_id", &deptIDs).Error; err != nil {
				return fmt.Errorf("loading watching departments for url id=%d: %w", row.ID, err)
			}
			if len(deptIDs) == 0 {
				log.Printf("db: urls.id=%d has case metadata but no watching department, skipping case backfill", row.ID)
				continue
			}

			status := row.Status
			if status == "" {
				status = "requested"
			}
			for _, deptID := range deptIDs {
				c := Case{
					DepartmentID: deptID,
					DueDate:      row.DueDate,
				}
				if err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if err := tx.Create(&c).Error; err != nil {
						return err
					}
					return tx.Create(&CaseURL{CaseID: c.ID, URLID: row.ID, Status: status, AgencyID: row.AgencyID}).Error
				}); err != nil {
					return fmt.Errorf("backfilling case for url id=%d department=%d: %w", row.ID, deptID, err)
				}
			}
		}
	}
}

// BackfillCaseAgencyBatchSize bounds how many Case rows are read and
// applied per iteration, same statement_timeout-avoidance rationale as
// BackfillURLCaseMetadataBatchSize.
const BackfillCaseAgencyBatchSize = 500

// BackfillCaseAgencyIntoCaseURLs moves the legacy cases.agency_id value
// (superseded 2026-09-15 by CaseURL owning it per-domain — see Case's doc
// comment in models.go) onto every CaseURL row under that case, before the
// column is dropped (see db.Connect). Idempotent: only touches CaseURL rows
// whose own AgencyID is still nil, so a URL already migrated — or one added
// to a case after the move, or edited per-domain since — is left alone.
// Must run after AutoMigrate (CaseURL needs its new AgencyID column already
// added) and before cases.agency_id is dropped; the caller (db.Connect)
// guards the call itself with HasColumn(&Case{}, "agency_id"), same
// call-site-guard convention as BackfillURLCaseMetadataIntoCases.
func BackfillCaseAgencyIntoCaseURLs(ctx context.Context, database *gorm.DB) error {
	type legacyCaseAgencyRow struct {
		ID       uint
		AgencyID uint
	}
	lastID := uint(0)
	for {
		var rows []legacyCaseAgencyRow
		err := database.WithContext(ctx).
			Table("cases").
			Select("cases.id, cases.agency_id").
			Where("cases.id > ? AND cases.agency_id IS NOT NULL", lastID).
			Order("cases.id asc").
			Limit(BackfillCaseAgencyBatchSize).
			Find(&rows).Error
		if err != nil {
			return fmt.Errorf("loading legacy case agency values: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			lastID = row.ID
			if err := database.WithContext(ctx).Model(&CaseURL{}).
				Where("case_id = ? AND agency_id IS NULL", row.ID).
				Update("agency_id", row.AgencyID).Error; err != nil {
				return fmt.Errorf("backfilling agency for case id=%d: %w", row.ID, err)
			}
		}
	}
}
