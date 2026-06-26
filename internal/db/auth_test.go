package db_test

import (
	"context"
	"testing"

	"github.com/afif/dns-tracking/internal/db"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := db.HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "correct-horse" {
		t.Fatalf("expected hash to differ from plaintext")
	}
	if !db.CheckPassword(hash, "correct-horse") {
		t.Fatalf("expected CheckPassword to accept the correct password")
	}
	if db.CheckPassword(hash, "wrong-password") {
		t.Fatalf("expected CheckPassword to reject an incorrect password")
	}
}

func TestSeedAdmin_CreatesOnlyWhenEmpty(t *testing.T) {
	gormDB, s := rawConnect(t)
	ctx := context.Background()

	if err := db.MigrateAdminDepartments(gormDB); err != nil {
		t.Fatalf("MigrateAdminDepartments: %v", err)
	}
	if err := db.SeedAdmin(gormDB, "admin", "s3cret-pass"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}

	u, err := s.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u == nil {
		t.Fatalf("expected bootstrap admin user to exist")
	}
	if !u.IsAdmin {
		t.Fatalf("expected bootstrap user to be an admin")
	}
	if !db.CheckPassword(u.PasswordHash, "s3cret-pass") {
		t.Fatalf("expected stored hash to match the bootstrap password")
	}

	// Re-running with different credentials must be a no-op once a user exists.
	if err := db.SeedAdmin(gormDB, "someone-else", "different-pass"); err != nil {
		t.Fatalf("SeedAdmin (second run): %v", err)
	}
	again, _ := s.GetUserByUsername(ctx, "someone-else")
	if again != nil {
		t.Fatalf("expected SeedAdmin to be a no-op once users table is non-empty")
	}
}

func TestSeedAdmin_RequiresCredentialsWhenEmpty(t *testing.T) {
	gormDB, _ := rawConnect(t)
	if err := db.SeedAdmin(gormDB, "", ""); err == nil {
		t.Fatalf("expected an error when no bootstrap admin credentials are supplied and users table is empty")
	}
}
