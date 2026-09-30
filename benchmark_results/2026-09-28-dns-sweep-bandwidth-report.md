# DNS Sweep — Throughput, Bandwidth & Per-Query Cost

**Date:** 2026-09-28
**Branch:** `perf/dns-sweep-throughput` (commit `5d81154`)
**Scope:** full production-size sweep after the CRD import — 34,269 domains × 5 DNS servers (3 UDP, 2 DoH) ≈ 171k checks. Follows up [2026-07-21-dns-benchmark-report.md](./2026-07-21-dns-benchmark-report.md), which covered only 100 domains × 6 servers.

## 1. Throughput (before / after `5d81154`)

| | total time | UDP timeouts |
|---|---|---|
| Before (crawler fix only) | 11m51s | ~26.7k (~27% per UDP server) |
| After (pooled UDP sockets, concurrent servers, batched ingest) | **1m15s** | **132** |

Every timeout reads as `Compliant=true`, so the timeout drop is a correctness fix, not just a speedup. Root cause: WSL2 caps new UDP sockets at ~100/s host-wide; dialing one socket per query at 100 workers hit that cap. See the commit message for the full change list.

## 2. Bandwidth per sweep

**Method:** crawler run standalone (no server, so no localhost gRPC traffic), `--dns-workers 100`, DNS-only. Bytes/packets read from `/sys/class/net/<default-iface>/statistics/{rx,tx}_{bytes,packets}` before and after each run. UDP and DoH measured in separate runs.

| | checks | time | sent | received | total | per check |
|---|---|---|---|---|---|---|
| 3 UDP servers (Google, Cloudflare, TM 1) | 102,858 | 55s | 8.4 MiB (104k pkts) | 11.1 MiB (104k pkts) | **19 MiB** | **198 B** |
| 2 DoH servers (Google, Cloudflare) | 68,572 | 52s | 12.3 MiB | 26.4 MiB | **38 MiB** | **592 B** |
| **Full sweep (5 servers)** | 171,430 | | | | **≈ 57 MiB** | |

- Per DNS server per sweep: **~6.5 MiB (UDP)**, **~19 MiB (DoH)**. Adding a server adds that much.
- DoH costs ~3× UDP per check (HTTPS framing + TLS), despite connection reuse — 2/3 of the traffic for 40% of the checks.
- Hourly sweeps ≈ **1.4 GiB/day**.
- Not included: crawler→server gRPC traffic. Estimated (not measured) at 100–200 B per result, ≈ 17–35 MiB per sweep, if the two binaries run on separate hosts.
- Counters cover the whole interface, so a little unrelated background traffic is included — accurate to within a few percent.

## 3. Size of a single UDP query

~**200 B on the wire per round trip**: ~85 B out, ~112 B back.

| | query (sent) | response (received) | round trip |
|---|---|---|---|
| **Measured, per packet** | **84.5 B** | **111.8 B** | **≈ 196 B** |
| DNS message only | 34.9 B | 78.0 B | 112.9 B |
| + UDP/IP headers (28 B) | 62.9 B | 106.0 B | 168.9 B |
| + Ethernet header (14 B) | 76.9 B | 120.0 B | 196.9 B |

- Measured total matches the header arithmetic almost exactly; the small query/response split drift is background traffic.
- Query = 12 B DNS header + encoded name + 4 B (type A, class IN). Response echoes the question plus answer records — larger for CNAME chains or multiple A records.
- 104k packets each way for 103k checks → retries are negligible.

## 4. Single UDP query latency

From the last full sweep (`scan_run_id=10`), successful queries only (`latency_ms > 0`):

| server | median | average | p95 |
|---|---|---|---|
| Cloudflare UDP | 25 ms | 121 ms | 529 ms |
| TM 1 | 27 ms | 81 ms | 249 ms |
| Google UDP | 83 ms | 194 ms | 480 ms |

Most queries return in under 100 ms; the mean is dragged up by a slow tail, most likely resolver cache misses requiring a full recursive lookup.
