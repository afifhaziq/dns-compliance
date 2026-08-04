// Package legalcite parses free-text Malaysian legal citation shorthand
// ("Seksyen 233(1)(a)", "Perkara 121(1A)", "Bahagian IX") into structured
// fields, so db.Citation.Parsed can be populated automatically instead of
// requiring manual field-by-field entry. Malay-only by design: this is what
// Malaysian analysts actually record citations in (MCMC's national blocking
// list uses "Seksyen"/"Peraturan" exclusively, never the English "s"/
// "section" shorthand), so there's no dual-language alternation to
// maintain. Standalone package (no internal/db import), matching the
// internal/whois/internal/subfinder convention of returning a plain struct
// that the caller converts into the db.* shape.
package legalcite

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	ConfidenceOK          = "OK"
	ConfidenceNeedsReview = "NEEDS_REVIEW"
)

// Parsed mirrors db.LegalCitationParsed's JSON shape. Seksyen (Section) vs
// Perkara (Article) and their subsection/clause levels are never
// distinguished here — that's a display-only switch on
// Instrument.Type == CONSTITUTION applied by the caller; this parser is
// label-optional and instrument-type-agnostic (business rule 4).
type Parsed struct {
	Part               *int
	Chapter            *int
	ProvisionNum       *int
	ProvisionSuffix    string
	SubProvision       *int
	SubProvisionSuffix string
	Paragraph          string
	Subparagraph       string
	SubSubparagraph    string
	Schedule           *int
	ScheduleList       *int
}

type Result struct {
	Parsed     Parsed
	Confidence string // ConfidenceOK | ConfidenceNeedsReview
	SortKey    string
}

// SortKey is the same computation as db.BuildProvisionSortKey, duplicated
// here (not imported, per this package's no-internal/db-dependency rule) so
// a live preview can show the correct sort position before anything is
// persisted. A nil num sorts first via "".
func SortKey(num *int, suffix string) string {
	if num == nil {
		return ""
	}
	return fmt.Sprintf("%06d%s", *num, strings.ToUpper(suffix))
}

var (
	partRe    = regexp.MustCompile(`(?i)\bbahagian\s+([ivxlcdm]+|\d+)\b`)
	chapterRe = regexp.MustCompile(`(?i)\bbab\s+([ivxlcdm]+|\d+)\b`)
	// Malay drafting puts the keyword first, opposite of the English "First
	// Schedule" order — "Jadual Kesembilan", not "Kesembilan Jadual" (same
	// keyword-first pattern as bahagian/bab/seksyen above).
	scheduleRe     = regexp.MustCompile(`(?i)\bjadual\s+(kedua belas|pertama|kedua|ketiga|keempat|kelima|keenam|ketujuh|kelapan|kesembilan|kesepuluh|kesebelas|\d+)\b`)
	scheduleListRe = regexp.MustCompile(`(?i),?\s*senarai\s+([ivxlcdm]+|\d+)\b`)
	// Label group is optional (a bare "233(1)(a)" with no leading word still
	// parses) but when present it must be one of these — Malay only.
	// MCMC's national blocking list (the real-world data this parser is
	// built against) uses "Seksyen"/"Peraturan" exclusively; there's no
	// English "s"/"section" input to support in practice, so keeping only
	// one language avoids the alternation-ordering/prefix-collision
	// bookkeeping a dual-language list would need (see git history if that
	// ever needs to come back).
	provisionRe = regexp.MustCompile(`(?i)^(?:(?:seksyen|peraturan|perkara|fasal)\.?\s*)?(\d+)([a-zA-Z]?)`)
	// A number followed anywhere later by ", <digit>", "& [seksyen] <digit>",
	// or "dan [seksyen] <digit>" is a list of provisions joined into one
	// string (e.g. "Seksyen 211 dan 233 Akta ...", "Seksyen 7, 9, 10 ... dan
	// 16 Enakmen ...", "Seksyen 292 & Seksyen 372 ...", "Seksyen 4(1), 7(a),
	// 9 dan 12 ..." — the list marker doesn't have to sit immediately after
	// the first provision number; it can follow a subsection/paragraph
	// paren first). A Citation only has one provision_num field, so
	// silently keeping just the first number would quietly drop the rest —
	// flag NEEDS_REVIEW instead. Deliberately not anchored to the start of
	// tail, so it still fires when a paren group comes first; \b before
	// "dan" prevents matching inside an unrelated word ending in "...dan".
	multiProvisionRe = regexp.MustCompile(`(?i)(?:,\s*\d|&\s*(?:seksyen\s+)?\d|\bdan\s+(?:seksyen\s+)?\d)`)
	parenGroupRe     = regexp.MustCompile(`\(([^()]+)\)`)
	numSuffixRe      = regexp.MustCompile(`^(\d+)([a-zA-Z]?)$`)
	letterRe         = regexp.MustCompile(`^[a-zA-Z]$`)
	romanRe          = regexp.MustCompile(`(?i)^[ivxlcdm]+$`)

	// Malay ordinals for Jadual (Schedule) references, e.g. "Jadual Kesembilan".
	ordinalWords = map[string]int{
		"pertama": 1, "kedua": 2, "ketiga": 3, "keempat": 4, "kelima": 5, "keenam": 6,
		"ketujuh": 7, "kelapan": 8, "kesembilan": 9, "kesepuluh": 10, "kesebelas": 11, "kedua belas": 12,
	}
	romanValues = map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}
)

