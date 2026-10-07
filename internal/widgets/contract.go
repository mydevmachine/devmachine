// Package widgets reads, checks and writes what the app's widget engine draws:
// the engine contract, widget.yml files shipped in packages, and boards.
package widgets

// Engine is the version of the contract this CLI implements.
const Engine = "1.2"

// The layouts a surface can have.
const (
	LayoutCanvas = "canvas"
	LayoutStack  = "stack"
	LayoutSlot   = "slot"
)

// The states of a surface.
const (
	StatusAvailable = "available"
	StatusPlanned   = "planned"
)

// The presences of a context key on a surface.
const (
	PresenceAlways   = "always"
	PresenceOptional = "optional"
)

// The values a widget's context key takes.
const (
	ContextRequired = "required"
	ContextOptional = "optional"
)

// The kinds of source a widget can read.
const (
	SourceProvider = "provider"
	SourceCommand  = "command"
	SourceURL      = "url"
	SourcePrompt   = "prompt"
	SourceSession  = "session"
)

// The types a source or view field takes, as the contract names them.
const (
	FieldString   = "string"
	FieldBool     = "bool"
	FieldInt      = "int"
	FieldNumber   = "number"
	FieldDuration = "duration"
	FieldEvery    = "every"
	FieldEnum     = "enum"
	FieldList     = "list"
	FieldArgs     = "args"
	FieldTarget   = "target"
	FieldPath     = "path"
	FieldURL      = "url"
	FieldTemplate = "template"
	FieldRule     = "rule"
	FieldObject   = "object"
	FieldProvider = "provider"
)

// The ways a command or a url's output is read.
const (
	ParseText   = "text"
	ParseLines  = "lines"
	ParseNumber = "number"
	ParseJSON   = "json"
	ParseANSI   = "ansi"
	ParseStatus = "status"
)

// The ways a command runs.
const (
	ModePoll   = "poll"
	ModeStream = "stream"
)

// EveryManual runs a source only when somebody presses the widget's button.
const EveryManual = "manual"

// TargetLocal runs a source on the computer the app runs on.
const TargetLocal = "local"

// AcceptsKind starts an accepts entry that takes any output of one source kind.
const AcceptsKind = "kind:"

// SizeCustom is the size a board entry has after a free resize.
const SizeCustom = "custom"

// SizeAuto makes a widget in a sidebar as tall as what it shows. Only a view
// that grows takes it.
const SizeAuto = "auto"

// ContextKey is one value a surface hands to the widgets on it.
type ContextKey struct {
	Type     string `json:"type"`
	Presence string `json:"presence"`
}

// Surface is one place in the app that holds widgets.
type Surface struct {
	Layout  string                `json:"layout"`
	Status  string                `json:"status"`
	Context map[string]ContextKey `json:"context"`
}

// Arg is one argument a provider takes.
type Arg struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// Provider is one data source the app runs. Context names the keys the area
// must hand it, each required.
type Provider struct {
	Args     map[string]Arg    `json:"args"`
	MinEvery string            `json:"min_every"`
	Context  map[string]string `json:"context,omitempty"`
	Returns  map[string]string `json:"returns"`
}

// Field describes one key a source or a view takes.
type Field struct {
	Type     string           `json:"type"`
	Required bool             `json:"required,omitempty"`
	Default  any              `json:"default,omitempty"`
	Values   []string         `json:"values,omitempty"`
	Min      any              `json:"min,omitempty"`
	Max      any              `json:"max,omitempty"`
	Fields   map[string]Field `json:"fields,omitempty"`
}

// SourceKind is one kind of source: how often it may run, whether a widget
// written in a board waits for the owner's approval before it runs, and the
// keys it takes besides kind.
type SourceKind struct {
	MinEvery string           `json:"min_every,omitempty"`
	Approval bool             `json:"approval"`
	Fields   map[string]Field `json:"fields"`
}

// View is one way the app draws a source's output. Layouts, when set, are
// the only layouts it is drawn in; Grows means it takes size auto in a stack.
type View struct {
	Accepts []string         `json:"accepts"`
	Fields  map[string]Field `json:"fields,omitempty"`
	Layouts []string         `json:"layouts,omitempty"`
	Grows   bool             `json:"grows,omitempty"`
}

// StackEntry is how a widget sits in a stack: the board keys it must not
// have, and what its size and collapsed keys take.
type StackEntry struct {
	Forbids   []string `json:"forbids"`
	Size      string   `json:"size"`
	Collapsed string   `json:"collapsed"`
}

