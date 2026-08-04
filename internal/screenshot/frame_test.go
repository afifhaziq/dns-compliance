package screenshot

import (
	"strings"
	"testing"
	"time"
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
