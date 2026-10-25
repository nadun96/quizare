//go:build windows

package tutor

import (
	"syscall"
	"unsafe"
)

// freeSpace is the space left on the recordings' disk, when known.
func freeSpace(rs *recordings) (int64, bool) {
	if rs == nil {
		return 0, false
	}
	dir, err := syscall.UTF16PtrFromString(rs.dir)
	if err != nil {
		return 0, false
	}
	var free uint64
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(dir)), uintptr(unsafe.Pointer(&free)), 0, 0); r == 0 {
		return 0, false
	}
	return int64(free), true
}
