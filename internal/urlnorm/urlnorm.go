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
	"net"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

var (
	// schemePrefixRe matches a scheme with mistyped separators, as seen in
	// spreadsheet cells: "https;//x.com", "http:x.com", "https//x.com".
	schemePrefixRe = regexp.MustCompile(`(?i)^https?([:;]/*|//)`)
	// tldRe is what a real top-level label looks like: letters, or punycode.
	tldRe = regexp.MustCompile(`^([a-z]{2,63}|xn--[a-z0-9-]+)$`)
)

// Normalize strips scheme, userinfo, path, query, fragment, port, and any
// trailing dot/list punctuation, then lowercases the result. It also repairs
// the typos hand-typed lists are full of: whitespace inside the name
// ("www. example.com"), a mistyped scheme ("https;//"), a garbled port
// ("example.com:78zz.html"), and converts IDNs to punycode so DNS can
// resolve them. Returns an error if no plausible hostname can be extracted.
func Normalize(raw string) (string, error) {
	trimmed := strings.Join(strings.Fields(raw), "")
	if trimmed == "" {
		return "", fmt.Errorf("urlnorm: empty input")
	}

	rest := schemePrefixRe.ReplaceAllString(trimmed, "")
	// Drop the port by hand before parsing: url.Parse rejects a garbled one
	// ("example.com:78zz.html") outright instead of just ignoring it.
	if authority, _, _ := strings.Cut(rest, "/"); !strings.Contains(authority, "[") {
		if host, port, ok := strings.Cut(authority, ":"); ok && !strings.Contains(port, "@") {
			rest = host + rest[len(authority):]
		}
	}
	parsed, err := url.Parse("https://" + rest)
	if err != nil {
		return "", fmt.Errorf("urlnorm: parsing %q: %w", raw, err)
	}
	host := parsed.Hostname()

	// url.Parse accepts list punctuation in the host ("example.com;" from
	// spreadsheet cells), so strip it along with the trailing dot.
	host = strings.TrimRight(strings.ToLower(host), ".;,")
	if host == "" {
		return "", fmt.Errorf("urlnorm: no hostname found in %q", raw)
	}
	if net.ParseIP(host) != nil {
		return host, nil
	}
	if host, err = idna.Lookup.ToASCII(host); err != nil {
		return "", fmt.Errorf("urlnorm: invalid hostname in %q: %w", raw, err)
	}
	if !tldRe.MatchString(host[strings.LastIndex(host, ".")+1:]) || !strings.Contains(host, ".") {
		return "", fmt.Errorf("urlnorm: no valid top-level domain in %q", raw)
	}
	return host, nil
}
