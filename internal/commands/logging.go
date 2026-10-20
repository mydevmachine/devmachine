package commands

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/logfile"
	"github.com/mydevmachine/devmachine/internal/redact"
	"github.com/spf13/cobra"
)

type logMode int

const (
	logFailures logMode = iota + 1
	logEverything
)

// appPackage is the package the macOS app polls through `run --package`.
const appPackage = "devmachine-app"

// loggedCommands change a machine, or set one up, so their whole output is
// what somebody needs when it goes wrong. Every other command is a read, often
// one the app polls every few seconds, and only its failure is worth a line.
var loggedCommands = map[string]bool{
	"setup":                   true,
	"machine setup":           true,
	"machine doctor":          true,
	"machines add":            true,
	"machines create-local":   true,
	"machines delete-local":   true,
	"machines start":          true,
	"machines stop":           true,
	"machines trust":          true,
	"machines password-login": true,
	"sync":                    true,
	"update":                  true,
	"doctor":                  true,
	"packages add":            true,
	"packages remove":         true,
	"packages rm":             true,
	"packages install":        true,
	"packages update":         true,
	"login":                   true,
	"credentials push":        true,
	"secrets set":             true,
	"secrets rm":              true,
	"workspaces new":          true,
	"workspaces rm":           true,
	"workspaces destroy":      true,
	"expose add":              true,
	"expose rm":               true,
	"dns add":                 true,
	"dns rm":                  true,
	"scheduled add":           true,
	"scheduled edit":          true,
	"scheduled rm":            true,
	"scheduled pause":         true,
	"scheduled resume":        true,
	"scheduled run":           true,
	"upload":                  true,
	"run":                     true,
}

// logModeFor is how much of an invocation of cmd reaches the log.
func logModeFor(cmd *cobra.Command) logMode {
	path := strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
	if path == "run" {
		pkg, _ := cmd.Flags().GetString("package")
		noLog, _ := cmd.Flags().GetBool("no-log")
		if noLog || pkg == appPackage {
			return logFailures
		}
	}
	if loggedCommands[path] {
		return logEverything
	}
	return logFailures
}

// invocation is one run of the CLI, as the log sees it. It exists only when
// the CLI runs through run, so a test that builds a command tree never
// writes a log by accident.
type invocation struct {
	argv    []string
	started time.Time
	mode    logMode
	dir     string
	log     *logfile.Log
}

// startLog decides what the log keeps of this run and, for a command logged
// whole, writes the start line and copies both outputs into the log.
func startLog(cmd *cobra.Command, opts *options) {
	inv := opts.invocation
	if inv == nil {
		return
	}
	dir := logDir(opts)
	if dir == "" {
		return
	}
	inv.dir = dir
	inv.mode = logModeFor(cmd)
	if inv.mode != logEverything {
		return
	}
	inv.log = logfile.Open(dir)
	inv.log.Line("start", fmt.Sprintf("devmachine %s %s/%s: %s",
		version, runtime.GOOS, runtime.GOARCH, argvLine(inv.argv)))
	root := cmd.Root()
	root.SetOut(inv.log.Tee(root.OutOrStdout(), "out"))
	root.SetErr(inv.log.Tee(root.ErrOrStderr(), "err"))
}

// logDir is the absolute configuration directory the log goes under, or ""
// when there is none.
func logDir(opts *options) string {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir
}

// finishLog writes how the run ended, and returns the log's path when the
// person should be pointed at it.
//
// A run refused before startLog, for a flag it does not know or a command
// that does not exist, still leaves its one failed line.
func finishLog(opts *options, err error) string {
	inv := opts.invocation
	if inv == nil {
		return ""
	}
	if inv.dir == "" {
		if err == nil {
			return ""
		}
		inv.dir, inv.mode = logDir(opts), logFailures
		if inv.dir == "" {
			return ""
		}
	}
	took := time.Since(inv.started).Round(time.Millisecond)
	switch inv.mode {
	case logEverything:
		defer inv.log.Close()
		if err == nil {
			inv.log.Line("end", fmt.Sprintf("ok after %s", took))
			return ""
		}
		inv.log.Line("end", fmt.Sprintf("failed (exit %d) after %s: %v", exitCode(err), took, err))
		if inv.log.Opened() {
			return logfile.Path(inv.dir)
		}
	case logFailures:
		if err == nil {
			return ""
		}
		log := logfile.Open(inv.dir)
		log.Line("failed", fmt.Sprintf("%s (exit %d) after %s: %v", argvLine(inv.argv), exitCode(err), took, err))
		log.Close()
	}
	return ""
}

func argvLine(argv []string) string {
	words := redact.Argv(argv)
	for i, w := range words {
		if w == "" || strings.ContainsAny(w, " \t\n\"'\\") {
			words[i] = strconv.Quote(w)
		}
	}
	return strings.Join(words, " ")
}

func newLogsCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Find the CLI's own log",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print where the CLI writes its log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, _, err := config.Dir(opts.configDir); err != nil {
				return err
			}
			path := logfile.Path(logDir(opts))
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Path string `json:"path"`
				}{path})
			}
			cmd.Println(path)
			return nil
		},
	})
	return cmd
}
