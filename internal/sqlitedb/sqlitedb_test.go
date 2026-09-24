package sqlitedb

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotBeforeMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	v1 := []string{`CREATE TABLE t (a TEXT)`}
	db, err := Open(path, v1)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO t VALUES ('avant')`)
	db.Close()
	if snaps, _ := filepath.Glob(path + ".pre-v*"); len(snaps) != 0 {
		t.Fatal("snapshot on first creation")
	}

	db, err = Open(path, append(v1, `ALTER TABLE t ADD COLUMN b TEXT`))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	snaps, _ := filepath.Glob(path + ".pre-v*")
	if len(snaps) != 1 || !strings.Contains(snaps[0], ".pre-v1-") {
		t.Fatalf("snapshots = %v", snaps)
	}
	old, err := Open(snaps[0], v1) // the copy is the old schema, with the data
	if err != nil {
		t.Fatal(err)
	}
	var a string
	old.QueryRow(`SELECT a FROM t`).Scan(&a)
	old.Close()
	if a != "avant" {
		t.Fatalf("snapshot content = %q", a)
	}
	// An older program refuses a newer database instead of damaging it.
	if _, err := Open(path, v1); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("downgrade: %v", err)
	}
}
