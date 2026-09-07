# DNS Error Classification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store *why* a DNS resolution failed (NXDOMAIN vs. SERVFAIL vs. REFUSED vs. timeout vs. empty-NOERROR vs. other) as a real, queryable field on each scan result, instead of only a free-text `Error` string — so a transient/rate-limited/refused query is visible as distinct from a genuine block, rather than indistinguishable from one.

**Architecture:** `internal/dns`'s custom UDP/DoT/DoH resolvers already inspect the raw DNS RCode internally but discard it into one generic `"no A records for %s"` string for everything except NXDOMAIN. Preserve that RCode in a small typed error, classify it (plus transport-level timeouts) into a fixed set of string categories in Go, store the result as `ScanResult.ErrorClass`, carry it crawler → server over the existing gRPC report, and have the frontend read that field directly instead of re-guessing the category from the raw error string via substring matching. **This plan does not change `Compliant` or any compliance-percentage/stats calculation** — it only adds a new, more trustworthy signal alongside the existing one. Widening that signal into a decision (e.g. retrying before finalizing, or introducing an "uncertain" verdict) is a deliberate follow-up, not part of this change.

**Tech Stack:** Go 1.26 (stdlib `net`, `context`, `errors`; `golang.org/x/net/dns/dnsmessage`), GORM/PostgreSQL, protobuf/gRPC, React/TypeScript frontend.

**Spec:** No separate spec doc — this plan's design was worked out directly against the current code during a conversation diagnosing a silent-failure risk in `internal/pipeline.checkDNS` (see the Design Notes below for the reasoning and the exact current-state facts it's built on).

## Design Notes (context an executor needs, since there's no separate spec file)

- `internal/pipeline/pipeline.go`'s `checkDNS` (lines 120-165) currently does this on any resolve error, no matter the cause:
  ```go
  ip, latencyMs, err := resolve(ctx, u.Hostname())
  if err != nil {
      return SiteResult{
          URL:       rawURL,
          Timestamp: time.Now(),
          Compliant: true,
          Error:     err.Error(),
      }
  }
  ```
  A timeout, a REFUSED, a SERVFAIL, and a genuine NXDOMAIN all produce `Compliant: true` with no other distinguishing structured field — only the free-text `Error` string differs, and nothing downstream parses it except a cosmetic frontend badge.
- `internal/dns/resolver.go`'s `firstA` (lines 338-357, used by both `NewResolver` and `NewDoTResolver`) and `NewDoHResolver` (lines 205-259) already branch on RCode internally — NXDOMAIN gets wrapped as `&net.DNSError{IsNotFound: true}` — but every other RCode (SERVFAIL, REFUSED, NOERROR-with-no-answers) collapses into the same `fmt.Errorf("no A records for %s", host)`. That RCode is sitting right there in `reply.Header.RCode` and is being thrown away.
- `web/src/lib/dns-error.ts`'s `classifyDNSError` already proves the categories are useful (it renders a badge in the scan-results table today) but only pattern-matches the raw string *after* the RCode has already been discarded server-side, and its `servfail` bucket matches wording (`"server misbehaving"`, `"connection refused"`) that the custom resolvers never actually produce — it only ever fires for the system resolver path. This plan makes the classification accurate by doing it in Go, where the RCode is actually available, and stops the frontend from needing to guess at all.
- Existing rows in the DB only have the free-text `Error` string, never a preserved RCode — a one-time backfill approximates their category via the same substring matching the frontend used to do, since the original RCode can't be recovered after the fact.

## Global Constraints

- Follow this repo's GORM `AutoMigrate` convention for schema changes: add a tagged struct field, don't hand-write DDL.
- Any backfill of pre-existing rows must be a batched, idempotent, non-fatal Go function in `internal/db/migrate.go`, matching `BackfillURLValues`'s exact shape (bounded `UPDATE ... LIMIT ?` loop, logged-not-fatal at the call site) — a first-deploy run must not risk tripping a managed-Postgres `statement_timeout`.
- No change to `Compliant`, compliance-percentage math, ISP timing, or notification logic in this plan.
- Every Go step follows TDD: failing test first, then minimal implementation.
- Commit after each task.

---

### Task 1: Preserve DNS RCode and classify it (`internal/dns`)

