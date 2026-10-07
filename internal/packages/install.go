package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/widgets"
)

// Summary is what a package brings, as `packages install` shows it before
// asking: its widgets and whether each runs code, the commands its
// entrypoint answers, every executable file, every task file, and the
// credentials it asks for. Its tasks run as root on every machine it is
// added to, so nothing here is left out.
type Summary struct {
	Name        string          `json:"name"`
	Scope       string          `json:"scope"`
	Summary     string          `json:"summary"`
	Needs       []string        `json:"needs"`
	Widgets     []SummaryWidget `json:"widgets"`
	Commands    []string        `json:"commands"`
	Providers   []string        `json:"providers"`
	Scripts     []string        `json:"scripts"`
	Tasks       []string        `json:"tasks"`
	Credentials []string        `json:"credentials"`
}

// SummaryWidget is one widget a package brings: its full name, what feeds
// it, and whether it runs code, which makes it ask before it runs.
type SummaryWidget struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	RunsCode bool   `json:"runs_code"`
}

// Summarize reads what the package in dir brings.
func Summarize(dir string) (Summary, error) {
	m, err := ParseManifest(dir)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Name: m.Name, Scope: m.Scope, Summary: strings.TrimSpace(m.Summary), Needs: nonNil(m.Needs),
		Widgets: []SummaryWidget{}, Commands: nonNil(m.Commands), Providers: sortedKeys(m.Providers),
		Scripts: []string{}, Tasks: []string{}, Credentials: []string{}}
	if root, ok := WidgetsDir(m); ok {
		found, _ := widgets.LoadAll(WidgetOwner(m), root)
		for _, w := range found {
			source := w.Source.Kind
			if w.Source.Kind == widgets.SourceProvider {
				source += " " + w.Source.Name
			}
			s.Widgets = append(s.Widgets, SummaryWidget{Name: m.Name + "/" + w.Name, Source: source, RunsCode: widgets.RunsCode(w.Source)})
		}
	}
	for _, c := range m.Credentials {
		s.Credentials = append(s.Credentials, fmt.Sprintf("%s (%s)", c.Name, c.Kind))
	}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if first, _, _ := strings.Cut(rel, "/"); first == "tasks" || first == "handlers" {
			s.Tasks = append(s.Tasks, rel)
		}
		if info, err := entry.Info(); err == nil && entry.Type().IsRegular() && info.Mode()&0o111 != 0 {
			s.Scripts = append(s.Scripts, rel)
		}
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("reading what %s brings: %w", dir, err)
	}
	return s, nil
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// InvalidPackageError is a fetched package that does not validate, with
// every problem at once.
type InvalidPackageError struct {
	Where    string
	Problems []Problem
}

func (e *InvalidPackageError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = "  " + p.Error()
	}
	return fmt.Sprintf("the package at %s has %d problem(s):\n%s", e.Where, len(e.Problems), strings.Join(lines, "\n"))
}

// Staged is a package fetched from a git address into a temporary folder
// beside your own packages, checked, and not installed yet.
type Staged struct {
	Dir     string
	Name    string
	Origin  Origin
	Summary Summary

	temp string
	keep bool
}

// tempPrefix starts every folder Stage fetches into. A dot keeps it out of
// ValidName, so it never collides with a package.
const tempPrefix = ".install-"

// staleAfter is how old a fetch folder must be before Stage sweeps it as
// left behind by an interrupted command; a younger one may be another
// command's fetch in progress.
const staleAfter = time.Hour

// rename is the seam a test replaces to make a move fail.
var rename = os.Rename

