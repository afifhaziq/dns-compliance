package screenshot

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestBuildFrameHTMLIncludesISPChipWhenBothPresent(t *testing.T) {
	html := buildFrameHTML([]byte("fake-png"), "https://example.com", time.Now(), "Cloudflare", "1.1.1.1:853")
	if !strings.Contains(html, "Cloudflare · 1.1.1.1:853") {
		t.Fatalf("expected ISP/address chip in output, got:\n%s", html)
	}
}

func TestBuildFrameHTMLOmitsChipWhenBothEmpty(t *testing.T) {
	html := buildFrameHTML([]byte("fake-png"), "https://example.com", time.Now(), "", "")
	if strings.Contains(html, "·") {
		t.Fatalf("expected no ISP/address chip when both empty, got:\n%s", html)
	}
}

func TestBuildFrameHTMLFallsBackToWhicheverSideIsSet(t *testing.T) {
	html := buildFrameHTML([]byte("fake-png"), "https://example.com", time.Now(), "Cloudflare", "")
	if !strings.Contains(html, ">Cloudflare<") {
		t.Fatalf("expected ISP-only chip, got:\n%s", html)
	}

	html = buildFrameHTML([]byte("fake-png"), "https://example.com", time.Now(), "", "1.1.1.1:853")
	if !strings.Contains(html, ">1.1.1.1:853<") {
		t.Fatalf("expected address-only chip, got:\n%s", html)
	}
}

func TestBuildFrameHTMLStillIncludesTimestampChip(t *testing.T) {
	capturedAt := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	html := buildFrameHTML([]byte("fake-png"), "https://example.com", capturedAt, "", "")
	if !strings.Contains(html, "2026-08-04 20:00:00 MYT") {
		t.Fatalf("expected timestamp chip (UTC+8), got:\n%s", html)
	}
}

// solidPNG builds a minimal single-color PNG of the given pixel dimensions,
// for feeding into Frame as pageBytes without needing a real screenshot.
func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: 200})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding test PNG: %v", err)
	}
	return buf.Bytes()
}

// TestFrameClearsDeviceMetricsOverrideBetweenCalls reproduces the shared-tab
// leak from the final-review finding: Frame previously left its
// SetDeviceMetricsOverride in place after capturing, so a later Frame call on
// the same chromedp tab (frameScreenshots' whole point) measured its content
// height floored at the *previous* call's height instead of its own. A tall
// page framed first, followed by a much shorter page, must not produce a
// second output that's still stretched to the first output's height.
func TestFrameClearsDeviceMetricsOverrideBetweenCalls(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run screenshot tests (requires Chrome)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, AllocatorOptions...)
	defer allocCancel()
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()

	tallPNG := solidPNG(t, 960, 400)  // displayed at 1920px wide -> ~800px tall
	shortPNG := solidPNG(t, 960, 50) // displayed at 1920px wide -> ~100px tall

	tallOut, err := Frame(tabCtx, tallPNG, "https://example.com", time.Now(), "", "")
	if err != nil {
		t.Fatalf("first (tall) Frame call failed: %v", err)
	}
	shortOut, err := Frame(tabCtx, shortPNG, "https://example.com", time.Now(), "", "")
	if err != nil {
		t.Fatalf("second (short) Frame call failed: %v", err)
	}

	tallCfg, _, err := image.DecodeConfig(bytes.NewReader(tallOut))
	if err != nil {
		t.Fatalf("decoding tall output PNG: %v", err)
	}
	shortCfg, _, err := image.DecodeConfig(bytes.NewReader(shortOut))
	if err != nil {
		t.Fatalf("decoding short output PNG: %v", err)
	}

	// Without the fix, shortCfg.Height would be floored at ~tallCfg.Height
	// (the leftover override). With the fix it reflects its own, much
	// shorter content instead.
	if shortCfg.Height >= tallCfg.Height {
		t.Fatalf("second (short) frame height %d not smaller than first (tall) frame height %d — device-metrics override leaked across the shared tab", shortCfg.Height, tallCfg.Height)
	}
}
