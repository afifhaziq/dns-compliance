# URLs & DNS Servers Pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the URLs and DNS Servers management pages with full CRUD — list, add (modal), delete (confirm modal) — using the animate-ui Radix dialog component.

**Architecture:** Each page is a self-contained route component with local state. A shared `DeleteConfirmDialog` handles deletion across both pages. API clients are expanded in-place. One shared CSS section is added to `index.css` for modal form styles.

**Tech Stack:** React 19, TypeScript, TanStack Router, animate-ui Radix dialog (shadcn), existing custom CSS (`index.css`), existing `api/client.ts` fetch wrapper.

---

### Task 1: Install animate-ui dialog and fix the DELETE client bug

**Files:**
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Install the dialog component**

```bash
cd /home/afif/dns-compliance/web
npx shadcn@latest add @animate-ui/components-radix-dialog
```

Expected output: 4 files created under `src/components/animate-ui/` and `src/hooks/` and `src/lib/`.

- [ ] **Step 2: Fix the `api/client.ts` DELETE 204 bug**

The current `request()` always calls `res.json()`. A 204 No Content response has no body, so `res.json()` throws a SyntaxError on delete. Fix it:

Read `web/src/api/client.ts` — it currently has:
```ts
if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
return res.json() as Promise<T>
```

Replace with:
```ts
if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
if (res.status === 204) return undefined as T
return res.json() as Promise<T>
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd /home/afif/dns-compliance/web
npm run build 2>&1 | tail -5
```

Expected: build succeeds with no TypeScript errors.

- [ ] **Step 4: Commit**

```bash
git -C /home/afif/dns-compliance add web/src/api/client.ts web/src/components/animate-ui web/src/hooks web/src/lib/get-strict-context.tsx
git -C /home/afif/dns-compliance commit -m "feat: install animate-ui dialog; fix client 204 handling"
```

---

### Task 2: Add `URLEntry` type and expand API clients

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/urls.ts`
- Modify: `web/src/api/dns-servers.ts`

- [ ] **Step 1: Add `URLEntry` to `types.ts`**

Add after the last existing type:

```ts
export type URLEntry = { id: number; url: string; created_at: string }
```

- [ ] **Step 2: Replace `web/src/api/urls.ts`**

```ts
import { api } from './client'
import type { URLEntry } from './types'

export async function fetchUrls(): Promise<URLEntry[]> {
  const data = await api.get<URLEntry[]>('/urls')
  return Array.isArray(data) ? data : []
}

export async function fetchUrlCount(): Promise<number> {
  return (await fetchUrls()).length
}

export async function createUrl(url: string): Promise<URLEntry> {
  return api.post<URLEntry>('/urls', { url })
}

export async function deleteUrl(id: number): Promise<void> {
  await api.delete<void>(`/urls/${id}`)
}
```

- [ ] **Step 3: Replace `web/src/api/dns-servers.ts`**

```ts
import { api } from './client'
import type { DNSServer } from './types'

export async function fetchDnsServers(): Promise<DNSServer[]> {
  const data = await api.get<DNSServer[]>('/dns-servers')
  return Array.isArray(data) ? data : []
}

export async function fetchDnsServerCount(): Promise<number> {
  return (await fetchDnsServers()).length
}

export async function createDnsServer(data: {
  name: string
  address: string
  protocol: string
}): Promise<DNSServer> {
  return api.post<DNSServer>('/dns-servers', data)
}

export async function deleteDnsServer(id: number): Promise<void> {
  await api.delete<void>(`/dns-servers/${id}`)
}
```

- [ ] **Step 4: Verify type-check**

```bash
cd /home/afif/dns-compliance/web && npm run build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git -C /home/afif/dns-compliance add web/src/api/types.ts web/src/api/urls.ts web/src/api/dns-servers.ts
git -C /home/afif/dns-compliance commit -m "feat: expand URL and DNS server API clients with full CRUD"
```

---

### Task 3: Add CSS for form fields, modal buttons, and protocol badges

**Files:**
- Modify: `web/src/index.css`

- [ ] **Step 1: Add new CSS section to `web/src/index.css`**

Append this block at the end of the file (before the `@theme inline` block — insert it just before line that starts with `@theme inline`):

```css
/* ─── Modal Form Fields ──────────────────────────────────────────────────── */

