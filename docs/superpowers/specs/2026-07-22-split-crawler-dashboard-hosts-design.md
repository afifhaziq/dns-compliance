# Split Crawler and Dashboard onto Separate Hosts — Design

**Date:** 2026-07-22
**Branch:** main (design phase — not yet branched)

## Overview

Today `cmd/server` and `cmd/crawler` must run on the same machine: `Scanner.execCrawler` (`internal/server/scanner.go`) spawns the crawler as a **local subprocess** via `exec.Command`, and feeds it the site list and DNS server config as **local temp files** (`writeTempLines`/`writeDNSYAML`) passed as `--sites`/`--dns-servers` CLI args. Neither mechanism works across hosts.

Results already flow back over the network today — the crawler dials out to the dashboard's `ComplianceService.Submit` gRPC endpoint (`internal/server/grpc.go`) to report results, including screenshot bytes inline in `SiteResult.screenshot`. That direction is already host-agnostic.

This design adds the missing direction: a new gRPC service, hosted by the crawler, that the dashboard calls to *trigger* a sweep — replacing `exec.Command` entirely. The crawler becomes a persistent process (`--listen-addr`) instead of a one-shot CLI invocation per scan.

## Architecture

Both processes end up playing client *and* server roles, to two different services:

- **Dashboard hosts `ComplianceService.Submit`** (unchanged) — crawler is the client, reporting results.
- **Crawler hosts the new `CrawlerControl.StartSweep`** — dashboard's `Scanner` is the client, triggering sweeps.

`StartSweep` is a **blocking unary RPC**: the crawler runs the full sweep and only returns once it's done. This mirrors today's `cmd.Run()`-blocks-until-process-exit behavior exactly, so `Scanner.execCrawler`'s post-completion bookkeeping (`store.CompleteScanRun`, broadcaster publish) needs no structural change — just swap what it's blocking on. No new completion callback, polling, or state machine.

## Proto Changes

Appended to the existing `proto/compliance.proto` (one file, no new build target):

```proto
message DNSServerConfig {
  string isp      = 1;
  string name     = 2;
  string address  = 3;
  string protocol = 4;
}

message SweepRequest {
  repeated string          urls          = 1;
  repeated DNSServerConfig dns_servers   = 2;
  repeated string          compliant_ips = 3;
  bool                     screenshots   = 4;
}

message SweepAck {
  bool   accepted = 1;
  string error    = 2;
}

service CrawlerControl {
  rpc StartSweep(SweepRequest) returns (SweepAck);
}
```

This also **removes** `writeTempLines`/`writeDNSYAML` and their temp-file plumbing from `scanner.go` — `SweepRequest` is built as an in-memory struct directly from `db.URL`/`db.DNSServer` rows instead, a net reduction in code.

## Crawler Changes (`cmd/crawler/main.go`)

- New flags: `--listen-addr` (e.g. `:50052`) and `--auth-token`.
- When `--listen-addr` is set, `main()` starts a gRPC server registering `CrawlerControl` instead of running one sweep and exiting. `--sites`/`--interval`/one-shot CLI mode are **unchanged and still fully supported** — orthogonal to listen mode, not replaced, so standalone CLI usage (`go run ./cmd/crawler --sites sites.txt`) keeps working exactly as documented today.
- The existing `runSweep()` is reused unmodified by both the CLI path and the new RPC handler. The inline "build `[]serverEntry` from parsed DNS config" loop currently in `main()` is extracted into a small shared helper (`buildServerEntries([]dnsconfig.Server) []serverEntry`) so the RPC handler can call it with servers converted from `SweepRequest.dns_servers` instead of a parsed YAML file.
- The RPC handler builds `pipeline.Config.CompliantIPs` from `SweepRequest.compliant_ips` and `takeScreenshots` from `SweepRequest.screenshots`, then calls `runSweep(...)` using the crawler's own already-established `--grpc-addr` client connection (dialed once at startup, kept alive across sweeps — no longer redialed per invocation).
- A mutex + `running bool` on the server struct guards against overlapping sweeps, returning `SweepAck{Accepted: false, Error: "sweep in progress"}` if busy. This is defense-in-depth: the dashboard's own `Scanner.running` already serializes triggers today, but the crawler is now a shared network service and shouldn't silently corrupt itself (concurrent Chrome `--host-resolver-rules` allocators) if that invariant is ever violated.

