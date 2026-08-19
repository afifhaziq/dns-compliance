package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
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

// ListDepartmentsOpen is the same underlying data as ListDepartments
// (super-admin-only, /api/admin/departments) but exposed to any
// authenticated role at GET /api/departments, for the Requesting Dept
// dropdown any regular user needs when adding/editing a domain's case
// metadata.
func (h *Handlers) ListDepartmentsOpen(w http.ResponseWriter, r *http.Request) {
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

func (h *Handlers) UpdateDepartment(w http.ResponseWriter, r *http.Request) {
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
	d, err := h.store.UpdateDepartment(r.Context(), uint(id), body.Name)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
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

// UpdateUser is super-admin-only (see router.go — routed under the
// requireAdmin group, not requireAnyAdmin) so there's no caller-role
// branching here, unlike CreateUser/DeleteUser/ResetUserPassword.
func (h *Handlers) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Username     string `json:"username"`
		IsAdmin      bool   `json:"is_admin"`
		IsDeptAdmin  bool   `json:"is_dept_admin"`
		DepartmentID *uint  `json:"department_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
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
	u, err := h.store.UpdateUser(r.Context(), uint(id), db.User{
		Username:     body.Username,
		IsAdmin:      body.IsAdmin,
		IsDeptAdmin:  body.IsDeptAdmin,
		DepartmentID: body.DepartmentID,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// ResetUserPassword generates a random temporary password, returned once in
// the response — the caller (an admin) hands it to the user out of band.
// MustChangePassword is set so the user is forced to set their own password
// at next login (see AuthHandlers.ChangePassword / RootLayout's gate).
func (h *Handlers) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
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
		// department admin: only a plain member of their own department —
		// same scoping as DeleteUser.
		target, err := h.store.GetUserByID(r.Context(), uint(id))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if target == nil || target.IsAdmin || target.IsDeptAdmin ||
			target.DepartmentID == nil || caller.DepartmentID == nil ||
			*target.DepartmentID != *caller.DepartmentID {
			writeError(w, http.StatusForbidden, "cannot reset this user's password")
			return
		}
	}
	temp, err := generateTempPassword()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	hash, err := db.HashPassword(temp)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.store.SetUserPassword(r.Context(), uint(id), hash, true); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"temp_password": temp})
}

// generateTempPassword returns a random, URL-safe temporary password for an
// admin-initiated reset — same shape as generateSessionToken (auth.go) but
// shorter, since a human has to read/copy/type this one.
func generateTempPassword() (string, error) {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
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

// Agencies — an admin-managed lookup table for the government agency behind
// a takedown request. Read open to any authenticated role (registered
// below, outside this admin-gated group); create/delete gated to
// admin-or-dept-admin, matching the DNS-server/ISP-logo/legal-catalog
// pattern rather than the stricter super-admin-only Department/CompliantIP
// pattern.

func (h *Handlers) ListAgencies(w http.ResponseWriter, r *http.Request) {
	agencies, err := h.store.ListAgencies(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agencies)
}

func (h *Handlers) CreateAgency(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	a, err := h.store.CreateAgency(r.Context(), body.Name)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handlers) UpdateAgency(w http.ResponseWriter, r *http.Request) {
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
	a, err := h.store.UpdateAgency(r.Context(), uint(id), body.Name)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *Handlers) DeleteAgency(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteAgency(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Due Date Presets — the "Time to Block" duration options on the watchlist
// page (urls.tsx). Read open to any authenticated role; create/delete
// gated to admin-or-dept-admin, same pattern as Agency above.

func (h *Handlers) ListDueDatePresets(w http.ResponseWriter, r *http.Request) {
	presets, err := h.store.ListDueDatePresets(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, presets)
}

func (h *Handlers) CreateDueDatePreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label   string `json:"label"`
		Minutes int    `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Label == "" || body.Minutes <= 0 {
		writeError(w, http.StatusBadRequest, "label is required and minutes must be positive")
		return
	}
	p, err := h.store.CreateDueDatePreset(r.Context(), body.Label, body.Minutes)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handlers) DeleteDueDatePreset(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteDueDatePreset(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ISP Logos

func (h *Handlers) ListISPLogos(w http.ResponseWriter, r *http.Request) {
	logos, err := h.store.ListISPLogos(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, logos)
}

func (h *Handlers) UpsertISPLogo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ISP     string `json:"isp"`
		LogoURL string `json:"logo_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ISP == "" || body.LogoURL == "" {
		writeError(w, http.StatusBadRequest, "isp and logo_url are required")
		return
	}
	logo, err := h.store.UpsertISPLogo(r.Context(), body.ISP, body.LogoURL)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, logo)
}

func (h *Handlers) DeleteISPLogo(w http.ResponseWriter, r *http.Request) {
	isp, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil || isp == "" {
		writeError(w, http.StatusBadRequest, "invalid isp")
		return
	}
	if err := h.store.DeleteISPLogo(r.Context(), isp); err != nil {
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
	dnsWorkers, err := h.store.GetDNSWorkers(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	slaInterval, err := h.store.GetSLAInterval(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	slaStreakThreshold, err := h.store.GetSLAStreakThreshold(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"interval_minutes":     minutes,
		"enabled":              enabled,
		"dns_workers":          dnsWorkers,
		"sla_interval_minutes": slaInterval,
		"sla_streak_threshold": slaStreakThreshold,
	})
}

func (h *Handlers) SetScanInterval(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IntervalMinutes    int  `json:"interval_minutes"`
		Enabled            bool `json:"enabled"`
		DNSWorkers         int  `json:"dns_workers"`
		SLAIntervalMinutes int  `json:"sla_interval_minutes"`
		SLAStreakThreshold int  `json:"sla_streak_threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IntervalMinutes < 1 || body.DNSWorkers < 1 || body.DNSWorkers > math.MaxInt32 || body.SLAIntervalMinutes < 1 || body.SLAStreakThreshold < 1 {
		writeError(w, http.StatusBadRequest, "interval_minutes, dns_workers, sla_interval_minutes, and sla_streak_threshold must be positive integers")
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
	if err := h.store.SetDNSWorkers(r.Context(), body.DNSWorkers); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.store.SetSLAInterval(r.Context(), body.SLAIntervalMinutes); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.store.SetSLAStreakThreshold(r.Context(), body.SLAStreakThreshold); err != nil {
		writeInternalError(w, err)
		return
	}
	// Restart the scheduler's wait timer now rather than finishing out
	// whatever was left of the previous interval — see
	// Scanner.NotifyScheduleChanged.
	h.scanner.NotifyScheduleChanged()
	w.WriteHeader(http.StatusNoContent)
}
