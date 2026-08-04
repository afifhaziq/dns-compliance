# URL Compliance Case Fields Design

**Date:** 2026-08-04
**Branch:** main (design phase — not yet branched)

## Overview

Add Excel-sourced case metadata (Agency, reference number, requesting department, requested-at, status) to a department's watchlist entry for a domain, and rename the existing `DepartmentURL.OrderedAt` to `DueDate` — it now doubles as the SLA deadline (with time-of-day, since some orders require blocking within 6h/24h) that feature 3's notification system schedules a scan-and-notify check against.

## Background

`DepartmentURL` already carries an optional `OrderedAt *time.Time`, "the takedown-order date used by the time-to-compliance metric" (`ispComplianceTiming`). Per product decision, this field is being repurposed/renamed to `DueDate` rather than adding a parallel field — the time-to-compliance metric now effectively measures "did we comply before the deadline" instead of "how long after the order," which is at least as meaningful.

The new Agency/reference-number/requesting-department/status/requested-at fields are per-department-per-URL (many `DepartmentURL` rows can reference the same `Agency`/ref-number/department value — no admin-curated lookup table was requested for these, just plain columns), not global on `URL` — same scope as the existing `DueDate`.

`Status` (`requested | uplift | suspended`) is **independent** of the app's existing derived `Compliant` field — blocked/not-blocked already comes from scan results (per product decision, Excel's blocked/not-blocked column maps onto `Compliant`, not this new field).

## Decisions

- New nullable columns on `DepartmentURL`: `Agency string`, `ReferenceNumber string`, `RequestingDept string` (deliberately *not* named `Department`/reusing the `Department` FK — this is free text from Excel, e.g. a ministry name, distinct from the app's own CMOD/CRD RBAC `Department` model), `Status string`, `RequestedAt *time.Time`.
- `OrderedAt` renamed to `DueDate` (same type, `*time.Time`, already supports time-of-day — no type change, just the rename and a frontend input upgrade).
- `Status` is a free string column with a small server-side allow-list (`requested`, `uplift`, `suspended`) validated in the handler — not a DB enum type, matching this codebase's existing string-enum convention (`Instrument.Type`, `ScanRun.Status`, etc.).

## Migration

This codebase has no formal migration files — `internal/db/db.go`'s `Connect` just runs `gorm.AutoMigrate`, which **adds** columns matching current struct tags but never renames or drops. A plain rename in the Go struct would silently orphan the existing `ordered_at` column and its data on any already-deployed instance.

Add an explicit, idempotent rename **before** the `AutoMigrate` call in `Connect`:

```go
if database.Migrator().HasColumn(&DepartmentURL{}, "ordered_at") && !database.Migrator().HasColumn(&DepartmentURL{}, "due_date") {
    if err := database.Migrator().RenameColumn(&DepartmentURL{}, "ordered_at", "due_date"); err != nil {
        return nil, fmt.Errorf("renaming ordered_at to due_date: %w", err)
    }
}
```

Note `RenameColumn` takes raw column-name strings (works fine even though the Go struct no longer has an `OrderedAt` field) and must run against both the Postgres production path and the SQLite in-memory test path (`internal/db` tests) — verify it works on both drivers, not just Postgres.

## Backend

- `db.DepartmentURL`/`db.URLEntry`: rename `OrderedAt` → `DueDate` (JSON `ordered_at` → `due_date`), add the four new fields (JSON: `agency`, `reference_number`, `requesting_dept`, `status`, `requested_at`).
- `PATCH /api/urls/{id}`: extend the existing partial-update body to accept `due_date`, `agency`, `reference_number`, `requesting_dept`, `status`, `requested_at` alongside the existing `enabled` — same "only touches fields present in the body" semantics already used for `enabled`/`ordered_at`. Prefer one flexible update path (e.g. a single store method taking a struct of optional pointers, one `UPDATE`) over one setter method per field — mirror however the current handler already builds its partial update, don't fragment it into 6 near-duplicate `SetURLX` methods.
- `db.Store`'s `SetURLOrderedAt` → `SetURLDueDate` (or folded into the combined update method above).
- `ISPTimingResult.WithOrderDateCount` → `WithDueDateCount` (same field, renamed for consistency — `ispComplianceTiming`'s actual logic is unchanged, it already just reads whatever `*time.Time` is there).

## Frontend

- `web/src/api/types.ts`: `URLEntry.ordered_at` → `due_date`, add `agency?`, `reference_number?`, `requesting_dept?`, `status?`, `requested_at?`.
- `web/src/api/urls.ts`: rename `setUrlOrderedAt` → `setUrlDueDate` (or fold into a combined setter matching the backend's combined update method), add corresponding setters/fields for the new columns.
- `urls.tsx` watchlist rows: rename the existing "order date" `<input type="date">` to a due-date `<input type="datetime-local">` (carries the SLA time-of-day now), and add inline-editable fields for Agency/Reference Number/Requesting Department (plain text) and Status (a 3-option select: Requested/Uplift/Suspended) — same optimistic-update-with-rollback pattern already used for `enabled`/`ordered_at` in this file.

## Testing

- `internal/db` tests (SQLite in-memory): update any test referencing `OrderedAt`/`ordered_at` to `DueDate`/`due_date`; add a migration test that seeds `ordered_at` data on an existing-schema DB, runs the rename, and asserts the value survived under `due_date`.
- Server handler test for `PATCH /api/urls/{id}` covering the new fields (partial update — setting only `status` doesn't clobber `due_date`, etc.).
- Manual `dev.sh` verification: set Agency/reference number/requesting department/status/due-date-with-time on a watchlist row, refresh, confirm all persisted and displayed correctly; confirm the ISP timing page's time-to-compliance figure still computes (now against `due_date`).

## Out of Scope

- Any admin-curated lookup table / CRUD UI for Agency or Requesting Department — plain free-text columns for now; revisit only if the values need dropdown-style curation later.
- A bulk Excel import pipeline itself — this spec only adds the fields, API, and manual-entry UI; it doesn't build a CSV/XLSX upload feature.
- CLAUDE.md's frontend-architecture bullet list documents `ordered_at`/`setUrlOrderedAt` extensively — update the relevant passages (`urls.tsx`, `admin`/ISP-timing sections, `db.URLEntry`) to match the rename as part of this work, same as prior features' `docs: update CLAUDE.md` commits.
