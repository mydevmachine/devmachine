package widgets

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// BoardFormat is the board format this CLI reads and writes.
const BoardFormat = 1

// ErrBoardChanged is returned when the board on disk is not the one that was
// read, so writing would drop somebody else's change.
var ErrBoardChanged = errors.New("the board changed on disk since it was read; run the command again")

var (
	instanceID   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	instanceType = regexp.MustCompile(`^[a-z][a-z0-9_-]*/[a-z][a-z0-9-]*$`)
	yamlLine     = regexp.MustCompile(`line (\d+)`)
)

// Frame is where an instance is drawn, in points.
type Frame struct {
	X int `yaml:"x" json:"x"`
	Y int `yaml:"y" json:"y"`
	W int `yaml:"w" json:"w"`
	H int `yaml:"h" json:"h"`
}

// Instance is one widget placed on a board.
type Instance struct {
	ID        string         `yaml:"id" json:"id"`
	Type      string         `yaml:"type" json:"type"`
	With      map[string]any `yaml:"with" json:"with"`
	Frame     Frame          `yaml:"frame" json:"frame"`
	Size      string         `yaml:"size" json:"size"`
	Minimized bool           `yaml:"minimized" json:"minimized"`
	Z         int            `yaml:"z" json:"z"`

	// Inline is true when the entry carries its own source and view instead
	// of a type, and Line is where the entry starts in the file.
	Inline bool `yaml:"-" json:"-"`
	Line   int  `yaml:"-" json:"-"`
}

// Board is what <config>/boards/<surface>.yml holds.
type Board struct {
	Format  int        `yaml:"format"`
	Surface string     `yaml:"surface"`
	Widgets []Instance `yaml:"widgets"`
}

// Lookup finds a widget in the catalog by its full name.
type Lookup func(name string) (Entry, bool)

// BoardsDir is where the boards live.
func BoardsDir(configDir string) string { return filepath.Join(configDir, "boards") }

// BoardPath is the board file of one surface.
func BoardPath(configDir, surface string) string {
	return filepath.Join(BoardsDir(configDir), surface+".yml")
}

// NewBoard is an empty board for a surface.
func NewBoard(surface string) Board {
	return Board{Format: BoardFormat, Surface: surface, Widgets: []Instance{}}
}

// ParseBoard reads a board's text. A file that is not YAML, or not shaped
// like a board, is one problem pointing at its line.
func ParseBoard(path string, body []byte) (Board, []Problem) {
	fail := func(err error) (Board, []Problem) {
		p := Problem{Path: path, Message: fmt.Sprintf("parsing the board: %v", err)}
		if match := yamlLine.FindStringSubmatch(err.Error()); match != nil {
			p.Line, _ = strconv.Atoi(match[1])
		}
		return Board{}, []Problem{p}
	}
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return fail(err)
	}
	var b Board
	if err := root.Decode(&b); err != nil {
		return fail(err)
	}
	if entries := field(mapping(&root), "widgets"); entries != nil && entries.Kind == yaml.SequenceNode {
		for i, entry := range entries.Content {
			if i >= len(b.Widgets) {
				break
			}
			b.Widgets[i].Line = entry.Line
			b.Widgets[i].Inline = field(entry, "source") != nil || field(entry, "view") != nil
		}
	}
	return b, nil
}

