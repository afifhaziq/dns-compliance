package db

import (
	"fmt"
	"strings"
	"time"
)

type DNSServer struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ISP       string    `gorm:"not null;default:'Unknown'" json:"isp"`
	Name      string    `gorm:"not null" json:"name"`
	Address   string    `gorm:"not null" json:"address"`
	Protocol  string    `gorm:"not null" json:"protocol"` // udp, dot, doh
	Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// URL.URL is expected to already be normalized (bare lowercase hostname,
// see internal/urlnorm) by the time it reaches the database — normalization
// happens in the handler/store layer, not via a DB trigger.
type URL struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	URL       string    `gorm:"uniqueIndex;not null" json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

type Department struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"` // "CMOD", "CRD", ...
	CreatedAt time.Time `json:"created_at"`
}

// User.DepartmentID is required for all users including admins. Admins are
// assigned to the "Admin" department. is_admin is the authoritative super
// admin flag (global, cross-department). is_dept_admin scopes the same kind
// of management capability (users, DNS servers) to the user's own
// DepartmentID — mutually exclusive with is_admin. Neither flag set means a
// plain department member (watchlist-only).
type User struct {
	ID           uint        `gorm:"primaryKey" json:"id"`
	Username     string      `gorm:"uniqueIndex;not null" json:"username"`
	PasswordHash string      `gorm:"not null" json:"-"`
	IsAdmin      bool        `gorm:"not null;default:false" json:"is_admin"`
	IsDeptAdmin  bool        `gorm:"not null;default:false" json:"is_dept_admin"`
	DepartmentID *uint       `gorm:"index" json:"department_id,omitempty"`
	Department   *Department `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

type Session struct {
	Token     string    `gorm:"primaryKey" json:"-"`
	UserID    uint      `gorm:"not null;index" json:"user_id"`
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// DepartmentURL links a department's watchlist to a shared URL row. Removing
// a domain from a watchlist only deletes this row — it never touches URL or
// ScanResult, so scan history is preserved even once no department watches
// a domain anymore. Its OnDelete:CASCADE only fires on the admin-only
// "purge a domain" path that deletes the URL row itself.
type DepartmentURL struct {
	DepartmentID uint       `gorm:"primaryKey;autoIncrement:false" json:"department_id"`
	URLID        uint       `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
	URL          URL        `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	Enabled      bool       `gorm:"not null;default:true" json:"enabled"`
	OrderedAt    *time.Time `json:"ordered_at,omitempty"` // optional: when the takedown order was issued for this domain, set at add-time or later
	CreatedAt    time.Time  `json:"created_at"`
}

// URLEntry is the department-scoped view of a URL, carrying the watchlist
// enabled flag and order date that the shared URL model does not have.
type URLEntry struct {
	ID        uint       `json:"id"`
	URL       string     `json:"url"`
	Enabled   bool       `json:"enabled"`
	OrderedAt *time.Time `json:"ordered_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ScanSettings is a single-row (ID 1) table holding the admin-configurable
// scan schedule. SeedScanInterval creates the row from the --interval flag
// on first boot; after that the admin panel is authoritative. Enabled gates
// whether the scheduler's cron sweep actually fires — see StartScheduler.
// Defaults to false: automated scanning is an explicit admin opt-in, not
// something a fresh deployment starts doing on its own.
// DNSWorkers is passed to the crawler on every StartSweep RPC (see
// Scanner.run in internal/server/scanner.go). Defaults to 100, well above
// the crawler CLI's own --dns-workers default of 20 (which only applies to
// standalone/manual crawler runs) — benchmarking against a realistic
// domain-list scale showed 20 concurrent lookups is far too low for a
// full sweep to complete within a reasonable interval.
type ScanSettings struct {
	ID              uint `gorm:"primaryKey" json:"id"`
	IntervalMinutes int  `gorm:"not null" json:"interval_minutes"`
	Enabled         bool `gorm:"not null;default:false" json:"enabled"`
	DNSWorkers      int  `gorm:"not null;default:100" json:"dns_workers"`
}

// CompliantIP is an IP address that counts as compliant even when DNS
// resolves — used to classify ISP block-pages (e.g. MCMC's redirect IP)
// as compliant rather than as violations.
type CompliantIP struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Address   string    `gorm:"uniqueIndex;not null" json:"address"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// ISPLogo is an admin-set logo URL for one ISP, rendered in the Overview
// page's ISPBentoGrid. Keyed by ISP name (not FK'd to DNSServer, since one
// ISP name is shared across multiple DNSServer rows). Purely cosmetic;
// never affects compliance calculations.
type ISPLogo struct {
	ISP       string    `gorm:"primaryKey" json:"isp"`
	LogoURL   string    `json:"logo_url"`
	CreatedAt time.Time `json:"created_at"`
}

type ScanRun struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TriggeredBy string     `json:"triggered_by"` // "scheduled", "manual", "screenshot"
	Status      string     `json:"status"`       // "running", "completed", "failed"
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// The composite index (url_value, dns_server_id, scanned_at) serves the
// latest-scan-per-(domain, server) subqueries in ispStats and
// resurfacedDomains, which would otherwise aggregate the whole table on
// every ISP page load. Column order matters — do not reorder.
type ScanResult struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	ScanRunID          uint      `gorm:"not null;index" json:"scan_run_id"`
	URLID              uint      `gorm:"not null;index" json:"url_id"`
	URLRef             URL       `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	URLValue           string    `gorm:"not null;index:idx_scan_results_url_server_time,priority:1" json:"url"`
	DNSServerID        uint      `gorm:"not null;index;index:idx_scan_results_url_server_time,priority:2" json:"dns_server_id"`
	DNSServer          DNSServer `gorm:"foreignKey:DNSServerID" json:"dns_server"`
	Compliant          bool      `gorm:"not null" json:"compliant"`
	ResolvedIP         string    `json:"resolved_ip"`
	ResolvedIPv6       string    `json:"resolved_ipv6"`
	ResolvedASN        uint      `json:"resolved_asn"`
	ResolvedOrg        string    `json:"resolved_org"`
	ResolvedNetName    string    `json:"resolved_netname"`
	ResolvedAbuseEmail string    `json:"resolved_abuse_email"`
	ScreenshotURL      string    `json:"screenshot_url"`
	Error              string    `json:"error"`
	LatencyMs          int64     `gorm:"default:0" json:"latency_ms"`
	ScannedAt          time.Time `gorm:"index;index:idx_scan_results_url_server_time,priority:3" json:"scanned_at"`
}

