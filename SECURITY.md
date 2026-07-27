# Security Audit Report

**Tool:** dns-compliance  
**Audited:** 2026-05-15  
**Endpoints audited:** 11  
**Score:** 32 / 100 — Significant Issues

> This is an internal ISP compliance tool. Several findings are lower impact when deployed on a private network, but should be addressed before any public or multi-tenant exposure.

---

## Summary

| Severity | Count |
|----------|-------|
| Critical | 2 |
| High | 4 |
| Medium | 3 |

---

## Critical

### SEC-001: No authentication on any endpoint

All 11 endpoints are fully public — no API key, Bearer token, or middleware-level auth is applied. Anyone who can reach `:8080` can trigger scans, delete records, and add DNS servers.

**Affected:** All endpoints  
**OWASP:** API2 — Broken Authentication  
**Fix:** Add an API key middleware (e.g. check `Authorization: Bearer <key>` header) in `internal/server/router.go` before the `/api` route group.

```go
r.Route("/api", func(r chi.Router) {
    r.Use(apiKeyMiddleware(os.Getenv("API_KEY")))
    // ... routes
})
```

---

### SEC-002: All endpoints use HTTP (no HTTPS)

Every Postman request targets `http://localhost:8080`. In a deployed environment, scan results, DNS data, and any auth tokens travel unencrypted.

**OWASP:** API8 — Security Misconfiguration  
**Fix:** Terminate TLS at a reverse proxy (nginx/caddy) in front of the server, or pass a TLS cert/key to the Go HTTP server. Update Postman collection base URL to `https://`.

---

## High

### SEC-003: SSRF via `POST /api/screenshot` and `POST /api/urls`

`POST /api/screenshot` accepts `{"url": "...", "dns_server_id": 1}` and the server launches headless Chrome to fetch that URL. `POST /api/urls` adds URLs that the scheduler later crawls. Both allow an attacker to supply internal addresses (`169.254.169.254`, `10.x.x.x`, `192.168.x.x`).

**OWASP:** API7 — Server Side Request Forgery  
**Fix:** Validate the `url` field against a blocklist of private/loopback CIDR ranges before accepting it. Resolve the hostname and reject private IPs:

```go
func isPrivateURL(rawURL string) bool {
    u, err := url.Parse(rawURL)
    if err != nil { return true }
    addrs, err := net.LookupHost(u.Hostname())
    if err != nil { return false }
    for _, addr := range addrs {
        ip := net.ParseIP(addr)
        if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
            return true
        }
    }
    return false
}
```

---

### SEC-004: No rate limiting — `POST /api/scan` can be spammed

`POST /api/scan` spawns a crawler subprocess. The mutex guard prevents parallel runs, but rapid requests can keep the scanner perpetually busy, starve DB connections, and exhaust system resources. No `429 Too Many Requests` response is defined.

**OWASP:** API4 — Unrestricted Resource Consumption  
**Fix:** Add a per-IP or global rate limiter middleware (e.g. `golang.org/x/time/rate`) on the scan and screenshot endpoints. Also define a minimum cool-down between scan triggers.

---

### SEC-005: Raw database errors leaked in responses

When `POST /api/urls` receives a duplicate URL, the response body is:

```json
{"error": "ERROR: duplicate key value violates unique constraint \"idx_urls_url\" (SQLSTATE 23505)"}
```

This reveals the DB engine (PostgreSQL), constraint names, and internal schema.

**OWASP:** API8 — Security Misconfiguration  
**Fix:** Wrap DB errors in `internal/server/handlers.go` and map known error codes to safe messages:

```go
// in writeDBError helper
if strings.Contains(err.Error(), "23505") {
    writeError(w, http.StatusConflict, "resource already exists")
    return
}
writeError(w, http.StatusInternalServerError, "internal error")
```

---

### SEC-006: No function-level authorization on destructive operations

`DELETE /api/urls/{id}` and `DELETE /api/dns-servers/{id}` have no ownership check — any caller can delete any record by iterating integer IDs. `POST /api/scan` and `POST /api/screenshot` (expensive, privileged operations) are equally open.

**OWASP:** API5 — Broken Function Level Authorization  
**Fix:** At minimum, require a separate elevated secret/role for mutating operations. Long-term, add an ownership model to the `URL` and `DNSServer` DB models.

---

## Medium

### SEC-007: No input length limits on string fields

`url`, `name`, `address`, and `protocol` fields in request bodies have no `maxLength` validation. Oversized inputs pass through to the DB and DNS/HTTP stack unchecked.

**Fix:** Add explicit length checks in each handler before calling the store:

```go
if len(body.URL) > 2048 {
    writeError(w, http.StatusBadRequest, "url too long")
    return
}
```

---

### SEC-008: Collection missing 5 endpoints (inventory gap)

The Postman collection has 6 requests; the server exposes 11. Undocumented endpoints are invisible to security reviewers and consumers.

**Missing from collection:**
- `GET /api/urls`
- `GET /api/scan/status`
- `GET /api/results/*` (per-URL history)
- `POST /api/dns-servers`
- `DELETE /api/urls/{id}`
- `DELETE /api/dns-servers/{id}`

**OWASP:** API9 — Improper Inventory Management  
**Fix:** Run `/postman:sync` to regenerate the collection from the full OpenAPI spec, or add the missing requests manually.

---

### SEC-009: User-supplied DNS servers are trusted unconditionally

`POST /api/dns-servers` lets any caller register an arbitrary DNS server (UDP/DoT/DoH). The crawler then uses it to resolve hostnames. A malicious DNS server could return attacker-controlled IPs, directing Chrome at arbitrary hosts.

**OWASP:** API6 — Unrestricted Access to Sensitive Business Flows  
**Fix:** Restrict DNS server management to authenticated admins (see SEC-001). Consider validating that the address resolves to a non-private IP before saving.

---

## Additional Notes

- **gRPC on `:50051` and `:50052`** supports mutual TLS via `--tls-cert`/`--tls-key`/`--tls-ca` on both binaries (`internal/grpcauth`), and both `ComplianceService.Submit` and `CrawlerControl.StartSweep` are authenticated by the shared `--auth-token`/`--crawler-token` secret. Both are **opt-in**: with no TLS flags the transport is still plaintext, and with an empty token the auth check is skipped — each case logs a startup warning. A deployment where the crawler runs on a separate host (see `docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md`) should set both, and should still firewall these ports to the crawler↔server link rather than exposing them publicly.
- **MinIO** screenshots bucket is set to public policy — anyone with the URL can view screenshots. Scope access if screenshots contain sensitive content.
- **No CORS headers** are configured. Add explicit `Access-Control-Allow-Origin` headers if a browser frontend will consume this API.
