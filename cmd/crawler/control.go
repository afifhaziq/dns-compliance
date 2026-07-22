package main

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const authMetadataKey = "x-auth-token"

// authInterceptor rejects any RPC whose x-auth-token metadata doesn't match
// token, using a constant-time comparison — this is a real credential
// check, not a lookup, so timing must not leak partial matches.
func authInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing auth token")
		}
		got := strings.Join(md.Get(authMetadataKey), "")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid auth token")
		}
		return handler(ctx, req)
	}
}