// DomainWhois caches RDAP registration metadata for a domain (not a scan —
// registrar/expiry rarely change, so this is refreshed on its own slow
// cadence rather than every scan). Absent row = never fetched. FetchError
// holds the last fetch failure (e.g. no RDAP coverage for the TLD); the
// stale registrar/date fields, if any, are left in place rather than wiped.
type DomainWhois struct {
	URLID               uint       `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
	Registrar           string     `json:"registrar"`
	RegistrarURL        string     `json:"registrar_url"`
	RegistrarAbuseEmail string     `json:"registrar_abuse_email"`
	RegistrarAbusePhone string     `json:"registrar_abuse_phone"`
	DomainCreated       *time.Time `json:"domain_created,omitempty"`
	DomainExpires       *time.Time `json:"domain_expires,omitempty"`
	LastFetchedAt       time.Time  `gorm:"index" json:"last_fetched_at"`
	FetchError          string     `json:"fetch_error,omitempty"`
}

// SubdomainScan caches a subfinder enumeration result for a domain. Unlike
// DomainWhois, it's never refreshed on a background schedule — populated
// lazily on watchlist-add and only ever re-run after that via an explicit
// refresh (see fetchAndStoreSubdomains). Absent row = never fetched.
type SubdomainScan struct {
	URLID      uint      `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
	Subdomains []string  `gorm:"serializer:json" json:"subdomains"`
	FetchedAt  time.Time `json:"fetched_at"`
	FetchError string    `json:"fetch_error,omitempty"`
}

