# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./cmd/...

# Run (sites file or inline URLs)
go run ./cmd/main.go --sites sites.txt [--grpc-addr localhost:50051] [--interval 5]
go run ./cmd/main.go https://example.com https://example2.com

# Test all packages
go test ./...

# Test a single package
go test ./internal/pipeline/...
go test -run TestCompliantSiteSkipsScreenshot ./internal/pipeline/...

# Regenerate protobuf (requires protoc + protoc-gen-go + protoc-gen-go-grpc)
protoc --go_out=. --go-grpc_out=. proto/compliance.proto
```

## Architecture

The tool checks whether websites are accessible — framed as a *compliance* check where a site that resolves DNS and loads is a **violation** (`Compliant=false`) and one that fails DNS is **compliant** (`Compliant=true`). This inverted semantics is intentional.

**Two-stage concurrent pipeline** (`internal/pipeline/pipeline.go`):
1. DNS worker pool (default 20 goroutines) — resolves hostnames; sites that fail DNS are immediately emitted as compliant results and skip stage 2
2. Screenshot worker pool (default 5 goroutines) — only processes sites that resolved DNS; captures full-page PNG via headless Chrome

`pipeline.Config` injects `Resolve` and `Capture` as function values, which makes unit tests use mocks without any real network or browser — see `pipeline_test.go` for the pattern.

**Screenshot output** (`internal/screenshot/`):
- `capture.go`: Launches headless Chrome via `chromedp`, renders at 1920×1080, expands to full page height
- `frame.go`: Wraps the page PNG in a Chrome browser UI mockup by writing a temp HTML file and screenshotting it; if framing fails, falls back to the raw PNG
- Screenshots saved to `screenshots/<hostname>/<timestamp>.png` by `cmd/main.go`

**gRPC output** (`internal/sender/`, `proto/`):
- `proto/compliance.proto` defines `ComplianceService.Submit(ComplianceReport)`; generated Go files are committed in `proto/`
- `sender.Send` submits a `ComplianceReport`; if `--grpc-addr` is omitted, results are only printed to stdout as a tab-aligned table

**Module name**: `github.com/afif/dns-tracking` (declared in `go.mod`, despite the repo directory being `dns-compliance`)
