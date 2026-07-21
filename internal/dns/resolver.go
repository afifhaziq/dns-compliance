package dns

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Resolve performs an A-record lookup for host and returns the first resolved IP
// and the round-trip latency in milliseconds.
// Returns an error if the domain does not resolve or the context is cancelled.
func Resolve(ctx context.Context, host string) (string, int64, error) {
	return resolve(ctx, net.DefaultResolver, host)
}

// NewResolver returns a Resolve-compatible function that sends queries to server
// (e.g. "8.8.8.8:53") instead of the system resolver.
//
// This treats any RCode (REFUSED, SERVFAIL, etc.) as a final answer — no
// retries once a response actually arrives. net.Resolver's built-in client
// only treats NXDOMAIN as final and silently retries everything else
// (including REFUSED) cfg.attempts x len(servers) times, which is what made
// real ISP resolvers that answer REFUSED instantly look like they were
// timing out. It does still retry once (see exchangeWithRetry) when no
// response arrives at all — genuine packet loss — since that's a transport
// failure, not an answer.
func NewResolver(server string) func(context.Context, string) (string, int64, error) {
	return func(ctx context.Context, host string) (string, int64, error) {
		start := time.Now()
		conn, err := (&net.Dialer{}).DialContext(ctx, "udp", server)
		if err != nil {
			return "", 0, err
		}
		defer conn.Close()

		query, err := buildQuery(host, dnsmessage.TypeA)
		if err != nil {
			return "", 0, err
		}

		body, err := exchangeWithRetry(ctx, func(deadline time.Time) ([]byte, error) {
			conn.SetDeadline(deadline)
			if _, err := conn.Write(query); err != nil {
				return nil, err
			}
			buf := make([]byte, 4096)
			n, err := conn.Read(buf)
			if err != nil {
				return nil, err
			}
			return buf[:n], nil
		})
		if err != nil {
			return "", 0, err
		}

		ip, err := firstA(body, host)
		if err != nil {
			return "", 0, err
		}
		return ip, time.Since(start).Milliseconds(), nil
	}
}

// exchangeWithRetry calls do up to twice, splitting whatever time remains on
// ctx's deadline fairly between attempts, and retries only when do itself
// fails to produce a response at all — a dropped packet or a dead
// connection. It never retries once do returns a response, no matter what
// RCode that response carries: a REFUSED/SERVFAIL/empty-NOERROR answer is a
// real, final answer, not a failure, and retrying on it is exactly the bug
// net.Resolver's built-in client has (see NewResolver's comment).
func exchangeWithRetry(ctx context.Context, do func(deadline time.Time) ([]byte, error)) ([]byte, error) {
	const maxAttempts = 2
	overallDeadline, hasDeadline := ctx.Deadline()
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		deadline := overallDeadline
		if hasDeadline {
			remainingAttempts := time.Duration(maxAttempts - attempt)
			deadline = time.Now().Add(time.Until(overallDeadline) / remainingAttempts)
		}
		body, err := do(deadline)
		if err == nil {
			return body, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func resolve(ctx context.Context, r *net.Resolver, host string) (string, int64, error) {
	start := time.Now()
	addrs, err := r.LookupIP(ctx, "ip4", host)
	if err != nil {
		return "", 0, err
	}
	if len(addrs) == 0 {
		return "", 0, fmt.Errorf("no addresses returned for %s", host)
	}
	return addrs[0].String(), time.Since(start).Milliseconds(), nil
}

// NewDoTResolver returns a resolver that sends queries to address over DNS-over-TLS
// (port 853). address should be host:port, e.g. "1.1.1.1:853".
//
// Like NewResolver, this treats any RCode as final — no retries on
// REFUSED/SERVFAIL/etc. — but does retry once (fresh connection) if no
// response arrives at all. See NewResolver's comment for why.
func NewDoTResolver(address string) func(context.Context, string) (string, int64, error) {
	tlsHost, _, _ := net.SplitHostPort(address)
	dialer := tls.Dialer{Config: &tls.Config{ServerName: tlsHost}}
	return func(ctx context.Context, host string) (string, int64, error) {
		start := time.Now()
		query, err := buildQuery(host, dnsmessage.TypeA)
		if err != nil {
			return "", 0, err
		}

		resp, err := exchangeWithRetry(ctx, func(deadline time.Time) ([]byte, error) {
			conn, err := dialer.DialContext(ctx, "tcp", address)
			if err != nil {
				return nil, err
			}
			defer conn.Close()
			conn.SetDeadline(deadline)
			if err := writeTCPMessage(conn, query); err != nil {
				return nil, err
			}
			return readTCPMessage(conn)
		})
		if err != nil {
			return "", 0, err
		}

		ip, err := firstA(resp, host)
		if err != nil {
			return "", 0, err
		}
		return ip, time.Since(start).Milliseconds(), nil
	}
}

func newDoTResolver(address string) *net.Resolver {
	host, _, _ := net.SplitHostPort(address)
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := tls.Dialer{Config: &tls.Config{ServerName: host}}
			return d.DialContext(ctx, "tcp", address)
		},
	}
}