// IPInfo caches ASN + network-operator lookups from ipinfo.io, keyed by
// resolved IP rather than domain — an IP's ASN is stable regardless of which
// domain currently resolves to it, so a distinct IP is looked up at most
// once, ever (no periodic refresh; ASN reassignment is rare enough not to
// matter here). Absent row = never fetched.
type IPInfo struct {
	IP         string    `gorm:"primaryKey" json:"ip"`
	ASN        uint      `json:"asn"`
	Org        string    `json:"org"`
	NetName    string    `json:"netname"`
	AbuseEmail string    `json:"abuse_email"`
	FetchedAt  time.Time `json:"fetched_at"`
	FetchError string    `json:"fetch_error,omitempty"`
}

// Favicon caches a domain's icon so the browser never has to fetch it
// directly — this app tracks domains under active enforcement, so a
// client-side favicon request would reveal an analyst's presence to the
// target's server logs. Fetched at most once per domain, ever (no periodic
// refresh; favicons rarely change).
type Favicon struct {
	Domain      string    `gorm:"primaryKey" json:"domain"`
	ContentType string    `json:"content_type"`
	Data        []byte    `json:"-"`
	FetchedAt   time.Time `json:"fetched_at"`
	FetchError  string    `json:"fetch_error,omitempty"`
}

type ProgressEntry struct {
	DNSServerID uint   `json:"dns_server_id"`
	Name        string `json:"name"`
	Completed   int    `json:"completed"`
}

// DailyComplianceStat is one (DNS server, calendar day) bucket of compliance
// results, pre-aggregated server-side so clients (e.g. the compliance
// heatmap) don't need to fetch and group every raw ScanResult themselves.
type DailyComplianceStat struct {
	DNSServerID   uint   `json:"dns_server_id"`
	DNSServerName string `json:"dns_server_name"`
	Day           string `json:"day"` // YYYY-MM-DD
	Total         int    `json:"total"`
	Compliant     int    `json:"compliant"`
	Level         int    `json:"level"`
}

// ISPServerStat holds per-DNS-server compliance and latency statistics for
// a single ISP, aggregated over the latest scan per (url_value, dns_server_id).
type ISPServerStat struct {
	DNSServer    DNSServer `json:"dns_server"`
	Compliant    int       `json:"compliant"`
	Total        int       `json:"total"`
	AvgLatencyMs float64   `json:"avg_latency_ms"`
	MinLatencyMs int64     `json:"min_latency_ms"`
	MaxLatencyMs int64     `json:"max_latency_ms"`
}

// ISPStatsResult is the response shape for GET /api/isps/{isp}.
type ISPStatsResult struct {
	ISP                string          `json:"isp"`
	Servers            []ISPServerStat `json:"servers"`
	MostViolatedDomain string          `json:"most_violated_domain"`
}

// ISPTrendStat is one calendar day of aggregated compliance for an ISP,
// used by GET /api/isps/{isp}/trend and GET /api/trend.
type ISPTrendStat struct {
	Day       string `json:"day"` // YYYY-MM-DD
	Total     int    `json:"total"`
	Compliant int    `json:"compliant"`
}

// ServerUptimeStat is one calendar day's up/down status for a single DNS
// server, used by GET /api/dns-servers/{id}/uptime. A day is "down" when a
// majority of that day's scans against the server errored with a timeout or
// SERVFAIL — i.e. the resolver itself failed to answer, as opposed to a
// domain simply being blocked (which is a normal, non-error result).
type ServerUptimeStat struct {
	Day string `json:"day"` // YYYY-MM-DD
	Up  bool   `json:"up"`
}

// DomainTiming is how long one domain took (or has been waiting) to be
// blocked by an ISP, measured from its order date. Blocked=false means
// still open — DaysToBlock is then "days waited so far", not a final figure.
type DomainTiming struct {
	Domain      string `json:"domain"`
	DaysToBlock int    `json:"days_to_block"`
	Blocked     bool   `json:"blocked"`
}

