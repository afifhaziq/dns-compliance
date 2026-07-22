package db

import (
	"context"
	"time"
)

// URLStore covers the global/admin URL catalog plus department watchlists —
// CreateURL normalizes + get-or-creates, so the same domain added by
// different departments always resolves to one shared row (see
// AddURLToWatchlist).
type URLStore interface {
	ListURLs(ctx context.Context) ([]URL, error)
	CreateURL(ctx context.Context, rawURL string) (URL, error)
	DeleteURL(ctx context.Context, id uint) error                     // admin-only hard purge; cascades to ScanResult
	GetURLByValue(ctx context.Context, urlValue string) (*URL, error) // nil, nil if urlValue is unknown

	ListDepartmentURLs(ctx context.Context, departmentID uint) ([]URLEntry, error)
	AddURLToWatchlist(ctx context.Context, departmentID uint, rawURL string) (URL, error)
	RemoveURLFromWatchlist(ctx context.Context, departmentID, urlID uint) (bool, error)                // false if no row was deleted (not on that watchlist)
	SetURLEnabled(ctx context.Context, departmentID, urlID uint, enabled bool) (bool, error)           // false if the URL is not on that watchlist
	SetURLOrderedAt(ctx context.Context, departmentID, urlID uint, orderedAt *time.Time) (bool, error) // nil clears the order date; false if the URL is not on that watchlist
	ListWatchedURLs(ctx context.Context) ([]URL, error)                                                // urls with >=1 enabled DepartmentURL row — used by the scan sweep
	ListUnassignedURLs(ctx context.Context) ([]URL, error)                                             // admin view: urls with 0 DepartmentURL rows
	URLOwnedByDepartment(ctx context.Context, departmentID uint, urlValue string) (bool, error)

	// Watchlist activity — counts DepartmentURL rows (watchlist "requests")
	// created since a given time; used for the "requested this month" stat.
	CountDepartmentURLsSince(ctx context.Context, since time.Time) (int, error)
	CountDepartmentURLsSinceForDepartment(ctx context.Context, since time.Time, departmentID uint) (int, error)
}

// DNSServerStore is the shared/global DNS server catalog — not
// department-scoped, results reference servers by name.
type DNSServerStore interface {
	ListDNSServers(ctx context.Context) ([]DNSServer, error)
	CreateDNSServer(ctx context.Context, s DNSServer) (DNSServer, error)
	DeleteDNSServer(ctx context.Context, id uint) error
}

// ScanRunStore tracks the lifecycle of a scan sweep (one StartSweep call to
// the crawler's control service), independent of the ScanResult rows it produces.
type ScanRunStore interface {
	CreateScanRun(ctx context.Context, triggeredBy string) (ScanRun, error)
	CompleteScanRun(ctx context.Context, id uint, status string, completedAt time.Time) error
	ActiveScanRun(ctx context.Context) (*ScanRun, error)
	LastScanRun(ctx context.Context) (*ScanRun, error)
	ScanProgress(ctx context.Context, runID uint) ([]ProgressEntry, error)
}

// ResultStore ingests and queries per-scan compliance results.
type ResultStore interface {
	LatestResults(ctx context.Context) ([]ScanResult, error)
	LatestResultsForDepartment(ctx context.Context, departmentID uint) ([]ScanResult, error)
	ResultsByURL(ctx context.Context, urlValue string, since, until time.Time) ([]ScanResult, error)
	DailyComplianceByURL(ctx context.Context, urlValue string, since, until time.Time) ([]DailyComplianceStat, error)
	InsertResult(ctx context.Context, r ScanResult) error
	UpdateScreenshot(ctx context.Context, resultID uint, screenshotURL string) error
}

