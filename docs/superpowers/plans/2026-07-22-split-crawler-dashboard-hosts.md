# Split Crawler and Dashboard onto Separate Hosts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `Scanner.execCrawler`'s local `exec.Command`/temp-file subprocess spawn with a gRPC call to a persistent crawler process, so the crawler and dashboard can run on separate hosts.

**Architecture:** The crawler gains a `--listen-addr` mode that hosts a new `CrawlerControl.StartSweep` gRPC service (auth-gated by a shared static token); the dashboard's `Scanner` becomes a client of it instead of spawning a subprocess. `StartSweep` is a blocking unary call — it returns only once the sweep is done — so it's a drop-in replacement for `cmd.Run()` blocking until process exit; all downstream bookkeeping (`CompleteScanRun`, SSE broadcasts) is otherwise untouched. Results still flow back over the pre-existing `ComplianceService.Submit` RPC, unchanged.

**Tech Stack:** Go 1.26, gRPC (`google.golang.org/grpc` v1.81.0), Protocol Buffers (`google.golang.org/protobuf` v1.36.11), `protoc` (already installed at `/usr/bin/protoc`, v3.21.12).

**Design spec:** [docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md](../specs/2026-07-22-split-crawler-dashboard-hosts-design.md) — read this first for the full rationale; this plan implements it task-by-task.

## Global Constraints

- Auth is a shared static token compared with `crypto/subtle.ConstantTimeCompare` — not mTLS (explicitly rejected in the design spec).
- The crawler's standalone CLI mode (`--sites`, `--interval`, one-shot, printing to stdout) must keep working exactly as today — `--listen-addr` is a new orthogonal mode, not a replacement.
- No new third-party dependencies — only stdlib (`crypto/subtle`) and already-imported `google.golang.org/grpc` packages (`metadata`, `codes`, `status`, `credentials/insecure`).
- `proto/compliance.pb.go` and `proto/compliance_grpc.pb.go` are generated files — edit `proto/compliance.proto` and regenerate; never hand-edit the `.pb.go` files.
- Every existing test in `internal/server` and `internal/dnsconfig` must keep passing (`go test ./...`).

---

### Task 1: Extend the proto definition and regenerate Go bindings

**Files:**
- Modify: `proto/compliance.proto`
- Regenerate: `proto/compliance.pb.go`, `proto/compliance_grpc.pb.go`

**Interfaces:**
- Produces: `pb.DNSServerConfig{Isp, Name, Address, Protocol string}`, `pb.SweepRequest{Urls []string, DnsServers []*DNSServerConfig, CompliantIps []string, Screenshots bool}`, `pb.SweepAck{Accepted bool, Error string}`, `pb.CrawlerControlServer` (interface with `StartSweep(context.Context, *SweepRequest) (*SweepAck, error)`), `pb.CrawlerControlClient` (interface with `StartSweep(ctx context.Context, in *SweepRequest, opts ...grpc.CallOption) (*SweepAck, error)`), `pb.UnimplementedCrawlerControlServer`, `pb.RegisterCrawlerControlServer(s grpc.ServiceRegistrar, srv CrawlerControlServer)`, `pb.NewCrawlerControlClient(cc grpc.ClientConnInterface) CrawlerControlClient`. All later tasks depend on these exact names.

- [ ] **Step 1: Append the new messages and service to the proto file**

Open `proto/compliance.proto`. It currently ends with:

```proto
service ComplianceService {
  rpc Submit(ComplianceReport) returns (Acknowledgement);
}
```

Append after that closing brace:

```proto

message DNSServerConfig {
  string isp      = 1;
  string name     = 2;
  string address  = 3;
  string protocol = 4;
}

message SweepRequest {
  repeated string          urls          = 1;
  repeated DNSServerConfig dns_servers   = 2;
  repeated string          compliant_ips = 3;
  bool                     screenshots   = 4;
}

message SweepAck {
  bool   accepted = 1;
  string error    = 2;
}

service CrawlerControl {
  rpc StartSweep(SweepRequest) returns (SweepAck);
}
```

- [ ] **Step 2: Install the protoc plugins (not present in this environment)**

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

Verify both are on PATH:

```bash
which protoc-gen-go protoc-gen-go-grpc
```

Expected: both print a path under `$(go env GOPATH)/bin`.

- [ ] **Step 3: Regenerate the Go bindings**

```bash
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/compliance.proto
```

Expected: no output on success; `proto/compliance.pb.go` and `proto/compliance_grpc.pb.go` are modified (check with `git diff --stat proto/`).

- [ ] **Step 4: Verify the module still builds**

```bash
go build ./...
```

Expected: succeeds with no errors (nothing references the new types yet, so this just confirms the generated code itself compiles).

- [ ] **Step 5: Commit**

```bash
git add proto/compliance.proto proto/compliance.pb.go proto/compliance_grpc.pb.go
git commit -m "$(cat <<'EOF'
feat(proto): add CrawlerControl.StartSweep RPC for remote sweep triggering

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Extract `buildServerEntries` helper in the crawler

**Files:**
- Modify: `cmd/crawler/main.go`
- Test: `cmd/crawler/main_test.go` (new)

**Interfaces:**
- Consumes: `dnsconfig.Server{ISP, Name, Address, Protocol string}` (existing, `internal/dnsconfig/config.go`), `dns.NewDoTResolver/NewDoHResolver/NewResolver(addr string) func(context.Context, string) (string, int64, error)` (existing, `internal/dns`).
- Produces: `buildServerEntries(servers []dnsconfig.Server) []serverEntry` — used by Task 4's RPC handler in addition to the existing CLI path in this file.

This is a pure refactor (identical behavior, same inline logic moved into a named function) so it can be tested directly rather than driven by a failing-test-first cycle — the test below pins the extracted function's behavior once it exists.

- [ ] **Step 1: Extract the function**

In `cmd/crawler/main.go`, find the `serverEntry` type definition (around line 131):

```go
type serverEntry struct {
	name    string
	resolve func(context.Context, string) (string, int64, error)
}
```

Add immediately after it:

```go

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
		entries[i] = serverEntry{name: s.Name, resolve: resolveFn}
	}
	return entries
}
```

Now find this block in `main()` (around line 54-78):

```go
	// Build the list of DNS servers to query. Default: system resolver.
	var servers []serverEntry
	if *dnsServers != "" {
		cfg, err := dnsconfig.Load(*dnsServers)
		if err != nil {
			log.Fatalf("loading DNS servers: %v", err)
		}
		for _, s := range cfg.Servers {
			var resolveFn func(context.Context, string) (string, int64, error)
			switch s.Protocol {
			case "dot":
				resolveFn = dns.NewDoTResolver(s.Address)
			case "doh":
				resolveFn = dns.NewDoHResolver(s.Address)
			default:
				resolveFn = dns.NewResolver(s.Address)
			}
			servers = append(servers, serverEntry{
				name:    s.Name,
				resolve: resolveFn,
			})
		}
	} else {
		servers = []serverEntry{{name: "", resolve: dns.Resolve}}
	}
