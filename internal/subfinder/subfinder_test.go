package subfinder

import (
	"reflect"
	"testing"
)

func TestParseLines(t *testing.T) {
	cases := map[string]struct {
		in   string
		want []string
	}{
		"basic":               {"www.example.com\napi.example.com\n", []string{"www.example.com", "api.example.com"}},
		"blank lines dropped": {"www.example.com\n\n\napi.example.com\n", []string{"www.example.com", "api.example.com"}},
		"whitespace trimmed":  {"  www.example.com  \n", []string{"www.example.com"}},
		"empty output":        {"", nil},
		"only whitespace":     {"\n \n", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseLines([]byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseLines(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}
