package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anlekg/quarel/internal/sqlitedb"
)

var schema = []string{`CREATE TABLE notes (id INTEGER PRIMARY KEY, text TEXT)`, `ALTER TABLE notes ADD COLUMN author TEXT`}

func sampleDir(t *testing.T) (Spec, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dir, "server.db"), schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notes (text, author) VALUES ('bonjour', 'alice')`); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "server.key"), []byte("clé\n"), 0o600)
	os.MkdirAll(filepath.Join(dir, "attachments"), 0o700)
	os.WriteFile(filepath.Join(dir, "attachments", "f1"), []byte("fichier"), 0o600)
	return Spec{Kind: "quarel-server", DataDir: dir, Database: "server.db", Required: []string{"server.key"},
		Files: []string{"absent.keys"}, Dirs: []string{"attachments", "acme"}}, func() { db.Close() }
}

func TestBackupRestore(t *testing.T) {
	spec, closeDB := sampleDir(t)
	defer closeDB() // the database stays open: backups are taken while the service runs
	var buf bytes.Buffer
	m, err := Create(spec, "serveur abc", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 2 || m.Files != 3 {
		t.Fatalf("manifest = %+v", m)
	}
	archive := buf.Bytes()

	target := spec
	target.DataDir = filepath.Join(t.TempDir(), "data")
	if _, _, err := Restore(target, bytes.NewReader(archive), false, 2); err != nil {
		t.Fatal(err)
	}
	db, err := sqlitedb.Open(filepath.Join(target.DataDir, "server.db"), schema)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	db.QueryRow(`SELECT text FROM notes WHERE author = 'alice'`).Scan(&text)
	db.Close()
	if text != "bonjour" {
		t.Fatalf("restored database: %q", text)
	}
	if data, _ := os.ReadFile(filepath.Join(target.DataDir, "attachments", "f1")); string(data) != "fichier" {
		t.Fatal("attachment not restored")
	}

	// Existing data is never overwritten without --force, and is kept aside with it.
	if _, _, err := Restore(target, bytes.NewReader(archive), false, 2); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("restore over existing data: %v", err)
	}
	os.WriteFile(filepath.Join(target.DataDir, "server.key"), []byte("autre"), 0o600)
	_, kept, err := Restore(target, bytes.NewReader(archive), true, 2)
	if err != nil || kept == "" {
		t.Fatalf("forced restore: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(kept, "server.key")); string(data) != "autre" {
		t.Fatal("previous data not kept aside")
	}

	// Checks before touching anything.
	other := target
	other.Kind = "quarel-identity"
	if _, _, err := Restore(other, bytes.NewReader(archive), true, 2); err == nil || !strings.Contains(err.Error(), "pas d'un") {
		t.Fatalf("wrong kind: %v", err)
	}
	if _, _, err := Restore(target, bytes.NewReader(archive), true, 1); err == nil || !strings.Contains(err.Error(), "plus récente") {
		t.Fatalf("newer schema: %v", err)
	}
}

func TestRestoreRejectsTraversalAndGarbage(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	manifest, _ := json.Marshal(Manifest{Format: Format, Kind: "quarel-server", SchemaVersion: 1})
	for _, e := range []struct {
		name string
		data []byte
	}{{manifestName, manifest}, {"../../evil", []byte("x")}} {
		tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o600, Size: int64(len(e.data)), Typeflag: tar.TypeReg})
		tw.Write(e.data)
	}
	tw.Close()
	gz.Close()
	spec := Spec{Kind: "quarel-server", DataDir: filepath.Join(t.TempDir(), "d"), Database: "server.db"}
	if _, _, err := Restore(spec, &buf, false, 5); err == nil || !strings.Contains(err.Error(), "refusée") {
		t.Fatalf("path traversal accepted: %v", err)
	}
	if _, _, err := Restore(spec, strings.NewReader("pas une archive"), false, 5); err == nil {
		t.Fatal("garbage accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(spec.DataDir), "evil")); err == nil {
		t.Fatal("file written outside the data directory")
	}
}

func TestBackupNeedsKeys(t *testing.T) {
	spec, closeDB := sampleDir(t)
	defer closeDB()
	os.Remove(filepath.Join(spec.DataDir, "server.key"))
	if _, err := Create(spec, "", &bytes.Buffer{}); err == nil {
		t.Fatal("backup without the server key")
	}
}
