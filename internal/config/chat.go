package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ChatHarnesses are the coding CLIs a chat can run on.
var ChatHarnesses = []string{"claude", "codex"}

// ChatLocal is the chat.default_workspace value that means this computer.
const ChatLocal = "local"

// Chat is how `devmachine chat` starts a conversation when the command does
// not say. Every field left out takes its default.
type Chat struct {
	DefaultWorkspace string `yaml:"default_workspace,omitempty" json:"default_workspace"`
	Folder           string `yaml:"folder,omitempty" json:"folder"`
	Harness          string `yaml:"harness,omitempty" json:"harness"`
	// MCP is whether a new chat starts the account's MCP servers, hooks and
	// plugins. Off by default: they are most of a harness's start-up.
	MCP bool `yaml:"mcp,omitempty" json:"mcp"`
}

// The defaults of the chat settings.
const (
	DefaultChatFolder  = "~/chats"
	DefaultChatHarness = "claude"
)

// ChatKeys are the settings `devmachine config set` accepts.
var ChatKeys = []string{"chat.default_workspace", "chat.folder", "chat.harness", "chat.mcp"}

// ChatFolder is chat.folder, or its default.
func (c Config) ChatFolder() string {
	if c.Chat.Folder != "" {
		return c.Chat.Folder
	}
	return DefaultChatFolder
}

// ChatHarness is chat.harness, or its default.
func (c Config) ChatHarness() string {
	if c.Chat.Harness != "" {
		return c.Chat.Harness
	}
	return DefaultChatHarness
}

// ChatWorkspace is where a new chat runs: chat.default_workspace, else the
// first workspace, else this computer.
func (c Config) ChatWorkspace() string {
	switch {
	case c.Chat.DefaultWorkspace != "":
		return c.Chat.DefaultWorkspace
	case len(c.Workspaces) > 0:
		return c.Workspaces[0].Name
	}
	return ChatLocal
}

func (c Config) validateChat() error {
	if c.Chat.Harness != "" && !slices.Contains(ChatHarnesses, c.Chat.Harness) {
		return fmt.Errorf("chat.harness %q: use one of %s", c.Chat.Harness, strings.Join(ChatHarnesses, ", "))
	}
	if w := c.Chat.DefaultWorkspace; w != "" && w != ChatLocal && !slices.Contains(c.workspaceNames(), w) {
		return fmt.Errorf("chat.default_workspace %q is not a workspace in %s: name one, or %q for this computer", w, FileName, ChatLocal)
	}
	return nil
}

// SetChat writes one chat setting; an empty value takes it out, so its
// default applies again. The rest of the file is left as it was.
func SetChat(dir, key, value string) error {
	name, ok := strings.CutPrefix(key, "chat.")
	if !ok || !slices.Contains(ChatKeys, key) {
		return fmt.Errorf("unknown setting %q: the settings are %s", key, strings.Join(ChatKeys, ", "))
	}
	node := stringNode(value)
	if name == "mcp" && value != "" {
		on, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("chat.mcp is true or false, not %q", value)
		}
		node = boolNode(on)
	}
	return locked(dir, func() error {
		return editDocument(dir, func(root *yaml.Node) error {
			chat := field(root, "chat")
			if chat == nil || chat.Kind != yaml.MappingNode {
				if value == "" {
					return nil
				}
				chat = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				setField(root, "chat", chat)
			}
			setField(chat, name, node)
			if len(chat.Content) == 0 {
				setField(root, "chat", nil)
			}
			var check Config
			if err := root.Decode(&check); err != nil {
				return fmt.Errorf("reading the edited %s: %w", FileName, err)
			}
			return check.validateChat()
		})
	})
}
