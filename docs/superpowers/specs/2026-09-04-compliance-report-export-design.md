# Exportable compliance report (CRD + CMOD Excel export) — design

Resolves the "Exportable compliance report" TODO in the root `CLAUDE.md`. Scope: let a user download the current app data as `.xlsx`, shaped to match the two legacy Excel files the CRD and CMOD departments already track blocking cases in — `Blocking Full List_1.xlsx` (CRD) and `Masterlist Blocking CMOD.xlsx`'s `BLK` sheet (CMOD) — so the export is a drop-in replacement for the file people already circulate, not a new report format they have to learn.

Two independent exports, each sourced from the page that already shows the matching data shape:

- **CRD export**, from `urls.tsx`'s Cases view (`GET /api/case-summaries`) — one row per (domain, offence).
- **CMOD export**, from `docs.tsx` (`GET /api/case-letters`) — one row per (letter, domain).

## Context this design relies on

- `internal/blockimport/` (+ `cmd/import-crd`, `cmd/import-cmod`) already exists and is merged — it imports these same two legacy files *into* the DB. The root `CLAUDE.md`'s TODO section calling this "nothing imported yet" is stale and should be corrected as a follow-up, separate from this change.
- The importer deliberately does **not** write `Agency`/`Category`/`Element`/`Citation`/`OICUserID` for historical rows (see `write.go`'s `CategoriesObserved`-only comment and `write_cmod.go`'s `foldOICIntoRemarks`), pending open sign-off questions in `docs/blocking-list-migration-clarifications.md` and `docs/cmod-blocking-list-migration-clarifications.md`. Exported historical rows will show these columns blank until that separate migration work lands — expected, not a defect in this feature.
- `excelize/v2` is already a `go.mod` dependency (currently `// indirect`, used by `blockimport` to *read* `.xlsx`) — no new dependency needed to *write* one.
- `OICUserID`/`CaseLetter.OICUser` already exist in the schema and `AddCaseLetter`'s request body already accepts `oic_user_id` — it has just never been defaulted or exposed in the UI until this change.

## Part 1 — OIC field (prerequisite for a non-empty CMOD export)

### Backend

- `AddCaseLetter` (`internal/server/case_handlers.go`): when the request omits `oic_user_id`, default it to the authenticated caller's own `user.ID` before calling `store.AddCaseLetter`. Explicit `oic_user_id` in the body still wins (the "user can override" half).
- `db.CaseLetterFields` (`internal/db/models.go`) gains `OICUserID **uint` — same three-state nil/clear/set convention as `CaseFields.AgencyID` — so `UpdateCaseLetterFields` can set or clear it later. `UpdateCaseLetter` handler and `internal/db/cases.go`'s `UpdateCaseLetterFields` implementation both thread this through the same way `AgencyID` already does in `UpdateCaseFields`.
- New route `GET /api/users/open` → `ListUsersOpen` handler, gated by `requireAuth` only (not `requireDeptAdmin`/`requireAdmin`) — mirrors the existing `GET /api/departments` (`ListDepartmentsOpen`) "any authenticated user" pattern. Reuses `store.ListUsers(ctx)` plus the same own-department-filter branch `ListUsers` already has for a non-admin caller (admin callers see everyone). Returns full `db.User` rows (password hash already excluded via `json:"-"`).
- No change to `listCaseLetters`'s SQL — OIC's username is resolved client-side (and in the export, server-side) from the already-fetched user list rather than adding a join.

### Frontend

