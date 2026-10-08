package widgets

import (
	"fmt"
	"slices"
	"strings"
)

// validateInline checks a widget written straight into a board. It has no
// package and no inputs: its templates may only name what the board's area
// hands every widget, and a script is an absolute path on the target.
func validateInline(w Instance, label, path string, surface Surface, c Contract) []Problem {
	var problems []Problem
	at := func(field, format string, args ...any) {
		problems = append(problems, Problem{Path: path, Line: w.lineOf(field), Message: label + ": " + fmt.Sprintf(format, args...)})
	}

	if strings.TrimSpace(w.Title) == "" {
		at("title", "a widget written in the board needs a title")
	}
	if len(w.With) > 0 {
		at("with", "a widget written in the board has no inputs, so it takes no with")
	}
	if w.has("every") {
		at("every", "a widget written in the board sets how often in source.every, not every")
	}
	for _, size := range w.Sizes {
		if _, ok := c.Presets[size]; !ok {
			at("sizes", "size %q: the presets are %s", size, strings.Join(presetNames(c), ", "))
		}
	}
	for _, layout := range w.Fits {
		if !slices.Contains(c.Layouts, layout) {
			at("fits", "fits %q: the layouts are %s", layout, strings.Join(c.Layouts, ", "))
		}
	}
	if len(w.Fits) > 0 && surface.Layout != "" && !slices.Contains(w.Fits, surface.Layout) {
		at("fits", "fits %s, and this board's area is laid out as %s", strings.Join(w.Fits, ", "), surface.Layout)
	}

	sizes := w.Sizes
	if len(sizes) == 0 {
		sizes = presetNames(c)
	}
	switch {
	case surface.Layout == LayoutStack:
		view := ""
		if w.View != nil {
			view = w.View.Kind
		}
		problems = append(problems, stackSizeProblems(w, label, path, sizes, view, Grows(view))...)
	case surface.Layout != LayoutSlot && surface.Layout != LayoutTabs:
		if _, preset := c.Presets[w.Size]; preset && !slices.Contains(sizes, w.Size) {
			at("size", "size %s is not one of its sizes: %s", w.Size, strings.Join(sizes, ", "))
		}
		view := ""
		if w.View != nil {
			view = w.View.Kind
		}
		problems = append(problems, canvasFrameProblems(w, label, path, "its", sizes, view, Grows(view))...)
	}

	if w.Source == nil || w.View == nil {
		at("source", "a widget written in the board needs both a source and a view")
		return problems
	}
	if view, ok := c.Views[w.View.Kind]; ok && surface.Layout != "" && !DrawnIn(w.View.Kind, surface.Layout) {
		if surface.Layout == LayoutSlot {
			at("view", "%s", MenubarViewProblem(w.View.Kind))
		} else {
			at("view", "the %s view is drawn only in %s, and this board's area is laid out as %s",
				w.View.Kind, strings.Join(view.Layouts, ", "), surface.Layout)
		}
	}
	context := map[string]string{}
	for key := range surface.Context {
		context[key] = ContextOptional
	}
	scope := sourceScope{context: context, inline: true}
	checkSource(*w.Source, w.sourceNode, scope, c, at)
	checkView(*w.View, w.viewNode, *w.Source, scope, c, at)
	return problems
}
