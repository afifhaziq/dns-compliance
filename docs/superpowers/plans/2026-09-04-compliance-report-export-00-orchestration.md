# Exportable Compliance Report (CRD + CMOD Excel Export) — Orchestration

> Meta-document for the human/orchestrator running this as parallel
> worktrees (or parallel subagents). Not itself an implementation plan —
> see the eight numbered plans below.
>
> **Note on how these plans were produced:** the user asked for this
> broken down via Orca orchestration (`/orchestration`), but Orca's CLI
> (`orca-ide`) was blocked by the host's endpoint security (Cylance
> Script Control rejects the PowerShell bridge it shells out to) —
> unrelated to this repo, needs to be fixed on the Windows host, not
> worked around. These eight plans were produced instead by three
> parallel `fork` subagents (one per independent chain below), following
> `superpowers:writing-plans` conventions and the
> `2026-08-19-cases-schema-00-orchestration.md` precedent's numbered-plan
> shape, so they can still be handed to separate worktrees/agents the
> same way that migration was.

**Goal:** Ship the "Exportable compliance report" TODO from the root
`CLAUDE.md` — let a user download the current app data as `.xlsx`,
shaped to match the two legacy Excel files CRD and CMOD already track
blocking cases in, per
`docs/superpowers/specs/2026-09-04-compliance-report-export-design.md`
(read that spec first — every plan below implements one slice of it and
assumes it as background).

**Why split this way:** The design spec has three largely-independent
parts — an OIC-user field prerequisite (Part 1), the export backend
(Part 2), and the frontend export UI (Part 3) — with the export backend
and frontend further splitting along CRD-vs-CMOD and client-vs-per-page
lines. Eight independently-mergeable units, each sized to fit a fresh
agent's context (~150k tokens) without needing this conversation's
history — every plan is self-contained, includes the exact current code
it modifies, and states its own dependency.

## Task list & worktrees

| # | Plan file | Branch | Depends on |
|---|---|---|---|
| 01 | `2026-09-04-compliance-report-export-01-oic-backend.md` | `compliance-export/01-oic-backend` | — |
| 02 | `2026-09-04-compliance-report-export-02-oic-frontend.md` | `compliance-export/02-oic-frontend` | 01 merged |
| 03 | `2026-09-04-compliance-report-export-03-export-crd.md` | `compliance-export/03-export-crd` | — |
| 04 | `2026-09-04-compliance-report-export-04-export-cmod.md` | `compliance-export/04-export-cmod` | — |
| 05 | `2026-09-04-compliance-report-export-05-export-routes.md` | `compliance-export/05-export-routes` | 03 merged AND 04 merged |
| 06 | `2026-09-04-compliance-report-export-06-export-frontend-client.md` | `compliance-export/06-export-frontend-client` | 05 merged |
| 07 | `2026-09-04-compliance-report-export-07-export-ui-urls.md` | `compliance-export/07-export-ui-urls` | 06 merged |
| 08 | `2026-09-04-compliance-report-export-08-export-ui-docs.md` | `compliance-export/08-export-ui-docs` | 06 merged |

```
01 (oic-backend) ─→ 02 (oic-frontend)              [independent chain]

03 (export-crd)  ─┐
                   ├─→ 05 (export-routes) ─→ 06 (export-frontend-client) ─┬─→ 07 (export-ui-urls)
04 (export-cmod) ─┘                                                       └─→ 08 (export-ui-docs)
```

Two independent chains run concurrently from the start:

- **OIC chain (01→02):** the OIC field is a prerequisite for a
  *non-empty* CMOD export (Column 7, "OIC"), per the design spec — but
  it is not a *code* dependency of Plans 03-08. It can merge before,
  after, or interleaved with the export chain; the only real deadline is
  "before anyone relies on the CMOD export's OIC column actually being
  populated for newly-created letters."
- **Export chain (03,04→05→06→{07,08}):** 03 and 04 are independent of
  each other (disjoint new files in `internal/blockexport/`, see 05's
  note on a possible trivial `go.mod` conflict from both promoting
  `excelize/v2` to a direct dependency — keep either version). 05 is a
  real code dependency on both (calls their `Flatten*`/`Write*`
  functions). 06 is a real code dependency on 05 (calls the two new HTTP
  routes). 07 and 08 both depend on 06 (need `exportCaseSummaries`/
  `exportCaseLetters`/`downloadBlob`) but are independent of each other
  (disjoint files — `urls.tsx` vs `docs.tsx`).

Six worktrees can be active at once at peak: 01, 03, 04 start
immediately; 02 starts once 01 merges; 05 starts once both 03 and 04
merge; 06 starts once 05 merges; 07 and 08 start together once 06
merges.

## Worktree setup (per task)

Use `superpowers:using-git-worktrees` (native tool if available, else
`.worktrees/<branch>` fallback) for each task, e.g.:

```bash
git worktree add .worktrees/compliance-export-01-oic-backend -b compliance-export/01-oic-backend
```

Each task's worktree branches from `main` **after** its listed
dependency has merged to `main` — not from another task's unmerged
branch. If a downstream task's worktree is created before its dependency
merges, rebase it onto `main` once the dependency lands, before that
task's agent starts writing code.

## Merge order

1. **01 and 03 and 04** can merge any time, in any order, independently
   of each other and of everything else (01 touches
   `internal/db`/`internal/server` OIC fields only; 03/04 add disjoint
   new files under `internal/blockexport/`).
2. **02** merges only after 01.
3. **05** merges only after both 03 and 04 — real code dependency, not
   just ordering. If both 03 and 04 independently promoted
   `github.com/xuri/excelize/v2` from `// indirect` to direct in
   `go.mod`, a merge conflict there is cosmetic — keep either version,
   then run `go mod tidy` once on `main` to confirm it's settled.
4. **06** merges only after 05 — calls its two new routes.
5. **07 and 08** merge only after 06, in either order — disjoint files,
   no conflict risk between them.

Each task's own plan ends with its independent test/build verification —
run that in the worktree before handing it back for merge. Run a full
`go build ./... && go test ./...` on `main` after 05 merges (first point
the whole export backend is wired together), and
`go build ./... && go test ./... && cd web && npm run build` on `main`
once more after 08 merges (first point the whole feature, OIC included
if 02 has also landed by then, is wired end-to-end).

## What's deliberately out of scope here

Per the design spec's own "Non-goals / explicitly out of scope" section
— repeated here so no task accidentally picks any of this up:

- Fixing `internal/blockimport`'s gaps (Agency/Category/Element/
  Citation/OIC not persisted for historical rows) — separate work,
  blocked on sign-offs tracked in
  `docs/blocking-list-migration-clarifications.md` and
  `docs/cmod-blocking-list-migration-clarifications.md`. Exported
  historical rows will show these columns blank — expected, not a defect
  in any of these eight plans.
- An OIC *column* in the `docs.tsx` grid itself (Plan 02 only touches
  the Add/Edit Document dialogs).
- PDF export, or any format beyond `.xlsx`.
- Correcting the root `CLAUDE.md` TODO's stale "nothing imported yet"
  line — a separate, quick follow-up.
