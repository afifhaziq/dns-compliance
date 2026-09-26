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
	"time"
	"unicode"

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
	NoticeDate      *time.Time // "Tarikh Maklum IASP (Blocked)" — nil when blank or not a real date ("NA", "Oct/Nov")
	UpliftDate      *time.Time // "Tarikh Maklum ISP (Uplift)" — same parsing rules
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
	// Offences are this domain's own classifications, one per distinct
	// (citation, category, element, sub-element) tuple among its rows. Kept
	// per domain, not collapsed to one case-wide winner, so a citation is
	// never paired with a category from a different row -- see the note on
	// CollapsedCase's case-level fields below.
	Offences []DomainOffence
}

// DomainOffence is the classification carried by one spreadsheet row.
type DomainOffence struct {
	CitationText string
	Categories   []string // split on "," for compound Kategori values
	Element      string
	SubElement   string
}

func (o DomainOffence) key() string {
	return strings.Join([]string{o.CitationText, strings.Join(o.Categories, ","), o.Element, o.SubElement}, "\x00")
}

// addOffence appends o to d unless d already carries an identical one.
func (d *CollapsedDomain) addOffence(o DomainOffence) {
	if o.key() == "\x00\x00\x00" {
		return // row carried no classification at all
	}
	for _, e := range d.Offences {
		if e.key() == o.key() {
			return
		}
	}
	d.Offences = append(d.Offences, o)
}

// CollapsedCase is one (base reference number) group after collapsing — the
// unit that becomes one Case. Domains holds every (url, status, agency)
// tuple under this reference, since a reference legitimately covers many
// urls.
type CollapsedCase struct {
	ReferenceNumber string
	NMSMD           string // secondary MCMC reference (see CRDRow.NMSMD) -- goes on CaseLetter.ReferenceNumberExternal alongside ReferenceNumber's own internal/external routing
	Domains         []CollapsedDomain
	// Case-level classification: the most common value of each field across
	// the group's rows, each picked independently, so they can mix values
	// from different rows. Only a fallback (WriteCRDCases uses it when no
	// domain carries its own Offences) -- Domains[].Offences is authoritative.
	Categories   []string // split on "," for compound Kategori values, most-common-group's categories
	Element      string
	SubElement   string
	CitationText string
	// NoticeDate is the earliest parseable "Tarikh Maklum IASP (Blocked)" across
	// the group's rows (the CRD export reads it back as the earliest Notice's
	// LetterDate). nil when no row has a real date.
	NoticeDate *time.Time
	// UpliftDate is the same for "Tarikh Maklum ISP (Uplift)"; becomes a Notice
	// (Uplift) letter carrying only that date.
	UpliftDate *time.Time
}

// parseNoticeDate reads the sheet's dd-Mon-yy dates ("30-May-11", "7-Jul-12");
// anything else ("NA", "Oct/Nov", blank) is nil. UTC midnight, matching how the
// dashboard stores letter dates.
func parseNoticeDate(raw string) *time.Time {
	t, err := time.Parse("2-Jan-06", strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	return &t
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
	noticeDateCol := col("Tarikh Maklum IASP (Blocked)")
	upliftDateCol := col("Tarikh Maklum ISP (Uplift)")

	var out []CRDRow
	for _, r := range rows[headerRow+1:] {
		category := cellAt(r, categoryCol)
		element := cellAt(r, elementCol)
		// 12 column-shift rows: Kategori blank, Elemen holds a category name.
		if category == "" && element != "" {
			category, element = element, ""
		}

		subElement := cellAt(r, subElementCol)
		// 11 rows (Jelik, "Ngeri / Grafik keterlaluan") have a Sub-Elemen but
		// no Elemen; a sub-element can't exist without a parent element, so
		// the value is treated as the element (stakeholder decision).
		if element == "" && subElement != "" {
			element, subElement = subElement, ""
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
			SubElement:      subElement,
			CitationText:    cellAt(r, citationCol),
			Agency:          cellAt(r, agencyCol),
			Year:            year,
			NoticeDate:      parseNoticeDate(cellAt(r, noticeDateCol)),
			UpliftDate:      parseNoticeDate(cellAt(r, upliftDateCol)),
		})
	}
	canonicalizeCategoryCasing(out)
	return out, nil
}

// canonicalizeCategoryCasing rewrites every Kategori token to a single
// spelling per case-insensitive form ("Tidak berdaftar" / "Tidak Berdaftar").
// Categories are per-citation rows, so the importer's LOWER(name) get-or-create
// can't merge these across citations -- the spellings would otherwise surface
// as separate choices in the offence picker. The spelling with the most
// capitals wins (the catalog is Title Case throughout, and the sheet's
// lowercase variant is the typo, even where it happens to be commoner); ties
// go to the lexicographically smallest.
func canonicalizeCategoryCasing(rows []CRDRow) {
	caps := func(s string) (n int) {
		for _, r := range s {
			if unicode.IsUpper(r) {
				n++
			}
		}
		return
	}
	best := map[string]string{}
	for _, r := range rows {
		for _, c := range splitCategories(r.Category) {
			l := strings.ToLower(c)
			if b, ok := best[l]; !ok || caps(c) > caps(b) || (caps(c) == caps(b) && c < b) {
				best[l] = c
			}
		}
	}
	for i := range rows {
		parts := splitCategories(rows[i].Category)
		for j, c := range parts {
			parts[j] = best[strings.ToLower(c)]
		}
		rows[i].Category = strings.Join(parts, ",")
	}
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
		c.Domains[domainIdx[row.Domain]].addOffence(DomainOffence{
			CitationText: row.CitationText, Categories: splitCategories(row.Category),
			Element: row.Element, SubElement: row.SubElement,
		})

		bumpCount(categoryCounts[key], row.Category)
		bumpCount(elementCounts[key], row.Element)
		bumpCount(subElementCounts[key], row.SubElement)
		bumpCount(citationCounts[key], row.CitationText)
		bumpCount(nmsmdCounts[key], row.NMSMD)
		c.NoticeDate = earlierDate(c.NoticeDate, row.NoticeDate)
		c.UpliftDate = earlierDate(c.UpliftDate, row.UpliftDate)
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

// earlierDate returns the earlier of two optional dates (nil = absent).
func earlierDate(cur, candidate *time.Time) *time.Time {
	if candidate != nil && (cur == nil || candidate.Before(*cur)) {
		return candidate
	}
	return cur
}

func bumpCount(counts map[string]int, val string) {
	if val == "" {
		return
	}
	counts[val]++
}

// mostCommon returns the value with the highest count. Ties go to the
// lexicographically smallest value so the import is reproducible -- Go's map
// iteration order is random, and a random tie-break made every run of the
// import classify tied cases differently.
// ponytail: arbitrary-but-stable on ties; a first-seen rule would need order
// tracking in every counts map.
func mostCommon(counts map[string]int) string {
	best, bestCount := "", 0
	for val, count := range counts {
		if count > bestCount || (count == bestCount && val < best) {
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
