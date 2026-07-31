# DB Schema Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the correctness and scale defects found in the schema audit, so the database stays trustworthy and queryable once the ~38k-domain MCMC blocking list is imported.

**Architecture:** No new tables and no schema redesign. The core fix is making `scan_results.url_value` — a denormalized column the audit found is silently load-bearing as the grouping key for every aggregate — guaranteed-correct at write time, plus a one-time backfill for existing rows. Everything else is additive: three indexes, a timezone constant, and three small correctness patches.

**Tech Stack:** Go 1.26, GORM (`gorm.io/gorm`) with `gorm.io/driver/postgres`, SQLite (`glebarez/sqlite`) in-memory for tests.

## Global Constraints

- **Every query must run on both Postgres and SQLite.** Tests use SQLite in-memory (`newTestStore`/`rawConnect` in `internal/db/*_test.go`); production is Postgres. Postgres-only syntax (`DISTINCT ON`, `ILIKE`, `UPDATE ... FROM`) is forbidden in `internal/db/postgres.go` — the existing code already works around this by aggregating in Go, and the comments at `postgres.go:159` and `:266` explain why. Follow that precedent.
- **Migrations run on every startup and must be idempotent.** They are called from `cmd/server/main.go` before `db.NewStore`, alongside the existing `db.NormalizeAndDedupeURLs` (line 63) and `db.MigrateAdminDepartments` (line 71).
- **Do not change compliance semantics.** `Compliant` stays A-record-based: DNS resolves = violation (`false`), DNS fails = compliant (`true`). No task here touches that inversion.
- **Timezone for all calendar-day bucketing is `Asia/Kuala_Lumpur`** (UTC+8). This is a Malaysian regulatory tool.
- Run the full suite with `go test ./...` before every commit.

## Findings Covered

| # | Finding | Task |
|---|---|---|
| 1 | `url_value` diverges from `urls.url`, splitting/losing history | 1 |
| 2 | `LIKE` case-sensitive on Postgres, case-insensitive on SQLite | 1 (subsumed) |
| 3 | No index on `scanned_at` / `url_value` / `dns_server_id` | 2 |
| 6 | Day bucketing is UTC, users are UTC+8 | 3 |
| 8 | Unknown DNS server name silently drops a whole sweep | 4 |
| 9 | `favicons.domain` unnormalized → duplicate rows + repeat fetches | 5 |
| 10 | Sessions never garbage-collected | 6 |

**Deliberately deferred** (see "Deferred" at the end for rationale): #4/#7 query restructure, #5 partitioning, #11 unique constraint, #12 `department_urls.url_id` index.

---

### Task 1: Make `url_value` always equal `urls.url`

The audit's critical finding. `scan_results.url_value` is the `GROUP BY`/join key in 24 of 25 query sites, but `grpc.go:98` writes the **raw** crawler string while `url_id` points at the **normalized** `urls` row. When they disagree, a domain's history splits into two logical domains or disappears from lookups entirely.

Two halves: stop writing bad data (ingest), then repair existing rows (backfill).

This also **subsumes finding #2**: `urlnorm.Normalize` lowercases (`internal/urlnorm/urlnorm.go`), so once `url_value` is guaranteed normalized it is always lowercase, and the existing `LIKE` with a lowercased needle at `postgres.go:231` becomes correct. No `ILIKE` is needed — which is fortunate, since SQLite has no `ILIKE`, and wrapping the column in `LOWER()` would defeat the index added in Task 2.

**Files:**
- Modify: `internal/server/grpc.go:52-98`
- Modify: `internal/db/migrate.go` (append new function)
- Modify: `cmd/server/main.go:63-70` (call the new migration)
- Test: `internal/db/migrate_test.go` (append)
- Test: `internal/server/grpc_test.go` (append)

**Interfaces:**
- Consumes: `urlnorm.Normalize(raw string) (string, error)`; `db.Store.CreateURL(ctx, rawURL string) (URL, error)` (already normalizes and get-or-creates).
- Produces: `db.BackfillURLValues(ctx context.Context, database *gorm.DB) error` — used by `cmd/server/main.go`.

