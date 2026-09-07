package dns

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

// RCodeError reports a final, non-NXDOMAIN DNS response — SERVFAIL,
// REFUSED, or a NOERROR with no A records. Both firstA and NewDoHResolver
// preserve the RCode in a RCodeError rather than collapsing it into a generic
// "no A records" string. This lets a caller tell a resolver actively refusing
// or failing a query apart from a genuine non-answer. That distinction
// matters: an ISP's rate-limiting or source-IP allowlisting typically shows
// up as REFUSED or SERVFAIL, which looks identical to a real block if this
// information is thrown away.
type RCodeError struct {
	RCode dnsmessage.RCode
	Host  string
}

func (e *RCodeError) Error() string {
	return fmt.Sprintf("dns: %s for %s", e.RCode, e.Host)
}

// Classify buckets a DNS resolution error into the small set of categories
// ScanResult.ErrorClass stores. Returns "" for a nil error (success).
func Classify(err error) string {
	if err == nil {
		return ""
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsNotFound:
			return "nxdomain"
		case dnsErr.IsTimeout:
			return "timeout"
		case strings.Contains(dnsErr.Err, "server misbehaving"):
			return "servfail" // system resolver's own SERVFAIL wording
		}
	}

	var rcErr *RCodeError
	if errors.As(err, &rcErr) {
		switch rcErr.RCode {
		case dnsmessage.RCodeServerFailure:
			return "servfail"
		case dnsmessage.RCodeRefused:
			return "refused"
		default:
			return "empty" // NOERROR, no A records
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}

	return "other"
}
