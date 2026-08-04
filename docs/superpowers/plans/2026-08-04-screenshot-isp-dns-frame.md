# Screenshot ISP/DNS Frame Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Burn the ISP name and DNS server address into every screenshot's evidence frame, per DNS server, without re-introducing per-server raw pixel captures.

**Architecture:** Split capture from framing. `CaptureWithAllocator` keeps deduping raw pixel captures by `(url, resolvedIP)` exactly as today, but stops baking the frame in — it now returns the raw PNG plus the capture timestamp. A new exported `screenshot.Frame(...)` does the framing (a cheap local-HTML render, no site navigation) and gains `isp`/`dnsAddress` params for a second toolbar chip. `cmd/crawler/main.go` threads ISP+address through its DNS server list (`dnsconfig.Server` already carries both — they just weren't copied into `serverEntry`) and adds a new post-capture pass, `frameScreenshots`, that calls `screenshot.Frame` once per `pipeline.SiteResult` (i.e. once per DNS server), reusing the shared raw pixels captured once per unique `(url, resolvedIP)`.

**Tech Stack:** Go, chromedp (headless Chrome), existing `internal/screenshot`/`internal/dnsconfig`/`internal/pipeline` packages.

## Global Constraints

- Raw pixel capture stays deduped per `(url, resolvedIP)` — no perf regression on the expensive (navigate + idle-wait) path.
- Framing runs once per DNS server that maps to a given `(url, resolvedIP)` result, reusing the shared raw pixels — this is the one intentionally-repeated step, and it's cheap (local HTML render only).
- Empty `isp`/`dnsAddress` (system resolver, no DNS server context) renders no second chip — same "no server → system" fallback already documented for screenshot file paths.
- No change to the existing timestamp chip or to `captureResolved`'s `(url, resolvedIP)` dedup strategy.
- `go test ./...` must pass before this is done (screenshot/Chrome-dependent tests are skipped without `INTEGRATION=1`, same as today).

---

### Task 1: Export `screenshot.Frame` with an ISP/DNS-address chip

**Files:**
- Modify: `internal/screenshot/frame.go`
- Test: `internal/screenshot/frame_test.go` (new)

**Interfaces:**
- Produces: `Frame(chromeCtx context.Context, pageBytes []byte, rawURL string, capturedAt time.Time, isp, dnsAddress string) ([]byte, error)` — exported, replaces the old unexported `addBrowserFrame` (same 4-arg shape plus the 2 new trailing string params). Renders the existing timestamp chip plus, when `isp`/`dnsAddress` isn't both empty, a second chip `"{isp} · {dnsAddress}"` (falls back to whichever single value is non-empty if only one is set).
- Produces (unexported, for Task 1's own test): `buildFrameHTML(pageBytes []byte, rawURL string, capturedAt time.Time, isp, dnsAddress string) string` — pure string templating, no chromedp — this is what `Frame` calls before handing the HTML to chromedp.

This task only touches `internal/screenshot`; nothing here calls `Frame` yet (that's Task 3). Existing callers of the old `addBrowserFrame` still compile until Task 2 removes the only call site.

- [ ] **Step 1: Write the failing test for the pure HTML builder**

Create `internal/screenshot/frame_test.go`:

```go
package screenshot

import (
	"strings"
	"testing"
	"time"
)

func TestBuildFrameHTMLIncludesISPChipWhenBothPresent(t *testing.T) {
	html := buildFrameHTML([]byte("fake-png"), "https://example.com", time.Now(), "Cloudflare", "1.1.1.1:853")
	if !strings.Contains(html, "Cloudflare · 1.1.1.1:853") {
		t.Fatalf("expected ISP/address chip in output, got:\n%s", html)
	}
}

func TestBuildFrameHTMLOmitsChipWhenBothEmpty(t *testing.T) {
	html := buildFrameHTML([]byte("fake-png"), "https://example.com", time.Now(), "", "")
	if strings.Contains(html, "·") {
		t.Fatalf("expected no ISP/address chip when both empty, got:\n%s", html)
	}
}

func TestBuildFrameHTMLFallsBackToWhicheverSideIsSet(t *testing.T) {
	html := buildFrameHTML([]byte("fake-png"), "https://example.com", time.Now(), "Cloudflare", "")
	if !strings.Contains(html, ">Cloudflare<") {
		t.Fatalf("expected ISP-only chip, got:\n%s", html)
	}

	html = buildFrameHTML([]byte("fake-png"), "https://example.com", time.Now(), "", "1.1.1.1:853")
	if !strings.Contains(html, ">1.1.1.1:853<") {
		t.Fatalf("expected address-only chip, got:\n%s", html)
	}
}

func TestBuildFrameHTMLStillIncludesTimestampChip(t *testing.T) {
	capturedAt := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	html := buildFrameHTML([]byte("fake-png"), "https://example.com", capturedAt, "", "")
	if !strings.Contains(html, "2026-08-04 20:00:00 MYT") {
		t.Fatalf("expected timestamp chip (UTC+8), got:\n%s", html)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/screenshot/... -run TestBuildFrameHTML -v`
Expected: FAIL — `buildFrameHTML` undefined.

- [ ] **Step 3: Implement `buildFrameHTML` and exported `Frame`**

Replace the `browserHTML` toolbar markup and `addBrowserFrame` function in `internal/screenshot/frame.go`. In the `browserHTML` const, insert a placeholder right after the existing clock chip:

```html
  <div class="clock-chip">{{TIMESTAMP}}</div>
  {{ISP_CHIP}}
  <div class="menu-btn">&#8942;</div>
```

Then replace the `addBrowserFrame` function (and everything from it through the end of the file except `hostnameFromURL`) with:

```go
// ispDNSLabel joins isp and dnsAddress with " · ", falling back to whichever
// single value is set, or "" when both are empty (system resolver — no
// second chip is rendered in that case).
func ispDNSLabel(isp, dnsAddress string) string {
	switch {
	case isp != "" && dnsAddress != "":
		return isp + " · " + dnsAddress
	case isp != "":
		return isp
	default:
		return dnsAddress
	}
}

// buildFrameHTML renders the browser-mockup HTML with the page screenshot,
// timestamp chip, and (when isp/dnsAddress carry a value) a second chip
// identifying which ISP/DNS server resolved this capture. Pure string
// templating — no chromedp — so it's unit-testable without Chrome.
func buildFrameHTML(pageBytes []byte, rawURL string, capturedAt time.Time, isp, dnsAddress string) string {
	hostname := hostnameFromURL(rawURL)
	ispChip := ""
	if label := ispDNSLabel(isp, dnsAddress); label != "" {
		ispChip = `<div class="clock-chip">` + html.EscapeString(label) + `</div>`
	}
	return strings.NewReplacer(
		"{{HOSTNAME}}", html.EscapeString(hostname),
		"{{URL}}", html.EscapeString(rawURL),
		"{{TIMESTAMP}}", html.EscapeString(capturedAt.In(malaysiaTime).Format("2006-01-02 15:04:05 MST")),
		"{{ISP_CHIP}}", ispChip,
		"{{BASE64}}", base64.StdEncoding.EncodeToString(pageBytes),
	).Replace(browserHTML)
}

// Frame composites pageBytes into a Chrome-like browser mockup by navigating
// to a locally generated HTML page and screenshotting it. capturedAt is
// stamped into the mockup as a system-tray-style clock chip. isp/dnsAddress,
// when non-empty, render a second chip identifying which ISP/DNS server
// produced this capture — pass "" for both when there's no DNS server
// context (system resolver). chromeCtx is a chromedp tab context; callers
// may reuse the same one across multiple Frame calls since this only
// navigates to a local data: URL, never the target site.
func Frame(chromeCtx context.Context, pageBytes []byte, rawURL string, capturedAt time.Time, isp, dnsAddress string) ([]byte, error) {
	htmlContent := buildFrameHTML(pageBytes, rawURL, capturedAt, isp, dnsAddress)
	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(htmlContent))

	var buf []byte
	if err := chromedp.Run(chromeCtx,
		chromedp.Navigate(dataURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, contentSize, _, _, _, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}
			fullH := int64(math.Ceil(contentSize.Height))
			if err := emulation.SetDeviceMetricsOverride(1920, fullH, 1, false).Do(ctx); err != nil {
				return err
			}
			buf, err = page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatPng).
				WithCaptureBeyondViewport(true).
				Do(ctx)
			return err
		}),
	); err != nil {
		return nil, err
	}
	return buf, nil
}

func hostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return rawURL
}
```

Also add a `.clock-chip` doesn't need a new CSS class — the second chip reuses the existing `.clock-chip` style, so no CSS changes beyond the `{{ISP_CHIP}}` placeholder itself.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/screenshot/... -run TestBuildFrameHTML -v`
Expected: PASS (all 4 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/screenshot/frame.go internal/screenshot/frame_test.go
git commit -m "feat: export screenshot.Frame with an ISP/DNS-address chip"
```

---

### Task 2: Decouple raw capture from framing in `CaptureWithAllocator`

**Files:**
- Modify: `internal/screenshot/capture.go`

**Interfaces:**
- Consumes: `Frame` is no longer called from this file at all (moved to the crawler in Task 3) — this task only removes the old in-capture framing call.
- Produces: `CaptureWithAllocator(ctx, allocCtx context.Context, rawURL string, waitIdle, postIdleSleep time.Duration) ([]byte, time.Time, error)` — same params, now returns the **raw, unframed** page bytes plus `capturedAt` (needed by Task 3's later `screenshot.Frame` call) instead of framed bytes. `Capture(ctx, rawURL) ([]byte, error)` keeps its existing 2-value signature (wraps `CaptureWithAllocator`, discards the timestamp) so `capture_test.go` needs no changes.

- [ ] **Step 1: Update the failing build (no new test needed — existing `capture_test.go` covers this via `Capture`)**

`capture_test.go` already asserts `Capture(...)` returns valid unframed-or-framed PNG bytes (magic-byte check only, not frame content), so it will keep passing once `Capture` is updated in Step 2. No new test file for this task — Task 1's `frame_test.go` and Task 3's crawler tests are the coverage for the behavior split.

- [ ] **Step 2: Update `CaptureWithAllocator` and `Capture`**

In `internal/screenshot/capture.go`, change the `Capture` function to:

```go
// Capture launches a headless Chrome instance, navigates to rawURL, and returns
// a full-page PNG screenshot. The context controls the total time budget;
// cancelling it will abort the browser process.
func Capture(ctx context.Context, rawURL string) ([]byte, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, AllocatorOptions...)
	defer allocCancel()
	buf, _, err := CaptureWithAllocator(ctx, allocCtx, rawURL, 5*time.Second, 2*time.Second)
	return buf, err
}
```

Change the `CaptureWithAllocator` signature and its final block. The doc comment and body become:

```go
// CaptureWithAllocator is like Capture but uses an existing allocator context
// instead of spawning a new Chrome process. Use this with a shared allocator to
// avoid per-URL process startup overhead. waitIdle is the maximum time to wait
// for network idle after navigation; the screenshot is taken immediately when
// idle is detected or after waitIdle elapses, whichever comes first. Returns
// the raw, unframed page screenshot plus the time it was captured — callers
// that want the browser-mockup frame call screenshot.Frame separately (kept
// out of this function so the same raw pixels can be framed once per DNS
// server without repeating the expensive navigate+idle-wait capture).
func CaptureWithAllocator(ctx, allocCtx context.Context, rawURL string, waitIdle, postIdleSleep time.Duration) ([]byte, time.Time, error) {
```

Keep the body identical through the `chromedp.Run(tabCtx, ...)` call, but change its error return and drop the trailing frame call:

```go
	); err != nil {
		return nil, time.Time{}, err
	}

	return pageBuf, capturedAt, nil
}
```

(This removes the trailing `// Wrap the page screenshot in a Chrome-like browser mockup. ... framed, err := addBrowserFrame(...)` block entirely.)

- [ ] **Step 3: Build and run existing tests**

Run: `go build ./internal/... && go test ./internal/screenshot/...`
Expected: `internal/screenshot` and every other `internal/` package build clean and its tests pass (including the skipped `INTEGRATION`-gated ones). Don't run `go build ./...`/`go test ./...` yet — `cmd/crawler` still calls the old 2-value `CaptureWithAllocator`/`captureWithSchemeFallback` signature and won't build until Task 3 updates its call sites; that's expected at this checkpoint, not a regression.

- [ ] **Step 4: Commit**

```bash
git add internal/screenshot/capture.go
git commit -m "refactor: CaptureWithAllocator returns raw pixels + capturedAt, no longer frames"
```

---

### Task 3: Thread ISP/DNS-address through the crawler and frame once per DNS server

**Files:**
- Modify: `cmd/crawler/main.go`
- Test: `cmd/crawler/main_test.go`

**Interfaces:**
- Consumes: `screenshot.CaptureWithAllocator(...) ([]byte, time.Time, error)` and `screenshot.Frame(chromeCtx, pageBytes, rawURL, capturedAt, isp, dnsAddress) ([]byte, error)` from Tasks 1–2. `screenshot.AllocatorOptions` (already exported, used for the framing allocator — no `--host-resolver-rules` needed since framing never navigates to the target site).
- Produces: `serverEntry` gains `isp`, `address string` fields (alongside existing `name`, `resolve`). `captureResolved(...)` gains a third return value `map[string]time.Time` (capture timestamps keyed by `shotKey`). New function `frameScreenshots(ctx context.Context, results []pipeline.SiteResult, servers []serverEntry, capturedAts map[string]time.Time)` — mutates `results[i].Screenshot` in place.

- [ ] **Step 1: Write the failing test for `serverEntry` carrying ISP/address**

Update `TestBuildServerEntries` in `cmd/crawler/main_test.go` to also assert `isp`/`address`:

```go
func TestBuildServerEntries(t *testing.T) {
	servers := []dnsconfig.Server{
		{ISP: "Google", Name: "Google UDP", Address: "8.8.8.8:53", Protocol: "udp"},
		{ISP: "Cloudflare", Name: "Cloudflare DoT", Address: "1.1.1.1:853", Protocol: "dot"},
		{ISP: "Cloudflare", Name: "Cloudflare DoH", Address: "https://1.1.1.1/dns-query", Protocol: "doh"},
	}

	entries := buildServerEntries(servers)

	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	for i, e := range entries {
		if e.name != servers[i].Name {
			t.Errorf("entry %d: want name %q, got %q", i, servers[i].Name, e.name)
		}
		if e.isp != servers[i].ISP {
			t.Errorf("entry %d: want isp %q, got %q", i, servers[i].ISP, e.isp)
		}
		if e.address != servers[i].Address {
			t.Errorf("entry %d: want address %q, got %q", i, servers[i].Address, e.address)
		}
		if e.resolve == nil {
			t.Errorf("entry %d: resolve func is nil", i)
		}
	}
}
```

Also add a new test for `frameScreenshots`'s empty-input guard (the one path testable without launching Chrome — it returns before touching chromedp):

```go
func TestFrameScreenshotsNoOpWhenNoCapturedAts(t *testing.T) {
	results := []pipeline.SiteResult{
		{URL: "https://example.com", ResolvedIP: "1.2.3.4", DNSServer: "Cloudflare DoT", Screenshot: []byte("fake-png")},
	}
	// No capturedAts means no screenshots were actually taken this sweep;
	// frameScreenshots must return immediately without launching Chrome.
	frameScreenshots(context.Background(), results, nil, nil)

	if string(results[0].Screenshot) != "fake-png" {
		t.Errorf("expected screenshot bytes untouched, got %q", results[0].Screenshot)
	}
}
```

This test file will need `"context"` added to its imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go vet ./cmd/crawler/... ; go test ./cmd/crawler/... -run 'TestBuildServerEntries|TestFrameScreenshotsNoOpWhenNoCapturedAts' -v`
Expected: compile failure — `e.isp`/`e.address` undefined on `serverEntry`, `frameScreenshots` undefined. (The whole package also still fails to build from Task 2's signature change until this task's implementation lands — that's expected.)

- [ ] **Step 3: Update `serverEntry` and `buildServerEntries`**

In `cmd/crawler/main.go`:

```go
type serverEntry struct {
	name    string
	isp     string
	address string
	resolve func(context.Context, string) (string, int64, error)
}

// buildServerEntries converts parsed DNS server configs into resolver
// functions, used by both the --dns-servers YAML file path (CLI mode) and
// the StartSweep RPC path (listen mode, see control.go).
func buildServerEntries(servers []dnsconfig.Server) []serverEntry {
	entries := make([]serverEntry, len(servers))
	for i, s := range servers {
		var resolveFn func(context.Context, string) (string, int64, error)
		switch s.Protocol {
		case "dot":
			resolveFn = dns.NewDoTResolver(s.Address)
		case "doh":
			resolveFn = dns.NewDoHResolver(s.Address)
		default:
			resolveFn = dns.NewResolver(s.Address)
		}
		entries[i] = serverEntry{name: s.Name, isp: s.ISP, address: s.Address, resolve: resolveFn}
	}
	return entries
}
```

- [ ] **Step 4: Update `captureWithSchemeFallback` and `captureResolved` to thread `capturedAt` through**

```go
// captureWithSchemeFallback prefixes a bare hostname (watchlist URLs are
// stored scheme-less, see internal/urlnorm) with https:// so Chrome's
// navigate call accepts it as an absolute URL, then falls back to plain
// http:// if that connection is refused — some blocked/parked sites (e.g.
// domain-parking pages) only ever serve on port 80. URLs that already carry
// an explicit scheme are tried as-is, with no fallback.
func captureWithSchemeFallback(ctx, allocCtx context.Context, rawURL string, waitIdle, postIdleSleep time.Duration) ([]byte, time.Time, error) {
	if strings.Contains(rawURL, "://") {
		return screenshot.CaptureWithAllocator(ctx, allocCtx, rawURL, waitIdle, postIdleSleep)
	}
	buf, capturedAt, err := screenshot.CaptureWithAllocator(ctx, allocCtx, "https://"+rawURL, waitIdle, postIdleSleep)
	if err == nil {
		return buf, capturedAt, nil
	}
	return screenshot.CaptureWithAllocator(ctx, allocCtx, "http://"+rawURL, waitIdle, postIdleSleep)
}
```

In `captureResolved`, add a `capturedAts` map alongside `shots`/`errs`, populate it in the job goroutine, and return it:

```go
// captureResolved screenshots each unique (URL, resolvedIP) pair, forcing
// Chrome to connect to the pre-resolved IP via --host-resolver-rules so the
// screenshot reflects what that DNS server's users actually see.
// Returns the screenshot bytes, any capture errors, and each capture's
// timestamp — all keyed by shotKey(url, ip). The timestamp map is what lets
// frameScreenshots stamp the correct capture time into each DNS server's
// framed copy even though the raw capture itself only happened once.
func captureResolved(
	ctx context.Context,
	results []pipeline.SiteResult,
	ssWorkers int,
	ssTimeout time.Duration,
	waitIdle time.Duration,
	postIdleSleep time.Duration,
) (map[string][]byte, map[string]string, map[string]time.Time) {
	// Collect unique (url, ip) jobs preserving order.
	seen := make(map[string]struct{})
	var jobs []screenshotJob
	for _, r := range results {
		if !r.DNSResolved {
			continue
		}
		key := shotKey(r.URL, r.ResolvedIP)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			jobs = append(jobs, screenshotJob{url: r.URL, ip: r.ResolvedIP})
		}
	}
	if len(jobs) == 0 {
		return nil, nil, nil
	}

	shots := make(map[string][]byte, len(jobs))
	errs := make(map[string]string, len(jobs))
	capturedAts := make(map[string]time.Time, len(jobs))
	var mu sync.Mutex

	for _, group := range groupJobs(jobs) {
		// Build --host-resolver-rules for this group.
		hostSeen := make(map[string]struct{})
		var parts []string
		for _, j := range group {
			h := hostnameFromURL(j.url)
			if _, ok := hostSeen[h]; !ok {
				hostSeen[h] = struct{}{}
				parts = append(parts, "MAP "+h+" "+j.ip)
			}
		}
		rules := strings.Join(parts, ", ")

		opts := screenshot.AllocatorOptionsWithHostRules(rules)
		groupAllocCtx, groupAllocCancel := chromedp.NewExecAllocator(ctx, opts...)

		var wg sync.WaitGroup
		sem := make(chan struct{}, ssWorkers)
		for _, j := range group {
			j := j
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				siteCtx, cancel := context.WithTimeout(ctx, ssTimeout)
				defer cancel()

				buf, capturedAt, err := captureWithSchemeFallback(siteCtx, groupAllocCtx, j.url, waitIdle, postIdleSleep)
				if err != nil {
					log.Printf("screenshot failed for %s: %v", j.url, err)
					mu.Lock()
					errs[shotKey(j.url, j.ip)] = err.Error()
					mu.Unlock()
					return
				}
				mu.Lock()
				shots[shotKey(j.url, j.ip)] = buf
				capturedAts[shotKey(j.url, j.ip)] = capturedAt
				mu.Unlock()
			}()
		}
		wg.Wait()
		groupAllocCancel()
	}
	return shots, errs, capturedAts
}
```

- [ ] **Step 5: Add `frameScreenshots` and wire it into `runSweep`**

Add this new function near `assignScreenshots`:

```go
// frameScreenshots burns each result's own ISP/DNS-server chip into its
// screenshot. assignScreenshots has already copied the shared raw pixels
// (deduped per (url, resolvedIP)) into every result sharing that pair;
// this pass re-renders each one through screenshot.Frame with that specific
// result's DNS server metadata, so two servers that resolved to the same IP
// still end up with two separately (and correctly) labeled images. Framing
// only renders a local HTML wrapper (no navigation to the target site), so
// repeating it once per DNS server is cheap — a single shared Chrome tab
// handles every result sequentially.
func frameScreenshots(ctx context.Context, results []pipeline.SiteResult, servers []serverEntry, capturedAts map[string]time.Time) {
	if len(capturedAts) == 0 {
		return
	}
	byName := make(map[string]serverEntry, len(servers))
	for _, s := range servers {
		byName[s.name] = s
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, screenshot.AllocatorOptions...)
	defer allocCancel()
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()

	for i, r := range results {
		if len(r.Screenshot) == 0 {
			continue
		}
		capturedAt, ok := capturedAts[shotKey(r.URL, r.ResolvedIP)]
		if !ok {
			continue
		}
		meta := byName[r.DNSServer]
		framed, err := screenshot.Frame(tabCtx, r.Screenshot, r.URL, capturedAt, meta.isp, meta.address)
		if err != nil {
			log.Printf("framing failed for %s (%s): %v", r.URL, r.DNSServer, err)
			continue
		}
		results[i].Screenshot = framed
	}
}
```

In `runSweep`, update the screenshot phase:

```go
	// Phase 2: Screenshot each unique (URL, IP) pair (only when --screenshots is set).
	var screenshots map[string][]byte
	var screenshotErrs map[string]string
	var capturedAts map[string]time.Time
	if takeScreenshots {
		screenshots, screenshotErrs, capturedAts = captureResolved(ctx, allResults, baseCfg.ScreenshotWorkers, baseCfg.ScreenshotTimeout, waitIdle, postIdleSleep)
	}

	// Attach screenshots to the first matching result per URL; mark others shared.
	assignScreenshots(allResults, screenshots, screenshotErrs)

	// Frame each result with its own ISP/DNS-address chip — must happen after
	// assignScreenshots so every DNS server's copy gets framed, not just the
	// one raw capture per (url, resolvedIP).
	if takeScreenshots {
		frameScreenshots(ctx, allResults, servers, capturedAts)
	}