- [ ] **Step 1: Write the failing backfill test**

Append to `internal/db/migrate_test.go`:

```go
func TestBackfillURLValues_RewritesDivergedRows(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	u, err := s.CreateURL(ctx, "https://Example.com/")
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	if u.URL != "example.com" {
		t.Fatalf("expected normalized url row, got %q", u.URL)
	}

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	// A legacy row whose url_value kept the raw pre-normalization string.
	if err := gormDB.Create(&db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: "https://Example.com/",
		DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed diverged result: %v", err)
	}

	if err := db.BackfillURLValues(ctx, gormDB); err != nil {
		t.Fatalf("BackfillURLValues: %v", err)
	}

	var got db.ScanResult
	if err := gormDB.First(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.URLValue != "example.com" {
		t.Fatalf("expected url_value backfilled to example.com, got %q", got.URLValue)
	}
}

func TestBackfillURLValues_IsIdempotent(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	u, _ := s.CreateURL(ctx, "example.com")
	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	if err := gormDB.Create(&db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: "example.com",
		DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := db.BackfillURLValues(ctx, gormDB); err != nil {
			t.Fatalf("BackfillURLValues run %d: %v", i, err)
		}
	}

	var got db.ScanResult
	gormDB.First(&got)
	if got.URLValue != "example.com" {
		t.Fatalf("expected url_value unchanged, got %q", got.URLValue)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run TestBackfillURLValues ./internal/db/...`
Expected: FAIL — `undefined: db.BackfillURLValues`

- [ ] **Step 3: Implement the backfill**

Append to `internal/db/migrate.go`:

```go
// BackfillURLValues repairs scan_results rows whose denormalized url_value
// drifted from the canonical urls.url it points at via url_id — the state
// NormalizeAndDedupeURLs used to leave behind (it reassigned url_id and
// renamed urls.url, but never rewrote url_value), and that a pre-fix
// grpcServer.Submit wrote directly by storing the raw crawler string.
//
// url_value is the GROUP BY / join key for nearly every aggregate in
// internal/db/postgres.go, so a diverged row silently reads as a separate
// domain — or as no domain at all, when a handler looks it up by its
// normalized name. Idempotent: rows already in sync match nothing.
//
// Written as a correlated subquery rather than Postgres's UPDATE ... FROM so
// the same statement also runs on the SQLite backend used by tests.
func BackfillURLValues(ctx context.Context, database *gorm.DB) error {
	const canonical = "(SELECT url FROM urls WHERE urls.id = scan_results.url_id)"
	return database.WithContext(ctx).Exec(
		"UPDATE scan_results SET url_value = " + canonical +
			" WHERE url_id IS NOT NULL AND url_value <> " + canonical,
	).Error
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -run TestBackfillURLValues ./internal/db/...`
Expected: PASS (both tests)

- [ ] **Step 5: Wire the migration into server startup**

In `cmd/server/main.go`, immediately after the existing `NormalizeAndDedupeURLs` block (line 63-70) and before `MigrateAdminDepartments`, add:

```go
	// Must run after NormalizeAndDedupeURLs — that pass rewrites urls.url and
	// reassigns scan_results.url_id, which is exactly what leaves url_value
	// stale.
	if err := db.BackfillURLValues(context.Background(), gormDB); err != nil {
		log.Fatalf("backfilling scan_results.url_value: %v", err)
	}
```

- [ ] **Step 6: Write the failing ingest test**

Append to `internal/server/grpc_test.go`. It uses the file's existing `mockStore` / `mockStorage` / `newTestGRPCClient` helpers — `mockStore.CreateURL` already mirrors the real get-or-create-by-normalized-value behaviour, and `ListWatchedURLs` returns `nil`, so this exercises the fallback path.

