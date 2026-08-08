# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`README.md` is stale — it documents a pre-refactor single-binary CLI (`cmd/main.go`, binary `dns-compliance`) that no longer exists, predating the `cmd/server`/`cmd/crawler` split. Don't follow its build/usage instructions; the Commands section below is authoritative.

Frontend, server/auth, and database details live in their own directory-scoped `CLAUDE.md` files (`web/CLAUDE.md`, `internal/server/CLAUDE.md`, `internal/db/CLAUDE.md`) rather than here — they load automatically when working under those directories. The legal citation catalog and notifications subsystems are covered by the `legal-citation-catalog` and `notifications` skills instead, since they span multiple directories.

## Commands

```bash
# Full-stack dev startup (PostgreSQL via Docker, server on :8080, Vite on :5173)
# Builds crawler, starts postgres, launches server + frontend; Ctrl+C shuts all down
./dev.sh

# Build both binaries
go build -o server  ./cmd/server/
go build -o crawler ./cmd/crawler/

# Baseline DNS benchmark (dig/nslookup/curl) to compare against crawler concurrency
./dns_benchmark.sh [site-list.txt] [dns-server.yaml]   # writes to benchmark_results/

# Generate a private CA + one leaf cert per binary, to enable mutual TLS on the
# gRPC link between server and crawler (see gRPC section below); output goes to
# the gitignored certs/, unrelated to any public HTTPS cert the dashboard uses
./scripts/gen-mtls-certs.sh [extra-hostname-or-IP ...]

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
--listen-addr :50052       # run as a persistent gRPC control service instead of a one-shot sweep; the dashboard triggers sweeps via CrawlerControl.StartSweep instead of exec'ing this binary — see Architecture below
--auth-token ...           # shared secret for both gRPC directions: required on incoming StartSweep RPCs, and sent with outgoing Submit RPCs; must match the server's --crawler-token
--tls-cert certs/crawler.crt   # env: TLS_CERT — enables mTLS when set with --tls-key and --tls-ca
--tls-key certs/crawler.key    # env: TLS_KEY
--tls-ca certs/ca.crt          # env: TLS_CA — CA that signed both binaries' certs

# Server key flags (all accept env-var fallbacks)
--db-url "host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"
--minio-endpoint localhost:9000   # env: MINIO_ENDPOINT
--minio-access-key minioadmin     # env: MINIO_ACCESS_KEY
--minio-secret-key minioadmin     # env: MINIO_SECRET_KEY
--minio-bucket screenshots        # env: MINIO_BUCKET
--crawler-addr localhost:50052    # env: CRAWLER_ADDR — gRPC address of the crawler's control service (see --listen-addr above)
--crawler-token ...                # env: CRAWLER_TOKEN — shared secret for both gRPC directions: sent with outgoing StartSweep RPCs, and required on incoming Submit RPCs; must match the crawler's --auth-token
--redis-addr localhost:6379        # env: REDIS_ADDR — Redis address for the notification task queue (asynq); see the "notifications" skill
--seed-dns dns-server.yaml        # seeds dns_servers table on first run if empty
--interval 60                     # scheduled scan interval in minutes (default 60)
--cookie-secure                   # mark the session cookie Secure (default true); env: COOKIE_SECURE — set false for local plain-HTTP dev
--bootstrap-admin-username admin  # env: BOOTSTRAP_ADMIN_USERNAME — creates the admin user only if `users` table is empty
--bootstrap-admin-password ...    # env: BOOTSTRAP_ADMIN_PASSWORD — required alongside the username on first run, or no one can log in
# Local dev DB (docker-compose.dev.yml) admin login: admin / aaAA1234
--ipinfo-token ...                 # env: IPINFO_TOKEN — ipinfo.io API token for ASN/org lookups; empty uses the unauthenticated (lower rate limit) tier
--whois-refresh-interval 1440      # minutes between WHOIS/RDAP refresh sweeps (default 1440 = 24h)
--whois-stale-days 30              # re-fetch a domain's WHOIS/RDAP data once its cached copy is older than this many days (default 30)
--subfinder-path subfinder          # env: SUBFINDER_PATH — path to the subfinder binary (github.com/projectdiscovery/subfinder); empty disables subdomain enumeration entirely
--tls-cert certs/server.crt    # env: TLS_CERT — enables mTLS when set with --tls-key and --tls-ca
--tls-key certs/server.key     # env: TLS_KEY
--tls-ca certs/ca.crt          # env: TLS_CA — CA that signed both binaries' certs

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

## Docker

```bash
# Full stack with MinIO (supply DB_URL separately or use dev overlay):
docker compose up

# Dev overlay adds a local PostgreSQL container (port 5432 published for local psql/GUI access)
# and pre-sets COOKIE_SECURE=false — no extra flags needed for local plain-HTTP dev:
docker compose -f docker-compose.yml -f docker-compose.dev.yml up