// ResolveIPv6 performs an AAAA-record lookup for host and returns the first
// resolved IPv6 address. Informational only — never affects the compliance
// verdict, so callers ignore errors.
func ResolveIPv6(ctx context.Context, host string) (string, error) {
	return resolveIPv6(ctx, net.DefaultResolver, host)
}

// NewResolverIPv6 returns an AAAA-lookup function that queries server instead
// of the system resolver.
func NewResolverIPv6(server string) func(context.Context, string) (string, error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "udp", server)
		},
	}
	return func(ctx context.Context, host string) (string, error) {
		return resolveIPv6(ctx, r, host)
	}
}

// NewDoTResolverIPv6 returns an AAAA-lookup function that queries address over
// DNS-over-TLS.
func NewDoTResolverIPv6(address string) func(context.Context, string) (string, error) {
	r := newDoTResolver(address)
	return func(ctx context.Context, host string) (string, error) {
		return resolveIPv6(ctx, r, host)
	}
}

func resolveIPv6(ctx context.Context, r *net.Resolver, host string) (string, error) {
	addrs, err := r.LookupIP(ctx, "ip6", host)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no AAAA addresses returned for %s", host)
	}
	return addrs[0].String(), nil
}

// NewDoHResolver returns a resolver that sends queries to endpoint over
// DNS-over-HTTPS. endpoint is a full URL, e.g. "https://1.1.1.1/dns-query".
func NewDoHResolver(endpoint string) func(context.Context, string) (string, int64, error) {
	client := &http.Client{}
	return func(ctx context.Context, host string) (string, int64, error) {
		msg := dnsmessage.Message{
			Header: dnsmessage.Header{ID: 1, RecursionDesired: true},
			Questions: []dnsmessage.Question{{
				Name:  dohName(host + "."),
				Type:  dnsmessage.TypeA,
				Class: dnsmessage.ClassINET,
			}},
		}
		buf, err := msg.Pack()
		if err != nil {
			return "", 0, fmt.Errorf("building DNS query: %w", err)
		}

		reqURL := endpoint + "?dns=" + base64.RawURLEncoding.EncodeToString(buf)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", 0, err
		}
		req.Header.Set("Accept", "application/dns-message")

		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return "", 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", 0, fmt.Errorf("DoH server returned HTTP %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", 0, err
		}

		var reply dnsmessage.Message
		if err := reply.Unpack(body); err != nil {
			return "", 0, fmt.Errorf("parsing DoH response: %w", err)
		}
		if reply.Header.RCode == dnsmessage.RCodeNameError {
			return "", 0, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		for _, ans := range reply.Answers {
			if a, ok := ans.Body.(*dnsmessage.AResource); ok {
				return fmt.Sprintf("%d.%d.%d.%d", a.A[0], a.A[1], a.A[2], a.A[3]), time.Since(start).Milliseconds(), nil
			}
		}
		return "", 0, fmt.Errorf("no A records for %s", host)
	}
}

