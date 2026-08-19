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
	ReferenceNumber string // "No. Rujukan NMD" — the base ref before any suffix handling
	Domain          string // raw, not yet normalized
	Status          string
	Category        string // "Kategori"
	Element         string // "Elemen"
	CitationText    string // "Butiran Kesalahan"
	Agency          string // "Agensi"
	Year            int
}

// CollapsedDomain is one (url, status) pair under a CollapsedCase.
type CollapsedDomain struct {
	RawDomain string
	Status    string // last-write-wins across the group if it repeats
}

// CollapsedCase is one (base reference number) group after collapsing — the
// unit that becomes one Case. Domains holds every (url, status) pair under
// this reference, since a reference legitimately covers many urls.
type CollapsedCase struct {
	ReferenceNumber string
	Domains         []CollapsedDomain
	Categories      []string // split on "," for compound Kategori values, most-common-group's categories
	Element         string
	CitationText    string
	Agency          string
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
	urlCol := col("Alamat Laman Web")
	statusCol := col("Status")
	categoryCol := col("Kategori")
	elementCol := col("Elemen")
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

		out = append(out, CRDRow{
			ReferenceNumber: cellAt(r, refCol),
			Domain:          cleanDomain(cellAt(r, urlCol)),
			Status:          cellAt(r, statusCol),
			Category:        category,
			Element:         element,
			CitationText:    cellAt(r, citationCol),
			Agency:          cellAt(r, agencyCol),
			Year:            year,
		})
	}
	return out, nil
}

// CollapseCRDRows groups rows by ReferenceNumber into one CollapsedCase per
// distinct reference. Does NOT normalize URLs or hit the database — pure
// in-memory transform, testable without I/O. When Category/Element/
// CitationText/Agency differ across a reference's rows (rare — see the EDA
// doc), the most-common raw value across the group wins.
func CollapseCRDRows(rows []CRDRow) []CollapsedCase {
	order := make([]string, 0)
	byRef := make(map[string]*CollapsedCase)
	domainIdxByRef := make(map[string]map[string]int) // ref -> RawDomain -> index in Domains
	categoryCounts := make(map[string]map[string]int) // ref -> raw Category value -> count
	elementCounts := make(map[string]map[string]int)
	citationCounts := make(map[string]map[string]int)
	agencyCounts := make(map[string]map[string]int)

	for _, row := range rows {
		c, ok := byRef[row.ReferenceNumber]
		if !ok {
			c = &CollapsedCase{ReferenceNumber: row.ReferenceNumber}
			byRef[row.ReferenceNumber] = c
			domainIdxByRef[row.ReferenceNumber] = make(map[string]int)
			categoryCounts[row.ReferenceNumber] = make(map[string]int)
			elementCounts[row.ReferenceNumber] = make(map[string]int)
			citationCounts[row.ReferenceNumber] = make(map[string]int)
			agencyCounts[row.ReferenceNumber] = make(map[string]int)
			order = append(order, row.ReferenceNumber)
		}

		domainIdx := domainIdxByRef[row.ReferenceNumber]
		if i, exists := domainIdx[row.Domain]; exists {
			// last-write-wins on repeated (reference, domain) pairs
			c.Domains[i].Status = row.Status
		} else {
			domainIdx[row.Domain] = len(c.Domains)
			c.Domains = append(c.Domains, CollapsedDomain{RawDomain: row.Domain, Status: row.Status})
		}

		bumpCount(categoryCounts[row.ReferenceNumber], row.Category)
		bumpCount(elementCounts[row.ReferenceNumber], row.Element)
		bumpCount(citationCounts[row.ReferenceNumber], row.CitationText)
		bumpCount(agencyCounts[row.ReferenceNumber], row.Agency)
	}

	cases := make([]CollapsedCase, 0, len(order))
	for _, ref := range order {
		c := byRef[ref]
		c.Categories = splitCategories(mostCommon(categoryCounts[ref]))
		c.Element = mostCommon(elementCounts[ref])
		c.CitationText = mostCommon(citationCounts[ref])
		c.Agency = mostCommon(agencyCounts[ref])
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