// ISPStatsStore aggregates ScanResult rows into per-ISP compliance, trend,
// and time-to-compliance stats.
type ISPStatsStore interface {
	ISPStats(ctx context.Context, isp string) (ISPStatsResult, error)
	ISPStatsForDepartment(ctx context.Context, isp string, departmentID uint) (ISPStatsResult, error)
	ISPTrend(ctx context.Context, isp string, since, until time.Time) ([]ISPTrendStat, error)
	ISPTrendForDepartment(ctx context.Context, isp string, since, until time.Time, departmentID uint) ([]ISPTrendStat, error)
	ISPComplianceTiming(ctx context.Context, isp string) (ISPTimingResult, error)
	ISPComplianceTimingForDepartment(ctx context.Context, isp string, departmentID uint) (ISPTimingResult, error)
	NationalTrend(ctx context.Context, since, until time.Time) ([]ISPTrendStat, error)
	NationalTrendForDepartment(ctx context.Context, since, until time.Time, departmentID uint) ([]ISPTrendStat, error)
	ResurfacedDomains(ctx context.Context) ([]ResurfacedDomain, error)
	ResurfacedDomainsForDepartment(ctx context.Context, departmentID uint) ([]ResurfacedDomain, error)
}

// DepartmentStore is the departments table — admin-only by nature (cross-
// department), seeded once by db.SeedDepartments.
type DepartmentStore interface {
	ListDepartments(ctx context.Context) ([]Department, error)
	CreateDepartment(ctx context.Context, name string) (Department, error)
}

// UserStore covers user accounts (admin, department-admin, and plain
// department-member roles).
type UserStore interface {
	ListUsers(ctx context.Context) ([]User, error)
	CreateUser(ctx context.Context, u User) (User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetUserByID(ctx context.Context, id uint) (*User, error)
	DeleteUser(ctx context.Context, id uint) error
}

// SessionStore backs the session-cookie auth flow.
type SessionStore interface {
	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, token string) (*Session, error)
	DeleteSession(ctx context.Context, token string) error
}

// CompliantIPStore is the admin-managed list of IPs treated as compliant
// even when DNS resolves (e.g. an ISP's block-page IP).
type CompliantIPStore interface {
	ListCompliantIPs(ctx context.Context) ([]CompliantIP, error)
	CreateCompliantIP(ctx context.Context, address, note string) (CompliantIP, error)
	DeleteCompliantIP(ctx context.Context, id uint) error
}

// ScanSettingsStore holds the single admin-configurable scan cadence row.
type ScanSettingsStore interface {
	GetScanInterval(ctx context.Context) (int, error)
	SetScanInterval(ctx context.Context, minutes int) error
}

// EnrichmentStore covers the fetch-once (or fetch-rarely) caches keyed by
// domain or IP rather than by scan run: WHOIS/RDAP, ASN/NetName IP info,
// favicons, and subfinder subdomain enumeration. None of these affect the
// compliance verdict — informational only.
type EnrichmentStore interface {
	UpsertDomainWhois(ctx context.Context, w DomainWhois) error
	GetDomainWhois(ctx context.Context, urlValue string) (*DomainWhois, error)           // nil, nil if never fetched (or urlValue is unknown)
	ListStaleDomains(ctx context.Context, olderThan time.Time, limit int) ([]URL, error) // watched URLs with no DomainWhois row or LastFetchedAt < olderThan

	GetIPInfo(ctx context.Context, ip string) (*IPInfo, error) // nil, nil if never fetched
	UpsertIPInfo(ctx context.Context, info IPInfo) error

	GetFavicon(ctx context.Context, domain string) (*Favicon, error) // nil, nil if never fetched
	UpsertFavicon(ctx context.Context, fav Favicon) error

	GetSubdomainScan(ctx context.Context, urlValue string) (*SubdomainScan, error) // nil, nil if never fetched (or urlValue is unknown)
	UpsertSubdomainScan(ctx context.Context, s SubdomainScan) error
}

// Store is the full persistence port — the union of every aggregate-scoped
// store above. Multi-aggregate consumers (Handlers, Scanner) depend on this.
// A consumer that only ever touches one aggregate should depend on that
// sub-interface directly instead (see StartWhoisRefresher's db.EnrichmentStore,
// StartScheduler's db.ScanSettingsStore) — it documents the dependency at a
// glance and narrows what a test double for it needs to implement.
type Store interface {
	URLStore
	DNSServerStore
	ScanRunStore
	ResultStore
	ISPStatsStore
	DepartmentStore
	UserStore
	SessionStore
	CompliantIPStore
	ScanSettingsStore
	EnrichmentStore
}