// Contract is everything a widget may name, at one engine version.
type Contract struct {
	Engine       string                       `json:"engine"`
	Layouts      []string                     `json:"layouts"`
	Unit         int                          `json:"unit"`
	Snap         int                          `json:"snap"`
	Presets      map[string][2]int            `json:"presets"`
	StackRow     int                          `json:"stack_row"`
	StackEntry   StackEntry                   `json:"stack_entry"`
	Surfaces     map[string]Surface           `json:"surfaces"`
	ContextTypes map[string]map[string]string `json:"context_types"`
	Providers    map[string]Provider          `json:"providers"`
	Views        map[string]View              `json:"views"`
	SourceKinds  []string                     `json:"source_kinds"`
	Sources      map[string]SourceKind        `json:"sources"`
	Formats      map[string]int               `json:"formats"`
}

// CurrentContract returns engine 1.2, built fresh on every call so no caller
// can change what another one reads.
func CurrentContract() Contract {
	appProvider := func(args map[string]Arg, returns map[string]string) Provider {
		return Provider{Args: args, MinEvery: "5s", Returns: returns}
	}
	sessionProvider := func(returns map[string]string) Provider {
		p := appProvider(map[string]Arg{}, returns)
		p.Context = map[string]string{"session": ContextRequired}
		return p
	}
	stackView := func(provider string) View {
		return View{Accepts: []string{provider}, Layouts: []string{LayoutStack}, Grows: true}
	}
	timeout := func(fallback string) Field {
		return Field{Type: FieldDuration, Default: fallback, Max: "10m"}
	}
	target := Field{Type: FieldTarget, Default: TargetLocal}
	tail := Field{Type: FieldInt, Min: 1, Max: 2000}
	return Contract{
		Engine:  Engine,
		Layouts: []string{LayoutCanvas, LayoutStack, LayoutSlot},
		Unit:    80,
		Snap:    8,
		Presets: map[string][2]int{
			"small": {2, 2}, "medium": {4, 2}, "large": {4, 4}, "wide": {8, 2}, "tall": {2, 4},
		},
		StackRow: 40,
		StackEntry: StackEntry{
			Forbids: []string{"frame", "z"}, Size: "preset | auto", Collapsed: "bool",
		},
		Surfaces: map[string]Surface{
			"home": {Layout: LayoutCanvas, Status: StatusAvailable, Context: map[string]ContextKey{}},
			"sidebar": {Layout: LayoutStack, Status: StatusAvailable, Context: map[string]ContextKey{
				"selected": {Type: "workspace", Presence: PresenceOptional},
			}},
			"context-sidebar": {Layout: LayoutStack, Status: StatusAvailable, Context: map[string]ContextKey{
				"machine":   {Type: "machine", Presence: PresenceAlways},
				"session":   {Type: "session", Presence: PresenceAlways},
				"workspace": {Type: "workspace", Presence: PresenceOptional},
				"path":      {Type: "path", Presence: PresenceOptional},
				"repo":      {Type: "repo", Presence: PresenceOptional},
				"branch":    {Type: "string", Presence: PresenceOptional},
				"harness":   {Type: "string", Presence: PresenceOptional},
			}},
		},
		ContextTypes: map[string]map[string]string{
			"machine":   {"name": "string"},
			"workspace": {"name": "string", "machine": "machine", "user": "string", "path": "path"},
			"session":   {"name": "string", "kind": "string", "harness": "string?"},
			"repo":      {"owner": "string", "name": "string"},
			"path":      {},
			"string":    {},
		},
		Providers: map[string]Provider{
			"app/clock": appProvider(map[string]Arg{},
				map[string]string{"time": "string", "date": "string", "host": "string"}),
			"app/summary": appProvider(map[string]Arg{},
				map[string]string{"sessions": "int", "harness_sessions": "int", "workspaces": "int"}),
			"app/machines": appProvider(map[string]Arg{},
				map[string]string{"list": "machine_stats"}),
			"app/harness-usage": appProvider(map[string]Arg{"harness": {Type: "string", Required: true}},
				map[string]string{"harness": "string", "windows": "list", "error": "string?"}),
			"app/workspaces": appProvider(map[string]Arg{},
				map[string]string{"workspaces": "workspace_sessions"}),
			"app/session-context": sessionProvider(map[string]string{
				"cwd": "path", "harness": "string?", "plan": "plan?", "prs": "list",
				"links": "list", "agents": "list", "monitors": "list", "shells": "list",
			}),
			"app/shortcuts":    sessionProvider(map[string]string{"shortcuts": "list"}),
			"app/publish-port": appProvider(map[string]Arg{}, map[string]string{"available": "bool"}),
		},
		Views: map[string]View{
			"app.clock":         {Accepts: []string{"app/clock"}},
			"app.summary":       {Accepts: []string{"app/summary"}},
			"app.machines":      {Accepts: []string{"app/machines"}},
			"app.harness-usage": {Accepts: []string{"app/harness-usage"}},
			"app.workspaces":    stackView("app/workspaces"),
			"app.shortcuts":     stackView("app/shortcuts"),
			"app.publish-port":  stackView("app/publish-port"),
			"app.monitors":      stackView("app/session-context"),
			"app.shells":        stackView("app/session-context"),
			"app.sub-agents":    stackView("app/session-context"),
			"app.todo":          stackView("app/session-context"),
			"app.pull-requests": stackView("app/session-context"),
			"app.links":         stackView("app/session-context"),
			"text": {Accepts: []string{ParseText, ParseLines, ParseANSI}, Fields: map[string]Field{
				"wrap": {Type: FieldBool, Default: true},
				"tail": tail,
			}},
			"number": {Accepts: []string{ParseNumber, ParseJSON}, Fields: map[string]Field{
				"value":  {Type: FieldTemplate},
				"unit":   {Type: FieldString},
				"format": {Type: FieldEnum, Values: []string{"plain", "percent", "bytes", "duration"}, Default: "plain"},
			}},
			"gauge": {Accepts: []string{ParseNumber, ParseJSON}, Fields: map[string]Field{
				"value": {Type: FieldTemplate},
				"min":   {Type: FieldNumber, Default: 0},
				"max":   {Type: FieldNumber, Default: 100},
				"unit":  {Type: FieldString},
				"warn":  {Type: FieldNumber},
				"crit":  {Type: FieldNumber},
			}},
			"status": {Accepts: []string{ParseStatus, ParseNumber, ParseJSON, ParseText}, Fields: map[string]Field{
				"value": {Type: FieldTemplate},
				"ok":    {Type: FieldRule},
				"warn":  {Type: FieldRule},
			}},
			"list": {Accepts: []string{ParseLines, ParseJSON}, Fields: map[string]Field{
				"item": {Type: FieldObject, Fields: map[string]Field{
					"title":    {Type: FieldTemplate, Default: "{{item}}"},
					"subtitle": {Type: FieldTemplate},
					"status":   {Type: FieldTemplate},
					"link":     {Type: FieldTemplate},
				}},
			}},
			"sparkline": {Accepts: []string{ParseNumber, ParseJSON}, Fields: map[string]Field{
				"value": {Type: FieldTemplate},
				"unit":  {Type: FieldString},
				"max":   {Type: FieldNumber},
			}},
			"markdown": {Accepts: []string{ParseText}},
			"web": {Accepts: []string{AcceptsKind + SourceURL}, Fields: map[string]Field{
				"zoom": {Type: FieldNumber, Default: 1, Min: 0.5, Max: 2},
			}},
			"terminal": {Accepts: []string{ParseANSI, ParseText, AcceptsKind + SourceSession}, Fields: map[string]Field{
				"tail": tail,
			}},
		},
		SourceKinds: []string{SourceProvider, SourceCommand, SourceURL, SourcePrompt, SourceSession},
		Sources: map[string]SourceKind{
			SourceProvider: {Fields: map[string]Field{
				"name":  {Type: FieldProvider, Required: true},
				"with":  {Type: FieldArgs},
				"every": {Type: FieldDuration, Required: true},
			}},
			SourceCommand: {MinEvery: "5s", Approval: true, Fields: map[string]Field{
				"run":     {Type: FieldString},
				"script":  {Type: FieldPath},
				"args":    {Type: FieldList},
				"shell":   {Type: FieldBool, Default: false},
				"target":  target,
				"every":   {Type: FieldEvery},
				"timeout": timeout("30s"),
				"mode":    {Type: FieldEnum, Values: []string{ModePoll, ModeStream}, Default: ModePoll},
				"keep":    {Type: FieldInt, Default: 200, Min: 1, Max: 2000},
				"parse":   {Type: FieldEnum, Values: []string{ParseText, ParseLines, ParseNumber, ParseJSON, ParseANSI}, Default: ParseText},
			}},
			SourceURL: {MinEvery: "5s", Fields: map[string]Field{
				"url":     {Type: FieldURL, Required: true},
				"every":   {Type: FieldEvery, Required: true},
				"timeout": timeout("30s"),
				"parse":   {Type: FieldEnum, Values: []string{ParseStatus, ParseText, ParseJSON}, Default: ParseStatus},
			}},
			SourcePrompt: {MinEvery: "15m", Approval: true, Fields: map[string]Field{
				"harness": {Type: FieldEnum, Values: []string{"claude", "codex"}, Required: true},
				"prompt":  {Type: FieldString, Required: true},
				"target":  target,
				"every":   {Type: FieldEvery, Default: EveryManual},
				"timeout": timeout("5m"),
			}},
			SourceSession: {MinEvery: "2s", Approval: true, Fields: map[string]Field{
				"target":  target,
				"session": {Type: FieldString, Required: true},
				"every":   {Type: FieldEvery, Required: true},
			}},
		},
		Formats: map[string]int{"widget": 1, "board": 1},
	}
}

// Grows says whether a view takes size auto in a stack: it is as tall as
// what it shows.
func Grows(view string) bool { return CurrentContract().Views[view].Grows }

// IsStack says whether a surface lays its widgets out as an ordered list.
func IsStack(surface string) bool {
	return CurrentContract().Surfaces[surface].Layout == LayoutStack
}
