package db

import (
	"context"
	"time"
)

// URLStore covers the global/admin URL catalog plus department watchlists —
// CreateURL normalizes + get-or-creates, so the same domain added by
// different departments always resolves to one shared row (see
// AddURLToWatchlist).
type URLStore interface {
	ListURLs(ctx context.Context) ([]URL, error)
	CreateURL(ctx context.Context, rawURL string) (URL, error)
	DeleteURL(ctx context.Context, id uint) error                     // admin-only hard purge; cascades to ScanResult
	GetURLByValue(ctx context.Context, urlValue string) (*URL, error) // nil, nil if urlValue is unknown
	GetURLByID(ctx context.Context, id uint) (*URL, error)            // nil, nil if id is unknown

	ListDepartmentURLs(ctx context.Context, departmentID uint) ([]URLEntry, error)
	// ListURLEntriesPage is the Domain view's server-side paged variant;
	// p.DepartmentID nil = every department's watchlist (admin).
	ListURLEntriesPage(ctx context.Context, p URLListParams) ([]URLEntry, int, error)
	AddURLToWatchlist(ctx context.Context, departmentID uint, rawURL string) (URL, error)
	// EnsureURLOnDepartmentList makes sure urlID has a DepartmentURL row for
	// departmentID, creating one with Enabled: false if none exists yet —
	// called wherever a URL becomes associated with a department via a
	// Case, so it shows up in that department's Domain tab (switched off)
	// without silently opting it into the scan sweep. A pre-existing row
	// (from AddURLToWatchlist or an earlier case) is left untouched.
	EnsureURLOnDepartmentList(ctx context.Context, departmentID, urlID uint) error
	RemoveURLFromWatchlist(ctx context.Context, departmentID, urlID uint) (bool, error)      // false if no row was deleted (not on that watchlist)
	SetURLEnabled(ctx context.Context, departmentID, urlID uint, enabled bool) (bool, error) // false if the URL is not on that watchlist
	ListWatchedURLs(ctx context.Context) ([]URL, error)                                      // urls with >=1 enabled DepartmentURL row — used by the scan sweep
	ListUnassignedURLs(ctx context.Context) ([]URL, error)                                   // admin view: urls with 0 DepartmentURL rows
	URLOwnedByDepartment(ctx context.Context, departmentID uint, urlValue string) (bool, error)
	// DepartmentIDsWatchingURL returns every department with a DepartmentURL
	// row for urlID (regardless of Enabled) — used to fan a case-field
	// change out to every department that watches it, not just the one that
	// made the PATCH.
	DepartmentIDsWatchingURL(ctx context.Context, urlID uint) ([]uint, error)

	// Watchlist activity — counts DepartmentURL rows (watchlist "requests")
	// created since a given time; used for the "requested this month" stat.
	CountDepartmentURLsSince(ctx context.Context, since time.Time) (int, error)
	CountDepartmentURLsSinceForDepartment(ctx context.Context, since time.Time, departmentID uint) (int, error)
}

// DNSServerStore is the shared/global DNS server catalog — not
// department-scoped, results reference servers by name.
type DNSServerStore interface {
	ListDNSServers(ctx context.Context) ([]DNSServer, error)
	// ListEnabledDNSServers is what a sweep actually scans against — disabled
	// servers stay in ListDNSServers (so past results/admin UI still show
	// them) but are skipped by future scans.
	ListEnabledDNSServers(ctx context.Context) ([]DNSServer, error)
	CreateDNSServer(ctx context.Context, s DNSServer) (DNSServer, error)
	UpdateDNSServer(ctx context.Context, id uint, s DNSServer) (DNSServer, error)
	DeleteDNSServer(ctx context.Context, id uint) error
	SetDNSServerEnabled(ctx context.Context, id uint, enabled bool) error
}

