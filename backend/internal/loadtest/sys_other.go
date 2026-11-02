//go:build !linux && !windows

package loadtest

// Elsewhere the report has no machine figures.

type cpuMeter struct{}

func newCPUMeter() *cpuMeter         { return &cpuMeter{} }
func (c *cpuMeter) percent() float64 { return -1 }
func memAvailableMB() float64        { return -1 }
func memTotalMB() float64            { return -1 }
func load1() float64                 { return -1 }
