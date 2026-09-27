# CRD blocking-list import (`Blocking Full List_1.xlsx`)

Source: sheet "2011-2026" (38,156 rows, 2011–2026), gitignored in repo root. Importer: `go run ./cmd/import-crd --file "Blocking Full List_1.xlsx" --db-url "$DB_URL" --dry-run=false` (`internal/blockimport/`). Legal classification lookup: `docs/blocking-list-citation-classification.csv` (only `status=confirmed` rows are used).

This file records the import rules in force and what is still open. Section numbers are cited from code — keep them stable.

## Open items

1. **Verify the Akta numbers added 2026-09-27.** The `(Akta NNN)` suffixes on 29 Acts in the classification CSV (e.g. AKM 1998 → 588, Kanun Keseksaan → 574) were filled from memory because the `mylaw-my` MCP was down. Spot-check against the official list.
2. **Catalog review beyond AKM 1998.** Only Akta Komunikasi dan Multimedia 1998 has been walked through (§3). The other 48 instruments still need the same check (also the "Legal citation mapping correctness" TODO in `CLAUDE.md`).
3. **Seksyen 211 inherits Seksyen 233's classification.** All Seksyen 211 offences come from splitting "Seksyen 211 dan 233" cells (§3), so 211 carries whatever category the row had — including Palsu › Kepentingan Negara (23). Confirm that's acceptable.
4. **Workbook sheet B is above the dashboard** (§6; ~1.5% for 2024–25, 3–4% for 2022–23): the workbook counts spreadsheet rows, the import collapses byte-identical duplicates (§5) and different paths on one hostname. Decide whether to accept the gap or count per row.
5. **2022–2024 sheet A category differences** (2024 Jelik 11 vs 10; 2022 Palsu 16 vs 12, Jelik 1 vs 5; 2023 Jelik 24 vs 25): the source sheet gives the dashboard's numbers, the workbook was compiled from an earlier version. Confirm which is authoritative.
6. **`case_letters` has no foreign key to `cases`** (`CaseLetter.CaseID`, `internal/db/models.go`), so deleting a case leaves its letters behind — the re-migrate procedure below has to delete them explicitly.
7. **`case_urls.original_url` isn't exposed** in the API/UI yet; the exact cited URL (e.g. a `t.me/<channel>` path) is stored but not visible.
8. **Selangor Syariah Enactment spelling:** the catalog uses `Enakmen Jenayah Syariah (Negeri Selangor) 1995`; the stakeholder once wrote it without "Negeri". Flag if that form should be canonical.

## Re-migrating

Re-runs skip cases that already exist and never update catalog rows, so rule changes only take effect on a clean import. Any environment imported before 2026-09-27 needs this:

```sql
BEGIN;
DELETE FROM case_letters;   -- no FK to cases (open item 6)
DELETE FROM cases;          -- cascades case_urls + url_offences
DELETE FROM instruments;    -- cascades citations → categories → elements → sub_elements
COMMIT;
```

This also removes any legal citations or offences entered by hand. `urls` and scan history are untouched. Last local import (2026-09-27): 15,644 cases, 37,410 case_urls, 39,097 url_offences.

## 1. Status mapping

`mapCRDStatus` (`internal/blockimport/write.go`), case-insensitive: `Blocked` → `blocked`, `Uplift` → `uplift`, `Suspended` → `suspended`, `Not Blocked` → `not_blocked`, empty → `requested`. These record what the ISP reported, not a live DNS check.

## 2. Case grouping

