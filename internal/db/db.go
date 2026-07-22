package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect opens a database connection using the given dialector and runs AutoMigrate.
func Connect(dialector gorm.Dialector) (*gorm.DB, error) {
	gormLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
		},
	)
	database, err := gorm.Open(dialector, &gorm.Config{Logger: gormLogger})
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}
	if err := database.AutoMigrate(
		&Department{}, &User{}, &Session{}, &DNSServer{}, &URL{}, &DepartmentURL{}, &ScanRun{}, &ScanResult{}, &CompliantIP{}, &DomainWhois{}, &IPInfo{}, &Favicon{}, &ScanSettings{}, &SubdomainScan{},
	); err != nil {
		return nil, fmt.Errorf("migrating schema: %w", err)
	}
	return database, nil
}

// Seed inserts default DNS servers if the dns_servers table is empty.
func Seed(database *gorm.DB, entries []DNSServer) error {
	var count int64
	database.Model(&DNSServer{}).Count(&count)
	if count > 0 {
		return nil
	}
	return database.Create(&entries).Error
}

// SeedDepartments inserts the fixed CMOD/CRD/Admin departments if the
// departments table is empty. More departments can be added later just by
// inserting rows; this seed only covers the initial bootstrap.
func SeedDepartments(database *gorm.DB) error {
	var count int64
	database.Model(&Department{}).Count(&count)
	if count > 0 {
		return nil
	}
	return database.Create(&[]Department{{Name: "CMOD"}, {Name: "CRD"}, {Name: "Admin"}}).Error
}

// SeedScanInterval creates the single ScanSettings row from the --interval
// flag if it doesn't exist yet. After the first boot, the admin panel is
// authoritative and this is a no-op.
func SeedScanInterval(database *gorm.DB, minutes int) error {
	var count int64
	database.Model(&ScanSettings{}).Count(&count)
	if count > 0 {
		return nil
	}
	return database.Create(&ScanSettings{ID: 1, IntervalMinutes: minutes, Enabled: true}).Error
}

// MigrateAdminDepartments ensures an "Admin" department exists and updates
// any admin users whose DepartmentID is nil to point to it. Idempotent —
// safe to call on every startup.
func MigrateAdminDepartments(database *gorm.DB) error {
	var adminDept Department
	if err := database.
		Where("name = ?", "Admin").
		FirstOrCreate(&adminDept, Department{Name: "Admin"}).Error; err != nil {
		return fmt.Errorf("ensure admin department: %w", err)
	}
	return database.Model(&User{}).
		Where("is_admin = ? AND department_id IS NULL", true).
		Update("department_id", adminDept.ID).Error
}
