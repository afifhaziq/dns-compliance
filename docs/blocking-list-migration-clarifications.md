# Blocking Full List_1.xlsx migration — open questions for sign-off

Source: test migration analysis of `Blocking Full List_1.xlsx`, sheet "2011-2026" (38,156 data rows, 2011–2026). Read-only analysis, nothing imported yet. Full findings summary is in the conversation that produced this doc; this file is just the questions that need a decision from the business/product side before any real import is written.

DNS server/ISP list is **not** in scope — it isn't in the spreadsheet and the existing `dns_servers` table is retained as-is.

---

## 1. Status mapping

**Correction:** this section's original framing is stale. It checked spreadsheet values against `URL.Status`, but case metadata has since moved off `URL` onto `Case`/`CaseURL` (case_urls.status — "this url's own status within the case"). The app already has a UI setting for exactly this, the case Status field (`urls.tsx`'s `STATUS_OPTIONS`), and the live allowed set has grown since this doc was first written: `{"", "requested", "uplift", "suspended", "internal"}` (`internal/server/handlers.go:244`, `urlStatusAllowed`) — **4 real values now, not 3.**

97.2% of rows (37,082 / 38,156) have a `Status` value with no direct match in that set. Actual spreadsheet values:

| Value | Rows |
|---|---|
| `Blocked` | 37,082 |
| `Uplift` | 771 |
| `Not Blocked` | 268 |
| `Suspended` | 9 |
| `Not blocked` | 4 |
| `blocked` (lowercase) | 1 |
| *(empty)* | 21 |

**Resolved:** `Blocked`/`Not Blocked` are not a live compliance check — they're the manual record of what the ISP told CRD when the takedown request was actioned (or not), so neither should be skipped or derived from the app's live `Compliant` field. `Suspended`/`Uplift` are direct case-insensitive matches to the app's existing `suspended`/`uplift` values.

**Still open — proposed mapping, needs a confirm:**
- `Blocked` (37,082 rows, 97%) → `requested`. Reasoning: in the app's model a case starts life as `requested` and only changes when something happens (uplifted, suspended); a spreadsheet row that just says "still blocked, ISP confirmed" with no further lifecycle event is exactly that steady state, so it needs no new enum value — it's the same `requested` a live-created case would sit in.
- `Uplift` → `uplift`, `Suspended` → `suspended` (case-insensitive, direct).
- `Not Blocked`/`Not blocked` (272 rows) → **unresolved.** No existing value obviously fits. `internal` was added for a different purpose (2026-08-21, "internal case phase" alongside a letter-capture feature) and its product meaning hasn't been confirmed to match "ISP did not action the block" — don't assume it's a match without checking. Options: reuse `internal` if its meaning does line up, add a new value (e.g. `not_blocked`), or treat these 272 rows as informational-only (imported with blank status).

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

**Resolved:** per-event history must be preserved and queryable — no collapsing to "current status + latest citation."

**Correction:** this is not just a design proposal — the `cases`/`case_letters`/`case_urls` schema already shipped (`docs/db-schema.dbml`, `docs/db-schema-proposed.dbml` is now historical, marked "SHIPPED" at its own file header) and is live in the app today (`urls.tsx`'s Cases view, `docs.tsx`'s Documents view). `urls` stays one row per domain; `case_urls` is the many-to-many join carrying its own `status` per (case, url) pair; `case_letters` carries per-letter `reference_number_external`/`reference_number_internal`, dates, subject, remarks. A repeated domain just gets one additional `case_urls` row per event, all pointing at the same `urls` row — there's no "duplicate domain" collision to resolve, since uniqueness lives on `urls.url`, not on how many cases reference it. Per-event category/element/sub-element differences go on `url_offences` (recorded_at-stamped, already supports multiple rows per url over time).

The one open wrinkle this reintroduces: `cases` is meant to be "one row per real-world request," but `No. Rujukan NMD` can't be trusted to group rows into a case — one blanket reference value (`"JK KPN(PR) 168/6"`) alone is reused across 9,206 unrelated PDRM rows spanning 2021–2026. Default plan: import each CRD spreadsheet row as its **own** `case` (1 case_urls + 1 case_letters + 1 url_offences row per row), rather than trying to group rows by matching NMD into a shared case — safe under the blanket-reference risk, and doesn't lose anything since case-level grouping wasn't reliable to begin with. Worth a stakeholder confirm, but this is the safe default absent a better grouping signal.

