package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mydevmachine/devmachine/internal/download"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/spf13/cobra"
)

// defaultDownloadDir is where a download lands when --to is not given.
var defaultDownloadDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding your Downloads folder: %w", err)
	}
	return filepath.Join(home, "Downloads"), nil
}

type downloadResult struct {
	Remote string `json:"remote"`
	Local  string `json:"local,omitempty"`
	Bytes  int64  `json:"bytes"`
	Folder bool   `json:"folder"`
	Error  string `json:"error,omitempty"`
	err    error
}

func newDownloadCmd(opts *options) *cobra.Command {
	var workspace, to string
	var progress bool

	c := &cobra.Command{
		Use:   "download <remote-path>...",
		Short: "Bring files or folders from a workspace's or a machine's home to this computer",
		Long: "Copies each path from the machine to this computer, as the account of " +
			"the workspace --workspace names, or of the machine's admin with --machine. " +
			"With neither, it acts on the only machine configured. A relative path, " +
			"or one starting with ~/, is read from that account's home.\n\n" +
			"Files land in ~/Downloads unless --to names another folder. Each keeps " +
			"its name; a name already taken gets -2, -3 and so on: nothing is ever " +
			"overwritten. A folder arrives as one <name>" + download.FolderExt + " archive.\n\n" +
			"Prints the path each one was saved at, one per line. With several paths " +
			"it tries all of them and exits non-zero if any failed.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDownload(cmd, opts, workspace, to, progress, args)
		},
	}
	c.Flags().StringVar(&workspace, "workspace", "", "read as this workspace's account instead of the machine admin's")
	c.Flags().StringVar(&to, "to", "", "folder on this computer to save into (default ~/Downloads)")
	c.Flags().BoolVar(&progress, "progress", false, "write the bytes received so far to stderr, one JSON line at a time")
	return c
}

func runDownload(cmd *cobra.Command, opts *options, workspace, to string, progress bool, paths []string) error {
	dest, err := downloadDestination(to)
	if err != nil {
		return err
	}
	tgt, err := transferTarget(opts, workspace, "download")
	if err != nil {
		return err
	}

	client, _, err := dialMux(cmd.Context(), tgt.machine, tgt.user)
	if err != nil {
		return err
	}
	defer client.Close()

	// Every path is sized before the first byte moves, so a progress reader
	// knows the whole total from the start instead of watching it grow.
	results := make([]downloadResult, len(paths))
	sources := make([]download.Source, len(paths))
	for i, p := range paths {
		r := &results[i]
		r.Remote = p
		src, err := download.Stat(cmd.Context(), client, p)
		if errors.Is(err, remote.ErrHostKeyRejected) {
			record(opts, tgt, "download "+p, false)
			return explainHostKey(cmd.Context(), tgt.machine, err)
		}
		if err != nil {
			r.err = err
			continue
		}
		sources[i] = src
		r.Remote, r.Folder = src.Path, src.Folder
		if progress {
			writeProgress(cmd.ErrOrStderr(), src, 0)
		}
	}
	for i, p := range paths {
		r := &results[i]
		if r.err == nil {
			src := sources[i]
			var report func(int64)
			if progress {
				report = func(done int64) { writeProgress(cmd.ErrOrStderr(), src, done) }
			}
			got, err := download.Fetch(cmd.Context(), client, src, dest, report)
			r.Local, r.Bytes, r.err = got.Local, got.Bytes, err
		}
		record(opts, tgt, "download "+p, r.err == nil)
		if errors.Is(r.err, remote.ErrHostKeyRejected) {
			return explainHostKey(cmd.Context(), tgt.machine, r.err)
		}
	}
	return reportDownloads(cmd, opts, results)
}

// downloadProgress is one line of --progress. A folder's total is 0: its
// archive is packed as it travels, so nobody knows its size until it ends.
type downloadProgress struct {
	Remote string `json:"remote"`
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
}

func writeProgress(w io.Writer, src download.Source, done int64) {
	line, err := json.Marshal(downloadProgress{Remote: src.Path, Done: done, Total: src.Size})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "%s\n", line)
}

// downloadDestination is refused before anything connects: a typo in --to
// should not create a folder somewhere unexpected.
func downloadDestination(to string) (string, error) {
	if to == "" {
		dir, err := defaultDownloadDir()
		if err != nil {
			return "", err
		}
		to = dir
	}
	abs, err := filepath.Abs(to)
	if err != nil {
		return "", fmt.Errorf("--to %s: %w", to, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("--to %s: %w", to, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--to %s is not a folder", to)
	}
	return abs, nil
}

func reportDownloads(cmd *cobra.Command, opts *options, results []downloadResult) error {
	var failed []error
	for i := range results {
		if results[i].err != nil {
			results[i].Error = results[i].err.Error()
			failed = append(failed, results[i].err)
		}
	}

	if opts.format == formatJSON {
		if err := writeJSON(cmd.OutOrStdout(), results); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			if r.err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), r.Local)
			}
		}
	}

	switch {
	case len(failed) == 0:
		return nil
	case len(results) == 1:
		return failed[0]
	}
	if opts.format != formatJSON {
		for _, err := range failed {
			fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
		}
	}
	return fmt.Errorf("%d of %d paths were not downloaded", len(failed), len(results))
}
