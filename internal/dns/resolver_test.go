package dns_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/dns"
)

func TestResolveKnownDomain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ip, _, err := dns.Resolve(ctx, "google.com")
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

	_, _, err := dns.Resolve(ctx, "this-domain-does-not-exist-xyz123abc.com")
	if err == nil {
		t.Fatal("expected error for non-existent domain, got nil")
	}
}

func TestResolveRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := dns.Resolve(ctx, "google.com")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestNewResolverKnownDomain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resolve := dns.NewResolver("8.8.8.8:53")
	ip, _, err := resolve(ctx, "google.com")
	// CI runners can't reach public DNS directly; a timeout there says nothing about the resolver.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Skipf("8.8.8.8 unreachable from this network: %v", err)
	}
	if err != nil {
		t.Fatalf("expected google.com to resolve via 8.8.8.8, got error: %v", err)
	}
	if ip == "" {
		t.Fatal("expected non-empty IP")
	}
}

func TestNewResolverNXDomain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resolve := dns.NewResolver("8.8.8.8:53")
	output, _, err := resolve(ctx, "https://www.tiktok.com/@mbah.sugeng.sujiwo/photo/7367929772485152005")
	print(output)
	if err == nil {
		t.Fatal("expected error for non-existent domain, got nil")
	}
}
