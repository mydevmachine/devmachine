package widgets

import (
	"fmt"
	"slices"
	"strings"
)

// OptionWarnings names each choice value in with that its option source
// lacks: a machine or workspace config.yml does not have, or a harness the
// contract does not know. The app leaves such a name out, so it is a
// warning, never a problem: a removed machine must not lock a board.
func OptionWarnings(id string, entry Entry, with map[string]any, names TargetNames) []string {
	c := CurrentContract()
	var warnings []string
	for _, name := range sortedKeys(with) {
		input, ok := entry.Inputs[name]
		if !ok || input.Type != InputChoice {
			continue
		}
		for _, value := range choiceValues(with[name]) {
			if missing := missingOption(input.From, value, c, names); missing != "" {
				warnings = append(warnings, fmt.Sprintf("%s: input %q names %s: the app leaves it out", id, name, missing))
			}
		}
	}
	return warnings
}

// BoardOptionWarnings is OptionWarnings for every typed widget on board b,
// each at its with line.
func BoardOptionWarnings(b Board, path string, lookup Lookup, names TargetNames) []Problem {
	var warnings []Problem
	for _, w := range b.Widgets {
		if w.Inline {
			continue
		}
		entry, known := lookup(w.Type)
		if !known {
			continue
		}
		for _, message := range OptionWarnings(w.ID, entry, w.With, names) {
			warnings = append(warnings, Problem{Path: path, Line: w.lineOf("with"), Message: message})
		}
	}
	return warnings
}

func missingOption(source, value string, c Contract, names TargetNames) string {
	if value == "" {
		return ""
	}
	switch source {
	case OptionMachines:
		if !slices.Contains(names.Machines, value) {
			return fmt.Sprintf("machine %q, which config.yml does not have", value)
		}
	case OptionWorkspaces:
		if !slices.Contains(names.Workspaces, value) {
			return fmt.Sprintf("workspace %q, which config.yml does not have", value)
		}
	case OptionHarnesses:
		if values := c.OptionSources[source].Values; !slices.Contains(values, value) {
			return fmt.Sprintf("harness %q, which is not one of %s", value, strings.Join(values, ", "))
		}
	}
	return ""
}

func choiceValues(value any) []string {
	if name, ok := value.(string); ok {
		return []string{name}
	}
	names, _ := nameList(value)
	return names
}
