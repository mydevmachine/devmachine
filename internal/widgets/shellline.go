package widgets

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The app runs a shell line as /bin/sh -c '<line>' sh v1 … vN, with each
// template turned into "${N}", so the shell never parses a value as code.
// These are the places where the shell, or the program it starts, reads that
// value again as code, and so where a template is refused.
const (
	inSingle    = '\''
	inANSI      = 'S'
	inDouble    = '"'
	inBackticks = '`'
	inSubshell  = 'c'
	inTest      = '['
	inArith     = '('
	inParen     = 'p'
)

var (
	codeShells      = []string{"sh", "bash", "zsh", "dash", "ksh"}
	commandPrefixes = []string{"sudo", "env", "exec", "command", "builtin", "nice", "nohup", "time", "{", "!", "if", "then", "do", "else", "elif", "while", "until"}
	assignment      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	codeFlag        = regexp.MustCompile(`^-[A-Za-z]*c[A-Za-z]*$`)
)

type shellFrame struct {
	words []string
	word  string
}

func (f *shellFrame) endWord() {
	if f.word != "" {
		f.words = append(f.words, f.word)
		f.word = ""
	}
}

type shellScanner struct {
	line     string
	starts   map[int]int
	stack    []byte
	frames   []*shellFrame
	problems []string
}

// unsafeTemplates describes each template of a shell line that the shell
// would read again as code, one sentence each, to follow "source.run".
func unsafeTemplates(line string) []string {
	matches := template.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return nil
	}
	if strings.Contains(template.ReplaceAllString(line, "x"), "<<") {
		var problems []string
		for _, at := range matches {
			problems = append(problems, fmt.Sprintf("has a heredoc (<<), and the shell reads %s in it as code — pass the value as an argument instead", line[at[0]:at[1]]))
		}
		return problems
	}
	s := &shellScanner{line: line, starts: map[int]int{}, frames: []*shellFrame{{}}}
	for _, at := range matches {
		s.starts[at[0]] = at[1]
	}
	for i := 0; i < len(line); i++ {
		if end, ok := s.starts[i]; ok {
			s.template(line[i:end])
			s.frame().word += "x"
			i = end - 1
			continue
		}
		i = s.step(i)
	}
	return s.problems
}

func (s *shellScanner) top() byte {
	if len(s.stack) == 0 {
		return 0
	}
	return s.stack[len(s.stack)-1]
}

func (s *shellScanner) frame() *shellFrame { return s.frames[len(s.frames)-1] }

func isQuote(c byte) bool { return c == inSingle || c == inANSI || c == inDouble }

func (s *shellScanner) push(c byte) {
	s.stack = append(s.stack, c)
	if !isQuote(c) {
		s.frames = append(s.frames, &shellFrame{})
	}
}

func (s *shellScanner) pop() {
	c := s.top()
	s.stack = s.stack[:len(s.stack)-1]
	if !isQuote(c) {
		s.frames = s.frames[:len(s.frames)-1]
		s.frame().word += "x"
	}
}

// escapes says whether a backslash at i hides the next byte. It never hides
// a template, which is substituted whatever comes before it.
func (s *shellScanner) escapes(i int) bool {
	_, isTemplate := s.starts[i+1]
	return s.line[i] == '\\' && i+1 < len(s.line) && !isTemplate
}

// step reads the byte at i and returns the index of the last byte it used.
func (s *shellScanner) step(i int) int {
	c, rest, f := s.line[i], s.line[i:], s.frame()
	switch s.top() {
	case inSingle:
		if c == '\'' {
			s.pop()
		} else {
			f.word += string(c)
		}
		return i
	case inANSI:
		switch {
		case s.escapes(i):
			return i + 1
		case c == '\'':
			s.pop()
		}
		return i
	case inDouble:
		switch {
		case s.escapes(i):
			f.word += string(s.line[i+1])
			return i + 1
		case c == '"':
			s.pop()
		case c == '`':
			s.push(inBackticks)
		case strings.HasPrefix(rest, "$(("):
			s.push(inArith)
			return i + 2
		case strings.HasPrefix(rest, "$("):
			s.push(inSubshell)
			return i + 1
		default:
			f.word += string(c)
		}
		return i
	}
	return s.stepCode(i)
}

