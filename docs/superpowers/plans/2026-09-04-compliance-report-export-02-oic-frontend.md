# OIC Field — Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an "OIC" `<Select>` to both `AddDocumentDialog` and `EditDocumentDialog` on `web/src/routes/docs.tsx`, sourced from the new `GET /api/users/open` route, scoped to the case's own department, defaulting to the logged-in user at creation.

**Architecture:** One new API module (`web/src/api/users.ts`, mirrors the existing `web/src/api/departments.ts`), one new field wired into `web/src/api/cases.ts`'s `updateCaseLetter`, and two dialog edits in `docs.tsx` that mirror how Recipient (a plain string `<Select>`) is already wired — the OIC picker is the same shape, just backed by users instead of a free-text lookup table, and additionally department-scoped.

**Tech Stack:** React 19 + TypeScript, existing `Select`/`SelectTrigger`/`SelectContent`/`SelectItem` components (`@/components/ui/select`) — no new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-04-compliance-report-export-design.md`, "Part 1 — OIC field" section (the "Frontend" subsection specifically).

**Prerequisite:** `2026-09-04-compliance-report-export-01-oic-backend.md` must already be merged to `main` — this plan calls `GET /api/users/open` (Task 4 of that plan) and sends `oic_user_id` through `PATCH /api/cases/{id}/letters/{letter_id}` (Task 2 of that plan). It does not depend on the export-backend chain (Plans 03-08).

## Global Constraints

- Every step below contains real, complete code — no "add appropriate error handling" placeholders.
- Go marshals a nil slice to JSON `null`, not `[]` (see `web/CLAUDE.md`) — every new fetcher that expects an array must guard with `Array.isArray(data) ? data : []`, matching every existing fetcher in this codebase (`fetchDepartmentsOpen`, `fetchCaseSummaries`, etc.).
- `cd web && npm run build` (runs `tsc --noEmit` as part of the production build, per `web/CLAUDE.md`'s Commands section) must pass after every task.
- No OIC *column* in the `docs.tsx` grid itself — this plan only touches the two dialogs (`AddDocumentDialog`, `EditDocumentDialog`).

---

### Task 1: `web/src/api/users.ts` — open users fetcher

**Files:**
- Create: `web/src/api/users.ts`

**Pattern to mirror exactly** — `web/src/api/departments.ts` (full current file):
```ts
import { api } from './client'
import type { Department } from './types'

// Open read of the department list (GET /api/departments) — distinct from
// admin.ts's fetchDepartments, which hits the super-admin-only
// /api/admin/departments and backs the admin Departments tab. This one is
// for the Requesting Dept dropdown any authenticated user needs when
// adding/editing a domain's case metadata.
export async function fetchDepartmentsOpen(): Promise<Department[]> {
  const data = await api.get<Department[]>('/departments')
  return Array.isArray(data) ? data : []
}
```
The `User` type already exists at `web/src/api/types.ts:169-178`:
```ts
export type User = {
  id: number
  username: string
  is_admin: boolean
  is_dept_admin: boolean
  department_id?: number
  department?: Department
  must_change_password: boolean
  created_at: string
}
```

**Interfaces (produced, relied on by Task 3):**
```ts
export async function fetchUsersOpen(): Promise<User[]>
```

- [ ] **Step 1: Write the file** `web/src/api/users.ts`:
```ts
import { api } from './client'
import type { User } from './types'

// Open read of the user list (GET /api/users/open), department-scoped
// server-side for a non-admin caller (their own department only; admin
// sees everyone) — distinct from admin.ts's user-management fetchers,
// which hit the requireAnyAdmin-gated /api/admin/users. This one backs the
// OIC picker any authenticated user sees when recording a case's letters
// (docs.tsx).
export async function fetchUsersOpen(): Promise<User[]> {
  const data = await api.get<User[]>('/users/open')
  return Array.isArray(data) ? data : []
}
```
- [ ] **Step 2: Typecheck** — there's no unit test harness for this one-function file (mirrors `departments.ts`, which also has none); verification is Task 3's manual smoke-test plus `npm run build`'s `tsc --noEmit` catching any type error.
  Run: `cd web && npm run build`
  Expected: builds clean (this file compiles; nothing references it yet until Task 3).
- [ ] **Step 3: Commit**
```bash
git add web/src/api/users.ts
git commit -m "web: add fetchUsersOpen for the OIC picker"
```

---

### Task 2: `updateCaseLetter` — accept `oicUserId`

**Files:**
- Modify: `web/src/api/cases.ts` (the `CaseLetterFieldsUpdate` type and `updateCaseLetter` function, currently lines 102-132)

**Current code** (`web/src/api/cases.ts:102-132`):
```ts
export type CaseLetterFieldsUpdate = Partial<{
  subject: string
  workflowStatus: string
  referenceNumberExternal: string
  referenceNumberInternal: string
  recipient: string
  requestor: string
  remarks: string
  letterDate: string | null
  receivedAt: string | null
  submittedAt: string | null
}>