```

Replace it with:

```go
	// Build the list of DNS servers to query. Default: system resolver.
	var servers []serverEntry
	if *dnsServers != "" {
		cfg, err := dnsconfig.Load(*dnsServers)
		if err != nil {
			log.Fatalf("loading DNS servers: %v", err)
		}
		servers = buildServerEntries(cfg.Servers)
	} else {
		servers = []serverEntry{{name: "", resolve: dns.Resolve}}
	}
```

- [ ] **Step 2: Write the test**

Create `cmd/crawler/main_test.go`:

```go
package main

import "testing"

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
		if e.resolve == nil {
			t.Errorf("entry %d: resolve func is nil", i)
		}
	}
}
```

This needs the `dnsconfig` import; add it:

```go
package main

import (
	"testing"

	"github.com/afif/dns-tracking/internal/dnsconfig"
)

func TestBuildServerEntries(t *testing.T) {
	...
}
```

- [ ] **Step 3: Run the test**

```bash
go test ./cmd/crawler/... -run TestBuildServerEntries -v
```

Expected: `PASS`.

- [ ] **Step 4: Verify the crawler still builds and its existing manual behavior is unaffected**

```bash
go build -o crawler ./cmd/crawler/
./crawler --sites site-list.txt --dns-timeout 3 2>&1 | head -5
```

Expected: runs a normal DNS-only sweep exactly as before (output table), confirming the refactor changed nothing observable.

- [ ] **Step 5: Commit**

```bash
git add cmd/crawler/main.go cmd/crawler/main_test.go
git commit -m "$(cat <<'EOF'
refactor(crawler): extract buildServerEntries for reuse by the new RPC trigger path

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Crawler auth interceptor

**Files:**
- Create: `cmd/crawler/control.go`
- Test: `cmd/crawler/control_test.go` (new)

**Interfaces:**
- Produces: `const authMetadataKey = "x-auth-token"`, `authInterceptor(token string) grpc.UnaryServerInterceptor` — consumed by Task 4's `runListenMode`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/crawler/control_test.go`:

```go
package main

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthInterceptorRejectsMissingToken(t *testing.T) {
	interceptor := authInterceptor("secret")
	ctx := context.Background() // no metadata attached

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	})

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

func TestAuthInterceptorRejectsWrongToken(t *testing.T) {
	interceptor := authInterceptor("secret")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-auth-token", "wrong"))

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	})

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

func TestAuthInterceptorAcceptsCorrectToken(t *testing.T) {
	interceptor := authInterceptor("secret")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-auth-token", "secret"))

	called := false
	resp, err := interceptor(ctx, "req", &grpc.UnaryServerInfo{}, func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
	if resp != "ok" {
		t.Fatalf("unexpected response: %v", resp)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./cmd/crawler/... -run TestAuthInterceptor -v
```

Expected: FAIL — `authInterceptor` is not defined.

- [ ] **Step 3: Implement `authInterceptor`**

Create `cmd/crawler/control.go`:

```go
package main

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const authMetadataKey = "x-auth-token"

// authInterceptor rejects any RPC whose x-auth-token metadata doesn't match
// token, using a constant-time comparison — this is a real credential
// check, not a lookup, so timing must not leak partial matches.
func authInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing auth token")
		}
		got := strings.Join(md.Get(authMetadataKey), "")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid auth token")
		}
		return handler(ctx, req)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./cmd/crawler/... -run TestAuthInterceptor -v
```

Expected: all 3 `PASS`.

- [ ] **Step 5: Commit**

```bash
git add cmd/crawler/control.go cmd/crawler/control_test.go
git commit -m "$(cat <<'EOF'
feat(crawler): add shared-token auth interceptor for the control service

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Crawler control server and `--listen-addr` mode

**Files:**
- Modify: `cmd/crawler/control.go`
- Modify: `cmd/crawler/main.go`

**Interfaces:**
- Consumes: `buildServerEntries` (Task 2), `authInterceptor` (Task 3), `runSweep(ctx context.Context, urls []string, servers []serverEntry, baseCfg pipeline.Config, waitIdle, postIdleSleep time.Duration, conn *grpc.ClientConn, takeScreenshots bool)` (existing, unmodified, in `main.go`), `pb.SweepRequest`/`pb.SweepAck`/`pb.RegisterCrawlerControlServer`/`pb.UnimplementedCrawlerControlServer` (Task 1), `dnsconfig.Server` (existing).
- Produces: `runListenMode(listenAddr, authToken, grpcAddr string, dnsWorkers, ssWorkers int, dnsTimeout, ssTimeout, waitIdle, postIdleSleep time.Duration)` — called from `main()`.

- [ ] **Step 1: Add the control server type to `cmd/crawler/control.go`**

Append to `cmd/crawler/control.go` (below `authInterceptor`):

```go

// controlServer implements CrawlerControl.StartSweep, reusing runSweep — the
// same function the standalone CLI path calls — against DNS servers and
// URLs carried in the request instead of parsed from local files.
type controlServer struct {
	pb.UnimplementedCrawlerControlServer
	conn          *grpc.ClientConn
	baseCfg       pipeline.Config
	waitIdle      time.Duration
	postIdleSleep time.Duration

	mu      sync.Mutex
	running bool
}

func newControlServer(conn *grpc.ClientConn, baseCfg pipeline.Config, waitIdle, postIdleSleep time.Duration) *controlServer {
	return &controlServer{conn: conn, baseCfg: baseCfg, waitIdle: waitIdle, postIdleSleep: postIdleSleep}
}

func (s *controlServer) StartSweep(ctx context.Context, req *pb.SweepRequest) (*pb.SweepAck, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return &pb.SweepAck{Accepted: false, Error: "sweep already in progress"}, nil
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	servers := buildServerEntries(dnsServerConfigsToServers(req.DnsServers))

	cfg := s.baseCfg
	cfg.CompliantIPs = req.CompliantIps

	runSweep(ctx, req.Urls, servers, cfg, s.waitIdle, s.postIdleSleep, s.conn, req.Screenshots)

	return &pb.SweepAck{Accepted: true}, nil
}

