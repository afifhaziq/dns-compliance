# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
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

# Server key flags (all accept env-var fallbacks)
--db-url "host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"
--minio-endpoint localhost:9000   # env: MINIO_ENDPOINT
--minio-access-key minioadmin     # env: MINIO_ACCESS_KEY
--minio-secret-key minioadmin     # env: MINIO_SECRET_KEY
--minio-bucket screenshots        # env: MINIO_BUCKET
--crawler-path ./crawler          # env: CRAWLER_PATH
--seed-dns dns-server.yaml        # seeds dns_servers table on first run if empty
--interval 60                     # scheduled scan interval in minutes (default 60)

# Note: --db-url accepts a PostgreSQL DSN (key=value pairs), NOT a postgresql:// URL

# Sites file format: one URL per line; # lines are comments;
# bare hostnames are accepted — the pipeline prefixes https://;
# duplicates across file + CLI args are silently dropped

# Install / sync dependencies
go mod tidy

# Test all packages (screenshot tests are skipped unless Chrome is available)
go test ./...

# Note: internal/dns/ tests make REAL network calls (8.8.8.8, google.com) — no build tag guard.
# They will fail if the network is unavailable.

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

# Dev overlay adds a local PostgreSQL container:
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

### Server (`cmd/server`, `internal/server/`)

- **HTTP** on `:8080` via [chi](https://github.com/go-chi/chi). REST routes all under `/api`:
  - `GET/POST/DELETE /api/urls` — manage the URL watchlist
  - `GET/POST/DELETE /api/dns-servers` — manage DNS server configs
  - `POST /api/scan`, `GET /api/scan/status` — trigger a DNS-only scan / poll status
  - `GET /api/results`, `GET /api/results/*url` — latest results / per-URL history
  - `POST /api/screenshot` — trigger a single-URL screenshot scan for a specific DNS server
- **gRPC** on `:50051` — receives `ComplianceReport` submissions from the crawler subprocess; uploads any screenshot bytes to MinIO, stores results in PostgreSQL.
- **Scanner** (`scanner.go`) — manages crawler subprocess lifecycle with a mutex-guarded `running` flag. `Trigger` does a DNS-only crawl (`--grpc-addr` set, no `--screenshots`). `TriggerScreenshot` adds `--screenshots` and targets a single URL + DNS server. Both write temp files for the URL list and DNS YAML then call `exec.CommandContext`. The crawler's stdout is wired to the server's stderr so its log output appears in server logs.
- **Scheduler** (`scheduler.go`) — `StartScheduler` ticks every `--interval` minutes (default 60) and calls `scanner.Trigger("scheduled")`.
- Server flags accept env-var fallbacks: `DB_URL`, `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET`, `CRAWLER_PATH`.

### Database (`internal/db/`)

- GORM with PostgreSQL driver (`gorm.io/driver/postgres`). SQLite driver (`glebarez/sqlite`) is also linked but PostgreSQL is the production path.
- Models: `DNSServer`, `URL`, `ScanRun`, `ScanResult`. `ScanResult` foreign-keys to both `ScanRun` and `DNSServer`.
- `db.Store` interface decouples handlers and the gRPC server from the concrete implementation — tests can swap in a fake.
- `db.Seed` populates `dns_servers` on first run from the YAML file passed via `--seed-dns` (skips if table is non-empty).

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
- YAML format for `--dns-servers`:
  ```yaml
  servers:
    - name: Cloudflare DoT
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

## Security

See [SECURITY.md](./SECURITY.md) for the full security audit report (score: 32/100).

Key issues to address before any non-private deployment:
- SEC-001: No authentication on any endpoint — add API key middleware in `internal/server/router.go`
- SEC-003: SSRF via `POST /api/screenshot` and `POST /api/urls` — validate URLs against private IP ranges
- SEC-005: Raw DB errors leaked in responses — map errors to safe messages in handlers
