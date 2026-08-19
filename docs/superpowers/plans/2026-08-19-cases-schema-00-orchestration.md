# Cases/Case Letters/Case URLs Migration — Orchestration

> Meta-document for the human/orchestrator running this as parallel worktrees.
> Not itself an implementation plan — see the six numbered plans below.

**Goal:** Ship the `cases`/`case_letters`/`case_urls` schema from
`docs/db-schema-proposed.dbml` (design finalized 2026-08-19 — see that file's
header for the resolved decisions on `requesting_dept_id` and
`case_letters.type`), wired end-to-end (models → store → API → frontend),
plus one-off importers for both departments' Excel case history.

**Why split this way:** CRD (`internal/db/CLAUDE.md`'s "one case per domain,
domain-block" model) and CMOD (memo/document tracking) both need to land on
the same schema without either department's workflow blocking the other's.
Six independently-mergeable units, each sized to fit a fresh agent's context
without needing this conversation's history — every plan below is
self-contained.

## Task list & worktrees

| # | Plan file | Branch | Depends on |
|---|---|---|---|
| 01 | `2026-08-19-cases-schema-01-foundation.md` | `cases-schema/01-foundation` | — |
| 02 | `2026-08-19-cases-schema-02-store-layer.md` | `cases-schema/02-store-layer` | 01 merged |
| 03 | `2026-08-19-cases-schema-03-api-handlers.md` | `cases-schema/03-api-handlers` | 02 merged |
| 04 | `2026-08-19-cases-schema-04-frontend.md` | `cases-schema/04-frontend` | 03 merged |
| 05 | `2026-08-19-cases-schema-05-crd-import.md` | `cases-schema/05-crd-import` | 01 merged |
| 06 | `2026-08-19-cases-schema-06-cmod-import.md` | `cases-schema/06-cmod-import` | 01 merged |

```
01 (foundation)
 ├─→ 02 (store layer) ─→ 03 (API handlers) ─→ 04 (frontend)
 ├─→ 05 (CRD import)         [independent of 02/03/04 and of 06]
 └─→ 06 (CMOD import)        [independent of 02/03/04 and of 05]
```

Five worktrees run this migration end to end: 01 is the sequential root;
once it merges, 02→03→04 (one chain) and 05 and 06 branch off it and can run
concurrently. 05/06 deliberately do **not** share code or depend on 02 —
each is a standalone `cmd/` binary reading straight off the models Task 01
adds, so a stalled or reworked API layer never blocks either import.

## Worktree setup (per task)

Use `superpowers:using-git-worktrees` (native tool if available, else
`.worktrees/<branch>` fallback) for each task, e.g.:

```bash
git worktree add .worktrees/cases-schema-01-foundation -b cases-schema/01-foundation
```

Task 02/03/04/05/06 branch from `main` **after** their listed dependency has
merged to `main` — not from each other's unmerged branches. If a downstream
task's worktree is created before its dependency merges, rebase it onto
`main` once the dependency lands, before that task's agent starts writing
code.

## Merge order

1. Merge 01 first, alone — everything else imports its model/migration
   changes.
2. Merge 05 and 06 any time after 01 (either order, they touch disjoint new
   files — `cmd/import-crd/` vs `cmd/import-cmod/` — so no conflict risk
   between them).
3. Merge 02, then 03, then 04, strictly in that order — each is a real code
   dependency on the previous (03's handlers call 02's store methods, 04's
   frontend calls 03's endpoints), not just a suggested ordering.

Each task's own plan ends with its independent test/build verification —
run that in the worktree before handing it back for merge. The orchestrator
does not need to re-run cross-task integration tests until after 04 merges
(the first point the full stack is wired together); at that point run
`go build ./...`, `go test ./...`, and `cd web && npm run build` once on
`main` as a final sanity check.

## What's deliberately out of scope here

- Deriving `urls.status`/`due_date`/`agency_id` the same way — left as a
  scalar, per the still-open question in the schema doc's header. Don't
  fold it into any of these six tasks.
- Cross-department reconciliation UI (surfacing when a CRD case and a CMOD
  case cover the same domain via `notice_ref_number` or domain-name
  overlap) — both importers write independent `cases` rows per department,
  by design (see Task 05/06's notes). A reconciliation view is a plausible
  follow-up, not part of this migration.
- Any change to `urls.status`/`agency_id`/`due_date` edit UI in `urls.tsx`
  — Task 04 only touches the reference-number and requesting-department
  parts of that page.
