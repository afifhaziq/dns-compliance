package favicon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAcceptsImageResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Write([]byte{0x00, 0x01, 0x02})
	}))
	defer srv.Close()

	res, err := get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("get() error = %v, want nil", err)
	}
	if res.ContentType != "image/x-icon" {
		t.Errorf("ContentType = %q, want image/x-icon", res.ContentType)
	}
	if len(res.Data) != 3 {
		t.Errorf("Data length = %d, want 3", len(res.Data))
	}
}

func TestGetRejectsNonImageResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>not found</html>"))
	}))
	defer srv.Close()

	if _, err := get(context.Background(), srv.URL); err == nil {
		t.Fatal("get() error = nil, want error for non-image content type")
	}
}

func TestGetRejectsEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
	}))
	defer srv.Close()

	if _, err := get(context.Background(), srv.URL); err == nil {
		t.Fatal("get() error = nil, want error for empty body")
	}
}
