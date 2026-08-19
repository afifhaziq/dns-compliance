// Command import-cmod is a one-off, re-runnable CLI that imports the BLK
// sheet of Masterlist Blocking CMOD.xlsx into cases/case_letters/case_urls.
// See docs/cmod-blocking-list-migration-clarifications.md for the decided
// mapping and docs/superpowers/plans/2026-08-19-cases-schema-06-cmod-import.md
// for the implementation plan.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/afif/dns-tracking/internal/blockimport"
	"github.com/afif/dns-tracking/internal/db"
	"gorm.io/driver/postgres"
)

func main() {
	file := flag.String("file", "", "path to Masterlist Blocking CMOD.xlsx")
	dbURL := flag.String("db-url", "", "PostgreSQL DSN (key=value pairs)")
	dryRun := flag.Bool("dry-run", true, "print what would be imported without writing (default true — pass --dry-run=false for a real run)")
	flag.Parse()
	if *file == "" || *dbURL == "" {
		fmt.Fprintln(os.Stderr, "--file and --db-url are required")
		os.Exit(1)
	}

	rows, err := blockimport.ParseCMODRows(*file)
	if err != nil {
		log.Fatalf("parsing %s: %v", *file, err)
	}
	cases := blockimport.CollapseCMODRows(rows)

	gormDB, err := db.Connect(postgres.Open(*dbURL))
	if err != nil {
		log.Fatalf("connecting to db: %v", err)
	}
	var cmodDept db.Department
	if err := gormDB.Where("name = ?", "CMOD").First(&cmodDept).Error; err != nil {
		log.Fatalf("looking up CMOD department (run db.SeedDepartments first): %v", err)
	}

	summary, err := blockimport.WriteCMODCases(context.Background(), gormDB, cmodDept.ID, cases, *dryRun)
	if err != nil {
		log.Fatalf("importing: %v", err)
	}
	mode := "DRY RUN"
	if !*dryRun {
		mode = "LIVE"
	}
	fmt.Printf("[%s] %d rows parsed, %d cases collapsed\n", mode, len(rows), len(cases))
	fmt.Printf("  cases created:         %d\n", summary.CasesCreated)
	fmt.Printf("  cases already existed: %d\n", summary.CasesSkippedExist)
	fmt.Printf("  urls skipped (bad url): %d\n", summary.URLsSkippedBadURL)
	fmt.Printf("  distinct offences observed (not imported, see plan's Global Constraints): %d\n", len(summary.CategoriesObserved))
}
