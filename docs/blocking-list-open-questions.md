# CRD blocking-list migration — open questions

Plain list of everything still needing a decision before the CRD `Blocking Full List_1.xlsx`
import can be finished. Background/rationale is in `docs/blocking-list-migration-clarifications.md`
if needed — this file is just the questions.

**Part 1 (items 1–11, the citation/Act-year questions) was answered by the stakeholder on
2026-09-13 and applied to `docs/blocking-list-citation-classification.csv` — see the "Eighth
batch" entry in `docs/blocking-list-migration-clarifications.md` for what was done with each
answer, including one place (item 3, Selangor spelling) the answer's exact wording wasn't
applied literally, and one place (item 10) it reversed an earlier confirmation.**

**Part 2, item 12 (status mapping) was answered 2026-09-13: add `blocked` and `not_blocked` as
new `case_urls.status` values rather than folding into an existing one — flow is
`internal → requested → {blocked, uplift, suspended, not_blocked}`. Applied to
`urlStatusAllowed`, `CASE_STATUS_OPTIONS`, `STATUS_OPTIONS`, and `mapCRDStatus`; see the
"Resolved by stakeholder" note under §1 of `docs/blocking-list-migration-clarifications.md`.**

**Item 14 (shortener/platform domains) was answered 2026-09-13: keep them on the watchlist
domain-level (scanning by hostname, unchanged) but default them to `enabled: false` (existing
`DepartmentURL.Enabled` feature — no new code needed there), and retain the exact cited
URL/path per case via the new `CaseURL.OriginalURL` field rather than discarding it.
Implemented in `internal/db/models.go` + `internal/blockimport/write.go`/`write_cmod.go`. See
the "Correction on the 117" note under §4 of `docs/blocking-list-migration-clarifications.md`
for the full exact-row list and design.**

**Items 13 and 15 (unrecoverable URLs, exact duplicates) were answered 2026-09-15: don't guess
the correct URL/collapse anything specially — the existing mechanisms already cover both. Item
13's 5 rows retain their exact cited text via `CaseURL.OriginalURL` (same field as item 14)
instead of being dropped; when `urlnorm.Normalize` can't extract any hostname at all,
`normalizeOrFallback` (`internal/blockimport/write.go`) falls back to a lowercased, trimmed copy
of the raw text as the URL row's storage key, so the case/citation record is never lost — these
rows just aren't scannable identities. Item 15's 474 duplicate-row groups share both reference
number and domain, so `CollapseCRDRows`'s existing per-reference dedup (`domainIdx`,
last-write-wins) already collapses each group to one `CollapsedDomain` — no new logic needed. See
the "Item 13/15 resolution" note under §4/§5 of `docs/blocking-list-migration-clarifications.md`.**

---

## Email draft

**Subject: CRD Blocking List Migration — 15 items need your confirmation**

Hi [Name],

We're migrating the historical CRD blocking list (`Blocking Full List_1.xlsx`) into the
new case-tracking system. Before we finish the import, we've hit a few places where the
spreadsheet data is ambiguous and needs a call from your side. Details below, grouped into
two sets: which law/year is actually correct on a citation, and how a few edge cases in
the data should be handled on import.

**Part 1 — Is this a typo, or a genuinely separate Act/year?**

1. **Gambling-house Act** — by far the biggest one: 38 citation variants, 13,180 rows
   (~35% of the whole spreadsheet). The dominant citation is *Akta Rumah Judi Terbuka
   1953*, but a meaningful chunk cite *1958* (943 rows) or *1972* (419 rows), plus
   thirteen more single rows scattered across 1959–1971. Are any of these genuine
   separate Acts or amendments, or should they all be treated as 1953?
2. **Wilayah Persekutuan Syariah Act** — cited as 1997 (dominant), 1998 (2 rows), and
   1999 (1 row). Same Act?
3. **Selangor Syariah Enactment** — cited as 1995 (dominant), 1996 (1 row), and 1997
   (1 row). Same Act?
4. **Official Secrets Act (Akta Rahsia Rasmi)** — cited as 1957 (2 rows) and 1972
   (1 row). The real OSA is Act 88 of 1972 — is the 1957 citation a typo?
5. **Excise Act (Akta Eksais)** — cited as 1976 (1 row) and 1977 (1 row), same
   sections both times. Typo, or two Acts?
6. **Control of Smoking Products for Health Act** — 10 rows cite it with no year, 9
   cite 2024. Same Act?
