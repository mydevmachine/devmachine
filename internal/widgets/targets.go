package widgets

import (
	"fmt"
	"slices"
)

// TargetNames are the machines and workspaces config.yml declares.
type TargetNames struct {
	Machines   []string
	Workspaces []string
}

// UnknownTarget says which machine or workspace s names that names lacks, or
// "" when there is none. A name holding a template is not checked: it is only
// known once a copy of the widget fills it in.
func UnknownTarget(s Source, names TargetNames) string {
	t := s.Target
	switch {
	case t.Machine != "" && !template.MatchString(t.Machine) && !slices.Contains(names.Machines, t.Machine):
		return fmt.Sprintf("source.target names machine %q, which config.yml does not have", t.Machine)
	case t.Workspace != "" && !template.MatchString(t.Workspace) && !slices.Contains(names.Workspaces, t.Workspace):
		return fmt.Sprintf("source.target names workspace %q, which config.yml does not have", t.Workspace)
	}
	return ""
}

// BoardTargetProblems is UnknownTarget for every widget written in board b.
// It stays apart from ValidateBoard on purpose: widgets add and remove must
// keep working on a board whose widget names a machine removed since.
func BoardTargetProblems(b Board, path string, names TargetNames) []Problem {
	var problems []Problem
	for _, w := range b.Widgets {
		if !w.Inline || w.Source == nil {
			continue
		}
		if message := UnknownTarget(*w.Source, names); message != "" {
			problems = append(problems, Problem{Path: path, Line: w.lineOf("source.target"), Message: w.ID + ": " + message})
		}
	}
	return problems
}
