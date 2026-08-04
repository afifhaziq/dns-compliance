# Multi-Offence Watchlist Design

**Date:** 2026-08-04
**Branch:** main (design phase — not yet branched)

## Overview

`urls.tsx`'s Add Domain dialog currently supports attaching exactly **one** offence (one Instrument→Citation→Category→Element chain, via `OffencePicker`) per submission, applied uniformly to every domain in the textarea. A domain can genuinely violate multiple sections/offences at once — real MCMC blocking-list data cites domains like `"Seksyen 4(1), 7(a), 9 dan 12 Enakmen..."`, i.e. one domain, four distinct offences. This adds support for attaching multiple offences to a domain, both at creation time and afterward on an already-listed watchlist row.

Scoped to the watchlist page (`urls.tsx`) only — not `domain.$url.tsx` (out of scope, see below).

## Background

A verification pass on `internal/legalcite.Parse` (see `legalcite-verify/docs/legalcite-verification-report.md`) found that "dan"/","/"&"-joined citation lists in the source data aren't a parsing edge case to smooth over — they're MCMC recording that one domain has multiple offences. The correct fix isn't teaching the parser to cram N provisions into one `Citation`; it's giving analysts a way to attach N separate offences to one domain, which is what this spec builds. The parser's `NEEDS_REVIEW` flag on such input is and remains the correct behavior — it should tell the analyst "stop, split this into separate offences" — so a companion parser fix (below) makes that signal fire reliably.

## Decisions

- **Watchlist page only.** No changes to `domain.$url.tsx`. Offence viewing/editing lives entirely on `urls.tsx` (Add Domain dialog + a new per-row action on already-added domains).
- **Same offence list applies to the whole Add-Domain batch.** Matches today's behavior (the current single offence already applies to every domain pasted in one submission). If a batch genuinely needs different offences per domain, split it into separate Add Domain submissions, or fix up per-row afterward via the new per-row editing.
- **No new backend offence-CRUD.** `AttachOffenceToURL` (plain insert, no uniqueness constraint), `fetchOffencesByUrl`/`GET /api/legal/offences/*url`, and `detachOffence`/`DELETE /api/legal/offences/{id}` already fully support N offences per URL — they're implemented and exported from `web/src/api/legal.ts` but currently have **zero frontend callers**. This spec is the first thing to actually use them.
- **Companion parser fix included**, since the `NEEDS_REVIEW` signal only matters now that there's a real "go split this into multiple offences" workflow for it to route analysts to. See below.

## Frontend

### `MultiOffencePicker` (new, extracted from today's `OffencePicker`)

`web/src/routes/urls.tsx`'s existing `OffencePicker` (single-selection, four cascading `Select`s: Instrument→Citation→Category→Element) becomes the internal "staging" picker inside a new component that manages a *list*:

```tsx
type StagedOffence = {
  instrumentId: number
  citationId: number
  categoryId: number
  elementId?: number
  label: string // e.g. "Seksyen 233(1)(a) — Obscene content", for the chip
}

function MultiOffencePicker({
  value, onChange, disabled,
}: {
  value: StagedOffence[]
  onChange: (offences: StagedOffence[]) => void
  disabled: boolean
}) { /* ... */ }
```

- Renders `value` as removable chips above the four `Select`s (`label [×]`, removing via `onChange(value.filter(...))`).
- The four `Select`s keep today's cascading behavior (Instrument resets Citation/Category/Element on change, etc.) but as *local* staging state, not the component's `value`.
- An "Add offence" button, enabled once Instrument+Citation+Category are all chosen (Element stays optional, same as today), appends the staged selection to `value` and resets the four `Select`s for the next entry.
- No knowledge of "creating a new domain" vs "editing an existing one" — that's what makes it reusable in both `AddUrlDialog` and the new `EditOffencesDialog`.

### `AddUrlDialog` changes

