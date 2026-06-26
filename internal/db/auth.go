package db

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// HashPassword bcrypt-hashes plain for storage in User.PasswordHash. Lives
// here (not internal/server) so both the bootstrap-admin seed below and the
// login handler hash/verify against the exact same algorithm.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(hash), err
}

// CheckPassword reports whether plain matches the given bcrypt hash.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// SeedAdmin creates the bootstrap admin user only if the users table is
// empty — every subsequent startup is a no-op regardless of the username
// and password passed in. Without this, a fresh deployment would have no
// way to log in at all. The bootstrap admin is assigned to the "Admin"
// department (MigrateAdminDepartments must run before this).
func SeedAdmin(database *gorm.DB, username, password string) error {
	var count int64
	if err := database.Model(&User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if username == "" || password == "" {
		return fmt.Errorf("db: bootstrap admin username and password are required when the users table is empty")
	}
	var adminDept Department
	if err := database.Where("name = ?", "Admin").First(&adminDept).Error; err != nil {
		return fmt.Errorf("db: admin department not found (run MigrateAdminDepartments first): %w", err)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return database.Create(&User{
		Username:     username,
		PasswordHash: hash,
		IsAdmin:      true,
		DepartmentID: &adminDept.ID,
	}).Error
}
