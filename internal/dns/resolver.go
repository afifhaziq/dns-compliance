package dns

import (
	"context"
	"fmt"
	"net"
)

// Resolve performs an A-record lookup for host and returns the first resolved IP.
// Returns an error if the domain does not resolve or the context is cancelled.
func Resolve(ctx context.Context, host string) (string, error) {
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no addresses returned for %s", host)
	}
	return addrs[0], nil
}
