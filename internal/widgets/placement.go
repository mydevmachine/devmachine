package widgets

import (
	"fmt"
	"slices"
	"strconv"
)

// The CLI has no window to measure, so a free spot is looked for inside a
// fixed band as wide as a laptop screen, starting at the margin the app's
// default board uses.
const (
	boardMargin = 24
	scanWidth   = 1280
)

// FrameFor is the width and height of a preset, in points.
func FrameFor(size string) (int, int, bool) {
	c := CurrentContract()
	preset, ok := c.Presets[size]
	if !ok {
		return 0, 0, false
	}
	return preset[0] * c.Unit, preset[1] * c.Unit, true
}

// MinFrame is the smallest width and height a widget taking these sizes may
// be resized to: the smallest preset it accepts, on each axis.
func MinFrame(sizes []string) (int, int) {
	minW, minH := 0, 0
	for _, size := range sizes {
		w, h, ok := FrameFor(size)
		if !ok {
			continue
		}
		if minW == 0 || w < minW {
			minW = w
		}
		if minH == 0 || h < minH {
			minH = h
		}
	}
	return minW, minH
}

// FreeSpot is the first place a w×h widget fits without touching another,
// scanning rows of the snap from the top left.
func FreeSpot(b Board, w, h int) Frame {
	snap := CurrentContract().Snap
	for y := boardMargin; ; y += snap {
		for x := boardMargin; x == boardMargin || x+w <= boardMargin+scanWidth; x += snap {
			candidate := Frame{X: x, Y: y, W: w, H: h}
			if !overlapsAny(candidate, b.Widgets, snap) {
				return candidate
			}
		}
	}
}

func overlapsAny(f Frame, widgets []Instance, gap int) bool {
	for _, other := range widgets {
		o := other.Frame
		apart := f.X+f.W+gap <= o.X || o.X+o.W+gap <= f.X || f.Y+f.H+gap <= o.Y || o.Y+o.H+gap <= f.Y
		if !apart {
			return true
		}
	}
	return false
}

// Snap rounds v to the nearest multiple of the snap.
func Snap(v int) int {
	snap := CurrentContract().Snap
	return (v + snap/2) / snap * snap
}

// NextID is base when no instance uses it, else base-2, base-3 and so on.
func NextID(b Board, base string) string {
	taken := make([]string, 0, len(b.Widgets))
	for _, w := range b.Widgets {
		taken = append(taken, w.ID)
	}
	if !slices.Contains(taken, base) {
		return base
	}
	for n := 2; ; n++ {
		if id := fmt.Sprintf("%s-%d", base, n); !slices.Contains(taken, id) {
			return id
		}
	}
}

// NextZ puts a new instance in front of every other.
func NextZ(b Board) int {
	z := 0
	for _, w := range b.Widgets {
		z = max(z, w.Z)
	}
	return z + 1
}

// HasID says whether an instance with this id is on the board.
func (b Board) HasID(id string) bool {
	return slices.ContainsFunc(b.Widgets, func(w Instance) bool { return w.ID == id })
}

// Remove takes the instance with this id off the board.
func (b *Board) Remove(id string) error {
	for i, w := range b.Widgets {
		if w.ID == id {
			b.Widgets = slices.Delete(b.Widgets, i, i+1)
			return nil
		}
	}
	return fmt.Errorf("no widget with id %q on the %s board", id, b.Surface)
}

// CoerceInput turns a --set value into the type its input declares.
func CoerceInput(name string, input Input, raw string) (any, error) {
	switch input.Type {
	case "number":
		if n, err := strconv.Atoi(raw); err == nil {
			return n, nil
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("input %s is a number, and %q is not", name, raw)
		}
		return f, nil
	case "boolean":
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("input %s is true or false, and %q is neither", name, raw)
		}
		return v, nil
	default:
		return raw, nil
	}
}
