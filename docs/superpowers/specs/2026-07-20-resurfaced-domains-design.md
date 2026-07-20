# Resurfaced Domains Design

**Date:** 2026-07-20
**Branch:** main (design phase — not yet branched)

## Overview

A **resurfaced domain** is one whose most recent scan flipped from compliant (blocked) back to non-compliant (resolving again), per DNS server. This is the highest-signal metric this tool can produce: it means an enforcement order that was working has stopped working — the block lapsed, was reverted, or never held. Unlike a domain that's simply "still violating" (never got blocked in the first place), a resurfaced domain represents a regression from a known-good state, which is operationally more urgent.

This spec adds it as a first-class, persisted, queryable metric — not an ephemeral client-side computation — so it's visible regardless of who's watching when it happens (scheduled scans included, not just manually-triggered ones).

## Domain Semantics Reminder

DNS fails = compliant (block is working). DNS resolves = violation (site still up). A domain "resurfaces" when, for a given `(url, dns_server)` pair, the previous scan had `compliant = true` and the latest scan has `compliant = false`.

## Relationship to the Existing "Newly Violating Domains" Feature

`scan-results.tsx` already has a conceptually identical signal ("Newly Violating Domains", see `2026-06-30-additional-stats-design.md`), but it's computed client-side: `handleScanSelected` snapshots current results into `sessionStorage` before a manually-triggered scan, then diffs against that baseline once the scan completes. That only catches regressions within one scan session someone happened to trigger and stay on the page for — it's invisible for the scheduled/automatic sweep (`StartScheduler`), which is when most scans actually happen.

**Decided: replace it.** The sessionStorage baseline-diff in `__root.tsx`/`scan-results.tsx` is removed once this endpoint exists — it's strictly less capable and would just be duplicate logic to maintain. `scan-results.tsx` calls `fetchResurfacedDomains()` instead (see Task 7 below); its summary stat and domain-card "New" badge stay, just backed by the real endpoint rather than a client-side diff.

**Decided: no time window.** A resurfaced domain stays flagged as long as its most recent scan is still a violation and the scan before that was compliant — however long ago that transition happened. It only drops off the list once the domain gets re-blocked (a new compliant scan lands). No 7/30-day expiry.

## Backend

### Model (`internal/db/models.go`)

Detection still happens per `(url, dns_server)` — that's the actual unit of "compliant → violation" — but the response rolls up to one row per domain, with the affected servers nested underneath (**decided: roll up to one row per domain**, not one row per domain+server):

```go
// ResurfacedDomain is a domain that flipped from compliant to violating on
// at least one DNS server — see "Resurfaced Domains Design". AffectedServers
// holds the (possibly partial) set of servers where the flip happened; a
// domain resurfacing everywhere at once still gets one ResurfacedDomain row.
type ResurfacedDomain struct {
	URLValue        string                  `json:"url"`
	ResurfacedAt    time.Time               `json:"resurfaced_at"`    // most recent flip across affected servers
	AffectedServers []ResurfacedServerEntry `json:"affected_servers"`
}

type ResurfacedServerEntry struct {
	DNSServerID     uint      `json:"dns_server_id"`
	DNSServerName   string    `json:"dns_server_name"`
	ISP             string    `json:"isp"`
	LastCompliantAt time.Time `json:"last_compliant_at"` // when the prior (still-blocked) scan ran
	ResurfacedAt    time.Time `json:"resurfaced_at"`      // when the violating scan ran
}
```

### Store interface (`internal/db/store.go`)

Add to the existing `ISPStatsStore` sub-interface — it's the same aggregate family as `ISPStats`/`ISPTrend`/`NationalTrend`, all reading `scan_results` joined to `dns_servers`/`department_urls`:

```go
ResurfacedDomains(ctx context.Context) ([]ResurfacedDomain, error)
ResurfacedDomainsForDepartment(ctx context.Context, departmentID uint) ([]ResurfacedDomain, error)
```