- `web/src/api/users.ts` (new file): `fetchUsersOpen(): Promise<User[]>` calling `GET /api/users/open`.
- `AddDocumentDialog` (`docs.tsx`): new "OIC" `<Select>` sourced from `fetchUsersOpen()`, defaulting to the logged-in user (`useAuth()`) whenever the dialog opens/resets, overridable before submit. Joins the existing `commonFields` bucket (same treatment as Recipient/Requestor) so it's set identically on both the Notice and Memo letters at creation.
- `EditDocumentDialog`: same "OIC" `<Select>`, seeded from `(notice ?? memo)?.oic_user_id`, saved through the same shared-fields `PATCH` both letters already receive.
- Scope: the picker offers only the case's own department's users (not system-wide) — matches every other case-editing field's department scoping. An admin's own department pool is themselves + whichever department the case belongs to (their session doesn't have a fixed department, so the picker uses the *case's* `department_id`, not the caller's).
- Out of scope for this pass: an OIC *column* in the docs.tsx grid itself. The dialogs are where it's set; the CMOD export surfaces it regardless.

## Part 2 — Export backend (`internal/blockexport`, new package)

Mirrors `blockimport`'s existing shape: pure, unit-testable flatten functions separated from the thin `excelize`-writing code.

```
internal/blockexport/
  crd.go       // FlattenCRDRows, WriteCRDWorkbook
  crd_test.go
  cmod.go      // FlattenCMODRows, WriteCMODWorkbook
  cmod_test.go
```

### Routes

Both under the existing authenticated `/api` group, same RBAC as their JSON-list counterparts (department-scoped for non-admins, via the same `store` methods):

- `GET /api/case-summaries/export?case_ids=1,2,3` → CRD-format `.xlsx`
- `GET /api/case-letters/export?letter_ids=1,2,3` → CMOD-format `.xlsx`

`case_ids` / `letter_ids` are optional and comma-separated. Omitted means "everything in my RBAC scope" (the "All cases" scope option below). When present, the handler still calls the normal department-scoped `store` list method first and then filters to the given ID set in Go — it never trusts a client-supplied ID list as a substitute for re-fetching authoritative data, so a non-admin can't widen their own scope by passing another department's IDs.

Note the deliberate asymmetry: CRD takes `case_ids`, CMOD takes `letter_ids`. `urls.tsx`'s Cases-view filters (search, agency, status) are case/domain-grained, so case IDs fully capture "what's on screen." `docs.tsx`'s filters (Type, Workflow Status) are letter-grained — filtering to `Type = Notice` hides Memo subrows while their parent case row stays visible — so only a letter-ID list can reproduce "what's on screen" without silently re-including filtered-out letters.

### Response

`Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`, `Content-Disposition: attachment; filename="blocking-list-export-<YYYY-MM-DD>.xlsx"` (CRD) / `"cmod-blocking-export-<YYYY-MM-DD>.xlsx"` (CMOD).

### CRD export — `FlattenCRDRows`

Input: `[]db.CaseSummary` (from `ListCases`/`ListCasesForDepartment`, optionally ID-filtered) **plus** `[]db.CaseLetterEntry` for the same case IDs (from `ListCaseLetters`/`ListCaseLettersForDepartment`) — needed because `CaseSummary`'s `Notice*` fields collapse to whichever `Notice`/`Notice (Uplift)` letter is most recent, losing the original block date once a case is later uplifted. `FlattenCRDRows` cross-references the raw letter list per case to recover both dates independently, without touching `caseSummaryQuery` itself (zero risk to the existing Cases view).

Columns, in this order:

| # | Column | Source |
|---|---|---|
| 1 | `Tahun` | Year of that case's earliest `Notice`-type letter's `LetterDate`; falls back to `Case.RequestedAt`, then `Case.CreatedAt` |
| 2 | `Alamat Laman Web` | `CaseSummaryDomain.URL` |
| 3 | `Butiran Kesalahan` | `OffenceEntry.Citation` |
| 4 | `Agensi` | `Case.AgencyName` |
| 5 | `Tarikh Maklum IASP (Blocked)` | That case's earliest `Notice` (not `Notice (Uplift)`) letter's `LetterDate` |
| 6 | `No. Rujukan NMD` | That Notice letter's `ReferenceNumberExternal` |
| 7 | `Kategori` | `OffenceEntry.Category` |
| 8 | `Elemen` | `OffenceEntry.Element` |
| 9 | `Sub-Elemen` | `OffenceEntry.SubElement` |
| 10 | `Status` | `CaseSummaryDomain.Status`, Title-cased as-is (`Requested`/`Uplift`/`Suspended`/`Internal` — app's own vocabulary, not translated to legacy "Blocked") |
| 11 | `Tarikh Maklum ISP (Uplift)` | That case's `Notice (Uplift)` letter's `LetterDate`, blank if none exists |
| 12 | `No. Rujukan NMSMD` | The same Notice letter's `ReferenceNumberInternal` |
| 13 | `Remarks` | The Notice letter's `Remarks` |
| 14 | `.my` | `Yes`/`No` via `strings.HasSuffix(url, ".my")` |
| 15 | `Case ID` *(extra)* | `Case.ID` |
| 16 | `Department` *(extra)* | via the case's `department_id` → name (already available on `CaseLetterEntry.DepartmentName`) |
| 17 | `Due Date` *(extra)* | `Case.DueDate` |

**Grain**: one row per `CaseSummaryDomain` × its `Offences`. A domain with zero attached offences still emits exactly one row (columns 3, 7–9 blank) rather than being dropped.

### CMOD export — `FlattenCMODRows`

Input: `[]db.CaseLetterEntry` (from `ListCaseLetters`/`ListCaseLettersForDepartment`, ID-filtered to the requested `letter_ids` or full RBAC scope) plus each letter's case's domains' offences (fetched the same way the CRD export does, keyed by the URLs already on `CaseLetterEntry.URLs`).

Columns, in this order:

| # | Column | Source |
|---|---|---|
| 1 | `No` | Sequential row number in the output |
| 2 | `Letter Date` | `CaseLetter.LetterDate` |
| 3 | `Recipient` | `CaseLetter.Recipient` |
| 4 | `Type` | `CaseLetter.Type` |
| 5 | `Subject` | `CaseLetter.Subject` |
| 6 | `Reference No` | `CaseLetter.ReferenceNumberExternal` |
| 7 | `OIC` | `OICUser.Username` via the id→username map built from `GET /api/users/open`-equivalent department lookup (fetched once per export, not joined in SQL) |
| 8 | `Requestor` | `CaseLetter.Requestor` |
| 9 | `Offence` | The domain's `OffenceEntry.Category` (same per-domain `URLOffence` lookup the CRD export uses — offences attach to the domain, not the letter, so this is populated even though the sheet's grain is per-letter) |
| 10 | `Link` | The one domain for this row |
| 11 | `Remarks` | `CaseLetter.Remarks` |
| 12 | `Agency` | The parent case's `Agency.Name` |
| 13 | `Status` | `CaseLetter.WorkflowStatus` (Draft/Pending Legal/Pending TSC/Submitted) — matches the legacy column's actual semantics (letter-approval workflow), not the domain's block/uplift/suspended status |
| 14 | `Received` | `CaseLetter.ReceivedAt` |
| 15 | `Submission` | `CaseLetter.SubmittedAt` |
| 16 | `Case ID` *(extra)* | `CaseLetter.CaseID` |
| 17 | `Internal Ref (No. Rujukan NMSMD)` *(extra)* | `CaseLetter.ReferenceNumberInternal` |

**Grain**: one row per `CaseLetterEntry` × its `URLs` — a letter covering 3 domains produces 3 rows (`Link` varies, everything else repeats), mirroring the legacy sheet's own "One Link Per Row" header. A domain with multiple offences repeats the row again per offence (same rule as CRD), so the full expansion is letter × domain × offence.

## Part 3 — Frontend

- `web/src/api/client.ts`: new `getBlob(path: string): Promise<Blob>` helper alongside the existing JSON-returning helpers (none of today's calls download binary).
- `web/src/api/cases.ts`: `exportCaseSummaries(caseIds?: number[]): Promise<Blob>` and `exportCaseLetters(letterIds?: number[]): Promise<Blob>`, building the `?case_ids=`/`?letter_ids=` query string when a scoped ID list is passed.
- Both `urls.tsx` (Cases view) and `docs.tsx` get an "Export" `<Button>` next to their existing toolbar, paired with a `<Select>` for scope: **"Current view"** / **"All cases"**.
  - "Current view" collects the case IDs (`urls.tsx`) or letter IDs (`docs.tsx`) already visible after client-side search/filter — from `caseTreeData`/`filtered`, which already hold exactly this — and passes them.
  - "All cases" omits the ID param.
- On click: `fetch` the blob, then a small shared `downloadBlob(blob, filename)` helper (`URL.createObjectURL` → temporary `<a download>` click → `URL.revokeObjectURL`) triggers the browser's save dialog. Filename read from the response's `Content-Disposition` header, falling back to a client-computed default if absent.

## Testing

- `internal/blockexport/crd_test.go`, `cmod_test.go`: table-driven unit tests on `FlattenCRDRows`/`FlattenCMODRows` — pure functions, no DB or `excelize` involved — covering: the multi-offence row-expansion, zero-offence single-row fallback, the Notice-vs-Notice(Uplift) date-splitting logic, and the letter × domain expansion for CMOD.
- One smoke test per format writes an actual workbook via `WriteCRDWorkbook`/`WriteCMODWorkbook` and re-reads header + first data row cells with `excelize.OpenFile` to confirm the sheet isn't corrupt and columns land in the declared order.
- `internal/server`: handler tests for `GET /api/case-summaries/export` and `GET /api/case-letters/export` covering RBAC scoping (non-admin can't widen scope via a foreign ID) and the ID-filter behavior, following the existing `fullMockStore` pattern in `handlers_test.go`.
- `AddCaseLetter`'s new OIC-default behavior gets a handler test asserting an omitted `oic_user_id` lands as the caller's own ID.

## Non-goals / explicitly out of scope

- Fixing the underlying `blockimport` gaps (Agency/Category/Element/Citation/OIC not persisted for historical rows) — separate work, blocked on the sign-offs already tracked in the two migration-clarification docs.
- An OIC column in the `docs.tsx` grid.
- PDF export, or any format beyond `.xlsx`.
- Correcting the root `CLAUDE.md` TODO's stale "nothing imported yet" line — quick follow-up, not bundled here to keep this diff focused.
