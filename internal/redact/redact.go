// Package redact takes secrets out of text before it is written to a log.
//
// It errs towards hiding: a log somebody sends to get help must be safe to
// send without reading it first. What it keeps on purpose is what makes a log
// worth sending: addresses, host names, machine, workspace and user names, and
// the commands that ran.
package redact

import (
	"regexp"
	"strings"
)

// Mask stands in for every value taken out.
const Mask = "***"

const sensitiveName = `(?:token|secret|password|passwd|api[_-]?key|private[_-]?key|auth)`

var (
	sensitiveFlag = regexp.MustCompile(`(?i)` + sensitiveName)

	keyBegin = regexp.MustCompile(`-----BEGIN ([A-Z0-9 ]*)PRIVATE KEY-----`)
	keyEnd   = regexp.MustCompile(`-----END ([A-Z0-9 ]*)PRIVATE KEY-----`)
	keyBlock = regexp.MustCompile(`-----BEGIN ([A-Z0-9 ]*)PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
)

type rule struct {
	re   *regexp.Regexp
	with string
}

var rules = []rule{
	{regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?)[^"'\r\n]+`), "${1}" + Mask},
	{regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`), "${1}" + Mask},
	{regexp.MustCompile(`\b(gh[pousr]_)[A-Za-z0-9]{16,}`), "${1}" + Mask},
	{regexp.MustCompile(`\b(github_pat_)[A-Za-z0-9_]{16,}`), "${1}" + Mask},
	{regexp.MustCompile(`\b(sk-)[A-Za-z0-9_-]{16,}`), "${1}" + Mask},
	{regexp.MustCompile(`\b(tskey-)[A-Za-z0-9_-]+`), "${1}" + Mask},
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^\s:/@]+):[^\s@/]+@`), "${1}:" + Mask + "@"},
	{regexp.MustCompile(`(?i)("[^"]*` + sensitiveName + `[^"]*"\s*:\s*)("(?:[^"\\]|\\.)*"|[^\s,}\]]+)`), `${1}"` + Mask + `"`},
	{regexp.MustCompile(`(?i)('[^']*` + sensitiveName + `[^']*'\s*:\s*)('(?:[^'\\]|\\.)*'|[^\s,}\]]+)`), `${1}'` + Mask + `'`},
	{regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*` + sensitiveName + `[A-Za-z0-9_.-]*\s*=\s*)("[^"]*"|'[^']*'|[^\s,;&"']+)`), "${1}" + Mask},
	{regexp.MustCompile(`^([+-]?\s*(?:export\s+)?[A-Z][A-Z0-9_]*=)(\S.*)$`), "${1}" + Mask},
}

// Redactor redacts a stream one line at a time. It carries what a single line
// cannot know: that a private key began on an earlier line and has not ended.
type Redactor struct {
	inKey bool
}

// Line returns the line with its secrets masked, and false when the line is
// part of a private key and must not be written at all.
func (r *Redactor) Line(line string) (string, bool) {
	if r.inKey {
		if m := keyEnd.FindString(line); m != "" {
			r.inKey = false
			return m, true
		}
		return "", false
	}
	line = keyBlock.ReplaceAllString(line, "-----BEGIN ${1}PRIVATE KEY----- "+Mask)
	for _, loc := range keyBegin.FindAllStringIndex(line, -1) {
		if strings.HasPrefix(line[loc[1]:], " "+Mask) {
			continue
		}
		r.inKey = true
		return line[:loc[1]] + " " + Mask, true
	}
	for _, rl := range rules {
		line = rl.re.ReplaceAllString(line, rl.with)
	}
	return line, true
}

// String redacts text that may hold several lines.
func String(text string) string {
	var r Redactor
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if kept, ok := r.Line(line); ok {
			out = append(out, kept)
		}
	}
	return strings.Join(out, "\n")
}

// valueFlags are the flags whose value is a separate word, so the word after
// them is not a positional argument.
var valueFlags = map[string]bool{
	"config": true, "format": true, "machine": true, "workspace": true, "env-file": true,
}

// Argv redacts a command line. A value given to a flag named like a secret is
// masked, and so is the value `secrets set` takes as an argument; every other
// word goes through String.
func Argv(args []string) []string {
	out := make([]string, len(args))
	var positional []string
	maskNext, skipNext := false, false
	for i, arg := range args {
		switch {
		case maskNext:
			out[i] = Mask
			maskNext = false
			continue
		case skipNext:
			out[i] = String(arg)
			skipNext = false
			continue
		case strings.HasPrefix(arg, "-") && arg != "-" && arg != "--":
			name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			switch {
			case secretFlag(name) && hasValue:
				out[i] = arg[:len(arg)-len(value)] + Mask
			case secretFlag(name):
				out[i] = arg
				maskNext = i+1 < len(args) && !strings.HasPrefix(args[i+1], "-")
			default:
				out[i] = String(arg)
				skipNext = !hasValue && valueFlags[name]
			}
			continue
		}
		positional = append(positional, arg)
		if len(positional) > 3 && positional[0] == "secrets" && positional[1] == "set" {
			out[i] = Mask
			continue
		}
		out[i] = String(arg)
	}
	return out
}

func secretFlag(name string) bool {
	if strings.HasSuffix(name, "-stdin") || strings.HasSuffix(name, "-file") {
		return false
	}
	return name == "value" || sensitiveFlag.MatchString(name)
}
