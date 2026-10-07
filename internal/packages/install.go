package packages

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
}

// Stage fetches address at ref and checks what came: one package at the top
// of the repository, links that stay inside it, a name, and a package.yml
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
	temp, err := os.MkdirTemp(local, ".install-")
	if err != nil {
		return nil, fmt.Errorf("making a folder to fetch into: %w", err)
	}
	staged := &Staged{temp: temp}
	fail := func(err error) (*Staged, error) {
		staged.Discard()
		return nil, err
	}

	clone := filepath.Join(temp, "clone")
	commit, err := Clone(ctx, address, ref, clone)
	if err != nil {
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

// Discard removes whatever is left of the temporary folder. It is safe to
// call more than once, and after Install.
func (s *Staged) Discard() {
	if s.temp != "" {
		_ = os.RemoveAll(s.temp)
	}
}

// Install records the origin and moves the package into your packages,
// replacing the copy there for an update. The old copy is put back when the
// move fails, so a failed update never leaves you with neither.
func (s *Staged) Install(configDir string, now time.Time) (string, error) {
	s.Origin.InstalledAt = now.UTC().Format(time.RFC3339)
	if err := WriteOrigin(s.Dir, s.Origin); err != nil {
		return "", err
	}
	target := filepath.Join(LocalDir(configDir), s.Name)
	previous := filepath.Join(s.temp, "previous")
	hadPrevious := false
	if _, err := os.Lstat(target); err == nil {
		if err := os.Rename(target, previous); err != nil {
			return "", fmt.Errorf("moving the old %s aside: %w", s.Name, err)
		}
		hadPrevious = true
	}
	if err := os.Rename(s.Dir, target); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(previous, target); restoreErr != nil {
				return "", fmt.Errorf("moving %s into %s: %w (and putting the old copy back failed: %v)", s.Name, target, err, restoreErr)
			}
		}
		return "", fmt.Errorf("moving %s into %s: %w", s.Name, target, err)
	}
	return target, nil
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
