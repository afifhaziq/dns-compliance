package db

import (
	"context"
	"fmt"

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
// Written as a correlated subquery rather than Postgres's UPDATE ... FROM so
// the same statement also runs on the SQLite backend used by tests.
func BackfillURLValues(ctx context.Context, database *gorm.DB) error {
	const canonical = "(SELECT url FROM urls WHERE urls.id = scan_results.url_id)"
	return database.WithContext(ctx).Exec(
		"UPDATE scan_results SET url_value = " + canonical +
			" WHERE url_id IS NOT NULL AND url_value <> " + canonical,
	).Error
}
