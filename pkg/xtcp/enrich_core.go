package xtcp

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"sync"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// Compile-time gating for the two heavyweight enrichers.
//
// The container / LLDP / NIC / nsid enrichers are always compiled in and
// toggled only at runtime, because they cost nothing to carry: each is a
// pure-Go protocol or ioctl implementation with no third-party dependency.
// ASN and locality are different. pkg/ipasn pulls in parquet-go and bart;
// pkg/localnet pulls in bart and holds one prefix trie per namespace. Left
// unconditional they link into every build flavor including `min`, whose
// entire purpose is to be the stdlib-only slim daemon — and before the
// enrichment work, parquet-go reached the daemon only via `dest_s3parquet`.
//
// So they follow the same pattern as the destinations (see
// destinations_core.go): a `//go:build enrich_<feature>` file holds the
// implementation and registers a factory from init(), and XTCP holds only a
// narrow interface. Keeping the concrete types (ipasn.Index,
// localnet.Snapshot) off the XTCP struct is what actually lets the linker drop
// the packages — the same rule the destination registry relies on. Because
// dispatch goes through the registry rather than a direct call, no
// `!enrich_<feature>` stub file is needed: an unregistered enricher simply has
// no factory.
//
// Runtime enablement is unchanged and still defaults to off: the build tag
// only decides whether the code is in the binary at all. Asking for an
// enricher that was not compiled in is a startup error, not a silent no-op —
// see checkEnrichersCompiledIn.

// Enricher identifiers. These are the `enrich_<name>` build tag suffixes, the
// names an operator sees in error messages and in the compiledInEnrichers
// metric, and the label values on that metric. Exported because cmd/xtcp2
// asks EnricherCompiledIn about them when it builds `-help`.
const (
	EnricherAsn      = "asn"
	EnricherLocality = "locality"
)

// knownEnrichers is the closed set of every compile-time-gated enricher that
// has ever existed, independent of which are in this binary. It exists so the
// operator-facing error can tell "no such enricher" from "that enricher exists
// but this build does not have it", exactly as knownSchemes does for
// destinations. Never remove an entry; a retired enricher stays listed so its
// error message keeps making sense.
var knownEnrichers = []string{
	EnricherAsn,
	EnricherLocality,
}

// enricherFlag maps an enricher to the CLI flag that turns it on, so the
// "not compiled in" error names the thing the operator actually typed.
var enricherFlag = map[string]string{
	EnricherAsn:      "-enrichAsn",
	EnricherLocality: "-enrichLocality",
}

// EnricherFactory builds and installs one compile-time-gated enricher on x.
// Called once from initEnrichers when that enricher's config toggle is set.
// Best-effort like every other enricher: a factory that cannot do its job
// logs, bumps a counter and leaves the interface field nil, which makes the
// stamping path a no-op for it. Errors are not returned because none of them
// are fatal — the fatal case (the code is not in the binary at all) is caught
// earlier by checkEnrichersCompiledIn.
type EnricherFactory func(ctx context.Context, x *XTCP)

var (
	enricherRegistryMu sync.RWMutex
	enricherRegistry   = map[string]EnricherFactory{}
)

// RegisterEnricher wires a factory into the runtime dispatch map. Called from
// `func init()` in each `//go:build enrich_<name>` file.
//
// Panics on an unknown name (the name and knownEnrichers have drifted) and on
// duplicate registration, which in the intended usage can only mean two files
// claim the same `enrich_<name>` tag. Same reasoning as RegisterDestination:
// failing loudly at package init makes a build-tag bug impossible to ship,
// where a silent last-writer-wins replace would lurk until someone noticed the
// wrong enricher running.
func RegisterEnricher(name string, f EnricherFactory) {
	if !IsKnownEnricher(name) {
		panic(fmt.Sprintf("xtcp: RegisterEnricher called for unknown enricher %q — add it to knownEnrichers", name))
	}
	enricherRegistryMu.Lock()
	defer enricherRegistryMu.Unlock()
	if _, exists := enricherRegistry[name]; exists {
		panic(fmt.Sprintf("xtcp: RegisterEnricher called twice for %q — duplicate //go:build tag?", name))
	}
	enricherRegistry[name] = f
}

