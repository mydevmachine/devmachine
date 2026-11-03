package dns

import (
	"errors"
	"fmt"
	"strings"
)

// The situations every provider can be in, named once so the CLI can say what
// happened instead of repeating a vendor's string.
var (
	ErrZoneNotFound    = errors.New("zone not found, or the token cannot see it")
	ErrUnauthenticated = errors.New("the token was rejected")
	ErrForbidden       = errors.New("the token cannot change this zone")
	ErrInvalidRecord   = errors.New("the record was rejected")
	ErrRateLimited     = errors.New("rate limited")
	ErrUnsupportedType = errors.New("this record type is not supported yet")
	ErrAmbiguous       = errors.New("the name holds several values")
)

// kinds is the vocabulary a provider reports in, and the only part of the
// contract that has to stay stable across a process boundary.
var kinds = map[string]error{
	"zone_not_found":  ErrZoneNotFound,
	"unauthenticated": ErrUnauthenticated,
	"forbidden":       ErrForbidden,
	"invalid_record":  ErrInvalidRecord,
	"rate_limited":    ErrRateLimited,
	"ambiguous":       ErrAmbiguous,
}

// ErrorForKind turns what a provider reported into what the CLI understands.
//
// An unknown kind is still an error carrying both the kind and the message: a
// provider written after this binary shipped must be able to fail loudly.
func ErrorForKind(kind, message string) error {
	if sentinel, ok := kinds[kind]; ok {
		return fmt.Errorf("%w: %s", sentinel, message)
	}
	return fmt.Errorf("the provider failed with %q: %s", kind, message)
}

// supportedTypes are the types every provider carries. Each has a written
// form CanonicalValue checks, so one Value string round-trips on every
// registrar; SOA is left out because a registrar owns it.
var supportedTypes = []string{"A", "AAAA", "CNAME", "TXT", "MX", "SRV", "NS", "CAA"}

// SupportedType normalises t to upper case and rejects anything this version
// cannot carry.
func SupportedType(t string) (string, error) {
	up := strings.ToUpper(strings.TrimSpace(t))
	for _, s := range supportedTypes {
		if s == up {
			return up, nil
		}
	}
	return "", fmt.Errorf("%w: %q. Supported: %s",
		ErrUnsupportedType, t, strings.Join(supportedTypes, ", "))
}
