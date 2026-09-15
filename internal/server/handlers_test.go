package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/server"
	"github.com/afif/dns-tracking/internal/urlnorm"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

// fullMockStore implements db.Store completely for handler tests.
type fullMockStore struct {
	urls               []db.URL
	dnsServers         []db.DNSServer
	results            []db.ScanResult
	activeRun          *db.ScanRun
	lastRun            *db.ScanRun
	progress           []db.ProgressEntry
	departments        []db.Department
	users              []db.User
	sessions           []db.Session
	departmentURLs     []db.DepartmentURL
	domainWhois        []db.DomainWhois
	ipInfo             []db.IPInfo
	favicons           []db.Favicon
	subdomainScans     []db.SubdomainScan
	ispLogos           []db.ISPLogo
	gridPrefs          []db.GridPreference
	agencies           []db.Agency
	recipients         []db.Recipient
	requestors         []db.Requestor
	dueDatePresets     []db.DueDatePreset
	instruments        []db.Instrument
	citations          []db.Citation
	categories         []db.Category
	elements           []db.Element
	subElements        []db.SubElement
	urlOffences        []db.URLOffence
	notifications      []db.Notification
	cases              []db.Case
	caseURLs           []db.CaseURL
	caseLetters        []db.CaseLetter
	scheduleMu         sync.Mutex // guards the fields below; the scheduler goroutine reads them concurrently with test/handler writes
	scanInterval       int
	scanEnabled        bool
	dnsWorkers         int
	slaInterval        int
	slaStreakThreshold int
}

func (m *fullMockStore) ListURLs(_ context.Context) ([]db.URL, error) { return m.urls, nil }

