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
	if err := database.AutoMigrate(&DNSServer{}, &URL{}, &ScanRun{}, &ScanResult{}); err != nil {
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
