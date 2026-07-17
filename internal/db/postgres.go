package db

import (
	"context"
	"sort"
	"time"

	"github.com/afif/dns-tracking/internal/urlnorm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type postgresStore struct{ db *gorm.DB }

func NewStore(db *gorm.DB) Store { return &postgresStore{db: db} }

func (s *postgresStore) ListURLs(ctx context.Context) ([]URL, error) {
	var urls []URL
	return urls, s.db.WithContext(ctx).Order("created_at asc").Find(&urls).Error
}

// CreateURL normalizes rawURL and gets-or-creates the row by normalized
// value, so the same domain added in different formats always resolves to
// one shared row instead of violating URL's unique index.
func (s *postgresStore) CreateURL(ctx context.Context, rawURL string) (URL, error) {
	normalized, err := urlnorm.Normalize(rawURL)
	if err != nil {
		return URL{}, err
	}
	var u URL
	err = s.db.WithContext(ctx).
		Where("url = ?", normalized).
		Attrs(URL{URL: normalized}).
		FirstOrCreate(&u).Error
	return u, err
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
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("dns_server_id = ?", id).Delete(&ScanResult{}).Error; err != nil {
			return err
		}
		return tx.Delete(&DNSServer{}, id).Error
	})
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

// LatestResultsForDepartment is the same query as LatestResults, scoped to
// URLs on the given department's watchlist.
func (s *postgresStore) LatestResultsForDepartment(ctx context.Context, departmentID uint) ([]ScanResult, error) {
	var results []ScanResult
	sub := s.db.Model(&ScanResult{}).
		Select("url_value, dns_server_id, MAX(scanned_at) as max_scanned_at").
		Group("url_value, dns_server_id")
	err := s.db.WithContext(ctx).
		Joins("JOIN (?) as latest ON scan_results.url_value = latest.url_value AND scan_results.dns_server_id = latest.dns_server_id AND scan_results.scanned_at = latest.max_scanned_at", sub).
		Joins("JOIN department_urls du ON du.url_id = scan_results.url_id AND du.department_id = ?", departmentID).
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
		dnsServerName       string
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

// Departments

func (s *postgresStore) ListDepartments(ctx context.Context) ([]Department, error) {
	var departments []Department
	return departments, s.db.WithContext(ctx).Order("name asc").Find(&departments).Error
}

func (s *postgresStore) CreateDepartment(ctx context.Context, name string) (Department, error) {
	d := Department{Name: name}
	return d, s.db.WithContext(ctx).Create(&d).Error
}

// Users

func (s *postgresStore) ListUsers(ctx context.Context) ([]User, error) {
	var users []User
	return users, s.db.WithContext(ctx).Preload("Department").Order("username asc").Find(&users).Error
}

func (s *postgresStore) CreateUser(ctx context.Context, u User) (User, error) {
	return u, s.db.WithContext(ctx).Create(&u).Error
}

func (s *postgresStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := s.db.WithContext(ctx).Preload("Department").Where("username = ?", username).First(&u).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *postgresStore) GetUserByID(ctx context.Context, id uint) (*User, error) {
	var u User
	err := s.db.WithContext(ctx).Preload("Department").First(&u, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *postgresStore) DeleteUser(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&User{}, id).Error
}

// Sessions

func (s *postgresStore) CreateSession(ctx context.Context, sess Session) error {
	return s.db.WithContext(ctx).Create(&sess).Error
}

func (s *postgresStore) GetSession(ctx context.Context, token string) (*Session, error) {
	var sess Session
	err := s.db.WithContext(ctx).Where("token = ? AND expires_at > ?", token, time.Now()).First(&sess).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *postgresStore) DeleteSession(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token = ?", token).Delete(&Session{}).Error
}

// Department watchlists

func (s *postgresStore) ListDepartmentURLs(ctx context.Context, departmentID uint) ([]URLEntry, error) {
	var entries []URLEntry
	err := s.db.WithContext(ctx).
		Table("urls").
		Select("urls.id, urls.url, urls.created_at, du.enabled, du.ordered_at").
		Joins("JOIN department_urls du ON du.url_id = urls.id AND du.department_id = ?", departmentID).
		Order("urls.created_at asc").
		Scan(&entries).Error
	return entries, err
}

// AddURLToWatchlist gets-or-creates the URL by normalized value, then links
// it to the department's watchlist. Re-adding an already-watched domain is a
// silent no-op (OnConflict DoNothing on the composite primary key).
func (s *postgresStore) AddURLToWatchlist(ctx context.Context, departmentID uint, rawURL string) (URL, error) {
	u, err := s.CreateURL(ctx, rawURL)
	if err != nil {
		return URL{}, err
	}
	link := DepartmentURL{DepartmentID: departmentID, URLID: u.ID}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error
	return u, err
}

// RemoveURLFromWatchlist deletes the DepartmentURL link only — it never
// touches URL or ScanResult, so scan history is preserved even once no
// department watches the domain anymore. Returns false if the URL wasn't
// actually on that department's watchlist (no row deleted).
func (s *postgresStore) RemoveURLFromWatchlist(ctx context.Context, departmentID, urlID uint) (bool, error) {
	res := s.db.WithContext(ctx).
		Where("department_id = ? AND url_id = ?", departmentID, urlID).
		Delete(&DepartmentURL{})
	return res.RowsAffected > 0, res.Error
}

func (s *postgresStore) SetURLEnabled(ctx context.Context, departmentID, urlID uint, enabled bool) (bool, error) {
	res := s.db.WithContext(ctx).
		Model(&DepartmentURL{}).
		Where("department_id = ? AND url_id = ?", departmentID, urlID).
		Update("enabled", enabled)
	return res.RowsAffected > 0, res.Error
}

// SetURLOrderedAt sets or clears (orderedAt == nil) the takedown-order date
// for one department's watchlist entry. Optional field — leaving it unset
// just excludes the domain from time-to-compliance aggregates.
func (s *postgresStore) SetURLOrderedAt(ctx context.Context, departmentID, urlID uint, orderedAt *time.Time) (bool, error) {
	res := s.db.WithContext(ctx).
		Model(&DepartmentURL{}).
		Where("department_id = ? AND url_id = ?", departmentID, urlID).
		Update("ordered_at", orderedAt)
	return res.RowsAffected > 0, res.Error
}

// ListWatchedURLs returns every URL enabled by at least one department —
// the set the scheduled/manual scan sweep should actually scan. A URL
// disabled by all watching departments is excluded.
func (s *postgresStore) ListWatchedURLs(ctx context.Context) ([]URL, error) {
	var urls []URL
	err := s.db.WithContext(ctx).
		Distinct().
		Joins("JOIN department_urls du ON du.url_id = urls.id AND du.enabled = true").
		Order("urls.created_at asc").
		Find(&urls).Error
	return urls, err
}

// ListUnassignedURLs returns URLs with no department watching them —
// pre-migration legacy rows, or domains every department has since removed.
// Admin-only view.
func (s *postgresStore) ListUnassignedURLs(ctx context.Context) ([]URL, error) {
	var urls []URL
	err := s.db.WithContext(ctx).
		Joins("LEFT JOIN department_urls du ON du.url_id = urls.id").
		Where("du.url_id IS NULL").
		Order("urls.created_at asc").
		Find(&urls).Error
	return urls, err
}

func (s *postgresStore) URLOwnedByDepartment(ctx context.Context, departmentID uint, urlValue string) (bool, error) {
	normalized, err := urlnorm.Normalize(urlValue)
	if err != nil {
		return false, err
	}
	var count int64
	err = s.db.WithContext(ctx).
		Model(&DepartmentURL{}).
		Joins("JOIN urls ON urls.id = department_urls.url_id").
		Where("department_urls.department_id = ? AND urls.url = ?", departmentID, normalized).
		Count(&count).Error
	return count > 0, err
}

func (s *postgresStore) CountDepartmentURLsSince(ctx context.Context, since time.Time) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&DepartmentURL{}).Where("created_at >= ?", since).Count(&count).Error
	return int(count), err
}

