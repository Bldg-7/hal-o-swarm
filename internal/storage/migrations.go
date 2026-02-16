package storage

import (
	"crypto/md5"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration represents a single migration file
type Migration struct {
	Version  string
	Filename string
	Content  string
	Checksum string
}

// MigrationRunner handles applying migrations to the database
type MigrationRunner struct {
	db *sql.DB
}

// NewMigrationRunner creates a new migration runner
func NewMigrationRunner(db *sql.DB) *MigrationRunner {
	return &MigrationRunner{db: db}
}

// Migrate applies all pending migrations to the database
func (mr *MigrationRunner) Migrate() error {
	// Enable WAL mode for better concurrency
	if _, err := mr.db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Create schema_migrations table if it doesn't exist
	if err := mr.createMigrationsTable(); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Load all migrations from embedded filesystem
	migrations, err := mr.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Apply each migration that hasn't been applied yet
	for _, migration := range migrations {
		if err := mr.applyMigration(migration); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", migration.Version, err)
		}
	}

	return nil
}

// createMigrationsTable creates the schema_migrations tracking table
func (mr *MigrationRunner) createMigrationsTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		checksum TEXT NOT NULL,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)
	`
	_, err := mr.db.Exec(query)
	return err
}

// loadMigrations loads all migration files from the embedded filesystem
func (mr *MigrationRunner) loadMigrations() ([]Migration, error) {
	var migrations []Migration

	// Read all SQL files from migrations directory
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		content, err := fs.ReadFile(migrationsFS, filepath.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %w", entry.Name(), err)
		}

		// Extract version from filename (e.g., "001_initial_schema.sql" -> "001")
		version := strings.Split(entry.Name(), "_")[0]
		checksum := calculateChecksum(string(content))

		migrations = append(migrations, Migration{
			Version:  version,
			Filename: entry.Name(),
			Content:  string(content),
			Checksum: checksum,
		})
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// applyMigration applies a single migration if it hasn't been applied yet
func (mr *MigrationRunner) applyMigration(migration Migration) error {
	// Check if migration has already been applied
	var existingChecksum string
	err := mr.db.QueryRow(
		"SELECT checksum FROM schema_migrations WHERE version = ?",
		migration.Version,
	).Scan(&existingChecksum)

	if err == nil {
		// Migration already applied, verify checksum
		if existingChecksum != migration.Checksum {
			return fmt.Errorf(
				"checksum mismatch for migration %s: expected %s, got %s",
				migration.Version,
				existingChecksum,
				migration.Checksum,
			)
		}
		// Migration already applied with correct checksum, skip
		return nil
	}

	if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	// Apply the migration
	if _, err := mr.db.Exec(migration.Content); err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// Record the migration as applied
	_, err = mr.db.Exec(
		"INSERT INTO schema_migrations (version, checksum) VALUES (?, ?)",
		migration.Version,
		migration.Checksum,
	)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return nil
}

// calculateChecksum computes MD5 checksum of migration content
func calculateChecksum(content string) string {
	hash := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", hash)
}