```

- [ ] **Step 6: Add `"context"` import to `main_test.go` and run all crawler tests**

Run: `go build ./... && go test ./cmd/crawler/... -v`
Expected: PASS — `TestBuildServerEntries` (with isp/address assertions), `TestFrameScreenshotsNoOpWhenNoCapturedAts`, and the pre-existing `TestAssignScreenshotsCopiesToEverySharedResult`/`TestAssignScreenshotsCopiesErrorsToEverySharedResult` all pass; whole repo builds clean.

- [ ] **Step 7: Commit**

```bash
git add cmd/crawler/main.go cmd/crawler/main_test.go
git commit -m "feat: frame each DNS server's screenshot with its own ISP/address chip"
```

---

### Task 4: Update CLAUDE.md and run full verification

**Files:**
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: nothing new — this documents Tasks 1–3's finished behavior.

- [ ] **Step 1: Update the "Screenshot capture" section of CLAUDE.md**

In the `### Screenshot capture (`internal/screenshot/`, `cmd/crawler/main.go`)` section, replace:

```
- `CaptureWithAllocator(ctx, allocCtx, rawURL, waitIdle, postIdleSleep)` — set UA + stealth JS → enable lifecycle events → navigate → wait for `networkIdle` (capped at `waitIdle`) → sleep `postIdleSleep` → full-page screenshot → frame.
```

