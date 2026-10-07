package widgets

import (
	"errors"
	"fmt"
	"strings"
)

// The markers around the part of docs/reference/widget-format.md that is
// generated from the contract.
const (
	ReferenceStart = "<!-- generated from the engine contract by `make widget-format`: start -->"
	ReferenceEnd   = "<!-- generated from the engine contract by `make widget-format`: end -->"
)

// ReferenceTables is the generated part of the widget format page.
func ReferenceTables() string {
	c := CurrentContract()
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	line("Engine **%s**. Widget format %d, board format %d.", c.Engine, c.Formats["widget"], c.Formats["board"])
	line("")
	line("### Sizes")
	line("")
	line("One unit is %dpt; positions and free resizes snap to %dpt.", c.Unit, c.Snap)
	line("")
	line("One preset row in a sidebar is %dpt high, and a widget there is as wide as the panel. "+
		"`size: auto` makes a view that grows as tall as what it shows.", c.StackRow)
	line("")
	line("The menu bar holds at most %d widgets, left to right, each one line drawn by one of the views %s. "+
		"Text shows its first line, cut at %d characters. A widget there runs at most every %s. "+
		"A tab in the menu bar popover fills it, so a `size` there is ignored.",
		c.SlotMax, "`"+strings.Join(c.SlotEntry.Views, "`, `")+"`", c.SlotEntry.TextMax, c.SlotEntry.MinEvery)
	line("")
	line("| Preset | Units | Points |")
	line("| --- | --- | --- |")
	for _, name := range presetNames(c) {
		p := c.Presets[name]
		line("| `%s` | %d×%d | %d×%d |", name, p[0], p[1], p[0]*c.Unit, p[1]*c.Unit)
	}
	line("")
	line("### Surfaces")
	line("")
	line("| Surface | Layout | Status | Context it gives |")
	line("| --- | --- | --- | --- |")
	for _, name := range surfaceNames(c) {
		s := c.Surfaces[name]
		var keys []string
		for _, key := range sortedKeys(s.Context) {
			keys = append(keys, fmt.Sprintf("`%s` (%s, %s)", key, s.Context[key].Type, s.Context[key].Presence))
		}
		line("| `%s` | %s | %s | %s |", name, s.Layout, s.Status, orNone(strings.Join(keys, ", ")))
	}
	line("")
	line("### Context types")
	line("")
	line("| Type | Fields |")
	line("| --- | --- |")
	for _, name := range sortedKeys(c.ContextTypes) {
		var fields []string
		for _, f := range sortedKeys(c.ContextTypes[name]) {
			fields = append(fields, fmt.Sprintf("`%s` %s", f, c.ContextTypes[name][f]))
		}
		line("| `%s` | %s |", name, orNone(strings.Join(fields, ", ")))
	}
	line("")
	line("### Inputs")
	line("")
	line("| Type | Fields besides `type`, `default` and `summary` | A board's `with` value |")
	line("| --- | --- | --- |")
	for _, name := range sortedKeys(c.InputTypes) {
		input := c.InputTypes[name]
		line("| `%s` | %s | %s |", name, orNone(describeFields(input.Fields)), input.With)
	}
	line("")
	sources := make([]string, 0, len(c.OptionSources))
	for _, name := range sortedKeys(c.OptionSources) {
		from := "the " + c.OptionSources[name].Config + " in config.yml"
		if values := c.OptionSources[name].Values; len(values) > 0 {
			from = strings.Join(values, ", ")
		}
		sources = append(sources, fmt.Sprintf("`%s` (%s)", name, from))
	}
	line("A choice takes its options from %s.", strings.Join(sources, ", "))
	line("")
	line("An entry with a `type` may also set %s on a board: they change only that copy.", describeFields(c.EntryOverrides))
	line("")
	line("### Providers")
	line("")
	line("| Provider | Arguments | Context it needs | Minimum `every` | Returns |")
	line("| --- | --- | --- | --- | --- |")
	for _, name := range providerNames(c) {
		p := c.Providers[name]
		var args, returns, context []string
		for _, a := range sortedKeys(p.Args) {
			required := "optional"
			if p.Args[a].Required {
				required = "required"
			}
			args = append(args, fmt.Sprintf("`%s` %s, %s", a, p.Args[a].Type, required))
		}
		for _, key := range sortedKeys(p.Context) {
			context = append(context, fmt.Sprintf("`%s` %s", key, p.Context[key]))
		}
		for _, r := range sortedKeys(p.Returns) {
			returns = append(returns, fmt.Sprintf("`%s` %s", r, p.Returns[r]))
		}
		line("| `%s` | %s | %s | %s | %s |", name, orNone(strings.Join(args, "; ")), orNone(strings.Join(context, ", ")),
			p.MinEvery, strings.Join(returns, ", "))
	}
	line("")
	pp := c.PackageProvider
	withFlag, withRest, found := strings.Cut(pp.With, " after ")
	with := "`" + withFlag + "`"
	if found {
		with += " after " + withRest
	}
	line("### Package providers")
	line("")
	line("A package declares providers in its `package.yml`; a widget names one `%s`. `%s/…` is the app's own. "+
		"It runs on a %s, never on your computer, and its answer is one JSON object, read like `parse: %s`. "+
		"Each `with` key becomes %s. Its `min_every` is at least %s, and `returns` types are %s, with `%s` for an optional field. "+
		"Its widgets wait for approval when the package is %s.",
		pp.Name, strings.Join(pp.Reserved, "/…`, `"), strings.Join(pp.Targets, " or a "), pp.Output, with,
		pp.MinEvery, strings.Join(pp.Returns, ", "), pp.Optional, pp.Approval)
	line("")
	line("### Source kinds")
	line("")
	line("| Kind | Minimum `every` | Waits for approval in a board | Fields besides `kind` |")
	line("| --- | --- | --- | --- |")
	for _, name := range c.SourceKinds {
		kind := c.Sources[name]
		minimum := kind.MinEvery
		if minimum == "" {
			minimum = "the provider's"
		}
		approval := "no"
		if kind.Approval {
			approval = "yes"
		}
		line("| `%s` | %s | %s | %s |", name, minimum, approval, describeFields(kind.Fields))
	}
	line("")
	line("### Views")
	line("")
	line("| View | Draws | Drawn in | Fields besides `kind` |")
	line("| --- | --- | --- | --- |")
	for _, name := range viewNames(c) {
		view := c.Views[name]
		var layouts []string
		for _, layout := range c.Layouts {
			if DrawnIn(name, layout) {
				layouts = append(layouts, layout)
			}
		}
		drawn := strings.Join(layouts, ", ")
		if len(layouts) == len(c.Layouts) {
			drawn = "any layout"
		}
		if view.Grows {
			drawn += "; grows with its content"
		}
		line("| `%s` | `%s` | %s | %s |", name, strings.Join(view.Accepts, "`, `"), drawn, orNone(describeFields(view.Fields)))
	}
	return b.String()
}

