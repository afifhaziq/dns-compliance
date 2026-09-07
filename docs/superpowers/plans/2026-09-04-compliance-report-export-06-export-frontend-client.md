# Export Frontend Client Plumbing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the client-side plumbing — a blob-fetching API helper, two
export-triggering functions, and a shared file-download helper — that Plan
07 (`urls.tsx` Cases-view export button) and Plan 08 (`docs.tsx` export
button) both build on. No UI in this plan; it's pure `web/src/api`/`web/src/lib`
code.

**Architecture:** `web/src/api/client.ts`'s existing `api` object only ever
returns parsed JSON (`res.json()`). This plan adds a sibling `getBlob`
method that fetches and returns the raw response body as a `Blob` plus the
server-supplied filename (from `Content-Disposition`), without touching the
existing `request<T>`/`api.get/post/patch/put/delete` functions at all. Two
new functions in `web/src/api/cases.ts` call it with the two export routes'
query-string shapes. A new `web/src/lib/download.ts` triggers the browser's
save dialog from a `Blob` — the standard `URL.createObjectURL` → temporary
`<a download>` click → `URL.revokeObjectURL` dance, since there's no
existing helper for this anywhere in the codebase (checked: nothing in
`web/src/lib/*.ts` does this today) and no library dependency does it either
(this is 6 lines of native browser API, not something to pull in a package
for).

**Tech Stack:** TypeScript, native `fetch`/`Blob`/`URL.createObjectURL` — no
new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-04-compliance-report-export-design.md`
(Part 3, "Frontend" section).

**Prerequisite:** `2026-09-04-compliance-report-export-05-export-routes.md`
merged — the backend routes `GET /api/case-summaries/export` and
`GET /api/case-letters/export` must exist for end-to-end manual testing
(Task 3's verification step). The code in this plan compiles and typechecks
fine even if that backend work hasn't landed yet, since nothing here calls
those routes except at runtime — but you can't actually verify a download
works without them.

## Global Constraints

- **No test runner exists in this frontend.** `web/package.json`'s `scripts`
  are `dev` (`vite`), `build` (`tsc -b && vite build`), `lint` (`eslint .`),
  `preview` (`vite preview`) — there is no `test` script, no Jest, no
  Vitest. "Verify" in this plan means: `cd web && npm run build` passes
  (this runs `tsc -b`, a full typecheck, before the Vite build), plus a
  manual sanity check described in Task 3. Do not add a test framework to
  make this plan superficially resemble the Go plans' TDD shape — that
  would be a new, unrequested dependency for a handful of typed functions
  with no branching logic worth a unit test.
- Every step below contains the exact code to write — no "add appropriate
  error handling" placeholders.
- Do not modify `request<T>` or any of the existing `api.get/post/patch/put/delete`
  methods in `client.ts` — `getBlob` is additive only.

---

### Task 1: `getBlob` in `web/src/api/client.ts`

**Files:**
- Modify: `web/src/api/client.ts` (current full content below — 31 lines)

**Current file:**
```ts
const BASE = '/api'

type RequestOptions = {
  skipAuthRedirect?: boolean
}

async function request<T>(path: string, init?: RequestInit, opts?: RequestOptions): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'fetch', ...init?.headers },
    credentials: 'same-origin',
    ...init,
  })
  if (res.status === 401 && !opts?.skipAuthRedirect && window.location.pathname !== '/login') {
    window.location.assign('/login')
    throw new Error('401 Unauthorized')
  }
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>(path, undefined, opts),
  post: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>(path, { method: 'POST', body: JSON.stringify(body) }, opts),
  patch: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>(path, { method: 'PATCH', body: JSON.stringify(body) }, opts),
  put: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>(path, { method: 'PUT', body: JSON.stringify(body) }, opts),
  delete: <T>(path: string, opts?: RequestOptions) => request<T>(path, { method: 'DELETE' }, opts),
}
```

**Interfaces (produced, relied on by Task 2 in this plan and by Plan 07/Plan 08):**
```ts
export type BlobDownload = { blob: Blob; filename: string | null }

// api.getBlob(path) — GET request expecting a binary body (e.g. an .xlsx
// export), not JSON. Returns the raw Blob plus whatever filename the server
// suggested via Content-Disposition (null if the header is missing or has
// no filename= parameter) — the caller decides the fallback filename.
getBlob: (path: string, opts?: RequestOptions) => Promise<BlobDownload>
```
This exact `BlobDownload` shape (`{ blob: Blob; filename: string | null }`)
is what Task 2's `exportCaseSummaries`/`exportCaseLetters` return, and what
Plan 07/Plan 08's click handlers destructure (`const { blob, filename } = await exportCaseSummaries(ids)`).
Do not change this shape without updating those two plans to match.

- [ ] **Step 1: Add `filenameFromContentDisposition` and `requestBlob`, and extend the exported `api` object.**

Add this above the `export const api = {` line:
```ts
export type BlobDownload = { blob: Blob; filename: string | null }

// Content-Disposition looks like `attachment; filename="blocking-list-export-2026-09-04.xlsx"`
// (see internal/server's export handlers) — quotes are optional per RFC 6266,
// so the regex tolerates both `filename=foo.xlsx` and `filename="foo.xlsx"`.
function filenameFromContentDisposition(header: string | null): string | null {
  if (!header) return null
  const match = /filename="?([^";]+)"?/i.exec(header)
  return match?.[1]?.trim() ?? null
}