// ScanRunStore tracks the lifecycle of a scan sweep (one StartSweep call to
// the crawler's control service), independent of the ScanResult rows it produces.
type ScanRunStore interface {
	CreateScanRun(ctx context.Context, triggeredBy string) (ScanRun, error)
	CompleteScanRun(ctx context.Context, id uint, status string, completedAt time.Time) error
	ActiveScanRun(ctx context.Context) (*ScanRun, error)
	LastScanRun(ctx context.Context) (*ScanRun, error)
	ScanProgress(ctx context.Context, runID uint) ([]ProgressEntry, error)
}

// ResultStore ingests and queries per-scan compliance results.
type ResultStore interface {
	LatestResults(ctx context.Context) ([]ScanResult, error)
	LatestResultsForDepartment(ctx context.Context, departmentID uint) ([]ScanResult, error)
	ResultsByURL(ctx context.Context, urlValue string, since, until time.Time) ([]ScanResult, error)
	DailyComplianceByURL(ctx context.Context, urlValue string, since, until time.Time) ([]DailyComplianceStat, error)
	InsertResult(ctx context.Context, r ScanResult) error
	UpdateScreenshot(ctx context.Context, resultID uint, screenshotURL string) error

	// ListDomainSummaries/ForDepartment back GET /api/domains — a paginated,
	// lifetime (not just-latest-run) aggregate per domain, sorted by most
	// recently scanned first. The department variant matches on
	// department_urls.department_id only (no enabled filter), so a domain
	// disabled from the watchlist is still findable by its history. filter is
	// a DomainSummaryFilter{} zero value for no filtering.
	ListDomainSummaries(ctx context.Context, page, pageSize int, filter DomainSummaryFilter) ([]DomainSummary, int, error)
	ListDomainSummariesForDepartment(ctx context.Context, page, pageSize int, departmentID uint, filter DomainSummaryFilter) ([]DomainSummary, int, error)

	// DomainServerSummaries backs the Domain page's expanded-row breakdown —
	// unscoped like ResultsByURL/DailyComplianceByURL; the handler enforces
	// department ownership via requireDomainOwnership before calling it.
	DomainServerSummaries(ctx context.Context, urlValue string) ([]DomainServerSummary, error)
}

// ISPStatsStore aggregates ScanResult rows into per-ISP compliance, trend,
// and time-to-compliance stats.
type ISPStatsStore interface {
	ISPStats(ctx context.Context, isp string) (ISPStatsResult, error)
	ISPStatsForDepartment(ctx context.Context, isp string, departmentID uint) (ISPStatsResult, error)
	ISPTrend(ctx context.Context, isp string, since, until time.Time) ([]ISPTrendStat, error)
	ISPTrendForDepartment(ctx context.Context, isp string, since, until time.Time, departmentID uint) ([]ISPTrendStat, error)
	ISPComplianceTiming(ctx context.Context, isp string) (ISPTimingResult, error)
	ISPComplianceTimingForDepartment(ctx context.Context, isp string, departmentID uint) (ISPTimingResult, error)
	NationalTrend(ctx context.Context, since, until time.Time) ([]ISPTrendStat, error)
	NationalTrendForDepartment(ctx context.Context, since, until time.Time, departmentID uint) ([]ISPTrendStat, error)
	ServerUptime(ctx context.Context, dnsServerID uint, since, until time.Time) ([]ServerUptimeStat, error)
	ServerUptimeForDepartment(ctx context.Context, dnsServerID uint, since, until time.Time, departmentID uint) ([]ServerUptimeStat, error)
	ResurfacedDomains(ctx context.Context) ([]ResurfacedDomain, error)
	ResurfacedDomainsForDepartment(ctx context.Context, departmentID uint) ([]ResurfacedDomain, error)
	// SLAActiveURLs returns the normalized URL values of watched domains
	// still under active SLA tracking: DueDate has passed, and at least one
	// enabled DNS server hasn't yet reached streakThreshold consecutive
	// compliant scans for that (url, server) pair. See StartSLAScheduler
	// (internal/server/scheduler.go).
	SLAActiveURLs(ctx context.Context, streakThreshold int) ([]string, error)
}

