//go:build !windows

package filesystem

import (
	"syscall"
)

func getDiskFreeSpace(path string, free *uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return err
	}
	*free = stat.Bavail * uint64(stat.Bsize)
	return nil
}