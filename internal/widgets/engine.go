package widgets

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var engineConstraintPattern = regexp.MustCompile(`^(>=|>|=)\s*(\d+)\.(\d+)$`)

type engineConstraint struct {
	op           string
	major, minor int
}

func parseEngineConstraint(s string) (engineConstraint, error) {
	match := engineConstraintPattern.FindStringSubmatch(strings.TrimSpace(s))
	if match == nil {
		return engineConstraint{}, errors.New(`write it as ">= 1.0", "> 1.0" or "= 1.0"`)
	}
	major, _ := strconv.Atoi(match[2])
	minor, _ := strconv.Atoi(match[3])
	return engineConstraint{op: match[1], major: major, minor: minor}, nil
}

func (c engineConstraint) allows(version string) bool {
	major, minorText, _ := strings.Cut(version, ".")
	gotMajor, _ := strconv.Atoi(major)
	gotMinor, _ := strconv.Atoi(minorText)
	diff := gotMajor - c.major
	if diff == 0 {
		diff = gotMinor - c.minor
	}
	switch c.op {
	case "=":
		return diff == 0
	case ">":
		return diff > 0
	default:
		return diff >= 0
	}
}
