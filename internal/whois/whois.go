// Package whois fetches per-domain registration metadata (registrar,
// creation/expiry dates) via RDAP. Fails soft — network/RDAP errors are
// returned to the caller, never panics; callers are expected to record the
// error and move on rather than block on a slow/unreachable RDAP server.
package whois

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openrdap/rdap"
)

// Result is the per-domain RDAP data worth caching.
type Result struct {
	Registrar           string
	RegistrarURL        string
	RegistrarAbuseEmail string
	RegistrarAbusePhone string
	DomainCreated       *time.Time
	DomainExpires       *time.Time
}

// Fetcher looks up RDAP data for domain. Exposed as a var type so tests can
// inject a fake instead of hitting the network.
type Fetcher func(ctx context.Context, domain string) (Result, error)

// Fetch is the default Fetcher, backed by RDAP (with bootstrap discovery of
// the right registry server for domain).
func Fetch(ctx context.Context, domain string) (Result, error) {
	client := &rdap.Client{}
	req := (&rdap.Request{
		Type:  rdap.DomainRequest,
		Query: domain,
	}).WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	d, ok := resp.Object.(*rdap.Domain)
	if !ok {
		return Result{}, fmt.Errorf("whois: unexpected RDAP response for %s", domain)
	}
	return parseDomain(d), nil
}

// parseDomain maps RDAP's generic event/entity lists onto the fields we
// care about. Mirrors rdap.Response.ToWhoisStyleResponse's field mapping
// (registration/expiration events, "registrar" role entity).
func parseDomain(d *rdap.Domain) Result {
	var res Result
	for _, e := range d.Events {
		t, err := time.Parse(time.RFC3339, e.Date)
		if err != nil {
			continue
		}
		switch e.Action {
		case "registration":
			res.DomainCreated = &t
		case "expiration":
			res.DomainExpires = &t
		}
	}
	for _, entity := range d.Entities {
		for _, role := range entity.Roles {
			if role != "registrar" {
				continue
			}
			if entity.VCard != nil {
				res.Registrar = entity.VCard.Name()
			}
			for _, link := range entity.Links {
				if link.Rel == "about" {
					res.RegistrarURL = link.Href
					break
				}
			}
			for _, sub := range entity.Entities {
				for _, subRole := range sub.Roles {
					if subRole == "abuse" && sub.VCard != nil {
						res.RegistrarAbuseEmail = strings.TrimPrefix(sub.VCard.Email(), "mailto:")
						res.RegistrarAbusePhone = strings.TrimPrefix(sub.VCard.Tel(), "tel:")
					}
				}
			}
		}
	}
	return res
}

// IPResult is the per-IP RDAP data worth caching.
type IPResult struct {
	NetName    string
	AbuseEmail string
}

// IPFetcher looks up RDAP data for an IP's allocation block. Exposed as a
// var type so tests can inject a fake instead of hitting the network.
type IPFetcher func(ctx context.Context, ip string) (IPResult, error)

// FetchIP is the default IPFetcher, backed by an RDAP IP-network lookup
// (bootstraps to the correct RIR — ARIN/RIPE/APNIC/etc — same mechanism as
// Fetch's domain bootstrap).
func FetchIP(ctx context.Context, ip string) (IPResult, error) {
	client := &rdap.Client{}
	req := (&rdap.Request{
		Type:  rdap.IPRequest,
		Query: ip,
	}).WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return IPResult{}, err
	}
	n, ok := resp.Object.(*rdap.IPNetwork)
	if !ok {
		return IPResult{}, fmt.Errorf("whois: unexpected RDAP response for %s", ip)
	}
	return IPResult{NetName: n.Name, AbuseEmail: parseIPAbuseEmail(n.Entities)}, nil
}

// parseIPAbuseEmail looks for a role="abuse" entity in an IP network's
// entity list — unlike domain RDAP (where abuse sits nested under the
// registrar entity, see parseDomain), IP-network abuse contacts are
// typically either a top-level entity or one level of nesting down, so both
// are checked here.
func parseIPAbuseEmail(entities []rdap.Entity) string {
	for _, entity := range entities {
		for _, role := range entity.Roles {
			if role == "abuse" && entity.VCard != nil {
				return strings.TrimPrefix(entity.VCard.Email(), "mailto:")
			}
		}
		if email := parseIPAbuseEmail(entity.Entities); email != "" {
			return email
		}
	}
	return ""
}
