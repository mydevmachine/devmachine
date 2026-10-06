package widgets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

func TestEveryViewAcceptsAKnownProvider(t *testing.T) {
	c := CurrentContract()
	for name, view := range c.Views {
		for _, provider := range view.Accepts {
			if _, ok := c.Providers[provider]; !ok {
				t.Errorf("view %s accepts unknown provider %s", name, provider)
			}
		}
	}
}
