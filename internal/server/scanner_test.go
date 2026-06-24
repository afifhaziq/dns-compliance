package server_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/server"
)

// fakeCrawlerScript exits 0 immediately, simulating a successful crawler run.
const fakeCrawlerScript = "#!/bin/sh\nexit 0\n"

func writeFakeCrawler(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "crawler-*.sh")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	f.WriteString(fakeCrawlerScript)
	f.Close()
	os.Chmod(f.Name(), 0755)
	return f.Name()
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
	return []db.DNSServer{{ID: 1, Name: "G", Address: "8.8.8.8:53", Protocol: "udp"}}, nil
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

func TestScannerTriggerRunsAndCompletes(t *testing.T) {
	crawlerPath := writeFakeCrawler(t)
	store := &completionCapture{}
	sc := server.NewScanner(crawlerPath, "localhost:50051", store)

	if err := sc.Trigger(context.Background(), "manual"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	waitUntil(t, func() bool { return !sc.IsRunning() }, 3*time.Second)

	if len(store.completed) == 0 {
		t.Fatal("expected CompleteScanRun to be called")
	}
}

func TestScannerRejectsConcurrentRun(t *testing.T) {
	crawlerPath := writeFakeCrawler(t)
	// Use a script that sleeps briefly so the second Trigger hits while running.
	f, _ := os.CreateTemp(t.TempDir(), "slow-*.sh")
	f.WriteString("#!/bin/sh\nsleep 0.3\nexit 0\n")
	f.Close()
	os.Chmod(f.Name(), 0755)

	store := &completionCapture{}
	sc := server.NewScanner(f.Name(), "localhost:50051", store)

	_ = sc.Trigger(context.Background(), "manual")
	err := sc.Trigger(context.Background(), "manual")
	if err == nil {
		t.Fatal("expected error for concurrent scan")
	}

	// Wait for first scan to finish.
	waitUntil(t, func() bool { return !sc.IsRunning() }, 3*time.Second)
	_ = crawlerPath
}
