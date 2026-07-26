package agent

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Proc paths are overridable for tests (linux-style /proc layout).
var (
	procUptime  = "/proc/uptime"
	procMeminfo = "/proc/meminfo"
	procLoadavg = "/proc/loadavg"
)

// SetProcPathsForTest overrides /proc file paths; returns a restore func.
// Intended for unit tests only.
func SetProcPathsForTest(uptime, meminfo, loadavg string) func() {
	oldU, oldM, oldL := procUptime, procMeminfo, procLoadavg
	if uptime != "" {
		procUptime = uptime
	}
	if meminfo != "" {
		procMeminfo = meminfo
	}
	if loadavg != "" {
		procLoadavg = loadavg
	}
	return func() {
		procUptime, procMeminfo, procLoadavg = oldU, oldM, oldL
	}
}

// CollectStats gathers guest resource basics from /proc (Linux) or stubs.
// When /proc is present (or overridden for tests), values are filled even on
// non-Linux GOOS so unit tests can inject temp /proc trees on darwin.
func CollectStats() Stats {
	st := Stats{}
	if err := readUptime(&st); err != nil {
		// fall through with zeros
	}
	if err := readMeminfo(&st); err != nil {
		// fall through
	}
	if err := readLoadavg(&st); err != nil {
		// fall through
	}
	// Best-effort root filesystem size via Statfs (works on linux + darwin).
	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err == nil {
		// Bsize is int64 on some platforms; cast carefully.
		bsize := uint64(fs.Bsize) //nolint:unconvert
		st.DiskTotal = fs.Blocks * bsize
		st.DiskFree = fs.Bavail * bsize
	}
	// On pure darwin without /proc overrides, leave mem/load/uptime at 0
	// (stubs for local agent tests that do not inject /proc).
	_ = runtime.GOOS
	return st
}

func readUptime(st *Stats) error {
	b, err := os.ReadFile(procUptime)
	if err != nil {
		return err
	}
	// "12345.67 98765.43"
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return fmt.Errorf("empty uptime")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return err
	}
	st.UptimeSec = v
	return nil
}

func readMeminfo(st *Stats) error {
	f, err := os.Open(procMeminfo)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		// "MemTotal:       16384000 kB"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		// Values are in kB.
		bytes := val * 1024
		switch key {
		case "MemTotal":
			st.MemTotal = bytes
		case "MemAvailable":
			st.MemAvail = bytes
		case "MemFree":
			// Use MemFree only if MemAvailable never appears.
			if st.MemAvail == 0 {
				st.MemAvail = bytes
			}
		}
	}
	return sc.Err()
}

func readLoadavg(st *Stats) error {
	b, err := os.ReadFile(procLoadavg)
	if err != nil {
		return err
	}
	// "0.15 0.10 0.05 1/234 5678"
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return fmt.Errorf("empty loadavg")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return err
	}
	st.Load1 = v
	return nil
}
