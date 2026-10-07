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
	Moved   string            `json:"moved,omitempty"`
	Order   []string          `json:"order,omitempty"`
}

func newWidgetsAddCmd(opts *options) *cobra.Command {
	var board, id, size, at, after, before string
	var sets []string
	c := &cobra.Command{
		Use:   "add <package/widget>",
		Short: "Place a widget on a board",
		Long: "On Home, without --at it takes the first free spot, scanning rows of 8pt " +
			"from the top left. In a sidebar it goes at the end of the list, or next to " +
			"--after or --before. The board is re-read first and written atomically; a " +
			"board with a problem is refused, never rewritten.",
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
			stack := widgets.IsStack(board)
			switch {
			case stack && at != "":
				return fmt.Errorf("--at places a widget on Home's canvas; in the %s area use --after or --before", board)
			case !stack && (after != "" || before != ""):
				return fmt.Errorf("--after and --before order a sidebar's list; the %s area is a canvas, so use --at", board)
			}
			if !slices.Contains(entry.Surfaces, board) {
				return fmt.Errorf("%s does not fit the %s area: it fits %s", entry.Name, board, strings.Join(entry.Surfaces, ", "))
			}

			path := widgets.BoardPath(dir, board)
			b, read, err := readValidBoard(path, board, catalog.Find)
			if err != nil {
				return err
			}
			if other, taken := b.IDOfType(entry.Name); entry.Single && taken {
				return fmt.Errorf("%s goes on a board once, and the %s board has it as %s", entry.Name, board, other)
			}

			if stack {
				size, err = stackSize(entry, size)
			} else {
				size, err = canvasSize(entry, size)
			}
			if err != nil {
				return err
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

			instance := widgets.Instance{ID: id, Type: entry.Name, With: with, Size: size}
			where := "at the end"
			if stack {
				if err := b.Insert(instance, after, before); err != nil {
					return err
				}
				switch {
				case after != "":
					where = "after " + after
				case before != "":
					where = "before " + before
				}
			} else {
				w, h, _ := widgets.FrameFor(size)
				instance.Frame = widgets.Frame{W: w, H: h}
				if at == "" {
					instance.Frame = widgets.FreeSpot(b, w, h)
				} else if instance.Frame.X, instance.Frame.Y, err = parsePoint(at); err != nil {
					return err
				}
				instance.Z = widgets.NextZ(b)
				b.Widgets = append(b.Widgets, instance)
				where = fmt.Sprintf("at %d,%d", instance.Frame.X, instance.Frame.Y)
			}
			if err := widgets.WriteBoard(path, read, b); err != nil {
				return err
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), boardChange{Board: board, Path: path, Widget: &instance})
			}
			cmd.Printf("added %s (%s) to the %s board %s\n", id, entry.Name, board, where)
			return nil
		},
	}
	c.Flags().StringVar(&board, "board", "home", "the board to place it on: home, sidebar or context-sidebar")
	c.Flags().StringVar(&id, "id", "", "the instance id (default: the widget's name, made unique)")
	c.Flags().StringArrayVar(&sets, "set", nil, "an input value, as name=value (repeatable)")
	c.Flags().StringVar(&size, "size", "", "a preset the widget takes, or auto in a sidebar (default: auto when its view grows, else its default_size)")
	c.Flags().StringVar(&at, "at", "", "on Home, the top-left corner in points, as x,y (default: the first free spot)")
	c.Flags().StringVar(&after, "after", "", "in a sidebar, the id it goes right after")
	c.Flags().StringVar(&before, "before", "", "in a sidebar, the id it goes right before")
	c.MarkFlagsMutuallyExclusive("after", "before")
	return c
}

// canvasSize is the preset a widget on Home gets: --size, or its default_size.
func canvasSize(entry widgets.Entry, size string) (string, error) {
	if size == "" {
		size = entry.DefaultSize
	}
	if !slices.Contains(entry.Sizes, size) {
		return "", fmt.Errorf("%s does not come in size %q: it takes %s", entry.Name, size, strings.Join(entry.Sizes, ", "))
	}
	return size, nil
}

// stackSize is the size a widget in a sidebar gets: --size, else auto when
// its view grows with its content, else its default_size.
func stackSize(entry widgets.Entry, size string) (string, error) {
	grows := widgets.Grows(entry.View.Kind)
	switch {
	case size == "" && grows:
		return widgets.SizeAuto, nil
	case size == widgets.SizeAuto && grows:
		return size, nil
	case size == widgets.SizeAuto:
		return "", fmt.Errorf("%s does not grow with its content, so it takes no --size auto: it takes %s", entry.Name, strings.Join(entry.Sizes, ", "))
	}
	return canvasSize(entry, size)
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
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) && !widgets.IsStack(board) {
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

func newWidgetsMoveCmd(opts *options) *cobra.Command {
	var board, after, before string
	c := &cobra.Command{
		Use:   "move <id>",
		Short: "Move a widget up or down a sidebar's list",
		Long: "Puts the widget right after --after or right before --before. Only the " +
			"sidebar and the context sidebar have an order; a missing board is read as " +
			"the area's default board. The board is re-read first and written atomically.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			if err := checkBoardSurface(board); err != nil {
				return err
			}
			if !widgets.IsStack(board) {
				return fmt.Errorf("the %s area is a canvas: a widget there has a place, not a turn in a list; move it in the app or change its frame", board)
			}
			catalog, err := cachedCatalog(dir)
			if err != nil {
				return err
			}
			path := widgets.BoardPath(dir, board)
			b, read, err := readValidBoard(path, board, catalog.Find)
			if err != nil {
				return err
			}
			if err := b.Move(args[0], after, before); err != nil {
				return err
			}
			if err := widgets.WriteBoard(path, read, b); err != nil {
				return err
			}
			if opts.format == formatJSON {
				order := make([]string, len(b.Widgets))
				for i, w := range b.Widgets {
					order[i] = w.ID
				}
				return writeJSON(cmd.OutOrStdout(), boardChange{Board: board, Path: path, Moved: args[0], Order: order})
			}
			if after != "" {
				cmd.Printf("moved %s after %s on the %s board\n", args[0], after, board)
			} else {
				cmd.Printf("moved %s before %s on the %s board\n", args[0], before, board)
			}
			return nil
		},
	}
	c.Flags().StringVar(&board, "board", "", "the board: sidebar or context-sidebar")
	c.Flags().StringVar(&after, "after", "", "the id it goes right after")
	c.Flags().StringVar(&before, "before", "", "the id it goes right before")
	c.MarkFlagsMutuallyExclusive("after", "before")
	c.MarkFlagsOneRequired("after", "before")
	_ = c.MarkFlagRequired("board")
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

// readValidBoard reads the board, or starts the area's default board when there is none.
// A board with a problem is refused: rewriting it would turn somebody's typo
// into lost widgets.
func readValidBoard(path, surface string, lookup widgets.Lookup) (widgets.Board, []byte, error) {
	b, read, problems, err := boardProblems(path, lookup)
	if errors.Is(err, os.ErrNotExist) {
		return widgets.DefaultBoard(surface), nil, nil
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
