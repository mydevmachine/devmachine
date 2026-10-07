package widgets

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// entryReporter adds a problem about one of w's keys, at that key's line,
// starting with the widget's id.
func entryReporter(problems *[]Problem, w Instance, label, path string) reporter {
	return func(field, format string, args ...any) {
		*problems = append(*problems, Problem{Path: path, Line: w.lineOf(field), Message: label + ": " + fmt.Sprintf(format, args...)})
	}
}

// stackEntryProblems are the keys a widget in a stack must not have: its
// place is its position in the list, and it folds with collapsed.
func stackEntryProblems(w Instance, label, path string) []Problem {
	var problems []Problem
	at := entryReporter(&problems, w, label, path)
	for _, key := range CurrentContract().StackEntry.Forbids {
		if w.has(key) {
			at(key, "a widget in a sidebar has no %s; its place is its position in the list", key)
		}
	}
	if w.has("minimized") {
		at("minimized", "a widget in a sidebar folds with collapsed: true, not minimized")
	}
	return problems
}

// stackSizeProblems checks size in a stack: left out, a preset the widget
// takes, or auto when its view grows with its content.
func stackSizeProblems(w Instance, label, path string, sizes []string, view string, grows bool) []Problem {
	var problems []Problem
	at := entryReporter(&problems, w, label, path)
	switch {
	case w.Size == "", w.Size == SizeAuto && grows:
	case w.Size == SizeAuto && view == "":
		at("size", "size auto follows the content, and a widget with no view does not grow: use %s", strings.Join(sizes, ", "))
	case w.Size == SizeAuto:
		at("size", "size auto follows the content, and the %s view does not grow: use %s", view, strings.Join(sizes, ", "))
	case !slices.Contains(sizes, w.Size):
		at("size", "size %q in a sidebar is auto or one of %s", w.Size, strings.Join(sizes, ", "))
	}
	return problems
}

// fitProblems says whether a catalog widget may sit on an area: its fits
// names the area's layout, and the area gives every context key it requires.
func fitProblems(w Instance, label, path, name string, surface Surface, entry Entry) []Problem {
	var problems []Problem
	at := entryReporter(&problems, w, label, path)
	if !slices.Contains(entry.Fits, surface.Layout) {
		at("type", "%s does not fit the %s area: its fits has no %s", w.Type, name, surface.Layout)
		return problems
	}
	if surface.Layout == LayoutSlot && !DrawnIn(entry.View.Kind, surface.Layout) {
		at("type", "%s", MenubarViewProblem(entry.View.Kind))
		return problems
	}
	for _, key := range sortedKeys(entry.Context) {
		if _, given := surface.Context[key]; entry.Context[key] == ContextRequired && !given {
			at("type", "%s needs context.%s, which the %s area does not give", w.Type, key, name)
		}
	}
	return problems
}

// DefaultSidebarWidget is the widget the default sidebar board holds: the
// workspace list the sidebar always showed.
const DefaultSidebarWidget = "devmachine-app/workspaces"

// DefaultContextSidebar are the devmachine-app widgets of the default context
// sidebar board, in the order the Context tab always showed its sections.
var DefaultContextSidebar = []string{"shortcuts", "publish-port", "monitors", "shells", "sub-agents", "todo", "pull-requests", "links"}

// DefaultMenubar are the devmachine-app widgets of the default menu bar
// title, left to right: the mark and the open pull request count it always
// showed.
var DefaultMenubar = []string{"brand", "open-pull-requests"}

// DefaultMenubarPanel are the devmachine-app widgets of the default menu bar
// popover, in the order of its tabs: Pull Requests, then Usage.
var DefaultMenubarPanel = []string{"pull-requests-panel", "usage-panel"}

// DefaultBoard is the board an area has when its file is missing: what the
// app draws, and writes, before anybody changed it. Home starts empty.
func DefaultBoard(surface string) Board {
	b := NewBoard(surface)
	var types []string
	size := SizeAuto
	switch surface {
	case "sidebar":
		types = []string{DefaultSidebarWidget}
	case "context-sidebar":
		types = appWidgets(DefaultContextSidebar)
	case "menubar":
		types, size = appWidgets(DefaultMenubar), ""
	case "menubar-panel":
		types, size = appWidgets(DefaultMenubarPanel), ""
	}
	for _, t := range types {
		_, name, _ := strings.Cut(t, "/")
		b.Widgets = append(b.Widgets, Instance{ID: name, Type: t, Size: size})
	}
	return b
}

func appWidgets(names []string) []string {
	types := make([]string, len(names))
	for i, name := range names {
		types[i] = "devmachine-app/" + name
	}
	return types
}

// Insert puts w in the list right after the widget with id after, right
// before the one with id before, or at the end when both are "".
func (b *Board) Insert(w Instance, after, before string) error {
	at := len(b.Widgets)
	switch {
	case after != "":
		i, err := b.index(after)
		if err != nil {
			return err
		}
		at = i + 1
	case before != "":
		i, err := b.index(before)
		if err != nil {
			return err
		}
		at = i
	}
	b.Widgets = slices.Insert(b.Widgets, at, w)
	return nil
}

// IDOfType is the id of the first widget of this type on the board.
func (b Board) IDOfType(widgetType string) (string, bool) {
	for _, w := range b.Widgets {
		if w.Type == widgetType {
			return w.ID, true
		}
	}
	return "", false
}

// Entry is the instance with this id, to change in place.
func (b *Board) Entry(id string) (*Instance, error) {
	i, err := b.index(id)
	if err != nil {
		return nil, err
	}
	return &b.Widgets[i], nil
}

func (b Board) index(id string) (int, error) {
	for i, w := range b.Widgets {
		if w.ID == id {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no widget with id %q on the %s board", id, b.Surface)
}

// Move takes the widget with this id out of the list and puts it right after
// the widget after, or right before the widget before; exactly one is set.
// A refused move leaves the list as it was.
func (b *Board) Move(id, after, before string) error {
	if (after == "") == (before == "") {
		return errors.New("move needs exactly one of after or before")
	}
	if after+before == id {
		return fmt.Errorf("%s cannot move next to itself", id)
	}
	from, err := b.index(id)
	if err != nil {
		return err
	}
	if _, err := b.index(after + before); err != nil {
		return err
	}
	w := b.Widgets[from]
	b.Widgets = slices.Delete(b.Widgets, from, from+1)
	return b.Insert(w, after, before)
}
