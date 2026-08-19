# Cases Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the now-dead `reference_number`/`requesting_dept_id`
editable fields from the watchlist page (`web/src/routes/urls.tsx`),
replace them with the server-derived read-only `current_reference_number`/
`requesting_departments` columns, and add a minimal case-history view/add
UI backed by the three endpoints from
`2026-08-19-cases-schema-03-api-handlers.md`.

**Prerequisite:** `2026-08-19-cases-schema-03-api-handlers.md` must already
be merged to `main` — this plan assumes `GET/POST /api/cases/*url` and
`POST /api/cases/{case_id}/letters` exist, and that `GET /api/urls`
(`URLEntry`) returns `current_reference_number`/`requesting_departments`
instead of `reference_number`/`requesting_dept_id`/`requesting_dept_name`.
Start this worktree from `main` after that merge.

**Architecture:** Trim `AddUrlDialog` and the per-row edit dialog in
`urls.tsx` to drop the two removed fields; add two new grid columns backed
by the derived data (no client-side change needed there beyond reading the
new field names); add one new small dialog component,
`CaseHistoryDialog`, opened from a new per-row action, that lists cases
(read-only) and offers a small form to open a new case / add a letter to
an existing one.

**Tech Stack:** React 19, TypeScript, TanStack Table (existing column
defs), the project's existing `Dialog`/`Select`/form-field components
(reuse, don't reinvent).

**Spec:** `internal/server/CLAUDE.md`'s `PATCH /api/urls/{id}` bullet
(now stale after Task 03 — this plan's Task 3 below fixes the JSDoc-style
comment in `urls.tsx` itself, not that file) and the three new case routes
from Task 03's plan.

## Global Constraints

- All field edits still commit through the existing optimistic-update
  pattern (`commitField`/blur-commit for text, immediate-commit for
  selects) — don't introduce a different update style for the new
  read-only columns (they have no edit affordance at all, so this mostly
  means: don't wire `onChange` handlers to fields that no longer exist).
- Reuse existing `Dialog`/`DialogFooter`/`Select`/`form-field` primitives
  for `CaseHistoryDialog` — no new dialog framework.
- `npm run build` (which runs `tsc --noEmit`) must pass after every task —
  this codebase's type-check is part of the build, not a separate step.

---

### Task 1: Update `URLEntry`/case-field types and `web/src/api/urls.ts`

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/urls.ts`
- Create: `web/src/api/cases.ts`

**Interfaces (produced, relied on by Tasks 2-4):**
```ts
// web/src/api/types.ts — replace the URLEntry fields
export interface URLEntry {
  // ...existing fields unchanged (id, url, enabled, due_date, agency_id,
  // agency_name, status, requested_at, created_at)...
  current_reference_number?: string
  requesting_departments?: string[]
  // reference_number, requesting_dept_id, requesting_dept_name: REMOVED
}

export interface Case {
  id: number
  department_id: number
  created_at: string
  phase: string
  letters: CaseLetter[]
}

export interface CaseLetter {
  id: number
  case_id: number
  type: string
  reference_number?: string
  workflow_status?: string
  letter_date?: string
  submitted_at?: string
  subject?: string
  oic_user_id?: number
  requestor?: string
  remarks?: string
  created_at: string
}
```

```ts
// web/src/api/cases.ts — new file, mirror the fetch style of
// web/src/api/urls.ts (check that file's imports/error-handling first —
// likely a shared `client.ts` helper, e.g. `api.get`/`api.post`)
import { api } from './client'
import type { Case, CaseLetter } from './types'

export async function listCases(url: string): Promise<Case[]> {
  const data = await api.get(`/cases/${encodeURIComponent(url)}`)
  return Array.isArray(data) ? data : [] // Go nil slice -> JSON null, see web/CLAUDE.md
}

export async function createCase(url: string, phase: string): Promise<Case> {
  return api.post(`/cases/${encodeURIComponent(url)}`, { phase })
}

export async function addCaseLetter(
  caseId: number,
  fields: Partial<Omit<CaseLetter, 'id' | 'case_id' | 'created_at'>>,
): Promise<CaseLetter> {
  return api.post(`/cases/${caseId}/letters`, fields)
}
```
(Check `web/src/api/urls.ts`'s actual `api.get`/`api.post` call shape
before writing this — copy its exact pattern for URL-encoding the `*url`
wildcard segment; `AttachOffence`'s frontend caller, if one already exists
for `POST /api/legal/offences/*url`, is the closest precedent to copy
verbatim.)

- [ ] **Step 1:** Update `URLEntry` in `web/src/api/types.ts` per above.
- [ ] **Step 2:** Add `Case`/`CaseLetter` types to `web/src/api/types.ts`.
- [ ] **Step 3:** In `web/src/api/urls.ts`, remove `reference_number` from
  whatever payload type wraps `setUrlFields`'s body
  (`web/src/api/urls.ts:35`) and the corresponding
  `if (fields.reference_number !== undefined) ...` line
  (`web/src/api/urls.ts:50`). Leave `requesting_dept_id` alone here if it
  isn't already present in this file (grep first — Task 03 removed it
  server-side from `ToggleURL`'s body, but if `urls.ts` never sent it,
  nothing to change).
- [ ] **Step 4:** Create `web/src/api/cases.ts` per above.
- [ ] **Step 5: `npm run build`** (from `web/`) — expect new type errors in
  `urls.tsx` referencing the now-removed fields; that's expected, fixed in
  Task 2. Confirm the errors are *only* in `urls.tsx` and nowhere else.
- [ ] **Step 6: Commit**
```bash
git add web/src/api/types.ts web/src/api/urls.ts web/src/api/cases.ts
git commit -m "web: update URLEntry for derived case fields, add cases API client"
```

---

### Task 2: Strip `reference_number`/`requesting_dept_id` from `urls.tsx`'s dialogs and grid

**Files:**
- Modify: `web/src/routes/urls.tsx`

- [ ] **Step 1: `AddUrlDialog`** — remove:
  - `referenceNumber`/`setReferenceNumber` state (`urls.tsx:354`)
  - `requestingDeptId`/`setRequestingDeptId` state (`urls.tsx:355`)
  - the `useEffect` resetting `requestingDeptId` on open
    (`urls.tsx:362-367`)
  - `referenceNumber`/`requestingDeptId` resets inside `reset()`
    (`urls.tsx:371`, the relevant part of that line only — keep
    `setValue('')`, `setOffences([])`, `setError(null)`, `setAgencyId('')`,
    `setStatus('requested')`, `setDueDurationMinutes('1440')`)
  - the two `caseFields` lines building `reference_number`/
    `requesting_dept_id` (`urls.tsx:386-387`)
  - the "Requesting Dept." `<Select>` field block (`urls.tsx:458-469`)
  - the "Reference No." `<input>` field block (`urls.tsx:498-509`)
- [ ] **Step 2: Per-row edit dialog** — remove:
  - the "Requesting Dept." `<Select>` field block (`urls.tsx:678-692`) and
    its `onRequestingDeptChange` handler (grep its definition — likely
    alongside `onAgencyChange`/`onStatusChange`; delete only the
    requesting-dept one, keep agency/status)
  - the "Reference No." `<input>` field block (`urls.tsx:724-736`) and its
    three handlers `handleRefFocus`/`handleRefChange`/`handleRefBlur`
    (`urls.tsx:922-939`) plus the `refOriginalRef` ref they use (grep its
    declaration — delete it only if nothing else uses it)
- [ ] **Step 3: Filters** — the `requesting_dept` filter field
  (`urls.tsx:950`) currently compares
  `String(u.requesting_dept_id ?? '') === deptFilter`
  (`urls.tsx:967`). Since `requesting_departments` is now a `string[]` of
  department *names* (not one id), rewrite the filter predicate to resolve
  the filter's department id to a name first, then check membership:
```ts
  const deptFilterName = deptFilter ? departments.find(d => String(d.id) === deptFilter)?.name : undefined
  // ...inside the `filtered` useMemo predicate, replace the old line with:
  (!deptFilterName || (u.requesting_departments ?? []).includes(deptFilterName)) &&
```
  Also update the search predicate (`urls.tsx:965`, currently matches
  against `u.reference_number`) to match against
  `u.current_reference_number` instead.
- [ ] **Step 4: Grid columns** — replace the `reference_number` column
  (`urls.tsx:1022-1029`) with:
```ts
  {
    id: 'current_reference_number',
    accessorFn: u => u.current_reference_number ?? '',
    size: 130,
    header: 'Reference No.',
    meta: { headerTitle: 'Reference No.', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
    cell: ({ row }) => <span className="dns-name">{row.original.current_reference_number || '—'}</span>,
  },
```
  and replace the `requesting_dept` column (`urls.tsx:1030-1037`) with:
```ts
  {
    id: 'requesting_departments',
    accessorFn: u => (u.requesting_departments ?? []).join(', '),
    size: 160,
    header: 'Requesting Dept.',
    meta: { headerTitle: 'Requesting Dept.', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
    cell: ({ row }) => <span className="dns-name">{(row.original.requesting_departments ?? []).join(', ') || '—'}</span>,
  },
```
- [ ] **Step 5: `npm run build`** — should now be clean of the errors
  introduced by Task 1's type change (no remaining references to
  `reference_number`/`requesting_dept_id`/`requesting_dept_name` anywhere
  in `urls.tsx`; grep to confirm zero hits).
- [ ] **Step 6: Commit**
```bash
git add web/src/routes/urls.tsx
git commit -m "web: replace editable reference/requesting-dept fields with derived columns"
```

---

### Task 3: `CaseHistoryDialog` — view + open a case, per row

**Files:**
- Create: `web/src/components/case-history-dialog.tsx`
- Modify: `web/src/routes/urls.tsx`

**Scope, deliberately minimal:** a read-only list of a domain's cases
(department, phase, letters — reference number, type, letter date) plus
one small form to open a new case (department is implicit, server-side,
per Task 03) with a phase select and, inline, the fields for its first
letter. No letter-editing, no per-letter delete — those aren't needed for
this migration to ship (departments were manually editing one
`reference_number` string before; a read history + add-case/add-letter
flow is already strictly more capable). Extend later if departments ask
for edit/delete.

**Component shape** (mirror this codebase's existing dialog components —
check `EditOffencesDialog` in `urls.tsx` for the closest structural
precedent: a per-row dialog fed the row's data plus shared lookup lists):
```tsx
// web/src/components/case-history-dialog.tsx
import { useEffect, useState } from 'react'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog' // match this project's actual dialog import path — check urls.tsx's existing imports
import { listCases, createCase, addCaseLetter } from '@/api/cases'
import type { Case } from '@/api/types'

const PHASE_OPTIONS = [
  { value: 'requested', label: 'Requested' },
  { value: 'uplift', label: 'Uplift' },
  { value: 'suspended', label: 'Suspended' },
] // mirror STATUS_OPTIONS in urls.tsx exactly (same vocabulary) — import it instead of redefining if it's exported

export function CaseHistoryDialog({ open, onClose, url }: { open: boolean; onClose: () => void; url: string }) {
  const [cases, setCases] = useState<Case[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [newPhase, setNewPhase] = useState('requested')

  const load = async () => {
    setLoading(true)
    try {
      setCases(await listCases(url))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load cases')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { if (open) load() }, [open, url])

  const handleAddCase = async () => {
    try {
      await createCase(url, newPhase)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create case')
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && onClose()}>
      <DialogContent>
        <DialogHeader><DialogTitle>Case History — {url}</DialogTitle></DialogHeader>
        {loading && <p>Loading…</p>}
        {error && <p className="form-error">{error}</p>}
        {!loading && cases.length === 0 && <p>No cases yet.</p>}
        <ul className="case-history-list">
          {cases.map(c => (
            <li key={c.id}>
              <strong>Case #{c.id}</strong> — {c.phase}
              <ul>
                {c.letters.map(l => (
                  <li key={l.id}>{l.type}{l.reference_number ? ` — ${l.reference_number}` : ''}</li>
                ))}
              </ul>
            </li>
          ))}
        </ul>
        <DialogFooter>
          {/* phase select + "Open New Case" button wired to handleAddCase — follow this project's existing form-field markup from AddUrlDialog's status Select for exact structure */}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
```
(This is a starting skeleton, not a placeholder — fill in the phase
`<Select>` and button using `urls.tsx`'s existing `Select`/`form-field`
markup verbatim, e.g. copy the structure of the Status select at
`urls.tsx:696-705`. Adding a first letter inline is a stretch goal for this
task; if time-boxed, ship without it — `POST /api/cases/{case_id}/letters`
is still reachable later by extending this same dialog, nothing about
skipping it now blocks anything else in this migration.)

- [ ] **Step 1:** Create `web/src/components/case-history-dialog.tsx` per
  above, matching this project's actual `Dialog`/`Select` import paths
  (check `urls.tsx`'s import block first).
- [ ] **Step 2:** In `urls.tsx`, add a "Cases" action to each row (e.g.
  alongside the existing edit/delete row actions — grep for where the
  per-row action buttons are defined, likely near the `columns` `id:
  'actions'` column or a dedicated actions cell) that opens
  `CaseHistoryDialog` for that row's `url`.
- [ ] **Step 3: `npm run build`**
  Expected: clean build, no type errors.
- [ ] **Step 4: Manual verification** — per the repo's UI-change policy
  (`CLAUDE.md`'s "For UI or frontend changes" guidance), start the dev
  server (`./dev.sh` or `npm run dev` + `go run ./cmd/server/...` per
  `web/CLAUDE.md`) and confirm in a browser: the watchlist grid shows the
  new Reference No./Requesting Dept. columns as read-only text, the old
  editable inputs for those two fields are gone from both dialogs, and
  the new "Cases" action opens `CaseHistoryDialog` showing an empty state
  for a fresh domain, then lets you open a case and see it appear in the
  list.
- [ ] **Step 5: Commit**
```bash
git add web/src/components/case-history-dialog.tsx web/src/routes/urls.tsx
git commit -m "web: add CaseHistoryDialog, wire a per-row Cases action"
```

## Verification for this plan as a whole

```bash
cd web && npm run build && npm run lint
```
Both must pass. Then do the manual browser check in Task 3/Step 4 — this
migration removes a previously-editable field, so confirming the
replacement reads correctly (not just that it compiles) matters more than
usual here.