```go
func TestSubmitStoresNormalizedURLValue(t *testing.T) {
	store := &mockStore{
		activeScanRun: &db.ScanRun{ID: 1, Status: "running"},
		dnsServers:    []db.DNSServer{{ID: 3, Name: "Google"}},
	}
	client := newTestGRPCClient(t, store, &mockStorage{})

	_, err := client.Submit(context.Background(), &pb.ComplianceReport{
		Results: []*pb.SiteResult{{
			Url:       "https://Example.com/path?q=1",
			Compliant: false,
			DnsServer: "Google",
			Timestamp: time.Now().Unix(),
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(store.insertedResults) != 1 {
		t.Fatalf("expected 1 inserted result, got %d", len(store.insertedResults))
	}
	got := store.insertedResults[0]
	// url_value is the GROUP BY/join key for every aggregate — it must match
	// the urls row url_id points at, not the raw string the crawler was fed.
	if got.URLValue != "example.com" {
		t.Fatalf("expected normalized url_value example.com, got %q", got.URLValue)
	}
	if got.URLID == 0 {
		t.Fatal("expected non-zero URLID")
	}
}
```

- [ ] **Step 7: Run it to verify it fails**

Run: `go test -run TestSubmitStoresNormalizedURLValue ./internal/server/...`
Expected: FAIL — `URLValue` is `https://Example.com/path`, not `example.com`

- [ ] **Step 8: Fix the ingest path**

In `internal/server/grpc.go`, replace the URL-resolution block (lines 52-74, from `for _, r := range report.Results {` through the closing brace of the `if urlID == 0` fallback) with:

```go
	for _, r := range report.Results {
		// urls.url is stored normalized, and scan_results.url_value is the
		// GROUP BY / join key for nearly every aggregate in internal/db —
		// so it must be the normalized value, not the raw string the crawler
		// happened to be fed. Resolve both together.
		urlValue := r.Url
		if norm, err := urlnorm.Normalize(r.Url); err == nil {
			urlValue = norm
		}
		urlID := urlIDByValue[urlValue]
		if urlID == 0 {
			// Not on any enabled watchlist — e.g. a "Scan Selected" ad-hoc URL
			// or a disabled watchlist entry. ScanResult.URLID FKs to urls.id,
			// so without this the insert below would fail and the row would be
			// dropped. Get-or-create by normalized value, same as
			// AddToWatchlist; u.URL is then authoritative for url_value.
			u, err := s.store.CreateURL(ctx, r.Url)
			if err != nil {
				log.Printf("grpc: resolve url id for %s: %v", r.Url, err)
				continue
			}
			urlID, urlValue = u.ID, u.URL
		}
```

Then change the struct literal at what was line 98 from `URLValue: r.Url,` to:

```go
			URLValue:           urlValue,
```

Note the behavioural change in the fallback: it now `continue`s on a `CreateURL` error instead of inserting with `URLID: 0`. That insert would have failed on the FK constraint anyway (GORM creates real FKs — `DisableForeignKeyConstraintWhenMigrating` is unset), so this only makes the existing failure explicit and skips a doomed round-trip.

- [ ] **Step 9: Run the full suite**

Run: `go test ./...`
Expected: one **known** failure — `TestSubmitStoresResults` (`grpc_test.go:141`) asserts the old raw-string behaviour:

```go
	if store.insertedResults[0].URLValue != "https://example.com" {
```

That assertion encoded the bug. Change it to:

```go
	if store.insertedResults[0].URLValue != "example.com" {
```

Re-run `go test ./...`; expected PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/server/grpc.go internal/server/grpc_test.go internal/db/migrate.go internal/db/migrate_test.go cmd/server/main.go
git commit -m "fix: store normalized url_value on scan results, backfill diverged rows

url_value is the GROUP BY/join key for nearly every aggregate, but Submit
wrote the raw crawler string while url_id pointed at the normalized urls
row. Diverged rows read as a separate domain, or as none at all when a
handler looks one up by its normalized name. Also fixes case-sensitive
LIKE search on Postgres, since normalized values are always lowercase.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: Index `scan_results` on the columns it is actually filtered by

