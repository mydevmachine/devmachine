// Package stats reads what a machine is spending: memory, swap, disk, load.
//
// Parsing is separate from collecting on purpose. The parser is the part that
// gets subtle, so it is a pure function tested against output captured from a
// real machine, with no connection involved.
package stats

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// The commands a snapshot is built from. Byte counts everywhere, so nothing
// depends on a locale or on a human-readable unit.
const (
	freeCommand    = "free -b"
	dfCommand      = "df -B1 /"
	loadavgCommand = "cat /proc/loadavg"
	nprocCommand   = "nproc"
)

// Snapshot is one reading of a machine.
type Snapshot struct {
	MemTotalBytes     uint64  `json:"mem_total_bytes"`
	MemUsedBytes      uint64  `json:"mem_used_bytes"`
	MemAvailableBytes uint64  `json:"mem_available_bytes"`
	SwapTotalBytes    uint64  `json:"swap_total_bytes"`
	SwapUsedBytes     uint64  `json:"swap_used_bytes"`
	DiskTotalBytes    uint64  `json:"disk_total_bytes"`
	DiskUsedBytes     uint64  `json:"disk_used_bytes"`
	DiskAvailBytes    uint64  `json:"disk_available_bytes"`
	Load1             float64 `json:"load_1"`
	Load5             float64 `json:"load_5"`
	Load15            float64 `json:"load_15"`
	CPUs              int     `json:"cpus"`
}

// Collect runs the commands on the machine and parses what they said.
func Collect(ctx context.Context, client remote.Client) (Snapshot, error) {
	var s Snapshot

	kernel, err := client.Run(ctx, remote.UnameCommand)
	if err != nil {
		return s, fmt.Errorf("reading which system this is: %w", err)
	}
	if strings.TrimSpace(kernel) == remote.KernelDarwin {
		return collectMac(ctx, client)
	}

	free, err := client.Run(ctx, freeCommand)
	if err != nil {
		return s, fmt.Errorf("reading memory: %w", err)
	}
	df, err := client.Run(ctx, dfCommand)
	if err != nil {
		return s, fmt.Errorf("reading disk: %w", err)
	}
	loadavg, err := client.Run(ctx, loadavgCommand)
	if err != nil {
		return s, fmt.Errorf("reading load: %w", err)
	}
	// A machine without nproc still gives a useful snapshot, so this one is
	// allowed to say nothing.
	nproc, _ := client.Run(ctx, nprocCommand)

	return Parse(free, df, loadavg, nproc)
}

// Parse turns the four command outputs into a snapshot.
func Parse(free, df, loadavg, nproc string) (Snapshot, error) {
	var s Snapshot

	mem, err := memoryRow(free, "Mem:")
	if err != nil {
		return s, fmt.Errorf("reading `%s` output: %w", freeCommand, err)
	}
	s.MemTotalBytes, s.MemUsedBytes = mem[0], mem[1]
	if len(mem) >= 6 {
		s.MemAvailableBytes = mem[5]
	}

	// Swap is optional: a machine with none prints a row of zeros, and one
	// without the row at all is not an error.
	if swap, err := memoryRow(free, "Swap:"); err == nil {
		s.SwapTotalBytes, s.SwapUsedBytes = swap[0], swap[1]
	}

	disk, err := diskRow(df)
	if err != nil {
		return s, fmt.Errorf("reading `%s` output: %w", dfCommand, err)
	}
	s.DiskTotalBytes, s.DiskUsedBytes, s.DiskAvailBytes = disk[0], disk[1], disk[2]

	if l := strings.Fields(loadavg); len(l) >= 3 {
		s.Load1, _ = strconv.ParseFloat(l[0], 64)
		s.Load5, _ = strconv.ParseFloat(l[1], 64)
		s.Load15, _ = strconv.ParseFloat(l[2], 64)
	}

	if n, err := strconv.Atoi(strings.TrimSpace(nproc)); err == nil {
		s.CPUs = n
	}
	return s, nil
}

// memoryRow returns the numbers of one row of `free -b`, in its own order.
func memoryRow(free, prefix string) ([]uint64, error) {
	for _, line := range strings.Split(free, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != prefix {
			continue
		}
		return parseUints(fields[1:])
	}
	return nil, fmt.Errorf("no %q row", prefix)
}

// diskRow returns total, used and available from `df -B1`.
func diskRow(df string) ([]uint64, error) {
	for _, line := range strings.Split(df, "\n") {
		fields := strings.Fields(line)
		// The header has no numbers, and a long device name makes df wrap the
		// row; requiring the numeric columns handles both without guessing.
		if len(fields) < 4 {
			continue
		}
		values, err := parseUints(fields[1:4])
		if err != nil {
			continue
		}
		return values, nil
	}
	return nil, fmt.Errorf("no data row")
}

func parseUints(fields []string) ([]uint64, error) {
	out := make([]uint64, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", f)
		}
		out = append(out, v)
	}
	return out, nil
}