func describeFields(fields map[string]Field) string {
	parts := make([]string, 0, len(fields))
	for _, name := range sortedKeys(fields) {
		parts = append(parts, describeField(name, fields[name]))
	}
	return strings.Join(parts, "; ")
}

func describeField(name string, f Field) string {
	details := []string{f.Type}
	if f.Required {
		details = append(details, "required")
	}
	if len(f.Values) > 0 {
		details = append(details, strings.Join(f.Values, "/"))
	}
	if f.Default != nil {
		details = append(details, fmt.Sprintf("default `%v`", f.Default))
	}
	if f.Min != nil {
		details = append(details, fmt.Sprintf("min %v", f.Min))
	}
	if f.Max != nil {
		details = append(details, fmt.Sprintf("max %v", f.Max))
	}
	text := fmt.Sprintf("`%s` %s", name, strings.Join(details, ", "))
	if len(f.Fields) > 0 {
		text += " (" + describeFields(f.Fields) + ")"
	}
	return text
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// ReplaceReference returns page with the text between the markers replaced by
// ReferenceTables.
func ReplaceReference(page string) (string, error) {
	before, rest, found := strings.Cut(page, ReferenceStart)
	if !found {
		return "", errors.New("the page has no start marker for the generated tables")
	}
	_, after, found := strings.Cut(rest, ReferenceEnd)
	if !found {
		return "", errors.New("the page has no end marker for the generated tables")
	}
	return before + ReferenceStart + "\n\n" + ReferenceTables() + "\n" + ReferenceEnd + after, nil
}
