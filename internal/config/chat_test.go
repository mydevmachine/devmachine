package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const chatBase = "# my machines\nmachines:\n  - name: main\n    hosts: [203.0.113.10]\nworkspaces:\n  - name: alice\n    machine: main\n  - name: bob\n    machine: main\n"

func chatDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestChatDefaults(t *testing.T) {
	var c Config
	if c.ChatWorkspace() != ChatLocal || c.ChatFolder() != "~/chats" || c.ChatHarness() != "claude" {
		t.Fatalf("defaults = %q %q %q", c.ChatWorkspace(), c.ChatFolder(), c.ChatHarness())
	}
	c.Workspaces = []Workspace{{Name: "alice"}, {Name: "bob"}}
	if c.ChatWorkspace() != "alice" {
		t.Fatalf("first workspace = %q", c.ChatWorkspace())
	}
	c.Chat = Chat{DefaultWorkspace: "bob", Folder: "~/notes", Harness: "codex"}
	if c.ChatWorkspace() != "bob" || c.ChatFolder() != "~/notes" || c.ChatHarness() != "codex" {
		t.Fatalf("set = %+v", c.Chat)
	}
}

func TestSetChatWritesAndClears(t *testing.T) {
	dir := chatDir(t, chatBase)
	for key, value := range map[string]string{"chat.harness": "codex", "chat.default_workspace": "bob", "chat.folder": "~/notes"} {
		if err := SetChat(dir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Chat != (Chat{DefaultWorkspace: "bob", Folder: "~/notes", Harness: "codex"}) {
		t.Fatalf("chat = %+v", c.Chat)
	}
	body, _ := os.ReadFile(filepath.Join(dir, FileName))
	if !strings.Contains(string(body), "# my machines") {
		t.Fatalf("the comment was lost:\n%s", body)
	}
	for _, key := range ChatKeys {
		if err := SetChat(dir, key, ""); err != nil {
			t.Fatal(err)
		}
	}
	body, _ = os.ReadFile(filepath.Join(dir, FileName))
	if strings.Contains(string(body), "chat") {
		t.Fatalf("an empty chat block was left:\n%s", body)
	}
}

func TestSetChatMCP(t *testing.T) {
	dir := chatDir(t, chatBase)
	if err := SetChat(dir, "chat.mcp", "true"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil || !c.Chat.MCP {
		t.Fatalf("chat = %+v, %v", c.Chat, err)
	}
	if err := SetChat(dir, "chat.mcp", "maybe"); err == nil || !strings.Contains(err.Error(), "true or false") {
		t.Fatalf("err = %v", err)
	}
	if err := SetChat(dir, "chat.mcp", "false"); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(dir); c.Chat.MCP {
		t.Fatal("false did not turn it off")
	}
	if err := SetChat(dir, "chat.mcp", ""); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, FileName))
	if strings.Contains(string(body), "mcp") {
		t.Fatalf("unset left the key:\n%s", body)
	}
}

func TestSetChatRefusesWhatWouldNotLoad(t *testing.T) {
	dir := chatDir(t, chatBase)
	for key, value := range map[string]string{
		"chat.harness":           "pi",
		"chat.default_workspace": "carol",
		"chat.color":             "blue",
	} {
		if err := SetChat(dir, key, value); err == nil {
			t.Errorf("%s=%s was accepted", key, value)
		}
	}
	if err := SetChat(dir, "chat.default_workspace", "local"); err != nil {
		t.Fatalf("local refused: %v", err)
	}
}

func TestValidateRefusesAnUnknownChatHarness(t *testing.T) {
	dir := chatDir(t, chatBase+"chat:\n  harness: pi\n")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "chat.harness") {
		t.Fatalf("Validate = %v", err)
	}
}
