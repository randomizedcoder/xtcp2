// Package parse turns a fetched feed body into normalized model.Records.
//
// Each feed format is handled by a Parser registered under a string key (the
// `parser:` field in a source config). Parsers set only the feed-derived
// fields (prefix + any service/region/direction metadata) on top of a base
// record built from the source's defaults; CIDR validation and ip_version
// derivation happen later in the combine stage, so parsers never need to touch
// net/netip themselves.
package parse

import "github.com/randomizedcoder/xtcp2/internal/ipfeed/model"

// Defaults are record fields applied to every row a source produces, before
// the parser overlays feed-specific values.
type Defaults struct {
	NetworkOwner    string
	ServiceOperator string
	Service         string
	Product         string
	Region          string
	Direction       string
}

// SourceMeta is the subset of a source config that parsers need. It is a
// plain struct (not the config type) so the parse package does not depend on
// the config package, keeping the dependency edge one-directional.
type SourceMeta struct {
	Name       string
	Provider   string
	URL        string
	SourceType string
	Confidence string
	Defaults   Defaults
	Opts       map[string]any
}

// Base returns a record pre-filled with the source's provenance and default
// fields. Parsers copy this and set Prefix (+ optional overrides) per row.
func (m SourceMeta) Base(retrievedAt string) model.Record {
	return model.Record{
		NetworkOwner:    m.Defaults.NetworkOwner,
		ServiceOperator: m.Defaults.ServiceOperator,
		Provider:        m.Provider,
		Service:         m.Defaults.Service,
		Product:         m.Defaults.Product,
		Region:          m.Defaults.Region,
		Direction:       m.Defaults.Direction,
		SourceName:      m.Name,
		SourceType:      m.SourceType,
		SourceURL:       m.URL,
		RetrievedAt:     retrievedAt,
		Confidence:      m.Confidence,
	}
}

// Parser converts a raw feed body into records. Implementations must not
// perform network I/O and should be deterministic for a given input.
type Parser interface {
	// Parse returns the records found in data. retrievedAt is an RFC3339 UTC
	// timestamp for the fetch, stamped onto every record.
	Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error)
}

var registry = map[string]Parser{}

// Register adds a parser under name. It panics on a duplicate name, since that
// is a programming error discoverable at init time.
func Register(name string, p Parser) {
	if _, dup := registry[name]; dup {
		panic("parse: duplicate parser registered: " + name)
	}
	registry[name] = p
}

// Get returns the parser registered under name.
func Get(name string) (Parser, bool) {
	p, ok := registry[name]
	return p, ok
}

// Registered reports whether a parser key is known. Used by config validation.
func Registered(name string) bool {
	_, ok := registry[name]
	return ok
}

// Names returns the sorted-insertion-agnostic set of registered parser keys
// (used in error messages).
func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
