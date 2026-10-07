package widgets

import (
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
	case w.Size == SizeAuto:
		at("size", "size auto follows the content, and the %s view does not grow: use %s", view, strings.Join(sizes, ", "))
	case !slices.Contains(sizes, w.Size):
		at("size", "size %q in a sidebar is auto or one of %s", w.Size, strings.Join(sizes, ", "))
	}
	return problems
}
