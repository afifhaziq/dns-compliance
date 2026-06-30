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
func NewResolver(server string) func(context.Context, string) (string, int64, error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "udp", server)
		},
	}
	return func(ctx context.Context, host string) (string, int64, error) {
		return resolve(ctx, r, host)
	}
}

func resolve(ctx context.Context, r *net.Resolver, host string) (string, int64, error) {
	start := time.Now()
	addrs, err := r.LookupHost(ctx, host)
	if err != nil {
		return "", 0, err
	}
	if len(addrs) == 0 {
		return "", 0, fmt.Errorf("no addresses returned for %s", host)
	}
	return addrs[0], time.Since(start).Milliseconds(), nil
}

// NewDoTResolver returns a resolver that sends queries to address over DNS-over-TLS
// (port 853). address should be host:port, e.g. "1.1.1.1:853".
func NewDoTResolver(address string) func(context.Context, string) (string, int64, error) {
	host, _, _ := net.SplitHostPort(address)
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := tls.Dialer{Config: &tls.Config{ServerName: host}}
			return d.DialContext(ctx, "tcp", address)
		},
	}
	return func(ctx context.Context, host string) (string, int64, error) {
		return resolve(ctx, r, host)
	}
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

func dohName(s string) dnsmessage.Name {
	n, err := dnsmessage.NewName(s)
	if err != nil {
		panic("invalid DNS name: " + s)
	}
	return n
}
