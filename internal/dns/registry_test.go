package dns

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubZoner answers with a fixed list, and counts how often it was asked.
type stubZoner struct {
	name  string
	zones []string
	err   error
	asked int
}

func (s *stubZoner) Name() string { return s.name }

func (s *stubZoner) List(context.Context, string) ([]Record, error) {
	return nil, errors.New("not used in these tests")
}

func (s *stubZoner) Upsert(context.Context, string, Record) error {
	return errors.New("not used in these tests")
}

func (s *stubZoner) Append(context.Context, string, Record) error {
	return errors.New("not used in these tests")
}

func (s *stubZoner) Delete(context.Context, string, Record) error {
	return errors.New("not used in these tests")
}

func (s *stubZoner) Zones(context.Context) ([]string, error) {
	s.asked++
	if s.err != nil {
		return nil, s.err
	}
	return s.zones, nil
}

func TestWhoHoldsPicksTheProviderWithTheZone(t *testing.T) {
	hostinger := &stubZoner{name: "hostinger", zones: []string{"example.com"}}
	cloudflare := &stubZoner{name: "cloudflare", zones: []string{"client.example.net"}}

	got, err := WhoHolds(context.Background(), []Zoner{hostinger, cloudflare}, "app.client.example.net")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "cloudflare" || got.Zone != "client.example.net" {
		t.Fatalf("got %#v", got)
	}
}

func TestWhoHoldsPrefersTheLongestZone(t *testing.T) {
	// Both match by suffix. Picking the shorter one writes the record into
	// the wrong registrar's account, which is the mistake this exists to stop.
	one := &stubZoner{name: "hostinger", zones: []string{"example.com"}}
	two := &stubZoner{name: "cloudflare", zones: []string{"client.example.com"}}

	got, err := WhoHolds(context.Background(), []Zoner{one, two}, "app.client.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Zone != "client.example.com" {
		t.Fatalf("got %#v", got)
	}
}

func TestWhoHoldsMatchesTheZoneItself(t *testing.T) {
	p := &stubZoner{name: "hostinger", zones: []string{"example.com"}}
	got, err := WhoHolds(context.Background(), []Zoner{p}, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Zone != "example.com" {
		t.Fatalf("got %#v", got)
	}
}

func TestWhoHoldsIgnoresACoincidentalSuffix(t *testing.T) {
	p := &stubZoner{name: "hostinger", zones: []string{"example.com"}}
	if _, err := WhoHolds(context.Background(), []Zoner{p}, "notexample.com"); !errors.Is(err, ErrNoHolder) {
		t.Fatalf("got %v, want ErrNoHolder", err)
	}
}

func TestWhoHoldsRefusesToGuessWhenTwoClaimTheSameZone(t *testing.T) {
	one := &stubZoner{name: "hostinger", zones: []string{"example.com"}}
	two := &stubZoner{name: "cloudflare", zones: []string{"example.com"}}

	_, err := WhoHolds(context.Background(), []Zoner{one, two}, "www.example.com")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("got %v, want ErrAmbiguous", err)
	}
	for _, want := range []string{"hostinger", "cloudflare", "--dns-provider"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error leaves out %q: %v", want, err)
		}
	}
}

func TestWhoHoldsAsksEachProviderOnce(t *testing.T) {
	one := &stubZoner{name: "hostinger", zones: []string{"example.com"}}
	two := &stubZoner{name: "cloudflare", zones: []string{"client.example.net"}}

	if _, err := WhoHolds(context.Background(), []Zoner{one, two}, "www.example.com"); err != nil {
		t.Fatal(err)
	}
	if one.asked != 1 || two.asked != 1 {
		t.Fatalf("asked %d and %d times", one.asked, two.asked)
	}
}

func TestWhoHoldsKeepsGoingWhenOneProviderIsBroken(t *testing.T) {
	// One registrar being down, or one token being stale, must not stop a
	// command that was going to the other one anyway.
	broken := &stubZoner{name: "hostinger", err: errors.New("the token was rejected")}
	working := &stubZoner{name: "cloudflare", zones: []string{"client.example.net"}}

	got, err := WhoHolds(context.Background(), []Zoner{broken, working}, "app.client.example.net")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "cloudflare" {
		t.Fatalf("got %#v", got)
	}
}

func TestWhoHoldsReportsEveryFailureWhenNobodyHoldsIt(t *testing.T) {
	broken := &stubZoner{name: "hostinger", err: errors.New("the token was rejected")}

	_, err := WhoHolds(context.Background(), []Zoner{broken}, "www.example.com")
	if !errors.Is(err, ErrNoHolder) {
		t.Fatalf("got %v", err)
	}
	// Falling through to manual because a token expired, without saying so,
	// is how somebody spends an afternoon.
	if !strings.Contains(err.Error(), "the token was rejected") {
		t.Fatalf("the error hides why: %v", err)
	}
}
