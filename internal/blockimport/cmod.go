// Package blockimport parses and writes departments' Excel-tracked
// blocking-case history into cases/case_letters/case_urls.
package blockimport

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"
)

// CMODRow is one raw BLK-sheet row after column-name resolution.
type CMODRow struct {
	LetterDate      string // raw cell text -- mixed formats (plain strings and real Excel datetimes); parsed to *time.Time in write_cmod.go, not here
	Recipient       string
	Type            string // "Memo" | "Notice" | "Memo (Uplift)" | "Notice (Uplift)"
	Subject         string
	ReferenceNumber string // includes the -1/-2/-3/-4 suffix at this stage
	OIC             string
	Requestor       string
	Offence         string
	Links           []string // already split from the raw "Link (One Link Per Row)" cell -- see splitLinks
	Remarks         string
	Agency          string
	Status          string // CMOD's workflow status: Draft | Pending Legal | Pending TSC | Submitted
	Received        string
	Submission      string
}

// cleanHeader strips the "(isi ikut format...)" instructional suffix some
// header cells carry after a newline, e.g. "Received\n(isi ikut format...)".
func cleanHeader(raw string) string {
	return strings.TrimSpace(strings.SplitN(raw, "\n", 2)[0])
}

// ParseCMODRows reads every BLK-sheet data row from path.
func ParseCMODRows(path string) ([]CMODRow, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	rows, err := f.GetRows("BLK")
	if err != nil {
		return nil, fmt.Errorf("reading BLK sheet: %w", err)
	}

	headerRow := -1
	colIdx := map[string]int{}
	for i, row := range rows {
		idx := map[string]int{}
		for c, cell := range row {
			idx[cleanHeader(cell)] = c
		}
		if _, ok := idx["Reference No"]; ok {
			headerRow = i
			colIdx = idx
			break
		}
	}
	if headerRow == -1 {
		return nil, fmt.Errorf("BLK sheet: no header row found (expected a \"Reference No\" column)")
	}

	get := func(row []string, name string) string {
		i, ok := colIdx[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var out []CMODRow
	for _, row := range rows[headerRow+1:] {
		if len(row) == 0 || get(row, "Reference No") == "" {
			continue // blank/trailing row
		}
		for _, ref := range splitMultiRef(get(row, "Reference No")) {
			out = append(out, CMODRow{
				LetterDate:      get(row, "Letter Date"),
				Recipient:       get(row, "Recipient"),
				Type:            get(row, "Type"),
				Subject:         get(row, "Subject"),
				ReferenceNumber: ref,
				OIC:             get(row, "OIC"),
				Requestor:       get(row, "Requestor"),
				Offence:         get(row, "Offence"),
				Links:           splitLinks(get(row, "Link (One Link Per Row)")),
				Remarks:         get(row, "Remarks"),
				Agency:          get(row, "Agency"),
				Status:          get(row, "Status"),
				Received:        get(row, "Received"),
				Submission:      get(row, "Submission"),
			})
		}
	}
	return out, nil
}

// splitLinks splits a "Link (One Link Per Row)" cell on newlines and trims
// each -- handles the rows that violate the sheet's own one-link-per-row
// rule. A normal single-URL cell returns a length-1 slice.
func splitLinks(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, "\n") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// splitMultiRef splits the one known malformed "Reference No" cell that
// crams multiple case numbers into a single comma-joined value, e.g.
// "MCMC(S)CMOD/BLK/2026(19-1, 20-1, 21-1)" -> one ref per case number, each
// keeping the shared prefix. A normal single-ref cell (no comma) returns a
// length-1 slice unchanged.
func splitMultiRef(ref string) []string {
	if !strings.Contains(ref, ",") {
		return []string{ref}
	}
	open := strings.LastIndex(ref, "(")
	closeParen := strings.LastIndex(ref, ")")
	if open == -1 || closeParen == -1 || closeParen < open {
		return []string{ref}
	}
	prefix, inner := ref[:open], ref[open+1:closeParen]
	var out []string
	for _, part := range strings.Split(inner, ",") {
		out = append(out, prefix+"("+strings.TrimSpace(part)+")")
	}
	return out
}

var refSuffixPattern = regexp.MustCompile(`-\d+\)$`)

// baseReferenceNumber strips CMOD's -1/-2/-3/-4 suffix (Memo/Notice/
// Memo (Uplift)/Notice (Uplift)) to recover the real case identifier --
// e.g. "MCMC(S)CMOD/BLK/2026(19-1)" -> "MCMC(S)CMOD/BLK/2026(19)". Works
// regardless of prefix variant (the sheet has two inconsistent prefixes for
// the same case series).
func baseReferenceNumber(ref string) string {
	return refSuffixPattern.ReplaceAllString(ref, ")")
}

// splitOffence splits a comma-joined compound Offence value (e.g. "Palsu,
// Jelik Melampau") into its individual values, trimmed. A single value
// returns a length-1 slice.
func splitOffence(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

var letterTypeOrder = map[string]int{
	"Memo":            0,
	"Notice":          1,
	"Memo (Uplift)":   2,
	"Notice (Uplift)": 3,
}

// CollapsedCMODCase is one base-reference-number group -- the unit that
// becomes one Case with (up to 4) CaseLetter rows.
type CollapsedCMODCase struct {
	BaseReferenceNumber string
	Letters             []CMODRow         // 1-4 rows, one per Type present for this base ref
	URLs                []string          // union of every Links entry across all of this case's rows, deduplicated
	Offence             string            // from whichever row has it set (should agree across the group -- logged if it doesn't, never silently picked)
	AgencyByURL         map[string]string // raw link -> that row's Agency (last non-empty wins)
	OffenceByURL        map[string]string // raw link -> that row's Offence (last non-empty wins)
}

// CollapseCMODRows groups rows by baseReferenceNumber. Pure in-memory
// transform, no I/O.
func CollapseCMODRows(rows []CMODRow) []CollapsedCMODCase {
	type group struct {
		baseRef  string
		byType   map[string][]CMODRow
		urls     []string
		seenURL  map[string]bool
		offence  string
		agency   map[string]string
		offByURL map[string]string
	}
	order := []string{}
	groups := map[string]*group{}

	for _, row := range rows {
		base := baseReferenceNumber(row.ReferenceNumber)
		g, ok := groups[base]
		if !ok {
			g = &group{baseRef: base, byType: map[string][]CMODRow{}, seenURL: map[string]bool{}, agency: map[string]string{}, offByURL: map[string]string{}}
			groups[base] = g
			order = append(order, base)
		}
		g.byType[row.Type] = append(g.byType[row.Type], row)
		for _, u := range row.Links {
			if !g.seenURL[u] {
				g.seenURL[u] = true
				g.urls = append(g.urls, u)
			}
			if row.Agency != "" {
				g.agency[u] = row.Agency
			}
			if row.Offence != "" {
				g.offByURL[u] = row.Offence
			}
		}
		if row.Offence != "" {
			if g.offence == "" {
				g.offence = row.Offence
			} else if g.offence != row.Offence {
				log.Printf("blockimport: case %s has disagreeing Offence values (%q vs %q), keeping the first seen", base, g.offence, row.Offence)
			}
		}
	}

	cases := make([]CollapsedCMODCase, 0, len(order))
	for _, base := range order {
		g := groups[base]
		var types []string
		for t := range g.byType {
			types = append(types, t)
		}
		// stable canonical order: Memo, Notice, Memo (Uplift), Notice (Uplift)
		for i := 0; i < len(types); i++ {
			for j := i + 1; j < len(types); j++ {
				if letterTypeOrder[types[j]] < letterTypeOrder[types[i]] {
					types[i], types[j] = types[j], types[i]
				}
			}
		}
		letters := make([]CMODRow, 0, len(types))
		for _, t := range types {
			letters = append(letters, collapseLetterGroup(g.byType[t]))
		}
		cases = append(cases, CollapsedCMODCase{
			BaseReferenceNumber: base,
			Letters:             letters,
			URLs:                g.urls,
			Offence:             g.offence,
			AgencyByURL:         g.agency,
			OffenceByURL:        g.offByURL,
		})
	}
	return cases
}

// collapseLetterGroup collapses every raw row sharing the same (base
// reference, Type) -- one per domain the letter covers -- down to the
// single CaseLetter this becomes. Scalar fields (Subject/Status/etc.) are
// taken from the first row; OIC can genuinely vary per domain within the
// same letter (observed in the real sheet), so distinct non-empty OIC
// values are joined rather than dropped.
func collapseLetterGroup(rows []CMODRow) CMODRow {
	rep := rows[0]
	if len(rows) == 1 {
		return rep
	}
	seenOIC := map[string]bool{rep.OIC: rep.OIC != ""}
	oics := []string{}
	if rep.OIC != "" {
		oics = append(oics, rep.OIC)
	}
	for _, r := range rows[1:] {
		if r.OIC != "" && !seenOIC[r.OIC] {
			seenOIC[r.OIC] = true
			oics = append(oics, r.OIC)
		}
	}
	rep.OIC = strings.Join(oics, "; ")
	return rep
}
