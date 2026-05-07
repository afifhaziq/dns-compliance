# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./cmd/...

# Run (sites file or inline URLs — always quote URLs containing ? or & in zsh)
go run ./cmd/main.go --sites sites.txt
go run ./cmd/main.go "https://example.com" "https://example2.com"

# Key flags
--dns-timeout 5          # seconds for DNS resolution per site (default 5)
--screenshot-timeout 30  # seconds for navigation + idle wait + capture (default 30)
--wait-idle 5            # max seconds to wait for networkIdle event (default 5)
--post-idle-sleep 2000   # milliseconds to sleep after idle before capture (default 2000)
--screenshot-workers 5   # concurrent Chrome tabs (default 5)
--dns-workers 20         # concurrent DNS lookups (default 20)
--interval 10            # repeat sweep every N minutes; 0 = run once (default 0)
--grpc-addr localhost:50051  # send report via gRPC; omit to print table to stdout

# Test all packages
go test ./...

# Test a single package / single test
go test ./internal/pipeline/...
go test -run TestCompliantSiteSkipsScreenshot ./internal/pipeline/...

# Regenerate protobuf (requires protoc + protoc-gen-go + protoc-gen-go-grpc)
protoc --go_out=. --go-grpc_out=. proto/compliance.proto
```

## Architecture

The tool checks whether websites are accessible — framed as a *compliance* check where a site that resolves DNS is a **violation** (`Compliant=false`) and one that fails DNS is **compliant** (`Compliant=true`). This inverted semantics is intentional: the goal is to verify ISP takedown compliance.

**Two-stage concurrent pipeline** (`internal/pipeline/pipeline.go`):
1. DNS worker pool — resolves hostnames; sites that fail DNS are immediately emitted as compliant and skip stage 2. Uses `cfg.DNSTimeout` per site.
2. Screenshot worker pool — only processes sites that resolved DNS; captures full-page PNG via headless Chrome. Uses `cfg.ScreenshotTimeout` per site, independently of DNS time.

`pipeline.Config` injects `Resolve`, `Capture`, and `OnResult` as function values. `OnResult` is called as each result arrives, used in `cmd/main.go` for real-time `[n/total]` progress logging. Tests use mock functions for `Resolve` and `Capture` — see `pipeline_test.go`.

**Screenshot pipeline** (`internal/screenshot/`):
- `AllocatorOptions` is a package-level exported var; `cmd/main.go` creates **one shared Chrome process** at startup via `chromedp.NewExecAllocator(ctx, screenshot.AllocatorOptions...)`. Each URL gets a new tab (`chromedp.NewContext(allocCtx)`), not a new process.
- `CaptureWithAllocator(ctx, allocCtx, rawURL, waitIdle, postIdleSleep)` — the main capture function. Sequence: set UA + stealth JS → enable lifecycle events → navigate → wait for `networkIdle` or `waitIdle` cap → sleep `postIdleSleep` → full-page screenshot → browser frame.
- Stealth measures applied per tab: realistic Windows Chrome UA, `Accept-Language`, `platform`, hides `navigator.webdriver`, spoofs `navigator.plugins`/`languages`/`window.chrome`, patches `permissions.query`. `disable-blink-features=AutomationControlled` is set at the allocator level.
- `frame.go`: wraps the PNG in a Chrome UI mockup via a `data:text/html;base64,...` URL (no temp files). Falls back to raw PNG if framing fails.
- Screenshots saved to `screenshots/<hostname>/<timestamp>-<urlhash>.png` — the 8-char FNV hash disambiguates multiple URLs sharing the same hostname and timestamp.

**gRPC output** (`internal/sender/`, `proto/`):
- `proto/compliance.proto` defines `ComplianceService.Submit(ComplianceReport)`; generated Go files are committed in `proto/`.
- `sender.Send` submits a report; if `--grpc-addr` is omitted, results are printed as a tab-aligned table.

**Module name**: `github.com/afif/dns-tracking` (declared in `go.mod`, despite the repo directory being `dns-compliance`)
