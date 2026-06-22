# Per-URL Scan History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an auditor view a single URL's DNS compliance history (chart + table) over the last 7 days, reached via a new icon button on the Overview table.

**Architecture:** Backend gains an additive `?since=` query param on the existing `GET /api/results/*url` endpoint (default: 7 days back). Frontend gains a new route `/results/$url` that fetches that endpoint, renders a multi-series step-line chart (one line per DNS server, using the already-installed-but-unused `@bklit` chart primitives under `web/src/components/charts/`) plus a filterable table of every individual scan result. Entry point is a new icon button in the Overview table's `URLGroupRow`.

**Tech Stack:** Go 1.26 / chi / GORM (backend), React 19 / TypeScript / TanStack Router / visx (frontend). New frontend dependency: `@bklit/line-chart` (shadcn-registry component, not an npm package — pulled via `npx shadcn@latest add`, registry already configured in `web/components.json`).

## Global Constraints

- Module name is `github.com/afif/dns-tracking` (per `go.mod`), despite the repo directory being `dns-compliance`.
- Backend tests: run `go test ./...` from repo root. `internal/db` tests use an in-memory SQLite store (`newTestStore` helper in `internal/db/postgres_test.go`) — no real Postgres needed.
- Frontend has **no test runner configured** (confirmed: no `vitest`/`jest` in `web/package.json`, no test scripts). Frontend verification in this plan means: `cd web && npm run build` (runs `tsc -b && vite build` — catches type errors) and manual browser verification via the running dev stack. Do not introduce a new test framework as part of this plan — out of scope.
- Frontend lint baseline is **84-85 pre-existing problems** (confirmed via `npm run lint --prefix web`) unrelated to this work — don't try to fix them, just don't add new ones in files you touch.
- Follow existing route-file conventions: helper components/functions live inline in the route file that uses them (see `web/src/routes/index.tsx`), not extracted into shared files, unless already shared (e.g. `web/src/api/results.ts`).
- The dev stack (Postgres + MinIO + server in Docker, `npm run dev` for the web frontend) may already be running from a previous session. Check with `docker compose -f docker-compose.yml -f docker-compose.dev.yml ps` and `curl -s http://localhost:8080/api/urls` before starting it again.

---

### Task 1: Backend — add `since` filtering to `ResultsByURL` at the store layer

**Files:**
- Modify: `internal/db/store.go:28`
- Modify: `internal/db/postgres.go:77-84`
- Modify: `internal/server/grpc.go:70`
- Modify: `internal/server/grpc_test.go:38-40`
- Test: `internal/db/postgres_test.go` (append)

