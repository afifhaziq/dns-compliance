package main

import (
	"testing"

	"github.com/afif/dns-tracking/internal/dnsconfig"
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
