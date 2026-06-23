package db

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type postgresStore struct{ db *gorm.DB }

func NewStore(db *gorm.DB) Store { return &postgresStore{db: db} }

func (s *postgresStore) ListURLs(ctx context.Context) ([]URL, error) {
	var urls []URL
	return urls, s.db.WithContext(ctx).Order("created_at asc").Find(&urls).Error
}

func (s *postgresStore) CreateURL(ctx context.Context, rawURL string) (URL, error) {
	u := URL{URL: rawURL}
	return u, s.db.WithContext(ctx).Create(&u).Error
}

func (s *postgresStore) DeleteURL(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&URL{}, id).Error
}

func (s *postgresStore) ListDNSServers(ctx context.Context) ([]DNSServer, error) {
	var servers []DNSServer
	return servers, s.db.WithContext(ctx).Order("created_at asc").Find(&servers).Error
}

func (s *postgresStore) CreateDNSServer(ctx context.Context, srv DNSServer) (DNSServer, error) {
	return srv, s.db.WithContext(ctx).Create(&srv).Error
}

func (s *postgresStore) DeleteDNSServer(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&DNSServer{}, id).Error
}

func (s *postgresStore) CreateScanRun(ctx context.Context, triggeredBy string) (ScanRun, error) {
	run := ScanRun{TriggeredBy: triggeredBy, Status: "running", StartedAt: time.Now()}
	return run, s.db.WithContext(ctx).Create(&run).Error
}

func (s *postgresStore) CompleteScanRun(ctx context.Context, id uint, status string, completedAt time.Time) error {
	return s.db.WithContext(ctx).Model(&ScanRun{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "completed_at": completedAt}).Error
}

func (s *postgresStore) ActiveScanRun(ctx context.Context) (*ScanRun, error) {
	var run ScanRun
	err := s.db.WithContext(ctx).Where("status = ?", "running").First(&run).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *postgresStore) LatestResults(ctx context.Context) ([]ScanResult, error) {
	var results []ScanResult
	// Subquery: latest scanned_at per (url_value, dns_server_id)
	sub := s.db.Model(&ScanResult{}).
		Select("url_value, dns_server_id, MAX(scanned_at) as max_scanned_at").
		Group("url_value, dns_server_id")
	err := s.db.WithContext(ctx).
		Joins("JOIN (?) as latest ON scan_results.url_value = latest.url_value AND scan_results.dns_server_id = latest.dns_server_id AND scan_results.scanned_at = latest.max_scanned_at", sub).
		Preload("DNSServer").
		Order("scan_results.url_value, scan_results.dns_server_id").
		Find(&results).Error
	return results, err
}

func (s *postgresStore) ResultsByURL(ctx context.Context, urlValue string, since, until time.Time) ([]ScanResult, error) {
	var results []ScanResult
	q := s.db.WithContext(ctx).Where("url_value = ? AND scanned_at >= ?", urlValue, since)
	if !until.IsZero() {
		q = q.Where("scanned_at <= ?", until)
	}
	err := q.Preload("DNSServer").Order("scanned_at desc").Find(&results).Error
	return results, err
}

// dailyComplianceRow is the lean projection fetched for aggregation — avoids
// pulling every ScanResult column (and the full nested DNSServer object) over
// the wire just to collapse rows down to one bucket per (server, day).
type dailyComplianceRow struct {
	DNSServerID   uint
	DNSServerName string
	ScannedAt     time.Time
	Compliant     bool
}

func (s *postgresStore) DailyComplianceByURL(ctx context.Context, urlValue string, since, until time.Time) ([]DailyComplianceStat, error) {
	var rows []dailyComplianceRow
	q := s.db.WithContext(ctx).
		Table("scan_results").
		Select("scan_results.dns_server_id, dns_servers.name as dns_server_name, scan_results.scanned_at, scan_results.compliant").
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Where("scan_results.url_value = ? AND scan_results.scanned_at >= ?", urlValue, since)
	if !until.IsZero() {
		q = q.Where("scan_results.scanned_at <= ?", until)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}

	// Grouped in Go rather than via a SQL date-truncation function: Postgres
	// and the SQLite test backend don't share a single portable function that
	// both (a) accepts the same syntax and (b) reliably scans back as a string.
	type bucketKey struct {
		dnsServerID uint
		day         string
	}
	type bucket struct {
		dnsServerName        string
		total, compliantSum int
	}
	buckets := make(map[bucketKey]*bucket)
	order := make([]bucketKey, 0)
	for _, r := range rows {
		k := bucketKey{dnsServerID: r.DNSServerID, day: r.ScannedAt.Format("2006-01-02")}
		b, ok := buckets[k]
		if !ok {
			b = &bucket{dnsServerName: r.DNSServerName}
			buckets[k] = b
			order = append(order, k)
		}
		b.total++
		if r.Compliant {
			b.compliantSum++
		}
	}

	stats := make([]DailyComplianceStat, 0, len(order))
	for _, k := range order {
		b := buckets[k]
		stats = append(stats, DailyComplianceStat{
			DNSServerID:   k.dnsServerID,
			DNSServerName: b.dnsServerName,
			Day:           k.day,
			Total:         b.total,
			Compliant:     b.compliantSum,
			Level:         DailyComplianceLevel(b.total, b.compliantSum),
		})
	}
	return stats, nil
}

func (s *postgresStore) InsertResult(ctx context.Context, r ScanResult) error {
	return s.db.WithContext(ctx).Create(&r).Error
}

func (s *postgresStore) UpdateScreenshot(ctx context.Context, resultID uint, screenshotURL string) error {
	return s.db.WithContext(ctx).Model(&ScanResult{}).Where("id = ?", resultID).
		Update("screenshot_url", screenshotURL).Error
}

func (s *postgresStore) LastScanRun(ctx context.Context) (*ScanRun, error) {
	var run ScanRun
	err := s.db.WithContext(ctx).Order("started_at DESC").First(&run).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

type progressRow struct {
	DNSServerID uint
	Name        string
	Completed   int
}

func (s *postgresStore) ScanProgress(ctx context.Context, runID uint) ([]ProgressEntry, error) {
	var rows []progressRow
	err := s.db.WithContext(ctx).
		Model(&DNSServer{}).
		Select("dns_servers.id as dns_server_id, dns_servers.name, COUNT(scan_results.id) as completed").
		Joins("LEFT JOIN scan_results ON scan_results.dns_server_id = dns_servers.id AND scan_results.scan_run_id = ?", runID).
		Group("dns_servers.id, dns_servers.name").
		Order("dns_servers.name").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	entries := make([]ProgressEntry, len(rows))
	for i, r := range rows {
		entries[i] = ProgressEntry{DNSServerID: r.DNSServerID, Name: r.Name, Completed: r.Completed}
	}
	return entries, nil
}
