# Encrypt the gRPC Links with mTLS, and Authenticate Submit — Design

**Date:** 2026-07-27
**Branch:** main (design phase — not yet branched)

## Overview

Both gRPC links between `cmd/server` and `cmd/crawler` run in plaintext today, via `insecure.NewCredentials()`. That was a deliberate, documented tradeoff back when the two processes shared a host — see `SECURITY.md` (SEC-001) and the "gRPC transport uses no TLS; both crawler and server must be on a trusted network" note in `CLAUDE.md`. Since `2026-07-22-split-crawler-dashboard-hosts-design.md` made a genuine split-host deployment possible, that link can now cross a real network, where plaintext exposes both scan results and the shared `--auth-token` secret to anyone on the path.

Two gaps get closed here:

1. **No encryption** on either link.
2. **No authentication at all on `ComplianceService.Submit`** (crawler → server). `CrawlerControl.StartSweep` (server → crawler) already has a shared-token check; Submit has nothing, so anyone who can reach `:50051` can inject arbitrary scan results.

Both are addressed **opt-in**: with no new flags set, behavior is byte-for-byte identical to today, so `dev.sh` and every existing deployment keep working untouched.

## Architecture

Each binary plays **both** TLS roles, which drives most of the design:

| | acts as TLS server | acts as TLS client |
|---|---|---|
| `cmd/server` | Submit listener (`main.go:129`) | dials crawler control (`main.go:117`) |
| `cmd/crawler` | control listener (`control.go:123`) | dials server to Submit (`control.go:105`, `main.go:80`) |

Five call sites total, all currently `insecure.NewCredentials()` or a bare `grpc.NewServer()`.

Because each binary is both client and server, **each leaf certificate must carry both `serverAuth` and `clientAuth` Extended Key Usage**. A cert with only `serverAuth` — the default in most copy-pasted openssl recipes — fails the handshake with an opaque `bad certificate` error.

### New package: `internal/grpcauth`

`cmd/crawler` cannot import `internal/server` (that would pull in db, storage, and minio for the sake of one constant), so the shared pieces move to a small dedicated package. It replaces the currently hand-synced duplication between `cmd/crawler/control.go:25` (`authMetadataKey`) and `internal/server/scanner.go:20` (`crawlerAuthMetadataKey`, whose comment explicitly says it must match the other by hand).

Three functions, no config structs:

```go
// AppendToken attaches the shared secret as outgoing metadata. Client side.
func AppendToken(ctx context.Context, token string) context.Context

// UnaryInterceptor returns nil when token is empty — callers must treat a nil
// interceptor as "no auth configured" and log a warning. See "Empty token" below.
func UnaryInterceptor(token string) grpc.UnaryServerInterceptor

// Creds builds one TransportCredentials usable for BOTH dialing and listening.
// Returns enabled=false when all three paths are empty (today's plaintext path);
// errors when only some are set, so a half-configured deployment fails at
// startup instead of silently downgrading to plaintext.
func Creds(certFile, keyFile, caFile string) (creds credentials.TransportCredentials, enabled bool, err error)
```

`Creds` builds a single `*tls.Config` with all four of these set:

- `Certificates` — this binary's own leaf cert + key
- `RootCAs` — verify the peer when acting as a client
- `ClientCAs` — verify the peer when acting as a server
- `ClientAuth: tls.RequireAndVerifyClientCert` — **the field that makes it mutual**

`crypto/tls` ignores whichever fields are irrelevant to the role it's playing, so one config genuinely serves both. The risk is `ClientAuth`: omit it and the server accepts *any* client, with no error and no log line — exactly the failure mTLS exists to prevent. This is asserted explicitly in tests rather than assumed.

## Flags

Three new flags per binary, with env fallbacks matching the existing `--crawler-token`/`CRAWLER_TOKEN` convention:

| Flag | Env | Meaning |
|---|---|---|
| `--tls-cert` | `TLS_CERT` | PEM path to this binary's leaf certificate |
| `--tls-key` | `TLS_KEY` | PEM path to its private key |
| `--tls-ca` | `TLS_CA` | PEM path to the CA that signed both binaries' certs |

All three empty → plaintext, exactly as today. All three set → mTLS on every gRPC path in that binary. Any other combination → startup error.

### `--auth-token` semantics change

The crawler's flag help currently reads *"required on incoming StartSweep RPCs when `--listen-addr` is set."* Once Submit is authenticated, the token is also needed by **one-shot CLI mode** (`crawler --sites sites.txt --grpc-addr ...`), because `runSweep` — shared by both modes (`control.go:81` and `main.go:107/114/118`) — is what calls `sender.Send`. The flag help and the `CLAUDE.md` crawler-flags section both need updating. This is a mild breaking change for anyone running the crawler standalone against a server that has a token configured.

## Submit Authentication

Reuses the existing shared secret (crawler `--auth-token` / server `--crawler-token`, already required to match) rather than introducing a second one.

- `sender.Send` gains a `token string` parameter and calls `grpcauth.AppendToken`.
- Both `Send` call sites (`cmd/crawler/main.go:202` and `:249`) live inside `runSweep`, which currently has no access to the token, so `runSweep` gains a `token string` parameter too. It is threaded from its two modes:
  - **One-shot:** `main.go` passes `*authToken` at all three `runSweep` call sites (`:107`, `:114`, `:118`).
  - **Listen mode:** `controlServer` gains a `token` field (set via a new `newControlServer` parameter) and passes it at `control.go:81`. `runListenMode` already has `authToken` in scope for the interceptor, so nothing new needs to be plumbed into it.
