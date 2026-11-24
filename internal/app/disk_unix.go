//go:build !windows

package app

import "syscall"

// freeSpace returns the free bytes on the volume holding path (0 if unknown).
func freeSpace(path string) int64 {
	if path == "" {
		return 0
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