// DepartmentStore is the departments table — admin-only by nature (cross-
// department), seeded once by db.SeedDepartments.
type DepartmentStore interface {
	ListDepartments(ctx context.Context) ([]Department, error)
	CreateDepartment(ctx context.Context, name string) (Department, error)
	UpdateDepartment(ctx context.Context, id uint, name string) (Department, error)
}

// UserStore covers user accounts (admin, department-admin, and plain
// department-member roles).
type UserStore interface {
	ListUsers(ctx context.Context) ([]User, error)
	CreateUser(ctx context.Context, u User) (User, error)
	UpdateUser(ctx context.Context, id uint, u User) (User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetUserByID(ctx context.Context, id uint) (*User, error)
	DeleteUser(ctx context.Context, id uint) error
	// SetUserPassword sets a new bcrypt hash, and MustChangePassword — true
	// for an admin-initiated reset (forces a change at next login), false
	// for a user changing their own password.
	SetUserPassword(ctx context.Context, id uint, hash string, mustChange bool) error
}

// SessionStore backs the session-cookie auth flow.
type SessionStore interface {
	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, token string) (*Session, error)
	DeleteSession(ctx context.Context, token string) error

	// DeleteExpiredSessions removes sessions past their expiry. Returns the
	// number of rows deleted. GetSession already filters these out, so this
	// is purely to stop the table growing without bound.
	DeleteExpiredSessions(ctx context.Context) (int64, error)
}

// CompliantIPStore is the admin-managed list of IPs treated as compliant
// even when DNS resolves (e.g. an ISP's block-page IP).
type CompliantIPStore interface {
	ListCompliantIPs(ctx context.Context) ([]CompliantIP, error)
	CreateCompliantIP(ctx context.Context, address, note string) (CompliantIP, error)
	DeleteCompliantIP(ctx context.Context, id uint) error
}

// AgencyStore is the admin-managed agency lookup table — read open to any
// authenticated role, mutations gated to admin-or-dept-admin (see router.go).
type AgencyStore interface {
	ListAgencies(ctx context.Context) ([]Agency, error)
	CreateAgency(ctx context.Context, name string) (Agency, error)
	UpdateAgency(ctx context.Context, id uint, name string) (Agency, error)
	DeleteAgency(ctx context.Context, id uint) error
}

// RecipientStore and RequestorStore are the admin-managed lookup tables
// backing the Docs page's Recipient/Requestor dropdowns — same shape as
// AgencyStore.
type RecipientStore interface {
	ListRecipients(ctx context.Context) ([]Recipient, error)
	CreateRecipient(ctx context.Context, name string) (Recipient, error)
	UpdateRecipient(ctx context.Context, id uint, name string) (Recipient, error)
	DeleteRecipient(ctx context.Context, id uint) error
}

type RequestorStore interface {
	ListRequestors(ctx context.Context) ([]Requestor, error)
	CreateRequestor(ctx context.Context, name string) (Requestor, error)
	UpdateRequestor(ctx context.Context, id uint, name string) (Requestor, error)
	DeleteRequestor(ctx context.Context, id uint) error
}

// DueDatePresetStore is the admin/dept-admin-managed list of "Time to
// Block" duration options — same read-open/write-gated shape as
// AgencyStore, not department-scoped.
type DueDatePresetStore interface {
	ListDueDatePresets(ctx context.Context) ([]DueDatePreset, error)
	CreateDueDatePreset(ctx context.Context, label string, minutes int) (DueDatePreset, error)
	DeleteDueDatePreset(ctx context.Context, id uint) error
}

// ISPLogoStore is the admin-managed ISP name → logo URL lookup, rendered on
// the Overview page's ISPBentoGrid.
type ISPLogoStore interface {
	ListISPLogos(ctx context.Context) ([]ISPLogo, error)
	UpsertISPLogo(ctx context.Context, isp, logoURL string) (ISPLogo, error)
	DeleteISPLogo(ctx context.Context, isp string) error
}