func (s *postgresStore) CountDepartmentURLsSinceForDepartment(ctx context.Context, since time.Time, departmentID uint) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&DepartmentURL{}).
		Where("department_id = ? AND created_at >= ?", departmentID, since).
		Count(&count).Error
	return int(count), err
}

func (s *postgresStore) ListCompliantIPs(ctx context.Context) ([]CompliantIP, error) {
	var ips []CompliantIP
	return ips, s.db.WithContext(ctx).Order("created_at asc").Find(&ips).Error
}

func (s *postgresStore) CreateCompliantIP(ctx context.Context, address, note string) (CompliantIP, error) {
	ip := CompliantIP{Address: address, Note: note}
	return ip, s.db.WithContext(ctx).Create(&ip).Error
}

func (s *postgresStore) DeleteCompliantIP(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&CompliantIP{}, id).Error
}

func (s *postgresStore) GetScanInterval(ctx context.Context) (int, error) {
	var settings ScanSettings
	if err := s.db.WithContext(ctx).First(&settings, 1).Error; err != nil {
		return 0, err
	}
	return settings.IntervalMinutes, nil
}

func (s *postgresStore) SetScanInterval(ctx context.Context, minutes int) error {
	return s.db.WithContext(ctx).
		Model(&ScanSettings{ID: 1}).
		Update("interval_minutes", minutes).Error
}

