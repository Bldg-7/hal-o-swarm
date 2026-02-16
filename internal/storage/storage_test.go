package storage

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateFresh(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	runner := NewMigrationRunner(db)
	if err := runner.Migrate(); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if !tableExists(t, db, "events") {
		t.Error("events table not created")
	}
	if !tableExists(t, db, "sessions") {
		t.Error("sessions table not created")
	}
	if !tableExists(t, db, "nodes") {
		t.Error("nodes table not created")
	}
	if !tableExists(t, db, "costs") {
		t.Error("costs table not created")
	}
	if !tableExists(t, db, "command_idempotency") {
		t.Error("command_idempotency table not created")
	}
	if !tableExists(t, db, "schema_migrations") {
		t.Error("schema_migrations table not created")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	runner := NewMigrationRunner(db)

	if err := runner.Migrate(); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	if err := runner.Migrate(); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query migrations: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 migration record, got %d", count)
	}
}

func TestMigrateChecksumMismatch(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	runner := NewMigrationRunner(db)

	if err := runner.Migrate(); err != nil {
		t.Fatalf("initial migration failed: %v", err)
	}

	_, err := db.Exec("UPDATE schema_migrations SET checksum = 'invalid' WHERE version = '001'")
	if err != nil {
		t.Fatalf("failed to corrupt checksum: %v", err)
	}

	if err := runner.Migrate(); err == nil {
		t.Error("expected checksum mismatch error, got nil")
	}
}

func setupTestDB(t *testing.T) *sql.DB {
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpfile.Close()

	db, err := sql.Open("sqlite", tmpfile.Name())
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
		os.Remove(tmpfile.Name())
	})

	return db
}

func tableExists(t *testing.T, db *sql.DB, tableName string) bool {
	var exists int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
		tableName,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("failed to check table existence: %v", err)
	}
	return exists > 0
}
