package widgets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

func TestTheSidebarsHaveBoards(t *testing.T) {
	c := CurrentContract()
	for _, name := range []string{"sidebar", "context-sidebar"} {
		if s := c.Surfaces[name]; s.Status != StatusAvailable || s.Layout != LayoutStack {
			t.Errorf("%s is %+v", name, s)
		}
		if !IsStack(name) {
			t.Errorf("IsStack(%s) is false", name)
		}
	}
	if IsStack("home") {
		t.Error("IsStack(home) is true")
	}
	if c.StackRow != 40 || !slices.Equal(c.StackEntry.Forbids, []string{"frame", "z"}) {
		t.Fatalf("stack_row %d, stack_entry %+v", c.StackRow, c.StackEntry)
	}
}

func TestAProviderNeedsOnlyContextKeysASurfaceGives(t *testing.T) {
	c := CurrentContract()
	for name, p := range c.Providers {
		for key, value := range p.Context {
			if !slices.Contains(contextKeys(c), key) || value != ContextRequired {
				t.Errorf("provider %s needs context.%s %s", name, key, value)
			}
		}
	}
}

func TestOnlyViewsThatGrowTakeSizeAuto(t *testing.T) {
	if !Grows("app.todo") || Grows("app.clock") || Grows("text") || Grows("nope") {
		t.Fatal("Grows is wrong")
	}
}

func TestThePackageProviderRulesAreTheSpecs(t *testing.T) {
	pp := CurrentContract().PackageProvider
	if pp.Name != "<package>/<command>" || !slices.Equal(pp.Reserved, []string{"app"}) || pp.MinEvery != "5s" {
		t.Fatalf("got %+v", pp)
	}
	if !slices.Equal(pp.Returns, []string{"string", "number", "bool", "list", "object"}) || pp.Optional != "?" {
		t.Fatalf("returns %v optional %q", pp.Returns, pp.Optional)
	}
	if !slices.Equal(pp.Targets, []string{"machine", "workspace"}) || pp.Output != ParseJSON || pp.Approval != TrustThirdParty {
		t.Fatalf("got %+v", pp)
	}
	if !slices.Equal(pp.Trust, []string{TrustOfficial, TrustLocal, TrustThirdParty}) {
		t.Fatalf("trust %v", pp.Trust)
	}
}

func TestAProviderSourceTakesATargetAndATimeout(t *testing.T) {
	fields := CurrentContract().Sources[SourceProvider].Fields
	if fields["target"].Type != FieldTarget || fields["target"].Default != nil {
		t.Fatalf("target %+v", fields["target"])
	}
	if fields["timeout"].Default != "30s" || fields["timeout"].Max != "10m" || fields["every"].Type != FieldEvery {
		t.Fatalf("timeout %+v every %+v", fields["timeout"], fields["every"])
	}
}
