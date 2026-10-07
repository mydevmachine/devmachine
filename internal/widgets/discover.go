package widgets

import (
	"fmt"
	"os"
	"path/filepath"
)

// LoadAll reads every widget in root, a package's widgets folder: each direct
// child folder holding a widget.yml is one widget. A widget with a problem is
// left out of the list and its problems are returned, so one broken widget
// never hides the others.
func LoadAll(root string) ([]Widget, []Problem) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, []Problem{{Path: root, Message: fmt.Sprintf("the widgets folder is not there: %v", err)}}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, []Problem{{Path: root, Message: "the widgets folder must be a real folder, not a link or a file"}}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, []Problem{{Path: root, Message: fmt.Sprintf("reading the widgets folder: %v", err)}}
	}

	var found []Widget
	var problems []Problem
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			problems = append(problems, Problem{Path: dir, Message: "a widget folder must not be a link: it has to stay inside its package"})
			continue
		}
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
			continue
		}
		w, widgetProblems := Load(dir)
		if len(widgetProblems) > 0 {
			problems = append(problems, widgetProblems...)
			continue
		}
		found = append(found, w)
	}
	if len(found) == 0 && len(problems) == 0 {
		problems = append(problems, Problem{Path: root, Message: "the widgets folder holds no folder with a widget.yml"})
	}
	return found, problems
}
