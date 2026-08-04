package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/legalcite"
	"github.com/go-chi/chi/v5"
)

// Instruments

func (h *Handlers) ListInstruments(w http.ResponseWriter, r *http.Request) {
	instruments, err := h.store.ListInstruments(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instruments)
}

type instrumentBody struct {
	Type         string `json:"type"`
	Jurisdiction string `json:"jurisdiction"`
	Number       string `json:"number"`
	Year         *int   `json:"year"`
	ShortTitle   string `json:"short_title"`
}

// Number is deliberately not required — plenty of Malaysian instruments
// (older pre-1968-revision Acts, most state Enactments) have no commonly
// cited official number, or the analyst simply doesn't have it on hand.
// GetOrCreateInstrument's dedup key still includes it, so leaving it blank
// only risks under-deduping (two different unnumbered same-year same-type
// instruments colliding) — never a false negative on a real duplicate.
func (b instrumentBody) valid() bool {
	return b.Type != "" && b.Jurisdiction != "" && b.ShortTitle != ""
}

// CreateInstrument gets-or-creates by (type, jurisdiction, number, year) so
// the same law is never duplicated across citations — matching CreateURL's
// get-or-create pattern.
func (h *Handlers) CreateInstrument(w http.ResponseWriter, r *http.Request) {
	var body instrumentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.valid() {
		writeError(w, http.StatusBadRequest, "type, jurisdiction, and short_title are required")
		return
	}
	instrument, err := h.store.GetOrCreateInstrument(r.Context(), db.Instrument{
		Type: body.Type, Jurisdiction: body.Jurisdiction, Number: body.Number, Year: body.Year, ShortTitle: body.ShortTitle,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, instrument)
}

func (h *Handlers) UpdateInstrument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body instrumentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.valid() {
		writeError(w, http.StatusBadRequest, "type, jurisdiction, and short_title are required")
		return
	}
	instrument, err := h.store.UpdateInstrument(r.Context(), uint(id), db.Instrument{
		Type: body.Type, Jurisdiction: body.Jurisdiction, Number: body.Number, Year: body.Year, ShortTitle: body.ShortTitle,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instrument)
}

// DeleteInstrument cascades to every Citation/Category/Element/URLOffence
// beneath it — see the FK OnDelete:CASCADE chain on db.Citation etc.
func (h *Handlers) DeleteInstrument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteInstrument(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Citations

func (h *Handlers) ListCitationsByInstrument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	citations, err := h.store.ListCitationsByInstrument(r.Context(), uint(id))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, citations)
}

// legalParsedToDB converts a legalcite.Parse result into the db-shaped,
// JSON-tagged struct clients expect — legalcite.Parsed itself carries no
// JSON tags, by design (see the package doc: it returns a plain struct for
// the caller to convert, rather than depending on internal/db).
func legalParsedToDB(p legalcite.Parsed) db.LegalCitationParsed {
	return db.LegalCitationParsed{
		Part: p.Part, Chapter: p.Chapter,
		ProvisionNum: p.ProvisionNum, ProvisionSuffix: p.ProvisionSuffix,
		SubProvision: p.SubProvision, SubProvisionSuffix: p.SubProvisionSuffix,
		Paragraph: p.Paragraph, Subparagraph: p.Subparagraph, SubSubparagraph: p.SubSubparagraph,
		Schedule: p.Schedule, ScheduleList: p.ScheduleList,
	}
}