func (s *postgresStore) ISPStats(ctx context.Context, isp string) (ISPStatsResult, error) {
	// Subquery: latest scan per (url_value, dns_server_id)
	sub := s.db.Model(&ScanResult{}).
		Select("url_value, dns_server_id, MAX(scanned_at) as max_scanned_at").
		Group("url_value, dns_server_id")

	// Query 1: per-server compliance + latency stats
	type perServerRow struct {
		DNSServerID  uint
		Compliant    int
		Total        int
		AvgLatencyMs float64
		MinLatencyMs int64
		MaxLatencyMs int64
	}
	var rows []perServerRow
	err := s.db.WithContext(ctx).
		Table("scan_results").
		Select(`scan_results.dns_server_id,
            SUM(CASE WHEN scan_results.compliant = true THEN 1 ELSE 0 END) AS compliant,
            COUNT(*) AS total,
            AVG(CASE WHEN scan_results.latency_ms > 0 THEN scan_results.latency_ms END) AS avg_latency_ms,
            MIN(CASE WHEN scan_results.latency_ms > 0 THEN scan_results.latency_ms END) AS min_latency_ms,
            MAX(CASE WHEN scan_results.latency_ms > 0 THEN scan_results.latency_ms END) AS max_latency_ms`).
		Joins("JOIN (?) AS latest ON scan_results.url_value = latest.url_value AND scan_results.dns_server_id = latest.dns_server_id AND scan_results.scanned_at = latest.max_scanned_at", sub).
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Where("dns_servers.isp = ?", isp).
		Group("scan_results.dns_server_id").
		Scan(&rows).Error
	if err != nil {
		return ISPStatsResult{}, err
	}

	// Build DNS server map to avoid N+1 (use existing ListDNSServers)
	allServers, err := s.ListDNSServers(ctx)
	if err != nil {
		return ISPStatsResult{}, err
	}
	serverByID := make(map[uint]DNSServer, len(allServers))
	for _, srv := range allServers {
		serverByID[srv.ID] = srv
	}

	serverStats := make([]ISPServerStat, 0, len(rows))
	for _, row := range rows {
		serverStats = append(serverStats, ISPServerStat{
			DNSServer:    serverByID[row.DNSServerID],
			Compliant:    row.Compliant,
			Total:        row.Total,
			AvgLatencyMs: row.AvgLatencyMs,
			MinLatencyMs: row.MinLatencyMs,
			MaxLatencyMs: row.MaxLatencyMs,
		})
	}

	// Query 2: most violated domain
	type violationRow struct {
		URLValue       string
		ViolationCount int
	}
	var vrow violationRow
	if err := s.db.WithContext(ctx).
		Table("scan_results").
		Select("scan_results.url_value, COUNT(*) AS violation_count").
		Joins("JOIN (?) AS latest ON scan_results.url_value = latest.url_value AND scan_results.dns_server_id = latest.dns_server_id AND scan_results.scanned_at = latest.max_scanned_at", sub).
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Where("dns_servers.isp = ? AND scan_results.compliant = false", isp).
		Group("scan_results.url_value").
		Order("violation_count DESC").
		Limit(1).
		Scan(&vrow).Error; err != nil {
		return ISPStatsResult{}, err
	}

	return ISPStatsResult{
		ISP:                isp,
		Servers:            serverStats,
		MostViolatedDomain: vrow.URLValue,
	}, nil
}

