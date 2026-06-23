package db

import "time"

type DNSServer struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null" json:"name"`
	Address   string    `gorm:"not null" json:"address"`
	Protocol  string    `gorm:"not null" json:"protocol"` // udp, dot, doh
	CreatedAt time.Time `json:"created_at"`
}

type URL struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	URL       string    `gorm:"uniqueIndex;not null" json:"url"`
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
