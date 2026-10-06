package stats

import (
	"context"
	"testing"

	"github.com/mydevmachine/devmachine/internal/remote"
)

func macOutputs(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		remote.UnameCommand: "Darwin\n",
		macMemsizeCommand:   fixture(t, "macos/memsize.txt"),
		macVMStatCommand:    fixture(t, "macos/vm_stat.txt"),
		macSwapCommand:      fixture(t, "macos/swapusage.txt"),
		macDfCommand:        fixture(t, "macos/df.txt"),
		macLoadavgCommand:   fixture(t, "macos/loadavg.txt"),
		macCPUCountCommand:  fixture(t, "macos/ncpu.txt"),
	}
}

func TestCollectReadsAMac(t *testing.T) {
	got, err := Collect(context.Background(), fakeClient{out: macOutputs(t)})
	if err != nil {
		t.Fatal(err)
	}
	const mib = 1 << 20
	want := Snapshot{
		MemTotalBytes:     25769803776,
		MemAvailableBytes: (7870 + 367938 + 7010) * 16384,
		MemUsedBytes:      25769803776 - (7870+367938+7010)*16384,
		SwapTotalBytes:    7168 * mib,
		DiskTotalBytes:    482797652 * 1024,
		DiskUsedBytes:     235311392 * 1024,
		DiskAvailBytes:    202779112 * 1024,
		Load1:             2.98,
		Load5:             3.04,
		Load15:            2.80,
		CPUs:              12,
	}
	swapUsed := got.SwapUsedBytes
	got.SwapUsedBytes = 0
	if got != want {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
	if exact := uint64(568844 * mib / 100); swapUsed+mib < exact || swapUsed > exact+mib {
		t.Fatalf("swap used = %d, want about %d", swapUsed, exact)
	}
}

func TestCollectOnAMacWithNoSwap(t *testing.T) {
	out := macOutputs(t)
	out[macSwapCommand] = "total = 0.00M  used = 0.00M  free = 0.00M  (encrypted)\n"
	got, err := Collect(context.Background(), fakeClient{out: out})
	if err != nil {
		t.Fatal(err)
	}
	if got.SwapTotalBytes != 0 || got.SwapUsedBytes != 0 {
		t.Fatalf("swap = %d of %d", got.SwapUsedBytes, got.SwapTotalBytes)
	}
}

func TestCollectOnAMacRefusesAVMStatItCannotRead(t *testing.T) {
	out := macOutputs(t)
	out[macVMStatCommand] = "nothing useful\n"
	if _, err := Collect(context.Background(), fakeClient{out: out}); err == nil {
		t.Fatal("it read memory from output it does not understand")
	}
}
