// Package blockimport parses and imports the CRD/CMOD department's
// Excel-tracked blocklist case history into the cases/case_letters/case_urls
// schema. See docs/blocking-list-migration-clarifications.md for the EDA
// this package implements the decisions from.
package blockimport

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const crdSheetName = "2011-2026"

// CRDRow is one raw spreadsheet row after column-name resolution, before
// any collapsing/validation.
type CRDRow struct {
	ReferenceNumber string // "No. Rujukan NMD" — the base ref before any suffix handling. Falls back to NMSMD when NMD itself is blank (see ParseCRDRows).
	NMSMD           string // "No. Rujukan NMSMD" — a secondary MCMC reference (e.g. a re-block's follow-up case number); "" once consumed as the ReferenceNumber fallback above, so it's never carried twice.
	Domain          string // raw, not yet normalized
	Status          string
	Category        string // "Kategori"
	Element         string // "Elemen"
	SubElement      string // "Sub-Elemen"
	CitationText    string // "Butiran Kesalahan"
	Agency          string // "Agensi"
	Year            int
}

// isInternalReference reports whether a "No. Rujukan NMD"/"NMSMD" value is
// in MCMC/SKMM's own case-numbering scheme (SKMM being the Malay-language
// name of the same regulator, e.g. "SKMM(T)09-NMD/800/2013/Jld.1(011)")
// rather than a reference the requesting agency assigned on its own (e.g.
// PDRM's "JK KPN(PR) 168/6", "SB-2021-0070-HQR", "EP(SIFU)-2020-0004-HQR")
// -- checked against the real file: 1,578 of 1,841 reference numbers
// (85.7%) are MCMC/SKMM-prefixed, and all 140 rows carrying a populated
// NMSMD have an MCMC/SKMM-prefixed NMD (0 have an external-prefixed one).
// An internal reference is this department's own numbering and is trusted
// as a grouping key (see groupingKey); anything else came from outside the
// department, is not trusted as one, and goes on
// CaseLetter.ReferenceNumberExternal instead of ReferenceNumberInternal.
func isInternalReference(ref string) bool {
	upper := strings.ToUpper(strings.TrimSpace(ref))
	return strings.HasPrefix(upper, "MCMC") || strings.HasPrefix(upper, "SKMM")
}

// groupingKey is what CollapseCRDRows actually collapses rows under. A
// shared internal (MCMC/SKMM) reference number groups multiple rows/domains
// into one real case, matching how MCMC actually issues one Notice covering
// many domains. A non-internal reference is NOT trusted as a grouping key --
// some are reused as a blanket case number across thousands of unrelated
// rows from other agencies (e.g. PDRM's "JK KPN(PR) 168/6", 9,206 rows /
// 7,652 distinct domains, several of them not even gambling-related) -- so
// those rows key on (reference, normalized domain) instead, keeping each
// domain in its own case rather than merging unrelated domains' category/
// citation data under one winner. A blank reference (no NMD or NMSMD at
// all) behaves the same way: each domain still gets its own case rather
// than all blank-ref rows piling into one.
//
// The domain is normalized here (not the raw string) specifically so this
// stays consistent with WriteCRDCases's rerun-idempotency check, which can
// only compare against the normalized urls.url column: two raw spellings of
// the same site (e.g. "http://foo.com" and "https://foo.com/") under the
// same blanket reference must collapse into one CollapsedCase here too, or
// the second one would silently vanish later -- CollapseCRDRows would treat
// them as two distinct groups, but the idempotency check would find the
// first one's URL row already existing and skip the second as "already
// imported" without ever writing its data.
func groupingKey(row CRDRow) string {
	if isInternalReference(row.ReferenceNumber) {
		return row.ReferenceNumber
	}
	return row.ReferenceNumber + "\x00" + normalizeOrFallback(row.Domain)
}

// CollapsedDomain is one (url, status, agency) tuple under a CollapsedCase.
type CollapsedDomain struct {
	RawDomain string
	Status    string // last-write-wins across the group if it repeats
	// Agency ("Agensi") is this specific domain's own requesting agency —
	// not collapsed to a case-wide "most common" value (unlike Category/
	// CitationText/Element/SubElement, which still are, see CollapsedCase):
	// verified against the real file, 8 internal references genuinely
	// cover domains requested by different agencies, including 4 with an
	// exact 100/100 split across 800 domains (PDRM's gambling-law citation
	// vs MCMC's obscenity-law citation) — collapsing to one winner there
	// would silently mislabel roughly half of every one of those cases'
	// domains. Last-write-wins across the group if it repeats, same as
	// Status.
	Agency string
}

