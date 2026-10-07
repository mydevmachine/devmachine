package widgets

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Source is where a widget's data comes from. Which fields count depends on
// Kind; the contract's sources table lists them.
type Source struct {
	Kind           string            `yaml:"kind" json:"kind"`
	Name           string            `yaml:"name,omitempty" json:"name,omitempty"`
	With           map[string]string `yaml:"with,omitempty" json:"with,omitempty"`
	Every          string            `yaml:"every,omitempty" json:"every,omitempty"`
	Run            string            `yaml:"run,omitempty" json:"run,omitempty"`
	Script         string            `yaml:"script,omitempty" json:"script,omitempty"`
	Args           []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Shell          bool              `yaml:"shell,omitempty" json:"shell,omitempty"`
	Target         Target            `yaml:"target,omitempty" json:"target,omitzero"`
	Timeout        string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Mode           string            `yaml:"mode,omitempty" json:"mode,omitempty"`
	Keep           int               `yaml:"keep,omitempty" json:"keep,omitempty"`
	Parse          string            `yaml:"parse,omitempty" json:"parse,omitempty"`
	URL            string            `yaml:"url,omitempty" json:"url,omitempty"`
	Harness        string            `yaml:"harness,omitempty" json:"harness,omitempty"`
	PermissionMode string            `yaml:"permission_mode,omitempty" json:"permission_mode,omitempty"`
	Prompt         string            `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Session        string            `yaml:"session,omitempty" json:"session,omitempty"`
}

// MarshalJSON prints a provider source in the shape engine 1.0 printed, with
// with and every always there, so an app built for 1.0 reads it unchanged;
// a package provider's target and timeout follow only when set.
func (s Source) MarshalJSON() ([]byte, error) {
	if s.Kind == SourceProvider {
		with := s.With
		if with == nil {
			with = map[string]string{}
		}
		return json.Marshal(struct {
			Kind    string            `json:"kind"`
			Name    string            `json:"name"`
			With    map[string]string `json:"with"`
			Every   string            `json:"every"`
			Target  Target            `json:"target,omitzero"`
			Timeout string            `json:"timeout,omitempty"`
		}{s.Kind, s.Name, with, s.Every, s.Target, s.Timeout})
	}
	type plain Source
	return json.Marshal(plain(s))
}

// Target is where a command, a prompt or a session runs: the computer the app
// runs on, a machine, or a workspace. A name may hold a template.
type Target struct {
	Local     bool
	Machine   string
	Workspace string

	problem string
}

const targetHelp = "write local, {machine: <name>} or {workspace: <name>}"

// IsZero says the target was not written, which means local.
func (t Target) IsZero() bool { return !t.Local && t.Machine == "" && t.Workspace == "" }

// UnmarshalYAML accepts any shape and keeps what is wrong with it for the
// checks, so a bad target is one problem at its line instead of a file that
// does not parse.
func (t *Target) UnmarshalYAML(node *yaml.Node) error {
	*t = Target{}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == TargetLocal {
			t.Local = true
			return nil
		}
		t.problem = fmt.Sprintf("source.target %q: %s", node.Value, targetHelp)
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			if value.Kind != yaml.ScalarNode {
				t.problem = "source.target: " + targetHelp
				return nil
			}
			switch key {
			case "machine":
				t.Machine = value.Value
			case "workspace":
				t.Workspace = value.Value
			default:
				t.problem = fmt.Sprintf("source.target has an unknown key %q: %s", key, targetHelp)
				return nil
			}
		}
		if (t.Machine == "") == (t.Workspace == "") {
			t.problem = "source.target names one machine or one workspace: " + targetHelp
		}
	default:
		t.problem = "source.target: " + targetHelp
	}
	return nil
}

// MarshalYAML writes the target the way a person writes it.
func (t Target) MarshalYAML() (any, error) {
	switch {
	case t.Machine != "":
		return map[string]string{"machine": t.Machine}, nil
	case t.Workspace != "":
		return map[string]string{"workspace": t.Workspace}, nil
	}
	return TargetLocal, nil
}

// MarshalJSON prints "local", {"machine": name} or {"workspace": name}.
func (t Target) MarshalJSON() ([]byte, error) {
	v, err := t.MarshalYAML()
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// UnmarshalJSON reads what MarshalJSON prints.
func (t *Target) UnmarshalJSON(body []byte) error {
	*t = Target{}
	var local string
	if err := json.Unmarshal(body, &local); err == nil {
		t.Local = local == TargetLocal
		return nil
	}
	var named map[string]string
	if err := json.Unmarshal(body, &named); err != nil {
		return fmt.Errorf("reading a target: %w", err)
	}
	t.Machine, t.Workspace = named["machine"], named["workspace"]
	return nil
}

// sourceScope is what a source may name: the inputs and context keys its
// templates can use, the package folder its script lives in ("" when
// unknown), and the package providers it may read with the name of the
// package it ships in ("" for a widget written in a board or read alone). An
// inline source is one written straight into a board.
type sourceScope struct {
	inputs     map[string]Input
	context    map[string]string
	packageDir string
	owner      string
	providers  ProviderSet
	inline     bool
}

// reporter adds one problem about a field, at that field's line.
type reporter func(field, format string, args ...any)

var listReference = regexp.MustCompile(`^item(\.[A-Za-z0-9_-]+)*$`)

// streamParses are the ways a stream can be read: line by line.
var streamParses = []string{ParseText, ParseLines, ParseANSI}

func checkSource(s Source, node *yaml.Node, scope sourceScope, c Contract, at reporter) {
	switch {
	case s.Kind == "":
		at("source", "every widget needs source.kind: engine %s knows %s", Engine, strings.Join(c.SourceKinds, ", "))
		return
	case !slices.Contains(c.SourceKinds, s.Kind):
		at("source.kind", "source.kind %q: engine %s knows %s", s.Kind, Engine, strings.Join(c.SourceKinds, ", "))
		return
	}
	kind := c.Sources[s.Kind]
	for _, key := range mappingKeys(node) {
		if _, ok := kind.Fields[key]; !ok && key != "kind" {
			at("source."+key, "source.%s is not a field of a %s source: it takes %s",
				key, s.Kind, strings.Join(sortedKeys(kind.Fields), ", "))
		}
	}
	switch s.Kind {
	case SourceProvider:
		checkProvider(s, scope, c, at)
	case SourceCommand:
		checkCommand(s, scope, kind, at)
	case SourceURL:
		checkURL(s, scope, kind, at)
	case SourcePrompt:
		checkPrompt(s, scope, kind, at)
	case SourceSession:
		checkSession(s, scope, kind, at)
	}
}

func checkProvider(s Source, scope sourceScope, c Contract, at reporter) {
	if pkg, command, ok := SplitProviderName(s.Name); ok {
		checkPackageProvider(s, pkg, command, scope, c, at)
		return
	}
	provider, ok := c.Providers[s.Name]
	if !ok {
		at("source.name", "source.name %q is not a provider engine %s knows: %s",
			s.Name, Engine, strings.Join(providerNames(c), ", "))
		return
	}
	if !s.Target.IsZero() || s.Target.problem != "" {
		at("source.target", "source.target: %s is the app's own data, so it takes no target", s.Name)
	}
	if s.Timeout != "" {
		at("source.timeout", "source.timeout: %s is the app's own data, so it takes no timeout", s.Name)
	}
	for _, key := range sortedKeys(provider.Context) {
		switch {
		case scope.inline && scope.context[key] == "":
			at("source.name", "source.name %s needs context.%s, which this board's area does not give", s.Name, key)
		case !scope.inline && scope.context[key] != ContextRequired:
			at("source.name", "source.name %s needs context.%s: declare context: {%s: required}", s.Name, key, key)
		}
	}
	for _, arg := range sortedKeys(s.With) {
		if _, ok := provider.Args[arg]; !ok {
			at("source.with", "source.with.%s is not an argument of %s", arg, s.Name)
		}
	}
	for _, arg := range sortedKeys(provider.Args) {
		if _, given := s.With[arg]; provider.Args[arg].Required && !given {
			at("source.with", "source.with.%s is required by %s", arg, s.Name)
		}
	}
	for _, arg := range sortedKeys(s.With) {
		checkTemplates(s.With[arg], "source.with."+arg, "source.with", scope, at)
	}
	minimum, _ := time.ParseDuration(provider.MinEvery)
	every, err := time.ParseDuration(s.Every)
	switch {
	case s.Every == "":
		at("source", "every widget needs source.every, at least %s for %s", provider.MinEvery, s.Name)
	case err != nil:
		at("source.every", "source.every %q is not a duration: write it like 60s or 5m", s.Every)
	case every < minimum:
		at("source.every", "source.every %s is below the %s minimum of %s", s.Every, s.Name, provider.MinEvery)
	}
}

func checkCommand(s Source, scope sourceScope, kind SourceKind, at reporter) {
	switch {
	case s.Run == "" && s.Script == "":
		at("source", "a command source needs run or script")
	case s.Run != "" && s.Script != "":
		at("source.script", "a command source has run or script, not both")
	}
	program := strings.TrimSpace(s.Run)
	templates := template.FindAllStringSubmatch(s.Run, -1)
	switch {
	case s.Shell:
		for _, match := range templates {
			if name := EnvName(match[1]); name != "" {
				at("source.run", `source.run is a shell line, so it cannot hold %s: read it as "$%s" instead`, match[0], name)
			} else {
				at("source.run", "source.run is a shell line, so it cannot hold %s: read values as $DM_INPUT_<NAME> or $DM_CONTEXT_<KEY> instead", match[0])
			}
		}
		if len(s.Args) > 0 {
			at("source.args", "a shell line takes no source.args: write the words in source.run, and read values as $DM_INPUT_<NAME>")
		}
	case len(templates) > 0:
		at("source.run", "source.run cannot hold %s: a value must not pick the program — name the program in run, and put the value in source.args", templates[0][0])
	case strings.ContainsFunc(program, unicode.IsSpace):
		at("source.run", "source.run %q has spaces: put each argument in source.args, or set shell: true to run it as a shell line", s.Run)
	case strings.HasPrefix(program, "-"):
		at("source.run", "source.run %q starts with -: a program name never does — name the program, and put options in source.args", s.Run)
	case strings.Contains(program, "="):
		at("source.run", "source.run %q has =: it reads as a variable to set, not a program — name the program, or set shell: true", s.Run)
	}
	for i, arg := range s.Args {
		checkTemplates(arg, fmt.Sprintf("source.args[%d]", i), "source.args", scope, at)
	}
	if s.Script != "" {
		checkScript(s.Script, scope, at)
	}
	checkTarget(s.Target, scope, at)
	checkEnum(s, "mode", s.Mode, kind, at)
	checkEnum(s, "parse", s.Parse, kind, at)

	keep := kind.Fields["keep"]
	if s.Mode != ModeStream {
		if s.Keep != 0 {
			at("source.keep", "source.keep only applies to mode: stream")
		}
		checkEvery(s, kind, true, at)
		checkTimeout(s, kind, at)
		return
	}
	if s.Every != "" {
		at("source.every", "a stream runs while the widget is on screen: remove source.every")
	}
	if s.Timeout != "" {
		at("source.timeout", "a stream has no timeout: remove source.timeout")
	}
	if s.Parse != "" && !slices.Contains(streamParses, s.Parse) {
		at("source.parse", "a stream reads lines as they come: source.parse is %s", strings.Join(streamParses, ", "))
	}
	if s.Keep != 0 && (s.Keep < intOf(keep.Min) || s.Keep > intOf(keep.Max)) {
		at("source.keep", "source.keep %d is outside %v to %v lines", s.Keep, keep.Min, keep.Max)
	}
}

func checkURL(s Source, scope sourceScope, kind SourceKind, at reporter) {
	switch {
	case s.URL == "":
		at("source", "a url source needs source.url")
	case !strings.HasPrefix(s.URL, "https://") && !strings.HasPrefix(s.URL, "http://"):
		at("source.url", "source.url %q: write a full address starting with https:// or http://", s.URL)
	}
	checkTemplates(s.URL, "source.url", "source.url", scope, at)
	checkEvery(s, kind, true, at)
	checkTimeout(s, kind, at)
	checkEnum(s, "parse", s.Parse, kind, at)
}

func checkPrompt(s Source, scope sourceScope, kind SourceKind, at reporter) {
	if s.Harness == "" {
		at("source", "a prompt source needs source.harness: %s", strings.Join(kind.Fields["harness"].Values, " or "))
	}
	checkEnum(s, "harness", s.Harness, kind, at)
	checkPermissionMode(s, kind, at)
	if strings.TrimSpace(s.Prompt) == "" {
		at("source", "a prompt source needs source.prompt: the text sent to the harness")
	}
	checkTemplates(s.Prompt, "source.prompt", "source.prompt", scope, at)
	checkTarget(s.Target, scope, at)
	checkEvery(s, kind, false, at)
	checkTimeout(s, kind, at)
}

func checkPermissionMode(s Source, kind SourceKind, at reporter) {
	modes, known := kind.Fields["permission_mode"].ByHarness[s.Harness]
	switch {
	case s.PermissionMode == "" || !known:
	case !slices.Contains(modes, s.PermissionMode):
		at("source.permission_mode", "source.permission_mode %q: a %s prompt takes %s", s.PermissionMode, s.Harness, strings.Join(modes, ", "))
	default:
		if problem := promptEveryProblem(s, s.Every); problem != "" {
			at("source.permission_mode", "%s", problem)
		}
	}
}

// promptEveryProblem says why a prompt cannot run every `every`, or "": a
// permission mode that runs without any check runs only when somebody
// presses refresh. An every that is not a duration is reported elsewhere.
func promptEveryProblem(s Source, every string) string {
	if s.Kind != SourcePrompt || !IsDangerousPermissionMode(s.PermissionMode) {
		return ""
	}
	if _, err := time.ParseDuration(every); err != nil {
		return ""
	}
	return fmt.Sprintf("permission_mode %s runs without any check, so it runs only when you press refresh: write every: manual", s.PermissionMode)
}

func needsPermissionModeEngine(w Widget) bool {
	constraint, err := parseEngineConstraint(w.Requires.Engine)
	return err == nil && constraint.allows(LastEngineWithoutPermissionModes) &&
		w.Source.Kind == SourcePrompt && w.Source.PermissionMode != ""
}

func checkSession(s Source, scope sourceScope, kind SourceKind, at reporter) {
	if strings.TrimSpace(s.Session) == "" {
		at("source", "a session source needs source.session: the name of the session to show")
	}
	checkTemplates(s.Session, "source.session", "source.session", scope, at)
	checkTarget(s.Target, scope, at)
	checkEvery(s, kind, true, at)
}

func checkTarget(t Target, scope sourceScope, at reporter) {
	if t.problem != "" {
		at("source.target", "%s", t.problem)
		return
	}
	checkTemplates(t.Machine, "source.target.machine", "source.target", scope, at)
	checkTemplates(t.Workspace, "source.target.workspace", "source.target", scope, at)
}

func checkEvery(s Source, kind SourceKind, required bool, at reporter) {
	minimum, _ := time.ParseDuration(kind.MinEvery)
	every, err := time.ParseDuration(s.Every)
	switch {
	case s.Every == "":
		if required {
			at("source", "a %s source needs source.every: a duration of at least %s, or manual", s.Kind, kind.MinEvery)
		}
	case s.Every == EveryManual:
	case err != nil:
		at("source.every", "source.every %q is neither a duration nor manual: write it like 60s, 5m or manual", s.Every)
	case every < minimum:
		at("source.every", "source.every %s is below the %s minimum of %s", s.Every, s.Kind, kind.MinEvery)
	}
}

func checkTimeout(s Source, kind SourceKind, at reporter) {
	if s.Timeout == "" {
		return
	}
	limit := fmt.Sprint(kind.Fields["timeout"].Max)
	maximum, _ := time.ParseDuration(limit)
	timeout, err := time.ParseDuration(s.Timeout)
	switch {
	case err != nil:
		at("source.timeout", "source.timeout %q is not a duration: write it like 30s or 5m", s.Timeout)
	case timeout <= 0:
		at("source.timeout", "source.timeout %s must be above zero", s.Timeout)
	case timeout > maximum:
		at("source.timeout", "source.timeout %s is above the %s maximum", s.Timeout, limit)
	}
}

func checkEnum(s Source, field, value string, kind SourceKind, at reporter) {
	values := kind.Fields[field].Values
	if value != "" && !slices.Contains(values, value) {
		at("source."+field, "source.%s %q: a %s source takes %s", field, value, s.Kind, strings.Join(values, ", "))
	}
}

func checkScript(script string, scope sourceScope, at reporter) {
	switch {
	case template.MatchString(script):
		at("source.script", "source.script cannot hold a template: it names one file")
	case scope.inline && !path.IsAbs(script):
		at("source.script", "source.script %q: a widget written in a board names an absolute path on the target", script)
	case scope.inline:
	case path.IsAbs(script) || !staysInside(script):
		at("source.script", "source.script %q: a package widget names a file inside its package, relative to package.yml", script)
	case scope.packageDir != "":
		if problem := scriptFileProblem(scope.packageDir, script); problem != "" {
			at("source.script", "source.script %q %s", script, problem)
		}
	}
}

func staysInside(p string) bool {
	clean := path.Clean(p)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

// scriptFileProblem says what is wrong with the file a package widget runs,
// or "" when it is a file inside the package that every account can run: a
// workspace target runs it as that workspace's own account.
func scriptFileProblem(packageDir, script string) string {
	root, err := filepath.EvalSymlinks(packageDir)
	if err != nil {
		return fmt.Sprintf("cannot be checked: %v", err)
	}
	file, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(script)))
	if err != nil {
		return "is not in the package"
	}
	if rel, err := filepath.Rel(root, file); err != nil || !staysInside(filepath.ToSlash(rel)) {
		return "leads outside the package through a link"
	}
	info, err := os.Stat(file)
	switch {
	case err != nil:
		return "is not in the package"
	case info.IsDir():
		return "is a folder, not a file"
	case info.Mode()&0o111 == 0:
		return "is not executable: run chmod +x on it"
	case info.Mode()&0o005 != 0o005:
		return "only its owner can run it: a workspace runs it as its own account, so make it chmod 755"
	}
	return ""
}

var notEnvChar = regexp.MustCompile(`[^A-Z0-9]`)

// EnvName is the environment variable a shell line reads a value from:
// inputs.max_lines is DM_INPUT_MAX_LINES and context.workspace is
// DM_CONTEXT_WORKSPACE. It is "" for a reference to anything else.
func EnvName(ref string) string {
	scope, key, ok := strings.Cut(strings.TrimSpace(ref), ".")
	if !ok || key == "" {
		return ""
	}
	prefix := map[string]string{"inputs": "DM_INPUT_", "context": "DM_CONTEXT_"}[scope]
	if prefix == "" {
		return ""
	}
	return prefix + notEnvChar.ReplaceAllString(strings.ToUpper(key), "_")
}

func checkTemplates(value, label, line string, scope sourceScope, at reporter) {
	for _, match := range template.FindAllStringSubmatch(value, -1) {
		ref := reference.FindStringSubmatch(match[1])
		switch {
		case listReference.MatchString(match[1]):
			at(line, "template %s in %s: item only works inside a list view's item", match[0], label)
		case ref == nil:
			at(line, "template %s in %s names neither inputs.<name> nor context.<name>", match[0], label)
		case ref[1] == "inputs" && scope.inline:
			at(line, "template %s in %s: a widget written in a board has no inputs", match[0], label)
		case ref[1] == "inputs":
			if _, ok := scope.inputs[ref[2]]; !ok {
				at(line, "template %s in %s needs inputs.%s", match[0], label, ref[2])
			}
		default:
			if _, ok := scope.context[ref[2]]; !ok {
				at(line, "template %s in %s needs context.%s to be declared", match[0], label, ref[2])
			}
		}
	}
}

// sourceOutput is what a source hands its view: a provider's name, or how a
// command or a url is parsed. A package provider gives one JSON object; a
// prompt gives text; a session gives nothing a parse names, so only a view
// that takes its kind draws it. known is false for a source whose own problem
// is already reported.
func sourceOutput(s Source, scope sourceScope, c Contract) (string, bool) {
	parsed := func(fallback string) (string, bool) {
		output := orDefault(s.Parse, fallback)
		return output, slices.Contains(c.Sources[s.Kind].Fields["parse"].Values, output)
	}
	switch s.Kind {
	case SourceProvider:
		if pkg, command, ok := SplitProviderName(s.Name); ok {
			if scope.inline && scope.providers == nil {
				return ParseJSON, true
			}
			_, known := scope.providers[pkg][command]
			return ParseJSON, known && (scope.inline || pkg == scope.owner)
		}
		return s.Name, isProvider(c, s.Name)
	case SourceCommand:
		return parsed(ParseText)
	case SourceURL:
		return parsed(ParseStatus)
	case SourcePrompt:
		return ParseText, true
	case SourceSession:
		return "", true
	}
	return "", false
}

func accepts(view View, kind, output string) bool {
	for _, accepted := range view.Accepts {
		if (output != "" && accepted == output) || accepted == AcceptsKind+kind {
			return true
		}
	}
	return false
}

func mappingKeys(m *yaml.Node) []string {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		keys = append(keys, m.Content[i].Value)
	}
	return keys
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// intOf reads an int the contract stores as any; anything else is 0.
func intOf(v any) int {
	n, _ := floatOf(v)
	return int(n)
}

// floatOf reads a number written in YAML or stored in the contract, which
// arrives as one of several Go types.
func floatOf(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