func dnsServerConfigsToServers(configs []*pb.DNSServerConfig) []dnsconfig.Server {
	servers := make([]dnsconfig.Server, len(configs))
	for i, c := range configs {
		servers[i] = dnsconfig.Server{ISP: c.Isp, Name: c.Name, Address: c.Address, Protocol: c.Protocol}
	}
	return servers
}

// runListenMode starts a persistent gRPC server exposing CrawlerControl,
// used by the dashboard to trigger sweeps remotely instead of exec'ing this
// binary as a local subprocess — see
// docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md.
func runListenMode(listenAddr, authToken, grpcAddr string, dnsWorkers, ssWorkers int, dnsTimeout, ssTimeout, waitIdle, postIdleSleep time.Duration) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var conn *grpc.ClientConn
	if grpcAddr != "" {
		var err error
		conn, err = grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("connecting to gRPC server: %v", err)
		}
		defer conn.Close()
	}

	baseCfg := pipeline.Config{
		DNSWorkers:        dnsWorkers,
		ScreenshotWorkers: ssWorkers,
		DNSTimeout:        dnsTimeout,
		ScreenshotTimeout: ssTimeout,
	}

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(authInterceptor(authToken)))
	pb.RegisterCrawlerControlServer(grpcSrv, newControlServer(conn, baseCfg, waitIdle, postIdleSleep))

	go func() {
		<-ctx.Done()
		log.Println("shutting down")
		grpcSrv.GracefulStop()
	}()

	log.Printf("crawler control listening on %s", listenAddr)
	if err := grpcSrv.Serve(lis); err != nil {
		log.Printf("serve: %v", err)
	}
}
```

Update the import block at the top of `cmd/crawler/control.go` to:

```go
package main

import (
	"context"
	"crypto/subtle"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/afif/dns-tracking/internal/dnsconfig"
	"github.com/afif/dns-tracking/internal/pipeline"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)
```

- [ ] **Step 2: Wire `--listen-addr`/`--auth-token` into `main()`**

In `cmd/crawler/main.go`, find:

```go
	compliantIPsFlag := flag.String("compliant-ips", "", "comma-separated IPs treated as compliant even when DNS resolves (e.g. MCMC block-page IP)")
	flag.Parse()
```

Replace with:

```go
	compliantIPsFlag := flag.String("compliant-ips", "", "comma-separated IPs treated as compliant even when DNS resolves (e.g. MCMC block-page IP)")
	listenAddr := flag.String("listen-addr", "", "gRPC listen address for persistent trigger mode (e.g. :50052); when set, runs as a long-lived server instead of a one-shot sweep")
	authToken := flag.String("auth-token", "", "shared secret required on incoming StartSweep RPCs when --listen-addr is set")
	flag.Parse()

	if *listenAddr != "" {
		runListenMode(*listenAddr, *authToken, *grpcAddr, *dnsWorkers, *ssWorkers,
			time.Duration(*dnsTimeoutSec)*time.Second, time.Duration(*ssTimeoutSec)*time.Second,
			time.Duration(*waitIdleSec)*time.Second, time.Duration(*postIdleSleepMs)*time.Millisecond)
		return
	}
```

Note this reads `*grpcAddr`, `*dnsWorkers`, `*ssWorkers`, `*dnsTimeoutSec`, `*ssTimeoutSec`, `*waitIdleSec`, `*postIdleSleepMs` — all already declared earlier in the flag block (lines 32-43 of the original file), so no new flag-order dependency issues.

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: succeeds. (No test here — the auth interceptor is already tested in Task 3, and `controlServer.StartSweep`/`runListenMode` are integration-shaped code exercised by Task 10's manual end-to-end check rather than a unit test, since testing them meaningfully requires a real listener and a real `runSweep` call.)

- [ ] **Step 4: Manually smoke-test listen mode starts and rejects bad auth**

```bash
go build -o crawler ./cmd/crawler/
./crawler --listen-addr :50099 --auth-token smoke-test &
CRAWLER_SMOKE_PID=$!
sleep 1
grpcurl -plaintext -H "x-auth-token: wrong" -d '{}' localhost:50099 compliance.CrawlerControl/StartSweep || true
kill $CRAWLER_SMOKE_PID
```

Expected: the process logs `crawler control listening on :50099`; the `grpcurl` call (skip this specific sub-step if `grpcurl` isn't installed — the interceptor is already unit-tested in Task 3) returns an `Unauthenticated` error rather than hanging or crashing the process.

- [ ] **Step 5: Commit**

```bash
git add cmd/crawler/control.go cmd/crawler/main.go
git commit -m "$(cat <<'EOF'
feat(crawler): add --listen-addr persistent control-service mode

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Scanner — replace exec with a gRPC `StartSweep` call

**Files:**
- Modify: `internal/server/scanner.go` (full rewrite)
- Modify: `internal/server/scanner_test.go` (full rewrite)

**Interfaces:**
- Consumes: `pb.SweepRequest`/`pb.SweepAck`/`pb.DNSServerConfig` (Task 1), `db.Store` methods (existing, unchanged: `ListWatchedURLs`, `ListDNSServers`, `ListCompliantIPs`, `CreateScanRun`, `CompleteScanRun`), `urlnorm.Normalize` (existing), `buildProgressPayload` (existing, unchanged, in `internal/server/handlers.go` or similar — not modified by this task).
- Produces: `crawlerClient` interface (`StartSweep(ctx context.Context, req *pb.SweepRequest, opts ...grpc.CallOption) (*pb.SweepAck, error)`), `NewScanner(crawler crawlerClient, crawlerToken string, store db.Store, broadcaster *Broadcaster) *Scanner` — **signature change**, consumed by Task 6's `cmd/server/main.go`.

- [ ] **Step 1: Write the failing tests first**

Replace the full contents of `internal/server/scanner_test.go`:

