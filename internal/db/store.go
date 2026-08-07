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
	SetURLEnabled(ctx context.Context, departmentID, urlID uint, enabled bool) (bool, error) // false if the URL is not on that watchlist
	// UpdateURLCaseFields writes to the shared URL row (case metadata is
	// global, see URL's doc comment) but only after verifying departmentID
	// actually watches urlID — the write target is no longer department-
	// scoped, so authorization must be checked explicitly instead of
	// falling out of a WHERE clause. Only non-nil fields in `fields` are
	// applied; false if the URL is not on that department's watchlist.
	UpdateURLCaseFields(ctx context.Context, departmentID, urlID uint, fields URLCaseFields) (bool, error)
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
	// ListEnabledDNSServers is what a sweep actually scans against — disabled
	// servers stay in ListDNSServers (so past results/admin UI still show
	// them) but are skipped by future scans.
	ListEnabledDNSServers(ctx context.Context) ([]DNSServer, error)
	CreateDNSServer(ctx context.Context, s DNSServer) (DNSServer, error)
	UpdateDNSServer(ctx context.Context, id uint, s DNSServer) (DNSServer, error)
	DeleteDNSServer(ctx context.Context, id uint) error
	SetDNSServerEnabled(ctx context.Context, id uint, enabled bool) error
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

	// ListDomainSummaries/ForDepartment back GET /api/domains — a paginated,
	// lifetime (not just-latest-run) aggregate per domain, sorted by most
	// recently scanned first. The department variant matches on
	// department_urls.department_id only (no enabled filter), so a domain
	// disabled from the watchlist is still findable by its history. filter is
	// a DomainSummaryFilter{} zero value for no filtering.
	ListDomainSummaries(ctx context.Context, page, pageSize int, filter DomainSummaryFilter) ([]DomainSummary, int, error)
	ListDomainSummariesForDepartment(ctx context.Context, page, pageSize int, departmentID uint, filter DomainSummaryFilter) ([]DomainSummary, int, error)

	// DomainServerSummaries backs the Domain page's expanded-row breakdown —
	// unscoped like ResultsByURL/DailyComplianceByURL; the handler enforces
	// department ownership via requireDomainOwnership before calling it.
	DomainServerSummaries(ctx context.Context, urlValue string) ([]DomainServerSummary, error)
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
	ServerUptime(ctx context.Context, dnsServerID uint, since, until time.Time) ([]ServerUptimeStat, error)
	ServerUptimeForDepartment(ctx context.Context, dnsServerID uint, since, until time.Time, departmentID uint) ([]ServerUptimeStat, error)
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

	// DeleteExpiredSessions removes sessions past their expiry. Returns the
	// number of rows deleted. GetSession already filters these out, so this
	// is purely to stop the table growing without bound.
	DeleteExpiredSessions(ctx context.Context) (int64, error)
}

// CompliantIPStore is the admin-managed list of IPs treated as compliant
// even when DNS resolves (e.g. an ISP's block-page IP).
type CompliantIPStore interface {
	ListCompliantIPs(ctx context.Context) ([]CompliantIP, error)
	CreateCompliantIP(ctx context.Context, address, note string) (CompliantIP, error)
	DeleteCompliantIP(ctx context.Context, id uint) error
}

// AgencyStore is the admin-managed agency lookup table — read open to any
// authenticated role, mutations gated to admin-or-dept-admin (see router.go).
type AgencyStore interface {
	ListAgencies(ctx context.Context) ([]Agency, error)
	CreateAgency(ctx context.Context, name string) (Agency, error)
	DeleteAgency(ctx context.Context, id uint) error
}

// ISPLogoStore is the admin-managed ISP name → logo URL lookup, rendered on
// the Overview page's ISPBentoGrid.
type ISPLogoStore interface {
	ListISPLogos(ctx context.Context) ([]ISPLogo, error)
	UpsertISPLogo(ctx context.Context, isp, logoURL string) (ISPLogo, error)
	DeleteISPLogo(ctx context.Context, isp string) error
}

// GridPreferenceStore holds each user's saved data-grid layout (column
// visibility, sort, page size), keyed by (user, grid). Personal to the
// calling user — no admin gating, ownership is implicit in the userID param.
type GridPreferenceStore interface {
	GetGridPreference(ctx context.Context, userID uint, gridKey string) (*GridPreference, error) // nil, nil if never saved
	SaveGridPreference(ctx context.Context, pref GridPreference) (GridPreference, error)          // upsert by (user_id, grid_key)
}