// ISPTimingResult is the response shape for GET /api/isps/{isp}/timing.
// Median/avg are computed only over domains with Blocked=true; domains with
// no recorded order date are excluded entirely (WithOrderDateCount tracks
// coverage against TotalDomains so the figure isn't silently misleading).
type ISPTimingResult struct {
	ISP                string         `json:"isp"`
	MedianDaysToBlock  float64        `json:"median_days_to_block"`
	AvgDaysToBlock     float64        `json:"avg_days_to_block"`
	BlockedCount       int            `json:"blocked_count"`
	StillOpenCount     int            `json:"still_open_count"`
	WithOrderDateCount int            `json:"with_order_date_count"`
	TotalDomains       int            `json:"total_domains"`
	Slowest            []DomainTiming `json:"slowest"` // top 5 by days-to-block, blocked and still-open combined
}

// ResurfacedServerEntry is one DNS server on which a domain flipped from
// compliant to violating — see ResurfacedDomain.
type ResurfacedServerEntry struct {
	DNSServerID     uint      `json:"dns_server_id"`
	DNSServerName   string    `json:"dns_server_name"`
	ISP             string    `json:"isp"`
	LastCompliantAt time.Time `json:"last_compliant_at"`
	ResurfacedAt    time.Time `json:"resurfaced_at"`
}

// ResurfacedDomain is a domain whose most recent scan flipped from compliant
// (blocked) to violating (resolving again) on at least one DNS server —
// the highest-signal regression this tool detects, since it means an
// enforcement order that was working has stopped working. AffectedServers
// holds the (possibly partial) set of servers where the flip happened; a
// domain resurfacing on every server at once still gets one row here.
type ResurfacedDomain struct {
	URLValue        string                  `json:"url"`
	ResurfacedAt    time.Time               `json:"resurfaced_at"` // most recent flip across affected servers
	AffectedServers []ResurfacedServerEntry `json:"affected_servers"`
}

// NotificationDetails is a free-form per-notification-type payload (e.g.
// resurfaced's affected-server list, due_date_reached's per-server
// breakdown) — stored as jsonb via GORM's json serializer, same pattern
// Citation.Parsed already uses.
type NotificationDetails map[string]any

