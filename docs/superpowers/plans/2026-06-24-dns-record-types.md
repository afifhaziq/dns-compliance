# Additional DNS Record Types Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only "DNS Records" panel to `/results/$url` showing the host's current AAAA, CNAME, MX, TXT, and NS records — a live lookup via the system resolver, separate from the compliance-scan pipeline, cached per-hostname for the browser tab's session.

**Architecture:** A new, store-independent backend endpoint (`GET /api/dns-records/*url`) runs concurrent stdlib `net.Lookup*` calls against the system resolver and returns a flat JSON record set. The frontend fetches this once per page visit (checking a `sessionStorage` cache first) and renders it in a new panel between the page header and the existing year-heatmap section.

**Tech Stack:** Go stdlib `net` package (no new dependency) for the backend; React + TypeScript + `sessionStorage` for the frontend. No DB, proto, or pipeline changes.

## Global Constraints

- No DB schema change, no proto change, no changes to `internal/dns`, `internal/pipeline`, or the crawler — this is fully additive (per spec's Goals/Non-goals).
- The new endpoint uses the container's system resolver (`net.DefaultResolver`), not any of the configured `dns_servers` rows — it is explicitly *not* tied to a specific DNS server (per spec's Non-goals).
- A record is never shown in this panel — it's already shown per-DNS-server in the existing results table; this panel only covers AAAA, CNAME, MX, TXT, NS (per spec's Non-goals).
- No manual refresh control, no backend caching/TTL, no polling — frontend caches per-hostname in `sessionStorage` for the tab's lifetime only (per spec's Non-goals and Goals).
- No JS/TS test runner exists in this repo — frontend verification is `cd web && npx tsc -b` (type-check) plus manual check via `npm run dev`. Backend verification is real `go test` (the new endpoint hits real DNS, same category as `internal/dns/`'s existing tests per `CLAUDE.md`).

---

### Task 1: Backend — `GET /api/dns-records/*url` endpoint

**Files:**
- Modify: `internal/server/handlers.go` (add handler + helpers, near the end of the "Results" section, after `HeatmapByURL`)
- Modify: `internal/server/router.go` (register the route)
- Test: `internal/server/handlers_test.go` (add two new tests)

**Interfaces:**
- Produces: `GET /api/dns-records/*url` → `200 OK` with body:
  ```json
  {
    "hostname": "example.com",
    "resolved": true,
    "records": {
      "aaaa": ["2606:2800:220:1:248:1893:25c8:1946"],
      "cname": [],
      "mx": ["10 mail.example.com"],
      "txt": ["v=spf1 -all"],
      "ns": ["a.iana-servers.net", "b.iana-servers.net"]
    }
  }
  ```
  or, when the hostname doesn't resolve at all: `{"hostname": "...", "resolved": false}` (no `records` key — Go's `omitempty` on a nil pointer).
- Consumes: nothing from `db.Store` — this handler is independent of the database, unlike every other handler in this file.

- [ ] **Step 1: Write the failing tests**

Add to `internal/server/handlers_test.go`, after `TestHeatmapByURL_GroupsByDay` (currently ending at line 401):

```go
func TestDNSRecordsByURL_ResolvesKnownHost(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/dns-records/google.com", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Hostname string `json:"hostname"`
		Resolved bool   `json:"resolved"`
		Records  *struct {
			AAAA  []string `json:"aaaa"`
			CNAME []string `json:"cname"`
			MX    []string `json:"mx"`
			TXT   []string `json:"txt"`
			NS    []string `json:"ns"`
		} `json:"records"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Hostname != "google.com" {
		t.Fatalf("expected hostname google.com, got %q", resp.Hostname)
	}
	if !resp.Resolved {
		t.Fatalf("expected resolved=true for google.com")
	}
	if resp.Records == nil || len(resp.Records.NS) == 0 {
		t.Fatalf("expected at least one NS record for google.com, got %+v", resp.Records)
	}
}

