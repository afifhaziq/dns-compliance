# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Full-stack dev startup (PostgreSQL via Docker, server on :8080, Vite on :5173)
# Builds crawler, starts postgres, launches server + frontend; Ctrl+C shuts all down
./dev.sh

# Build both binaries
go build -o server  ./cmd/server/
go build -o crawler ./cmd/crawler/

# Run the crawler standalone (sites file or inline URLs — always quote URLs with ? or & in zsh)
go run ./cmd/crawler/ --sites sites.txt
go run ./cmd/crawler/ "https://example.com" "https://example2.com"

# Run the server (requires PostgreSQL + MinIO; see Docker section below)
go run ./cmd/server/ --http-addr :8080 --grpc-addr :50051

# Crawler key flags
--dns-timeout 5          # seconds for DNS resolution per site (default 5)
--screenshot-timeout 30  # seconds for navigation + idle wait + capture (default 30)
--wait-idle 5            # max seconds to wait for networkIdle event (default 5)
--post-idle-sleep 2000   # milliseconds to sleep after idle before capture (default 2000)
--screenshot-workers 5   # concurrent Chrome tabs (default 5)
--dns-workers 20         # concurrent DNS lookups (default 20)
--interval 10            # repeat sweep every N minutes; 0 = run once (default 0)
--screenshots            # enable screenshot capture (default: DNS-only)
--grpc-addr localhost:50051  # send report via gRPC; omit to print table to stdout
--dns-servers dns-server.yaml  # YAML file of DNS servers; omit to use system resolver
--compliant-ips 1.2.3.4,5.6.7.8  # IPs treated as compliant even when DNS resolves (e.g. ISP block-page IP); server passes this automatically from the admin-managed list, see "Domain semantics" below

# Server key flags (all accept env-var fallbacks)
--db-url "host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"
--minio-endpoint localhost:9000   # env: MINIO_ENDPOINT
--minio-access-key minioadmin     # env: MINIO_ACCESS_KEY
--minio-secret-key minioadmin     # env: MINIO_SECRET_KEY
--minio-bucket screenshots        # env: MINIO_BUCKET
--crawler-path ./crawler          # env: CRAWLER_PATH
--seed-dns dns-server.yaml        # seeds dns_servers table on first run if empty
--interval 60                     # scheduled scan interval in minutes (default 60)
--cookie-secure                   # mark the session cookie Secure (default true); env: COOKIE_SECURE — set false for local plain-HTTP dev
--bootstrap-admin-username admin  # env: BOOTSTRAP_ADMIN_USERNAME — creates the admin user only if `users` table is empty
--bootstrap-admin-password ...    # env: BOOTSTRAP_ADMIN_PASSWORD — required alongside the username on first run, or no one can log in
--ipinfo-token ...                 # env: IPINFO_TOKEN — ipinfo.io API token for ASN/org lookups; empty uses the unauthenticated (lower rate limit) tier
--whois-refresh-interval 1440      # minutes between WHOIS/RDAP refresh sweeps (default 1440 = 24h)
--whois-stale-days 30              # re-fetch a domain's WHOIS/RDAP data once its cached copy is older than this many days (default 30)

# Note: --db-url accepts a PostgreSQL DSN (key=value pairs), NOT a postgresql:// URL

# Sites file format: one URL per line; # lines are comments;
# bare hostnames are accepted — the pipeline prefixes https://;
# duplicates across file + CLI args are silently dropped

# Install / sync dependencies
go mod tidy

# Test all packages (screenshot tests are skipped unless Chrome is available)
go test ./...

# Test dependency summary:
# internal/db/       — uses SQLite in-memory; no PostgreSQL or external services needed
# internal/pipeline/ — fully mocked (Resolve + Capture injected via pipeline.Config)
# internal/dns/      — makes REAL network calls to 8.8.8.8/google.com; fails offline
# internal/screenshot/ — requires Chrome; guarded by INTEGRATION=1 build tag

# Test a single package / single test
go test ./internal/pipeline/...
go test -run TestCompliantSiteSkipsScreenshot ./internal/pipeline/...

# Run screenshot integration tests (require Chrome installed)
INTEGRATION=1 go test ./internal/screenshot/...

# Regenerate protobuf (requires protoc + protoc-gen-go + protoc-gen-go-grpc)
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/compliance.proto
```

## Web frontend (`web/`)

```bash
# Install dependencies (first time or after package.json changes)
cd web && npm install

# Dev server with hot reload (proxies /api to localhost:8080)
npm run dev        # serves on http://localhost:5173

# Production build (output to web/dist/)
npm run build

# Lint only (ESLint); type-check runs as part of build: tsc --noEmit
npm run lint

