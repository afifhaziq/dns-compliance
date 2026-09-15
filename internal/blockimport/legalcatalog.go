package blockimport

import (
	"context"
	"encoding/csv"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/legalcite"
	"gorm.io/gorm"
)

// citationTarget is one (Instrument, Citation-provision) pair a raw
// spreadsheet citation resolves to. Usually one per raw citation; a compound
// cell like "Seksyen 211 dan 233 Akta..." resolves to two, per the
// compound-citation general rule in docs/blocking-list-migration-clarifications.md §3.
type citationTarget struct {
	Instrument string // free-text instrument name, e.g. "Akta Rumah Judi Terbuka 1953 (Akta 289)"
	Provision  string // e.g. "Seksyen 4(1)(c)"
}

// LoadCitationClassification reads the hand-classified citation lookup
// (docs/blocking-list-citation-classification.csv: raw_citation, row_count,
// instrument, citation_provision, status, notes) into a map keyed by the
// exact raw "Butiran Kesalahan" text. Only status=="confirmed" rows are
// included -- an unclassified or still-open raw citation simply has no
// entry, which the caller treats as "no offence to attach" rather than
// guessing. A raw_citation can appear more than once (one row per split
// provision for a compound cell), so the value is a slice.
func LoadCitationClassification(path string) (map[string][]citationTarget, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := headerIndex(header)
	rawCol, instrCol, provCol, statusCol := idx["raw_citation"], idx["instrument"], idx["citation_provision"], idx["status"]

	out := make(map[string][]citationTarget)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if cellAt(row, statusCol) != "confirmed" {
			continue
		}
		raw := cellAt(row, rawCol)
		if raw == "" {
			continue
		}
		out[raw] = append(out[raw], citationTarget{
			Instrument: cellAt(row, instrCol),
			Provision:  cellAt(row, provCol),
		})
	}
	return out, nil
}

var (
	// trailingAktaNumberRe pulls "(Akta 289)"-style official-number suffixes
	// off the end of an instrument string, e.g. "Akta Rumah Judi Terbuka
	// 1953 (Akta 289)" -> Number "289", ShortTitle "Akta Rumah Judi Terbuka 1953".
	trailingAktaNumberRe = regexp.MustCompile(`\s*\(Akta (\d+)\)\s*$`)
	// trailingYearRe pulls a trailing 4-digit year off the (Number-stripped)
	// short title. Absent for the one instrument cited without a year by
	// convention (Kanun Keseksaan, the Penal Code).
	trailingYearRe = regexp.MustCompile(`\b(\d{4})\s*$`)
)

// malaysianStates is the fixed 13-state jurisdiction list this app already
// uses (see JURISDICTIONS in web/src/routes/legal-citations.tsx) -- an
// instrument naming one of these (however it's positioned in the string,
// with or without a "Negeri" prefix) is state-scoped; everything else,
// including "Wilayah Persekutuan" enactments, is FEDERAL.
var malaysianStates = []string{
	"Johor", "Kedah", "Kelantan", "Melaka", "Negeri Sembilan", "Pahang",
	"Perak", "Perlis", "Pulau Pinang", "Sabah", "Sarawak", "Selangor", "Terengganu",
}

// parseInstrumentText mechanically derives an Instrument's structured
// fields from the free-text name a human already classified each raw
// citation under (docs/blocking-list-citation-classification.csv's
// "instrument" column) -- no external lookup, nothing invented beyond
// what's already spelled out in that string, per the same "don't guess"
// rule applied to the unrecoverable-URL rows (item 13).
func parseInstrumentText(raw string) db.Instrument {
	shortTitle := strings.TrimSpace(raw)
	number := ""
	if m := trailingAktaNumberRe.FindStringSubmatchIndex(shortTitle); m != nil {
		number = shortTitle[m[2]:m[3]]
		shortTitle = strings.TrimSpace(shortTitle[:m[0]])
	}
	var year *int
	if m := trailingYearRe.FindStringSubmatch(shortTitle); m != nil {
		if y, err := strconv.Atoi(m[1]); err == nil {
			year = &y
		}
	}
	jurisdiction := "FEDERAL"
	for _, state := range malaysianStates {
		if strings.Contains(shortTitle, state) {
			jurisdiction = state
			break
		}
	}
	return db.Instrument{
		Type:         instrumentType(shortTitle),
		Jurisdiction: jurisdiction,
		Number:       number,
		Year:         year,
		ShortTitle:   shortTitle,
	}
}

// instrumentType maps the instrument name's leading Malay word onto
// Instrument.Type's enum -- see INSTRUMENT_TYPE_LABELS in
// web/src/routes/legal-citations.tsx for the exact word<->type pairing this
// mirrors (Peraturan == REGULATION specifically; Kaedah, "Rules", has no
// dedicated enum value and falls under the generic SUBSIDIARY).
func instrumentType(shortTitle string) string {
	first, _, _ := strings.Cut(shortTitle, " ")
	switch first {
	case "Enakmen":
		return "ENACTMENT"
	case "Ordinan":
		return "ORDINANCE"
	case "Peraturan", "Peraturan-peraturan":
		return "REGULATION"
	case "Kaedah-Kaedah":
		return "SUBSIDIARY"
	default: // "Akta", "Kanun" (Penal Code, enacted as an Act), anything else
		return "ACT"
	}
}

