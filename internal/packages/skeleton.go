package packages

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// SchemaField is one field of package.yml, as the CLI publishes it.
type SchemaField struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Summary  string `json:"summary"`
}

// SchemaDoc is the whole format, answered by `packages schema`.
//
// It is published by the binary rather than by a page, because a page drifts
// the moment the format changes and the validator cannot.
type SchemaDoc struct {
	Formats []int         `json:"formats"`
	Fields  []SchemaField `json:"fields"`
}

// schemaFields is the one table. A test asserts every yaml tag on Manifest
// appears here, so a new field cannot arrive undocumented.
var schemaFields = []SchemaField{
	{"format", true, "The shape of this file. This CLI reads format 1."},
	{"name", true, "The package's name, which has to be the directory it lives in."},
	{"scope", true, `Where it is installed: "machine" or "workspace".`},
	{"summary", true, "One line saying what it installs. It is what `packages list` prints."},
	{"category", false, `A word or two grouping it with packages like it, such as "Security" or "DNS". The packages page filters by it.`},
	{"platforms", false, `The operating systems it runs on: "linux", "macos", or both. Left out, any.`},
	{"requires", false, `Which CLI can run it, written as requires.cli: ">= 0.2.0".`},
	{"needs", false, "Packages that have to run before this one. It is the only thing that decides order."},
	{"provides", false, "Places other packages may write into, as <place>: <absolute path on the machine>."},
	{"extends", false, "Contributions to another package's place, as <package>.<place>: <path inside this package>."},
	{"variables", false, `Values this package reads, each with a summary, a default and optionally a type: "string", "boolean", "number", "list" or "map". A list of mappings describes its entries with items.fields.`},
	{"credentials", false, "What its tool cannot work without, and how each one is obtained."},
	{"requires_files", false, "Files that have to be on the machine before it runs."},
	{"skills", false, "A package-relative directory whose direct children are Agent Skills."},
	{"widgets", false, "A package-relative folder whose direct children holding a widget.yml are widgets for the app."},
	{"kind", false, `The contract an entrypoint answers. The only one so far is "dns".`},
	{"entrypoint", false, "An executable in the package the CLI can call on the machine."},
	{"commands", false, `What the entrypoint accepts: a list, or ["*"] for anything.`},
	{"providers", false, "Commands of the entrypoint a widget may read, as <command>: {returns: {<field>: <type>}, min_every: <duration>}."},
	{"network", false, "A private network this machine package answers for: prefix, resolve, and optionally join and self_name."},
	{"bootstrap", false, "A POSIX sh script in a machine package that prepares the machine for Ansible, such as a Mac package manager. Path inside the package, executable."},
}

// Schema returns the format this CLI reads.
func Schema() SchemaDoc {
	return SchemaDoc{Formats: ReadableFormats, Fields: schemaFields}
}

// skeletonManifest and the two files beside it are a package that already
// validates and already does something. A skeleton full of "add tasks here"
// teaches the wrong thing on the first run.
const skeletonManifest = `format: 1
name: {{.Name}}
scope: {{.Scope}}
summary: Replace this line with what {{.Name}} installs.
`

const skeletonTasks = `---
- name: Install what {{.Name}} needs
  package:
    name: "{{"{{"}} {{.Var}}_packages {{"}}"}}"
    state: present
  when: {{.Var}}_packages | length > 0
`

const skeletonDefaults = `---
{{.Var}}_packages: []
`

// skeletonDNSManifest is what `packages new --kind dns` writes instead of
// skeletonManifest: a package that already declares the contract described in
// docs/reference/dns-provider-contract.md, so `packages validate` passes
// before a single line of the provider is edited.
const skeletonDNSManifest = `format: 1
name: {{.Name}}
scope: {{.Scope}}
summary: Replace this line with what {{.Name}} installs.
kind: dns
entrypoint: bin/provider
commands: [zones, list, upsert, delete, help]
`

// skeletonDNSProvider already answers docs/reference/dns-provider-contract.md:
// `list` returns an empty set rather than failing, and `upsert`/`delete`
// report a clear "not implemented yet" in the shape the contract defines,
// rather than a file of comments nobody runs.
const skeletonDNSProvider = `#!/usr/bin/env python3
import json
import sys


def fail(kind, message):
    print(json.dumps({"error": {"kind": kind, "message": message}}))
    sys.exit(1)


COMMANDS = [
    {"name": "zones", "summary": "The zones this token can see."},
    {"name": "list", "summary": "Every record in a zone.", "args": "<zone>"},
    {"name": "upsert", "summary": "Make a name hold exactly one value.", "args": "<zone>"},
    {"name": "delete", "summary": "Remove one value from a name.", "args": "<zone>"},
    {"name": "help", "summary": "This list."},
]


def main(argv):
    if len(argv) < 2:
        fail("invalid_record", "usage: provider zones|list|upsert|delete|help [<zone>]")

    command = argv[1]

    if command == "zones":
        print(json.dumps({"zones": []}))
        return

    if command == "help":
        print(json.dumps({"commands": COMMANDS}))
        return

    if len(argv) < 3:
        fail("invalid_record", "usage: provider " + command + " <zone>")

    if command == "list":
        print(json.dumps({"records": []}))
        return

    if command in ("upsert", "delete"):
        fail("invalid_record", command + " is not implemented yet: edit bin/provider")

    fail("invalid_record", "unknown command " + repr(command))


if __name__ == "__main__":
    main(sys.argv)
`

// WriteSkeleton writes a new package into dir.
//
// It refuses to write over one that is already there: `packages new` is how a
// package starts, never how one is edited. kind is empty for an ordinary
// package, or "dns" for one that already answers the DNS provider contract.
func WriteSkeleton(dir, name, scope, kind string) error {
	if !lowerName.MatchString(name) {
		return fmt.Errorf("name %q: use lower case letters, digits, dashes and underscores", name)
	}
	if scope != ScopeMachine && scope != ScopeWorkspace {
		return fmt.Errorf("scope %q: a package is installed on a %s or in a %s, and there is no third place",
			scope, ScopeMachine, ScopeWorkspace)
	}
	if kind != "" && kind != kindDNS {
		return fmt.Errorf("kind %q: the only kind so far is %q", kind, kindDNS)
	}
	if _, err := os.Stat(ManifestPath(dir)); err == nil {
		return fmt.Errorf("%s already exists", ManifestPath(dir))
	}

	manifest := skeletonManifest
	if kind == kindDNS {
		manifest = skeletonDNSManifest
	}

	data := struct{ Name, Scope, Var string }{name, scope, ansibleVariable(name)}
	for path, body := range map[string]string{
		FileName:                              manifest,
		filepath.Join("tasks", "main.yml"):    skeletonTasks,
		filepath.Join("defaults", "main.yml"): skeletonDefaults,
	} {
		rendered, err := render(body, data)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(rendered), 0o644); err != nil {
			return err
		}
	}

	if kind == kindDNS {
		target := filepath.Join(dir, "bin", "provider")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(skeletonDNSProvider), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func render(body string, data any) (string, error) {
	parsed, err := template.New("skeleton").Parse(body)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := parsed.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

// ansibleVariable turns a package name into something Ansible accepts: a dash
// is fine in a name and never fine in a variable.
func ansibleVariable(name string) string { return strings.ReplaceAll(name, "-", "_") }
