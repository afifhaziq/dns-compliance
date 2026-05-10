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
			servers = append(servers, serverEntry{
				name:    s.Name,
				resolve: dns.NewResolver(s.Address),
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

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, screenshot.AllocatorOptions...)
	defer allocCancel()

	captureFn := func(captureCtx context.Context, rawURL string) ([]byte, error) {
		return screenshot.CaptureWithAllocator(captureCtx, allocCtx, rawURL,
			time.Duration(*waitIdleSec)*time.Second,
			time.Duration(*postIdleSleepMs)*time.Millisecond,
		)
	}

	baseCfg := pipeline.Config{
		DNSWorkers:        *dnsWorkers,
		ScreenshotWorkers: *ssWorkers,
		DNSTimeout:        time.Duration(*dnsTimeoutSec) * time.Second,
		ScreenshotTimeout: time.Duration(*ssTimeoutSec) * time.Second,
	}

	if *intervalM == 0 {
		runSweep(ctx, urls, servers, baseCfg, captureFn, conn)
		return
	}

	ticker := time.NewTicker(time.Duration(*intervalM) * time.Minute)
	defer ticker.Stop()

	runSweep(ctx, urls, servers, baseCfg, captureFn, conn)
	for {
		select {
		case <-ticker.C:
			runSweep(ctx, urls, servers, baseCfg, captureFn, conn)
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
	captureFn func(context.Context, string) ([]byte, error),
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

	// Phase 2: Screenshot each URL that resolved on at least one server (once).
	screenshots := captureResolved(ctx, allResults, baseCfg, captureFn)

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

// captureResolved screenshots each URL that resolved on at least one server.
// Returns a map of URL → PNG bytes.
func captureResolved(
	ctx context.Context,
	results []pipeline.SiteResult,
	cfg pipeline.Config,
	captureFn func(context.Context, string) ([]byte, error),
) map[string][]byte {
	// Collect unique resolved URLs preserving order.
	seen := make(map[string]struct{})
	var resolvedURLs []string
	for _, r := range results {
		if r.DNSResolved {
			if _, ok := seen[r.URL]; !ok {
				seen[r.URL] = struct{}{}
				resolvedURLs = append(resolvedURLs, r.URL)
			}
		}
	}
	if len(resolvedURLs) == 0 {
		return nil
	}

	shots := make(map[string][]byte, len(resolvedURLs))
	var mu sync.Mutex
	sem := make(chan struct{}, cfg.ScreenshotWorkers)
	var wg sync.WaitGroup

	for _, rawURL := range resolvedURLs {
		rawURL := rawURL
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			siteCtx, cancel := context.WithTimeout(ctx, cfg.ScreenshotTimeout)
			defer cancel()

			buf, err := captureFn(siteCtx, rawURL)
			if err != nil {
				log.Printf("screenshot failed for %s: %v", rawURL, err)
				return
			}
			mu.Lock()
			shots[rawURL] = buf
			mu.Unlock()
		}()
	}
	wg.Wait()
	return shots
}

// assignScreenshots copies screenshot bytes into the first SiteResult for each
// URL that has a screenshot; subsequent results for the same URL keep nil bytes
// (they share the saved file shown in the table).
func assignScreenshots(results []pipeline.SiteResult, shots map[string][]byte) {
	assigned := make(map[string]bool)
	for i, r := range results {
		buf, ok := shots[r.URL]
		if !ok {
			continue
		}
		if !assigned[r.URL] {
			results[i].Screenshot = buf
			assigned[r.URL] = true
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
		if path, ok := paths[r.URL]; ok {
			if !sharedPrinted[r.URL] {
				screenshotCol = path
				sharedPrinted[r.URL] = true
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

			siteDir := filepath.Join("screenshots", hostnameFromURL(r.URL))
			if err := os.MkdirAll(siteDir, 0755); err != nil {
				log.Printf("creating screenshot dir: %v", err)
				return
			}
			path := filepath.Join(siteDir, timestamp+"-"+urlHash(r.URL)+".png")
			if err := os.WriteFile(path, r.Screenshot, 0644); err != nil {
				log.Printf("saving screenshot for %s: %v", r.URL, err)
				return
			}
			mu.Lock()
			paths[r.URL] = path
			mu.Unlock()
		}()
	}
	wg.Wait()
	return paths
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