// GridPreferenceStore holds each user's saved data-grid layout (column
// visibility, sort, page size), keyed by (user, grid). Personal to the
// calling user — no admin gating, ownership is implicit in the userID param.
type GridPreferenceStore interface {
	GetGridPreference(ctx context.Context, userID uint, gridKey string) (*GridPreference, error) // nil, nil if never saved
	SaveGridPreference(ctx context.Context, pref GridPreference) (GridPreference, error)         // upsert by (user_id, grid_key)
}

// ScanSettingsStore holds the single admin-configurable scan cadence row.
type ScanSettingsStore interface {
	GetScanInterval(ctx context.Context) (int, error)
	SetScanInterval(ctx context.Context, minutes int) error
	GetScanEnabled(ctx context.Context) (bool, error)
	SetScanEnabled(ctx context.Context, enabled bool) error
	GetDNSWorkers(ctx context.Context) (int, error)
	SetDNSWorkers(ctx context.Context, workers int) error
	GetSLAInterval(ctx context.Context) (int, error)
	SetSLAInterval(ctx context.Context, minutes int) error
	GetSLAStreakThreshold(ctx context.Context) (int, error)
	SetSLAStreakThreshold(ctx context.Context, scans int) error
}

// EnrichmentStore covers the fetch-once (or fetch-rarely) caches keyed by
// domain or IP rather than by scan run: WHOIS/RDAP, ASN/NetName IP info,
// favicons, and subfinder subdomain enumeration. None of these affect the
// compliance verdict — informational only.
type EnrichmentStore interface {
	UpsertDomainWhois(ctx context.Context, w DomainWhois) error
	GetDomainWhois(ctx context.Context, urlValue string) (*DomainWhois, error)           // nil, nil if never fetched (or urlValue is unknown)
	ListStaleDomains(ctx context.Context, olderThan time.Time, limit int) ([]URL, error) // watched URLs with no DomainWhois row or LastFetchedAt < olderThan

	GetIPInfo(ctx context.Context, ip string) (*IPInfo, error) // nil, nil if never fetched
	UpsertIPInfo(ctx context.Context, info IPInfo) error

	GetFavicon(ctx context.Context, domain string) (*Favicon, error) // nil, nil if never fetched
	UpsertFavicon(ctx context.Context, fav Favicon) error

	GetSubdomainScan(ctx context.Context, urlValue string) (*SubdomainScan, error) // nil, nil if never fetched (or urlValue is unknown)
	UpsertSubdomainScan(ctx context.Context, s SubdomainScan) error
}

