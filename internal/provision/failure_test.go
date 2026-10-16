package provision

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

const failedOnOldAnsible = `
TASK [scheduled : Download the release and check it against its checksums] ****
changed: [devmachine]

TASK [scheduled : Unpack the release] *****************************************
fatal: [devmachine]: FAILED! => {"changed": false, "msg": "Failed to find handler for \"/tmp/x/devmachine_0.15.0_darwin_arm64.tar.gz\". Make sure the required command to extract the file is installed.\nCommand \"/usr/bin/tar\" detected as tar type bsd. GNU tar required."}

TASK [scheduled : Remove the download] ****************************************
ok: [devmachine]

PLAY RECAP ********************************************************************
devmachine                 : ok=9    changed=1    unreachable=0    failed=1    skipped=2    rescued=0    ignored=0
`

const failedOnNewAnsible = `
TASK [scheduled : Unpack the release] *****************************************
[ERROR]: Task failed: Module failed: Failed to find handler for "/tmp/x/devmachine_0.15.0_darwin_arm64.tar.gz".
Origin: /tmp/run/roles/scheduled/tasks/main.yml:75:7

fatal: [devmachine]: FAILED! => {"ansible_facts": {"discovered_interpreter_python": "/usr/bin/python3"}, "changed": false, "msg": "Failed to find handler for \"/tmp/x/devmachine_0.15.0_darwin_arm64.tar.gz\"."}

PLAY RECAP ********************************************************************
devmachine                 : ok=9    changed=1    unreachable=0    failed=1    skipped=2    rescued=0    ignored=0
`

func TestReadFailureNamesTheTaskAndItsMessage(t *testing.T) {
	for name, output := range map[string]string{"ansible-core 2.14": failedOnOldAnsible, "ansible-core 2.19": failedOnNewAnsible} {
		t.Run(name, func(t *testing.T) {
			got, ok := readFailure(output)
			if !ok {
				t.Fatal("no failure found")
			}
			if got.Task != "scheduled : Unpack the release" {
				t.Fatalf("task = %q", got.Task)
			}
			if !strings.HasPrefix(got.Message, `Failed to find handler for "/tmp/x/devmachine_0.15.0_darwin_arm64.tar.gz".`) {
				t.Fatalf("message = %q", got.Message)
			}
			if strings.Contains(got.Message, "\n") {
				t.Fatalf("message spans lines: %q", got.Message)
			}
		})
	}
}

func TestReadFailureSkipsAnIgnoredFailure(t *testing.T) {
	output := `
TASK [base : Read the version] ************************************************
fatal: [devmachine]: FAILED! => {"changed": false, "msg": "not there"}
...ignoring

TASK [git : Install git] ******************************************************
failed: [devmachine] (item=git) => {"ansible_loop_var": "item", "changed": false, "item": "git", "msg": "No package matching 'git'"}
`
	got, ok := readFailure(output)
	if !ok || got.Task != "git : Install git" || got.Message != "No package matching 'git'" {
		t.Fatalf("got = %+v ok = %v", got, ok)
	}
}

func TestReadFailureWithoutAFailure(t *testing.T) {
	if got, ok := readFailure(okRecap); ok {
		t.Fatalf("found a failure in a clean run: %+v", got)
	}
}

func TestReadFailureKeepsAMessageThatIsNotJSON(t *testing.T) {
	output := "TASK [x : y] ***\nfatal: [devmachine]: FAILED! => not json at all\n"
	got, ok := readFailure(output)
	if !ok || got.Message != "not json at all" {
		t.Fatalf("got = %+v ok = %v", got, ok)
	}
}

func TestApplyNamesTheFailedTask(t *testing.T) {
	c := &fakeClient{output: failedOnOldAnsible, err: errors.New("Process exited with status 2")}
	a := &Ansible{Client: c}

	_, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil), Options{Out: io.Discard})
	if err == nil {
		t.Fatal("a failed run was reported as success")
	}
	for _, want := range []string{"main", `"scheduled : Unpack the release"`, "GNU tar required"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error leaves out %q: %v", want, err)
		}
	}
}