```go
package server_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/server"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
)

// fakeCrawlerClient stands in for the crawler's StartSweep RPC without a
// real network connection — see the crawlerClient interface in
// internal/server/scanner.go.
type fakeCrawlerClient struct {
	delay    time.Duration
	rejected bool
	err      error

	mu    sync.Mutex
	calls int
}

func (f *fakeCrawlerClient) StartSweep(_ context.Context, _ *pb.SweepRequest, _ ...grpc.CallOption) (*pb.SweepAck, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.rejected {
		return &pb.SweepAck{Accepted: false, Error: "busy"}, nil
	}
	return &pb.SweepAck{Accepted: true}, nil
}

// completionCapture records CreateScanRun and CompleteScanRun calls.
type completionCapture struct {
	db.Store
	created   []db.ScanRun
	completed []uint
}

func (c *completionCapture) CreateScanRun(_ context.Context, by string) (db.ScanRun, error) {
	r := db.ScanRun{ID: uint(len(c.created) + 1), TriggeredBy: by, Status: "running", StartedAt: time.Now()}
	c.created = append(c.created, r)
	return r, nil
}
func (c *completionCapture) CompleteScanRun(_ context.Context, id uint, _ string, _ time.Time) error {
	c.completed = append(c.completed, id)
	return nil
}
func (c *completionCapture) ActiveScanRun(_ context.Context) (*db.ScanRun, error) { return nil, nil }
func (c *completionCapture) ListWatchedURLs(_ context.Context) ([]db.URL, error) {
	return []db.URL{{ID: 1, URL: "https://example.com"}}, nil
}
func (c *completionCapture) ListDNSServers(_ context.Context) ([]db.DNSServer, error) {
	return []db.DNSServer{{ID: 1, Name: "G", ISP: "Google", Address: "8.8.8.8:53", Protocol: "udp"}}, nil
}
func (c *completionCapture) ListCompliantIPs(_ context.Context) ([]db.CompliantIP, error) {
	return nil, nil
}
func (c *completionCapture) LastScanRun(_ context.Context) (*db.ScanRun, error) {
	if len(c.created) == 0 {
		return nil, nil
	}
	r := c.created[len(c.created)-1]
	return &r, nil
}
func (c *completionCapture) ScanProgress(_ context.Context, _ uint) ([]db.ProgressEntry, error) {
	return []db.ProgressEntry{{DNSServerID: 1, Name: "G", Completed: 0}}, nil
}

func waitUntil(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestScannerTargetedURLs(t *testing.T) {
	crawler := &fakeCrawlerClient{}
	store := &completionCapture{}
	sc := server.NewScanner(crawler, "test-token", store, nil)

	if err := sc.Trigger(context.Background(), "manual", []string{"example.com", "https://EXAMPLE.COM"}); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	waitUntil(t, func() bool { return !sc.IsRunning() }, 3*time.Second)

	if len(store.completed) == 0 {
		t.Fatal("expected CompleteScanRun to be called")
	}
}

func TestScannerTriggerRunsAndCompletes(t *testing.T) {
	crawler := &fakeCrawlerClient{}
	store := &completionCapture{}
	sc := server.NewScanner(crawler, "test-token", store, nil)

	if err := sc.Trigger(context.Background(), "manual", nil); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	waitUntil(t, func() bool { return !sc.IsRunning() }, 3*time.Second)

	if len(store.completed) == 0 {
		t.Fatal("expected CompleteScanRun to be called")
	}
}

func TestScannerPublishesInitialProgressBeforeCrawlerRuns(t *testing.T) {
	// The fake crawler sleeps briefly so the test can observe the initial
	// (pre-sweep) publish before the completion publish that fires once
	// StartSweep returns.
	crawler := &fakeCrawlerClient{delay: 200 * time.Millisecond}
	store := &completionCapture{}
	broadcaster := server.NewBroadcaster()
	ch := broadcaster.Subscribe()
	sc := server.NewScanner(crawler, "test-token", store, broadcaster)

	if err := sc.Trigger(context.Background(), "manual", nil); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	var payload struct {
		ScanRun   db.ScanRun         `json:"scan_run"`
		TotalURLs int                `json:"total_urls"`
		PerDNS    []db.ProgressEntry `json:"per_dns"`
	}
	select {
	case data := <-ch:
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected an initial progress publish before the crawler produced any results")
	}

	if payload.ScanRun.Status != "running" {
		t.Fatalf("expected running status on initial publish, got %q", payload.ScanRun.Status)
	}
	for _, e := range payload.PerDNS {
		if e.Completed != 0 {
			t.Fatalf("expected 0 completed on initial publish, got %d for %s", e.Completed, e.Name)
		}
	}

	waitUntil(t, func() bool { return !sc.IsRunning() }, 3*time.Second)
}

func TestScannerRejectsConcurrentRun(t *testing.T) {
	// Sleeps briefly so the second Trigger hits while the first is still running.
	crawler := &fakeCrawlerClient{delay: 300 * time.Millisecond}
	store := &completionCapture{}
	sc := server.NewScanner(crawler, "test-token", store, nil)

	_ = sc.Trigger(context.Background(), "manual", nil)
	err := sc.Trigger(context.Background(), "manual", nil)
	if err == nil {
		t.Fatal("expected error for concurrent scan")
	}

	// Wait for first scan to finish.
	waitUntil(t, func() bool { return !sc.IsRunning() }, 3*time.Second)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/server/... -run TestScanner -v
```

Expected: FAIL — `server.NewScanner` still has the old `(crawlerPath, grpcAddr string, ...)` signature, so this won't even compile yet.

- [ ] **Step 3: Rewrite `internal/server/scanner.go`**

Replace the full contents of `internal/server/scanner.go`:

