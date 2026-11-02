//go:build windows

package loadtest

import (
	"syscall"
	"unsafe"
)

// The machine as Windows reports it, for a test on a developer's computer.
// Windows has no load average.

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	getSystemTimes    = kernel32.NewProc("GetSystemTimes")
	globalMemoryStats = kernel32.NewProc("GlobalMemoryStatusEx")
)

type cpuMeter struct{ idle, total uint64 }

func newCPUMeter() *cpuMeter {
	c := &cpuMeter{}
	c.idle, c.total = cpuTimes()
	return c
}

func (c *cpuMeter) percent() float64 {
	idle, total := cpuTimes()
	if total <= c.total {
		return -1
	}
	busy := 1 - float64(idle-c.idle)/float64(total-c.total)
	c.idle, c.total = idle, total
	return round1(busy * 100)
}

func cpuTimes() (idle, total uint64) {
	var i, k, u syscall.Filetime
	if r, _, _ := getSystemTimes.Call(uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u))); r == 0 {
		return 0, 0
	}
	ft := func(f syscall.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
	return ft(i), ft(k) + ft(u) // kernel time includes idle time
}

type memoryStatus struct {
	Length, MemoryLoad                     uint32
	TotalPhys, AvailPhys                   uint64
	TotalPageFile, AvailPageFile           uint64
	TotalVirtual, AvailVirtual, AvailExtra uint64
}

func memory() (total, avail float64) {
	m := memoryStatus{Length: uint32(unsafe.Sizeof(memoryStatus{}))}
	if r, _, _ := globalMemoryStats.Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return -1, -1
	}
	return round1(float64(m.TotalPhys) / (1 << 20)), round1(float64(m.AvailPhys) / (1 << 20))
}

func memAvailableMB() float64 { _, a := memory(); return a }
func memTotalMB() float64     { t, _ := memory(); return t }
func load1() float64          { return -1 }
