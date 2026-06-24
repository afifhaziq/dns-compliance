package urlnorm_test

import (
	"testing"

	"github.com/afif/dns-tracking/internal/urlnorm"
)

func TestNormalize_VariousFormats(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://Example.com/", "example.com"},
		{"example.com", "example.com"},
		{"EXAMPLE.COM", "example.com"},
		{"http://example.com:8080/path?q=1", "example.com"},
		{"  example.com  ", "example.com"},
		{"example.com.", "example.com"},
		{"https://user:pass@example.com/", "example.com"},
	}
	for _, c := range cases {
		got, err := urlnorm.Normalize(c.in)
		if err != nil {
			t.Fatalf("Normalize(%q): unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalize_RejectsEmpty(t *testing.T) {
	cases := []string{"", "   ", "https://"}
	for _, in := range cases {
		if _, err := urlnorm.Normalize(in); err == nil {
			t.Errorf("Normalize(%q): expected error, got none", in)
		}
	}
}
