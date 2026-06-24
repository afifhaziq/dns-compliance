# Additional DNS Record Types on the Per-URL History Page — Design

## Problem

`results.$url.tsx` only ever shows the A-record IP that resolved during a compliance scan (`r.resolved_ip`). There's no way to see a host's AAAA, CNAME, MX, TXT, or NS records — useful supplementary context for an auditor inspecting a domain (e.g. spotting a CNAME pointing at a CDN, or mail records on a supposedly-parked domain), but unrelated to the compliant/violation logic the rest of the page (and the whole product) is built around.

## Goals

- Show AAAA, CNAME, MX, TXT, and NS records for the page's hostname, displayed once near the top of the page.
- Keep this informational and separate from compliance semantics — it's a live, present-tense lookup ("what does this host look like right now"), not tied to any specific configured DNS server or historical scan.
- Minimal backend footprint: no DB schema change, no proto change, no changes to the crawler pipeline or the existing `internal/dns` resolver abstractions used for compliance scanning.
- Avoid redundant network lookups: cache per-hostname results in the browser tab for the session, so revisiting the same URL's history page later (same tab) doesn't re-query.

## Non-goals (out of scope for this spec)

- Tying the lookup to one of the user's configured DNS servers (Google/Cloudflare/etc.) or to DoT/DoH — this is a single system-resolver lookup. (Extending `internal/dns`'s DoH/DoT resolvers to support non-A query types is a separate, larger change, noted in the prior conversation but explicitly deferred.)
- Historical/per-day tracking of these record types (no heatmap, no DB persistence) — always "right now" data.
- A manual refresh control, backend-side caching/TTL, or polling. The cache lives only as long as the browser tab does.
- Showing the A record here too — it's already shown per-DNS-server in the existing results table; duplicating it in this new panel would conflate the two different meanings of "resolved IP" (compliance-scan result vs. live lookup).

## Backend change

New handler `DNSRecordsByURL` in `internal/server/handlers.go`, registered as `GET /api/dns-records/*` in `internal/server/router.go` (same `*url` wildcard convention as `/api/results/*` and `/api/heatmap/*`).

1. Percent-decode the wildcard param, then extract the hostname the same way as elsewhere (`net/url`'s `(*url.URL).Hostname()` after `url.Parse`).
2. Run `net.DefaultResolver.LookupHost(ctx, hostname)` first, with a short timeout (reuse the existing 5s default convention from `--dns-timeout`). If this errors with "no such host" (NXDOMAIN) or any other resolution failure, return immediately:
   ```json
   { "hostname": "example.com", "resolved": false }
   ```
   No further lookups run — if the base host doesn't resolve, CNAME/MX/TXT/NS lookups would fail too.
3. If step 2 succeeds, concurrently run (`errgroup` or a plain `sync.WaitGroup`, each with its own timeout context):
   - `net.DefaultResolver.LookupCNAME(ctx, hostname)` → single string, or empty if same as hostname / lookup errors
   - `net.DefaultResolver.LookupMX(ctx, hostname)` → `[]*net.MX`
   - `net.DefaultResolver.LookupTXT(ctx, hostname)` → `[]string`
   - `net.DefaultResolver.LookupNS(ctx, hostname)` → `[]*net.NS`

   A per-type lookup error (e.g. "no such host" because that specific record type has no data) is treated as an empty result for that type, **not** a request failure — only a failure of the initial `LookupHost` in step 2 produces `resolved: false`.
4. Response shape:
   ```json
   {
     "hostname": "example.com",
     "resolved": true,
     "records": {
       "aaaa": ["2606:2800:220:1:248:1893:25c8:1946"],
       "cname": [],
       "mx": ["10 mail.example.com."],
       "txt": ["v=spf1 -all"],
       "ns": ["a.iana-servers.net.", "b.iana-servers.net."]
     }
   }
   ```
   `mx` entries are formatted as `"<preference> <host>"` strings (simplest shape for display; no separate preference field needed since nothing sorts/filters by it). All other types are plain string slices. Empty slice, not `null`, when a type has no records — keeps the frontend from needing a null check.

This uses the container's system resolver (`/etc/resolv.conf` inside the Docker container, i.e. Docker's embedded DNS), exactly like any other stdlib `net.Lookup*` call already implicitly available in Go — no new dependency, no change to `internal/dns`.

## Frontend changes

### New API module: `web/src/api/dns-records.ts`

```ts
export interface DnsRecordsResponse {
  hostname: string
  resolved: boolean
  records?: {
    aaaa: string[]
    cname: string[]
    mx: string[]
    txt: string[]
    ns: string[]
  }
}

export async function fetchDnsRecords(url: string): Promise<DnsRecordsResponse> {
  const res = await fetch(`/api/dns-records/${encodeURIComponent(url)}`)
  if (!res.ok) throw new Error(`Failed to load DNS records: ${res.status}`)
  return res.json()
}
```

Type added alongside the other shared API types in `web/src/api/types.ts` (or co-located in the new module, matching how `scan.ts` keeps its own response types — co-locating is simpler here since nothing else needs this type).

### Session cache: `web/src/lib/dns-records-cache.ts`

A tiny `sessionStorage` wrapper, keyed by hostname (not full URL — the record types are host-level, so `https://example.com/page` and `https://example.com/` should share a cache entry):

```ts
const KEY_PREFIX = 'dns-records:'

export function getCachedDnsRecords(hostname: string): DnsRecordsResponse | null {
  try {
    const raw = sessionStorage.getItem(KEY_PREFIX + hostname)
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

export function setCachedDnsRecords(hostname: string, data: DnsRecordsResponse): void {
  try {
    sessionStorage.setItem(KEY_PREFIX + hostname, JSON.stringify(data))
  } catch {
    // sessionStorage unavailable (e.g. privacy mode) — silently skip caching
  }
}
```

