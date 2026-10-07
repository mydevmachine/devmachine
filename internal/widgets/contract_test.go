package widgets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestContractMatchesTheGoldenFile(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("testdata", "contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	ours, err := json.Marshal(CurrentContract())
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(golden, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(ours, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("the contract drifted from testdata/contract.json, which the app vendors\nwant %v\ngot  %v", want, got)
	}
}

func TestContractIsAFreshCopyEachTime(t *testing.T) {
	first := CurrentContract()
	first.Views["app.clock"] = View{Accepts: []string{"changed"}}
	if got := CurrentContract().Views["app.clock"].Accepts[0]; got != "app/clock" {
		t.Fatalf("got %q", got)
	}
}

func TestEveryViewAcceptsSomethingASourceGives(t *testing.T) {
	c := CurrentContract()
	outputs := map[string]bool{ParseText: true}
	for _, kind := range []string{SourceCommand, SourceURL} {
		for _, value := range c.Sources[kind].Fields["parse"].Values {
			outputs[value] = true
		}
	}
	for name, view := range c.Views {
		for _, accepted := range view.Accepts {
			kind, byKind := strings.CutPrefix(accepted, AcceptsKind)
			_, isProvider := c.Providers[accepted]
			_, isKind := c.Sources[kind]
			if isProvider || outputs[accepted] || (byKind && isKind) {
				continue
			}
			t.Errorf("view %s accepts %s, which no source gives", name, accepted)
		}
	}
}

func TestEverySourceKindHasItsFields(t *testing.T) {
	c := CurrentContract()
	if len(c.Sources) != len(c.SourceKinds) {
		t.Fatalf("source_kinds %v and sources %v differ", c.SourceKinds, sortedKeys(c.Sources))
	}
	for _, kind := range c.SourceKinds {
		if len(c.Sources[kind].Fields) == 0 {
			t.Errorf("source kind %s has no fields", kind)
		}
	}
}