- The server's Submit listener (`cmd/server/main.go:129`) gets `grpcauth.UnaryInterceptor(*crawlerToken)`.

### Empty token

`subtle.ConstantTimeCompare("", "")` returns 1, so an empty configured token accepts *every* request while appearing to enforce auth. The default for both `--auth-token` and `CRAWLER_TOKEN` is empty, and `docker-compose.yml` interpolates `${CRAWLER_TOKEN}`, which is empty when the env var is unset — so this is a realistic deployment, not a hypothetical.

`UnaryInterceptor` therefore returns `nil` for an empty token, and each call site skips installing it and logs a loud startup warning that the link is unauthenticated. This preserves the opt-in rollout (nothing breaks) while removing the false sense of security.

This same hole exists **today** on StartSweep (`cmd/crawler/control.go:123` installs `authInterceptor("")` unconditionally). Routing both directions through the shared package fixes both at once.

## Certificate Generation

`scripts/gen-mtls-certs.sh`, openssl-based, writes to a gitignored `certs/`:

- One self-signed CA (`ca.crt` / `ca.key`)
- One leaf per binary (`server.crt`/`.key`, `crawler.crt`/`.key`), each signed by that CA and each carrying **both `serverAuth` and `clientAuth`** EKU

This is a **private CA**, unrelated to any public domain certificate the dashboard's HTTPS endpoint may later use. A public CA cert proves identity to browsers and authenticates one direction; this one is trusted only by these two binaries, authenticates both directions, and must work for peers that may have no public hostname at all. The two can coexist without interacting.

### SANs

The client verifies the peer's hostname against the certificate's SANs, so the generated certs must cover every address actually dialed:

| Dialer | Address | Source |
|---|---|---|
| server → crawler | `localhost:50052` | `dev.sh:54` |
| server → crawler | `crawler:50052` | `docker-compose.yml` |
| crawler → server | `server:50051` | `docker-compose.yml` |
| crawler → server | `:50051` | `dev.sh:46` — **broken under TLS** |

Default SAN set: `localhost`, `127.0.0.1`, `server`, `crawler`. The script accepts extra hostnames/IPs as arguments for real split-host deployments (an IP peer needs an IP SAN).

`dev.sh:46` passes a bare `--grpc-addr :50051`, which gives `grpc.NewClient` an empty ServerName and therefore nothing to verify against any SAN. It is harmless today (TLS is off) but makes "turn TLS on in dev" fail confusingly on the first attempt, so it is corrected to `localhost:50051` as part of this work.

Validity is 10 years, because rotation is not automated. This is a deliberate shortcut for a two-process private link, marked with a `ponytail:` comment naming the upgrade path rather than left as an unexamined default.

## Error Handling

- **Partial TLS config** (1–2 of the 3 flags) → `log.Fatal` at startup. Fails loudly rather than silently serving plaintext.
- **Unreadable/malformed cert, key, or CA** → `log.Fatal` at startup.
- **Handshake failure at runtime** (wrong CA, expired cert, missing `clientAuth` EKU, hostname/SAN mismatch) → surfaces as a normal gRPC dial/RPC error on the existing paths. `Scanner.Trigger` already handles a failed `StartSweep` and `runSweep` already logs a failed `Send`; neither needs new plumbing.
- **Missing/invalid token** → `codes.Unauthenticated`, unchanged from the existing interceptor.

## Testing

New `internal/grpcauth` tests, generating a throwaway CA and leaf certs in-process with `crypto/x509` rather than shelling out to the script:

- `AppendToken` / `UnaryInterceptor` round-trip: matching token passes, wrong token gets `Unauthenticated`, missing metadata gets `Unauthenticated`.
- `UnaryInterceptor("")` returns nil.
- `Creds` flag combinations: all-empty → `enabled=false`; partial → error; all-set → working credentials.
- **Mutual verification over a real TCP listener** (`net.Listen("tcp", "127.0.0.1:0")`, not `bufconn`): a client presenting a cert from a *different* CA is rejected. This is the test that catches a missing `ClientAuth` field, and it needs a real listener because the existing `bufconn` harness bypasses the transport.
- A leaf cert lacking `clientAuth` EKU is rejected when dialing.

Existing `bufconn` tests in `internal/sender` and `internal/server` keep using insecure credentials and are unaffected, apart from `sender.Send`'s new parameter.

## Deployment Notes

- `.gitignore` gains `certs/` — currently absent, and generated private keys must never be committable.
- `docker-compose.yml` keeps working unchanged (TLS stays off unless the vars are set). Actually enabling it there requires mounting `certs/` into both services and setting the three env vars on each; that is a documented follow-up step, not part of this change.
- `CLAUDE.md`: document the three new flags per binary, the `--auth-token` semantics change, and replace the "gRPC transport uses no TLS" line.
- `SECURITY.md`: update the SEC-001 note, which currently records that gRPC is unauthenticated by design.

## Out of Scope

- Automated certificate rotation or short-lived certs.
- Certificate revocation (CRL/OCSP).
- TLS on any non-gRPC surface — the HTTP API's transport security is a separate concern.
- Replacing the shared-secret scheme with per-client identities derived from the client certificate. Once mTLS is in place the cert *is* an identity and the token becomes arguably redundant, but collapsing them is a follow-up, not this change.
