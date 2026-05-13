package xtcp

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Destination is the shipping target xtcp writes each marshalled record to.
// One Destination per running process; chosen at startup based on
// config.Dest's scheme ("kafka:..." → kafka factory).
//
// Send is invoked from a single deserializer goroutine in production;
// implementations may assume serial access. Concurrent callers are not
// supported without an internal mutex.
type Destination interface {
	Send(ctx context.Context, b *[]byte) (int, error)
	Close() error
}

// DestinationFactory builds a Destination for the running process. Called
// once at startup with the configured XTCP. Factories are responsible for
// validating the destination spec (in x.config.Dest) and dialling/connecting
// any backing client. Returning a non-nil error aborts process startup.
type DestinationFactory func(ctx context.Context, x *XTCP) (Destination, error)

// knownSchemes is the closed set of every destination scheme xtcp2 has ever
// supported. The set of compiled-in schemes is a subset (those whose
// `dest_<scheme>` build tag is set). Used by the CLI error path to
// distinguish "unknown scheme" from "exists but not compiled into this
// binary" so the operator gets the right hint.
var knownSchemes = []string{
	"null", "udp", "unix", "unixgram",
	"kafka", "nats", "nsq", "valkey",
}

var (
	destRegistryMu sync.RWMutex
	destRegistry   = map[string]DestinationFactory{}
)

// RegisterDestination wires a factory into the runtime dispatch map. Called
// from `func init()` in each per-scheme file. Idempotent and order-
// independent: redefining a scheme replaces the previous factory, but in
// practice every scheme is registered from exactly one tagged file.
func RegisterDestination(scheme string, f DestinationFactory) {
	destRegistryMu.Lock()
	defer destRegistryMu.Unlock()
	destRegistry[scheme] = f
}

// IsKnownScheme reports whether scheme was ever a valid xtcp2 destination
// scheme, regardless of whether it's compiled into this binary.
func IsKnownScheme(scheme string) bool {
	for _, s := range knownSchemes {
		if s == scheme {
			return true
		}
	}
	return false
}

// CompiledInSchemes returns the sorted list of schemes whose factories are
// linked into this binary. Used by `xtcp2 -help` and the "not compiled in"
// error path.
func CompiledInSchemes() []string {
	destRegistryMu.RLock()
	defer destRegistryMu.RUnlock()
	out := make([]string, 0, len(destRegistry))
	for s := range destRegistry {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// lookupDestinationFactory returns the factory for scheme and a status that
// distinguishes "not in the known set" from "known but not compiled in."
type destLookup int

const (
	destLookupFound destLookup = iota
	destLookupUnknown
	destLookupNotCompiledIn
)

func lookupDestinationFactory(scheme string) (DestinationFactory, destLookup) {
	destRegistryMu.RLock()
	f, ok := destRegistry[scheme]
	destRegistryMu.RUnlock()
	if ok {
		return f, destLookupFound
	}
	if IsKnownScheme(scheme) {
		return nil, destLookupNotCompiledIn
	}
	return nil, destLookupUnknown
}

// destinationLookupError formats the operator-facing error for a missing
// or unknown destination scheme. Returned by InitDests; the caller prints
// it and exits non-zero.
func destinationLookupError(scheme string, status destLookup) error {
	switch status {
	case destLookupUnknown:
		return fmt.Errorf("unknown destination %q; valid schemes are: %v",
			scheme, knownSchemes)
	case destLookupNotCompiledIn:
		return fmt.Errorf("destination %q is not compiled into this binary; "+
			"rebuild with '-tags dest_%s' (or use the matching `xtcp2-%s` Nix attribute). "+
			"Compiled-in destinations: %v",
			scheme, scheme, scheme, CompiledInSchemes())
	}
	return nil
}
