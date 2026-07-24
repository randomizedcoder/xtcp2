package xtcp

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Destination is the shipping target xtcp writes each marshaled record to.
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
// validating the destination spec (in x.config.Dest) and dialing/connecting
// any backing client. Returning a non-nil error aborts process startup.
type DestinationFactory func(ctx context.Context, x *XTCP) (Destination, error)

// Destination scheme identifiers. Doubles as the scheme prefix in `-dest
// <scheme>:<addr>` and (for unix/unixgram/udp) as the corresponding net
// package network name accepted by net.Dial / net.Listen.
const (
	schemeNull      = "null"
	schemeStdout    = "stdout"
	schemeStderr    = "stderr"
	schemeFile      = "file"
	schemeTCP       = "tcp"
	schemeHTTP      = "http"
	schemeHTTPS     = "https"
	schemeUDP       = "udp"
	schemeUnix      = "unix"
	schemeUnixgram  = "unixgram"
	schemeKafka     = "kafka"
	schemeNats      = "nats"
	schemeNsq       = "nsq"
	schemeValkey    = "valkey"
	schemeS3Parquet = "s3parquet"

	// schemeNullPrefix is the `-dest` value that selects the null sink
	// without an address payload. Used as a no-op destination in tests.
	schemeNullPrefix = "null:"
)

// knownSchemes is the closed set of every destination scheme xtcp2 has ever
// supported. The set of compiled-in schemes is a subset (those whose
// `dest_<scheme>` build tag is set). Used by the CLI error path to
// distinguish "unknown scheme" from "exists but not compiled into this
// binary" so the operator gets the right hint.
var knownSchemes = []string{
	schemeNull, schemeStdout, schemeStderr, schemeFile,
	schemeTCP, schemeHTTP, schemeHTTPS,
	schemeUDP, schemeUnix, schemeUnixgram,
	schemeKafka, schemeNats, schemeNsq, schemeValkey,
	schemeS3Parquet,
}

var (
	destRegistryMu sync.RWMutex
	destRegistry   = map[string]DestinationFactory{}

	// libraryDefaultDest maps a library (build-tag-gated) destination scheme
	// to the canonical `-dest` value the CLI should default to. Populated
	// from the same init() that calls RegisterDestination. Stdlib
	// destinations (null/stdout/file/tcp/...) register none, so registering a
	// default is what marks a scheme as a "library" destination for
	// auto-defaulting. Guarded by destRegistryMu.
	libraryDefaultDest = map[string]string{}
)

// RegisterDestination wires a factory into the runtime dispatch map. Called
// from `func init()` in each per-scheme file.
//
// Panics on duplicate registration of the same scheme. In the intended
// usage every scheme is registered from exactly one tagged file, so a
// duplicate registration means two files claimed the same `dest_<scheme>`
// tag — almost certainly a build-tag bug. Failing loudly at package init
// makes that bug impossible to ship; a silent last-writer-wins replace
// would let it lurk until someone notices the wrong factory is chosen.
func RegisterDestination(scheme string, f DestinationFactory) {
	destRegistryMu.Lock()
	defer destRegistryMu.Unlock()
	if _, exists := destRegistry[scheme]; exists {
		panic(fmt.Sprintf("xtcp: RegisterDestination called twice for scheme %q — duplicate //go:build tag?", scheme))
	}
	destRegistry[scheme] = f
}

// RegisterLibraryDefaultDest records the canonical `-dest` value for a
// library (build-tag-gated) destination. The CLI uses it as the default
// `-dest` when exactly one library destination is compiled into the binary,
// so a single-destination build (e.g. s3parquet-only) works without the
// operator overriding the default. Called from the same init() as
// RegisterDestination; stdlib destinations register none and thus never
// participate in auto-defaulting.
func RegisterLibraryDefaultDest(scheme, dest string) {
	destRegistryMu.Lock()
	defer destRegistryMu.Unlock()
	libraryDefaultDest[scheme] = dest
}

// CompiledInLibrarySchemes returns the sorted list of library-destination
// schemes linked into this binary (those that registered a default dest).
func CompiledInLibrarySchemes() []string {
	destRegistryMu.RLock()
	defer destRegistryMu.RUnlock()
	out := make([]string, 0, len(libraryDefaultDest))
	for s := range libraryDefaultDest {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// LibraryDefaultDest returns the registered default `-dest` for a library
// scheme, or "" if the scheme registered none / is not compiled in.
func LibraryDefaultDest(scheme string) string {
	destRegistryMu.RLock()
	defer destRegistryMu.RUnlock()
	return libraryDefaultDest[scheme]
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
	case destLookupFound:
		// Caller shouldn't be asking for an error message in the OK case;
		// fall through to the generic nil return rather than panic.
	}
	return nil
}
