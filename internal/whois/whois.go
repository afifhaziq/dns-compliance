// Package whois fetches per-domain registration metadata (registrar,
// creation/expiry dates) via RDAP. Fails soft — network/RDAP errors are
// returned to the caller, never panics; callers are expected to record the
// error and move on rather than block on a slow/unreachable RDAP server.
package whois

import (
	"context"
	"fmt"
	"time"

	"github.com/openrdap/rdap"
)

// Result is the per-domain RDAP data worth caching.
type Result struct {
	Registrar     string
	DomainCreated *time.Time
	DomainExpires *time.Time
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
			if role == "registrar" && entity.VCard != nil {
				res.Registrar = entity.VCard.Name()
			}
		}
	}
	return res
}
