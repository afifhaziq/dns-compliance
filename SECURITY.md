# Security Audit Report

**Tool:** dns-compliance
**Originally audited:** 2026-05-15
**Re-verified against current code:** 2026-08-04
**Score:** 71 / 100 — Good, some gaps remain

> This is an internal ISP compliance tool. Several findings are lower impact when deployed on a private network, but should be addressed before any public or multi-tenant exposure.
>
> This report supersedes the 2026-05-15 version. Session-cookie auth + RBAC, SSRF blocking, rate limiting, and safe error responses have all landed since then — see "Resolved since last audit" below. Re-run this audit again after any further RBAC or input-validation changes.

---

## Summary

| Severity | Count |
|----------|-------|
| High | 1 |
| Medium | 4 |
| Low | 2 |

---

## Open findings

### SEC-002: HTTP API has no TLS termination — High

`cmd/server/main.go` still calls plain `http.Server.ListenAndServe()` — no cert/key flags exist for the HTTP API (the `--tls-cert`/`--tls-key`/`--tls-ca` flags only feed the gRPC crawler↔server link, see "Resolved since last audit" below). Session cookies and scan data travel unencrypted unless something in front of this terminates TLS.

**Fix:** Terminate TLS at a reverse proxy (nginx/caddy) in front of the server, or add `ListenAndServeTLS` with cert/key flags for the HTTP listener specifically. Note `--cookie-secure` already exists and should be `true` in any such deployment.

---

### SEC-009: DNS server addresses aren't validated against private/internal ranges — Medium

`POST /api/dns-servers` and `PATCH /api/dns-servers/{id}` now require `requireAnyAdmin` (was previously anonymous), so this is no longer publicly exploitable. But `CreateDNSServer`/`UpdateDNSServer` (`internal/server/handlers.go`) still only check that `isp`/`address` are non-empty — nothing stops an admin or department-admin from pointing a "DNS server" at internal infrastructure, which the crawler will then trust for resolution.

**Fix:** Reuse the `isPrivateHost` check already added for SEC-003 (`internal/server/ssrf.go`) on the server `address` field, or explicitly accept the risk since this now requires an elevated role.

---

### SEC-010: `POST /api/hosting/{ip}` has no scoping check — Medium

Unlike every other domain-data endpoint (`/api/domain/*url`, `/api/subdomains/*url`, etc.), which go through `requireDomainOwnership`, `RefreshHostingInfo` takes an arbitrary `ip` path param with no ownership/department check — any authenticated user can force a re-fetch against ipinfo.io/RDAP for any IP. Low actual risk (it only queries third-party info APIs, not DNS/Chrome), but it's an inconsistency with the rest of the domain-scoped surface and an easy way to burn ipinfo.io rate limit.

**Fix:** Either scope it to IPs seen in the caller's department's scan history, or explicitly document it as intentionally unscoped (it's IP-keyed cache data, not watchlist data — same rationale as `db.IPInfo` itself).

---

### SEC-004b: No account lockout on repeated failed logins — Medium

`POST /api/auth/login` is now rate-limited (5/min per IP, `internal/server/router.go`), which slows brute force but doesn't stop a distributed or slow attempt against a single username — there's no failure counter or per-account lockout in `auth_handlers.go`. Already flagged as a follow-up in CLAUDE.md's SEC-001 note.

**Fix:** Track failed attempts per username (or per session-attempt) and lock/delay after N failures within a window.

---

### SEC-007: No input length limits on string fields — Low

Still true as of this audit: no `maxLength`-style checks anywhere in `internal/server/*.go` for `url`, `name`, `address`, `protocol`, etc. `CreateDNSServer`/`UpdateDNSServer` only check non-empty. Low severity since these are now admin-gated inputs, not public ones.

**Fix:** Add explicit length checks before calling the store.

---

### MinIO screenshots bucket is public — Low

`docker-compose.yml` still runs `mc anonymous set public local/screenshots` on init — anyone with a screenshot URL can view it, no auth required. Unchanged from the original audit. Acceptable if screenshot URLs are never exposed outside the authenticated dashboard, but worth confirming that's still true now that the dashboard has grown (e.g. shared links, exports).

---

## Resolved since last audit

- **SEC-001 (auth on every endpoint)** — Session-cookie auth (`requireAuth`) now wraps all of `/api` except login; `requireAdmin`/`requireAnyAdmin` gate admin-only and admin-or-dept-admin routes respectively. Login uses bcrypt (`internal/db/auth.go`). See CLAUDE.md's "Auth & RBAC" section for the full model.
- **SEC-003 (SSRF via `/api/screenshot`, `/api/urls`)** — `internal/server/ssrf.go`'s `isPrivateHost` resolves the hostname and rejects loopback/private/link-local/unspecified addresses before `AddToWatchlist` or `TriggerScreenshot` will accept a URL.
- **SEC-004 (no rate limiting on scan)** — Token-bucket rate limiting (`internal/server/ratelimit.go`, `golang.org/x/time/rate`) on `POST /api/scan`/`POST /api/screenshot` (per-user) and `POST /api/auth/login` (per-IP), returning `429`. The scanner's mutex guard against concurrent runs is still in place too.
- **SEC-005 (raw DB errors leaked)** — `writeInternalError` logs the real error server-side and returns a fixed `"internal error"` string to the client; used consistently across store-layer failures.
- **SEC-006 (no function-level authz on deletes)** — `DELETE /api/urls/{id}` is department-scoped (only deletes if it belongs to the caller's department); `DELETE /api/dns-servers/{id}` requires `requireAnyAdmin`; domain-scoped routes go through `requireDomainOwnership` (404s, not 403s, for a non-owning department — avoids confirming a domain exists).
- **SEC-008 (Postman inventory gap)** — moot; no Postman collection exists in the repo anymore. The API has grown well past the original 11 endpoints (~50 routes now) — if an external inventory/spec is ever wanted again, generate it from `router.go` rather than hand-maintaining one.
- **No CORS headers** — still true, but now an intentional part of the CSRF defense rather than an oversight: `internal/server/csrf.go`'s `requireFetchHeader` requires `X-Requested-With: fetch` on state-changing requests, relying on the absence of a CORS allowlist to keep cross-origin browser requests from ever reaching that code path.

---

## New surface area since last audit (not yet deeply reviewed)

The API surface has grown significantly — legal citation catalog (`/api/legal/*`), subdomain enumeration (`/api/subdomains/*`, shells out to `subfinder`), WHOIS/RDAP enrichment (`/api/domain/*url`), favicon proxying (`/api/favicon/*url`), ISP logos. Mutation routes on all of these are admin-or-dept-admin gated and read routes follow the existing department-ownership pattern, consistent with the rest of the app — but none of these have had a dedicated pass the way the original 11 endpoints did. Worth a follow-up audit focused specifically on: the `subfinder` subprocess invocation (command injection surface, even though input is a normalized hostname), and RDAP/ipinfo.io outbound requests (SSRF-adjacent, since they're server-initiated fetches keyed off user-supplied domains).

---

## Additional Notes

- **gRPC on `:50051` and `:50052`** supports mutual TLS via `--tls-cert`/`--tls-key`/`--tls-ca` on both binaries (`internal/grpcauth`), and both `ComplianceService.Submit` and `CrawlerControl.StartSweep` are authenticated by the shared `--auth-token`/`--crawler-token` secret. Both are still **opt-in**, unchanged from the original audit: with no TLS flags the transport is plaintext, and with an empty token the auth check is skipped — each case logs a startup warning. A deployment where the crawler runs on a separate host should set both, and should still firewall these ports to the crawler↔server link rather than exposing them publicly.
