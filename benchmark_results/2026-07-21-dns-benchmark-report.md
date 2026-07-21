# DNS Compliance System — End-to-End Benchmark & Ablation Report

**Date:** 2026-07-21
**Scope:** `dig`/`nslookup` baseline vs. crawler vs. full server pipeline, across the live DNS server set, with a concurrency ablation and a root-cause fix validated end-to-end.

---

## 1. Objective

Benchmark this DNS-compliance system end-to-end against a raw `dig`/`nslookup` baseline, and identify — with evidence, not guesswork — which layer of the stack (crawler DNS resolution, server ingestion, or frontend delivery) actually limits scan speed. Where a real bottleneck was found, root-cause it, fix it, and re-measure to confirm the fix worked.

## 2. Problem

This system polls ISP DNS resolvers on a schedule to detect takedown-order compliance (a domain that still resolves = violation; one that fails to resolve = compliant). Scan speed directly bounds how many domains × how many DNS servers can be checked per sweep, and how quickly a regression (a domain becoming reachable again) is detected. Before this investigation, there was no data on where time was actually going — whether the DNS resolution itself, the crawler's own pipeline architecture, the Go server's ingestion path (DB writes, gRPC, broadcast), or the frontend was the limiting factor. Isolated component profiling doesn't answer this on its own — it required a layered ablation with a common reference (`dig`) as ground truth.

## 3. Method

All benchmarks used the same 100-domain site list (`site-list.txt`) against the same 6 real DNS servers pulled directly from the live `dns_servers` table (not a synthetic list):

| ISP | Name | Address | Protocol |
|---|---|---|---|
| Cloudflare | Cloudflare DoH | `https://1.1.1.1/dns-query` | doh |
| Cloudflare | Cloudflare DoT | `1.1.1.1:853` | dot |
| Digi | Digi 1 | `182.62.210.14:53` | udp (real Malaysian ISP resolver, ACL-restricted to subscriber IPs) |
| Google | "Google 1" *(mislabeled — actually Cloudflare `1.1.1.1:53`, a pre-existing data bug unrelated to this benchmark)* | `1.1.1.1:53` | udp |
| Google | Google 2 | `8.8.8.8:53` | udp |
| Google | Google DoH 2 | `https://dns.google/dns-query` | doh |

**Layer 1 — raw baseline.** `dns_benchmark.sh` (an existing but broken script in the repo — its hand-rolled YAML parser expected `- name:` as the first field per server entry, but the live schema puts `- isp:` first; fixed as part of this work) ran `dig`/`nslookup`/`curl` sequentially, one query at a time, no concurrency. This is the zero-pipeline-overhead reference point.

**Layer 2 — crawler-only.** The `crawler` binary run directly, DNS-only (`--screenshots` off, no `--grpc-addr`), against the same 100×6 = 600 site/server checks. Run at `--dns-workers 20` (the default) and `--dns-workers 1` (serialized) to separate the pipeline's concurrency gain from the underlying protocol/implementation cost.

**Layer 3 — full end-to-end.** The real server (Postgres + MinIO + gRPC + SSE) driven through its actual HTTP API: log in, `POST /api/scan` with the same 100 URLs, and an `EventSource`-equivalent listener on `GET /api/scan/progress/stream` timestamping the moment a client would actually see the run complete. Temporary `time.Now()`/log instrumentation was added to `scanner.go` (subprocess exec duration) and `grpc.go` (per-result DB insert / broadcast timing), captured, then reverted — the repo carries no trace of it.

**Root-cause phase.** A 100%-timeout anomaly on Digi triggered a focused investigation: isolated `dig` A/AAAA tests against Digi, code review of the crawler's IPv6 side-lookup wiring, and finally reading Go's own stdlib source (`/usr/local/go/src/net/dnsclient_unix.go`) to find the actual mechanism, followed by a fix and a full re-run of Layers 2 and 3 to quantify the fix's impact.

## 4. Results

### 4.1 Layer 1 — raw dig/nslookup/curl baseline (unaffected by the later fix; not re-run)