7. **Communication and Multimedia Act 1998, Section 263** (25 rows) — is this a real
   provision, or a typo for the far more common Section 233?
8. **Medicines (Advertisement and Sale) Act 1956, bare "Section 4"** (2 rows) — its
   own citation, or a truncated 4A/4B?
9. **Two rows cite "Enakmen (Kesalahan) Jenayah Syariah 1997" without naming a
   state anywhere in the cell** — which state's enactment is this?
10. **Trademarks Act 2019** (37 rows) vs. the already-confirmed **Trademarks and
    Copyright Act 2019** (5 rows) — same Act, or genuinely two separate ones?
11. **National Film Development Corporation Malaysia Act** — cited as 1981
    (133 rows, dominant) vs. 1982 (2 rows). Typo?

**Part 2 — How should these be handled on import?**

12. **Status mapping** — 272 rows are marked "Not Blocked" / "Not blocked," and our
    system doesn't currently have a matching status value. Should we reuse the
    existing `internal` status, add a new one, or import these with a blank status?
13. **5 URLs have no recoverable hostname** (e.g. truncated or garbled entries with
    no match elsewhere in the sheet). Should we try to track down the correct URL
    from source records, or drop these rows from the import?
14. **117+ rows point at shared platform/shortener domains** (`t.me`, `bit.ly`,
    etc.). Were these actually DNS-blocked at the entire-domain level, or was the
    takedown handled a different way (e.g. a platform-level report)? This determines
    whether we monitor them live going forward or keep them as a paper record only.
15. **474 rows are exact duplicates** — same domain, same year, every field
    identical. Should we collapse each set down to a single record, or is there a
    legitimate reason the same event would be logged twice?

Happy to walk through any of these on a call if that's easier than replying in
writing. Let us know once you've had a chance to go through them — this is the
last blocker before we can finish the import.

Thanks,
[Your name]

---

## Source list (for reference)

1. **Gambling-house Act** (38 citations, 13,180 rows, ~35% of the whole sheet) — is
   `Akta Rumah Judi Terbuka 1958`, `...1972`, and thirteen singleton years 1959–1971
   the same Act as the dominant `Akta Rumah Judi Terbuka 1953`, or genuine separate
   Acts/amendments?
2. **Wilayah Persekutuan Syariah Act** — 1997 (dominant) vs 1998 (2 rows) vs 1999 (1 row)?
3. **Selangor Syariah Enactment** — 1995 (dominant) vs 1996 (1 row) vs 1997 (1 row)?
4. **Official Secrets Act (Akta Rahsia Rasmi)** — 1957 (2 rows) vs 1972 (1 row)? Real
   OSA is Act 88 of 1972.
5. **Excise Act (Akta Eksais)** — 1976 (1 row) vs 1977 (1 row), same section list both times?
6. **Control of Smoking Products for Health Act (Akta Kawalan Produk Merokok Demi
   Kesihatan Awam)** — 10 rows cite it with no year, 9 cite `2024`. Same Act?
7. **Akta Komunikasi dan Multimedia 1998, Seksyen 263** (25 rows) — real provision, or
   typo for the dominant §233?
8. **Akta Ubat (Iklan & Jualan) 1956, bare "Seksyen 4"** (2 rows) — its own Citation,
   or a truncated 4A/4B?
9. **Two compound cells cite "Enakmen (Kesalahan) Jenayah Syariah 1997" with no state
   named anywhere in the cell** — which state's enactment is this?
10. **Akta Cap Dagangan 2019** (37 rows) vs the already-confirmed `Akta Cap Dagangan
    Hakcipta 2019` (5 rows) — same Act, or genuinely two?
11. **Akta Perbadanan Kemajuan Filem Nasional Malaysia** — 1981 (133 rows, dominant)
    vs 1982 (2 rows) — typo?
12. **Status mapping** — `"Not Blocked"`/`"Not blocked"` (272 rows) has no target value
    in the app's case-status field yet. Reuse `internal`, add a new value, or import blank?
13. **5 URLs with no recoverable hostname** — recover from source records, or drop from import?
14. **117+ rows point at shortener/platform domains** (`t.me`, `bit.ly`, etc.) — were
    these actually DNS-blocked at the whole-domain level, or actioned a different way
    (e.g. platform takedown)? Affects whether they should be live-monitored `urls` or
    paper-trail-only.
15. **474 exact-duplicate rows** (same domain, same year, every column identical) —
    collapse to one case each, or import as-is (two cases)?
