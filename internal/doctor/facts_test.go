package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/remote"
)

func TestDoctorKeepsWhatItReadAboutTheMachine(t *testing.T) {
	dir := configDir(t, machineWith)
	client := working("")
	client.out[facts.ObserveCommand] = "kernel=Linux\nmachine=x86_64\n" +
		"ansible_playbook=/usr/bin/ansible-playbook\nos-release.ID=arch\n"

	Run(context.Background(), dir, "", dialling(client), nil)

	got, found, err := facts.Load(dir, "main")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got.Distribution != "Archlinux" || got.PkgMgr != "pacman" || got.AnsiblePlaybook != "/usr/bin/ansible-playbook" {
		t.Fatalf("%#v", got)
	}
}

func TestDoctorKeepsNothingForAMachineItCouldNotReach(t *testing.T) {
	dir := configDir(t, machineWith)
	Run(context.Background(), dir, "", func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nil, "", errors.New("no route to host")
	}, nil)

	if _, found, _ := facts.Load(dir, "main"); found {
		t.Fatal("facts were written for a machine nobody reached")
	}
}
