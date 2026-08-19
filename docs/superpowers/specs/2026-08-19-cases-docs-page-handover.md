# Cases/Docs UX Refresh — Handover

> **Status:** hand-off document, not an implementation plan. Written for
> whichever agent picks up next. Captures (1) exactly what's shipped and
> tested as of 2026-08-19, (2) how the system works today, and (3) the new
> ask from the user, with open design questions flagged rather than
> pre-decided. **Run `superpowers:brainstorming` with the user before
> writing an implementation plan** — several of the open questions below
> change the shape of the schema/API work, and nothing here has been
> confirmed with them yet.

## 1. What's shipped (as of this doc, all on `main`)

The `cases`/`case_letters`/`case_urls` migration described in
`docs/superpowers/plans/2026-08-19-cases-schema-00-orchestration.md` is
**fully merged and tested** — all 6 tasks (foundation, store layer, API
handlers, frontend, CRD import, CMOD import). `go build ./...`,
`go test ./...`, and `cd web && npm run build` all pass clean on `main`.
Two things happened *after* that orchestration run finished that the plan
docs don't mention, both already merged:

- **Fixed a real authorization bug in Task 03** before merging: `POST
  /api/cases/{id}/letters` was checking whether the caller's department
  *watches the case's URL* (`requireDomainOwnership`) instead of whether it
  *owns the case* (`cases.department_id`). Since this app deliberately lets
  multiple departments share watchlist visibility on the same normalized
  domain, that let any department watching a domain add letters to another
  department's case. Fixed to use `db.Store.GetCase`'s `DepartmentID`
  instead; see `internal/server/case_handlers.go`'s `AddCaseLetter`.
- **Extended `CaseHistoryDialog`'s "Open New Case" form** (commit
  `cc79038`) to also collect a Letter Type + Reference Number and call
  `POST /api/cases/{id}/letters` right after `POST /api/cases/*url`, in one
  step. Originally shipped with *only* a phase selector — a case has no
  number of its own (it lives on the first `CaseLetter`), so there was no
  way to actually record a case/reference number through the UI at all.
  This was live-verified working end-to-end via Orca's embedded browser
  (create case → letter with ref number → grid's "Reference No." column
  picks it up on reload).

**Nothing imported yet** — the CRD/CMOD import CLIs (`cmd/import-crd`,
`cmd/import-cmod`) are built, tested, and dry-run-verified against the real
Excel files, but `--dry-run=false` has never been run. That's intentional,
per both import plans — pending product sign-off on status mapping,
category/offence splitting, and the `oic_user_id`↔`users` name-matching
question (see the two clarifications docs below). **Don't run a real
import without explicit user sign-off.**

Two manually-created test cases exist in the local dev DB from live
verification (`github.com`, Case #5 no ref number, Case #7
`MCMC(S)CRD/2026/TEST-001`) — harmless, but worth knowing about if dev-DB
state looks unexpectedly non-empty.

## 2. How the system works today

### Schema (`docs/db-schema.dbml`, `internal/db/models.go`)

```
cases            — thin anchor: id, department_id, created_at.
                   One row per real-world case/request. Every actual fact
                   (ref numbers, letters, subject, OIC...) lives on
                   case_letters, not here.

case_letters     — one row per actual letter/document. type (Memo | Notice
                   | Memo (Uplift) | Notice (Uplift)), reference_number,
                   workflow_status (CMOD-only: Draft | Pending Legal |
                   Pending TSC | Submitted), letter_date, submitted_at,
                   subject, oic_user_id (FK users, unverified name-match),
                   requestor, remarks. A block+uplift case gets up to 4
                   rows; a CRD-sourced case typically gets 1.

case_urls        — many-to-many join, cases <-> urls. (case_id, url_id) PK,
                   plus its own `phase` (requested | uplift | suspended) —
                   carried here, not on case_letters, because two URLs in
                   the same case can reach different outcomes (confirmed:
                   2/65 CMOD cases, 96/1841 CRD ref-groups have mixed
                   status across their own URLs).
```

Key relationship: **one case can cover many URLs, and one URL can belong to
many cases over time** (reblocked under a new reference). `urls.url` stays
the unique bare-hostname row; `urls.reference_number`/
`urls.requesting_dept_id` are **gone** — replaced by querying
`case_letters`/`case_urls` (see `ListDepartmentURLs`'s derived
`current_reference_number`/`requesting_departments` columns,
`internal/db/postgres.go`).

