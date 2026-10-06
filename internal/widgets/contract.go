// Package widgets reads, checks and writes what the app's widget engine draws:
// the engine contract, widget.yml files shipped in packages, and boards.
package widgets

// Engine is the version of the contract this CLI implements.
const Engine = "1.0"

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

// SourceProvider is the only source kind engine 1.0 knows.
const SourceProvider = "provider"

// SizeCustom is the size a board entry has after a free resize.
const SizeCustom = "custom"

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

// Provider is one data source the app runs.
type Provider struct {
	Args     map[string]Arg    `json:"args"`
	MinEvery string            `json:"min_every"`
	Returns  map[string]string `json:"returns"`
}

// View is one way the app draws a provider's output.
type View struct {
	Accepts []string `json:"accepts"`
}

// Contract is everything a widget may name, at one engine version.
type Contract struct {
	Engine       string                       `json:"engine"`
	Layouts      []string                     `json:"layouts"`
	Unit         int                          `json:"unit"`
	Snap         int                          `json:"snap"`
	Presets      map[string][2]int            `json:"presets"`
	Surfaces     map[string]Surface           `json:"surfaces"`
	ContextTypes map[string]map[string]string `json:"context_types"`
	Providers    map[string]Provider          `json:"providers"`
	Views        map[string]View              `json:"views"`
	SourceKinds  []string                     `json:"source_kinds"`
	Formats      map[string]int               `json:"formats"`
}

// CurrentContract returns engine 1.0, built fresh on every call so no caller
// can change what another one reads.
func CurrentContract() Contract {
	appProvider := func(args map[string]Arg, returns map[string]string) Provider {
		return Provider{Args: args, MinEvery: "5s", Returns: returns}
	}
	return Contract{
		Engine:  Engine,
		Layouts: []string{LayoutCanvas, LayoutStack, LayoutSlot},
		Unit:    80,
		Snap:    8,
		Presets: map[string][2]int{
			"small": {2, 2}, "medium": {4, 2}, "large": {4, 4}, "wide": {8, 2}, "tall": {2, 4},
		},
		Surfaces: map[string]Surface{
			"home": {Layout: LayoutCanvas, Status: StatusAvailable, Context: map[string]ContextKey{}},
			"sidebar": {Layout: LayoutStack, Status: StatusPlanned, Context: map[string]ContextKey{
				"selected": {Type: "workspace", Presence: PresenceOptional},
			}},
			"context-sidebar": {Layout: LayoutStack, Status: StatusPlanned, Context: map[string]ContextKey{
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
		},
		Views: map[string]View{
			"app.clock":         {Accepts: []string{"app/clock"}},
			"app.summary":       {Accepts: []string{"app/summary"}},
			"app.machines":      {Accepts: []string{"app/machines"}},
			"app.harness-usage": {Accepts: []string{"app/harness-usage"}},
		},
		SourceKinds: []string{SourceProvider},
		Formats:     map[string]int{"widget": 1, "board": 1},
	}
}