// ParseCitationPreview runs legalcite.Parse against free-text input without
// persisting anything — backs the live preview shown while an analyst types
// a citation, before confirming or hand-correcting it.
func (h *Handlers) ParseCitationPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RawText string `json:"raw_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RawText == "" {
		writeError(w, http.StatusBadRequest, "raw_text is required")
		return
	}
	result := legalcite.Parse(body.RawText)
	writeJSON(w, http.StatusOK, map[string]any{
		"parsed":           legalParsedToDB(result.Parsed),
		"parse_confidence": result.Confidence,
	})
}

type citationBody struct {
	InstrumentID    uint                   `json:"instrument_id"`
	RawText         string                 `json:"raw_text"`
	Parsed          db.LegalCitationParsed `json:"parsed"`
	ParseConfidence string                 `json:"parse_confidence"`
}

// CreateCitation persists the client-confirmed (or hand-corrected) parse
// from ParseCitationPreview. The server never re-parses RawText on save —
// only the client-supplied Parsed/ParseConfidence are trusted — but it does
// always recompute SortKey itself server-side (see db.CreateCitation).
func (h *Handlers) CreateCitation(w http.ResponseWriter, r *http.Request) {
	var body citationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.InstrumentID == 0 || body.RawText == "" {
		writeError(w, http.StatusBadRequest, "instrument_id and raw_text are required")
		return
	}
	if body.ParseConfidence == "" {
		body.ParseConfidence = legalcite.ConfidenceNeedsReview
	}
	citation, err := h.store.CreateCitation(r.Context(), db.Citation{
		InstrumentID: body.InstrumentID, RawText: body.RawText, Parsed: body.Parsed, ParseConfidence: body.ParseConfidence,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, citation)
}

func (h *Handlers) UpdateCitation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body citationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.InstrumentID == 0 || body.RawText == "" {
		writeError(w, http.StatusBadRequest, "instrument_id and raw_text are required")
		return
	}
	if body.ParseConfidence == "" {
		body.ParseConfidence = legalcite.ConfidenceNeedsReview
	}
	citation, err := h.store.UpdateCitation(r.Context(), uint(id), db.Citation{
		InstrumentID: body.InstrumentID, RawText: body.RawText, Parsed: body.Parsed, ParseConfidence: body.ParseConfidence,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, citation)
}

// DeleteCitation cascades to every Category/Element/URLOffence beneath it.
func (h *Handlers) DeleteCitation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteCitation(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Categories

func (h *Handlers) ListCategoriesByCitation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	categories, err := h.store.ListCategoriesByCitation(r.Context(), uint(id))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, categories)
}

// CreateCategory is scoped to one citation, not a shared lookup — see
// db.Category's doc comment.
func (h *Handlers) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CitationID uint   `json:"citation_id"`
		Name       string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CitationID == 0 || body.Name == "" {
		writeError(w, http.StatusBadRequest, "citation_id and name are required")
		return
	}
	category, err := h.store.CreateCategory(r.Context(), db.Category{CitationID: body.CitationID, Name: body.Name})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, category)
}

func (h *Handlers) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	category, err := h.store.UpdateCategory(r.Context(), uint(id), body.Name)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, category)
}

// DeleteCategory cascades to every Element/URLOffence beneath it.
func (h *Handlers) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteCategory(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Elements

func (h *Handlers) ListElementsByCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	elements, err := h.store.ListElementsByCategory(r.Context(), uint(id))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, elements)
}

func (h *Handlers) CreateElement(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CategoryID uint   `json:"category_id"`
		Name       string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CategoryID == 0 || body.Name == "" {
		writeError(w, http.StatusBadRequest, "category_id and name are required")
		return
	}
	element, err := h.store.CreateElement(r.Context(), db.Element{CategoryID: body.CategoryID, Name: body.Name})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, element)
}

func (h *Handlers) UpdateElement(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	element, err := h.store.UpdateElement(r.Context(), uint(id), body.Name)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, element)
}

func (h *Handlers) DeleteElement(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteElement(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// URL <-> offence linking. Department-ownership-scoped (404, not 403), like
// ResultsByURL/DomainInfoByURL — an offence record is per-monitored-domain
// enforcement data, not shared infrastructure like the catalog above.

func (h *Handlers) OffencesByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := urlParamFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
		return
	}
	offences, err := h.store.ListOffencesByURL(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, offences)
}

func (h *Handlers) AttachOffence(w http.ResponseWriter, r *http.Request) {
	urlValue, err := urlParamFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
		return
	}
	var body struct {
		CategoryID uint  `json:"category_id"`
		ElementID  *uint `json:"element_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CategoryID == 0 {
		writeError(w, http.StatusBadRequest, "category_id is required")
		return
	}
	offence, err := h.store.AttachOffenceToURL(r.Context(), urlValue, body.CategoryID, body.ElementID, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, offence)
}

// DetachOffence is keyed by the offence's own surrogate ID, not by URL —
// ownership is resolved via GetOffence's preloaded URL before checking.
func (h *Handlers) DetachOffence(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	offence, err := h.store.GetOffence(r.Context(), uint(id))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if offence == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !requireDomainOwnership(h, w, r, offence.URL.URL) {
		return
	}
	if err := h.store.DetachOffenceFromURL(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
