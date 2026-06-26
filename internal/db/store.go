package db

import (
	"context"
	"time"
)

type Store interface {
	// URLs (global/admin scope — CreateURL normalizes + get-or-creates)
	ListURLs(ctx context.Context) ([]URL, error)
	CreateURL(ctx context.Context, rawURL string) (URL, error)
	DeleteURL(ctx context.Context, id uint) error // admin-only hard purge; cascades to ScanResult

	// DNS Servers
	ListDNSServers(ctx context.Context) ([]DNSServer, error)
	CreateDNSServer(ctx context.Context, s DNSServer) (DNSServer, error)
	DeleteDNSServer(ctx context.Context, id uint) error

	// Scan Runs
	CreateScanRun(ctx context.Context, triggeredBy string) (ScanRun, error)
	CompleteScanRun(ctx context.Context, id uint, status string, completedAt time.Time) error
	ActiveScanRun(ctx context.Context) (*ScanRun, error)
	LastScanRun(ctx context.Context) (*ScanRun, error)
	ScanProgress(ctx context.Context, runID uint) ([]ProgressEntry, error)

	// Scan Results
	LatestResults(ctx context.Context) ([]ScanResult, error)
	LatestResultsForDepartment(ctx context.Context, departmentID uint) ([]ScanResult, error)
	ResultsByURL(ctx context.Context, urlValue string, since, until time.Time) ([]ScanResult, error)
	DailyComplianceByURL(ctx context.Context, urlValue string, since, until time.Time) ([]DailyComplianceStat, error)
	InsertResult(ctx context.Context, r ScanResult) error
	UpdateScreenshot(ctx context.Context, resultID uint, screenshotURL string) error

	// Departments
	ListDepartments(ctx context.Context) ([]Department, error)
	CreateDepartment(ctx context.Context, name string) (Department, error)

	// Users
	ListUsers(ctx context.Context) ([]User, error)
	CreateUser(ctx context.Context, u User) (User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetUserByID(ctx context.Context, id uint) (*User, error)
	DeleteUser(ctx context.Context, id uint) error

	// Sessions
	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, token string) (*Session, error)
	DeleteSession(ctx context.Context, token string) error

	// Department watchlists
	ListDepartmentURLs(ctx context.Context, departmentID uint) ([]URLEntry, error)
	AddURLToWatchlist(ctx context.Context, departmentID uint, rawURL string) (URL, error)
	RemoveURLFromWatchlist(ctx context.Context, departmentID, urlID uint) (bool, error) // false if no row was deleted (not on that watchlist)
	SetURLEnabled(ctx context.Context, departmentID, urlID uint, enabled bool) (bool, error) // false if the URL is not on that watchlist
	ListWatchedURLs(ctx context.Context) ([]URL, error)                                      // urls with >=1 enabled DepartmentURL row — used by the scan sweep
	ListUnassignedURLs(ctx context.Context) ([]URL, error)                                   // admin view: urls with 0 DepartmentURL rows
	URLOwnedByDepartment(ctx context.Context, departmentID uint, urlValue string) (bool, error)
}
