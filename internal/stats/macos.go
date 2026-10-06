package stats

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// A Mac has no free, no /proc and no df -B1, so it is read with sysctl,
// vm_stat and df -k instead.
const (
	macMemsizeCommand  = "sysctl -n hw.memsize"
	macVMStatCommand   = "vm_stat"
	macSwapCommand     = "sysctl -n vm.swapusage"
	macDfCommand       = "df -k /System/Volumes/Data 2>/dev/null || df -k /"
	macLoadavgCommand  = "sysctl -n vm.loadavg"
	macCPUCountCommand = "sysctl -n hw.ncpu"
)

func collectMac(ctx context.Context, client remote.Client) (Snapshot, error) {
	read := func(what, command string) (string, error) {
		out, err := client.Run(ctx, command)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", what, err)
		}
		return out, nil
	}
	var s Snapshot

	memsize, err := read("memory", macMemsizeCommand)
	if err != nil {
		return s, err
	}
	vmstat, err := read("memory", macVMStatCommand)
	if err != nil {
		return s, err
	}
	swap, err := read("swap", macSwapCommand)
	if err != nil {
		return s, err
	}
	df, err := read("disk", macDfCommand)
	if err != nil {
		return s, err
	}
	loadavg, err := read("load", macLoadavgCommand)
	if err != nil {
		return s, err
	}
	ncpu, _ := client.Run(ctx, macCPUCountCommand)

	if s.MemTotalBytes, err = strconv.ParseUint(strings.TrimSpace(memsize), 10, 64); err != nil {
		return s, fmt.Errorf("reading `%s` output: %w", macMemsizeCommand, err)
	}
	if s.MemAvailableBytes, err = macAvailable(vmstat); err != nil {
		return s, fmt.Errorf("reading `%s` output: %w", macVMStatCommand, err)
	}
	s.MemAvailableBytes = min(s.MemAvailableBytes, s.MemTotalBytes)
	s.MemUsedBytes = s.MemTotalBytes - s.MemAvailableBytes

	s.SwapTotalBytes, s.SwapUsedBytes = macSwap(swap)

	disk, err := diskRow(df)
	if err != nil {
		return s, fmt.Errorf("reading `df -k` output: %w", err)
	}
	s.DiskTotalBytes, s.DiskUsedBytes, s.DiskAvailBytes = disk[0]*1024, disk[1]*1024, disk[2]*1024

	if l := strings.Fields(strings.Trim(strings.TrimSpace(loadavg), "{}")); len(l) >= 3 {
		s.Load1, _ = strconv.ParseFloat(l[0], 64)
		s.Load5, _ = strconv.ParseFloat(l[1], 64)
		s.Load15, _ = strconv.ParseFloat(l[2], 64)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(ncpu)); err == nil {
		s.CPUs = n
	}
	return s, nil
}

// macAvailable is what macOS can hand out without swapping: free pages, and
// the inactive and speculative ones it drops first.
func macAvailable(vmstat string) (uint64, error) {
	var pageSize uint64
	pages := map[string]uint64{}
	for _, line := range strings.Split(vmstat, "\n") {
		if _, rest, ok := strings.Cut(line, "page size of "); ok {
			pageSize, _ = strconv.ParseUint(strings.Fields(rest)[0], 10, 64)
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if n, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(value), "."), 10, 64); err == nil {
			pages[strings.TrimSpace(name)] = n
		}
	}
	free, ok := pages["Pages free"]
	if pageSize == 0 || !ok {
		return 0, fmt.Errorf("no page size or no free pages")
	}
	return (free + pages["Pages inactive"] + pages["Pages speculative"]) * pageSize, nil
}

// macSwap reads "total = 2048.00M  used = 1024.50M  free = ...". A Mac with
// no swap file says zero, which is not an error.
func macSwap(swapusage string) (total, used uint64) {
	fields := strings.Fields(swapusage)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i+1] != "=" {
			continue
		}
		switch fields[i] {
		case "total":
			total = megabytes(fields[i+2])
		case "used":
			used = megabytes(fields[i+2])
		}
	}
	return total, used
}

func megabytes(s string) uint64 {
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "M"), 64)
	if err != nil || v < 0 {
		return 0
	}
	return uint64(v * (1 << 20))
}
