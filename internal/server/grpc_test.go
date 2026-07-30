package server_test

import (
	"context"
	"net"
	"sort"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/server"
	"github.com/afif/dns-tracking/internal/storage"
	"github.com/afif/dns-tracking/internal/urlnorm"
	pb "github.com/afif/dns-tracking/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// mockStore implements db.Store for gRPC tests.
type mockStore struct {
	db.Store
	insertedResults    []db.ScanResult
	activeScanRun      *db.ScanRun
	lastScanRun        *db.ScanRun
	dnsServers         []db.DNSServer
	ipInfo             *db.IPInfo // returned by GetIPInfo, nil = cache miss
	updatedScreenshots map[uint]string
	createdURLs        []db.URL
}

func (m *mockStore) InsertResult(_ context.Context, r db.ScanResult) error {
	r.ID = uint(len(m.insertedResults) + 1)
	m.insertedResults = append(m.insertedResults, r)
	return nil
}
func (m *mockStore) ActiveScanRun(_ context.Context) (*db.ScanRun, error) {
	return m.activeScanRun, nil
}
func (m *mockStore) LastScanRun(_ context.Context) (*db.ScanRun, error) {
	return m.lastScanRun, nil
}
func (m *mockStore) ListDNSServers(_ context.Context) ([]db.DNSServer, error) {
	return m.dnsServers, nil
}
func (m *mockStore) ListURLs(_ context.Context) ([]db.URL, error) {
	return nil, nil
}
func (m *mockStore) ListWatchedURLs(_ context.Context) ([]db.URL, error) {
	return nil, nil
}

// CreateURL mirrors postgresStore's get-or-create-by-normalized-value
// behavior — Submit falls back to this when a result's URL isn't on any
// watched list (see the URLID resolution fallback in grpc.go).
func (m *mockStore) CreateURL(_ context.Context, rawURL string) (db.URL, error) {
	normalized, err := urlnorm.Normalize(rawURL)
	if err != nil {
		return db.URL{}, err
	}
	for _, u := range m.createdURLs {
		if u.URL == normalized {
			return u, nil
		}
	}
	u := db.URL{ID: uint(len(m.createdURLs) + 1), URL: normalized, CreatedAt: time.Now()}
	m.createdURLs = append(m.createdURLs, u)
	return u, nil
}
// ResultsByURL mirrors postgresStore's "scanned_at desc" ordering — Submit
// relies on results[0] being the most-recently-inserted row.
func (m *mockStore) ResultsByURL(_ context.Context, _ string, _, _ time.Time) ([]db.ScanResult, error) {
	out := append([]db.ScanResult(nil), m.insertedResults...)
	sort.Slice(out, func(i, j int) bool { return out[i].ScannedAt.After(out[j].ScannedAt) })
	return out, nil
}
func (m *mockStore) UpdateScreenshot(_ context.Context, id uint, url string) error {
	if m.updatedScreenshots == nil {
		m.updatedScreenshots = make(map[uint]string)
	}
	m.updatedScreenshots[id] = url
	return nil
}
func (m *mockStore) GetIPInfo(_ context.Context, _ string) (*db.IPInfo, error) { return m.ipInfo, nil }
func (m *mockStore) UpsertIPInfo(_ context.Context, _ db.IPInfo) error         { return nil }

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
	pb.RegisterComplianceServiceServer(grpcSrv, server.NewGRPCServer(store, stor, nil, nil, nil))
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
				Url:        "https://example.com",
				Compliant:  false,
				ResolvedIp: "1.2.3.4",
				DnsServer:  "Google",
				Timestamp:  time.Now().Unix(),
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

// A screenshot request runs under its own "screenshot" ScanRun, which
// LastScanRun (and therefore the Results page) deliberately excludes — so
// the freshly-inserted row above is invisible there. Submit must also stamp
// the screenshot URL onto the row LastScanRun currently considers "the
// results", or the button spins with nothing to show for it.
func TestSubmitScreenshotUpdatesVisibleRow(t *testing.T) {
	visibleRow := db.ScanResult{
		ID:          1,
		ScanRunID:   100,
		DNSServerID: 7,
		URLValue:    "https://example.com",
		ScannedAt:   time.Now().Add(-time.Hour),
	}
	store := &mockStore{
		activeScanRun:   &db.ScanRun{ID: 200, Status: "running"},
		lastScanRun:     &db.ScanRun{ID: 100},
		dnsServers:      []db.DNSServer{{ID: 7, Name: "Cloudflare DoT"}},
		insertedResults: []db.ScanResult{visibleRow},
	}
	stor := &mockStorage{}
	client := newTestGRPCClient(t, store, stor)

	_, err := client.Submit(context.Background(), &pb.ComplianceReport{
		Results: []*pb.SiteResult{
			{
				Url:        "https://example.com",
				DnsServer:  "Cloudflare DoT",
				Screenshot: []byte("fake-png"),
				Timestamp:  time.Now().Unix(),
			},
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(store.insertedResults) != 2 {
		t.Fatalf("expected 2 rows (pre-existing + new screenshot row), got %d", len(store.insertedResults))
	}
	newRowID := store.insertedResults[1].ID
	wantURL := "http://minio/screenshots/test.png"
	if got := store.updatedScreenshots[newRowID]; got != wantURL {
		t.Errorf("new screenshot row (id %d): got %q, want %q", newRowID, got, wantURL)
	}
	if got := store.updatedScreenshots[visibleRow.ID]; got != wantURL {
		t.Errorf("visible row (id %d) not updated: got %q, want %q", visibleRow.ID, got, wantURL)
	}
}

func TestSubmitReadsNetNameFromCache(t *testing.T) {
	store := &mockStore{
		activeScanRun: &db.ScanRun{ID: 1, Status: "running"},
		ipInfo:        &db.IPInfo{IP: "1.2.3.4", ASN: 15169, Org: "Google LLC", NetName: "GOOGLE"},
	}
	client := newTestGRPCClient(t, store, &mockStorage{})

	_, err := client.Submit(context.Background(), &pb.ComplianceReport{
		Results: []*pb.SiteResult{
			{
				Url:        "https://example.com",
				Compliant:  false,
				ResolvedIp: "1.2.3.4",
				DnsServer:  "Google",
				Timestamp:  time.Now().Unix(),
			},
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(store.insertedResults) != 1 {
		t.Fatalf("expected 1 inserted result, got %d", len(store.insertedResults))
	}
	if got := store.insertedResults[0].ResolvedNetName; got != "GOOGLE" {
		t.Fatalf("ResolvedNetName = %q, want %q", got, "GOOGLE")
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
