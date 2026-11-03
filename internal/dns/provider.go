package dns

import "context"

// Record is one value at one name.
//
// One Record is one value, never an RRset. A name that holds several values
// is several Records, and Append is how one more joins them.
type Record struct {
	// Name is the label, not the full name: "www", or "@" for the apex.
	Name string `json:"name"`
	// Type is one of the types SupportedType accepts.
	Type string `json:"type"`
	// Value is the record's content, written the way CanonicalValue writes
	// it: a priority, weight or port is inside this string, never a field.
	Value string `json:"value"`
	// TTL of zero means "let the provider choose".
	TTL int `json:"ttl,omitempty"`
}

// Provider is a place records live.
//
// Only `manual` implements this in Go. Every other provider is a package
// holding an executable, wrapped by External — so the rest of the CLI cannot
// tell the two apart, and no vendor reaches it.
type Provider interface {
	// Name is how the provider is written in the configuration.
	Name() string

	// List returns every supported record in the zone. Types this version
	// cannot carry are left out rather than mangled.
	List(ctx context.Context, zone string) ([]Record, error)

	// Upsert makes the records for (Name, Type) be exactly this one Value.
	// An existing value for that name and type is replaced, not added to.
	// A record that already holds exactly this value is left alone.
	Upsert(ctx context.Context, zone string, r Record) error

	// Append adds this Value next to whatever the name and type already hold.
	// A value already there is left alone, so appending twice is one record.
	Append(ctx context.Context, zone string, r Record) error

	// Delete removes the record with this exact Name, Type and Value. Other
	// values for the same name and type are left in place. An empty Value
	// means "remove every value for this name and type".
	Delete(ctx context.Context, zone string, r Record) error
}
