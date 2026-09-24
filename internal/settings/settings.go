// Package settings holds the QUAREL_* configuration of a server: environment
// variables first (they always win, e.g. from Docker Compose), then the
// settings file of the data directory, edited through the admin interface.
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// FileName is the settings file, in the data directory.
const FileName = "settings.json"

var (
	mu   sync.RWMutex
	file = map[string]string{}
)

// Get returns the value of a setting: the environment variable if set and not
// empty, else the settings file, else "".
func Get(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	mu.RLock()
	defer mu.RUnlock()
	return file[key]
}

// Locked reports whether key is set by the environment (the file cannot change it).
func Locked(key string) bool { return os.Getenv(key) != "" }

// FromFile returns the value stored in the settings file.
func FromFile(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	return file[key]
}

// DataDir is the data directory: QUAREL_DATA_DIR, else Default.
func DataDir() string {
	if v := os.Getenv("QUAREL_DATA_DIR"); v != "" {
		return v
	}
	return Default
}

// Default is the data directory when QUAREL_DATA_DIR is not set ("./data";
// the Windows build sets a per-user folder).
var Default = "./data"

// Load reads the settings file of dir (a missing file means no settings).
func Load(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	values := map[string]string{}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(data, &values); err != nil {
			return errors.New(FileName + ": " + err.Error())
		}
	}
	mu.Lock()
	file = values
	mu.Unlock()
	return nil
}

// Save replaces the settings file of dir (atomically, readable by the owner
// only: it may hold passwords) and the values in memory. Empty values are dropped.
func Save(dir string, values map[string]string) error {
	clean := map[string]string{}
	for k, v := range values {
		if v != "" {
			clean[k] = v
		}
	}
	keys := make([]string, 0, len(clean))
	for k := range clean {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(clean))
	for _, k := range keys {
		ordered[k] = clean[k]
	}
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, FileName+".tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, FileName)); err != nil {
		return err
	}
	mu.Lock()
	file = clean
	mu.Unlock()
	return nil
}

// Snapshot returns a copy of the settings file values.
func Snapshot() map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]string, len(file))
	for k, v := range file {
		out[k] = v
	}
	return out
}
