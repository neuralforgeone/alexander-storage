// Package main is the entry point for the Alexander Storage database migration tool.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Version information (set at build time)
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

const defaultMigrationsPath = "migrations/postgres"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "version":
		fmt.Printf("Alexander Storage Migration Tool\n")
		fmt.Printf("Version: %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Git Commit: %s\n", GitCommit)

	case "up":
		if err := runMigrations(0); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Migrations applied successfully")

	case "down":
		if err := runMigrations(-1); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Rolled back last migration")

	case "status":
		if err := showStatus(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "create":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: alexander-migrate create <name>")
			os.Exit(1)
		}
		if err := createMigration(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "force":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: alexander-migrate force <version>")
			os.Exit(1)
		}
		version, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid version: %v\n", err)
			os.Exit(1)
		}
		if err := forceVersion(version); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Forced migration version to %d\n", version)

	case "help", "-h", "--help":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func databaseURL() (string, error) {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url, nil
	}
	return "", fmt.Errorf("DATABASE_URL environment variable is required")
}

func migrationsSource() (string, error) {
	path := os.Getenv("MIGRATIONS_PATH")
	if path == "" {
		path = defaultMigrationsPath
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("migrations path not found: %s", abs)
	}
	return "file://" + filepath.ToSlash(abs), nil
}

func newMigrator() (*migrate.Migrate, error) {
	dbURL, err := databaseURL()
	if err != nil {
		return nil, err
	}
	source, err := migrationsSource()
	if err != nil {
		return nil, err
	}
	return migrate.New(source, dbURL)
}

func runMigrations(steps int) error {
	m, err := newMigrator()
	if err != nil {
		return err
	}
	defer m.Close()

	if steps == 0 {
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			return err
		}
		return nil
	}
	if err := m.Steps(steps); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

func showStatus() error {
	m, err := newMigrator()
	if err != nil {
		return err
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err == migrate.ErrNilVersion {
		fmt.Println("Status: no migrations applied")
		return nil
	}
	if err != nil {
		return err
	}

	fmt.Printf("Current version: %d\n", version)
	if dirty {
		fmt.Println("State: DIRTY (manual intervention required)")
	} else {
		fmt.Println("State: clean")
	}
	return nil
}

func forceVersion(version int) error {
	m, err := newMigrator()
	if err != nil {
		return err
	}
	defer m.Close()
	return m.Force(version)
}

func createMigration(name string) error {
	name = strings.ReplaceAll(name, " ", "_")
	next, err := nextMigrationVersion()
	if err != nil {
		return err
	}

	dir := os.Getenv("MIGRATIONS_PATH")
	if dir == "" {
		dir = defaultMigrationsPath
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	version := fmt.Sprintf("%06d", next)
	upPath := filepath.Join(dir, version+"_"+name+".up.sql")
	downPath := filepath.Join(dir, version+"_"+name+".down.sql")

	upContent := fmt.Sprintf("-- Migration: %s\n-- Created: %s\n\n", name, time.Now().Format(time.RFC3339))
	downContent := fmt.Sprintf("-- Rollback: %s\n-- Created: %s\n\n", name, time.Now().Format(time.RFC3339))

	if err := os.WriteFile(upPath, []byte(upContent), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(downPath, []byte(downContent), 0644); err != nil {
		return err
	}

	fmt.Printf("Created migration files:\n  %s\n  %s\n", upPath, downPath)
	return nil
}

func nextMigrationVersion() (int, error) {
	dir := os.Getenv("MIGRATIONS_PATH")
	if dir == "" {
		dir = defaultMigrationsPath
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}

	maxVersion := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		if v > maxVersion {
			maxVersion = v
		}
	}

	versions := make([]int, 0)
	for _, entry := range entries {
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) >= 1 {
			if v, err := strconv.Atoi(parts[0]); err == nil {
				versions = append(versions, v)
			}
		}
	}
	sort.Ints(versions)
	_ = versions

	return maxVersion + 1, nil
}

func printUsage() {
	fmt.Println(`Alexander Storage Migration Tool

Usage:
  alexander-migrate <command> [arguments]

Commands:
  up          Run all pending migrations
  down        Rollback the last migration
  status      Show current migration status
  create      Create a new migration file
  force       Force set migration version (use with caution)
  version     Print version information
  help        Show this help message

Environment Variables:
  DATABASE_URL      PostgreSQL connection string
  MIGRATIONS_PATH   Path to migration files (default: migrations/postgres)

Examples:
  alexander-migrate up
  alexander-migrate down
  alexander-migrate status
  alexander-migrate create add_indexes
  alexander-migrate force 5`)
}