with:

```
- `CaptureWithAllocator(ctx, allocCtx, rawURL, waitIdle, postIdleSleep) ([]byte, time.Time, error)` — set UA + stealth JS → enable lifecycle events → navigate → wait for `networkIdle` (capped at `waitIdle`) → sleep `postIdleSleep` → full-page screenshot. Returns the **raw, unframed** page bytes plus the capture timestamp — framing is a separate step (see below) so the same raw pixels captured once per unique `(url, resolvedIP)` can be framed once per DNS server without repeating the expensive navigate+idle-wait capture.
```

Replace:

```
- `frame.go`: wraps PNG in a Chrome UI mockup via a `data:text/html;base64,...` URL. Falls back to raw PNG if framing fails.
```

with:

```
- `frame.go`: `screenshot.Frame(chromeCtx, pageBytes, rawURL, capturedAt, isp, dnsAddress string) ([]byte, error)` wraps a raw PNG in a Chrome UI mockup via a `data:text/html;base64,...` URL, stamping a timestamp chip plus a second `"{isp} · {dnsAddress}"` chip (omitted when both are empty — the system-resolver case). `cmd/crawler/main.go`'s `frameScreenshots` calls this once per `pipeline.SiteResult` (i.e. once per DNS server) after `captureResolved`'s raw-pixel dedup and `assignScreenshots`'s copy-to-every-shared-result step, so two DNS servers that resolved to the same IP still each get a separately, correctly labeled framed image instead of one misattributed pair. `screenshot.AllocatorOptions` (no `--host-resolver-rules` needed, since framing never navigates to the target site) backs a single shared Chrome tab reused across all of a sweep's framing calls. Falls back to leaving the result's already-assigned raw screenshot bytes in place if framing errors for that result (logged, not fatal).
```

- [ ] **Step 2: Run the full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all packages pass (screenshot/DNS integration tests skip without `INTEGRATION=1`/network access, matching existing baseline behavior documented in CLAUDE.md's Commands section).

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for per-DNS-server screenshot framing"
```

---

## Manual Verification (not part of automated tests — do after Task 4)

Per the spec's Testing section, run `dev.sh` and:
1. Trigger a screenshot scan against 2+ DNS servers that resolve a domain to **different** IPs — confirm each captured image shows its own correct ISP+DNS address chip.
2. Trigger a screenshot scan against 2 DNS servers that resolve to the **same** IP — confirm both still get correctly, separately labeled images (not one misattributed pair, not a single shared image).
