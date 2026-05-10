# DNS Compliance Checker

Verifies ISP takedown compliance by checking whether blocked websites are still accessible via DNS. A site that **fails** DNS resolution is considered **compliant** (the ISP has blocked it); a site that **resolves** is a **violation**.

For each non-compliant site, a full-page screenshot is taken as evidence, saved to `<dns_server>/<hostname>/`.

## Requirements

- Go 1.21+
- Google Chrome (for screenshots)

## Build

```bash
go build -o dns-compliance ./cmd/main.go
```

## Usage

```bash
# Check a list of sites using system DNS
./dns-compliance --sites sites.txt

# Check specific URLs inline
./dns-compliance "https://example.com" "https://example2.com"

# Check against multiple DNS servers (plain, DoT, DoH)
./dns-compliance --sites sites.txt --dns-servers dns-server.yaml
```

## DNS Servers Config

Create a YAML file to query multiple resolvers and compare results:

```yaml
servers:
  - name: Google
    address: 8.8.8.8:53
    protocol: udp       # plain DNS (default if omitted)

  - name: Cloudflare DoT
    address: 1.1.1.1:853
    protocol: dot       # DNS-over-TLS — encrypted, bypasses DNS hijacking

  - name: Cloudflare DoH
    address: https://1.1.1.1/dns-query
    protocol: doh       # DNS-over-HTTPS — bypasses both hijacking and port blocks
```

`protocol` can be `udp`, `dot`, or `doh`. Use DoH when your ISP blocks direct UDP/TCP to external resolvers.

## Output

```
URL                  DNS_SERVER      COMPLIANT  RESOLVED_IP     SCREENSHOT
https://example.com  Google          false      93.184.216.34   Google/example.com/2026-05-10T12-00-00-a1b2c3d4.png
https://example.com  Cloudflare DoH  true                       no
```

- `COMPLIANT=true` — DNS failed; site is blocked as required
- `COMPLIANT=false` — DNS resolved; site is accessible (violation)
- `(shared)` in SCREENSHOT — same IP resolved by multiple servers; screenshot taken once

Screenshots use the resolved IP directly via Chrome's `--host-resolver-rules`, so each screenshot reflects what that DNS server's users actually see — not what your system DNS returns.

## Key Flags

| Flag | Default | Description |
|---|---|---|
| `--sites` | | File with one URL per line |
| `--dns-servers` | | YAML file of DNS servers |
| `--dns-workers` | 20 | Concurrent DNS lookups |
| `--screenshot-workers` | 5 | Concurrent Chrome tabs |
| `--dns-timeout` | 5s | Per-site DNS timeout |
| `--screenshot-timeout` | 30s | Per-site screenshot timeout |
| `--interval` | 0 | Repeat every N minutes (0 = run once) |
| `--grpc-addr` | | Send results to a gRPC server |
