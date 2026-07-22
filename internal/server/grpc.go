package server

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/ipinfo"
	"github.com/afif/dns-tracking/internal/storage"
	"github.com/afif/dns-tracking/internal/urlnorm"
	"github.com/afif/dns-tracking/internal/whois"
	pb "github.com/afif/dns-tracking/proto"
)

type grpcServer struct {
	pb.UnimplementedComplianceServiceServer
	store        db.Store
	storage      storage.Storage
	broadcaster  *Broadcaster
	ipFetch      ipinfo.Fetcher  // nil disables ASN/org lookups
	netnameFetch whois.IPFetcher // nil disables NetName lookups
}

func NewGRPCServer(store db.Store, stor storage.Storage, broadcaster *Broadcaster, ipFetch ipinfo.Fetcher, netnameFetch whois.IPFetcher) pb.ComplianceServiceServer {
	return &grpcServer{store: store, storage: stor, broadcaster: broadcaster, ipFetch: ipFetch, netnameFetch: netnameFetch}
}

func (s *grpcServer) Submit(ctx context.Context, report *pb.ComplianceReport) (*pb.Acknowledgement, error) {
	run, err := s.store.ActiveScanRun(ctx)
	if err != nil {
		log.Printf("grpc: fetch active scan run: %v", err)
	}
	var runID uint
	if run != nil {
		runID = run.ID
	}

	servers, _ := s.store.ListDNSServers(ctx)
	serverByName := make(map[string]uint, len(servers))
	for _, srv := range servers {
		serverByName[srv.Name] = srv.ID
	}

	urls, _ := s.store.ListWatchedURLs(ctx)
	urlIDByValue := make(map[string]uint, len(urls))
	for _, u := range urls {
		urlIDByValue[u.URL] = u.ID
	}

	for _, r := range report.Results {
		// urls.url is stored normalized; r.Url comes from the crawler as
		// whatever raw string was fed into it, so normalize defensively
		// before the lookup. URLValue itself stays raw — it's just for
		// display.
		urlID := urlIDByValue[r.Url]
		if urlID == 0 {
			if norm, err := urlnorm.Normalize(r.Url); err == nil {
				urlID = urlIDByValue[norm]
			}
		}

		lookupIP := r.ResolvedIp
		if lookupIP == "" {
			lookupIP = r.ResolvedIpv6
		}
		var asn uint
		var org, netname, abuseEmail string
		if lookupIP != "" {
			if cached, _ := s.store.GetIPInfo(ctx, lookupIP); cached != nil {
				asn, org, netname, abuseEmail = cached.ASN, cached.Org, cached.NetName, cached.AbuseEmail
			} else if s.ipFetch != nil {
				// Cache miss — fetch is detached from this request so a
				// slow/unreachable ipinfo.io/RDAP never delays result
				// ingestion. This scan's row is inserted with a blank
				// ASN/org/netname; the cache fill only benefits later scans
				// of the same IP.
				go fetchAndCacheIPInfo(s.store, s.ipFetch, s.netnameFetch, lookupIP)
			}
		}

		result := db.ScanResult{
			ScanRunID:          runID,
			URLID:              urlID,
			URLValue:           r.Url,
			DNSServerID:        serverByName[r.DnsServer],
			Compliant:          r.Compliant,
			ResolvedIP:         r.ResolvedIp,
			ResolvedIPv6:       r.ResolvedIpv6,
			ResolvedASN:        asn,
			ResolvedOrg:        org,
			ResolvedNetName:    netname,
			ResolvedAbuseEmail: abuseEmail,
			Error:              r.Error,
			LatencyMs:          r.GetLatencyMs(),
			ScannedAt:          time.Unix(r.Timestamp, 0),
		}

		if err := s.store.InsertResult(ctx, result); err != nil {
			log.Printf("grpc: insert result for %s: %v", r.Url, err)
			continue
		}

		if len(r.Screenshot) > 0 && s.storage != nil {
			screenshotURL, err := s.storage.Upload(ctx, r.Screenshot)
			if err != nil {
				log.Printf("grpc: upload screenshot for %s: %v", r.Url, err)
				continue
			}
			// Find the just-inserted result to update its screenshot URL.
			// time.Time{} = no lower bound; we just need the row inserted above.
			results, err := s.store.ResultsByURL(ctx, r.Url, time.Time{}, time.Time{})
			if err == nil && len(results) > 0 {
				_ = s.store.UpdateScreenshot(ctx, results[0].ID, screenshotURL)
			}
			// The row above belongs to *this* request's scan run, which is a
			// "screenshot" run — LastScanRun (and therefore LatestResults,
			// what the Results page actually renders) deliberately skips
			// those, so that row is otherwise invisible until the next real
			// sweep. Stamp the same URL directly onto the row LatestResults
			// currently shows for this DNS server too, or the button spins
			// and no picture ever appears there.
			if lastRun, err := s.store.LastScanRun(ctx); err == nil && lastRun != nil {
				for _, res := range results {
					if res.DNSServerID == serverByName[r.DnsServer] && res.ScanRunID == lastRun.ID {
						_ = s.store.UpdateScreenshot(ctx, res.ID, screenshotURL)
						break
					}
				}
			}
		}
	}

	if s.broadcaster != nil {
		if data, err := buildProgressPayload(ctx, s.store); err == nil && data != nil {
			s.broadcaster.Publish(data)
		}
	}

	return &pb.Acknowledgement{Ok: true}, nil
}

// fetchAndCacheIPInfo runs an ipinfo.io lookup and (if netnameFetch is set) an
// RDAP IP-network lookup, merging both into a single db.IPInfo before caching
// it keyed by ip — UpsertIPInfo overwrites the whole row on conflict, so two
// separate upserts here would let whichever finishes last wipe the other's
// fields. Sequential, not parallel: this already runs in a detached goroutine
// per cache-miss IP in Submit, so the extra worst-case latency doesn't block
// ingestion there; RefreshHostingInfo calls it synchronously instead, since
// that path needs the fresh result in the response.
func fetchAndCacheIPInfo(store db.EnrichmentStore, fetch ipinfo.Fetcher, netnameFetch whois.IPFetcher, ip string) db.IPInfo {
	fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	res, err := fetch(fetchCtx, ip)
	cancel()

	info := db.IPInfo{IP: ip, FetchedAt: time.Now()}
	var errs []string
	if err != nil {
		errs = append(errs, err.Error())
	} else {
		info.ASN, info.Org = res.ASN, res.Org
	}

	if netnameFetch != nil {
		nCtx, nCancel := context.WithTimeout(context.Background(), 10*time.Second)
		nRes, nErr := netnameFetch(nCtx, ip)
		nCancel()
		if nErr != nil {
			errs = append(errs, nErr.Error())
		} else {
			info.NetName = nRes.NetName
			info.AbuseEmail = nRes.AbuseEmail
		}
	}
	if len(errs) > 0 {
		info.FetchError = strings.Join(errs, "; ")
	}

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dbCancel()
	if err := store.UpsertIPInfo(dbCtx, info); err != nil {
		log.Printf("ipinfo: upsert for %s: %v", ip, err)
	}
	return info
}
