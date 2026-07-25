package server

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// keyedRateLimiter hands out one token-bucket limiter per key (IP or user
// ID), created lazily on first use.
//
// ponytail: limiters are never evicted, so long-running deployments with
// many distinct keys grow this map unboundedly — add a cleanup sweep (e.g.
// a periodic goroutine dropping limiters idle past N minutes) if that ever
// shows up in memory profiles.
type keyedRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit
	burst    int
}

func newKeyedRateLimiter(r rate.Limit, burst int) *keyedRateLimiter {
	return &keyedRateLimiter{limiters: make(map[string]*rate.Limiter), r: r, burst: burst}
}

func (k *keyedRateLimiter) allow(key string) bool {
	k.mu.Lock()
	l, ok := k.limiters[key]
	if !ok {
		l = rate.NewLimiter(k.r, k.burst)
		k.limiters[key] = l
	}
	k.mu.Unlock()
	return l.Allow()
}

// clientIP strips the port off RemoteAddr. Assumes a direct connection or a
// trusted reverse proxy that overwrites RemoteAddr — it does not honor
// X-Forwarded-For, which an untrusted client could spoof to bypass the
// limiter.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimitByIP rejects requests over (r, burst) per client IP with 429.
// Intended for pre-auth routes (login) where no user identity exists yet.
func rateLimitByIP(r rate.Limit, burst int) func(http.Handler) http.Handler {
	limiter := newKeyedRateLimiter(r, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !limiter.allow(clientIP(req)) {
				writeError(w, http.StatusTooManyRequests, "too many requests, try again shortly")
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

// rateLimitByUser rejects requests over (r, burst) per authenticated user
// with 429. Must run after requireAuth. Falls back to clientIP if somehow
// no user is in context, so it never panics.
func rateLimitByUser(r rate.Limit, burst int) func(http.Handler) http.Handler {
	limiter := newKeyedRateLimiter(r, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			key := clientIP(req)
			if user, ok := userFromContext(req.Context()); ok {
				key = user.Username
			}
			if !limiter.allow(key) {
				writeError(w, http.StatusTooManyRequests, "too many requests, try again shortly")
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}
