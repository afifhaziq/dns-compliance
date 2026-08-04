# Tabbed Admin Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure `/admin` into four tabs (Departments, Users, IP, Scan Settings) with the active tab synced to a `?tab=` URL param, and remove the ISP Logos section from this page entirely.

**Architecture:** `admin.index.tsx` first loses everything that exists only to support the ISP Logos section (Task 1) — a clean, independently-reviewable deletion. Then its four remaining sections get wrapped in the codebase's existing `Tabs`/`TabsList`/`TabsTrigger`/`TabsContent` components (`web/src/components/motion/tabs.tsx`, already used by `domain.$url.tsx`), controlled by a `tab` value read from/written to the route's `?tab=` search param, with a new small `RestrictedTabNotice` shown on tabs a department admin can't use (Task 2). No backend changes; no changes to the `ISPLogo` feature itself, only its redundant copy on this page.

**Tech Stack:** React + TypeScript, TanStack Router (`validateSearch`, `useNavigate`), the existing vendored `Tabs` component.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-04-tabbed-admin-page-design.md` — read it for full rationale; this plan implements it task-by-task.
- All 4 tabs render for every role (no role-based tab filtering); a department admin sees `RestrictedTabNotice` inside Departments/IP/Scan Settings instead of the real content.
- Default tab (missing or invalid `?tab=`) is `users`.
- Active tab is a *controlled* `Tabs` (`value`/`onValueChange`), not `defaultValue`-only, so switching tabs updates the URL and a refresh preserves the current tab.
- No change to the `ISPLogo` backend, its API, or `dns-servers.tsx`'s own logo-editing entry point — only `admin.index.tsx`'s copy of this UI is removed.
- No existing test suite covers this file — verify via `tsc`/lint plus manual `dev.sh` checks, consistent with the rest of the frontend per `CLAUDE.md`.

---

### Task 1: Remove the ISP Logos section from `admin.index.tsx`

**Files:**
- Modify: `web/src/routes/admin.index.tsx`

**Interfaces:**
- Consumes: nothing new.
- Produces: a smaller `admin.index.tsx` with no `ISPLogo`/`DNSServer`/ISP-logo-dialog code left in it — Task 2 builds on this cleaned-up file. `AdminPage`'s `load()` signature and behavior for departments/users/compliantIPs/scanSchedule are unchanged.

- [ ] **Step 1: Remove ISP-logo-only imports**

Replace lines 16-19:
```ts
import { fetchISPLogos, upsertISPLogo, deleteISPLogo } from '../api/isp-logos'
import { fetchDnsServers } from '../api/dns-servers'
import type { CompliantIP, Department, DNSServer, ISPLogo, User } from '../api/types'
import { ISPLogoChip } from '@/components/isp-logo-chip'
```
with:
```ts
import type { CompliantIP, Department, User } from '../api/types'
```

- [ ] **Step 2: Delete the `AddISPLogoDialog` component**

Delete the entire block from the `/* ─── Add ISP Logo Dialog ─── */` comment through its closing `}` (currently lines 334-419):
```tsx
/* ─── Add ISP Logo Dialog ────────────────────────────────────────────────── */

