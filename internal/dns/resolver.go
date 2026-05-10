package dns

import (
	"context"
	"fmt"
	"net"
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
