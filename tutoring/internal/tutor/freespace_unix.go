//go:build !windows

package tutor

import "syscall"

// freeSpace is the space left on the recordings' disk, when known.
func freeSpace(rs *recordings) (int64, bool) {
	if rs == nil {
		return 0, false
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(rs.dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
