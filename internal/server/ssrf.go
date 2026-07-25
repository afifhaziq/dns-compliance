package server

import (
	"net"
	"strings"

	"github.com/afif/dns-tracking/internal/urlnorm"
)

// isPrivateHost reports whether raw's hostname is unsuitable as an outbound
// target: unparseable, "localhost", or resolving to a loopback/private/
// link-local/unspecified address. Callers that hand user-supplied URLs to
// the crawler or headless Chrome (AddToWatchlist, TriggerScreenshot) must
// check this first — otherwise an authenticated user can point either at
// 169.254.169.254 or an internal 10.x/192.168.x address.
func isPrivateHost(raw string) bool {
	host, err := urlnorm.Normalize(raw)
	if err != nil {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		// Unresolvable — not itself a private-IP finding; the crawler's own
		// DNS check reports this failure later.
		return false
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}
