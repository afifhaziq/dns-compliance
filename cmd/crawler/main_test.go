package main

import (
	"testing"

	"github.com/afif/dns-tracking/internal/dnsconfig"
	"github.com/afif/dns-tracking/internal/pipeline"
)

func TestBuildServerEntries(t *testing.T) {
	servers := []dnsconfig.Server{
		{ISP: "Google", Name: "Google UDP", Address: "8.8.8.8:53", Protocol: "udp"},
		{ISP: "Cloudflare", Name: "Cloudflare DoT", Address: "1.1.1.1:853", Protocol: "dot"},
		{ISP: "Cloudflare", Name: "Cloudflare DoH", Address: "https://1.1.1.1/dns-query", Protocol: "doh"},
	}

	entries := buildServerEntries(servers)

	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	for i, e := range entries {
		if e.name != servers[i].Name {
			t.Errorf("entry %d: want name %q, got %q", i, servers[i].Name, e.name)
		}
		if e.resolve == nil {
			t.Errorf("entry %d: resolve func is nil", i)
		}
	}
}

// Two DNS servers resolving the same domain to the same IP share one Chrome
// capture — assignScreenshots must still give each of them its own copy of
// the bytes, so the server uploads (and shows evidence for) every DNS
// server's row, not just the first to resolve that pair.
func TestAssignScreenshotsCopiesToEverySharedResult(t *testing.T) {
	results := []pipeline.SiteResult{
		{URL: "https://example.com", ResolvedIP: "1.2.3.4", DNSServer: "Cloudflare DoT"},
		{URL: "https://example.com", ResolvedIP: "1.2.3.4", DNSServer: "Cloudflare DoH"},
	}
	shots := map[string][]byte{
		shotKey("https://example.com", "1.2.3.4"): []byte("fake-png"),
	}

	assignScreenshots(results, shots, nil)

	for i, r := range results {
		if string(r.Screenshot) != "fake-png" {
			t.Errorf("result %d (%s): expected shared screenshot bytes, got %q", i, r.DNSServer, r.Screenshot)
		}
	}
}

func TestAssignScreenshotsCopiesErrorsToEverySharedResult(t *testing.T) {
	results := []pipeline.SiteResult{
		{URL: "https://example.com", ResolvedIP: "1.2.3.4", DNSServer: "Cloudflare DoT"},
		{URL: "https://example.com", ResolvedIP: "1.2.3.4", DNSServer: "Cloudflare DoH"},
	}
	errs := map[string]string{
		shotKey("https://example.com", "1.2.3.4"): "capture timed out",
	}

	assignScreenshots(results, nil, errs)

	for i, r := range results {
		if r.Error != "capture timed out" {
			t.Errorf("result %d (%s): expected shared error, got %q", i, r.DNSServer, r.Error)
		}
	}
}
