package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
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

// oicUserInDepartment reports whether oicUserID refers to a real user
// belonging to departmentID. AddCaseLetter/UpdateCaseLetter use this to
// keep a case letter's OIC pinned to the case's own department — without
// it, any department could set oic_user_id to another department's (or an
// admin's) user id, and ExportCaseLetters would then surface that user's
// username in the exported spreadsheet. A generic false (not
// distinguishing "no such user" from "wrong department") avoids this
// check itself becoming a cross-department user-enumeration oracle.
func (h *Handlers) oicUserInDepartment(ctx context.Context, oicUserID uint, departmentID uint) (bool, error) {
	u, err := h.store.GetUserByID(ctx, oicUserID)
	if err != nil {
		return false, err
	}
	return u != nil && u.DepartmentID != nil && *u.DepartmentID == departmentID, nil
}

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
// trusting the request body. agency_id/due_date are optional case-level
// defaults set at creation time (0/omitted agency_id, omitted/empty
// due_date just leave the field unset) — status is required/validated and
// becomes this url's own CaseURL.Status (see db.Store.CreateCase; status is
// per-url, not stored on Case itself).
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
		Status      string  `json:"status"`
		AgencyID    *uint   `json:"agency_id"`
		DueDate     *string `json:"due_date"`
		OriginalURL string  `json:"original_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !urlStatusAllowed[body.Status] || body.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required and must be one of: requested, blocked, uplift, suspended, not_blocked, internal")
		return
	}
	var opts db.CaseCreateOptions
	if body.AgencyID != nil && *body.AgencyID != 0 {
		opts.AgencyID = body.AgencyID
	}
	if body.DueDate != nil {
		dueDate, err := parseOptionalRFC3339(*body.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date, expected RFC3339")
			return
		}
		opts.DueDate = dueDate
	}
	opts.OriginalURL = strings.TrimSpace(body.OriginalURL)

	u, err := h.store.GetURLByValue(r.Context(), urlValue)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if u == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	c, err := h.store.CreateCase(r.Context(), *user.DepartmentID, u.ID, body.Status, opts)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// UpdateCase applies a partial update to a case's shared fields
// (due_date/requested_at) — Case's own case-level defaults, shared by
// every url the case covers. Status is NOT here — it's per-url,
// see UpdateCaseURLStatus. Ownership: the case's own DepartmentID must
// match the caller's (404, not 403, same non-confirming pattern as
// AddCaseLetter/AddCaseURL), admin bypasses. Clear sentinels match PATCH
// /api/urls/{id}'s old convention: "" clears due_date/requested_at.
// Only keys present in the body are touched.
func (h *Handlers) UpdateCase(w http.ResponseWriter, r *http.Request) {
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
		DueDate     *string `json:"due_date"`
		RequestedAt *string `json:"requested_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	var fields db.CaseFields
	if body.DueDate != nil {
		dueDate, err := parseOptionalRFC3339(*body.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date, expected RFC3339")
			return
		}
		fields.DueDate = &dueDate
	}
	if body.RequestedAt != nil {
		requestedAt, err := parseOptionalRFC3339(*body.RequestedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid requested_at, expected RFC3339")
			return
		}
		fields.RequestedAt = &requestedAt
	}

	found, err := h.store.UpdateCaseFields(r.Context(), c.DepartmentID, uint(id), fields)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// DueDate lives on Case now, not URL — this is the only remaining
	// trigger point for (re)scheduling the due-date-reached task per url
	// the case covers (PATCH /api/urls/{id} lost this ability when it was
	// narrowed to {enabled} only). Only fires when this request actually
	// touched due_date, mirroring ToggleURL's cancel-on-remove call: fetch
	// urlIDs are best-effort, logged not fatal, since the field update
	// above already succeeded and shouldn't roll back over a notify hiccup.
	if h.notify != nil && fields.DueDate != nil {
		urlIDs, err := h.store.ListCaseURLIDs(r.Context(), uint(id))
		if err != nil {
			log.Printf("notify: list urls for case=%d due-date reschedule: %v", id, err)
		}
		for _, urlID := range urlIDs {
			if err := h.notify.RescheduleDueDate(c.DepartmentID, urlID, *fields.DueDate); err != nil {
				log.Printf("notify: reschedule due-date task for department=%d url=%d: %v", c.DepartmentID, urlID, err)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
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
		Type                    string     `json:"type"`
		ReferenceNumberExternal string     `json:"reference_number_external"`
		ReferenceNumberInternal string     `json:"reference_number_internal"`
		WorkflowStatus          string     `json:"workflow_status"`
		Recipient               string     `json:"recipient"`
		LetterDate              *time.Time `json:"letter_date"`
		ReceivedAt              *time.Time `json:"received_at"`
		SubmittedAt             *time.Time `json:"submitted_at"`
		Subject                 string     `json:"subject"`
		OICUserID               *uint      `json:"oic_user_id"`
		Requestor               string     `json:"requestor"`
		Remarks                 string     `json:"remarks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}

	if body.OICUserID != nil && *body.OICUserID != 0 {
		ok, err := h.oicUserInDepartment(r.Context(), *body.OICUserID, c.DepartmentID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid oic_user_id")
			return
		}
	}
	if body.OICUserID == nil {
		body.OICUserID = &user.ID
	}

	letter, err := h.store.AddCaseLetter(r.Context(), db.CaseLetter{
		CaseID:                  uint(id),
		Type:                    body.Type,
		ReferenceNumberExternal: body.ReferenceNumberExternal,
		ReferenceNumberInternal: body.ReferenceNumberInternal,
		WorkflowStatus:          body.WorkflowStatus,
		Recipient:               body.Recipient,
		LetterDate:              body.LetterDate,
		ReceivedAt:              body.ReceivedAt,
		SubmittedAt:             body.SubmittedAt,
		Subject:                 body.Subject,
		OICUserID:               body.OICUserID,
		Requestor:               body.Requestor,
		Remarks:                 body.Remarks,
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
		URL         string `json:"url"`
		Status      string `json:"status"`
		OriginalURL string `json:"original_url"`
		AgencyID    *uint  `json:"agency_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" || !urlStatusAllowed[body.Status] || body.Status == "" {
		writeError(w, http.StatusBadRequest, "url and status are required, status must be one of: requested, blocked, uplift, suspended, not_blocked, internal")
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
	var agencyID *uint
	if body.AgencyID != nil && *body.AgencyID != 0 {
		agencyID = body.AgencyID
	}

	cu, err := h.store.AddURLToCase(r.Context(), uint(id), u.ID, body.Status, strings.TrimSpace(body.OriginalURL), agencyID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	// So this domain shows up (switched off) in the case's own department's
	// Domain tab even if that department never separately added it to its
	// watchlist — see CreateCase's identical call for the case's first url.
	if err := h.store.EnsureURLOnDepartmentList(r.Context(), c.DepartmentID, u.ID); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, cu)
}

// UpdateCaseURLStatus sets one url's own Status within a case — the
// per-domain field a case's urls can diverge on. Ownership check identical
// to AddCaseURL's.
func (h *Handlers) UpdateCaseURLStatus(w http.ResponseWriter, r *http.Request) {
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
	urlID, err := strconv.ParseUint(chi.URLParam(r, "url_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url_id")
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
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !urlStatusAllowed[body.Status] || body.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required and must be one of: requested, blocked, uplift, suspended, not_blocked, internal")
		return
	}

	found, err := h.store.UpdateCaseURLStatus(r.Context(), uint(id), uint(urlID), body.Status)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateCaseURLAgency sets one url's own CaseURL.AgencyID within a case —
// the per-domain field a case's urls can diverge on (moved off Case
// 2026-09-15, see CaseURL's doc comment in internal/db/models.go). 0 clears
// it, matching the same clear-sentinel convention UpdateCase used to use
// for agency_id before the move. Ownership check identical to
// AddCaseURL/UpdateCaseURLStatus's.
func (h *Handlers) UpdateCaseURLAgency(w http.ResponseWriter, r *http.Request) {
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
	urlID, err := strconv.ParseUint(chi.URLParam(r, "url_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url_id")
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
		AgencyID *uint `json:"agency_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var agencyID *uint
	if body.AgencyID != nil && *body.AgencyID != 0 {
		agencyID = body.AgencyID
	}

	found, err := h.store.UpdateCaseURLAgency(r.Context(), uint(id), uint(urlID), agencyID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveCaseURL unlinks one url from a case (DELETE /api/cases/{id}/urls/{url_id}).
// Same ownership check as UpdateCaseURLAgency; 409 if it is the case's last url.
func (h *Handlers) RemoveCaseURL(w http.ResponseWriter, r *http.Request) {
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
	urlID, err := strconv.ParseUint(chi.URLParam(r, "url_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url_id")
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
	found, err := h.store.RemoveURLFromCase(r.Context(), uint(id), uint(urlID))
	if errors.Is(err, db.ErrLastCaseURL) {
		writeError(w, http.StatusConflict, "a case must keep at least one domain")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	if ps, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && ps > 0 && ps <= maxCaseLettersPageSize {
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

// ListCaseSummaries is the Cases view's data source (GET
// /api/case-summaries) — one page of cases, each with its own fields, its
// Notice letter's fields and every domain it covers, plus the total number of
// cases matching the filters. Query params (all optional): page, page_size,
// q (notice refs or domain), status, agency_id, dept_id (requesting dept),
// created_op/created_from/created_to and due_op/due_from/due_to (date
// filters), sort (id|due_date|notice_letter_date|uplift_letter_date) and dir (asc|desc). Same admin-global/
// non-admin-department-scoped split as ListCaseLetters; the full-list export
// lives at /case-summaries/export instead.
func (h *Handlers) ListCaseSummaries(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	qs := r.URL.Query()
	p := db.CaseListParams{
		Page:     1,
		PageSize: defaultDomainSummaryPageSize,
		Query:    qs.Get("q"),
		Status:   qs.Get("status"),
		Created:  db.DateFilter{Op: qs.Get("created_op"), From: qs.Get("created_from"), To: qs.Get("created_to")},
		Due:      db.DateFilter{Op: qs.Get("due_op"), From: qs.Get("due_from"), To: qs.Get("due_to")},
		SortDesc: qs.Get("dir") == "desc",
	}
	if n, err := strconv.Atoi(qs.Get("page")); err == nil && n > 0 {
		p.Page = n
	}
	if n, err := strconv.Atoi(qs.Get("page_size")); err == nil && n > 0 && n <= maxDomainSummaryPageSize {
		p.PageSize = n
	}
	if n, err := strconv.ParseUint(qs.Get("agency_id"), 10, 64); err == nil {
		id := uint(n)
		p.AgencyID = &id
	}
	if n, err := strconv.ParseUint(qs.Get("dept_id"), 10, 64); err == nil {
		id := uint(n)
		p.RequestingDept = &id
	}
	if sort := qs.Get("sort"); sort == "id" || sort == "due_date" || sort == "notice_letter_date" || sort == "uplift_letter_date" {
		p.SortBy = sort
	}
	if !user.IsAdmin {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		p.DepartmentID = user.DepartmentID
	}
	cases, total, err := h.store.ListCaseSummariesPage(r.Context(), p)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if cases == nil {
		cases = []db.CaseSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": cases, "total": total})
}

// UpdateCaseLetter applies a partial update to one CaseLetter's fields —
// the Cases view's edit form writes Notice/Memo subject, workflow status,
// reference numbers, recipient/requestor, dates, and remarks through this.
// Ownership: same case-DepartmentID check as AddCaseLetter/UpdateCase; the
// store call further scopes by (case, letter) so a letter can't be edited
// through the wrong case id (see UpdateCaseLetterFields).
func (h *Handlers) UpdateCaseLetter(w http.ResponseWriter, r *http.Request) {
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
	letterID, err := strconv.ParseUint(chi.URLParam(r, "letter_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid letter_id")
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
		Subject                 *string `json:"subject"`
		WorkflowStatus          *string `json:"workflow_status"`
		ReferenceNumberExternal *string `json:"reference_number_external"`
		ReferenceNumberInternal *string `json:"reference_number_internal"`
		Recipient               *string `json:"recipient"`
		Requestor               *string `json:"requestor"`
		Remarks                 *string `json:"remarks"`
		LetterDate              *string `json:"letter_date"`
		ReceivedAt              *string `json:"received_at"`
		SubmittedAt             *string `json:"submitted_at"`
		OICUserID               *uint   `json:"oic_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	fields := db.CaseLetterFields{
		Subject: body.Subject, WorkflowStatus: body.WorkflowStatus,
		ReferenceNumberExternal: body.ReferenceNumberExternal, ReferenceNumberInternal: body.ReferenceNumberInternal,
		Recipient: body.Recipient, Requestor: body.Requestor, Remarks: body.Remarks,
	}
	if body.LetterDate != nil {
		t, err := parseOptionalRFC3339(*body.LetterDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid letter_date, expected RFC3339")
			return
		}
		fields.LetterDate = &t
	}
	if body.ReceivedAt != nil {
		t, err := parseOptionalRFC3339(*body.ReceivedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid received_at, expected RFC3339")
			return
		}
		fields.ReceivedAt = &t
	}
	if body.SubmittedAt != nil {
		t, err := parseOptionalRFC3339(*body.SubmittedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid submitted_at, expected RFC3339")
			return
		}
		fields.SubmittedAt = &t
	}
	if body.OICUserID != nil {
		var oicUserID *uint
		if *body.OICUserID != 0 {
			ok, err := h.oicUserInDepartment(r.Context(), *body.OICUserID, c.DepartmentID)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid oic_user_id")
				return
			}
			oicUserID = body.OICUserID
		}
		fields.OICUserID = &oicUserID
	}

	found, err := h.store.UpdateCaseLetterFields(r.Context(), uint(id), uint(letterID), fields)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteCaseLetter removes one of a case's letters (Notice or Memo) — the
// Docs page's per-row delete action. Ownership: same case-DepartmentID
// check as UpdateCaseLetter; the store call further scopes by (case,
// letter) so a letter can't be deleted through the wrong case id.
func (h *Handlers) DeleteCaseLetter(w http.ResponseWriter, r *http.Request) {
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
	letterID, err := strconv.ParseUint(chi.URLParam(r, "letter_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid letter_id")
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

	found, err := h.store.DeleteCaseLetter(r.Context(), uint(id), uint(letterID))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// BlockingStats (GET /api/blocking-stats) — year/agency/offence block counts;
// admin: global, non-admin: own department only.
func (h *Handlers) BlockingStats(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var dept *uint
	if !user.IsAdmin {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		dept = user.DepartmentID
	}
	rows, err := h.store.BlockingStats(r.Context(), dept)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load blocking stats")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}
