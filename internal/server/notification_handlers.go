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

// MarkNotificationRead — PATCH /api/notifications/{id}/read. 404s (not
// 403) for a notification owned by another department, matching the
// requireDomainOwnership convention used by /api/results etc. — avoids
// confirming the notification exists to a department that can't see it.
func (h *Handlers) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.store.GetNotification(r.Context(), uint(id))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if n == nil {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}
	if !user.IsAdmin && (user.DepartmentID == nil || *user.DepartmentID != n.DepartmentID) {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}
	if err := h.store.MarkNotificationRead(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
