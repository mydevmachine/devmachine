// Package packages fetches, verifies, reads and checks recipes.
//
// A package is an Ansible role with one extra file beside it. Nothing is
// translated: what is written is what runs, so a failure points at the line
// somebody wrote rather than at generated YAML they have never seen.
package packages

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FileName is the one file a role needs beside it to be a package.
const FileName = "package.yml"

// The scopes a package can declare. Scope is a property of the software, not a
// preference: Docker is installed once and serves everyone, Claude Code has a
// login per person.
const (
	ScopeMachine   = "machine"
	ScopeWorkspace = "workspace"
)

// The kinds of credential, and the only hard one is the first.
const (
	// KindManual cannot be automated: a browser or a device code, and a
	// person. The name is about who does it, not about what it produces —
	// `tailscale up` authenticates a whole machine, and calling that a
	// "login" suggested a user session it has nothing to do with.
	KindManual = "manual"
	// KindSecret is a value somebody hands over once.
	KindSecret = "secret"
	// KindFile is a file somebody drops on the machine.
	KindFile = "file"
)

// The platforms a package can say it runs on. A machine, and so a workspace
// on it, can be either.
const (
	PlatformLinux = "linux"
	PlatformMacOS = "macos"
)

// KnownPlatforms is every value `platforms` accepts.
var KnownPlatforms = []string{PlatformLinux, PlatformMacOS}

// ReadableFormats are the shapes of package.yml this CLI can read. A set, not
// a number: adding a field is not a new format, so most releases add nothing
// here, and the ones that do keep reading the old shape for as long as it is
// worth doing.
var ReadableFormats = []int{1}

// Variable is a value a package reads, and what it falls back to.
type Variable struct {
	Summary string `yaml:"summary"`
	Default any    `yaml:"default"`
}

// Credential is something a package's tool cannot work without, and how it is
// obtained.
//
// How belongs here rather than in the operator's configuration, because the
// package is the only thing that knows it: nobody else knows that this tool
// logs in with one command and leaves its session in one file.
type Credential struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
	// Scope is what this package recommends, and the operator may override it.
	Scope string `yaml:"scope"`
	// Shareable says whether a copy of StoredAt works on another account. It
	// is a fact about the tool, not a preference: a session file copies, and a
	// token bound to a device or a browser does not. Which is which is found
	// by trying, so leaving it out means no.
	Shareable bool `yaml:"shareable"`
	// Command is what a person runs, for KindManual.
	Command string `yaml:"command"`
	// StoredAt is where the tool keeps the result, so doctor can look and a
	// later version can copy it. It is a claim, not a guarantee.
	StoredAt string `yaml:"stored_at"`
	// Env and Path are where a KindSecret or KindFile value is delivered.
	Env  string `yaml:"env"`
	Path string `yaml:"path"`
}

// Network makes a package the answer for a private network's host entries.
//
// An entry written `<prefix>:<name>` in a machine's hosts belongs to the
// package that declares that prefix. The core knows the prefix and the
// scripts, never the product behind them.
type Network struct {
	Prefix string `yaml:"prefix"`
	// Resolve runs on the person's computer: it gets the name as its one
	// argument and prints the addresses, one per line.
	Resolve string `yaml:"resolve"`
	// Join and SelfName run on the machine, as its admin: the first signs
	// it in, the second prints the name it answers to on the network.
	Join     string `yaml:"join"`
	SelfName string `yaml:"self_name"`
}

// SkillContribution names the package-relative directory whose direct
// children are Agent Skills.
type SkillContribution struct {
	Path string `yaml:"path"`
}

// Provider is one of a package's commands that a widget may read: the
// fields of the one JSON object it prints, and how often it may run at most.
type Provider struct {
	Returns  map[string]string `yaml:"returns"`
	MinEvery string            `yaml:"min_every"`
}

