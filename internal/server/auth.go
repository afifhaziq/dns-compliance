package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/afif/dns-tracking/internal/db"
)

const sessionCookieName = "session_token"
const sessionDuration = 7 * 24 * time.Hour

type contextKey int

const userContextKey contextKey = iota

// generateSessionToken returns a random, URL-safe token suitable as a
// Session.Token primary key — 256 bits of entropy from crypto/rand.
func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func setSessionCookie(w http.ResponseWriter, token string, secure bool, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

func userFromContext(ctx context.Context) (*db.User, bool) {
	u, ok := ctx.Value(userContextKey).(*db.User)
	return u, ok
}

// authStore is the narrow slice of db.Store requireAuth actually needs —
// session lookup plus the user it belongs to.
type authStore interface {
	db.SessionStore
	db.UserStore
}

// requireAuth resolves the session cookie to a user and attaches it to the
// request context, 401ing if the cookie is missing, the session doesn't
// exist, or it has expired (GetSession already filters expired rows).
func requireAuth(store authStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil || cookie.Value == "" {
				writeError(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			sess, err := store.GetSession(r.Context(), cookie.Value)
			if err != nil || sess == nil {
				writeError(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			user, err := store.GetUserByID(r.Context(), sess.UserID)
			if err != nil || user == nil {
				writeError(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireAdmin must run after requireAuth in the middleware chain. Super
// admin only — for routes that are inherently cross-department.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok || !user.IsAdmin {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAnyAdmin must run after requireAuth. Allows a super admin or a
// department admin through; handlers behind it that need to restrict a
// department admin to their own department do so themselves via
// userFromContext.
func requireAnyAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok || (!user.IsAdmin && !user.IsDeptAdmin) {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