One global list, not split per-ISP — the ISP detail page can filter the (already correctly department-scoped) list down to its own ISP client-side, same as it already does with plain array data elsewhere. Simpler than adding a parameterized endpoint for what's expected to be a small result set (bounded by watchlist size, not scan-result history size).

### Implementation (`internal/db/postgres.go`)

Reuses the "latest per group" subquery idiom already established in `ispStats` (`MAX(scanned_at)` joined back), extended one step further to also find the *second*-latest row per group — avoids window functions, which the codebase has previously steered away from for SQLite-test-driver portability reasons (see `ispComplianceTiming`'s comment).

```go
func (s *postgresStore) resurfacedDomains(ctx context.Context, departmentID *uint) ([]ResurfacedDomain, error) {
	// Latest scanned_at per (url_value, dns_server_id).
	latest := s.db.Model(&ScanResult{}).
		Select("url_value, dns_server_id, MAX(scanned_at) as max_scanned_at").
		Group("url_value, dns_server_id")

	// Latest scanned_at per group that's strictly before the latest one —
	// i.e. the previous scan.
	previous := s.db.Table("scan_results").
		Select("scan_results.url_value, scan_results.dns_server_id, MAX(scan_results.scanned_at) as prev_scanned_at").
		Joins("JOIN (?) AS latest ON scan_results.url_value = latest.url_value AND scan_results.dns_server_id = latest.dns_server_id AND scan_results.scanned_at < latest.max_scanned_at", latest).
		Group("scan_results.url_value, scan_results.dns_server_id")

	type row struct {
		URLValue        string
		DNSServerID     uint
		DNSServerName   string
		ISP             string
		LastCompliantAt time.Time
		ResurfacedAt    time.Time
	}
	q := s.db.WithContext(ctx).
		Table("scan_results AS latest_sr").
		Select(`latest_sr.url_value,
            latest_sr.dns_server_id,
            dns_servers.name AS dns_server_name,
            dns_servers.isp,
            prev_sr.scanned_at AS last_compliant_at,
            latest_sr.scanned_at AS resurfaced_at`).
		Joins("JOIN (?) AS latest ON latest_sr.url_value = latest.url_value AND latest_sr.dns_server_id = latest.dns_server_id AND latest_sr.scanned_at = latest.max_scanned_at", latest).
		Joins("JOIN (?) AS prev ON latest_sr.url_value = prev.url_value AND latest_sr.dns_server_id = prev.dns_server_id", previous).
		Joins("JOIN scan_results AS prev_sr ON prev_sr.url_value = prev.url_value AND prev_sr.dns_server_id = prev.dns_server_id AND prev_sr.scanned_at = prev.prev_scanned_at").
		Joins("JOIN dns_servers ON dns_servers.id = latest_sr.dns_server_id").
		Where("latest_sr.compliant = false AND prev_sr.compliant = true")
	if departmentID != nil {
		q = q.Joins("JOIN department_urls ON department_urls.url_id = latest_sr.url_id AND department_urls.department_id = ? AND department_urls.enabled = true", *departmentID)
	}

	var rows []row
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}

	// Roll up per-server rows into one entry per domain.
	byURL := make(map[string]*ResurfacedDomain)
	order := make([]string, 0)
	for _, r := range rows {
		d, ok := byURL[r.URLValue]
		if !ok {
			d = &ResurfacedDomain{URLValue: r.URLValue}
			byURL[r.URLValue] = d
			order = append(order, r.URLValue)
		}
		d.AffectedServers = append(d.AffectedServers, ResurfacedServerEntry{
			DNSServerID: r.DNSServerID, DNSServerName: r.DNSServerName, ISP: r.ISP,
			LastCompliantAt: r.LastCompliantAt, ResurfacedAt: r.ResurfacedAt,
		})
		if r.ResurfacedAt.After(d.ResurfacedAt) {
			d.ResurfacedAt = r.ResurfacedAt
		}
	}

	out := make([]ResurfacedDomain, len(order))
	for i, url := range order {
		out[i] = *byURL[url]
	}
	return out, nil
}

func (s *postgresStore) ResurfacedDomains(ctx context.Context) ([]ResurfacedDomain, error) {
	return s.resurfacedDomains(ctx, nil)
}

func (s *postgresStore) ResurfacedDomainsForDepartment(ctx context.Context, departmentID uint) ([]ResurfacedDomain, error) {
	return s.resurfacedDomains(ctx, &departmentID)
}
```

This is a real query (not Go-side row-pulling like `ispComplianceTiming`) because — unlike that function's `MIN()`-into-`time.Time` scan issue — this only ever does `MAX()` inside subqueries and never scans an aggregate directly into a `time.Time` field at the top level (the top-level `Scan` reads plain columns), which is the same shape `ispStats` already uses successfully against both backends. Worth confirming against the SQLite test driver before treating this as settled, but no reason to expect the same failure mode.

### Handler (`internal/server/handlers.go`)

```go
func (h *Handlers) ResurfacedDomains(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var domains []db.ResurfacedDomain
	var err error
	if user.IsAdmin {
		domains, err = h.store.ResurfacedDomains(r.Context())
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		domains, err = h.store.ResurfacedDomainsForDepartment(r.Context(), *user.DepartmentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load resurfaced domains")
		return
	}
	writeJSON(w, http.StatusOK, domains)
}
```

Same admin-global / department-scoped branching as `ISPStats`/`NationalTrend`.

### Router (`internal/server/router.go`)

```go
r.Get("/resurfaced", h.ResurfacedDomains)
```

Inside the existing `requireAuth` group, alongside `/trend` and `/isps/{isp}`.

### Test stub (`internal/server/handlers_test.go`)

```go
func (m *fullMockStore) ResurfacedDomains(_ context.Context) ([]db.ResurfacedDomain, error) {
	return nil, nil
}
func (m *fullMockStore) ResurfacedDomainsForDepartment(_ context.Context, _ uint) ([]db.ResurfacedDomain, error) {
	return nil, nil
}
```

## Frontend

### Types (`web/src/api/types.ts`)

```typescript
export type ResurfacedServerEntry = {
  dns_server_id: number
  dns_server_name: string
  isp: string
  last_compliant_at: string  // RFC3339
  resurfaced_at: string      // RFC3339
}

export type ResurfacedDomain = {
  url: string
  resurfaced_at: string      // RFC3339, most recent across affected_servers
  affected_servers: ResurfacedServerEntry[]
}
```

### API client (`web/src/api/results.ts`)

```typescript
export async function fetchResurfacedDomains(): Promise<ResurfacedDomain[]> {
  return api.get<ResurfacedDomain[]>('/resurfaced')
}
```

Lives alongside `fetchNationalTrend`/`groupResults` — same "compliance aggregate" family, not a new module.

### Overview page (`web/src/routes/index.tsx`)

A fourth stat tile next to "URLs requested this month", styled with the violation color (`label-violation` / `text-violation-text`) since — unlike the other three neutral stats — this one is inherently bad news:

```tsx
<div>
  <p className="server-count label-violation">{resurfacedCount}</p>
  <p className="dash-label">Resurfaced</p>
</div>
```

`resurfacedCount` = `resurfacedDomains.length` — already one row per domain, so no dedup needed. Fetched in the same `Promise.all` as the other Overview stats.

Only render the tile when `resurfacedCount > 0` — don't clutter the dashboard with a permanent "0" (the other three tiles are always-meaningful counts; this one is an alert, and an alert with nothing to report shouldn't take up space).

