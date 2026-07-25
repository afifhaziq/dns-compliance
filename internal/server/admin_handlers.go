package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/go-chi/chi/v5"
)

// Departments

func (h *Handlers) ListDepartments(w http.ResponseWriter, r *http.Request) {
	departments, err := h.store.ListDepartments(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, departments)
}

func (h *Handlers) CreateDepartment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	d, err := h.store.CreateDepartment(r.Context(), body.Name)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// Users

func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	caller, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !caller.IsAdmin {
		// department admin: only their own department's users
		var scoped []db.User
		for _, u := range users {
			if u.DepartmentID != nil && caller.DepartmentID != nil && *u.DepartmentID == *caller.DepartmentID {
				scoped = append(scoped, u)
			}
		}
		users = scoped
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	caller, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		IsAdmin      bool   `json:"is_admin"`
		IsDeptAdmin  bool   `json:"is_dept_admin"`
		DepartmentID *uint  `json:"department_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if !caller.IsAdmin {
		// department admin: can only create a plain member of their own
		// department — granting admin/dept-admin is a super-admin-only act.
		body.IsAdmin = false
		body.IsDeptAdmin = false
		body.DepartmentID = caller.DepartmentID
	}
	if body.IsAdmin && body.IsDeptAdmin {
		writeError(w, http.StatusBadRequest, "user cannot be both is_admin and is_dept_admin")
		return
	}
	if !body.IsAdmin && body.DepartmentID == nil {
		writeError(w, http.StatusBadRequest, "department_id is required for non-admin users")
		return
	}
	if body.IsAdmin {
		body.DepartmentID = nil
	}
	hash, err := db.HashPassword(body.Password)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	u, err := h.store.CreateUser(r.Context(), db.User{
		Username:     body.Username,
		PasswordHash: hash,
		IsAdmin:      body.IsAdmin,
		IsDeptAdmin:  body.IsDeptAdmin,
		DepartmentID: body.DepartmentID,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	caller, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !caller.IsAdmin {
		// department admin: only a plain member of their own department
		target, err := h.store.GetUserByID(r.Context(), uint(id))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if target == nil || target.IsAdmin || target.IsDeptAdmin ||
			target.DepartmentID == nil || caller.DepartmentID == nil ||
			*target.DepartmentID != *caller.DepartmentID {
			writeError(w, http.StatusForbidden, "cannot delete this user")
			return
		}
	}
	if err := h.store.DeleteUser(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// URLs (admin-only views/actions)

func (h *Handlers) ListUnassignedURLs(w http.ResponseWriter, r *http.Request) {
	urls, err := h.store.ListUnassignedURLs(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, urls)
}

// PurgeURL hard-deletes a URL row (cascading to its ScanResult and
// DepartmentURL rows) — unlike RemoveFromWatchlist, which only ever
// unlinks a department from a domain, this permanently destroys history.
func (h *Handlers) PurgeURL(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteURL(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Compliant IPs

func (h *Handlers) ListCompliantIPs(w http.ResponseWriter, r *http.Request) {
	ips, err := h.store.ListCompliantIPs(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ips)
}

func (h *Handlers) CreateCompliantIP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address string `json:"address"`
		Note    string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Address == "" {
		writeError(w, http.StatusBadRequest, "address is required")
		return
	}
	ip, err := h.store.CreateCompliantIP(r.Context(), body.Address, body.Note)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ip)
}

func (h *Handlers) DeleteCompliantIP(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteCompliantIP(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Scan schedule

func (h *Handlers) GetScanInterval(w http.ResponseWriter, r *http.Request) {
	minutes, err := h.store.GetScanInterval(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	enabled, err := h.store.GetScanEnabled(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interval_minutes": minutes, "enabled": enabled})
}

func (h *Handlers) SetScanInterval(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IntervalMinutes int  `json:"interval_minutes"`
		Enabled         bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IntervalMinutes < 1 {
		writeError(w, http.StatusBadRequest, "interval_minutes must be a positive integer")
		return
	}
	if err := h.store.SetScanInterval(r.Context(), body.IntervalMinutes); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.store.SetScanEnabled(r.Context(), body.Enabled); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
