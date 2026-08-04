# Screenshot ISP/DNS Frame Design

**Date:** 2026-08-04
**Branch:** main (design phase — not yet branched)

## Overview

Burn the ISP name and the DNS server's own address into the evidence-frame chip alongside the existing capture timestamp, so an exported screenshot is self-contained evidence of which ISP/resolver was used, not just when it was taken.

## Background

`internal/screenshot/frame.go`'s `addBrowserFrame` already stamps a `{{TIMESTAMP}}` clock-chip into the toolbar of the Chrome-mockup wrapper. It's called once, inside `CaptureWithAllocator` (`internal/screenshot/capture.go`), immediately after the raw page screenshot — i.e. once per unique `(url, resolvedIP)` capture.

That dedup key is the catch: `cmd/crawler/main.go`'s `captureResolved`/`groupJobs` intentionally dedupe screenshot jobs by `(url, resolvedIP)`, not `(url, dnsServer)` — when multiple DNS servers resolve a hostname to the *same* IP, they share one captured image (see `shotKey`/`groupJobs`). A single ISP+DNS-address baked in at capture time would misattribute the evidence for every server but the first whenever servers agree on an IP (the common case, per the existing doc comment on `groupJobs`).

## Decision

Split capture from framing:
- Raw pixel capture stays deduped per `(url, resolvedIP)` exactly as today (no perf regression) — `CaptureWithAllocator` returns **unframed** page bytes; the internal `addBrowserFrame` call is removed from it.
- Framing becomes a separate, cheap step (it's just rendering a static local HTML wrapper via chromedp, no navigation to the target site or idle-wait — safe to repeat per DNS server) done **once per DNS server** that maps to a given `(url, resolvedIP)` result, reusing the shared raw pixels.

`addBrowserFrame` gains two new params and becomes exported (e.g. `screenshot.Frame(chromeCtx, pageBytes, rawURL, capturedAt, isp, dnsAddress string) ([]byte, error)`), rendering a second chip next to the existing timestamp chip (e.g. `ISP · 1.1.1.1`). Empty `isp`/`dnsAddress` (system resolver, no DNS server context) renders nothing extra — same "no server → system" fallback already documented for screenshot file paths in CLAUDE.md.

## Implementation notes

- Audit every `CaptureWithAllocator` call site (`cmd/crawler/main.go` has at least 3 — `captureResolved`'s per-job goroutine plus the ad-hoc single-URL fallback paths around the `MAP`/host-resolver-rules block) and move the `Frame(...)` call to wherever that raw result gets attached to a specific per-DNS-server report entry, passing that entry's `DNSServer.ISP`/`DNSServer.Address`.
- The `POST /api/screenshot` path (single URL + `dns_server_ids`) already has DNS server context per resolved (url, IP) pair — same treatment.
- `internal/screenshot/frame_test.go` (if present) and any capture tests need updating for the new signature; add/extend a test asserting the ISP/address chip renders only when non-empty.

## Testing

- Unit test around `Frame`/`addBrowserFrame` HTML output for the new chip (string-contains assertion, matching existing test style for the timestamp chip if one exists).
- Manual `dev.sh` verification: run a screenshot scan against 2+ DNS servers that resolve to different IPs, confirm each captured image shows its own correct ISP+DNS address; run against 2 servers that resolve to the *same* IP, confirm both still get correctly (separately) labeled images rather than one misattributed pair.

## Out of Scope

- Any change to the timestamp chip itself.
- Any change to `captureResolved`'s dedup strategy for raw pixels — only the framing step moves.
