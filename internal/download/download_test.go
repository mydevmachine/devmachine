package download

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shellClient runs the download's shell commands on this computer, as ssh
// would run them on the machine.
type shellClient struct{ commands []string }

func (c *shellClient) Run(ctx context.Context, command string) (string, error) {
	return c.RunInput(ctx, command, strings.NewReader(""))
}

func (c *shellClient) RunInput(ctx context.Context, command string, stdin io.Reader) (string, error) {
	var out bytes.Buffer
	err := c.Stream(ctx, command, &out, nil)
	return out.String(), err
}

func (c *shellClient) Stream(ctx context.Context, command string, stdout, stderr io.Writer) error {
	c.commands = append(c.commands, command)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdout = stdout
	if stderr != nil {
		cmd.Stderr = stderr
	}
	return cmd.Run()
}

func (c *shellClient) Upload(context.Context, string, io.Reader) error { return nil }
func (c *shellClient) Close() error                                    { return nil }

func machineDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNumberedPutsTheCounterBeforeTheExtension(t *testing.T) {
	cases := []struct {
		name string
		n    int
		want string
	}{
		{"report.pdf", 1, "report.pdf"},
		{"report.pdf", 2, "report-2.pdf"},
		{"photo.final.png", 3, "photo.final-3.png"},
		{"Makefile", 2, "Makefile-2"},
		{".env", 2, ".env-2"},
		{"site.tar.gz", 2, "site-2.tar.gz"},
		{"notes.", 2, "notes.-2"},
		{"my report ção.txt", 2, "my report ção-2.txt"},
	}
	for _, c := range cases {
		if got := Numbered(c.name, c.n); got != c.want {
			t.Errorf("Numbered(%q, %d) = %q, want %q", c.name, c.n, got, c.want)
		}
	}
}

func TestLocalNameReplacesWhatCannotBeAFileName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":     "report.pdf",
		"line\nbreak":    "line_break",
		"nul\x00byte":    "nul_byte",
		"":               "download",
		".":              "download",
		"..":             "download",
		"with space ção": "with space ção",
	}
	for in, want := range cases {
		if got := LocalName(in); got != want {
			t.Errorf("LocalName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatReportsAFileWithItsSizeAndFullPath(t *testing.T) {
	dir := machineDir(t)
	file := filepath.Join(dir, "it's a \"report\" ção $(id).txt")
	write(t, file, "hello")

	got, err := Stat(context.Background(), &shellClient{}, file)
	if err != nil {
		t.Fatal(err)
	}
	want := Source{Path: file, Size: 5}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestStatReportsAFolder(t *testing.T) {
	dir := machineDir(t)
	folder := filepath.Join(dir, "my site")
	write(t, filepath.Join(folder, "index.html"), "x")

	got, err := Stat(context.Background(), &shellClient{}, folder)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Folder || got.Path != folder {
		t.Fatalf("got %+v", got)
	}
}

func TestStatRefusesWhatIsMissingOrNotAFile(t *testing.T) {
	dir := machineDir(t)
	cases := map[string]string{
		filepath.Join(dir, "missing.txt"): "does not exist",
		"/dev/null":                       "not a regular file",
		"/":                               "the whole disk",
	}
	for path, want := range cases {
		_, err := Stat(context.Background(), &shellClient{}, path)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Stat(%q) = %v, want an error saying %q", path, err, want)
		}
	}
}

func TestStatNeverPutsThePathInTheCommand(t *testing.T) {
	dir := machineDir(t)
	canary := filepath.Join(dir, "canary")
	write(t, canary, "alive")
	hostile := "'; rm -f " + canary + "; echo '"
	c := &shellClient{}

	_, _ = Stat(context.Background(), c, hostile)
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("the path ran as a command: %v", err)
	}
	for _, cmd := range c.commands {
		if strings.Contains(cmd, "rm -f") {
			t.Fatalf("the path is in the command: %s", cmd)
		}
	}
}

func TestFetchStreamsAFileUnderItsOwnName(t *testing.T) {
	remote := machineDir(t)
	local := machineDir(t)
	file := filepath.Join(remote, "photo ção.png")
	write(t, file, "png bytes")

	got, err := Fetch(context.Background(), &shellClient{}, Source{Path: file, Size: 9}, local, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(local, "photo ção.png")
	if got.Local != want || got.Bytes != 9 {
		t.Fatalf("got %+v, want %s", got, want)
	}
	body, _ := os.ReadFile(want)
	if string(body) != "png bytes" {
		t.Fatalf("body %q", body)
	}
}

func TestFetchReportsAFewRunningTotalsEndingWithTheWholeFile(t *testing.T) {
	remote := machineDir(t)
	local := machineDir(t)
	file := filepath.Join(remote, "big.bin")
	const size = 4 << 20
	write(t, file, strings.Repeat("x", size))

	var reports []int64
	_, err := Fetch(context.Background(), &shellClient{}, Source{Path: file, Size: size}, local,
		func(done int64) { reports = append(reports, done) })
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) < 2 || len(reports) > 101 {
		t.Fatalf("got %d reports, want a running total without one per write", len(reports))
	}
	for i := 1; i < len(reports); i++ {
		if reports[i] <= reports[i-1] {
			t.Fatalf("reports go backwards or repeat: %v", reports)
		}
	}
	if reports[len(reports)-1] != size {
		t.Fatalf("the last report is %d, want %d", reports[len(reports)-1], size)
	}
}

func TestFetchNumbersACollisionInsteadOfOverwriting(t *testing.T) {
	remote := machineDir(t)
	local := machineDir(t)
	file := filepath.Join(remote, "report.pdf")
	write(t, file, "new")
	write(t, filepath.Join(local, "report.pdf"), "old")
	write(t, filepath.Join(local, "report-2.pdf"), "older")

	got, err := Fetch(context.Background(), &shellClient{}, Source{Path: file, Size: 3}, local, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got.Local) != "report-3.pdf" {
		t.Fatalf("got %s", got.Local)
	}
	old, _ := os.ReadFile(filepath.Join(local, "report.pdf"))
	if string(old) != "old" {
		t.Fatalf("the existing file was overwritten: %q", old)
	}
	assertNoLeftovers(t, local)
}

func TestFetchPacksAFolderIntoATarball(t *testing.T) {
	remote := machineDir(t)
	local := machineDir(t)
	folder := filepath.Join(remote, "my site")
	write(t, filepath.Join(folder, "index.html"), "<h1>hi</h1>")
	write(t, filepath.Join(folder, "css", "a.css"), "body{}")

	got, err := Fetch(context.Background(), &shellClient{}, Source{Path: folder, Folder: true}, local, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got.Local) != "my site.tar.gz" {
		t.Fatalf("got %s", got.Local)
	}
	out, err := exec.Command("tar", "tzf", got.Local).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"my site/index.html", "my site/css/a.css"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("the archive lacks %s:\n%s", want, out)
		}
	}
}

