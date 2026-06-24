// Package urlnorm canonicalizes raw URL/hostname input into the bare
// lowercase hostname used as the storage key for the urls table — so the
// same domain entered in different formats (with/without scheme, trailing
// slash, casing) always resolves to one shared row.
//
// This is intentionally separate from pipeline.normalizeURL, which only
// prefixes a scheme so the crawler can navigate to a bare hostname — it does
// not strip back down to a bare domain, and must keep working unchanged.
package urlnorm

import (
	"fmt"
	"net/url"
	"strings"
)

// Normalize strips scheme, userinfo, path, query, fragment, port, and any
// trailing dot, then lowercases the result. Returns an error if no usable
// hostname can be extracted.
func Normalize(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("urlnorm: empty input")
	}

	withScheme := trimmed
	if !strings.Contains(withScheme, "://") {
		withScheme = "https://" + withScheme
	}

	parsed, err := url.Parse(withScheme)
	if err != nil {
		return "", fmt.Errorf("urlnorm: parsing %q: %w", raw, err)
	}

	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", fmt.Errorf("urlnorm: no hostname found in %q", raw)
	}
	return host, nil
}