// CollapsedCase is one (base reference number) group after collapsing — the
// unit that becomes one Case. Domains holds every (url, status, agency)
// tuple under this reference, since a reference legitimately covers many
// urls.
type CollapsedCase struct {
	ReferenceNumber string
	NMSMD           string // secondary MCMC reference (see CRDRow.NMSMD) -- goes on CaseLetter.ReferenceNumberExternal alongside ReferenceNumber's own internal/external routing
	Domains         []CollapsedDomain
	Categories      []string // split on "," for compound Kategori values, most-common-group's categories
	Element         string
	SubElement      string
	CitationText    string
}

var (
	numberedListPrefixRe = regexp.MustCompile(`^\d+\.\s*`)
	strayScheseSpaceRe   = regexp.MustCompile(`^(https?://)\s+`)
)

// findHeaderRow returns the index of the first row containing anchorCol
// (trimmed, exact match), or -1 if none does.
func findHeaderRow(rows [][]string, anchorCol string) int {
	for i, r := range rows {
		for _, v := range r {
			if strings.TrimSpace(v) == anchorCol {
				return i
			}
		}
	}
	return -1
}

func headerIndex(headers []string) map[string]int {
	idx := make(map[string]int, len(headers))
	for i, h := range headers {
		idx[strings.TrimSpace(h)] = i
	}
	return idx
}

func cellAt(row []string, col int) string {
	if col < 0 || col >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[col])
}

// cleanDomain strips a leading numbered-list artifact ("19. http://...")
// and a stray space right after the scheme ("http:// www.foo.com").
func cleanDomain(raw string) string {
	cleaned := numberedListPrefixRe.ReplaceAllString(raw, "")
	cleaned = strayScheseSpaceRe.ReplaceAllString(cleaned, "$1")
	return cleaned
}