func (s *postgresStore) ISPStatsForDepartment(ctx context.Context, isp string, departmentID uint) (ISPStatsResult, error) {
	// Subquery: latest scan per (url_value, dns_server_id)
	sub := s.db.Model(&ScanResult{}).
		Select("url_value, dns_server_id, MAX(scanned_at) as max_scanned_at").
		Group("url_value, dns_server_id")

	// Query 1: per-server compliance + latency stats, scoped to department's enabled watchlist
	type perServerRow struct {
		DNSServerID  uint
		Compliant    int
		Total        int
		AvgLatencyMs float64
		MinLatencyMs int64
		MaxLatencyMs int64
	}
	var rows []perServerRow
	err := s.db.WithContext(ctx).
		Table("scan_results").
		Select(`scan_results.dns_server_id,
            SUM(CASE WHEN scan_results.compliant = true THEN 1 ELSE 0 END) AS compliant,
            COUNT(*) AS total,
            AVG(CASE WHEN scan_results.latency_ms > 0 THEN scan_results.latency_ms END) AS avg_latency_ms,
            MIN(CASE WHEN scan_results.latency_ms > 0 THEN scan_results.latency_ms END) AS min_latency_ms,
            MAX(CASE WHEN scan_results.latency_ms > 0 THEN scan_results.latency_ms END) AS max_latency_ms`).
		Joins("JOIN (?) AS latest ON scan_results.url_value = latest.url_value AND scan_results.dns_server_id = latest.dns_server_id AND scan_results.scanned_at = latest.max_scanned_at", sub).
		Joins("JOIN department_urls ON department_urls.url_id = scan_results.url_id AND department_urls.department_id = ? AND department_urls.enabled = true", departmentID).
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Where("dns_servers.isp = ?", isp).
		Group("scan_results.dns_server_id").
		Scan(&rows).Error
	if err != nil {
		return ISPStatsResult{}, err
	}

	// Build DNS server map to avoid N+1
	allServers, err := s.ListDNSServers(ctx)
	if err != nil {
		return ISPStatsResult{}, err
	}
	serverByID := make(map[uint]DNSServer, len(allServers))
	for _, srv := range allServers {
		serverByID[srv.ID] = srv
	}

	serverStats := make([]ISPServerStat, 0, len(rows))
	for _, row := range rows {
		serverStats = append(serverStats, ISPServerStat{
			DNSServer:    serverByID[row.DNSServerID],
			Compliant:    row.Compliant,
			Total:        row.Total,
			AvgLatencyMs: row.AvgLatencyMs,
			MinLatencyMs: row.MinLatencyMs,
			MaxLatencyMs: row.MaxLatencyMs,
		})
	}

	// Query 2: most violated domain, scoped to department's enabled watchlist
	type violationRow struct {
		URLValue       string
		ViolationCount int
	}
	var vrow violationRow
	if err := s.db.WithContext(ctx).
		Table("scan_results").
		Select("scan_results.url_value, COUNT(*) AS violation_count").
		Joins("JOIN (?) AS latest ON scan_results.url_value = latest.url_value AND scan_results.dns_server_id = latest.dns_server_id AND scan_results.scanned_at = latest.max_scanned_at", sub).
		Joins("JOIN department_urls ON department_urls.url_id = scan_results.url_id AND department_urls.department_id = ? AND department_urls.enabled = true", departmentID).
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Where("dns_servers.isp = ? AND scan_results.compliant = false", isp).
		Group("scan_results.url_value").
		Order("violation_count DESC").
		Limit(1).
		Scan(&vrow).Error; err != nil {
		return ISPStatsResult{}, err
	}

	return ISPStatsResult{
		ISP:                isp,
		Servers:            serverStats,
		MostViolatedDomain: vrow.URLValue,
	}, nil
}

