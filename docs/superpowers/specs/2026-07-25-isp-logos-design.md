# ISP Logos Design

**Date:** 2026-07-25
**Branch:** main (design phase — not yet branched)

## Overview

The Overview page's `ISPBentoGrid` (`web/src/components/isp-bento-grid.tsx`) currently renders each ISP as a text-only card (name + compliance gauge + stats). This adds an optional logo image to each card's header.

`ISP` is not currently a first-class entity — it's a free-text string repeated across `DNSServer` rows (`internal/db/models.go`'s `DNSServer.ISP`), grouped implicitly wherever the UI needs it (`getISPNames`, `groupByISP`). This spec introduces the smallest possible entity for it: a lookup table keyed by ISP name, holding just a logo URL.

## Decisions

- **Logo source: admin-set URL, not upload or auto-fetch.** An admin/dept-admin pastes a link to an already-hosted image (their choice of host — Wikimedia Commons, the ISP's own brand-assets page, a CDN they control). No file upload endpoint, no server-side storage, no favicon-style fetch-and-cache — the `<img src>` points straight at the admin-supplied URL. Rejected alternatives:
  - Reusing the favicon-fetch pipeline (`internal/favicon/`) — favicons are low-resolution `.ico`/tiny raster icons, not brand-quality logos.
  - Real upload + server-side storage (like MinIO screenshots) — more robust against dead links, but disproportionate infrastructure for a cosmetic feature; can be revisited later if dead links become a real problem.
  - Self-hosting logo files in `web/public/logos/` — more reliable (no external dependency) but requires a code commit to add a new ISP's logo, which the admin explicitly did not want.
- **New table, not a field on `DNSServer`.** A `LogoURL` column on `DNSServer` would need to be kept in sync across every server row sharing that ISP (e.g. Cloudflare's DoT and UDP rows) — a new `ISPLogo` table keyed by ISP name avoids that duplication entirely.
- **Admin UI: new small section, mirroring the existing Compliant IPs manager.** Same list + "Add" dialog pattern already established in `admin.index.tsx` for `db.CompliantIP` — a table (ISP, logo preview, delete) plus an "Add ISP Logo" dialog (ISP name, logo URL). Scoped **admin-or-dept-admin** (`requireAnyAdmin`), matching DNS server mutation scope, since ISP names originate from the DNS server catalog those two roles already manage — not admin-only like Compliant IPs, which is a compliance-decision list rather than cosmetic metadata.
- **Rendering: fixed-size neutral chip, not raw `<img>`.** Every logo — regardless of source aspect ratio (portrait, square, circular badge) or baked-in background — renders inside a uniform 32×32px rounded chip using `object-fit: contain`, on a neutral gray/border background token (per `DESIGN.md`'s achromatic-plus-one-accent system; no color-coding). This keeps mismatched source assets visually consistent without any image processing. ISPs with no `logo_url` row fall back to a monogram chip (first letter of the ISP name), same size/shape, so the grid stays visually even regardless of logo coverage.

## Backend

### Model (`internal/db/models.go`)

```go
// ISPLogo is an admin-set logo URL for one ISP, rendered in the Overview
// page's ISPBentoGrid. Keyed by ISP name (not FK'd to DNSServer, since one
// ISP name is shared across multiple DNSServer rows) — see "ISP Logos
// Design". Purely cosmetic; never affects compliance calculations.
type ISPLogo struct {
	ISP      string `gorm:"primaryKey" json:"isp"`
	LogoURL  string `json:"logo_url"`
	CreatedAt time.Time `json:"created_at"`
}
```

Register in `internal/db/db.go`'s `AutoMigrate` call alongside the existing model list.

### Store interface (`internal/db/store.go`)

New sub-interface, same shape as `CompliantIPStore`:

```go
// ISPLogoStore is the admin-managed ISP name → logo URL lookup, rendered on
// the Overview page's ISPBentoGrid — see "ISP Logos Design".
type ISPLogoStore interface {
	ListISPLogos(ctx context.Context) ([]ISPLogo, error)
	UpsertISPLogo(ctx context.Context, isp, logoURL string) (ISPLogo, error)
	DeleteISPLogo(ctx context.Context, isp string) error
}
```

Embed `ISPLogoStore` in the top-level `Store` interface alongside `CompliantIPStore`.

`UpsertISPLogo` (not `Create`) because the ISP name is the primary key and re-adding the same ISP should overwrite its logo rather than error — simpler than requiring a separate "edit" flow in the admin UI's first version.

### Postgres implementation (`internal/db/postgres.go`)

Mirrors `ListCompliantIPs`/`CreateCompliantIP`/`DeleteCompliantIP`:

```go
func (s *postgresStore) ListISPLogos(ctx context.Context) ([]ISPLogo, error) {
	var logos []ISPLogo
	err := s.db.WithContext(ctx).Order("isp").Find(&logos).Error
	return logos, err
}

func (s *postgresStore) UpsertISPLogo(ctx context.Context, isp, logoURL string) (ISPLogo, error) {
	logo := ISPLogo{ISP: isp, LogoURL: logoURL}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "isp"}},
		DoUpdates: clause.AssignmentColumns([]string{"logo_url"}),
	}).Create(&logo).Error
	return logo, err
}

func (s *postgresStore) DeleteISPLogo(ctx context.Context, isp string) error {
	return s.db.WithContext(ctx).Delete(&ISPLogo{}, "isp = ?", isp).Error
}
```

### Handlers (`internal/server/admin_handlers.go`)

Mirrors `ListCompliantIPs`/`CreateCompliantIP`/`DeleteCompliantIP`:

- `ListISPLogos` — any authenticated role (the bento grid needs it for every user, not just admins), returns `[]db.ISPLogo`.
- `UpsertISPLogo` — admin-or-dept-admin; body `{ "isp": string, "logo_url": string }`, both required (400 if either is empty); calls `store.UpsertISPLogo`.
- `DeleteISPLogo` — admin-or-dept-admin; path param `{isp}` (URL-encoded, since ISP names can contain spaces — use a chi wildcard route `/admin/isp-logos/*` like the existing `*url` routes, not `{isp}`, to be safe with arbitrary characters).

All three use `writeInternalError` for store failures, not raw `err.Error()` (per current handler conventions).

### Router (`internal/server/router.go`)

```go
r.Get("/isp-logos", h.ListISPLogos) // inside the requireAuth group, any role

r.Group(func(r chi.Router) {
    r.Use(requireAnyAdmin)
    // ...existing dns-servers/admin/users routes...
    r.Post("/admin/isp-logos", h.UpsertISPLogo)
    r.Delete("/admin/isp-logos/*", h.DeleteISPLogo)
})
```

## Frontend

### Types & API (`web/src/api/types.ts`, new `web/src/api/isp-logos.ts`)

```ts
export type ISPLogo = { isp: string; logo_url: string; created_at: string }
```

```ts
// web/src/api/isp-logos.ts
import { api } from './client'
import type { ISPLogo } from './types'

export const fetchISPLogos = () => api.get<ISPLogo[]>('/isp-logos')
export const upsertISPLogo = (isp: string, logoUrl: string) =>
  api.post<ISPLogo>('/admin/isp-logos', { isp, logo_url: logoUrl })
export const deleteISPLogo = (isp: string) =>
  api.delete<void>(`/admin/isp-logos/${encodeURIComponent(isp)}`)
```

### Admin UI (`web/src/routes/admin.index.tsx`)

New "ISP Logos" section, visible to admin and dept-admin (same visibility as the DNS Servers management controls, not the super-admin-only sections). Table columns: logo preview (the same 32px chip component used on the bento grid), ISP name, delete button. "Add" dialog: an ISP name field (a `<select>` populated from the distinct ISP names already in `fetchDnsServers()`, not free text — avoids typos creating an orphaned logo entry for an ISP that doesn't exist) and a logo URL text input.

### Logo chip component (`web/src/components/isp-logo-chip.tsx`, new)

Shared between the admin table preview and the bento grid, so the two places render identically:

```tsx
export function ISPLogoChip({ isp, logoUrl, size = 32 }: { isp: string; logoUrl?: string; size?: number }) {
  return (
    <div className="isp-logo-chip" style={{ width: size, height: size }}>
      {logoUrl
        ? <img src={logoUrl} alt="" style={{ objectFit: 'contain', width: '100%', height: '100%' }} />
        : <span className="isp-logo-fallback">{isp.charAt(0).toUpperCase()}</span>}
    </div>
  )
}
```

`.isp-logo-chip` CSS (added to `web/src/index.css` alongside other `dash-*`/`bento-*` tokens): neutral rounded box using existing gray/border custom properties, centers its content. No new color tokens.

### Bento grid wiring (`isp-bento-grid.tsx`)

`ISPBentoGrid` fetches `fetchISPLogos()` once alongside `getISPNames(results)` (not per-card — one request for the whole grid), builds an `isp → logo_url` map, and passes the matching `logoUrl` (possibly `undefined`) into each `ISPCard`, which renders `<ISPLogoChip isp={isp} logoUrl={logoUrl} />` in `.bento-card-header` before the ISP name link.

## Testing

- Backend: a `handlers_test.go` case per new handler (list/upsert/delete), following the existing `TestListCompliantIPs`/`TestCreateCompliantIP` shape — upsert-on-conflict behavior (re-adding the same ISP overwrites rather than erroring) gets its own case.
- Frontend: no existing test suite covers `admin.index.tsx` or `isp-bento-grid.tsx` today (manual/dev-server verification only, consistent with how the rest of the frontend is currently validated per `CLAUDE.md`) — verify manually via `dev.sh`: add a logo, confirm it renders in both the admin table and the bento grid, confirm the fallback monogram shows for an ISP with no logo set, confirm delete removes it from both places.

## Out of Scope

- Logo upload/storage — deferred; URL-only for now (see Decisions above).
- Dead-link detection or health-checking of admin-supplied URLs.
- Per-DNS-server (as opposed to per-ISP) logos.
