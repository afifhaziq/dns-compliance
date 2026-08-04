# Tabbed Admin Page Design

**Date:** 2026-08-04
**Branch:** main (design phase — not yet branched)

## Overview

`web/src/routes/admin.index.tsx` currently stacks five sections vertically (Departments, Scan Settings, Users, ISP Logos, Compliant IPs). This restructures it into four tabs — Departments, Users, IP, Scan Settings — and removes the ISP Logos section from this page entirely (the feature itself is untouched; it's still manageable from `dns-servers.tsx`'s `EditISPLogoDialog`, this just removes the redundant admin-page copy).

## Background

Today, three of the five sections (Departments, Scan Settings, Compliant IPs) are gated `me?.is_admin` (super-admin only) — a department admin never sees them at all, only Users and ISP Logos. `load()` already conditions its fetches on `me?.is_admin`, so a dept-admin never calls the restricted endpoints today. This scoping is preserved exactly; tabs don't change who can see what data, only how the four remaining sections are laid out.

## Decisions

- **All 4 tabs shown to every role**, including a department admin. A dept-admin sees Departments/IP/Scan Settings tabs but gets a restricted-access notice inside them instead of the real content — chosen over filtering the tab bar per role so a dept-admin isn't left with a single-tab page, and because the underlying data these tabs would show is already 403'd server-side regardless of what the frontend renders.
- **Default tab: Users.** The one tab every role has real content in, so where you land doesn't depend on your role.
- **Active tab is synced to a `?tab=` URL search param**, using the exact `validateSearch` pattern `domain.$url.tsx` already uses for its Overview/History tabs — but *controlled* (`Tabs value={tab} onValueChange=...`), not `defaultValue`-only like `domain.$url.tsx`, because a controlled `Tabs` is what makes "refresh keeps you on the same tab" actually work: clicking a tab has to update the URL, not just local component state.
- **ISP Logos section deleted outright**, not hidden/moved — along with everything in this file that existed only to support it (see Frontend section below). Nothing about the `ISPLogo` API, model, or `dns-servers.tsx`'s own logo-editing entry point changes.

## Frontend (`web/src/routes/admin.index.tsx`)

### Route

```ts
export const Route = createFileRoute('/admin/')({
  component: AdminPage,
  validateSearch: (search: Record<string, unknown>): { tab: 'departments' | 'users' | 'ip' | 'scan-settings' } => ({
    tab: (['departments', 'users', 'ip', 'scan-settings'] as const).includes(search.tab as any)
      ? (search.tab as 'departments' | 'users' | 'ip' | 'scan-settings')
      : 'users',
  }),
})
```

Invalid or missing `?tab=` falls back to `users`.

### `AdminPage`

- Add `import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/motion/tabs'` and `import { useNavigate } from '@tanstack/react-router'` (the file's existing `createFileRoute` import from `@tanstack/react-router` gets `useNavigate` added alongside it).
- `const { tab } = Route.useSearch()`, `const navigate = useNavigate()`.
- Wrap the four remaining sections:
  ```tsx
  <Tabs value={tab} onValueChange={next => navigate({ to: '/admin', search: { tab: next as typeof tab } })} variant="underline">
    <TabsList>
      <TabsTrigger value="departments">Departments</TabsTrigger>
      <TabsTrigger value="users">Users</TabsTrigger>
      <TabsTrigger value="ip">IP</TabsTrigger>
      <TabsTrigger value="scan-settings">Scan Settings</TabsTrigger>
    </TabsList>
    <TabsContent value="departments">{me?.is_admin ? <>{/* existing Departments section JSX, unmodified */}</> : <RestrictedTabNotice />}</TabsContent>
    <TabsContent value="users">{/* existing Users section JSX, unmodified — no role gate, matches today */}</TabsContent>
    <TabsContent value="ip">{me?.is_admin ? <>{/* existing Compliant IPs section JSX, unmodified */}</> : <RestrictedTabNotice />}</TabsContent>
    <TabsContent value="scan-settings">{me?.is_admin && scanSchedule !== null ? <ScanSettingsSection value={scanSchedule} onSaved={setScanSchedule} /> : <RestrictedTabNotice />}</TabsContent>
  </Tabs>
  ```
  Each `TabsContent`'s inner content is today's existing JSX for that section moved as-is — no visual redesign of the sections themselves, just their container (stacked `<div className='mb-4'>` → `<TabsContent>`).

### `RestrictedTabNotice` (new, small)

```tsx
function RestrictedTabNotice() {
  return (
    <div className="error-state">
      <p className="error-message">Admin access required.</p>
    </div>
  )
}
```

Reuses the exact `error-state`/`error-message` classes already used for this file's whole-page "no access" case (for a user with neither `is_admin` nor `is_dept_admin`) — same visual language, just scoped to one tab instead of the whole page.

### ISP Logos removal — full list

Everything below is deleted from `admin.index.tsx`; nothing here is deleted from the wider codebase (the `ISPLogo` API/model/`dns-servers.tsx` entry point are all untouched):

- `AddISPLogoDialog` function (the whole component).
- `fetchISPLogos`/`upsertISPLogo`/`deleteISPLogo` import from `../api/isp-logos`.
- `fetchDnsServers` import from `../api/dns-servers` — its only consumer in this file is building `ispOptions` for `AddISPLogoDialog`; nothing else here touches `dnsServers`, so it's fully dead once that dialog goes.
- `ISPLogoChip` import.
- `ISPLogo` and `DNSServer` from the `types` import (keep `CompliantIP`, `Department`, `User`).
- `ispLogos`/`setIspLogos` and `dnsServers`/`setDnsServers` state.
- `addLogoOpen`/`setAddLogoOpen` and `deleteLogoTarget`/`setDeleteLogoTarget` state.
- `handleDeleteLogo`.
- The ISP Logos `<Table>` section JSX.
- The `<AddISPLogoDialog .../>` render and the ISP-logo `<DeleteConfirmDialog .../>` render.

`load()` drops its `Promise.all([fetchISPLogos(), fetchDnsServers()])` block (the one thing it fetched unconditionally for both roles) — the rest of `load()`'s role-conditioned fetching (departments/users/compliantIPs/scanSchedule for `is_admin`, users-only otherwise) is unchanged.

## Error Handling

Unchanged from today — the page-level `error` state and its retry button still wrap everything. No new network calls are introduced, only removed (the ISP-logo-related ones), so no new failure modes.

## Testing

No existing test suite covers `admin.index.tsx` (manual/dev-server verification only, consistent with the rest of the frontend per `CLAUDE.md`). Verify manually via `dev.sh`:
- As super-admin: all 4 tabs render their real content, default tab on load is Users, ISP Logos section is gone from this page, ISP logo management is still reachable and working from `/dns-servers`.
- As a department admin: Users tab works exactly as before; Departments, IP, and Scan Settings tabs show "Admin access required" instead of crashing, an empty table, or a 403 in the console.
- Click a non-default tab (e.g. IP), refresh the page — confirm it's still on that tab (URL round-trip via `?tab=`).
- Directly visit `/admin?tab=bogus` — confirm it falls back to the Users tab rather than erroring.

## Out of Scope

- Any change to the `ISPLogo` backend (model/store/handlers) or to `dns-servers.tsx`'s own logo-editing UI — untouched.
- Redesigning the visual content of any individual section (Departments table, Users table, Compliant IPs table, Scan Settings controls) — only their container changes.
- Any change to server-side authorization (`requireAdmin`/`requireAnyAdmin`) — the restricted-tab notice is a frontend-only reflection of access that's already enforced server-side.
