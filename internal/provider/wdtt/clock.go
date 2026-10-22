package wdtt

import (
	"os"
	"time"
)

// nowUnix is a seam for tests.
var nowUnix = func() int64 { return time.Now().Unix() }

// Clock abstracts time for deadline tests: no test ever waits 30 real minutes.
type Clock interface {
	Now() time.Time
	// After behaves like time.After for the injected clock.
	After(d time.Duration) <-chan time.Time
}

// RealClock is the production clock.
type RealClock struct{}

// Now returns wall-clock time.
func (RealClock) Now() time.Time { return time.Now() }

// After delegates to time.After.
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// MemAvailableMB reads the device admission figure. On Linux this is
// /proc/meminfo MemAvailable; other platforms and tests inject a value.
type MemAvailableMB func() (int, error)

// RealMemAvailableMB parses /proc/meminfo (Linux only).
func RealMemAvailableMB() (int, error) {
	b, err := osReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	return parseMemAvailableMB(string(b))
}

// Signal shims (portable without syscall on every target).
var (
	osInterrupt = os.Interrupt
	osKill      = os.Kill
)
