package harness

import (
	"slices"
	"testing"
)

func TestClaudeArgv(t *testing.T) {
	h, _ := Get("claude")
	got, err := h.Argv("say \"hi\" $(rm -rf /)\nnext", "sonnet", "acceptEdits")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "-p", "--model", "sonnet", "--permission-mode", "acceptEdits", "--", "say \"hi\" $(rm -rf /)\nnext"}
	if !slices.Equal(got, want) {
		t.Fatalf("argv = %q", got)
	}
}

func TestClaudeArgvDefaults(t *testing.T) {
	h, _ := Get("claude")
	got, err := h.Argv("x", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"claude", "-p", "--", "x"}) {
		t.Fatalf("argv = %q", got)
	}
}

func TestCodexArgvSandboxAndFlagModes(t *testing.T) {
	h, _ := Get("codex")
	got, _ := h.Argv("x", "gpt-5", "workspace-write")
	want := []string{"codex", "exec", "--skip-git-repo-check", "-m", "gpt-5", "--sandbox", "workspace-write", "--", "x"}
	if !slices.Equal(got, want) {
		t.Fatalf("argv = %q", got)
	}
	got, _ = h.Argv("x", "", "approve-for-me")
	if !slices.Equal(got, []string{"codex", "exec", "--skip-git-repo-check", "--approve-for-me", "--", "x"}) {
		t.Fatalf("argv = %q", got)
	}
}

func TestUnknownModeRefused(t *testing.T) {
	h, _ := Get("claude")
	if _, err := h.Argv("x", "", "yolo"); err == nil {
		t.Fatal("want an error for an unknown mode")
	}
}

func TestDangerousAndNames(t *testing.T) {
	c, _ := Get("claude")
	if !c.IsDangerous("bypassPermissions") || c.IsDangerous("auto") {
		t.Fatal("claude dangerous modes wrong")
	}
	if !slices.Equal(Names(), []string{"claude", "codex"}) {
		t.Fatalf("names = %v", Names())
	}
	if _, ok := Get("gemini"); ok {
		t.Fatal("gemini is not a harness yet")
	}
}

func TestWidgetListsUnchanged(t *testing.T) {
	modes := ModesByHarness()
	if !slices.Equal(modes["claude"], []string{"manual", "dontAsk", "plan", "acceptEdits", "auto", "bypassPermissions"}) {
		t.Fatalf("claude modes = %v", modes["claude"])
	}
	if !slices.Equal(AllDangerous(), []string{"bypassPermissions", "danger-full-access", "dangerously-bypass-approvals-and-sandbox"}) {
		t.Fatalf("dangerous = %v", AllDangerous())
	}
}
