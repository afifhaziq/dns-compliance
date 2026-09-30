# Masterlist Blocking CMOD.xlsx migration — EDA findings & open questions

Source: `Masterlist Blocking CMOD.xlsx`, copied into repo root (gitignored, not committed). Read-only analysis, nothing imported yet — same status as the earlier `Blocking Full List_1.xlsx` ("CRD") pass in `docs/blocking-list-migration-clarifications.md`.

## Import rules in force (2026-09-30)

Importer: `go run ./cmd/import-cmod --file "Masterlist Blocking CMOD.xlsx" --db-url "$DB_URL" --oic-password <pw> --dry-run=false` (`internal/blockimport/cmod.go`, `write_cmod.go`). Last local run: 67 cases (50 linked from CRD, 17 new), 294 case_urls, 69 url_offences, 44 OIC accounts. Re-runs skip cases already under CMOD.

1. **Linking to CRD (§7).** A CMOD case whose Memo/Notice ref already sits on a CRD case (CRD stores it in `reference_number_internal`) is not duplicated: that case moves to CMOD, CMOD's letters merge into it by type (the existing Notice keeps its internal ref and gains CMOD's as external), CMOD domains missing from it are added, and CRD's offences are kept. A moved case's domains also leave CRD's watchlist when no other CRD case covers them (only auto-linked, disabled rows; 212 locally). Where the two disagree, CMOD's sheet wins (e.g. case 54's Notice is Draft though CRD recorded it blocked). 55 older CRD cases with `CMOD/BLK/2024|2025` refs aren't in this sheet and stay with CRD (decided 2026-09-30).
2. **Status (§1).** CMOD has no per-domain status: its `case_urls.status` is `""`, including the 50 moved cases (their CRD `blocked` etc. is dropped). Its status is each letter's `workflow_status` (Draft/Pending Legal/Pending TSC/Submitted), all imported including drafts. The server rejects a domain status on a CMOD case and a workflow status on any other department's letter, and the UI shows each department only its own kind — including the Cases/Domain view filters (`workflow_status` query param, matched against any of the case's letters, since Pending Legal/TSC only occur on Memos). `BlockingStats` counts a CMOD domain as blocked when its case has a Submitted Notice and no Submitted Notice (Uplift).
3. **Offences (§2).** CMOD records only a category, so the citation is the one CRD used for the same notices: Judi dalam talian → Akta Rumah Judi Terbuka 1953 s4(1) › Judi; Palsu / Lucah / Jelik Melampau → AKM 1998 s233 › same name (`cmodOffences`). The compound `Palsu, Jelik Melampau` becomes two offences. Only domains without an offence on that case get one.
4. **Agency (§3).** Per domain (`case_urls.agency_id`), from that row's `Agency`, get-or-created.
5. **OIC (§6).** One CMOD account per distinct OIC name (`Mas Atika` → `mas_atika`), all with the `--oic-password` and a forced change on first login. A letter links its first OIC; a letter covering domains from several OICs keeps the full list in Remarks (`OIC: A; B`).

The sections below are the original EDA; questions they raise are answered above.

## Workbook shape

4 sheets: `FAKE NEWS`, `BLK`, `EVENT`, `xCRR`. Only **`BLK`** (499 rows, header on row 2) has real data and matches the DNS-blocking domain model. The other three are correspondence-tracking templates for unrelated case types (fake-news takedown letters, misc events, content-removal casework under a different "URLs get removed, not DNS-blocked" concept) — each has only a single seeded row number and a lone example `Reference No`, no actual rows filled in. **Out of scope**, same call as CRD's DNS-server sheet.

`BLK` columns: `No, Letter Date, Recipient, Type, Subject, Reference No, OIC, Requestor, Offence, Link (One Link Per Row), Remarks, Agency, Status, Received, Submission`.

## This sheet tracks correspondence, not blocking events — grain mismatch

CRD's sheet was one row per (domain, blocking-event). CMOD's `BLK` sheet is one row per **letter** in a request's paper trail. Every blocking request produces a `Memo` (internal ratification, ref suffix `-1`) then a `Notice` (sent to ISPs, suffix `-2`); if later uplifted, a `Memo (Uplift)` (`-3`) and `Notice (Uplift)` (`-4`) follow — confirmed by cross-tabbing the ref-no suffix against `Type`, which lines up perfectly (241×Memo→`-1`, 243×Notice→`-2`, 6×Memo(Uplift)+1×Notice(Uplift)→`-3`, 1×Memo(Uplift)+7×Notice(Uplift)→`-4`).

Stripping the `-N` suffix from `Reference No` recovers the real case identifier (**this is the "ref no" tie the ask referred to**):

- 499 rows → 65 distinct base cases → 254 distinct (case, URL) pairs → 244 distinct URLs.
- i.e. each real blocking-event is duplicated 2× (block) or 4× (block+uplift) as separate correspondence rows for the same URL. Importing rows as-is would double- or quadruple-count.
- 10 URLs appear under more than one base case (re-blocked later under a new case number) — genuine repeat history, same shape as CRD's repeated domains.

**Decision:** don't drop the Memo/Notice split — the new table carries both as separate columns, `memo_ref_number` and `notice_ref_number`, on one row per (base case, URL, phase). "Phase" is block (`-1`/`-2`) vs. uplift (`-3`/`-4`); a URL that gets uplifted produces a second row (new phase, `Status`→uplift) rather than overwriting the first, preserving the history CRD open-question #2 asked about. `memo_ref_number` is CMOD-internal (never left the department, never appears in CRD — see §7 below); `notice_ref_number` is the one that ties to the wider system.

## 1. `Status` is a workflow state, not a blocking outcome — worse mismatch than CRD

Values: `Submitted` (454), `Draft` (38), `Pending Legal` (6), `Pending TSC` (1). None match the app's allowed set (`internal/server/handlers.go:243`: `"" | requested | uplift | suspended`) — and unlike CRD, none are even a casing-only near-match. This `Status` describes **letter approval progress** (draft → pending legal/TSC sign-off → submitted), a different axis entirely from CRD's blocked/uplift/suspended case status.

**Question:** is there a real case-status signal to derive here at all? Candidates: derive `requested`/`uplift` from `Type` (`Notice`→blocked/requested, `Notice (Uplift)`→uplift) instead of the `Status` column; treat `Status` as a separate "submission workflow" field with no home in the current schema (drop it, or store in `Remarks`)?

## 2. `Offence` maps cleanly to `Category` — cleaner than CRD

Only 5 values: `Judi dalam talian` (412), `Palsu` (46), `Lucah` (32), `Jelik Melampau` (7), and one compound `Palsu, Jelik Melampau` (2 rows) — same comma-joined-compound issue CRD had (§3 in the CRD doc), just far smaller. No column-shift bug, no undocumented sprawl.

**Question:** same as CRD's — do the 2 compound rows split into two `URLOffence` links, or pick one primary? And is `Judi`/`Palsu`/`Lucah`/`Jelik Melampau` the same taxonomy as CRD's `Kategori`, or a separate namespace (CMOD is casework, CRD looked like a different department's historical log)? If shared, needs one merged `Category` seed list across both sources.

Note `Offence` has no `Elemen`/`Sub-Elemen` split at all — CMOD rows can only ever populate `url_offences.category_id`, never `element_id`/`sub_element_id`.

## 3. `Agency` — only 2 values, need confirming against the new `agencies` table

`PDRM` (404), `MCMC` (95). Clean, but the `agencies` table (added today per `docs/db-schema.dbml`) is presumably unseeded — confirm both names get seeded/`GetOrCreate`d rather than assumed to exist.

## 4. `Reference No` format is inconsistent, one malformed row

- 495 rows use `MCMC(S)CMOD/BLK/2026(NN-N)`; 4 rows (`base_case` 64, 65) use a longer prefix `MCMC(S)NSS/CPMD/CMOD/BLK/2026(NN-N)` — same case series, different prefix, presumably just inconsistent typing rather than a different series.
- 1 row: `MCMC(S)CMOD/BLK/2026(19-1, 20-1, 21-1)` — three case numbers crammed into one cell (comma-joined, mirrors the CRD compound-category bug but in the ref-no field instead). Needs manual correction/splitting before it can key anything.

**Question:** confirm this is a data-entry error to fix (split to 3 real ref-no values) rather than one case that legitimately spans 3 case numbers.

## 5. Link column: 2 rows violate the sheet's own "One Link Per Row" header

`edisisiasat4.wordpress.com\nedisisiasat5.wordpress.com` (2 rows, same base case) and a 13-URL newline-joined block under case 65 (2 rows). These fail hostname normalization outright as a single value — confirmed by replicating `urlnorm.Normalize`'s logic in Python against all 244 unique `Link` values: exactly these 2 fail, everything else (including 46 links that carry a path like `128mega.net/web/index` or a scheme) normalizes cleanly since `Normalize` strips scheme/path.

**Question:** split each newline-joined cell into one row per URL, all sharing the same base case/ref no? (Straightforward fix, just flagging it's needed — much cleaner than CRD's 26 unrecoverable/garbled URLs, none of these are actually corrupted.)

## 6. Columns with no home in the current schema

- `Recipient`, `Type`, `Subject`, `OIC`, `Requestor`, `Received`, `Submission` — correspondence/workflow metadata (who the letter went to, who handled it, internal timestamps) that CRD's sheet didn't have and `urls`/`url_offences` has no columns for. `Received`/`Submission` are also sparse (441/499 and 112/499 null respectively) and inconsistently formatted (`Letter Date` mixes plain date strings like `1-Jan-2026` with 4 actual Excel datetime cells; `Received` sometimes carries a time-of-day, sometimes just a date).
- `Remarks` — same gap as CRD (open question there too, never resolved): no `remarks` column exists on `urls` today.

**Question:** is any of this worth capturing (e.g. `OIC` as a lightweight case-owner field), or is it fine to drop entirely on import since none of it is queryable/displayed in the app today?

## 7. Tying CMOD to CRD — via `notice_ref_number` only, not any CMOD ref

**Correction from an earlier pass of this doc**, which claimed the two ref-no namespaces don't overlap at all — checked properly and that was wrong. There *is* a literal, exact-string overlap, but only through one specific column pairing:

- Checked every CMOD `Reference No` against both of CRD's reference columns (`No. Rujukan NMD` and `No. Rujukan NMSMD`), broken down by CMOD `Type`:

  | CMOD `Type` (suffix) | distinct refs | found in CRD `No. Rujukan NMD` | found in `No. Rujukan NMSMD` |
  |---|---|---|---|
  | Memo (`-1`) | 62 | 0 | 0 |
  | **Notice (`-2`)** | **65** | **50** | 0 |
  | Memo (Uplift) (`-3`) | 3 | 0 | 0 |
  | Notice (Uplift) (`-4`) | 4 | 0 | 0 |

  At the row level: 220 of CRD's 2026-tagged rows carry a `No. Rujukan NMD` that exactly matches a CMOD row, and every one of those 220 is a `Notice`-type row. Zero Memo, zero Uplift (either type), zero hits against `NMSMD` at all.
- This lines up with what each ref actually is: `memo_ref_number` is CMOD's internal ratification request, never sent outside the department — CRD has no way to know it exists. `notice_ref_number` is what actually goes out to ISPs, i.e. the citable reference for why a domain is blocked — that's the one CRD would file under `No. Rujukan NMD`.
- Of CMOD's 65 distinct Notice refs, 50 (77%) already match a CRD row; the other 15 don't yet — either not transcribed into CRD yet, or this year's CMOD activity CRD hasn't caught up to. (Separately, domain-level overlap is broader: 231 of CMOD's 242 normalizable domains — 95% — appear *somewhere* in CRD's 34,312 domains, including via older non-CMOD ref numbers like police references. Domain overlap is the fallback signal when `notice_ref_number` doesn't hit; it is not itself a join key since `urls.url` isn't unique-per-case the way a reference number is.)

**Decision:** `notice_ref_number` is the tie to the existing system — it maps onto `urls.reference_number` (CRD's `No. Rujukan NMD` is presumably what populated that field on import). `memo_ref_number` has no CRD counterpart and stays CMOD-only metadata.

**Open question:** for the 15 Notice refs (and all Memo/Uplift refs) with no CRD match, and the domains that only overlap by name (not by ref number) — same reconcile-vs-append question as before: does CMOD's data update the existing `urls` row for that domain, or land as an additional `url_offences` entry alongside whatever CRD already recorded?

---

Both this file and the CRD one live in `docs/` uncommitted pending sign-off on the open questions — happy to combine them into one importer spec once decisions land on: status mapping (both sources), one-row-per-case collapsing (both), compound category/offence splitting (both), and the domain-level CRD↔CMOD overlap check (this one).