func (s *postgresStore) ISPTrend(ctx context.Context, isp string, since, until time.Time) ([]ISPTrendStat, error) {
	var rows []ISPTrendStat
	err := s.db.WithContext(ctx).
		Table("scan_results").
		Select(`TO_CHAR(scan_results.scanned_at, 'YYYY-MM-DD') AS day,
            COUNT(*) AS total,
            SUM(CASE WHEN scan_results.compliant = true THEN 1 ELSE 0 END) AS compliant`).
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Where("dns_servers.isp = ? AND scan_results.scanned_at >= ? AND scan_results.scanned_at <= ?", isp, since, until).
		Group("TO_CHAR(scan_results.scanned_at, 'YYYY-MM-DD')").
		Order("day").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *postgresStore) ISPTrendForDepartment(ctx context.Context, isp string, since, until time.Time, departmentID uint) ([]ISPTrendStat, error) {
	var rows []ISPTrendStat
	err := s.db.WithContext(ctx).
		Table("scan_results").
		Select(`TO_CHAR(scan_results.scanned_at, 'YYYY-MM-DD') AS day,
            COUNT(*) AS total,
            SUM(CASE WHEN scan_results.compliant = true THEN 1 ELSE 0 END) AS compliant`).
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Joins("JOIN department_urls ON department_urls.url_id = scan_results.url_id AND department_urls.department_id = ? AND department_urls.enabled = true", departmentID).
		Where("dns_servers.isp = ? AND scan_results.scanned_at >= ? AND scan_results.scanned_at <= ?", isp, since, until).
		Group("TO_CHAR(scan_results.scanned_at, 'YYYY-MM-DD')").
		Order("day").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// dailyTrend aggregates compliance across all ISPs into one bucket per
// calendar day, optionally scoped to one department's enabled watchlist.
// Aggregated in Go (not SQL, unlike ISPTrend's TO_CHAR) so it also works
// against the in-memory SQLite backend used by tests.
func (s *postgresStore) dailyTrend(ctx context.Context, since, until time.Time, departmentID *uint) ([]ISPTrendStat, error) {
	type row struct {
		ScannedAt time.Time
		Compliant bool
	}
	q := s.db.WithContext(ctx).
		Table("scan_results").
		Select("scan_results.scanned_at, scan_results.compliant").
		Where("scan_results.scanned_at >= ? AND scan_results.scanned_at <= ?", since, until)
	if departmentID != nil {
		q = q.Joins("JOIN department_urls ON department_urls.url_id = scan_results.url_id AND department_urls.department_id = ? AND department_urls.enabled = true", *departmentID)
	}
	var rows []row
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}

	type bucket struct{ total, compliant int }
	buckets := make(map[string]*bucket)
	order := make([]string, 0)
	for _, r := range rows {
		day := r.ScannedAt.Format("2006-01-02")
		b, ok := buckets[day]
		if !ok {
			b = &bucket{}
			buckets[day] = b
			order = append(order, day)
		}
		b.total++
		if r.Compliant {
			b.compliant++
		}
	}
	sort.Strings(order)

	stats := make([]ISPTrendStat, 0, len(order))
	for _, day := range order {
		b := buckets[day]
		stats = append(stats, ISPTrendStat{Day: day, Total: b.total, Compliant: b.compliant})
	}
	return stats, nil
}

