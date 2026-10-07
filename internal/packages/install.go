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
