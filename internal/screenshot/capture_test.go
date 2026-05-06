package screenshot_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/screenshot"
)

func TestCaptureReturnsPNG(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run screenshot tests (requires Chrome)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	buf, err := screenshot.Capture(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if len(buf) == 0 {
		t.Fatal("expected non-empty screenshot bytes")
	}
	// PNG magic bytes: 0x89 0x50 0x4E 0x47
	if len(buf) < 4 || buf[0] != 0x89 || buf[1] != 0x50 || buf[2] != 0x4E || buf[3] != 0x47 {
		t.Fatal("output is not a valid PNG")
	}
}

func TestCaptureRespectsTimeout(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run screenshot tests (requires Chrome)")
	}

	// 1ms is too short for any navigation — should always time out
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := screenshot.Capture(ctx, "https://example.com")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
