package dns

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Manual is the one Provider compiled into the binary.
//
// It makes no request: a registrar with no package still gets a useful
// answer, the exact record to create by hand, and `dns status` still says
// whether that record took effect.
type Manual struct {
	out io.Writer
	// because is the sentence that says why the record is done by hand, or
	// "" for the ordinary case: no installed provider holds the zone.
	because string
}

// NewManual returns a Manual that prints its instructions to out.
func NewManual(out io.Writer) *Manual {
	return &Manual{out: out}
}

// newManualBecause is a Manual that gives its own reason, for the cases
// where "no installed provider holds" the zone would be untrue.
func newManualBecause(out io.Writer, because string) *Manual {
	return &Manual{out: out, because: because}
}

// Reason is the sentence that says why a record for zone is done by hand.
func (m *Manual) Reason(zone string) string { return m.reason(zone) }

func (m *Manual) reason(zone string) string {
	if m.because != "" {
		return m.because
	}
	return fmt.Sprintf("No installed provider holds %s.", zone)
}

// Name is how manual is written in a --dns-provider flag.
func (m *Manual) Name() string { return ProviderManual }

// List cannot work: manual never speaks to a registrar, so it has nothing to
// read a zone from.
func (m *Manual) List(context.Context, string) ([]Record, error) {
	return nil, errors.New(
		"manual cannot list a zone it does not speak to. Check a name with `devmachine dns status`")
}

// Upsert prints the record to create by hand, how to check it took effect,
// and how to stop being manual for this registrar.
func (m *Manual) Upsert(_ context.Context, zone string, r Record) error {
	fmt.Fprintf(m.out, "%s Create this record by hand:\n\n", m.reason(zone))
	// The full name, never "@": with no provider the zone is only a guess,
	// and "@" read as the apex of the real zone points the wrong name.
	fmt.Fprintf(m.out, "  %s\t%s\t%s", fullName(r.Name, zone), r.Type, r.Value)
	if r.TTL > 0 {
		fmt.Fprintf(m.out, "\t%d", r.TTL)
	}
	fmt.Fprintln(m.out)
	fmt.Fprintf(m.out, "\nCheck it with `devmachine dns status %s`.\n", fullName(r.Name, zone))
	fmt.Fprintln(m.out,
		"Run `devmachine packages list` to see whether a provider package exists for this registrar,",
		"and `devmachine packages add <name>` to let the CLI do this instead.")
	return nil
}

// Append prints the record to add by hand, next to the values already there.
func (m *Manual) Append(_ context.Context, zone string, r Record) error {
	fmt.Fprintf(m.out, "%s Add this record by hand, and keep the values the name already holds:\n\n", m.reason(zone))
	fmt.Fprintf(m.out, "  %s\t%s\t%s", fullName(r.Name, zone), r.Type, r.Value)
	if r.TTL > 0 {
		fmt.Fprintf(m.out, "\t%d", r.TTL)
	}
	fmt.Fprintln(m.out)
	fmt.Fprintf(m.out, "\nCheck it with `devmachine dns status %s`.\n", fullName(r.Name, zone))
	return nil
}

// Delete prints the record to remove by hand.
func (m *Manual) Delete(_ context.Context, zone string, r Record) error {
	fmt.Fprintf(m.out, "%s Remove this record by hand:\n\n", m.reason(zone))
	fmt.Fprintf(m.out, "  %s\t%s\t%s\n", fullName(r.Name, zone), r.Type, r.Value)
	return nil
}

// fullName joins a record's label onto its zone, the way somebody would type
// it into `dns status`.
func fullName(label, zone string) string {
	if label == "" || label == "@" {
		return zone
	}
	return label + "." + zone
}
