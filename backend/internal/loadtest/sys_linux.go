//go:build linux

package loadtest

import (
	"os"
	"strconv"
	"strings"
)

// The machine as Linux reports it: CPU of every process (PostgreSQL too),
// available memory and the load average.

type cpuMeter struct{ idle, total uint64 }

func newCPUMeter() *cpuMeter {
	c := &cpuMeter{}
	c.idle, c.total = cpuTimes()
	return c
}

// percent is the machine's CPU use since the last call, % of all cores.
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
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
		if i == 3 || i == 4 { // idle, iowait
			idle += n
		}
	}
	return idle, total
}

func memAvailableMB() float64 {
	return meminfo("MemAvailable:")
}

func memTotalMB() float64 {
	return meminfo("MemTotal:")
}

func meminfo(key string) float64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return -1
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, key) {
			f := strings.Fields(l)
			kb, _ := strconv.ParseFloat(f[1], 64)
			return round1(kb / 1024)
		}
	}
	return -1
}

func load1() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return -1
	}
	f := strings.Fields(string(b))
	x, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return -1
	}
	return x
}