**Interfaces:**
- Produces: `db.Store.ResultsByURL(ctx context.Context, urlValue string, since time.Time) ([]ScanResult, error)` — passing the zero value `time.Time{}` means "no lower bound" (returns everything for that URL, matching today's behavior).

- [ ] **Step 1: Write the failing tests in `internal/db/postgres_test.go`**

Append to the end of the file:

```go
func TestResultsByURL_FiltersSince(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	old := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now().AddDate(0, 0, -10),
	}
	recent := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: true, ScannedAt: time.Now(),
	}
	if err := s.InsertResult(ctx, old); err != nil {
		t.Fatalf("InsertResult old: %v", err)
	}
	if err := s.InsertResult(ctx, recent); err != nil {
		t.Fatalf("InsertResult recent: %v", err)
	}

	since := time.Now().AddDate(0, 0, -7)
	results, err := s.ResultsByURL(ctx, "https://example.com", since)
	if err != nil {
		t.Fatalf("ResultsByURL: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after since filter, got %d", len(results))
	}
	if !results[0].Compliant {
		t.Fatalf("expected the recent compliant result, got %+v", results[0])
	}
}

func TestResultsByURL_ZeroTimeReturnsAll(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	old := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Now().AddDate(-1, 0, 0),
	}
	if err := s.InsertResult(ctx, old); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	results, err := s.ResultsByURL(ctx, "https://example.com", time.Time{})
	if err != nil {
		t.Fatalf("ResultsByURL: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with zero-time (unbounded), got %d", len(results))
	}
}
```

- [ ] **Step 2: Run the new tests to verify they fail to compile (signature mismatch)**

Run: `go test ./internal/db/... -run TestResultsByURL -v`
Expected: FAIL — compile error, `too many arguments in call to s.ResultsByURL`.

- [ ] **Step 3: Update the `Store` interface in `internal/db/store.go:28`**

Change:
```go
	ResultsByURL(ctx context.Context, urlValue string) ([]ScanResult, error)
```
To:
```go
	ResultsByURL(ctx context.Context, urlValue string, since time.Time) ([]ScanResult, error)
```

- [ ] **Step 4: Update the implementation in `internal/db/postgres.go:77-84`**

Change:
```go
func (s *postgresStore) ResultsByURL(ctx context.Context, urlValue string) ([]ScanResult, error) {
	var results []ScanResult
	err := s.db.WithContext(ctx).
		Where("url_value = ?", urlValue).
		Preload("DNSServer").
		Order("scanned_at desc").
		Find(&results).Error
	return results, err
}
```
To:
```go
func (s *postgresStore) ResultsByURL(ctx context.Context, urlValue string, since time.Time) ([]ScanResult, error) {
	var results []ScanResult
	err := s.db.WithContext(ctx).
		Where("url_value = ? AND scanned_at >= ?", urlValue, since).
		Preload("DNSServer").
		Order("scanned_at desc").
		Find(&results).Error
	return results, err
}
```

- [ ] **Step 5: Fix the call site in `internal/server/grpc.go:70`**

Change:
```go
			results, err := s.store.ResultsByURL(ctx, r.Url)
```
To:
```go
			// time.Time{} = no lower bound; we just need the row inserted above.
			results, err := s.store.ResultsByURL(ctx, r.Url, time.Time{})
```
(`time` is already imported in this file — used a few lines above via `time.Unix(r.Timestamp, 0)`.)

- [ ] **Step 6: Fix the mock signature in `internal/server/grpc_test.go:38-40`**

Change:
```go
func (m *mockStore) ResultsByURL(_ context.Context, _ string) ([]db.ScanResult, error) {
	return m.insertedResults, nil
}
```
To:
```go
func (m *mockStore) ResultsByURL(_ context.Context, _ string, _ time.Time) ([]db.ScanResult, error) {
	return m.insertedResults, nil
}
```
(`time` is already imported in this file.)

- [ ] **Step 7: Run the full backend test suite**

Run: `go test ./...`
Expected: PASS for `internal/db` and `internal/server` (note: `internal/dns` makes real network calls and may fail/skip if offline — that's pre-existing and unrelated to this change).

- [ ] **Step 8: Commit**

```bash
git add internal/db/store.go internal/db/postgres.go internal/server/grpc.go internal/server/grpc_test.go internal/db/postgres_test.go
git commit -m "feat(db): add since filter to ResultsByURL"
```

---

### Task 2: Backend — `?since=` query param on the HTTP handler, with percent-decoding fix

**Context:** `chi.URLParam(r, "*")` on a wildcard route returns the **raw, still-percent-encoded** path segment — it does NOT decode `%2F` back to `/`. This was verified empirically: a request to `/api/results/https%3A%2F%2Fexample.com%2F` makes `chi.URLParam(r, "*")` return the literal string `"https%3A%2F%2Fexample.com%2F"`, not `"https://example.com/"`. Since the frontend (Task 3) will call this endpoint with `encodeURIComponent(url)`, the handler must explicitly `url.PathUnescape` the captured value before using it. This is a latent bug in the existing handler that has gone unnoticed because nothing currently calls this endpoint with a slash-containing URL in its path.

**Files:**
- Modify: `internal/server/handlers.go:1-13` (imports), `:185-193` (handler)
- Modify: `internal/server/handlers_test.go:77-85` (mock), append new tests

**Interfaces:**
- Consumes: `db.Store.ResultsByURL(ctx, urlValue string, since time.Time)` from Task 1.
- Produces: `GET /api/results/*url` now accepts an optional `?since=<RFC3339>` query param; defaults to `now - 7 days` when absent or unparseable. Percent-encoded URLs in the path (e.g. `https%3A%2F%2Fexample.com`) are now correctly decoded.

- [ ] **Step 1: Write the failing tests in `internal/server/handlers_test.go`**

Append to the end of the file:

```go
func TestResultsByURL_DefaultWindow(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", ScannedAt: time.Now().AddDate(0, 0, -10)},
		{ID: 2, URLValue: "https://example.com", ScannedAt: time.Now()},
	}}
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result within default 7-day window, got %d", len(results))
	}
	if results[0].ID != 2 {
		t.Fatalf("expected the recent result (id=2), got id=%d", results[0].ID)
	}
}

func TestResultsByURL_ExplicitSince(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", ScannedAt: time.Now().AddDate(0, 0, -10)},
		{ID: 2, URLValue: "https://example.com", ScannedAt: time.Now()},
	}}
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com?since="+since, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 2 {
		t.Fatalf("expected 2 results within 30-day window, got %d", len(results))
	}
}

func TestResultsByURL_PercentEncodedSlashes(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com/", ScannedAt: time.Now()},
	}}
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results/https%3A%2F%2Fexample.com%2F", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}
```

Add `"net/url"` to the test file's import block (it currently imports `bytes`, `context`, `encoding/json`, `net/http`, `net/http/httptest`, `testing`, `time`, plus the project packages and chi).

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `go test ./internal/server/... -run TestResultsByURL -v`
Expected: FAIL — `fullMockStore` doesn't implement the new 3-arg signature yet (compile error), or once that's fixed, the handler doesn't yet apply `since`/decode the path.

- [ ] **Step 3: Update `fullMockStore.ResultsByURL` in `internal/server/handlers_test.go:77-85`**

Change:
```go
func (m *fullMockStore) ResultsByURL(_ context.Context, u string) ([]db.ScanResult, error) {
	var out []db.ScanResult
	for _, r := range m.results {
		if r.URLValue == u {
			out = append(out, r)
		}
	}
	return out, nil
}
```
To:
```go
func (m *fullMockStore) ResultsByURL(_ context.Context, u string, since time.Time) ([]db.ScanResult, error) {
	var out []db.ScanResult
	for _, r := range m.results {
		if r.URLValue == u && !r.ScannedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Update imports in `internal/server/handlers.go:1-13`**

Change:
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/go-chi/chi/v5"
)
```
To:
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/go-chi/chi/v5"
)
```

- [ ] **Step 5: Update the handler in `internal/server/handlers.go:185-193`**

Change:
```go
func (h *Handlers) ResultsByURL(w http.ResponseWriter, r *http.Request) {
	urlValue := chi.URLParam(r, "*")
	results, err := h.store.ResultsByURL(r.Context(), urlValue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}
```
To:
```go
func (h *Handlers) ResultsByURL(w http.ResponseWriter, r *http.Request) {
	urlValue, err := url.PathUnescape(chi.URLParam(r, "*"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}

	since := time.Now().AddDate(0, 0, -7)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			since = parsed
		}
	}

	results, err := h.store.ResultsByURL(r.Context(), urlValue, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}
```

- [ ] **Step 6: Run the tests again to verify they pass**

Run: `go test ./internal/server/... -run TestResultsByURL -v`
Expected: PASS (all three new tests).

- [ ] **Step 7: Run the full backend suite**

Run: `go test ./...`
Expected: PASS (same caveat about `internal/dns` needing network access as Task 1).

- [ ] **Step 8: Commit**

```bash
git add internal/server/handlers.go internal/server/handlers_test.go
git commit -m "feat(server): add since param and fix percent-decoding on ResultsByURL handler"
```

---

### Task 3: Frontend — API client function `fetchResultsByUrl`

**Files:**
- Modify: `web/src/api/results.ts`

**Interfaces:**
- Consumes: `GET /api/results/*url?since=` from Task 2.
- Produces: `fetchResultsByUrl(url: string, sinceDays?: number): Promise<ScanResult[]>` — used by Task 5.

- [ ] **Step 1: Add the function to `web/src/api/results.ts`**

Append after the existing `fetchResults` function (keep `groupResults`/`lastScanTime` below it, matching current file order):

```ts
export async function fetchResultsByUrl(url: string, sinceDays = 7): Promise<ScanResult[]> {
  const since = new Date(Date.now() - sinceDays * 24 * 60 * 60 * 1000).toISOString()
  const res = await fetch(`/api/results/${encodeURIComponent(url)}?since=${encodeURIComponent(since)}`)
  if (!res.ok) throw new Error(`Failed to load results: ${res.status}`)
  return res.json()
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc -b --noEmit`
Expected: exits 0, no new errors. (Functional verification happens in Task 5 once this is actually called from a route.)

- [ ] **Step 3: Commit**

```bash
git add web/src/api/results.ts
git commit -m "feat(web): add fetchResultsByUrl API client function"
```

---

### Task 4: Frontend — install the `@bklit/line-chart` chart component

**Context:** This is a shadcn-registry component (copy-paste source, not an npm package). The `@bklit` registry is already configured in `web/components.json`. Running the install command pulls `line-chart.tsx`, `line.tsx`, `grid.tsx`, `x-axis.tsx`, the `tooltip/` directory, and shared files like `chart-context.tsx` into `web/src/components/charts/` — some of which already exist there (from an earlier, currently-unused `live-line-chart.tsx` scaffold). This is safe to overwrite: nothing in the app currently imports anything from `web/src/components/charts/` (verified via `grep -rn "LiveLineChart\|components/charts" web/src --include="*.tsx"` returning no hits outside that directory itself), and the registry's current `chart-context.tsx` is a strict superset of what's already vendored (diffed: only additions, no removals).

**Files:**
- Create (via CLI, not manually): multiple files under `web/src/components/charts/` — exact set determined by the registry, not enumerated here.

**Interfaces:**
- Produces: `LineChart` (default export, `web/src/components/charts/line-chart.tsx`), `Line` (default export, `.../line.tsx`), `Grid` (default export, `.../grid.tsx`), `XAxis` (default export, `.../x-axis.tsx`), `ChartTooltip` (named export, `.../tooltip/index.ts`) — all consumed by Task 6.

- [ ] **Step 1: Confirm nothing currently depends on the existing chart scaffold**

Run: `grep -rn "LiveLineChart\|from '@/components/charts\|from '../components/charts" web/src --include="*.tsx" | grep -v "web/src/components/charts/"`
Expected: no output (confirms it's safe to let the installer overwrite shared files).

- [ ] **Step 2: Run the installer**

Run: `cd web && npx shadcn@latest add @bklit/line-chart -y -o`
Expected: exits 0; output lists files written under `src/components/charts/`.

- [ ] **Step 3: Verify the key files landed**

Run: `ls web/src/components/charts/line-chart.tsx web/src/components/charts/line.tsx web/src/components/charts/grid.tsx web/src/components/charts/x-axis.tsx web/src/components/charts/tooltip/index.ts`
Expected: all five paths exist (no "No such file" errors).

- [ ] **Step 4: Verify the build still succeeds**

Run: `cd web && npm run build`
Expected: exits 0. (If `@visx/curve`, `@visx/shape`, or `motion` are missing as direct dependencies, the installer should have added them to `package.json` automatically — check `git diff web/package.json` if the build fails on a missing-module error.)

- [ ] **Step 5: Commit**

```bash
git add web/components.json web/package.json web/package-lock.json web/src/components/charts/
git commit -m "feat(web): install @bklit/line-chart chart components"
```

---

### Task 5: Frontend — new route `/results/$url` (header, data, filter bar, table — no chart yet)

**Context:** Splitting the chart out into Task 6 means this task already produces a complete, useful, shippable page (full per-scan table with filtering) even if the chart integration in Task 6 hits friction.

**Important encoding detail (verified against TanStack Router source, `@tanstack/router-core`):** for a *named* path param like `$url` (not a `$splat`), `<Link params={{ url }}>` calls `encodeURIComponent` on the value internally, and `Route.useParams()` calls `decodeURIComponent` internally when reading it back. **Do not double-encode** — pass the raw URL string as the param value when linking (Task 7), and read `Route.useParams().url` directly as the already-decoded raw URL string in this route (no manual `decodeURIComponent` call).

**Files:**
- Create: `web/src/routes/results.$url.tsx`

**Interfaces:**
- Consumes: `fetchResultsByUrl(url: string, sinceDays?: number)` from Task 3; `ScanResult` type from `web/src/api/types.ts`; `ToggleGroup`/`ToggleGroupItem` from `@/components/animate-ui/components/radix/toggle-group` (already used in `index.tsx`).
- Produces: route `/results/$url`, registered in `routeTree.gen.ts` automatically once the dev server or build runs.

- [ ] **Step 1: Create `web/src/routes/results.$url.tsx`**

```tsx
import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowLeftIcon } from 'lucide-react'
import { fetchResultsByUrl } from '../api/results'
import type { ScanResult } from '../api/types'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'

export const Route = createFileRoute('/results/$url')({ component: URLHistoryPage })

type StatusFilter = 'all' | 'violations' | 'compliant'

const DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

function EmptyIcon() {
  return (
    <svg className="empty-icon" width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <rect x="8" y="4" width="24" height="32" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M32 4L40 12V36C40 37.1 39.1 38 38 38H32" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M40 12H32V4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 18H26M14 24H22" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

function StatusDot({ compliant }: { compliant: boolean }) {
  return (
    <span className="status-dot-label">
      <span className={`status-dot ${compliant ? 'dot-compliant' : 'dot-violation'}`} aria-hidden="true" />
      <span className={compliant ? 'label-compliant' : 'label-violation'}>
        {compliant ? 'Compliant' : 'Violation'}
      </span>
    </span>
  )
}

function HistorySkeletonRows() {
  return (
    <>
      {[180, 140, 220, 160].map((w, i) => (
        <tr key={i} className="skeleton-row">
          <td className="col-domain"><span className="skeleton" style={{ width: w, height: 14 }} /></td>
          <td className="col-status"><span className="skeleton" style={{ width: 100, height: 20, borderRadius: 4 }} /></td>
          <td className="col-ip" />
          <td className="col-evidence" />
          <td className="col-last-scanned" />
        </tr>
      ))}
    </>
  )
}

function URLHistoryPage() {
  const { url } = Route.useParams()
  const hostname = useMemo(() => { try { return new URL(url).hostname } catch { return url } }, [url])

  const [results, setResults] = useState<ScanResult[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [dnsFilter, setDnsFilter] = useState<string>('all')

  const load = useCallback(async () => {
    try {
      setError(null)
      const raw = await fetchResultsByUrl(url)
      setResults(raw)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load results')
    } finally {
      setLoading(false)
    }
  }, [url])

  useEffect(() => { load() }, [load])

  const dnsServers = useMemo(() => {
    const seen = new Map<string, string>()
    for (const r of results) seen.set(r.dns_server.name, r.dns_server.name)
    return Array.from(seen.values()).sort()
  }, [results])

  const filtered = useMemo(() => {
    return results.filter(r => {
      if (dnsFilter !== 'all' && r.dns_server.name !== dnsFilter) return false
      if (statusFilter === 'violations' && r.compliant) return false
      if (statusFilter === 'compliant' && !r.compliant) return false
      return true
    })
  }, [results, statusFilter, dnsFilter])

  return (
    <>
      <Link to="/" className="back-link">
        <ArrowLeftIcon className="back-link-icon" />
        Overview
      </Link>

      <div className="page-header">
        <h1 className="page-title">{hostname}</h1>
        <p className="page-subtitle">{url} · Last 7 days</p>
      </div>

      <div className="filter-bar">
        <span className="filter-label" id="status-label">Status</span>
        <ToggleGroup
          type="single"
          value={statusFilter}
          onValueChange={v => { if (v) setStatusFilter(v as StatusFilter) }}
          variant="outline"
          aria-labelledby="status-label"
        >
          <ToggleGroupItem value="all">All</ToggleGroupItem>
          <ToggleGroupItem value="violations">Violations</ToggleGroupItem>
          <ToggleGroupItem value="compliant">Compliant</ToggleGroupItem>
        </ToggleGroup>

        {dnsServers.length > 1 && (
          <>
            <span className="filter-label" id="dns-label">DNS Server</span>
            <select
              className="filter-select"
              value={dnsFilter}
              onChange={e => setDnsFilter(e.target.value)}
              aria-labelledby="dns-label"
            >
              <option value="all">All servers</option>
              {dnsServers.map(name => (
                <option key={name} value={name}>{name}</option>
              ))}
            </select>
          </>
        )}
      </div>

      <div className="results-wrap">
        {error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !loading && results.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No results yet</p>
            <p className="empty-body">No scans for this domain in the last 7 days.</p>
          </div>
        ) : (
          <table className="results-table" aria-label={`Scan history for ${hostname}`}>
            <thead>
              <tr>
                <th className="col-domain" scope="col">DNS Server</th>
                <th className="col-status" scope="col">Status</th>
                <th className="col-ip" scope="col">Resolved IP</th>
                <th className="col-evidence" scope="col">Evidence</th>
                <th className="col-last-scanned" scope="col">Scanned At</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <HistorySkeletonRows />
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={5}>
                    <div className="empty-state" style={{ padding: '3rem 0' }}>
                      <p className="empty-heading">No results match the current filters</p>
                    </div>
                  </td>
                </tr>
              ) : (
                filtered.map(r => (
                  <tr key={r.id} className={!r.compliant ? 'violation-row' : ''}>
                    <td className="col-domain"><span className="dns-name">{r.dns_server.name}</span></td>
                    <td className="col-status"><StatusDot compliant={r.compliant} /></td>
                    <td className="col-ip">
                      {r.resolved_ip ? <span className="ip-value">{r.resolved_ip}</span> : <span className="empty-cell" aria-label="Not resolved">—</span>}
                    </td>
                    <td className="col-evidence">
                      {r.screenshot_url ? (
                        <a href={r.screenshot_url} target="_blank" rel="noopener noreferrer" className="screenshot-link" aria-label={`View screenshot for ${r.dns_server.name}`}>
                          View screenshot
                        </a>
                      ) : <span className="empty-cell" aria-label="No screenshot">—</span>}
                    </td>
                    <td className="col-last-scanned">
                      {r.scanned_at ? <span>{DATE_FMT.format(new Date(r.scanned_at))}</span> : <span className="empty-cell">—</span>}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </div>
    </>
  )
}
```

- [ ] **Step 2: Add the `.back-link` styles to `web/src/index.css`**

Insert after the `.btn-row-delete:focus-visible` block (around line 1316-1319):

```css
.back-link {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-1);
  font-size: 0.8125rem;
  font-weight: 500;
  color: var(--stone-muted);
  text-decoration: none;
  margin-bottom: var(--sp-3);
  transition: color var(--duration-fast) var(--ease-out);
}

.back-link:hover {
  color: var(--stone-text);
}

.back-link-icon {
  width: 14px;
  height: 14px;
}
```

- [ ] **Step 3: Build to regenerate the route tree and type-check**

Run: `cd web && npm run build`
Expected: exits 0. `web/src/routeTree.gen.ts` is auto-updated (via the `TanStackRouterVite` Vite plugin) to include the new `/results/$url` route — do not edit that file by hand.

- [ ] **Step 4: Manually verify in the browser**

With the backend running (`docker compose -f docker-compose.yml -f docker-compose.dev.yml ps` to check, or start it per `CLAUDE.md`) and `cd web && npm run dev` running, navigate directly to a URL like `http://localhost:5173/results/https%3A%2F%2Fexample.com` (percent-encoded form of a URL that has scan results — check `curl -s http://localhost:8080/api/urls` for a real one) and confirm:
- Page renders without console errors.
- "Overview" back-link navigates to `/`.
- Table shows rows; Status/DNS Server filters narrow them.
- If that URL has no results in the last 7 days, the "No results yet" empty state renders instead of an error.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/results.\$url.tsx web/src/index.css web/src/routeTree.gen.ts
git commit -m "feat(web): add per-URL scan history page (table + filters, no chart yet)"
```

---

### Task 6: Frontend — add the compliance-over-time chart to the history page

**Files:**
- Modify: `web/src/routes/results.$url.tsx`

**Interfaces:**
- Consumes: `LineChart`, `Line`, `Grid`, `XAxis`, `ChartTooltip` from Task 4's installed components; `decimateTimeSeries` from `web/src/components/charts/decimate-time-series.ts` (pre-existing, previously unused).
- Produces: a chart section rendered above the filter bar in `URLHistoryPage`.

- [ ] **Step 1: Add imports to `web/src/routes/results.$url.tsx`**

Add below the existing imports:

```tsx
import { curveStepAfter } from '@visx/curve'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'
import { Grid } from '@/components/charts/grid'
import { XAxis } from '@/components/charts/x-axis'
import { ChartTooltip } from '@/components/charts/tooltip'
import { decimateTimeSeries } from '@/components/charts/decimate-time-series'
```

- [ ] **Step 2: Add the pivot helper and point cap, above the `URLHistoryPage` function**

```tsx
// Caps rendered chart points regardless of scan frequency — decimateTimeSeries
// (LTTB) below picks the most visually significant points rather than every
// Nth one, so brief compliance flips are more likely to survive than with
// naive uniform sampling or a frequency-coupled bucket size (e.g. "daily").
const MAX_CHART_POINTS = 240

type ChartPoint = { date: Date; [dnsServerName: string]: number | Date }

function pivotByRun(results: ScanResult[]): ChartPoint[] {
  const runs = new Map<number, { date: Date; values: Record<string, number> }>()
  for (const r of results) {
    const existing = runs.get(r.scan_run_id)
    const values = existing?.values ?? {}
    values[r.dns_server.name] = r.compliant ? 1 : 0
    const date = existing?.date ?? new Date(r.scanned_at)
    runs.set(r.scan_run_id, { date, values })
  }
  return Array.from(runs.values())
    .sort((a, b) => a.date.getTime() - b.date.getTime())
    .map(({ date, values }) => ({ date, ...values }))
}
```

- [ ] **Step 3: Compute chart data inside `URLHistoryPage`, after the `filtered` useMemo**

```tsx
  const chartData = useMemo(
    () => decimateTimeSeries(pivotByRun(filtered), MAX_CHART_POINTS, dnsServers),
    [filtered, dnsServers],
  )
```

- [ ] **Step 4: Render the chart, inserted just before the `<div className="filter-bar">` block**

```tsx
      {!loading && !error && results.length > 0 && (
        <div className="dash-section">
          <LineChart data={chartData} xDataKey="date" aspectRatio="16 / 6">
            <Grid horizontal />
            <XAxis />
            {dnsServers.map((name, i) => (
              <Line
                key={name}
                dataKey={name}
                curve={curveStepAfter}
                stroke={`var(--chart-${(i % 5) + 1})`}
              />
            ))}
            <ChartTooltip />
          </LineChart>
        </div>
      )}

```

(Each `<Line>` uses `curveStepAfter` instead of the component's default `curveNatural` — a smooth spline would visually imply fractional/intermediate compliance states between 0 and 1, which don't exist for this binary data. Colors cycle through the existing achromatic `--chart-1` through `--chart-5` tokens already defined in `web/src/index.css:87-91`, consistent with the design system's one-accent-color rule — these are grayscale steps, not a new hue.)

- [ ] **Step 5: Build and verify**

Run: `cd web && npm run build`
Expected: exits 0.

- [ ] **Step 6: Manually verify in the browser**

Reload `/results/<url>` for a URL with results across more than one DNS server. Confirm:
- The chart renders above the filter bar with one step-line per DNS server.
- Lines are visually distinguishable (different gray shades).
- Hovering shows the tooltip (from `ChartTooltip`).
- Toggling the Status/DNS Server filters updates the chart, not just the table (since `chartData` derives from `filtered`).
- No console errors (in particular, watch for anything related to `useChartConfig`/`ChartConfigProvider` — if the tooltip throws on missing context, report this and fall back to omitting `<ChartTooltip />` for this iteration rather than guessing at a fix).

- [ ] **Step 7: Commit**

```bash
git add web/src/routes/results.\$url.tsx
git commit -m "feat(web): add compliance-over-time chart to per-URL history page"
```

---

### Task 7: Frontend — entry-point icon button on the Overview table

**Files:**
- Modify: `web/src/routes/index.tsx` (the `URLGroupRow` function)
- Modify: `web/src/index.css`

**Interfaces:**
- Consumes: route `/results/$url` from Task 5.
- Produces: a clickable history icon in each Overview table row, independent of the existing expand/collapse chevron and the hostname's hover-preview link.

- [ ] **Step 1: Add the `Link` and icon imports to `web/src/routes/index.tsx`**

Find the existing `createFileRoute` import line near the top of the file. Change:
```tsx
import { createFileRoute } from '@tanstack/react-router'
```
To:
```tsx
import { createFileRoute, Link } from '@tanstack/react-router'
```
And add a new import line:
```tsx
import { HistoryIcon } from 'lucide-react'
```

- [ ] **Step 2: Add the icon button in `URLGroupRow`**

Find this block inside `URLGroupRow` (in the `<tr className="url-row">`):
```tsx
        <td className="col-ip" />
        <td className="col-evidence" />
        <td className="col-last-scanned">
```
Change to:
```tsx
        <td className="col-ip" />
        <td className="col-evidence">
          <Link
            to="/results/$url"
            params={{ url }}
            className="btn-row-history"
            aria-label={`View history for ${hostname}`}
            onClick={e => e.stopPropagation()}
          >
            <HistoryIcon className="btn-row-history-icon" />
          </Link>
        </td>
        <td className="col-last-scanned">
```

(`url` here is the destructured `group.url` already in scope at the top of `URLGroupRow` — `const { violationCount, totalCount, hostname, url } = group`. Pass it **raw, not `encodeURIComponent`'d** — TanStack Router's `Link` encodes named params internally; double-encoding would corrupt the round trip back through `Route.useParams()` in Task 5.)

- [ ] **Step 3: Add button styles to `web/src/index.css`**

Insert near the `.btn-row-delete` rules (after the `.back-link-icon` block added in Task 5):

```css
.btn-row-history {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--stone-muted);
  background: transparent;
  border: none;
  padding: 2px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: color var(--duration-fast) var(--ease-out);
}

.btn-row-history:hover {
  color: var(--stone-text);
}

.btn-row-history:focus-visible {
  outline: 2px solid var(--ink);
  outline-offset: 2px;
}

.btn-row-history-icon {
  width: 16px;
  height: 16px;
}
```

- [ ] **Step 4: Build and verify**

Run: `cd web && npm run build`
Expected: exits 0.

- [ ] **Step 5: Manually verify in the browser**

On `/`, in the "Compliance Results" table, confirm:
- Each row shows a small history icon in the evidence column.
- Clicking it navigates to `/results/<that url>`, landing on the correct domain's history page.
- Clicking it does **not** also toggle the row's inline expand/collapse.
- The existing chevron-click-to-expand and hostname hover-preview behaviors are unchanged.

- [ ] **Step 6: Commit**

```bash
git add web/src/routes/index.tsx web/src/index.css
git commit -m "feat(web): add history icon entry point to Overview compliance table"
```

---

### Task 8: Final end-to-end verification

**Files:** none (verification only)

- [ ] **Step 1: Full backend test suite**

Run: `go test ./...`
Expected: PASS (modulo the pre-existing `internal/dns` network-dependent tests).

- [ ] **Step 2: Full frontend build**

Run: `cd web && npm run build`
Expected: exits 0.

- [ ] **Step 3: Lint regression check**

Run: `cd web && npm run lint 2>&1 | tail -5`
Expected: problem count is ≤ the pre-change baseline of 84-85 (re-confirm the current baseline with `git stash && npm run lint 2>&1 | tail -5 && git stash pop` if unsure) — i.e. no *new* lint errors introduced by this feature's files.

- [ ] **Step 4: Full manual walkthrough**

With the Docker stack and `npm run dev` both running:
1. Trigger a scan (or use existing data) so at least one URL has results from more than one DNS server.
2. From `/`, click a history icon → lands on `/results/<url>`.
3. Confirm chart + table both render, filters affect both.
4. Click "Overview" back-link → returns to `/`.
5. Confirm the Overview page's own table/filter/expand behavior (built earlier, unrelated to this feature) still works unchanged.

- [ ] **Step 5: Report**

Summarize what was built, any deviations from the plan (e.g. if the `ChartTooltip` context issue from Task 6 Step 6 came up and was worked around), and confirm the design spec's goals (`docs/superpowers/specs/2026-06-22-per-url-scan-history-design.md`) are met.