### ISP detail page (`web/src/routes/isps.$isp.tsx`)

New section between "Most Non-Compliant Domain" and "Time to Compliance" — this is where an ISP's operator-facing regression list belongs:

```tsx
// A domain rolls up multiple servers; only the ones matching this ISP matter here.
const resurfacedForThisISP = resurfacedDomains
  .map(d => ({ ...d, affected_servers: d.affected_servers.filter(s => s.isp === isp) }))
  .filter(d => d.affected_servers.length > 0)

{resurfacedForThisISP.length > 0 && (
  <div className="dash-section">
    <p className="section-title">Resurfaced Domains</p>
    <ul>
      {resurfacedForThisISP.map(d => (
        <li key={d.url}>
          <Link to="/domain/$url" params={{ url: d.url }}>{d.url}</Link>
          {' '}— resurfaced on {d.affected_servers.length} server{d.affected_servers.length > 1 ? 's' : ''}
          {' '}({d.affected_servers.map(s => s.dns_server_name).join(', ')})
        </li>
      ))}
    </ul>
  </div>
)}
```

Fetch the same global (department-scoped) list as the Overview page, filter+remap client-side to this ISP's subset of `affected_servers`, in the existing `Promise.all` alongside `fetchISPStats`/`fetchISPTrend`/`fetchISPTiming`.

