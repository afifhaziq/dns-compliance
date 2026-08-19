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
	// The case-metadata columns (due_date, agency, reference_number,
	// requesting_dept, status, requested_at) moved off DepartmentURL onto
	// URL — a domain has one legal case, not one per watching department.
	// AutoMigrate only adds columns matching current struct tags, it never
	// drops ones that used to exist — without this, an already-migrated dev
	// DB would keep six dead columns on department_urls forever. This
	// branch has never been deployed with real data, so this is a plain
	// idempotent drop, not a data-preserving copy: a no-op once the columns
	// are gone, including on a fresh DB where they never existed.
	for _, col := range []string{"due_date", "agency", "reference_number", "requesting_dept", "status", "requested_at"} {
		if database.Migrator().HasColumn(&DepartmentURL{}, col) {
			if err := database.Migrator().DropColumn(&DepartmentURL{}, col); err != nil {
				return nil, fmt.Errorf("dropping department_urls.%s: %w", col, err)
			}
		}
	}
	// DueDatePreset.Hours was renamed to Minutes for finer-grained "Time to
	// Block" durations. This must run before AutoMigrate: AutoMigrate would
	// try to add the new column as NOT NULL (per the struct tag) in one
	// step, which Postgres rejects on a non-empty table with no default —
	// so add it nullable, backfill from the old column, then tighten it,
	// leaving AutoMigrate's own pass over DueDatePreset a no-op.
	if database.Migrator().HasColumn(&DueDatePreset{}, "hours") && !database.Migrator().HasColumn(&DueDatePreset{}, "minutes") {
		if err := database.Exec("ALTER TABLE due_date_presets ADD COLUMN minutes bigint").Error; err != nil {
			return nil, fmt.Errorf("adding due_date_presets.minutes: %w", err)
		}
		if err := database.Exec("UPDATE due_date_presets SET minutes = hours * 60").Error; err != nil {
			return nil, fmt.Errorf("backfilling due_date_presets.minutes: %w", err)
		}
		if err := database.Exec("ALTER TABLE due_date_presets ALTER COLUMN minutes SET NOT NULL").Error; err != nil {
			return nil, fmt.Errorf("setting due_date_presets.minutes not null: %w", err)
		}
	}
	if database.Migrator().HasColumn(&DueDatePreset{}, "hours") {
		if err := database.Migrator().DropColumn(&DueDatePreset{}, "hours"); err != nil {
			return nil, fmt.Errorf("dropping due_date_presets.hours: %w", err)
		}
	}
	if err := database.AutoMigrate(
		&Department{}, &User{}, &Session{}, &DNSServer{}, &URL{}, &DepartmentURL{}, &ScanRun{}, &ScanResult{}, &CompliantIP{}, &DomainWhois{}, &IPInfo{}, &Favicon{}, &ScanSettings{}, &SubdomainScan{}, &ISPLogo{},
		&Instrument{}, &Citation{}, &Category{}, &Element{}, &SubElement{}, &URLOffence{},
		&Agency{}, &DueDatePreset{}, &GridPreference{}, &Notification{},
		&Case{}, &CaseLetter{}, &CaseURL{},
	); err != nil {
		return nil, fmt.Errorf("migrating schema: %w", err)
	}
	// urls.reference_number/urls.requesting_dept_id are replaced by the
	// cases/case_letters/case_urls tables — a url can carry many reference
	// numbers over its history and a single reference number legitimately
	// covers many urls (see docs/db-schema.dbml's cases table note). Runs
	// after AutoMigrate (which is purely additive — it never drops or
	// renames the old columns) so cases/case_letters/case_urls already
	// exist to receive the backfilled rows, then drops the two old columns
	// once every row has been moved.
	if err := backfillURLReferenceNumbersIntoCases(database); err != nil {
		return nil, fmt.Errorf("backfilling urls.reference_number into cases: %w", err)
	}
	return database, nil
}