// IsKnownEnricher reports whether name was ever a valid xtcp2 enricher,
// regardless of whether it is compiled into this binary.
func IsKnownEnricher(name string) bool {
	for _, n := range knownEnrichers {
		if n == name {
			return true
		}
	}
	return false
}

// CompiledInEnrichers returns the sorted list of compile-time-gated enrichers
// linked into this binary. Used by `xtcp2 -help`, the startup config dump, the
// compiledInEnrichers gauge and the "not compiled in" error path, so an
// operator can always find out what the artifact in front of them can do.
func CompiledInEnrichers() []string {
	enricherRegistryMu.RLock()
	defer enricherRegistryMu.RUnlock()
	out := make([]string, 0, len(enricherRegistry))
	for n := range enricherRegistry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// EnricherCompiledIn reports whether one compile-time-gated enricher is
// present in this binary. Used by the CLI so `-help` describes the artifact
// in front of the operator rather than the feature set of some other build.
func EnricherCompiledIn(name string) bool {
	_, status := lookupEnricherFactory(name)
	return status == enricherLookupFound
}

// enricherLookup distinguishes "not in the known set" from "known but not
// compiled in", mirroring destLookup.
type enricherLookup int

const (
	enricherLookupFound enricherLookup = iota
	enricherLookupUnknown
	enricherLookupNotCompiledIn
)

func lookupEnricherFactory(name string) (EnricherFactory, enricherLookup) {
	enricherRegistryMu.RLock()
	f, ok := enricherRegistry[name]
	enricherRegistryMu.RUnlock()
	if ok {
		return f, enricherLookupFound
	}
	if IsKnownEnricher(name) {
		return nil, enricherLookupNotCompiledIn
	}
	return nil, enricherLookupUnknown
}

// enricherLookupError formats the operator-facing error for an enricher that
// was requested but cannot run.
//
// This is deliberately fatal at startup, unlike every other enricher failure.
// The others degrade because a socket or a device might be missing on this
// particular host — a runtime fact an operator cannot fix by redeploying the
// same image. A missing build tag is the opposite: it is a property of the
// artifact, identical on every host it runs on, and the failure it produces
// (silently empty ASN columns across a whole fleet) is exactly what this
// gating exists to make visible.
func enricherLookupError(name string, status enricherLookup) error {
	switch status {
	case enricherLookupUnknown:
		return fmt.Errorf("unknown enricher %q; valid enrichers are: %v",
			name, knownEnrichers)
	case enricherLookupNotCompiledIn:
		flag := enricherFlag[name]
		if flag == "" {
			flag = name
		}
		return fmt.Errorf("%s requested but the %s enricher is not compiled into this binary; "+
			"rebuild with '-tags enrich_%s' (or use a matching `xtcp2-*-%s` / `xtcp2-*-enrich` Nix attribute). "+
			"Compiled-in enrichers: %v",
			flag, name, name, name, CompiledInEnrichers())
	case enricherLookupFound:
		// Caller shouldn't be asking for an error message in the OK case;
		// fall through to the generic nil return rather than panic.
	}
	return nil
}

// requestedEnrichers lists the compile-time-gated enrichers this config turns
// on, in knownEnrichers order.
func requestedEnrichers(c *xtcp_config.XtcpConfig) []string {
	if c == nil {
		return nil
	}
	var out []string
	if c.GetEnrichAsnEnable() {
		out = append(out, EnricherAsn)
	}
	if c.GetEnrichLocalityEnable() {
		out = append(out, EnricherLocality)
	}
	return out
}

// checkConfigEnrichersCompiledIn returns an error naming the first enricher c
// asks for that is not in this binary, nil when every requested one is
// present.
//
// Two callers, deliberately with different severities. Startup (initEnrichers)
// treats it as fatal: the operator asked for something this artifact cannot
// do, on every host it will ever run on. The gRPC config service treats it as
// FailedPrecondition and keeps running: Set re-execs the same binary, so the
// new config would hit the identical wall, and refusing it is strictly better
// than restarting into it.
func checkConfigEnrichersCompiledIn(c *xtcp_config.XtcpConfig) error {
	for _, name := range requestedEnrichers(c) {
		if _, status := lookupEnricherFactory(name); status != enricherLookupFound {
			return enricherLookupError(name, status)
		}
	}
	return nil
}

// checkEnrichersCompiledIn is checkConfigEnrichersCompiledIn against the
// daemon's own config.
func (x *XTCP) checkEnrichersCompiledIn() error {
	return checkConfigEnrichersCompiledIn(x.config)
}

// initGatedEnrichers runs the factory for each requested compile-time-gated
// enricher. checkEnrichersCompiledIn has already established that every one of
// them is registered, so a missing factory here would be a logic error rather
// than an operator mistake; it is skipped rather than re-reported.
func (x *XTCP) initGatedEnrichers(ctx context.Context) {
	for _, name := range requestedEnrichers(x.config) {
		if f, status := lookupEnricherFactory(name); status == enricherLookupFound {
			f(ctx, x)
		}
	}
}

// ---- the seams ---------------------------------------------------------
//
// Both interfaces deal only in scalars and in types owned by this package, so
// neither pkg/ipasn nor pkg/localnet is nameable from untagged code. That is
// the whole mechanism: a type from those packages on the XTCP struct, or in a
// method signature reachable from untagged code, would drag the package back
// into every build regardless of tags.

// asnLookuper maps a destination address to its ASN and network owner.
// Implemented by the enrich_asn build over pkg/ipasn.
type asnLookuper interface {
	// LookupAsn resolves addr by longest-prefix match. ok=false on a miss.
	LookupAsn(addr netip.Addr) (asn uint32, networkOwner string, ok bool)
}

// localityResult is one destination's classification, flattened out of
// localnet.Resolution so that type stays inside the tagged build. Defined here
// (untagged) so the seam signature compiles in every build, the same reason
// s3FlushControl lives in destinations_core.go.
type localityResult struct {
	Locality      xtcp_flat_record.XtcpFlatRecord_Locality
	EgressIfindex uint32
	EgressIfname  string
	BoundIfname   string
	// Remote is false for a self / connected-subnet destination, which skips
	// the internet ASN lookup.
	Remote bool
}

// localityEnricher classifies a destination within the socket's own network
// namespace. Implemented by the enrich_locality build over pkg/localnet, which
// owns the per-namespace snapshot map, its negative cache and the rtnetlink
// discovery that fills it.
type localityEnricher interface {
	// Refresh rebuilds snapshots for the current namespace set. Called only
	// from the single-owner reconcile path (discoverNamespaces) under
	// reconcileMu, so implementations need no additional lock for their
	// refresh bookkeeping.
	Refresh(nss map[uint64]nsIdentity)
	// Resolve classifies dst for the namespace identified by inode.
	// ok=false when that namespace has no snapshot yet, which leaves the
	// caller's default of "remote" in place.
	Resolve(inode uint64, dst netip.Addr, boundIfindex uint32) (localityResult, bool)
	// Active reports whether any snapshot has ever been published, so the
	// stamping path can stay a true no-op until one has.
	Active() bool
}

// refreshLocality is the untagged entry point the reconcile path calls, so
// ns_discover.go needs no build tag of its own. A no-op when locality is
// disabled or not compiled in.
func (x *XTCP) refreshLocality(nss map[uint64]nsIdentity) {
	if x.locality == nil {
		return
	}
	x.locality.Refresh(nss)
}