- Per-event history is kept: `urls` is one row per hostname, and each block event is its own `case_urls` row.
- **Internal reference** (`MCMC`/`SKMM` prefix) = one case covering all its domains.
- **Anything else** (e.g. PDRM's blanket `JK KPN(PR) 168/6`, reused on 9,206 unrelated rows) or a blank reference = one case per (reference, normalized domain, notice date), so a later re-block is a separate case with its own year. The re-run check matches the same key.
- A domain repeated within one group: status and agency are last-write-wins.
- **Agency is per domain** (`case_urls.agency_id`), not per case: some internal references cover PDRM gambling and MCMC obscenity domains 100/100. Agensi = who handled it, not whose law applies.
- **Offences are per domain, per row** (`url_offences.case_id`), so a citation is never paired with another row's category.
- Blank NMD with an NMSMD → the NMSMD becomes the reference (3 rows). An uplift date becomes a `Notice (Uplift)` letter.

## 3. Legal catalog

Five levels: Instrument → Citation → Category → Element → SubElement (see the `legal-citation-catalog` skill). Categories are scoped per citation.

- **Citations** come from the hand classification CSV (184 raw strings, all confirmed). Variant spellings and years are standardized onto one Instrument, e.g. every gambling-house year variant (1958, 1972, …) → `Akta Rumah Judi Terbuka 1953 (Akta 289)`, and `Seksyen 263 AKM` → Seksyen 233.
- **A compound citation** ("Seksyen 211 dan 233", multi-Act lettered cells) splits into independent Citations. Each split gets the row's full category/element data.
- **Instrument fields** are parsed from the CSV text: type from the leading word, number from `(Akta N)`, trailing year, and state from the 13-state list (else `FEDERAL`). Unnumbered Acts are also matched by title, so same-year Acts don't merge.
- **Labels** (`crd.go`):
  - Spacing is normalized: no spaces around `-`, one space each side of `/`.
  - Category casing is unified.
  - Confirmed misspellings are fixed (`labelAliases`).
  - `Keganasan / Grafik Melampau` → `Ngeri / Grafik Keterlaluan`.
- **Compounds**:
  - A Kategori splits on `,`, `/` and ` dan `, one offence per category.
  - The `Dewasa / Kanak-kanak` element splits into two offences.
- **Row fixes**:
  - A category entered in the Elemen column (12 rows) moves to Kategori.
  - A Sub-Elemen with no Elemen (11 rows) becomes the element.
  - `Phishing` and `Palsu (Phishing)` become Palsu › Phishing.
- **Kept as recorded (stakeholder, 2026-09-27)**:
  - The standalone s233 categories Fitnah, Politik and Jelik Melampau.
  - Politik both as an element and as a sub-element under Kepentingan Negara / Fitnah.
  - The pairings Lucah › Hina Agama, Lucah › Kepentingan Negara and Jelik › Dewasa.
  - Bare `Seksyen 4 Akta Ubat` stays its own Citation (don't guess 4A/4B).

## 4. URLs

- `urls.url` is the bare hostname (`urlnorm.Normalize`). The exact cited text is kept on `case_urls.original_url`.
- 31 malformed URLs (stray spaces, scheme typos, list prefixes) are auto-fixed.
- 5 URLs are unrecoverable (`http://my/MarioLink168`, `https://.my/OneAsia88kasihONG222`, `https://freestreams-live`, `https://solar123movies.cB33:B69om/`, `mvbet88my1`). They are stored under their raw text, not guessed and not dropped.
- 117 rows point at shortener/platform domains (`t.me` 92, `bit.ly`, `wa.me`, …), which DNS can't block per path. The importer creates no watchlist rows, so nothing is scanned until a department adds a domain deliberately.

## 5. Duplicate rows

474 groups (980 rows) are byte-identical, including the year. They collapse to one case/domain via the §2 grouping.

## 6. Dashboard vs the MCMC stats workbook

Reference: `data/14 Jumlah Sekatan Laman Sesawang 01092026.xlsx`. The Blocking Statistics page (`BlockingStats` in `internal/db/cases.go`, `blocking-stats.tsx`) mirrors it:

- Counts status `blocked` only, by the earliest Notice letter's year.
- Counts each (case, domain) once per category.
- Attributes by offence (display only): the 5 MCMC categories (Lucah, Sumbang, Palsu, Jelik, Mengancam) go to sheet A whatever the Agensi, and MCMC-handled Judi shows under PDRM.

Result (dashboard / workbook):

| Sheet | 2025 | 2024 | 2023 | 2022 |
|---|---|---|---|---|
| A · MCMC | 736 / 736 | 891 / 890 | 1153 / 1156 | 1615 / 1618 |
| B · Other agencies | 2597 / 2633 | 2687 / 2726 | 2457 / 2568 | 2858 / 2954 |

Remaining gaps: open items 4 and 5.