This still doesn't decide §5's exact-duplicate rows (474 groups, byte-identical including year) — under this model they'd just become two cases for what looks like one event. That's still worth asking about separately: collapse those before import, or let them become two cases as data-entry noise it's not worth cleaning?

---

## 3. Category (`Kategori`) data doesn't match the spreadsheet's own legend

The workbook's own "Directory" sheet lists 4 canonical categories (Lucah, Kepentingan Negara, Jelik, Palsu). Actual data has 54 distinct values. Most of the extra 50 are legitimate categories the legend simply never documented (Judi 13,178 rows, Penyalahgunaan Hakcipta 5,278 rows, Penjualan Tanpa Kebenaran 3,262 rows, etc.) — not a data problem, just confirms the Directory sheet can't be used as the seed list for the `Category` table.

**Resolved:** ignore the Directory sheet — it's stale. The `Category` seed list is extracted straight from the main sheet's actual data instead, and (per the wrinkle below) each value is mapped under its own `Instrument`/`Citation`, not treated as a flat global list. Casing duplicates within the 50 (e.g. `"Tidak berdaftar"` vs `"Tidak Berdaftar"`) still need a human pass during that classification — not auto-collapsed.

Two things in this column are real problems:

- **8 distinct values (71 rows) are comma-joined compounds** — e.g. `"Jelik, Palsu, Lucah"`, `"Jelik, Palsu"` — a single cell describing multiple categories at once.
- **12 rows have a column-shift data-entry bug**: `Kepentingan Negara` (a valid category name) appears in the `Elemen` column instead of `Kategori`, with `Kategori` left blank on those rows. As-is this would violate the catalog's parent-must-exist ordering (`Element` needs a `Category` to attach to).

**Resolved:**
- Compound rows: `URLOffence` already supports multiple rows per `URL` (surrogate-PK join, no uniqueness constraint blocking it — same mechanism the `MultiOffencePicker` UI already exercises), so each compound cell splits into one `URLOffence` per listed category rather than picking a primary. 34 of the 71 rows also carry an `Elemen`/`Sub-Elemen` value (e.g. `"Jelik, Palsu"` + Elemen `"Kepentingan Negara"`) — since `Element` is scoped to one specific `Category` and names aren't shared across categories (`"Politik"` under `Jelik` and `"Politik"` under `Palsu` are two distinct rows, not one Element with two parents), the row asserts N *independent* facts, not one ambiguous one: the same Elemen/Sub-Elemen value attaches under **every** split category as its own `URLOffence`. Row 7027 (`Jelik, Palsu` + Elemen `Kepentingan Negara` + Sub-Elemen `Politik`) becomes two rows — `(Jelik, Kepentingan Negara, Politik)` and `(Palsu, Kepentingan Negara, Politik)` — both real, no case-by-case lookup needed. Mechanical, fully applied in the extract below. One side-effect worth a heads-up, not a blocker: this introduces 5 (Category, Element, Sub-Element) combinations that never occur on their own anywhere else in the sheet (e.g. `(Lucah, Politik)`, `(Palsu, Kepentingan Negara, Politik)`) — new catalog rows, not typos.
- Column-shift rows: confirmed, correct by moving the `Elemen` value into `Kategori` (already applied in the extract below).

**New wrinkle, not previously documented:** `Category` isn't a flat/global table in the shipped catalog — it's scoped to a `Citation`, which is scoped to an `Instrument` (five-level catalog: Instrument → Citation → Category → Element → SubElement; see the `legal-citation-catalog` skill). The same category name under two different citations is deliberately two separate rows. That means before any `Kategori` value can be imported at all, every row's `Butiran Kesalahan` (citation) text first needs to resolve to one canonical `Instrument`+`Citation` pair.

