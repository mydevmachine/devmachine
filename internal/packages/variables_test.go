package packages

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoList is a list variable shaped like a workspace's repositories, the case
// that made typed variables worth having.
func repoList(t *testing.T) Variable {
	t.Helper()
	var v Variable
	err := yaml.Unmarshal([]byte(`summary: Repositories to clone.
type: list
default: []
items:
  fields:
    name: {summary: The folder., required: true}
    url: {summary: Where it is cloned from., required: true}
    branch: {summary: The branch to check out.}
    shallow: {summary: Clone only the last commit., type: boolean}
`), &v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func settingValue(t *testing.T, text string) any {
	t.Helper()
	var value any
	if err := yaml.Unmarshal([]byte(text), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCheckAcceptsAListWhoseEntriesHaveEveryRequiredField(t *testing.T) {
	value := settingValue(t, `[{name: app, url: git@example.com:alice/app.git}, {name: api, url: https://example.com/api.git, branch: dev, shallow: true}]`)
	if err := repoList(t).Check(value); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRefusesAnEntryMissingARequiredField(t *testing.T) {
	value := settingValue(t, `[{name: app, url: git@example.com:alice/app.git}, {name: api}]`)
	err := repoList(t).Check(value)
	if err == nil || err.Error() != `[1]: "url" is required` {
		t.Fatalf("got %v", err)
	}
}

func TestCheckRefusesAFieldTheListDoesNotHaveAndNamesTheOnesItDoes(t *testing.T) {
	value := settingValue(t, `[{title: app, url: git@example.com:alice/app.git}]`)
	err := repoList(t).Check(value)
	if err == nil {
		t.Fatal("a mistyped field was accepted")
	}
	for _, want := range []string{`[0]: "title" is not a field of this list`, "branch, name, shallow, url"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%q does not say %q", err, want)
		}
	}
}

func TestCheckRefusesAFieldOfTheWrongType(t *testing.T) {
	value := settingValue(t, `[{name: app, url: git@example.com:alice/app.git, shallow: "yes please"}]`)
	err := repoList(t).Check(value)
	if err == nil || err.Error() != `[0].shallow: a boolean, got the string "yes please"` {
		t.Fatalf("got %v", err)
	}
}

func TestCheckRefusesAnEntryThatIsNotAMapping(t *testing.T) {
	err := repoList(t).Check(settingValue(t, `[git@example.com:alice/app.git]`))
	if err == nil || !strings.Contains(err.Error(), `[0]: each entry is a mapping of branch, name, shallow, url, got the string "git@example.com:alice/app.git"`) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckRefusesAValueOfTheWrongType(t *testing.T) {
	cases := []struct {
		kind, value, want string
	}{
		{"list", `app`, `a list, got the string "app"`},
		{"boolean", `"true"`, `a boolean, got the string "true"`},
		{"number", `[1]`, "a number, got a list"},
		{"string", `{a: 1}`, "a string, got a map"},
		{"map", `3`, "a map, got the number 3"},
	}
	for _, c := range cases {
		err := Variable{Type: c.kind}.Check(settingValue(t, c.value))
		if err == nil || err.Error() != c.want {
			t.Errorf("%s with %s: got %v, want %q", c.kind, c.value, err, c.want)
		}
	}
}

func TestCheckAcceptsEveryTypeItsOwnShape(t *testing.T) {
	cases := map[string]string{
		"string": `hello`, "boolean": `false`, "number": `8080`, "list": `[a, b]`, "map": `{A: "1"}`,
	}
	for kind, value := range cases {
		if err := (Variable{Type: kind}).Check(settingValue(t, value)); err != nil {
			t.Errorf("%s with %s: %v", kind, value, err)
		}
	}
	if err := (Variable{Type: "number"}).Check(1.5); err != nil {
		t.Errorf("a float: %v", err)
	}
}

// A variable written before types existed has none, and every value it was
// ever given keeps working.
func TestCheckAcceptsAnythingForAnUntypedVariable(t *testing.T) {
	for _, value := range []any{"a", 3, true, []any{"x"}, map[string]any{"k": "v"}} {
		if err := (Variable{Summary: "old"}).Check(value); err != nil {
			t.Fatalf("%v: %v", value, err)
		}
	}
}

func TestValidateAcceptsATypedListVariable(t *testing.T) {
	dir := writePackage(t, "repos", `format: 1
name: repos
scope: workspace
summary: Clones repositories.
variables:
  list:
    summary: What to clone.
    type: list
    default: [{name: app, url: https://example.com/app.git}]
    items:
      fields:
        name: {summary: The folder., required: true}
        url: {summary: The source., required: true}
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")

	problems, err := Validate(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("%v %#v", err, problems)
	}
}

func TestValidateReportsEveryMalformedVariable(t *testing.T) {
	dir := writePackage(t, "repos", `format: 1
name: repos
scope: workspace
summary: Clones repositories.
variables:
  colour:
    summary: A colour.
    type: text
  port:
    summary: A port.
    type: number
    default: "eighty"
  hosts:
    summary: Hosts.
    type: map
    items:
      fields:
        name: {summary: A name.}
  empty:
    summary: Nothing in it.
    type: list
    items:
      fields: {}
  odd:
    summary: An odd field.
    type: list
    items:
      fields:
        size: {summary: A size., type: list}
  wrong_default:
    summary: A default missing a field.
    type: list
    default: [{name: app}]
    items:
      fields:
        name: {summary: The folder., required: true}
        url: {summary: The source., required: true}
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, `variable "colour": type "text"`)
	problemAbout(t, problems, `variable "port": the default is not its own type: a number, got the string "eighty"`)
	problemAbout(t, problems, `variable "hosts": items describes the entries of a list, and this is a map`)
	problemAbout(t, problems, `variable "empty": items.fields is empty`)
	problemAbout(t, problems, `variable "odd": field "size" has type "list"`)
	problemAbout(t, problems, `variable "wrong_default": the default is not its own type: [0]: "url" is required`)
	if len(problems) != 6 {
		t.Fatalf("want 6 problems, got %#v", problems)
	}
}