```go
package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/urlnorm"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// crawlerAuthMetadataKey carries the shared secret required by the
// crawler's StartSweep RPC — must match authMetadataKey in cmd/crawler's
// authInterceptor.
const crawlerAuthMetadataKey = "x-auth-token"

// crawlerClient is the subset of pb.CrawlerControlClient the Scanner needs,
// narrowed to a small interface so tests can inject a fake instead of
// standing up a real gRPC server — see
// docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md.
type crawlerClient interface {
	StartSweep(ctx context.Context, req *pb.SweepRequest, opts ...grpc.CallOption) (*pb.SweepAck, error)
}

type Scanner struct {
	crawler      crawlerClient
	crawlerToken string
	store        db.Store
	broadcaster  *Broadcaster
	mu           sync.Mutex
	running      bool
}

func NewScanner(crawler crawlerClient, crawlerToken string, store db.Store, broadcaster *Broadcaster) *Scanner {
	return &Scanner{crawler: crawler, crawlerToken: crawlerToken, store: store, broadcaster: broadcaster}
}

func (sc *Scanner) IsRunning() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.running
}

// Trigger starts a DNS-only scan in a background goroutine. urls is an
// optional list of specific domains to scan; nil or empty means scan all
// enabled watched URLs. Returns an error if a scan is already in progress.
func (sc *Scanner) Trigger(ctx context.Context, triggeredBy string, urls []string) error {
	sc.mu.Lock()
	if sc.running {
		sc.mu.Unlock()
		return errors.New("scan already in progress")
	}
	sc.running = true
	sc.mu.Unlock()

	go sc.run(context.WithoutCancel(ctx), triggeredBy, urls)
	return nil
}

// TriggerScreenshot spawns a single-URL scan with screenshots enabled.
func (sc *Scanner) TriggerScreenshot(ctx context.Context, rawURL string, dnsServerID uint) error {
	sc.mu.Lock()
	if sc.running {
		sc.mu.Unlock()
		return errors.New("scan already in progress")
	}
	sc.running = true
	sc.mu.Unlock()

	go sc.runScreenshot(context.WithoutCancel(ctx), rawURL, dnsServerID)
	return nil
}

func (sc *Scanner) run(ctx context.Context, triggeredBy string, requestedURLs []string) {
	defer sc.setRunning(false)

	var urlObjs []db.URL
	if len(requestedURLs) == 0 {
		var err error
		urlObjs, err = sc.store.ListWatchedURLs(ctx)
		if err != nil || len(urlObjs) == 0 {
			log.Printf("scanner: no URLs to scan (err=%v)", err)
			return
		}
	} else {
		seen := make(map[string]bool)
		for _, raw := range requestedURLs {
			norm, err := urlnorm.Normalize(raw)
			if err != nil {
				continue
			}
			if !seen[norm] {
				seen[norm] = true
				urlObjs = append(urlObjs, db.URL{URL: norm})
			}
		}
		if len(urlObjs) == 0 {
			log.Printf("scanner: no valid URLs in targeted scan request")
			return
		}
	}

	servers, err := sc.store.ListDNSServers(ctx)
	if err != nil {
		log.Printf("scanner: load DNS servers: %v", err)
		return
	}

	run, err := sc.store.CreateScanRun(ctx, triggeredBy)
	if err != nil {
		log.Printf("scanner: create scan run: %v", err)
		return
	}

	req := &pb.SweepRequest{
		Urls:         urlValues(urlObjs),
		DnsServers:   dnsServersToProto(servers),
		CompliantIps: sc.compliantIPs(ctx),
	}
	sc.runCrawler(ctx, req, run.ID)
}

func (sc *Scanner) runScreenshot(ctx context.Context, rawURL string, dnsServerID uint) {
	defer sc.setRunning(false)

	servers, err := sc.store.ListDNSServers(ctx)
	if err != nil {
		log.Printf("scanner: load DNS servers: %v", err)
		return
	}

	var target []db.DNSServer
	for _, s := range servers {
		if s.ID == dnsServerID {
			target = []db.DNSServer{s}
			break
		}
	}
	if len(target) == 0 {
		log.Printf("scanner: DNS server ID %d not found", dnsServerID)
		return
	}

	run, err := sc.store.CreateScanRun(ctx, "screenshot")
	if err != nil {
		log.Printf("scanner: create scan run: %v", err)
		return
	}

	req := &pb.SweepRequest{
		Urls:         []string{rawURL},
		DnsServers:   dnsServersToProto(target),
		CompliantIps: sc.compliantIPs(ctx),
		Screenshots:  true,
	}
	sc.runCrawler(ctx, req, run.ID)
}

// compliantIPs fetches the compliant IPs from the store as a string slice
// for SweepRequest.CompliantIps. Returns nil if the list is empty or the
// fetch fails (non-fatal; scan proceeds without it).
func (sc *Scanner) compliantIPs(ctx context.Context) []string {
	ips, err := sc.store.ListCompliantIPs(ctx)
	if err != nil {
		log.Printf("scanner: load compliant IPs: %v", err)
		return nil
	}
	addrs := make([]string, len(ips))
	for i, ip := range ips {
		addrs[i] = ip.Address
	}
	return addrs
}

func (sc *Scanner) runCrawler(ctx context.Context, req *pb.SweepRequest, runID uint) {
	// Publish the fresh (0-completed) run immediately, before the crawler
	// produces any results — otherwise SSE subscribers keep showing the
	// previous run's final tally until the first result streams in, which
	// reads as the progress bar starting full and then resetting.
	if sc.broadcaster != nil {
		if data, err := buildProgressPayload(ctx, sc.store); err == nil && data != nil {
			sc.broadcaster.Publish(data)
		}
	}

	authedCtx := metadata.AppendToOutgoingContext(ctx, crawlerAuthMetadataKey, sc.crawlerToken)
	status := "completed"
	ack, err := sc.crawler.StartSweep(authedCtx, req)
	if err != nil {
		log.Printf("scanner: crawler StartSweep failed: %v", err)
		status = "failed"
	} else if !ack.Accepted {
		log.Printf("scanner: crawler rejected sweep: %s", ack.Error)
		status = "failed"
	}
	now := time.Now()
	_ = sc.store.CompleteScanRun(ctx, runID, status, now)

	// StartSweep only returns once the crawler has finished streaming
	// results via Submit; nothing else announces the run flipping to
	// completed/failed, so SSE subscribers would be stuck on the last
	// "running" payload forever without this.
	if sc.broadcaster != nil {
		if data, err := buildProgressPayload(ctx, sc.store); err == nil && data != nil {
			sc.broadcaster.Publish(data)
		}
	}
}

func (sc *Scanner) setRunning(v bool) {
	sc.mu.Lock()
	sc.running = v
	sc.mu.Unlock()
}

func urlValues(urls []db.URL) []string {
	vals := make([]string, len(urls))
	for i, u := range urls {
		vals[i] = u.URL
	}
	return vals
}

func dnsServersToProto(servers []db.DNSServer) []*pb.DNSServerConfig {
	out := make([]*pb.DNSServerConfig, len(servers))
	for i, s := range servers {
		out[i] = &pb.DNSServerConfig{Isp: s.ISP, Name: s.Name, Address: s.Address, Protocol: s.Protocol}
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/server/... -run TestScanner -v
```

Expected: all 4 tests `PASS`.

- [ ] **Step 5: Run the full `internal/server` suite to check for regressions**

```bash
go test ./internal/server/...
```