`scan_results` carries indexes only on `scan_run_id` and `url_id`. Every trend, heatmap, uptime, ISP-stats and domain-summary query filters or groups on `scanned_at`, `url_value`, or `dns_server_id` — all unindexed. At 38k domains × ~8 servers × daily scans (~111M rows/year) each of those is a sequential scan.

The composite `(url_value, dns_server_id, scanned_at)` also directly serves the `MAX(scanned_at) GROUP BY url_value, dns_server_id` subquery that `ispStats` (`postgres.go:683`) and `resurfacedDomains` (`:960`) run on every page load — which is why the query restructure is deferred rather than done here (see "Deferred").

**Files:**
- Modify: `internal/db/models.go:125-144` (`ScanResult` struct tags)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing — index-only change, no API surface.

- [ ] **Step 1: Add the index tags**

In `internal/db/models.go`, change three fields on `ScanResult`:

```go
	URLValue           string    `gorm:"not null;index:idx_scan_results_url_server_time,priority:1" json:"url"`
	DNSServerID        uint      `gorm:"not null;index;index:idx_scan_results_url_server_time,priority:2" json:"dns_server_id"`
```

and:

```go
	ScannedAt          time.Time `gorm:"index;index:idx_scan_results_url_server_time,priority:3" json:"scanned_at"`
```

That yields three indexes: the composite `idx_scan_results_url_server_time (url_value, dns_server_id, scanned_at)`, plus standalone indexes on `dns_server_id` (for `ScanProgress`'s `GROUP BY` and `DeleteDNSServer`) and `scanned_at` (for the date-range scans in every trend/uptime query).

Add a comment above the struct explaining the composite, so the next reader does not "tidy" it away:

```go
// The composite index (url_value, dns_server_id, scanned_at) serves the
// latest-scan-per-(domain, server) subqueries in ispStats and
// resurfacedDomains, which would otherwise aggregate the whole table on
// every ISP page load. Column order matters — do not reorder.
```

- [ ] **Step 2: Verify migration applies cleanly**

Run: `go test ./internal/db/...`
Expected: PASS. `db.Connect` runs `AutoMigrate` in every test, so a malformed tag fails here immediately.

- [ ] **Step 3: Verify against a real Postgres**

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d postgres
go run ./cmd/server/ --db-url "host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable" --http-addr :8080
```

Let it boot, then in another shell:

```bash
docker compose exec postgres psql -U postgres -d dns_compliance -c "\d scan_results"
```

Expected: the `Indexes:` section lists `idx_scan_results_url_server_time`, plus indexes on `dns_server_id` and `scanned_at`. Stop the server afterwards.

- [ ] **Step 4: Commit**

```bash
git add internal/db/models.go
git commit -m "perf: index scan_results on url_value, dns_server_id, scanned_at

Every trend, heatmap, uptime and ISP-stats query filters on columns that
had no index; only scan_run_id and url_id were covered. The composite also
serves the latest-per-(domain,server) subqueries that previously scanned
the whole table on each ISP page load.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: Bucket calendar days in Malaysia time, not UTC

Four call sites bucket results into calendar days with `.Format("2006-01-02")` on a `time.Time` the driver hands back in UTC. A scan at 07:00 Malaysia time (UTC+8) is 23:00 UTC the previous day, so it lands in the **wrong cell** on the heatmap and the wrong point on every trend line. For a tool whose output is enforcement evidence, that is a reporting defect.

**Files:**
- Modify: `internal/db/postgres.go` (add package-level var; 4 call sites at `:173`, `:803`, `:838`, and the `serverUptime` day bucket near `:908`)
- Test: `internal/db/postgres_test.go` (append)

**Interfaces:**
- Consumes: nothing.
- Produces: `db.reportingLocation` (unexported package-level `*time.Location`) — used by all day-bucketing code in this package.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/postgres_test.go`:

```go
func TestDailyComplianceByURLBucketsInMalaysiaTime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateURL(ctx, "example.com")
	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	// 2026-07-15 23:30 UTC == 2026-07-16 07:30 in Malaysia (UTC+8).
	// The analyst scanned on the 16th and must see it on the 16th.
	scanned := time.Date(2026, 7, 15, 23, 30, 0, 0, time.UTC)
	if err := s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, URLValue: u.URL,
		DNSServerID: srv.ID, Compliant: true, ScannedAt: scanned,
	}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	stats, err := s.DailyComplianceByURL(ctx, "example.com",
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DailyComplianceByURL: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(stats))
	}
	if stats[0].Day != "2026-07-16" {
		t.Fatalf("expected day 2026-07-16 (Malaysia time), got %q", stats[0].Day)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestDailyComplianceByURLBucketsInMalaysiaTime ./internal/db/...`
Expected: FAIL — `expected day 2026-07-16 (Malaysia time), got "2026-07-15"`

- [ ] **Step 3: Add the location and use it**

Near the top of `internal/db/postgres.go`, after the imports:

```go
// reportingLocation is the timezone every calendar-day bucket in this package
// is computed in. Results are stored as timestamptz (UTC instants); bucketing
// them by UTC date would put a 07:00 Malaysia-time scan on the previous day's
// heatmap cell. This tool reports to a Malaysian regulator, so days are
// Malaysian days. Falls back to a fixed +08:00 offset if the host image has
// no tzdata (the debian-slim runtime does, but a scratch image would not).
var reportingLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Kuala_Lumpur")
	if err != nil {
		return time.FixedZone("MYT", 8*60*60)
	}
	return loc
}()
```

Then at each of the four bucketing sites, replace

```go
r.ScannedAt.Format("2006-01-02")
```

with

```go
r.ScannedAt.In(reportingLocation).Format("2006-01-02")
```

Find them all with:

```bash
grep -n 'Format("2006-01-02")' internal/db/postgres.go
```

Every hit in that file must be converted — leaving one behind means two views of the same data disagree about what day it is.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/db/...`
Expected: PASS. Existing day-bucket tests may need their expected dates shifted; if one now fails, confirm the new value is the Malaysia-time day before changing it.