// NewDoHResolverIPv6 returns an AAAA-lookup function that queries endpoint
// over DNS-over-HTTPS.
func NewDoHResolverIPv6(endpoint string) func(context.Context, string) (string, error) {
	client := &http.Client{}
	return func(ctx context.Context, host string) (string, error) {
		msg := dnsmessage.Message{
			Header: dnsmessage.Header{ID: 1, RecursionDesired: true},
			Questions: []dnsmessage.Question{{
				Name:  dohName(host + "."),
				Type:  dnsmessage.TypeAAAA,
				Class: dnsmessage.ClassINET,
			}},
		}
		buf, err := msg.Pack()
		if err != nil {
			return "", fmt.Errorf("building DNS query: %w", err)
		}

		reqURL := endpoint + "?dns=" + base64.RawURLEncoding.EncodeToString(buf)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/dns-message")

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("DoH server returned HTTP %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		var reply dnsmessage.Message
		if err := reply.Unpack(body); err != nil {
			return "", fmt.Errorf("parsing DoH response: %w", err)
		}
		if reply.Header.RCode == dnsmessage.RCodeNameError {
			return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		for _, ans := range reply.Answers {
			if a, ok := ans.Body.(*dnsmessage.AAAAResource); ok {
				return net.IP(a.AAAA[:]).String(), nil
			}
		}
		return "", fmt.Errorf("no AAAA records for %s", host)
	}
}

func dohName(s string) dnsmessage.Name {
	n, err := dnsmessage.NewName(s)
	if err != nil {
		panic("invalid DNS name: " + s)
	}
	return n
}

// buildQuery marshals a single-question DNS query for host (an A or AAAA
// lookup depending on qtype) to wire format.
func buildQuery(host string, qtype dnsmessage.Type) ([]byte, error) {
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		return nil, fmt.Errorf("building DNS query: %w", err)
	}
	msg := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 1, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: name, Type: qtype, Class: dnsmessage.ClassINET}},
	}
	return msg.Pack()
}

// firstA parses a raw DNS response and returns the first A record found.
// Any RCode other than NXDOMAIN (REFUSED, SERVFAIL, or a NOERROR with no
// answers) falls through to the generic "no A records" error — the caller
// gets an immediate, final answer either way, matching dig's non-retrying
// behavior instead of net.Resolver's retry-on-anything-but-NXDOMAIN.
func firstA(body []byte, host string) (string, error) {
	var reply dnsmessage.Message
	if err := reply.Unpack(body); err != nil {
		return "", fmt.Errorf("parsing DNS response: %w", err)
	}
	if reply.Header.RCode == dnsmessage.RCodeNameError {
		return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	for _, ans := range reply.Answers {
		if a, ok := ans.Body.(*dnsmessage.AResource); ok {
			return fmt.Sprintf("%d.%d.%d.%d", a.A[0], a.A[1], a.A[2], a.A[3]), nil
		}
	}
	return "", fmt.Errorf("no A records for %s", host)
}

// writeTCPMessage writes msg to conn with the 2-byte length prefix DNS-over-TCP
// (and therefore DoT) requires (RFC 1035 §4.2.2).
func writeTCPMessage(conn net.Conn, msg []byte) error {
	lenBuf := []byte{byte(len(msg) >> 8), byte(len(msg))}
	_, err := conn.Write(append(lenBuf, msg...))
	return err
}

// readTCPMessage reads one length-prefixed DNS message from conn.
func readTCPMessage(conn net.Conn) ([]byte, error) {
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, err
	}
	resp := make([]byte, int(lenBuf[0])<<8|int(lenBuf[1]))
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, err
	}
	return resp, nil
}
