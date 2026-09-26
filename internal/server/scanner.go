package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/grpcauth"
	"github.com/afif/dns-tracking/internal/urlnorm"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
)

// ErrNoWatchedURLs is returned by Trigger for a full sweep (nil/empty urls)
// when the watchlist has nothing enabled to scan — surfaced to API callers
// instead of silently accepting the request and having the background run
// log-and-return with nothing to show for it (see run's own belt-and-suspenders
// re-check below, for the rare TOCTOU window between this check and the goroutine
// actually starting).
var ErrNoWatchedURLs = errors.New("no enabled watchlist domains to scan")

// ErrNoEnabledDNSServers is returned by Trigger when there are no enabled DNS
// servers to sweep against, for the same reason as ErrNoWatchedURLs above.
var ErrNoEnabledDNSServers = errors.New("no enabled DNS servers to scan against")

// crawlerClient is the subset of pb.CrawlerControlClient the Scanner needs,
// narrowed to a small interface so tests can inject a fake instead of
// standing up a real gRPC server — see
// docs/superpowers/specs/2026-07-22-split-crawler-dashboard-hosts-design.md.
type crawlerClient interface {
	StartSweep(ctx context.Context, req *pb.SweepRequest, opts ...grpc.CallOption) (*pb.SweepAck, error)
}

type Scanner struct {
	crawler          crawlerClient
	crawlerToken     string
	store            db.Store
	broadcaster      *Broadcaster
	mu               sync.Mutex
	running          bool
	cancelRun        context.CancelFunc
	scheduleReset    chan struct{}
	slaScheduleReset chan struct{}
}

func NewScanner(crawler crawlerClient, crawlerToken string, store db.Store, broadcaster *Broadcaster) *Scanner {
	return &Scanner{
		crawler:          crawler,
		crawlerToken:     crawlerToken,
		store:            store,
		broadcaster:      broadcaster,
		scheduleReset:    make(chan struct{}, 1),
		slaScheduleReset: make(chan struct{}, 1),
	}
}

// NotifyScheduleChanged tells StartScheduler's and StartSLAScheduler's loops
// to abandon whatever is left of their current wait and restart it
// immediately using the freshly-saved settings — so an admin save actually
// starts both cadences from that moment, instead of finishing out however
// much of the stale interval happened to be left. Non-blocking (a pending
// signal is enough; no need to queue more) and nil-safe, since some tests
// construct routes with a nil *Scanner.
func (sc *Scanner) NotifyScheduleChanged() {
	if sc == nil {
		return
	}
	select {
	case sc.scheduleReset <- struct{}{}:
	default:
	}
	select {
	case sc.slaScheduleReset <- struct{}{}:
	default:
	}
}

func (sc *Scanner) IsRunning() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.running
}

// Cancel stops the in-progress scan, if any, by cancelling the context
// StartSweep was called with. That context is also what pipeline.Run's
// per-item timeouts (checkDNS/takeScreenshot) derive from on the crawler
// side, so in-flight work fails fast and the sweep winds down instead of
// running to completion. Returns an error if no scan is running.
func (sc *Scanner) Cancel() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if !sc.running || sc.cancelRun == nil {
		return errors.New("no scan in progress")
	}
	sc.cancelRun()
	return nil
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
	sc.mu.Unlock()

	// Pre-checks so a request with nothing to scan gets a clear synchronous
	// error instead of a 202 followed by run() silently logging and
	// returning. Every caller (manual trigger, "Scan Selected", both
	// schedulers) routes through here, so this is the one place to catch it.
	if len(urls) == 0 {
		watched, err := sc.store.ListWatchedURLs(ctx)
		if err != nil {
			return fmt.Errorf("load watchlist: %w", err)
		}
		if len(watched) == 0 {
			return ErrNoWatchedURLs
		}
	}
	servers, err := sc.store.ListEnabledDNSServers(ctx)
	if err != nil {
		return fmt.Errorf("load dns servers: %w", err)
	}
	if len(servers) == 0 {
		return ErrNoEnabledDNSServers
	}

	sc.mu.Lock()
	if sc.running {
		sc.mu.Unlock()
		return errors.New("scan already in progress")
	}
	sc.running = true
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	sc.cancelRun = cancel
	sc.mu.Unlock()

	go sc.run(runCtx, triggeredBy, urls)
	return nil
}

