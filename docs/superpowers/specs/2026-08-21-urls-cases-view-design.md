# urls.tsx: Cases View + Case Edit Form

**Date:** 2026-08-21
**Branch:** main (design phase — not yet branched)

## Overview

`/urls` currently shows one row per domain (`URLEntry`, from `GET /api/urls` → `ListDepartmentURLs`). A case that covers several domains ("N URLs in one Notice") is invisible as a group — each domain is its own row with the case's shared fields (agency/status/due date/ref number) repeated on every one, and there is no view that groups by case.

This adds a second view mode, **Cases view**, that groups domains under their case: one expandable row per case (case #, agency, status, due date, ref number, domain count, plus the Notice letter's subject/workflow status), expanding to one subrow per domain showing that domain's own Phase override. A `ToggleGroup` switches between **Domain view** (today's table, largely unchanged) and **Cases view**, state kept in the URL (`?view=domains|cases`).

Case-level editing (agency/status/due date/Notice+Memo letter fields) moves to a single edit action on the case row in Cases view, reusing `AddUrlDialog` in an add/edit-dual-purpose mode (the same pattern `InstrumentFormDialog` already uses on `legal-citations.tsx`). Domain view's action column drops to just "view details" (→ `/domain/$url`) and delete — no edit icon there.

## Background

- `Case` metadata (Agency/Status/DueDate) and `CaseLetter` (Notice/Memo, with subject/ref numbers/workflow status/dates) already exist — see "Case metadata" in `internal/db/CLAUDE.md`. A case covers 1+ domains via `CaseURL` (many-to-many, carrying a per-domain `Phase` override of `Case.Status`).
- The only existing per-case read endpoints are `GET /api/cases/*url` (cases for one domain — `CasesByURL`, used by the now-removed `CaseHistoryDialog`) and `GET /api/case-letters` (every `CaseLetter` across every case, one row per letter, paginated — `docs.tsx`'s data source). Neither returns "every case for this department, each with its shared fields and its list of domains" — that's the new piece.
- There is no update path for `CaseLetter` fields today — only `AddCaseLetter` (create). Editing the case row's Notice subject/workflow status/ref numbers needs a new `PATCH` endpoint.
- `urls.tsx` already has: `Status` as an inline `<Select>` per domain row (patches the domain's *latest* case via `case_id` → `updateCase`), a `Link` to `/domain/$url` on the Grip icon, and no edit icon (dropped in the prior session along with `CaseHistoryDialog`/`EditUrlDialog`'s wiring — those two dialog components remain in the codebase unused, left as-is, not deleted).

## Decisions

- **New read endpoint**: `GET /api/case-summaries` (not `/api/cases` — would collide with the existing `/cases/*` wildcard route) returns `[]CaseSummary`, one row per case: case fields, the Notice letter's fields (chosen over Memo when both exist — same "Notice is authoritative" convention `current_reference_number` already uses), and the list of domains the case covers (`url_id`, `url`, `phase`).
- **New write endpoint**: `PATCH /api/cases/{id}/letters/{letter_id}` (`UpdateCaseLetter`) — partial update of one `CaseLetter`'s fields, same ownership check (`GetCase` → `DepartmentID` match) as `AddCaseLetter`/`UpdateCase`.
- **Cases view is a real tree grid** (`getSubRows`/`getExpandedRowModel`), not a nested/bespoke inner table — mirrors `results.index.tsx`'s `DomainRow`/`ServerRow` discriminated-union pattern (there: `CaseRow`/`DomainSubRow`).
- **`AddUrlDialog` becomes add/edit-dual-purpose** (`editing: CaseSummary | null` prop), matching `InstrumentFormDialog`'s pattern in `legal-citations.tsx`: a `useEffect` on `[open, editing]` prefills every field when `editing` is set, submit branches to `updateCase`/`updateCaseLetter`(s) instead of `createCase`/`addCaseLetter`. The domain textarea stays editable in edit mode (per earlier decision) — prefilled with the case's current domain list (one per line); on save, any line not already linked to the case is added via `addUrlToCase`. No domain *removal* through this form — there's no "unlink domain from case" endpoint, and that's out of scope here; removing stays a domain-view/subrow action (existing watchlist delete).
- **Domain-subrow actions**: Status `<Select>` (same mechanism as today's Domain view — denormalized `Case.Status`, edited via `updateCase(caseId, {status})`, kept on the subrow rather than the case row so switching views doesn't lose the quick single-click status edit), Phase `<Select>` (→ `updateCaseURLPhase`, the per-domain override), "view details" link (→ `/domain/$url`), delete (existing `DeleteConfirmDialog` + `DELETE /api/urls/{id}` flow, unchanged).
- **Case-row actions**: only `SquarePenIcon` (from `legal-citations.tsx`) opening the prefilled edit form — that's where Agency/Due Date/letter fields are edited (nothing else on this row is interactive; Status/Due Date/Agency/Ref No. display as plain text, matching the "case row: edit case only" split).
- **View toggle**: `ToggleGroup` (`Domain` / `Cases`) above the table, same component already used on `domain.$url.tsx`. State via `validateSearch` on the `/urls` route (`?view=domains|cases`, default `domains`) — same convention as `domain.$url.tsx`'s `?tab=`.
- **Filters/search apply to both views**, reinterpreted at case granularity for Cases view: Status/Agency filters match the case's own fields; Requesting Dept. filter matches if any domain in the case belongs to that department; search matches the case's ref number or any of its domains' hostnames. Date filters (Date Added, Due Date) — Cases view has no per-case "created_at" (cases don't currently expose one in `CaseSummary`; add `CreatedAt` to the new struct so "Date Added" still works, filtered against the case's own `CreatedAt` instead of `URL.CreatedAt`).
- **Grid preferences**: Cases view gets its own `useGridPreference` key (`'urls-cases'`) for sorting/columnVisibility/pageSize, independent of Domain view's existing `'urls'` key — different column sets, no reason to share one saved layout.
- **`+ Create Case` button** stays shared across both views (unchanged `AddUrlDialog` add-mode, `editing={null}`).

## Backend

### `internal/db/models.go`

```go
// CaseSummary is one row for the Cases view: a Case's own fields plus its
// Notice letter's fields (Notice chosen over Memo when both exist, same
// convention as current_reference_number) and every domain it covers.
// Not a persisted table.
type CaseSummary struct {
	ID                             uint       `json:"id"`
	AgencyID                       *uint      `json:"agency_id,omitempty"`
	AgencyName                     string     `json:"agency_name,omitempty"`
	Status                         string     `json:"status,omitempty"`
	DueDate                        *time.Time `json:"due_date,omitempty"`
	RequestedAt                    *time.Time `json:"requested_at,omitempty"`
	CreatedAt                      time.Time  `json:"created_at"`
	NoticeLetterID                 *uint      `json:"notice_letter_id,omitempty"`
	NoticeSubject                  string     `json:"notice_subject,omitempty"`
	NoticeWorkflowStatus           string     `json:"notice_workflow_status,omitempty"`
	NoticeReferenceNumberExternal  string     `json:"notice_reference_number_external,omitempty"`
	NoticeReferenceNumberInternal  string     `json:"notice_reference_number_internal,omitempty"`
	NoticeRecipient                string     `json:"notice_recipient,omitempty"`
	NoticeRequestor                string     `json:"notice_requestor,omitempty"`
	NoticeLetterDate               *time.Time `json:"notice_letter_date,omitempty"`
	NoticeReceivedAt               *time.Time `json:"notice_received_at,omitempty"`
	NoticeSubmittedAt              *time.Time `json:"notice_submitted_at,omitempty"`
	NoticeRemarks                  string     `json:"notice_remarks,omitempty"`
	MemoLetterID                   *uint      `json:"memo_letter_id,omitempty"`
	MemoSubject                    string     `json:"memo_subject,omitempty"`
	MemoReferenceNumberInternal    string     `json:"memo_reference_number_internal,omitempty"`
	Domains                        []CaseSummaryDomain `gorm:"-" json:"domains"`
}

type CaseSummaryDomain struct {
	URLID uint   `json:"url_id"`
	URL   string `json:"url"`
	Phase string `json:"phase"`
}
```

(Memo fields kept minimal — only what the edit form's existing Memo section already collects; extend later if the form grows.)

### `internal/db/store.go` (`CaseStore`)

- `ListCases(ctx context.Context) ([]CaseSummary, error)` — admin/global.
- `ListCasesForDepartment(ctx context.Context, departmentID uint) ([]CaseSummary, error)` — same split as `ListURLs`/`ListDepartmentURLs`.
- `UpdateCaseLetterFields(ctx context.Context, departmentID, letterID uint, fields CaseLetterFields) (bool, error)` — ownership resolved via the letter's `CaseID` → `GetCase`, same as `AddCaseLetter`. `CaseLetterFields` mirrors `CaseFields`'s optional-pointer partial-update shape, one field per `CaseLetter` column the edit form can touch (subject, workflow_status, reference_number_external/internal, recipient, requestor, letter_date, received_at, submitted_at, remarks).

### `internal/db/postgres.go`

`ListCasesForDepartment` follows `ListDepartmentURLs`'s exact shape:

```go
func (s *postgresStore) ListCasesForDepartment(ctx context.Context, departmentID uint) ([]CaseSummary, error) {
	var summaries []CaseSummary
	err := s.db.WithContext(ctx).
		Table("cases").
		Select(`cases.id, cases.agency_id, agencies.name as agency_name,
			cases.status, cases.due_date, cases.requested_at, cases.created_at,
			notice.id as notice_letter_id, notice.subject as notice_subject,
			notice.workflow_status as notice_workflow_status,
			notice.reference_number_external as notice_reference_number_external,
			notice.reference_number_internal as notice_reference_number_internal,
			notice.recipient as notice_recipient, notice.requestor as notice_requestor,
			notice.letter_date as notice_letter_date, notice.received_at as notice_received_at,
			notice.submitted_at as notice_submitted_at, notice.remarks as notice_remarks,
			memo.id as memo_letter_id, memo.subject as memo_subject,
			memo.reference_number_internal as memo_reference_number_internal`).
		Joins("LEFT JOIN agencies ON agencies.id = cases.agency_id").
		Joins(`LEFT JOIN case_letters notice ON notice.id = (
			SELECT cl.id FROM case_letters cl
			WHERE cl.case_id = cases.id AND cl.type IN ('Notice', 'Notice (Uplift)')
			ORDER BY cl.letter_date DESC LIMIT 1)`).
		Joins(`LEFT JOIN case_letters memo ON memo.id = (
			SELECT cl.id FROM case_letters cl
			WHERE cl.case_id = cases.id AND cl.type IN ('Memo', 'Memo (Uplift)')
			ORDER BY cl.letter_date DESC LIMIT 1)`).
		Where("cases.department_id = ?", departmentID).
		Order("cases.created_at desc").
		Scan(&summaries).Error
	if err != nil {
		return nil, err
	}
	// Domains: multi-valued, same merge-in-Go pattern ListDepartmentURLs
	// uses for RequestingDepartments — fetch case_urls+urls separately,
	// index by case id, attach.
	...
	return summaries, nil
}
```

`ListCases` is the same query without the `WHERE cases.department_id = ?` clause. Extract the shared Select/Joins/domain-merge into one unexported helper both call (mirrors how `ListDepartmentURLs`/`ListURLs` could but currently don't share — don't over-abstract beyond what's needed here: one helper, not a generic query builder).

### `internal/server/case_handlers.go`

- `ListCaseSummaries` — same admin/non-admin branch as `ListURLs` (`handlers.go`): `writeJSON(w, http.StatusOK, ...)` of `store.ListCases`/`ListCasesForDepartment`.
- `UpdateCaseLetter` — resolve `letter_id` + `id` (case id) path params, `GetCase` for ownership (same 404-not-403 pattern), decode a partial body (same optional-field shape as `AddCaseLetter`'s body struct but every field a pointer so "not present" vs "explicit empty string" is distinguishable — mirrors `UpdateCase`'s `*string`/`*uint` body), call `store.UpdateCaseLetterFields`, `204` on success.

### `internal/server/router.go`

```go
r.Get("/case-summaries", h.ListCaseSummaries)
r.Patch("/cases/{id}/letters/{letter_id}", h.UpdateCaseLetter)
```

Both inside the existing `requireAuth` group alongside `/cases/*`/`/case-letters` — no admin gating, same as the rest of the case routes.

## Frontend

### `web/src/api/types.ts`

- `CaseSummary` / `CaseSummaryDomain` types mirroring the Go structs above.

### `web/src/api/cases.ts`

- `fetchCaseSummaries(): Promise<CaseSummary[]>` → `GET /case-summaries` (nil-slice guard, same as every other list fetcher).
- `updateCaseLetter(caseId: number, letterId: number, fields: Partial<...>): Promise<void>` → `PATCH /cases/{caseId}/letters/{letterId}`.

### `web/src/routes/urls.tsx`

- `Route`'s `validateSearch`: `{ view: 'domains' | 'cases' }`, default `'domains'` (same shape as `domain.$url.tsx`'s `tab`).
- `ToggleGroup`/`ToggleGroupItem` (Domain / Cases) in the header, driving the search param via `navigate({ search: prev => ({ ...prev, view }) })`.
- `AddUrlDialog` gains `editing: CaseSummary | null`:
  - `useEffect` on `[open, editing]`: when `editing` is set, populate `value` (domain textarea) from `editing.domains.map(d => d.url).join('\n')`, `agencyId`, `phase` (from... actually there's no case-level "phase" separate from `status` at the case level — the dialog's existing `phase` field maps to `Case.Status` at creation; in edit mode it maps the same way), due-date fields aren't duration-based once a concrete `due_date` already exists — show the existing computed deadline as read-only text (existing `DUE_DATE_FMT`) with the duration picker available to *replace* it, external/internal ref from `editing.notice_reference_number_external/internal`, Notice/Memo subject+ref from the `notice_*`/`memo_*` fields, recipient/requestor/workflow status/dates/remarks from the Notice fields.
  - `handleSubmit` branches: `editing` present → `updateCase(editing.id, {...})` for case-level fields, then the same `richFields` object (recipient/requestor/workflow_status/dates/remarks — these are written identically to both letters at *create* time today, see `AddUrlDialog.handleSubmit`) applied via `updateCaseLetter` to **both** `notice_letter_id` and `memo_letter_id` when both exist, so editing keeps them in sync the same way creating does; `subject`/`reference_number_internal` update only their own letter. Any letter id that's `undefined` (case has no Memo yet, or — defensively — no Notice) falls back to `addCaseLetter` instead of `updateCaseLetter` for that one letter. Plus `addUrlToCase` for any domain line not in `editing.domains`. `editing` absent → today's create flow, unchanged.
  - Dialog title/description swap ("Edit Case" vs "Create Case"), same as `InstrumentFormDialog`.
- New `CasesTable` (or inline, mirroring `LatestScanTab`'s `DomainRow`/`ServerRow` shape in `results.index.tsx`):
  - `CaseRow = { kind: 'case'; summary: CaseSummary; subRows: DomainSubRow[] }`, `DomainSubRow = { kind: 'domain'; caseId: number; domain: CaseSummaryDomain }`.
  - Columns: Case #, Agency, Status, Due Date, Ref No. (external), Domains (count), Subject, Workflow Status, Action (edit icon, case rows only — blank/absent on domain subrows other than their own action cell below).
  - `getSubRows: r => r.kind === 'case' ? r.subRows : undefined`, `getExpandedRowModel()`.
  - Case row cells: all plain text/labels (`STATUS_OPTIONS.find(...).label`, `DUE_DATE_FMT`, etc. — same formatting helpers Domain view already has), except the Action cell (edit icon only).
  - Domain subrow cells: Status `<Select>` in the row's Status-column position (exact same optimistic-update-with-rollback `handleStatusChange`-style logic as Domain view, just addressed by `caseId` instead of re-deriving `case_id` from a `URLEntry`), Phase `<Select>` in a dedicated Phase column (`updateCaseURLPhase`), a Grip `Link` to `/domain/$url` and delete in the Action cell (reuses the existing `DeleteConfirmDialog` flow keyed by `url_id`).
- Domain view: unchanged from the current implementation (Grip link, Status select, delete — no edit icon).

## Naming Note (pre-existing, not introduced here)

Three distinct UI affordances end up touching only two underlying fields, and the naming doesn't disambiguate them — worth stating explicitly rather than leaving implicit:

1. The edit form's **"Phase"** field (inherited unchanged from `AddUrlDialog`, `CASE_PHASE_OPTIONS`) writes `Case.Status` — same field as (2) below, just a confusingly-named entry point that already exists today.
2. The domain subrow's **"Status"** `<Select>` (`STATUS_OPTIONS`, includes a blank "—") also writes `Case.Status` — same field as (1), reusing today's Domain-view naming/options list.
3. The domain subrow's **"Phase"** `<Select>` (`CASE_PHASE_OPTIONS`, no blank option) writes `CaseURL.Phase` — the actual per-domain override, distinct from (1) and (2).

Not renaming any of these in this pass (out of scope, and (1)'s naming predates this design) — just flagging it so implementation doesn't conflate (1)/(2) writing the same field with (3) writing a different one.

## Data Flow / Edge Cases

- A case with zero letters yet (rare — `CreateCaseForURL` always pairs with an immediate `AddCaseLetter` in the current `AddUrlDialog` flow, but defensive anyway): `notice_letter_id` is null, edit form's letter fields start blank, submit calls `addCaseLetter` (type `Notice`) instead of `updateCaseLetter` when there's no existing id to target.
- A case with a Memo but no Notice (shouldn't happen given the create flow, but the query's `LEFT JOIN`s tolerate it): `notice_*` fields are all blank; edit form still works, just creates a Notice letter on first save if the user fills anything in.
- Adding a new domain line in edit mode that's already on a *different* case: `addUrlToCase` doesn't care — a domain can belong to multiple cases, this just adds one more link. No special handling needed.
- Removing a domain line from the edit-mode textarea: explicitly a no-op (documented above) — the diff only adds, never removes, so deleting text from that field silently does nothing on save. Worth a one-line hint under the field ("removing a line here won't unlink it — remove a domain from Cases view instead") so this isn't a silent surprise.

## Testing

- `internal/db` tests (SQLite in-memory): `ListCasesForDepartment`/`ListCases` return the right Notice-vs-Memo letter picked when both exist, domains list correct for a multi-domain case, department scoping.
- Server handler tests: `UpdateCaseLetter` ownership check (404 for another department's case, admin bypass), partial-update semantics (touching only `subject` doesn't clobber `workflow_status`).
- Manual `dev.sh` verification: create a multi-domain case via `+ Create Case`, switch to Cases view, confirm it groups correctly with the right domain count; expand it, confirm Phase edits per domain persist; edit the case via the pencil icon, confirm every field prefills, change agency + Notice subject, save, confirm both persisted; add a new domain line during edit, confirm it's linked to the case afterward; add a bogus domain line then remove it before saving, confirm nothing broke.

## Out of Scope

- Domain removal *from a case* via the edit form (no backend support — flagged above).
- Cross-case domain move/merge tooling.
- Docs page (`docs.tsx`) changes — untouched by this work, though `CaseSummary`'s Notice-letter fields overlap conceptually with what `case-letters` already exposes; no attempt made here to unify the two read models.
