package commands

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/widgets"
	"github.com/spf13/cobra"
)

type boardChange struct {
	Board   string            `json:"board"`
	Path    string            `json:"path"`
	Widget  *widgets.Instance `json:"widget,omitempty"`
	Removed string            `json:"removed,omitempty"`
}

func newWidgetsAddCmd(opts *options) *cobra.Command {
	var board, id, size, at string
	var sets []string
	c := &cobra.Command{
		Use:   "add <package/widget>",
		Short: "Place a widget on a board",
		Long: "Without --at it takes the first free spot, scanning rows of 8pt from " +
			"the top left. The board is re-read first and written atomically; a board " +
			"with a problem is refused, never rewritten.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			catalog, dir, err := loadCatalog(cmd.Context(), opts)
			if err != nil {
				return err
			}
			entry, ok := catalog.Find(args[0])
			if !ok {
				return fmt.Errorf("no widget named %q: `devmachine widgets list` shows every widget there is", args[0])
			}
			if !entry.Available {
				return fmt.Errorf("%s is not available yet: %s", entry.Name, entry.UnavailableReason)
			}
			if err := checkBoardSurface(board); err != nil {
				return err
			}
			if !slices.Contains(entry.Surfaces, board) {
				return fmt.Errorf("%s does not fit the %s area: it fits %s", entry.Name, board, strings.Join(entry.Surfaces, ", "))
			}

			path := widgets.BoardPath(dir, board)
			b, read, err := readValidBoard(path, board, catalog.Find)
			if err != nil {
				return err
			}

			if size == "" {
				size = entry.DefaultSize
			}
			if !slices.Contains(entry.Sizes, size) {
				return fmt.Errorf("%s does not come in size %q: it takes %s", entry.Name, size, strings.Join(entry.Sizes, ", "))
			}
			with, err := inputValues(entry, sets)
			if err != nil {
				return err
			}
			switch {
			case id == "":
				id = widgets.NextID(b, entry.Widget)
			case !widgets.ValidInstanceID(id):
				return fmt.Errorf("id %q: use lower case letters, digits and dashes", id)
			case b.HasID(id):
				return fmt.Errorf("id %q is already on the %s board", id, board)
			}
			w, h, _ := widgets.FrameFor(size)
			var frame widgets.Frame
			if at == "" {
				frame = widgets.FreeSpot(b, w, h)
			} else {
				frame = widgets.Frame{W: w, H: h}
				if frame.X, frame.Y, err = parsePoint(at); err != nil {
					return err
				}
			}

			instance := widgets.Instance{ID: id, Type: entry.Name, With: with, Frame: frame, Size: size, Z: widgets.NextZ(b)}
			b.Widgets = append(b.Widgets, instance)
			if err := widgets.WriteBoard(path, read, b); err != nil {
				return err
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), boardChange{Board: board, Path: path, Widget: &instance})
			}
			cmd.Printf("added %s (%s) to the %s board at %d,%d\n", id, entry.Name, board, frame.X, frame.Y)
			return nil
		},
	}
	c.Flags().StringVar(&board, "board", "home", "the board to place it on")
	c.Flags().StringVar(&id, "id", "", "the instance id (default: the widget's name, made unique)")
	c.Flags().StringArrayVar(&sets, "set", nil, "an input value, as name=value (repeatable)")
	c.Flags().StringVar(&size, "size", "", "a preset the widget takes (default: its default_size)")
	c.Flags().StringVar(&at, "at", "", "the top-left corner in points, as x,y (default: the first free spot)")
	return c
}

func newWidgetsRemoveCmd(opts *options) *cobra.Command {
	var board string
	c := &cobra.Command{
		Use:   "remove <id>",
		Short: "Take a widget off a board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			if err := checkBoardSurface(board); err != nil {
				return err
			}
			catalog, err := cachedCatalog(dir)
			if err != nil {
				return err
			}
			path := widgets.BoardPath(dir, board)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("there is no %s board at %s", board, path)
			}
			b, read, err := readValidBoard(path, board, catalog.Find)
			if err != nil {
				return err
			}
			if err := b.Remove(args[0]); err != nil {
				return err
			}
			if err := widgets.WriteBoard(path, read, b); err != nil {
				return err
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), boardChange{Board: board, Path: path, Removed: args[0]})
			}
			cmd.Printf("removed %s from the %s board\n", args[0], board)
			return nil
		},
	}
	c.Flags().StringVar(&board, "board", "home", "the board to take it off")
	return c
}

func checkBoardSurface(board string) error {
	surface, ok := widgets.CurrentContract().Surfaces[board]
	switch {
	case !ok:
		return fmt.Errorf("there is no %s area: the areas are home, sidebar and context-sidebar", board)
	case surface.Status == widgets.StatusPlanned:
		return fmt.Errorf("the %s area arrives in a later version", board)
	}
	return nil
}

// readValidBoard reads the board, or starts an empty one when there is none.
// A board with a problem is refused: rewriting it would turn somebody's typo
// into lost widgets.
func readValidBoard(path, surface string, lookup widgets.Lookup) (widgets.Board, []byte, error) {
	b, read, problems, err := boardProblems(path, lookup)
	if errors.Is(err, os.ErrNotExist) {
		return widgets.NewBoard(surface), nil, nil
	}
	if err != nil {
		return widgets.Board{}, nil, err
	}
	if len(problems) > 0 {
		lines := make([]string, len(problems))
		for i, p := range problems {
			lines[i] = p.Error()
		}
		return widgets.Board{}, nil, fmt.Errorf("the board has a problem, so it was not changed:\n%s", strings.Join(lines, "\n"))
	}
	return b, read, nil
}

func inputValues(entry widgets.Entry, sets []string) (map[string]any, error) {
	with := map[string]any{}
	for _, set := range sets {
		name, raw, ok := strings.Cut(set, "=")
		if !ok {
			return nil, fmt.Errorf("--set %q: write it as name=value", set)
		}
		input, known := entry.Inputs[name]
		if !known {
			return nil, fmt.Errorf("%s has no input %q: it takes %s", entry.Name, name, orNoInputs(sortedNames(entry.Inputs)))
		}
		value, err := widgets.CoerceInput(name, input, raw)
		if err != nil {
			return nil, err
		}
		with[name] = value
	}
	return with, nil
}

func orNoInputs(names []string) string {
	if len(names) == 0 {
		return "no input"
	}
	return strings.Join(names, ", ")
}

func parsePoint(s string) (int, int, error) {
	xs, ys, ok := strings.Cut(s, ",")
	x, errX := strconv.Atoi(strings.TrimSpace(xs))
	y, errY := strconv.Atoi(strings.TrimSpace(ys))
	if !ok || errX != nil || errY != nil || x < 0 || y < 0 {
		return 0, 0, fmt.Errorf("--at %q: write it as x,y in points, both 0 or more", s)
	}
	return widgets.Snap(x), widgets.Snap(y), nil
}
