package sender_test

import (
	"context"
	"net"
	"testing"
	"time"

	pb "github.com/afif/dns-tracking/proto"
	"github.com/afif/dns-tracking/internal/sender"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

	if err := sender.Send(context.Background(), conn, report); err != nil {
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
