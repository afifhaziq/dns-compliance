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
