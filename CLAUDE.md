# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`README.md` is stale — documents a pre-refactor single-binary CLI (`cmd/main.go`, binary `dns-compliance`) that no longer exists. Ignore it; this file is authoritative.

Frontend, server/auth, and database details live in `web/CLAUDE.md`, `internal/server/CLAUDE.md`, `internal/db/CLAUDE.md` — they load automatically under those directories. The legal citation catalog and notifications subsystems are covered by the `legal-citation-catalog` and `notifications` skills instead, since they span multiple directories.

## Commands

Both binaries are self-documenting — `go run ./cmd/crawler/ --help` / `go run ./cmd/server/ --help` list every flag with its default and description. Only cross-file gotchas are called out below.

```bash
./dev.sh    # full-stack dev: Postgres via Docker, server :8080, Vite :5173; Ctrl+C shuts all down

go build -o server  ./cmd/server/
go build -o crawler ./cmd/crawler/

./dns_benchmark.sh [site-list.txt] [dns-server.yaml]    # baseline dig/nslookup/curl → benchmark_results/
./scripts/gen-mtls-certs.sh [extra-hostname-or-IP ...]  # private CA + leaf cert per binary → gitignored certs/

go run ./cmd/crawler/ --sites sites.txt
go run ./cmd/crawler/ "https://example.com" "https://example2.com"  # quote URLs with ? or & in zsh
go run ./cmd/server/ --http-addr :8080 --grpc-addr :50051  # requires Postgres + MinIO, see Docker below

go mod tidy
go test ./...                                  # internal/dns/ hits real network (8.8.8.8), fails offline
go test -run TestName ./internal/pipeline/...  # internal/screenshot/ needs Chrome + INTEGRATION=1

protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/compliance.proto
```

Sites file: one URL per line, `#` comments, bare hostnames get `https://` prefixed, duplicates across file + CLI args silently dropped.

Flag gotchas not covered by `--help`:
- `--db-url` (server): a PostgreSQL DSN (`key=value` pairs), not a `postgresql://` URL.
- `--compliant-ips` (crawler): the server passes this automatically from the admin-managed list (see Domain semantics below) — set by hand only when running the crawler standalone.
- `--listen-addr` (crawler): runs a persistent gRPC control service; the dashboard triggers sweeps via `CrawlerControl.StartSweep` rather than exec'ing the binary.
- `--auth-token` (crawler) / `--crawler-token` (server): shared secret, must match on both sides.
- `--tls-cert`/`--tls-key`/`--tls-ca` (both binaries): all three or none — see gRPC section below.
- `--redis-addr` (server): notification task queue backend — see the `notifications` skill.
- `--bootstrap-admin-username`/`--bootstrap-admin-password` (server): creates the admin only if `users` is empty; both required together on first run, or no one can log in. Local dev DB (`docker-compose.dev.yml`) login: `admin` / `aaAA1234`.
- `--subfinder-path` (server): empty disables subdomain enumeration entirely.

## Docker

```bash
docker compose up   # supply DB_URL separately, or use the dev overlay below

docker compose -f docker-compose.yml -f docker-compose.dev.yml up
# dev overlay: adds a local Postgres container (port 5432 published) and presets COOKIE_SECURE=false
```

Dockerfile is multi-stage: builder (`golang:1.26`) produces both binaries plus a standalone `go install` of subfinder (not a go.mod dependency — only ever shelled out to, see `internal/subfinder/`); runtime (`debian:bookworm-slim`) bundles Chromium and ships subfinder at `/app/subfinder` (`SUBFINDER_PATH`). ENTRYPOINT is `/app/server`; `docker-compose.yml` runs a second container from the same image with `entrypoint: ["/app/crawler"]` as the `crawler` service — the two talk over gRPC (`CrawlerControl.StartSweep`, `ComplianceService.Submit`), not exec. Compose also runs a `redis` service (AOF persistence) backing the notification queue.

## Architecture

Two-binary system:
- **`cmd/crawler`** — CLI that runs DNS checks (and optionally screenshots), reports via gRPC or stdout.
- **`cmd/server`** — long-running backend: REST API + gRPC receiver, persists to PostgreSQL, stores screenshots in MinIO, triggers crawler sweeps over gRPC.

### Domain semantics

