package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/credentials"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// External is a provider that runs as an executable on the machine, with its
// token already delivered there by `credentials push`.
type External struct {
	name       string
	client     remote.Client
	entrypoint string
	credential string
	commands   []string
}

// NewExternal wraps a package's entrypoint as a Provider.
//
// entrypoint is the absolute path to the executable on the machine.
// credential is the name `credentials push` delivered the token under, so
// `/etc/devmachine/<credential>/env` is what gets sourced before every call.
// commands is what the package declared it accepts; ["*"] means anything.
func NewExternal(name string, client remote.Client, entrypoint, credential string, commands []string) *External {
	return &External{name: name, client: client, entrypoint: entrypoint,
		credential: credential, commands: commands}
}

// Name is the package's name, which is also what --dns-provider matches.
func (e *External) Name() string { return e.name }

// List runs the package's `list` command.
func (e *External) List(ctx context.Context, zone string) ([]Record, error) {
	out, err := e.ask(ctx, "", "list", zone)
	if err != nil {
		return nil, err
	}
	return out.Records, nil
}

// Zones runs the package's `zones` command, so WhoHolds can ask what a token
// can see.
func (e *External) Zones(ctx context.Context) ([]string, error) {
	out, err := e.ask(ctx, "", "zones")
	if err != nil {
		return nil, err
	}
	return out.Zones, nil
}

// Help runs the package's `help` command, so the CLI can describe a provider
// it never had to be taught about.
func (e *External) Help(ctx context.Context) ([]Command, error) {
	out, err := e.ask(ctx, "", "help")
	if err != nil {
		return nil, err
	}
	return out.Commands, nil
}

// Upsert runs the package's `upsert` command.
func (e *External) Upsert(ctx context.Context, zone string, r Record) error {
	return e.write(ctx, "upsert", zone, r)
}

// Delete runs the package's `delete` command.
func (e *External) Delete(ctx context.Context, zone string, r Record) error {
	return e.write(ctx, "delete", zone, r)
}

func (e *External) write(ctx context.Context, action, zone string, r Record) error {
	t, err := SupportedType(r.Type)
	if err != nil {
		return err
	}
	r.Type = t

	body, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("building the record for %s: %w", e.name, err)
	}
	_, err = e.ask(ctx, string(body), action, zone)
	return err
}

// providerAnswer is what a provider's entrypoint prints on stdout, whichever
// command it was asked to run.
type providerAnswer struct {
	Records  []Record  `json:"records"`
	Zones    []string  `json:"zones"`
	Commands []Command `json:"commands"`
	Error    *struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	} `json:"error"`
}

// Command is one thing a provider's entrypoint accepts, as it describes
// itself to `help`.
type Command struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Args    string `json:"args,omitempty"`
}

func (e *External) ask(ctx context.Context, stdin string, args ...string) (providerAnswer, error) {
	var answer providerAnswer

	out, runErr := e.client.Run(ctx, e.shellFor(args, stdin))

	// The answer is read before the exit status is judged: the contract says
	// a failure carries its reason on stdout, and that reason is better than
	// an exit code.
	if strings.TrimSpace(out) != "" {
		if err := json.Unmarshal([]byte(out), &answer); err != nil && runErr == nil {
			return answer, fmt.Errorf("the %s provider answered something that is not JSON: %s",
				e.name, strings.TrimSpace(out))
		}
	}
	if answer.Error != nil {
		return answer, ErrorForKind(answer.Error.Kind, answer.Error.Message)
	}
	if runErr != nil {
		detail := strings.TrimSpace(out)
		if detail == "" {
			detail = runErr.Error()
		}
		return answer, fmt.Errorf("the %s provider failed: %s", e.name, detail)
	}
	return answer, nil
}

// shellFor builds the one command line that runs on the machine.
//
// The credential is sourced rather than passed: the value is already on the
// machine, in the file `credentials push` wrote, and reading it here would mean
// carrying a secret to the operator's computer and back for no reason. It also
// never reaches a command argument, where `ps` shows it to every account on
// the machine — only the credential's name does, and that name is not a
// secret.
//
// `set -a` exports what the file sets and `set +a` stops there, so nothing else
// is added and a provider cannot come to depend on something that happens to be
// in one operator's shell.
//
// A file that is not there yet is skipped, not sourced: `help` needs no
// credential, and dash ends the whole command when `.` cannot read its file.
func (e *External) shellFor(args []string, stdin string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		if a == "" {
			continue
		}
		quoted = append(quoted, quoteArg(a))
	}

	run := fmt.Sprintf("%s %s", quoteArg(e.entrypoint), strings.Join(quoted, " "))
	if e.credential != "" {
		env := quoteArg(credentials.EnvFile(e.credential))
		run = fmt.Sprintf("[ ! -r %[1]s ] || { set -a; . %[1]s; set +a; }; %[2]s", env, run)
	}
	if stdin == "" {
		return run
	}
	// Group the setup and entrypoint so the pipe reaches the provider. Without
	// the subshell, `|` binds only to `set -a`; the provider sees an empty stdin
	// and rejects every write as malformed JSON.
	return fmt.Sprintf("printf %s | ( %s )", shellQuote(stdin), run)
}

// shellSafeArg matches a word that reads back identically whether or not it
// is quoted: the ordinary shape of a path, a command name or a domain.
var shellSafeArg = regexp.MustCompile(`^[A-Za-z0-9@%_+=:,./-]+$`)

// quoteArg quotes only what needs it, so a plain path or domain reads in a
// command line the way it was typed, and anything else is quoted rather than
// trusted.
func quoteArg(s string) string {
	if s != "" && shellSafeArg.MatchString(s) {
		return s
	}
	return shellQuote(s)
}

// shellQuote wraps a value so a shell treats it as one word, whatever is in it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (e *External) accepts(command string) bool {
	return slices.Contains(e.commands, "*") || slices.Contains(e.commands, command)
}

// Call is the generic door: whatever the package accepts, with its output
// and its errors going straight to the caller, each to its own writer.
func (e *External) Call(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("say what to run: %s accepts %s", e.name, strings.Join(e.commands, ", "))
	}
	if !e.accepts(args[0]) {
		return fmt.Errorf("%s does not accept %q. It accepts %s",
			e.name, args[0], strings.Join(e.commands, ", "))
	}

	return e.client.Stream(ctx, e.shellFor(args, ""), out, errOut)
}