// LegalCitationStore covers the full Instrument→Citation→Category→Element
// legal-reference catalog plus the URL↔offence join (URLOffence). Kept as
// one sub-interface rather than split per-table: every real consumer walks
// the whole chain together (listing a URL's offences means resolving
// Element→Category→Citation→Instrument to render anything useful), the
// same reasoning EnrichmentStore already uses to bundle unrelated cache
// tables under one interface.
type LegalCitationStore interface {
	ListInstruments(ctx context.Context) ([]Instrument, error)
	// GetOrCreateInstrument finds an existing row by (type, jurisdiction,
	// number, year) or creates one, mirroring CreateURL's
	// get-or-create-by-natural-key pattern so the same law is never
	// duplicated across citations.
	GetOrCreateInstrument(ctx context.Context, in Instrument) (Instrument, error)
	UpdateInstrument(ctx context.Context, id uint, in Instrument) (Instrument, error)
	DeleteInstrument(ctx context.Context, id uint) error // cascades to Citation/Category/Element/URLOffence

	ListCitationsByInstrument(ctx context.Context, instrumentID uint) ([]Citation, error)
	// ListAllCitations/ListAllCategories/ListAllElements/ListAllSubElements
	// are the flat, unscoped counterparts of the four List*By* methods
	// above — see their doc comment in legalcite.go. Used only by the
	// legal-citations catalog page's up-front tree load, which needs every
	// row at each level, not one parent's worth.
	ListAllCitations(ctx context.Context) ([]Citation, error)
	// CreateCitation/UpdateCitation always recompute SortKey server-side
	// from c.Parsed.ProvisionNum/ProvisionSuffix — never trust a
	// client-supplied sort key.
	CreateCitation(ctx context.Context, c Citation) (Citation, error)
	UpdateCitation(ctx context.Context, id uint, c Citation) (Citation, error)
	DeleteCitation(ctx context.Context, id uint) error // cascades to Category/Element/URLOffence

	ListCategoriesByCitation(ctx context.Context, citationID uint) ([]Category, error)
	ListAllCategories(ctx context.Context) ([]Category, error)
	CreateCategory(ctx context.Context, cat Category) (Category, error)
	UpdateCategory(ctx context.Context, id uint, name string) (Category, error)
	DeleteCategory(ctx context.Context, id uint) error // cascades to Element/URLOffence

	ListElementsByCategory(ctx context.Context, categoryID uint) ([]Element, error)
	ListAllElements(ctx context.Context) ([]Element, error)
	CreateElement(ctx context.Context, el Element) (Element, error)
	UpdateElement(ctx context.Context, id uint, name string) (Element, error)
	DeleteElement(ctx context.Context, id uint) error // cascades to SubElement/URLOffence

	ListSubElementsByElement(ctx context.Context, elementID uint) ([]SubElement, error)
	ListAllSubElements(ctx context.Context) ([]SubElement, error)
	CreateSubElement(ctx context.Context, se SubElement) (SubElement, error)
	UpdateSubElement(ctx context.Context, id uint, name string) (SubElement, error)
	DeleteSubElement(ctx context.Context, id uint) error // cascades to URLOffence

	// ListOffencesByURL preloads Category (and its parent Citation/
	// Instrument) plus Element and SubElement so a listing can render full
	// context in one query. Keyed by urlValue, not urlID, matching the *url
	// wildcard convention used by every other domain-scoped read.
	ListOffencesByURL(ctx context.Context, urlValue string) ([]URLOffence, error)
	// GetOffence preloads URL — used by the detach handler to resolve the
	// owning department before deleting, since DELETE is keyed by the
	// offence's own surrogate ID, not by URL. nil, nil if not found.
	GetOffence(ctx context.Context, id uint) (*URLOffence, error)
	AttachOffenceToURL(ctx context.Context, urlValue string, categoryID uint, elementID *uint, subElementID *uint) (URLOffence, error)
	DetachOffenceFromURL(ctx context.Context, id uint) error
}

// NotificationStore covers the notification-center table — same
// admin-global/department-scoped read pattern as ResultStore's
// ListDomainSummaries/ForDepartment. CreateNotification and
// HasRecentResurfacedNotification are called only from internal/notify's
// task handlers, not from any HTTP handler directly.
type NotificationStore interface {
	CreateNotification(ctx context.Context, n Notification) (Notification, error)
	ListNotifications(ctx context.Context, page, pageSize int) ([]Notification, int, error)
	ListNotificationsForDepartment(ctx context.Context, page, pageSize int, departmentID uint) ([]Notification, int, error)
	UnreadCount(ctx context.Context) (int, error)
	UnreadCountForDepartment(ctx context.Context, departmentID uint) (int, error)
	GetNotification(ctx context.Context, id uint) (*Notification, error) // nil, nil if not found
	MarkNotificationRead(ctx context.Context, id uint) error
	DeleteNotification(ctx context.Context, id uint) error // dismiss; ownership is checked by the caller before invoking this
	// ClearAllNotifications/ForDepartment bulk-dismiss — same admin-global vs
	// department-scoped split as ListNotifications/UnreadCount, so "Clear
	// all" clears exactly the set the caller can currently see, not just
	// the dropdown's first page.
	ClearAllNotifications(ctx context.Context) error
	ClearAllNotificationsForDepartment(ctx context.Context, departmentID uint) error

	// HasRecentResurfacedNotification is the dedup check for the periodic
	// resurfaced sweep: true if a "resurfaced" notification for
	// (departmentID, urlValue) already has CreatedAt >= sinceResurfacedAt,
	// meaning this specific regression event was already notified.
	HasRecentResurfacedNotification(ctx context.Context, departmentID uint, urlValue string, sinceResurfacedAt time.Time) (bool, error)
}