// backfillURLReferenceNumbersIntoCases losslessly moves any populated
// urls.reference_number/urls.requesting_dept_id values into
// cases/case_letters/case_urls, then drops both columns. Not reversible —
// must complete in the same Connect call that reads the old columns, never
// as a separate manual step. Idempotent: a no-op once the columns are gone,
// including on a fresh DB where they never existed.
func backfillURLReferenceNumbersIntoCases(database *gorm.DB) error {
	if database.Migrator().HasColumn(&URL{}, "reference_number") || database.Migrator().HasColumn(&URL{}, "requesting_dept_id") {
		if err := database.Transaction(func(tx *gorm.DB) error {
			type legacyURLCaseRow struct {
				ID               uint
				ReferenceNumber  string
				RequestingDeptID *uint
				Status           string
			}
			var rows []legacyURLCaseRow
			if err := tx.Table("urls").
				Select("id, reference_number, requesting_dept_id, status").
				Where("reference_number <> '' OR requesting_dept_id IS NOT NULL").
				Find(&rows).Error; err != nil {
				return fmt.Errorf("loading legacy case metadata: %w", err)
			}

			for _, row := range rows {
				if row.RequestingDeptID == nil {
					// A reference number with no requesting department has no
					// department to attribute a case to — log and skip, same
					// "log and skip, non-fatal" philosophy BackfillURLValues
					// already uses for imperfect backfills.
					log.Printf("db: urls.id=%d has reference_number %q but no requesting_dept_id, skipping case backfill", row.ID, row.ReferenceNumber)
					continue
				}
				c := Case{DepartmentID: *row.RequestingDeptID}
				if err := tx.Create(&c).Error; err != nil {
					return fmt.Errorf("creating case for url id=%d: %w", row.ID, err)
				}
				if err := tx.Create(&CaseLetter{CaseID: c.ID, Type: "Notice", ReferenceNumber: row.ReferenceNumber}).Error; err != nil {
					return fmt.Errorf("creating case_letter for url id=%d: %w", row.ID, err)
				}
				phase := row.Status
				if phase == "" {
					phase = "requested"
				}
				if err := tx.Create(&CaseURL{CaseID: c.ID, URLID: row.ID, Phase: phase}).Error; err != nil {
					return fmt.Errorf("creating case_url for url id=%d: %w", row.ID, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	if database.Migrator().HasColumn(&URL{}, "reference_number") {
		if err := database.Migrator().DropColumn(&URL{}, "reference_number"); err != nil {
			return fmt.Errorf("dropping urls.reference_number: %w", err)
		}
	}
	if database.Migrator().HasColumn(&URL{}, "requesting_dept_id") {
		if err := database.Migrator().DropColumn(&URL{}, "requesting_dept_id"); err != nil {
			return fmt.Errorf("dropping urls.requesting_dept_id: %w", err)
		}
	}
	return nil
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

// SeedDueDatePresets inserts the original hardcoded "Time to Block" duration
// options (6/24/48/72 hours, 7 days) if the due_date_presets table is
// empty, so existing installs see the same choices they always did before
// this became admin/dept-admin-configurable. After the first boot, the
// watchlist page's own management dialog is authoritative.
func SeedDueDatePresets(database *gorm.DB) error {
	var count int64
	database.Model(&DueDatePreset{}).Count(&count)
	if count > 0 {
		return nil
	}
	return database.Create(&[]DueDatePreset{
		{Label: "6 hours", Minutes: 6 * 60},
		{Label: "24 hours", Minutes: 24 * 60},
		{Label: "48 hours", Minutes: 48 * 60},
		{Label: "72 hours", Minutes: 72 * 60},
		{Label: "7 days", Minutes: 7 * 24 * 60},
	}).Error
}

// SeedScanInterval creates the single ScanSettings row from the --interval/
// --sla-interval/--sla-streak-threshold flags if it doesn't exist yet, with
// the schedule disabled — automated scanning is an explicit admin opt-in,
// not a fresh deployment's default. After the first boot, the admin panel
// is authoritative and this is a no-op.
func SeedScanInterval(database *gorm.DB, minutes, slaMinutes, slaStreakThreshold int) error {
	var count int64
	database.Model(&ScanSettings{}).Count(&count)
	if count > 0 {
		return nil
	}
	return database.Create(&ScanSettings{
		ID:                 1,
		IntervalMinutes:    minutes,
		Enabled:            false,
		SLAIntervalMinutes: slaMinutes,
		SLAStreakThreshold: slaStreakThreshold,
	}).Error
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
