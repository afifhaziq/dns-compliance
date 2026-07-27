// Package grpcauth holds the shared-secret and mutual-TLS plumbing used by
// both gRPC links between cmd/server and cmd/crawler. It is its own package
// because cmd/crawler cannot import internal/server — that would pull in db,
// storage, and minio for the sake of one constant.
package grpcauth

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// MetadataKey carries the shared secret on every authenticated RPC.
const MetadataKey = "x-auth-token"

// AppendToken attaches the shared secret to an outgoing RPC's metadata.
func AppendToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, MetadataKey, token)
}

// UnaryInterceptor rejects any RPC whose MetadataKey metadata doesn't match
// token, using a constant-time comparison — this is a real credential check,
// not a lookup, so timing must not leak partial matches.
//
// It returns nil when token is empty, because subtle.ConstantTimeCompare("",
// "") reports a match: installing it would accept every request while
// appearing to enforce auth. Callers must treat nil as "no auth configured",
// skip installing the interceptor, and log a warning.
func UnaryInterceptor(token string) grpc.UnaryServerInterceptor {
	if token == "" {
		return nil
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing auth token")
		}
		got := strings.Join(md.Get(MetadataKey), "")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid auth token")
		}
		return handler(ctx, req)
	}
}
