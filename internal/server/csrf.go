package server

import "net/http"

// requireFetchHeader rejects state-changing requests that don't carry
// X-Requested-With: fetch. Paired with SameSite=Lax cookies and the absence
// of any CORS allowlist, this closes the remaining CSRF gap without a
// token: a plain cross-site <form> POST (the classic CSRF vector) cannot
// set a custom header, and a cross-origin fetch/XHR that tried to would be
// blocked by the browser before it ever reached this server, since no
// Access-Control-Allow-Origin is configured. web/src/api/client.ts sends
// this header on every request.
func requireFetchHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("X-Requested-With") != "fetch" {
			writeError(w, http.StatusForbidden, "missing required header")
			return
		}
		next.ServeHTTP(w, r)
	})
}
