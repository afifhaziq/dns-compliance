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
	"github.com/afif/dns-tracking/internal/input"
	"github.com/afif/dns-tracking/internal/pipeline"
	"github.com/afif/dns-tracking/internal/screenshot"
	"github.com/afif/dns-tracking/internal/sender"
	pb "github.com/afif/dns-tracking/proto"
	"github.com/chromedp/chromedp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
	flag.Parse()

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
		for _, s := range cfg.Servers {
			var resolveFn func(context.Context, string) (string, error)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var conn *grpc.ClientConn
	if *grpcAddr != "" {
		conn, err = grpc.NewClient(*grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("connecting to gRPC server: %v", err)
		}
		defer conn.Close()
	}

	baseCfg := pipeline.Config{
		DNSWorkers:        *dnsWorkers,
		ScreenshotWorkers: *ssWorkers,
		DNSTimeout:        time.Duration(*dnsTimeoutSec) * time.Second,
		ScreenshotTimeout: time.Duration(*ssTimeoutSec) * time.Second,
	}
	waitIdle := time.Duration(*waitIdleSec) * time.Second
	postIdleSleep := time.Duration(*postIdleSleepMs) * time.Millisecond

	if *intervalM == 0 {
		runSweep(ctx, urls, servers, baseCfg, waitIdle, postIdleSleep, conn)
		return
	}

	ticker := time.NewTicker(time.Duration(*intervalM) * time.Minute)
	defer ticker.Stop()

	runSweep(ctx, urls, servers, baseCfg, waitIdle, postIdleSleep, conn)
	for {
		select {
		case <-ticker.C:
			runSweep(ctx, urls, servers, baseCfg, waitIdle, postIdleSleep, conn)
		case <-ctx.Done():
			log.Println("shutting down")
			return
		}
	}
}

type serverEntry struct {
	name    string
	resolve func(context.Context, string) (string, error)
}

func runSweep(
	ctx context.Context,
	urls []string,
	servers []serverEntry,
	baseCfg pipeline.Config,
	waitIdle time.Duration,
	postIdleSleep time.Duration,
	conn *grpc.ClientConn,
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

	// Phase 2: Screenshot each unique (URL, IP) pair using its resolved IP.
	screenshots := captureResolved(ctx, allResults, baseCfg.ScreenshotWorkers, baseCfg.ScreenshotTimeout, waitIdle, postIdleSleep)

	// Attach screenshots to the first matching result per URL; mark others shared.
	assignScreenshots(allResults, screenshots)

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

	if conn != nil {
		report := buildReport(allResults)
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := sender.Send(sendCtx, conn, report); err != nil {
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
// Returns a map keyed by shotKey(url, ip).
func captureResolved(
	ctx context.Context,
	results []pipeline.SiteResult,
	ssWorkers int,
	ssTimeout time.Duration,
	waitIdle time.Duration,
	postIdleSleep time.Duration,
) map[string][]byte {
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
		return nil
	}

	shots := make(map[string][]byte, len(jobs))
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

				buf, err := screenshot.CaptureWithAllocator(siteCtx, groupAllocCtx, j.url, waitIdle, postIdleSleep)
				if err != nil {
					log.Printf("screenshot failed for %s: %v", j.url, err)
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
	return shots
}

// assignScreenshots copies screenshot bytes into the first SiteResult for each
// (URL, IP) pair; subsequent results for the same pair keep nil bytes and
// display "(shared)" in the table.
func assignScreenshots(results []pipeline.SiteResult, shots map[string][]byte) {
	assigned := make(map[string]bool)
	for i, r := range results {
		key := shotKey(r.URL, r.ResolvedIP)
		buf, ok := shots[key]
		if !ok {
			continue
		}
		if !assigned[key] {
			results[i].Screenshot = buf
			assigned[key] = true
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

func buildReport(results []pipeline.SiteResult) *pb.ComplianceReport {
	pbResults := make([]*pb.SiteResult, len(results))
	for i, r := range results {
		pbResults[i] = &pb.SiteResult{
			Url:        r.URL,
			Timestamp:  r.Timestamp.Unix(),
			Compliant:  r.Compliant,
			ResolvedIp: r.ResolvedIP,
			Screenshot: r.Screenshot,
			Error:      r.Error,
			DnsServer:  r.DNSServer,
		}
	}
	return &pb.ComplianceReport{Results: pbResults}
}
