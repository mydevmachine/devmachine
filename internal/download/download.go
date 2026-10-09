// Package download brings files and folders from a machine to this computer.
package download

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mydevmachine/devmachine/internal/remote"
	"golang.org/x/text/unicode/norm"
)

// FolderExt is what a folder becomes on this computer: one archive, written
// whole or not at all.
const FolderExt = ".tar.gz"

const tempPattern = ".devmachine-download-*"

// Source is one path on the machine, resolved: absolute, and known to be a
// file of Size bytes or a folder.
type Source struct {
	Path   string
	Size   int64
	Folder bool
}

// Result is where a Source landed on this computer.
type Result struct {
	Local string
	Bytes int64
}

// statScript resolves a path the way upload's --dir reads one: relative to
// the home, `~/` too, or absolute. It has no single quote in it, since it
// travels inside one; the path arrives base64-encoded, so it is never shell
// syntax.
//
// The second argument is the same path composed (NFC), tried only when the
// path as given is not there: a Mac app's process arguments arrive
// decomposed (NFD), while a Linux file name is almost always composed.
const statScript = `set -eu
fail() {
	printf "error %s\n" "$*"
	exit 1
}
resolve() {
	case "$1" in
	"~") printf %s "${HOME:-}" ;;
	"~/"*) printf %s "${HOME:-}/${1#"~/"}" ;;
	/*) printf %s "$1" ;;
	*) printf %s "${HOME:-}/$1" ;;
	esac
}
p=$(printf %s "$1" | base64 -d) || fail "cannot decode the path"
c=$(printf %s "$2" | base64 -d) || fail "cannot decode the path"
p=$(resolve "$p")
c=$(resolve "$c")
if [ ! -e "$p" ] && [ ! -L "$p" ] && { [ -e "$c" ] || [ -L "$c" ]; }; then
	p=$c
fi
[ "$p" != "/" ] || fail "/ is the whole disk: name a folder inside it"
if [ -d "$p" ]; then
	[ -r "$p" ] && [ -x "$p" ] || fail "$p cannot be read"
	printf "folder %s\n" "$p"
elif [ -f "$p" ]; then
	[ -r "$p" ] || fail "$p cannot be read"
	printf "file %s %s\n" "$(wc -c < "$p" | tr -d " ")" "$p"
elif [ -e "$p" ]; then
	fail "$p is not a regular file"
else
	fail "$p does not exist"
fi
`

// streamScript writes the file, or a gzipped tar of the folder with the
// folder's own name at its top, to stdout and nothing else.
const streamScript = `set -eu
p=$(printf %s "$1" | base64 -d)
if [ "$2" = folder ]; then
	exec tar -C "$(dirname -- "$p")" -czf - -- "$(basename -- "$p")"
fi
exec cat -- "$p"
`

func encoded(s string) string {
	return `"` + base64.StdEncoding.EncodeToString([]byte(s)) + `"`
}

func statCommand(p string) string {
	return "sh -c '" + statScript + "' devmachine-download " + encoded(p) + " " + encoded(norm.NFC.String(p))
}

func streamCommand(s Source) string {
	kind := "file"
	if s.Folder {
		kind = "folder"
	}
	return "sh -c '" + streamScript + "' devmachine-download " + encoded(s.Path) + " " + kind
}

// Stat asks the machine what p is, before anything is written here.
func Stat(ctx context.Context, c remote.Client, p string) (Source, error) {
	out, err := c.Run(ctx, statCommand(p))
	line := strings.TrimSuffix(out, "\n")
	if msg, ok := strings.CutPrefix(line, "error "); ok {
		return Source{}, errors.New(msg)
	}
	if err != nil {
		return Source{}, fmt.Errorf("asking the machine about %s: %w", p, err)
	}
	if folder, ok := strings.CutPrefix(line, "folder "); ok {
		return Source{Path: folder, Folder: true}, nil
	}
	if rest, ok := strings.CutPrefix(line, "file "); ok {
		size, full, found := strings.Cut(rest, " ")
		n, convErr := strconv.ParseInt(size, 10, 64)
		if found && convErr == nil {
			return Source{Path: full, Size: n}, nil
		}
	}
	return Source{}, fmt.Errorf("asking the machine about %s: unexpected answer %q", p, out)
}

