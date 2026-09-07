# Docs Page Export Button Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an "Export" button + scope picker ("Current view" / "All
cases") to `docs.tsx` (`/docs`), downloading the CMOD-format `.xlsx` via the
already-existing `GET /api/case-letters/export` route.

**Architecture:** `docs.tsx`'s toolbar already has a `filter-bar` div
(search input, `Filters`, a "Columns" button) above the letters `DataGrid`.
This plan adds one more control cluster to that row: an `exportScope` piece
of local state, a scope `<Select>`, and an "Export" `<Button>` whose click
handler calls `exportCaseLetters` (from Plan 06) with either the letter IDs
currently visible after filtering ("Current view") or `undefined` ("All
cases"), then hands the result to `downloadBlob` (also Plan 06).

**Critical scope-source detail:** unlike `urls.tsx`'s Cases view (where
"current view" maps to *case* IDs), this page's filters (Type, Workflow
Status, Department, search) are **letter-grained** — filtering to `Type =
Notice` hides Memo subrows while their parent case-group row stays visible.
So "current view" here must be the letter IDs in the already-filtered flat
list (`filtered`, a `CaseLetterEntry[]`), **not** the case IDs in
`caseTreeData` (which groups by case and would silently re-include a
filtered-out letter's siblings). Confirmed from the real file: `filtered`
(line 835) is exactly the flat, already-filtered `CaseLetterEntry[]` that
`caseTreeData` (line 861) groups by `case_id` afterward — `filtered.map(l => l.id)`
is the correct source.

**Tech Stack:** React, TypeScript — no new dependencies. Reuses this file's
already-imported `Button`/`Select`/`SelectTrigger`/`SelectContent`/`SelectItem`
components and its existing `error`/`loading`-state UI conventions.

**Spec:** `docs/superpowers/specs/2026-09-04-compliance-report-export-design.md`
(Part 3).

**Prerequisite:** `2026-09-04-compliance-report-export-06-export-frontend-client.md`
merged — this plan calls `exportCaseLetters` (from `web/src/api/cases.ts`)
and `downloadBlob` (from `web/src/lib/download.ts`), both added there, and
depends on their exact contract:
```ts
export type BlobDownload = { blob: Blob; filename: string | null }
export function exportCaseLetters(letterIds?: number[]): Promise<BlobDownload>
export function downloadBlob(blob: Blob, filename: string): void
```

## Global Constraints

- **No test runner** — see Plan 06's Global Constraints for the same note.
  "Verify" means `cd web && npm run build` (typecheck) plus the manual
  browser check in Task 1's Step 5.
- Every step contains real, copy-pasteable JSX/TS against the actual current
  file — no "wire this up" placeholders.
- Reuse this file's own existing patterns exactly: the `Select`/`SelectTrigger`/
  `SelectContent`/`SelectItem` compound-component shape already used
  elsewhere in this file, and the page-level `error`/`setError` state +
  `<p className="error-message">{error}</p>` rendering already used for
  this page's error state (line ~1069-1072). Do not introduce a toast
  library or a new error-display pattern — this codebase has none
  (confirmed: no `sonner`/`useToast`/`toast(` anywhere under `web/src`).
- Filenames must match Plan 05's backend contract for this route:
  `cmod-blocking-export-<YYYY-MM-DD>.xlsx` (fallback default only — the
  server-supplied `Content-Disposition` filename wins when present, exactly
  as Plan 06's `getBlob`/Plan 07's urls.tsx handler already do for the CRD
  export).

---

### Task 1: Export button + scope select in the toolbar

**Files:**
- Modify: `web/src/routes/docs.tsx`

**Current relevant code** (read the live file yourself to confirm nothing's
drifted, but as of this plan, these are the exact lines):

Line 15, the existing import from `@/api/cases`:
```ts
import { fetchAllCaseLetters, createCase, addCaseLetter, addUrlToCase, updateCaseLetter, deleteCaseLetter } from '@/api/cases'
```

`filtered` (line 835, inside the page component) — the flat, already-filtered
letter list this plan's "current view" scope reads from:
```ts
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    const matchesQuery = (l: CaseLetterEntry) =>
      !query ||
      (l.reference_number_external ?? '').toLowerCase().includes(query) ||
      (l.reference_number_internal ?? '').toLowerCase().includes(query) ||
      (l.recipient ?? '').toLowerCase().includes(query) ||
      (l.subject ?? '').toLowerCase().includes(query) ||
      (l.requestor ?? '').toLowerCase().includes(query) ||
      (l.remarks ?? '').toLowerCase().includes(query) ||
      (l.urls ?? []).some(u => u.toLowerCase().includes(query))
    return letters.filter(l =>
      matchesQuery(l) &&
      (!typeFilter || l.type === typeFilter) &&
      (!deptFilter || l.department_name === deptFilter) &&
      (!workflowFilter || l.workflow_status === workflowFilter)
    )
  }, [letters, search, typeFilter, deptFilter, workflowFilter])
```
`CaseLetterEntry.id` (from `web/src/api/types.ts`'s `CaseLetter` base type)
is a `number` — `filtered.map(l => l.id)` gives the letter-ID scope.

The toolbar (`filter-bar` div, around line 1082-1095):
```tsx
          <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full">
            <Input
              type="search"
              placeholder="Search documents..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="max-w-64"
              aria-label="Search documents"
            />
            <Filters filters={filters} fields={filterFields} onChange={setFilters} />
            <div style={{ marginLeft: 'auto' }}>
              <DataGridColumnVisibility table={table} trigger={<Button variant="outline">Columns</Button>} />
            </div>
          </div>
```

**Interfaces (consumes Plan 06):**
```ts
import { exportCaseLetters } from '@/api/cases' // add to the existing import on line 15
import { downloadBlob } from '@/lib/download'
```

- [ ] **Step 1: Extend the existing `@/api/cases` import (line 15).**
```ts
import { fetchAllCaseLetters, createCase, addCaseLetter, addUrlToCase, updateCaseLetter, deleteCaseLetter, exportCaseLetters } from '@/api/cases'
```
Add a new import line right after it:
```ts
import { downloadBlob } from '@/lib/download'
```

- [ ] **Step 2: Add export state inside the page component, near its other
  `useState` declarations.**
```ts
  const [exportScope, setExportScope] = useState<'current' | 'all'>('current')
  const [exporting, setExporting] = useState(false)
```

- [ ] **Step 3: Add the `handleExportLetters` handler, anywhere among the
  component's other handler functions (it needs to be defined after
  `filtered` and after the component's `error`/`setError` state, both of
  which already exist earlier in the same component):**
```ts
  const handleExportLetters = useCallback(async () => {
    setExporting(true)
    setError(null)
    try {
      const ids = exportScope === 'current' ? filtered.map(l => l.id) : undefined
      const { blob, filename } = await exportCaseLetters(ids)
      downloadBlob(blob, filename ?? `cmod-blocking-export-${new Date().toISOString().slice(0, 10)}.xlsx`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to export documents')
    } finally {
      setExporting(false)
    }
  }, [exportScope, filtered])
```
`useCallback` is already imported at the top of this file (line 1) — no new
import needed for it.

- [ ] **Step 4: Add the Export controls to the toolbar**, in the
  `filter-bar` div shown above, right before the existing
  `<div style={{ marginLeft: 'auto' }}>` (Columns button) block:
```tsx
            <Filters filters={filters} fields={filterFields} onChange={setFilters} />
            <Select value={exportScope} onValueChange={v => setExportScope(v as 'current' | 'all')}>
              <SelectTrigger aria-label="Export scope" className="w-40" />
              <SelectContent>
                <SelectItem index={0} value="current">Current view</SelectItem>
                <SelectItem index={1} value="all">All cases</SelectItem>
              </SelectContent>
            </Select>
            <Button variant="outline" onClick={handleExportLetters} disabled={exporting}>
              {exporting ? 'Exporting…' : 'Export'}
            </Button>
            <div style={{ marginLeft: 'auto' }}>
              <DataGridColumnVisibility table={table} trigger={<Button variant="outline">Columns</Button>} />
            </div>
```

- [ ] **Step 5: Manual verification (no unit test harness — see Global
  Constraints).**
  1. Run: `cd web && npm run build` — expect success (typecheck).
  2. Start the full stack (`./dev.sh`, or the Go server + `npm run dev`
     separately) — requires Plan 05's backend routes merged.
  3. Log in, navigate to `/docs`.
  4. With "Current view" selected and at least one document visible, click
     Export — a `.xlsx` should download named
     `cmod-blocking-export-<today's date>.xlsx`. Open it and spot check the
     header row has the 17 CMOD columns from Plan 04's `FlattenCMODRows` in
     order, with `Link` varying across rows that share the same letter.
  5. Apply the Type filter (e.g. `Type = Notice`) so some Memo subrows
     disappear from `caseTreeData` while their parent case-group row stays
     visible, then click Export with "Current view" — confirm (via the
     browser Network tab) the `letter_ids` query param lists only the
     Notice-type letters actually still visible, not their filtered-out
     Memo siblings' ids nor every letter under those cases' parent rows.
  6. Switch to "All cases", click Export — confirm no `letter_ids` param.
  7. Force a request failure (devtools network block) and click Export —
     confirm the page's existing `error-message` banner shows a message
     rather than an uncaught exception.

- [ ] **Step 6: Commit**
```bash
git add web/src/routes/docs.tsx
git commit -m "docs: add Export button + scope picker"
```

## Verification for this plan as a whole

```bash
cd web && npm run build
```
Must pass. Step 5's manual browser verification (full stack running, Plan
05 merged) is the actual acceptance check, particularly the letter-grained
(not case-grained) filtering behavior called out above — that's the one
detail most likely to be gotten wrong by an implementer who pattern-matches
too closely off Plan 07's case-ID version instead of reading this plan's
"Critical scope-source detail" section.
