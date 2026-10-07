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
	line("### Providers")
	line("")
	line("| Provider | Arguments | Minimum `every` | Returns |")
	line("| --- | --- | --- | --- |")
	for _, name := range providerNames(c) {
		p := c.Providers[name]
		var args, returns []string
		for _, a := range sortedKeys(p.Args) {
			required := "optional"
			if p.Args[a].Required {
				required = "required"
			}
			args = append(args, fmt.Sprintf("`%s` %s, %s", a, p.Args[a].Type, required))
		}
		for _, r := range sortedKeys(p.Returns) {
			returns = append(returns, fmt.Sprintf("`%s` %s", r, p.Returns[r]))
		}
		line("| `%s` | %s | %s | %s |", name, orNone(strings.Join(args, "; ")), p.MinEvery, strings.Join(returns, ", "))
	}
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
	line("| View | Draws | Fields besides `kind` |")
	line("| --- | --- | --- |")
	for _, name := range viewNames(c) {
		view := c.Views[name]
		line("| `%s` | `%s` | %s |", name, strings.Join(view.Accepts, "`, `"), orNone(describeFields(view.Fields)))
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
