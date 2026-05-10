package dnsconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/afif/dns-tracking/internal/dnsconfig"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "dns-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestLoadValid(t *testing.T) {
	path := writeTemp(t, `
servers:
  - name: Google
    address: 8.8.8.8:53
  - name: Cloudflare
    address: 1.1.1.1:53
`)
	cfg, err := dnsconfig.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("want 2 servers, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "Google" || cfg.Servers[0].Address != "8.8.8.8:53" {
		t.Errorf("unexpected first server: %+v", cfg.Servers[0])
	}
}

func TestLoadNameDefaultsToAddress(t *testing.T) {
	path := writeTemp(t, `
servers:
  - address: 9.9.9.9:53
`)
	cfg, err := dnsconfig.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].Name != "9.9.9.9:53" {
		t.Errorf("want name to default to address, got %q", cfg.Servers[0].Name)
	}
}

func TestLoadMissingAddress(t *testing.T) {
	path := writeTemp(t, `
servers:
  - name: NoAddress
`)
	_, err := dnsconfig.Load(path)
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestLoadEmptyServers(t *testing.T) {
	path := writeTemp(t, `servers: []`)
	_, err := dnsconfig.Load(path)
	if err == nil {
		t.Fatal("expected error for empty server list")
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	path := writeTemp(t, `:::not yaml:::`)
	_, err := dnsconfig.Load(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := dnsconfig.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