// Stage fetches address at ref and checks what came: links that stay inside
// it, one package at the top of the repository, a name, and a package.yml
// that validates. The temporary folder sits inside your packages folder so
// Install is one rename; its name starts with a dot and holds no package.yml
// at its top, so nothing reads it as a package.
func Stage(ctx context.Context, configDir, address, ref string) (*Staged, error) {
	where := address
	if ref != "" {
		where += "@" + ref
	}
	local := LocalDir(configDir)
	if err := os.MkdirAll(local, 0o755); err != nil {
		return nil, fmt.Errorf("making %s: %w", local, err)
	}
	sweepStale(local, time.Now())
	temp, err := os.MkdirTemp(local, tempPrefix)
	if err != nil {
		return nil, fmt.Errorf("making a folder to fetch into: %w", err)
	}
	staged := &Staged{temp: temp}
	fail := func(err error) (*Staged, error) {
		staged.Discard()
		return nil, err
	}

	clone := filepath.Join(temp, ".clone")
	commit, err := Clone(ctx, address, ref, clone)
	if err != nil {
		return fail(err)
	}
	// Before anything reads a file: package.yml itself may be a link to
	// /dev/zero, a pipe or a file on this computer.
	if err := linksStayInside(clone); err != nil {
		return fail(err)
	}
	if _, err := os.Stat(ManifestPath(clone)); err != nil {
		return fail(fmt.Errorf("%s has no package.yml at its top: a package from a git address is one package, with package.yml beside tasks/", where))
	}
	m, err := ParseManifest(clone)
	if err != nil {
		return fail(err)
	}
	if !ValidName(m.Name) {
		return fail(fmt.Errorf("the package at %s is named %q: a package's name is lower case letters, digits, dashes and underscores", where, m.Name))
	}
	dir := filepath.Join(temp, m.Name)
	if err := os.Rename(clone, dir); err != nil {
		return fail(fmt.Errorf("naming the fetched package %s: %w", m.Name, err))
	}
	if err := linksStayInside(dir); err != nil {
		return fail(err)
	}
	problems, err := Validate(dir)
	if err != nil {
		return fail(err)
	}
	if len(problems) > 0 {
		return fail(&InvalidPackageError{Where: where, Problems: problems})
	}
	summary, err := Summarize(dir)
	if err != nil {
		return fail(err)
	}
	staged.Dir, staged.Name, staged.Summary = dir, m.Name, summary
	staged.Origin = Origin{URL: address, Ref: ref, Commit: commit}
	return staged, nil
}

// sweepStale removes fetch folders an interrupted command left behind. It
// keeps one holding an old copy that could not be put back.
func sweepStale(local string, now time.Time) {
	entries, err := os.ReadDir(local)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), tempPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < staleAfter {
			continue
		}
		path := filepath.Join(local, e.Name())
		if _, err := os.Lstat(filepath.Join(path, ".previous")); err == nil {
			continue
		}
		_ = os.RemoveAll(path)
	}
}

// Touch marks the temporary folder as in use, right after a person
// answered: a prompt left open past staleAfter would otherwise let another
// command's Stage sweep it before Install. It fails when one already has.
func (s *Staged) Touch(now time.Time) error {
	if _, err := os.Lstat(s.Dir); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the fetched copy of %s was swept away while waiting for your answer (another command clears a fetch older "+
			"than %s): run the command again", s.Name, staleAfter)
	}
	if err := os.Chtimes(s.temp, now, now); err != nil {
		return fmt.Errorf("marking the fetched copy of %s in use: %w", s.Name, err)
	}
	return nil
}

// Discard removes whatever is left of the temporary folder. It is safe to
// call more than once, and after Install. It keeps the folder when it holds
// an old copy that could not be put back.
func (s *Staged) Discard() {
	if s.temp != "" && !s.keep {
		_ = os.RemoveAll(s.temp)
	}
}

// Install records the origin and moves the package into your packages. It
// refuses when a folder of that name appeared there since Stage.
func (s *Staged) Install(configDir string, now time.Time) (string, error) {
	target := filepath.Join(LocalDir(configDir), s.Name)
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("%s appeared while %s was being fetched: nothing was written", target, s.Name)
	}
	if err := s.writeOrigin(now); err != nil {
		return "", err
	}
	if err := rename(s.Dir, target); err != nil {
		return "", fmt.Errorf("moving %s into %s: %w", s.Name, target, err)
	}
	return target, nil
}