func (s *shellScanner) stepCode(i int) int {
	c, rest, f, top := s.line[i], s.line[i:], s.frame(), s.top()
	wordStart := f.word == ""
	switch {
	case s.escapes(i):
		f.word += string(s.line[i+1])
		return i + 1
	case c == '\'':
		s.push(inSingle)
	case strings.HasPrefix(rest, "$'"):
		s.push(inANSI)
		return i + 1
	case c == '"':
		s.push(inDouble)
	case c == '`' && top == inBackticks:
		s.pop()
	case c == '`':
		s.push(inBackticks)
	case strings.HasPrefix(rest, "$(("):
		s.push(inArith)
		return i + 2
	case strings.HasPrefix(rest, "$("):
		s.push(inSubshell)
		return i + 1
	case top == inArith && strings.HasPrefix(rest, "))"):
		s.pop()
		return i + 1
	case (top == inArith || top == inParen) && c == '(':
		s.push(inParen)
	case top == inParen && c == ')':
		s.pop()
	case top == inArith || top == inParen:
	case top == inTest && strings.HasPrefix(rest, "]]"):
		s.pop()
		return i + 1
	case top == inTest:
	case c == '#' && wordStart:
		return len(s.line)
	case wordStart && strings.HasPrefix(rest, "[[") && (len(rest) == 2 || isShellSpace(rest[2])):
		s.push(inTest)
		return i + 1
	case wordStart && strings.HasPrefix(rest, "(("):
		s.push(inArith)
		return i + 1
	case c == '(':
		s.push(inSubshell)
	case c == ')' && top == inSubshell:
		s.pop()
	case isShellSpace(c):
		f.endWord()
	case strings.IndexByte(";&|", c) >= 0:
		f.endWord()
		f.words = nil
	default:
		f.word += string(c)
	}
	return i
}

func isShellSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func (s *shellScanner) template(t string) {
	report := func(format string) { s.problems = append(s.problems, fmt.Sprintf(format, t)) }
	switch {
	case s.top() == inSingle:
		report(`puts %s inside '…', where the shell never expands a value — take the quotes away, or use "…"`)
	case s.top() == inANSI:
		report(`puts %s inside $'…', where the shell never expands a value — take the quotes away, or use "…"`)
	case slices.Contains(s.stack, inBackticks):
		report("puts %s inside backticks, which read the value again as shell code — use $(…) instead")
	case slices.Contains(s.stack, inTest):
		report("puts %s inside [[ … ]], where the shell can run code hidden in a value — use [ … ] instead")
	case slices.Contains(s.stack, inArith):
		report("puts %s inside (( … )) or $(( … )), where the shell can run code hidden in a value — check it with [ … ] instead")
	default:
		for _, f := range s.frames {
			if runner, ok := codeRunner(f.words); ok {
				report("passes %s to " + runner + ", which runs it as shell code — keep templates out of it")
				return
			}
		}
	}
}

// codeRunner names the command among words that reads its arguments as
// shell code: eval, let, or a shell given -c.
func codeRunner(words []string) (string, bool) {
	i := 0
	for i < len(words) && (assignment.MatchString(words[i]) || slices.Contains(commandPrefixes, words[i]) || (i > 0 && strings.HasPrefix(words[i], "-"))) {
		i++
	}
	if i < len(words) {
		if name := path.Base(words[i]); name == "eval" || name == "let" {
			return name, true
		}
	}
	for j, word := range words {
		if !slices.Contains(codeShells, path.Base(word)) {
			continue
		}
		for _, flag := range words[j+1:] {
			if codeFlag.MatchString(flag) {
				return path.Base(word) + " -c", true
			}
		}
	}
	return "", false
}