- [ ] **Step 5: Commit**

```bash
git add internal/db/postgres.go internal/db/postgres_test.go
git commit -m "fix: bucket heatmap and trend days in Malaysia time

Day buckets were computed on the UTC date, so a 07:00 MYT scan landed on
the previous day's heatmap cell and trend point.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Stop silently dropping results for an unknown DNS server

`grpc.go` resolves `DNSServerID: serverByName[r.DnsServer]`, which yields `0` on a miss. GORM creates a real FK constraint, so the insert fails, gets logged, and the row is discarded. Rename a DNS server mid-sweep and that server's entire result set for the run vanishes behind one log line.

**Files:**
- Modify: `internal/server/grpc.go` (the `result := db.ScanResult{...}` block)

**Interfaces:**
- Consumes: `serverByName map[string]uint` (built at `grpc.go:41-44`).
- Produces: nothing.

- [ ] **Step 1: Add the explicit guard**

In `internal/server/grpc.go`, immediately before the `result := db.ScanResult{` literal:

```go
		dnsServerID, ok := serverByName[r.DnsServer]
		if !ok {
			// A server renamed or deleted mid-sweep. dns_server_id has a real
			// FK, so inserting 0 here fails anyway — but as a constraint
			// violation buried in a log line, which reads like a database
			// problem rather than a config change. Say what actually happened.
			log.Printf("grpc: unknown dns server %q reported for %s — result dropped; was it renamed mid-sweep?", r.DnsServer, r.Url)
			continue
		}
```

and change the struct field to:

```go
			DNSServerID:        dnsServerID,
```

- [ ] **Step 2: Fix the three tests this exposes**

Run: `go test ./internal/server/...`
Expected: **three known failures** — `TestSubmitStoresResults`, `TestSubmitUploadsScreenshot`, and `TestSubmitReadsNetNameFromCache` all submit a `DnsServer` name that is not in their `mockStore.dnsServers`, so the new guard skips the result and their insert assertions fail.

These tests only passed before because `mockStore.InsertResult` has no FK enforcement — against real Postgres they were asserting a state that cannot exist. Fix each by giving the store a matching server, e.g. for `TestSubmitStoresResults`:

```go
	store := &mockStore{
		activeScanRun: &db.ScanRun{ID: 1, Status: "running"},
		dnsServers:    []db.DNSServer{{ID: 3, Name: "Google"}},
	}
```

`TestSubmitUploadsScreenshot` sends no `DnsServer` at all — add `DnsServer: "Google"` to its `pb.SiteResult` as well as the store entry. `TestSubmitReadsNetNameFromCache` sends `"Google"`; it just needs the store entry.

- [ ] **Step 3: Re-run the suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/server/grpc.go
git commit -m "fix: log unknown DNS server names explicitly on submit

A renamed server produced dns_server_id=0, an FK violation, and a log line
that read as a database fault rather than a config change.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: Normalize the favicon cache key

`FaviconByURL` keys `db.Favicon` on `parsed.Hostname()` without normalizing, so `Example.com` and `example.com` become two rows — each triggering its own outbound fetch. That breaks the "at most once per domain, ever" guarantee the model comment makes, and partially defeats the reason the cache exists (not touching the target's server logs more than necessary).

**Files:**
- Modify: `internal/server/handlers.go:874-890` (`FaviconByURL`)
- Test: `internal/server/handlers_test.go` (append)

**Interfaces:**
- Consumes: `urlnorm.Normalize`.
- Produces: nothing.

- [ ] **Step 1: Write the failing test**

Append to `internal/server/handlers_test.go`. This needs no fetcher stub: `setupRouter` passes `nil` for `faviconFetch`, so a cache **miss** returns 503. Seeding the cache under the normalized key and requesting the mixed-case form means a 200 can only happen if normalization worked.

```go
func TestFaviconByURLNormalizesCacheKey(t *testing.T) {
	store := &fullMockStore{
		favicons: []db.Favicon{{
			Domain:      "example.com",
			ContentType: "image/png",
			Data:        []byte{0x89, 'P', 'N', 'G'},
			FetchedAt:   time.Now(),
		}},
	}
	cookie := deptCookie(store, 1)
	router := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/favicon/Example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// faviconFetch is nil in setupRouter, so a cache miss 503s. A 200 proves
	// "Example.com" normalized down to the cached "example.com" row.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from normalized cache hit, got %d (%s)", w.Code, w.Body.String())
	}
	if len(store.favicons) != 1 {
		t.Fatalf("expected no second favicon row, got %d", len(store.favicons))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestFaviconByURLNormalizesCacheKey ./internal/server/...`
Expected: FAIL — recorded domain is `Example.com`

- [ ] **Step 3: Normalize the key**

In `internal/server/handlers.go`, replace the domain-extraction block (lines 881-883):

```go
	domain := urlValue
	if parsed, parseErr := url.Parse(urlValue); parseErr == nil && parsed.Hostname() != "" {
		domain = parsed.Hostname()
	}
```

with:

```go
	// Normalize before using as the cache key: db.Favicon is keyed by domain
	// and fetched at most once ever, so "Example.com" and "example.com"
	// hitting separate rows means a second outbound request to a domain this
	// tool is deliberately trying not to touch twice.
	domain, err := urlnorm.Normalize(urlValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
```

Check whether `url` is still used elsewhere in the file before removing its import — it almost certainly is (`url.PathUnescape` on the line above). Add the `urlnorm` import if not already present.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/server/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/handlers.go internal/server/handlers_test.go
git commit -m "fix: normalize favicon cache key

Example.com and example.com cached as separate rows, each triggering its
own outbound fetch and breaking the fetch-once-per-domain guarantee.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: Garbage-collect expired sessions

`GetSession` filters `expires_at > now`, but nothing ever deletes expired rows, so the table grows without bound. Sessions also have no FK to `users`, so deleting a user orphans rows (harmless today — `requireAuth` rejects a session whose user is gone — but the rows still accumulate).

Reuse the existing background-loop pattern from `internal/server/whois_refresh.go` rather than inventing a new one.

**Files:**
- Create: `internal/server/session_cleanup.go`
- Modify: `internal/db/store.go` (add one method to `SessionStore`)
- Modify: `internal/db/postgres.go` (implement it, near the other session methods at `:462-480`)
- Modify: `cmd/server/main.go` (start the loop alongside `StartWhoisRefresher`)
- Test: `internal/db/postgres_test.go` (append)

**Interfaces:**
- Consumes: `db.SessionStore`.
- Produces: `db.SessionStore.DeleteExpiredSessions(ctx context.Context) (int64, error)` — returns rows deleted; `server.StartSessionCleanup(ctx context.Context, store db.SessionStore)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/postgres_test.go`:

```go
func TestDeleteExpiredSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "CMOD")
	u, _ := s.CreateUser(ctx, db.User{Username: "a", PasswordHash: "x", DepartmentID: &dept.ID})

	live := db.Session{Token: "live", UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}
	dead := db.Session{Token: "dead", UserID: u.ID, ExpiresAt: time.Now().Add(-time.Hour)}
	if err := s.CreateSession(ctx, live); err != nil {
		t.Fatalf("CreateSession live: %v", err)
	}
	if err := s.CreateSession(ctx, dead); err != nil {
		t.Fatalf("CreateSession dead: %v", err)
	}

	n, err := s.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row deleted, got %d", n)
	}

	if got, _ := s.GetSession(ctx, "live"); got == nil {
		t.Fatal("live session should have survived")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestDeleteExpiredSessions ./internal/db/...`
Expected: FAIL — `DeleteExpiredSessions` undefined on the interface

- [ ] **Step 3: Add the store method**

In `internal/db/store.go`, add to the `SessionStore` interface:

```go
	// DeleteExpiredSessions removes sessions past their expiry. Returns the
	// number of rows deleted. GetSession already filters these out, so this
	// is purely to stop the table growing without bound.
	DeleteExpiredSessions(ctx context.Context) (int64, error)
```

In `internal/db/postgres.go`, after `DeleteSession` (around line 480):

```go
func (s *postgresStore) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&Session{})
	return res.RowsAffected, res.Error
}
```

- [ ] **Step 4: Run the test**

Run: `go test -run TestDeleteExpiredSessions ./internal/db/...`
Expected: PASS

- [ ] **Step 5: Add the background loop**

Read `internal/server/whois_refresh.go` first and mirror its structure. Create `internal/server/session_cleanup.go`:

```go
package server

