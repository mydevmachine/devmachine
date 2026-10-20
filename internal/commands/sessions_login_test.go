package commands

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/mydevmachine/devmachine/internal/remote"
)

type loginRemote struct {
	fakeRemote
	login  error
	logins *atomic.Int32
}

func (l loginRemote) Login(context.Context) error {
	l.logins.Add(1)
	return l.login
}

func TestSessionsLoginWaitingIsItsOwnKind(t *testing.T) {
	dir := configWith(t, sessionsConfig)
	var logins atomic.Int32
	waiting := fmt.Errorf("%w: machine %q answered", remote.ErrLoginWaiting, "main")
	dialByUser(t, map[string]remote.Client{
		"alice": loginRemote{fakeRemote: fakeRemote{out: busySessionOutput}, login: waiting, logins: &logins},
		"bob":   loginRemote{fakeRemote: fakeRemote{out: "@@NOW@@\n1000000\n"}, logins: &logins},
	}, nil)

	got, _ := sessionsJSON(t, "--config", dir, "sessions", "--json")

	if got[0].Error == nil || got[0].Error.Kind != "login-waiting" || len(got[0].Sessions) != 0 {
		t.Fatalf("alice = %+v", got[0])
	}
	if got[1].Error != nil {
		t.Fatalf("bob = %+v", got[1])
	}
}

func TestSessionsLogsInOncePerMachineAndUser(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"+
		"workspaces:\n  - name: alice\n    machine: main\n  - name: web\n    machine: main\n    user: alice\n  - name: bob\n    machine: main\n")
	var logins atomic.Int32
	alice := loginRemote{fakeRemote: fakeRemote{out: "@@NOW@@\n1000000\n"}, logins: &logins}
	bob := loginRemote{fakeRemote: fakeRemote{out: "@@NOW@@\n1000000\n"}, logins: &logins}
	dialByUser(t, map[string]remote.Client{"alice": alice, "bob": bob}, nil)

	got, _ := sessionsJSON(t, "--config", dir, "sessions", "--json")

	if len(got) != 3 {
		t.Fatalf("workspaces = %+v", got)
	}
	if n := logins.Load(); n != 2 {
		t.Fatalf("logins = %d, want 2: alice's two workspaces share one login, bob has his own", n)
	}
}

func TestSessionsARefusedLoginAnswersEveryWorkspaceOfThatUser(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"+
		"workspaces:\n  - name: alice\n    machine: main\n  - name: web\n    machine: main\n    user: alice\n")
	var logins atomic.Int32
	refused := fmt.Errorf("%w: machine %q refused", remote.ErrAuthRefused, "main")
	dialByUser(t, map[string]remote.Client{
		"alice": loginRemote{fakeRemote: fakeRemote{out: busySessionOutput}, login: refused, logins: &logins},
	}, nil)

	got, _ := sessionsJSON(t, "--config", dir, "sessions", "--json")

	for _, w := range got {
		if w.Error == nil || w.Error.Kind != "login-refused" {
			t.Fatalf("%s = %+v", w.Name, w)
		}
	}
	if n := logins.Load(); n != 1 {
		t.Fatalf("logins = %d, want 1", n)
	}
}
