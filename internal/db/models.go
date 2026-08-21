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
//
// URL is purely domain identity now — no case metadata. Agency/Status/
// DueDate/RequestedAt moved to Case (see Case's doc comment): a domain can
// carry many cases over its history (reblocked under a new reference), so a
// scalar column on URL could only ever hold the latest one. URLEntry still
// exposes these under the same JSON field names for frontend compatibility,
// derived from the URL's most-recently-created Case (see ListDepartmentURLs).
type URL struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	URL       string    `gorm:"uniqueIndex;not null" json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

// Agency is an admin-managed lookup table for the government agency behind
// a takedown request — replaces an earlier free-text Agency column. Read
// open to any authenticated role; create/delete gated to admin-or-dept-admin
// (see router.go), matching the DNS-server/ISP-logo/legal-catalog pattern
// rather than the stricter super-admin-only Department/CompliantIP pattern.
type Agency struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Recipient and Requestor are admin-managed lookup tables backing the
// CaseLetter.Recipient/Requestor dropdowns on the Docs page's Add Document
// dialog — same shape as Agency (read open, mutations admin-or-dept-admin
// gated, see router.go). CaseLetter itself keeps them as plain strings
// (the selected name), not a foreign key — these tables only exist to
// populate the picker with admin-configured options.
type Recipient struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Requestor struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// DueDatePreset is an admin/dept-admin-managed duration option shown in the
// watchlist's "Time to Block" picker (urls.tsx) — e.g. Label "24 hours",
// Minutes 1440; the picker computes the actual due_date as now+Minutes at
// selection time. Minute granularity (not just hours) exists so a short
// preset (e.g. 2 minutes) can be used to test due-date notifications
// without waiting hours. Shared/global like Agency/DNSServer, not
// department-scoped. Read open to any authenticated role; create/delete
// gated to admin-or-dept-admin (see router.go), same pattern as Agency.
type DueDatePreset struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Label     string    `gorm:"not null" json:"label"`
	Minutes   int       `gorm:"not null" json:"minutes"`
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
	ID                 uint        `gorm:"primaryKey" json:"id"`
	Username           string      `gorm:"uniqueIndex;not null" json:"username"`
	PasswordHash       string      `gorm:"not null" json:"-"`
	IsAdmin            bool        `gorm:"not null;default:false" json:"is_admin"`
	IsDeptAdmin        bool        `gorm:"not null;default:false" json:"is_dept_admin"`
	DepartmentID       *uint       `gorm:"index" json:"department_id,omitempty"`
	Department         *Department `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	MustChangePassword bool        `gorm:"not null;default:false" json:"must_change_password"`
	CreatedAt          time.Time   `json:"created_at"`
}

