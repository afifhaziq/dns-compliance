# URLs & DNS Servers Pages — Design Spec

**Date:** 2026-05-25  
**Status:** Approved

## Overview

Build out the URLs and DNS Servers management pages in the React frontend. Both pages are currently stubs ("coming soon"). All backend API endpoints are fully implemented and ready.

## Backend API (reference)

| Method | Endpoint | Body / Notes |
|--------|----------|--------------|
| GET | `/api/urls` | Returns `URLEntry[]` |
| POST | `/api/urls` | `{ url: string }` → 201 `URLEntry` |
| DELETE | `/api/urls/{id}` | → 204 No Content |
| GET | `/api/dns-servers` | Returns `DNSServer[]` |
| POST | `/api/dns-servers` | `{ name?: string, address: string, protocol: "udp"\|"dot"\|"doh" }` → 201 `DNSServer` |
| DELETE | `/api/dns-servers/{id}` | → 204 No Content |

## Component Installation

Before implementation, install the animate-ui dialog:

```bash
cd web && npx shadcn@latest add @animate-ui/components-radix-dialog
```

All modals (add and delete confirm) use this dialog component.

## Data Layer Changes

### `web/src/api/types.ts`

Add:
```ts
export type URLEntry = { id: number; url: string; created_at: string }
```

(`DNSServer` type already exists.)

### `web/src/api/urls.ts`

Replace current file with full CRUD:
- `fetchUrls(): Promise<URLEntry[]>` — GET /api/urls
- `createUrl(url: string): Promise<URLEntry>` — POST /api/urls
- `deleteUrl(id: number): Promise<void>` — DELETE /api/urls/{id}
- Keep `fetchUrlCount()` but delegate to `fetchUrls()` to avoid double fetch on Overview page, or leave as-is (it's a separate call on that page).

### `web/src/api/dns-servers.ts`

Replace current file with full CRUD:
- `fetchDnsServers(): Promise<DNSServer[]>` — GET /api/dns-servers
- `createDnsServer(data: { name: string; address: string; protocol: string }): Promise<DNSServer>` — POST /api/dns-servers
- `deleteDnsServer(id: number): Promise<void>` — DELETE /api/dns-servers/{id}
- Keep `fetchDnsServerCount()` delegating to `fetchDnsServers()`.

## Shared Components

### `<DeleteConfirmDialog>`

Location: `web/src/components/delete-confirm-dialog.tsx`

Props:
```ts
{
  open: boolean
  onConfirm: () => Promise<void>
  onCancel: () => void
  itemLabel: string      // e.g. "example.com" or "Cloudflare DoT"
  loading?: boolean
}
```

Renders: animate-ui Dialog with title "Delete [itemLabel]?", body "This will remove it from all future scans.", Cancel + Delete (red) buttons. Delete button shows loading state while `onConfirm` is in flight.

## URLs Page (`/urls`)

### Layout

- Page header: "URLs" title
- Header action: "Add URL" button (triggers add modal)
- Table using existing `.results-table` CSS class
- Columns: URL (full text), Created (formatted date), Actions (Delete button)
- Empty state: matches Results page style — icon + "No URLs yet" heading + "Add a URL to start monitoring DNS compliance." body

### Add URL Modal

Fields:
- URL input (required, placeholder `https://example.com`)

Validation: non-empty string. Submit calls `createUrl()`, closes modal on success, refreshes list. Displays inline error message on failure.

### Delete Flow

Click Delete → `<DeleteConfirmDialog>` opens with `itemLabel` = the URL string. Confirm calls `deleteUrl(id)`, closes dialog, refreshes list.

### State

Local component state: `urls`, `loading`, `error`, `addOpen` (boolean), `deleteTarget` (`URLEntry | null`).

## DNS Servers Page (`/dns-servers`)

### Layout

- Page header: "DNS Servers" title
- Header action: "Add Server" button
- Table using `.results-table` CSS class
- Columns: Name, Address (monospace), Protocol (badge: `udp` / `dot` / `doh`), Actions (Delete button)
- Empty state: icon + "No DNS servers yet" + "Add a DNS server to begin scanning." body

### Add DNS Server Modal

Fields:
- Name (optional, placeholder `e.g. Cloudflare DoT`)
- Address (required, placeholder `1.1.1.1:853` or `https://...`)
- Protocol (select): `udp` (default), `dot`, `doh`

Validation: address non-empty. If name is empty, it is sent as empty string (backend accepts it). Submit calls `createDnsServer()`, closes modal, refreshes list. Inline error on failure.

### Delete Flow

Same `<DeleteConfirmDialog>` pattern with `itemLabel` = server name (or address if name is empty).

### State

Local: `servers`, `loading`, `error`, `addOpen`, `deleteTarget` (`DNSServer | null`).

## Visual Style

- Tables: reuse `.results-table`, `.col-domain`, `.col-status` etc. CSS classes from `index.css` — no new CSS needed for table structure.
- Protocol badge: small pill using existing `.summary-chip` class variants (or inline style matching the chip pattern).
- Add / Delete buttons: use `.btn-primary` for Add; Delete uses a ghost/text style with red `var(--violation-text)` color.
- Modals: styled to match the existing design system tokens (`--stone-panel`, `--stone-border`, etc.).

## Error Handling

- List fetch errors: show inline error message with a Retry button (same pattern as Results page).
- Modal submit errors: display error text inside the modal (below the form), do not close the modal.
- Delete errors: display error text inside the confirm dialog.

## Out of Scope

- Edit/update existing items (no PUT endpoints on the backend)
- Pagination (not needed at current scale)
- Bulk delete
- URL validation beyond non-empty (backend handles further validation)