No TTL — entries live until the tab closes, matching the agreed caching behavior. Wrapped in try/catch since `sessionStorage` can throw in some browser privacy modes; caching is a nice-to-have, never a hard requirement for the feature to work.

### Page wiring (`web/src/routes/results.$url.tsx`)

New state: `dnsRecords: DnsRecordsResponse | null`, `dnsRecordsLoading: boolean`.

```ts
useEffect(() => {
  const cached = getCachedDnsRecords(hostname)
  if (cached) {
    setDnsRecords(cached)
    setDnsRecordsLoading(false)
    return
  }
  setDnsRecordsLoading(true)
  fetchDnsRecords(url)
    .then(data => { setDnsRecords(data); setCachedDnsRecords(hostname, data) })
    .catch(() => setDnsRecords({ hostname, resolved: false }))
    .finally(() => setDnsRecordsLoading(false))
}, [url, hostname])
```

This fetches once per mount of this route (i.e. once per page visit), consulting the session cache first — satisfies "only request a new record if the page is open" while avoiding repeat network calls within the same tab across visits.

### UI: new `DnsRecordsPanel` component, same file

Placed directly after the `page-header` block and before the existing heatmap `dash-section` (per the agreed placement).

```tsx
const RECORD_LABELS = [
  ['aaaa', 'AAAA'],
  ['cname', 'CNAME'],
  ['mx', 'MX'],
  ['txt', 'TXT'],
  ['ns', 'NS'],
] as const

function DnsRecordsPanel({ data, loading }: { data: DnsRecordsResponse | null; loading: boolean }) {
  if (loading) {
    return (
      <div className="dash-section dns-records-panel">
        <p className="dash-label">DNS Records</p>
        <div className="dns-records-grid">
          {RECORD_LABELS.map(([key]) => (
            <div key={key} className="dns-record-block">
              <span className="skeleton" style={{ width: 60, height: 11 }} />
              <span className="skeleton" style={{ width: 120, height: 14, marginTop: 6 }} />
            </div>
          ))}
        </div>
      </div>
    )
  }

  if (!data || !data.resolved || !data.records) {
    return (
      <div className="dash-section dns-records-panel">
        <p className="dash-label">DNS Records</p>
        <p className="dns-records-error">Unable to resolve DNS records for this host.</p>
      </div>
    )
  }

  return (
    <div className="dash-section dns-records-panel">
      <p className="dash-label">DNS Records</p>
      <div className="dns-records-grid">
        {RECORD_LABELS.map(([key, label]) => {
          const values = data.records![key]
          return (
            <div key={key} className="dns-record-block">
              <span className="dns-record-type">{label}</span>
              {values.length === 0 ? (
                <span className="empty-cell">—</span>
              ) : (
                <span className="dns-record-values">
                  {values.map((v, i) => <span key={i} className="ip-value">{v}</span>)}
                </span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
```

Rendered in the page body as `<DnsRecordsPanel data={dnsRecords} loading={dnsRecordsLoading} />` right after the `page-header` `<div>`.

### New CSS (`web/src/index.css`)

A handful of small rules alongside the existing `.dash-section`/`.dash-label`/`.empty-cell`/`.ip-value` block (reusing those existing classes for the heading and value styling — only the grid/layout wrapper classes are new):

```css
.dns-records-panel {
  @apply mb-8;
}

.dns-records-grid {
  @apply flex flex-wrap gap-x-8 gap-y-4 mt-2;
}

.dns-record-block {
  @apply flex flex-col gap-1 min-w-[140px];
}

.dns-record-type {
  @apply text-[11px] font-semibold tracking-[0.06em] uppercase text-stone-muted;
}

.dns-record-values {
  @apply flex flex-col gap-0.5;
}

.dns-records-error {
  @apply text-sm text-stone-muted mt-2;
}
```

No new color tokens — `ip-value`/`empty-cell`/`dash-label` already carry the achromatic styling from DESIGN.md.

## Testing

- Backend: a new `internal/server/handlers_test.go` case for `DNSRecordsByURL` — since this hits real DNS (no mockable resolver injected, mirroring `internal/dns`'s existing "makes real network calls" test category per CLAUDE.md), test against a hostname known to resolve (e.g. `example.com`) and assert the response shape, plus a case for a deliberately non-existent hostname asserting `resolved: false`. Consistent with the existing test dependency matrix — this test requires network access, like `internal/dns/`'s tests.
- Frontend: no test runner configured for routes (consistent with the rest of this page). Verify manually: panel shows real AAAA/MX/TXT/NS values for a known domain, shows `—` for types with no records (e.g. a domain with no MX), shows the inline error for a non-resolving hostname, and a second visit to the same URL within the same tab does not trigger a new network request (confirm via browser devtools Network tab).

## Open implementation detail

The handler's per-type concurrency (step 3) needs a decision at implementation time on how individual lookup errors are distinguished from "genuinely empty": Go's `net.LookupMX`/`LookupTXT`/`LookupNS`/`LookupCNAME` return a `*net.DNSError`, and only `dnsErr.IsNotFound` (or message containing "no such host") should be treated as empty; any other error type (timeout, server failure) should probably still degrade to an empty list for that type rather than failing the whole request, since the overall "host resolved" check already happened in step 2 — but this means a transient per-type failure looks identical to "no records of this type" to the end user. Acceptable for a first cut; revisit if this proves confusing in practice.