func TestDNSRecordsByURL_NXDOMAIN(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/dns-records/this-host-should-not-exist-zzqxv12345.invalid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Hostname string `json:"hostname"`
		Resolved bool   `json:"resolved"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Resolved {
		t.Fatalf("expected resolved=false for a non-existent host")
	}
}
```

(`.invalid` is the RFC 2606 reserved TLD guaranteed to never resolve — deterministic NXDOMAIN without depending on a specific real domain's absence.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/... -run TestDNSRecordsByURL -v`
Expected: FAIL — `404 page not found` (route doesn't exist yet), since `w.Code` will be 404, not 200.

- [ ] **Step 3: Implement the handler**

Add to `internal/server/handlers.go`, after the `HeatmapByURL` function (after the closing brace currently at line 243):

```go
// DNS Records (live lookup, independent of the compliance-scan pipeline)

type dnsRecordSet struct {
	AAAA  []string `json:"aaaa"`
	CNAME []string `json:"cname"`
	MX    []string `json:"mx"`
	TXT   []string `json:"txt"`
	NS    []string `json:"ns"`
}

type dnsRecordsResponse struct {
	Hostname string        `json:"hostname"`
	Resolved bool          `json:"resolved"`
	Records  *dnsRecordSet `json:"records,omitempty"`
}

func (h *Handlers) DNSRecordsByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}

	hostname := urlValue
	if parsed, parseErr := url.Parse(urlValue); parseErr == nil && parsed.Hostname() != "" {
		hostname = parsed.Hostname()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(ctx, hostname)
	if err != nil {
		writeJSON(w, http.StatusOK, dnsRecordsResponse{Hostname: hostname, Resolved: false})
		return
	}

	records := lookupDNSRecordSet(ctx, hostname, addrs)
	writeJSON(w, http.StatusOK, dnsRecordsResponse{Hostname: hostname, Resolved: true, Records: &records})
}

func lookupDNSRecordSet(ctx context.Context, hostname string, addrs []string) dnsRecordSet {
	set := dnsRecordSet{AAAA: aaaaFromAddrs(addrs)}

	var wg sync.WaitGroup
	wg.Add(4)

	go func() { defer wg.Done(); set.CNAME = lookupCNAME(ctx, hostname) }()
	go func() { defer wg.Done(); set.MX = lookupMX(ctx, hostname) }()
	go func() { defer wg.Done(); set.TXT = lookupTXT(ctx, hostname) }()
	go func() { defer wg.Done(); set.NS = lookupNS(ctx, hostname) }()

	wg.Wait()
	return set
}

func aaaaFromAddrs(addrs []string) []string {
	out := make([]string, 0)
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() == nil {
			out = append(out, a)
		}
	}
	return out
}

func lookupCNAME(ctx context.Context, hostname string) []string {
	cname, err := net.DefaultResolver.LookupCNAME(ctx, hostname)
	if err != nil {
		return []string{}
	}
	trimmed := strings.TrimSuffix(cname, ".")
	if trimmed == "" || strings.EqualFold(trimmed, strings.TrimSuffix(hostname, ".")) {
		return []string{}
	}
	return []string{trimmed}
}

func lookupMX(ctx context.Context, hostname string) []string {
	records, err := net.DefaultResolver.LookupMX(ctx, hostname)
	out := make([]string, 0, len(records))
	if err != nil {
		return out
	}
	for _, mx := range records {
		out = append(out, fmt.Sprintf("%d %s", mx.Pref, strings.TrimSuffix(mx.Host, ".")))
	}
	return out
}

func lookupTXT(ctx context.Context, hostname string) []string {
	records, err := net.DefaultResolver.LookupTXT(ctx, hostname)
	if err != nil {
		return []string{}
	}
	return records
}

func lookupNS(ctx context.Context, hostname string) []string {
	records, err := net.DefaultResolver.LookupNS(ctx, hostname)
	out := make([]string, 0, len(records))
	if err != nil {
		return out
	}
	for _, ns := range records {
		out = append(out, strings.TrimSuffix(ns.Host, "."))
	}
	return out
}
```

Add `"net"`, `"strings"`, and `"sync"` to the import block at the top of `internal/server/handlers.go` (currently `"context"`, `"encoding/json"`, `"fmt"`, `"net/http"`, `"net/url"`, `"strconv"`, `"time"`, plus the two local packages) — alphabetized: `"net"` goes after `"fmt"` and before `"net/http"`; `"strings"` and `"sync"` go after `"strconv"`.

Modify `internal/server/router.go`: add one line inside the `/api` route group, after `r.Get("/heatmap/*", h.HeatmapByURL)`:

```go
r.Get("/dns-records/*", h.DNSRecordsByURL)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestDNSRecordsByURL -v`
Expected: PASS (requires network access — this test makes a real DNS lookup, same as `internal/dns/`'s existing tests).

- [ ] **Step 5: Run the full server test suite to check for regressions**

Run: `go test ./internal/server/... ./internal/db/...`
Expected: PASS (all existing tests still pass — this change added a new handler and import, touching no existing logic).

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers.go internal/server/router.go internal/server/handlers_test.go
git commit -m "feat(server): add GET /api/dns-records endpoint for AAAA/CNAME/MX/TXT/NS lookup"
```

---

### Task 2: Frontend — API client, types, and session cache

**Files:**
- Create: `web/src/api/dns-records.ts`
- Create: `web/src/lib/dns-records-cache.ts`

**Interfaces:**
- Consumes: nothing from earlier tasks (calls the new endpoint by URL string, no generated client).
- Produces: `DnsRecordsResponse` type, `fetchDnsRecords(url: string): Promise<DnsRecordsResponse>`, `getCachedDnsRecords(hostname: string): DnsRecordsResponse | null`, `setCachedDnsRecords(hostname: string, data: DnsRecordsResponse): void` — all consumed by Task 3.

- [ ] **Step 1: Create the API client module**

Create `web/src/api/dns-records.ts`:

```ts
export interface DnsRecordSet {
  aaaa: string[]
  cname: string[]
  mx: string[]
  txt: string[]
  ns: string[]
}

export interface DnsRecordsResponse {
  hostname: string
  resolved: boolean
  records?: DnsRecordSet
}

export async function fetchDnsRecords(url: string): Promise<DnsRecordsResponse> {
  const res = await fetch(`/api/dns-records/${encodeURIComponent(url)}`)
  if (!res.ok) throw new Error(`Failed to load DNS records: ${res.status}`)
  return res.json()
}
```

- [ ] **Step 2: Create the session cache module**

Create `web/src/lib/dns-records-cache.ts`:

```ts
import type { DnsRecordsResponse } from '@/api/dns-records'

const KEY_PREFIX = 'dns-records:'

export function getCachedDnsRecords(hostname: string): DnsRecordsResponse | null {
  try {
    const raw = sessionStorage.getItem(KEY_PREFIX + hostname)
    return raw ? JSON.parse(raw) as DnsRecordsResponse : null
  } catch {
    return null
  }
}

export function setCachedDnsRecords(hostname: string, data: DnsRecordsResponse): void {
  try {
    sessionStorage.setItem(KEY_PREFIX + hostname, JSON.stringify(data))
  } catch {
    // sessionStorage unavailable (e.g. browser privacy mode) — caching is a
    // nice-to-have, never required for the feature to function.
  }
}
```

- [ ] **Step 3: Type-check**

Run: `cd web && npx tsc -b`
Expected: no errors (these two files aren't imported anywhere yet, so this only confirms they're syntactically and type-correct in isolation).

- [ ] **Step 4: Commit**

```bash
git add web/src/api/dns-records.ts web/src/lib/dns-records-cache.ts
git commit -m "feat(web): add DNS records API client and session cache"
```

---

### Task 3: Frontend — wire the DNS Records panel into the per-URL history page

**Files:**
- Modify: `web/src/routes/results.$url.tsx`
- Modify: `web/src/index.css`

**Interfaces:**
- Consumes: `fetchDnsRecords` and `DnsRecordsResponse` from `web/src/api/dns-records.ts` (Task 2); `getCachedDnsRecords`/`setCachedDnsRecords` from `web/src/lib/dns-records-cache.ts` (Task 2).

- [ ] **Step 1: Add imports**

In `web/src/routes/results.$url.tsx`, add after the existing `fetchDnsServers` import (line 4):

```ts
import { fetchDnsRecords } from '../api/dns-records'
import type { DnsRecordSet, DnsRecordsResponse } from '../api/dns-records'
import { getCachedDnsRecords, setCachedDnsRecords } from '@/lib/dns-records-cache'
```

- [ ] **Step 2: Add the `DnsRecordsPanel` component**

Add after the `EmptyIcon` function (after its closing brace, currently line 44) and before `StatusDot`:

```tsx
const DNS_RECORD_LABELS: ReadonlyArray<readonly [keyof DnsRecordSet, string]> = [
  ['aaaa', 'AAAA'],
  ['cname', 'CNAME'],
  ['mx', 'MX'],
  ['txt', 'TXT'],
  ['ns', 'NS'],
]

function DnsRecordsPanel({ data, loading }: { data: DnsRecordsResponse | null; loading: boolean }) {
  if (loading) {
    return (
      <div className="dash-section dns-records-panel">
        <p className="dash-label">DNS Records</p>
        <div className="dns-records-grid">
          {DNS_RECORD_LABELS.map(([key]) => (
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

  const records = data.records

  return (
    <div className="dash-section dns-records-panel">
      <p className="dash-label">DNS Records</p>
      <div className="dns-records-grid">
        {DNS_RECORD_LABELS.map(([key, label]) => {
          const values = records[key]
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

- [ ] **Step 3: Add state and the fetch-with-cache effect**

In `URLHistoryPage`, add after the `dnsServerListLoading` state declaration (currently line 116):

```ts
  const [dnsRecords, setDnsRecords] = useState<DnsRecordsResponse | null>(null)
  const [dnsRecordsLoading, setDnsRecordsLoading] = useState(true)
```

Add after the `fetchDnsServers` effect (currently ending at line 142, before the `heatmapDnsServers` useMemo):

```ts
  // Fetches once per page visit; a sessionStorage cache (keyed by hostname,
  // not full URL) means revisiting this page later in the same tab skips
  // the network call entirely — these records don't change fast enough to
  // need re-fetching on every visit.
  useEffect(() => {
    const cached = getCachedDnsRecords(hostname)
    if (cached) {
      setDnsRecords(cached)
      setDnsRecordsLoading(false)
      return
    }
    setDnsRecordsLoading(true)
    fetchDnsRecords(url)
      .then(data => {
        setDnsRecords(data)
        setCachedDnsRecords(hostname, data)
      })
      .catch(() => setDnsRecords({ hostname, resolved: false }))
      .finally(() => setDnsRecordsLoading(false))
  }, [url, hostname])
```

- [ ] **Step 4: Render the panel**

In the JSX returned by `URLHistoryPage`, add the panel right after the `page-header` `<div>` (currently lines 165-168) and before the heatmap section's opening `{(dnsServerListLoading || ...`:

```tsx
      <div className="page-header">
        <h1 className="page-title">{hostname}</h1>
        <p className="page-subtitle">{url} · Last 7 days</p>
      </div>

      <DnsRecordsPanel data={dnsRecords} loading={dnsRecordsLoading} />

      {(dnsServerListLoading || heatmapDnsServers.length > 0) && (
```

- [ ] **Step 5: Add CSS**

In `web/src/index.css`, add after the `.dash-table-wrap` block (currently ending at line 767, just before the `/* ─── Server Status Table ─── */` comment):

```css
/* ─── DNS Records Panel ──────────────────────────────────────────────────── */

.dns-records-panel {
  @apply pb-2;
}

.dns-records-grid {
  @apply flex flex-wrap gap-x-8 gap-y-4 mt-1;
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
  @apply text-sm text-stone-muted mt-1;
}
```

- [ ] **Step 6: Type-check**

Run: `cd web && npx tsc -b`
Expected: no errors.

- [ ] **Step 7: Manual verification via dev server**

The Go server must be rebuilt to pick up Task 1's changes if it's running in Docker (the project's `docker-compose.yml` setup) — stale images don't auto-update. Rebuild and restart:

```bash
docker compose build server && docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d server
```

Then run the frontend dev server: `cd web && npm run dev`, and open `http://localhost:5173/results/<a-watched-url>` (via the history icon on the Overview compliance table, or by typing the URL-encoded path directly).

Check:
- A "DNS Records" panel appears between the page title and the year-heatmap section, showing AAAA/CNAME/MX/TXT/NS values (or `—` for empty types) for a real domain.
- Reload the page once — a loading skeleton briefly appears before the real values render (first fetch, no cache yet).
- Navigate back to Overview (`← Overview` link) and back into the same URL's history page again, in the same tab — the panel renders immediately with no loading skeleton (served from `sessionStorage`, confirm via browser DevTools Network tab showing no new `/api/dns-records/...` request).
- Open DevTools Network tab and confirm `/api/dns-records/...` returns 200 with the expected JSON shape.
- Visit a URL whose hostname doesn't resolve (or temporarily test against an obviously fake one) and confirm the panel shows "Unable to resolve DNS records for this host." instead of empty rows.

- [ ] **Step 8: Commit**

```bash
git add web/src/routes/results.\$url.tsx web/src/index.css
git commit -m "feat(web): show AAAA/CNAME/MX/TXT/NS records panel on per-URL history page"
```