// getOrCreateInstrument replicates postgresStore.GetOrCreateInstrument
// (internal/db/legalcite.go) against a raw *gorm.DB -- that method is
// unexported and scoped to db.Store, same reasoning as createURL above.
func getOrCreateInstrument(ctx context.Context, tx *gorm.DB, in db.Instrument) (db.Instrument, error) {
	q := tx.WithContext(ctx).Where("type = ? AND jurisdiction = ? AND number = ?", in.Type, in.Jurisdiction, in.Number)
	if in.Year != nil {
		q = q.Where("year = ?", *in.Year)
	} else {
		q = q.Where("year IS NULL")
	}
	var existing db.Instrument
	err := q.Attrs(in).FirstOrCreate(&existing).Error
	return existing, err
}

// getOrCreateCitation get-or-creates a Citation under instrumentID by its
// exact provision text, computing Parsed/SortKey/ParseConfidence via
// legalcite.Parse the same way CreateCitation's caller
// (ParseCitationPreview + CreateCitation, internal/server/legal_handlers.go)
// does -- NEEDS_REVIEW citations (an unparseable provision, still real
// data) are stored as-is rather than rejected, matching that flow.
func getOrCreateCitation(ctx context.Context, tx *gorm.DB, instrumentID uint, provisionText string) (db.Citation, error) {
	result := legalcite.Parse(provisionText)
	target := db.Citation{
		InstrumentID:    instrumentID,
		RawText:         provisionText,
		Parsed:          legalParsedToDB(result.Parsed),
		ParseConfidence: result.Confidence,
		SortKey:         db.BuildProvisionSortKey(result.Parsed.ProvisionNum, result.Parsed.ProvisionSuffix),
	}
	var existing db.Citation
	err := tx.WithContext(ctx).Where("instrument_id = ? AND raw_text = ?", instrumentID, provisionText).
		Attrs(target).FirstOrCreate(&existing).Error
	return existing, err
}

// legalParsedToDB mirrors internal/server/legal_handlers.go's function of
// the same name -- legalcite.Parsed carries no JSON tags by design (the
// package returns a plain struct for each caller to convert), so this
// small copy is duplicated rather than shared across packages.
func legalParsedToDB(p legalcite.Parsed) db.LegalCitationParsed {
	return db.LegalCitationParsed{
		Part: p.Part, Chapter: p.Chapter,
		ProvisionNum: p.ProvisionNum, ProvisionSuffix: p.ProvisionSuffix,
		SubProvision: p.SubProvision, SubProvisionSuffix: p.SubProvisionSuffix,
		Paragraph: p.Paragraph, Subparagraph: p.Subparagraph, SubSubparagraph: p.SubSubparagraph,
		Schedule: p.Schedule, ScheduleList: p.ScheduleList,
	}
}

func getOrCreateCategory(ctx context.Context, tx *gorm.DB, citationID uint, name string) (db.Category, error) {
	var existing db.Category
	err := tx.WithContext(ctx).Where("citation_id = ? AND name = ?", citationID, name).
		Attrs(db.Category{CitationID: citationID, Name: name}).FirstOrCreate(&existing).Error
	return existing, err
}

func getOrCreateElement(ctx context.Context, tx *gorm.DB, categoryID uint, name string) (db.Element, error) {
	var existing db.Element
	err := tx.WithContext(ctx).Where("category_id = ? AND name = ?", categoryID, name).
		Attrs(db.Element{CategoryID: categoryID, Name: name}).FirstOrCreate(&existing).Error
	return existing, err
}

func getOrCreateSubElement(ctx context.Context, tx *gorm.DB, elementID uint, name string) (db.SubElement, error) {
	var existing db.SubElement
	err := tx.WithContext(ctx).Where("element_id = ? AND name = ?", elementID, name).
		Attrs(db.SubElement{ElementID: elementID, Name: name}).FirstOrCreate(&existing).Error
	return existing, err
}

// attachOffences creates one URLOffence per (citation target x category)
// combination for every url in urlIDs -- both axes can be plural (a
// compound citation cell splits into several targets; a compound Kategori
// cell splits into several categories), and per the compound rule in
// docs/blocking-list-migration-clarifications.md §3, every combination is
// an independent real fact, not an ambiguous one to pick among.
func attachOffences(ctx context.Context, tx *gorm.DB, urlIDs []uint, targets []citationTarget, categories []string, element, subElement string) (int, error) {
	created := 0
	for _, target := range targets {
		instrument, err := getOrCreateInstrument(ctx, tx, parseInstrumentText(target.Instrument))
		if err != nil {
			return created, err
		}
		citation, err := getOrCreateCitation(ctx, tx, instrument.ID, target.Provision)
		if err != nil {
			return created, err
		}
		for _, catName := range categories {
			category, err := getOrCreateCategory(ctx, tx, citation.ID, catName)
			if err != nil {
				return created, err
			}
			var elementID, subElementID *uint
			if element != "" {
				el, err := getOrCreateElement(ctx, tx, category.ID, element)
				if err != nil {
					return created, err
				}
				elementID = &el.ID
				if subElement != "" {
					se, err := getOrCreateSubElement(ctx, tx, el.ID, subElement)
					if err != nil {
						return created, err
					}
					subElementID = &se.ID
				}
			}
			for _, urlID := range urlIDs {
				offence := db.URLOffence{URLID: urlID, CategoryID: category.ID, ElementID: elementID, SubElementID: subElementID, RecordedAt: time.Now()}
				if err := tx.WithContext(ctx).Create(&offence).Error; err != nil {
					return created, err
				}
				created++
			}
		}
	}
	return created, nil
}