100 sites × 6 servers × 2 tools = 1,200 queries, sequential: **786s total, 655ms/query average.**

| Server | Queries | Time | Avg/query |
|---|---|---|---|
| Cloudflare DoH | 200 | 31.4s | 157ms |
| **Cloudflare DoT** | 200 | **522.3s** | **2,611ms** |
| Digi 1 | 200 | 40.8s | 204ms (fast REFUSED) |
| Google 1 (mislabeled) | 200 | 42.0s | 210ms |
| Google 2 | 200 | 118.7s | 593ms |
| Google DoH 2 | 200 | 30.8s | 154ms |

Cross-checking one domain (`12bet.com`) across servers showed the DoT number wasn't "slow domains" — UDP resolved in 78ms, DoH in 217ms, but `dig +tls` timed out at 5,022ms on the same domain against the same resolver. Root cause: zero TLS session reuse (a fresh handshake per query).

### 4.2 Layer 2 — crawler-only DNS timing

| Run | Total time | Notes |
|---|---|---|
| Pre-fix, `--dns-workers 20` | 44s | ~17.9× faster than Layer 1, from concurrency alone |
| Pre-fix, `--dns-workers 1` (serialized) | 682s (11m22s) | Digi alone: 478s — **100% of its 100 queries timed out** |
| Post-fix (zero-retry), `--dns-workers 20` (3 runs) | 25.1s / 3.7s / 11.7s | Digi now answers in ms, but see the retry-regression note in §4.4 |
| Post-fix (zero-retry), `--dns-workers 1` (serialized) | 45s | down from 682s — 15.2× from the fix alone |
| **Post-fix + retry-on-transport-failure**, `--dns-workers 20` (3 runs) | **4.0s / 4.9s / 10.2s** | retry refinement re-adds resilience without reintroducing the REFUSED bug — see §4.4 |
| **Post-fix + retry-on-transport-failure**, `--dns-workers 1` (serialized) | **51.8s** | slightly above the zero-retry 45s, the expected cost of giving genuinely-lost packets a second chance |

Per-server pass breakdown, pre-fix vs. post-fix (serialized, `--dns-workers 1`, isolating protocol cost with concurrency removed as a variable):

| Server | Pre-fix (100 sites, serial) | Post-fix (100 sites, serial) |
|---|---|---|
| Cloudflare DoH | 5s | ~2s |
| Cloudflare DoT | 17s | ~6s |
| **Digi 1** | **478s** (100/100 timeout) | **~5s** (fast REFUSED) |
| Google 1 | 48s | ~6s |
| Google 2 | 90s | ~16s |
| Google DoH 2 | 20s | ~8s |
| **Total** | **682s** | **45s** |

### 4.3 Layer 3 — full end-to-end (server ingestion + SSE)

Pre-fix: **47.4–49.6s** trigger-to-visible for the full 600-check sweep, of which the server's own processing (DB insert + broadcast across 600 gRPC `Submit` calls) summed to **2,275ms total (3.79ms/call average)** — under 5% of total time. `POST /api/scan` itself returns in ~3.5ms (fire-and-forget). SSE delivery is push-based and effectively instant once the broadcast fires.

Post-fix, three runs (server-log "Sweep complete in Ns", the authoritative source after a bug in one of my own polling scripts — see note below): **4s, 4s, 9s.** One run was independently cross-checked via SSE-listener wall-clock: 3.912s trigger-to-visible, consistent with the server's own 4s figure.

*Methodology note:* two of the three post-fix end-to-end samples were originally measured with a script that polled `GET /api/scan/status` for `"running":false` — a field that endpoint doesn't return (it returns `{"status":"idle"}`), so that script just spun for its full 60s timeout both times, producing bogus ~58-59s readings. Those are discarded; the numbers above come from the server's own authoritative "Sweep complete in Ns" log line instead.

### 4.4 Root cause: why Digi timed out, and the fix

Confirmed directly from Go's stdlib source (`net/dnsclient_unix.go`), not inferred:

