//go:build !windows

package diskspace

import "golang.org/x/sys/unix"

// Free returns the bytes available to this process on the disk holding dir.
func Free(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
