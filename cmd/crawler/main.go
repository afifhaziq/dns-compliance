package main

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/afif/dns-tracking/internal/dns"
	"github.com/afif/dns-tracking/internal/dnsconfig"
	"github.com/afif/dns-tracking/internal/grpcauth"
	"github.com/afif/dns-tracking/internal/input"
	"github.com/afif/dns-tracking/internal/pipeline"
	"github.com/afif/dns-tracking/internal/screenshot"
	"github.com/afif/dns-tracking/internal/sender"
	pb "github.com/afif/dns-tracking/proto"
	"github.com/chromedp/chromedp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// grpcTransport carries the resolved gRPC security settings shared by both
// crawler modes, so runListenMode doesn't grow three more positional
// parameters on top of the nine it already has.
type grpcTransport struct {
	dialOpt grpc.DialOption
	creds   credentials.TransportCredentials // nil when TLS is off
	token   string
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	sitesFile    := flag.String("sites", "", "path to file with one URL per line")
	dnsServers   := flag.String("dns-servers", "", "path to YAML file listing DNS servers to query")
	dnsWorkers   := flag.Int("dns-workers", 20, "number of DNS worker goroutines")
	ssWorkers    := flag.Int("screenshot-workers", 5, "number of screenshot worker goroutines")
	intervalM    := flag.Int("interval", 0, "sweep interval in minutes; 0 = run once and exit")
	grpcAddr     := flag.String("grpc-addr", "", "gRPC server address (e.g. localhost:50051); empty prints to stdout")
	dnsTimeoutSec  := flag.Int("dns-timeout", 5, "time budget in seconds for DNS resolution per site")
	ssTimeoutSec   := flag.Int("screenshot-timeout", 30, "time budget in seconds for screenshot per site (navigation + idle wait + capture)")
	waitIdleSec    := flag.Int("wait-idle", 5, "max seconds to wait for network idle after page load before screenshotting anyway")
	postIdleSleepMs := flag.Int("post-idle-sleep", 2000, "milliseconds to sleep after network idle before taking the screenshot")
	takeScreenshots  := flag.Bool("screenshots", false, "capture screenshots for resolved sites (default: DNS-only)")
	compliantIPsFlag := flag.String("compliant-ips", "", "comma-separated IPs treated as compliant even when DNS resolves (e.g. MCMC block-page IP)")
	listenAddr := flag.String("listen-addr", "", "gRPC listen address for persistent trigger mode (e.g. :50052); when set, runs as a long-lived server instead of a one-shot sweep")
	authToken := flag.String("auth-token", envOr("CRAWLER_TOKEN", ""), "shared secret for both gRPC directions: required on incoming StartSweep RPCs, and sent with outgoing Submit RPCs")
	tlsCert := flag.String("tls-cert", envOr("TLS_CERT", ""), "PEM path to this binary's leaf certificate; enables mTLS when set together with --tls-key and --tls-ca")
	tlsKey := flag.String("tls-key", envOr("TLS_KEY", ""), "PEM path to the private key for --tls-cert")
	tlsCA := flag.String("tls-ca", envOr("TLS_CA", ""), "PEM path to the CA that signed both binaries' certificates")
	flag.Parse()

	creds, tlsOn, err := grpcauth.Creds(*tlsCert, *tlsKey, *tlsCA)
	if err != nil {
		log.Fatalf("TLS config: %v", err)
	}
	transport := grpcTransport{
		dialOpt: grpc.WithTransportCredentials(insecure.NewCredentials()),
		token:   *authToken,
	}
	if tlsOn {
		transport.dialOpt = grpc.WithTransportCredentials(creds)
		transport.creds = creds
	} else {
		log.Print("WARNING: gRPC links are unencrypted — set --tls-cert, --tls-key and --tls-ca to enable mTLS")
	}

	if *listenAddr != "" {
		runListenMode(*listenAddr, transport, *grpcAddr, *dnsWorkers, *ssWorkers,
			time.Duration(*dnsTimeoutSec)*time.Second, time.Duration(*ssTimeoutSec)*time.Second,
			time.Duration(*waitIdleSec)*time.Second, time.Duration(*postIdleSleepMs)*time.Millisecond)
		return
	}

	urls, err := input.Load(*sitesFile, flag.Args())
	if err != nil {
		log.Fatalf("loading URLs: %v", err)
	}
	if len(urls) == 0 {
		log.Fatal("no URLs provided — use --sites or pass URLs as arguments")
	}

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var conn *grpc.ClientConn
	if *grpcAddr != "" {
		conn, err = grpc.NewClient(*grpcAddr, transport.dialOpt)
		if err != nil {
			log.Fatalf("connecting to gRPC server: %v", err)
		}
		defer conn.Close()
	}

	var compliantIPs []string
	if *compliantIPsFlag != "" {
		for _, ip := range strings.Split(*compliantIPsFlag, ",") {
			if t := strings.TrimSpace(ip); t != "" {
				compliantIPs = append(compliantIPs, t)
			}
		}
	}

	baseCfg := pipeline.Config{
		DNSWorkers:        *dnsWorkers,
		ScreenshotWorkers: *ssWorkers,
		DNSTimeout:        time.Duration(*dnsTimeoutSec) * time.Second,
		ScreenshotTimeout: time.Duration(*ssTimeoutSec) * time.Second,
		CompliantIPs:      compliantIPs,
	}
	waitIdle := time.Duration(*waitIdleSec) * time.Second
	postIdleSleep := time.Duration(*postIdleSleepMs) * time.Millisecond

	if *intervalM == 0 {
		runSweep(ctx, urls, servers, baseCfg, waitIdle, postIdleSleep, conn, transport.token, *takeScreenshots)
		return
	}

	ticker := time.NewTicker(time.Duration(*intervalM) * time.Minute)
	defer ticker.Stop()

	runSweep(ctx, urls, servers, baseCfg, waitIdle, postIdleSleep, conn, transport.token, *takeScreenshots)
	for {
		select {
		case <-ticker.C:
			runSweep(ctx, urls, servers, baseCfg, waitIdle, postIdleSleep, conn, transport.token, *takeScreenshots)
		case <-ctx.Done():
			log.Println("shutting down")
			return
		}
	}
}