// TriggerScreenshot spawns a single-URL scan with screenshots enabled,
// targeting the given DNS servers. The crawler resolves each server then
// captures all resulting (url, IP) pairs concurrently through its own
// screenshot worker pool — see captureResolved in cmd/crawler/main.go — so
// one call with N servers is not N times slower than one with a single
// server.
func (sc *Scanner) TriggerScreenshot(ctx context.Context, rawURL string, dnsServerIDs []uint) error {
	sc.mu.Lock()
	if sc.running {
		sc.mu.Unlock()
		return errors.New("scan already in progress")
	}
	sc.running = true
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	sc.cancelRun = cancel
	sc.mu.Unlock()

	go sc.runScreenshot(runCtx, rawURL, dnsServerIDs)
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

	servers, err := sc.store.ListEnabledDNSServers(ctx)
	if err != nil {
		log.Printf("scanner: load DNS servers: %v", err)
		return
	}
	if len(servers) == 0 {
		log.Printf("scanner: no enabled DNS servers to scan against")
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
		DnsWorkers:   sc.dnsWorkers(ctx),
	}
	sc.runCrawler(ctx, req, run.ID)
}

func (sc *Scanner) runScreenshot(ctx context.Context, rawURL string, dnsServerIDs []uint) {
	defer sc.setRunning(false)

	servers, err := sc.store.ListDNSServers(ctx)
	if err != nil {
		log.Printf("scanner: load DNS servers: %v", err)
		return
	}

	wanted := make(map[uint]bool, len(dnsServerIDs))
	for _, id := range dnsServerIDs {
		wanted[id] = true
	}
	var target []db.DNSServer
	for _, s := range servers {
		if wanted[s.ID] {
			target = append(target, s)
		}
	}
	if len(target) == 0 {
		log.Printf("scanner: no matching DNS servers for IDs %v", dnsServerIDs)
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
		DnsWorkers:   sc.dnsWorkers(ctx),
	}
	sc.runCrawler(ctx, req, run.ID)
}

// dnsWorkers fetches the admin-configured DNS worker count for
// SweepRequest.DnsWorkers. Returns 0 (non-fatal) on a read failure, which
// tells the crawler to fall back to its own --dns-workers CLI default.
func (sc *Scanner) dnsWorkers(ctx context.Context) int32 {
	workers, err := sc.store.GetDNSWorkers(ctx)
	if err != nil {
		log.Printf("scanner: load dns workers setting: %v", err)
		return 0
	}
	if workers > math.MaxInt32 {
		// SetDNSWorkers already rejects values this large; this is a
		// belt-and-suspenders guard against int32 overflow, not a case
		// that should be reachable.
		return math.MaxInt32
	}
	return int32(workers) // #nosec G115 -- bounds-checked above; gosec can't see across the guard clause
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
	// Bookkeeping below must survive Cancel() cancelling ctx — otherwise a
	// cancelled scan's CompleteScanRun/broadcaster publish would themselves
	// get cancelled and the run would stay stuck showing "running" forever.
	bgCtx := context.WithoutCancel(ctx)

	// Publish the fresh (0-completed) run immediately, before the crawler
	// produces any results — otherwise SSE subscribers keep showing the
	// previous run's final tally until the first result streams in, which
	// reads as the progress bar starting full and then resetting.
	if sc.broadcaster != nil {
		if data, err := buildProgressPayload(bgCtx, sc.store); err == nil && data != nil {
			sc.broadcaster.Publish(data)
		}
	}

	authedCtx := grpcauth.AppendToken(ctx, sc.crawlerToken)
	status := "completed"
	ack, err := sc.crawler.StartSweep(authedCtx, req)
	if err != nil {
		if ctx.Err() != nil {
			status = "cancelled"
		} else {
			status = "failed"
		}
		log.Printf("scanner: crawler StartSweep failed: %v", err)
	} else if !ack.Accepted {
		log.Printf("scanner: crawler rejected sweep: %s", ack.Error)
		status = "failed"
	}
	now := time.Now()
	_ = sc.store.CompleteScanRun(bgCtx, runID, status, now)

	// StartSweep only returns once the crawler has finished streaming
	// results via Submit; nothing else announces the run flipping to
	// completed/failed, so SSE subscribers would be stuck on the last
	// "running" payload forever without this.
	if sc.broadcaster != nil {
		if data, err := buildProgressPayload(bgCtx, sc.store); err == nil && data != nil {
			sc.broadcaster.Publish(data)
		}
	}
}

func (sc *Scanner) setRunning(v bool) {
	sc.mu.Lock()
	sc.running = v
	if !v {
		sc.cancelRun = nil
	}
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
