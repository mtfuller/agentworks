package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"
)

func TestOpenAppliesMigrationsAndPragmas(t *testing.T) {
	store := openTestStore(t)

	version, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version != 9 {
		t.Fatalf("schema version = %d, want 9", version)
	}
	var journalMode string
	if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}
	var foreignKeys, busyTimeout int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || busyTimeout != 5000 {
		t.Fatalf("foreign_keys=%d busy_timeout=%d", foreignKeys, busyTimeout)
	}

	wantTables := []string{
		"approvals", "events", "memory_audits", "memory_proposals", "monitor_states", "outcomes", "run_attempts", "run_conclusions", "run_leases",
		"runs", "runtime_events", "source_cursors", "source_poll_attempts", "sources", "subscriptions", "timers", "work_items",
	}
	for _, table := range wantTables {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s missing", table)
		}
	}
}

func TestMigrationFailureRollsBackOnlyFailingMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dataSourceName(path, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrations := fstest.MapFS{
		"migrations/001_good.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE good (id INTEGER PRIMARY KEY) STRICT;`)},
		"migrations/002_bad.sql":  &fstest.MapFile{Data: []byte(`CREATE TABLE incomplete (id INTEGER); THIS IS NOT SQL;`)},
	}
	if err := applyMigrations(context.Background(), db, migrations); err == nil {
		t.Fatal("applyMigrations succeeded, want failure")
	}
	assertTableCount(t, db, "good", 1)
	assertTableCount(t, db, "incomplete", 0)
	var versions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Fatalf("recorded migrations = %d, want 1", versions)
	}
}

func TestMigrationRejectsBadFilename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dataSourceName(path, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bad := fstest.MapFS{"migrations/no-version.sql": &fstest.MapFile{Data: []byte(`SELECT 1;`)}}
	if err := applyMigrations(context.Background(), db, bad); err == nil {
		t.Fatal("applyMigrations succeeded, want filename error")
	}
}

func TestMigrationHistoryIsForwardOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dataSourceName(path, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	original := fstest.MapFS{
		"migrations/001_first.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE first (id INTEGER PRIMARY KEY) STRICT;`)},
	}
	if err := applyMigrations(ctx, db, original); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(ctx, db, original); err != nil {
		t.Fatalf("reapplying unchanged migrations: %v", err)
	}
	changed := fstest.MapFS{
		"migrations/001_first.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE edited (id INTEGER PRIMARY KEY) STRICT;`)},
	}
	if err := applyMigrations(ctx, db, changed); err == nil {
		t.Fatal("edited applied migration was accepted")
	}
	assertTableCount(t, db, "edited", 0)
	duplicateVersion := fstest.MapFS{
		"migrations/001_renamed.sql": &fstest.MapFile{Data: []byte(`SELECT 1;`)},
	}
	if err := applyMigrations(ctx, db, duplicateVersion); err == nil {
		t.Fatal("renamed applied migration was accepted")
	}
}

func TestMigrationUpgradesVersionOneDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dataSourceName(path, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, err := fs.ReadFile(migrationFiles, "migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	versionOne := fstest.MapFS{
		"migrations/001_initial.sql": &fstest.MapFile{Data: first},
	}
	ctx := context.Background()
	if err := applyMigrations(ctx, db, versionOne); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(ctx, db, migrationFiles); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 9 {
		t.Fatalf("schema version = %d, want 9", version)
	}
	var harnessDefault string
	rows, err := db.Query(`PRAGMA table_info(runs)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "harness" {
			harnessDefault = fmt.Sprint(defaultValue)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if harnessDefault != "'claude-code'" {
		t.Fatalf("harness default = %q", harnessDefault)
	}
}

func TestCommittedStateSurvivesReopenAndRollbackDoesNot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	if _, _, err := store.CreateRun(ctx, testRun("committed", now)); err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack := testRun("rolled-back", now)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO runs(id, idempotency_key, agent, workspace, harness, permission, state, pinned, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
	`, rolledBack.ID, rolledBack.IdempotencyKey, rolledBack.Agent, rolledBack.Workspace,
		rolledBack.Harness, rolledBack.Permission, rolledBack.State, millis(now), millis(now)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.GetRun(ctx, "committed"); err != nil {
		t.Fatalf("committed run: %v", err)
	}
	if _, err := reopened.GetRun(ctx, "rolled-back"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back run error = %v, want ErrNotFound", err)
	}
}

func TestConcurrentIdempotentRunCreation(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	const workers = 24
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	createdCh := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			run := testRun("shared", now)
			stored, created, err := store.CreateRun(ctx, run)
			if err == nil && stored.ID != "shared" {
				err = fmt.Errorf("stored ID = %q", stored.ID)
			}
			errCh <- err
			createdCh <- created
		}(i)
	}
	wg.Wait()
	close(errCh)
	close(createdCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	created := 0
	for value := range createdCh {
		if value {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created count = %d, want 1", created)
	}
	runs, err := store.ListRuns(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(runs))
	}
}

func TestWALAllowsReaderWhileWriterCommits(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	reader, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	var before int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateRun(ctx, testRun("during-reader", time.Now().UTC())); err != nil {
		t.Fatalf("writer blocked by reader: %v", err)
	}
	var snapshot int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if before != 0 || snapshot != 0 {
		t.Fatalf("reader snapshot changed: before=%d after=%d", before, snapshot)
	}
	if err := reader.Commit(); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("visible runs after reader commit = %d, want 1", len(runs))
	}
}

func TestDataSourceName(t *testing.T) {
	dsn := dataSourceName(filepath.Join(t.TempDir(), "state with spaces.db"), 2500*time.Millisecond)
	if dsn == "" {
		t.Fatal("empty DSN")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	var timeout int
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout != 2500 {
		t.Fatalf("busy timeout = %d, want 2500", timeout)
	}
}

func TestMigrationVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
		ok   bool
	}{{"001_initial.sql", 1, true}, {"12_more.sql", 12, true}, {"bad.sql", 0, false}, {"000_nope.sql", 0, false}} {
		got, err := migrationVersion(tc.name)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("migrationVersion(%q) = %d, %v", tc.name, got, err)
		}
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return store
}

func testRun(id string, now time.Time) Run {
	return Run{
		ID:             id,
		IdempotencyKey: "manual:" + id,
		Agent:          "builder",
		Workspace:      "default",
		Harness:        "claude-code",
		Permission:     PermissionReadwrite,
		State:          RunPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func assertTableCount(t *testing.T, db *sql.DB, name string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("table %s count = %d, want %d", name, count, want)
	}
}

var _ fs.FS = fstest.MapFS{}