// Partial update of one CaseLetter's fields (PATCH
// /api/cases/{caseId}/letters/{letterId}) — only keys present in `fields`
// are sent. Date fields clear via null -> "" (same sentinel convention as
// updateCase's dueDate/requestedAt above); string fields clear via "".
export async function updateCaseLetter(caseId: number, letterId: number, fields: CaseLetterFieldsUpdate): Promise<void> {
  const body: Record<string, string> = {}
  if (fields.subject !== undefined) body.subject = fields.subject
  if (fields.workflowStatus !== undefined) body.workflow_status = fields.workflowStatus
  if (fields.referenceNumberExternal !== undefined) body.reference_number_external = fields.referenceNumberExternal
  if (fields.referenceNumberInternal !== undefined) body.reference_number_internal = fields.referenceNumberInternal
  if (fields.recipient !== undefined) body.recipient = fields.recipient
  if (fields.requestor !== undefined) body.requestor = fields.requestor
  if (fields.remarks !== undefined) body.remarks = fields.remarks
  if (fields.letterDate !== undefined) body.letter_date = fields.letterDate ?? ''
  if (fields.receivedAt !== undefined) body.received_at = fields.receivedAt ?? ''
  if (fields.submittedAt !== undefined) body.submitted_at = fields.submittedAt ?? ''
  await api.patch<void>(`/cases/${caseId}/letters/${letterId}`, body)
}
```
The backend's `oic_user_id` clear sentinel is `0` (a number), not `""` — see Plan 01's Task 2 (`UpdateCaseLetter` handler treats `oic_user_id: 0` as "clear", matching `updateCase`'s existing `agency_id` convention in this same file at line 38: `body.agency_id = fields.agencyId ?? 0`). Follow that exact convention here, not the string-clear one used by the other `CaseLetterFieldsUpdate` fields.

- [ ] **Step 1: Implement** — no separate test file exists for this module (mirrors the rest of `cases.ts`, which has no accompanying `.test.ts`); Task 4 exercises this through the real dialog. Change:
```ts
export type CaseLetterFieldsUpdate = Partial<{
  subject: string
  workflowStatus: string
  referenceNumberExternal: string
  referenceNumberInternal: string
  recipient: string
  requestor: string
  remarks: string
  letterDate: string | null
  receivedAt: string | null
  submittedAt: string | null
  oicUserId: number | null
}>

