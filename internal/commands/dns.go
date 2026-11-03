package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/dns"
	"github.com/mydevmachine/devmachine/internal/provision"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/spf13/cobra"
)

func newDNSCmd(opts *options) *cobra.Command {
	var providerFlag, zoneFlag string

	cmd := &cobra.Command{
		Use:   "dns",
		Short: "DNS for the names this setup serves",
	}
	cmd.PersistentFlags().StringVar(&providerFlag, "dns-provider", "",
		"act through this provider instead of asking which installed one holds the zone")
	cmd.PersistentFlags().StringVar(&zoneFlag, "zone", "",
		"the zone to act in, for a provider whose token cannot list it")

	cmd.AddCommand(newDNSStatusCmd(opts))
	cmd.AddCommand(newDNSProvidersCmd(opts))
	cmd.AddCommand(newDNSListCmd(opts, &providerFlag, &zoneFlag))
	cmd.AddCommand(newDNSCheckCmd(opts, &providerFlag, &zoneFlag))
	cmd.AddCommand(newDNSAddCmd(opts, &providerFlag, &zoneFlag))
	cmd.AddCommand(newDNSRmCmd(opts, &providerFlag, &zoneFlag))
	return cmd
}

func newDNSStatusCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status [host]",
		Short: "Check a name from outside: DNS, TLS and one request",
		Long: "With no host it checks the configured domain. The check is made " +
			"from your computer, not from the machine, so it sees what anybody " +
			"else would see.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host := ""
			if len(args) == 1 {
				host = args[0]
			}
			if host == "" {
				cfg, err := loadConfig(opts)
				if err != nil {
					return err
				}
				if cfg.Domain == "" {
					return errors.New("no host given and no `domain` in the configuration")
				}
				host = cfg.Domain
			}

			report := dns.Status(cmd.Context(), host)

			if opts.format == formatJSON {
				if err := writeJSON(cmd.OutOrStdout(), struct {
					dns.Report
					OK bool `json:"ok"`
				}{report, report.OK()}); err != nil {
					return err
				}
			} else {
				cmd.Printf("%s\n", report.Host)
				for _, s := range report.Steps {
					cmd.Printf("  %-4s  %-5s  %s\n", s.Status, s.Name, s.Detail)
				}
			}

			if !report.OK() {
				return errors.New("the name is not serving")
			}
			return nil
		},
	}
}

// dnsZoner is what `dns providers` needs from an installed provider: enough
// to say what it holds, without depending on dns.External directly.
type dnsZoner interface {
	Name() string
	Zones(ctx context.Context) ([]string, error)
}

// dnsInstalled is the seam a test replaces so `dns providers` never dials a
// machine for real. It wraps dns.Installed behind an interface so a test can
// hand it a stub provider instead of a real dns.External.
var dnsInstalled = func(dir, machine, base string, client remote.Client) ([]dnsZoner, error) {
	providers, err := dns.Installed(dir, machine, base, client)
	if err != nil {
		return nil, err
	}
	out := make([]dnsZoner, len(providers))
	for i, p := range providers {
		out[i] = p
	}
	return out, nil
}

// providerRow is one installed provider, as `dns providers` reports it.
type providerRow struct {
	Name  string   `json:"name"`
	Zones []string `json:"zones"`
	Error string   `json:"error,omitempty"`
}

func newDNSProvidersCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "providers",
		Short: "Every installed DNS provider, and the zones it can see",
		Long: "The one read that shows the whole picture, and the first thing " +
			"to run when a DNS command did not do what was expected.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tgt, err := machineTarget(opts)
			if err != nil {
				return err
			}
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			client, _, err := dialAdmin(cmd.Context(), tgt.machine)
			if err != nil {
				return err
			}
			defer client.Close()

			base, err := provision.Base(tgt.machine)
			if err != nil {
				return err
			}
			providers, err := dnsInstalled(dir, tgt.machine.Name, base, client)
			if err != nil {
				return err
			}

			rows := make([]providerRow, 0, len(providers))
			for _, p := range providers {
				row := providerRow{Name: p.Name()}
				zones, zerr := p.Zones(cmd.Context())
				if zerr != nil {
					row.Error = zerr.Error()
				} else {
					row.Zones = zones
				}
				rows = append(rows, row)
			}

			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Providers []providerRow `json:"providers"`
				}{rows})
			}

			if len(rows) == 0 {
				cmd.Println("No DNS provider is installed. Run `devmachine packages list` to see what is available.")
				return nil
			}
			for _, row := range rows {
				if row.Error != "" {
					cmd.Printf("%-14s %s\n", row.Name, row.Error)
					continue
				}
				cmd.Printf("%-14s %s\n", row.Name, strings.Join(row.Zones, ", "))
			}
			return nil
		},
	}
}

// chooseDNS resolves a name to the provider that would act on it, dialling
// the machine along the way. It is a seam: a test replaces it to drive
// dns list/check/add/rm without a machine to reach.
var chooseDNS = defaultChooseDNS

func defaultChooseDNS(ctx context.Context, opts *options, name, providerFlag, zoneFlag string, out io.Writer,
) (dns.Choice, target, error) {
	tgt, err := machineTarget(opts)
	if err != nil {
		return dns.Choice{}, target{}, err
	}
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return dns.Choice{}, target{}, err
	}

	client, _, err := dialAdmin(ctx, tgt.machine)
	if err != nil {
		// A DNS command with the machine down still has manual to fall back
		// to: dns.Choose treats a nil client as "nothing to ask", not as a
		// reason to stop.
		client = nil
	}

	base, err := provision.Base(tgt.machine)
	if err != nil {
		return dns.Choice{}, target{}, err
	}
	choice, err := dns.Choose(ctx, dir, tgt.machine.Name, base, name, providerFlag, client, out)
	if err != nil {
		return dns.Choice{}, target{}, err
	}
	if zoneFlag != "" {
		choice.Zone = zoneFlag
	}
	return choice, tgt, nil
}

// announce prints which provider answered and why, always on stderr: stdout
// is data, and which provider was asked is diagnostics.
func announce(w io.Writer, choice dns.Choice) {
	fmt.Fprintf(w, "%s: %s\n", choice.Name, choice.Why)
}

// labelFor turns a full name into the label a provider's model expects: the
// zone itself becomes "@", and anything else has the zone stripped off.
func labelFor(name, zone string) string {
	if name == zone {
		return "@"
	}
	return strings.TrimSuffix(name, "."+zone)
}

func newDNSListCmd(opts *options, providerFlag, zoneFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list [zone]",
		Short: "Every record a provider holds for a zone",
		Long: "This is a different question from `dns status`: it asks the " +
			"registrar, not the public internet, so it sees a record that has " +
			"not propagated yet and says nothing about one that has.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			zone := ""
			if len(args) == 1 {
				zone = args[0]
			}
			if zone == "" {
				cfg, err := loadConfig(opts)
				if err != nil {
					return err
				}
				if cfg.Domain == "" {
					return errors.New("no zone given and no `domain` in the configuration")
				}
				zone = cfg.Domain
			}

			choice, _, err := chooseDNS(cmd.Context(), opts, zone, *providerFlag, *zoneFlag, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			announce(cmd.ErrOrStderr(), choice)

			records, err := choice.Provider.List(cmd.Context(), choice.Zone)
			if err != nil {
				return err
			}

			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Zone     string       `json:"zone"`
					Provider string       `json:"provider"`
					Records  []dns.Record `json:"records"`
				}{choice.Zone, choice.Name, records})
			}
			for _, r := range records {
				cmd.Printf("%-16s %-6s %-24s %d\n", r.Name, r.Type, r.Value, r.TTL)
			}
			return nil
		},
	}
}

