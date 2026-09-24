// Package backup exports and restores the data directory of a Quarel
// service as a single .tar.gz archive: a consistent snapshot of the SQLite
// database (taken with VACUUM INTO, safe while the service runs), the keys,
// and the stored files. Restoring checks the archive before touching
// anything and keeps the previous data aside.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Format is the version of the archive layout.
const Format = 1

const manifestName = "quarel-backup.json"

// Spec describes what a service keeps in its data directory.
type Spec struct {
	Kind     string   // "quarel-server" or "quarel-identity": an archive restores only into its kind
	DataDir  string   // the service's data directory
	Database string   // database file name, e.g. "server.db"
	Required []string // files that must exist (keys): a backup without them is useless
	Files    []string // other files, included when present
	Dirs     []string // directories copied recursively (regular files only)
}

// Manifest is stored first in every archive.
type Manifest struct {
	Format        int       `json:"format"`
	Kind          string    `json:"kind"`
	CreatedAt     time.Time `json:"created_at"`
	SchemaVersion int       `json:"schema_version"`
	Identity      string    `json:"identity,omitempty"` // server ID or issuer, for the operator to check
	Files         int       `json:"files"`
}

// Create writes an archive of spec's data directory to out.
func Create(spec Spec, identity string, out io.Writer) (Manifest, error) {
	m := Manifest{Format: Format, Kind: spec.Kind, CreatedAt: time.Now().UTC().Truncate(time.Second), Identity: identity}
	for _, f := range spec.Required {
		if _, err := os.Stat(filepath.Join(spec.DataDir, f)); err != nil {
			return m, fmt.Errorf("%s introuvable dans %s : est-ce le bon dossier de données ?", f, spec.DataDir)
		}
	}
	tmp, err := os.MkdirTemp("", "quarel-backup-")
	if err != nil {
		return m, err
	}
	defer os.RemoveAll(tmp)
	snapshot := filepath.Join(tmp, spec.Database)
	if m.SchemaVersion, err = Snapshot(filepath.Join(spec.DataDir, spec.Database), snapshot); err != nil {
		return m, err
	}

	type entry struct{ name, src string }
	entries := []entry{{spec.Database, snapshot}}
	for _, f := range append(append([]string{}, spec.Required...), spec.Files...) {
		if _, err := os.Stat(filepath.Join(spec.DataDir, f)); err == nil {
			entries = append(entries, entry{f, filepath.Join(spec.DataDir, f)})
		}
	}
	for _, d := range spec.Dirs {
		root := filepath.Join(spec.DataDir, d)
		err := filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if !de.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(spec.DataDir, p)
			entries = append(entries, entry{filepath.ToSlash(rel), p})
			return nil
		})
		if err != nil {
			return m, err
		}
	}
	m.Files = len(entries)

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	mdata, _ := json.MarshalIndent(m, "", "  ")
	if err := writeEntry(tw, manifestName, int64(len(mdata)), strings.NewReader(string(mdata))); err != nil {
		return m, err
	}
	for _, e := range entries {
		f, err := os.Open(e.src)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) { // an attachment deleted meanwhile
				m.Files--
				continue
			}
			return m, err
		}
		st, err := f.Stat()
		if err == nil {
			err = writeEntry(tw, e.name, st.Size(), f)
		}
		f.Close()
		if err != nil {
			return m, err
		}
	}
	if err := tw.Close(); err != nil {
		return m, err
	}
	return m, gz.Close()
}

func writeEntry(tw *tar.Writer, name string, size int64, r io.Reader) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: time.Now(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := io.CopyN(tw, r, size)
	return err
}

