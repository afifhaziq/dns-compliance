package dns

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"

	"golang.org/x/net/dns/dnsmessage"
)

// Resolve performs an A-record lookup for host and returns the first resolved IP.
// Returns an error if the domain does not resolve or the context is cancelled.
func Resolve(ctx context.Context, host string) (string, error) {
	return resolve(ctx, net.DefaultResolver, host)
}

// NewResolver returns a Resolve-compatible function that sends queries to server
// (e.g. "8.8.8.8:53") instead of the system resolver.
func NewResolver(server string) func(context.Context, string) (string, error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "udp", server)
		},
	}
	return func(ctx context.Context, host string) (string, error) {
		return resolve(ctx, r, host)
	}
}

func resolve(ctx context.Context, r *net.Resolver, host string) (string, error) {
	addrs, err := r.LookupHost(ctx, host)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no addresses returned for %s", host)
	}
	return addrs[0], nil
}

// NewDoTResolver returns a resolver that sends queries to address over DNS-over-TLS
// (port 853). address should be host:port, e.g. "1.1.1.1:853".
func NewDoTResolver(address string) func(context.Context, string) (string, error) {
	host, _, _ := net.SplitHostPort(address)
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := tls.Dialer{Config: &tls.Config{ServerName: host}}
			return d.DialContext(ctx, "tcp", address)
		},
	}
	return func(ctx context.Context, host string) (string, error) {
		return resolve(ctx, r, host)
	}
}

// NewDoHResolver returns a resolver that sends queries to endpoint over
// DNS-over-HTTPS. endpoint is a full URL, e.g. "https://1.1.1.1/dns-query".
func NewDoHResolver(endpoint string) func(context.Context, string) (string, error) {
	client := &http.Client{}
	return func(ctx context.Context, host string) (string, error) {
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
			if a, ok := ans.Body.(*dnsmessage.AResource); ok {
				return fmt.Sprintf("%d.%d.%d.%d", a.A[0], a.A[1], a.A[2], a.A[3]), nil
			}
		}
		return "", fmt.Errorf("no A records for %s", host)
	}
}

func dohName(s string) dnsmessage.Name {
	n, err := dnsmessage.NewName(s)
	if err != nil {
		panic("invalid DNS name: " + s)
	}
	return n
}
