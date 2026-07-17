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
	abuseVCard, err := rdap.NewVCard([]byte(`["vcard",[["version",{},"text","4.0"],["email",{},"text","abuse@registrar.example"],["tel",{},"text","+1.5551234567"]]]`))
	if err != nil {
		t.Fatalf("NewVCard (abuse): %v", err)
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
			{
				Roles: []string{"registrar"},
				VCard: vcard,
				Links: []rdap.Link{{Rel: "about", Href: "https://registrar.example/"}},
				Entities: []rdap.Entity{
					{Roles: []string{"abuse"}, VCard: abuseVCard},
				},
			},
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
	if res.RegistrarURL != "https://registrar.example/" {
		t.Errorf("RegistrarURL = %q, want %q", res.RegistrarURL, "https://registrar.example/")
	}
	if res.RegistrarAbuseEmail != "abuse@registrar.example" {
		t.Errorf("RegistrarAbuseEmail = %q, want %q", res.RegistrarAbuseEmail, "abuse@registrar.example")
	}
	if res.RegistrarAbusePhone != "+1.5551234567" {
		t.Errorf("RegistrarAbusePhone = %q, want %q", res.RegistrarAbusePhone, "+1.5551234567")
	}
}

// TestParseDomainStripsURISchemes confirms the tel:/mailto: scheme prefix is
// stripped — real RDAP registrars (observed live against a .lu domain) encode
// these as "uri"-typed jCard values rather than the plain "text" type the
// spec's example uses, so parseDomain must handle both.
func TestParseDomainStripsURISchemes(t *testing.T) {
	abuseVCard, err := rdap.NewVCard([]byte(`["vcard",[["version",{},"text","4.0"],["email",{},"uri","mailto:abuse@registrar.example"],["tel",{},"uri","tel:+352.27220150"]]]`))
	if err != nil {
		t.Fatalf("NewVCard (abuse): %v", err)
	}

	d := &rdap.Domain{
		Entities: []rdap.Entity{
			{
				Roles: []string{"registrar"},
				Entities: []rdap.Entity{
					{Roles: []string{"abuse"}, VCard: abuseVCard},
				},
			},
		},
	}

	res := parseDomain(d)

	if res.RegistrarAbuseEmail != "abuse@registrar.example" {
		t.Errorf("RegistrarAbuseEmail = %q, want %q", res.RegistrarAbuseEmail, "abuse@registrar.example")
	}
	if res.RegistrarAbusePhone != "+352.27220150" {
		t.Errorf("RegistrarAbusePhone = %q, want %q", res.RegistrarAbusePhone, "+352.27220150")
	}
}

// TestParseDomainNoAbuseOrLinks confirms the 3 new fields stay empty when
// the registrar entity has no links/nested abuse entity — the common case
// for registries that don't publish this data via RDAP.
func TestParseDomainNoAbuseOrLinks(t *testing.T) {
	vcard, err := rdap.NewVCard([]byte(`["vcard",[["version",{},"text","4.0"],["fn",{},"text","Example Registrar Inc."]]]`))
	if err != nil {
		t.Fatalf("NewVCard: %v", err)
	}

	d := &rdap.Domain{
		Entities: []rdap.Entity{
			{Roles: []string{"registrar"}, VCard: vcard},
		},
	}

	res := parseDomain(d)

	if res.RegistrarURL != "" {
		t.Errorf("RegistrarURL = %q, want empty", res.RegistrarURL)
	}
	if res.RegistrarAbuseEmail != "" {
		t.Errorf("RegistrarAbuseEmail = %q, want empty", res.RegistrarAbuseEmail)
	}
	if res.RegistrarAbusePhone != "" {
		t.Errorf("RegistrarAbusePhone = %q, want empty", res.RegistrarAbusePhone)
	}
}

// TestParseIPAbuseEmail covers the two shapes RIRs actually use for an IP
// network's abuse contact — a top-level role="abuse" entity, and one nested
// a level down — plus the no-match case.
func TestParseIPAbuseEmail(t *testing.T) {
	abuseVCard, err := rdap.NewVCard([]byte(`["vcard",[["version",{},"text","4.0"],["email",{},"text","abuse@netops.example"]]]`))
	if err != nil {
		t.Fatalf("NewVCard: %v", err)
	}

	t.Run("top-level abuse entity", func(t *testing.T) {
		entities := []rdap.Entity{
			{Roles: []string{"administrative"}},
			{Roles: []string{"abuse"}, VCard: abuseVCard},
		}
		if got := parseIPAbuseEmail(entities); got != "abuse@netops.example" {
			t.Errorf("parseIPAbuseEmail = %q, want %q", got, "abuse@netops.example")
		}
	})

	t.Run("nested abuse entity", func(t *testing.T) {
		entities := []rdap.Entity{
			{
				Roles:    []string{"registrant"},
				Entities: []rdap.Entity{{Roles: []string{"abuse"}, VCard: abuseVCard}},
			},
		}
		if got := parseIPAbuseEmail(entities); got != "abuse@netops.example" {
			t.Errorf("parseIPAbuseEmail = %q, want %q", got, "abuse@netops.example")
		}
	})

	t.Run("no abuse entity", func(t *testing.T) {
		entities := []rdap.Entity{{Roles: []string{"administrative"}}}
		if got := parseIPAbuseEmail(entities); got != "" {
			t.Errorf("parseIPAbuseEmail = %q, want empty", got)
		}
	})
}