## Auth

A shared static token, sent as gRPC metadata (`x-auth-token`) by the dashboard's outgoing client call, checked by a unary server interceptor on the crawler using `subtle.ConstantTimeCompare` (not `==` — this is a real credential comparison). Rejects with `codes.Unauthenticated` on mismatch or missing token.

This matches the project's existing "trusted network, no transport TLS" posture (gRPC already uses `insecure.NewCredentials()` throughout) — the token gates *who may trigger a sweep*, at the same trust tier as the already-unauthenticated `Submit` RPC, not a full mTLS setup this project has avoided everywhere else.

## Dashboard Changes (`internal/server/scanner.go`, `cmd/server/main.go`)

- `Scanner` takes a small `crawlerClient` interface instead of a `crawlerPath` string:
  ```go
  type crawlerClient interface {
      StartSweep(ctx context.Context, req *pb.SweepRequest) (*pb.SweepAck, error)
  }
  ```
  This also fixes `scanner_test.go`, which currently fakes the crawler by writing a shell script to disk and `exec`-ing it — with the interface, tests inject a fake `crawlerClient` directly.
- `execCrawler` is replaced by a method that builds a `pb.SweepRequest` in memory from the already-loaded `db.URL`/`db.DNSServer`/`db.CompliantIP` rows and calls `crawlerClient.StartSweep`. The outgoing call attaches the shared token via `metadata.AppendToOutgoingContext` and uses `context.WithoutCancel(ctx)` (as today) with no additional deadline — sweeps can legitimately run for minutes.
- New server flags/env vars: `--crawler-addr`/`CRAWLER_ADDR` (dial target, e.g. `localhost:50052`) and `--crawler-token`/`CRAWLER_TOKEN`, replacing `--crawler-path`/`CRAWLER_PATH`.

## Local Dev (`dev.sh`)

Builds the crawler binary as today, then launches it as a second background process instead of leaving it for the server to exec on demand:

```bash
./crawler --listen-addr :50052 --grpc-addr :50051 --auth-token dev-secret &
CRAWLER_PID=$!
```

Tracked and killed in `cleanup()` the same way `SERVER_PID`/`VITE_PID` already are. The server is launched with `--crawler-addr localhost:50052 --crawler-token dev-secret` instead of `--crawler-path ./crawler`.

## Docker (`docker-compose.yml`)

New `crawler` service, reusing the existing built image (which already contains both binaries — see `Dockerfile`) with an `entrypoint` override (the image's default `ENTRYPOINT` is `/app/server`):

```yaml
crawler:
  build: .
  entrypoint: ["/app/crawler"]
  command: ["--listen-addr", ":50052", "--grpc-addr", "server:50051", "--auth-token", "${CRAWLER_TOKEN}"]
```

`server` service gains `--crawler-addr crawler:50052 --crawler-token ${CRAWLER_TOKEN}` (env `CRAWLER_ADDR`/`CRAWLER_TOKEN`). Both services read `CRAWLER_TOKEN` from the environment. Splitting to a genuinely separate host at that point is just pointing `CRAWLER_ADDR` at a different address — no further code change, which is the actual goal of this work.

## Testing

- Rewrite `internal/server/scanner_test.go` against the new fake `crawlerClient` interface — same four existing cases (targeted URLs, trigger-and-complete, initial-progress-publish-before-completion, reject-concurrent-run), no more temp shell scripts.
- Add one crawler-side test for the auth interceptor: reject on wrong/missing token, accept on correct token — the one required check for the new branching logic.

## Docs

Update `CLAUDE.md`: the Scanner description in Architecture, the Commands section's crawler/server flag tables, and Docker section. Replace TODO item 3 with a short note that it's done (or remove it entirely), once implemented.

## Out of Scope

- mTLS (explicitly rejected in favor of the shared-token approach — see Auth).
- Removing the crawler's own `--interval` standalone-ticker mode — kept, orthogonal to `--listen-addr`.
- Any change to how screenshot bytes are transmitted (`SiteResult.screenshot` inline in `Submit`) — already host-agnostic, untouched.
- Actually deploying to two physically separate hosts — this design makes it *possible* (`docker-compose` proves it works across containers/network), not a production deployment change.
