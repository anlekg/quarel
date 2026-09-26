package diskspace

import (
	"testing"

	"github.com/anlekg/quarel/internal/httpapi"
)

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	free, err := Free(dir)
	if err != nil || free == 0 {
		t.Fatalf("free space: %d %v", free, err)
	}
	if err := Check(dir, 1<<20, 1<<20); err != nil {
		t.Fatalf("plenty of space refused: %v", err)
	}
	if err := Check(dir, int64(free), 1); !httpapi.IsCode(err, "storage_full") {
		t.Fatalf("full disk accepted: %v", err)
	}
}