// ValidateBoard returns everything wrong with b. A type the catalog does not
// know is not a problem: the app keeps the entry and shows a placeholder, so
// a missing package never loses a layout.
func ValidateBoard(b Board, path string, lookup Lookup) []Problem {
	c := CurrentContract()
	var problems []Problem
	at := func(line int, format string, args ...any) {
		problems = append(problems, Problem{Path: path, Line: line, Message: fmt.Sprintf(format, args...)})
	}

	if b.Format != BoardFormat {
		at(0, "board format %d, and this CLI reads board format %d", b.Format, BoardFormat)
		return problems
	}
	surface, ok := c.Surfaces[b.Surface]
	switch {
	case !ok:
		at(0, "surface %q: the surfaces are %s", b.Surface, strings.Join(surfaceNames(c), ", "))
	case surface.Status == StatusPlanned:
		at(0, "the %s area arrives in a later version", b.Surface)
	}
	if stem, isYAML := strings.CutSuffix(filepath.Base(path), ".yml"); isYAML && ok && stem != b.Surface {
		at(0, "surface is %q but the file is %s: a board is found by its file name", b.Surface, filepath.Base(path))
	}

	seen := map[string]bool{}
	for _, w := range b.Widgets {
		label := w.ID
		switch {
		case !instanceID.MatchString(w.ID):
			at(w.Line, "id %q: use lower case letters, digits and dashes", w.ID)
		case seen[w.ID]:
			at(w.Line, "id %q is used twice: every widget on a board has its own id", w.ID)
		}
		seen[w.ID] = true

		switch {
		case w.Type == "" && w.Inline:
			at(w.Line, "%s: a widget with its own source and view needs engine 1.1; this CLI implements engine %s", label, Engine)
			continue
		case w.Type == "":
			at(w.Line, "%s: every widget needs a type, written <package>/<widget>", label)
			continue
		case !instanceType.MatchString(w.Type):
			at(w.Line, "%s: type %q is written <package>/<widget>", label, w.Type)
			continue
		}

		if w.Frame.X < 0 || w.Frame.Y < 0 {
			at(w.Line, "%s: frame x and y cannot be negative", label)
		}
		if w.Size != SizeCustom {
			if _, ok := c.Presets[w.Size]; !ok {
				at(w.Line, "%s: size %q is a preset (%s) or custom", label, w.Size, strings.Join(presetNames(c), ", "))
			}
		}

		entry, known := lookup(w.Type)
		if !known {
			if w.Frame.W <= 0 || w.Frame.H <= 0 {
				at(w.Line, "%s: frame w and h must be above zero", label)
			}
			continue
		}
		minW, minH := MinFrame(entry.Sizes)
		if w.Frame.W < minW || w.Frame.H < minH {
			at(w.Line, "%s: frame %dx%d is smaller than %s's minimum of %dx%d", label, w.Frame.W, w.Frame.H, w.Type, minW, minH)
		}
		if _, preset := c.Presets[w.Size]; preset && !slices.Contains(entry.Sizes, w.Size) {
			at(w.Line, "%s: %s does not come in size %s: it takes %s", label, w.Type, w.Size, strings.Join(entry.Sizes, ", "))
		}
		for _, name := range sortedKeys(w.With) {
			input, ok := entry.Inputs[name]
			switch {
			case !ok:
				at(w.Line, "%s: %s has no input %q", label, w.Type, name)
			case !valueFits(input.Type, w.With[name]):
				at(w.Line, "%s: input %q is a %s, and %v is not", label, name, input.Type, w.With[name])
			}
		}
	}
	return problems
}

// ReadBoard reads the board at path and returns it with the bytes it was read
// from, which WriteBoard needs to know nobody changed it since.
func ReadBoard(path string) (Board, []byte, []Problem, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Board{}, nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	b, problems := ParseBoard(path, body)
	return b, body, problems, nil
}

// WriteBoard replaces the board at path with b, atomically. read is what the
// file held when it was read, nil when it did not exist; when the file holds
// something else now, nothing is written and ErrBoardChanged comes back.
func WriteBoard(path string, read []byte, b Board) error {
	current, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if read != nil {
			return ErrBoardChanged
		}
	case err != nil:
		return fmt.Errorf("reading %s: %w", path, err)
	case read == nil || !bytes.Equal(current, read):
		return ErrBoardChanged
	}

	body, err := EncodeBoard(b)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// EncodeBoard writes b in the key order the app also writes, so the two
// writers never fight over the same file's layout.
func EncodeBoard(b Board) ([]byte, error) {
	widgets := &yaml.Node{Kind: yaml.SequenceNode}
	for _, w := range b.Widgets {
		entry := &yaml.Node{Kind: yaml.MappingNode}
		put(entry, "id", scalarNode(w.ID))
		put(entry, "type", scalarNode(w.Type))
		if len(w.With) > 0 {
			with := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
			for _, name := range sortedKeys(w.With) {
				value := &yaml.Node{}
				if err := value.Encode(w.With[name]); err != nil {
					return nil, fmt.Errorf("writing input %s of %s: %w", name, w.ID, err)
				}
				put(with, name, value)
			}
			put(entry, "with", with)
		}
		frame := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
		put(frame, "x", intNode(w.Frame.X))
		put(frame, "y", intNode(w.Frame.Y))
		put(frame, "w", intNode(w.Frame.W))
		put(frame, "h", intNode(w.Frame.H))
		put(entry, "frame", frame)
		put(entry, "size", scalarNode(w.Size))
		put(entry, "minimized", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(w.Minimized)})
		put(entry, "z", intNode(w.Z))
		widgets.Content = append(widgets.Content, entry)
	}
	if len(widgets.Content) == 0 {
		widgets.Style = yaml.FlowStyle
	}

	doc := &yaml.Node{Kind: yaml.MappingNode}
	put(doc, "format", intNode(b.Format))
	put(doc, "surface", scalarNode(b.Surface))
	put(doc, "widgets", widgets)

	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("writing the board: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("writing the board: %w", err)
	}
	return buffer.Bytes(), nil
}

func put(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content, scalarNode(key), value)
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func intNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)}
}

func field(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
