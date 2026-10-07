package widgets

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the file that makes a folder a widget.
const FileName = "widget.yml"

// WidgetFormat is the widget.yml format this CLI reads.
const WidgetFormat = 1

// Input is a value a person sets on one instance of a widget.
type Input struct {
	Type    string `yaml:"type" json:"type"`
	Default any    `yaml:"default" json:"default"`
	Summary string `yaml:"summary" json:"summary"`
}

// ViewRef names how a widget is drawn.
type ViewRef struct {
	Kind string `yaml:"kind" json:"kind"`
}

// Widget is what widget.yml holds.
type Widget struct {
	Format   int    `yaml:"format"`
	Name     string `yaml:"name"`
	Summary  string `yaml:"summary"`
	Requires struct {
		Engine string `yaml:"engine"`
	} `yaml:"requires"`
	Fits        []string          `yaml:"fits"`
	Context     map[string]string `yaml:"context"`
	Inputs      map[string]Input  `yaml:"inputs"`
	Source      Source            `yaml:"source"`
	View        ViewRef           `yaml:"view"`
	Sizes       []string          `yaml:"sizes"`
	DefaultSize string            `yaml:"default_size"`
	Places      []string          `yaml:"places"`

	// Dir is the folder the widget was read from.
	Dir string `yaml:"-"`
	// PackageDir is the folder holding the package.yml the widget came with,
	// "" when it was read on its own; a script is checked against it.
	PackageDir string `yaml:"-"`
}

// Problem is one thing wrong with a widget or a board, and where it is.
type Problem struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

func (p Problem) Error() string {
	if p.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", p.Path, p.Line, p.Message)
	}
	return fmt.Sprintf("%s: %s", p.Path, p.Message)
}

var (
	widgetName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	inputName  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	template   = regexp.MustCompile(`\{\{\s*([^}]*?)\s*\}\}`)
	reference  = regexp.MustCompile(`^(inputs|context)\.([a-z][a-z0-9_-]*)$`)
)

// inputTypes are the value types an input may declare.
var inputTypes = []string{"string", "number", "boolean"}

var widgetFields = []string{
	"format", "name", "summary", "requires", "fits", "context", "inputs",
	"source", "view", "sizes", "default_size", "places",
}

// Load reads and checks the widget in dir, outside any package.
func Load(dir string) (Widget, []Problem) { return LoadIn("", dir) }

// LoadIn reads and checks the widget in dir, which belongs to the package in
// packageDir ("" when unknown). The widget is only usable when no problem
// comes back.
func LoadIn(packageDir, dir string) (Widget, []Problem) {
	path := filepath.Join(dir, FileName)
	fail := func(message string) (Widget, []Problem) {
		return Widget{Dir: dir, PackageDir: packageDir}, []Problem{{Path: path, Message: message}}
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return fail(fmt.Sprintf("reading %s: %v", FileName, err))
	}
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return fail(fmt.Sprintf("parsing %s: %v", FileName, err))
	}
	var w Widget
	if err := root.Decode(&w); err != nil {
		return fail(fmt.Sprintf("parsing %s: %v", FileName, err))
	}
	w.Dir, w.PackageDir = dir, packageDir
	return w, Validate(w, path, &root)
}

