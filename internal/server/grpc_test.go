package server_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/server"
	"github.com/afif/dns-tracking/internal/storage"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// mockStore implements db.Store for gRPC tests.
type mockStore struct {
	db.Store
	insertedResults []db.ScanResult
	activeScanRun   *db.ScanRun
	dnsServers      []db.DNSServer
}

func (m *mockStore) InsertResult(_ context.Context, r db.ScanResult) error {
	m.insertedResults = append(m.insertedResults, r)
	return nil
}
func (m *mockStore) ActiveScanRun(_ context.Context) (*db.ScanRun, error) {
	return m.activeScanRun, nil
}
func (m *mockStore) ListDNSServers(_ context.Context) ([]db.DNSServer, error) {
	return m.dnsServers, nil
}
func (m *mockStore) ListURLs(_ context.Context) ([]db.URL, error) {
	return nil, nil
}
func (m *mockStore) ResultsByURL(_ context.Context, _ string, _ time.Time) ([]db.ScanResult, error) {
	return m.insertedResults, nil
}
func (m *mockStore) UpdateScreenshot(_ context.Context, _ uint, _ string) error { return nil }

// mockStorage satisfies storage.Storage.
type mockStorage struct{ uploadedCount int }

func (m *mockStorage) Upload(_ context.Context, _ []byte) (string, error) {
	m.uploadedCount++
	return "http://minio/screenshots/test.png", nil
}

var _ storage.Storage = (*mockStorage)(nil)

func newTestGRPCClient(t *testing.T, store db.Store, stor storage.Storage) pb.ComplianceServiceClient {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	grpcSrv := grpc.NewServer()
	pb.RegisterComplianceServiceServer(grpcSrv, server.NewGRPCServer(store, stor, nil))
	go grpcSrv.Serve(lis) //nolint:errcheck
	t.Cleanup(grpcSrv.Stop)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewComplianceServiceClient(conn)
}

func TestSubmitStoresResults(t *testing.T) {
	store := &mockStore{activeScanRun: &db.ScanRun{ID: 1, Status: "running"}}
	client := newTestGRPCClient(t, store, &mockStorage{})

	_, err := client.Submit(context.Background(), &pb.ComplianceReport{
		Results: []*pb.SiteResult{
			{
				Url:       "https://example.com",
				Compliant: false,
				ResolvedIp: "1.2.3.4",
				DnsServer: "Google",
				Timestamp: time.Now().Unix(),
			},
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(store.insertedResults) != 1 {
		t.Fatalf("expected 1 inserted result, got %d", len(store.insertedResults))
	}
	if store.insertedResults[0].URLValue != "https://example.com" {
		t.Fatalf("unexpected URL: %s", store.insertedResults[0].URLValue)
	}
}

func TestSubmitUploadsScreenshot(t *testing.T) {
	store := &mockStore{activeScanRun: &db.ScanRun{ID: 1, Status: "running"}}
	stor := &mockStorage{}
	client := newTestGRPCClient(t, store, stor)

	_, err := client.Submit(context.Background(), &pb.ComplianceReport{
		Results: []*pb.SiteResult{
			{
				Url:        "https://example.com",
				Screenshot: []byte("fake-png"),
				Timestamp:  time.Now().Unix(),
			},
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if stor.uploadedCount != 1 {
		t.Fatalf("expected 1 upload, got %d", stor.uploadedCount)
	}
}

func TestSubmitNoActiveRun(t *testing.T) {
	store := &mockStore{} // no active scan run
	client := newTestGRPCClient(t, store, &mockStorage{})

	_, err := client.Submit(context.Background(), &pb.ComplianceReport{
		Results: []*pb.SiteResult{
			{Url: "https://example.com", Timestamp: time.Now().Unix()},
		},
	})
	if err != nil {
		t.Fatalf("Submit with no active run should succeed: %v", err)
	}
	if len(store.insertedResults) != 1 {
		t.Fatalf("expected result inserted even without active run")
	}
}