Expected: `ok` — `grpc_test.go`/`handlers_test.go` are untouched by this task and should be unaffected (they don't construct a `Scanner`).

- [ ] **Step 6: Commit**

```bash
git add internal/server/scanner.go internal/server/scanner_test.go
git commit -m "$(cat <<'EOF'
feat(server): trigger sweeps via gRPC StartSweep instead of exec.Command

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Wire the dashboard's `cmd/server/main.go` to the crawler client

**Files:**
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `server.NewScanner(crawler crawlerClient, crawlerToken string, ...)` (Task 5 — note `crawlerClient` is unexported, so `main.go` passes a `pb.CrawlerControlClient` value, which satisfies the interface structurally), `pb.NewCrawlerControlClient(cc grpc.ClientConnInterface) pb.CrawlerControlClient` (Task 1).

- [ ] **Step 1: Replace the `--crawler-path` flag**

In `cmd/server/main.go`, find:

```go
	crawlerPath := flag.String("crawler-path", envOr("CRAWLER_PATH", "./crawler"), "path to crawler binary")
```

Replace with:

```go
	crawlerAddr := flag.String("crawler-addr", envOr("CRAWLER_ADDR", "localhost:50052"), "gRPC address of the crawler's control service")
	crawlerToken := flag.String("crawler-token", envOr("CRAWLER_TOKEN", ""), "shared secret sent with StartSweep RPCs; must match the crawler's --auth-token")
```

- [ ] **Step 2: Dial the crawler and construct the Scanner with the client instead of the path**

Find:

```go
	broadcaster := server.NewBroadcaster()

	// Scanner manages crawler subprocess lifecycle.
	sc := server.NewScanner(*crawlerPath, *grpcAddr, store, broadcaster)
```

Replace with:

```go
	broadcaster := server.NewBroadcaster()

	// Scanner triggers sweeps on the crawler's persistent control service
	// over gRPC instead of exec'ing a local subprocess — see
	// docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md.
	crawlerConn, err := grpc.NewClient(*crawlerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("connecting to crawler: %v", err)
	}
	defer crawlerConn.Close()
	sc := server.NewScanner(pb.NewCrawlerControlClient(crawlerConn), *crawlerToken, store, broadcaster)
```

- [ ] **Step 3: Add the missing import**

Find the import block:

```go
import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/dnsconfig"
	"github.com/afif/dns-tracking/internal/favicon"
	"github.com/afif/dns-tracking/internal/ipinfo"
	"github.com/afif/dns-tracking/internal/server"
	"github.com/afif/dns-tracking/internal/storage"
	"github.com/afif/dns-tracking/internal/subfinder"
	"github.com/afif/dns-tracking/internal/whois"
	pb "github.com/afif/dns-tracking/proto"
	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
)
```

Add `"google.golang.org/grpc/credentials/insecure"` after the `"google.golang.org/grpc"` line:

```go
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/driver/postgres"
```

- [ ] **Step 4: Build**

```bash
go build ./...
```

Expected: succeeds.

- [ ] **Step 5: Run the full test suite**

```bash
go test ./...
```

Expected: `ok` for every package (screenshot tests skip without Chrome; `internal/dns` needs network — both as documented in `CLAUDE.md`).

- [ ] **Step 6: Commit**

```bash
git add cmd/server/main.go
git commit -m "$(cat <<'EOF'
feat(server): dial the crawler's control service via --crawler-addr/--crawler-token

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Update `dev.sh` to launch the crawler as a persistent listener

**Files:**
- Modify: `dev.sh`

- [ ] **Step 1: Track a `CRAWLER_PID` and kill it in cleanup**

Find:

```bash
DB_URL="host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"
SERVER_PID=""
VITE_PID=""

cleanup() {
  echo ""
  echo "Shutting down..."
  [[ -n "$SERVER_PID" ]] && { kill -- -"$SERVER_PID" 2>/dev/null || kill "$SERVER_PID" 2>/dev/null || true; }
  [[ -n "$VITE_PID" ]]  && { kill -- -"$VITE_PID"  2>/dev/null || kill "$VITE_PID"  2>/dev/null || true; }
  docker compose -f docker-compose.yml -f docker-compose.dev.yml stop postgres minio
}
trap cleanup EXIT INT TERM
```

Replace with:

```bash
DB_URL="host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"
CRAWLER_TOKEN="dev-secret"
SERVER_PID=""
VITE_PID=""
CRAWLER_PID=""

cleanup() {
  echo ""
  echo "Shutting down..."
  [[ -n "$SERVER_PID" ]]  && { kill -- -"$SERVER_PID"  2>/dev/null || kill "$SERVER_PID"  2>/dev/null || true; }
  [[ -n "$VITE_PID" ]]    && { kill -- -"$VITE_PID"    2>/dev/null || kill "$VITE_PID"    2>/dev/null || true; }
  [[ -n "$CRAWLER_PID" ]] && { kill -- -"$CRAWLER_PID" 2>/dev/null || kill "$CRAWLER_PID" 2>/dev/null || true; }
  docker compose -f docker-compose.yml -f docker-compose.dev.yml stop postgres minio
}
trap cleanup EXIT INT TERM
```

- [ ] **Step 2: Launch the crawler as a background listener before the server, and point the server at it**

Find:

```bash
echo "==> Starting server on :8080..."
go run ./cmd/server/ \
  --db-url "$DB_URL" \
  --http-addr :8080 \
  --grpc-addr :50051 \
  --crawler-path ./crawler \
  --subfinder-path "$(go env GOPATH)/bin/subfinder" \
  --cookie-secure=false \
  --bootstrap-admin-username admin \
  --bootstrap-admin-password admin \
  --seed-dns dns-server.yaml > >(sed -u 's/^/[server] /') 2>&1 &
SERVER_PID=$!
```

Replace with:

```bash
echo "==> Starting crawler control service on :50052..."
./crawler --listen-addr :50052 --grpc-addr :50051 --auth-token "$CRAWLER_TOKEN" > >(sed -u 's/^/[crawler] /') 2>&1 &
CRAWLER_PID=$!

echo "==> Starting server on :8080..."
go run ./cmd/server/ \
  --db-url "$DB_URL" \
  --http-addr :8080 \
  --grpc-addr :50051 \
  --crawler-addr localhost:50052 \
  --crawler-token "$CRAWLER_TOKEN" \
  --subfinder-path "$(go env GOPATH)/bin/subfinder" \
  --cookie-secure=false \
  --bootstrap-admin-username admin \
  --bootstrap-admin-password admin \
  --seed-dns dns-server.yaml > >(sed -u 's/^/[server] /') 2>&1 &
SERVER_PID=$!
```

- [ ] **Step 3: Include the crawler in the final `wait`**

Find:

```bash
wait "$SERVER_PID" "$VITE_PID"
```

Replace with:

```bash
wait "$SERVER_PID" "$VITE_PID" "$CRAWLER_PID"
```

- [ ] **Step 4: Manually verify**

```bash
./dev.sh
```

Expected: logs show, in order, `[crawler] crawler control listening on :50052`, then `[server] gRPC listening on :50051` and `[server] HTTP listening on :8080`, then `[web] ...` Vite output. Press Ctrl+C — expected all three background processes (and postgres/minio) stop cleanly (verify with `docker compose ps` and `ps aux | grep -E 'crawler|server'` showing nothing lingering).

