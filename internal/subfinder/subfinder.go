// Package subfinder enumerates subdomains for a domain via the subfinder
// CLI (github.com/projectdiscovery/subfinder), shelled out to as a
// subprocess — mirrors how internal/whois wraps RDAP lookups.
package subfinder

import (
	"context"
	"os/exec"
	"strings"
)

// Fetcher enumerates subdomains for domain. Exposed as a var type so tests
// can inject a fake instead of shelling out.
type Fetcher func(ctx context.Context, domain string) ([]string, error)

// NewFetcher returns a Fetcher backed by the subfinder binary at binPath.
func NewFetcher(binPath string) Fetcher {
	return func(ctx context.Context, domain string) ([]string, error) {
		out, err := exec.CommandContext(ctx, binPath, "-d", domain, "-silent").Output()
		if err != nil {
			return nil, err
		}
		return parseLines(out), nil
	}
}

// parseLines splits subfinder -silent's one-hostname-per-line stdout,
// trimming whitespace and dropping blank lines.
func parseLines(out []byte) []string {
	var subs []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			subs = append(subs, line)
		}
	}
	return subs
}
