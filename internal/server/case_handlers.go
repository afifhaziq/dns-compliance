package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/urlnorm"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

// Cases <-> URL. Department-ownership-scoped (404, not 403), same split as
// the legal-offence routes in legal_handlers.go: a case is per-monitored-
// domain enforcement data, not shared infrastructure like the legal
// citation catalog.

func (h *Handlers) CasesByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := urlParamFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
		return
	}
	cases, err := h.store.ListCasesForURL(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cases)
}

// CreateCaseForURL creates a case anchored to urlValue for the caller's own
// department — department_id is never client-supplied, mirroring how
// AddToWatchlist always uses the caller's own DepartmentID rather than
// trusting the request body.
func (h *Handlers) CreateCaseForURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := urlParamFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	if !requireDomainOwnership(h, w, r, urlValue) {
		return
	}
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if user.DepartmentID == nil {
		writeError(w, http.StatusForbidden, "user has no department")
		return
	}

	var body struct {
		Phase string `json:"phase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !urlStatusAllowed[body.Phase] || body.Phase == "" {
		writeError(w, http.StatusBadRequest, "phase is required and must be one of: requested, uplift, suspended")
		return
	}

	u, err := h.store.GetURLByValue(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if u == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	c, err := h.store.CreateCase(r.Context(), *user.DepartmentID, u.ID, body.Phase)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// AddCaseLetter appends one letter to an existing case, keyed by the case's
// own surrogate ID. A case belongs to exactly one requesting department
// regardless of how many URLs it covers via case_urls, so ownership is
// checked against the case's own DepartmentID — not URL watchlist
// membership, which is a separate, broader axis (multiple departments can
// watch the same domain without owning this case).
func (h *Handlers) AddCaseLetter(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	c, err := h.store.GetCase(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeInternalError(w, err)
		return
	}
	if !user.IsAdmin && (user.DepartmentID == nil || *user.DepartmentID != c.DepartmentID) {
		writeError(w, http.StatusNotFound, "not found") // 404, not 403 — same non-confirming pattern as requireDomainOwnership
		return
	}

	var body struct {
		Type            string     `json:"type"`
		ReferenceNumber string     `json:"reference_number"`
		WorkflowStatus  string     `json:"workflow_status"`
		Recipient       string     `json:"recipient"`
		LetterDate      *time.Time `json:"letter_date"`
		ReceivedAt      *time.Time `json:"received_at"`
		SubmittedAt     *time.Time `json:"submitted_at"`
		Subject         string     `json:"subject"`
		OICUserID       *uint      `json:"oic_user_id"`
		Requestor       string     `json:"requestor"`
		Remarks         string     `json:"remarks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}

	letter, err := h.store.AddCaseLetter(r.Context(), db.CaseLetter{
		CaseID:          uint(id),
		Type:            body.Type,
		ReferenceNumber: body.ReferenceNumber,
		WorkflowStatus:  body.WorkflowStatus,
		Recipient:       body.Recipient,
		LetterDate:      body.LetterDate,
		ReceivedAt:      body.ReceivedAt,
		SubmittedAt:     body.SubmittedAt,
		Subject:         body.Subject,
		OICUserID:       body.OICUserID,
		Requestor:       body.Requestor,
		Remarks:         body.Remarks,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, letter)
}

// AddCaseURL links an additional URL to an existing case — the "N URLs in
// one Notice" shape, needed when a batch of domains is added under one
// case rather than one case per domain. Ownership via the case's own
// DepartmentID, same rationale as AddCaseLetter above (a case belongs to
// exactly one requesting department, not URL watchlist membership).
func (h *Handlers) AddCaseURL(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	c, err := h.store.GetCase(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeInternalError(w, err)
		return
	}
	if !user.IsAdmin && (user.DepartmentID == nil || *user.DepartmentID != c.DepartmentID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var body struct {
		URL   string `json:"url"`
		Phase string `json:"phase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" || !urlStatusAllowed[body.Phase] || body.Phase == "" {
		writeError(w, http.StatusBadRequest, "url and phase are required, phase must be one of: requested, uplift, suspended")
		return
	}
	normalized, err := urlnorm.Normalize(body.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	u, err := h.store.GetURLByValue(r.Context(), normalized)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if u == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	cu, err := h.store.AddURLToCase(r.Context(), uint(id), u.ID, body.Phase)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, cu)
}

// ListCaseLetters is the Docs page's data source — every CaseLetter across
// every case, admin: global, non-admin: scoped to their own department's
// cases (cases.department_id, same ownership axis AddCaseLetter checks),
// paginated like DomainSummaries.
func (h *Handlers) ListCaseLetters(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	pageSize := defaultDomainSummaryPageSize
	if ps, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && ps > 0 && ps <= maxDomainSummaryPageSize {
		pageSize = ps
	}

	var letters []db.CaseLetterEntry
	var total int
	var err error
	if user.IsAdmin {
		letters, total, err = h.store.ListCaseLetters(r.Context(), page, pageSize)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		letters, total, err = h.store.ListCaseLettersForDepartment(r.Context(), page, pageSize, *user.DepartmentID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"letters": letters, "total": total})
}
