package db

import (
	"context"
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
		Select("urls.id, urls.url, urls.created_at, du.enabled").
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
