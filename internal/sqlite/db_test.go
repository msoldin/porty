package sqlite

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestOpenRestrictsPermissiveExistingDataDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := Open(context.Background(), filepath.Join(directory, "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("data directory mode = %04o, want 0700", got)
	}
}

func TestOpenAppliesMigrationsIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "porty.db")
	for attempt := 0; attempt < 2; attempt++ {
		db, err := Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open() attempt %d error = %v", attempt+1, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}

	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, table := range []string{"goose_db_version", "app_state", "repository_auth", "users", "sessions", "stacks", "stack_environment", "operations", "deployments", "audit_events"} {
		var count int
		err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
		if err != nil {
			t.Fatalf("query table %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %s count = %d, want 1", table, count)
		}
	}

	var migrations int
	if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM goose_db_version WHERE version_id=1 AND is_applied=1").Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 1 {
		t.Fatalf("migration count = %d, want 1", migrations)
	}
	var appStateRows int
	if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM app_state WHERE id=1 AND setup_state='unregistered'").Scan(&appStateRows); err != nil {
		t.Fatal(err)
	}
	if appStateRows != 1 {
		t.Fatalf("app_state seed rows = %d, want 1", appStateRows)
	}
	var foreignKeys, busyTimeout int
	var journalMode string
	for _, query := range []struct {
		SQL string
		Out any
	}{
		{"PRAGMA foreign_keys", &foreignKeys},
		{"PRAGMA busy_timeout", &busyTimeout},
		{"PRAGMA journal_mode", &journalMode},
	} {
		if err := db.QueryRowContext(context.Background(), query.SQL).Scan(query.Out); err != nil {
			t.Fatal(err)
		}
	}
	if foreignKeys != 1 || busyTimeout != 5000 || journalMode != "wal" {
		t.Fatalf("SQLite pragmas = foreign_keys:%d busy_timeout:%d journal_mode:%s", foreignKeys, busyTimeout, journalMode)
	}
	if db.Stats().MaxOpenConnections != 1 {
		t.Fatalf("max open connections = %d, want 1", db.Stats().MaxOpenConnections)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestOpenPreservesExistingUserDataWhenMigrationFails(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "porty.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.ExecContext(ctx, `CREATE TABLE users (marker TEXT); INSERT INTO users(marker) VALUES ('keep')`); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(ctx, path); err == nil {
		db.Close()
		t.Fatal("Open succeeded despite conflicting schema")
	}
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var marker string
	if err := check.QueryRowContext(ctx, `SELECT marker FROM users`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != "keep" {
		t.Fatalf("existing data = %q, want keep", marker)
	}
}

func TestInitialMigrationDownRemovesDevelopmentSchema(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"app_state", "repository_auth", "users", "sessions", "stacks", "stack_environment", "operations", "deployments", "audit_events"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("table %s remained after Down", table)
		}
	}
}
