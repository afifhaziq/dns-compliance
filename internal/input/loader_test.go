package input_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/afif/dns-tracking/internal/input"
)

func TestLoadFromArgs(t *testing.T) {
	urls, err := input.Load("", []string{"https://example.com", "https://foo.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("want 2 urls, got %d", len(urls))
	}
}

func TestLoadFromFile(t *testing.T) {
	f, err := os.CreateTemp("", "sites-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("https://a.com\nhttps://b.com\n")
	f.Close()

	urls, err := input.Load(f.Name(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("want 2 urls, got %d", len(urls))
	}
}

func TestDeduplication(t *testing.T) {
	urls, err := input.Load("", []string{"https://example.com", "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 {
		t.Fatalf("want 1 url after dedup, got %d", len(urls))
	}
}

func TestBothSourcesCombined(t *testing.T) {
	f, err := os.CreateTemp("", "sites-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("https://file-only.com\n")
	f.Close()

	urls, err := input.Load(f.Name(), []string{"https://arg-only.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 {
		t.Fatalf("want 2 urls, got %d", len(urls))
	}
}

func TestSkipsCommentsAndBlankLines(t *testing.T) {
	f, err := os.CreateTemp("", "sites-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("# comment\nhttps://example.com\n\nhttps://foo.com\n")
	f.Close()

	urls, err := input.Load(f.Name(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 {
		t.Fatalf("want 2 urls, got %d", len(urls))
	}
}

func TestFileNotFound(t *testing.T) {
	_, err := input.Load(filepath.Join(t.TempDir(), "nonexistent.txt"), nil)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
