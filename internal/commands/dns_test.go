package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/dns"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// configDir is a plain, one-machine configuration: the ordinary starting
// point for a DNS command test.
func configDir(t *testing.T) string {
	t.Helper()
	return configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
}

// fakeZoner is a dnsZoner a test builds by hand, for `dns providers`.
type fakeZoner struct {
	name  string
	zones []string
	err   error
}

func (f fakeZoner) Name() string { return f.name }

func (f fakeZoner) Zones(context.Context) ([]string, error) { return f.zones, f.err }

// stubInstalled replaces dnsInstalled with one that answers with these
// providers, and dial with one that never really connects.
func stubInstalled(providers []fakeZoner) func() {
	origInstalled, origDial := dnsInstalled, dial
	dnsInstalled = func(string, string, string, remote.Client) ([]dnsZoner, error) {
		out := make([]dnsZoner, len(providers))
		for i, p := range providers {
			out[i] = p
		}
		return out, nil
	}
	dial = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return fakeRemote{}, "203.0.113.10", nil
	}
	return func() { dnsInstalled = origInstalled; dial = origDial }
}

func TestDNSProvidersListsEachOneAndItsZones(t *testing.T) {
	defer stubInstalled([]fakeZoner{
		{name: "hostinger", zones: []string{"example.com"}},
		{name: "cloudflare", zones: []string{"client.example.net"}},
	})()

	out, err := execute(t, "--config", configDir(t), "--format", "json", "dns", "providers")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Providers []struct {
			Name  string   `json:"name"`
			Zones []string `json:"zones"`
			Error string   `json:"error,omitempty"`
		} `json:"providers"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got.Providers) != 2 || got.Providers[1].Zones[0] != "client.example.net" {
		t.Fatalf("got %#v", got.Providers)
	}
}

func TestDNSProvidersReportsOneThatCannotBeAsked(t *testing.T) {
	defer stubInstalled([]fakeZoner{
		{name: "hostinger", err: errors.New("the token was rejected")},
	})()

	out, err := execute(t, "--config", configDir(t), "dns", "providers")
	// A broken provider is what this command exists to show, so listing is
	// still a success.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "the token was rejected") {
		t.Fatalf("got %q", out)
	}
}

func TestDNSProvidersSaysWhenNoneAreInstalled(t *testing.T) {
	defer stubInstalled(nil)()

	out, err := execute(t, "--config", configDir(t), "dns", "providers")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "packages list") {
		t.Fatalf("it does not say where to find one: %q", out)
	}
}

// configWithDomain is a one-machine configuration with a `domain` set, for a
// command that falls back to it when no zone or host was given.
func configWithDomain(t *testing.T, domain string) string {
	t.Helper()
	return configWith(t, fmt.Sprintf("machines:\n  - name: main\n    hosts: [203.0.113.10]\ndomain: %s\n", domain))
}

// fake is a dns.Provider a test drives by hand, recording what was written to
// it so a test can assert on the exact call.
type fake struct {
	records    []dns.Record
	listErr    error
	upserts    int
	appends    int
	lastRecord dns.Record
	lastZone   string
}

func (f *fake) Name() string { return "fake" }

func (f *fake) List(context.Context, string) ([]dns.Record, error) { return f.records, f.listErr }

func (f *fake) Upsert(_ context.Context, zone string, r dns.Record) error {
	f.upserts++
	f.lastRecord, f.lastZone = r, zone
	return nil
}

func (f *fake) Append(_ context.Context, zone string, r dns.Record) error {
	f.appends++
	f.lastRecord, f.lastZone = r, zone
	return nil
}

func (f *fake) Delete(_ context.Context, zone string, r dns.Record) error {
	f.lastRecord, f.lastZone = r, zone
	return nil
}

// stubChoose replaces chooseDNS with one that always resolves to provider p,
// holding zone, without dialling anything.
func stubChoose(provider, zone string, p dns.Provider) func() {
	orig := chooseDNS
	chooseDNS = func(context.Context, *options, string, string, string, io.Writer) (dns.Choice, target, error) {
		return dns.Choice{Provider: p, Name: provider, Zone: zone, Why: fmt.Sprintf("%s holds %s", provider, zone)},
			target{machine: config.Machine{Name: "main"}}, nil
	}
	return func() { chooseDNS = orig }
}

// stubChooseError replaces chooseDNS with one that always fails, for a
// command that has to stop before touching a provider.
func stubChooseError(err error) func() {
	orig := chooseDNS
	chooseDNS = func(context.Context, *options, string, string, string, io.Writer) (dns.Choice, target, error) {
		return dns.Choice{}, target{}, err
	}
	return func() { chooseDNS = orig }
}

func TestDNSListPrintsARecordPerLine(t *testing.T) {
	defer stubChoose("hostinger", "example.com", &fake{records: []dns.Record{
		{Name: "www", Type: "A", Value: "198.51.100.10", TTL: 3600},
	}})()

	out, err := execute(t, "--config", configDir(t), "dns", "list", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "www") || !strings.Contains(out, "198.51.100.10") {
		t.Fatalf("got %q", out)
	}
}

func TestDNSListJSONSaysWhichZoneAndProvider(t *testing.T) {
	defer stubChoose("cloudflare", "client.example.net", &fake{
		records: []dns.Record{{Name: "www", Type: "A", Value: "198.51.100.10"}},
	})()

	out, _, err := executeSplit(t, "--config", configDir(t), "dns", "list", "client.example.net", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Zone     string       `json:"zone"`
		Provider string       `json:"provider"`
		Records  []dns.Record `json:"records"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Zone != "client.example.net" || got.Provider != "cloudflare" {
		t.Fatalf("got %#v", got)
	}
}

