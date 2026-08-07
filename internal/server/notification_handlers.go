package server

import (
	"net/http"
	"strconv"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/go-chi/chi/v5"
)

const (
	defaultNotificationPageSize = 20
	maxNotificationPageSize     = 100
)

// ListNotifications — GET /api/notifications?page=&page_size= — same
// admin-global/department-scoped branch shape as ResurfacedDomains/ISPStats.
func (h *Handlers) ListNotifications(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	pageSize := defaultNotificationPageSize
	if ps, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && ps > 0 && ps <= maxNotificationPageSize {
		pageSize = ps
	}

	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var notifications []db.Notification
	var total int
	var err error
	if user.IsAdmin {
		notifications, total, err = h.store.ListNotifications(r.Context(), page, pageSize)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		notifications, total, err = h.store.ListNotificationsForDepartment(r.Context(), page, pageSize, *user.DepartmentID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": notifications, "total": total})
}

// UnreadNotificationCount — GET /api/notifications/unread-count
func (h *Handlers) UnreadNotificationCount(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var count int
	var err error
	if user.IsAdmin {
		count, err = h.store.UnreadCount(r.Context())
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		count, err = h.store.UnreadCountForDepartment(r.Context(), *user.DepartmentID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

// ClearAllNotifications — DELETE /api/notifications, bulk-dismiss. Same
// admin-global/department-scoped branch shape as ListNotifications.
func (h *Handlers) ClearAllNotifications(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var err error
	if user.IsAdmin {
		err = h.store.ClearAllNotifications(r.Context())
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		err = h.store.ClearAllNotificationsForDepartment(r.Context(), *user.DepartmentID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ownedNotificationID parses the {id} URL param and 404s (not 403) unless
// it belongs to the caller's own department (or the caller is admin) —
// matching the requireDomainOwnership convention used by /api/results etc.,
// so a department can't confirm another department's notification exists.
// Returns ok=false after already writing a response.
func (h *Handlers) ownedNotificationID(w http.ResponseWriter, r *http.Request, user *db.User) (id uint, ok bool) {
	parsed, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	n, err := h.store.GetNotification(r.Context(), uint(parsed))
	if err != nil {
		writeInternalError(w, err)
		return 0, false
	}
	if n == nil || (!user.IsAdmin && (user.DepartmentID == nil || *user.DepartmentID != n.DepartmentID)) {
		writeError(w, http.StatusNotFound, "notification not found")
		return 0, false
	}
	return uint(parsed), true
}

// MarkNotificationRead — PATCH /api/notifications/{id}/read.
func (h *Handlers) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id, ok := h.ownedNotificationID(w, r, user)
	if !ok {
		return
	}
	if err := h.store.MarkNotificationRead(r.Context(), id); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteNotification — DELETE /api/notifications/{id}, dismisses one
// notification. Same 404-not-403 ownership check as MarkNotificationRead.
func (h *Handlers) DeleteNotification(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id, ok := h.ownedNotificationID(w, r, user)
	if !ok {
		return
	}
	if err := h.store.DeleteNotification(r.Context(), id); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
