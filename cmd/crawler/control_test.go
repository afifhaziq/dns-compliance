package main

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthInterceptorRejectsMissingToken(t *testing.T) {
	interceptor := authInterceptor("secret")
	ctx := context.Background() // no metadata attached

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	})

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

func TestAuthInterceptorRejectsWrongToken(t *testing.T) {
	interceptor := authInterceptor("secret")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-auth-token", "wrong"))

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	})

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

func TestAuthInterceptorAcceptsCorrectToken(t *testing.T) {
	interceptor := authInterceptor("secret")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-auth-token", "secret"))

	called := false
	resp, err := interceptor(ctx, "req", &grpc.UnaryServerInfo{}, func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
	if resp != "ok" {
		t.Fatalf("unexpected response: %v", resp)
	}
}