# Serve production build locally
npm run preview
```

For full-stack dev, run the Go server (`go run ./cmd/server/ ...`) alongside `npm run dev`. Vite proxies all `/api` requests to `localhost:8080`, so both must be running.

**Frontend architecture:**
- React 19 + TypeScript + Vite
- **TanStack Router** with file-based routing: add a file in `web/src/routes/` and the route is discovered automatically. `web/src/routeTree.gen.ts` is auto-generated by the Vite plugin — never edit it manually.
- Routes: `index.tsx` (`/` — Overview dashboard: renders a "National Compliance" summary (overall % + a 30-day national trend chart via `fetchNationalTrend`, only shown once ≥2 data points exist) above a single `ISPStatusTable`; ISP aggregation (`computeISPStats`) and the table/skeleton components live in the shared `@/components/isp-status-table.tsx`, not inline in the route — CSS progress-bar rows sorted worst-compliance-first, each ISP name linking to `/isps/$isp`; no per-DNS-server breakdown here, that granularity lives on the ISP detail page), `urls.tsx` (department watchlist management — all users manage only their own department's watchlist; each row has an animated enable/disable switch (`r-switch.tsx`) plus a native `<input type="date">` for the optional takedown-order date, both calling `PATCH /api/urls/{id}` with optimistic updates that roll back on failure; hard-delete/"purge" action is admin-only and shown conditionally), `dns-servers.tsx` (DNS server management — viewable by anyone, but add/delete 403s for non-admins), `results.tsx` (layout route, just an `Outlet`), `results.index.tsx` (`/results` — full compliance results table via `URLGroupRow`/`SubRows`, expandable per-DNS-server rows, history icon per row linking to `/results/$url`), `results.$url.tsx` (`/results/$url` — per-URL history, split into an `Overview`/`History` `Tabs` pair (`@/components/ui/tabs.tsx`): Overview holds the per-DNS-server year heatmap plus `DnsRecordsPanel` and `DomainInfoPanel`; History holds the paginated, scan-run-grouped results table with status/DNS-server filters; a `Breadcrumbs` trail (`@/components/breadcrumbs.tsx`) sits above both), `isps.$isp.tsx` (`/isps/$isp` — ISP detail page: fetches `fetchISPStats(isp)`, `fetchISPTrend(isp, 30)`, and `fetchISPTiming(isp)` in parallel; renders a `Breadcrumbs` trail, a "Most Non-Compliant Domain" callout, a "Time to Compliance" stat block (median days to block, still-open count, order-date coverage, plus a top-5 "slowest to block" table — hidden entirely when no domain has an order date) fed by `fetchISPTiming`, a 30-day compliance trend sparkline via the `LineChart`/`Line` primitives (only shown once ≥2 data points exist), and a per-server table with Compliance / Violations / Avg Latency / Min-Max Latency columns — latency columns show `—` when `avg_latency_ms` is 0, i.e. no measurements), `scan-results.tsx` (`/scan-results?urls=&triggeredAt=` — progressive results page reached only from the "Scan Selected" flow, not "Scan All"; polls `fetchResults()` every 3s while scanning, filters to the target URLs scanned after `triggeredAt`, and on completion diffs against a `sessionStorage` baseline snapshotted at scan-trigger time to compute newly-violating domains; also shows a client-computed "Worst ISP" stat linking to `/isps/$isp` and a DNS-error "Reason" column via `dns-error.ts`), `login.tsx` (public route, the only one reachable without a session), `admin.tsx`+`admin.index.tsx` (`/admin` — admin-only: departments table, "Create User" dialog, and a compliant-IPs table + "Add" dialog for managing the block-page IP exception list (see "Domain semantics" above); non-admins who navigate here directly see an "Admin access required" message rather than a redirect, since the API calls are gated server-side anyway).
- **Auth**: `web/src/routes/__root.tsx` defines an `AuthContext`/`useAuth()` (hydrated via `fetchMe()` on mount) alongside the pre-existing `ScanContext`. `RootLayout` gates rendering on this: no session → `<Navigate to="/login">` instead of `<Outlet>`; logged in while on `/login` → `<Navigate to="/">`. The navbar shows split "Scan All" / "Scan Selected" buttons: "Scan All" (`handleScanClick`) just calls `triggerScan()` and stays on the current page; "Scan Selected" opens `ScanSelectedDialog` — a modal that lists the calling user's watchlist as checkboxes (enabled URLs pre-checked) plus an ad-hoc URL textarea (entries normalized client-side via `normalizeForClient()`, a JS mirror of `urlnorm.Normalize` defined in `__root.tsx`) — `handleScanSelected` snapshots current results into `sessionStorage` (key `scan-baseline-${triggeredAt}`) as a diffing baseline, calls `triggerScan(urls)`, then navigates to `/scan-results` with the target URLs and trigger timestamp. `web/src/api/auth.ts` exports `login`, `logout`, and `fetchMe` (resolves to `null` on 401 instead of throwing, via `skipAuthRedirect`, so the root layout's own login check never causes a redirect loop). `web/src/api/client.ts` redirects to `/login` on any other 401 unless the call opts out with `{ skipAuthRedirect: true }`.
- `web/src/api/` — one module per resource (`urls.ts`, `dns-servers.ts`, `scan.ts`, `results.ts`, `auth.ts`, `admin.ts`, `isps.ts`, `domain.ts`) all calling through `client.ts` which is a thin `fetch` wrapper (sets `credentials: 'same-origin'`, handles 401s, and exposes `api.patch()`). Types shared in `types.ts` (includes `Department`, `User`, `URLEntry` with `enabled: boolean`, `ISPStats`, `ISPServerStat`, `ISPTrendStat`); `scan.ts` also owns `ScanProgressResponse` and `ProgressEntry` types; `triggerScan(urls?: string[])` sends `POST /api/scan` with an optional JSON body `{ "urls": [...] }` for targeted scans — no body means full sweep. `results.ts` exports `groupResults()`, `lastScanTime()`, `fetchResultsByUrl()`, and `fetchHeatmapByUrlAndYear()`. `urls.ts` exports `fetchUrlCount()`, `createUrl(url)` (no department_id — all users use their own department), `deleteUrl()`, `setUrlEnabled(id, enabled)`, and `setUrlOrderedAt(id, orderedAt)` (all three `PATCH /api/urls/{id}` with different body fields). `dns-servers.ts` exports `fetchDnsServerCount()`. `admin.ts` exports `fetchDepartments`, `createDepartment`, `fetchUsers`, `createUser`, `deleteUser`, `fetchUnassignedUrls`, `purgeUrl` (admin hard-delete, distinct from `urls.ts`'s watchlist-scoped `deleteUrl`), and `fetchCompliantIPs`/`createCompliantIP`/`deleteCompliantIP`. `isps.ts` exports `fetchISPStats(isp)` (`GET /api/isps/{isp}`), `fetchISPTrend(isp, sinceDays=30)` (`GET /api/isps/{isp}/trend?since=...`), and `fetchISPTiming(isp)` (`GET /api/isps/{isp}/timing`). `results.ts` also exports `fetchNationalTrend(sinceDays=30)` (`GET /api/trend?since=...`), used by the Overview page's national trend chart. `domain.ts` exports `fetchDomainInfo(url)` (`GET /api/domain/*url`), consumed by `results.$url.tsx` and `results.index.tsx` to show registrar/WHOIS data alongside IPv6/ASN fields already present on `ScanResult`. `web/src/lib/dns-error.ts` exports `classifyDNSError(error)` — pure string match on `ScanResult.error` classifying it as `nxdomain`/`timeout`/`servfail`/`other`/`none` — and `dnsErrorLabel(type)` for display text; used by `scan-results.tsx`'s Reason column.
- Scan state (polling, refresh signal) lives in a React context defined in `web/src/routes/__root.tsx` and consumed via `useScan()` in child routes.
- **Theming**: `web/src/main.tsx` wraps the app in `next-themes`' `ThemeProvider` (`attribute="class"`, `defaultTheme="light"`); `web/src/components/theme-switch.tsx` (`ThemeSwitch`) toggles it from the navbar. Dark-mode styles live alongside light ones in `web/src/index.css` under `html.dark`.
- `@` alias resolves to `web/src/` (configured in `vite.config.ts`).
- UI components: `web/src/components/ui/` — shadcn-style HTML primitive wrappers: `table.tsx` exports `Table`, `TableHeader`, `TableBody`, `TableRow`, `TableHead`, `TableCell`, `TableCaption` — use these instead of raw `<table>`/`<tr>`/`<td>` in all new table UI; `button.tsx` provides a base `Button`; `select.tsx` — custom animated dropdown (ported from fluidfunctionalism.com, trimmed to this app's design tokens) used by the filter/select controls on `dns-servers.tsx`, `admin.index.tsx`, `results.index.tsx`, and `results.$url.tsx`; built on three standalone primitives also usable elsewhere: `web/src/hooks/use-proximity-hover.ts` (cursor-proximity active-item tracking for menus), `web/src/lib/scroll-fade.tsx` (`useScrollEdges`/`ScrollEdgeCue` — fade + chevron affordance at scrollable edges), and `web/src/lib/springs.ts` (shared Framer Motion spring presets: `fast`/`moderate`/`slow`); `r-switch.tsx` — `@iconiq/r-switch` Framer Motion animated toggle switch, used for per-URL enable/disable in `urls.tsx`; `web/src/components/unlumen-ui/` — Radix UI primitives with Tailwind CSS, plus effects (`aurora-bars.tsx` — Framer Motion animated aurora background, used on the login page); `web/src/components/animate-ui/` — animated Radix primitives (`dialog.tsx` for motion transitions, `preview-link-card` for hover-preview links, `toggle-group.tsx` used by the status/DNS-server filters on `results.$url.tsx`); `web/src/components/charts/` — the `@bklit/line-chart`-based chart primitives (`line-chart.tsx`, `line.tsx`, `grid.tsx`, `x-axis.tsx`, `tooltip/`) plus `LiveLineChart` and supporting utilities (`decimate-time-series` — LTTB downsampling wired into the shared `time-series-chart-shell.tsx` so every `LineChart` gets it, `use-animated-y-domains`, etc.); `LiveLineChart` drives the animated real-time scan-progress chart on `results.index.tsx`, and the static `LineChart`/`Line` primitives render the national trend sparkline on `index.tsx` and the ISP trend sparkline on `isps.$isp.tsx`; `web/src/components/breadcrumbs.tsx` (`Breadcrumbs`) — simple `>`-separated trail component, used on `isps.$isp.tsx` and `results.$url.tsx`; `web/src/components/charts/heatmap/` — GitHub-style year heatmap primitives (`heatmap-chart.tsx`, `heatmap-cells.tsx`, `heatmap-x-axis.tsx`/`heatmap-y-axis.tsx`, `heatmap-legend.tsx`, `heatmap-tooltip.tsx`, `heatmap-chart-loading.tsx` for the skeleton state); `web/src/lib/heatmap-year.ts` bridges server-side `DailyComplianceStat[]` into the heatmap's column/bin shape (`buildYearHeatmapColumns`) and maps the 0-4 compliance `level` to CSS color tokens (`HEATMAP_LEVEL_COLORS`, mirroring `db.DailyComplianceLevel`) — rendered once per DNS server on `results.$url.tsx`; `web/src/components/aicanvas/glass-navbar.tsx` — the top navigation bar, reads `useAuth()` to conditionally show the Admin link (`is_admin` only) and a `LogoutButton`; `delete-confirm-dialog.tsx` — shared delete confirmation dialog used by both management pages, takes an optional `description` prop so callers can give an accurate, role-specific warning (e.g. "removes from your department's watchlist" vs. admin's "permanently deletes"). Use the `cn()` helper from `web/src/lib/utils.ts` for conditional class composition. Icons from `lucide-react`; animations via `motion` (Framer Motion).

## Docker

```bash
# Full stack with MinIO (supply DB_URL separately or use dev overlay):
docker compose up

# Dev overlay adds a local PostgreSQL container (port 5432 published for local psql/GUI access)
# and pre-sets COOKIE_SECURE=false — no extra flags needed for local plain-HTTP dev:
docker compose -f docker-compose.yml -f docker-compose.dev.yml up

# The Dockerfile is multi-stage: builder (golang:1.26) produces both binaries;
# runtime (debian:bookworm-slim) includes Chromium for screenshot support.
# ENTRYPOINT is /app/server; /app/crawler is the default CRAWLER_PATH.
```

## Architecture

This is a **two-binary system**:

- **`cmd/crawler`** — standalone CLI that runs DNS checks (and optionally screenshots) and reports results via gRPC or stdout.
- **`cmd/server`** — long-running backend that exposes a REST API and a gRPC receiver, persists results to PostgreSQL, stores screenshots in MinIO, and manages the crawler as a subprocess.

### Domain semantics

The tool checks ISP takedown compliance. A site that **resolves DNS** is a **violation** (`Compliant=false`); one that **fails DNS** is **compliant** (`Compliant=true`). This inversion is intentional.

**Compliant IPs exception**: some ISPs redirect blocked domains to a block-page IP instead of failing DNS outright (e.g. MCMC's redirect IP in Malaysia). `internal/pipeline.checkDNS` treats a resolved IP as compliant anyway if it matches the `CompliantIPs` list (`internal/pipeline/pipeline.go`). The list is admin-managed via `db.CompliantIP` rows (`GET/POST /api/admin/compliant-ips`, `DELETE /api/admin/compliant-ips/{id}` — admin-only, handlers in `admin_handlers.go`) and surfaced in the `/admin` UI (`admin.index.tsx`'s `AddCompliantIPDialog` + list). `scanner.go` fetches the current list (`store.ListCompliantIPs`) before every `Trigger`/`TriggerScreenshot` and passes it to the crawler as `--compliant-ips ip1,ip2,...` (also settable directly when running the crawler standalone).

### Server (`cmd/server`, `internal/server/`)

- **HTTP** on `:8080` via [chi](https://github.com/go-chi/chi). All routes under `/api` except login require a valid session cookie (`requireAuth`); routes under `/api/admin/*` plus DNS server mutation additionally require `is_admin` (`requireAdmin`). See "Auth & RBAC" below.
  - `POST /api/auth/login` (public), `POST /api/auth/logout`, `GET /api/auth/me`
  - `GET /api/urls` — admin: every URL globally; non-admin: only their own department's watchlist (`ListDepartmentURLs`); returns `URLEntry[]` (includes `enabled` flag per department)
  - `POST /api/urls` (`AddToWatchlist`) — gets-or-creates the URL by normalized value and links it to the calling user's department watchlist; all roles (including admin) use their own `DepartmentID`
  - `PATCH /api/urls/{id}` — updates the calling user's department watchlist entry; body accepts `{ "enabled"?: bool, "ordered_at"?: string }` and only touches fields present in the body. `enabled` toggles `DepartmentURL.Enabled` (`ListWatchedURLs` only includes enabled URLs, so disabling a URL removes it from future scans without deleting watchlist membership). `ordered_at` sets `DepartmentURL.OrderedAt` — RFC3339 to set, `""` to clear — the optional takedown-order date used by the time-to-compliance metric (see "ISP stats scoping" below)
  - `DELETE /api/urls/{id}` (`RemoveFromWatchlist`) — unlinks the URL from the calling user's department watchlist only; never deletes the `URL` row or its `ScanResult` history. 404 if the URL wasn't actually on that watchlist.
  - `GET /api/dns-servers` — any authenticated role (results reference servers by name, so everyone needs to read this)
  - `POST /api/dns-servers` — **admin-only**; body requires `isp` (string, mandatory), `address`, and optionally `name`/`protocol`
  - `DELETE /api/dns-servers/{id}` — **admin-only**
  - `POST /api/scan`, `GET /api/scan/status` — trigger a DNS-only scan / poll status
  - `GET /api/scan/progress` — per-DNS-server result counts for the active scan run (polled by dashboard); `total_urls` reflects `ListWatchedURLs` (domains on at least one watchlist), not the global pool
  - `GET /api/scan/progress/stream` — SSE endpoint; server pushes the same `ScanProgressResponse` JSON whenever the `Broadcaster` publishes (crawler calls this implicitly via gRPC `Submit`)
  - `GET /api/results` — admin: global `LatestResults`; non-admin: `LatestResultsForDepartment`
  - `GET /api/results/*url`, `GET /api/heatmap/*url` — non-admin gets a 404 (not 403, to avoid confirming the domain exists) unless `URLOwnedByDepartment` passes; admin unscoped. `since`/`until` query params are RFC3339, defaulting to the last 7 days
  - `GET /api/dns-records/*url` — live DNS lookup, unscoped for any authenticated role (not watchlist data)
  - `GET /api/domain/*url` (`DomainInfoByURL`) — cached RDAP registrar/creation/expiry info for a domain, scoped like `/api/results` (404, not 403, for a non-owning department). Returns `{"fetched":false}` (not a 404) when the domain is owned but has no cached `DomainWhois` row yet
  - `POST /api/screenshot` — trigger a single-URL screenshot scan for a specific DNS server, unscoped for any authenticated role
  - `GET /api/isps/{isp}` (`ISPStats`) — per-DNS-server compliance + latency aggregates for one ISP, plus its most-violated domain; admin: global `store.ISPStats`; non-admin: `ISPStatsForDepartment`, scoped to their department's enabled watchlist (see "ISP stats scoping" below)
  - `GET /api/isps/{isp}/trend` (`ISPTrend`) — daily compliance counts for one ISP over a `since`/`until` RFC3339 window (defaults to last 30 days); same admin/department scoping as `ISPStats`
  - `GET /api/isps/{isp}/timing` (`ISPTiming`) — time-to-compliance for one ISP: median/avg days from a domain's `DepartmentURL.OrderedAt` to its first compliant scan by that ISP, plus blocked/still-open/coverage counts and a top-5 "slowest to block" list; same admin/department scoping as `ISPStats`. Domains with no `OrderedAt` are excluded, not defaulted to some other date — see "ISP stats scoping" below
  - `GET /api/trend` (`NationalTrend`) — daily compliance counts aggregated across **all** ISPs (same shape as `ISPTrend`, no `isp` filter); same admin/department scoping, backs the Overview page's national trend chart
  - `GET/POST /api/admin/departments`, `GET/POST /api/admin/users`, `DELETE /api/admin/users/{id}` — **admin-only** department/user management
  - `GET/POST /api/admin/compliant-ips`, `DELETE /api/admin/compliant-ips/{id}` — **admin-only**; manages the `db.CompliantIP` list consumed by the crawler's `--compliant-ips` flag (see "Domain semantics" above)
  - `GET /api/admin/urls/unassigned` — **admin-only**: URLs with zero `DepartmentURL` links (pre-migration legacy rows, or domains every department has since removed)
  - `DELETE /api/admin/urls/{id}` (`PurgeURL`) — **admin-only** hard delete; cascades to `ScanResult` and `DepartmentURL` via FK. The only way to actually destroy scan history — `RemoveFromWatchlist` never does.
- **gRPC** on `:50051` — receives `ComplianceReport` submissions from the crawler subprocess; uploads any screenshot bytes to MinIO, stores results in PostgreSQL, then calls `broadcaster.Publish` so all SSE subscribers receive the updated progress. Unauthenticated — runs on the trusted crawler↔server link, not the public API.
- **Broadcaster** (`broadcaster.go`) — in-memory fan-out pub/sub for SSE. `Subscribe()` returns a buffered `chan []byte`; slow consumers are silently dropped via a non-blocking `select`. Wired into `Handlers` and called from `grpcServer.Submit` after each batch insert.
- **Scanner** (`scanner.go`) — manages crawler subprocess lifecycle with a mutex-guarded `running` flag. `Trigger(ctx, reason, urls []string)` does a DNS-only crawl against the provided URL list, or against `store.ListWatchedURLs(ctx)` (enabled-only) when `urls` is `nil`. `TriggerScreenshot` adds `--screenshots` and targets a single URL + DNS server. Both write temp files for the URL list and DNS YAML then call `exec.CommandContext`. The crawler's stdout is wired to the server's stderr so its log output appears in server logs.
- **Scheduler** (`scheduler.go`) — `StartScheduler` ticks every `--interval` minutes (default 60) and calls `scanner.Trigger(ctx, "scheduled", nil)` (full sweep, unchanged).
- Server flags accept env-var fallbacks: `DB_URL`, `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET`, `CRAWLER_PATH`, `COOKIE_SECURE`, `BOOTSTRAP_ADMIN_USERNAME`, `BOOTSTRAP_ADMIN_PASSWORD`.

### Auth & RBAC (`internal/server/auth.go`, `auth_handlers.go`, `admin_handlers.go`, `internal/db/auth.go`)

- **Session-cookie auth**, not JWT: `POST /api/auth/login` checks `db.CheckPassword` (bcrypt, via `golang.org/x/crypto/bcrypt`) against `User.PasswordHash`, then creates a random-token `Session` row (`db.HashPassword`/`CheckPassword` live in `internal/db` so both the login handler and `db.SeedAdmin` share one implementation) and sets an HTTP-only, `SameSite=Lax` cookie (`session_token`). `--cookie-secure` controls the `Secure` flag — set `false` for local plain-HTTP dev, since Vite's proxy talks over plain HTTP.
- `requireAuth(store)` middleware (`auth.go`) resolves the cookie → `Session` → `User` and attaches the full `*db.User` to the request context (`userFromContext`); 401s if the cookie is missing, the session doesn't exist, or it's expired (`GetSession` filters `expires_at > now`). `requireAdmin` must run after it and 403s unless `user.IsAdmin`.
- **Three roles for now**: `Admin` is a boolean flag on `User` (`IsAdmin`), not a department — admins see/manage everything and have their own department (created by the admin migration in `internal/db/migrate.go`). `CMOD`/`CRD` are real rows in the `departments` table (seeded by `db.SeedDepartments`, only if the table is empty) — more departments can be added later by inserting rows, no schema or code change needed.
- **Bootstrap admin**: `db.SeedAdmin(gormDB, username, password)` creates the first admin user only if `users` is empty, using `--bootstrap-admin-username`/`--bootstrap-admin-password`. Without this on a fresh deployment, no one could ever log in. Re-running with different flags is a silent no-op once any user exists.
- **Account creation is admin-provisioned only** — no self-registration, no password-reset flow. Admin sets a username + temporary password directly via the `/admin` UI (`POST /api/admin/users`).
- **ISP stats scoping** (`internal/db/postgres.go`, `ISPStats`/`ISPTrend`/`ISPTiming`/`NationalTrend` handlers in `internal/server/handlers.go`): follows the same admin-global / non-admin-department-scoped pattern as `/api/results`. Admins get `store.ISPStats`/`ISPTrend`/`ISPComplianceTiming`/`NationalTrend` (unscoped); non-admins (403 if `user.DepartmentID` is nil) get the `*ForDepartment` variant, which joins `department_urls` filtered to `department_id = ? AND enabled = true` — so non-admins only see ISP aggregates over their own department's enabled watchlist domains. Latency aggregates (`AVG`/`MIN`/`MAX` of `latency_ms`) exclude zero-latency rows (`CASE WHEN latency_ms > 0 THEN latency_ms END`), since 0 means "no measurement" (compliant/failed-DNS rows), not an instant response.
- **Time-to-compliance** (`ispComplianceTiming` in `internal/db/postgres.go`): `DepartmentURL.OrderedAt` is an optional, nullable takedown-order date — settable at add-time or later via `PATCH /api/urls/{id}`, never inferred from `CreatedAt`. For a given ISP, a domain's "days to block" is its first compliant scan by that ISP's servers minus `OrderedAt`, clamped to 0 if the compliant scan predates the order (already-blocked-before-order, not a negative duration). Domains with no `OrderedAt` are excluded from the aggregate entirely — `WithOrderDateCount`/`TotalDomains` in the response exists so the UI can show real coverage instead of silently blending in a proxy date. Admin scope uses the earliest `OrderedAt` across departments per domain; aggregation happens in Go (not SQL `MIN()`) because the SQLite test driver can't scan a `MIN()` of a datetime column into `time.Time`.

### Database (`internal/db/`)

- GORM with PostgreSQL driver (`gorm.io/driver/postgres`). SQLite driver (`glebarez/sqlite`) is also linked but PostgreSQL is the production path.
- Models: `DNSServer`, `URL`, `ScanRun`, `ScanResult`, `Department`, `User`, `Session`, `DepartmentURL`, `DomainWhois`, `IPInfo`. `DNSServer` has a required `ISP` field (groups servers in the UI's `groupByISP()`; `not null;default:'Unknown'`). `ScanResult` foreign-keys to `ScanRun`, `DNSServer`, and `URL` (via `URLID`); `URLValue` is kept denormalized alongside `URLID` for display/matching without a join. `ScanResult.LatencyMs int64` (`default:0`) holds DNS round-trip latency in milliseconds, 0 when resolution failed or wasn't measured — captured end-to-end from `internal/dns` resolver return values through `pipeline.Result.LatencyMs` → the crawler's report entry (`proto/compliance.proto`'s `latency_ms` field) → `grpcServer.Submit`; surfaced in the ISP detail page's Avg/Min/Max Latency columns (see "ISP stats scoping" above for how zero-latency rows are excluded from those aggregates). `ScanResult.ResolvedIPv6`/`ResolvedASN`/`ResolvedOrg` hold the IPv6/ASN/org enrichment data (see "IP/ASN + WHOIS enrichment" below) — informational only, never affect `Compliant`. `User.DepartmentID` is non-nil for all users including admin (admin's department is created by migration). `DepartmentURL` is a composite-PK (`department_id`, `url_id`) join table with an `Enabled bool` field and an optional `OrderedAt *time.Time` (the takedown-order date used by time-to-compliance, see "ISP stats scoping" above) — see "Domain normalization & watchlists" below. `URLEntry` is a read-side struct pairing a `*URL` with the calling department's `Enabled` flag and `OrderedAt`; `ListDepartmentURLs` returns `[]URLEntry`. `SetURLEnabled(ctx, deptID, urlID, enabled)` updates `DepartmentURL.Enabled` in place; `SetURLOrderedAt(ctx, deptID, urlID, orderedAt)` does the same for `OrderedAt` (nil clears it); `ListWatchedURLs` only returns URLs where `enabled = true`, so disabling a URL silently excludes it from scans.
- `db.Store` interface decouples handlers and the gRPC server from the concrete implementation — tests can swap in a fake. Handler tests use `fullMockStore` (defined in `internal/server/handlers_test.go`) which implements all Store methods; copy this struct when adding new server tests.
- **IP/ASN + WHOIS enrichment** (`internal/ipinfo/`, `internal/whois/`): informational data points that never affect the compliance verdict, which stays A-record-based.
  - `ScanResult.ResolvedIPv6` — AAAA-record lookup done alongside the A-record check (`internal/dns/resolver.go`'s `ResolveIPv6`/`NewResolverIPv6`/`NewDoTResolverIPv6`/`NewDoHResolverIPv6`, wired through `pipeline.Config.ResolveIPv6`); nil function = skip.
  - `ScanResult.ResolvedASN`/`ResolvedOrg` — the resolved IP's ASN + network operator via `ipinfo.Fetcher` (`internal/ipinfo/`, ipinfo.io; `--ipinfo-token` for the authenticated tier). Cached in `db.IPInfo` keyed by IP (not domain, since an IP's ASN doesn't depend on which domain currently resolves to it) — fetched **at most once per distinct IP, ever** (no periodic refresh). `grpcServer.Submit` (`internal/server/grpc.go`) checks the cache first; on a miss it spawns a **detached goroutine** (`fetchAndCacheIPInfo`) so a slow/unreachable ipinfo.io never blocks scan ingestion — that scan's row simply has a zero ASN/Org until a later scan re-checks the now-warm cache.
  - `db.DomainWhois` caches RDAP registrar + creation/expiry dates per domain (`internal/whois/`, via `github.com/openrdap/rdap`), surfaced at `GET /api/domain/*url`. Lazily fetched in a detached goroutine on watchlist-add (`fetchAndStoreWhois` in `handlers.go`, only if `whoisFetch` is non-nil), then kept warm by `server.StartWhoisRefresher` (`internal/server/whois_refresh.go`) — a background loop, separate from the scan `Scheduler`, that every `--whois-refresh-interval` (default 24h) re-fetches domains whose cached copy is older than `--whois-stale-days` (default 30), in batches of `whoisRefreshBatchSize` (20) with a fixed pause between fetches (`ponytail`-marked: sequential, no backoff/jitter — upgrade if RDAP 429s show up). A failed fetch is recorded in `FetchError` but leaves any previously-cached registrar/dates in place rather than wiping them.
- `db.Seed` populates `dns_servers` on first run from the YAML file passed via `--seed-dns` (skips if table is non-empty); `db.SeedDepartments` similarly seeds `CMOD`/`CRD` only if `departments` is empty; `db.SeedAdmin` creates the bootstrap admin only if `users` is empty (see "Auth & RBAC" above).
- `PurgeURL`'s `DeleteURL` is a plain `Delete(&URL{}, id)` — dependent `ScanResult` and `DepartmentURL` rows are removed via `OnDelete:CASCADE` FK constraints, not application code. If you add another table that references `URL`, give it the same FK-level cascade rather than reintroducing manual cleanup. `RemoveFromWatchlist`, by contrast, deletes only the `DepartmentURL` row — it's the one path that's deliberately *not* a cascade. `DeleteDNSServer` is different: it manually deletes `ScanResult` rows in a transaction before deleting the server, because there is no FK-level `OnDelete:CASCADE` from `ScanResult` to `DNSServer` — if you add other tables referencing `DNSServer`, extend that transaction rather than adding a FK cascade.
- `grpcServer.Submit` resolves `URLID` by matching `ScanResult.URLValue` against `store.ListWatchedURLs()` in memory (`urlIDByValue` map), defensively re-normalizing via `urlnorm.Normalize` if the raw value isn't an exact match — there's no DB-level join here, so a URL must be on at least one department's watchlist before results referencing it can be inserted with a non-zero `URLID`.
- `DailyComplianceByURL(ctx, urlValue, since, until)` aggregates `ScanResult` rows server-side into one `DailyComplianceStat` per (DNS server, calendar day) — avoids shipping raw rows to the client just to bucket them for the heatmap. `DailyComplianceLevel(total, compliant)` (`internal/db/models.go`) buckets each day onto a 0-4 scale (0 = no scans, 1 = fully compliant, 2-4 = increasing violation share) — the frontend's `HEATMAP_LEVEL_COLORS` (`web/src/lib/heatmap-year.ts`) must stay in sync with this scale if it changes.

### Domain normalization & watchlists (`internal/urlnorm/`, `internal/db/migrate.go`)

- `urlnorm.Normalize(raw string) (string, error)` canonicalizes any input (`https://Example.com/`, `example.com`, etc.) down to a lowercase bare hostname — strips scheme, userinfo, path, query, fragment, port, trailing dot. This is a separate, new package — **do not** reuse `pipeline.normalizeURL` (`internal/pipeline/pipeline.go`), which only prefixes a scheme for crawling and must keep working exactly as today.
- `URL.URL` is expected to always be normalized by the time it's stored. `postgresStore.CreateURL` normalizes + `FirstOrCreate`s by the normalized value, so the same domain added in different raw formats by different departments always resolves to one shared row — this is what lets departments share scan history for overlapping domains while keeping separate watchlist visibility (via `DepartmentURL`).
- `db.NormalizeAndDedupeURLs(ctx, gormDB)` is a one-time, idempotent, transactional backfill (called from `main.go` on every startup, before `db.NewStore`) that normalizes any pre-existing unnormalized `urls.url` rows and merges post-normalization duplicates onto the lowest-ID row, reassigning `ScanResult.URLID` and `DepartmentURL` links before deleting the duplicate. Existing rows get **no** `DepartmentURL` links from this — they're visible only via the admin "unassigned" view (`GET /api/admin/urls/unassigned`) until a department explicitly adds them.
- `ListURLs`/`CreateURL`/`DeleteURL` on `db.Store` are the **global/admin** scope, unchanged signatures from before RBAC existed. `ListDepartmentURLs`, `AddURLToWatchlist`, `RemoveURLFromWatchlist`, `ListWatchedURLs`, `ListUnassignedURLs`, `URLOwnedByDepartment` are the new department-scoped methods layered on top — see the route list above for which handler calls which.

### Storage (`internal/storage/`)

- `storage.Storage` interface with a single `Upload(ctx, []byte) (string, error)` method.
- `minioStorage` uploads PNGs as `<uuid>.png`; returns a public HTTP URL (`http://<endpoint>/<bucket>/<uuid>.png`). The `minio-init` container in docker-compose sets the bucket policy to public.

### Crawler pipeline (`internal/pipeline/`, `cmd/crawler/`)

**Two-stage concurrent pipeline**:
1. DNS worker pool — resolves hostnames; sites that fail DNS are immediately emitted as compliant and skip stage 2. Uses `cfg.DNSTimeout` per site.
2. Screenshot worker pool — only processes sites that resolved DNS; captures full-page PNG via headless Chrome. Uses `cfg.ScreenshotTimeout` per site.

`pipeline.Config` injects `Resolve`, `Capture`, and `OnResult` as function values. Tests use mock functions for `Resolve` and `Capture`.

When `--screenshots` is off (default), `Capture` is a no-op. When multiple DNS servers are configured, the crawler runs one DNS-only `pipeline.Run` per server then calls `captureResolved` once at the end to deduplicate `(url, resolvedIP)` screenshot jobs.

**Go concurrency model**: goroutines communicate via channels; `sync.WaitGroup` is a barrier (like `asyncio.gather`); `context.Context` carries deadlines and cancellation.

### Screenshot capture (`internal/screenshot/`, `cmd/crawler/main.go`)

- `AllocatorOptionsWithHostRules(rules)` builds Chrome allocator options with `--host-resolver-rules` so Chrome connects to the pre-resolved IP rather than re-resolving.
- `CaptureWithAllocator(ctx, allocCtx, rawURL, waitIdle, postIdleSleep)` — set UA + stealth JS → enable lifecycle events → navigate → wait for `networkIdle` (capped at `waitIdle`) → sleep `postIdleSleep` → full-page screenshot → frame.
- Stealth: Windows Chrome UA, `Accept-Language`, `platform`, hides `navigator.webdriver`, spoofs plugins/languages/`window.chrome`, patches `permissions.query`. `disable-blink-features=AutomationControlled` at allocator level.
- `frame.go`: wraps PNG in a Chrome UI mockup via a `data:text/html;base64,...` URL. Falls back to raw PNG if framing fails.
- Crawler saves screenshots locally to `<dns_label>/<hostname>/<timestamp>-<urlhash>.png` (spaces in DNS name → underscores; no server → `system`). Server-mode screenshots go to MinIO instead.
- **Screenshot batching** (`groupJobs` in `cmd/crawler/main.go`): Chrome's `--host-resolver-rules` can only map each hostname to one IP per allocator. When multiple DNS servers resolve the same hostname to *different* IPs, `groupJobs` splits those jobs across separate Chrome allocator instances to avoid conflicts. In the common case (all servers agree on the same IP) everything runs in one batch.

### DNS resolution (`internal/dns/`, `internal/dnsconfig/`)

- `dns.Resolve` — system resolver. `dns.NewResolver(addr)` — plain UDP. `dns.NewDoTResolver(addr)` — DNS-over-TLS. `dns.NewDoHResolver(endpoint)` — DNS-over-HTTPS (RFC 8484 GET wire format).
- YAML format for `--dns-servers` (`isp` is **required**; `name` defaults to the address if omitted):
  ```yaml
  servers:
    - isp: Cloudflare
      name: Cloudflare DoT
      address: 1.1.1.1:853
      protocol: dot   # udp | dot | doh
  ```
- **DNS checks hostname only**, not the full URL path. `dig @<server> www.example.com` is the correct manual equivalent — passing the full URL to dig returns NXDOMAIN.

### gRPC (`internal/sender/`, `internal/server/grpc.go`, `proto/`)

- `proto/compliance.proto` defines `ComplianceService.Submit(ComplianceReport)`; generated Go files are committed in `proto/`.
- Crawler-side: `sender.Send` submits a report; `printTable` always prints to stdout as well.
- Server-side: `grpcServer.Submit` looks up the active `ScanRun`, matches DNS server names to IDs, calls `store.InsertResult` for each entry, and uploads any screenshot bytes to MinIO.
- gRPC transport uses no TLS (`insecure.NewCredentials()`); both crawler and server must be on a trusted network.

### URL loading (`internal/input/`)

- `input.Load(filePath, args)` merges file + CLI args into a deduplicated slice. Bare hostnames are normalized to `https://` by `pipeline.normalizeURL`.

**Module name**: `github.com/afif/dns-tracking` (in `go.mod`, despite the repo directory being `dns-compliance`)

## TODO

- **Overview page (`web/src/routes/index.tsx`)**: renders a "National Compliance" summary (overall %, a checks-compliant count, and a 30-day national trend chart) above a single ISP-grouped `ISPStatusTable` (CSS progress-bar rows, one per ISP, sorted worst-compliance-first, linking to `/isps/$isp`). The per-DNS-server breakdown and expandable "Compliance Results" table (`URLGroupRow`/`SubRows`) live on `/results` (`results.index.tsx`), not here. Still missing: an animated chart variant for the ISP table — replace or augment the CSS bars with the `LiveLineChart` pattern from `results.index.tsx` if a richer visualization is wanted; and ISP rank-movement (e.g. a slope chart comparing this period vs. last) — needs historical ranking snapshots, not just the current one.
- **Screenshot viewing page**: `screenshot_url` is currently only ever surfaced as a raw "View screenshot" link (`results.index.tsx`, `results.$url.tsx`) that opens the bare PNG in a new tab — there's no in-app gallery for browsing evidence. Likely lands on `results.$url.tsx` (`/results/$url`), since that page already lists every scan row (DNS server, timestamp, screenshot) for one URL: add a thumbnail/lightbox view over the existing history table instead of (or alongside) the plain link, so an auditor can flip through a domain's captured screenshots over time without leaving the app.
- **Exportable compliance report**: per-ISP / per-period PDF or CSV bundling compliance %, time-to-compliance (`GET /api/isps/{isp}/timing`), regressions (domains that flip compliant → violation), and screenshot evidence over time — the artifact that leaves the building for an enforcement action, rather than something only viewable in-app. **Open questions requiring product/User-department sign-off before building:** which page/table hosts the export button (`/results`? the ISP detail page? Overview?), PDF vs CSV vs both, and the exact fields/evidence each export must legally contain.
- **Domain status page**: `results.$url.tsx` splits into an Overview tab (heatmap, `DnsRecordsPanel`, `DomainInfoPanel` registrar/WHOIS) and a History tab (the paginated scan-by-scan table), but there's still no dedicated "current status at a glance" summary — e.g. latest verdict per DNS server condensed into one card — separate from the two existing tabs.
- **Error column on scan result tables**: `db.ScanResult.Error` (`internal/db/models.go`) already captures DNS-query failures (populated end-to-end via `pipeline.SiteResult.Error` → gRPC `Submit`), but neither `results.index.tsx` nor `results.$url.tsx`'s history table renders it — a failed lookup just shows an empty "Resolved IP" cell with no reason. Screenshot failures are a separate gap: `captureResolved` (`cmd/crawler/main.go`) only `log.Printf`s a capture error (e.g. `context deadline exceeded`, `net::ERR_CONNECTION_REFUSED`) and drops it — it never reaches `SiteResult.Error`, so a stuck screenshot silently reverts to the camera icon with zero indication why. Add an "Error" column to both tables sourced from `ScanResult.Error`, and wire `captureResolved`'s per-job error into the corresponding `SiteResult.Error` (mirroring how `pipeline.takeScreenshot` already does it for the single-server path) so screenshot failures populate the same field as DNS failures instead of only appearing in server logs.

## Product & Design

[PRODUCT.md](./PRODUCT.md) defines the target users (regulatory auditors, not developers) and brand personality (neutral, evidence-first, no editorializing on violations). [DESIGN.md](./DESIGN.md) defines the visual system ("The Registry" — achromatic gray scale plus a single ledger-indigo accent reserved for actions/identity, never for compliance status). Read both before making UI changes — the anti-references (generic SaaS dashboards, security-product dark/neon aesthetics) are deliberate constraints, not omissions.

`docs/superpowers/specs/` and `docs/superpowers/plans/` hold dated design-spec/implementation-plan pairs behind individual features (e.g. `2026-06-30-additional-stats-design.md` covers ISP grouping, latency capture, ISP stats/trend, DNS error classification, worst-ISP stat, and newly-violating domains) — useful for archaeology on *why* a feature looks the way it does.

## Security

See [SECURITY.md](./SECURITY.md) for the full security audit report (score: 32/100).

Key issues to address before any non-private deployment:
- SEC-001: ~~No authentication on any endpoint~~ — resolved: session-cookie auth + RBAC (`requireAuth`/`requireAdmin` in `internal/server/router.go`, see "Auth & RBAC" above). Still worth a follow-up audit: gRPC remains unauthenticated by design (trusted crawler↔server link only) and rate-limiting/lockout on `POST /api/auth/login` doesn't exist yet.
- SEC-003: SSRF via `POST /api/screenshot` and `POST /api/urls` — validate URLs against private IP ranges
- SEC-005: Raw DB errors leaked in responses — map errors to safe messages in handlers