func (s *postgresStore) NationalTrend(ctx context.Context, since, until time.Time) ([]ISPTrendStat, error) {
	return s.dailyTrend(ctx, since, until, nil)
}

func (s *postgresStore) NationalTrendForDepartment(ctx context.Context, since, until time.Time, departmentID uint) ([]ISPTrendStat, error) {
	return s.dailyTrend(ctx, since, until, &departmentID)
}

// ispComplianceTiming computes time-to-block stats for one ISP, optionally
// scoped to one department's watchlist. Aggregated in Go, following the same
// SQLite-portability reasoning as dailyTrend/DailyComplianceByURL.
func (s *postgresStore) ispComplianceTiming(ctx context.Context, isp string, departmentID *uint) (ISPTimingResult, error) {
	// Plain (non-aggregated) column read, reduced to a min-per-url_id map in
	// Go: SQLite's driver can't scan a SQL MIN() of a datetime column
	// directly into time.Time, so the reduction happens here instead.
	type deptURLOrderRow struct {
		URLID     uint
		OrderedAt time.Time
	}
	orderQuery := s.db.WithContext(ctx).
		Table("department_urls").
		Select("url_id, ordered_at").
		Where("ordered_at IS NOT NULL")
	if departmentID != nil {
		orderQuery = orderQuery.Where("department_id = ?", *departmentID)
	}
	var deptURLOrderRows []deptURLOrderRow
	if err := orderQuery.Scan(&deptURLOrderRows).Error; err != nil {
		return ISPTimingResult{}, err
	}
	orderedAtByURL := make(map[uint]time.Time, len(deptURLOrderRows))
	for _, r := range deptURLOrderRows {
		existing, ok := orderedAtByURL[r.URLID]
		if !ok || r.OrderedAt.Before(existing) {
			orderedAtByURL[r.URLID] = r.OrderedAt
		}
	}
	orderRows := make([]deptURLOrderRow, 0, len(orderedAtByURL))
	for urlID, orderedAt := range orderedAtByURL {
		orderRows = append(orderRows, deptURLOrderRow{URLID: urlID, OrderedAt: orderedAt})
	}

	// Total monitored domains in this scope — the denominator for the
	// "N domains have a recorded order date" coverage figure.
	totalQuery := s.db.WithContext(ctx).Table("department_urls").Select("COUNT(DISTINCT url_id)")
	if departmentID != nil {
		totalQuery = totalQuery.Where("department_id = ?", *departmentID)
	}
	var totalDomains int64
	if err := totalQuery.Scan(&totalDomains).Error; err != nil {
		return ISPTimingResult{}, err
	}

	// Compliant scans for this ISP, oldest first, so the first hit per url_id
	// found below is the first-observed-compliant timestamp.
	type complianceRow struct {
		URLID     uint
		URLValue  string
		ScannedAt time.Time
	}
	complianceQuery := s.db.WithContext(ctx).
		Table("scan_results").
		Select("scan_results.url_id, urls.url as url_value, scan_results.scanned_at").
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Joins("JOIN urls ON urls.id = scan_results.url_id").
		Where("dns_servers.isp = ? AND scan_results.compliant = true", isp).
		Order("scan_results.scanned_at asc")
	if departmentID != nil {
		complianceQuery = complianceQuery.Joins("JOIN department_urls du2 ON du2.url_id = scan_results.url_id AND du2.department_id = ?", *departmentID)
	}
	var complianceRows []complianceRow
	if err := complianceQuery.Scan(&complianceRows).Error; err != nil {
		return ISPTimingResult{}, err
	}

	firstCompliantAfterOrder := make(map[uint]time.Time)
	domainNameByURL := make(map[uint]string)
	for _, r := range complianceRows {
		domainNameByURL[r.URLID] = r.URLValue
		orderedAt, hasOrder := orderedAtByURL[r.URLID]
		if !hasOrder {
			continue
		}
		if _, already := firstCompliantAfterOrder[r.URLID]; already {
			continue
		}
		if r.ScannedAt.Before(orderedAt) {
			continue // compliant scan predates the recorded order — not this order's block event
		}
		firstCompliantAfterOrder[r.URLID] = r.ScannedAt
	}

	// Domain names for ordered URLs that have no compliant scan yet (still open).
	var missingIDs []uint
	for _, r := range orderRows {
		if _, known := domainNameByURL[r.URLID]; !known {
			missingIDs = append(missingIDs, r.URLID)
		}
	}
	if len(missingIDs) > 0 {
		var missing []URL
		if err := s.db.WithContext(ctx).Where("id IN ?", missingIDs).Find(&missing).Error; err != nil {
			return ISPTimingResult{}, err
		}
		for _, u := range missing {
			domainNameByURL[u.ID] = u.URL
		}
	}

	now := time.Now()
	timings := make([]DomainTiming, 0, len(orderRows))
	var blockedDays []float64
	for _, r := range orderRows {
		domain := domainNameByURL[r.URLID]
		if firstCompliant, blocked := firstCompliantAfterOrder[r.URLID]; blocked {
			days := firstCompliant.Sub(r.OrderedAt).Hours() / 24
			if days < 0 {
				days = 0 // order recorded after the domain was already observed compliant
			}
			timings = append(timings, DomainTiming{Domain: domain, DaysToBlock: int(days + 0.5), Blocked: true})
			blockedDays = append(blockedDays, days)
		} else {
			waited := now.Sub(r.OrderedAt).Hours() / 24
			if waited < 0 {
				waited = 0
			}
			timings = append(timings, DomainTiming{Domain: domain, DaysToBlock: int(waited + 0.5), Blocked: false})
		}
	}

	sort.Slice(timings, func(i, j int) bool { return timings[i].DaysToBlock > timings[j].DaysToBlock })
	slowest := timings
	if len(slowest) > 5 {
		slowest = slowest[:5]
	}

	sort.Float64s(blockedDays)
	var median, avg float64
	if n := len(blockedDays); n > 0 {
		if n%2 == 1 {
			median = blockedDays[n/2]
		} else {
			median = (blockedDays[n/2-1] + blockedDays[n/2]) / 2
		}
		sum := 0.0
		for _, d := range blockedDays {
			sum += d
		}
		avg = sum / float64(n)
	}

	return ISPTimingResult{
		ISP:                isp,
		MedianDaysToBlock:  median,
		AvgDaysToBlock:     avg,
		BlockedCount:       len(blockedDays),
		StillOpenCount:     len(orderRows) - len(blockedDays),
		WithOrderDateCount: len(orderRows),
		TotalDomains:       int(totalDomains),
		Slowest:            slowest,
	}, nil
}