`urls.status`/`due_date`/`agency_id` remain plain scalars on `urls` —
**deliberately not folded into this schema**, per the still-open question
in `docs/db-schema.dbml`'s header. Don't fold them in as part of this work
either unless the user explicitly asks for it.

### Backend (`internal/db/store.go`'s `CaseStore`, `internal/server/case_handlers.go`)

```go
CreateCase(ctx, departmentID, urlID uint, phase string) (Case, error)   // links to exactly ONE url at creation
AddCaseLetter(ctx, letter CaseLetter) (CaseLetter, error)
ListCasesForURL(ctx, urlValue string) ([]CaseWithLetters, error)        // ownership-agnostic; handler checks
GetCase(ctx, id uint) (Case, error)                                     // for ownership checks
```

Routes, all under `requireAuth` (not admin-gated), department-ownership
scoped (404-not-403):

- `GET /api/cases/*url` (`CasesByURL`) — every case covering a URL, via `requireDomainOwnership`.
- `POST /api/cases/*url` (`CreateCaseForURL`) — body `{"phase": "requested"|"uplift"|"suspended"}`; `department_id` is always the caller's own, never client-supplied.
- `POST /api/cases/{id}/letters` (`AddCaseLetter`) — keyed by the case's own surrogate ID (not URL); ownership via `GetCase`'s `DepartmentID` (see the bug fix above).

**Important gap:** there is no endpoint to list `case_letters` across
*all* domains/cases — only per-URL (`GET /api/cases/*url`). Anything like
a flat "Docs" table needs a new list endpoint + store method (see §4).

**Also a gap:** `CreateCase`'s signature only accepts one `urlID`. There is
no store method or route to attach an *additional* URL to an existing case
after creation — so today's schema can technically model "one Notice
covering 10 URLs" (`case_urls` is many-to-many) but nothing in the API
actually lets a user build that shape. Every case created through the
current UI ends up covering exactly one URL.

### Frontend

- `web/src/routes/urls.tsx` (navbar: "Watchlist") — the domain grid. Has
  two new read-only columns, `current_reference_number`/
  `requesting_departments`, derived server-side. A per-row "Cases" action
  opens `CaseHistoryDialog`.
