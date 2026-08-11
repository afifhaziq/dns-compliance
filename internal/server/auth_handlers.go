package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/afif/dns-tracking/internal/db"
)

type AuthHandlers struct {
	store        db.Store
	cookieSecure bool
}

func NewAuthHandlers(store db.Store, cookieSecure bool) *AuthHandlers {
	return &AuthHandlers{store: store, cookieSecure: cookieSecure}
}

func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := h.store.GetUserByUsername(r.Context(), body.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	if user == nil || !db.CheckPassword(user.PasswordHash, body.Password) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token, err := generateSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	expiresAt := time.Now().Add(sessionDuration)
	if err := h.store.CreateSession(r.Context(), db.Session{Token: token, UserID: user.ID, ExpiresAt: expiresAt}); err != nil {
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}

	setSessionCookie(w, token, h.cookieSecure, expiresAt)
	writeJSON(w, http.StatusOK, user)
}

func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		_ = h.store.DeleteSession(r.Context(), cookie.Value)
	}
	clearSessionCookie(w, h.cookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// ChangePassword is self-service — the caller changes their own password,
// verifying the current one first. Used both for a voluntary change and to
// clear MustChangePassword after an admin-initiated reset (see
// Handlers.ResetUserPassword).
func (h *AuthHandlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	caller, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CurrentPassword == "" || body.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}
	if !db.CheckPassword(caller.PasswordHash, body.CurrentPassword) {
		writeError(w, http.StatusBadRequest, "current password is incorrect")
		return
	}
	hash, err := db.HashPassword(body.NewPassword)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.store.SetUserPassword(r.Context(), caller.ID, hash, false); err != nil {
		writeInternalError(w, err)
		return
	}
	user, err := h.store.GetUserByID(r.Context(), caller.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}