type serverEntry struct {
	name    string
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
		entries[i] = serverEntry{name: s.Name, resolve: resolveFn}
	}
	return entries
}

func runSweep(
	ctx context.Context,
	urls []string,
	servers []serverEntry,
	baseCfg pipeline.Config,
	waitIdle time.Duration,
	postIdleSleep time.Duration,
	conn *grpc.ClientConn,
	token string,
	takeScreenshots bool,
) {
	start := time.Now()
	total := len(urls) * len(servers)
	log.Printf("Starting sweep — %d sites × %d DNS server(s) = %d checks", len(urls), len(servers), total)

	// Phase 1: DNS-only pass for each server (no-op Capture).
	var allResults []pipeline.SiteResult
	completed := 0
	noop := func(_ context.Context, _ string) ([]byte, error) { return nil, nil }

	for _, srv := range servers {
		cfg := baseCfg
		cfg.Resolve = srv.resolve
		cfg.Capture = noop
		cfg.OnResult = func(r pipeline.SiteResult) {
			completed++
			status := "compliant"
			if !r.Compliant {
				status = "non-compliant"
			}
			serverLabel := srv.name
			if serverLabel == "" {
				serverLabel = "system"
			}
			detail := " dns=" + serverLabel
			if r.ResolvedIP != "" {
				detail += " ip=" + r.ResolvedIP
			}
			if r.Error != "" {
				detail += " err=" + r.Error
			}
			log.Printf("[%d/%d] %s — %s%s", completed, total, r.URL, status, detail)

			// Stream each DNS-only result as it completes so the server's scan
			// progress (GET /api/scan/progress) advances live instead of
			// jumping from 0 to total once the whole sweep finishes. Skipped
			// when screenshots are enabled: that path attaches screenshot
			// bytes to the same result after this loop, so it stays a single
			// batched send below to avoid inserting the DNS-only row twice.
			if conn != nil && !takeScreenshots {
				r.DNSServer = serverLabel
				sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				if err := sender.Send(sendCtx, conn, token, buildReport([]pipeline.SiteResult{r})); err != nil {
					log.Printf("gRPC stream send failed for %s (%s): %v", r.URL, serverLabel, err)
				}
				cancel()
			}
		}

		results, err := pipeline.Run(ctx, urls, cfg)
		if err != nil {
			log.Printf("sweep error (server %s): %v", srv.name, err)
			continue
		}
		for i := range results {
			results[i].DNSServer = srv.name
		}
		allResults = append(allResults, results...)
	}

	// Phase 2: Screenshot each unique (URL, IP) pair (only when --screenshots is set).
	var screenshots map[string][]byte
	var screenshotErrs map[string]string
	if takeScreenshots {
		screenshots, screenshotErrs = captureResolved(ctx, allResults, baseCfg.ScreenshotWorkers, baseCfg.ScreenshotTimeout, waitIdle, postIdleSleep)
	}

	// Attach screenshots to the first matching result per URL; mark others shared.
	assignScreenshots(allResults, screenshots, screenshotErrs)

	compliant, nonCompliant := 0, 0
	for _, r := range allResults {
		if r.Compliant {
			compliant++
		} else {
			nonCompliant++
		}
	}
	log.Printf("Sweep complete in %s — %d compliant, %d non-compliant",
		time.Since(start).Round(time.Second), compliant, nonCompliant)

	paths := saveScreenshots(allResults, start)

	// DNS-only results were already streamed to the server one-by-one above;
	// only screenshot-bearing results still need this final batched send.
	if conn != nil && takeScreenshots {
		report := buildReport(allResults)
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := sender.Send(sendCtx, conn, token, report); err != nil {
			log.Printf("gRPC send failed: %v", err)
		} else {
			log.Printf("Report sent to %s", conn.Target())
		}
	}
	printTable(allResults, paths)
}

