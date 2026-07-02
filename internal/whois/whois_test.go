package whois

import (
	"testing"

	"github.com/openrdap/rdap"
)

// TestParseDomain is the one runnable check for parseDomain's event/entity
// mapping — the only non-trivial logic in this package (Fetch itself is a
// thin RDAP client call).
func TestParseDomain(t *testing.T) {
	vcard, err := rdap.NewVCard([]byte(`["vcard",[["version",{},"text","4.0"],["fn",{},"text","Example Registrar Inc."]]]`))
	if err != nil {
		t.Fatalf("NewVCard: %v", err)
	}

	d := &rdap.Domain{
		Events: []rdap.Event{
			{Action: "registration", Date: "2001-01-01T00:00:00Z"},
			{Action: "expiration", Date: "2030-01-01T00:00:00Z"},
			{Action: "last changed", Date: "2020-01-01T00:00:00Z"}, // ignored
			{Action: "expiration", Date: "not-a-date"},             // unparsable, ignored
		},
		Entities: []rdap.Entity{
			{Roles: []string{"administrative"}},
			{Roles: []string{"registrar"}, VCard: vcard},
		},
	}

	res := parseDomain(d)

	if res.Registrar != "Example Registrar Inc." {
		t.Errorf("Registrar = %q, want %q", res.Registrar, "Example Registrar Inc.")
	}
	if res.DomainCreated == nil || res.DomainCreated.Year() != 2001 {
		t.Errorf("DomainCreated = %v, want 2001", res.DomainCreated)
	}
	if res.DomainExpires == nil || res.DomainExpires.Year() != 2030 {
		t.Errorf("DomainExpires = %v, want 2030", res.DomainExpires)
	}
}
