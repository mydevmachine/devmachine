package logfile

import "strings"

const esc = 0x1b

// plain is line as a reader of the file should see it: without the escape
// sequences that colour it or move a terminal's cursor, and without the other
// control characters a terminal acts on instead of showing.
func plain(line string) string {
	if !strings.ContainsFunc(line, isControl) {
		return line
	}
	var b strings.Builder
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == esc:
			i = skipEscape(line, i+1)
		case c == '\t' || !isControl(rune(c)):
			b.WriteByte(c)
			i++
		default:
			i++
		}
	}
	return b.String()
}

func isControl(r rune) bool {
	return r < 0x20 && r != '\t' || r == 0x7f
}

// skipEscape returns where the text resumes after the escape sequence whose
// first byte after ESC is at i.
func skipEscape(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[':
		for i++; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return i
	case ']', 'P', 'X', '^', '_':
		for i++; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == esc && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return i
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
		i++
	}
	if i < len(s) {
		i++
	}
	return i
}
