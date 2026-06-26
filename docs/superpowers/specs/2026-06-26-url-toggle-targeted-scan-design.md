# Design: URL Toggle & Targeted Scan

**Date:** 2026-06-26
**Status:** Approved

## Overview

Two related features:

1. **URL Toggle** — each department independently enables/disables individual URLs from the scan sweep via a switch in their domain list.
2. **Targeted Scan** — the Run Scan button gains a "Scan Selected" mode letting users pick specific domains (from their watchlist) and/or enter ad-hoc URLs for a one-off scan.

Both features consolidate same-domain deduplication so a URL is only ever scanned once per DNS server regardless of how many departments watch it.

---

## Feature 1: URL Toggle

### Database

Add `Enabled bool` to `DepartmentURL`:

```go
type DepartmentURL struct {
    DepartmentID uint      `gorm:"primaryKey;autoIncrement:false"`
    URLID        uint      `gorm:"primaryKey;autoIncrement:false"`
    URL          URL       `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE"`
    Enabled      bool      `gorm:"not null;default:true"`
    CreatedAt    time.Time
}
```

GORM `AutoMigrate` adds the column; existing rows get `enabled = true` via the column default.

### Admin Department

`db.SeedDepartments` seeds a third department **"Admin"** alongside CMOD and CRD (only if `departments` table is empty — existing deployments keep their data). `db.SeedAdmin` assigns the bootstrap admin user to the Admin department (`DepartmentID = admin_dept_id`) instead of leaving it `nil`.

The existing invariant *"admin DepartmentID is nil"* is removed. `is_admin` remains the authoritative admin flag; `DepartmentID` is now required for every user including admins. Application code that checked `user.DepartmentID == nil` to detect admins is updated to check `user.IsAdmin` instead.

### Consolidation Rule — `ListWatchedURLs`

A URL enters the scan sweep if **at least one** department has it enabled:

```sql
SELECT DISTINCT urls.*
FROM urls
JOIN department_urls du ON du.url_id = urls.id AND du.enabled = true
ORDER BY urls.created_at ASC
```

If CMOD enables `example.com` and CRD disables it, the domain is still scanned once. Only when every watching department has it disabled does it drop out.

### Store Changes

New method on `db.Store`:

```go
SetURLEnabled(ctx context.Context, departmentID, urlID uint, enabled bool) error
```

`ListDepartmentURLs` return type changes — it must carry the `enabled` flag. Two options: return a new `WatchlistEntry` struct (URL + Enabled), or update `URL` to carry the flag when fetched in department scope. Use a new `URLEntry` struct to avoid polluting the shared `URL` model:

```go
type URLEntry struct {
    ID        uint      `json:"id"`
    URL       string    `json:"url"`
    Enabled   bool      `json:"enabled"`
    CreatedAt time.Time `json:"created_at"`
}
```

`ListDepartmentURLs` returns `[]URLEntry`. The existing `ListURLs` (admin global scope, used only by unassigned-URLs admin view) continues returning `[]URL`.

### API

| Method | Path | Auth | Body | Behaviour |
|--------|------|------|------|-----------|
| `PATCH` | `/api/urls/:id` | any authenticated | `{ "enabled": bool }` | Calls `SetURLEnabled` for user's own department. 404 if the URL is not on that department's watchlist. |

`GET /api/urls` for non-admin already calls `ListDepartmentURLs` — it now returns `URLEntry` (with `enabled`). Admin's `GET /api/urls` calls `ListURLs` — unchanged, no `enabled` field (admin sees all URLs globally without per-department context).

`AddToWatchlist` handler: admin no longer requires `department_id` in the body — they have their own department now, same flow as non-admins. The `"department_id is required"` guard for admins is removed.

### Frontend

1. Install: `npx shadcn@latest add @iconiq/r-switch`
2. `URLEntry` type in `web/src/api/types.ts` gains `enabled: boolean`.
3. `fetchUrls()` return type updated to `URLEntry[]`.
4. New API call in `web/src/api/urls.ts`: `setUrlEnabled(id: number, enabled: boolean)` → `PATCH /api/urls/:id`.
5. URL table in `urls.tsx` gets a Switch column. Toggle is optimistic — flip the local state immediately, revert on API error.
6. Admin's URL page now calls `ListDepartmentURLs` (Admin department) instead of `ListURLs` — admin sees and toggles their own department's URLs exactly like any other user. The global `ListURLs` is retained for the admin unassigned-URLs view (`GET /api/admin/urls/unassigned`) only.

---

## Feature 2: Targeted Scan

### UI

The Run Scan button becomes a split button with two actions:

- **Scan All** — existing behaviour, triggers `POST /api/scan` with no body.
- **Scan Selected** — opens a dialog containing:
  - Checklist of the user's own department's watched domains (all pre-checked by default).
  - A textarea for ad-hoc URLs, one per line (not saved to any watchlist).
  - "Start Scan" button that triggers `POST /api/scan` with the deduplicated URL list.

### Deduplication

Before the URL list reaches the crawler, normalize and deduplicate by hostname:

```
selected_watchlist_urls ∪ ad_hoc_urls  →  urlnorm.Normalize each  →  deduplicate  →  pass to scanner
```

This prevents double-scanning if the user picks a domain that also appears in their ad-hoc input.

### API

`POST /api/scan` gains an optional JSON body:

```json
{ "urls": ["example.com", "pornhub.com"] }
```

- Body absent or `urls` empty → scan all enabled watched URLs (current behaviour).
- `urls` provided → scan exactly those URLs (deduplicated and normalized server-side).

Non-admin users may only trigger scans; they are not restricted to their own watchlist for the URL list (to allow ad-hoc entries). The result visibility is still scoped by department.

### Backend

`Scanner.Trigger` signature change:

```go
func (sc *Scanner) Trigger(ctx context.Context, triggeredBy string, urls []string) error
```

- `urls == nil` or `len(urls) == 0` → call `ListWatchedURLs` as before.
- `urls` provided → normalize + deduplicate, write directly to the temp URL file. Skip `ListWatchedURLs`.

The scheduler calls `Trigger` with `nil` urls (no change in scheduled behaviour).

### Ad-hoc URL Handling

Ad-hoc URLs (not in any watchlist) are inserted via `CreateURL` (`FirstOrCreate` on normalized hostname) so they get a row ID. They are **not** linked to any `DepartmentURL` — they remain "unassigned." Scan results for these URLs are stored with a valid `URLID` but have no `DepartmentURL` link, so they appear in admin's global `LatestResults` view but not in any department user's scoped results.

---

## Consolidation Across Features

Both features share the same deduplication guarantee: the crawler receives each hostname exactly once per scan run, regardless of how many departments watch it or how the scan was triggered.

| Scan mode | Deduplication point |
|-----------|-------------------|
| Scheduled / Scan All | `ListWatchedURLs` DISTINCT + `enabled = true` |
| Scan Selected | Server-side normalize + deduplicate before writing temp file |

---

## Error Handling

- `PATCH /api/urls/:id` with an ID not on the user's watchlist → `404 Not Found`.
- `PATCH /api/urls/:id` while a scan is running → `200 OK` (toggle takes effect on the next scan run, not the current one).
- Scan Selected with an empty URL list (user unchecked all and typed nothing) → `400 Bad Request` from the server; frontend validates before submitting.
- Ad-hoc URL that fails `urlnorm.Normalize` → silently skipped server-side (same behaviour as the existing pipeline's invalid-URL handling).

---

## Out of Scope

- Per-DNS-server toggle (URLs are enabled/disabled across all DNS servers).
- Saving ad-hoc scan URLs to the watchlist automatically.
- Admin being able to toggle a URL for a specific non-admin department (admin manages the Admin department only via this UI; global overrides are an admin-panel concern if ever needed).
- Rate-limiting or lockout on scan triggers.
