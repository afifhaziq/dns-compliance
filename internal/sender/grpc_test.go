package sender_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	pb "github.com/afif/dns-tracking/proto"
	"github.com/afif/dns-tracking/internal/grpcauth"
	"github.com/afif/dns-tracking/internal/sender"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

type fakeServer struct {
	pb.UnimplementedComplianceServiceServer
	received *pb.ComplianceReport
}

func (s *fakeServer) Submit(_ context.Context, r *pb.ComplianceReport) (*pb.Acknowledgement, error) {
	s.received = r
	return &pb.Acknowledgement{Ok: true}, nil
}

func startFakeServer(t *testing.T) (*fakeServer, *grpc.ClientConn) {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	fake := &fakeServer{}
	pb.RegisterComplianceServiceServer(srv, fake)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop() })

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return fake, conn
}

func TestSendReport(t *testing.T) {
	fake, conn := startFakeServer(t)

	report := &pb.ComplianceReport{
		Results: []*pb.SiteResult{
			{
				Url:        "https://example.com",
				Timestamp:  time.Now().Unix(),
				Compliant:  false,
				ResolvedIp: "1.2.3.4",
			},
		},
	}

	if err := sender.Send(context.Background(), conn, "", report); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if fake.received == nil {
		t.Fatal("server did not receive the report")
	}
	if len(fake.received.Results) != 1 {
		t.Fatalf("want 1 result, got %d", len(fake.received.Results))
	}
	if fake.received.Results[0].Url != "https://example.com" {
		t.Errorf("unexpected URL: %s", fake.received.Results[0].Url)
	}
}

func TestSend_AttachesToken(t *testing.T) {
	var gotToken string
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			gotToken = strings.Join(md.Get(grpcauth.MetadataKey), "")
		}
		return handler(ctx, req)
	}

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer(grpc.UnaryInterceptor(interceptor))
	pb.RegisterComplianceServiceServer(srv, &fakeServer{})
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sender.Send(ctx, conn, "s3cret", &pb.ComplianceReport{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotToken != "s3cret" {
		t.Fatalf("want token s3cret on the wire, got %q", gotToken)
	}
}
