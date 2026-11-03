package dns

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ErrBadValue means the value does not fit the grammar of its type, before
// any registrar was asked.
var ErrBadValue = errors.New("the value does not fit the type")

// caaTags are the property tags RFC 8659 defines.
var caaTags = []string{"issue", "issuewild", "iodef"}

// CanonicalValue checks value against the grammar of its type and returns the
// one spelling every provider is handed.
//
// A value is written the way a zone file or `dig` writes it: `10
// mail.example.com` for MX, `10 5 5060 sip.example.com` for SRV, `0 issue
// "ca.example.net"` for CAA. One string round-trips through every registrar,
// whether it keeps a priority inside its content or in a field of its own;
// splitting it is the provider's job. Host names lose their trailing dot and
// their case here, so the same record spelt two ways compares equal later.
func CanonicalValue(typ, value string) (string, error) {
	t, err := SupportedType(typ)
	if err != nil {
		return "", err
	}
	switch t {
	case "A":
		return canonicalAddress(value, false)
	case "AAAA":
		return canonicalAddress(value, true)
	case "CNAME", "NS":
		fields := strings.Fields(value)
		if len(fields) != 1 {
			return "", fmt.Errorf("%w: %s wants \"<host>\", got %q", ErrBadValue, t, value)
		}
		return canonicalHost(fields[0]), nil
	case "TXT":
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%w: a TXT value cannot be empty", ErrBadValue)
		}
		return value, nil
	case "MX":
		return canonicalMX(value)
	case "SRV":
		return canonicalSRV(value)
	case "CAA":
		return canonicalCAA(value)
	}
	return "", fmt.Errorf("%w: %q", ErrUnsupportedType, typ)
}

func canonicalAddress(value string, v6 bool) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(value))
	isV4 := ip != nil && ip.To4() != nil
	switch {
	case ip == nil, v6 && isV4:
		if v6 {
			return "", fmt.Errorf("%w: AAAA wants an IPv6 address, got %q", ErrBadValue, value)
		}
		return "", fmt.Errorf("%w: A wants an IPv4 address, got %q", ErrBadValue, value)
	case !v6 && !isV4:
		return "", fmt.Errorf("%w: A wants an IPv4 address, got %q", ErrBadValue, value)
	}
	return ip.String(), nil
}

func canonicalHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

func canonicalMX(value string) (string, error) {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return "", fmt.Errorf("%w: MX wants \"<priority> <host>\", got %q", ErrBadValue, value)
	}
	priority, err := number(fields[0], 65535)
	if err != nil {
		return "", fmt.Errorf("%w: MX wants \"<priority> <host>\": %v", ErrBadValue, err)
	}
	return fmt.Sprintf("%d %s", priority, canonicalHost(fields[1])), nil
}

func canonicalSRV(value string) (string, error) {
	fields := strings.Fields(value)
	if len(fields) != 4 {
		return "", fmt.Errorf("%w: SRV wants \"<priority> <weight> <port> <target>\", got %q", ErrBadValue, value)
	}
	numbers := make([]int, 3)
	for i, field := range fields[:3] {
		n, err := number(field, 65535)
		if err != nil {
			return "", fmt.Errorf("%w: SRV wants \"<priority> <weight> <port> <target>\": %v", ErrBadValue, err)
		}
		numbers[i] = n
	}
	return fmt.Sprintf("%d %d %d %s", numbers[0], numbers[1], numbers[2], canonicalHost(fields[3])), nil
}

func canonicalCAA(value string) (string, error) {
	fields := strings.Fields(value)
	if len(fields) < 3 {
		return "", fmt.Errorf("%w: CAA wants \"<flags> <tag> <value>\", got %q", ErrBadValue, value)
	}
	flags, err := number(fields[0], 255)
	if err != nil {
		return "", fmt.Errorf("%w: CAA wants \"<flags> <tag> <value>\": %v", ErrBadValue, err)
	}
	tag := strings.ToLower(fields[1])
	known := false
	for _, t := range caaTags {
		known = known || t == tag
	}
	if !known {
		return "", fmt.Errorf("%w: a CAA tag is one of %s, got %q", ErrBadValue, strings.Join(caaTags, ", "), fields[1])
	}
	rest := strings.Trim(strings.Join(fields[2:], " "), `"`)
	return fmt.Sprintf(`%d %s "%s"`, flags, tag, rest), nil
}

func number(field string, limit int) (int, error) {
	n, err := strconv.Atoi(field)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", field)
	}
	if n < 0 || n > limit {
		return 0, fmt.Errorf("%d is not between 0 and %d", n, limit)
	}
	return n, nil
}