import (
	"context"
	"log"
	"time"

	"github.com/afif/dns-tracking/internal/db"
)

// sessionCleanupInterval is how often expired sessions are swept. Sessions
// last 7 days (sessionDuration in auth.go) and GetSession already ignores
// expired rows, so this is bounded-growth housekeeping, not a security
// control — hourly is plenty.
const sessionCleanupInterval = time.Hour

// StartSessionCleanup deletes expired sessions on a fixed cadence until ctx
// is cancelled. Failures are logged and retried on the next tick; a database
// blip must not kill the loop.
func StartSessionCleanup(ctx context.Context, store db.SessionStore) {
	go func() {
		ticker := time.NewTicker(sessionCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n, err := store.DeleteExpiredSessions(ctx)
				if err != nil {
					log.Printf("session cleanup: %v", err)
					continue
				}
				if n > 0 {
					log.Printf("session cleanup: removed %d expired sessions", n)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
```

- [ ] **Step 6: Start it from main**

In `cmd/server/main.go`, next to the existing `StartWhoisRefresher` call, add:

```go
	server.StartSessionCleanup(ctx, store)
```

using whatever context variable that call site already has in scope.

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`
Expected: PASS. If `fullMockStore` in `internal/server/handlers_test.go` no longer satisfies `db.Store`, add the new method to it — this is the documented pattern for extending that fake.

- [ ] **Step 8: Commit**

```bash
git add internal/db/store.go internal/db/postgres.go internal/db/postgres_test.go internal/server/session_cleanup.go internal/server/handlers_test.go cmd/server/main.go
git commit -m "feat: sweep expired sessions hourly

The sessions table had no cleanup path; GetSession filtered expired rows
but nothing ever deleted them.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: Re-sync CLAUDE.md

Several documented behaviours change here. Update `CLAUDE.md`:

- [ ] **Step 1: Apply the edits**

- **Database section** — `scan_results.url_value` is documented as "kept denormalized alongside `URLID` for display/matching without a join". Add that it is now guaranteed equal to `urls.url` (written normalized by `Submit`, backfilled by `db.BackfillURLValues`) because it is the `GROUP BY`/join key for the aggregate queries, not merely display.
- **Database section** — note the new indexes on `scan_results`, and that the composite `(url_value, dns_server_id, scanned_at)` exists specifically to serve the latest-per-(domain, server) subqueries.
- **Database section** — note that all calendar-day bucketing uses `Asia/Kuala_Lumpur`, not UTC.
- **Domain normalization & watchlists section** — add `db.BackfillURLValues` next to `db.NormalizeAndDedupeURLs` as a startup migration, and note the ordering dependency between them.
- **Auth & RBAC section** — mention `StartSessionCleanup` and that expired sessions are swept hourly.
- **Favicon caching section** — note the cache key is `urlnorm.Normalize`d.

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: sync CLAUDE.md with schema hardening changes

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Verification

After all tasks:

- [ ] `go test ./...` passes.
- [ ] Boot against a real Postgres and confirm the indexes exist:

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d postgres
```

then after starting the server once, `\d scan_results` shows `idx_scan_results_url_server_time`, `dns_server_id`, and `scanned_at` indexes.

- [ ] Confirm the backfill is a no-op on a clean DB: start the server twice, and check the second boot logs no error from `BackfillURLValues`.
- [ ] Run a real sweep via `./dev.sh`, then load `/results`, a domain's `/domain/$url` Overview (heatmap), and an ISP detail page. All three read `url_value`; all three must show the same domain set they did before.
- [ ] Spot-check timezone: after a scan, confirm the heatmap cell for today matches the Malaysian calendar date, not the UTC one.

## Deferred

Not in this plan, with reasons — these are decisions, not oversights:

- **Partitioning `scan_results` (#5).** You chose keep-everything-partition-only, but partitioning must come *after* Task 2's indexes are measured under real load. It also requires abandoning `AutoMigrate` for this table (GORM cannot express `PARTITION BY RANGE`), which is a meaningful architectural commitment. Revisit when the table passes ~10M rows or when an ISP page exceeds ~1s with the new indexes in place.
- **Restructuring the `MAX(scanned_at)` subqueries (#4, #7).** The natural fix is Postgres `DISTINCT ON`, which is also tie-safe — but SQLite has no `DISTINCT ON`, and this package's stated convention (`postgres.go:159`, `:266`) is that every query must run on both. Task 2's composite index addresses the performance half without a semantic change. The tie fan-out requires two rows sharing a microsecond for the same (domain, server), which needs a real occurrence before it justifies a dialect split.
- **Unique constraint on `(scan_run_id, url_id, dns_server_id)` (#11).** Would make a retried `Submit` idempotent, but `Submit` has no retry today, and adding the constraint against existing data requires deciding what to do with any duplicates already present. Worth doing when retry is added.
- **Standalone index on `department_urls.url_id` (#12).** Only two callers (`ListUnassignedURLs`, the dedupe migration), neither hot. Not worth the write cost yet.