# The Dockerfile is multi-stage: builder (golang:1.26) produces both binaries
# plus a standalone `go install` of subfinder (not a go.mod dependency —
# only ever shelled out to, see internal/subfinder/); runtime
# (debian:bookworm-slim) includes Chromium for screenshot support and ships
# the subfinder binary at /app/subfinder (SUBFINDER_PATH).
# ENTRYPOINT is /app/server. docker-compose.yml runs a second container from
# the same image with an `entrypoint: ["/app/crawler"]` override, as the
# `crawler` service — the two talk over gRPC (CrawlerControl.StartSweep to
# trigger sweeps, ComplianceService.Submit to report results), not exec.
# docker-compose.yml/docker-compose.dev.yml also run a `redis` service (with
# AOF persistence) backing the notification task queue — see the "notifications" skill.
```

## Architecture

This is a **two-binary system**:

- **`cmd/crawler`** — standalone CLI that runs DNS checks (and optionally screenshots) and reports results via gRPC or stdout.
- **`cmd/server`** — long-running backend that exposes a REST API and a gRPC receiver, persists results to PostgreSQL, stores screenshots in MinIO, and triggers sweeps on the crawler's persistent control service over gRPC.

### Domain semantics

The tool checks ISP takedown compliance. A site that **resolves DNS** is a **violation** (`Compliant=false`); one that **fails DNS** is **compliant** (`Compliant=true`). This inversion is intentional.

**Compliant IPs exception**: some ISPs redirect blocked domains to a block-page IP instead of failing DNS outright (e.g. MCMC's redirect IP in Malaysia). `internal/pipeline.checkDNS` treats a resolved IP as compliant anyway if it matches the `CompliantIPs` list (`internal/pipeline/pipeline.go`). The list is admin-managed via `db.CompliantIP` rows (`GET/POST /api/admin/compliant-ips`, `DELETE /api/admin/compliant-ips/{id}` — admin-only, handlers in `admin_handlers.go`) and surfaced in the `/admin` UI (`admin.index.tsx`'s `AddCompliantIPDialog` + list). `scanner.go` fetches the current list (`store.ListCompliantIPs`) before every `Trigger`/`TriggerScreenshot` and passes it to the crawler as `--compliant-ips ip1,ip2,...` (also settable directly when running the crawler standalone).

### Domain normalization & watchlists (`internal/urlnorm/`, `internal/db/migrate.go`)

- `urlnorm.Normalize(raw string) (string, error)` canonicalizes any input (`https://Example.com/`, `example.com`, etc.) down to a lowercase bare hostname — strips scheme, userinfo, path, query, fragment, port, trailing dot. This is a separate, new package — **do not** reuse `pipeline.normalizeURL` (`internal/pipeline/pipeline.go`), which only prefixes a scheme for crawling and must keep working exactly as today.
- `URL.URL` is expected to always be normalized by the time it's stored. `postgresStore.CreateURL` normalizes + `FirstOrCreate`s by the normalized value, so the same domain added in different raw formats by different departments always resolves to one shared row — this is what lets departments share scan history for overlapping domains while keeping separate watchlist visibility (via `DepartmentURL`).
- `db.NormalizeAndDedupeURLs(ctx, gormDB)` is a one-time, idempotent, transactional backfill (called from `main.go` on every startup, before `db.NewStore`) that normalizes any pre-existing unnormalized `urls.url` rows and merges post-normalization duplicates onto the lowest-ID row, reassigning `ScanResult.URLID` and `DepartmentURL` links before deleting the duplicate. Existing rows get **no** `DepartmentURL` links from this — they're visible only via the admin "unassigned" view (`GET /api/admin/urls/unassigned`) until a department explicitly adds them.
- `db.BackfillURLValues(ctx, gormDB)` runs immediately after `NormalizeAndDedupeURLs` in `main.go`, and must — that pass is what rewrites `urls.url` and reassigns `ScanResult.URLID`, leaving any row it touches with a stale `scan_results.url_value`. `BackfillURLValues` re-syncs `url_value` to its `urls.url` via `URLID` for every row where they've diverged (a correlated subquery, not Postgres's `UPDATE ... FROM`, so the same statement runs on SQLite too); idempotent, a no-op once everything's in sync. Batched via `id IN (SELECT id ... LIMIT n)`, looping until a batch affects zero rows (capped by an iteration guard) rather than one unbounded `UPDATE` — a first-deploy run rewrites every pre-existing row, and an unbatched statement hitting Postgres's `statement_timeout` would otherwise crash-loop the server on every restart. `main.go`'s call site logs and continues on error instead of `log.Fatalf`, so a slow/partial backfill degrades (repaired further on the next boot) rather than blocking startup.
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
- `CaptureWithAllocator(ctx, allocCtx, rawURL, waitIdle, postIdleSleep) ([]byte, time.Time, error)` — set UA + stealth JS → enable lifecycle events → navigate → wait for `networkIdle` (capped at `waitIdle`) → sleep `postIdleSleep` → full-page screenshot. Returns the **raw, unframed** page bytes plus the capture timestamp — framing is a separate step (see below) so the same raw pixels captured once per unique `(url, resolvedIP)` can be framed once per DNS server without repeating the expensive navigate+idle-wait capture.
- Stealth: Windows Chrome UA, `Accept-Language`, `platform`, hides `navigator.webdriver`, spoofs plugins/languages/`window.chrome`, patches `permissions.query`. `disable-blink-features=AutomationControlled` at allocator level.
- `frame.go`: `screenshot.Frame(chromeCtx, pageBytes, rawURL, capturedAt, isp, dnsAddress string) ([]byte, error)` wraps a raw PNG in a Chrome UI mockup via a `data:text/html;base64,...` URL, stamping a timestamp chip plus a second `"{isp} · {dnsAddress}"` chip (omitted when both are empty — the system-resolver case). `cmd/crawler/main.go`'s `frameScreenshots` calls this once per `pipeline.SiteResult` (i.e. once per DNS server) after `captureResolved`'s raw-pixel dedup and `assignScreenshots`'s copy-to-every-shared-result step, so two DNS servers that resolved to the same IP still each get a separately, correctly labeled framed image instead of one misattributed pair. `screenshot.AllocatorOptions` (no `--host-resolver-rules` needed, since framing never navigates to the target site) backs a single shared Chrome tab reused across all of a sweep's framing calls. Falls back to leaving the result's already-assigned raw screenshot bytes in place if framing errors for that result (logged, not fatal).
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
- gRPC transport is **plaintext by default**; setting `--tls-cert`/`--tls-key`/`--tls-ca` on **both** binaries turns on mutual TLS for both directions (`internal/grpcauth`). All three flags or none — a partial set is a startup error rather than a silent downgrade. Generate the private CA and leaf certs with `scripts/gen-mtls-certs.sh` (output in the gitignored `certs/`); this CA is unrelated to any public HTTPS certificate the dashboard's domain uses, and each leaf must carry both `serverAuth` and `clientAuth` EKU because each binary is both a TLS client and a TLS server. Both `CrawlerControl.StartSweep` and `ComplianceService.Submit` are authenticated by the same shared secret (`--auth-token`/`--crawler-token`); an empty secret disables the check and logs a startup warning rather than silently accepting everything.