- Replace the four `instrumentId/citationId/categoryId/elementId` state fields with `const [offences, setOffences] = useState<StagedOffence[]>([])`.
- Render `<MultiOffencePicker value={offences} onChange={setOffences} disabled={loading} />` in place of today's `<OffencePicker .../>`.
- `handleSubmit`: unchanged `createUrl` step, then
  ```ts
  await Promise.all(
    domains.flatMap(d => offences.map(o => attachOffence(d, o.categoryId, o.elementId)))
  )
  ```
  (today's `if (categoryId !== '') { ... }` guard becomes simply `offences.length > 0` implicitly via `flatMap` on an empty array — no offences means no calls.)
- `reset()` clears `offences` to `[]` instead of the four scalar fields.

### `EditOffencesDialog` (new)

```tsx
function EditOffencesDialog({
  url, open, onClose,
}: {
  url: string
  open: boolean
  onClose: () => void
}) { /* ... */ }
```

- On open: `fetchOffencesByUrl(url)` → render each `URLOffence` as a removable chip (`formatParsedCitation(o.category.citation.parsed)` + `o.category.name` + optional `o.element.name`, mirroring the label format `MultiOffencePicker` chips already use).
- Removing a chip calls `detachOffence(offence.id)` **immediately** (not batched — no "submit" step in this dialog, consistent with how `setUrlEnabled`/`setUrlOrderedAt` already act immediately elsewhere on this page), then re-fetches to confirm server state.
- Below the existing-offences list, a `MultiOffencePicker` in "add more" mode: `value` starts empty each time, but instead of accumulating for a later batch submit, each "Add offence" click calls `attachOffence(url, ...)` immediately, then re-fetches and clears local staging.
- A failed attach/detach shows an inline error and re-fetches rather than trusting optimistic local state — this dialog can be reopened anytime, so there's no urgency to make failures silently self-heal.

### Watchlist table

One more per-row icon button (alongside the existing history icon / enable switch / order-date input / delete), opening `EditOffencesDialog` for that row's URL.

## Backend: `internal/legalcite/parser.go` fix

Two independent, small changes verified against every case in `parser_test.go` (including the ones that deliberately feed instrument-name text alongside the citation, e.g. `"Seksyen 58 ... Modal dan Perkhidmatan 2007"` expecting `OK`) to confirm neither regresses existing behavior.

### Fix 1 — widen `multiProvisionRe`

Currently `^`-anchored, so it only catches a list marker sitting immediately after the first provision number — misses `"4(1), 7(a), 9 dan 12"` because the tail starts with `"(1)..."`, not a joiner. Drop the anchor (search the whole tail) and add `&` as a third joiner alongside `,`/`dan`:

```go
multiProvisionRe = regexp.MustCompile(`(?i)(?:,\s*\d|&\s*(?:seksyen\s+)?\d|\bdan\s+(?:seksyen\s+)?\d)`)
```

The `\b` before `dan` matters once unanchored (prevents a false match inside a word ending in "...dan"). Every joiner variant requires a digit (optionally after "seksyen") immediately after it — this is what keeps it from firing on an instrument name that happens to contain "dan"/"&" followed by an ordinary word.

### Fix 2 — stop the suffix letter from swallowing a typo'd "dan"

`provisionRe`'s `([a-zA-Z]?)` group greedily grabs one letter right after the digits with no way to check what follows (Go's RE2 has no lookahead). Fix in code: after matching, if the captured suffix letter is immediately followed by *another* letter, it's not a real amendment suffix (real ones like `4A`/`372B` are always followed by a space/paren/end-of-string) — discard it and re-anchor `tail` right after the digits instead of after the bogus letter, so the stray letter flows into `tail` where Fix 1's widened detector now catches it:

```go
num, _ := strconv.Atoi(remainder[m[2]:m[3]])
p.ProvisionNum = &num
suffixStart, suffixEnd, tailStart := m[4], m[5], m[1]
if suffixEnd > suffixStart && suffixEnd < len(remainder) && letterRe.MatchString(remainder[suffixEnd:suffixEnd+1]) {
    tailStart = m[3] // discard the bogus suffix; re-anchor right after the digits
} else {
    p.ProvisionSuffix = strings.ToUpper(remainder[suffixStart:suffixEnd])
}
tail := remainder[tailStart:]
```

(`letterRe` already exists in the package for exactly this single-letter check.)

### Testing

Add to `TestParse_RealWorldMalaySamples` (`internal/legalcite/parser_test.go`), using the exact breaking inputs from the verification report:

| Input | Before | After |
|---|---|---|
| `"Seksyen 292 & Seksyen 372"` | `ProvisionNum=292`, `OK` | `ProvisionNum=292`, `NEEDS_REVIEW` |
| `"Peraturan 62 & 63"` | `ProvisionNum=62`, `OK` | `ProvisionNum=62`, `NEEDS_REVIEW` |
| `"Seksyen 4(1), 7(a), 9 dan 12"` | `ProvisionNum=4, SubProvision=1, Paragraph=a`, `OK` | `ProvisionNum=4, SubProvision=1`, `NEEDS_REVIEW` |
| `"Seksyen 14dan 15"` | `ProvisionNum=14, ProvisionSuffix=D`, `OK` | `ProvisionNum=14`, `NEEDS_REVIEW` |

Then run the full existing suite (`go test ./internal/legalcite/...`) to confirm zero regressions on the pre-existing cases — that's the real verification step, not the by-hand trace above.

## Error Handling

- `AddUrlDialog` submit: domain creation and offence attachment both run via `Promise.all`, matching the existing pattern — a partial failure surfaces the same generic error banner used today. No granular per-item retry; that's new complexity the dialog doesn't have today either, and this feature isn't the one to introduce it.
- `EditOffencesDialog`: see above — re-fetch on failure rather than trust optimistic local state.

## Testing (frontend)

No existing test suite covers `urls.tsx` (manual/dev-server verification only, consistent with the rest of the frontend per `CLAUDE.md`) — verify manually via `dev.sh`: add a domain with 2+ staged offences and confirm all attach; open `EditOffencesDialog` on an existing row, remove one offence, add a different one, confirm the chip list matches `GET /api/legal/offences/*url` after each action.

## Out of Scope

- `domain.$url.tsx` offence display/editing — deferred.
- Per-domain offence lists within a single Add-Domain batch (same offence list applies to every domain in the batch).
- New backend offence-CRUD endpoints — existing ones are sufficient and were simply unused until now.
- Any other `legalcite.Parse` gaps beyond the two fixed here (e.g. the closed Malay-label set, or `"Kaedah"` support) — out of scope, unchanged.
