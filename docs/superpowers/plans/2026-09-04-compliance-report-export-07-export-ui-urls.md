# Cases-View Export Button Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an "Export" button + scope picker ("Current view" / "All
cases") to `urls.tsx`'s Cases view (`/urls?view=cases`), downloading the
CRD-format `.xlsx` via the already-existing `GET /api/case-summaries/export`
route.

**Architecture:** `urls.tsx`'s Cases-view toolbar already has a `filter-bar`
div (search input, `Filters`, a "Columns" button) right above the
`casesTable` `DataGrid`. This plan adds one more control cluster to that
same row: an `exportScope` piece of local state, a scope `<Select>`, and an
"Export" `<Button>` whose click handler calls `exportCaseSummaries` (from
Plan 06) with either the case IDs already visible in `caseTreeData`
("Current view") or `undefined` ("All cases"), then hands the result to
`downloadBlob` (also Plan 06).

**Tech Stack:** React, TypeScript — no new dependencies. Reuses this file's
already-imported `Button`/`Select`/`SelectTrigger`/`SelectContent`/`SelectItem`
components and its existing `error`/`loading`-state UI conventions.

**Spec:** `docs/superpowers/specs/2026-09-04-compliance-report-export-design.md`
(Part 3).

**Prerequisite:** `2026-09-04-compliance-report-export-06-export-frontend-client.md`
merged — this plan calls `exportCaseSummaries` (from `web/src/api/cases.ts`)
and `downloadBlob` (from `web/src/lib/download.ts`), both added there, and
depends on their exact contract:
```ts
export type BlobDownload = { blob: Blob; filename: string | null }
export function exportCaseSummaries(caseIds?: number[]): Promise<BlobDownload>
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
  elsewhere in this file (e.g. around line 232, the offence picker's
  Instrument `<Select>` — note `SelectItem` takes a required `index` prop),
  and the page-level `error`/`setError` state + `<p className="error-message">{error}</p>`
  rendering already used for both the Domain view and Cases view error
  states (lines ~1645-1648 and ~1697-1700). Do not introduce a toast library
  or a new error-display pattern — this codebase has none (confirmed: no
  `sonner`/`useToast`/`toast(` anywhere under `web/src`).
- This plan only touches the **Cases view** toolbar (`view === 'cases'`
  branch), not the Domain-view toolbar — the CRD export is
  one-row-per-(domain,offence), matching the Cases view's shape, per the
  design spec.

---

### Task 1: Export button + scope select in the Cases-view toolbar

**Files:**
- Modify: `web/src/routes/urls.tsx`

**Current relevant code** (read the live file yourself to confirm nothing's
drifted since this was written — but as of this plan, these are the exact
lines):

Line 24, the existing import from `../api/cases`:
```ts
import { createCase, addCaseLetter, addUrlToCase, updateCase, updateCaseURLStatus, updateCaseLetter, fetchCaseSummaries } from '../api/cases'
```

`CaseRow`/`caseTreeData` (lines 1055, 1437, inside `URLsPage`):
```ts
type CaseRow = { kind: 'case'; summary: CaseSummary; subRows: DomainSubRow[] }
...
const caseTreeData = useMemo<CaseRow[]>(() => filteredCases.map(summary => ({
  kind: 'case', summary, subRows: /* ... */
})), [filteredCases])
```
`caseTreeData[i].summary.id` is a case's `CaseSummary.id` — a `number`.
"Current view" scope is exactly `caseTreeData.map(r => r.summary.id)`, since
`caseTreeData` is already derived from `filteredCases` (the search/filter
result), matching the design spec's "collects the case IDs already visible
after client-side search/filter."

The Cases-view toolbar (inside the `view === 'cases'` ternary branch, the
`filter-bar` div around line 1710-1724):
```tsx
          <div className="flex flex-col items-stretch w-full gap-4 mt-4">
            <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full">
              <Input
                type="search"
                placeholder="Search case ref. or domain..."
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="max-w-64"
                aria-label="Search cases"
              />
              <Filters filters={filters} fields={filterFields} onChange={setFilters} />
              <div style={{ marginLeft: 'auto' }}>
                <DataGridColumnVisibility table={casesTable} trigger={<Button variant="outline">Columns</Button>} />
              </div>
            </div>
```

An existing `<Select>` usage elsewhere in this same file to copy the exact
compound-component shape from (around line 232):
```tsx
        <Select
          value={String(instrumentId)}
          onValueChange={v => setInstrumentId(v === '' ? '' : Number(v))}
        >
          <SelectTrigger aria-labelledby="offence-picker-label" placeholder="Instrument…" className="w-full" />
          <SelectContent>
            <SelectItem index={0} value="">No instrument</SelectItem>
            {instruments.map((inst, i) => (
              <SelectItem key={inst.id} index={i + 1} value={String(inst.id)}>{inst.short_title}</SelectItem>
            ))}
          </SelectContent>
        </Select>
```

**Interfaces (consumes Plan 06):**
```ts
import { exportCaseSummaries } from '../api/cases' // add to the existing import on line 24
import { downloadBlob } from '@/lib/download'
```

- [ ] **Step 1: Extend the existing `../api/cases` import (line 24).**
```ts
import { createCase, addCaseLetter, addUrlToCase, updateCase, updateCaseURLStatus, updateCaseLetter, fetchCaseSummaries, exportCaseSummaries } from '../api/cases'
```
Add a new import line right after it:
```ts
import { downloadBlob } from '@/lib/download'
```

- [ ] **Step 2: Add export state inside `URLsPage`, near the other `useState`
  declarations (e.g. right after `const [editingCase, setEditingCase] = useState<CaseSummary | null>(null)` around line 1064).**
```ts
  const [exportScope, setExportScope] = useState<'current' | 'all'>('current')
  const [exporting, setExporting] = useState(false)
```

- [ ] **Step 3: Add the `handleExportCases` handler inside `URLsPage`,
  anywhere among the component's other handler functions (e.g. near where
  `caseTreeData` is defined, since it reads that value):**
```ts
  const handleExportCases = useCallback(async () => {
    setExporting(true)
    setError(null)
    try {
      const ids = exportScope === 'current' ? caseTreeData.map(r => r.summary.id) : undefined
      const { blob, filename } = await exportCaseSummaries(ids)
      downloadBlob(blob, filename ?? `blocking-list-export-${new Date().toISOString().slice(0, 10)}.xlsx`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to export cases')
    } finally {
      setExporting(false)
    }
  }, [exportScope, caseTreeData])
```
`useCallback` is already imported at the top of this file (line 1) — no new
import needed for it.

- [ ] **Step 4: Add the Export controls to the Cases-view toolbar**, in the
  `filter-bar` div shown above, right before the existing `<div style={{ marginLeft: 'auto' }}>` (Columns button) block — so the row reads:
  search input → Filters → Export scope Select → Export Button → (auto-margin) Columns button:
```tsx
              <Filters filters={filters} fields={filterFields} onChange={setFilters} />
              <Select value={exportScope} onValueChange={v => setExportScope(v as 'current' | 'all')}>
                <SelectTrigger aria-label="Export scope" className="w-40" />
                <SelectContent>
                  <SelectItem index={0} value="current">Current view</SelectItem>
                  <SelectItem index={1} value="all">All cases</SelectItem>
                </SelectContent>
              </Select>
              <Button variant="outline" onClick={handleExportCases} disabled={exporting}>
                {exporting ? 'Exporting…' : 'Export'}
              </Button>
              <div style={{ marginLeft: 'auto' }}>
                <DataGridColumnVisibility table={casesTable} trigger={<Button variant="outline">Columns</Button>} />
              </div>
```

- [ ] **Step 5: Manual verification (no unit test harness — see Global
  Constraints).**
  1. Run: `cd web && npm run build` — expect success (typecheck).
  2. Start the full stack per the repo's `./dev.sh` (or `go run ./cmd/server/ ...`
     alongside `cd web && npm run dev`, per `web/CLAUDE.md`'s Commands
     section) — requires Plan 05's backend routes to already be merged for
     this step to actually succeed, not just typecheck.
  3. Log in, navigate to `/urls?view=cases`.
  4. With "Current view" selected and at least one case visible, click
     Export — a `.xlsx` file should download named
     `blocking-list-export-<today's date>.xlsx` (confirm the browser's
     downloads list or save dialog). Open it (any spreadsheet app) and spot
     check the header row has the 17 CRD columns from Plan 03's
     `FlattenCRDRows` in order.
  5. Type something into the search box that filters the case list down to
     a subset, click Export again with "Current view" — confirm (via the
     browser's Network tab) the request URL's `case_ids` query param only
     lists the filtered subset's ids.
  6. Switch to "All cases", click Export — confirm the request has no
     `case_ids` param at all.
  7. Stop the server mid-request or otherwise force a failure (e.g. use
     devtools to block the request) and click Export — confirm the page's
     existing error banner (`error-message`) shows a message instead of a
     silent failure or an uncaught exception in the console.

- [ ] **Step 6: Commit**
```bash
git add web/src/routes/urls.tsx
git commit -m "urls: add Export button + scope picker to Cases view"
```

## Verification for this plan as a whole

```bash
cd web && npm run build
```
Must pass. Step 5 above (manual browser verification against a running
full stack, backend Plan 05 merged) is the actual acceptance check — a
clean typecheck alone doesn't confirm the download or scope-filtering
behaves correctly.