type Session struct {
	Token     string    `gorm:"primaryKey" json:"-"`
	UserID    uint      `gorm:"not null;index" json:"user_id"`
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// DepartmentURL links a department's watchlist to a shared URL row. Carries
// no case metadata — that lives on Case now (see Case's doc comment).
// Removing a domain from a watchlist only deletes this row — it never
// touches URL or ScanResult, so scan history is preserved even once no
// department watches a domain anymore. Its OnDelete:CASCADE only fires on
// the admin-only "purge a domain" path that deletes the URL row itself.
type DepartmentURL struct {
	DepartmentID uint      `gorm:"primaryKey;autoIncrement:false" json:"department_id"`
	URLID        uint      `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
	URL          URL       `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	Enabled      bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// URLEntry is the department-scoped watchlist row shape returned to the
// frontend: URL identity plus DepartmentURL's Enabled, plus case metadata
// derived from the URL's most-recently-created Case (see
// postgresStore.ListDepartmentURLs) — not stored on URL itself. Field names
// and JSON tags are unchanged from when these lived directly on URL, for
// frontend compatibility. AgencyName is denormalized in so the frontend
// doesn't need to cross-reference the Agency list just to render a cell;
// AgencyID is included too since the inline-edit dropdown needs the raw id
// to preselect the current option. A URL with zero Cases yields all of
// these as null/empty.
type URLEntry struct {
	ID                     uint       `json:"id"`
	URL                    string     `json:"url"`
	Enabled                bool       `json:"enabled"`
	DueDate                *time.Time `json:"due_date,omitempty"`
	AgencyID               *uint      `json:"agency_id,omitempty"`
	AgencyName             string     `json:"agency_name,omitempty"`
	Status                 string     `json:"status,omitempty"`
	RequestedAt            *time.Time `json:"requested_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	CurrentReferenceNumber string     `json:"current_reference_number,omitempty"`
	RequestingDepartments  []string   `gorm:"-" json:"requesting_departments,omitempty"`
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
// SLAIntervalMinutes/SLAStreakThreshold configure a second, independent
// schedule (see StartSLAScheduler in internal/server/scheduler.go) that
// scans only URLs still under active SLA tracking — those with a DueDate
// in the past that haven't yet racked up SLAStreakThreshold consecutive
// compliant scans on every enabled DNS server — at a (typically shorter)
// cadence than the normal IntervalMinutes sweep, so time-to-compliance
// (see ispComplianceTiming) is measured at finer granularity than the
// scan history of URLs with no due date needs.
type ScanSettings struct {
	ID                 uint `gorm:"primaryKey" json:"id"`
	IntervalMinutes    int  `gorm:"not null" json:"interval_minutes"`
	Enabled            bool `gorm:"not null;default:false" json:"enabled"`
	DNSWorkers         int  `gorm:"not null;default:100" json:"dns_workers"`
	SLAIntervalMinutes int  `gorm:"not null;default:15" json:"sla_interval_minutes"`
	SLAStreakThreshold int  `gorm:"not null;default:3" json:"sla_streak_threshold"`
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

// GridPreference persists one user's saved layout (column visibility, sort
// column/direction, page size) for a named data grid, so it survives a
// reload or a switch to another device instead of resetting every session.
// Keyed by (UserID, GridKey) since one user can have a different layout per
// grid (e.g. the watchlist grid vs the results grid). OnDelete:CASCADE since
// a preference has no meaning once its owning user is gone.
type GridPreference struct {
	UserID           uint            `gorm:"primaryKey;autoIncrement:false" json:"-"`
	GridKey          string          `gorm:"primaryKey" json:"-"`
	User             User            `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	ColumnVisibility map[string]bool `gorm:"type:jsonb;serializer:json" json:"column_visibility"`
	SortField        string          `json:"sort_field"`
	SortDesc         bool            `json:"sort_desc"`
	PageSize         int             `json:"page_size"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type ScanRun struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TriggeredBy string     `json:"triggered_by"` // "scheduled", "scheduled-sla", "manual", "screenshot"
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
// no recorded due date are excluded entirely (WithDueDateCount tracks
// coverage against TotalDomains so the figure isn't silently misleading).
type ISPTimingResult struct {
	ISP               string         `json:"isp"`
	MedianDaysToBlock float64        `json:"median_days_to_block"`
	AvgDaysToBlock    float64        `json:"avg_days_to_block"`
	BlockedCount      int            `json:"blocked_count"`
	StillOpenCount    int            `json:"still_open_count"`
	WithDueDateCount  int            `json:"with_due_date_count"`
	TotalDomains      int            `json:"total_domains"`
	Slowest           []DomainTiming `json:"slowest"` // top 5 by days-to-block, blocked and still-open combined
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
	// ScanRunID/ScannedAt record which scan produced this notification.
	// "due_date_reached" always sets both (one targeted Trigger call is
	// always exactly one ScanRun). "resurfaced" sets only ScannedAt (the
	// flip time) — a resurfacing event can span servers checked in
	// different scan runs, so there's no single ScanRunID to attribute it
	// to.
	ScanRunID *uint      `json:"scan_run_id,omitempty"`
	ScannedAt *time.Time `json:"scanned_at,omitempty"`
	ReadAt    *time.Time `gorm:"index:idx_notifications_dept_read,priority:2" json:"read_at,omitempty"`
	CreatedAt time.Time  `gorm:"index" json:"created_at"`
}

// DomainSummaryFilter narrows ListDomainSummaries/ForDepartment — every
// field is optional (empty/zero = no filter). Search matches a substring of
// the domain. DNSServerIDs, when set, also restricts the aggregate counts to
// those servers' scans only (not just which domains touched them) — matching
// ANY of the listed IDs, or NONE of them when DNSServerExclude is true (the
// "is any of"/"is not any of" filter operators). Statuses is any combination
// of "compliant" (never once violated) or "violations" (violated at least
// once); StatusExclude flips the same any/none semantics.
type DomainSummaryFilter struct {
	Search           string
	DNSServerIDs     []uint
	DNSServerExclude bool
	Statuses         []string
	StatusExclude    bool
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

// SubElement is an optional sub-category of an Element — not every element
// has one, mirroring how not every Category has an Element.
type SubElement struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ElementID uint      `gorm:"not null;index" json:"element_id"`
	Element   Element   `gorm:"foreignKey:ElementID;constraint:OnDelete:CASCADE" json:"-"`
	Name      string    `gorm:"not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// URLOffence links a URL to the specific offence it committed, at Category
// granularity with an optional Element and, one level deeper, an optional
// SubElement. Uses a surrogate ID PK rather than a composite one (unlike
// DepartmentURL) because ElementID/SubElementID are nullable and SQL
// NULL != NULL breaks composite-PK uniqueness semantics.
type URLOffence struct {
	ID           uint        `gorm:"primaryKey" json:"id"`
	URLID        uint        `gorm:"not null;index" json:"url_id"`
	URL          URL         `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	CategoryID   uint        `gorm:"not null;index" json:"category_id"`
	Category     Category    `gorm:"foreignKey:CategoryID;constraint:OnDelete:CASCADE" json:"category"`
	ElementID    *uint       `gorm:"index" json:"element_id,omitempty"`
	Element      *Element    `gorm:"foreignKey:ElementID;constraint:OnDelete:CASCADE" json:"element,omitempty"`
	SubElementID *uint       `gorm:"index" json:"sub_element_id,omitempty"`
	SubElement   *SubElement `gorm:"foreignKey:SubElementID;constraint:OnDelete:CASCADE" json:"sub_element,omitempty"`
	RecordedAt   time.Time   `gorm:"not null" json:"recorded_at"`
}

// Case is one row per real-world case/request — the same role ScanRun
// already plays for ScanResult. Letter-grain facts (reference numbers,
// subject, OIC, workflow status...) still live on CaseLetter, since the
// source data's real grain is one row per letter/document. Agency/Status/
// DueDate/RequestedAt live here instead, as the case-level defaults shared
// by every URL the case covers (formerly scalar columns on URL, before a
// domain could carry more than one case) — CaseURL.Phase is the per-domain
// override within this case (see CaseURL's doc comment).
type Case struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	DepartmentID uint       `gorm:"not null;index" json:"department_id"`
	Department   Department `gorm:"foreignKey:DepartmentID" json:"-"`
	CreatedAt    time.Time  `json:"created_at"`

	// DueDate is the takedown-order SLA deadline (carries time-of-day — some
	// orders require blocking within 6h/24h).
	DueDate *time.Time `json:"due_date,omitempty"`
	// AgencyID is nullable and OnDelete:SET NULL — deleting an Agency must
	// not cascade-delete the Case.
	AgencyID *uint   `gorm:"index" json:"agency_id,omitempty"`
	Agency   *Agency `gorm:"foreignKey:AgencyID;constraint:OnDelete:SET NULL" json:"agency,omitempty"`
	// Status is requested | uplift | suspended, validated server-side
	// (internal/server/handlers.go's urlStatusAllowed) — independent of the
	// derived Compliant field; blocked/not-blocked already comes from scan
	// results. This is the case-level default; CaseURL.Phase carries the
	// same vocabulary as a per-domain override.
	Status      string     `json:"status,omitempty"`
	RequestedAt *time.Time `json:"requested_at,omitempty"`
}

// CaseFields is a partial update to a Case's shared fields, mirroring the
// double-pointer clear-vs-untouched contract the old URLCaseFields used:
// outer nil = don't touch, outer non-nil pointing at a nil inner = clear,
// outer non-nil pointing at &v = set. AgencyID/DueDate/RequestedAt need this
// three-state contract since none of them has a natural empty-value
// sentinel to mean "clear". Status is a plain pointer since "" already
// means clear (see urlStatusAllowed).
type CaseFields struct {
	AgencyID    **uint
	Status      *string
	DueDate     **time.Time
	RequestedAt **time.Time
}

// CaseCreateOptions carries the optional case-level fields CreateCase can
// set at creation time, alongside the always-required phase.
type CaseCreateOptions struct {
	AgencyID *uint
	DueDate  *time.Time
}

// CaseLetter is one row per actual letter/document (Memo, Notice, Memo
// (Uplift), Notice (Uplift)) FK'd to a Case — a case with a block plus a
// later uplift gets up to 4 rows here. Mirrors the source sheet's real
// grain (one row per letter), which an earlier single-Case-row design
// collapsed away.
type CaseLetter struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	CaseID uint   `gorm:"not null;index" json:"case_id"`
	Type   string `gorm:"not null" json:"type"` // Memo | Notice | Memo (Uplift) | Notice (Uplift)
	// ReferenceNumberExternal is "No. Rujukan NMD" — the citable reference
	// that actually goes to the ISP/regulator; current_reference_number
	// (URLEntry, ListDepartmentURLs) derives from this field on Notice-type
	// letters. ReferenceNumberInternal is "No. Rujukan NMSMD" — MCMC-internal,
	// never sent externally. Renamed/split from a single ReferenceNumber
	// field (db.Connect's case_letters.reference_number -> ...external
	// rename) since the two were previously conflated.
	ReferenceNumberExternal string     `json:"reference_number_external,omitempty"`
	ReferenceNumberInternal string     `json:"reference_number_internal,omitempty"`
	WorkflowStatus          string     `json:"workflow_status,omitempty"` // CMOD-only: Draft | Pending Legal | Pending TSC | Submitted
	Recipient               string     `json:"recipient,omitempty"`       // CMOD's "Recipient" column -- who the letter was sent to, same grain as Requestor/Subject (per letter, not per URL)
	LetterDate              *time.Time `json:"letter_date,omitempty"`
	ReceivedAt              *time.Time `json:"received_at,omitempty"` // CMOD's "Received" column -- sparse/inconsistently formatted in the source sheet, same grain as SubmittedAt
	SubmittedAt             *time.Time `json:"submitted_at,omitempty"`
	Subject                 string     `json:"subject,omitempty"`
	OICUserID               *uint      `gorm:"index" json:"oic_user_id,omitempty"`
	OICUser                 *User      `gorm:"foreignKey:OICUserID;constraint:OnDelete:SET NULL" json:"oic_user,omitempty"`
	Requestor               string     `json:"requestor,omitempty"`
	Remarks                 string     `json:"remarks,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
}

// CaseURL is the many-to-many join between cases and urls — a case
// genuinely covers many urls (e.g. one Notice listing 10 URLs) and a url
// genuinely belongs to many cases over its history (reblocked later under
// a new reference). Carries its own Phase rather than being a plain
// junction table: some urls within the same case reach a different
// outcome than their siblings, so phase varies per url, not per letter.
// Case.Status is this case's default status/phase (shared by every url it
// covers); Phase here is that specific url's override within this case —
// they start equal at creation (see CreateCase) and only diverge if
// someone later calls UpdateCaseURLPhase for this one url. Sole source of
// truth for the url<->reference-number relationship now that
// URL.ReferenceNumber is gone — "current" reference/status for display is
// derived by querying the most recent CaseLetter row for the case (via
// LetterDate) joined through CaseURL, not stored as a scalar on URL.
type CaseURL struct {
	CaseID uint   `gorm:"primaryKey;autoIncrement:false" json:"case_id"`
	URLID  uint   `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
	Case   Case   `gorm:"foreignKey:CaseID;constraint:OnDelete:CASCADE" json:"-"`
	URL    URL    `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	Phase  string `gorm:"not null" json:"phase"` // requested | uplift | suspended
}

// CaseLetterEntry is one row for the Docs page: a CaseLetter plus its
// case's department and every URL the case covers (via CaseURL — shared
// across all of a case's letters, since URLs belong to the case, not the
// individual letter). Not a persisted table.
type CaseLetterEntry struct {
	CaseLetter
	DepartmentID   uint     `json:"department_id"`
	DepartmentName string   `json:"department_name"`
	URLs           []string `gorm:"-" json:"urls"`
}

// CaseSummary is one row for the Cases view (GET /api/case-summaries): a
// Case's own fields plus its Notice letter's fields (Notice chosen over
// Memo when both exist, same convention as current_reference_number, see
// ListDepartmentURLs) and every domain it covers. Not a persisted table.
type CaseSummary struct {
	ID                             uint                `json:"id"`
	AgencyID                       *uint               `json:"agency_id,omitempty"`
	AgencyName                     string              `json:"agency_name,omitempty"`
	Status                         string              `json:"status,omitempty"`
	DueDate                        *time.Time          `json:"due_date,omitempty"`
	RequestedAt                    *time.Time          `json:"requested_at,omitempty"`
	CreatedAt                      time.Time           `json:"created_at"`
	NoticeLetterID                 *uint               `json:"notice_letter_id,omitempty"`
	NoticeSubject                  string              `json:"notice_subject,omitempty"`
	NoticeWorkflowStatus           string              `json:"notice_workflow_status,omitempty"`
	NoticeReferenceNumberExternal  string              `json:"notice_reference_number_external,omitempty"`
	NoticeReferenceNumberInternal  string              `json:"notice_reference_number_internal,omitempty"`
	NoticeRecipient                string              `json:"notice_recipient,omitempty"`
	NoticeRequestor                string              `json:"notice_requestor,omitempty"`
	NoticeLetterDate               *time.Time          `json:"notice_letter_date,omitempty"`
	NoticeReceivedAt               *time.Time          `json:"notice_received_at,omitempty"`
	NoticeSubmittedAt              *time.Time          `json:"notice_submitted_at,omitempty"`
	NoticeRemarks                  string              `json:"notice_remarks,omitempty"`
	MemoLetterID                   *uint               `json:"memo_letter_id,omitempty"`
	MemoSubject                    string              `json:"memo_subject,omitempty"`
	MemoReferenceNumberInternal    string              `json:"memo_reference_number_internal,omitempty"`
	Domains                        []CaseSummaryDomain `gorm:"-" json:"domains"`
}

// CaseSummaryDomain is one domain a CaseSummary covers, via CaseURL.
type CaseSummaryDomain struct {
	URLID uint   `json:"url_id"`
	URL   string `json:"url"`
	Phase string `json:"phase"`
}

// CaseLetterFields is a partial update to a CaseLetter's fields (PATCH
// /api/cases/{id}/letters/{letter_id}). String fields use the same
// present-but-empty-clears convention as CaseFields.Status (nil = don't
// touch, non-nil "" = clear, non-nil non-"" = set); the three date fields
// have no natural empty sentinel so they use CaseFields.DueDate's
// double-pointer convention instead (outer nil = don't touch, outer
// non-nil -> nil inner = clear, outer non-nil -> &v = set).
type CaseLetterFields struct {
	Subject                 *string
	WorkflowStatus          *string
	ReferenceNumberExternal *string
	ReferenceNumberInternal *string
	Recipient               *string
	Requestor               *string
	Remarks                 *string
	LetterDate              **time.Time
	ReceivedAt              **time.Time
	SubmittedAt             **time.Time
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
