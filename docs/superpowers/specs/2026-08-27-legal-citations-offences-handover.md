# Legal Citation Catalog + Offence Columns — Handover

> **Status:** hand-off document, not an implementation plan. Written for
> whichever agent picks up next. Captures (1) exactly what changed in this
> session and why, (2) how the affected pieces work today, and (3) one
> concrete open gap the user flagged but did not yet ask to be fixed.

## 0. Scope note — don't confuse this session's diff with the pre-existing one

`git status` on `main` already showed a large set of modified files
(`internal/blockimport/*`, `internal/db/cases.go`, `db.go`, `migrate.go`,
`store.go`, `internal/server/case_handlers.go`, `router.go`,
`web/src/routes/docs.tsx`, `web/src/api/cases.ts`,
`web/src/lib/case-options.ts`, the deleted
`web/src/components/case-history-dialog.tsx`, the three `CLAUDE.md` files,
`docs/db-schema.dbml`) **before this session started** — that's unrelated,
still-uncommitted work from an earlier `urls-cases-view` session. Nothing in
this document touches that diff or explains it.

This session's *own* edits are confined to:

- `internal/db/models.go` — `OffenceEntry` struct, `URLEntry.Offences`,
  `CaseSummaryDomain.Offences`, `Instrument.Type` doc comment.
- `internal/db/postgres.go` — `offencesByURLIDs` helper, wired into
  `ListDepartmentURLs`.
- `internal/db/cases.go` — `offencesByURLIDs` wired into
  `listCaseSummaries`.
- `web/src/api/types.ts` — `OffenceEntry` type, `Instrument.type` gains
  `'REGULATION'`.
- `web/src/routes/urls.tsx` — `joinedCell` helper, 4 new offence columns ×
  2 views (Domain view + Cases view).
- `web/src/routes/legal-citations.tsx` — `REGULATION`/"Peraturan" instrument
  type option.

None of this session's changes are committed yet. `go build ./...`,
`go test ./internal/db/... ./internal/server/...`, and `cd web && npx tsc
--noEmit` all pass clean as of this doc.

## 1. What shipped this session

### 1a. "Peraturan" (Regulation) instrument type