// Notification is a queued alert for a department about a domain event —
// either a resurfaced (compliant->violating) regression or a due-date scan
// outcome. URLID/URLValue mirror ScanResult's dual FK+denormalized-value
// pattern: URLID cascades on URL purge, URLValue is the read/query key so
// list/dedup queries don't need a join.
type Notification struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	DepartmentID uint   `gorm:"not null;index:idx_notifications_dept_read,priority:1" json:"department_id"`
	URLID        uint   `gorm:"not null;index" json:"url_id"`
	URL          URL    `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	URLValue     string `gorm:"not null;index" json:"url"`
	Type         string `gorm:"not null" json:"type"` // "resurfaced" | "due_date_reached"
	// Compliant is always nil for "resurfaced" (the type itself is the
	// signal) and always set for "due_date_reached" (the scan outcome).
	Compliant *bool               `json:"compliant,omitempty"`
	Details   NotificationDetails `gorm:"type:jsonb;serializer:json" json:"details,omitempty"`
	ReadAt    *time.Time          `gorm:"index:idx_notifications_dept_read,priority:2" json:"read_at,omitempty"`
	CreatedAt time.Time           `gorm:"index" json:"created_at"`
}

// DomainSummaryFilter narrows ListDomainSummaries/ForDepartment — every
// field is optional (zero value = no filter). Search matches a substring of
// the domain; DNSServerID, when set, also restricts the aggregate counts to
// that server's scans only (not just which domains touched it); Status is
// "compliant" (never once violated) or "violations" (violated at least
// once), any other value is treated as no filter.
type DomainSummaryFilter struct {
	Search      string
	DNSServerID uint
	Status      string
}

// DomainSummary is one row of GET /api/domains — a lifetime aggregate over
// every ScanResult ever recorded for a domain (not just the latest scan
// run), used to browse/look up any domain with scan history.
type DomainSummary struct {
	URLValue       string    `json:"url"`
	TotalScans     int       `json:"total_scans"`
	CompliantScans int       `json:"compliant_scans"`
	LastScannedAt  time.Time `json:"last_scanned_at"`
}

// DomainServerSummary is one DNS server's lifetime aggregate for a single
// domain — the nested per-row breakdown under GET /api/domains/*url, same
// shape as DomainSummary but scoped to one domain and split by server.
type DomainServerSummary struct {
	DNSServerID    uint      `json:"dns_server_id"`
	DNSServerName  string    `json:"dns_server_name"`
	ISP            string    `json:"isp"`
	Address        string    `json:"address"` // IP:port for udp/dot, full URL for doh
	TotalScans     int       `json:"total_scans"`
	CompliantScans int       `json:"compliant_scans"`
	LastScannedAt  time.Time `json:"last_scanned_at"`
}

// Instrument is a Malaysian law (Act, Ordinance, Enactment, subsidiary
// legislation, or Constitution) — created/selected once and reused via
// GetOrCreateInstrument rather than re-entered per citation. Jurisdiction
// scopes legal force informationally only (a state Enactment only applies
// within that state); nothing in this package enforces that beyond storage.
type Instrument struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Type         string    `gorm:"not null;index" json:"type"`         // ACT, ORDINANCE, ENACTMENT, SUBSIDIARY, CONSTITUTION
	Jurisdiction string    `gorm:"not null;index" json:"jurisdiction"` // FEDERAL, or a state name
	Number       string    `gorm:"not null;default:''" json:"number"`  // "588", "A1220", "No. 9 of 1995" — always a string, amendment/state formats break plain int. May be "" — plenty of instruments (older pre-1968-revision Acts, most state Enactments) have no commonly cited official number
	Year         *int      `json:"year,omitempty"`
	ShortTitle   string    `gorm:"not null" json:"short_title"`
	CreatedAt    time.Time `json:"created_at"`
}

// LegalCitationParsed is the structured breakdown of Citation.RawText,
// produced by internal/legalcite.Parse or hand-corrected when
// ParseConfidence is NEEDS_REVIEW. Section/Article and Subsection/Clause
// share the same ProvisionNum/SubProvision fields — which label applies is
// a display-only switch on Instrument.Type == "CONSTITUTION", not a
// separate set of columns. Suffixes (e.g. the "A" in "4A") are captured
// regardless of instrument type, since e.g. Article 121(1A) is a real,
// frequently-cited provision.
type LegalCitationParsed struct {
	Part               *int   `json:"part,omitempty"`
	Chapter            *int   `json:"chapter,omitempty"`
	ProvisionNum       *int   `json:"provision_num,omitempty"`
	ProvisionSuffix    string `json:"provision_suffix,omitempty"`
	SubProvision       *int   `json:"sub_provision,omitempty"`
	SubProvisionSuffix string `json:"sub_provision_suffix,omitempty"`
	Paragraph          string `json:"paragraph,omitempty"`
	Subparagraph       string `json:"subparagraph,omitempty"`
	SubSubparagraph    string `json:"sub_subparagraph,omitempty"`
	Schedule           *int   `json:"schedule,omitempty"`
	ScheduleList       *int   `json:"schedule_list,omitempty"`
}

// Citation is one specific provision cited under an Instrument. RawText is
// exactly what the user typed — the source of truth/audit trail. Parsed is
// stored as a genuine Postgres jsonb column (not the plain-JSON
// serializer-only pattern SubdomainScan.Subdomains uses) so it stays
// independently indexable via parsed->>'key' if a future feature needs
// that. SortKey is a derived, zero-padded sortable string over
// ProvisionNum+ProvisionSuffix (see BuildProvisionSortKey) — recomputed by
// the store on every create/update, never client-supplied — because a
// plain ORDER BY on provision_num would put "4A" after "40".
type Citation struct {
	ID              uint                `gorm:"primaryKey" json:"id"`
	InstrumentID    uint                `gorm:"not null;index" json:"instrument_id"`
	Instrument      Instrument          `gorm:"foreignKey:InstrumentID;constraint:OnDelete:CASCADE" json:"instrument"`
	RawText         string              `gorm:"not null" json:"raw_text"`
	Parsed          LegalCitationParsed `gorm:"type:jsonb;serializer:json" json:"parsed"`
	SortKey         string              `gorm:"index" json:"-"`
	ParseConfidence string              `gorm:"not null;default:'NEEDS_REVIEW'" json:"parse_confidence"` // OK, NEEDS_REVIEW
	CreatedAt       time.Time           `json:"created_at"`
}

// Category is scoped to one Citation, not a shared global lookup — the
// same category name under two different citations is deliberately two
// separate rows, since category vocabulary is specific to the wording of
// the provision it's cited under (e.g. the content-offence categories that
// make sense under CMA 1998 s233 don't generalize to an unrelated Act).
type Category struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CitationID uint      `gorm:"not null;index" json:"citation_id"`
	Citation   Citation  `gorm:"foreignKey:CitationID;constraint:OnDelete:CASCADE" json:"citation"`
	Name       string    `gorm:"not null" json:"name"`
	CreatedAt  time.Time `json:"created_at"`
}

// Element is an optional sub-category of a Category — not every category
// has one (e.g. "Indecent" stands alone; "Harassment" splits into
// "Menacing"/"Obscene" elements).
type Element struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CategoryID uint      `gorm:"not null;index" json:"category_id"`
	Category   Category  `gorm:"foreignKey:CategoryID;constraint:OnDelete:CASCADE" json:"-"`
	Name       string    `gorm:"not null" json:"name"`
	CreatedAt  time.Time `json:"created_at"`
}

// URLOffence links a URL to the specific offence it committed, at Category
// granularity with an optional Element. Uses a surrogate ID PK rather than
// a composite one (unlike DepartmentURL) because ElementID is nullable and
// SQL NULL != NULL breaks composite-PK uniqueness semantics.
type URLOffence struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	URLID      uint      `gorm:"not null;index" json:"url_id"`
	URL        URL       `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	CategoryID uint      `gorm:"not null;index" json:"category_id"`
	Category   Category  `gorm:"foreignKey:CategoryID;constraint:OnDelete:CASCADE" json:"category"`
	ElementID  *uint     `gorm:"index" json:"element_id,omitempty"`
	Element    *Element  `gorm:"foreignKey:ElementID;constraint:OnDelete:CASCADE" json:"element,omitempty"`
	RecordedAt time.Time `gorm:"not null" json:"recorded_at"`
}

// BuildProvisionSortKey returns a zero-padded, suffix-aware sortable
// representation of a section/article number + its letter suffix, so a
// plain ORDER BY doesn't put "4A" after "40". A nil num (a Part-only or
// Schedule-only citation with no provision number) sorts first via "".
func BuildProvisionSortKey(num *int, suffix string) string {
	if num == nil {
		return ""
	}
	return fmt.Sprintf("%06d%s", *num, strings.ToUpper(suffix))
}

// DailyComplianceLevel buckets a day's results onto the heatmap's 5-level
// scale: 0 = no scans, 1 = fully compliant, 2-4 = increasing violation
// severity (share of that day's scans that failed).
func DailyComplianceLevel(total, compliant int) int {
	if total == 0 {
		return 0
	}
	violations := total - compliant
	if violations == 0 {
		return 1
	}
	rate := float64(violations) / float64(total)
	if rate <= 1.0/3.0 {
		return 2
	}
	if rate <= 2.0/3.0 {
		return 3
	}
	return 4
}