// CreateURL mirrors postgresStore's get-or-create-by-normalized-value
// behavior so mock-store-backed tests exercise the same dedup semantics.
func (m *fullMockStore) CreateURL(_ context.Context, rawURL string) (db.URL, error) {
	normalized, err := urlnorm.Normalize(rawURL)
	if err != nil {
		return db.URL{}, err
	}
	for _, u := range m.urls {
		if u.URL == normalized {
			return u, nil
		}
	}
	u := db.URL{ID: uint(len(m.urls) + 1), URL: normalized, CreatedAt: time.Now()}
	m.urls = append(m.urls, u)
	return u, nil
}
func (m *fullMockStore) DeleteURL(_ context.Context, id uint) error {
	for i, u := range m.urls {
		if u.ID == id {
			m.urls = append(m.urls[:i], m.urls[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *fullMockStore) ListDNSServers(_ context.Context) ([]db.DNSServer, error) {
	return m.dnsServers, nil
}
func (m *fullMockStore) ListEnabledDNSServers(_ context.Context) ([]db.DNSServer, error) {
	var enabled []db.DNSServer
	for _, s := range m.dnsServers {
		if s.Enabled {
			enabled = append(enabled, s)
		}
	}
	return enabled, nil
}
func (m *fullMockStore) SetDNSServerEnabled(_ context.Context, id uint, enabled bool) error {
	for i, s := range m.dnsServers {
		if s.ID == id {
			m.dnsServers[i].Enabled = enabled
			return nil
		}
	}
	return fmt.Errorf("dns server %d not found", id)
}
func (m *fullMockStore) CreateDNSServer(_ context.Context, s db.DNSServer) (db.DNSServer, error) {
	s.ID = uint(len(m.dnsServers) + 1)
	m.dnsServers = append(m.dnsServers, s)
	return s, nil
}
func (m *fullMockStore) UpdateDNSServer(_ context.Context, id uint, s db.DNSServer) (db.DNSServer, error) {
	for i, existing := range m.dnsServers {
		if existing.ID == id {
			s.ID = id
			m.dnsServers[i] = s
			return s, nil
		}
	}
	return db.DNSServer{}, fmt.Errorf("dns server %d not found", id)
}
func (m *fullMockStore) DeleteDNSServer(_ context.Context, id uint) error {
	for i, s := range m.dnsServers {
		if s.ID == id {
			m.dnsServers = append(m.dnsServers[:i], m.dnsServers[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *fullMockStore) CreateScanRun(_ context.Context, by string) (db.ScanRun, error) {
	return db.ScanRun{ID: 1, TriggeredBy: by, Status: "running", StartedAt: time.Now()}, nil
}
func (m *fullMockStore) CompleteScanRun(_ context.Context, _ uint, _ string, _ time.Time) error {
	return nil
}
func (m *fullMockStore) ActiveScanRun(_ context.Context) (*db.ScanRun, error) {
	return m.activeRun, nil
}
func (m *fullMockStore) LastScanRun(_ context.Context) (*db.ScanRun, error) {
	return m.lastRun, nil
}
func (m *fullMockStore) ScanProgress(_ context.Context, _ uint) ([]db.ProgressEntry, error) {
	return m.progress, nil
}
func (m *fullMockStore) LatestResults(_ context.Context) ([]db.ScanResult, error) {
	return m.results, nil
}
func (m *fullMockStore) ResultsByURL(_ context.Context, u string, since, until time.Time) ([]db.ScanResult, error) {
	var out []db.ScanResult
	for _, r := range m.results {
		if r.URLValue != u || r.ScannedAt.Before(since) {
			continue
		}
		if !until.IsZero() && r.ScannedAt.After(until) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
func (m *fullMockStore) DailyComplianceByURL(_ context.Context, u string, since, until time.Time) ([]db.DailyComplianceStat, error) {
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
	for _, r := range m.results {
		if r.URLValue != u || r.ScannedAt.Before(since) {
			continue
		}
		if !until.IsZero() && r.ScannedAt.After(until) {
			continue
		}
		k := bucketKey{dnsServerID: r.DNSServerID, day: r.ScannedAt.Format("2006-01-02")}
		b, ok := buckets[k]
		if !ok {
			b = &bucket{dnsServerName: r.DNSServer.Name}
			buckets[k] = b
			order = append(order, k)
		}
		b.total++
		if r.Compliant {
			b.compliantSum++
		}
	}
	out := make([]db.DailyComplianceStat, 0, len(order))
	for _, k := range order {
		b := buckets[k]
		out = append(out, db.DailyComplianceStat{
			DNSServerID:   k.dnsServerID,
			DNSServerName: b.dnsServerName,
			Day:           k.day,
			Total:         b.total,
			Compliant:     b.compliantSum,
			Level:         db.DailyComplianceLevel(b.total, b.compliantSum),
		})
	}
	return out, nil
}

func (m *fullMockStore) InsertResult(_ context.Context, r db.ScanResult) error {
	m.results = append(m.results, r)
	return nil
}
func (m *fullMockStore) UpdateScreenshot(_ context.Context, _ uint, _ string) error { return nil }

func (m *fullMockStore) LatestResultsForDepartment(ctx context.Context, departmentID uint) ([]db.ScanResult, error) {
	watchedIDs := make(map[uint]bool)
	for _, du := range m.departmentURLs {
		if du.DepartmentID == departmentID {
			watchedIDs[du.URLID] = true
		}
	}
	var out []db.ScanResult
	for _, r := range m.results {
		if watchedIDs[r.URLID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *fullMockStore) ListDepartments(_ context.Context) ([]db.Department, error) {
	return m.departments, nil
}
func (m *fullMockStore) CreateDepartment(_ context.Context, name string) (db.Department, error) {
	d := db.Department{ID: uint(len(m.departments) + 1), Name: name, CreatedAt: time.Now()}
	m.departments = append(m.departments, d)
	return d, nil
}
func (m *fullMockStore) UpdateDepartment(_ context.Context, id uint, name string) (db.Department, error) {
	for i, d := range m.departments {
		if d.ID == id {
			m.departments[i].Name = name
			return m.departments[i], nil
		}
	}
	return db.Department{}, nil
}

func (m *fullMockStore) ListUsers(_ context.Context) ([]db.User, error) { return m.users, nil }
func (m *fullMockStore) CreateUser(_ context.Context, u db.User) (db.User, error) {
	u.ID = uint(len(m.users) + 1)
	u.CreatedAt = time.Now()
	m.users = append(m.users, u)
	return u, nil
}
func (m *fullMockStore) UpdateUser(_ context.Context, id uint, u db.User) (db.User, error) {
	for i, existing := range m.users {
		if existing.ID == id {
			u.ID = id
			u.CreatedAt = existing.CreatedAt
			m.users[i] = u
			return u, nil
		}
	}
	return db.User{}, nil
}
func (m *fullMockStore) SetUserPassword(_ context.Context, id uint, hash string, mustChange bool) error {
	for i, u := range m.users {
		if u.ID == id {
			m.users[i].PasswordHash = hash
			m.users[i].MustChangePassword = mustChange
			return nil
		}
	}
	return nil
}
func (m *fullMockStore) GetUserByUsername(_ context.Context, username string) (*db.User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return &u, nil
		}
	}
	return nil, nil
}
func (m *fullMockStore) GetUserByID(_ context.Context, id uint) (*db.User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return &u, nil
		}
	}
	return nil, nil
}
func (m *fullMockStore) DeleteUser(_ context.Context, id uint) error {
	for i, u := range m.users {
		if u.ID == id {
			m.users = append(m.users[:i], m.users[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) CreateSession(_ context.Context, s db.Session) error {
	m.sessions = append(m.sessions, s)
	return nil
}
func (m *fullMockStore) GetSession(_ context.Context, token string) (*db.Session, error) {
	for _, s := range m.sessions {
		if s.Token == token && s.ExpiresAt.After(time.Now()) {
			return &s, nil
		}
	}
	return nil, nil
}
func (m *fullMockStore) DeleteSession(_ context.Context, token string) error {
	for i, s := range m.sessions {
		if s.Token == token {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *fullMockStore) DeleteExpiredSessions(_ context.Context) (int64, error) {
	var kept []db.Session
	var n int64
	now := time.Now()
	for _, s := range m.sessions {
		if s.ExpiresAt.After(now) {
			kept = append(kept, s)
		} else {
			n++
		}
	}
	m.sessions = kept
	return n, nil
}

// latestCaseForURL returns urlID's most-recently-created Case (mirroring
// postgresStore.ListDepartmentURLs' correlated subquery), or nil if it has
// none. m.cases is scanned in insertion order, which — since every test
// seeds cases in chronological order and the mock's CreateCase appends —
// is an adequate proxy for "most recently created" without needing a real
// CreatedAt clock.
func (m *fullMockStore) latestCaseForURL(urlID uint) *db.Case {
	var latest *db.Case
	for i := range m.cases {
		c := &m.cases[i]
		for _, cu := range m.caseURLs {
			if cu.CaseID == c.ID && cu.URLID == urlID {
				latest = c
			}
		}
	}
	return latest
}

func (m *fullMockStore) ListDepartmentURLs(_ context.Context, departmentID uint) ([]db.URLEntry, error) {
	var out []db.URLEntry
	for _, du := range m.departmentURLs {
		if du.DepartmentID != departmentID {
			continue
		}
		for _, u := range m.urls {
			if u.ID == du.URLID {
				entry := db.URLEntry{ID: u.ID, URL: u.URL, Enabled: du.Enabled, CreatedAt: u.CreatedAt}
				if c := m.latestCaseForURL(u.ID); c != nil {
					entry.DueDate = c.DueDate
					entry.RequestedAt = c.RequestedAt
					for _, cu := range m.caseURLs {
						if cu.CaseID == c.ID && cu.URLID == u.ID {
							entry.Status = cu.Status
							if cu.AgencyID != nil {
								entry.AgencyID = cu.AgencyID
								for _, a := range m.agencies {
									if a.ID == *cu.AgencyID {
										entry.AgencyName = a.Name
									}
								}
							}
						}
					}
				}
				out = append(out, entry)
			}
		}
	}
	return out, nil
}

func (m *fullMockStore) AddURLToWatchlist(ctx context.Context, departmentID uint, rawURL string) (db.URL, error) {
	u, err := m.CreateURL(ctx, rawURL)
	if err != nil {
		return db.URL{}, err
	}
	for _, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == u.ID {
			return u, nil // already linked — no-op
		}
	}
	m.departmentURLs = append(m.departmentURLs, db.DepartmentURL{DepartmentID: departmentID, URLID: u.ID, Enabled: true, CreatedAt: time.Now()})
	return u, nil
}

func (m *fullMockStore) SetURLEnabled(_ context.Context, departmentID, urlID uint, enabled bool) (bool, error) {
	for i, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == urlID {
			m.departmentURLs[i].Enabled = enabled
			return true, nil
		}
	}
	return false, nil
}

func (m *fullMockStore) ListAgencies(_ context.Context) ([]db.Agency, error) {
	return m.agencies, nil
}

func (m *fullMockStore) CreateAgency(_ context.Context, name string) (db.Agency, error) {
	a := db.Agency{ID: uint(len(m.agencies) + 1), Name: name, CreatedAt: time.Now()}
	m.agencies = append(m.agencies, a)
	return a, nil
}

func (m *fullMockStore) UpdateAgency(_ context.Context, id uint, name string) (db.Agency, error) {
	for i, a := range m.agencies {
		if a.ID == id {
			m.agencies[i].Name = name
			return m.agencies[i], nil
		}
	}
	return db.Agency{}, nil
}

func (m *fullMockStore) DeleteAgency(_ context.Context, id uint) error {
	for i, a := range m.agencies {
		if a.ID == id {
			m.agencies = append(m.agencies[:i], m.agencies[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) ListRecipients(_ context.Context) ([]db.Recipient, error) {
	return m.recipients, nil
}

func (m *fullMockStore) CreateRecipient(_ context.Context, name string) (db.Recipient, error) {
	r := db.Recipient{ID: uint(len(m.recipients) + 1), Name: name, CreatedAt: time.Now()}
	m.recipients = append(m.recipients, r)
	return r, nil
}

func (m *fullMockStore) UpdateRecipient(_ context.Context, id uint, name string) (db.Recipient, error) {
	for i, r := range m.recipients {
		if r.ID == id {
			m.recipients[i].Name = name
			return m.recipients[i], nil
		}
	}
	return db.Recipient{}, nil
}

func (m *fullMockStore) DeleteRecipient(_ context.Context, id uint) error {
	for i, r := range m.recipients {
		if r.ID == id {
			m.recipients = append(m.recipients[:i], m.recipients[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) ListRequestors(_ context.Context) ([]db.Requestor, error) {
	return m.requestors, nil
}

func (m *fullMockStore) CreateRequestor(_ context.Context, name string) (db.Requestor, error) {
	r := db.Requestor{ID: uint(len(m.requestors) + 1), Name: name, CreatedAt: time.Now()}
	m.requestors = append(m.requestors, r)
	return r, nil
}

func (m *fullMockStore) UpdateRequestor(_ context.Context, id uint, name string) (db.Requestor, error) {
	for i, r := range m.requestors {
		if r.ID == id {
			m.requestors[i].Name = name
			return m.requestors[i], nil
		}
	}
	return db.Requestor{}, nil
}

func (m *fullMockStore) DeleteRequestor(_ context.Context, id uint) error {
	for i, r := range m.requestors {
		if r.ID == id {
			m.requestors = append(m.requestors[:i], m.requestors[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) ListDueDatePresets(_ context.Context) ([]db.DueDatePreset, error) {
	return m.dueDatePresets, nil
}

func (m *fullMockStore) CreateDueDatePreset(_ context.Context, label string, minutes int) (db.DueDatePreset, error) {
	p := db.DueDatePreset{ID: uint(len(m.dueDatePresets) + 1), Label: label, Minutes: minutes, CreatedAt: time.Now()}
	m.dueDatePresets = append(m.dueDatePresets, p)
	return p, nil
}

func (m *fullMockStore) DeleteDueDatePreset(_ context.Context, id uint) error {
	for i, p := range m.dueDatePresets {
		if p.ID == id {
			m.dueDatePresets = append(m.dueDatePresets[:i], m.dueDatePresets[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) RemoveURLFromWatchlist(_ context.Context, departmentID, urlID uint) (bool, error) {
	for i, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == urlID {
			m.departmentURLs = append(m.departmentURLs[:i], m.departmentURLs[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *fullMockStore) ListWatchedURLs(_ context.Context) ([]db.URL, error) {
	watchedIDs := make(map[uint]bool)
	for _, du := range m.departmentURLs {
		if du.Enabled {
			watchedIDs[du.URLID] = true
		}
	}
	var out []db.URL
	for _, u := range m.urls {
		if watchedIDs[u.ID] {
			out = append(out, u)
		}
	}
	return out, nil
}

func (m *fullMockStore) ListUnassignedURLs(_ context.Context) ([]db.URL, error) {
	watchedIDs := make(map[uint]bool)
	for _, du := range m.departmentURLs {
		watchedIDs[du.URLID] = true
	}
	var out []db.URL
	for _, u := range m.urls {
		if !watchedIDs[u.ID] {
			out = append(out, u)
		}
	}
	return out, nil
}

func (m *fullMockStore) URLOwnedByDepartment(_ context.Context, departmentID uint, urlValue string) (bool, error) {
	normalized, err := urlnorm.Normalize(urlValue)
	if err != nil {
		return false, err
	}
	var urlID uint
	found := false
	for _, u := range m.urls {
		if u.URL == normalized {
			urlID = u.ID
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	for _, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == urlID {
			return true, nil
		}
	}
	return false, nil
}

func (m *fullMockStore) DepartmentIDsWatchingURL(_ context.Context, urlID uint) ([]uint, error) {
	var ids []uint
	for _, du := range m.departmentURLs {
		if du.URLID == urlID {
			ids = append(ids, du.DepartmentID)
		}
	}
	return ids, nil
}

func (m *fullMockStore) CountDepartmentURLsSince(_ context.Context, since time.Time) (int, error) {
	count := 0
	for _, du := range m.departmentURLs {
		if !du.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (m *fullMockStore) CountDepartmentURLsSinceForDepartment(_ context.Context, since time.Time, departmentID uint) (int, error) {
	count := 0
	for _, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && !du.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (m *fullMockStore) ListCompliantIPs(_ context.Context) ([]db.CompliantIP, error) {
	return nil, nil
}
func (m *fullMockStore) CreateCompliantIP(_ context.Context, address, note string) (db.CompliantIP, error) {
	return db.CompliantIP{Address: address, Note: note}, nil
}
func (m *fullMockStore) DeleteCompliantIP(_ context.Context, _ uint) error { return nil }

func (m *fullMockStore) ListISPLogos(_ context.Context) ([]db.ISPLogo, error) {
	return m.ispLogos, nil
}

func (m *fullMockStore) UpsertISPLogo(_ context.Context, isp, logoURL string) (db.ISPLogo, error) {
	for i, l := range m.ispLogos {
		if l.ISP == isp {
			m.ispLogos[i].LogoURL = logoURL
			return m.ispLogos[i], nil
		}
	}
	logo := db.ISPLogo{ISP: isp, LogoURL: logoURL, CreatedAt: time.Now()}
	m.ispLogos = append(m.ispLogos, logo)
	return logo, nil
}

func (m *fullMockStore) DeleteISPLogo(_ context.Context, isp string) error {
	for i, l := range m.ispLogos {
		if l.ISP == isp {
			m.ispLogos = append(m.ispLogos[:i], m.ispLogos[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) GetGridPreference(_ context.Context, userID uint, gridKey string) (*db.GridPreference, error) {
	for _, p := range m.gridPrefs {
		if p.UserID == userID && p.GridKey == gridKey {
			return &p, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) SaveGridPreference(_ context.Context, pref db.GridPreference) (db.GridPreference, error) {
	for i, p := range m.gridPrefs {
		if p.UserID == pref.UserID && p.GridKey == pref.GridKey {
			m.gridPrefs[i] = pref
			return pref, nil
		}
	}
	m.gridPrefs = append(m.gridPrefs, pref)
	return pref, nil
}

func (m *fullMockStore) ListInstruments(_ context.Context) ([]db.Instrument, error) {
	return m.instruments, nil
}
func (m *fullMockStore) GetOrCreateInstrument(_ context.Context, in db.Instrument) (db.Instrument, error) {
	for _, existing := range m.instruments {
		sameYear := (existing.Year == nil) == (in.Year == nil) && (existing.Year == nil || *existing.Year == *in.Year)
		if existing.Type == in.Type && existing.Jurisdiction == in.Jurisdiction && existing.Number == in.Number && sameYear {
			return existing, nil
		}
	}
	in.ID = uint(len(m.instruments) + 1)
	m.instruments = append(m.instruments, in)
	return in, nil
}
func (m *fullMockStore) UpdateInstrument(_ context.Context, id uint, in db.Instrument) (db.Instrument, error) {
	in.ID = id
	for i, existing := range m.instruments {
		if existing.ID == id {
			m.instruments[i] = in
			return in, nil
		}
	}
	return in, nil
}
func (m *fullMockStore) DeleteInstrument(_ context.Context, id uint) error {
	for i, in := range m.instruments {
		if in.ID == id {
			m.instruments = append(m.instruments[:i], m.instruments[i+1:]...)
		}
	}
	for _, c := range m.citations {
		if c.InstrumentID == id {
			_ = m.deleteCitationCascade(c.ID)
		}
	}
	return nil
}

func (m *fullMockStore) ListCitationsByInstrument(_ context.Context, instrumentID uint) ([]db.Citation, error) {
	var out []db.Citation
	for _, c := range m.citations {
		if c.InstrumentID == instrumentID {
			out = append(out, c)
		}
	}
	return out, nil
}
func (m *fullMockStore) CreateCitation(_ context.Context, c db.Citation) (db.Citation, error) {
	c.ID = uint(len(m.citations) + 1)
	c.SortKey = db.BuildProvisionSortKey(c.Parsed.ProvisionNum, c.Parsed.ProvisionSuffix)
	m.citations = append(m.citations, c)
	return c, nil
}
func (m *fullMockStore) UpdateCitation(_ context.Context, id uint, c db.Citation) (db.Citation, error) {
	c.ID = id
	c.SortKey = db.BuildProvisionSortKey(c.Parsed.ProvisionNum, c.Parsed.ProvisionSuffix)
	for i, existing := range m.citations {
		if existing.ID == id {
			m.citations[i] = c
			return c, nil
		}
	}
	return c, nil
}
func (m *fullMockStore) DeleteCitation(_ context.Context, id uint) error {
	return m.deleteCitationCascade(id)
}
func (m *fullMockStore) deleteCitationCascade(id uint) error {
	for i, c := range m.citations {
		if c.ID == id {
			m.citations = append(m.citations[:i], m.citations[i+1:]...)
		}
	}
	for _, cat := range m.categories {
		if cat.CitationID == id {
			_ = m.deleteCategoryCascade(cat.ID)
		}
	}
	return nil
}

func (m *fullMockStore) ListCategoriesByCitation(_ context.Context, citationID uint) ([]db.Category, error) {
	var out []db.Category
	for _, c := range m.categories {
		if c.CitationID == citationID {
			out = append(out, c)
		}
	}
	return out, nil
}
func (m *fullMockStore) CreateCategory(_ context.Context, cat db.Category) (db.Category, error) {
	cat.ID = uint(len(m.categories) + 1)
	m.categories = append(m.categories, cat)
	return cat, nil
}
func (m *fullMockStore) UpdateCategory(_ context.Context, id uint, name string) (db.Category, error) {
	for i, cat := range m.categories {
		if cat.ID == id {
			m.categories[i].Name = name
			return m.categories[i], nil
		}
	}
	return db.Category{}, nil
}
func (m *fullMockStore) DeleteCategory(_ context.Context, id uint) error {
	return m.deleteCategoryCascade(id)
}
func (m *fullMockStore) deleteCategoryCascade(id uint) error {
	for i, cat := range m.categories {
		if cat.ID == id {
			m.categories = append(m.categories[:i], m.categories[i+1:]...)
		}
	}
	for _, el := range m.elements {
		if el.CategoryID == id {
			m.deleteElementCascade(el.ID)
		}
	}
	return nil
}

func (m *fullMockStore) ListElementsByCategory(_ context.Context, categoryID uint) ([]db.Element, error) {
	var out []db.Element
	for _, el := range m.elements {
		if el.CategoryID == categoryID {
			out = append(out, el)
		}
	}
	return out, nil
}
func (m *fullMockStore) CreateElement(_ context.Context, el db.Element) (db.Element, error) {
	el.ID = uint(len(m.elements) + 1)
	m.elements = append(m.elements, el)
	return el, nil
}
func (m *fullMockStore) UpdateElement(_ context.Context, id uint, name string) (db.Element, error) {
	for i, el := range m.elements {
		if el.ID == id {
			m.elements[i].Name = name
			return m.elements[i], nil
		}
	}
	return db.Element{}, nil
}
func (m *fullMockStore) DeleteElement(_ context.Context, id uint) error {
	m.deleteElementCascade(id)
	return nil
}
func (m *fullMockStore) deleteElementCascade(id uint) {
	for i, el := range m.elements {
		if el.ID == id {
			m.elements = append(m.elements[:i], m.elements[i+1:]...)
		}
	}
	for _, se := range m.subElements {
		if se.ElementID == id {
			m.deleteSubElementCascade(se.ID)
		}
	}
	for i := 0; i < len(m.urlOffences); i++ {
		if m.urlOffences[i].ElementID != nil && *m.urlOffences[i].ElementID == id {
			m.urlOffences = append(m.urlOffences[:i], m.urlOffences[i+1:]...)
			i--
		}
	}
}

func (m *fullMockStore) ListSubElementsByElement(_ context.Context, elementID uint) ([]db.SubElement, error) {
	var out []db.SubElement
	for _, se := range m.subElements {
		if se.ElementID == elementID {
			out = append(out, se)
		}
	}
	return out, nil
}
func (m *fullMockStore) CreateSubElement(_ context.Context, se db.SubElement) (db.SubElement, error) {
	se.ID = uint(len(m.subElements) + 1)
	m.subElements = append(m.subElements, se)
	return se, nil
}
func (m *fullMockStore) UpdateSubElement(_ context.Context, id uint, name string) (db.SubElement, error) {
	for i, se := range m.subElements {
		if se.ID == id {
			m.subElements[i].Name = name
			return m.subElements[i], nil
		}
	}
	return db.SubElement{}, nil
}
func (m *fullMockStore) DeleteSubElement(_ context.Context, id uint) error {
	m.deleteSubElementCascade(id)
	return nil
}
func (m *fullMockStore) deleteSubElementCascade(id uint) {
	for i, se := range m.subElements {
		if se.ID == id {
			m.subElements = append(m.subElements[:i], m.subElements[i+1:]...)
		}
	}
	for i := 0; i < len(m.urlOffences); i++ {
		if m.urlOffences[i].SubElementID != nil && *m.urlOffences[i].SubElementID == id {
			m.urlOffences = append(m.urlOffences[:i], m.urlOffences[i+1:]...)
			i--
		}
	}
}

// findCategoryCitationInstrument resolves the preload chain a real
// ListOffencesByURL/GetOffence call would return via
// Preload("Category.Citation.Instrument").
func (m *fullMockStore) hydrateOffence(o db.URLOffence) db.URLOffence {
	for _, cat := range m.categories {
		if cat.ID == o.CategoryID {
			for _, c := range m.citations {
				if c.ID == cat.CitationID {
					for _, in := range m.instruments {
						if in.ID == c.InstrumentID {
							c.Instrument = in
						}
					}
					cat.Citation = c
				}
			}
			o.Category = cat
		}
	}
	if o.ElementID != nil {
		for _, el := range m.elements {
			if el.ID == *o.ElementID {
				elCopy := el
				o.Element = &elCopy
			}
		}
	}
	if o.SubElementID != nil {
		for _, se := range m.subElements {
			if se.ID == *o.SubElementID {
				seCopy := se
				o.SubElement = &seCopy
			}
		}
	}
	for _, u := range m.urls {
		if u.ID == o.URLID {
			o.URL = u
		}
	}
	return o
}

func (m *fullMockStore) ListOffencesByURL(_ context.Context, urlValue string) ([]db.URLOffence, error) {
	normalized, err := urlnorm.Normalize(urlValue)
	if err != nil {
		return nil, err
	}
	var urlID uint
	found := false
	for _, u := range m.urls {
		if u.URL == normalized {
			urlID = u.ID
			found = true
			break
		}
	}
	if !found {
		return nil, nil
	}
	var out []db.URLOffence
	for _, o := range m.urlOffences {
		if o.URLID == urlID {
			out = append(out, m.hydrateOffence(o))
		}
	}
	return out, nil
}
func (m *fullMockStore) GetOffence(_ context.Context, id uint) (*db.URLOffence, error) {
	for _, o := range m.urlOffences {
		if o.ID == id {
			hydrated := m.hydrateOffence(o)
			return &hydrated, nil
		}
	}
	return nil, nil
}
func (m *fullMockStore) AttachOffenceToURL(_ context.Context, urlValue string, categoryID uint, elementID *uint, subElementID *uint) (db.URLOffence, error) {
	normalized, err := urlnorm.Normalize(urlValue)
	if err != nil {
		return db.URLOffence{}, err
	}
	var urlID uint
	found := false
	for _, u := range m.urls {
		if u.URL == normalized {
			urlID = u.ID
			found = true
			break
		}
	}
	if !found {
		return db.URLOffence{}, fmt.Errorf("url not found: %s", urlValue)
	}
	o := db.URLOffence{
		ID: uint(len(m.urlOffences) + 1), URLID: urlID, CategoryID: categoryID, ElementID: elementID, SubElementID: subElementID, RecordedAt: time.Now(),
	}
	m.urlOffences = append(m.urlOffences, o)
	return o, nil
}
func (m *fullMockStore) DetachOffenceFromURL(_ context.Context, id uint) error {
	for i, o := range m.urlOffences {
		if o.ID == id {
			m.urlOffences = append(m.urlOffences[:i], m.urlOffences[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) GetScanInterval(_ context.Context) (int, error) {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	return m.scanInterval, nil
}
func (m *fullMockStore) SetScanInterval(_ context.Context, minutes int) error {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	m.scanInterval = minutes
	return nil
}
func (m *fullMockStore) GetScanEnabled(_ context.Context) (bool, error) {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	return m.scanEnabled, nil
}
func (m *fullMockStore) SetScanEnabled(_ context.Context, enabled bool) error {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	m.scanEnabled = enabled
	return nil
}
func (m *fullMockStore) GetDNSWorkers(_ context.Context) (int, error) {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	return m.dnsWorkers, nil
}
func (m *fullMockStore) SetDNSWorkers(_ context.Context, workers int) error {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	m.dnsWorkers = workers
	return nil
}
func (m *fullMockStore) GetSLAInterval(_ context.Context) (int, error) {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	return m.slaInterval, nil
}
func (m *fullMockStore) SetSLAInterval(_ context.Context, minutes int) error {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	m.slaInterval = minutes
	return nil
}
func (m *fullMockStore) GetSLAStreakThreshold(_ context.Context) (int, error) {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	return m.slaStreakThreshold, nil
}
func (m *fullMockStore) SetSLAStreakThreshold(_ context.Context, scans int) error {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()
	m.slaStreakThreshold = scans
	return nil
}

func (m *fullMockStore) ISPStats(_ context.Context, _ string) (db.ISPStatsResult, error) {
	return db.ISPStatsResult{}, nil
}

func (m *fullMockStore) ISPStatsForDepartment(_ context.Context, _ string, _ uint) (db.ISPStatsResult, error) {
	return db.ISPStatsResult{}, nil
}

func (m *fullMockStore) ISPTrend(_ context.Context, _ string, _, _ time.Time) ([]db.ISPTrendStat, error) {
	return nil, nil
}
func (m *fullMockStore) ISPTrendForDepartment(_ context.Context, _ string, _, _ time.Time, _ uint) ([]db.ISPTrendStat, error) {
	return nil, nil
}

func (m *fullMockStore) ISPComplianceTiming(_ context.Context, isp string) (db.ISPTimingResult, error) {
	return db.ISPTimingResult{ISP: isp}, nil
}
func (m *fullMockStore) ISPComplianceTimingForDepartment(_ context.Context, isp string, _ uint) (db.ISPTimingResult, error) {
	return db.ISPTimingResult{ISP: isp}, nil
}

func (m *fullMockStore) NationalTrend(_ context.Context, _, _ time.Time) ([]db.ISPTrendStat, error) {
	return nil, nil
}
func (m *fullMockStore) NationalTrendForDepartment(_ context.Context, _, _ time.Time, _ uint) ([]db.ISPTrendStat, error) {
	return nil, nil
}
func (m *fullMockStore) ServerUptime(_ context.Context, _ uint, _, _ time.Time) ([]db.ServerUptimeStat, error) {
	return nil, nil
}
func (m *fullMockStore) ServerUptimeForDepartment(_ context.Context, _ uint, _, _ time.Time, _ uint) ([]db.ServerUptimeStat, error) {
	return nil, nil
}
func (m *fullMockStore) ResurfacedDomains(_ context.Context) ([]db.ResurfacedDomain, error) {
	return nil, nil
}
func (m *fullMockStore) ResurfacedDomainsForDepartment(_ context.Context, _ uint) ([]db.ResurfacedDomain, error) {
	return nil, nil
}
func (m *fullMockStore) SLAActiveURLs(_ context.Context, _ int) ([]string, error) {
	return nil, nil
}
func (m *fullMockStore) ListDomainSummaries(_ context.Context, _, _ int, _ db.DomainSummaryFilter) ([]db.DomainSummary, int, error) {
	return nil, 0, nil
}
func (m *fullMockStore) ListDomainSummariesForDepartment(_ context.Context, _, _ int, _ uint, _ db.DomainSummaryFilter) ([]db.DomainSummary, int, error) {
	return nil, 0, nil
}
func (m *fullMockStore) DomainServerSummaries(_ context.Context, _ string) ([]db.DomainServerSummary, error) {
	return nil, nil
}

func (m *fullMockStore) UpsertDomainWhois(_ context.Context, w db.DomainWhois) error {
	for i, existing := range m.domainWhois {
		if existing.URLID == w.URLID {
			m.domainWhois[i] = w
			return nil
		}
	}
	m.domainWhois = append(m.domainWhois, w)
	return nil
}

func (m *fullMockStore) GetURLByValue(_ context.Context, urlValue string) (*db.URL, error) {
	normalized, err := urlnorm.Normalize(urlValue)
	if err != nil {
		return nil, err
	}
	for _, u := range m.urls {
		if u.URL == normalized {
			uCopy := u
			return &uCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) GetURLByID(_ context.Context, id uint) (*db.URL, error) {
	for _, u := range m.urls {
		if u.ID == id {
			uCopy := u
			return &uCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) GetDomainWhois(ctx context.Context, urlValue string) (*db.DomainWhois, error) {
	u, err := m.GetURLByValue(ctx, urlValue)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	urlID := u.ID
	for _, w := range m.domainWhois {
		if w.URLID == urlID {
			wCopy := w
			return &wCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) ListStaleDomains(_ context.Context, olderThan time.Time, limit int) ([]db.URL, error) {
	fetchedAt := make(map[uint]time.Time, len(m.domainWhois))
	for _, w := range m.domainWhois {
		fetchedAt[w.URLID] = w.LastFetchedAt
	}
	watchedIDs := make(map[uint]bool)
	for _, du := range m.departmentURLs {
		if du.Enabled {
			watchedIDs[du.URLID] = true
		}
	}
	var out []db.URL
	for _, u := range m.urls {
		if !watchedIDs[u.ID] {
			continue
		}
		last, fetched := fetchedAt[u.ID]
		if fetched && !last.Before(olderThan) {
			continue
		}
		out = append(out, u)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *fullMockStore) GetIPInfo(_ context.Context, ip string) (*db.IPInfo, error) {
	for _, info := range m.ipInfo {
		if info.IP == ip {
			infoCopy := info
			return &infoCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) UpsertIPInfo(_ context.Context, info db.IPInfo) error {
	for i, existing := range m.ipInfo {
		if existing.IP == info.IP {
			m.ipInfo[i] = info
			return nil
		}
	}
	m.ipInfo = append(m.ipInfo, info)
	return nil
}

func (m *fullMockStore) GetFavicon(_ context.Context, domain string) (*db.Favicon, error) {
	for _, fav := range m.favicons {
		if fav.Domain == domain {
			favCopy := fav
			return &favCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) UpsertFavicon(_ context.Context, fav db.Favicon) error {
	for i, existing := range m.favicons {
		if existing.Domain == fav.Domain {
			m.favicons[i] = fav
			return nil
		}
	}
	m.favicons = append(m.favicons, fav)
	return nil
}

func (m *fullMockStore) GetSubdomainScan(ctx context.Context, urlValue string) (*db.SubdomainScan, error) {
	u, err := m.GetURLByValue(ctx, urlValue)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	for _, s := range m.subdomainScans {
		if s.URLID == u.ID {
			sCopy := s
			return &sCopy, nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) UpsertSubdomainScan(_ context.Context, s db.SubdomainScan) error {
	for i, existing := range m.subdomainScans {
		if existing.URLID == s.URLID {
			m.subdomainScans[i] = s
			return nil
		}
	}
	m.subdomainScans = append(m.subdomainScans, s)
	return nil
}

// NotificationStore implementations for tests
func (m *fullMockStore) CreateNotification(_ context.Context, n db.Notification) (db.Notification, error) {
	n.ID = uint(len(m.notifications) + 1)
	n.CreatedAt = time.Now()
	m.notifications = append(m.notifications, n)
	return n, nil
}

func (m *fullMockStore) ListNotifications(_ context.Context, page, pageSize int) ([]db.Notification, int, error) {
	return m.listNotifications(page, pageSize, nil)
}

func (m *fullMockStore) ListNotificationsForDepartment(_ context.Context, page, pageSize int, departmentID uint) ([]db.Notification, int, error) {
	return m.listNotifications(page, pageSize, &departmentID)
}

func (m *fullMockStore) listNotifications(page, pageSize int, departmentID *uint) ([]db.Notification, int, error) {
	var filtered []db.Notification
	for _, n := range m.notifications {
		if departmentID == nil || n.DepartmentID == *departmentID {
			filtered = append(filtered, n)
		}
	}
	total := len(filtered)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return filtered[start:end], total, nil
}

func (m *fullMockStore) UnreadCount(_ context.Context) (int, error) {
	return m.unreadCount(nil), nil
}

func (m *fullMockStore) UnreadCountForDepartment(_ context.Context, departmentID uint) (int, error) {
	return m.unreadCount(&departmentID), nil
}

func (m *fullMockStore) unreadCount(departmentID *uint) int {
	count := 0
	for _, n := range m.notifications {
		if n.ReadAt == nil && (departmentID == nil || n.DepartmentID == *departmentID) {
			count++
		}
	}
	return count
}

func (m *fullMockStore) GetNotification(_ context.Context, id uint) (*db.Notification, error) {
	for i := range m.notifications {
		if m.notifications[i].ID == id {
			return &m.notifications[i], nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) MarkNotificationRead(_ context.Context, id uint) error {
	for i := range m.notifications {
		if m.notifications[i].ID == id {
			now := time.Now()
			m.notifications[i].ReadAt = &now
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) DeleteNotification(_ context.Context, id uint) error {
	for i, n := range m.notifications {
		if n.ID == id {
			m.notifications = append(m.notifications[:i], m.notifications[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) ClearAllNotifications(_ context.Context) error {
	m.notifications = nil
	return nil
}

func (m *fullMockStore) ClearAllNotificationsForDepartment(_ context.Context, departmentID uint) error {
	var kept []db.Notification
	for _, n := range m.notifications {
		if n.DepartmentID != departmentID {
			kept = append(kept, n)
		}
	}
	m.notifications = kept
	return nil
}

func (m *fullMockStore) HasRecentResurfacedNotification(_ context.Context, departmentID uint, urlValue string, since time.Time) (bool, error) {
	for _, n := range m.notifications {
		if n.DepartmentID == departmentID && n.URLValue == urlValue && n.Type == "resurfaced" && !n.CreatedAt.Before(since) {
			return true, nil
		}
	}
	return false, nil
}

func (m *fullMockStore) CreateCase(_ context.Context, departmentID, urlID uint, status string, opts db.CaseCreateOptions) (db.Case, error) {
	c := db.Case{ID: uint(len(m.cases) + 1), DepartmentID: departmentID, DueDate: opts.DueDate}
	m.cases = append(m.cases, c)
	m.caseURLs = append(m.caseURLs, db.CaseURL{CaseID: c.ID, URLID: urlID, Status: status, OriginalURL: opts.OriginalURL, AgencyID: opts.AgencyID})
	return c, nil
}
func (m *fullMockStore) UpdateCaseFields(_ context.Context, _ uint, caseID uint, fields db.CaseFields) (bool, error) {
	for i, c := range m.cases {
		if c.ID != caseID {
			continue
		}
		if fields.DueDate != nil {
			m.cases[i].DueDate = *fields.DueDate
		}
		if fields.RequestedAt != nil {
			m.cases[i].RequestedAt = *fields.RequestedAt
		}
		return true, nil
	}
	return false, nil
}
func (m *fullMockStore) AddCaseLetter(_ context.Context, letter db.CaseLetter) (db.CaseLetter, error) {
	letter.ID = uint(len(m.caseLetters) + 1)
	m.caseLetters = append(m.caseLetters, letter)
	return letter, nil
}
func (m *fullMockStore) ListCasesForURL(_ context.Context, urlValue string) ([]db.CaseWithLetters, error) {
	var u *db.URL
	for i := range m.urls {
		if m.urls[i].URL == urlValue {
			u = &m.urls[i]
			break
		}
	}
	if u == nil {
		return nil, nil
	}
	var out []db.CaseWithLetters
	for _, cu := range m.caseURLs {
		if cu.URLID != u.ID {
			continue
		}
		for _, c := range m.cases {
			if c.ID != cu.CaseID {
				continue
			}
			var letters []db.CaseLetter
			for _, l := range m.caseLetters {
				if l.CaseID == c.ID {
					letters = append(letters, l)
				}
			}
			out = append(out, db.CaseWithLetters{Case: c, Status: cu.Status, Letters: letters})
		}
	}
	return out, nil
}
func (m *fullMockStore) GetCase(_ context.Context, id uint) (db.Case, error) {
	for _, c := range m.cases {
		if c.ID == id {
			return c, nil
		}
	}
	return db.Case{}, gorm.ErrRecordNotFound
}
func (m *fullMockStore) AddURLToCase(_ context.Context, caseID, urlID uint, status, originalURL string, agencyID *uint) (db.CaseURL, error) {
	cu := db.CaseURL{CaseID: caseID, URLID: urlID, Status: status, OriginalURL: originalURL, AgencyID: agencyID}
	m.caseURLs = append(m.caseURLs, cu)
	return cu, nil
}
func (m *fullMockStore) UpdateCaseURLStatus(_ context.Context, caseID, urlID uint, status string) (bool, error) {
	for i, cu := range m.caseURLs {
		if cu.CaseID == caseID && cu.URLID == urlID {
			m.caseURLs[i].Status = status
			return true, nil
		}
	}
	return false, nil
}
func (m *fullMockStore) UpdateCaseURLAgency(_ context.Context, caseID, urlID uint, agencyID *uint) (bool, error) {
	for i, cu := range m.caseURLs {
		if cu.CaseID == caseID && cu.URLID == urlID {
			m.caseURLs[i].AgencyID = agencyID
			return true, nil
		}
	}
	return false, nil
}
func (m *fullMockStore) ListCaseURLIDs(_ context.Context, caseID uint) ([]uint, error) {
	var ids []uint
	for _, cu := range m.caseURLs {
		if cu.CaseID == caseID {
			ids = append(ids, cu.URLID)
		}
	}
	return ids, nil
}
func (m *fullMockStore) listCaseLetters(departmentID *uint, page, pageSize int) ([]db.CaseLetterEntry, int, error) {
	var entries []db.CaseLetterEntry
	for _, l := range m.caseLetters {
		var c db.Case
		found := false
		for _, cc := range m.cases {
			if cc.ID == l.CaseID {
				c, found = cc, true
				break
			}
		}
		if !found || (departmentID != nil && c.DepartmentID != *departmentID) {
			continue
		}
		var urls []string
		for _, cu := range m.caseURLs {
			if cu.CaseID != l.CaseID {
				continue
			}
			for _, u := range m.urls {
				if u.ID == cu.URLID {
					urls = append(urls, u.URL)
				}
			}
		}
		entries = append(entries, db.CaseLetterEntry{CaseLetter: l, DepartmentID: c.DepartmentID, URLs: urls})
	}
	total := len(entries)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return entries[start:end], total, nil
}
func (m *fullMockStore) ListCaseLetters(_ context.Context, page, pageSize int) ([]db.CaseLetterEntry, int, error) {
	return m.listCaseLetters(nil, page, pageSize)
}
func (m *fullMockStore) ListCaseLettersForDepartment(_ context.Context, page, pageSize int, departmentID uint) ([]db.CaseLetterEntry, int, error) {
	return m.listCaseLetters(&departmentID, page, pageSize)
}

func (m *fullMockStore) listCaseSummaries(departmentID *uint) []db.CaseSummary {
	var out []db.CaseSummary
	for _, c := range m.cases {
		if departmentID != nil && c.DepartmentID != *departmentID {
			continue
		}
		cs := db.CaseSummary{ID: c.ID, DueDate: c.DueDate, RequestedAt: c.RequestedAt, CreatedAt: c.CreatedAt}
		for _, l := range m.caseLetters {
			if l.CaseID != c.ID {
				continue
			}
			id := l.ID
			switch l.Type {
			case "Notice", "Notice (Uplift)":
				cs.NoticeLetterID = &id
				cs.NoticeSubject = l.Subject
				cs.NoticeWorkflowStatus = l.WorkflowStatus
				cs.NoticeReferenceNumberExternal = l.ReferenceNumberExternal
				cs.NoticeReferenceNumberInternal = l.ReferenceNumberInternal
			case "Memo", "Memo (Uplift)":
				cs.MemoLetterID = &id
				cs.MemoSubject = l.Subject
				cs.MemoReferenceNumberInternal = l.ReferenceNumberInternal
			}
		}
		for _, cu := range m.caseURLs {
			if cu.CaseID != c.ID {
				continue
			}
			for _, u := range m.urls {
				if u.ID == cu.URLID {
					cs.Domains = append(cs.Domains, db.CaseSummaryDomain{URLID: u.ID, URL: u.URL, Status: cu.Status})
				}
			}
		}
		out = append(out, cs)
	}
	return out
}

func (m *fullMockStore) ListCases(_ context.Context) ([]db.CaseSummary, error) {
	return m.listCaseSummaries(nil), nil
}

func (m *fullMockStore) ListCasesForDepartment(_ context.Context, departmentID uint) ([]db.CaseSummary, error) {
	return m.listCaseSummaries(&departmentID), nil
}

func (m *fullMockStore) UpdateCaseLetterFields(_ context.Context, caseID, letterID uint, fields db.CaseLetterFields) (bool, error) {
	for i, l := range m.caseLetters {
		if l.ID != letterID || l.CaseID != caseID {
			continue
		}
		if fields.Subject != nil {
			m.caseLetters[i].Subject = *fields.Subject
		}
		if fields.WorkflowStatus != nil {
			m.caseLetters[i].WorkflowStatus = *fields.WorkflowStatus
		}
		if fields.ReferenceNumberExternal != nil {
			m.caseLetters[i].ReferenceNumberExternal = *fields.ReferenceNumberExternal
		}
		if fields.ReferenceNumberInternal != nil {
			m.caseLetters[i].ReferenceNumberInternal = *fields.ReferenceNumberInternal
		}
		if fields.Recipient != nil {
			m.caseLetters[i].Recipient = *fields.Recipient
		}
		if fields.Requestor != nil {
			m.caseLetters[i].Requestor = *fields.Requestor
		}
		if fields.Remarks != nil {
			m.caseLetters[i].Remarks = *fields.Remarks
		}
		if fields.LetterDate != nil {
			m.caseLetters[i].LetterDate = *fields.LetterDate
		}
		if fields.ReceivedAt != nil {
			m.caseLetters[i].ReceivedAt = *fields.ReceivedAt
		}
		if fields.SubmittedAt != nil {
			m.caseLetters[i].SubmittedAt = *fields.SubmittedAt
		}
		if fields.OICUserID != nil {
			m.caseLetters[i].OICUserID = *fields.OICUserID
		}
		return true, nil
	}
	return false, nil
}

func (m *fullMockStore) DeleteCaseLetter(_ context.Context, caseID, letterID uint) (bool, error) {
	for i, l := range m.caseLetters {
		if l.ID != letterID || l.CaseID != caseID {
			continue
		}
		m.caseLetters = append(m.caseLetters[:i], m.caseLetters[i+1:]...)
		return true, nil
	}
	return false, nil
}

var _ db.Store = (*fullMockStore)(nil)

func setupRouter(store db.Store, sc *server.Scanner) http.Handler {
	r := chi.NewRouter()
	// whoisFetch/subfinderFetch/ipFetch/netnameFetch are nil — the lazy
	// on-add fetch goroutines never run in tests, so no test hits the
	// network or shells out.
	server.RegisterRoutes(r, store, sc, nil, false, nil, nil, nil, nil, nil, nil)
	// Handler tests exercise business logic, not the CSRF header check
	// itself (client.ts is what's responsible for sending it in practice),
	// so inject it here rather than at every httptest.NewRequest call site.
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Header.Set("X-Requested-With", "fetch")
		r.ServeHTTP(w, req)
	})
}

// loginAs seeds a user + session directly into the mock store and returns
// a cookie a test request can attach via req.AddCookie — bypassing the
// /api/auth/login flow for tests that aren't specifically about login.
func loginAs(store *fullMockStore, user db.User) *http.Cookie {
	user.ID = uint(len(store.users) + 1)
	user.CreatedAt = time.Now()
	store.users = append(store.users, user)

	token := fmt.Sprintf("test-session-token-%d", len(store.sessions)+1)
	store.sessions = append(store.sessions, db.Session{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	})
	return &http.Cookie{Name: "session_token", Value: token}
}

func adminCookie(store *fullMockStore) *http.Cookie {
	adminDeptID := uint(999)
	return loginAs(store, db.User{Username: "admin", PasswordHash: "x", IsAdmin: true, DepartmentID: &adminDeptID})
}

func deptCookie(store *fullMockStore, departmentID uint) *http.Cookie {
	return loginAs(store, db.User{
		Username:     fmt.Sprintf("dept-user-%d", departmentID),
		PasswordHash: "x",
		DepartmentID: &departmentID,
	})
}

func deptAdminCookie(store *fullMockStore, departmentID uint) *http.Cookie {
	return loginAs(store, db.User{
		Username:     fmt.Sprintf("dept-admin-%d", departmentID),
		PasswordHash: "x",
		IsDeptAdmin:  true,
		DepartmentID: &departmentID,
	})
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestListURLsEmpty(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var entries []db.URLEntry
	json.NewDecoder(w.Body).Decode(&entries)
	if len(entries) != 0 {
		t.Fatalf("expected empty list, got %v", entries)
	}
}

func TestAddToWatchlist_AdminUsesOwnDepartment(t *testing.T) {
	// Admin is now assigned to the "Admin" department — no body department_id needed.
	store := &fullMockStore{}
	cookie := adminCookie(store) // sets DepartmentID=999
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin adding URL to own department, got %d: %s", w.Code, w.Body.String())
	}
	var entry db.URLEntry
	json.NewDecoder(w.Body).Decode(&entry)
	if entry.URL != "example.com" {
		t.Fatalf("unexpected URL: %s", entry.URL)
	}
	if !entry.Enabled {
		t.Fatalf("expected newly added URL to be enabled")
	}
}

func TestAddToWatchlist_NonAdminUsesOwnDepartment(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	listReq.AddCookie(cookie)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	var entries []db.URLEntry
	json.NewDecoder(listW.Body).Decode(&entries)
	if len(entries) != 1 || entries[0].URL != "example.com" {
		t.Fatalf("expected the new url on the caller's own watchlist, got %v", entries)
	}
	if !entries[0].Enabled {
		t.Fatalf("expected newly added URL to be enabled")
	}
}

func TestAddToWatchlistMissingField(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"url": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty url, got %d", w.Code)
	}
}

func TestRemoveFromWatchlist_PreservesURLAndHistory(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
		results:        []db.ScanResult{{ID: 1, URLID: 1, URLValue: "example.com", ScannedAt: time.Now()}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/urls/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.urls) != 1 {
		t.Fatalf("expected the url row to survive removal from the watchlist, got %v", store.urls)
	}
	if len(store.results) != 1 {
		t.Fatalf("expected scan history to survive removal from the watchlist, got %v", store.results)
	}

	watched, _ := store.ListWatchedURLs(context.Background())
	if len(watched) != 0 {
		t.Fatalf("expected the url to no longer be actively watched, got %v", watched)
	}
}

func TestRemoveFromWatchlist_NotOnWatchlistReturns404(t *testing.T) {
	store := &fullMockStore{urls: []db.URL{{ID: 1, URL: "example.com"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/urls/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPurgeURL_AdminOnly(t *testing.T) {
	store := &fullMockStore{urls: []db.URL{{ID: 1, URL: "example.com"}}}
	nonAdmin := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/urls/1", nil)
	req.AddCookie(nonAdmin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", w.Code)
	}

	admin := adminCookie(store)
	req2 := httptest.NewRequest(http.MethodDelete, "/api/admin/urls/1", nil)
	req2.AddCookie(admin)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin, got %d: %s", w2.Code, w2.Body.String())
	}
	if len(store.urls) != 0 {
		t.Fatalf("expected the url to be hard-deleted, got %v", store.urls)
	}
}

func TestCreateDNSServer(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"isp": "Google", "name": "Google UDP", "address": "8.8.8.8:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListDNSServers_AllowedForNonAdmin(t *testing.T) {
	store := &fullMockStore{dnsServers: []db.DNSServer{{ID: 1, ISP: "Google", Name: "Google UDP", Address: "8.8.8.8:53", Protocol: "udp"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/dns-servers", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for a non-admin reading the DNS server list, got %d: %s", w.Code, w.Body.String())
	}
	var servers []db.DNSServer
	json.NewDecoder(w.Body).Decode(&servers)
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
}

func TestCreateDNSServer_ForbiddenForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"name": "Google", "address": "8.8.8.8:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateDNSServer_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"isp": "Google", "name": "Google UDP", "address": "8.8.8.8:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected a department admin to be able to add a DNS server (shared catalog), got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateDNSServer(t *testing.T) {
	store := &fullMockStore{dnsServers: []db.DNSServer{{ID: 1, ISP: "Google", Name: "Google UDP", Address: "8.8.8.8:53", Protocol: "udp"}}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"isp": "Google", "name": "Google Secondary", "address": "8.8.4.4:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPatch, "/api/dns-servers/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.dnsServers[0].Name != "Google Secondary" || store.dnsServers[0].Address != "8.8.4.4:53" {
		t.Fatalf("expected server fields to be updated, got %+v", store.dnsServers[0])
	}
}

func TestUpdateDNSServer_ForbiddenForNonAdmin(t *testing.T) {
	store := &fullMockStore{dnsServers: []db.DNSServer{{ID: 1, ISP: "Google", Name: "Google UDP", Address: "8.8.8.8:53", Protocol: "udp"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"isp": "Google", "name": "Google Secondary", "address": "8.8.4.4:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPatch, "/api/dns-servers/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateDNSServer_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{dnsServers: []db.DNSServer{{ID: 1, ISP: "Google", Name: "Google UDP", Address: "8.8.8.8:53", Protocol: "udp"}}}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"isp": "Google", "name": "Google Secondary", "address": "8.8.4.4:53", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPatch, "/api/dns-servers/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected a department admin to be able to edit a DNS server (shared catalog), got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestDNSServer_ForbiddenForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)
	body, _ := json.Marshal(map[string]string{"address": "127.0.0.1:1", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-admin, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestDNSServer_ReportsFailure(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	// port 0 never accepts a connection — a fast, network-independent way to
	// exercise the failure path without depending on a real resolver being reachable.
	body, _ := json.Marshal(map[string]string{"address": "127.0.0.1:0", "protocol": "udp"})
	req := httptest.NewRequest(http.MethodPost, "/api/dns-servers/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even on resolution failure, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Success || resp.Error == "" {
		t.Fatalf("expected success=false with an error message, got %+v", resp)
	}
}

func TestGetScanStatusIdle(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/status", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "idle" {
		t.Fatalf("expected idle, got %v", resp["status"])
	}
}

func TestGetLatestResults(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", Compliant: false, ScannedAt: time.Now()},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestScanProgressNotFound(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/progress", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestScanProgressWithRun(t *testing.T) {
	now := time.Now()
	store := &fullMockStore{
		urls: []db.URL{
			{ID: 1, URL: "https://a.com"},
			{ID: 2, URL: "https://b.com"},
		},
		departmentURLs: []db.DepartmentURL{
			{DepartmentID: 1, URLID: 1, Enabled: true},
			{DepartmentID: 1, URLID: 2, Enabled: true},
		},
		lastRun: &db.ScanRun{ID: 3, Status: "completed", StartedAt: now},
		progress: []db.ProgressEntry{
			{DNSServerID: 1, Name: "CF", Completed: 2},
			{DNSServerID: 2, Name: "Google", Completed: 1},
		},
	}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/progress", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ScanRun   map[string]any   `json:"scan_run"`
		TotalURLs int              `json:"total_urls"`
		PerDNS    []map[string]any `json:"per_dns"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalURLs != 2 {
		t.Fatalf("expected total_urls=2, got %d", resp.TotalURLs)
	}
	if len(resp.PerDNS) != 2 {
		t.Fatalf("expected 2 per_dns entries, got %d", len(resp.PerDNS))
	}
}

func TestResultsByURL_DefaultWindow(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "example.com", ScannedAt: time.Now().AddDate(0, 0, -10)},
		{ID: 2, URLValue: "example.com", ScannedAt: time.Now()},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result within default 7-day window, got %d", len(results))
	}
	if results[0].ID != 2 {
		t.Fatalf("expected the recent result (id=2), got id=%d", results[0].ID)
	}
}

func TestResultsByURL_ExplicitSince(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "example.com", ScannedAt: time.Now().AddDate(0, 0, -10)},
		{ID: 2, URLValue: "example.com", ScannedAt: time.Now()},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com?since="+since, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 2 {
		t.Fatalf("expected 2 results within 30-day window, got %d", len(results))
	}
}

func TestResultsByURL_ExplicitUntil(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "example.com", ScannedAt: time.Now().AddDate(-1, 0, 0)},
		{ID: 2, URLValue: "example.com", ScannedAt: time.Now()},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Now().AddDate(-2, 0, 0).Format(time.RFC3339))
	until := url.QueryEscape(time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com?since="+since+"&until="+until, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result within since/until window, got %d", len(results))
	}
	if results[0].ID != 1 {
		t.Fatalf("expected the older result (id=1), got id=%d", results[0].ID)
	}
}

func TestResultsByURL_404ForUnownedDomain(t *testing.T) {
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}, {ID: 2, URL: "other.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
		results:        []db.ScanResult{{ID: 1, URLValue: "other.com", ScannedAt: time.Now()}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/results/other.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a domain not on the caller's watchlist, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDomainServerSummaries_404ForUnownedDomain(t *testing.T) {
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}, {ID: 2, URL: "other.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/domains/other.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a domain not on the caller's watchlist, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHeatmapByURL_GroupsByDay(t *testing.T) {
	day := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "example.com", DNSServerID: 1, DNSServer: db.DNSServer{ID: 1, ISP: "Google", Name: "Google UDP"}, Compliant: true, ScannedAt: day.Add(1 * time.Hour)},
		{ID: 2, URLValue: "example.com", DNSServerID: 1, DNSServer: db.DNSServer{ID: 1, ISP: "Google", Name: "Google UDP"}, Compliant: false, ScannedAt: day.Add(2 * time.Hour)},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/heatmap/https://example.com?since="+since, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stats []db.DailyComplianceStat
	json.NewDecoder(w.Body).Decode(&stats)
	if len(stats) != 1 {
		t.Fatalf("expected 1 day bucket, got %d", len(stats))
	}
	if stats[0].Total != 2 || stats[0].Compliant != 1 {
		t.Fatalf("expected total=2 compliant=1, got %+v", stats[0])
	}
	if stats[0].Level != 3 {
		t.Fatalf("expected level 3 (1 of 2 violations, rate 0.5 is >1/3 and <=2/3), got %d", stats[0].Level)
	}
}

func TestDNSRecordsByURL_ResolvesKnownHost(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/dns-records/google.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Hostname   string `json:"hostname"`
		Resolved   bool   `json:"resolved"`
		ResolverIP string `json:"resolver_ip"`
		Records    *struct {
			A     []string `json:"a"`
			AAAA  []string `json:"aaaa"`
			CNAME []string `json:"cname"`
			MX    []string `json:"mx"`
			TXT   []string `json:"txt"`
			NS    []string `json:"ns"`
		} `json:"records"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Hostname != "google.com" {
		t.Fatalf("expected hostname google.com, got %q", resp.Hostname)
	}
	if !resp.Resolved {
		t.Fatalf("expected resolved=true for google.com")
	}
	if resp.ResolverIP == "" {
		t.Fatalf("expected a non-empty resolver_ip")
	}
	if resp.Records == nil || len(resp.Records.A) == 0 {
		t.Fatalf("expected at least one A record for google.com, got %+v", resp.Records)
	}
	if resp.Records == nil || len(resp.Records.NS) == 0 {
		t.Fatalf("expected at least one NS record for google.com, got %+v", resp.Records)
	}
}

func TestDNSRecordsByURL_NXDOMAIN(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/dns-records/this-host-should-not-exist-zzqxv12345.invalid", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Hostname string `json:"hostname"`
		Resolved bool   `json:"resolved"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Resolved {
		t.Fatalf("expected resolved=false for a non-existent host")
	}
}

func TestResultsByURL_PercentEncodedSlashes(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "example.com", ScannedAt: time.Now()},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/results/https%3A%2F%2Fexample.com%2F", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// TestResultsByURL_NormalizesMixedCaseAndScheme is a regression test for the
// bug where ResultsByURL/HeatmapByURL/DomainServerSummaries matched the raw
// `*url` wildcard segment against url_value directly instead of normalizing
// it first (like URLOwnedByDepartment/GetURLByValue do). Since
// scan_results.url_value now stores the normalized bare hostname, a
// mixed-case or scheme-prefixed bookmarked URL must resolve to the same scan
// history as the bare lowercase form rather than silently returning nothing.
func TestResultsByURL_NormalizesMixedCaseAndScheme(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "example.com", ScannedAt: time.Now()},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	for _, path := range []string{
		"/api/results/example.com",
		"/api/results/Example.com",
		"/api/results/https%3A%2F%2FExample.com%2F",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", path, w.Code, w.Body.String())
		}
		var results []db.ScanResult
		json.NewDecoder(w.Body).Decode(&results)
		if len(results) != 1 {
			t.Fatalf("%s: expected 1 result matching the normalized bare hostname, got %d", path, len(results))
		}
	}
}

// Auth

func TestLogin_Success(t *testing.T) {
	hash, _ := db.HashPassword("s3cret")
	store := &fullMockStore{users: []db.User{{ID: 1, Username: "alice", PasswordHash: hash, IsAdmin: true}}}
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "s3cret"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) == 0 {
		t.Fatalf("expected a session cookie to be set")
	}
	if len(store.sessions) != 1 {
		t.Fatalf("expected a session to be created, got %d", len(store.sessions))
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	hash, _ := db.HashPassword("s3cret")
	store := &fullMockStore{users: []db.User{{ID: 1, Username: "alice", PasswordHash: hash, IsAdmin: true}}}
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_RejectsMissingCookie(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLogout_ClearsSession(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if len(store.sessions) != 0 {
		t.Fatalf("expected the session to be deleted, got %d remaining", len(store.sessions))
	}

	// The same cookie must no longer work.
	req2 := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", w2.Code)
	}
}

// Admin: departments and users

func TestCreateDepartment_AdminOnly(t *testing.T) {
	store := &fullMockStore{}
	nonAdmin := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"name": "Legal"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/departments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(nonAdmin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", w.Code)
	}

	admin := adminCookie(store)
	req2 := httptest.NewRequest(http.MethodPost, "/api/admin/departments", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(admin)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin, got %d: %s", w2.Code, w2.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/departments", nil)
	listReq.AddCookie(admin)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	var departments []db.Department
	json.NewDecoder(listW.Body).Decode(&departments)
	if len(departments) != 1 || departments[0].Name != "Legal" {
		t.Fatalf("expected the new department to be listed, got %v", departments)
	}
}

func TestCreateUser_RequiresDepartmentForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{"username": "bob", "password": "pw12345", "is_admin": false})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when department_id is omitted for a non-admin user, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateUser_Success(t *testing.T) {
	store := &fullMockStore{departments: []db.Department{{ID: 1, Name: "CMOD"}}}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{
		"username": "bob", "password": "pw12345", "is_admin": false, "department_id": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var u db.User
	json.NewDecoder(w.Body).Decode(&u)
	if u.Username != "bob" || u.DepartmentID == nil || *u.DepartmentID != 1 {
		t.Fatalf("unexpected created user: %+v", u)
	}
	if u.PasswordHash != "" {
		t.Fatalf("expected PasswordHash to never be serialized in the API response, got %q", u.PasswordHash)
	}
	stored := store.users[len(store.users)-1]
	if stored.PasswordHash == "" || stored.PasswordHash == "pw12345" {
		t.Fatalf("expected the stored password to be hashed, not stored in plaintext")
	}

	// The new user can actually log in with the password they were given.
	loginBody, _ := json.Marshal(map[string]string{"username": "bob", "password": "pw12345"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("expected the newly created user to be able to log in, got %d: %s", loginW.Code, loginW.Body.String())
	}
}

func TestListUsers_DeptAdminSeesOnlyOwnDepartment(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "alice", DepartmentID: &dept1},
		{ID: 2, Username: "carol", DepartmentID: &dept2},
	}}
	cookie := deptAdminCookie(store, dept1) // becomes user ID 3
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var users []db.User
	json.NewDecoder(w.Body).Decode(&users)
	if len(users) != 2 {
		t.Fatalf("expected 2 department-1 users (alice + the dept admin itself), got %d: %+v", len(users), users)
	}
	for _, u := range users {
		if u.DepartmentID == nil || *u.DepartmentID != dept1 {
			t.Fatalf("expected only department-1 users, got %+v", u)
		}
	}
}

func TestListUsers_SuperAdminSeesAll(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "alice", DepartmentID: &dept1},
		{ID: 2, Username: "carol", DepartmentID: &dept2},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var users []db.User
	json.NewDecoder(w.Body).Decode(&users)
	if len(users) != 3 { // alice, carol, and the admin itself
		t.Fatalf("expected a super admin to see every user, got %d: %+v", len(users), users)
	}
}

func TestCreateUser_DeptAdminForcesOwnDepartmentAndPlainRole(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	store := &fullMockStore{}
	cookie := deptAdminCookie(store, dept1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{
		"username": "eve", "password": "pw12345",
		"is_admin": true, "is_dept_admin": true, "department_id": dept2,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var u db.User
	json.NewDecoder(w.Body).Decode(&u)
	if u.IsAdmin || u.IsDeptAdmin {
		t.Fatalf("expected a department admin to never grant admin/dept-admin, got %+v", u)
	}
	if u.DepartmentID == nil || *u.DepartmentID != dept1 {
		t.Fatalf("expected the created user pinned to the caller's own department (1), got %+v", u)
	}
}

func TestDeleteUser_DeptAdminScoping(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "same-dept-member", DepartmentID: &dept1},
		{ID: 2, Username: "other-dept-member", DepartmentID: &dept2},
		{ID: 3, Username: "same-dept-admin", IsDeptAdmin: true, DepartmentID: &dept1},
	}}
	cookie := deptAdminCookie(store, dept1)
	r := setupRouter(store, nil)

	del := func(id uint) int {
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", id), nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := del(2); code != http.StatusForbidden {
		t.Fatalf("expected 403 deleting a user in a different department, got %d", code)
	}
	if code := del(3); code != http.StatusForbidden {
		t.Fatalf("expected 403 deleting another department admin, got %d", code)
	}
	if code := del(1); code != http.StatusNoContent {
		t.Fatalf("expected 204 deleting a plain member of the caller's own department, got %d", code)
	}
}

func TestToggleURL_DisableAndReenable(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.departmentURLs[0].Enabled {
		t.Fatal("expected Enabled=false after toggle")
	}

	body2, _ := json.Marshal(map[string]bool{"enabled": true})
	req2 := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("want 204 on re-enable, got %d: %s", w2.Code, w2.Body.String())
	}
	if !store.departmentURLs[0].Enabled {
		t.Fatal("expected Enabled=true after re-enable")
	}
}

func TestToggleURL_NotOnWatchlistReturns404(t *testing.T) {
	store := &fullMockStore{urls: []db.URL{{ID: 1, URL: "example.com"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for URL not on watchlist, got %d", w.Code)
	}
}

// TestToggleURL_IgnoresCaseMetadataFields covers PATCH /api/urls/{id}'s
// narrowed body: due_date/agency_id/status/requested_at (now living on
// Case, not URL — see PATCH /api/cases/{id}) are silently ignored rather
// than rejected; only enabled is honored, and no Case is touched.
func TestToggleURL_IgnoresCaseMetadataFields(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"enabled": false, "due_date": "2026-01-15T00:00:00Z", "agency_id": 7,
		"status": "uplift", "requested_at": "2026-01-01T00:00:00Z",
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.departmentURLs[0].Enabled {
		t.Fatal("expected Enabled=false after toggle")
	}
	if len(store.cases) != 0 {
		t.Fatalf("expected the case-metadata fields to be ignored, no Case created, got %+v", store.cases)
	}
}

func TestDeleteUser_AdminOnly(t *testing.T) {
	store := &fullMockStore{users: []db.User{{ID: 5, Username: "bob"}}}
	nonAdmin := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/5", nil)
	req.AddCookie(nonAdmin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", w.Code)
	}

	admin := adminCookie(store)
	req2 := httptest.NewRequest(http.MethodDelete, "/api/admin/users/5", nil)
	req2.AddCookie(admin)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestURLsRequestedThisMonth_CountsOnlyThisCalendarMonth(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	now := time.Now().UTC()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastMonth := startOfMonth.AddDate(0, 0, -1) // last day of the previous month — outside the window even if within 30 days
	store := &fullMockStore{departmentURLs: []db.DepartmentURL{
		{DepartmentID: dept1, URLID: 1, CreatedAt: startOfMonth.Add(time.Hour)}, // this month, dept1
		{DepartmentID: dept2, URLID: 2, CreatedAt: now},                         // this month, dept2
		{DepartmentID: dept1, URLID: 3, CreatedAt: lastMonth},                   // previous month — must not count
	}}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/urls/requested-count", nil)
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]int
	json.NewDecoder(w.Body).Decode(&got)
	if got["count"] != 2 {
		t.Fatalf("expected admin to see 2 requests this calendar month (excluding last month's), got %+v", got)
	}
}

func TestURLsRequestedThisMonth_ScopedForDepartment(t *testing.T) {
	dept1, dept2 := uint(1), uint(2)
	now := time.Now().UTC()
	store := &fullMockStore{departmentURLs: []db.DepartmentURL{
		{DepartmentID: dept1, URLID: 1, CreatedAt: now},
		{DepartmentID: dept2, URLID: 2, CreatedAt: now},
		{DepartmentID: dept2, URLID: 3, CreatedAt: now},
	}}
	cookie := deptCookie(store, dept1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/urls/requested-count", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]int
	json.NewDecoder(w.Body).Decode(&got)
	if got["count"] != 1 {
		t.Fatalf("expected department 1 to see only its own request, got %+v", got)
	}
}

func TestScanInterval_GetAndSet_AdminOnly(t *testing.T) {
	store := &fullMockStore{scanInterval: 60, scanEnabled: true, dnsWorkers: 20, slaInterval: 15, slaStreakThreshold: 3}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/scan-interval", nil)
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		IntervalMinutes    int  `json:"interval_minutes"`
		Enabled            bool `json:"enabled"`
		DNSWorkers         int  `json:"dns_workers"`
		SLAIntervalMinutes int  `json:"sla_interval_minutes"`
		SLAStreakThreshold int  `json:"sla_streak_threshold"`
	}
	json.NewDecoder(w.Body).Decode(&got)
	if got.IntervalMinutes != 60 || !got.Enabled || got.DNSWorkers != 20 || got.SLAIntervalMinutes != 15 || got.SLAStreakThreshold != 3 {
		t.Fatalf("expected interval_minutes=60 enabled=true dns_workers=20 sla_interval_minutes=15 sla_streak_threshold=3, got %+v", got)
	}

	body, _ := json.Marshal(map[string]any{"interval_minutes": 15, "enabled": false, "dns_workers": 100, "sla_interval_minutes": 5, "sla_streak_threshold": 4})
	req2 := httptest.NewRequest(http.MethodPatch, "/api/admin/scan-interval", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(admin)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w2.Code, w2.Body.String())
	}
	if store.scanInterval != 15 {
		t.Fatalf("expected the stored interval to update to 15, got %d", store.scanInterval)
	}
	if store.scanEnabled {
		t.Fatalf("expected the stored enabled flag to update to false")
	}
	if store.dnsWorkers != 100 {
		t.Fatalf("expected the stored dns_workers to update to 100, got %d", store.dnsWorkers)
	}
	if store.slaInterval != 5 {
		t.Fatalf("expected the stored sla_interval to update to 5, got %d", store.slaInterval)
	}
	if store.slaStreakThreshold != 4 {
		t.Fatalf("expected the stored sla_streak_threshold to update to 4, got %d", store.slaStreakThreshold)
	}
}

func TestStartScheduler_SkipsTriggerWhenDisabled(t *testing.T) {
	// A watched URL is required so sc.run would actually reach the crawler
	// (and hold sc.running true behind the fake's delay) if Trigger fired —
	// otherwise it short-circuits at "no URLs to scan" regardless of the
	// enabled check, making the assertion below pass for the wrong reason.
	store := &fullMockStore{
		// scanInterval: 0 is non-positive, so StartScheduler falls back to
		// the short defaultInterval below instead of waiting a real minute.
		scanInterval:   0,
		scanEnabled:    false,
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
	}
	crawler := &fakeCrawlerClient{delay: 200 * time.Millisecond}
	sc := server.NewScanner(crawler, "", store, server.NewBroadcaster())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	server.StartScheduler(ctx, sc, store, 10*time.Millisecond)
	<-ctx.Done()
	if sc.IsRunning() {
		t.Fatal("expected Trigger to be skipped while the scan schedule is disabled")
	}
}

func TestStartScheduler_NotifyScheduleChangedRestartsWait(t *testing.T) {
	store := &fullMockStore{
		// A huge interval — long enough that the scheduler's initial wait
		// would never fire before this test's timeout on its own. Proves
		// NotifyScheduleChanged abandons that stale wait rather than the
		// test passing only because the interval happened to be short.
		scanInterval: 100000,
		scanEnabled:  true,
		urls:         []db.URL{{ID: 1, URL: "example.com"}},
		// A watched URL alone isn't enough — sc.run also short-circuits
		// before reaching the crawler if there are no enabled DNS servers,
		// which would flip running true->false almost instantly and make
		// waitUntil's polling miss the transient window.
		dnsServers:     []db.DNSServer{{ID: 1, Name: "G", ISP: "Google", Address: "8.8.8.8:53", Protocol: "udp", Enabled: true}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
	}
	crawler := &fakeCrawlerClient{delay: 50 * time.Millisecond}
	sc := server.NewScanner(crawler, "", store, server.NewBroadcaster())
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	server.StartScheduler(ctx, sc, store, 10*time.Millisecond)

	// Give the loop a moment to enter its (huge) wait, then simulate an
	// admin save that shortens the interval — NotifyScheduleChanged should
	// make the scheduler pick up the new value immediately instead of
	// finishing out the old wait.
	time.Sleep(10 * time.Millisecond)
	store.scheduleMu.Lock()
	store.scanInterval = 0 // non-positive -> falls back to the 10ms defaultInterval
	store.scheduleMu.Unlock()
	sc.NotifyScheduleChanged()

	waitUntil(t, sc.IsRunning, 300*time.Millisecond)
}

func TestScanInterval_ForbiddenForDeptAdmin(t *testing.T) {
	store := &fullMockStore{scanInterval: 60}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/scan-interval", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected scan interval to stay super-admin-only, got %d: %s", w.Code, w.Body.String())
	}
}

func TestScanInterval_RejectsNonPositive(t *testing.T) {
	store := &fullMockStore{}
	admin := adminCookie(store)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]int{"interval_minutes": 0})
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/scan-interval", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-positive interval, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSubdomainsByURL_FetchedFalseWhenNeverScanned(t *testing.T) {
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/subdomains/example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Fetched bool `json:"fetched"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Fetched {
		t.Fatalf("expected fetched=false with no cached SubdomainScan row, got true")
	}
}

func TestSubdomainsByURL_404ForUnownedDomain(t *testing.T) {
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}, {ID: 2, URL: "other.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/subdomains/other.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a domain not on the caller's watchlist, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefreshSubdomains_503WhenDisabled(t *testing.T) {
	// setupRouter always wires a nil subfinderFetch — mirrors production
	// running with --subfinder-path "" to disable enumeration entirely.
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: 1, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/subdomains/example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when subfinderFetch is nil, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefreshHostingInfo_503WhenDisabled(t *testing.T) {
	// setupRouter always wires a nil ipFetch — mirrors production running
	// with hosting lookups disabled.
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/hosting/1.2.3.4", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when ipFetch is nil, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListISPLogos_AllowedForNonAdmin(t *testing.T) {
	store := &fullMockStore{ispLogos: []db.ISPLogo{{ISP: "Cloudflare", LogoURL: "https://example.com/cf.svg"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/isp-logos", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var logos []db.ISPLogo
	if err := json.Unmarshal(w.Body.Bytes(), &logos); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(logos) != 1 || logos[0].ISP != "Cloudflare" {
		t.Fatalf("unexpected logos: %+v", logos)
	}
}

func TestUpsertISPLogo_ForbiddenForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"isp": "Cloudflare", "logo_url": "https://example.com/cf.svg"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/isp-logos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpsertISPLogo_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"isp": "Cloudflare", "logo_url": "https://example.com/cf.svg"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/isp-logos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpsertISPLogo_RequiresBothFields(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"isp": "Cloudflare"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/isp-logos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when logo_url is missing, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteISPLogo_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{ispLogos: []db.ISPLogo{{ISP: "Cloudflare", LogoURL: "https://example.com/cf.svg"}}}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/isp-logos/Cloudflare", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteISPLogo_HandlesEscapedName(t *testing.T) {
	store := &fullMockStore{ispLogos: []db.ISPLogo{{ISP: "Time dotCom", LogoURL: "https://example.com/timedotcom.svg"}}}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/isp-logos/Time%20dotCom", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.ispLogos) != 0 {
		t.Fatalf("expected the ISP logo to be deleted, got %d remaining: %+v", len(store.ispLogos), store.ispLogos)
	}
}

func TestGetGridPreference_NeverSaved_ReturnsEmpty(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/grid-preferences/urls", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "{}\n" {
		t.Fatalf("expected empty object, got %s", w.Body.String())
	}
}

func TestGetGridPreference_UnknownKey_BadRequest(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/grid-preferences/not-a-real-grid", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetGridPreference_EveryFrontendKeyIsAllowed guards against a grid
// added to the frontend (useGridPreference call sites: urls.tsx's Domain and
// Cases views, results.index.tsx, docs.tsx) whose key was never added to
// validGridKeys -- the fetch/save hook swallows the resulting 400 silently
// (see use-grid-preference.ts), so a missing key here shows up as nothing
// more than a saved layout that never persists, not a visible error.
func TestGetGridPreference_EveryFrontendKeyIsAllowed(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	for _, key := range []string{"urls", "urls-cases", "results", "docs"} {
		req := httptest.NewRequest(http.MethodGet, "/api/grid-preferences/"+key, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("key %q: expected 200, got %d: %s", key, w.Code, w.Body.String())
		}
	}
}

func TestSaveGridPreference_RoundTrip(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body := `{"column_visibility":{"agency":false},"sort_field":"status","sort_desc":true,"page_size":50}`
	req := httptest.NewRequest(http.MethodPut, "/api/grid-preferences/urls", bytes.NewReader([]byte(body)))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/grid-preferences/urls", nil)
	getReq.AddCookie(cookie)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	var pref db.GridPreference
	if err := json.Unmarshal(getW.Body.Bytes(), &pref); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pref.SortField != "status" || !pref.SortDesc || pref.PageSize != 50 {
		t.Fatalf("unexpected preference after round trip: %+v", pref)
	}
	if visible, ok := pref.ColumnVisibility["agency"]; !ok || visible {
		t.Fatalf("expected agency column hidden, got %+v", pref.ColumnVisibility)
	}
}

func TestSaveGridPreference_ScopedPerUser(t *testing.T) {
	store := &fullMockStore{}
	cookieA := deptCookie(store, 1)
	cookieB := deptCookie(store, 2)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/grid-preferences/urls", bytes.NewReader([]byte(`{"page_size":100}`)))
	req.AddCookie(cookieA)
	r.ServeHTTP(httptest.NewRecorder(), req)

	getReq := httptest.NewRequest(http.MethodGet, "/api/grid-preferences/urls", nil)
	getReq.AddCookie(cookieB)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	if getW.Body.String() != "{}\n" {
		t.Fatalf("expected user B to see no saved preference, got %s", getW.Body.String())
	}
}

func TestListAgencies_AllowedForNonAdmin(t *testing.T) {
	store := &fullMockStore{agencies: []db.Agency{{ID: 1, Name: "MCMC"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/agencies", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var agencies []db.Agency
	if err := json.Unmarshal(w.Body.Bytes(), &agencies); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(agencies) != 1 || agencies[0].Name != "MCMC" {
		t.Fatalf("unexpected agencies: %+v", agencies)
	}
}

func TestCreateAgency_ForbiddenForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"name": "MCMC"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/agencies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateAgency_AllowedForDeptAdmin confirms Agency mutations sit in the
// requireAnyAdmin group (like DNS servers/ISP logos), not the stricter
// requireAdmin group Department/CompliantIP use — a deliberate deviation
// from that precedent, easy to regress if "fixed" back to match it later.
func TestCreateAgency_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"name": "MCMC"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/agencies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgency_RequiresName(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/agencies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when name is missing, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteAgency_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{agencies: []db.Agency{{ID: 1, Name: "MCMC"}}}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/agencies/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.agencies) != 0 {
		t.Fatalf("expected the agency to be deleted, got %d remaining: %+v", len(store.agencies), store.agencies)
	}
}

func TestListDueDatePresets_AllowedForNonAdmin(t *testing.T) {
	store := &fullMockStore{dueDatePresets: []db.DueDatePreset{{ID: 1, Label: "24 hours", Minutes: 24 * 60}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/due-date-presets", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var presets []db.DueDatePreset
	if err := json.Unmarshal(w.Body.Bytes(), &presets); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(presets) != 1 || presets[0].Minutes != 24*60 {
		t.Fatalf("unexpected presets: %+v", presets)
	}
}

func TestCreateDueDatePreset_ForbiddenForNonAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{"label": "12 hours", "minutes": 12 * 60})
	req := httptest.NewRequest(http.MethodPost, "/api/due-date-presets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateDueDatePreset_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]any{"label": "12 hours", "minutes": 12 * 60})
	req := httptest.NewRequest(http.MethodPost, "/api/due-date-presets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateDueDatePreset_RequiresLabelAndPositiveMinutes(t *testing.T) {
	store := &fullMockStore{}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	for _, body := range []map[string]any{
		{"label": "", "minutes": 12},
		{"label": "12 hours", "minutes": 0},
		{"label": "12 hours", "minutes": -1},
	} {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/due-date-presets", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %+v: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
	}
}

func TestDeleteDueDatePreset_AllowedForDeptAdmin(t *testing.T) {
	store := &fullMockStore{dueDatePresets: []db.DueDatePreset{{ID: 1, Label: "24 hours", Minutes: 24 * 60}}}
	cookie := deptAdminCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/due-date-presets/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.dueDatePresets) != 0 {
		t.Fatalf("expected the preset to be deleted, got %d remaining: %+v", len(store.dueDatePresets), store.dueDatePresets)
	}
}

// TestListDepartmentsOpen_AllowedForNonAdmin proves the new GET
// /api/departments route is genuinely open (unlike the existing
// super-admin-only GET /api/admin/departments it sits alongside).
func TestListDepartmentsOpen_AllowedForNonAdmin(t *testing.T) {
	store := &fullMockStore{departments: []db.Department{{ID: 1, Name: "CMOD"}}}
	cookie := deptCookie(store, 1)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/departments", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var departments []db.Department
	if err := json.Unmarshal(w.Body.Bytes(), &departments); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(departments) != 1 || departments[0].Name != "CMOD" {
		t.Fatalf("unexpected departments: %+v", departments)
	}
}

func TestListUsersOpen_NonAdminSeesOwnDepartmentOnly(t *testing.T) {
	dept1 := uint(1)
	dept2 := uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "alice", DepartmentID: &dept1},
		{ID: 2, Username: "bob", DepartmentID: &dept2},
	}}
	cookie := deptCookie(store, 1) // becomes user ID 3 in store.users, department 1
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/users/open", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var users []db.User
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// alice (dept 1) and the caller (dept 1) are visible; bob (dept 2) is not.
	names := map[string]bool{}
	for _, u := range users {
		names[u.Username] = true
	}
	if !names["alice"] || names["bob"] {
		t.Fatalf("expected alice visible and bob hidden, got %+v", users)
	}
}

func TestListUsersOpen_AdminSeesEveryone(t *testing.T) {
	dept1 := uint(1)
	dept2 := uint(2)
	store := &fullMockStore{users: []db.User{
		{ID: 1, Username: "alice", DepartmentID: &dept1},
		{ID: 2, Username: "bob", DepartmentID: &dept2},
	}}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/users/open", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var users []db.User
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(users) != 3 { // alice, bob, and the admin caller loginAs adds
		t.Fatalf("expected admin to see all 3 users, got %d: %+v", len(users), users)
	}
}

func TestFaviconByURLNormalizesCacheKey(t *testing.T) {
	store := &fullMockStore{
		favicons: []db.Favicon{{
			Domain:      "example.com",
			ContentType: "image/png",
			Data:        []byte{0x89, 'P', 'N', 'G'},
			FetchedAt:   time.Now(),
		}},
	}
	cookie := deptCookie(store, 1)
	router := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/favicon/Example.com", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// faviconFetch is nil in setupRouter, so a cache miss 503s. A 200 proves
	// "Example.com" normalized down to the cached "example.com" row.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from normalized cache hit, got %d (%s)", w.Code, w.Body.String())
	}
	if len(store.favicons) != 1 {
		t.Fatalf("expected no second favicon row, got %d", len(store.favicons))
	}
}

type rescheduleCall struct {
	departmentID, urlID uint
	dueDate             *time.Time
}

type fakeNotifier struct {
	mu    sync.Mutex
	calls []rescheduleCall
}

func (f *fakeNotifier) RescheduleDueDate(departmentID, urlID uint, dueDate *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, rescheduleCall{departmentID, urlID, dueDate})
	return nil
}

// ToggleURL no longer touches due_date at all (see
// TestToggleURL_IgnoresCaseMetadataFields) — the old
// TestToggleURL_ReschedulesDueDateTask[ForEveryWatchingDepartment] tests
// exercised a fan-out that no longer has a trigger point on this route now
// that DueDate lives on Case, not URL. Re-wired instead to
// PATCH /api/cases/{id} — see TestUpdateCase_ReschedulesDueDateForEveryCaseURL
// in case_handlers_test.go.

func TestRemoveFromWatchlist_CancelsDueDateTask(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	notifier := &fakeNotifier{}
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, nil, nil, false, nil, nil, nil, nil, nil, notifier)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Header.Set("X-Requested-With", "fetch")
		r.ServeHTTP(w, req)
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/urls/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.calls) != 1 {
		t.Fatalf("expected 1 RescheduleDueDate call, got %d", len(notifier.calls))
	}
	if notifier.calls[0].dueDate != nil {
		t.Fatal("expected a nil due date on removal (cancel-only)")
	}
}

func TestListNotifications_ScopesToOwnDepartment(t *testing.T) {
	deptA, deptB := uint(1), uint(2)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptA, URLValue: "a.com", Type: "resurfaced"},
			{ID: 2, DepartmentID: deptB, URLValue: "b.com", Type: "resurfaced"},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Notifications []db.Notification `json:"notifications"`
		Total         int               `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 1 || len(body.Notifications) != 1 || body.Notifications[0].URLValue != "a.com" {
		t.Fatalf("expected only deptA's notification, got %+v", body)
	}
}

func TestUnreadNotificationCount(t *testing.T) {
	deptA := uint(1)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptA, URLValue: "a.com", Type: "resurfaced"},
			{ID: 2, DepartmentID: deptA, URLValue: "b.com", Type: "resurfaced", ReadAt: ptrTime(time.Now())},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/notifications/unread-count", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Count int `json:"count"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Count != 1 {
		t.Fatalf("expected count 1, got %d", body.Count)
	}
}

func TestMarkNotificationRead_404sForOtherDepartment(t *testing.T) {
	deptA, deptB := uint(1), uint(2)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptB, URLValue: "b.com", Type: "resurfaced"},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/notifications/1/read", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for another department's notification, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMarkNotificationRead_Success(t *testing.T) {
	deptA := uint(1)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptA, URLValue: "a.com", Type: "resurfaced"},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/notifications/1/read", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.notifications[0].ReadAt == nil {
		t.Fatal("expected ReadAt to be set")
	}
}

func TestDeleteNotification_404sForOtherDepartment(t *testing.T) {
	deptA, deptB := uint(1), uint(2)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptB, URLValue: "b.com", Type: "resurfaced"},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/notifications/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for another department's notification, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.notifications) != 1 {
		t.Fatalf("expected the other department's notification to survive, got %d remaining", len(store.notifications))
	}
}

func TestDeleteNotification_Success(t *testing.T) {
	deptA := uint(1)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptA, URLValue: "a.com", Type: "resurfaced"},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/notifications/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.notifications) != 0 {
		t.Fatalf("expected the notification to be deleted, got %d remaining", len(store.notifications))
	}
}

func TestClearAllNotifications_OnlyClearsOwnDepartment(t *testing.T) {
	deptA, deptB := uint(1), uint(2)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptA, URLValue: "a.com", Type: "resurfaced"},
			{ID: 2, DepartmentID: deptA, URLValue: "a2.com", Type: "due_date_reached"},
			{ID: 3, DepartmentID: deptB, URLValue: "b.com", Type: "resurfaced"},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/notifications", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.notifications) != 1 || store.notifications[0].DepartmentID != deptB {
		t.Fatalf("expected only deptB's notification to survive, got %+v", store.notifications)
	}
}

func TestClearAllNotifications_AdminClearsEverything(t *testing.T) {
	deptA, deptB := uint(1), uint(2)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptA, URLValue: "a.com", Type: "resurfaced"},
			{ID: 2, DepartmentID: deptB, URLValue: "b.com", Type: "resurfaced"},
		},
	}
	cookie := adminCookie(store)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/notifications", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.notifications) != 0 {
		t.Fatalf("expected every notification cleared for an admin, got %+v", store.notifications)
	}
}