// shotKey returns the map key for a (url, resolvedIP) screenshot pair.
func shotKey(url, ip string) string { return url + "|" + ip }

// screenshotJob is a (url, resolvedIP) pair that needs a screenshot.
type screenshotJob struct {
	url string
	ip  string
}

// groupJobs partitions jobs into batches where no two jobs in the same batch
// share a hostname but map to different IPs (which would conflict inside a
// single --host-resolver-rules string). In the common case — all DNS servers
// agree on the same IP — everything lands in one group.
func groupJobs(jobs []screenshotJob) [][]screenshotJob {
	type groupState struct {
		mappings map[string]string // hostname → ip
		jobs     []screenshotJob
	}
	var groups []groupState
	for _, job := range jobs {
		hostname := hostnameFromURL(job.url)
		placed := false
		for i := range groups {
			if existing, ok := groups[i].mappings[hostname]; !ok || existing == job.ip {
				groups[i].mappings[hostname] = job.ip
				groups[i].jobs = append(groups[i].jobs, job)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, groupState{
				mappings: map[string]string{hostname: job.ip},
				jobs:     []screenshotJob{job},
			})
		}
	}
	result := make([][]screenshotJob, len(groups))
	for i, g := range groups {
		result[i] = g.jobs
	}
	return result
}

// captureResolved screenshots each unique (URL, resolvedIP) pair, forcing
// Chrome to connect to the pre-resolved IP via --host-resolver-rules so the
// screenshot reflects what that DNS server's users actually see.
// Returns the screenshot bytes and any capture errors, both keyed by
// shotKey(url, ip).
func captureResolved(
	ctx context.Context,
	results []pipeline.SiteResult,
	ssWorkers int,
	ssTimeout time.Duration,
	waitIdle time.Duration,
	postIdleSleep time.Duration,
) (map[string][]byte, map[string]string) {
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
		return nil, nil
	}

	shots := make(map[string][]byte, len(jobs))
	errs := make(map[string]string, len(jobs))
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

				buf, err := captureWithSchemeFallback(siteCtx, groupAllocCtx, j.url, waitIdle, postIdleSleep)
				if err != nil {
					log.Printf("screenshot failed for %s: %v", j.url, err)
					mu.Lock()
					errs[shotKey(j.url, j.ip)] = err.Error()
					mu.Unlock()
					return
				}
				mu.Lock()
				shots[shotKey(j.url, j.ip)] = buf
				mu.Unlock()
			}()
		}
		wg.Wait()
		groupAllocCancel()
	}
	return shots, errs
}

