package pipeline_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/pipeline"
)

func mockResolve(ip string, err error) func(context.Context, string) (string, error) {
	return func(_ context.Context, _ string) (string, error) {
		return ip, err
	}
}

func mockCapture(buf []byte, err error) func(context.Context, string) ([]byte, error) {
	return func(_ context.Context, _ string) ([]byte, error) {
		return buf, err
	}
}

func TestCompliantSiteSkipsScreenshot(t *testing.T) {
	screenshotCalled := false
	cfg := pipeline.Config{
		DNSWorkers:        2,
		ScreenshotWorkers: 2,
		DNSTimeout:        5 * time.Second,
		ScreenshotTimeout: 5 * time.Second,
		Resolve:           mockResolve("", errors.New("NXDOMAIN")),
		Capture: func(ctx context.Context, url string) ([]byte, error) {
			screenshotCalled = true
			return nil, nil
		},
	}

	results, err := pipeline.Run(context.Background(), []string{"https://down-site.com"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Compliant {
		t.Error("site that failed DNS should be Compliant=true")
	}
	if r.Screenshot != nil {
		t.Error("screenshot should be nil for compliant site")
	}
	if screenshotCalled {
		t.Error("screenshot capture should not be called for sites that fail DNS")
	}
}

func TestNonCompliantSiteTakesScreenshot(t *testing.T) {
	fakePNG := []byte{0x89, 0x50, 0x4E, 0x47}
	cfg := pipeline.Config{
		DNSWorkers:        2,
		ScreenshotWorkers: 2,
		DNSTimeout:        5 * time.Second,
		ScreenshotTimeout: 5 * time.Second,
		Resolve:           mockResolve("1.2.3.4", nil),
		Capture:           mockCapture(fakePNG, nil),
	}

	results, err := pipeline.Run(context.Background(), []string{"https://live-site.com"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Compliant {
		t.Error("accessible site should be Compliant=false")
	}
	if r.ResolvedIP != "1.2.3.4" {
		t.Errorf("want resolved IP 1.2.3.4, got %s", r.ResolvedIP)
	}
	if len(r.Screenshot) == 0 {
		t.Error("expected non-empty screenshot for accessible site")
	}
}

func TestScreenshotFailureIsStillNonCompliant(t *testing.T) {
	cfg := pipeline.Config{
		DNSWorkers:        2,
		ScreenshotWorkers: 2,
		DNSTimeout:        5 * time.Second,
		ScreenshotTimeout: 5 * time.Second,
		Resolve:           mockResolve("1.2.3.4", nil),
		Capture:           mockCapture(nil, errors.New("page load failed")),
	}

	results, err := pipeline.Run(context.Background(), []string{"https://partial-site.com"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Compliant {
		t.Error("site with live DNS should be Compliant=false even if screenshot fails")
	}
	if r.Error == "" {
		t.Error("expected non-empty Error when screenshot fails")
	}
}

func TestMultipleSitesAllProcessed(t *testing.T) {
	fakePNG := []byte{0x89, 0x50, 0x4E, 0x47}
	cfg := pipeline.Config{
		DNSWorkers:        5,
		ScreenshotWorkers: 3,
		DNSTimeout:        5 * time.Second,
		ScreenshotTimeout: 5 * time.Second,
		Resolve:           mockResolve("1.1.1.1", nil),
		Capture:           mockCapture(fakePNG, nil),
	}

	urls := []string{
		"https://site1.com",
		"https://site2.com",
		"https://site3.com",
		"https://site4.com",
		"https://site5.com",
	}
	results, err := pipeline.Run(context.Background(), urls, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("want 5 results, got %d", len(results))
	}
}