// Snapshot copies a live SQLite database consistently and returns its schema version.
func Snapshot(src, dst string) (int, error) {
	if _, err := os.Stat(src); err != nil {
		return 0, err
	}
	db, err := sql.Open("sqlite", "file:"+src+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	if _, err := db.Exec(`VACUUM INTO ?`, dst); err != nil {
		return 0, fmt.Errorf("snapshot of %s: %w", src, err)
	}
	var version int
	err = db.QueryRow(`PRAGMA user_version`).Scan(&version)
	return version, err
}

// Restore checks an archive, then puts its content in spec.DataDir. Existing
// data is only replaced with force, and is then moved to a
// "before-restore-<time>" folder inside the data directory. maxSchema is the
// newest database version this program understands. The service must be stopped.
func Restore(spec Spec, in io.Reader, force bool, maxSchema int) (Manifest, string, error) {
	var m Manifest
	if err := os.MkdirAll(spec.DataDir, 0o700); err != nil {
		return m, "", err
	}
	staging, err := os.MkdirTemp(spec.DataDir, ".restore-")
	if err != nil {
		return m, "", err
	}
	defer os.RemoveAll(staging)

	gz, err := gzip.NewReader(in)
	if err != nil {
		return m, "", errors.New("ce fichier n'est pas une sauvegarde Quarel (gzip attendu)")
	}
	tr := tar.NewReader(gz)
	first := true
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return m, "", fmt.Errorf("archive illisible : %w", err)
		}
		name := path.Clean(h.Name)
		if h.Typeflag != tar.TypeReg || strings.HasPrefix(name, "../") || name == ".." || path.IsAbs(name) || strings.Contains(name, "\\") {
			return m, "", fmt.Errorf("entrée refusée dans l'archive : %q", h.Name)
		}
		if first {
			if name != manifestName {
				return m, "", errors.New("ce fichier n'est pas une sauvegarde Quarel (manifeste absent)")
			}
			if err := json.NewDecoder(io.LimitReader(tr, 1<<16)).Decode(&m); err != nil {
				return m, "", fmt.Errorf("manifeste illisible : %w", err)
			}
			switch {
			case m.Format != Format:
				return m, "", fmt.Errorf("format de sauvegarde %d non pris en charge", m.Format)
			case m.Kind != spec.Kind:
				return m, "", fmt.Errorf("cette sauvegarde est celle d'un %s, pas d'un %s", m.Kind, spec.Kind)
			case m.SchemaVersion > maxSchema:
				return m, "", fmt.Errorf("sauvegarde faite par une version plus récente (base v%d, ce programme connaît v%d) : mettez Quarel à jour d'abord", m.SchemaVersion, maxSchema)
			}
			first = false
			continue
		}
		dst := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return m, "", err
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return m, "", err
		}
		_, err = io.Copy(f, tr)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return m, "", err
		}
	}
	if first {
		return m, "", errors.New("archive vide")
	}
	for _, f := range append([]string{spec.Database}, spec.Required...) {
		if _, err := os.Stat(filepath.Join(staging, f)); err != nil {
			return m, "", fmt.Errorf("sauvegarde incomplète : %s manquant", f)
		}
	}

	// Move the current data aside (never delete it).
	var kept string
	current, err := os.ReadDir(spec.DataDir)
	if err != nil {
		return m, "", err
	}
	var existing []string
	for _, e := range current {
		if n := e.Name(); n != filepath.Base(staging) && !strings.HasPrefix(n, "before-restore-") && !strings.HasPrefix(n, ".restore-") {
			existing = append(existing, n)
		}
	}
	if len(existing) > 0 {
		if !force {
			return m, "", fmt.Errorf("%s contient déjà des données : arrêtez le service et relancez avec --force pour les remplacer (elles seront mises de côté, pas effacées)", spec.DataDir)
		}
		kept = filepath.Join(spec.DataDir, "before-restore-"+time.Now().Format("20060102-150405"))
		if err := os.Mkdir(kept, 0o700); err != nil {
			return m, "", err
		}
		for _, n := range existing {
			if err := os.Rename(filepath.Join(spec.DataDir, n), filepath.Join(kept, n)); err != nil {
				return m, "", err
			}
		}
	}
	restored, err := os.ReadDir(staging)
	if err != nil {
		return m, kept, err
	}
	for _, e := range restored {
		if err := os.Rename(filepath.Join(staging, e.Name()), filepath.Join(spec.DataDir, e.Name())); err != nil {
			return m, kept, err
		}
	}
	return m, kept, nil
}
