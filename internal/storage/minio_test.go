package storage_test

import (
	"context"
	"testing"

	"github.com/afif/dns-tracking/internal/storage"
)

type mockStorage struct {
	uploaded [][]byte
}

func (m *mockStorage) Upload(_ context.Context, data []byte) (string, error) {
	m.uploaded = append(m.uploaded, data)
	return "http://minio:9000/screenshots/test.png", nil
}

// Compile-time check that mockStorage satisfies the interface.
var _ storage.Storage = (*mockStorage)(nil)

func TestMockStorageUpload(t *testing.T) {
	s := &mockStorage{}
	url, err := s.Upload(context.Background(), []byte("fake-png"))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
	if len(s.uploaded) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(s.uploaded))
	}
}