// Partial update of one CaseLetter's fields (PATCH
// /api/cases/{caseId}/letters/{letterId}) — only keys present in `fields`
// are sent. Date fields clear via null -> "" (same sentinel convention as
// updateCase's dueDate/requestedAt above); string fields clear via "";
// oicUserId clears via null -> 0 (a number, never a real user id — same
// sentinel convention as updateCase's agencyId above).
export async function updateCaseLetter(caseId: number, letterId: number, fields: CaseLetterFieldsUpdate): Promise<void> {
  const body: Record<string, string | number> = {}
  if (fields.subject !== undefined) body.subject = fields.subject
  if (fields.workflowStatus !== undefined) body.workflow_status = fields.workflowStatus
  if (fields.referenceNumberExternal !== undefined) body.reference_number_external = fields.referenceNumberExternal
  if (fields.referenceNumberInternal !== undefined) body.reference_number_internal = fields.referenceNumberInternal
  if (fields.recipient !== undefined) body.recipient = fields.recipient
  if (fields.requestor !== undefined) body.requestor = fields.requestor
  if (fields.remarks !== undefined) body.remarks = fields.remarks
  if (fields.letterDate !== undefined) body.letter_date = fields.letterDate ?? ''
  if (fields.receivedAt !== undefined) body.received_at = fields.receivedAt ?? ''
  if (fields.submittedAt !== undefined) body.submitted_at = fields.submittedAt ?? ''
  if (fields.oicUserId !== undefined) body.oic_user_id = fields.oicUserId ?? 0
  await api.patch<void>(`/cases/${caseId}/letters/${letterId}`, body)
}
```
(Only change to the function body itself is the `Record<string, string>` → `Record<string, string | number>` type widening and the new `oicUserId` branch; every other line is unchanged.)
- [ ] **Step 2: Typecheck**
  Run: `cd web && npm run build`
  Expected: builds clean.
- [ ] **Step 3: Commit**
```bash
git add web/src/api/cases.ts
git commit -m "web: let updateCaseLetter set/clear oicUserId"
```

---

### Task 3: `CaseGroupRow` carries `departmentId`

**Files:**
- Modify: `web/src/routes/docs.tsx` (the `CaseGroupRow` type at line 729, and the `caseTreeData` builder at lines 861-874)

**Why:** `EditDocumentDialog`'s OIC picker must scope to "whichever department the case belongs to" (an admin's session has no fixed department). `CaseLetterEntry` (the underlying data `caseTreeData` groups) already carries `department_id` (`web/src/api/types.ts:118`) — this task just threads it onto the derived `CaseGroupRow` the dialog receives, it does not add any new fetch.

**Current code** (`web/src/routes/docs.tsx:729`):
```ts
type CaseGroupRow = { kind: 'case'; caseId: number; departmentName: string; urls: string[]; subRows: LetterSubRow[] }
```
**Current builder** (`web/src/routes/docs.tsx:861-874`):
```ts
  const caseTreeData = useMemo<CaseGroupRow[]>(() => {
    const byId = new Map<number, CaseGroupRow>()
    const order: number[] = []
    for (const l of filtered) {
      let group = byId.get(l.case_id)
      if (!group) {
        group = { kind: 'case', caseId: l.case_id, departmentName: l.department_name, urls: l.urls ?? [], subRows: [] }
        byId.set(l.case_id, group)
        order.push(l.case_id)
      }
      group.subRows.push({ kind: 'letter', letter: l })
    }
    return order.map(id => byId.get(id)!)
  }, [filtered])