// Validate returns everything wrong with w. root is the parsed file, used only
// to point each problem at its line; it may be nil.
func Validate(w Widget, path string, root *yaml.Node) []Problem {
	c := CurrentContract()
	lines := fieldLines(root)
	var problems []Problem
	at := func(field, format string, args ...any) {
		problems = append(problems, Problem{Path: path, Line: lines[field], Message: fmt.Sprintf(format, args...)})
	}

	// Every other rule assumes the fields mean what this CLI thinks they mean,
	// which is what a format or engine mismatch breaks, so nothing is
	// reported on top of either.
	if w.Format != WidgetFormat {
		at("format", "format %d, and this CLI reads widget format %d", w.Format, WidgetFormat)
		return problems
	}
	switch constraint, err := parseEngineConstraint(w.Requires.Engine); {
	case w.Requires.Engine == "":
		at("requires", `every widget needs requires.engine, for example ">= 1.0"`)
	case err != nil:
		at("requires.engine", "requires.engine %q: %v", w.Requires.Engine, err)
	case !constraint.allows(Engine):
		at("requires.engine", "requires engine %s, and this CLI implements engine %s: update with `devmachine update`",
			w.Requires.Engine, Engine)
		return problems
	}

	for _, key := range unknownKeys(root, widgetFields) {
		at(key, "unknown field %q", key)
	}

	switch {
	case !widgetName.MatchString(w.Name):
		at("name", "name %q: use lower case letters, digits and dashes, starting with a letter", w.Name)
	case w.Dir != "" && w.Name != filepath.Base(w.Dir):
		at("name", "name is %q but the folder is %q: a widget is found by its folder", w.Name, filepath.Base(w.Dir))
	}
	if strings.TrimSpace(w.Summary) == "" {
		at("summary", "every widget needs a one-line summary: it is what `widgets list` prints")
	}

	if len(w.Fits) == 0 {
		at("fits", "fits names the layouts the widget can be drawn in: %s", strings.Join(c.Layouts, ", "))
	}
	for _, layout := range w.Fits {
		if !slices.Contains(c.Layouts, layout) {
			at("fits", "fits %q: the layouts are %s", layout, strings.Join(c.Layouts, ", "))
		}
	}

	if len(w.Sizes) == 0 {
		at("sizes", "sizes names the presets the widget accepts: %s", strings.Join(presetNames(c), ", "))
	}
	for _, size := range w.Sizes {
		if _, ok := c.Presets[size]; !ok {
			at("sizes", "size %q: the presets are %s", size, strings.Join(presetNames(c), ", "))
		}
	}
	if !slices.Contains(w.Sizes, w.DefaultSize) {
		at("default_size", "default_size %q is not one of sizes", w.DefaultSize)
	}

	for _, surface := range w.Places {
		if _, ok := c.Surfaces[surface]; !ok {
			at("places", "places %q: the surfaces are %s", surface, strings.Join(surfaceNames(c), ", "))
		}
	}

	known := contextKeys(c)
	for _, key := range sortedKeys(w.Context) {
		if !slices.Contains(known, key) {
			at("context", "context key %q is not given by any surface: the keys are %s", key, strings.Join(known, ", "))
		}
		if value := w.Context[key]; value != ContextRequired && value != ContextOptional {
			at("context", "context key %q is %q: write required or optional", key, value)
		}
	}

	for _, name := range sortedKeys(w.Inputs) {
		input := w.Inputs[name]
		if !inputName.MatchString(name) {
			at("inputs", "input %q: use lower case letters, digits and underscores, starting with a letter", name)
		}
		if !slices.Contains(inputTypes, input.Type) {
			at("inputs", "input %q has type %q: the types are %s", name, input.Type, strings.Join(inputTypes, ", "))
		} else if input.Default != nil && !valueFits(input.Type, input.Default) {
			at("inputs", "input %q is a %s, and its default %v is not", name, input.Type, input.Default)
		}
	}

	scope := sourceScope{inputs: w.Inputs, context: w.Context, packageDir: w.PackageDir}
	checkSource(w.Source, field(mapping(root), "source"), scope, c, at)
	checkView(w.View, w.Source, c, at)
	return problems
}

// Surfaces lists, sorted, every surface w can be placed on: its layout is one
// w fits, and it gives every context key w requires. Planned surfaces count.
func Surfaces(w Widget) []string {
	c := CurrentContract()
	var out []string
	for _, name := range surfaceNames(c) {
		surface := c.Surfaces[name]
		if !slices.Contains(w.Fits, surface.Layout) {
			continue
		}
		fits := true
		for key, value := range w.Context {
			if _, ok := surface.Context[key]; value == ContextRequired && !ok {
				fits = false
			}
		}
		if fits {
			out = append(out, name)
		}
	}
	return out
}

func valueFits(kind string, value any) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case int, int64, float64:
			return true
		}
		return false
	case "boolean":
		_, ok := value.(bool)
		return ok
	}
	return false
}

// fieldLines maps "key" and "key.sub" onto the line each was written on, so a
// problem can point at it.
func fieldLines(root *yaml.Node) map[string]int {
	lines := map[string]int{}
	top := mapping(root)
	if top == nil {
		return lines
	}
	for i := 0; i+1 < len(top.Content); i += 2 {
		key, value := top.Content[i], top.Content[i+1]
		lines[key.Value] = key.Line
		if value.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(value.Content); j += 2 {
				lines[key.Value+"."+value.Content[j].Value] = value.Content[j].Line
			}
		}
	}
	return lines
}

func unknownKeys(root *yaml.Node, allowed []string) []string {
	top := mapping(root)
	if top == nil {
		return nil
	}
	var out []string
	for i := 0; i+1 < len(top.Content); i += 2 {
		if key := top.Content[i].Value; !slices.Contains(allowed, key) {
			out = append(out, key)
		}
	}
	return out
}

func mapping(root *yaml.Node) *yaml.Node {
	if root == nil || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	return root.Content[0]
}

func contextKeys(c Contract) []string {
	seen := map[string]bool{}
	for _, surface := range c.Surfaces {
		for key := range surface.Context {
			seen[key] = true
		}
	}
	return sortedKeys(seen)
}

func presetNames(c Contract) []string {
	names := sortedKeys(c.Presets)
	sort.SliceStable(names, func(i, j int) bool { return area(c.Presets[names[i]]) < area(c.Presets[names[j]]) })
	return names
}

func area(p [2]int) int { return p[0] * p[1] }

func isProvider(c Contract, name string) bool {
	_, ok := c.Providers[name]
	return ok
}

func surfaceNames(c Contract) []string  { return sortedKeys(c.Surfaces) }
func providerNames(c Contract) []string { return sortedKeys(c.Providers) }
func viewNames(c Contract) []string     { return sortedKeys(c.Views) }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