// Fetch streams s into dir, under its own name, or with -2, -3 and so on
// before the extension when that name is taken: nothing is overwritten. The
// bytes go to a hidden temporary file first, so a failed or cut transfer
// never leaves a partial file under the real name. A non-nil report gets the
// running total of bytes written, a few times along the way and once at the
// end.
func Fetch(ctx context.Context, c remote.Client, s Source, dir string, report func(done int64)) (Result, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return Result{}, fmt.Errorf("the destination folder: %w", err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("the destination %s is not a folder", dir)
	}
	name := LocalName(path.Base(s.Path))
	if s.Folder {
		name += FolderExt
	}

	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return Result{}, fmt.Errorf("writing in %s: %w", dir, err)
	}
	placed := false
	defer func() {
		if !placed {
			os.Remove(tmp.Name())
		}
	}()

	counted := &countingWriter{w: tmp, report: report, step: reportStep(s)}
	var stderr bytes.Buffer
	streamErr := c.Stream(ctx, streamCommand(s), counted, &stderr)
	closeErr := tmp.Close()
	if streamErr == nil {
		counted.flush()
	}
	if streamErr != nil {
		if errors.Is(streamErr, remote.ErrHostKeyRejected) {
			return Result{}, streamErr
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return Result{}, fmt.Errorf("reading %s: %s", s.Path, msg)
		}
		return Result{}, fmt.Errorf("reading %s: %w", s.Path, streamErr)
	}
	if closeErr != nil {
		return Result{}, fmt.Errorf("writing %s: %w", tmp.Name(), closeErr)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return Result{}, fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}

	final, err := place(tmp.Name(), dir, name)
	if err != nil {
		return Result{}, err
	}
	placed = true
	return Result{Local: final, Bytes: counted.n}, nil
}

// place claims the first free name with an exclusive create, then moves the
// finished file over that claim, which is its own and empty.
func place(tmp, dir, name string) (string, error) {
	for n := 1; n <= 1000; n++ {
		candidate := filepath.Join(dir, Numbered(name, n))
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("writing %s: %w", candidate, err)
		}
		f.Close()
		if err := os.Rename(tmp, candidate); err != nil {
			os.Remove(candidate)
			return "", fmt.Errorf("writing %s: %w", candidate, err)
		}
		return candidate, nil
	}
	return "", fmt.Errorf("%s already holds 1000 files named %s", dir, name)
}

var unsafeInName = strings.NewReplacer("/", "_", "\x00", "_", "\n", "_", "\r", "_")

// LocalName is name as a file name on this computer.
func LocalName(name string) string {
	name = unsafeInName.Replace(name)
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	return name
}

// Numbered is name with -n before its extension, the same counter upload
// uses on the machine. The first copy keeps the name as it is.
func Numbered(name string, n int) string {
	if n < 2 {
		return name
	}
	stem, ext := name, ""
	if strings.HasSuffix(name, FolderExt) && len(name) > len(FolderExt) {
		stem, ext = strings.TrimSuffix(name, FolderExt), FolderExt
	} else if i := strings.LastIndex(name, "."); i > 0 && i < len(name)-1 {
		stem, ext = name[:i], name[i:]
	}
	return stem + "-" + strconv.Itoa(n) + ext
}

const (
	minReportStep    = 64 << 10
	folderReportStep = 256 << 10
)

// reportStep keeps a file to about a hundred reports. A folder's archive has
// no size until it ends, so it reports every fixed number of bytes.
func reportStep(s Source) int64 {
	if s.Folder {
		return folderReportStep
	}
	return max(s.Size/100, minReportStep)
}

type countingWriter struct {
	w        io.Writer
	n        int64
	report   func(int64)
	step     int64
	reported int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	if c.report != nil && c.n-c.reported >= c.step {
		c.reported = c.n
		c.report(c.n)
	}
	return n, err
}

func (c *countingWriter) flush() {
	if c.report != nil && c.n != c.reported {
		c.reported = c.n
		c.report(c.n)
	}
}
