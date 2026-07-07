package db

import "time"

type DNSServer struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ISP       string    `gorm:"not null;default:'Unknown'" json:"isp"`
	Name      string    `gorm:"not null" json:"name"`
	Address   string    `gorm:"not null" json:"address"`
	Protocol  string    `gorm:"not null" json:"protocol"` // udp, dot, doh
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
// assigned to the "Admin" department. is_admin is the authoritative admin
// flag — DepartmentID being non-nil no longer implies non-admin.
type User struct {
	ID           uint        `gorm:"primaryKey" json:"id"`
	Username     string      `gorm:"uniqueIndex;not null" json:"username"`
	PasswordHash string      `gorm:"not null" json:"-"`
	IsAdmin      bool        `gorm:"not null;default:false" json:"is_admin"`
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

// CompliantIP is an IP address that counts as compliant even when DNS
// resolves — used to classify ISP block-pages (e.g. MCMC's redirect IP)
// as compliant rather than as violations.
type CompliantIP struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Address   string    `gorm:"uniqueIndex;not null" json:"address"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type ScanRun struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TriggeredBy string     `json:"triggered_by"` // "scheduled", "manual", "screenshot"
	Status      string     `json:"status"`       // "running", "completed", "failed"
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ScanResult struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	ScanRunID       uint      `gorm:"not null;index" json:"scan_run_id"`
	URLID           uint      `gorm:"not null;index" json:"url_id"`
	URLRef          URL       `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	URLValue        string    `gorm:"not null" json:"url"`
	DNSServerID     uint      `gorm:"not null" json:"dns_server_id"`
	DNSServer       DNSServer `gorm:"foreignKey:DNSServerID" json:"dns_server"`
	Compliant       bool      `gorm:"not null" json:"compliant"`
	ResolvedIP      string    `json:"resolved_ip"`
	ResolvedIPv6    string    `json:"resolved_ipv6"`
	ResolvedASN     uint      `json:"resolved_asn"`
	ResolvedOrg     string    `json:"resolved_org"`
	ResolvedNetName string    `json:"resolved_netname"`
	ScreenshotURL   string    `json:"screenshot_url"`
	Error           string    `json:"error"`
	LatencyMs       int64     `gorm:"default:0" json:"latency_ms"`
	ScannedAt       time.Time `json:"scanned_at"`
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
