// Command import-crd is a one-off CLI that imports CRD's Excel-tracked
// blocklist case history ("Blocking Full List_1.xlsx") into the
// cases/case_letters/case_urls schema. See
// docs/superpowers/plans/2026-08-19-cases-schema-05-crd-import.md and
// docs/blocking-list-migration-clarifications.md.
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
	file := flag.String("file", "", "path to Blocking Full List_1.xlsx")
	dbURL := flag.String("db-url", "", "PostgreSQL DSN (key=value pairs)")
	dryRun := flag.Bool("dry-run", true, "print what would be imported without writing (default true — pass --dry-run=false for a real run)")
	flag.Parse()
	if *file == "" || *dbURL == "" {
		fmt.Fprintln(os.Stderr, "--file and --db-url are required")
		os.Exit(1)
	}

	rows, err := blockimport.ParseCRDRows(*file)
	if err != nil {
		log.Fatalf("parsing %s: %v", *file, err)
	}
	cases := blockimport.CollapseCRDRows(rows)

	gormDB, err := db.Connect(postgres.Open(*dbURL))
	if err != nil {
		log.Fatalf("connecting to db: %v", err)
	}
	var crdDept db.Department
	if err := gormDB.Where("name = ?", "CRD").First(&crdDept).Error; err != nil {
		log.Fatalf("looking up CRD department (run db.SeedDepartments first): %v", err)
	}

	summary, err := blockimport.WriteCRDCases(context.Background(), gormDB, crdDept.ID, cases, *dryRun)
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
	fmt.Printf("  distinct categories observed (not imported, see plan's Global Constraints): %d\n", len(summary.CategoriesObserved))
}