Checks ISP takedown compliance. A site that **resolves DNS** is a **violation** (`Compliant=false`); one that **fails DNS** is **compliant** (`Compliant=true`) — an intentional inversion.

**Compliant IPs exception**: some ISPs redirect blocked domains to a block-page IP instead of failing DNS outright (e.g. MCMC's redirect IP in Malaysia). `internal/pipeline.checkDNS` treats a resolved IP as compliant anyway if it matches `CompliantIPs` (`internal/pipeline/pipeline.go`). Admin-managed via `db.CompliantIP` rows (`GET/POST /api/admin/compliant-ips`, `DELETE /api/admin/compliant-ips/{id}`, handlers in `admin_handlers.go`) and the `/admin` UI (`AddCompliantIPDialog`). `scanner.go` fetches the current list before every `Trigger`/`TriggerScreenshot` and passes it to the crawler as `--compliant-ips`.

### Domain normalization & watchlists (`internal/urlnorm/`, `internal/db/migrate.go`)

- `urlnorm.Normalize(raw string) (string, error)` canonicalizes any input down to a lowercase bare hostname. Separate from `pipeline.normalizeURL` (`internal/pipeline/pipeline.go`), which only prefixes a scheme for crawling and must keep working exactly as today — don't reuse one for the other.
- `URL.URL` is expected to always be normalized in storage. `postgresStore.CreateURL` normalizes + `FirstOrCreate`s by the normalized value, so the same domain added in different raw formats by different departments resolves to one shared row — this is what lets departments share scan history for overlapping domains while keeping separate watchlist visibility (`DepartmentURL`).
- `db.NormalizeAndDedupeURLs(ctx, gormDB)` — one-time, idempotent, transactional backfill (called from `main.go` on every startup, before `db.NewStore`) that normalizes pre-existing unnormalized `urls.url` rows and merges post-normalization duplicates onto the lowest-ID row, reassigning `ScanResult.URLID`/`DepartmentURL` links before deleting the duplicate. Existing rows get **no** `DepartmentURL` links from this — visible only via `GET /api/admin/urls/unassigned` until a department explicitly adds them.
- `db.BackfillURLValues(ctx, gormDB)` runs immediately after, and must — `NormalizeAndDedupeURLs` is what rewrites `urls.url` and reassigns `ScanResult.URLID`, leaving any touched row with a stale `scan_results.url_value`. This re-syncs it via a correlated subquery (not Postgres's `UPDATE ... FROM`, so it also runs on SQLite) wherever they've diverged; idempotent, no-op once in sync. Batched (`id IN (SELECT id ... LIMIT n)`, looping until a batch affects zero rows) rather than one unbounded `UPDATE` — a first-deploy run rewrites every pre-existing row, and an unbatched statement hitting Postgres's `statement_timeout` would crash-loop the server on every restart. Errors are logged and non-fatal so a slow/partial backfill degrades (repaired further next boot) instead of blocking startup.
- `ListURLs`/`CreateURL`/`DeleteURL` on `db.Store` are the global/admin scope, unchanged since before RBAC existed. `ListDepartmentURLs`, `AddURLToWatchlist`, `RemoveURLFromWatchlist`, `ListWatchedURLs`, `ListUnassignedURLs`, `URLOwnedByDepartment` are the department-scoped layer on top.

### Storage (`internal/storage/`)

`storage.Storage` interface, single `Upload(ctx, []byte) (string, error)` method. `minioStorage` uploads PNGs as `<uuid>.png`, returns a public HTTP URL; the `minio-init` container sets the bucket policy to public.

### Crawler pipeline (`internal/pipeline/`, `cmd/crawler/`)

Two-stage concurrent pipeline: a DNS worker pool resolves hostnames (sites that fail DNS emit as compliant and skip stage 2, using `cfg.DNSTimeout`), then a screenshot worker pool captures full-page PNGs for resolved sites (`cfg.ScreenshotTimeout`). `pipeline.Config` injects `Resolve`, `Capture`, `OnResult` as function values — tests mock `Resolve`/`Capture`. `Capture` is a no-op unless `--screenshots` is set. With multiple DNS servers, the crawler runs one DNS-only `pipeline.Run` per server, then `captureResolved` once at the end to dedupe `(url, resolvedIP)` screenshot jobs.

Go concurrency model: goroutines communicate via channels; `sync.WaitGroup` is a barrier (like `asyncio.gather`); `context.Context` carries deadlines and cancellation.

### Screenshot capture (`internal/screenshot/`, `cmd/crawler/main.go`)

- `AllocatorOptionsWithHostRules(rules)` sets `--host-resolver-rules` so Chrome connects to the pre-resolved IP instead of re-resolving.
- `CaptureWithAllocator(ctx, allocCtx, rawURL, waitIdle, postIdleSleep) ([]byte, time.Time, error)` — UA/stealth JS → lifecycle events → navigate → wait `networkIdle` (capped at `waitIdle`) → sleep `postIdleSleep` → full-page screenshot. Returns raw unframed page bytes + capture timestamp; framing is a separate step so raw pixels captured once per unique `(url, resolvedIP)` can be framed once per DNS server without repeating the expensive capture.
- Stealth: Windows Chrome UA, `Accept-Language`, `platform`, hides `navigator.webdriver`, spoofs plugins/languages/`window.chrome`, patches `permissions.query`; `disable-blink-features=AutomationControlled` at allocator level.
- `frame.go`: `screenshot.Frame(chromeCtx, pageBytes, rawURL, capturedAt, isp, dnsAddress string) ([]byte, error)` wraps a raw PNG in a Chrome UI mockup, stamping a timestamp chip + `"{isp} · {dnsAddress}"` chip (omitted when both empty — the system-resolver case). `frameScreenshots` in `cmd/crawler/main.go` calls this once per `pipeline.SiteResult` after dedup + the copy-to-every-shared-result step, so two DNS servers resolving to the same IP still each get a correctly labeled framed image. `screenshot.AllocatorOptions` (no host-resolver-rules needed) backs one shared Chrome tab reused across a sweep's framing calls. Falls back to the already-assigned raw bytes if framing errors for that result (logged, not fatal).
- Local save path: `<dns_label>/<hostname>/<timestamp>-<urlhash>.png` (spaces in DNS name → underscores; no server → `system`). Server-mode screenshots go to MinIO instead.
- **Screenshot batching** (`groupJobs`, `cmd/crawler/main.go`): `--host-resolver-rules` can only map one IP per hostname per allocator, so when DNS servers disagree on a hostname's IP, `groupJobs` splits those jobs across separate Chrome allocators to avoid conflicts.

### DNS resolution (`internal/dns/`, `internal/dnsconfig/`)

`dns.Resolve` (system resolver), `dns.NewResolver(addr)` (plain UDP), `dns.NewDoTResolver(addr)` (DoT), `dns.NewDoHResolver(endpoint)` (DoH, RFC 8484 GET wire format).

`--dns-servers` YAML (`isp` required, `name` defaults to address):
```yaml
servers:
  - isp: Cloudflare
    name: Cloudflare DoT
    address: 1.1.1.1:853
    protocol: dot   # udp | dot | doh
```

DNS checks hostname only, not the full URL path — `dig @<server> www.example.com` is the correct manual equivalent; passing the full URL returns NXDOMAIN.

### gRPC (`internal/sender/`, `internal/server/grpc.go`, `proto/`)

`proto/compliance.proto` defines `ComplianceService.Submit(ComplianceReport)`; generated Go files are committed. Crawler: `sender.Send` submits a report (and always prints to stdout too). Server: `grpcServer.Submit` looks up the active `ScanRun`, matches DNS server names to IDs, calls `store.InsertResult` for each entry, uploads any screenshot bytes to MinIO.

Plaintext by default; setting `--tls-cert`/`--tls-key`/`--tls-ca` on **both** binaries turns on mutual TLS (`internal/grpcauth`) — all three flags or none, a partial set is a startup error rather than a silent downgrade. Generate the CA + certs with `scripts/gen-mtls-certs.sh` (unrelated to any public HTTPS cert the dashboard's domain uses); each leaf needs both `serverAuth` and `clientAuth` EKU since each binary is both a TLS client and server. Both `CrawlerControl.StartSweep` and `ComplianceService.Submit` are authenticated by the same shared secret (`--auth-token`/`--crawler-token`) — an empty secret disables the check and logs a startup warning rather than silently accepting everything.

### URL loading (`internal/input/`)

`input.Load(filePath, args)` merges file + CLI args into a deduplicated slice. Bare hostnames are normalized to `https://` by `pipeline.normalizeURL`.

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
