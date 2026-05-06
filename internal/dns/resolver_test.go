package dns_test

import (
	"context"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/dns"
)

func TestResolveKnownDomain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ip, err := dns.Resolve(ctx, "google.com")
	if err != nil {
		t.Fatalf("expected google.com to resolve, got error: %v", err)
	}
	if ip == "" {
		t.Fatal("expected non-empty IP")
	}
}

func TestResolveNXDomain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := dns.Resolve(ctx, "this-domain-does-not-exist-xyz123abc.com")
	if err == nil {
		t.Fatal("expected error for non-existent domain, got nil")
	}
}

func TestResolveRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := dns.Resolve(ctx, "google.com")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}
