package desktop

import (
	"io"
	"os"
	"path/filepath"
)

// LogFile opens dir/name for appending, starting afresh when it grew over
// maxBytes (the previous one is kept as name.old): a program without a
// console still leaves a readable log.
func LogFile(dir, name string, maxBytes int64) (io.WriteCloser, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	if st, err := os.Stat(path); err == nil && st.Size() > maxBytes {
		os.Rename(path, path+".old")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}
