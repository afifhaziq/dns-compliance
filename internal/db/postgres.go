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
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u URL
		if err := tx.First(&u, id).Error; err != nil {
			return err
		}
		if err := tx.Where("url_value = ?", u.URL).Delete(&ScanResult{}).Error; err != nil {
			return err
		}
		return tx.Delete(&u).Error
	})
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

func (s *postgresStore) ResultsByURL(ctx context.Context, urlValue string) ([]ScanResult, error) {
	var results []ScanResult
	err := s.db.WithContext(ctx).
		Where("url_value = ?", urlValue).
		Preload("DNSServer").
		Order("scanned_at desc").
		Find(&results).Error
	return results, err
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