```

- [ ] **Step 1: Implement.**
  1. Change the type to:
  ```ts
  type CaseGroupRow = { kind: 'case'; caseId: number; departmentId: number; departmentName: string; urls: string[]; subRows: LetterSubRow[] }
  ```
  2. Change the group-creation line inside `caseTreeData` to:
  ```ts
        group = { kind: 'case', caseId: l.case_id, departmentId: l.department_id, departmentName: l.department_name, urls: l.urls ?? [], subRows: [] }
  ```
- [ ] **Step 2: Typecheck**
  Run: `cd web && npm run build`
  Expected: builds clean.
- [ ] **Step 3: Commit**
```bash
git add web/src/routes/docs.tsx
git commit -m "web: carry departmentId on CaseGroupRow for the OIC picker's scoping"
```

---

### Task 4: OIC `<Select>` in `AddDocumentDialog` and `EditDocumentDialog`

**Files:**
- Modify: `web/src/routes/docs.tsx`

**Interfaces (consumed):** `fetchUsersOpen(): Promise<User[]>` (Task 1), `updateCaseLetter(caseId, letterId, { oicUserId, ... })` (Task 2), `CaseGroupRow.departmentId` (Task 3), `useAuth()` from `@/routes/__root` (`web/src/routes/__root.tsx:252`, returns `{ me: User | null, ... }`).

**4a. `DocsPage` fetches the open user list once, alongside recipients/requestors.**

Current imports (`web/src/routes/docs.tsx:15-22`):
```ts
import { fetchAllCaseLetters, createCase, addCaseLetter, addUrlToCase, updateCaseLetter, deleteCaseLetter } from '@/api/cases'
import { createUrl, fetchUrls } from '@/api/urls'
import { fetchDepartmentsOpen } from '@/api/departments'
import { fetchRecipients } from '@/api/recipients'
import { fetchRequestors } from '@/api/requestors'
import { fetchAgencies } from '@/api/agencies'
import { fetchDueDatePresets } from '@/api/due-date-presets'
import type { CaseLetterEntry, Department, Recipient, Requestor, Agency, DueDatePreset } from '@/api/types'
```
Add:
```ts
import { fetchUsersOpen } from '@/api/users'
import { useAuth } from './__root'
```
and add `User` to the `@/api/types` import list: `import type { CaseLetterEntry, Department, Recipient, Requestor, Agency, DueDatePreset, User } from '@/api/types'`.

Current `DocsPage` state + `load` (`web/src/routes/docs.tsx:732-780`):
```ts
function DocsPage() {
  const [letters, setLetters] = useState<CaseLetterEntry[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [domainOptions, setDomainOptions] = useState<string[]>([])
  const [recipients, setRecipients] = useState<Recipient[]>([])
  const [requestors, setRequestors] = useState<Requestor[]>([])
  const [agencies, setAgencies] = useState<Agency[]>([])
  const [duePresets, setDuePresets] = useState<DueDatePreset[]>([])
  // ...
  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [l, d, u, rc, rq, ag, dp] = await Promise.all([
        fetchAllCaseLetters(), fetchDepartmentsOpen(), fetchUrls(), fetchRecipients(), fetchRequestors(),
        fetchAgencies(), fetchDueDatePresets(),
      ])
      setLetters(l)
      setDepartments(d)
      setDomainOptions(u.map(entry => entry.url))
      setRecipients(rc)
      setRequestors(rq)
      setAgencies(ag)
      setDuePresets(dp)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load documents')
    } finally {
      // ...
```
Change to add a `users` state and fetch:
```ts
  const [users, setUsers] = useState<User[]>([])
```
(placed next to the other list-state declarations), and in `load`:
```ts
      const [l, d, u, rc, rq, ag, dp, us] = await Promise.all([
        fetchAllCaseLetters(), fetchDepartmentsOpen(), fetchUrls(), fetchRecipients(), fetchRequestors(),
        fetchAgencies(), fetchDueDatePresets(), fetchUsersOpen(),
      ])
      setLetters(l)
      setDepartments(d)
      setDomainOptions(u.map(entry => entry.url))
      setRecipients(rc)
      setRequestors(rq)
      setAgencies(ag)
      setDuePresets(dp)
      setUsers(us)
```

**4b. Pass `users` to both dialogs** — current render calls (`web/src/routes/docs.tsx:1120-1139`):
```tsx
      <AddDocumentDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
        domainOptions={domainOptions}
        recipients={recipients}
        requestors={requestors}
        agencies={agencies}
        duePresets={duePresets}
        caseOptions={caseOptions}
      />

      <EditDocumentDialog
        open={editTarget !== null}
        onClose={() => setEditTarget(null)}
        onSaved={load}
        editing={editTarget}
        recipients={recipients}
        requestors={requestors}
      />
```
Add `users={users}` to both:
```tsx
      <AddDocumentDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
        domainOptions={domainOptions}
        recipients={recipients}
        requestors={requestors}
        agencies={agencies}
        duePresets={duePresets}
        caseOptions={caseOptions}
        users={users}
      />

      <EditDocumentDialog
        open={editTarget !== null}
        onClose={() => setEditTarget(null)}
        onSaved={load}
        editing={editTarget}
        recipients={recipients}
        requestors={requestors}
        users={users}
      />
```

**4c. `AddDocumentDialog` — add the OIC field.**

Current signature (`web/src/routes/docs.tsx:101-121`):
```tsx
function AddDocumentDialog({
  open,
  onClose,
  onAdded,
  domainOptions,
  recipients,
  requestors,
  agencies,
  duePresets,
  caseOptions,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  domainOptions: string[]
  recipients: Recipient[]
  requestors: Requestor[]
  agencies: Agency[]
  duePresets: DueDatePreset[]
  caseOptions: CaseOption[]
}) {
```
Change to add `users` to both the destructure and the type:
```tsx
function AddDocumentDialog({
  open,
  onClose,
  onAdded,
  domainOptions,
  recipients,
  requestors,
  agencies,
  duePresets,
  caseOptions,
  users,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  domainOptions: string[]
  recipients: Recipient[]
  requestors: Requestor[]
  agencies: Agency[]
  duePresets: DueDatePreset[]
  caseOptions: CaseOption[]
  users: User[]
}) {
```

Current state block (`web/src/routes/docs.tsx:122-149`) has `const [recipient, setRecipient] = useState('')` among others. Add, right after it:
```tsx
  const { me } = useAuth()
  const [oicUserId, setOicUserId] = useState<number | ''>('')
```
This dialog always creates a case under the caller's own department (`createCase`/`CreateCaseForURL` force `department_id` to the caller's own — see `internal/server/CLAUDE.md`'s "Cases <-> URL" section), so the picker's pool is simply "the caller's own department's users" — no separate department prop needed here. Add this `useMemo` near `domainItems` (`web/src/routes/docs.tsx:168-171`):
```tsx
  const oicOptions = useMemo(
    () => users.filter(u => u.department_id === me?.department_id),
    [users, me],
  )
```

Current `reset` (`web/src/routes/docs.tsx:151-159`):
```tsx
  const reset = () => {
    setDomains([]); setDomainQuery(''); setExistingCaseId('')
    setStatus('requested'); setAgencyId(''); setDueDurationMinutes('')
    setReferenceNumberExternal('')
    setRecipient(''); setNoticeSubject(''); setNoticeReferenceNumberInternal('')
    setMemoSubject(''); setMemoReferenceNumberInternal('')
    setRequestor(''); setWorkflowStatus('')
    setLetterDate(''); setReceivedAt(''); setSubmittedAt(''); setRemarks(''); setError(null)
  }
```
Per the spec, OIC defaults to the logged-in user "whenever the dialog opens/resets" — `reset()` is called both on mount-adjacent open and on close (`handleClose`), so defaulting inside `reset()` would re-default it on close too, which is fine (the dialog is about to unmount its visible state anyway) but won't re-default on a bare re-open without a close in between if the dialog stays mounted. Since this dialog's `open`/`onClose` props show it's conditionally rendered as a controlled `Dialog` (stays mounted, toggled via `open`), add a dedicated effect instead, mirroring how `EditDocumentDialog` re-seeds on `open`/`editing` change (`web/src/routes/docs.tsx:543-562`):
```tsx
  useEffect(() => {
    if (open) setOicUserId(me?.id ?? '')
  }, [open, me])
```
Place this effect right after the `reset`/`copySubjectFromMemo`/`copySubjectFromNotice` block. Add `useEffect` to the existing `react` import at the top of the file if not already imported — check `web/src/routes/docs.tsx:1`, which already imports `useCallback, useEffect, useMemo, useState` from `'react'`, so no import change needed here.

Current `commonFields` in `handleSubmit` (`web/src/routes/docs.tsx:188-197`):
```tsx
      const commonFields = {
        reference_number_external: referenceNumberExternal.trim() || undefined,
        recipient: recipient.trim() || undefined,
        requestor: requestor.trim() || undefined,
        workflow_status: workflowStatus || undefined,
        letter_date: isoFromDateInput(letterDate),
        received_at: isoFromDateInput(receivedAt),
        submitted_at: isoFromDateInput(submittedAt),
        remarks: remarks.trim() || undefined,
      }
```
Add one more key:
```tsx
      const commonFields = {
        reference_number_external: referenceNumberExternal.trim() || undefined,
        recipient: recipient.trim() || undefined,
        requestor: requestor.trim() || undefined,
        workflow_status: workflowStatus || undefined,
        letter_date: isoFromDateInput(letterDate),
        received_at: isoFromDateInput(receivedAt),
        submitted_at: isoFromDateInput(submittedAt),
        remarks: remarks.trim() || undefined,
        oic_user_id: oicUserId === '' ? undefined : oicUserId,
      }
```
This flows into both `letters` array entries (Notice + Memo) unchanged, since `letters = [{ ...commonFields, type: 'Notice', ... }, { ...commonFields, type: 'Memo', ... }]` already spreads `commonFields` into both — no other change needed in `handleSubmit`. `addCaseLetter`'s TS param type is `Partial<Omit<CaseLetter, 'id' | 'case_id' | 'created_at'>>` (`web/src/api/cases.ts:53-58`) and `CaseLetter` already has `oic_user_id?: number`, so no `cases.ts` change is needed for the Add path (only `updateCaseLetter`, Task 2, needed a signature change, since edits need the 0-clears-it sentinel that creation doesn't).

**JSX** — add the OIC `<Select>` right after the Recipient field (`web/src/routes/docs.tsx:430-441`):
```tsx
          <div className="form-field">
            <label className="form-label" id="add-doc-recipient-label">Recipient</label>
            <Select value={recipient} onValueChange={setRecipient} disabled={loading}>
              <SelectTrigger aria-labelledby="add-doc-recipient-label" placeholder="—" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="">—</SelectItem>
                {recipients.map((r, i) => (
                  <SelectItem key={r.id} index={i + 1} value={r.name}>{r.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="form-field">
            <label className="form-label" id="add-doc-oic-label">OIC</label>
            <Select
              value={oicUserId === '' ? '' : String(oicUserId)}
              onValueChange={v => setOicUserId(v === '' ? '' : Number(v))}
              disabled={loading}
            >
              <SelectTrigger aria-labelledby="add-doc-oic-label" placeholder="—" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="">—</SelectItem>
                {oicOptions.map((u, i) => (
                  <SelectItem key={u.id} index={i + 1} value={String(u.id)}>{u.username}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
```
(`Select`'s `value`/`onValueChange` here work on strings, same as every other `<Select>` in this file — `oicUserId` is stored as `number | ''` in state, matching `agencyId`'s own `useState<number | ''>('')` a few lines up, and converted to/from string only at the JSX boundary.)

**4d. `EditDocumentDialog` — add the OIC field.**

Current signature (`web/src/routes/docs.tsx:522-524`):
```tsx
function EditDocumentDialog({
  open, onClose, onSaved, editing, recipients, requestors,
}: { open: boolean; onClose: () => void; onSaved: () => void; editing: CaseGroupRow | null; recipients: Recipient[]; requestors: Requestor[] }) {
```
Change to:
```tsx
function EditDocumentDialog({
  open, onClose, onSaved, editing, recipients, requestors, users,
}: { open: boolean; onClose: () => void; onSaved: () => void; editing: CaseGroupRow | null; recipients: Recipient[]; requestors: Requestor[]; users: User[] }) {
```

Current state + seeding effect (`web/src/routes/docs.tsx:525-562`):
```tsx
  const notice = editing?.subRows.find(s => s.letter.type === 'Notice')?.letter
  const memo = editing?.subRows.find(s => s.letter.type === 'Memo')?.letter

  const [referenceNumberExternal, setReferenceNumberExternal] = useState('')
  const [recipient, setRecipient] = useState('')
  const [requestor, setRequestor] = useState('')
  // ... other fields
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!open || !editing) return
    const source = notice ?? memo
    setReferenceNumberExternal(source?.reference_number_external ?? '')
    setRecipient(source?.recipient ?? '')
    setRequestor(source?.requestor ?? '')
    // ... other fields
    setError(null)
  }, [open, editing, notice, memo])
```
Add a state var and seed it in the same effect:
```tsx
  const [oicUserId, setOicUserId] = useState<number | ''>('')
```
(placed next to `const [recipient, setRecipient] = useState('')`), and inside the `useEffect`, right after `setRecipient(source?.recipient ?? '')`:
```tsx
    setOicUserId(source?.oic_user_id ?? '')
```
The spec's exact wording is "seeded from `(notice ?? memo)?.oic_user_id`" — `source` here is already `notice ?? memo`, so `source?.oic_user_id ?? ''` is the correct expression (falls back to `''`, the "no selection" sentinel, when neither letter has one set).

Add the department-scoped options `useMemo`, next to the other hooks (after the `useEffect` block, before `handleClose`):
```tsx
  const oicOptions = useMemo(
    () => users.filter(u => u.department_id === editing?.departmentId),
    [users, editing],
  )
```

Current `commonFields` in `handleSubmit` (`web/src/routes/docs.tsx:576-585`):
```tsx
      const commonFields = {
        referenceNumberExternal: referenceNumberExternal.trim(),
        recipient: recipient.trim(),
        requestor: requestor.trim(),
        workflowStatus,
        letterDate: isoFromDateInput(letterDate) ?? null,
        receivedAt: isoFromDateInput(receivedAt) ?? null,
        submittedAt: isoFromDateInput(submittedAt) ?? null,
        remarks: remarks.trim(),
      }
```
Add one more key:
```tsx
      const commonFields = {
        referenceNumberExternal: referenceNumberExternal.trim(),
        recipient: recipient.trim(),
        requestor: requestor.trim(),
        workflowStatus,
        letterDate: isoFromDateInput(letterDate) ?? null,
        receivedAt: isoFromDateInput(receivedAt) ?? null,
        submittedAt: isoFromDateInput(submittedAt) ?? null,
        remarks: remarks.trim(),
        oicUserId: oicUserId === '' ? null : oicUserId,
      }
```
This flows unchanged into both `updateCaseLetter(notice.case_id, notice.id, { ...commonFields, ... })` and the `memo` call right below it (`web/src/routes/docs.tsx:587-600`) — no further change needed there, since both already spread `commonFields`.

**JSX** — add the OIC `<Select>` right after the Recipient field (`web/src/routes/docs.tsx:625-636`):
```tsx
          <div className="form-field">
            <label className="form-label" id="edit-doc-recipient-label">Recipient</label>
            <Select value={recipient} onValueChange={setRecipient} disabled={loading}>
              <SelectTrigger aria-labelledby="edit-doc-recipient-label" placeholder="—" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="">—</SelectItem>
                {recipients.map((r, i) => (
                  <SelectItem key={r.id} index={i + 1} value={r.name}>{r.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="form-field">
            <label className="form-label" id="edit-doc-oic-label">OIC</label>
            <Select
              value={oicUserId === '' ? '' : String(oicUserId)}
              onValueChange={v => setOicUserId(v === '' ? '' : Number(v))}
              disabled={loading}
            >
              <SelectTrigger aria-labelledby="edit-doc-oic-label" placeholder="—" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="">—</SelectItem>
                {oicOptions.map((u, i) => (
                  <SelectItem key={u.id} index={i + 1} value={String(u.id)}>{u.username}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
```

- [ ] **Step 1: Implement** all of 4a-4d above in `web/src/routes/docs.tsx`.
- [ ] **Step 2: Typecheck**
  Run: `cd web && npm run build`
  Expected: builds clean.
- [ ] **Step 3: Manual smoke test** (no frontend test harness exists in this codebase for route components — verification here is running the app, per the project's own convention of manual UI verification for route-level changes):
  1. `./dev.sh` (or `go run ./cmd/server/ ...` + `cd web && npm run dev`, per the repo-root `CLAUDE.md`).
  2. Log in, go to `/docs`, click "Add Document". Confirm the OIC `<Select>` is present, defaults to your own username, and only lists your own department's users (compare against `/admin`'s Users tab for your department).
  3. Submit, then open "Edit" on the created case row. Confirm OIC shows the value just set, and that changing it + saving persists (reload the page and re-open Edit to confirm).
  4. Clear OIC in Edit (select "—") and save; reload and re-open Edit to confirm it's now blank, not still showing the old value.
- [ ] **Step 4: Commit**
```bash
git add web/src/routes/docs.tsx
git commit -m "web: add OIC picker to Add/Edit Document dialogs"
```

## Self-Review (performed while writing this plan)

- **Spec coverage:** All five "Frontend" bullets under the design spec's "Part 1 — OIC field" are covered: `web/src/api/users.ts` (Task 1), `AddDocumentDialog`'s OIC select defaulting to the logged-in user and joining `commonFields` (Task 4c), `EditDocumentDialog`'s OIC select seeded from `(notice ?? memo)?.oic_user_id` and saved through the shared-fields PATCH (Task 4d), department-only scoping using the case's own department for Edit and the caller's own for Add (Tasks 3, 4c, 4d), and the explicit out-of-scope note (no grid column) is honored — no task touches the `columns` array or `DataGridTable`.
- **Placeholder scan:** Every step has complete code; the one place real judgment was needed (why an `open`-keyed `useEffect` was chosen over stuffing the default into `reset()` for `AddDocumentDialog`) is explained inline rather than left as a TODO.
- **Type consistency:** `oicUserId: number | ''` in component state everywhere it appears (4c, 4d); `oic_user_id` (snake_case) only at the API-body boundary (`commonFields` in both dialogs, matching every other field's snake_case-at-the-wire/camelCase-in-state split already used in this file); `CaseGroupRow.departmentId` (Task 3) is exactly what Task 4d's `oicOptions` `useMemo` reads (`editing?.departmentId`); `fetchUsersOpen`'s return type (`User[]`) matches the `users` prop type threaded through both dialogs.