### Results table filter (`web/src/routes/results.index.tsx`) — stretch goal

The existing `StatusFilter` type (`'all' | 'violations' | 'compliant'`) could grow a fourth value, `'resurfaced'`, reusing the same `ToggleGroup` control already there. Filtering would check group membership against the fetched resurfaced set (by URL). This gives ongoing, always-visible operational access to the metric beyond the two summary surfaces above, which matters given how critical this signal is — but it's additive UI, not required for v1, so calling it out separately rather than bundling it into the required task list.

## Implementation Tasks

| # | Task | Files | Type |
|---|------|-------|------|
| 1 | Backend model + store + postgres query | `models.go`, `store.go`, `postgres.go` | Go |
| 2 | Handler + router + test stub | `handlers.go`, `router.go`, `handlers_test.go` | Go |
| 3 | Frontend types + API client | `types.ts`, `results.ts` | TS |
| 4 | Overview stat tile | `index.tsx` | TSX |
| 5 | ISP detail page section | `isps.$isp.tsx` | TSX |
| 6 | *(stretch)* Results table filter chip | `results.index.tsx` | TSX |
| 7 | Remove sessionStorage diff, wire `scan-results.tsx` to `fetchResurfacedDomains()` | `__root.tsx`, `scan-results.tsx` | TSX |

Tasks 2 depends on 1. Tasks 3-7 depend on 2 for the API.

## Decisions

Settled before implementation (see prior discussion in this spec's originating conversation):

1. **Replace, don't duplicate.** The `scan-results.tsx` sessionStorage baseline-diff goes away; `fetchResurfacedDomains()` becomes the single source of truth for "what just started violating," covering scheduled scans too.
2. **No expiry.** A resurfaced domain stays on the list until it's re-blocked (a fresh compliant scan lands), no matter how long ago the regression happened. Old-and-still-broken is treated as equally worth surfacing as just-broken — the list only grows if nobody acts on it, which is itself the point.
3. **Roll up to one row per domain.** A domain resurfacing on multiple DNS servers at once shows as one entry with `affected_servers` listing which ones, not one row per server. Trades away at-a-glance per-server precision for a cleaner list; per-server detail is still available by expanding `affected_servers` in the UI (see ISP page section above) or drilling into the domain's own page.

## Verification

- `go test ./...` must pass after Task 1-2, including new tests asserting: (a) a compliant→violation transition is detected, (b) a compliant→compliant or violation→violation pair is *not* flagged, (c) a domain resurfacing on 2+ servers at once produces one `ResurfacedDomain` row with 2+ `affected_servers`, not multiple rows.
- `npm run build` from `web/` must compile with no TypeScript errors after each frontend task.
