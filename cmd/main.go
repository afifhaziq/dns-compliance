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
	sitesFile := flag.String("sites", "", "path to file with one URL per line")
	dnsWorkers := flag.Int("dns-workers", 20, "number of DNS worker goroutines")
	ssWorkers := flag.Int("screenshot-workers", 5, "number of screenshot worker goroutines")
	intervalM := flag.Int("interval", 0, "sweep interval in minutes; 0 = run once and exit")
	grpcAddr := flag.String("grpc-addr", "", "gRPC server address (e.g. localhost:50051); empty prints to stdout")
	dnsTimeoutSec := flag.Int("dns-timeout", 5, "time budget in seconds for DNS resolution per site")
	ssTimeoutSec  := flag.Int("screenshot-timeout", 30, "time budget in seconds for screenshot per site (navigation + idle wait + capture)")
	waitIdleSec     := flag.Int("wait-idle", 5, "max seconds to wait for network idle after page load before screenshotting anyway")
	postIdleSleepMs := flag.Int("post-idle-sleep", 2000, "milliseconds to sleep after network idle before taking the screenshot (allows lazy-loaded content to render)")
	flag.Parse()

	urls, err := input.Load(*sitesFile, flag.Args())
	if err != nil {
		log.Fatalf("loading URLs: %v", err)
	}
	if len(urls) == 0 {
		log.Fatal("no URLs provided — use --sites or pass URLs as arguments")
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

	cfg := pipeline.Config{
		DNSWorkers:        *dnsWorkers,
		ScreenshotWorkers: *ssWorkers,
		DNSTimeout:        time.Duration(*dnsTimeoutSec) * time.Second,
		ScreenshotTimeout: time.Duration(*ssTimeoutSec) * time.Second,
		Resolve:           dns.Resolve,
		Capture: func(captureCtx context.Context, rawURL string) ([]byte, error) {
			return screenshot.CaptureWithAllocator(captureCtx, allocCtx, rawURL,
				time.Duration(*waitIdleSec)*time.Second,
				time.Duration(*postIdleSleepMs)*time.Millisecond,
			)
		},
	}

	if *intervalM == 0 {
		runSweep(ctx, urls, cfg, conn)
		return
	}

	ticker := time.NewTicker(time.Duration(*intervalM) * time.Minute)
	defer ticker.Stop()

	runSweep(ctx, urls, cfg, conn)
	for {
		select {
		case <-ticker.C:
			runSweep(ctx, urls, cfg, conn)
		case <-ctx.Done():
			log.Println("shutting down")
			return
		}
	}
}

func runSweep(ctx context.Context, urls []string, cfg pipeline.Config, conn *grpc.ClientConn) {
	start := time.Now()
	total := len(urls)
	log.Printf("Starting sweep — %d sites", total)

	completed := 0
	cfg.OnResult = func(r pipeline.SiteResult) {
		completed++
		status := "compliant"
		if !r.Compliant {
			status = "non-compliant"
		}
		detail := ""
		if r.ResolvedIP != "" {
			detail += " ip=" + r.ResolvedIP
		}
		if len(r.Screenshot) > 0 {
			detail += " screenshot=ok"
		}
		if r.Error != "" {
			detail += " err=" + r.Error
		}
		log.Printf("[%d/%d] %s — %s%s", completed, total, r.URL, status, detail)
	}

	results, err := pipeline.Run(ctx, urls, cfg)
	if err != nil {
		log.Printf("sweep error: %v", err)
		return
	}

	compliant, nonCompliant := 0, 0
	for _, r := range results {
		if r.Compliant {
			compliant++
		} else {
			nonCompliant++
		}
	}
	log.Printf("Sweep complete in %s — %d compliant, %d non-compliant",
		time.Since(start).Round(time.Second), compliant, nonCompliant)

	paths := saveScreenshots(results, start)

	if conn != nil {
		report := buildReport(results)
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := sender.Send(sendCtx, conn, report); err != nil {
			log.Printf("gRPC send failed: %v", err)
		} else {
			log.Printf("Report sent to %s", conn.Target())
		}
	}
	printTable(results, paths)
}

func printTable(results []pipeline.SiteResult, paths map[string]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "URL\tCOMPLIANT\tRESOLVED_IP\tSCREENSHOT\tERROR")
	for _, r := range results {
		screenshotCol := "no"
		if path, ok := paths[r.URL]; ok {
			screenshotCol = path
		}
		fmt.Fprintf(w, "%s\t%v\t%s\t%s\t%s\n",
			r.URL, r.Compliant, r.ResolvedIP, screenshotCol, r.Error)
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
		}
	}
	return &pb.ComplianceReport{Results: pbResults}
}
