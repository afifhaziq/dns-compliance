package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/grpcauth"
	"github.com/afif/dns-tracking/internal/urlnorm"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
)

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
	sc.mu.Unlock()

	go sc.runScreenshot(context.WithoutCancel(ctx), rawURL, dnsServerIDs)
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

	authedCtx := grpcauth.AppendToken(ctx, sc.crawlerToken)
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
