package commands

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/widgets"
	"github.com/spf13/cobra"
)

// entryEdit is what `widgets set` was asked to change on one entry.
type entryEdit struct {
	title, every       string
	setTitle, setEvery bool
	sets               []string
}

func newWidgetsSetCmd(opts *options) *cobra.Command {
	var board, title, every string
	var sets []string
	c := &cobra.Command{
		Use:   "set <id>",
		Short: "Change one widget on a board: its title, how often it runs, its inputs",
		Long: "--title and --every change only this copy; an empty value takes the key off, so " +
			"the widget's own title or every comes back. --set gives an input a value, " +
			"name=a,b for a choice of many. A widget written in the board keeps its source: " +
			"edit the board file for that. The board is re-read first and written atomically.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			edit := entryEdit{title: title, every: every, sets: sets,
				setTitle: cmd.Flags().Changed("title"), setEvery: cmd.Flags().Changed("every")}
			if !edit.setTitle && !edit.setEvery && len(sets) == 0 {
				return errors.New("nothing to change: give --title, --every or --set")
			}
			if err := checkBoardSurface(board); err != nil {
				return err
			}
			catalog, dir, err := loadCatalog(cmd.Context(), opts)
			if err != nil {
				return err
			}
			path := widgets.BoardPath(dir, board)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) && !widgets.IsOrdered(board) {
				return fmt.Errorf("there is no %s board at %s", board, path)
			}
			b, read, err := readValidBoard(path, board, catalog.Find)
			if err != nil {
				return err
			}
			w, err := b.Entry(args[0])
			if err != nil {
				return err
			}
			entry, known := catalog.Find(w.Type)
			before, err := widgets.EncodeBoard(b)
			if err != nil {
				return err
			}
			with, err := applyEdit(w, entry, known, edit)
			if err != nil {
				return err
			}
			cfg, err := loadConfigOrEmpty(dir)
			if err != nil {
				return err
			}
			warnings := widgets.OptionWarnings(w.ID, entry, with, targetNamesOf(cfg))
			after, err := widgets.EncodeBoard(b)
			if err != nil {
				return err
			}
			if !bytes.Equal(before, after) {
				if err := widgets.WriteBoard(path, read, b); err != nil {
					return err
				}
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), boardChange{Board: board, Path: path, Widget: w, Warnings: warnings})
			}
			cmd.Printf("changed %s on the %s board: %s\n", w.ID, board, strings.Join(changedKeys(edit, with), ", "))
			printWarnings(cmd, warnings)
			return nil
		},
	}
	c.Flags().StringVar(&board, "board", "", "the board: home, sidebar, context-sidebar, menubar or menubar-panel")
	c.Flags().StringVar(&title, "title", "", `the title this copy shows; "" takes the override off`)
	c.Flags().StringVar(&every, "every", "", `how often this copy runs, as 60s, 5m or manual; "" takes the override off`)
	c.Flags().StringArrayVar(&sets, "set", nil, "an input value, as name=value, or name=a,b for a choice of many (repeatable)")
	_ = c.MarkFlagRequired("board")
	return c
}

// applyEdit changes w as edit says, or changes nothing and says why. It
// returns the input values it set, for the option warnings.
func applyEdit(w *widgets.Instance, entry widgets.Entry, known bool, edit entryEdit) (map[string]any, error) {
	if w.Inline {
		switch {
		case edit.setEvery || len(edit.sets) > 0:
			return nil, fmt.Errorf("%s is written in the board: change its source in the board file, and the app asks for approval again", w.ID)
		case strings.TrimSpace(edit.title) == "":
			return nil, fmt.Errorf("%s is written in the board and needs a title, so --title cannot be empty", w.ID)
		}
		w.Title = edit.title
		return nil, nil
	}
	if edit.setTitle && edit.title != "" && strings.TrimSpace(edit.title) == "" {
		return nil, errors.New(`--title is only spaces: write a title, or --title "" to show the widget's own`)
	}
	if edit.setEvery && edit.every != "" {
		if problem := widgets.EveryOverrideProblem(edit.every, entry, known); problem != "" {
			return nil, fmt.Errorf("%s: %s", w.ID, problem)
		}
	}
	var with map[string]any
	if len(edit.sets) > 0 {
		if !known {
			return nil, fmt.Errorf("%s is not in the catalog, so its inputs are unknown: `devmachine widgets list` shows what there is", w.Type)
		}
		var err error
		if with, err = inputValues(entry, edit.sets); err != nil {
			return nil, err
		}
	}
	if edit.setTitle {
		w.Title = edit.title
	}
	if edit.setEvery {
		w.Every = edit.every
	}
	if len(with) > 0 {
		if w.With == nil {
			w.With = map[string]any{}
		}
		maps.Copy(w.With, with)
	}
	return with, nil
}

func changedKeys(edit entryEdit, with map[string]any) []string {
	var keys []string
	if edit.setTitle {
		keys = append(keys, "title")
	}
	if edit.setEvery {
		keys = append(keys, "every")
	}
	return append(keys, slices.Sorted(maps.Keys(with))...)
}
