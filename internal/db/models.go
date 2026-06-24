package db

import "time"

type DNSServer struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
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

// User.DepartmentID is nil for admins (Admin is a cross-cutting flag, not a
// department of its own) and required for everyone else — enforced in
// application code rather than a DB constraint.
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
	DepartmentID uint      `gorm:"primaryKey;autoIncrement:false" json:"department_id"`
	URLID        uint      `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
	URL          URL       `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type ScanRun struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TriggeredBy string     `json:"triggered_by"` // "scheduled", "manual", "screenshot"
	Status      string     `json:"status"`       // "running", "completed", "failed"
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ScanResult struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	ScanRunID     uint      `gorm:"not null;index" json:"scan_run_id"`
	URLID         uint      `gorm:"not null;index" json:"url_id"`
	URLRef        URL       `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	URLValue      string    `gorm:"not null" json:"url"`
	DNSServerID   uint      `gorm:"not null" json:"dns_server_id"`
	DNSServer     DNSServer `gorm:"foreignKey:DNSServerID" json:"dns_server"`
	Compliant     bool      `gorm:"not null" json:"compliant"`
	ResolvedIP    string    `json:"resolved_ip"`
	ScreenshotURL string    `json:"screenshot_url"`
	Error         string    `json:"error"`
	ScannedAt     time.Time `json:"scanned_at"`
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