function AddISPLogoDialog({
  open,
  onClose,
  onAdded,
  ispOptions,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  ispOptions: string[]
}) {
  const [isp, setIsp] = useState('')
  const [logoUrl, setLogoUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setIsp(''); setLogoUrl(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!isp) { setError('ISP is required'); return }
    if (!logoUrl.trim()) { setError('Logo URL is required'); return }
    setLoading(true)
    setError(null)
    try {
      await upsertISPLogo(isp, logoUrl.trim())
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add logo')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Add ISP Logo</DialogTitle>
          <DialogDescription>
            Sets the logo shown for this ISP in the Overview page's bento grid. Re-adding an existing ISP overwrites its logo.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" id="isp-logo-isp-label">ISP</label>
            <Select value={isp} onValueChange={setIsp} disabled={loading}>
              <SelectTrigger aria-labelledby="isp-logo-isp-label" className="w-full" />
              <SelectContent>
                {ispOptions.map((name, i) => (
                  <SelectItem key={name} index={i} value={name}>{name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="isp-logo-url-input">Logo URL</label>
            <input
              id="isp-logo-url-input"
              className="form-input"
              type="text"
              placeholder="e.g. https://upload.wikimedia.org/.../cloudflare.svg"
              value={logoUrl}
              onChange={e => setLogoUrl(e.target.value)}
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Logo'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 3: Remove ISP-logo state from `AdminPage`**

Delete these two lines (currently lines 550-551):
```tsx
  const [ispLogos, setIspLogos] = useState<ISPLogo[]>([])
  const [dnsServers, setDnsServers] = useState<DNSServer[]>([])
```

Delete these two lines (currently lines 558-559):
```tsx
  const [addLogoOpen, setAddLogoOpen] = useState(false)
  const [deleteLogoTarget, setDeleteLogoTarget] = useState<ISPLogo | null>(null)
```

- [ ] **Step 4: Simplify `load()`**

Replace the whole `load` function (currently lines 563-592):
```tsx
  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      // ISP Logos and DNS servers are readable by admin and dept-admin alike
      // (dept-admins already manage the DNS server catalog), unlike the
      // super-admin-only fetches below.
      const [logos, servers] = await Promise.all([fetchISPLogos(), fetchDnsServers()])
      setIspLogos(logos)
      setDnsServers(servers)

      if (me?.is_admin) {
        // Departments/Compliant-IPs/scan interval stay super-admin-only
        // server-side — a department admin would just get a 403 fetching them.
        const [d, u, ips, schedule] = await Promise.all([
          fetchDepartments(), fetchUsers(), fetchCompliantIPs(), fetchScanInterval(),
        ])
        setDepartments(d)
        setUsers(u)
        setCompliantIPs(ips)
        setScanSchedule(schedule)
      } else {
        setUsers(await fetchUsers())
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load admin data')
    } finally {
      setLoading(false)
    }
  }, [me])
```
with:
```tsx
  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      if (me?.is_admin) {
        // Departments/Compliant-IPs/scan interval stay super-admin-only
        // server-side — a department admin would just get a 403 fetching them.
        const [d, u, ips, schedule] = await Promise.all([
          fetchDepartments(), fetchUsers(), fetchCompliantIPs(), fetchScanInterval(),
        ])
        setDepartments(d)
        setUsers(u)
        setCompliantIPs(ips)
        setScanSchedule(schedule)
      } else {
        setUsers(await fetchUsers())
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load admin data')
    } finally {
      setLoading(false)
    }
  }, [me])
```

- [ ] **Step 5: Delete `handleDeleteLogo`**

Delete (currently lines 622-627):
```tsx
  const handleDeleteLogo = async () => {
    if (!deleteLogoTarget) return
    await deleteISPLogo(deleteLogoTarget.isp)
    setDeleteLogoTarget(null)
    load()
  }
```

- [ ] **Step 6: Delete the ISP Logos table section JSX**

Delete the whole block (currently lines 717-762, the `<div className='mb-4'>` for ISP Logos, sitting between the Users section and the `{me?.is_admin && (` block that opens Compliant IPs):
```tsx
        <div className='mb-4'>
        <div className="page-header" style={{ marginBottom: 12 }}>
          <h2 className="section-title">ISP Logos</h2>
          <p className="page-subtitle" style={{ marginLeft: 8 }}>Shown next to each ISP's name on the Overview page</p>
          <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddLogoOpen(true)}>
            + Add Logo
          </button>
        </div>
        <Table className="results-table" aria-label="ISP Logos">
          <TableHeader>
            <TableRow>
              <TableHead className="col-status" scope="col">Logo</TableHead>
              <TableHead className="col-domain th-left" scope="col">ISP</TableHead>
              <TableHead className="col-evidence" scope="col" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {ispLogos.map(logo => (
              <TableRow key={logo.isp} className="admin-row">
                <TableCell className="col-status text-center">
                  <ISPLogoChip isp={logo.isp} logoUrl={logo.logo_url} size={24} />
                </TableCell>
                <TableCell className="col-domain">{logo.isp}</TableCell>
                <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                  <button
                    type="button"
                    className="screenshot-icon-btn"
                    onClick={() => setDeleteLogoTarget(logo)}
                    aria-label={`Delete logo for ${logo.isp}`}
                    title="Delete"
                  >
                    <XIcon size={16} />
                  </button>
                </TableCell>
              </TableRow>
            ))}
            {ispLogos.length === 0 && !loading && (
              <TableRow>
                <TableCell colSpan={3} style={{ textAlign: 'center', color: 'var(--stone-muted)', padding: '16px 0' }}>
                  No ISP logos configured
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        </div>
```

- [ ] **Step 7: Delete the ISP-logo dialog renders**

Delete (currently lines 822-827):
```tsx
      <AddISPLogoDialog
        open={addLogoOpen}
        onClose={() => setAddLogoOpen(false)}
        onAdded={load}
        ispOptions={Array.from(new Set(dnsServers.map(s => s.isp))).sort()}
      />
```

Delete (currently lines 841-847):
```tsx
      <DeleteConfirmDialog
        open={deleteLogoTarget !== null}
        itemLabel={deleteLogoTarget?.isp ?? ''}
        description="The bento grid will fall back to a monogram for this ISP."
        onConfirm={handleDeleteLogo}
        onCancel={() => setDeleteLogoTarget(null)}
      />
```

- [ ] **Step 8: Type-check and lint**

Run: `cd web && npx tsc --noEmit && npm run lint`

Expected: no errors — no leftover references to `ISPLogo`, `DNSServer`, `ispLogos`, `dnsServers`, `addLogoOpen`, `deleteLogoTarget`, `handleDeleteLogo`, `AddISPLogoDialog`, `ISPLogoChip`, `fetchISPLogos`, `fetchDnsServers`, `upsertISPLogo`, or `deleteISPLogo` anywhere in the file.

- [ ] **Step 9: Manual verification**

Run `./dev.sh` (or the server + `npm run dev` combo per `CLAUDE.md`). Log in as a super-admin and visit `/admin`:
1. Confirm the ISP Logos section is gone — page shows Departments, Scan Settings, Users, Compliant IPs only (still stacked, tabs are Task 2).
2. Confirm no console errors on load.
3. Visit `/dns-servers` and confirm ISP logo editing (clicking an `ISPLogoChip` on a card to open `EditISPLogoDialog`) still works exactly as before — this page and its logo-editing entry point are untouched.

- [ ] **Step 10: Commit**

```bash
git add web/src/routes/admin.index.tsx
git commit -m "$(cat <<'EOF'
refactor: remove ISP Logos section from the admin page

ISP logo management already has its own entry point on dns-servers.tsx
(EditISPLogoDialog, per CLAUDE.md) — this was a redundant second copy.
The ISPLogo feature/API/model are untouched; only this page's copy of
the management UI is removed. Prep step before tabbing the remaining
four sections (see docs/superpowers/specs/2026-08-04-tabbed-admin-page-design.md).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Restructure the remaining sections into tabs

**Files:**
- Modify: `web/src/routes/admin.index.tsx`

**Interfaces:**
- Consumes: the post-Task-1 file (no ISP Logos code remaining).
- Produces: `type AdminTab = 'departments' | 'users' | 'ip' | 'scan-settings'`, a `RestrictedTabNotice` component, and a route with `validateSearch` returning `{ tab: AdminTab }` — nothing outside this file depends on these, but they must stay internally consistent within it.

- [ ] **Step 1: Add imports**

Replace line 1:
```ts
import { useCallback, useEffect, useState } from 'react'
```
with (unchanged — no new React imports needed).

Replace line 2:
```ts
import { createFileRoute } from '@tanstack/react-router'
```
with:
```ts
import { createFileRoute, useNavigate } from '@tanstack/react-router'
```

Add a new import line right after the `@tanstack/react-router` import:
```ts
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/motion/tabs'
```

- [ ] **Step 2: Add the `AdminTab` type and route `validateSearch`**

Replace line 36 (`export const Route = createFileRoute('/admin/')({ component: AdminPage })`) with:
```ts
const ADMIN_TABS = ['departments', 'users', 'ip', 'scan-settings'] as const
type AdminTab = typeof ADMIN_TABS[number]

export const Route = createFileRoute('/admin/')({
  component: AdminPage,
  validateSearch: (search: Record<string, unknown>): { tab: AdminTab } => ({
    tab: ADMIN_TABS.includes(search.tab as AdminTab) ? (search.tab as AdminTab) : 'users',
  }),
})
```

- [ ] **Step 3: Add `RestrictedTabNotice`**

Add this right after the `ScanSettingsSection` function's closing `}` and before the `/* ─── Admin Page ─── */` comment:

```tsx
/* ─── Restricted Tab Notice ──────────────────────────────────────────────── */

function RestrictedTabNotice() {
  return (
    <div className="error-state">
      <p className="error-message">Admin access required.</p>
    </div>
  )
}
```

- [ ] **Step 4: Read/write the active tab in `AdminPage`**

In `AdminPage`, right after `const { me } = useAuth()`, add:
```tsx
  const { tab } = Route.useSearch()
  const navigate = useNavigate()
```

- [ ] **Step 5: Wrap the four sections in `Tabs`**

Replace the entire `<div className="results-wrap" style={{ marginBottom: 32 }}>...</div>` block (the whole section between the error-state block and the dialog renders — currently, post-Task-1, spans from the `Departments`/`is_admin` block through the `Compliant IPs`/`is_admin` block) with:

```tsx
      <div className="results-wrap" style={{ marginBottom: 32 }}>
        <Tabs
          value={tab}
          onValueChange={next => navigate({ to: '/admin', search: { tab: next as AdminTab } })}
          variant="underline"
        >
          <TabsList>
            <TabsTrigger value="departments">Departments</TabsTrigger>
            <TabsTrigger value="users">Users</TabsTrigger>
            <TabsTrigger value="ip">IP</TabsTrigger>
            <TabsTrigger value="scan-settings">Scan Settings</TabsTrigger>
          </TabsList>

          <TabsContent value="departments">
            {me?.is_admin ? (
              <div className='mb-4'>
                <div className="page-header" style={{ marginBottom: 12 }}>
                  <h2 className="section-title">Departments</h2>
                  <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddDeptOpen(true)}>
                    + Add Department
                  </button>
                </div>
                <Table className="results-table" aria-label="Departments">
                  <TableHeader>
                    <TableRow>
                      <TableHead className="col-domain th-left" scope="col">Name</TableHead>
                      <TableHead className="col-status" scope="col">Created</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {departments.map(d => (
                      <TableRow key={d.id} className="admin-row">
                        <TableCell className="col-domain">{d.name}</TableCell>
                        <TableCell className="col-status text-center">{DATE_FMT.format(new Date(d.created_at))}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <RestrictedTabNotice />
            )}
          </TabsContent>

          <TabsContent value="users">
            <div className='mb-4'>
              <div className="page-header" style={{ marginBottom: 12 }}>
                <h2 className="section-title">Users</h2>
                <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddUserOpen(true)}>
                  + Create User
                </button>
              </div>
              <Table className="results-table" aria-label="Users">
                <TableHeader>
                  <TableRow>
                    <TableHead className="col-domain th-left" scope="col">Username</TableHead>
                    <TableHead className="col-status" scope="col">Department</TableHead>
                    <TableHead className="col-status" scope="col">Created</TableHead>
                    <TableHead className="col-evidence" scope="col" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {users.map(u => (
                    <TableRow key={u.id} className="admin-row">
                      <TableCell className="col-domain">{u.username}</TableCell>
                      <TableCell className="col-status">
                        {u.is_admin ? 'Admin' : u.is_dept_admin ? `${u.department?.name ?? '—'} (Admin)` : u.department?.name ?? '—'}
                      </TableCell>
                      <TableCell className="col-status">{DATE_FMT.format(new Date(u.created_at))}</TableCell>
                      <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                        <button
                          type="button"
                          className="screenshot-icon-btn"
                          onClick={() => setDeleteTarget(u)}
                          aria-label={`Delete ${u.username}`}
                          title="Delete"
                        >
                          <XIcon size={16} />
                        </button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </TabsContent>

          <TabsContent value="ip">
            {me?.is_admin ? (
              <div className='mb-4'>
                <div className="page-header" style={{ marginBottom: 12 }}>
                  <h2 className="section-title">Compliant IPs</h2>
                  <p className="page-subtitle" style={{ marginLeft: 8 }}>DNS resolutions to these IPs are classified as compliant</p>
                  <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddIPOpen(true)}>
                    + Add IP
                  </button>
                </div>
                <Table className="results-table" aria-label="Compliant IPs">
                  <TableHeader>
                    <TableRow>
                      <TableHead className="col-domain th-left" scope="col">IP Address</TableHead>
                      <TableHead className="col-status" scope="col">Note</TableHead>
                      <TableHead className="col-status" scope="col">Added</TableHead>
                      <TableHead className="col-evidence" scope="col" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {compliantIPs.map(ip => (
                      <TableRow key={ip.id} className="admin-row">
                        <TableCell className="col-domain"><span className="ip-value">{ip.address}</span></TableCell>
                        <TableCell className="col-status">{ip.note || '—'}</TableCell>
                        <TableCell className="col-status">{DATE_FMT.format(new Date(ip.created_at))}</TableCell>
                        <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                          <button
                            type="button"
                            className="screenshot-icon-btn"
                            onClick={() => setDeleteIPTarget(ip)}
                            aria-label={`Delete ${ip.address}`}
                            title="Delete"
                          >
                            <XIcon size={16} />
                          </button>
                        </TableCell>
                      </TableRow>
                    ))}
                    {compliantIPs.length === 0 && !loading && (
                      <TableRow>
                        <TableCell colSpan={4} style={{ textAlign: 'center', color: 'var(--stone-muted)', padding: '16px 0' }}>
                          No compliant IPs configured
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <RestrictedTabNotice />
            )}
          </TabsContent>

          <TabsContent value="scan-settings">
            {me?.is_admin && scanSchedule !== null ? (
              <ScanSettingsSection value={scanSchedule} onSaved={setScanSchedule} />
            ) : (
              <RestrictedTabNotice />
            )}
          </TabsContent>
        </Tabs>
      </div>
```

- [ ] **Step 6: Type-check and lint**

Run: `cd web && npx tsc --noEmit && npm run lint`

Expected: no errors.

- [ ] **Step 7: Manual verification**

With `./dev.sh` running:
1. Log in as super-admin, visit `/admin` — confirm it defaults to the Users tab, all 4 tabs are clickable and show their correct content (Departments table, Users table, IP/Compliant-IPs table, Scan Settings controls), and switching tabs updates the URL to `?tab=departments` / `?tab=ip` / `?tab=scan-settings` / `?tab=users`.
2. While on a non-default tab (e.g. click IP so the URL is `/admin?tab=ip`), refresh the page — confirm it's still on the IP tab, not reset to Users.
3. Visit `/admin?tab=bogus` directly — confirm it falls back to the Users tab instead of erroring.
4. Log in as a department admin, visit `/admin` — confirm the Users tab works exactly as before (create/delete a user), and confirm Departments, IP, and Scan Settings tabs each show "Admin access required" instead of a table, an error, or a blank area.
5. Confirm every existing action still works from within its tab: Add Department, Create User (with role/department picker for super-admin, without for dept-admin), delete a user, Add/Delete Compliant IP, Save on Scan Settings.

- [ ] **Step 8: Commit**

```bash
git add web/src/routes/admin.index.tsx
git commit -m "$(cat <<'EOF'
feat: restructure the admin page into tabs

Departments, Users, IP, and Scan Settings become Tabs/TabsContent
(reusing the same component domain.$url.tsx already uses) instead of
stacked sections, with the active tab synced to ?tab= so a refresh or
shared link preserves it. A department admin sees all 4 tabs but gets
a RestrictedTabNotice on the three that are super-admin-only
server-side, instead of the tab simply not existing for them.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review

**Spec coverage:**
- 4 tabs (Departments, Users, IP, Scan Settings), in that order → Task 2, Step 5.
- All 4 tabs shown to every role, `RestrictedTabNotice` on the 3 super-admin-only ones for a dept-admin → Task 2, Steps 3 and 5.
- Default tab `users` on missing/invalid `?tab=` → Task 2, Step 2.
- Controlled `Tabs` (`value`/`onValueChange`) syncing to `?tab=`, not `defaultValue`-only → Task 2, Steps 2, 4, 5.
- Full ISP Logos removal checklist from the spec (component, 2 imports, type imports, 4 state fields, `handleDeleteLogo`, table JSX, 2 dialog renders, `load()` simplification) → Task 1, Steps 1-7, each spec bullet mapped to its own step.
- `dns-servers.tsx`'s ISP logo entry point left untouched → verified manually in Task 1, Step 9.3; no code in either task touches that file.
- No backend changes → neither task touches any Go file.

**Placeholder scan:** No "TBD"/"TODO"/"handle appropriately" — every step is a complete, literal diff or an exact shell command.

**Type consistency:** `AdminTab` (Task 2, Step 2) is used identically in Step 4's `Route.useSearch()`, Step 5's `onValueChange`/`TabsTrigger` values (`'departments' | 'users' | 'ip' | 'scan-settings'`, matching `ADMIN_TABS` exactly), and nowhere else needs it since this type isn't exported. `RestrictedTabNotice` (Step 3) takes no props and is called identically (bare `<RestrictedTabNotice />`) in all three of its Step 5 call sites. Every table's columns/state field names (`departments`, `users`, `compliantIPs`, `scanSchedule`) match their Task-1-untouched declarations exactly — Task 2 only moves this JSX, never renames anything it reads.