func TestFetchLeavesNothingBehindWhenTheStreamFails(t *testing.T) {
	local := machineDir(t)
	missing := filepath.Join(machineDir(t), "gone.txt")

	_, err := Fetch(context.Background(), &shellClient{}, Source{Path: missing, Size: 1}, local, nil)
	if err == nil {
		t.Fatal("a file that vanished was reported as downloaded")
	}
	assertNoLeftovers(t, local)
	entries, _ := os.ReadDir(local)
	if len(entries) != 0 {
		t.Fatalf("files were left: %v", entries)
	}
}

func TestFetchRefusesADestinationThatIsNotAFolder(t *testing.T) {
	remote := machineDir(t)
	file := filepath.Join(remote, "a.txt")
	write(t, file, "x")

	_, err := Fetch(context.Background(), &shellClient{}, Source{Path: file, Size: 1}, filepath.Join(remote, "nope"), nil)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want a missing folder", err)
	}
}

func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".devmachine-download-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files were left behind: %v", leftovers)
	}
}

func TestStatAlsoTriesTheComposedFormOfADecomposedPath(t *testing.T) {
	decomposed := "/home/acme/c\u0327a\u0303o.txt"
	composed := "/home/acme/\u00e7\u00e3o.txt"

	cmd := statCommand(decomposed)
	for _, form := range []string{decomposed, composed} {
		if !strings.Contains(cmd, encoded(form)) {
			t.Fatalf("the command does not carry %q", form)
		}
	}
}

func TestStatFindsAFileUnderTheComposedNameWhenGivenTheDecomposedOne(t *testing.T) {
	dir := machineDir(t)
	composed := filepath.Join(dir, "\u00e7\u00e3o.txt")
	write(t, composed, "x")

	got, err := Stat(context.Background(), &shellClient{}, filepath.Join(dir, "c\u0327a\u0303o.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Size != 1 {
		t.Fatalf("got %+v", got)
	}
}
