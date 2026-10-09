//go:build windows

package storage

import "golang.org/x/sys/windows"

// diskUsage returns the free (for this user) and total bytes of the disk path is on.
func diskUsage(path string) (free, total uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail, tot, all uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &tot, &all); err != nil {
		return 0, 0, err
	}
	return avail, tot, nil
}