func newDNSCheckCmd(opts *options, providerFlag, zoneFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "check <name>",
		Short: "Whether a name is pointed at the registrar",
		Long: "`dns status` asks the public internet, which can still say no " +
			"while the record is sitting right there, not yet propagated. This " +
			"asks the registrar directly.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			choice, _, err := chooseDNS(cmd.Context(), opts, name, *providerFlag, *zoneFlag, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			announce(cmd.ErrOrStderr(), choice)

			records, err := choice.Provider.List(cmd.Context(), choice.Zone)
			if err != nil {
				return err
			}

			label := labelFor(name, choice.Zone)
			var matched []dns.Record
			for _, r := range records {
				if r.Name == label {
					matched = append(matched, r)
				}
			}
			found := len(matched) > 0

			if opts.format == formatJSON {
				if err := writeJSON(cmd.OutOrStdout(), struct {
					Name     string       `json:"name"`
					Zone     string       `json:"zone"`
					Provider string       `json:"provider"`
					Found    bool         `json:"found"`
					Records  []dns.Record `json:"records"`
				}{name, choice.Zone, choice.Name, found, matched}); err != nil {
					return err
				}
			} else if found {
				cmd.Printf("%s is pointed, through %s:\n", name, choice.Name)
				for _, r := range matched {
					cmd.Printf("  %-6s %-24s %d\n", r.Type, r.Value, r.TTL)
				}
			} else {
				cmd.Printf("%s is not pointed, through %s\n", name, choice.Name)
			}

			if !found {
				return fmt.Errorf("%s is not pointed at %s", name, choice.Name)
			}
			return nil
		},
	}
}

// describeWrite says what `dns add` would do: the label, the zone, the
// provider and, without --append, whatever value is about to be replaced.
// Naming what is lost is the last chance to catch the record going into the
// wrong account; naming what stays, with --append, shows the name ends up
// holding both.
func describeWrite(ctx context.Context, choice dns.Choice, rec dns.Record, appending bool) string {
	var others []string
	if existing, err := choice.Provider.List(ctx, choice.Zone); err == nil {
		for _, r := range existing {
			if r.Name == rec.Name && r.Type == rec.Type && holdsAnotherValue(r, rec.Value) {
				others = append(others, r.Value)
			}
		}
	}
	verb, tail := "set", ", replacing %s"
	if appending {
		verb, tail = "add", ", next to %s"
	}
	line := fmt.Sprintf("%s %s %s %s in %s (provider %s)",
		verb, rec.Name, rec.Type, rec.Value, choice.Zone, choice.Name)
	if rec.TTL > 0 {
		line += fmt.Sprintf(" ttl %d", rec.TTL)
	}
	if len(others) > 0 {
		line += fmt.Sprintf(tail, strings.Join(others, ", "))
	}
	return line
}

// holdsAnotherValue says whether a listed record holds something other than
// value, spelt either way: a registrar may list `10 Mail.example.com.` for the
// `10 mail.example.com` that is about to be written, and that is the same
// record, not one about to be replaced.
func holdsAnotherValue(r dns.Record, value string) bool {
	listed, err := dns.CanonicalValue(r.Type, r.Value)
	if err != nil {
		listed = r.Value
	}
	return listed != value
}