- `web/src/components/case-history-dialog.tsx` — per-*domain* dialog: lists
  that domain's cases (read-only, with their letters), and a form to open
  a new case (phase + letter type + reference number, see §1). No
  letter-editing, no per-letter delete, no way to add a second letter to
  an existing case, no way to attach an additional URL to a case. All
  deliberately out of scope per the original plan (`2026-08-19-cases-schema-04-frontend.md`'s "Scope, deliberately minimal" note) — extend as this
  new work requires.
- `web/src/routes/AddUrlDialog` (top of `urls.tsx`) — batch-adds one or
  more domains at once, applying one staged set of offences/agency/status
  to *every* domain in the batch (see `web/CLAUDE.md`). **Does not touch
  cases at all today** — this is the integration point the user is asking
  for in §3.

## 3. The new ask (from the user, verbatim intent)

> refurnish the user process and clarify how does this system work.
> system should have a standalone Docs page. This page will replicate the
> cmod existing excel view. the layout is similar like the current urls
> page but its focuses on docs (memo, letter). user should link these
> case with the domain in url page. for now i intend to have user to add
> case to when adding the domain.

Breaking that into concrete asks:

1. **New standalone "Docs" page** (own route + navbar entry), grid-layout
   like `urls.tsx` (TanStack Table, filters, search — same visual/interaction
   language), but each row is a **document** (`case_letters` row: Memo or
   Notice), not a domain.
2. **"Replicate the CMOD existing Excel view"** — the target column set is
   the `BLK` sheet's columns, cross-referenced against what actually made
   it into `case_letters`:
   ```
   BLK sheet:  No, Letter Date, Recipient, Type, Subject, Reference No,
               OIC, Requestor, Offence, Link (One Link Per Row), Remarks,
               Agency, Status, Received, Submission
   case_letters: type, reference_number, workflow_status, letter_date,
               submitted_at, subject, oic_user_id, requestor, remarks
   ```
   `Recipient`, `Offence`, `Agency`, `Received` have **no column on
   `case_letters`** today (see `docs/cmod-blocking-list-migration-clarifications.md` §6 — this was already flagged as an open question during the
   original migration and never resolved). If the Docs page needs to show
   these, decide with the user whether to add columns to `case_letters`,
   derive them (e.g. `Agency`/`Offence` from `url_offences`/`Category` via
   the linked URL), or drop them. `Link` is the linked domain(s) —
   `case_urls`, already modeled, just needs a list endpoint.
3. **Link cases to a domain from the URL page** — this already exists in
   a limited form (`CaseHistoryDialog`'s per-row "Cases" action + "Open New
   Case" form). Clarify with the user whether "refurnish" means improving
   *that* flow, or something structurally different (e.g. a domain should
   show which case(s) it's under inline in the grid, not just behind a
   dialog).
4. **Add a case at domain-add time** — `AddUrlDialog` currently stages
   offences/agency/status and applies them to every domain in the batch on
   submit (see `MultiOffencePicker`, `urls.tsx:140-335`). The user wants
   the same for cases: pick/create a case while adding a domain, not only
   afterward via the per-row dialog.
   **Open design question:** when adding *multiple* domains in one batch,
   does "add case" create **one case covering all of them** (`case_urls`
   already supports this — matches CMOD's real-world "10 URLs in one
   Notice" pattern, and would finally use the many-to-many shape for real)
   or **one case per domain** (matches the current `CreateCase(urlID
   uint, ...)` single-URL signature, no backend change needed, but
   diverges from how CMOD's actual casework looks)? This decides whether
   `CreateCase`'s signature needs to change to accept multiple URL IDs, or
   whether a new `AddURLToCase` store method/route needs to exist instead.
   **Ask the user directly — this is the single biggest fork in the design.**

## 4. Concrete gaps to design before implementation planning

- No backend endpoint to list `case_letters` (or cases) across many
  domains/departments at once — needed for the Docs page's grid data
  source. Likely shape: `GET /api/case-letters` or `GET /api/docs`,
  department-scoped like `ListDepartmentURLs` (admin: global, non-admin:
  own department's cases only via `cases.department_id`), each row
  carrying its case's linked URL(s) (join through `case_urls`/`urls`).
  Needs pagination given CMOD alone is ~500 rows and CRD ~38k rows
  pre-collapse (1,841 cases) — don't ship an unbounded `SELECT *`.
- `Recipient`/`Offence`/`Agency`/`Received` have no column — decide
  add-vs-derive-vs-drop per field (§3.2).
- No way to attach more than one URL to a case, or add a second letter to
  an existing case, through any UI today — both needed if the Docs
  page/domain-linking rework wants to actually reflect CMOD's real
  one-case-many-letters-many-urls shape.
- Once real (non-dry-run) CRD/CMOD imports eventually run, the Docs page
  will be the first place that data is actually visible/usable — worth
  sequencing that import sign-off conversation alongside this work rather
  than fully independently, since testing the Docs page against only the
  two hand-created dev-DB test cases won't surface real-data issues (the
  known messy cases: comma-joined ref numbers, multi-URL letters, the 65
  CMOD/1841 CRD case counts, etc. — see both clarifications docs).

## 5. Relevant files

- Schema: `docs/db-schema.dbml` (search `Table cases`/`case_letters`/`case_urls`)
- Backend: `internal/db/store.go` (`CaseStore`), `internal/db/cases.go`, `internal/server/case_handlers.go`, `internal/server/router.go`
- Frontend: `web/src/routes/urls.tsx`, `web/src/components/case-history-dialog.tsx`, `web/src/api/cases.ts`, `web/src/api/types.ts`
- CMOD source structure + open questions: `docs/cmod-blocking-list-migration-clarifications.md`
- CRD source structure + open questions: `docs/blocking-list-migration-clarifications.md`
- Import CLIs (reference for real-world data shape/edge cases already handled): `internal/blockimport/cmod.go`, `internal/blockimport/crd.go`
- Original migration plans (context on why the schema looks the way it does): `docs/superpowers/plans/2026-08-19-cases-schema-*.md`