### URL loading (`internal/input/`)

- `input.Load(filePath, args)` merges file + CLI args into a deduplicated slice. Bare hostnames are normalized to `https://` by `pipeline.normalizeURL`.

**Module name**: `github.com/afif/dns-tracking` (in `go.mod`, despite the repo directory being `dns-compliance`)

## TODO

- **Exportable compliance report**: per-ISP / per-period PDF or CSV bundling compliance %, time-to-compliance (`GET /api/isps/{isp}/timing`), regressions (domains that flip compliant → violation), and screenshot evidence over time — the artifact that leaves the building for an enforcement action, rather than something only viewable in-app. **Open questions requiring product/User-department sign-off before building:** which page/table hosts the export button (`/results`? the ISP detail page? Overview?), PDF vs CSV vs both, and the exact fields/evidence each export must legally contain.
- **Domain status page**: `domain.$url.tsx` splits into an Overview tab (heatmap, `DnsRecordsPanel`, `DomainInfoPanel` registrar/WHOIS) and a History tab (the paginated scan-by-scan table), but there's still no dedicated "current status at a glance" summary — e.g. latest verdict per DNS server condensed into one card — separate from the two existing tabs.
- ~~**DNS Servers page → bento grid**~~ — done: `dns-servers.tsx` now renders one `.bento-card` per DNS server (not per ISP) in a `.bento-grid`, each card showing that server's `ISPLogoChip`+ISP name, protocol badge, name/address, and edit/delete actions.
## Product & Design

[PRODUCT.md](./PRODUCT.md) defines the target users (regulatory auditors, not developers) and brand personality (neutral, evidence-first, no editorializing on violations). [DESIGN.md](./DESIGN.md) defines the visual system ("The Registry" — achromatic gray scale plus a single ledger-indigo accent reserved for actions/identity, never for compliance status). Read both before making UI changes — the anti-references (generic SaaS dashboards, security-product dark/neon aesthetics) are deliberate constraints, not omissions.

`docs/superpowers/specs/` and `docs/superpowers/plans/` hold dated design-spec/implementation-plan pairs behind individual features (e.g. `2026-06-30-additional-stats-design.md` covers ISP grouping, latency capture, ISP stats/trend, DNS error classification, worst-ISP stat, and newly-violating domains) — useful for archaeology on *why* a feature looks the way it does.

## Security

See [SECURITY.md](./SECURITY.md) for the full security audit report (score: 32/100).

Key issues to address before any non-private deployment:
- SEC-001: ~~No authentication on any endpoint~~ — resolved: session-cookie auth + RBAC (`requireAuth`/`requireAdmin` in `internal/server/router.go`, see "Auth & RBAC" in `internal/server/CLAUDE.md`). Still worth a follow-up audit: gRPC remains unauthenticated by design (trusted crawler↔server link only) and rate-limiting/lockout on `POST /api/auth/login` doesn't exist yet.
- SEC-003: SSRF via `POST /api/screenshot` and `POST /api/urls` — validate URLs against private IP ranges
- SEC-005: Raw DB errors leaked in responses — map errors to safe messages in handlers
