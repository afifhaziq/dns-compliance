package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	pb "github.com/afif/dns-tracking/proto"
	"github.com/afif/dns-tracking/internal/dns"
	"github.com/afif/dns-tracking/internal/input"
	"github.com/afif/dns-tracking/internal/pipeline"
	"github.com/afif/dns-tracking/internal/screenshot"
	"github.com/afif/dns-tracking/internal/sender"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	sitesFile  := flag.String("sites", "", "path to file with one URL per line")
	dnsWorkers := flag.Int("dns-workers", 20, "number of DNS worker goroutines")
	ssWorkers  := flag.Int("screenshot-workers", 5, "number of screenshot worker goroutines")
	intervalM  := flag.Int("interval", 0, "sweep interval in minutes; 0 = run once and exit")
	grpcAddr   := flag.String("grpc-addr", "", "gRPC server address (e.g. localhost:50051); empty prints to stdout")
	timeoutSec := flag.Int("timeout", 30, "per-site total time budget in seconds (DNS + screenshot)")
	flag.Parse()

	urls, err := input.Load(*sitesFile, flag.Args())
	if err != nil {
		log.Fatalf("loading URLs: %v", err)
	}
	if len(urls) == 0 {
		log.Fatal("no URLs provided — use --sites or pass URLs as arguments")
	}

	var conn *grpc.ClientConn
	if *grpcAddr != "" {
		conn, err = grpc.NewClient(*grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("connecting to gRPC server: %v", err)
		}
		defer conn.Close()
	}

	cfg := pipeline.Config{
		DNSWorkers:        *dnsWorkers,
		ScreenshotWorkers: *ssWorkers,
		Timeout:           time.Duration(*timeoutSec) * time.Second,
		Resolve:           dns.Resolve,
		Capture:           screenshot.Capture,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	log.Printf("Starting sweep — %d sites", len(urls))

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

	if conn != nil {
		report := buildReport(results)
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := sender.Send(sendCtx, conn, report); err != nil {
			log.Printf("gRPC send failed: %v", err)
		} else {
			log.Printf("Report sent to %s", conn.Target())
		}
	} else {
		printTable(results)
	}
}

func printTable(results []pipeline.SiteResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "URL\tCOMPLIANT\tRESOLVED_IP\tSCREENSHOT\tERROR")
	for _, r := range results {
		hasScreenshot := "no"
		if len(r.Screenshot) > 0 {
			hasScreenshot = "yes"
		}
		fmt.Fprintf(w, "%s\t%v\t%s\t%s\t%s\n",
			r.URL, r.Compliant, r.ResolvedIP, hasScreenshot, r.Error)
	}
	w.Flush()
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
