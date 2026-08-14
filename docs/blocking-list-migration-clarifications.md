# Blocking Full List_1.xlsx migration — open questions for sign-off

Source: test migration analysis of `Blocking Full List_1.xlsx`, sheet "2011-2026" (38,156 data rows, 2011–2026). Read-only analysis, nothing imported yet. Full findings summary is in the conversation that produced this doc; this file is just the questions that need a decision from the business/product side before any real import is written.

DNS server/ISP list is **not** in scope — it isn't in the spreadsheet and the existing `dns_servers` table is retained as-is.

---

## 1. Status mapping

97.2% of rows (37,082 / 38,156) have a `Status` value with no direct match in the app's allowed set. The app validates `URL.Status` against exactly `{"", "requested", "uplift", "suspended"}` (case-sensitive, `internal/server/handlers.go:243`).

Actual spreadsheet values:

| Value | Rows |
|---|---|
| `Blocked` | 37,082 |
| `Uplift` | 771 |
| `Not Blocked` | 268 |
| `Suspended` | 9 |
| `Not blocked` | 4 |
| `blocked` (lowercase) | 1 |
| *(empty)* | 21 |

Even the values that look like matches fail purely on casing (`Uplift`→`uplift`, `Suspended`→`suspended`). `Blocked`/`Not Blocked`/`blocked` have no counterpart in the allowed set at all.

**Questions:**
- Does "Blocked" in this spreadsheet mean the same thing as `URL.Status`, or is it actually describing the *compliance outcome* (which this app already derives independently from live DNS scans, via `Compliant`)? If the latter, should `Status` import be skipped entirely for "Blocked" rows and left blank/`"requested"`?
- What should "Not Blocked" map to — is there a case-lifecycle state for "request never actioned" or "not currently enforced," or does it mean the row shouldn't be imported as an active case at all?
- Confirm case-insensitive mapping is fine for `Uplift`→`uplift`, `Suspended`→`suspended`.

---

## 2. One row per domain (schema) vs. one row per event (spreadsheet)

`URL.URL` has a unique index — the schema holds exactly one row per domain. The spreadsheet has one row per blocking *event*, and domains repeat across years.

- 2,890 domains (8.4% of ~34,300 distinct domains) appear in more than one row — 6,758 rows total.
- `t.me` alone has 92 rows across 2022–2023; `youtu.be` has 37; several gambling/piracy domains recur 6–14 times across 2019–2026.

**Do the repeats carry the same case details, or different ones?** Checked directly — of the 2,890 repeated domains:

| | Groups | % |
|---|---|---|
| Identical citation + category + element + sub-element + agency across every repeat | 2,151 | 74.4% |
| At least one of those fields differs across repeats | 739 | 25.6% |

Breaking down *which* field differs, per repeated-domain group (a group can vary on more than one):

| Field | Groups that vary |
|---|---|
| Year | 1,191 / 2,890 (expected — same domain blocked again in a later year) |
| Citation (`Butiran Kesalahan`) text | 645 / 2,890 |
| Element (`Elemen`) | 179 / 2,890 |
| Agency (`Agensi`) | 79 / 2,890 |
| Status | 48 / 2,890 |
| Category (`Kategori`) | 34 / 2,890 |

Category is nearly always stable (98.8% of repeat groups keep the same category) — most of the "citation differs" cases are the same law cited two different ways (e.g. `"Seksyen 211 dan 233 Akta Komunikasi dan Multimedia 1998"` on the first block vs. the abbreviated `"Seksyen 233 Akta Komunikasi dan Multimedia 1998"` or `"Seksyen 233 AKM 1998"` on a later one — same underlying provision, different citation string). But 48 groups show a genuine status change over time (e.g. blocked, later uplifted, blocked again), which is real case history, not noise.

**Questions:**
- Is per-event history (which year, which citation phrasing, which status, at that point in time) something that needs to be preserved and queryable, or is "current status + most recent/most complete citation" sufficient?
- If history matters: is a new table (one row per spreadsheet event, FK'd to the shared `URL` row) the right shape, or should this reuse/extend an existing concept (e.g. `URLOffence`, or something scan-result-shaped)?
- If history does *not* matter: for the 739 domains where attributes genuinely differ across repeats, which row should "win" when collapsing to one `URL` row — most recent year, or something else?

---

## 3. Category (`Kategori`) data doesn't match the spreadsheet's own legend

The workbook's own "Directory" sheet lists 4 canonical categories (Lucah, Kepentingan Negara, Jelik, Palsu). Actual data has 54 distinct values. Most of the extra 50 are legitimate categories the legend simply never documented (Judi 13,178 rows, Penyalahgunaan Hakcipta 5,278 rows, Penjualan Tanpa Kebenaran 3,262 rows, etc.) — not a data problem, just confirms the Directory sheet can't be used as the seed list for the `Category` table.

Two things in this column are real problems:

- **8 values are comma-joined compounds** — e.g. `"Jelik, Palsu, Lucah"` (40 rows), `"Jelik, Palsu"` (17 rows) — a single cell describing multiple categories at once.
- **12 rows have a column-shift data-entry bug**: `Kepentingan Negara` (a valid category name) appears in the `Elemen` column instead of `Kategori`, with `Kategori` left blank on those rows. As-is this would violate the catalog's parent-must-exist ordering (`Element` needs a `Category` to attach to).

**Questions:**
- For the 8 compound-category rows: should each map to multiple `Category`/`URLOffence` links on the same URL, or should one category be picked as primary?
- Confirm the 12 column-shift rows should be corrected (`Elemen` value moved to `Kategori`) rather than imported as-is or dropped.
- Should the 50 undocumented-but-real categories simply become the actual `Category` seed list (superseding the Directory sheet), or does someone need to review/rename them first (there are also casing duplicates within this set, e.g. `"Tidak berdaftar"` vs `"Tidak Berdaftar"`)?

---

## 4. 26 URLs fail normalization outright

These would hard-fail `CreateURL` today (`internal/urlnorm.Normalize` returns an error) unless corrected first:

- 20 rows: stray space right after the scheme (e.g. `http:// www.foo.com`) — likely fixable by a trim/regex pass.
- 3 rows: corrupted/garbled URL text with stray characters parsed as a port (e.g. `kkggr.com:7852ZPtx.html`, `https://solar123movies.cB33:B69om/`) — looks like Excel formula/cell-reference artifacts leaking into the value; may need to go back to whoever maintains the source list.
- 1 row: leading numbered-list artifact (`"19. http://www.japanfuck.net"`) — trim the prefix.

**Question:** for the 3 garbled rows specifically — is the correct URL recoverable from records elsewhere, or should they just be dropped from the import?
