// Package sqlitedb opens SQLite databases and applies schema migrations.
package sqlitedb

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// Open opens (creating if needed) the database at path and applies
// migrations in order. The number of applied migrations is stored in
// PRAGMA user_version: never edit an existing migration, only append.
func Open(path string, migrations []string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection serialises writes and avoids SQLITE_BUSY; plenty for these workloads.
	db.SetMaxOpenConns(1)
	if err := migrate(db, path, migrations); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// keptSnapshots is how many pre-migration copies are kept next to the database.
const keptSnapshots = 3

func migrate(db *sql.DB, path string, migrations []string) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this program (%d): this is an older version of Quarel; install the newer one again (or restore a backup made with this version)", version, len(migrations))
	}
	if version > 0 && version < len(migrations) {
		// An update is about to change the schema: keep a copy of the data first.
		snap := fmt.Sprintf("%s.pre-v%d-%s", path, version, time.Now().Format("20060102-150405"))
		if _, err := db.Exec(`VACUUM INTO ?`, snap); err != nil {
			return fmt.Errorf("copy before migrating: %w", err)
		}
		slog.Info("database copied before updating its schema", "copy", snap, "from", version, "to", len(migrations))
		pruneSnapshots(path)
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// pruneSnapshots keeps the most recent pre-migration copies.
func pruneSnapshots(path string) {
	old, _ := filepath.Glob(path + ".pre-v*")
	sort.Slice(old, func(i, j int) bool {
		a, _ := os.Stat(old[i])
		b, _ := os.Stat(old[j])
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	for i, f := range old {
		if i >= keptSnapshots {
			os.Remove(f)
		}
	}
}