Checked how bad this is against the real data — better than feared:
- Only **184 distinct raw citation strings** across all 38,156 rows (not 38k) — small enough to hand-classify once, not something that needs per-row heuristics.
- Running the existing `internal/legalcite.Parse` (already built for exactly this Malay-citation-shorthand problem) against all 184: **110 parse cleanly, 74 need manual review** (joined/ambiguous text, same as its documented behavior for strings like "Seksyen 211 dan 233 Akta...").
- `legalcite.Parse` only extracts the *provision* (section number), not which *Act* it belongs to — and provision numbers collide across unrelated Acts in this data (e.g. "Seksyen 5" appears under 11 different raw strings spanning at least 3 unrelated Acts: Akta Industri Pelancongan 1992, Akta Pemberi Pinjam Wang 1951, Akta Peranti Perubatan 2012). So Instrument identification can't be automated from provision number alone — matching each citation to the right `Instrument` needs either a manual pass over the 184 strings or a separate Act-name extraction step.

**Resolved:** treat this as a one-time manual pre-import task — hand-classify the 184 distinct citation strings into canonical `Instrument`/`Citation` rows first (collapsing obvious variants like `"AKM1998"`/`"AKM 1998"`/`"Akta Komunikasi dan Multimedia 1998"` into one Citation), then attach each spreadsheet row's `Category`/`Element`/`Sub-Element` under that citation — rather than auto-creating a new `Citation` per unique raw string, which would fragment the same real category across near-duplicate citations.