// assignScreenshots copies screenshot bytes into every SiteResult sharing a
// (URL, IP) pair — one Chrome capture per pair, but each DNS server still
// gets its own copy so the server uploads (and shows evidence for) every
// row independently, rather than only the first DNS server to resolve that
// pair. Capture errors are likewise copied onto every SiteResult sharing
// that pair, since each one otherwise shows an unexplained missing
// screenshot.
func assignScreenshots(results []pipeline.SiteResult, shots map[string][]byte, errs map[string]string) {
	for i, r := range results {
		key := shotKey(r.URL, r.ResolvedIP)
		if errMsg, ok := errs[key]; ok {
			results[i].Error = errMsg
		}
		if buf, ok := shots[key]; ok {
			results[i].Screenshot = buf
		}
	}
}

func printTable(results []pipeline.SiteResult, paths map[string]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "URL\tDNS_SERVER\tCOMPLIANT\tRESOLVED_IP\tSCREENSHOT\tERROR")
	sharedPrinted := make(map[string]bool)
	for _, r := range results {
		serverCol := r.DNSServer
		if serverCol == "" {
			serverCol = "system"
		}
		screenshotCol := "no"
		key := shotKey(r.URL, r.ResolvedIP)
		if path, ok := paths[key]; ok {
			if !sharedPrinted[key] {
				screenshotCol = path
				sharedPrinted[key] = true
			} else {
				screenshotCol = "(shared)"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%v\t%s\t%s\t%s\n",
			r.URL, serverCol, r.Compliant, r.ResolvedIP, screenshotCol, r.Error)
	}
	w.Flush()
}

func saveScreenshots(results []pipeline.SiteResult, sweepTime time.Time) map[string]string {
	const maxParallel = 16
	timestamp := sweepTime.Format("2006-01-02T15-04-05")
	paths := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallel)

	for _, r := range results {
		if len(r.Screenshot) == 0 {
			continue
		}
		r := r
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			siteDir := filepath.Join(dnsLabel(r.DNSServer), hostnameFromURL(r.URL))
			if err := os.MkdirAll(siteDir, 0755); err != nil {
				log.Printf("creating screenshot dir: %v", err)
				return
			}
			// Include IP in hash so the same URL at different IPs gets different filenames.
			path := filepath.Join(siteDir, timestamp+"-"+urlHash(r.URL+"|"+r.ResolvedIP)+".png")
			if err := os.WriteFile(path, r.Screenshot, 0644); err != nil {
				log.Printf("saving screenshot for %s: %v", r.URL, err)
				return
			}
			mu.Lock()
			paths[shotKey(r.URL, r.ResolvedIP)] = path
			mu.Unlock()
		}()
	}
	wg.Wait()
	return paths
}

// dnsLabel returns a filesystem-safe folder name for the DNS server.
// Spaces are replaced with underscores; empty means system resolver.
func dnsLabel(name string) string {
	if name == "" {
		return "system"
	}
	return strings.ReplaceAll(name, " ", "_")
}

func urlHash(rawURL string) string {
	h := fnv.New32a()
	h.Write([]byte(rawURL))
	return fmt.Sprintf("%08x", h.Sum32())
}

func hostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return rawURL
}

// captureWithSchemeFallback prefixes a bare hostname (watchlist URLs are
// stored scheme-less, see internal/urlnorm) with https:// so Chrome's
// navigate call accepts it as an absolute URL, then falls back to plain
// http:// if that connection is refused — some blocked/parked sites (e.g.
// domain-parking pages) only ever serve on port 80. URLs that already carry
// an explicit scheme are tried as-is, with no fallback.
func captureWithSchemeFallback(ctx, allocCtx context.Context, rawURL string, waitIdle, postIdleSleep time.Duration) ([]byte, error) {
	if strings.Contains(rawURL, "://") {
		return screenshot.CaptureWithAllocator(ctx, allocCtx, rawURL, waitIdle, postIdleSleep)
	}
	buf, err := screenshot.CaptureWithAllocator(ctx, allocCtx, "https://"+rawURL, waitIdle, postIdleSleep)
	if err == nil {
		return buf, nil
	}
	return screenshot.CaptureWithAllocator(ctx, allocCtx, "http://"+rawURL, waitIdle, postIdleSleep)
}

func buildReport(results []pipeline.SiteResult) *pb.ComplianceReport {
	pbResults := make([]*pb.SiteResult, len(results))
	for i, r := range results {
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
	}
	return &pb.ComplianceReport{Results: pbResults}
}