func (s *postgresStore) ISPComplianceTiming(ctx context.Context, isp string) (ISPTimingResult, error) {
	return s.ispComplianceTiming(ctx, isp, nil)
}

func (s *postgresStore) ISPComplianceTimingForDepartment(ctx context.Context, isp string, departmentID uint) (ISPTimingResult, error) {
	return s.ispComplianceTiming(ctx, isp, &departmentID)
}

// UpsertDomainWhois inserts or replaces the cached RDAP row for a domain —
// there's only ever one row per URLID (its primary key), so a re-fetch just
// overwrites the previous result.
func (s *postgresStore) UpsertDomainWhois(ctx context.Context, w DomainWhois) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "url_id"}}, UpdateAll: true}).
		Create(&w).Error
}

// GetURLByValue looks up a URL row by normalized value. Returns nil, nil
// (not an error) if urlValue doesn't match any known URL.
func (s *postgresStore) GetURLByValue(ctx context.Context, urlValue string) (*URL, error) {
	normalized, err := urlnorm.Normalize(urlValue)
	if err != nil {
		return nil, err
	}
	var u URL
	if err := s.db.WithContext(ctx).Where("url = ?", normalized).First(&u).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// GetDomainWhois looks up the cached RDAP row for urlValue. Returns
// nil, nil (not an error) both when the URL is unknown and when it's known
// but has never been fetched — callers can't distinguish the two, which
// matches this being a read-only cache lookup, not an existence check.
func (s *postgresStore) GetDomainWhois(ctx context.Context, urlValue string) (*DomainWhois, error) {
	u, err := s.GetURLByValue(ctx, urlValue)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	var w DomainWhois
	err = s.db.WithContext(ctx).Where("url_id = ?", u.ID).First(&w).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// GetSubdomainScan looks up the cached subfinder result for urlValue.
// Returns nil, nil (not an error) both when the URL is unknown and when
// it's known but never fetched — same semantics as GetDomainWhois.
func (s *postgresStore) GetSubdomainScan(ctx context.Context, urlValue string) (*SubdomainScan, error) {
	u, err := s.GetURLByValue(ctx, urlValue)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	var scan SubdomainScan
	err = s.db.WithContext(ctx).Where("url_id = ?", u.ID).First(&scan).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &scan, nil
}

// UpsertSubdomainScan inserts or replaces the cached subfinder result for a
// domain — one row per URLID (its primary key), so a re-fetch overwrites
// the previous result.
func (s *postgresStore) UpsertSubdomainScan(ctx context.Context, scan SubdomainScan) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "url_id"}}, UpdateAll: true}).
		Create(&scan).Error
}

