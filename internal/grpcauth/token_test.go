package grpcauth_test

import (
	"context"
	"testing"

	"github.com/afif/dns-tracking/internal/grpcauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// invoke runs interceptor against a request carrying sentToken as incoming
// metadata, mimicking what the gRPC transport does on the wire. When
// sendMetadata is false no metadata is attached at all.
func invoke(t *testing.T, interceptor grpc.UnaryServerInterceptor, sentToken string, sendMetadata bool) error {
	t.Helper()
	ctx := context.Background()
	if sendMetadata {
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(grpcauth.MetadataKey, sentToken))
	}
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	return err
}

func TestUnaryInterceptor_AcceptsMatchingToken(t *testing.T) {
	ic := grpcauth.UnaryInterceptor("s3cret")
	if ic == nil {
		t.Fatal("interceptor is nil for a non-empty token")
	}
	if err := invoke(t, ic, "s3cret", true); err != nil {
		t.Fatalf("matching token rejected: %v", err)
	}
}

func TestUnaryInterceptor_RejectsWrongToken(t *testing.T) {
	ic := grpcauth.UnaryInterceptor("s3cret")
	err := invoke(t, ic, "wrong", true)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

func TestUnaryInterceptor_RejectsMissingMetadata(t *testing.T) {
	ic := grpcauth.UnaryInterceptor("s3cret")
	err := invoke(t, ic, "", false)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

// An empty token must yield no interceptor at all: ConstantTimeCompare("", "")
// reports a match, so installing it would accept every request while looking
// like it enforces auth.
func TestUnaryInterceptor_NilForEmptyToken(t *testing.T) {
	if ic := grpcauth.UnaryInterceptor(""); ic != nil {
		t.Fatal("empty token produced an interceptor; it must return nil")
	}
}

func TestAppendToken_SetsOutgoingMetadata(t *testing.T) {
	ctx := grpcauth.AppendToken(context.Background(), "s3cret")
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("no outgoing metadata attached")
	}
	if got := md.Get(grpcauth.MetadataKey); len(got) != 1 || got[0] != "s3cret" {
		t.Fatalf("want [s3cret], got %v", got)
	}
}