- [ ] **Step 5: Commit**

```bash
git add dev.sh
git commit -m "$(cat <<'EOF'
chore(dev.sh): launch crawler as a persistent control-service process

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Split `docker-compose.yml` into two services; update `Dockerfile`

**Files:**
- Modify: `docker-compose.yml`
- Modify: `Dockerfile`

- [ ] **Step 1: Remove the now-unused `CRAWLER_PATH` env var from the Dockerfile**

In `Dockerfile`, find:

```dockerfile
WORKDIR /app
ENV CRAWLER_PATH=/app/crawler
ENV SUBFINDER_PATH=/app/subfinder

EXPOSE 8080 50051
ENTRYPOINT ["/app/server", "--seed-dns", "/app/dns-server.yaml"]
```

Replace with:

```dockerfile
WORKDIR /app
ENV SUBFINDER_PATH=/app/subfinder

EXPOSE 8080 50051 50052
ENTRYPOINT ["/app/server", "--seed-dns", "/app/dns-server.yaml"]
```

`CRAWLER_PATH` is dropped because `cmd/server` no longer reads it (replaced by `CRAWLER_ADDR`/`CRAWLER_TOKEN`, set per-service in `docker-compose.yml` below). Port `50052` is added to the documentation-only `EXPOSE` list since this same image now also runs as the crawler's control-service listener in the `crawler` service.

- [ ] **Step 2: Add the `crawler` service and update `server`'s environment in `docker-compose.yml`**

Find:

```yaml
services:
  server:
    build: .
    ports:
      - "8080:8080"
      - "50051:50051"
    environment:
      DB_URL: "${DB_URL}"
      MINIO_ENDPOINT: "minio:9000"
      MINIO_ACCESS_KEY: "minioadmin"
      MINIO_SECRET_KEY: "minioadmin"
      MINIO_BUCKET: "screenshots"
      CRAWLER_PATH: "/app/crawler"
    depends_on:
      minio:
        condition: service_started
    restart: unless-stopped
```

Replace with:

```yaml
services:
  server:
    build: .
    ports:
      - "8080:8080"
      - "50051:50051"
    environment:
      DB_URL: "${DB_URL}"
      MINIO_ENDPOINT: "minio:9000"
      MINIO_ACCESS_KEY: "minioadmin"
      MINIO_SECRET_KEY: "minioadmin"
      MINIO_BUCKET: "screenshots"
      CRAWLER_ADDR: "crawler:50052"
      CRAWLER_TOKEN: "${CRAWLER_TOKEN}"
    depends_on:
      minio:
        condition: service_started
      crawler:
        condition: service_started
    restart: unless-stopped

  crawler:
    build: .
    entrypoint: ["/app/crawler"]
    command:
      - "--listen-addr=:50052"
      - "--grpc-addr=server:50051"
      - "--auth-token=${CRAWLER_TOKEN}"
    restart: unless-stopped
```

(Leave `minio`, `minio-init`, and the `volumes:` block at the bottom of the file unchanged.)

- [ ] **Step 3: Document the new required env var**

Check whether a `.env.example` or similar exists:

```bash
ls .env* 2>/dev/null
```

If one exists, add `CRAWLER_TOKEN=` to it alongside any existing `DB_URL`. If none exists, skip this step — the project doesn't use one today, and inventing that convention here would be scope creep beyond this task.

- [ ] **Step 4: Verify the compose file is well-formed**

```bash
docker compose -f docker-compose.yml config --quiet
```

Expected: no output (success) — this validates YAML syntax and variable interpolation without actually starting anything.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile docker-compose.yml
git commit -m "$(cat <<'EOF'
chore(docker): split crawler into its own service, talking to server over gRPC

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Update `CLAUDE.md`

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update the crawler flags block**

Find (inside the `## Commands` fenced block):

```
--compliant-ips 1.2.3.4,5.6.7.8  # IPs treated as compliant even when DNS resolves (e.g. ISP block-page IP); server passes this automatically from the admin-managed list, see "Domain semantics" below

# Server key flags (all accept env-var fallbacks)
```

Replace with:

```
--compliant-ips 1.2.3.4,5.6.7.8  # IPs treated as compliant even when DNS resolves (e.g. ISP block-page IP); server passes this automatically from the admin-managed list, see "Domain semantics" below
--listen-addr :50052       # run as a persistent gRPC control service instead of a one-shot sweep; the dashboard triggers sweeps via CrawlerControl.StartSweep instead of exec'ing this binary — see Architecture below
--auth-token ...           # shared secret required on incoming StartSweep RPCs when --listen-addr is set; must match the server's --crawler-token

# Server key flags (all accept env-var fallbacks)
```

- [ ] **Step 2: Replace the `--crawler-path` line**

Find:

```
--crawler-path ./crawler          # env: CRAWLER_PATH
```

Replace with:

```
--crawler-addr localhost:50052    # env: CRAWLER_ADDR — gRPC address of the crawler's control service (see --listen-addr above)
--crawler-token ...                # env: CRAWLER_TOKEN — shared secret sent with StartSweep RPCs; must match the crawler's --auth-token
```

- [ ] **Step 3: Update the Scanner description in Architecture**

Find:

```
- **Scanner** (`scanner.go`) — manages crawler subprocess lifecycle with a mutex-guarded `running` flag. `Trigger(ctx, reason, urls []string)` does a DNS-only crawl against the provided URL list, or against `store.ListWatchedURLs(ctx)` (enabled-only) when `urls` is `nil`. `TriggerScreenshot` adds `--screenshots` and targets a single URL + DNS server. Both write temp files for the URL list and DNS YAML then call `exec.CommandContext`. The crawler's stdout is wired to the server's stderr so its log output appears in server logs.
```

Replace with:

```
- **Scanner** (`scanner.go`) — triggers sweeps on the crawler's persistent control service over gRPC (`CrawlerControl.StartSweep`, auth-gated by a shared `--crawler-token`/`--auth-token`), with a mutex-guarded `running` flag. `Trigger(ctx, reason, urls []string)` does a DNS-only crawl against the provided URL list, or against `store.ListWatchedURLs(ctx)` (enabled-only) when `urls` is `nil`. `TriggerScreenshot` sets `Screenshots: true` on the request and targets a single URL + DNS server. `StartSweep` is a blocking call — it returns only once the crawler has finished the sweep — so `Scanner` waits on it the same way it used to wait on the old `exec.Command` subprocess exiting, and `CompleteScanRun`/broadcaster bookkeeping is unchanged. This is what lets the crawler and dashboard run on separate hosts — see `docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md`.
```

