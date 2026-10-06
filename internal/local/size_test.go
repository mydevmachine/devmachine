package local

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

const gib = uint64(1) << 30

func TestRenderWithTheDefaultSizeIsTheEmbeddedTemplate(t *testing.T) {
	got, err := Render(Ubuntu, DefaultSize)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, Template(Ubuntu)) {
		t.Fatal("the default size changed the template: the flags' defaults and machine.yaml disagree")
	}
}

func TestRenderWritesTheChosenSize(t *testing.T) {
	got, err := Render(Ubuntu, Size{CPUs: 6, MemoryGiB: 12, DiskGiB: 80})
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	for _, want := range []string{"\ncpus: 6\n", "\nmemory: \"12GiB\"\n", "\ndisk: \"80GiB\"\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	for _, gone := range []string{"cpus: 2\n", "\"4GiB\"", "\"20GiB\""} {
		if strings.Contains(body, gone) {
			t.Fatalf("the default %q is still there", gone)
		}
	}
}

func TestCheckSizeAcceptsTheDefaultOnAModestComputer(t *testing.T) {
	if err := CheckSize(DefaultSize, 4, 8*gib); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSizeRefusesWhatThisComputerCannotGive(t *testing.T) {
	for _, tc := range []struct {
		name string
		size Size
		want string
	}{
		{"no cpu", Size{CPUs: 0, MemoryGiB: 4, DiskGiB: 20}, "--cpus"},
		{"more cpus than cores", Size{CPUs: 9, MemoryGiB: 4, DiskGiB: 20}, "8 cores"},
		{"no memory", Size{CPUs: 2, MemoryGiB: 0, DiskGiB: 20}, "--memory"},
		{"all the memory", Size{CPUs: 2, MemoryGiB: 16, DiskGiB: 20}, "16 GiB"},
		{"a tiny disk", Size{CPUs: 2, MemoryGiB: 4, DiskGiB: 9}, "at least 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckSize(tc.size, 8, 16*gib)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error naming %q", err, tc.want)
			}
		})
	}
}

func TestCheckSizeAcceptsTheEdges(t *testing.T) {
	if err := CheckSize(Size{CPUs: 8, MemoryGiB: 15, DiskGiB: 10}, 8, 16*gib); err != nil {
		t.Fatal(err)
	}
}

func TestCreateBootsTheChosenSize(t *testing.T) {
	r := stub(t, nil)
	stubHost(t, 8, 16*gib)

	if _, err := Create(context.Background(), "alpha", Ubuntu, Size{CPUs: 4, MemoryGiB: 8, DiskGiB: 40}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(r.bodies) != 1 || !strings.Contains(r.bodies[0], "\ncpus: 4\n") ||
		!strings.Contains(r.bodies[0], "\nmemory: \"8GiB\"\n") || !strings.Contains(r.bodies[0], "\ndisk: \"40GiB\"\n") {
		t.Fatalf("limactl got %q", r.bodies)
	}
}

func TestCreateRefusesATooBigSizeBeforeAskingLima(t *testing.T) {
	r := stub(t, nil)
	stubHost(t, 2, 8*gib)

	_, err := Create(context.Background(), "alpha", Ubuntu, Size{CPUs: 4, MemoryGiB: 4, DiskGiB: 20}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--cpus") {
		t.Fatalf("got %v", err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("limactl ran: %s", r.joined())
	}
}

func stubHost(t *testing.T, cpus int, memory uint64) {
	t.Helper()
	hostCPUs = func() int { return cpus }
	hostMemory = func() (uint64, error) { return memory, nil }
	t.Cleanup(func() { hostCPUs, hostMemory = realHostCPUs, realHostMemory })
}