func newDNSAddCmd(opts *options, providerFlag, zoneFlag *string) *cobra.Command {
	var check, yes, publish, appending bool
	var ttl int

	c := &cobra.Command{
		Use:   "add <name> <type> <value>",
		Short: "Make a name hold this value, alone or next to the values it has",
		Long: "Without --append it replaces whatever is already at this name and " +
			"type — it does not add to it. The question names the zone, the " +
			"provider and whatever value it is about to replace, because writing " +
			"the right record into the wrong account is the mistake this command " +
			"can make.\n\n" +
			"A value is written the way a zone file writes it: `10 mail.example.com` " +
			"for MX, `10 5 5060 sip.example.com` for SRV, `0 issue \"ca.example.net\"` " +
			"for CAA. Types: A, AAAA, CNAME, TXT, MX, SRV, NS, CAA.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, typ, value := args[0], args[1], args[2]

			normalised, err := dns.SupportedType(typ)
			if err != nil {
				return err
			}
			canonical, err := dns.CanonicalValue(normalised, value)
			if err != nil {
				return err
			}

			choice, tgt, err := chooseDNS(cmd.Context(), opts, name, *providerFlag, *zoneFlag, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			announce(cmd.ErrOrStderr(), choice)

			rec := dns.Record{Name: labelFor(name, choice.Zone), Type: normalised, Value: canonical, TTL: ttl}
			line := describeWrite(cmd.Context(), choice, rec, appending)

			if check {
				cmd.Printf("would %s\n", line)
				return nil
			}
			if !publish {
				ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(), "Really "+line+"?")
				if err != nil {
					return err
				}
				if !ok {
					return errDeclined
				}
			}

			logged := fmt.Sprintf("dns add %s %s %s", name, normalised, canonical)
			write := choice.Provider.Upsert
			if appending {
				logged += " --append"
				write = choice.Provider.Append
			}
			if err := write(cmd.Context(), choice.Zone, rec); err != nil {
				record(opts, tgt, logged, false)
				return err
			}
			record(opts, tgt, logged, true)
			cmd.Printf("%s\n", line)
			return nil
		},
	}
	c.Flags().BoolVar(&check, "check", false, "say what would change, and change nothing")
	c.Flags().BoolVar(&yes, "yes", false, "skip local-write questions; never publish")
	c.Flags().BoolVar(&publish, "publish", false, "publish the record without asking")
	c.Flags().BoolVar(&appending, "append", false, "keep the values the name already holds and add this one")
	c.Flags().IntVar(&ttl, "ttl", 0, "seconds a resolver may cache the record; 0 lets the registrar choose")
	return c
}

func valueOrEveryValue(v string) string {
	if v == "" {
		return "every value"
	}
	return v
}

func newDNSRmCmd(opts *options, providerFlag, zoneFlag *string) *cobra.Command {
	var check, yes bool

	c := &cobra.Command{
		Use:   "rm <name> <type> [value]",
		Short: "Remove a value, or every value, at a name and type",
		Long: "With a value, only that one is removed and the rest of the " +
			"name's values are left in place — that path is not atomic on every " +
			"registrar, and the command says so when more than one value is there.",
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, typ := args[0], args[1]
			value := ""
			if len(args) == 3 {
				value = args[2]
			}

			normalised, err := dns.SupportedType(typ)
			if err != nil {
				return err
			}
			if value != "" {
				if value, err = dns.CanonicalValue(normalised, value); err != nil {
					return err
				}
			}

			choice, tgt, err := chooseDNS(cmd.Context(), opts, name, *providerFlag, *zoneFlag, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			announce(cmd.ErrOrStderr(), choice)

			rec := dns.Record{Name: labelFor(name, choice.Zone), Type: normalised, Value: value}

			if existing, listErr := choice.Provider.List(cmd.Context(), choice.Zone); listErr == nil {
				count := 0
				for _, r := range existing {
					if r.Name == rec.Name && r.Type == rec.Type {
						count++
					}
				}
				if count > 1 {
					cmd.Printf("%s holds %d values; removing one at a time is not atomic on every registrar.\n",
						name, count)
				}
			}

			line := fmt.Sprintf("remove %s %s %s from %s (provider %s)",
				rec.Name, rec.Type, valueOrEveryValue(value), choice.Zone, choice.Name)

			if check {
				cmd.Printf("would %s\n", line)
				return nil
			}
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(), "Really "+line+"?")
				if err != nil {
					return err
				}
				if !ok {
					return errDeclined
				}
			}

			logged := fmt.Sprintf("dns rm %s %s %s", name, normalised, value)
			if err := choice.Provider.Delete(cmd.Context(), choice.Zone, rec); err != nil {
				record(opts, tgt, logged, false)
				return err
			}
			record(opts, tgt, logged, true)
			cmd.Printf("%s\n", line)
			return nil
		},
	}
	c.Flags().BoolVar(&check, "check", false, "say what would change, and change nothing")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return c
}