.form-field {
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
  margin-bottom: var(--sp-4);
}

.form-label {
  font-size: 0.8125rem;
  font-weight: 500;
  color: var(--stone-text);
}

.form-input {
  padding: 6px var(--sp-3);
  font-size: 0.875rem;
  font-family: inherit;
  color: var(--stone-text);
  background: var(--stone-bg);
  border: 1px solid var(--stone-border);
  border-radius: var(--radius-md);
  width: 100%;
  outline: none;
  transition: border-color var(--duration-fast) var(--ease-out);
}

.form-input:focus {
  border-color: var(--stone-muted);
}

.form-input::placeholder {
  color: var(--stone-muted);
}

.form-select {
  padding: 6px var(--sp-3);
  font-size: 0.875rem;
  font-family: inherit;
  color: var(--stone-text);
  background: var(--stone-bg);
  border: 1px solid var(--stone-border);
  border-radius: var(--radius-md);
  width: 100%;
  outline: none;
  cursor: pointer;
  transition: border-color var(--duration-fast) var(--ease-out);
}

.form-select:focus {
  border-color: var(--stone-muted);
}

.form-error {
  font-size: 0.8125rem;
  color: var(--violation-text);
  background: var(--violation-subtle);
  border-radius: var(--radius-md);
  padding: var(--sp-2) var(--sp-3);
  margin-top: calc(var(--sp-2) * -1);
  margin-bottom: var(--sp-4);
}

.btn-ghost {
  display: inline-flex;
  align-items: center;
  padding: 6px 14px;
  font-size: 0.8125rem;
  font-weight: 500;
  color: var(--stone-text);
  background: transparent;
  border: 1px solid var(--stone-border);
  border-radius: var(--radius-md);
  transition: background var(--duration-fast) var(--ease-out),
    border-color var(--duration-fast) var(--ease-out);
  white-space: nowrap;
}

.btn-ghost:hover {
  background: var(--stone-panel);
  border-color: var(--stone-muted);
}

.btn-ghost:focus-visible {
  outline: 2px solid var(--orange);
  outline-offset: 2px;
}

.btn-danger {
  display: inline-flex;
  align-items: center;
  padding: 6px 14px;
  font-size: 0.8125rem;
  font-weight: 500;
  color: #fff;
  background: var(--violation);
  border: none;
  border-radius: var(--radius-md);
  transition: opacity var(--duration-fast) var(--ease-out);
  white-space: nowrap;
}

.btn-danger:hover:not(:disabled) {
  opacity: 0.85;
}

.btn-danger:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn-danger:focus-visible {
  outline: 2px solid var(--violation);
  outline-offset: 2px;
}

.btn-row-delete {
  font-size: 0.8125rem;
  font-weight: 500;
  color: var(--violation-text);
  background: transparent;
  border: none;
  padding: 2px var(--sp-2);
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: color var(--duration-fast) var(--ease-out);
}

.btn-row-delete:hover {
  color: var(--violation);
}

.btn-row-delete:focus-visible {
  outline: 2px solid var(--violation);
  outline-offset: 2px;
}

/* ─── Protocol Badge ─────────────────────────────────────────────────────── */

.protocol-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  border-radius: var(--radius-full);
  font-size: 0.6875rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  background: var(--stone-panel);
  color: var(--stone-muted);
  font-family: var(--font-mono);
}
```

- [ ] **Step 2: Verify TypeScript compiles and no lint errors**

```bash
cd /home/afif/dns-compliance/web && npm run build 2>&1 | tail -5
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git -C /home/afif/dns-compliance add web/src/index.css
git -C /home/afif/dns-compliance commit -m "feat: add CSS for modal form fields, buttons, and protocol badge"
```

---

### Task 4: Create `DeleteConfirmDialog` shared component

**Files:**
- Create: `web/src/components/delete-confirm-dialog.tsx`

- [ ] **Step 1: Create the component**

```tsx
import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'

