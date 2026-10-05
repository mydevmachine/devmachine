package local

import (
	"fmt"
	"regexp"
	"runtime"
)

// Size is how much of this computer a local machine gets.
type Size struct {
	CPUs      int
	MemoryGiB int
	DiskGiB   int
}

// DefaultSize is the size every machine template carries.
var DefaultSize = Size{CPUs: 2, MemoryGiB: 4, DiskGiB: 20}

// MinDiskGiB leaves room for Ubuntu, Ansible and a workspace's packages; below
// it the first sync fills the disk.
const MinDiskGiB = 10

// Seams, so the tests do not depend on the computer they run on.
var (
	realHostCPUs   = runtime.NumCPU
	hostCPUs       = realHostCPUs
	realHostMemory = totalMemory
	hostMemory     = realHostMemory
)

var (
	cpusLine   = regexp.MustCompile(`(?m)^cpus: .*$`)
	memoryLine = regexp.MustCompile(`(?m)^memory: .*$`)
	diskLine   = regexp.MustCompile(`(?m)^disk: .*$`)
)

// Render is the distro's Lima template with size in place of the default one.
func Render(distro Distro, size Size) ([]byte, error) {
	out, ok := templates[distro]
	if !ok {
		return nil, fmt.Errorf("no machine template for %q", distro)
	}
	for _, r := range []struct {
		line  *regexp.Regexp
		value string
	}{
		{cpusLine, fmt.Sprintf("cpus: %d", size.CPUs)},
		{memoryLine, fmt.Sprintf("memory: \"%dGiB\"", size.MemoryGiB)},
		{diskLine, fmt.Sprintf("disk: \"%dGiB\"", size.DiskGiB)},
	} {
		if len(r.line.FindAllIndex(out, -1)) != 1 {
			return nil, fmt.Errorf("the machine template does not have exactly one line matching %s", r.line)
		}
		out = r.line.ReplaceAll(out, []byte(r.value))
	}
	return out, nil
}

// CheckSize refuses a size this computer cannot give. Memory must stay below
// the computer's own, or the computer itself runs out while the VM runs.
func CheckSize(size Size, cpus int, memory uint64) error {
	if size.CPUs < 1 || size.CPUs > cpus {
		return fmt.Errorf("--cpus %d: this computer has %d cores, so use 1 to %d", size.CPUs, cpus, cpus)
	}
	hostGiB := memory >> 30
	if size.MemoryGiB < 1 || uint64(size.MemoryGiB) >= hostGiB {
		return fmt.Errorf("--memory %d: this computer has %d GiB, so use 1 to %d", size.MemoryGiB, hostGiB, hostGiB-1)
	}
	if size.DiskGiB < MinDiskGiB {
		return fmt.Errorf("--disk %d: use at least %d GiB, or the first sync fills it", size.DiskGiB, MinDiskGiB)
	}
	return nil
}

func checkHostSize(size Size) error {
	memory, err := hostMemory()
	if err != nil {
		return fmt.Errorf("reading this computer's memory: %w", err)
	}
	return CheckSize(size, hostCPUs(), memory)
}
