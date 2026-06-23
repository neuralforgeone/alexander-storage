//go:build windows

package filesystem

import "golang.org/x/sys/windows"

func getDiskFreeSpace(path string, free *uint64) error {
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err := windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(path), &freeBytesAvailable, &totalBytes, &totalFreeBytes)
	if err != nil {
		return err
	}
	*free = freeBytesAvailable
	return nil
}