// Store is the full persistence port — the union of every aggregate-scoped
// store above. Multi-aggregate consumers (Handlers, Scanner) depend on this.
// A consumer that only ever touches one aggregate should depend on that
// sub-interface directly instead (see StartWhoisRefresher's db.EnrichmentStore,
// StartScheduler's db.ScanSettingsStore) — it documents the dependency at a
// glance and narrows what a test double for it needs to implement.
type Store interface {
	URLStore
	DNSServerStore
	ScanRunStore
	ResultStore
	ISPStatsStore
	DepartmentStore
	UserStore
	SessionStore
	CompliantIPStore
	AgencyStore
	RecipientStore
	RequestorStore
	DueDatePresetStore
	ISPLogoStore
	GridPreferenceStore
	ScanSettingsStore
	EnrichmentStore
	LegalCitationStore
	NotificationStore
	CaseStore
}

// CaseStore is the cases/case_letters/case_urls aggregate.
type CaseStore interface {
	// CreateCase creates a Case for departmentID and links it to urlID
	// with the given status (requested | uplift | suspended) via CaseURL —
	// status is per-domain (CaseURL.Status), not stored anywhere on Case
	// itself. opts optionally sets Case.AgencyID/DueDate at creation time.
	// Returns the created Case (zero letters — AddCaseLetter is separate).
	CreateCase(ctx context.Context, departmentID, urlID uint, status string, opts CaseCreateOptions) (Case, error)
	// AddCaseLetter appends one CaseLetter row to an existing case.
	AddCaseLetter(ctx context.Context, letter CaseLetter) (CaseLetter, error)
	// ListCasesForURL returns every case covering urlValue, each with its
	// letters (newest LetterDate first) and this url's own Status from
	// case_urls — ownership scoping happens at the handler layer
	// (requireDomainOwnership), same split as ListOffencesByURL/
	// AttachOffenceToURL already use.
	ListCasesForURL(ctx context.Context, urlValue string) ([]CaseWithLetters, error)
	// GetCase returns a case's own DepartmentID for an ownership check
	// (a case belongs to exactly one requesting department, regardless of
	// how many URLs it covers via case_urls), or gorm.ErrRecordNotFound.
	GetCase(ctx context.Context, id uint) (Case, error)
	// AddURLToCase links an additional URL to an existing case via CaseURL
	// with its own status and (optionally) its own requesting agency — the
	// "N URLs in one Notice" shape CreateCase alone can't build, since it
	// only ever links the one URL a case is opened for. Callers building a
	// batch (e.g. adding several domains under one case) call CreateCase
	// once for the first URL, then this for each of the rest. originalURL
	// seeds CaseURL.OriginalURL; empty means none was supplied. agencyID
	// seeds this new CaseURL's own AgencyID; nil means none was supplied.
	AddURLToCase(ctx context.Context, caseID, urlID uint, status, originalURL string, agencyID *uint) (CaseURL, error)
	// ListCaseURLIDs returns every URL id a case covers, via case_urls —
	// used to fan a case-level DueDate change out to a per-url due-date-
	// reached notification task for each url the case links.
	ListCaseURLIDs(ctx context.Context, caseID uint) ([]uint, error)
	// UpdateCaseURLStatus sets this one (case, url) pair's own Status, for
	// the domain(s) within a case that diverge from the rest. status is
	// validated against urlStatusAllowed by the caller (handler layer).
	// False if no such CaseURL row exists.
	UpdateCaseURLStatus(ctx context.Context, caseID, urlID uint, status string) (bool, error)
	// UpdateCaseURLAgency sets this one (case, url) pair's own AgencyID, for
	// the domain(s) within a case that were requested by a different agency
	// than the rest (see CaseURL.AgencyID's doc comment — moved off Case
	// 2026-09-15). nil clears it. False if no such CaseURL row exists.
	UpdateCaseURLAgency(ctx context.Context, caseID, urlID uint, agencyID *uint) (bool, error)
	// RemoveURLFromCase unlinks one url from a case: deletes its CaseURL row
	// and the url_offences that case raised for it (the URL itself, its
	// scan history and its other cases are untouched). ErrLastCaseURL if it
	// is the case's only url — a case with no domains is meaningless.
	// False if no such CaseURL row exists.
	RemoveURLFromCase(ctx context.Context, caseID, urlID uint) (bool, error)
	// UpdateCaseFields applies a partial update to a case's shared fields
	// (DueDate/RequestedAt), mirroring the old UpdateURLCaseFields' double-
	// pointer clear-vs-untouched semantics. Ownership (departmentID must own
	// caseID) is checked by the caller (handler layer, matching
	// AddCaseLetter/AddCaseURL's existing direct check), not here. False if
	// caseID doesn't exist.
	UpdateCaseFields(ctx context.Context, departmentID, caseID uint, fields CaseFields) (bool, error)
	// ListCaseLetters returns every CaseLetter across every department,
	// newest LetterDate first, each carrying its case's department and
	// linked URLs — the Docs page's data source. Paginated like
	// ListDomainSummaries (page is 1-indexed).
	ListCaseLetters(ctx context.Context, page, pageSize int) ([]CaseLetterEntry, int, error)
	// ListCaseLettersForDepartment is ListCaseLetters scoped to one
	// department's own cases (cases.department_id), for non-admin callers.
	ListCaseLettersForDepartment(ctx context.Context, page, pageSize int, departmentID uint) ([]CaseLetterEntry, int, error)

	// ListCases/ListCasesForDepartment back the Cases view (GET
	// /api/case-summaries) — one row per case with its own fields, its
	// Notice letter's fields, and every domain it covers. Same admin-global
	// vs department-scoped split as ListCaseLetters/ForDepartment.
	ListCases(ctx context.Context) ([]CaseSummary, error)
	// ListCaseSummariesPage is the Cases view's server-side paged, filtered and
	// sorted variant; ListCases stays for the full-list export.
	ListCaseSummariesPage(ctx context.Context, p CaseListParams) ([]CaseSummary, int, error)
	ListCasesForDepartment(ctx context.Context, departmentID uint) ([]CaseSummary, error)
	// BlockingStats backs the dashboard's Blocking Statistics page; nil departmentID = global.
	BlockingStats(ctx context.Context, departmentID *uint) ([]BlockingStatRow, error)

	// UpdateCaseLetterFields applies a partial update to one CaseLetter's
	// fields, scoped by (caseID, letterID) so a letter can't be edited
	// through a case it doesn't belong to. Ownership (departmentID owns
	// caseID) is checked by the caller (handler layer), same as
	// UpdateCaseFields. False if no such (case, letter) pair exists.
	UpdateCaseLetterFields(ctx context.Context, caseID, letterID uint, fields CaseLetterFields) (bool, error)
	// DeleteCaseLetter removes one CaseLetter, scoped by (caseID, letterID)
	// same as UpdateCaseLetterFields. False if no such (case, letter) pair
	// exists.
	DeleteCaseLetter(ctx context.Context, caseID, letterID uint) (bool, error)
}

// CaseWithLetters is Case plus its Letters and this url's own Status/Agency
// (both CaseURL-level, not Case-level — see CaseURL's doc comment) — the
// read shape ListCasesForURL returns. Not a persisted table.
type CaseWithLetters struct {
	Case
	Status     string       `json:"status"`
	AgencyID   *uint        `json:"agency_id,omitempty"`
	AgencyName string       `json:"agency_name,omitempty"`
	Letters    []CaseLetter `json:"letters"`
}