- **`checkHeader()` (line 240-249)** treats only `RCodeSuccess` and `RCodeNameError` (NXDOMAIN) as final answers. Every other RCode — including `REFUSED` — falls into `errServerMisbehaving`, the same bucket as "the server is broken."
- **`tryOneName()` (line 328-336)** only stops immediately on `errNoSuchHost` (NXDOMAIN). A `REFUSED` response is recorded as `lastErr` and the loop **retries** — `cfg.attempts × server-count` more times — instead of accepting it as a definitive negative answer the way `dig` (no retry, `+tries=1`) and a human operator would.

This meant every real query against Digi (which answers REFUSED correctly and fast, confirmed via direct `dig` A and AAAA tests, both ~16-20ms) got retried by Go's client, and those retries appear to go unanswered under load, burning the full context timeout before a generic timeout error bubbles up — masking a clean, fast REFUSED as a 5-second stall.

**Fix** ([internal/dns/resolver.go](../internal/dns/resolver.go)): `NewResolver` (UDP) and `NewDoTResolver` no longer use `net.Resolver` at all. Each now hand-builds a single DNS query (mirroring the pattern the DoH resolver already used), sends it once, and parses the raw response directly — any RCode other than NXDOMAIN falls through to a fast, final `"no A records for %s"` error. No retries, matching `dig`'s reference behavior. Confirmed via a direct before/after test: Digi went from **10 domains in ~9-10s (all timeouts)** to **10 domains in 0.028s (all fast REFUSED)**, with Cloudflare UDP/DoT still resolving real domains correctly.

A second, separate fix went in alongside this: `cmd/crawler/main.go` was unconditionally wiring an informational AAAA side-lookup (`ScanResult.ResolvedIPv6`) into every scan, when the intent was for AAAA to be on-demand only (via the separate `GET /api/dns-records` panel). That wiring was removed — scans are now A-only at the call-site level, though this was **not** what fixed Digi (verified: Digi still timed out 100% after this change alone, before the deeper `net.Resolver` replacement).

### 4.5 Follow-up: retry-on-transport-failure-only

The zero-retry design in §4.4 fixed REFUSED but, as flagged in the first version of this report, also removed all tolerance for genuine transient UDP packet loss — a real regression, visible as run-to-run variance (25.1s/3.7s/11.7s across identical runs; an isolated Cloudflare-UDP-only test showed 2-40 timeouts out of 100 across repeated runs).

**Fix:** `exchangeWithRetry` (`internal/dns/resolver.go`) now retries up to once — but *only* when the transport itself fails to produce a response at all (dial error, write error, or a read that times out with nothing back). It never retries once a response actually arrives, no matter its RCode — REFUSED, SERVFAIL, and NXDOMAIN all remain final on the first attempt, exactly as before. The two attempts split whatever time remains on the caller's context deadline, so total worst-case latency per site stays bounded by `--dns-timeout` regardless of retry count. For DoT, each attempt opens a fresh TCP+TLS connection (a partially-read TCP stream can't be safely reused for a retry); for UDP, the same socket is reused since UDP is connectionless.

**Verification:**
- Digi: unchanged, 10 domains in 0.024s — confirms REFUSED still isn't retried.
- Cloudflare UDP alone, 100 domains, 3 repeated runs: **0 timeouts each time** (down from the earlier 2-40/100 range).
- Full 6-server benchmark, 3 repeated runs at `--dns-workers 20`: **4.0s / 4.9s / 10.2s** — a much tighter spread than the zero-retry version's 3.7-25.1s, though not perfectly flat (some genuine double-loss cases still occur).
- Serialized (`--dns-workers 1`): 51.8s, up slightly from the zero-retry version's 45s — the expected, bounded cost of the extra attempt when it's actually needed.

## 5. Discussion

**The DNS layer was — and remains — the dominant cost, but the story changed.** Pre-fix, the bottleneck was overwhelmingly Digi's retry storm (478s of 682s serialized, 70%) plus DoT's handshake cost. Post-fix, both are largely resolved, and total pipeline time dropped from the 44-49s range to single digits in the best case.