// ListStaleDomains returns watched URLs (the same set ListWatchedURLs scans)
// that either have no DomainWhois row yet or whose last fetch predates
// olderThan — the refresher's work queue, capped by limit per tick.
func (s *postgresStore) ListStaleDomains(ctx context.Context, olderThan time.Time, limit int) ([]URL, error) {
	var urls []URL
	err := s.db.WithContext(ctx).
		Distinct("urls.*").
		Joins("JOIN department_urls du ON du.url_id = urls.id AND du.enabled = true").
		Joins("LEFT JOIN domain_whois dw ON dw.url_id = urls.id").
		Where("dw.url_id IS NULL OR dw.last_fetched_at < ?", olderThan).
		Order("urls.created_at asc").
		Limit(limit).
		Find(&urls).Error
	return urls, err
}

// GetIPInfo looks up the cached ASN/org row for ip. Returns nil, nil (not an
// error) when the IP has never been fetched.
func (s *postgresStore) GetIPInfo(ctx context.Context, ip string) (*IPInfo, error) {
	var info IPInfo
	err := s.db.WithContext(ctx).Where("ip = ?", ip).First(&info).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// UpsertIPInfo inserts or replaces the cached row for an IP — there's only
// ever one row per IP (its primary key), so a re-fetch just overwrites it.
func (s *postgresStore) UpsertIPInfo(ctx context.Context, info IPInfo) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "ip"}}, UpdateAll: true}).
		Create(&info).Error
}

// GetFavicon looks up the cached favicon for domain. Returns nil, nil (not
// an error) when the domain has never been fetched.
func (s *postgresStore) GetFavicon(ctx context.Context, domain string) (*Favicon, error) {
	var fav Favicon
	err := s.db.WithContext(ctx).Where("domain = ?", domain).First(&fav).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &fav, nil
}

// UpsertFavicon inserts or replaces the cached row for a domain — there's
// only ever one row per domain (its primary key), so a re-fetch just
// overwrites it.
func (s *postgresStore) UpsertFavicon(ctx context.Context, fav Favicon) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "domain"}}, UpdateAll: true}).
		Create(&fav).Error
}