// ParseCRDRows reads every data row from the sheet at path, resolving
// columns by header name. Returns an error only for a file-level failure
// (can't open, sheet not found) — a single malformed row is not a parse
// error, it's reflected in the returned rows (empty Domain, etc.) for the
// caller to validate downstream.
func ParseCRDRows(path string) ([]CRDRow, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	rows, err := f.GetRows(crdSheetName)
	if err != nil {
		return nil, fmt.Errorf("reading sheet %q: %w", crdSheetName, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("sheet %q has no rows", crdSheetName)
	}

	// The real file has a multi-row legend/summary preamble above the real
	// header row, so the header isn't always row 0 — locate it by scanning
	// for the row containing the reference-number column, which is present
	// in every version of this sheet.
	headerRow := findHeaderRow(rows, "No. Rujukan NMD")
	if headerRow == -1 {
		return nil, fmt.Errorf("sheet %q: no header row found (looking for %q)", crdSheetName, "No. Rujukan NMD")
	}

	idx := headerIndex(rows[headerRow])
	col := func(name string) int {
		i, ok := idx[name]
		if !ok {
			return -1
		}
		return i
	}

	refCol := col("No. Rujukan NMD")
	nmsmdCol := col("No. Rujukan NMSMD")
	urlCol := col("Alamat Laman Web")
	statusCol := col("Status")
	categoryCol := col("Kategori")
	elementCol := col("Elemen")
	subElementCol := col("Sub-Elemen")
	citationCol := col("Butiran Kesalahan")
	agencyCol := col("Agensi")
	yearCol := col("Tahun")

	var out []CRDRow
	for _, r := range rows[headerRow+1:] {
		category := cellAt(r, categoryCol)
		element := cellAt(r, elementCol)
		// 12 column-shift rows: Kategori blank, Elemen holds a category name.
		if category == "" && element != "" {
			category, element = element, ""
		}

		year, _ := strconv.Atoi(cellAt(r, yearCol))

		// 3 rows have a blank NMD but a populated NMSMD -- rather than lose
		// the only reference number the row actually has, NMSMD stands in
		// for it (and is not also carried separately, since it's now fully
		// consumed as the primary reference). All 3 real occurrences have an
		// MCMC/SKMM-prefixed NMSMD, so this doesn't change routing outcomes.
		ref := cellAt(r, refCol)
		nmsmd := cellAt(r, nmsmdCol)
		if ref == "" && nmsmd != "" {
			ref, nmsmd = nmsmd, ""
		}

		out = append(out, CRDRow{
			ReferenceNumber: ref,
			NMSMD:           nmsmd,
			Domain:          cleanDomain(cellAt(r, urlCol)),
			Status:          cellAt(r, statusCol),
			Category:        category,
			Element:         element,
			SubElement:      cellAt(r, subElementCol),
			CitationText:    cellAt(r, citationCol),
			Agency:          cellAt(r, agencyCol),
			Year:            year,
		})
	}
	return out, nil
}

// CollapseCRDRows groups rows by groupingKey into one CollapsedCase per
// distinct internal reference, or per distinct (reference, domain) pair when
// the reference isn't internal (see groupingKey). Does NOT normalize URLs or
// hit the database — pure in-memory transform, testable without I/O. When
// Category/Element/CitationText/NMSMD differ across a group's rows (rare for
// a real internal case; moot for the single-domain external/blank groups),
// the most-common raw value across the group wins. Agency does NOT collapse
// this way — it travels per-domain on CollapsedDomain, see its doc comment.
func CollapseCRDRows(rows []CRDRow) []CollapsedCase {
	order := make([]string, 0)
	byKey := make(map[string]*CollapsedCase)
	domainIdxByKey := make(map[string]map[string]int) // key -> RawDomain -> index in Domains
	categoryCounts := make(map[string]map[string]int) // key -> raw Category value -> count
	elementCounts := make(map[string]map[string]int)
	subElementCounts := make(map[string]map[string]int)
	citationCounts := make(map[string]map[string]int)
	nmsmdCounts := make(map[string]map[string]int)

	for _, row := range rows {
		key := groupingKey(row)
		c, ok := byKey[key]
		if !ok {
			c = &CollapsedCase{ReferenceNumber: row.ReferenceNumber}
			byKey[key] = c
			domainIdxByKey[key] = make(map[string]int)
			categoryCounts[key] = make(map[string]int)
			elementCounts[key] = make(map[string]int)
			subElementCounts[key] = make(map[string]int)
			citationCounts[key] = make(map[string]int)
			nmsmdCounts[key] = make(map[string]int)
			order = append(order, key)
		}

		domainIdx := domainIdxByKey[key]
		if i, exists := domainIdx[row.Domain]; exists {
			// last-write-wins on repeated (reference, domain) pairs
			c.Domains[i].Status = row.Status
			c.Domains[i].Agency = row.Agency
		} else {
			domainIdx[row.Domain] = len(c.Domains)
			c.Domains = append(c.Domains, CollapsedDomain{RawDomain: row.Domain, Status: row.Status, Agency: row.Agency})
		}

		bumpCount(categoryCounts[key], row.Category)
		bumpCount(elementCounts[key], row.Element)
		bumpCount(subElementCounts[key], row.SubElement)
		bumpCount(citationCounts[key], row.CitationText)
		bumpCount(nmsmdCounts[key], row.NMSMD)
	}

	cases := make([]CollapsedCase, 0, len(order))
	for _, key := range order {
		c := byKey[key]
		c.Categories = splitCategories(mostCommon(categoryCounts[key]))
		c.Element = mostCommon(elementCounts[key])
		c.SubElement = mostCommon(subElementCounts[key])
		c.CitationText = mostCommon(citationCounts[key])
		c.NMSMD = mostCommon(nmsmdCounts[key])
		cases = append(cases, *c)
	}
	return cases
}

func bumpCount(counts map[string]int, val string) {
	if val == "" {
		return
	}
	counts[val]++
}

// mostCommon returns the value with the highest count, breaking ties by
// first-seen-wins (Go map iteration order is randomized, so this scans in a
// deterministic way isn't guaranteed on ties — acceptable since ties only
// occur among values already judged equally representative).
func mostCommon(counts map[string]int) string {
	best, bestCount := "", 0
	for val, count := range counts {
		if count > bestCount {
			best, bestCount = val, count
		}
	}
	return best
}

// splitCategories splits an 8-known compound "Kategori" value ("Jelik,
// Palsu, Lucah") into its parts, trimming whitespace. A non-compound value
// returns a single-element slice; an empty value returns nil.
func splitCategories(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
