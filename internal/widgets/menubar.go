package widgets

import "fmt"

// MenubarViewProblem says why a view kind cannot be drawn in the menu bar,
// naming the view only when there is one.
func MenubarViewProblem(kind string) string {
	if kind == "" {
		return "a widget with no view cannot be drawn in the menu bar"
	}
	return fmt.Sprintf("the %s view cannot be drawn in the menu bar", kind)
}

func slotEntryProblems(w Instance, label, path string) []Problem {
	var problems []Problem
	at := entryReporter(&problems, w, label, path)
	for _, key := range CurrentContract().SlotEntry.Forbids {
		if !w.has(key) {
			continue
		}
		switch key {
		case "size":
			at(key, "a widget in the menu bar has no size: it is one line of text")
		case "collapsed", "minimized":
			at(key, "a widget in the menu bar does not fold: take it off with widgets remove")
		default:
			at(key, "a widget in the menu bar has no %s; its place is its position in the list", key)
		}
	}
	return problems
}

func tabsEntryProblems(w Instance, label, path string) []Problem {
	var problems []Problem
	at := entryReporter(&problems, w, label, path)
	for _, key := range CurrentContract().TabsEntry.Forbids {
		if !w.has(key) {
			continue
		}
		switch key {
		case "collapsed", "minimized":
			at(key, "a tab in the menu bar popover does not fold: take it off with widgets remove")
		default:
			at(key, "a tab in the menu bar popover has no %s; its place is its position in the list", key)
		}
	}
	return problems
}

// BoardWarnings are keys a board holds that do nothing where they are: a
// size on a tab, which always fills the menu bar popover. They never stop a
// board from being read or written.
func BoardWarnings(b Board, path string) []Problem {
	c := CurrentContract()
	if c.Surfaces[b.Surface].Layout != LayoutTabs {
		return nil
	}
	var warnings []Problem
	for _, w := range b.Widgets {
		for _, key := range c.TabsEntry.Ignores {
			if w.has(key) {
				warnings = append(warnings, Problem{Path: path, Line: w.lineOf(key),
					Message: w.ID + ": " + key + " is ignored in the menu bar popover: a tab fills it"})
			}
		}
	}
	return warnings
}
