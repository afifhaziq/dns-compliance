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
