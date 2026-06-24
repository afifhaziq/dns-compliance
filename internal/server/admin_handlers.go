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
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// Users

func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		IsAdmin      bool   `json:"is_admin"`
		DepartmentID *uint  `json:"department_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, err := h.store.CreateUser(r.Context(), db.User{
		Username:     body.Username,
		PasswordHash: hash,
		IsAdmin:      body.IsAdmin,
		DepartmentID: body.DepartmentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteUser(r.Context(), uint(id)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// URLs (admin-only views/actions)

func (h *Handlers) ListUnassignedURLs(w http.ResponseWriter, r *http.Request) {
	urls, err := h.store.ListUnassignedURLs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
