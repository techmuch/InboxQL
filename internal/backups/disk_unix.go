//go:build !windows

package backups

import "golang.org/x/sys/unix"

// FreeBytes is the room left on the disk holding path, for this user.
func FreeBytes(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