// Replace records the origin and swaps the package in for the copy in your
// packages. The old copy is put back when the move fails, so a failed update
// never leaves you with neither; when even that fails, it stays where it was
// moved and the error says where.
func (s *Staged) Replace(configDir string, now time.Time) (string, error) {
	if err := s.writeOrigin(now); err != nil {
		return "", err
	}
	target := filepath.Join(LocalDir(configDir), s.Name)
	previous := filepath.Join(s.temp, ".previous")
	hadPrevious := false
	if _, err := os.Lstat(target); err == nil {
		if err := rename(target, previous); err != nil {
			return "", fmt.Errorf("moving the old %s aside: %w", s.Name, err)
		}
		hadPrevious = true
	}
	if err := rename(s.Dir, target); err != nil {
		if hadPrevious {
			if restoreErr := rename(previous, target); restoreErr != nil {
				s.keep = true
				return "", fmt.Errorf("moving %s into %s: %w (and putting the old copy back failed: %v; it is in %s)",
					s.Name, target, err, restoreErr, previous)
			}
		}
		return "", fmt.Errorf("moving %s into %s: %w", s.Name, target, err)
	}
	return target, nil
}

func (s *Staged) writeOrigin(now time.Time) error {
	s.Origin.InstalledAt = now.UTC().Format(time.RFC3339)
	return WriteOrigin(s.Dir, s.Origin)
}

// linksStayInside refuses a link that leads out of the package: sync would
// copy whatever it points at, a key on this computer included, to every
// machine the package is added to.
func linksStayInside(dir string) error {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.Type()&fs.ModeSymlink == 0 {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("%s is a link to nothing: a package from a git address holds its own files", rel)
		}
		inside, err := filepath.Rel(root, target)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s is a link leading outside the package: a package from a git address holds its own files", rel)
		}
		return nil
	})
}

// Changes is what an update adds, removes and changes in what a package
// brings. A script or task changes when its bytes do; a credential when
// anything package.yml says about it does.
type Changes struct {
	WidgetsAdded       []string `json:"widgets_added"`
	WidgetsRemoved     []string `json:"widgets_removed"`
	CommandsAdded      []string `json:"commands_added"`
	CommandsRemoved    []string `json:"commands_removed"`
	ProvidersAdded     []string `json:"providers_added"`
	ProvidersRemoved   []string `json:"providers_removed"`
	CredentialsAdded   []string `json:"credentials_added"`
	CredentialsRemoved []string `json:"credentials_removed"`
	CredentialsChanged []string `json:"credentials_changed"`
	ScriptsAdded       []string `json:"scripts_added"`
	ScriptsRemoved     []string `json:"scripts_removed"`
	ScriptsChanged     []string `json:"scripts_changed"`
	TasksAdded         []string `json:"tasks_added"`
	TasksRemoved       []string `json:"tasks_removed"`
	TasksChanged       []string `json:"tasks_changed"`
}

// Empty says the update changes none of the package's widgets, commands,
// providers, credentials, scripts or tasks.
func (c Changes) Empty() bool {
	for _, list := range [][]string{c.WidgetsAdded, c.WidgetsRemoved, c.CommandsAdded, c.CommandsRemoved,
		c.ProvidersAdded, c.ProvidersRemoved, c.CredentialsAdded, c.CredentialsRemoved, c.CredentialsChanged,
		c.ScriptsAdded, c.ScriptsRemoved, c.ScriptsChanged, c.TasksAdded, c.TasksRemoved, c.TasksChanged} {
		if len(list) > 0 {
			return false
		}
	}
	return true
}