- [ ] **Step 4: Update the server env-var fallback list**

Find:

```
- Server flags accept env-var fallbacks: `DB_URL`, `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET`, `CRAWLER_PATH`, `COOKIE_SECURE`, `BOOTSTRAP_ADMIN_USERNAME`, `BOOTSTRAP_ADMIN_PASSWORD`.
```

Replace with:

```
- Server flags accept env-var fallbacks: `DB_URL`, `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET`, `CRAWLER_ADDR`, `CRAWLER_TOKEN`, `COOKIE_SECURE`, `BOOTSTRAP_ADMIN_USERNAME`, `BOOTSTRAP_ADMIN_PASSWORD`.
```

- [ ] **Step 5: Update the Docker section comment**

Find:

```
# The Dockerfile is multi-stage: builder (golang:1.26) produces both binaries
# plus a standalone `go install` of subfinder (not a go.mod dependency —
# only ever shelled out to, see internal/subfinder/); runtime
# (debian:bookworm-slim) includes Chromium for screenshot support and ships
# the subfinder binary at /app/subfinder (SUBFINDER_PATH).
# ENTRYPOINT is /app/server; /app/crawler is the default CRAWLER_PATH.
```

Replace with:

```
# The Dockerfile is multi-stage: builder (golang:1.26) produces both binaries
# plus a standalone `go install` of subfinder (not a go.mod dependency —
# only ever shelled out to, see internal/subfinder/); runtime
# (debian:bookworm-slim) includes Chromium for screenshot support and ships
# the subfinder binary at /app/subfinder (SUBFINDER_PATH).
# ENTRYPOINT is /app/server. docker-compose.yml runs a second container from
# the same image with an `entrypoint: ["/app/crawler"]` override, as the
# `crawler` service — the two talk over gRPC (CrawlerControl.StartSweep to
# trigger sweeps, ComplianceService.Submit to report results), not exec.
```

- [ ] **Step 6: Remove the now-completed TODO item**

Find:

```
- **Split crawler and dashboard onto separate hosts**: currently impossible without code changes. `Scanner.execCrawler` (`internal/server/scanner.go`) spawns the crawler as a **local subprocess** (`exec.Command`) and feeds it via **local temp files** (`writeTempLines`/`writeDNSYAML` write the site list and DNS-server YAML to disk, then pass the paths as `--sites`/`--dns-servers` CLI args) — this only works when both binaries run on the same host, since exec can't reach a binary on a different machine and the temp files aren't visible across hosts either. To actually split them:
  - Add a new RPC to `proto/*.proto` (e.g. `StartSweep(SweepRequest)`) carrying the URL list and DNS server list as message fields instead of file paths.
  - Turn `cmd/crawler/main.go` into a persistent gRPC server (new `--listen-addr`) instead of a one-shot CLI; on `StartSweep` it runs the same resolve/screenshot pipeline in-process.
  - Keep pushing results back to the dashboard via the existing `Submit` RPC, same as today.
  - Replace the `exec.Command` call in `Scanner.execCrawler` with a gRPC client call to the crawler's new endpoint.
  - Add auth (shared token/mTLS) on the new trigger RPC — it's a new network-reachable "tell me what to scan" surface that didn't exist when this was local-only exec.

  Until this lands, crawler and dashboard (`cmd/server`) must run on the same host; only Postgres/MinIO can be split off today (already network-configured via `--db-url`/`--minio-endpoint`).

```

Delete this whole item (including its trailing blank line) from the `## TODO` list — it's now implemented, per `docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md`.

- [ ] **Step 7: Commit**

```bash
git add CLAUDE.md
git commit -m "$(cat <<'EOF'
docs: update CLAUDE.md for the crawler/dashboard host split

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: End-to-end verification

**Files:** none (verification only)

- [ ] **Step 1: Full build and test suite**

```bash
go build -o server ./cmd/server/
go build -o crawler ./cmd/crawler/
go vet ./...
go test ./...
```

Expected: both binaries build; `go vet` reports nothing; `go test ./...` is `ok` for all packages (per `CLAUDE.md`, `internal/dns` needs network and `internal/screenshot` needs Chrome — same pre-existing caveats as before this change, not introduced by it).

- [ ] **Step 2: Run the real dev stack and trigger a scan**

```bash
./dev.sh
```

In a second terminal, once the logs show all three processes up:

```bash
curl -s -c /tmp/cookies.txt -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | head -c 200
echo
curl -s -b /tmp/cookies.txt -X POST http://localhost:8080/api/scan
sleep 2
curl -s -b /tmp/cookies.txt http://localhost:8080/api/scan/progress
```

Expected: login succeeds (sets the session cookie); `POST /api/scan` returns success; `GET /api/scan/progress` shows a `scan_run` with `status: "running"` and non-zero `total_urls`, and `[crawler]`-prefixed log lines in the `dev.sh` terminal show the sweep actually executing (DNS lookups, `[server]` lines show `Submit`-driven inserts). Wait for it to finish and re-check `/api/scan/progress` — `status` should flip to `"completed"`.

- [ ] **Step 3: Confirm the SSE stream carries the same live updates as before**

```bash
curl -N -s -b /tmp/cookies.txt http://localhost:8080/api/scan/progress/stream &
STREAM_PID=$!
curl -s -b /tmp/cookies.txt -X POST http://localhost:8080/api/scan
sleep 5
kill $STREAM_PID
```

Expected: the streamed output shows multiple `data: {...}` events with increasing `per_dns[].completed` counts, ending at `status: "completed"` — confirming the live-progress behavior traced through in the design spec's "Live Progress (SSE)" section still works with the crawler as a separate process.

- [ ] **Step 4: Clean up**

```bash
rm -f /tmp/cookies.txt
```

Press Ctrl+C in the `dev.sh` terminal; confirm (as in Task 7 Step 4) that the crawler, server, and Vite processes all exit and no orphans remain.

- [ ] **Step 5: Final check — confirm nothing references the old exec-based flags**

```bash
grep -rn "crawler-path\|CRAWLER_PATH\|writeTempLines\|writeDNSYAML" --include="*.go" --include="*.sh" --include="*.yml" --include="*.md" . 2>/dev/null | grep -v "docs/superpowers"
```

Expected: no matches (the design spec and this plan itself, under `docs/superpowers/`, are excluded and may still mention the old names historically — everything else should be clean).

No commit for this task — it's verification only, confirming the prior nine tasks' commits are correct together.
