package facts

import (
	"strings"
	"testing"
)

func TestReportedKeepsWhatTheMacBootstrapSaid(t *testing.T) {
	got := Reported(Facts{}, "/opt/local/bin/ansible-playbook-3.14", []string{"/opt/local/bin", "/opt/local/sbin"})
	if got.System != "Darwin" || got.OSFamily != "Darwin" || got.Distribution != "MacOSX" || got.ServiceMgr != "launchd" {
		t.Fatalf("got %#v", got)
	}
	if got.PkgMgr != "macports" || got.AnsiblePlaybook != "/opt/local/bin/ansible-playbook-3.14" ||
		strings.Join(got.PathPrefix, ":") != "/opt/local/bin:/opt/local/sbin" {
		t.Fatalf("got %#v", got)
	}

	read := Facts{System: "Darwin", OSFamily: "Darwin", Distribution: "MacOSX", DistributionVersion: "15.7.9", Architecture: "arm64"}
	got = Reported(read, "/opt/homebrew/bin/ansible-playbook", []string{"/opt/homebrew/bin", "/opt/homebrew/sbin"})
	if got.DistributionVersion != "15.7.9" || got.Architecture != "arm64" || got.PkgMgr != "homebrew" {
		t.Fatalf("what was read is lost: %#v", got)
	}
}
