package server

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/urlnorm"
	"gopkg.in/yaml.v3"
)

type Scanner struct {
	crawlerPath string
	grpcAddr    string
	store       db.Store
	mu          sync.Mutex
	running     bool
}

func NewScanner(crawlerPath, grpcAddr string, store db.Store) *Scanner {
	return &Scanner{crawlerPath: crawlerPath, grpcAddr: grpcAddr, store: store}
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

	urlFile, err := writeTempLines(urlObjs)
	if err != nil {
		log.Printf("scanner: write url file: %v", err)
		return
	}
	defer os.Remove(urlFile)

	dnsFile, err := sc.writeDNSYAML(servers)
	if err != nil {
		log.Printf("scanner: write dns yaml: %v", err)
		return
	}
	defer os.Remove(dnsFile)

	run, err := sc.store.CreateScanRun(ctx, triggeredBy)
	if err != nil {
		log.Printf("scanner: create scan run: %v", err)
		return
	}

	args := []string{
		"--sites", urlFile,
		"--dns-servers", dnsFile,
		"--grpc-addr", sc.grpcAddr,
	}
	if ipArg := sc.compliantIPsArg(ctx); ipArg != "" {
		args = append(args, "--compliant-ips", ipArg)
	}
	sc.execCrawler(ctx, args, run.ID)
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

	dnsFile, err := sc.writeDNSYAML(target)
	if err != nil {
		log.Printf("scanner: write dns yaml: %v", err)
		return
	}
	defer os.Remove(dnsFile)

	urlFile, err := writeTempLines([]db.URL{{URL: rawURL}})
	if err != nil {
		log.Printf("scanner: write url file: %v", err)
		return
	}
	defer os.Remove(urlFile)

	run, err := sc.store.CreateScanRun(ctx, "screenshot")
	if err != nil {
		log.Printf("scanner: create scan run: %v", err)
		return
	}

	args := []string{
		"--sites", urlFile,
		"--dns-servers", dnsFile,
		"--grpc-addr", sc.grpcAddr,
		"--screenshots",
	}
	if ipArg := sc.compliantIPsArg(ctx); ipArg != "" {
		args = append(args, "--compliant-ips", ipArg)
	}
	sc.execCrawler(ctx, args, run.ID)
}

// compliantIPsArg fetches the compliant IPs from the store and returns them
// as a comma-separated string suitable for --compliant-ips. Returns "" if
// the list is empty or the fetch fails (non-fatal; scan proceeds without it).
func (sc *Scanner) compliantIPsArg(ctx context.Context) string {
	ips, err := sc.store.ListCompliantIPs(ctx)
	if err != nil {
		log.Printf("scanner: load compliant IPs: %v", err)
		return ""
	}
	addrs := make([]string, len(ips))
	for i, ip := range ips {
		addrs[i] = ip.Address
	}
	return strings.Join(addrs, ",")
}

func (sc *Scanner) execCrawler(ctx context.Context, args []string, runID uint) {
	cmd := exec.CommandContext(ctx, sc.crawlerPath, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	status := "completed"
	if err := cmd.Run(); err != nil {
		log.Printf("scanner: crawler exited with error: %v", err)
		status = "failed"
	}
	now := time.Now()
	_ = sc.store.CompleteScanRun(ctx, runID, status, now)
}

func (sc *Scanner) setRunning(v bool) {
	sc.mu.Lock()
	sc.running = v
	sc.mu.Unlock()
}

type dnsYAMLEntry struct {
	ISP      string `yaml:"isp"`
	Name     string `yaml:"name"`
	Address  string `yaml:"address"`
	Protocol string `yaml:"protocol"`
}

type dnsYAMLConfig struct {
	Servers []dnsYAMLEntry `yaml:"servers"`
}

func (sc *Scanner) writeDNSYAML(servers []db.DNSServer) (string, error) {
	f, err := os.CreateTemp("", "dns-*.yaml")
	if err != nil {
		return "", err
	}
	defer f.Close()
	entries := make([]dnsYAMLEntry, len(servers))
	for i, s := range servers {
		entries[i] = dnsYAMLEntry{ISP: s.ISP, Name: s.Name, Address: s.Address, Protocol: s.Protocol}
	}
	if err := yaml.NewEncoder(f).Encode(dnsYAMLConfig{Servers: entries}); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func writeTempLines(urls []db.URL) (string, error) {
	f, err := os.CreateTemp("", "urls-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close()
	for _, u := range urls {
		f.WriteString(u.URL + "\n")
	}
	return f.Name(), nil
}