async function requestBlob(path: string, opts?: RequestOptions): Promise<BlobDownload> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'X-Requested-With': 'fetch' },
    credentials: 'same-origin',
  })
  if (res.status === 401 && !opts?.skipAuthRedirect && window.location.pathname !== '/login') {
    window.location.assign('/login')
    throw new Error('401 Unauthorized')
  }
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  const blob = await res.blob()
  const filename = filenameFromContentDisposition(res.headers.get('Content-Disposition'))
  return { blob, filename }
}
```
Then add `getBlob` as a new key in the existing `export const api = { ... }` object literal (alongside `get`/`post`/`patch`/`put`/`delete`, don't create a second export):
```ts
  getBlob: (path: string, opts?: RequestOptions) => requestBlob(path, opts),
```

- [ ] **Step 2: Typecheck.**
  Run: `cd web && npm run build`
  Expected: succeeds with no TypeScript errors. (There's no dedicated
  typecheck-only script — `npm run build` runs `tsc -b` first and fails
  fast on a type error before ever invoking `vite build`.)

- [ ] **Step 3: Commit**
```bash
git add web/src/api/client.ts
git commit -m "web: add api.getBlob for binary/xlsx download responses"
```

---

### Task 2: Export functions in `web/src/api/cases.ts`

**Files:**
- Modify: `web/src/api/cases.ts` (current full content is 136 lines; only
  the new additions are shown below — everything else in the file is
  untouched)

**Interfaces (consumes Task 1's `api.getBlob`/`BlobDownload`; produced,
relied on by Plan 07 and Plan 08):**
```ts
export function exportCaseSummaries(caseIds?: number[]): Promise<BlobDownload>
export function exportCaseLetters(letterIds?: number[]): Promise<BlobDownload>
```
`caseIds`/`letterIds` omitted or empty → no query param (server exports
everything in the caller's RBAC scope, per the design spec's "All cases"
option). A non-empty array → comma-joined `case_ids`/`letter_ids` query
param (server-side route contract from
`docs/superpowers/plans/2026-09-04-compliance-report-export-05-export-routes.md`:
`GET /api/case-summaries/export?case_ids=1,2,3` and
`GET /api/case-letters/export?letter_ids=1,2,3`).

- [ ] **Step 1: Add the import and the two functions.**

`cases.ts` already imports `{ api } from './client'` at the top (Task 1 put
`BlobDownload` in that same module) — extend the existing import line:
```ts
import { api, type BlobDownload } from './client'
```
Then add these two functions anywhere in the file (grouping them near the
other `fetchCase*`/`fetchAllCaseLetters` read functions is fine, but exact
placement doesn't matter — this is a flat function-exporting module with no
ordering requirement):
```ts
// Triggers the CRD-format .xlsx export (GET /api/case-summaries/export).
// caseIds scopes to "current view" (the Cases-view's caseTreeData ids);
// omitted/empty means "All cases" — every case in the caller's RBAC scope.
export function exportCaseSummaries(caseIds?: number[]): Promise<BlobDownload> {
  const path = caseIds?.length ? `/case-summaries/export?case_ids=${caseIds.join(',')}` : '/case-summaries/export'
  return api.getBlob(path)
}

// Triggers the CMOD-format .xlsx export (GET /api/case-letters/export).
// letterIds scopes to "current view" (the Docs page's post-filter letter
// ids); omitted/empty means "All cases".
export function exportCaseLetters(letterIds?: number[]): Promise<BlobDownload> {
  const path = letterIds?.length ? `/case-letters/export?letter_ids=${letterIds.join(',')}` : '/case-letters/export'
  return api.getBlob(path)
}
```

- [ ] **Step 2: Typecheck.**
  Run: `cd web && npm run build`
  Expected: succeeds.

- [ ] **Step 3: Commit**
```bash
git add web/src/api/cases.ts
git commit -m "web: add exportCaseSummaries/exportCaseLetters API functions"
```

---

### Task 3: `downloadBlob` helper

**Files:**
- Create: `web/src/lib/download.ts`

**Why a new file, not an existing one:** `web/src/lib/utils.ts` holds only
the unrelated `cn()` classname helper (4 lines, single responsibility).
Nothing else in `web/src/lib/` (`case-options.ts`, `dns-error.ts`,
`dns-records-cache.ts`, `heatmap-year.ts`, `relative-time.ts`, etc.) is
about file downloads. A new one-function file matches this directory's
existing pattern of one small file per concern.

**Interfaces (produced, relied on by Plan 07 and Plan 08):**
```ts
export function downloadBlob(blob: Blob, filename: string): void
```

- [ ] **Step 1: Write the file.**
```ts
// Triggers the browser's save dialog for an in-memory Blob — used by the
// Cases-view (urls.tsx) and Docs (docs.tsx) export buttons to save the
// .xlsx returned by exportCaseSummaries/exportCaseLetters.
export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
```

- [ ] **Step 2: Typecheck.**
  Run: `cd web && npm run build`
  Expected: succeeds.

- [ ] **Step 3: Manual sanity check (no unit test harness — see Global
  Constraints).** In a scratch spot (e.g. temporarily call it from a
  browser devtools console on any already-running page of this app, or
  wire it to a throwaway button), run:
  ```js
  downloadBlob(new Blob(['test'], { type: 'text/plain' }), 'test.txt')
  ```
  Expected: the browser's save dialog (or an automatic download, depending
  on browser settings) appears for a file named `test.txt` containing the
  text `test`. Remove any throwaway wiring used for this check before
  committing — it's a one-off verification, not a permanent test fixture.

- [ ] **Step 4: Commit**
```bash
git add web/src/lib/download.ts
git commit -m "web: add downloadBlob helper for triggering browser file saves"
```

## Verification for this plan as a whole

```bash
cd web && npm run build
```
Must pass. This plan has no runtime-observable effect on its own (nothing
calls `exportCaseSummaries`/`exportCaseLetters`/`downloadBlob` yet) — Plan
07 and Plan 08 are what wire these into visible UI and are where real
end-to-end verification (an actual file downloading from a live button
click) happens.