// ScanSettingsStore holds the single admin-configurable scan cadence row.
type ScanSettingsStore interface {
	GetScanInterval(ctx context.Context) (int, error)
	SetScanInterval(ctx context.Context, minutes int) error
	GetScanEnabled(ctx context.Context) (bool, error)
	SetScanEnabled(ctx context.Context, enabled bool) error
	GetDNSWorkers(ctx context.Context) (int, error)
	SetDNSWorkers(ctx context.Context, workers int) error
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

// LegalCitationStore covers the full Instrument→Citation→Category→Element
// legal-reference catalog plus the URL↔offence join (URLOffence). Kept as
// one sub-interface rather than split per-table: every real consumer walks
// the whole chain together (listing a URL's offences means resolving
// Element→Category→Citation→Instrument to render anything useful), the
// same reasoning EnrichmentStore already uses to bundle unrelated cache
// tables under one interface.
type LegalCitationStore interface {
	ListInstruments(ctx context.Context) ([]Instrument, error)
	// GetOrCreateInstrument finds an existing row by (type, jurisdiction,
	// number, year) or creates one, mirroring CreateURL's
	// get-or-create-by-natural-key pattern so the same law is never
	// duplicated across citations.
	GetOrCreateInstrument(ctx context.Context, in Instrument) (Instrument, error)
	UpdateInstrument(ctx context.Context, id uint, in Instrument) (Instrument, error)
	DeleteInstrument(ctx context.Context, id uint) error // cascades to Citation/Category/Element/URLOffence

	ListCitationsByInstrument(ctx context.Context, instrumentID uint) ([]Citation, error)
	// CreateCitation/UpdateCitation always recompute SortKey server-side
	// from c.Parsed.ProvisionNum/ProvisionSuffix — never trust a
	// client-supplied sort key.
	CreateCitation(ctx context.Context, c Citation) (Citation, error)
	UpdateCitation(ctx context.Context, id uint, c Citation) (Citation, error)
	DeleteCitation(ctx context.Context, id uint) error // cascades to Category/Element/URLOffence

	ListCategoriesByCitation(ctx context.Context, citationID uint) ([]Category, error)
	CreateCategory(ctx context.Context, cat Category) (Category, error)
	UpdateCategory(ctx context.Context, id uint, name string) (Category, error)
	DeleteCategory(ctx context.Context, id uint) error // cascades to Element/URLOffence

	ListElementsByCategory(ctx context.Context, categoryID uint) ([]Element, error)
	CreateElement(ctx context.Context, el Element) (Element, error)
	UpdateElement(ctx context.Context, id uint, name string) (Element, error)
	DeleteElement(ctx context.Context, id uint) error // cascades to SubElement/URLOffence

	ListSubElementsByElement(ctx context.Context, elementID uint) ([]SubElement, error)
	CreateSubElement(ctx context.Context, se SubElement) (SubElement, error)
	UpdateSubElement(ctx context.Context, id uint, name string) (SubElement, error)
	DeleteSubElement(ctx context.Context, id uint) error // cascades to URLOffence

	// ListOffencesByURL preloads Category (and its parent Citation/
	// Instrument) plus Element and SubElement so a listing can render full
	// context in one query. Keyed by urlValue, not urlID, matching the *url
	// wildcard convention used by every other domain-scoped read.
	ListOffencesByURL(ctx context.Context, urlValue string) ([]URLOffence, error)
	// GetOffence preloads URL — used by the detach handler to resolve the
	// owning department before deleting, since DELETE is keyed by the
	// offence's own surrogate ID, not by URL. nil, nil if not found.
	GetOffence(ctx context.Context, id uint) (*URLOffence, error)
	AttachOffenceToURL(ctx context.Context, urlValue string, categoryID uint, elementID *uint, subElementID *uint) (URLOffence, error)
	DetachOffenceFromURL(ctx context.Context, id uint) error
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
	AgencyStore
	ISPLogoStore
	GridPreferenceStore
	ScanSettingsStore
	EnrichmentStore
	LegalCitationStore
}