func TestDNSListFallsBackToTheConfiguredDomain(t *testing.T) {
	defer stubChoose("hostinger", "example.com", &fake{})()

	if _, err := execute(t, "--config", configWithDomain(t, "example.com"), "dns", "list"); err != nil {
		t.Fatal(err)
	}
}

func TestDNSListSaysWhichProviderItUsedOnStderr(t *testing.T) {
	defer stubChoose("hostinger", "example.com", &fake{})()

	_, errOut, err := executeSplit(t, "--config", configDir(t), "dns", "list", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	// stdout is data; which provider answered and why is diagnostics.
	if !strings.Contains(errOut, "hostinger") {
		t.Fatalf("stderr does not say what answered: %q", errOut)
	}
}

func TestDNSCheckFindsARecordThatIsThere(t *testing.T) {
	defer stubChoose("cloudflare", "client.example.net", &fake{records: []dns.Record{
		{Name: "app", Type: "A", Value: "198.51.100.10", TTL: 3600},
	}})()

	out, _, err := executeSplit(t, "--config", configDir(t), "dns", "check", "app.client.example.net", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Name     string       `json:"name"`
		Zone     string       `json:"zone"`
		Provider string       `json:"provider"`
		Found    bool         `json:"found"`
		Records  []dns.Record `json:"records"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !got.Found || got.Records[0].Value != "198.51.100.10" {
		t.Fatalf("got %#v", got)
	}
}

func TestDNSCheckExitsNonZeroWhenItIsNotPointed(t *testing.T) {
	defer stubChoose("cloudflare", "client.example.net", &fake{})()

	// A check is asked by a script and by a skill. "Not there" has to be
	// readable from the exit code, not only from the words.
	if _, err := execute(t, "--config", configDir(t), "dns", "check", "app.client.example.net"); err == nil {
		t.Fatal("a name that is not pointed exited 0")
	}
}

func TestDNSAddWritesNothingUnderCheck(t *testing.T) {
	p := &fake{}
	defer stubChoose("hostinger", "example.com", p)()

	out, err := execute(t, "--config", configDir(t),
		"dns", "add", "www.example.com", "A", "198.51.100.10", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if p.upserts != 0 {
		t.Fatalf("--check wrote %d record(s)", p.upserts)
	}
	if !strings.Contains(out, "would") {
		t.Fatalf("a dry run must say what it would do: %q", out)
	}
}

func TestDNSAddNamesTheZoneAndProviderInTheQuestion(t *testing.T) {
	p := &fake{}
	defer stubChoose("cloudflare", "client.example.net", p)()

	out, _ := executeWithInput(t, "n\n", "--config", configDir(t),
		"dns", "add", "app.client.example.net", "A", "198.51.100.10")

	// Writing the right record into the wrong account is the mistake this
	// command can make. The question is the last place to catch it.
	for _, want := range []string{"client.example.net", "cloudflare"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the question leaves out %q: %q", want, out)
		}
	}
	if p.upserts != 0 {
		t.Fatal("it wrote after a refusal")
	}
}

func TestDNSAddYesDoesNotPublish(t *testing.T) {
	p := &fake{}
	defer stubChoose("hostinger", "example.com", p)()

	out, err := execute(t, "--config", configDir(t),
		"dns", "add", "www.example.com", "A", "198.51.100.10", "--yes")
	if !errors.Is(err, errDeclined) {
		t.Fatalf("dns add --yes returned %v, want a declined publication", err)
	}
	if p.upserts != 0 {
		t.Fatalf("--yes published %d record(s)", p.upserts)
	}
	if !strings.Contains(out, "[y/N]") {
		t.Fatalf("--yes skipped the publication question: %q", out)
	}
}

func TestDNSAddPublishIsExplicitNonInteractiveConsent(t *testing.T) {
	p := &fake{}
	defer stubChoose("hostinger", "example.com", p)()

	out, err := execute(t, "--config", configDir(t),
		"dns", "add", "www.example.com", "A", "198.51.100.10", "--publish")
	if err != nil {
		t.Fatal(err)
	}
	if p.upserts != 1 {
		t.Fatalf("--publish wrote %d record(s), want 1", p.upserts)
	}
	if strings.Contains(out, "[y/N]") {
		t.Fatalf("--publish asked for confirmation: %q", out)
	}
}

func TestDNSAddSendsTheLabelNotTheFullName(t *testing.T) {
	p := &fake{}
	defer stubChoose("cloudflare", "client.example.net", p)()

	if _, err := execute(t, "--config", configDir(t),
		"dns", "add", "app.client.example.net", "A", "198.51.100.10", "--publish"); err != nil {
		t.Fatal(err)
	}
	// The provider's model is a label plus a zone. Sending the full name
	// would create app.client.example.net.client.example.net.
	if p.lastRecord.Name != "app" {
		t.Fatalf("wrote name %q, want %q", p.lastRecord.Name, "app")
	}
	if p.lastZone != "client.example.net" {
		t.Fatalf("wrote into zone %q", p.lastZone)
	}
}

func TestDNSAddWritesTheApexAsAtSign(t *testing.T) {
	p := &fake{}
	defer stubChoose("hostinger", "example.com", p)()

	if _, err := execute(t, "--config", configDir(t),
		"dns", "add", "example.com", "A", "198.51.100.10", "--publish"); err != nil {
		t.Fatal(err)
	}
	if p.lastRecord.Name != "@" {
		t.Fatalf("wrote name %q, want %q", p.lastRecord.Name, "@")
	}
}

func TestDNSAddRefusesAnUnsupportedTypeBeforeChoosing(t *testing.T) {
	p := &fake{}
	defer stubChoose("hostinger", "example.com", p)()

	_, err := execute(t, "--config", configDir(t),
		"dns", "add", "example.com", "SOA", "ns1 hostmaster 1 2 3 4 5", "--yes")
	if !errors.Is(err, dns.ErrUnsupportedType) {
		t.Fatalf("got %v", err)
	}
}

func TestDNSAddWritesAnMXInZoneFileForm(t *testing.T) {
	p := &fake{}
	defer stubChoose("hostinger", "example.com", p)()

	if _, err := execute(t, "--config", configDir(t),
		"dns", "add", "example.com", "mx", "10 Mail.Example.com.", "--publish"); err != nil {
		t.Fatal(err)
	}
	if p.lastRecord.Type != "MX" || p.lastRecord.Value != "10 mail.example.com" {
		t.Fatalf("wrote %#v", p.lastRecord)
	}
}

func TestDNSAddRefusesAValueThatDoesNotFitItsTypeBeforeWriting(t *testing.T) {
	p := &fake{}
	defer stubChoose("hostinger", "example.com", p)()

	_, err := execute(t, "--config", configDir(t),
		"dns", "add", "example.com", "MX", "mail.example.com", "--publish")
	if !errors.Is(err, dns.ErrBadValue) {
		t.Fatalf("got %v", err)
	}
	if p.upserts != 0 {
		t.Fatal("it wrote a value that does not fit")
	}
}

func TestDNSAddTTLReachesTheProvider(t *testing.T) {
	p := &fake{}
	defer stubChoose("hostinger", "example.com", p)()

	if _, err := execute(t, "--config", configDir(t),
		"dns", "add", "www.example.com", "A", "198.51.100.10", "--ttl", "300", "--publish"); err != nil {
		t.Fatal(err)
	}
	if p.lastRecord.TTL != 300 {
		t.Fatalf("wrote ttl %d, want 300", p.lastRecord.TTL)
	}
}

func TestDNSAddAppendKeepsTheValuesTheNameHolds(t *testing.T) {
	p := &fake{records: []dns.Record{{Name: "@", Type: "MX", Value: "10 mail.example.com"}}}
	defer stubChoose("hostinger", "example.com", p)()

	out, err := execute(t, "--config", configDir(t),
		"dns", "add", "example.com", "MX", "20 mail2.example.com", "--append", "--publish")
	if err != nil {
		t.Fatal(err)
	}
	if p.appends != 1 || p.upserts != 0 {
		t.Fatalf("appends %d, upserts %d; want one append and no upsert", p.appends, p.upserts)
	}
	// The line says what stays, so a person can see the name ends up with
	// both values rather than wondering whether the first one survived.
	if !strings.Contains(out, "next to 10 mail.example.com") {
		t.Fatalf("it does not say what it keeps: %q", out)
	}
}

func TestDNSAddAppendUnderCheckWritesNothing(t *testing.T) {
	p := &fake{records: []dns.Record{{Name: "@", Type: "MX", Value: "10 mail.example.com"}}}
	defer stubChoose("hostinger", "example.com", p)()

	out, err := execute(t, "--config", configDir(t),
		"dns", "add", "example.com", "MX", "20 mail2.example.com", "--append", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if p.appends+p.upserts != 0 {
		t.Fatal("--check wrote")
	}
	if !strings.Contains(out, "would add") {
		t.Fatalf("got %q", out)
	}
}

func TestDNSAddSaysWhatItReplacesWithoutAppend(t *testing.T) {
	p := &fake{records: []dns.Record{{Name: "@", Type: "MX", Value: "10 mail.example.com"}}}
	defer stubChoose("hostinger", "example.com", p)()

	out, err := execute(t, "--config", configDir(t),
		"dns", "add", "example.com", "MX", "20 mail2.example.com", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "replacing 10 mail.example.com") {
		t.Fatalf("it does not name the value about to be lost: %q", out)
	}
}

func TestDNSAddSeesAValueSpeltDifferentlyAsTheSameRecord(t *testing.T) {
	p := &fake{records: []dns.Record{{Name: "@", Type: "MX", Value: "10 Mail.Example.com."}}}
	defer stubChoose("hostinger", "example.com", p)()

	out, err := execute(t, "--config", configDir(t),
		"dns", "add", "example.com", "MX", "10 mail.example.com", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "replacing") {
		t.Fatalf("the registrar's spelling of the same value was taken for another one: %q", out)
	}
}

func TestDNSListShowsEveryType(t *testing.T) {
	defer stubChoose("hostinger", "example.com", &fake{records: []dns.Record{
		{Name: "@", Type: "MX", Value: "10 mail.example.com", TTL: 3600},
		{Name: "_sip._tcp", Type: "SRV", Value: "10 5 5060 sip.example.com", TTL: 3600},
	}})()

	out, err := execute(t, "--config", configDir(t), "dns", "list", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"MX", "10 mail.example.com", "SRV", "10 5 5060 sip.example.com"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the list leaves out %q: %q", want, out)
		}
	}
}

func TestDNSRmRemovesAValueSpeltDifferently(t *testing.T) {
	p := &fake{records: []dns.Record{{Name: "@", Type: "MX", Value: "10 mail.example.com"}}}
	defer stubChoose("hostinger", "example.com", p)()

	if _, err := execute(t, "--config", configDir(t),
		"dns", "rm", "example.com", "MX", "10 Mail.Example.com.", "--yes"); err != nil {
		t.Fatal(err)
	}
	if p.lastRecord.Value != "10 mail.example.com" {
		t.Fatalf("removed %q, want the canonical spelling", p.lastRecord.Value)
	}
}

func TestDNSAddStopsWhenTwoProvidersClaimTheZone(t *testing.T) {
	defer stubChooseError(fmt.Errorf("%w: example.com is held by hostinger and cloudflare. Say which with --dns-provider",
		dns.ErrAmbiguous))()

	_, err := execute(t, "--config", configDir(t),
		"dns", "add", "www.example.com", "A", "198.51.100.10", "--yes")
	if !errors.Is(err, dns.ErrAmbiguous) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "--dns-provider") {
		t.Fatalf("the error does not say how to resolve it: %v", err)
	}
}

func TestDNSRmWarnsBeforeTouchingANameWithSeveralValues(t *testing.T) {
	defer stubChoose("hostinger", "example.com", &fake{records: []dns.Record{
		{Name: "www", Type: "A", Value: "198.51.100.10"},
		{Name: "www", Type: "A", Value: "198.51.100.11"},
	}})()

	out, err := execute(t, "--config", configDir(t),
		"dns", "rm", "www.example.com", "A", "198.51.100.10", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not atomic") {
		t.Fatalf("no warning about the non-atomic path: %q", out)
	}
}

func TestDNSRmRemovesExactlyTheValueGiven(t *testing.T) {
	p := &fake{records: []dns.Record{{Name: "www", Type: "A", Value: "198.51.100.10"}}}
	defer stubChoose("hostinger", "example.com", p)()

	if _, err := execute(t, "--config", configDir(t),
		"dns", "rm", "www.example.com", "A", "198.51.100.10", "--yes"); err != nil {
		t.Fatal(err)
	}
	if p.lastRecord.Value != "198.51.100.10" || p.lastZone != "example.com" {
		t.Fatalf("got %#v in %q", p.lastRecord, p.lastZone)
	}
}

func TestDNSRmWithNoValueRemovesTheWholeSet(t *testing.T) {
	p := &fake{}
	defer stubChoose("hostinger", "example.com", p)()

	if _, err := execute(t, "--config", configDir(t),
		"dns", "rm", "www.example.com", "A", "--yes"); err != nil {
		t.Fatal(err)
	}
	if p.lastRecord.Value != "" {
		t.Fatalf("got %#v", p.lastRecord)
	}
}
