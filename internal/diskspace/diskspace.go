// Package diskspace keeps uploads from filling the disk of a home server:
// files that others send are refused when too little free space is left.
package diskspace

import (
	"fmt"
	"net/http"

	"github.com/anlekg/quarel/internal/httpapi"
)

// DefaultReserveMB is the free space kept for the system and the database.
const DefaultReserveMB = 1024

// Check returns a 507 API error when storing size more bytes in dir would
// leave less than reserve bytes free. It lets the upload through when the
// free space cannot be read.
func Check(dir string, reserve, size int64) error {
	free, err := Free(dir)
	if err != nil || reserve <= 0 {
		return nil
	}
	if int64(free) < reserve+size {
		return httpapi.Errf(http.StatusInsufficientStorage, "storage_full",
			"the server is almost out of disk space (%s left): files are refused for now", human(free))
	}
	return nil
}

func human(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	default:
		return fmt.Sprintf("%d MB", b>>20)
	}
}