// Manifest is what package.yml holds.
type Manifest struct {
	// Format is the shape of this file, and it is required. Without it the
	// first change to the format would make every existing recipe fail in a
	// different way, none of them saying why.
	Format  int    `yaml:"format"`
	Name    string `yaml:"name"`
	Scope   string `yaml:"scope"`
	Summary string `yaml:"summary"`
	// Category groups the package with others like it on the packages page.
	// Nothing in the CLI reads it, so no value is refused.
	Category string `yaml:"category"`
	// Platforms are the operating systems the package runs on. Left out, it
	// runs on any of them. It is what lets a client stop offering a macOS
	// package for a Linux server before a sync finds out the hard way.
	Platforms []string `yaml:"platforms"`
	Requires  struct {
		CLI string `yaml:"cli"`
	} `yaml:"requires"`
	// Needs is ordering, declared. Never implied by the order of a list:
	// implied ordering is what made the original repository impossible to
	// reason about.
	Needs []string `yaml:"needs"`
	// Provides names places other packages may write into.
	Provides map[string]string `yaml:"provides"`
	// Extends is a contribution to a place another package said may be
	// written to. It is not a patch, and it cannot reach anywhere else.
	Extends       map[string]string   `yaml:"extends"`
	Variables     map[string]Variable `yaml:"variables"`
	Credentials   []Credential        `yaml:"credentials"`
	RequiresFiles []string            `yaml:"requires_files"`
	Skills        *SkillContribution  `yaml:"skills"`
	// Widgets is a package-relative folder whose direct children holding a
	// widget.yml are widgets for the app.
	Widgets string   `yaml:"widgets"`
	Network *Network `yaml:"network"`

	// Kind, Entrypoint and Commands make a package callable: the contract it
	// answers, the executable to call on the machine, and what that executable
	// accepts.
	//
	// Commands is a list, or the single entry "*" for anything. `devmachine
	// run --package` is the door to them, and refuses anything else.
	Kind       string   `yaml:"kind"`
	Entrypoint string   `yaml:"entrypoint"`
	Commands   []string `yaml:"commands"`

	// Providers are the commands a widget may read, each printing one JSON
	// object. The key is the command; a widget names it <package>/<command>.
	Providers map[string]Provider `yaml:"providers"`

	// Bootstrap is a POSIX sh script that brings a machine to the point where
	// Ansible can run on it, for a system that has no package manager the CLI
	// can drive by itself. It belongs to the package that installs that
	// manager, so the CLI holds nothing system-specific beyond running it.
	Bootstrap string `yaml:"bootstrap"`

	// Path is the directory the manifest was read from, and Lines maps a
	// top-level field name, or a dotted path under providers, onto the line
	// it was written on.
	Path  string         `yaml:"-"`
	Lines map[string]int `yaml:"-"`
}

// ManifestPath is where a package's manifest lives.
func ManifestPath(dir string) string { return filepath.Join(dir, FileName) }

// ParseManifest reads the package.yml in dir.
func ParseManifest(dir string) (Manifest, error) {
	var m Manifest
	path := ManifestPath(dir)

	body, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("reading %s: %w", path, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return m, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := root.Decode(&m); err != nil {
		return m, fmt.Errorf("parsing %s: %w", path, err)
	}

	m.Path = dir
	m.Lines = topLevelLines(&root)
	return m, nil
}

// topLevelLines maps each top-level key onto the line it was written on, and
// every key under providers onto its own, so a validation message can point
// at it.
func topLevelLines(root *yaml.Node) map[string]int {
	lines := map[string]int{}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return lines
	}
	pairs := root.Content[0].Content
	for i := 0; i+1 < len(pairs); i += 2 {
		lines[pairs[i].Value] = pairs[i].Line
		if pairs[i].Value == "providers" {
			nestedLines(lines, "providers", pairs[i+1])
		}
	}
	return lines
}

func nestedLines(lines map[string]int, prefix string, node *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := prefix + "." + node.Content[i].Value
		lines[key] = node.Content[i].Line
		nestedLines(lines, key, node.Content[i+1])
	}
}

// FirstCLIWithWidgets is the first CLI version that reads `widgets:`.
const FirstCLIWithWidgets = "0.9.0"

// WidgetsDir is the folder a package's widgets live in, and false when the
// package declares none.
func WidgetsDir(m Manifest) (string, bool) {
	if m.Widgets == "" {
		return "", false
	}
	return filepath.Join(m.Path, m.Widgets), true
}