// Compare names what the package in afterDir brings that the one in
// beforeDir lacks, the reverse, and what both bring but differs.
func Compare(beforeDir, afterDir string) (Changes, error) {
	before, err := Summarize(beforeDir)
	if err != nil {
		return Changes{}, err
	}
	after, err := Summarize(afterDir)
	if err != nil {
		return Changes{}, err
	}
	beforeManifest, err := ParseManifest(beforeDir)
	if err != nil {
		return Changes{}, err
	}
	afterManifest, err := ParseManifest(afterDir)
	if err != nil {
		return Changes{}, err
	}
	names := func(ws []SummaryWidget) []string {
		out := make([]string, len(ws))
		for i, w := range ws {
			out[i] = w.Name
		}
		return out
	}
	var c Changes
	c.WidgetsAdded, c.WidgetsRemoved = difference(names(before.Widgets), names(after.Widgets))
	c.CommandsAdded, c.CommandsRemoved = difference(before.Commands, after.Commands)
	c.ProvidersAdded, c.ProvidersRemoved = difference(before.Providers, after.Providers)
	c.CredentialsAdded, c.CredentialsRemoved, c.CredentialsChanged = compareCredentials(beforeManifest.Credentials, afterManifest.Credentials)
	c.ScriptsAdded, c.ScriptsRemoved = difference(before.Scripts, after.Scripts)
	c.ScriptsChanged = changedFiles(beforeDir, afterDir, before.Scripts, after.Scripts)
	c.TasksAdded, c.TasksRemoved = difference(before.Tasks, after.Tasks)
	c.TasksChanged = changedFiles(beforeDir, afterDir, before.Tasks, after.Tasks)
	return c, nil
}

func compareCredentials(before, after []Credential) (added, removed, changed []string) {
	byName := func(list []Credential) ([]string, map[string]Credential) {
		names, out := make([]string, len(list)), make(map[string]Credential, len(list))
		for i, cr := range list {
			names[i], out[cr.Name] = cr.Name, cr
		}
		return names, out
	}
	beforeNames, beforeByName := byName(before)
	afterNames, afterByName := byName(after)
	added, removed = difference(beforeNames, afterNames)
	changed = []string{}
	for _, name := range afterNames {
		if old, ok := beforeByName[name]; ok && old != afterByName[name] {
			changed = append(changed, name)
		}
	}
	return added, removed, changed
}

// changedFiles names the files in both lists whose bytes differ between the
// two folders. One that cannot be read on either side counts as changed.
func changedFiles(beforeDir, afterDir string, before, after []string) []string {
	out := []string{}
	for _, rel := range after {
		if !slices.Contains(before, rel) {
			continue
		}
		old, errOld := os.ReadFile(filepath.Join(beforeDir, filepath.FromSlash(rel)))
		updated, errNew := os.ReadFile(filepath.Join(afterDir, filepath.FromSlash(rel)))
		if errOld != nil || errNew != nil || !bytes.Equal(old, updated) {
			out = append(out, rel)
		}
	}
	return out
}

func difference(before, after []string) (added, removed []string) {
	added, removed = []string{}, []string{}
	for _, name := range after {
		if !slices.Contains(before, name) {
			added = append(added, name)
		}
	}
	for _, name := range before {
		if !slices.Contains(after, name) {
			removed = append(removed, name)
		}
	}
	return added, removed
}

// Uninstall deletes a package installed from a git address and returns the
// folder it was in. It refuses a package without an origin: a package you
// wrote yourself is never deleted by a command.
func Uninstall(configDir, name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("%q is not a package name", name)
	}
	dir := filepath.Join(LocalDir(configDir), name)
	if _, err := os.Stat(ManifestPath(dir)); err != nil {
		return "", fmt.Errorf("no package %s in %s", name, LocalDir(configDir))
	}
	_, found, err := ReadOrigin(dir)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%s is your own package, not one installed from a git address: packages remove never deletes "+
			"your own; delete %s yourself if you mean it", name, dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("deleting %s: %w", dir, err)
	}
	return dir, nil
}
