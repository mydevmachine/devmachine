package widgets

import (
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// ViewRef is how a widget is drawn: a view, and the fields that view takes.
type ViewRef struct {
	Kind   string    `yaml:"kind" json:"kind"`
	Wrap   *bool     `yaml:"wrap,omitempty" json:"wrap,omitempty"`
	Tail   int       `yaml:"tail,omitempty" json:"tail,omitempty"`
	Unit   string    `yaml:"unit,omitempty" json:"unit,omitempty"`
	Format string    `yaml:"format,omitempty" json:"format,omitempty"`
	Value  string    `yaml:"value,omitempty" json:"value,omitempty"`
	Min    any       `yaml:"min,omitempty" json:"min,omitempty"`
	Max    any       `yaml:"max,omitempty" json:"max,omitempty"`
	Warn   any       `yaml:"warn,omitempty" json:"warn,omitempty"`
	Crit   any       `yaml:"crit,omitempty" json:"crit,omitempty"`
	OK     any       `yaml:"ok,omitempty" json:"ok,omitempty"`
	Zoom   any       `yaml:"zoom,omitempty" json:"zoom,omitempty"`
	Item   *ListItem `yaml:"item,omitempty" json:"item,omitempty"`
}

// ListItem is how a list view draws each item of its source.
type ListItem struct {
	Title    string `yaml:"title,omitempty" json:"title,omitempty"`
	Subtitle string `yaml:"subtitle,omitempty" json:"subtitle,omitempty"`
	Status   string `yaml:"status,omitempty" json:"status,omitempty"`
	Link     string `yaml:"link,omitempty" json:"link,omitempty"`
}

var (
	jsonReference = regexp.MustCompile(`^json(\.[A-Za-z0-9_-]+)+$`)
	statusRule    = regexp.MustCompile(`^(?:(?:<=|>=|<|>|==|!=)\s*-?\d+(?:\.\d+)?|(?:==|!=)\s*"[^"]*")$`)
)

func checkView(v ViewRef, node *yaml.Node, s Source, scope sourceScope, c Contract, at reporter) {
	view, ok := c.Views[v.Kind]
	switch {
	case v.Kind == "":
		at("view", "every widget needs view.kind")
		return
	case !ok:
		at("view.kind", "view.kind %q is not a view engine %s knows: %s", v.Kind, Engine, strings.Join(viewNames(c), ", "))
		return
	}
	for _, key := range mappingKeys(node) {
		if _, ok := view.Fields[key]; !ok && key != "kind" {
			at("view."+key, "view.%s is not a field of the %s view: %s", key, v.Kind, fieldList(view.Fields))
		}
	}
	output, known := sourceOutput(s, c)
	if !known {
		return
	}
	if !accepts(view, s.Kind, output) {
		if s.Kind == SourceProvider {
			at("view.kind", "view.kind %q draws %s, not %s", v.Kind, strings.Join(view.Accepts, ", "), s.Name)
			return
		}
		gives := output
		if gives == "" {
			gives = "a screen"
		}
		at("view.kind", "view.kind %q takes %s, and this %s source gives %s", v.Kind, strings.Join(view.Accepts, ", "), s.Kind, gives)
		return
	}
	checkViewFields(v, field(node, "item"), view, s, output, scope, at)
}

func checkViewFields(v ViewRef, itemNode *yaml.Node, view View, s Source, output string, scope sourceScope, at reporter) {
	if _, takesValue := view.Fields["value"]; takesValue {
		switch {
		case output == ParseJSON && v.Value == "":
			at("view", `view.value picks what to show out of the JSON: write it like "{{json.used}}"`)
		case output != ParseJSON && v.Value != "":
			at("view.value", "view.value only applies when source.parse is json")
		case v.Value != "":
			checkJSONTemplate(v.Value, at)
		}
	}

	numbers := map[string]any{"min": v.Min, "max": v.Max, "warn": v.Warn, "crit": v.Crit, "zoom": v.Zoom}
	for _, name := range sortedKeys(numbers) {
		if f, takes := view.Fields[name]; takes && f.Type == FieldNumber && numbers[name] != nil {
			checkNumber(name, numbers[name], f, at)
		}
	}
	if v.Kind == "gauge" {
		low, lowOK := floatOf(orAny(v.Min, view.Fields["min"].Default))
		high, highOK := floatOf(orAny(v.Max, view.Fields["max"].Default))
		if lowOK && highOK && low >= high {
			at("view.max", "view.max %v must be above view.min %v", high, low)
		}
	}
	if f, takes := view.Fields["tail"]; takes && v.Tail != 0 && (v.Tail < intOf(f.Min) || v.Tail > intOf(f.Max)) {
		at("view.tail", "view.tail %d is outside %v to %v lines", v.Tail, f.Min, f.Max)
	}
	if f, takes := view.Fields["format"]; takes && v.Format != "" && !slices.Contains(f.Values, v.Format) {
		at("view.format", "view.format %q: the %s view takes %s", v.Format, v.Kind, strings.Join(f.Values, ", "))
	}

	rules := map[string]any{"ok": v.OK, "warn": v.Warn}
	for _, name := range sortedKeys(rules) {
		f, takes := view.Fields[name]
		if !takes || f.Type != FieldRule || rules[name] == nil {
			continue
		}
		if text, isText := rules[name].(string); !isText || !statusRule.MatchString(strings.TrimSpace(text)) {
			at("view."+name, `view.%s %v is not a rule: write it like "< 300" or '== "ok"'`, name, rules[name])
		}
	}

	if v.Kind == "web" && s.Parse != "" {
		at("source.parse", "the web view loads the page itself: remove source.parse")
	}

	item, takesItem := view.Fields["item"]
	if !takesItem {
		return
	}
	for _, key := range mappingKeys(itemNode) {
		if _, ok := item.Fields[key]; !ok {
			at("view.item", "view.item.%s is not a field of a list item: it takes %s", key, strings.Join(sortedKeys(item.Fields), ", "))
		}
	}
	if v.Item == nil {
		return
	}
	for _, f := range []struct{ name, value string }{
		{"title", v.Item.Title}, {"subtitle", v.Item.Subtitle}, {"status", v.Item.Status}, {"link", v.Item.Link},
	} {
		checkItemTemplates(f.value, "view.item."+f.name, output, scope, at)
	}
}

func checkNumber(name string, value any, f Field, at reporter) {
	n, isNumber := floatOf(value)
	low, hasLow := floatOf(f.Min)
	high, hasHigh := floatOf(f.Max)
	switch {
	case !isNumber:
		at("view."+name, "view.%s is %v, and it has to be a number", name, value)
	case hasLow && n < low:
		at("view."+name, "view.%s %v is below %v", name, value, f.Min)
	case hasHigh && n > high:
		at("view."+name, "view.%s %v is above %v", name, value, f.Max)
	}
}

func checkJSONTemplate(value string, at reporter) {
	matches := template.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		at("view.value", `view.value %q names no field: write it like "{{json.used}}"`, value)
	}
	for _, match := range matches {
		if !jsonReference.MatchString(match[1]) {
			at("view.value", "template %s in view.value names json.<path>, like {{json.disk.used}}", match[0])
		}
	}
}

func checkItemTemplates(value, label, output string, scope sourceScope, at reporter) {
	for _, match := range template.FindAllStringSubmatch(value, -1) {
		switch ref := match[1]; {
		case ref == "item":
		case listReference.MatchString(ref):
			if output != ParseJSON {
				at("view.item", "template %s in %s reads a field, and only JSON items have fields: use {{item}}", match[0], label)
			}
		default:
			checkTemplates(match[0], label, "view.item", scope, at)
		}
	}
}

func fieldList(fields map[string]Field) string {
	if len(fields) == 0 {
		return "it takes only kind"
	}
	return "it takes kind, " + strings.Join(sortedKeys(fields), ", ")
}

func orAny(value, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}