**The server and frontend were never the bottleneck, and still aren't.** Confirmed twice now (pre- and post-fix): the server's DB insert + broadcast path costs under 4ms per result on average, `POST /api/scan` returns near-instantly, and SSE delivery is push-based with no polling delay. Nothing about the fix changes this conclusion — it was always about DNS resolution time, and remains so.

**Concurrency (`--dns-workers 20`) still matters a lot on its own merits.** Independent of the fix, the pipeline's worker pool gives a genuine 15-18× speedup by overlapping per-query latency — this is real architecture value, not just masking a problem.

**DoT's cost is client-implementation-dependent, not inherent to the protocol.** `dig +tls` failed catastrophically at scale (90% timeout rate) against Cloudflare due to zero TLS session reuse; Go's DoT client (same zero-reuse design) was far more resilient in practice, though still the second-slowest protocol after the fix (~6s serialized vs ~2s for DoH). If this is ever pointed at a real ISP's DoT endpoint instead of Cloudflare, don't assume the same resilience — Cloudflare's server-side handling is unusually good, and this wasn't independently re-tested against a resolver that behaves like Digi over DoT.

**A real trade-off was introduced by the initial fix, and then closed.** The zero-retry version traded away resilience to genuine transient UDP packet loss for correctness on REFUSED — the old `net.Resolver`-based implementation retried on *any* failure and silently masked real packet loss as a side effect, while the new one initially retried on nothing, so a single dropped packet against an otherwise-healthy resolver (Cloudflare, Google) became a hard 5-second timeout instead of a quiet, invisible second attempt. This showed up as real run-to-run variance (25.1s/3.7s/11.7s on identical 600-check runs; 2-40/100 timeouts on an isolated Cloudflare-UDP test) that didn't exist pre-fix. The closing fix (§4.5) restores the retry, but scoped correctly: retry only on a genuine transport-level failure (no response at all), never on an actual response of any kind — REFUSED included. Verified: Cloudflare UDP's isolated flakiness dropped to 0/100 timeouts across 3 repeated runs, and the full-benchmark spread tightened to 4.0-10.2s (still not perfectly flat — occasional genuine double-packet-loss still happens, which is inherent to UDP, not a bug).

**Data-integrity note, unrelated to performance:** the `dns_servers` row labeled "Google 1" is actually Cloudflare's `1.1.1.1:53`, not Google's `8.8.8.8` — a pre-existing labeling bug, surfaced by this benchmark, not caused by it.

## 6. Conclusion

The bottleneck was DNS resolution, specifically two separate implementation issues, not the server or frontend:

1. **Fixed:** `dig +tls`-style DoT fragility doesn't apply to this app (Go's client already handles it better), but the crawler's shared `net.Resolver`-based UDP/DoT path retried on `REFUSED` responses as if they were errors, turning fast, correct ISP-resolver rejections into 5-second stalls — confirmed from Go's own stdlib source, fixed by replacing that path with a single-shot hand-rolled query (matching the existing DoH resolver's pattern), and validated with a 300×+ speedup on the specific failing case (Digi: ~9s → 0.028s for 10 domains) plus a clean 15.2× reduction on the full serialized 600-check benchmark (682s → 45s).
2. **Confirmed non-issues:** server ingestion (DB + broadcast + SSE) and frontend delivery remain under 5% of total pipeline time, unchanged by this work.
3. **Fixed (was flagged as an open item, now closed):** the initial zero-retry design traded away resilience to genuine transient packet loss, producing real run-to-run variance (3.7s-26s on identical 600-check runs) that didn't exist before. `exchangeWithRetry` closes this gap by retrying once, but only on a genuine transport-level failure (no response at all) — never once an actual response, including REFUSED, comes back — so the original bug stays fixed. Verified: the isolated Cloudflare-UDP flakiness test went from 2-40/100 timeouts to 0/100 across 3 repeated runs, and the full-benchmark spread tightened from 3.7-25.1s to 4.0-10.2s.