// romanToInt parses a (subtractive-notation) roman numeral, case-insensitive.
func romanToInt(s string) (int, bool) {
	s = strings.ToUpper(s)
	if s == "" {
		return 0, false
	}
	total := 0
	for i := 0; i < len(s); i++ {
		v, ok := romanValues[s[i]]
		if !ok {
			return 0, false
		}
		if i+1 < len(s) {
			if next, ok := romanValues[s[i+1]]; ok && next > v {
				total -= v
				continue
			}
		}
		total += v
	}
	return total, true
}

// numeralOrOrdinal parses either a plain int, a roman numeral, or (for
// Schedule matches) a Malay ordinal word like "kesembilan".
func numeralOrOrdinal(s string) (int, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if n, ok := ordinalWords[s]; ok {
		return n, true
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	return romanToInt(s)
}

func trimPunct(s string) string {
	return strings.Trim(strings.TrimSpace(s), ",;.")
}

// Parse breaks free-text citation shorthand into a Result. Unparseable or
// ambiguous input sets Confidence to ConfidenceNeedsReview rather than
// guessing, keeping whatever parsed successfully before the point of
// ambiguity — never discarded.
func Parse(raw string) Result {
	var p Parsed
	remainder := strings.TrimSpace(raw)

	if m := partRe.FindStringSubmatchIndex(remainder); m != nil {
		if n, ok := numeralOrOrdinal(remainder[m[2]:m[3]]); ok {
			p.Part = &n
		}
		remainder = trimPunct(remainder[:m[0]] + " " + remainder[m[1]:])
	}
	if m := chapterRe.FindStringSubmatchIndex(remainder); m != nil {
		if n, ok := numeralOrOrdinal(remainder[m[2]:m[3]]); ok {
			p.Chapter = &n
		}
		remainder = trimPunct(remainder[:m[0]] + " " + remainder[m[1]:])
	}

	if remainder == "" {
		// Part/Chapter-only citation (e.g. "Part IX") — complete as-is.
		return Result{Parsed: p, Confidence: ConfidenceOK, SortKey: SortKey(p.ProvisionNum, p.ProvisionSuffix)}
	}

	if m := scheduleRe.FindStringSubmatchIndex(remainder); m != nil {
		if n, ok := numeralOrOrdinal(remainder[m[2]:m[3]]); ok {
			p.Schedule = &n
		}
		tail := remainder[m[1]:]
		if lm := scheduleListRe.FindStringSubmatch(tail); lm != nil {
			if n, ok := numeralOrOrdinal(lm[1]); ok {
				p.ScheduleList = &n
			}
		}
		return Result{Parsed: p, Confidence: ConfidenceOK, SortKey: SortKey(p.ProvisionNum, p.ProvisionSuffix)}
	}

	m := provisionRe.FindStringSubmatchIndex(remainder)
	if m == nil {
		// Nothing recognizable beyond any Part/Chapter already parsed.
		return Result{Parsed: p, Confidence: ConfidenceNeedsReview, SortKey: SortKey(p.ProvisionNum, p.ProvisionSuffix)}
	}
	num, _ := strconv.Atoi(remainder[m[2]:m[3]])
	p.ProvisionNum = &num
	suffixStart, suffixEnd, tailStart := m[4], m[5], m[1]
	if suffixEnd > suffixStart && suffixEnd < len(remainder) && letterRe.MatchString(remainder[suffixEnd:suffixEnd+1]) {
		// The captured "suffix" letter is immediately followed by another
		// letter — it's actually the start of a word (commonly a
		// missing-space typo like "14dan 15" for "14 dan 15"), not a real
		// amendment suffix. Real suffixes (4A, 372B) are always followed by
		// a non-letter (space/paren/end-of-string). Discard the bogus
		// suffix and re-anchor tail right after the digits so the stray
		// letter survives into tail, where multiProvisionRe now catches it.
		tailStart = m[3]
	} else {
		p.ProvisionSuffix = strings.ToUpper(remainder[suffixStart:suffixEnd])
	}
	tail := remainder[tailStart:]

	confidence := ConfidenceOK
	if multiProvisionRe.MatchString(tail) {
		// A joined list ("211 dan 233", "7, 9, 10 ... dan 16") — keep the
		// first provision number captured above (better than nothing) but
		// flag for review rather than silently dropping the rest.
		confidence = ConfidenceNeedsReview
	}
	slot := 0 // 0=subProvision, 1=paragraph, 2=subparagraph, 3=subSubparagraph
	for _, gm := range parenGroupRe.FindAllStringSubmatch(tail, -1) {
		content := strings.TrimSpace(gm[1])
		switch slot {
		case 0:
			nm := numSuffixRe.FindStringSubmatch(content)
			if nm == nil {
				confidence = ConfidenceNeedsReview
			} else {
				n, _ := strconv.Atoi(nm[1])
				p.SubProvision = &n
				p.SubProvisionSuffix = strings.ToUpper(nm[2])
			}
		case 1:
			if !letterRe.MatchString(content) {
				confidence = ConfidenceNeedsReview
			} else {
				p.Paragraph = strings.ToLower(content)
			}
		case 2:
			if !romanRe.MatchString(content) {
				confidence = ConfidenceNeedsReview
			} else {
				p.Subparagraph = strings.ToLower(content)
			}
		case 3:
			if !letterRe.MatchString(content) {
				confidence = ConfidenceNeedsReview
			} else {
				p.SubSubparagraph = strings.ToUpper(content)
			}
		default:
			confidence = ConfidenceNeedsReview
		}
		if confidence == ConfidenceNeedsReview {
			break
		}
		slot++
	}

	return Result{Parsed: p, Confidence: confidence, SortKey: SortKey(p.ProvisionNum, p.ProvisionSuffix)}
}
