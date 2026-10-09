package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

const sessionsConfig = "machines:\n  - name: main\n    hosts: [203.0.113.10]\n" +
	"workspaces:\n  - name: alice\n    machine: main\n  - name: bob\n    machine: main\n"

const busySessionOutput = "@@NOW@@\n1000000\n" +
	"@@SESSIONS@@\n" +
	"checkout\t2\t989200\t999998\t\tclaude\t/home/alice/acme-web\n" +
	"@@PANES@@\n" +
	"checkout\tclaude\t10\n" +
	"@@PS@@\n" +
	"   10     1 claude\n" +
	"@@GIT@@\n" +
	"/home/alice/acme-web\tfeature/checkout\n"

func dialByUser(t *testing.T, byUser map[string]remote.Client, failures map[string]error) {
	t.Helper()
	stub := func(_ context.Context, _ config.Machine, user string) (remote.Client, string, error) {
		if err := failures[user]; err != nil {
			return nil, "", err
		}
		c, ok := byUser[user]
		if !ok {
			t.Errorf("unexpected dial as %q", user)
			return nil, "", errors.New("unexpected dial")
		}
		return c, "203.0.113.10", nil
	}
	origDial, origMux := dial, dialMux
	dial, dialMux = stub, stub
	t.Cleanup(func() { dial, dialMux = origDial, origMux })
}

func sessionsJSON(t *testing.T, args ...string) ([]sessionsWorkspace, string) {
	t.Helper()
	out, err := execute(t, args...)
	if err != nil {
		t.Fatal(err, out)
	}
	var got struct {
		Workspaces []sessionsWorkspace `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err, out)
	}
	return got.Workspaces, out
}

func TestSessionsJSONListsEveryWorkspace(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	dialByUser(t, map[string]remote.Client{
		"alice": fakeRemote{out: busySessionOutput},
		"bob":   fakeRemote{out: "@@NOW@@\n1000000\n"},
	}, nil)

	got, _ := sessionsJSON(t, "--config", dir, "sessions", "--json")

	if len(got) != 2 || got[0].Name != "alice" || got[1].Name != "bob" {
		t.Fatalf("workspaces = %+v", got)
	}
	if got[0].Machine != "main" || len(got[0].Sessions) != 1 || !got[0].Sessions[0].Busy {
		t.Fatalf("alice = %+v", got[0])
	}
	if got[1].Error != nil || len(got[1].Sessions) != 0 {
		t.Fatalf("bob = %+v", got[1])
	}
}

func TestSessionsOneWorkspaceDownOthersAnswer(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	dialByUser(t, map[string]remote.Client{"alice": fakeRemote{out: busySessionOutput}},
		map[string]error{"bob": errors.New(`machine "main": no address answered: 203.0.113.10`)})

	got, raw := sessionsJSON(t, "--config", dir, "sessions", "--json")

	if len(got[0].Sessions) != 1 {
		t.Fatalf("alice = %+v", got[0])
	}
	if got[1].Error == nil || got[1].Error.Kind != "unreachable" {
		t.Fatalf("bob = %+v", got[1])
	}
	if !strings.Contains(raw, `"sessions": []`) {
		t.Fatalf("sessions must print as []:\n%s", raw)
	}
}

func TestSessionsTmuxMissing(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	dialByUser(t, map[string]remote.Client{
		"alice": fakeRemote{out: "@@NO_TMUX@@\n"},
		"bob":   fakeRemote{out: "@@NO_TMUX@@\n"},
	}, nil)

	got, _ := sessionsJSON(t, "--config", dir, "sessions", "--json")

	if got[0].Error == nil || got[0].Error.Kind != "tmux-missing" {
		t.Fatalf("alice = %+v", got[0])
	}
}

func TestSessionsHostKeyRejected(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	rejected := fmt.Errorf("machine %q: %w", "main", remote.ErrHostKeyRejected)
	dialByUser(t, map[string]remote.Client{"bob": fakeRemote{out: busySessionOutput}},
		map[string]error{"alice": rejected})

	got, _ := sessionsJSON(t, "--config", dir, "sessions", "--json")

	if got[0].Error == nil || got[0].Error.Kind != "host-key-rejected" {
		t.Fatalf("alice = %+v", got[0])
	}
}

func TestSessionsOtherError(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	dialByUser(t, map[string]remote.Client{"bob": fakeRemote{out: busySessionOutput}},
		map[string]error{"alice": errors.New("permission denied")})

	got, _ := sessionsJSON(t, "--config", dir, "sessions", "--json")

	if got[0].Error == nil || got[0].Error.Kind != "other" || !strings.Contains(got[0].Error.Message, "permission denied") {
		t.Fatalf("alice = %+v", got[0])
	}
}

func TestSessionsUnknownWorkspaceFails(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	dialByUser(t, nil, nil)

	_, err := execute(t, "--config", dir, "sessions", "--workspace", "carol", "--json")

	if err == nil || !strings.Contains(err.Error(), "carol") {
		t.Fatalf("error = %v, want it to name carol", err)
	}
}

func TestSessionsNoWorkspaces(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	dialByUser(t, nil, nil)

	got, raw := sessionsJSON(t, "--config", dir, "sessions", "--json")

	if len(got) != 0 || !strings.Contains(raw, `"workspaces": []`) {
		t.Fatalf("output:\n%s", raw)
	}
}

func TestSessionsWorkspaceFilter(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	dialByUser(t, map[string]remote.Client{"bob": fakeRemote{out: busySessionOutput}}, nil)

	got, _ := sessionsJSON(t, "--config", dir, "sessions", "--workspace", "bob", "--json")

	if len(got) != 1 || got[0].Name != "bob" {
		t.Fatalf("workspaces = %+v", got)
	}
}

func TestSessionsFormatJSONEqualsJSONFlag(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	dialByUser(t, map[string]remote.Client{
		"alice": fakeRemote{out: busySessionOutput},
		"bob":   fakeRemote{out: busySessionOutput},
	}, nil)

	_, viaFormat := sessionsJSON(t, "--config", dir, "--format", "json", "sessions")
	_, viaFlag := sessionsJSON(t, "--config", dir, "sessions", "--json")

	if viaFormat != viaFlag {
		t.Fatalf("outputs differ:\n%s\n%s", viaFormat, viaFlag)
	}
}

func TestSessionsTable(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	dialByUser(t, map[string]remote.Client{"alice": fakeRemote{out: busySessionOutput}},
		map[string]error{"bob": errors.New("no address answered")})

	out, err := execute(t, "--config", dir, "sessions")
	if err != nil {
		t.Fatal(err, out)
	}

	for _, want := range []string{"WORKSPACE", "SESSION", "BRANCH", "AGE", "STATE", "feature/checkout", "3h", "busy", "error: no address answered"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table is missing %q:\n%s", want, out)
		}
	}
}
