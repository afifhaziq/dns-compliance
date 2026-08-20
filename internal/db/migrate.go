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

		// Reassign scan results / watchlist links and delete duplicate rows
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

// BackfillURLCaseMetadataBatchSize caps each read batch. Unlike
// BackfillURLValues (one bulk UPDATE per batch), this backfill does
// per-row Go logic — one Case+CaseURL per distinct department watching a
// URL — so batching here bounds how many URL rows are loaded and processed
// per iteration rather than bounding a single SQL statement.
const BackfillURLCaseMetadataBatchSize = 500

// BackfillURLCaseMetadataIntoCases moves the legacy case-metadata still on
// urls (due_date/agency_id/status/requested_at — superseded by Case owning
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
// AgencyID/Status/DueDate/RequestedAt columns already added) and before the
// old urls columns are dropped.
func BackfillURLCaseMetadataIntoCases(ctx context.Context, database *gorm.DB) error {
	type legacyURLRow struct {
		ID          uint
		DueDate     *time.Time
		AgencyID    *uint
		Status      string
		RequestedAt *time.Time
	}

	lastID := uint(0)
	for {
		var rows []legacyURLRow
		err := database.WithContext(ctx).
			Table("urls").
			Select("urls.id, urls.due_date, urls.agency_id, urls.status, urls.requested_at").
			Where("urls.id > ?", lastID).
			Where("urls.due_date IS NOT NULL OR urls.agency_id IS NOT NULL OR urls.status <> '' OR urls.requested_at IS NOT NULL").
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

			phase := row.Status
			if phase == "" {
				phase = "requested"
			}
			for _, deptID := range deptIDs {
				c := Case{
					DepartmentID: deptID,
					AgencyID:     row.AgencyID,
					Status:       row.Status,
					DueDate:      row.DueDate,
					RequestedAt:  row.RequestedAt,
				}
				if err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if err := tx.Create(&c).Error; err != nil {
						return err
					}
					return tx.Create(&CaseURL{CaseID: c.ID, URLID: row.ID, Phase: phase}).Error
				}); err != nil {
					return fmt.Errorf("backfilling case for url id=%d department=%d: %w", row.ID, deptID, err)
				}
			}
		}
	}
}
