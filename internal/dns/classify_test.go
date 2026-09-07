package dns

import (
	"context"
	"errors"
	"net"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil is empty", nil, ""},
		{"nxdomain", &net.DNSError{Err: "no such host", IsNotFound: true}, "nxdomain"},
		{"servfail rcode", &RCodeError{RCode: dnsmessage.RCodeServerFailure, Host: "example.com"}, "servfail"},
		{"refused rcode", &RCodeError{RCode: dnsmessage.RCodeRefused, Host: "example.com"}, "refused"},
		{"noerror with no answers", &RCodeError{RCode: dnsmessage.RCodeSuccess, Host: "example.com"}, "empty"},
		{"context deadline exceeded", context.DeadlineExceeded, "timeout"},
		{"dns error timeout flag", &net.DNSError{Err: "i/o timeout", IsTimeout: true}, "timeout"},
		{"system resolver servfail wording", &net.DNSError{Err: "server misbehaving"}, "servfail"},
		{"unrecognized error", errors.New("boom"), "other"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.err); got != c.want {
				t.Errorf("Classify(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}
