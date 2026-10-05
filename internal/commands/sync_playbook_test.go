package commands

import (
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
)

func TestSyncCallsAnsibleByItsPathOnlyOnARemoteMac(t *testing.T) {
	mac := facts.Facts{System: "Darwin", AnsiblePlaybook: "/opt/homebrew/bin/ansible-playbook"}
	debian := facts.Facts{System: "Linux", AnsiblePlaybook: "/usr/bin/ansible-playbook"}
	server := config.Machine{Name: "studio"}

	if got := ansiblePlaybookFor(server, mac); got != "/opt/homebrew/bin/ansible-playbook" {
		t.Fatalf("a Mac got %q", got)
	}
	if got := ansiblePlaybookFor(server, debian); got != "" {
		t.Fatalf("a Linux machine got %q, and its command would change", got)
	}
	if got := ansiblePlaybookFor(config.Machine{Name: "mac", Self: true}, mac); got != "" {
		t.Fatalf("a self machine got %q", got)
	}
	if got := ansiblePlaybookFor(server, facts.Facts{}); got != "" {
		t.Fatalf("a machine never read got %q", got)
	}
}