`Instrument.Type` (`internal/db/models.go`) is a **plain unconstrained
string column** — no DB enum, no backend validation (`legal_handlers.go`'s
`instrumentBody.valid()` only checks non-empty, not membership in a fixed
set). The only place a fixed list exists is the frontend dropdown
(`legal-citations.tsx`'s `INSTRUMENT_TYPES`/`INSTRUMENT_TYPE_LABELS`), so
adding a new type is a frontend-only, two-constant change — no migration.

Added `REGULATION` → Malay label "Peraturan", alongside the existing
`ACT`/`ORDINANCE`/`ENACTMENT`/`SUBSIDIARY`/`CONSTITUTION`. Verified against
official sources this is legally correct: Malaysian subsidiary legislation
("Peraturan-peraturan...", e.g. the Control of Drugs and Cosmetics
Regulations 1984) is gazetted separately from Acts under its own **P.U.(A)
###/YY** numbering (Legislative Supplement A), made under an enabling
section of a parent Act but **not modeled as a parent-child link** in this
schema — `Instrument` has no `ParentInstrumentID`. A Peraturan is its own
top-level `Instrument` row; "Peraturan 7(1)(a)" as a *citation* plays the
exact same structural role "Seksyen 233(1)(a)" plays under an Akta —
`internal/legalcite/parser.go:77`'s `provisionRe` already treats
`seksyen|peraturan|perkara|fasal` as interchangeable synonyms feeding the
same `ProvisionNum`/`SubProvision`/`Paragraph` fields, so no parser change
was needed.

### 1b. Sample legal catalog data (live dev DB, not a migration)

Extracted the `Butiran Kesalahan` column (col D) from `Blocking Full List_1.xlsx`
(189 unique values across 38,147 rows — see repo-root `CLAUDE.md`'s TODO on
the still-pending CRD/CMOD import) via raw zipfile/XML parsing (no
`openpyxl`/`pip` available in this environment). Picked 10 for a diverse
spot-check and created them through the running dev server's real API
(`POST /api/legal/instruments`, `POST /api/legal/citations`,
`POST /api/legal/citations/parse-preview`) — **not a seed script, not
committed anywhere**, just live rows in the local Postgres `dns_compliance`
DB. 7/10 parsed clean (`OK`); 3 correctly flagged `NEEDS_REVIEW` by
`internal/legalcite.Parse` (a "dan"-joined multi-section citation, a stray
space before a paragraph marker, and an instrument short title with its own
parentheses confusing the paragraph parser) — this is expected, working
behavior, not a bug.

One environment note worth passing on: mid-session the local
`dns-compliance-postgres-1` container restarted (own named volume
`pgdata`, so not expected to lose data) and one earlier test row
(`Peraturan-peraturan Kawalan Dadah dan Kosmetik 1984`, instrument id 3) was
lost — root cause not chased down (not reproduced again, no test suite
runs against this DSN as far as was checked). Not something this doc's
changes caused; flagging in case it recurs.

Also demonstrated (per user question) that `Instrument.ShortTitle` is
**display-only** — `GetOrCreateInstrument`'s dedup key
(`internal/db/legalcite.go:18-27`) is `(type, jurisdiction, number, year)`
only, short_title plays no role in matching/parsing, and it's edited via
the existing pencil icon on `/legal-citations`' tree → `PATCH
/api/legal/instruments/{id}` (full-replace).

### 1c. Offence columns on the Domain view and Cases view

**Ask:** show, per domain/case, the offences attached to it, derived from
the legal citation catalog — evolved over the conversation from "add a
column" → split into 4 columns matching the Excel's own
Butiran-Kesalahan/Kategori/Elemen/Sub-Elemen layout → English headers
("Offence Details"/"Category"/"Element"/"Sub-Element") → full per-instance
wrapping instead of "+N more" truncation.

**Backend shape** (`OffenceEntry{Citation, Category, Element, SubElement}`,
all plain strings, `Element`/`SubElement` `omitempty`):

```go
// internal/db/postgres.go
func (s *postgresStore) offencesByURLIDs(ctx, urlIDs []uint) (map[uint][]OffenceEntry, error)
```

Joins `url_offences → categories → citations` (+ `LEFT JOIN elements`/
`sub_elements`), grouped by `url_id`. Called once per listing (not
per-row) from:

- `ListDepartmentURLs` (`postgres.go`) — same pattern as the existing
  `RequestingDepartments` batch (multi-valued, "not a scalar subquery"
  reasoning documented inline) — backs `URLEntry.Offences` on `GET
  /api/urls`.
- `listCaseSummaries` (`cases.go`) — backs `CaseSummaryDomain.Offences` on
  `GET /api/case-summaries`, keyed off the same `case_urls` join query that
  already builds each case's domain list.

**Frontend** (`web/src/routes/urls.tsx`):

- `joinedCell(values: (string | undefined)[])` — shared helper, renders
  one `<span>` per non-empty value stacked in a `flex flex-col`, or `—` if
  none. No truncation, no tooltip; the row grows to fit (`.col-status` has
  no fixed height/overflow constraint in `index.css`, confirmed by reading
  it before relying on wrapping working).
- Domain view (`columns`): 4 new columns inserted between
  `requesting_departments` and `status` — `offence_citation`,
  `offence_category`, `offence_element`, `offence_sub_element`, headers
  "Offence Details"/"Category"/"Element"/"Sub-Element".
- Cases view (`caseColumns`): same 4 columns inserted between `status` and
  `due_date`, following the exact case-row-blank/domain-subrow-populated
  convention the existing `status` column already uses (a case's domains
  can each carry different offences, so there's no single case-level
  value).

Verified end-to-end against the live dev server+DB (attached a "Lucah"
category under the Akta Komunikasi dan Multimedia 1998 citation to
`google.com`, confirmed both `GET /api/urls` and `GET /api/case-summaries`
return the structured `OffenceEntry` correctly) — not just typechecked.

## 2. Open item — NOT yet implemented, needs a decision or a go-ahead

**The user asked "when I open the edit form for case #10, I can't edit the
offences/categories, etc — same case shares those details, right?"**
Answer given (verified by tracing the code, not asserted): **no**, offences
are not case-level. `URLOffence.URLID` FKs to a domain, not a case — a case
is just `CaseURL`, a many-to-many join to domains. Two concrete gaps this
surfaced:

1. `AddUrlDialog` in edit mode (opened via the Cases-view case row's pencil
   icon, `urls.tsx:1585`, `setEditingCase`) still renders the
   `MultiOffencePicker`, but its own label says "attaches to every domain
   added above" (`urls.tsx:211`) — picks made there only attach to
   *newly-typed* domains added during that edit session
   (`createdDomains.flatMap(u => allOffences.map(...))`,
   `urls.tsx:510`). There is no way, from this dialog, to view or change
   offences already attached to the case's *existing* domains.
2. The Cases-view domain subrow's Action column
   (`caseColumns`'s `action` cell, `urls.tsx:1590-1596`) has **only** a
   "View details" link — no edit affordance at all. The only place that
   actually has a working offence editor is `EditUrlDialog`
   (`urls.tsx:846`, real "Edit Offences" section, `fetchOffencesByUrl` +
   add/remove), reachable only from the **Domain view**'s row pencil icon.

**Proposed fix, offered to the user but not yet confirmed:** add an edit
action to the Cases-view domain subrow that opens the same `EditUrlDialog`
(or a lighter offences-only variant) for that one domain, so a user
working from the Cases view doesn't have to switch to the Domain view to
fix a specific domain's offences. **Get explicit confirmation before
building this** — it's a real UI addition (a case row's own "Edit" already
means something different, per §1's `setEditingCase`, so the new
domain-subrow action needs a distinct label/icon to not read as "edit the
case").

## 3. Relevant files

- Legal citation catalog: `internal/db/legalcite.go`, `internal/server/legal_handlers.go`, `internal/legalcite/parser.go`, `web/src/routes/legal-citations.tsx`, `web/src/api/legal.ts` — see the `legal-citation-catalog` skill for the full 5-level hierarchy (Instrument→Citation→Category→Element→SubElement).
- Offence columns: `internal/db/models.go` (`OffenceEntry`, `URLEntry`, `CaseSummaryDomain`), `internal/db/postgres.go` (`offencesByURLIDs`, `ListDepartmentURLs`), `internal/db/cases.go` (`listCaseSummaries`), `web/src/routes/urls.tsx` (`joinedCell`, `columns`, `caseColumns`), `web/src/api/types.ts`.
- Excel source (gitignored, repo root): `Blocking Full List_1.xlsx`, sheet `2011-2026`, header row 14, `Butiran Kesalahan` = column D.
- Crawler↔server ports (asked about this session, no code changed): server's gRPC receiver `:50051` (crawler → server, `ComplianceService.Submit`); crawler's control listen address `:50052` (server → crawler, `CrawlerControl.StartSweep`) — see `docker-compose.yml`'s `CRAWLER_ADDR`/`--listen-addr` and repo-root `CLAUDE.md`'s gRPC section.