The extraction itself (citation × its observed `Category`/`Element`/`Sub-Element` combinations + row counts, plus `legalcite.Parse`'s confidence/provision-number for each citation) is done: `docs/blocking-list-citation-category-extract.csv`, 184 citation groups / 316 combo rows. Column-shift and compound-category corrections above are already applied and expanded — every compound cell is already split into its per-category rows, no remaining review flag. This is the input for the manual classification pass, not the classification itself — `Instrument`/`Citation` assignment per row still needs a human.

**In progress:** the manual classification pass is being worked through batch-by-batch (grep the raw text for a shared marker — e.g. a year — then confirm which grouped hits are really the same `Instrument`), tracked in `docs/blocking-list-citation-classification.csv`. First batch done: the 8 distinct citation strings containing "1998" (13,469 rows, ~35% of the sheet) — 5 confirmed as `Akta Komunikasi dan Multimedia 1998` (AKM 1998 / Communication and Multimedia Act 1998 / CMA 1998, all one Instrument) differing only by provision (`Seksyen 233`, `Seksyen 211 dan 233`, `Seksyen 263`) or Act-name spelling; 3 confirmed as unrelated Acts that just happen to say "1998" (Akta Kesalahan Jenayah Syariah (Wilayah-Wilayah Persekutuan), Akta Kemudahan dan Perkhidmatan Jagaan Kesihatan Swasta 1998 / Akta 586). Two things surfaced that still need an answer before those rows can be finalized: whether `Seksyen 263` is real or a typo for `Seksyen 233`, and how a citation that names two provisions at once (`"Seksyen 211 dan 233"`, `"Seksyen 7 dan Seksyen 8"`) should attach — one compound `Citation` row, or split like the compound-`Kategori` case above.

Second batch done: the 3 distinct citations containing "Hakcipta" (5,282 rows) — `Seksyen 41 Akta Hakcipta 1987` (5,230 rows) and `Seksyen 41 1(C) Akta Hakcipta 1987` (47 rows) confirmed as the same Instrument (`Akta Hakcipta 1987`, Copyright Act 1987), kept as **two separate `Citation` rows** rather than merged — the bare `Seksyen 41` is a legitimately less-specific cite of the same section, not an error, while `Seksyen 41 1(C)` narrows to the `(1)(c)` subsection/paragraph. `Seksyen 100 Akta Cap Dagangan Hakcipta 2019` (5 rows) confirmed as a genuinely different Act, name as written accepted as correct (not a mis-transcription).

Third batch done: the 11 distinct citations containing "Dadah"/"Kosmetik" (2,348 rows) split into two unrelated Instruments. 6 confirmed as `Peraturan Kawalan Dadah dan Kosmetik 1984` (Control of Drugs and Cosmetics Regulations 1984, 2,336 rows) — canonical name uses the `"dan"` spelling (not `"&"`/`"Peraturan-peraturan"` prefix), two real Citations (`Peraturan 7(1)(a)`, `Peraturan 18A(14)`). 4 confirmed as `Akta Dadah Berbahaya 1952` (Dangerous Drugs Act 1952, 8 rows) — including `Peraturan 5(1)(a) Peraturan Dadah Merbahaya 1952` (4 rows), confirmed as a double typo (wrong label `Peraturan`→`Akta`, wrong spelling `Merbahaya`→`Berbahaya`) that corrects to the same Citation as `Seksyen 5(1)(a) Akta Dadah Berbahaya 1952` below it.

Fourth batch done: `Akta Racun 1952` (Poisons Act 1952, 2 spacing variants, 799 rows, trivial merge); `Kanun Keseksaan` (Penal Code — no year, per convention — 10 variants, 1,155 rows) confirmed as one Instrument across its real distinct provisions (§292, §298A, §372, §372A, §372B, §372(1)(e), §420, §500), including a typo (`Seskyen`→`Seksyen`) and a casing variant folded in; `Akta Pasaran Modal dan Perkhidmatan 2007` (Capital Markets and Services Act 2007, 12 variants, 740 rows) confirmed as one Instrument — canonical name **drops** the `"Undang Undang"` prefix some rows carry, `Perkhidmation` confirmed as a typo for `Perkhidmatan`, and the one row citing **2012** instead of 2007 confirmed as the same Instrument, not a separate Act/amendment. Two more two-provisions-in-one-cell rows surfaced (`"Seksyen 292 & Seksyen 372 Kanun Keseksaan"`, `"Seksyen 212 dan 58 Akta Pasaran Modal dan Perkhidmatan 2007"`) — flagged `needs_decision`, same open question as the earlier compound-citation rows, not re-asked.

Fifth batch done: `Akta Makanan 1983` (Food Act 1983, 6 variants, 433 rows) confirmed as one Instrument across §17(2)/§17(1)(b)/§17(1)(d) (one singleton, `"Seksyen 17 (d)"`, kept as its own Citation rather than assumed identical to §17(1)(d) — missing the `(1)`, not enough evidence to merge); `Akta Ubat (Iklan dan Penjualan) 1956` (Medicines Advertisements and Sale Act 1956, 12 variants, 116 rows) confirmed as one Instrument, canonical name uses **`"dan Penjualan"`** (the `"Iklan & Jualan"` rows normalize to it, not merely a punctuation swap); `Akta Industri Pelancongan 1992` (Tourism Industry Act 1992, 4 variants, 94 rows) confirmed as one Instrument — `"Seksyen 5(2)(a) & (b)"` noted as covering two paragraphs of the same subsection (kept as one Citation, unlike the cross-section compound rows elsewhere). One more Act-spanning compound-citation row surfaced inside the Ubat batch (`Seksyen 4B Akta Ubat...1956` + `Peraturan 7(1)(a)` Dadah/Kosmetik Regs in one cell) — flagged `needs_decision`, same shape as the others.

(Note: two rows in the tracker CSV were briefly malformed — an unquoted comma inside the raw citation text split into extra columns — caught and fixed by re-quoting; flagging in case anyone diffs the CSV history.)

67 of 184 citations classified so far; 38 more (the gambling-house batch above) flagged pending the stakeholder answer; 79 untouched.

**Question, awaiting a stakeholder answer:** the next (and largest) batch is the gambling-house citations — 38 distinct raw strings, **13,180 rows (~35% of the sheet)** — almost certainly all `Akta Rumah Judi Terbuka 1953` (Common Gaming Houses Act 1953), varying by provision (`Seksyen 4`, `4(1)`, `4(1)(c)`, `4(1)(g)`, `4B(a)`, `8(1)`, `41(c)`, `4A(a)`…), Act-name spelling (`Judi` vs `Perjudian`), and — the part that needs an answer — **Act year**. Two year variants are too large to be a casual typo: `Akta Rumah Judi Terbuka 1958` (943 rows) and `...1972` (419 rows), plus thirteen singleton years scattered from 1959–1971.

Checked two theories for the year variants, neither holds up:
- Not "the row's own blocking year (`Tahun` column) got typed into the citation" — the `1958`/`1972`-citation rows' actual `Tahun` values are mostly 2021/2022, unrelated.
- Not an Excel autofill-drag artifact — the `1958`/`1972` rows are scattered non-consecutively across thousands of spreadsheet rows, not clustered together the way a drag error would be.

So `1958`/`1972` could be genuine citations to a real amendment Act (Malaysian law does have "Common Gaming Houses (Amendment) Act" citations by amendment year), not typos for 1953 — this needs someone with domain/legal knowledge, not something resolvable from the spreadsheet. **Question for the stakeholder:** are `Akta Rumah Judi Terbuka 1958`/`1972`/(the 13 other singleton years 1959–1971) the same Instrument as `Akta Rumah Judi Terbuka 1953`, or genuine separate Acts/amendments?

### Note on the 74 NEEDS_REVIEW citations (2,585 / 38,156 rows, 6.8%)

Superseded by `docs/blocking-list-citation-category-extract.csv` (`parse_confidence` column) — that has all 184 citations, not just these 74, plus their category/element/subelement combos. Full per-citation row-count table dropped from here; two things from it are worth keeping as prose:
- Most of the 74 are mechanical (stray spacing, `&` vs `dan`, a typo) rather than genuinely ambiguous — a normalization pass would likely clear most before anyone needs to read all 74 by hand.
- Two deserve individual attention: `Akta Rumah Judi Terbuka 1953` vs `Akta Rumah Perjudian Terbuka 1953` is the same law under two different Act names, not a formatting variant; and the handful of lettered multi-clause citations (`a) ... b) ... c) ...`) each cite several unrelated Acts in one cell.

---

## 4. URL normalization problems (revised — see below, was reported as "26 fail outright")

**Correction:** the original pass only checked for hard parse errors from `internal/urlnorm.Normalize`, which found 26. A second pass also checked for rows that *parse without error but produce garbage* (a bare scheme fragment, a lone TLD, an empty label before the TLD) — `Normalize` doesn't error on these, it just silently returns the wrong host. That found 4 more rows of the same mechanical-typo class below, plus 6 rows that are a genuinely new, unfixable-by-regex pattern. Total needing attention: **36 rows**, of which **31 are auto-fixable/resolved** and **5 need a human decision**.

**Auto-fixable / resolved (31 rows)**:
- 22 rows: stray space somewhere in the hostname (after the scheme, after `www.`, after a `m.` mobile prefix, or mid-hostname) — e.g. `http:// www.foo.com`, `https://www. escort33.com`, `m. starbook88.com`.
- 5 rows: scheme-separator typo — missing `//` (`http:linktr.ee/lalagroup`), missing `:` (`https//malaysiandrama.com/`), single `/` (`https:/jizzberry.com/`), or `;` instead of `:` (`https;//fcc-asia.com`).
- 1 row: leading numbered-list artifact (`"19. http://www.japanfuck.net"`).
- 1 row: `kkggr.com:7852ZPtx.html` → `kkggr.com` — confirmed against a clean `kkggr.com` row elsewhere in the same sheet; `:7852ZPtx.html` is garbage appended after the real hostname (not a valid port), not a typo in the domain. Strip everything from `:` onward when what follows isn't a valid all-digit port.
- 2 rows, confirmed against the original source: `http://my/idkuatong2` → `hi.jomwasap.my` (full link `https://hi.jomwasap.my/idkuatong2`, matching a sibling row in the same reference batch: `"http://rebrand.ly/12BNKFBW redirect to https://hi.jomwasap.my/12BNKFBW"`) and `https://.me/OHO24HRCHANNELCUCI` → `t.me` (full link `https://t.me/OHO24HRCHANNELCUCI`, matching several other `t.me/<code>` rows in that same batch).

**Note on what actually gets stored:** `db.URL` (`internal/db/models.go`) has a single `URL string` column, and `CreateURL`/`AddToWatchlist` (`internal/db/postgres.go`) always run the input through `urlnorm.Normalize` first, which strips scheme/path/query/port down to a bare lowercase hostname before storing — so only `hi.jomwasap.my` / `t.me` land in the database either way; the `/idkuatong2` and `/OHO24HRCHANNELCUCI` path segments are discarded regardless of whether the full link is known.

**New issue surfaced by this, bigger than these 2 rows — shortener/platform domains aren't blockable at the granularity the spreadsheet implies.** This app's whole compliance model is DNS-resolution-based (`Compliant` is strictly A-record-based — see "Domain semantics" in the root `CLAUDE.md`), and DNS resolution has zero visibility into the HTTP path: a resolver answering a query for `t.me` only ever sees the hostname, never `/OHO24HRCHANNELCUCI`. So an ISP **cannot** DNS-block one Telegram channel or one WhatsApp/shortlink target — the only DNS-level lever available is blocking the *entire* shared domain (all of Telegram, all of `bit.ly`, all of `wa.me`, etc.), which is a far more disruptive action than a single "Blocked" row in the spreadsheet plausibly represents, and something a telco is unlikely to have actually done for a one-off case.

Checked how big this is: **117 rows** (0.3% of the sheet) normalize to a known link-shortener or messaging-platform domain — `t.me` alone accounts for 92 of those (matches the 92-row `t.me` count already noted in §2), plus `bit.ly` (5), `hi.my`/`hi.jom.my`/`hi.jomwasap.my` (6 combined), `prelink.co`/`prilink.co` (5), `linktr.ee` (4), `cutt.ly` (2), `rebrand.ly` (2), `wa.me`/`wa.link` (2). This is a lower bound — only a hand-picked list of known shorteners was checked; there are likely more not on that list.

**Question:** does "Blocked" on one of these rows mean the telco actually DNS-blocked the whole shared domain (some jurisdictions have done exactly that to `t.me`), or was the takedown actioned a different way — e.g. a platform-level report to Telegram/Meta to remove the specific channel, not an ISP DNS block? This matters for import because:
- If these were never actually DNS-blocked, importing them as monitored `urls` means this app shows a **permanent, unresolvable violation** on every scan (`t.me` will never stop resolving) — misleading noise, not a real actionable DNS gap.
- If they belong in the record for audit-trail purposes but shouldn't be live-monitored, they may need to stay in `cases`/`case_letters` (the paper trail) without a corresponding DNS-scanned `urls`/`case_urls` row, or some other explicit "not DNS-blockable" marker — worth a product decision before deciding how (or whether) to import all 117+ of these rows, not just the 2 resolved above.

**Still needs a human decision (5 rows)** — checked each against the rest of the sheet for corroborating evidence:

- **2 rows, same batch, no link recovered yet:** `http://my/MarioLink168` and `https://.my/OneAsia88kasihONG222` — same reference (`SKMM(T)09-NMD/800/2022 (113)`) and almost certainly the same shortener-domain/campaign-code shape as `idkuatong2`/`OHO24HRCHANNELCUCI` above, but no sibling row in the batch points at a specific domain for either code the way `hi.jomwasap.my`/`t.me` did. Worth the same source-record check that resolved those two.
- **1 row, corroborated but still unresolved:** `https://freestreams-live` (truncated, no TLD) — the same agency/citation (`KPDNKK`, `Seksyen 41 Akta Hakcipta 1987`) has several sibling rows for what's clearly the same pirate streaming site rotating TLDs to dodge blocks: `freestreams-live1.com`, `freestreams-live.mp`, `freestreams-live.fi`, `freestreams-live1.md`, `freestreams-live1a.pk`. That confirms the *site* but not *which* TLD this particular truncated row intended — the rotation means guessing wrong is likely. No safe auto-fix.
- **1 row, no corroboration found:** `https://solar123movies.cB33:B69om/` — checked every other `123movies`-family domain in the sheet (80+ variants); none is `solar123movies.<anything>`, so there's nothing to confirm a guess against. `solar123movies.com` (i.e. `com` → `cB33:B69om`) is the obvious visual read, but unconfirmed.
- **1 row, no corroboration found:** `mvbet88my1` — no dot at all, no `mvbet88.*` variant found elsewhere in the sheet. `mvbet88.my` is a plausible guess (gambling-site naming pattern, `Judi` category) but unconfirmed.

**Question:** for these 5 — are the correct URLs recoverable from records elsewhere (the `SKMM(T)09-NMD/800/2022 (113)` source record is worth the same lookup that resolved the other two in that batch), or should they just be dropped from the import?

**Lesson for the real importer:** validate with "does `Normalize` return a plausible host (contains a dot, reasonable length)?", not just "did it return an error?" — a try/catch alone misses the silent-garbage cases above.

---

## 5. Exact full-row duplicates (not previously documented)

474 groups (980 rows total) are byte-for-byte identical across every column **including year** — e.g. rows 17922/17923 are both `https://www.weclub88.co/`, 2021, same citation, same status. This is distinct from the legitimate "same domain blocked again in a later year" case in section 2 — these are same-year copies, i.e. straightforward copy-paste data-entry duplicates.

**Question:** collapse each duplicate group to a single row before import (keeping one), or is there a reason a spreadsheet row might legitimately need to repeat identically (e.g. two separate manual actions logged the same day)?