**Files:**
- Create: `internal/dns/classify.go`
- Create: `internal/dns/classify_test.go`
- Modify: `internal/dns/resolver.go:356` (inside `firstA`)
- Modify: `internal/dns/resolver.go:257` (inside `NewDoHResolver`)

**Interfaces:**
- Produces: `type RCodeError struct { RCode dnsmessage.RCode; Host string }` (implements `error`) and `func Classify(err error) string`, returning one of `"" | "nxdomain" | "timeout" | "servfail" | "refused" | "empty" | "other"`. Task 3 calls `dns.Classify(err)` directly.

- [ ] **Step 1: Write the failing test**

Create `internal/dns/classify_test.go`:
```go
package dns

import (
	"context"
	"errors"
	"net"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil is empty", nil, ""},
		{"nxdomain", &net.DNSError{Err: "no such host", IsNotFound: true}, "nxdomain"},
		{"servfail rcode", &RCodeError{RCode: dnsmessage.RCodeServerFailure, Host: "example.com"}, "servfail"},
		{"refused rcode", &RCodeError{RCode: dnsmessage.RCodeRefused, Host: "example.com"}, "refused"},
		{"noerror with no answers", &RCodeError{RCode: dnsmessage.RCodeSuccess, Host: "example.com"}, "empty"},
		{"context deadline exceeded", context.DeadlineExceeded, "timeout"},
		{"dns error timeout flag", &net.DNSError{Err: "i/o timeout", IsTimeout: true}, "timeout"},
		{"system resolver servfail wording", &net.DNSError{Err: "server misbehaving"}, "servfail"},
		{"unrecognized error", errors.New("boom"), "other"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.err); got != c.want {
				t.Errorf("Classify(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dns/... -run TestClassify -v`
Expected: FAIL to compile — `undefined: RCodeError`, `undefined: Classify`.

- [ ] **Step 3: Implement `Classify` and `RCodeError`**

Create `internal/dns/classify.go`:
```go
package dns

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

// RCodeError reports a final, non-NXDOMAIN DNS response — SERVFAIL,
// REFUSED, or a NOERROR with no A records. Preserving the RCode (instead of
// collapsing it into a generic "no A records" string, as firstA and
// NewDoHResolver used to) lets a caller tell a resolver actively refusing
// or failing a query apart from a genuine non-answer. That distinction
// matters: an ISP's rate-limiting or source-IP allowlisting typically shows
// up as REFUSED or SERVFAIL, which looks identical to a real block if this
// information is thrown away.
type RCodeError struct {
	RCode dnsmessage.RCode
	Host  string
}

func (e *RCodeError) Error() string {
	return fmt.Sprintf("dns: %s for %s", e.RCode, e.Host)
}

// Classify buckets a DNS resolution error into the small set of categories
// ScanResult.ErrorClass stores. Returns "" for a nil error (success).
func Classify(err error) string {
	if err == nil {
		return ""
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsNotFound:
			return "nxdomain"
		case dnsErr.IsTimeout:
			return "timeout"
		case strings.Contains(dnsErr.Err, "server misbehaving"):
			return "servfail" // system resolver's own SERVFAIL wording
		}
	}

	var rcErr *RCodeError
	if errors.As(err, &rcErr) {
		switch rcErr.RCode {
		case dnsmessage.RCodeServerFailure:
			return "servfail"
		case dnsmessage.RCodeRefused:
			return "refused"
		default:
			return "empty" // NOERROR, no A records
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}

	return "other"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/dns/... -run TestClassify -v`
Expected: PASS

- [ ] **Step 5: Wire RCode preservation into the actual resolvers**

In `internal/dns/resolver.go`, `firstA` (around line 356), change:
```go
	return "", fmt.Errorf("no A records for %s", host)
}
```
to:
```go
	return "", &RCodeError{RCode: reply.Header.RCode, Host: host}
}
```