type Props = {
  open: boolean
  itemLabel: string
  onConfirm: () => Promise<void>
  onCancel: () => void
}

export function DeleteConfirmDialog({ open, itemLabel, onConfirm, onCancel }: Props) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleConfirm = async () => {
    setLoading(true)
    setError(null)
    try {
      await onConfirm()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed')
      setLoading(false)
    }
  }

  const handleCancel = () => {
    if (loading) return
    setError(null)
    onCancel()
  }

  return (
    <Dialog open={open} onOpenChange={open => { if (!open) handleCancel() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 400 }}>
        <DialogHeader>
          <DialogTitle>Delete "{itemLabel}"?</DialogTitle>
          <DialogDescription>
            This will remove it from all future scans.
          </DialogDescription>
        </DialogHeader>
        {error && <p className="form-error">{error}</p>}
        <DialogFooter>
          <button className="btn-ghost" onClick={handleCancel} disabled={loading}>
            Cancel
          </button>
          <button className="btn-danger" onClick={handleConfirm} disabled={loading}>
            {loading ? 'Deleting…' : 'Delete'}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 2: Type-check**

```bash
cd /home/afif/dns-compliance/web && npm run build 2>&1 | tail -5
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git -C /home/afif/dns-compliance add web/src/components/delete-confirm-dialog.tsx
git -C /home/afif/dns-compliance commit -m "feat: add DeleteConfirmDialog shared component"
```

---

### Task 5: Build the URLs page

**Files:**
- Modify: `web/src/routes/urls.tsx`

- [ ] **Step 1: Replace `web/src/routes/urls.tsx` with the full implementation**

```tsx
import { useCallback, useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchUrls, createUrl, deleteUrl } from '../api/urls'
import type { URLEntry } from '../api/types'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'

export const Route = createFileRoute('/urls')({ component: URLsPage })

/* ─── Add URL Dialog ─────────────────────────────────────────────────────── */

function AddUrlDialog({
  open,
  onClose,
  onAdded,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [url, setUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setUrl(''); setError(null) }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!url.trim()) { setError('URL is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createUrl(url.trim())
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add URL')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Add URL</DialogTitle>
          <DialogDescription>
            Add a URL to monitor for DNS compliance.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="add-url-input">URL</label>
            <input
              id="add-url-input"
              className="form-input"
              type="url"
              placeholder="https://example.com"
              value={url}
              onChange={e => setUrl(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add URL'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Skeleton ───────────────────────────────────────────────────────────── */

function SkeletonRows() {
  return (
    <>
      {[200, 160, 240].map((w, i) => (
        <tr key={i} className="skeleton-row">
          <td className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </td>
          <td className="col-status">
            <span className="skeleton" style={{ width: 90, height: 14 }} />
          </td>
          <td className="col-evidence" />
        </tr>
      ))}
    </>
  )
}

/* ─── Empty Icon ─────────────────────────────────────────────────────────── */

function EmptyIcon() {
  return (
    <svg className="empty-icon" width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <rect x="8" y="4" width="24" height="32" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M32 4L40 12V36C40 37.1 39.1 38 38 38H32" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M40 12H32V4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 18H26M14 24H22" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

/* ─── URLs Page ──────────────────────────────────────────────────────────── */

const DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric', month: 'short', year: 'numeric',
})

function URLsPage() {
  const [urls, setUrls] = useState<URLEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<URLEntry | null>(null)

  const load = useCallback(async () => {
    try {
      setError(null)
      setUrls(await fetchUrls())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load URLs')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleDelete = async () => {
    if (!deleteTarget) return
    await deleteUrl(deleteTarget.id)
    setDeleteTarget(null)
    load()
  }

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">URLs</h1>
        <p className="page-subtitle">{!loading && `${urls.length} monitored`}</p>
        <button
          className="btn-primary"
          style={{ marginLeft: 'auto' }}
          onClick={() => setAddOpen(true)}
        >
          + Add URL
        </button>
      </div>

      <div className="results-wrap">
        {error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !loading && urls.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No URLs yet</p>
            <p className="empty-body">Add a URL to start monitoring DNS compliance.</p>
            <button className="btn-primary" onClick={() => setAddOpen(true)}>Add URL</button>
          </div>
        ) : (
          <table className="results-table" aria-label="Monitored URLs">
            <thead>
              <tr>
                <th className="col-domain" scope="col">URL</th>
                <th className="col-status" scope="col">Added</th>
                <th className="col-evidence" scope="col" />
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <SkeletonRows />
              ) : (
                urls.map(u => (
                  <tr key={u.id} className="url-row">
                    <td className="col-domain">
                      <span className="hostname">{u.url}</span>
                    </td>
                    <td className="col-status">
                      <span className="dns-name">
                        {DATE_FMT.format(new Date(u.created_at))}
                      </span>
                    </td>
                    <td className="col-evidence" style={{ textAlign: 'right' }}>
                      <button
                        className="btn-row-delete"
                        onClick={() => setDeleteTarget(u)}
                        aria-label={`Delete ${u.url}`}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </div>

      <AddUrlDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
      />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget?.url ?? ''}
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </>
  )
}
```

- [ ] **Step 2: Type-check**

```bash
cd /home/afif/dns-compliance/web && npm run build 2>&1 | tail -10
```

Expected: success, no TypeScript errors.

- [ ] **Step 3: Commit**

```bash
git -C /home/afif/dns-compliance add web/src/routes/urls.tsx
git -C /home/afif/dns-compliance commit -m "feat: build URLs management page with add and delete"
```

---

### Task 6: Build the DNS Servers page

**Files:**
- Modify: `web/src/routes/dns-servers.tsx`

- [ ] **Step 1: Replace `web/src/routes/dns-servers.tsx` with the full implementation**

```tsx
import { useCallback, useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchDnsServers, createDnsServer, deleteDnsServer } from '../api/dns-servers'
import type { DNSServer } from '../api/types'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'

export const Route = createFileRoute('/dns-servers')({ component: DNSServersPage })

type Protocol = 'udp' | 'dot' | 'doh'

/* ─── Add DNS Server Dialog ──────────────────────────────────────────────── */

function AddDnsServerDialog({
  open,
  onClose,
  onAdded,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [protocol, setProtocol] = useState<Protocol>('udp')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setName(''); setAddress(''); setProtocol('udp'); setError(null) }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!address.trim()) { setError('Address is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createDnsServer({ name: name.trim(), address: address.trim(), protocol })
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add server')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 440 }}>
        <DialogHeader>
          <DialogTitle>Add DNS Server</DialogTitle>
          <DialogDescription>
            Add a resolver to use for compliance checks.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="dns-name-input">
              Name <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>(optional)</span>
            </label>
            <input
              id="dns-name-input"
              className="form-input"
              type="text"
              placeholder="e.g. Cloudflare DoT"
              value={name}
              onChange={e => setName(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="dns-address-input">Address</label>
            <input
              id="dns-address-input"
              className="form-input"
              type="text"
              placeholder="1.1.1.1:853 or https://1.1.1.1/dns-query"
              value={address}
              onChange={e => setAddress(e.target.value)}
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="dns-protocol-select">Protocol</label>
            <select
              id="dns-protocol-select"
              className="form-select"
              value={protocol}
              onChange={e => setProtocol(e.target.value as Protocol)}
              disabled={loading}
            >
              <option value="udp">udp — plain DNS</option>
              <option value="dot">dot — DNS-over-TLS</option>
              <option value="doh">doh — DNS-over-HTTPS</option>
            </select>
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Server'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Skeleton ───────────────────────────────────────────────────────────── */

function SkeletonRows() {
  return (
    <>
      {[120, 160, 100].map((w, i) => (
        <tr key={i} className="skeleton-row">
          <td className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </td>
          <td className="col-ip">
            <span className="skeleton" style={{ width: 130, height: 14 }} />
          </td>
          <td className="col-status">
            <span className="skeleton" style={{ width: 40, height: 20, borderRadius: 99 }} />
          </td>
          <td className="col-evidence" />
        </tr>
      ))}
    </>
  )
}

/* ─── Empty Icon ─────────────────────────────────────────────────────────── */

function EmptyIcon() {
  return (
    <svg className="empty-icon" width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <circle cx="24" cy="24" r="16" stroke="currentColor" strokeWidth="1.5" />
      <path d="M24 8C24 8 16 14 16 24C16 34 24 40 24 40" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M24 8C24 8 32 14 32 24C32 34 24 40 24 40" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M8 24H40" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

/* ─── DNS Servers Page ───────────────────────────────────────────────────── */

function serverLabel(s: DNSServer): string {
  return s.name || s.address
}

function DNSServersPage() {
  const [servers, setServers] = useState<DNSServer[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<DNSServer | null>(null)

  const load = useCallback(async () => {
    try {
      setError(null)
      setServers(await fetchDnsServers())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load DNS servers')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleDelete = async () => {
    if (!deleteTarget) return
    await deleteDnsServer(deleteTarget.id)
    setDeleteTarget(null)
    load()
  }

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">DNS Servers</h1>
        <p className="page-subtitle">{!loading && `${servers.length} configured`}</p>
        <button
          className="btn-primary"
          style={{ marginLeft: 'auto' }}
          onClick={() => setAddOpen(true)}
        >
          + Add Server
        </button>
      </div>

      <div className="results-wrap">
        {error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !loading && servers.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No DNS servers yet</p>
            <p className="empty-body">Add a DNS server to begin scanning.</p>
            <button className="btn-primary" onClick={() => setAddOpen(true)}>Add Server</button>
          </div>
        ) : (
          <table className="results-table" aria-label="DNS servers">
            <thead>
              <tr>
                <th className="col-domain" scope="col">Name</th>
                <th className="col-ip" scope="col">Address</th>
                <th className="col-status" scope="col">Protocol</th>
                <th className="col-evidence" scope="col" />
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <SkeletonRows />
              ) : (
                servers.map(s => (
                  <tr key={s.id} className="url-row">
                    <td className="col-domain">
                      <span className="hostname">{s.name || <span className="dns-name">(unnamed)</span>}</span>
                    </td>
                    <td className="col-ip">
                      <span className="ip-value">{s.address}</span>
                    </td>
                    <td className="col-status">
                      <span className="protocol-badge">{s.protocol}</span>
                    </td>
                    <td className="col-evidence" style={{ textAlign: 'right' }}>
                      <button
                        className="btn-row-delete"
                        onClick={() => setDeleteTarget(s)}
                        aria-label={`Delete ${serverLabel(s)}`}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </div>

      <AddDnsServerDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
      />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget ? serverLabel(deleteTarget) : ''}
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </>
  )
}
```

- [ ] **Step 2: Type-check**

```bash
cd /home/afif/dns-compliance/web && npm run build 2>&1 | tail -10
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git -C /home/afif/dns-compliance add web/src/routes/dns-servers.tsx
git -C /home/afif/dns-compliance commit -m "feat: build DNS Servers management page with add and delete"
```

---

### Task 7: Manual verification

- [ ] **Step 1: Start the dev server**

```bash
cd /home/afif/dns-compliance/web && npm run dev
```

Open http://localhost:5173 in a browser (with the Go backend running).

- [ ] **Step 2: Verify URLs page**

  1. Navigate to `/urls` — table loads with existing URLs (or empty state if none)
  2. Click "+ Add URL" — modal opens with animated entry
  3. Submit empty form — inline error "URL is required" appears, modal stays open
  4. Submit a valid URL — modal closes, row appears in table immediately
  5. Click Delete on a row — confirm modal opens naming the URL
  6. Click Cancel — modal closes, row remains
  7. Click Delete again → Confirm — row removed from table

- [ ] **Step 3: Verify DNS Servers page**

  1. Navigate to `/dns-servers` — table loads (or empty state)
  2. Click "+ Add Server" — modal with 3 fields opens
  3. Submit without address — error "Address is required"
  4. Fill address, leave name blank — submits as unnamed server, "(unnamed)" shown in table
  5. Fill all fields, select `doh` — new row with protocol badge `doh`
  6. Delete a server — confirm modal, then removed

- [ ] **Step 4: Verify dark mode**

  Toggle dark mode — tables, modals, inputs, badges, and buttons all render correctly with no white-on-white text.
