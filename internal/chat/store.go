// Package chat runs a quick chat or an ask through the coding CLI a person is
// already logged in to, and keeps the local copy of each conversation.
package chat

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// The kinds of record.
const (
	KindChat = "chat"
	KindAsk  = "ask"
)

// The modes of an ask.
const (
	ModeFork       = "fork"
	ModeTranscript = "transcript"
)

// The roles of a message.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// LocalWorkspace is the workspace a record names when it runs on this computer.
const LocalWorkspace = "local"

const titleMax = 60

// Record is one conversation, as `chat list` prints it.
type Record struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Harness          string    `json:"harness"`
	Workspace        string    `json:"workspace"`
	Folder           string    `json:"folder"`
	HarnessSessionID string    `json:"harness_session_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Kind             string    `json:"kind"`
	Session          string    `json:"session,omitempty"`
	SourceSessionID  string    `json:"source_session_id,omitempty"`
	Mode             string    `json:"mode,omitempty"`
	Excerpts         int       `json:"excerpts,omitempty"`
}

// Tool is one tool line of an answer.
type Tool struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// Message is one line of a conversation's local copy.
type Message struct {
	Role  string `json:"role"`
	Text  string `json:"text"`
	Tools []Tool `json:"tools"`
	// Denied are the tools the harness refused because nobody was there
	// to approve them.
	Denied []Tool    `json:"denied,omitempty"`
	At     time.Time `json:"at"`
	Error  *Failure  `json:"error,omitempty"`
}

// Store is the folder that holds every conversation's local copy.
type Store struct {
	dir string
}

// Open makes sure <configDir>/chats exists and is never committed.
func Open(configDir string) (*Store, error) {
	dir := filepath.Join(configDir, "chats")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	ignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(ignore, []byte("*\n"), 0o600); err != nil {
			return nil, fmt.Errorf("writing %s: %w", ignore, err)
		}
	}
	return &Store{dir: dir}, nil
}

// Title is the first non-empty line of a message, cut to 60 characters.
func Title(message string) string {
	for line := range strings.SplitSeq(message, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > titleMax {
			return string(r[:titleMax])
		}
		return line
	}
	return "New chat"
}

func newID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("making a chat id: %w", err)
	}
	return "c_" + hex.EncodeToString(b), nil
}

// Create stores a new record and returns it with its id and times set.
func (s *Store) Create(r Record) (Record, error) {
	id, err := newID()
	if err != nil {
		return Record{}, err
	}
	now := time.Now().UTC()
	r.ID, r.CreatedAt, r.UpdatedAt = id, now, now
	if r.Kind == "" {
		r.Kind = KindChat
	}
	err = s.locked(func() error {
		records, err := s.read()
		if err != nil {
			return err
		}
		return s.write(append(records, r))
	})
	return r, err
}

// Get is the record with this id.
func (s *Store) Get(id string) (Record, error) {
	records, err := s.read()
	if err != nil {
		return Record{}, err
	}
	for _, r := range records {
		if r.ID == id {
			return r, nil
		}
	}
	return Record{}, fmt.Errorf("no chat %q: `devmachine chat list --all` lists them", id)
}

// List is every record, the most recently updated first. Asks are left out
// unless all is set.
func (s *Store) List(all bool) ([]Record, error) {
	records, err := s.read()
	if err != nil {
		return nil, err
	}
	out := []Record{}
	for _, r := range records {
		if all || r.Kind != KindAsk {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b Record) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return out, nil
}

// Update changes the record with this id and moves its updated_at to now.
func (s *Store) Update(id string, change func(*Record)) error {
	return s.locked(func() error {
		records, err := s.read()
		if err != nil {
			return err
		}
		for i := range records {
			if records[i].ID == id {
				change(&records[i])
				records[i].UpdatedAt = time.Now().UTC()
				return s.write(records)
			}
		}
		return fmt.Errorf("no chat %q: `devmachine chat list --all` lists them", id)
	})
}

// Delete removes the record and its local messages. The harness's own
// transcript stays where it is.
func (s *Store) Delete(id string) error {
	return s.locked(func() error {
		records, err := s.read()
		if err != nil {
			return err
		}
		i := slices.IndexFunc(records, func(r Record) bool { return r.ID == id })
		if i < 0 {
			return fmt.Errorf("no chat %q: `devmachine chat list --all` lists them", id)
		}
		if err := s.write(slices.Delete(records, i, i+1)); err != nil {
			return err
		}
		if err := os.Remove(s.messagesPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing the messages of %s: %w", id, err)
		}
		return nil
	})
}

// AppendMessage adds one message to the conversation's local copy.
func (s *Store) AppendMessage(id string, m Message) error {
	if m.Tools == nil {
		m.Tools = []Tool{}
	}
	line, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding a message: %w", err)
	}
	return s.locked(func() error {
		path := s.messagesPath(id)
		body, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		return replaceFile(path, append(append(body, line...), '\n'))
	})
}

// Messages is the conversation's local copy, oldest first.
func (s *Store) Messages(id string) ([]Message, error) {
	path := s.messagesPath(id)
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	out := []Message{}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var m Message
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		if m.Tools == nil {
			m.Tools = []Tool{}
		}
		out = append(out, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return out, nil
}

func (s *Store) messagesPath(id string) string {
	return filepath.Join(s.dir, filepath.Base(id)+".jsonl")
}

func (s *Store) indexPath() string { return filepath.Join(s.dir, "index.json") }

func (s *Store) read() ([]Record, error) {
	body, err := os.ReadFile(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.indexPath(), err)
	}
	var index struct {
		Chats []Record `json:"chats"`
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.indexPath(), err)
	}
	return index.Chats, nil
}

func (s *Store) write(records []Record) error {
	body, err := json.MarshalIndent(struct {
		Chats []Record `json:"chats"`
	}{records}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the chat index: %w", err)
	}
	return replaceFile(s.indexPath(), append(body, '\n'))
}

func (s *Store) locked(write func() error) error {
	path := filepath.Join(s.dir, ".lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking %s: %w", path, err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return write()
}

func replaceFile(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