In `internal/dns/resolver.go`, `NewDoHResolver` (around line 257), change:
```go
		for _, ans := range reply.Answers {
			if a, ok := ans.Body.(*dnsmessage.AResource); ok {
				return fmt.Sprintf("%d.%d.%d.%d", a.A[0], a.A[1], a.A[2], a.A[3]), time.Since(start).Milliseconds(), nil
			}
		}
		return "", 0, fmt.Errorf("no A records for %s", host)
	}
}
```
to:
```go
		for _, ans := range reply.Answers {
			if a, ok := ans.Body.(*dnsmessage.AResource); ok {
				return fmt.Sprintf("%d.%d.%d.%d", a.A[0], a.A[1], a.A[2], a.A[3]), time.Since(start).Milliseconds(), nil
			}
		}
		return "", 0, &RCodeError{RCode: reply.Header.RCode, Host: host}
	}
}
```

Leave `NewDoHResolverIPv6` (line ~312) untouched — AAAA lookup errors are informational-only and already ignored by callers (see `ResolveIPv6`'s doc comment), so classifying them isn't worth the churn.

- [ ] **Step 6: Run the full package test suite to verify no regression**

Run: `go test ./internal/dns/... -v`
Expected: PASS, including the existing `TestResolveNXDomain`, `TestNewResolverNXDomain`, `TestResolveKnownDomain`, `TestNewResolverKnownDomain`, `TestResolveRespectsCancellation` — none of these assert on the exact wording of a non-NXDOMAIN error, only on NXDOMAIN behavior or success, which this step doesn't touch.

- [ ] **Step 7: Commit**

```bash
git add internal/dns/classify.go internal/dns/classify_test.go internal/dns/resolver.go
git commit -m "$(cat <<'EOF'
Preserve DNS RCode instead of collapsing it to a generic error

firstA and NewDoHResolver discarded SERVFAIL/REFUSED/empty-NOERROR into
the same "no A records" string NXDOMAIN never gets. A resolver actively
refusing or failing a query now surfaces as a distinct RCodeError,
classified by the new Classify() into nxdomain/timeout/servfail/refused/
empty/other — laying the groundwork for storing that as ScanResult.ErrorClass
instead of only a free-text Error string.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TbPsEEAVzvwkM3qvb5oxK1
EOF
)"
```

---

### Task 2: Add `ScanResult.ErrorClass` column + backfill for existing rows (`internal/db`)

**Files:**
- Modify: `internal/db/models.go` (the `ScanResult` struct, currently lines 245-264)
- Modify: `internal/db/migrate.go`
- Modify: `internal/db/migrate_test.go`
- Modify: `cmd/server/main.go` (lines 77-79)

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `db.ScanResult.ErrorClass string` (JSON tag `error_class`), `db.BackfillErrorClass(ctx, *gorm.DB) error`. Task 3/4 write to the field; Task 5 reads it via JSON.

- [ ] **Step 1: Add the field to the struct**

In `internal/db/models.go`, in the `ScanResult` struct:
```go
	Error              string    `json:"error"`
```
becomes:
```go
	Error              string    `json:"error"`
	ErrorClass         string    `json:"error_class"`
```

GORM's `AutoMigrate` (run from `db.Connect`, `internal/db/db.go`) adds the column automatically on next server start — no hand-written DDL.

- [ ] **Step 2: Write the failing backfill tests**

In `internal/db/migrate_test.go`, add (matching `TestBackfillURLValues_*`'s exact style — uses the existing `rawConnect(t)` helper already in this file):
```go
func TestBackfillErrorClass_ClassifiesExistingRows(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	u, _ := s.CreateURL(ctx, "example.com")
	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	seed := []db.ScanResult{
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(), Error: "no such host"},
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(), Error: "context deadline exceeded"},
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true, ScannedAt: time.Now(), Error: "no A records for example.com"},
		{ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: false, ScannedAt: time.Now()}, // violation row, no error — must stay untouched
	}
	for i := range seed {
		if err := gormDB.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	if err := db.BackfillErrorClass(ctx, gormDB); err != nil {
		t.Fatalf("BackfillErrorClass: %v", err)
	}

	var got []db.ScanResult
	if err := gormDB.Order("id asc").Find(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	want := []string{"nxdomain", "timeout", "other", ""}
	for i, w := range want {
		if got[i].ErrorClass != w {
			t.Errorf("row %d: ErrorClass = %q, want %q", i, got[i].ErrorClass, w)
		}
	}
}

func TestBackfillErrorClass_IsIdempotent(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	u, _ := s.CreateURL(ctx, "example.com")
	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")
	if err := gormDB.Create(&db.ScanResult{
		ScanRunID: run.ID, URLID: u.ID, DNSServerID: srv.ID, Compliant: true,
		ScannedAt: time.Now(), Error: "no such host",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := db.BackfillErrorClass(ctx, gormDB); err != nil {
			t.Fatalf("BackfillErrorClass run %d: %v", i, err)
		}
	}

	var got db.ScanResult
	gormDB.First(&got)
	if got.ErrorClass != "nxdomain" {
		t.Fatalf("expected ErrorClass unchanged at nxdomain, got %q", got.ErrorClass)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/db/... -run TestBackfillErrorClass -v`
Expected: FAIL to compile — `undefined: db.BackfillErrorClass`.

- [ ] **Step 4: Implement the backfill**

In `internal/db/migrate.go`, add:
```go
// ErrorClassBatchSize caps each UPDATE to a bounded chunk of rows, same
// rationale as BackfillURLValuesBatchSize: a first-deploy run rewrites
// every pre-existing scan_results row that has an error, and an unbatched
// statement risks tripping a managed-Postgres statement_timeout.
const ErrorClassBatchSize = 1000

// errorClassMaxIterations guards against spinning forever; the CASE below
// always assigns "other" as a last resort, so a row can never fail to
// converge, but the guard matches BackfillURLValues's defensive shape.
const errorClassMaxIterations = 100000

// BackfillErrorClass assigns error_class to any pre-existing scan_results
// row that has a raw error string but no classification yet — rows
// inserted before internal/dns started preserving RCode via RCodeError.
// This can only approximate the categories internal/dns.Classify computes
// going forward, matching on the raw error text the same way
// web/src/lib/dns-error.ts's classifyDNSError used to (client-side, made
// redundant by this backfill) — the original RCode isn't recoverable after
// the fact. Idempotent: only rows with error_class = '' and a non-empty
// error are touched, so a row already classified never changes again.
func BackfillErrorClass(ctx context.Context, database *gorm.DB) error {
	const stmt = `
		UPDATE scan_results SET error_class = CASE
			WHEN LOWER(error) LIKE 'invalid url:%' THEN 'invalid_url'
			WHEN LOWER(error) LIKE '%no such host%' OR LOWER(error) LIKE '%nxdomain%' THEN 'nxdomain'
			WHEN LOWER(error) LIKE '%timeout%' OR LOWER(error) LIKE '%deadline exceeded%' THEN 'timeout'
			WHEN LOWER(error) LIKE '%server misbehaving%' OR LOWER(error) LIKE '%connection refused%' OR LOWER(error) LIKE '%servfail%' THEN 'servfail'
			ELSE 'other'
		END
		WHERE id IN (SELECT id FROM scan_results WHERE error <> '' AND error_class = '' LIMIT ?)`

	for i := 0; i < errorClassMaxIterations; i++ {
		res := database.WithContext(ctx).Exec(stmt, ErrorClassBatchSize)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
	}
	return fmt.Errorf("backfilling scan_results.error_class: did not converge after %d iterations", errorClassMaxIterations)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/db/... -run TestBackfillErrorClass -v`
Expected: PASS

- [ ] **Step 6: Wire the backfill into server startup**

In `cmd/server/main.go`, immediately after the existing `db.BackfillURLValues` call (lines 77-79):
```go
	if err := db.BackfillURLValues(context.Background(), gormDB); err != nil {
		log.Printf("backfilling scan_results.url_value: %v", err)
	}
```
add:
```go

	// Best-effort: pre-existing rows only ever had the raw error string,
	// never the structured RCode internal/dns now preserves, so this only
	// approximates the same category via substring matching. Non-fatal for
	// the same reason BackfillURLValues is — don't crash-loop startup over
	// a slow backfill.
	if err := db.BackfillErrorClass(context.Background(), gormDB); err != nil {
		log.Printf("backfilling scan_results.error_class: %v", err)
	}
```

- [ ] **Step 7: Run the full db package suite**

Run: `go test ./internal/db/... -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/db/models.go internal/db/migrate.go internal/db/migrate_test.go cmd/server/main.go
git commit -m "$(cat <<'EOF'
Add ScanResult.ErrorClass column with a one-time backfill for existing rows

Adds the DB-level home for the DNS error classification internal/dns now
computes (Task 1), plus a batched, idempotent backfill approximating the
same categories for rows inserted before that change existed.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TbPsEEAVzvwkM3qvb5oxK1
EOF
)"
```

---

### Task 3: Populate `ErrorClass` in the crawler pipeline (`internal/pipeline`)

**Files:**
- Modify: `internal/pipeline/pipeline.go`
- Modify: `internal/pipeline/pipeline_test.go`

**Interfaces:**
- Consumes: `dns.Classify(err error) string` (Task 1).
- Produces: `SiteResult.ErrorClass string` — Task 4 reads this to populate the proto message.

- [ ] **Step 1: Write the failing test**

In `internal/pipeline/pipeline_test.go`, add `"net"` to the import block:
```go
import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/pipeline"
)
```
then add:
```go
func TestCheckDNSSetsErrorClassOnNXDOMAIN(t *testing.T) {
	cfg := pipeline.Config{
		DNSWorkers:        1,
		ScreenshotWorkers: 1,
		DNSTimeout:        5 * time.Second,
		ScreenshotTimeout: 5 * time.Second,
		Resolve:           mockResolve("", &net.DNSError{Err: "no such host", IsNotFound: true}),
		Capture:           mockCapture(nil, nil),
	}

	results, err := pipeline.Run(context.Background(), []string{"https://down-site.com"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].ErrorClass != "nxdomain" {
		t.Errorf("ErrorClass = %q, want %q", results[0].ErrorClass, "nxdomain")
	}
}

func TestCheckDNSSetsErrorClassOnUnrecognizedError(t *testing.T) {
	cfg := pipeline.Config{
		DNSWorkers:        1,
		ScreenshotWorkers: 1,
		DNSTimeout:        5 * time.Second,
		ScreenshotTimeout: 5 * time.Second,
		Resolve:           mockResolve("", errors.New("connection reset")),
		Capture:           mockCapture(nil, nil),
	}

	results, err := pipeline.Run(context.Background(), []string{"https://down-site.com"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].ErrorClass != "other" {
		t.Errorf("ErrorClass = %q, want %q", results[0].ErrorClass, "other")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/... -run TestCheckDNSSetsErrorClass -v`
Expected: FAIL — `SiteResult{...}.ErrorClass` undefined field (compile error).

- [ ] **Step 3: Implement**

In `internal/pipeline/pipeline.go`, add the import:
```go
import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/afif/dns-tracking/internal/dns"
)
```

Add the field to `SiteResult` (after `Error`):
```go
	Error        string // populated on DNS failure, timeout, or screenshot error
	ErrorClass   string // "" on success; see internal/dns.Classify for categories (nxdomain/timeout/servfail/refused/empty/other/invalid_url)
```

In `checkDNS`, the invalid-URL branch:
```go
	if err != nil || u.Hostname() == "" {
		return SiteResult{
			URL:       rawURL,
			Timestamp: time.Now(),
			Compliant: true,
			Error:     "invalid URL: " + rawURL,
		}
	}
```
becomes:
```go
	if err != nil || u.Hostname() == "" {
		return SiteResult{
			URL:        rawURL,
			Timestamp:  time.Now(),
			Compliant:  true,
			Error:      "invalid URL: " + rawURL,
			ErrorClass: "invalid_url",
		}
	}
```

And the resolve-error branch:
```go
	ip, latencyMs, err := resolve(ctx, u.Hostname())
	if err != nil {
		return SiteResult{
			URL:       rawURL,
			Timestamp: time.Now(),
			Compliant: true,
			Error:     err.Error(),
		}
	}
```
becomes:
```go
	ip, latencyMs, err := resolve(ctx, u.Hostname())
	if err != nil {
		return SiteResult{
			URL:        rawURL,
			Timestamp:  time.Now(),
			Compliant:  true,
			Error:      err.Error(),
			ErrorClass: dns.Classify(err),
		}
	}
```

Leave `takeScreenshot` untouched — a screenshot-capture failure isn't a DNS-classification question, and `ErrorClass` stays `""` for it (the domain already resolved by the time `takeScreenshot` runs).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/... -v`
Expected: PASS, including all pre-existing tests (`TestCompliantSiteSkipsScreenshot`, `TestNonCompliantSiteTakesScreenshot`, `TestScreenshotFailureIsStillNonCompliant`, `TestCompliantIPStaysCompliantThroughScreenshot`, `TestMultipleSitesAllProcessed`) — none assert on `ErrorClass`, so they're unaffected by its zero value.

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/pipeline.go internal/pipeline/pipeline_test.go
git commit -m "$(cat <<'EOF'
Populate SiteResult.ErrorClass from dns.Classify in checkDNS

Every DNS resolve failure used to produce Compliant: true with no
structured signal beyond the free-text Error string. checkDNS now also
records which category of failure it was (nxdomain/timeout/servfail/
refused/empty/other/invalid_url), without changing Compliant itself.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TbPsEEAVzvwkM3qvb5oxK1
EOF
)"
```

---

### Task 4: Carry `ErrorClass` over gRPC into the DB (`proto`, `cmd/crawler`, `internal/server`)

**Files:**
- Modify: `proto/compliance.proto`
- Regenerate: `proto/compliance.pb.go`, `proto/compliance_grpc.pb.go`
- Modify: `cmd/crawler/main.go:613-625` (`buildReport`)
- Modify: `internal/server/grpc.go:104-118` (`Submit`)
- Create: `cmd/crawler/main_test.go` addition
- Modify: `internal/server/grpc_test.go`

**Interfaces:**
- Consumes: `pipeline.SiteResult.ErrorClass` (Task 3), `db.ScanResult.ErrorClass` (Task 2).
- Produces: `pb.SiteResult.ErrorClass` (generated field), persisted into `db.ScanResult.ErrorClass` on `Submit`.

- [ ] **Step 1: Add the proto field**

In `proto/compliance.proto`, `SiteResult` message:
```proto
message SiteResult {
  string url           = 1;
  int64  timestamp     = 2;
  bool   compliant     = 3;
  string resolved_ip   = 4;
  bytes  screenshot    = 5;
  string error         = 6;
  string dns_server    = 7;
  int64  latency_ms    = 8;
  string resolved_ipv6 = 9;
  string error_class   = 10;
}
```

- [ ] **Step 2: Regenerate the Go bindings**

Run:
```bash
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/compliance.proto
```
Verify: `grep -n "ErrorClass" proto/compliance.pb.go` shows the new generated field and its getter (`GetErrorClass`).

- [ ] **Step 3: Write the failing crawler-side test**

In `cmd/crawler/main_test.go`, add:
```go
func TestBuildReportIncludesErrorClass(t *testing.T) {
	results := []pipeline.SiteResult{
		{URL: "https://blocked.example", Compliant: true, Error: "no such host", ErrorClass: "nxdomain"},
	}
	report := buildReport(results)
	if got := report.Results[0].ErrorClass; got != "nxdomain" {
		t.Errorf("ErrorClass = %q, want %q", got, "nxdomain")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./cmd/crawler/... -run TestBuildReportIncludesErrorClass -v`
Expected: FAIL — `report.Results[0].ErrorClass` is `""` (zero value; not yet wired).

- [ ] **Step 5: Wire it in `buildReport`**

In `cmd/crawler/main.go`, inside `buildReport`'s loop:
```go
		pbResults[i] = &pb.SiteResult{
			Url:          r.URL,
			Timestamp:    r.Timestamp.Unix(),
			Compliant:    r.Compliant,
			ResolvedIp:   r.ResolvedIP,
			ResolvedIpv6: r.ResolvedIPv6,
			Screenshot:   r.Screenshot,
			Error:        r.Error,
			DnsServer:    r.DNSServer,
			LatencyMs:    r.LatencyMs,
		}
```
becomes:
```go
		pbResults[i] = &pb.SiteResult{
			Url:          r.URL,
			Timestamp:    r.Timestamp.Unix(),
			Compliant:    r.Compliant,
			ResolvedIp:   r.ResolvedIP,
			ResolvedIpv6: r.ResolvedIPv6,
			Screenshot:   r.Screenshot,
			Error:        r.Error,
			ErrorClass:   r.ErrorClass,
			DnsServer:    r.DNSServer,
			LatencyMs:    r.LatencyMs,
		}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./cmd/crawler/... -run TestBuildReportIncludesErrorClass -v`
Expected: PASS

- [ ] **Step 7: Write the failing server-side test**

In `internal/server/grpc_test.go`, add (matching `TestSubmitStoresResults`'s exact shape):
```go
func TestSubmitStoresErrorClass(t *testing.T) {
	store := &mockStore{
		activeScanRun: &db.ScanRun{ID: 1, Status: "running"},
		dnsServers:    []db.DNSServer{{ID: 3, Name: "Google"}},
	}
	client := newTestGRPCClient(t, store, &mockStorage{})

	_, err := client.Submit(context.Background(), &pb.ComplianceReport{
		Results: []*pb.SiteResult{
			{
				Url:        "https://blocked.example",
				Compliant:  true,
				Error:      "no such host",
				ErrorClass: "nxdomain",
				DnsServer:  "Google",
				Timestamp:  time.Now().Unix(),
			},
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(store.insertedResults) != 1 {
		t.Fatalf("expected 1 inserted result, got %d", len(store.insertedResults))
	}
	if store.insertedResults[0].ErrorClass != "nxdomain" {
		t.Errorf("ErrorClass = %q, want %q", store.insertedResults[0].ErrorClass, "nxdomain")
	}
}
```

- [ ] **Step 8: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestSubmitStoresErrorClass -v`
Expected: FAIL — inserted result's `ErrorClass` is `""`.

- [ ] **Step 9: Wire it in `Submit`**

In `internal/server/grpc.go`, inside the `db.ScanResult{...}` literal:
```go
		result := db.ScanResult{
			ScanRunID:          runID,
			URLID:              urlID,
			URLValue:           urlValue,
			DNSServerID:        dnsServerID,
			Compliant:          r.Compliant,
			ResolvedIP:         r.ResolvedIp,
			ResolvedIPv6:       r.ResolvedIpv6,
			ResolvedASN:        asn,
			ResolvedOrg:        org,
			ResolvedNetName:    netname,
			ResolvedAbuseEmail: abuseEmail,
			Error:              r.Error,
			LatencyMs:          r.GetLatencyMs(),
			ScannedAt:          time.Unix(r.Timestamp, 0),
		}
```
becomes:
```go
		result := db.ScanResult{
			ScanRunID:          runID,
			URLID:              urlID,
			URLValue:           urlValue,
			DNSServerID:        dnsServerID,
			Compliant:          r.Compliant,
			ResolvedIP:         r.ResolvedIp,
			ResolvedIPv6:       r.ResolvedIpv6,
			ResolvedASN:        asn,
			ResolvedOrg:        org,
			ResolvedNetName:    netname,
			ResolvedAbuseEmail: abuseEmail,
			Error:              r.Error,
			ErrorClass:         r.ErrorClass,
			LatencyMs:          r.GetLatencyMs(),
			ScannedAt:          time.Unix(r.Timestamp, 0),
		}
```

- [ ] **Step 10: Run tests to verify they pass**

Run: `go test ./internal/server/... ./cmd/crawler/... -v`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add proto/compliance.proto proto/compliance.pb.go proto/compliance_grpc.pb.go \
        cmd/crawler/main.go cmd/crawler/main_test.go \
        internal/server/grpc.go internal/server/grpc_test.go
git commit -m "$(cat <<'EOF'
Carry ErrorClass from crawler to server over the gRPC compliance report

Adds SiteResult.error_class (field 10) to the proto, and threads it
through buildReport → ComplianceService.Submit → db.ScanResult, so the
classification computed in the crawler (Task 3) actually reaches storage.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TbPsEEAVzvwkM3qvb5oxK1
EOF
)"
```

---

### Task 5: Frontend reads the stored classification instead of re-guessing it

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/lib/dns-error.ts`
- Modify: `web/src/routes/scan-results.tsx:265`

**Interfaces:**
- Consumes: `ScanResult.error_class` (JSON field from Task 2/4, serialized as-is by `internal/server/handlers.go`'s existing `writeJSON(w, 200, results)` — no server handler change needed).
- Produces: `classifyDNSError(errorClass: string): DnsErrorType` (same name/signature as today, so `scan-results.tsx`'s only change is which field it's called with).

- [ ] **Step 1: Add the field to the frontend type**

In `web/src/api/types.ts`, in `ScanResult`:
```ts
  screenshot_url: string
  error: string
  latency_ms: number
```
becomes:
```ts
  screenshot_url: string
  error: string
  error_class: string
  latency_ms: number
```

- [ ] **Step 2: Replace the string-guessing classifier with a direct mapping**

Replace the full contents of `web/src/lib/dns-error.ts`:
```ts
// DnsErrorType mirrors the categories internal/dns.Classify computes in Go
// and stores as ScanResult.error_class — see internal/dns/classify.go.
export type DnsErrorType = 'nxdomain' | 'timeout' | 'servfail' | 'refused' | 'empty' | 'invalid_url' | 'other' | 'none'

const KNOWN: DnsErrorType[] = ['nxdomain', 'timeout', 'servfail', 'refused', 'empty', 'invalid_url', 'other']

// classifyDNSError maps ScanResult.error_class (computed server-side, see
// internal/dns.Classify) to a typed category. Compliant rows (DNS failed)
// have a non-empty error_class; violation rows (DNS resolved) have '' →
// 'none'. Falls back to 'other' for any value this frontend doesn't
// recognize yet, rather than silently rendering nothing.
export function classifyDNSError(errorClass: string): DnsErrorType {
  if (!errorClass) return 'none'
  return (KNOWN as string[]).includes(errorClass) ? (errorClass as DnsErrorType) : 'other'
}

export function dnsErrorLabel(type: DnsErrorType): string {
  switch (type) {
    case 'nxdomain':    return 'NXDOMAIN'
    case 'timeout':     return 'Timeout'
    case 'servfail':    return 'Server Error'
    case 'refused':     return 'Refused'
    case 'empty':       return 'Empty Response'
    case 'invalid_url': return 'Invalid URL'
    case 'other':       return 'Error'
    case 'none':        return ''
  }
}
```

- [ ] **Step 3: Point the one call site at the new field**

In `web/src/routes/scan-results.tsx:265`:
```tsx
                    const errType = classifyDNSError(r.error)
```
becomes:
```tsx
                    const errType = classifyDNSError(r.error_class)
```

- [ ] **Step 4: Type-check and build**

Run: `cd web && npm run build`
Expected: succeeds — `tsc --noEmit` passes (the new `error_class` field is required on `ScanResult`, matching what the backend now always sends; `classifyDNSError`'s call site and signature are otherwise unchanged).

- [ ] **Step 5: Manually verify in the browser**

Run the full stack (`./dev.sh`), trigger a scan against a domain that resolves and one that doesn't, open `/scan-results` (via the "Scan Selected" flow, per `web/CLAUDE.md`), and confirm the "Reason" column still renders a badge (NXDOMAIN / Timeout / etc.) exactly as before — this task only changes where the category comes from, not what's displayed.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/types.ts web/src/lib/dns-error.ts web/src/routes/scan-results.tsx
git commit -m "$(cat <<'EOF'
Read DNS error classification from the server instead of re-guessing it

error_class is now computed once in Go, where the actual DNS RCode is
available (see internal/dns.Classify), and carried all the way to the
frontend. classifyDNSError becomes a thin, accurate lookup instead of
pattern-matching a free-text error string that never had the RCode info
to begin with.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TbPsEEAVzvwkM3qvb5oxK1
EOF
)"
```

---

## What this plan deliberately does not do

- **Does not change `Compliant`.** A REFUSED/SERVFAIL/timeout result is still recorded as `Compliant: true`, identically to today, everywhere that boolean is read (stats, compliance %, ISP timing, notifications, regression detection). This plan only adds a second, more trustworthy field alongside it.
- **No retry-before-finalizing.** `exchangeWithRetry` still only retries a true no-response transport failure once; a fast REFUSED/SERVFAIL answer is still accepted as final on the first attempt (that's deliberate existing behavior, see the doc comments in `resolver.go`).
- **No "uncertain" verdict state**, no dashboard alerting on a spike of non-NXDOMAIN classes, no per-sweep canary/heartbeat domain.

These were flagged during the diagnosis this plan is based on as real follow-up value (especially given the discussion that motivated it — a shared/whitelisted egress IP is exactly the kind of setup where REFUSED/SERVFAIL from rate-limiting would previously have been invisible). They're left out here because they change what "compliant" *means* and how aggregate stats are computed — a materially bigger, riskier change that deserves its own sign-off rather than riding along with a "store an error flag" request. Once this plan ships, that data exists to make an informed call on whether it's worth doing.
