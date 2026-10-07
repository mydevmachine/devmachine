package widgets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAllKeepsTheGoodWidgetsAndReportsTheBadOne(t *testing.T) {
	root := t.TempDir()
	writeWidget(t, root, "usage", usageWidget)
	writeWidget(t, root, "bad", strings.Replace(usageWidget, "name: usage", "name: bad\n", 1)+"colour: red\n")
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	found, problems := LoadAll("", root)
	if len(found) != 1 || found[0].Name != "usage" {
		t.Fatalf("got %v", found)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Message, `unknown field "colour"`) {
		t.Fatalf("got %v", problems)
	}
}

func TestLoadAllRefusesALinkedWidget(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	target := writeWidget(t, elsewhere, "usage", usageWidget)
	if err := os.Symlink(target, filepath.Join(root, "usage")); err != nil {
		t.Fatal(err)
	}
	_, problems := LoadAll("", root)
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "must not be a link") {
		t.Fatalf("got %v", problems)
	}
}

func TestLoadAllOnAnEmptyFolderSaysSo(t *testing.T) {
	_, problems := LoadAll("", t.TempDir())
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "holds no folder with a widget.yml") {
		t.Fatalf("got %v", problems)
	}
}

func TestLoadAllOnAMissingFolderSaysSo(t *testing.T) {
	_, problems := LoadAll("", filepath.Join(t.TempDir(), "widgets"))
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "the widgets folder is not there") {
		t.Fatalf("got %v", problems)
	}
}
