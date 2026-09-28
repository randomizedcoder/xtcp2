package nlparity

// The goip parity allowlist: the loader, the validator, and the one rule that
// keeps the file honest.
//
// # Why the allowlist is a typed object and not a map[string]string
//
// An allowlist is where divergences go to be forgotten, and the only defense
// against that is making the forgetting expensive. Three properties do the
// work here, and each one is a validation error rather than a convention:
//
//   - Every entry carries a `reason`. An entry without one is a refusal to
//     explain, and it loads as an error.
//   - A `version-skew` entry carries an `ip_version`. Such an entry is a
//     statement about ONE iproute2 release, so it has to stop applying when
//     the pin moves; without the version there is nothing to compare the pin
//     against.
//   - Two entries may not share a locus. That is not a precedence rule to be
//     resolved left to right, because nothing in the file would say which
//     entry applied, and the report would cite a reason that was not the
//     reason.
//
// # The rule that cannot be expressed in the file, so it lives in the code
//
// Suppression applies to VALUE divergences only. Never to presence or absence,
// never to a transaction count, never to key-set membership or ordering.
//
// This is not a stylistic preference. Consider a goip that omits an attribute
// entirely at a locus whose value is noisy enough to have earned an entry: if
// presence were suppressible, the omission would be masked by the entry that
// exists for the value, and the run would report clean. The bug and its own
// cover story would arrive in the same file.
//
// So Suppresses refuses every class but DivergenceValue, whatever the file
// says, and there is no way to write an entry that overrides it. A test row
// asserts precisely that: a presence divergence at an allowlisted locus is
// still reported.

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
)

//go:embed goip-parity-allowlist.json
var allowlistFS embed.FS

// AllowlistFileCst is the embedded file's name, exported so the in-guest
// comparator and the Go test cite the same string rather than two copies.
const AllowlistFileCst = "goip-parity-allowlist.json"

// Kind classifies why a divergence is accepted. The four values are the plan's,
// and the set is closed: an unknown kind is a load error, so a typo cannot
// quietly become a fifth category that nothing reviews.
type Kind string

const (
	// KindVersionSkew: the pinned `ip` and some other release disagree, so the
	// divergence belongs to the target rather than to goip. Requires IPVersion.
	KindVersionSkew Kind = "version-skew"
	// KindSideSocket: traffic from a socket that is not the tool's own. Valid
	// for the advisory mesh topology only; the clean namespace emits none.
	KindSideSocket Kind = "side-socket"
	// KindVolatileFallback: a value that moved between the two control
	// captures. Each of these is a bug report against D_control.
	KindVolatileFallback Kind = "volatile-fallback"
	// KindAcceptedDivergence: goip is deliberately different and will stay so.
	KindAcceptedDivergence Kind = "accepted-divergence"
)

// validKinds is the closed set. A map rather than a slice so the error message
// can be built without a linear scan, and so adding a kind is one line.
var validKinds = map[Kind]bool{
	KindVersionSkew:        true,
	KindSideSocket:         true,
	KindVolatileFallback:   true,
	KindAcceptedDivergence: true,
}

// DivergenceClass is what the comparator found, not where. It exists so
// Suppresses can refuse the unsuppressible classes; see the header.
type DivergenceClass uint8

const (
	// DivergenceValue is an attribute present on both sides with different
	// payload bytes. The only suppressible class.
	DivergenceValue DivergenceClass = iota
	// DivergencePresence is an attribute on one side and not the other.
	DivergencePresence
	// DivergenceTransactionCount is an L1 finding: the two captures do not hold
	// the same number of transactions.
	DivergenceTransactionCount
	// DivergenceKeySet is an L3 finding: the reply object keys differ.
	DivergenceKeySet
	// DivergenceKeyOrder is an L3 finding: same keys, different order.
	DivergenceKeyOrder
	// DivergenceAttrOrder is an L3 finding: same attributes, different order.
	DivergenceAttrOrder
	// DivergenceHygiene is a fact about the CAPTURE rather than about either
	// tool: a multicast notification, an orphaned reply, an unattributable
	// transaction, or an unterminated one.
	//
	// Note what is NOT in that list. "More than one port id answered" was, and
	// the corpus disproved it: ll_link_get opens a fresh socket per call, so
	// eight of the eighteen clean guest captures answer on several port ids
	// with nothing unattributed. It is a Report sentinel now; see HygieneDiff.
	//
	// It is a class rather than prose so that it goes through the same
	// Suppressible gate as everything else and comes out false. A capture that
	// caught someone else's traffic did not measure what it claimed to, and an
	// allowlist entry saying "ignore that" would be an entry that makes every
	// other finding in the run meaningless.
	DivergenceHygiene
)

// String names the class for a report. Not `%d`: a report that says "class 3"
// makes the reader open this file.
func (d DivergenceClass) String() string {
	switch d {
	case DivergenceValue:
		return "value"
	case DivergencePresence:
		return "presence"
	case DivergenceTransactionCount:
		return "transaction-count"
	case DivergenceKeySet:
		return "key-set"
	case DivergenceKeyOrder:
		return "key-order"
	case DivergenceAttrOrder:
		return "attr-order"
	case DivergenceHygiene:
		return "hygiene"
	default:
		return fmt.Sprintf("DivergenceClass(%d)", uint8(d))
	}
}

// Suppressible reports whether a class may ever be allowlisted. Only values
// may be. See the header for why this is a property of the class rather than
// of the entry.
func (d DivergenceClass) Suppressible() bool { return d == DivergenceValue }

// Load errors. Each one names the field at fault so the message does not need
// the file open beside it.
var (
	ErrAllowlistBadJSON        = errors.New("nlparity: allowlist is not valid JSON")
	ErrAllowlistNoCommand      = errors.New("nlparity: allowlist entry has no command")
	ErrAllowlistNoLocus        = errors.New("nlparity: allowlist entry has no locus")
	ErrAllowlistNoReason       = errors.New("nlparity: allowlist entry has no reason")
	ErrAllowlistBadKind        = errors.New("nlparity: allowlist entry has an unknown kind")
	ErrAllowlistNoIPVersion    = errors.New("nlparity: version-skew entry has no ip_version")
	ErrAllowlistIPVersionUnked = errors.New("nlparity: ip_version on an entry that is not version-skew")
	ErrAllowlistDuplicate      = errors.New("nlparity: two allowlist entries share a locus")
)

// There is deliberately no "gated command has no entries" error. A command
// with nothing to accept is the goal state, not a misconfiguration; the
// gate-one-at-a-time discipline is served by every entry needing a reason,
// which is already enforced above.

// Entry is one accepted divergence.
type Entry struct {
	// Command is the argv the comparator drove both tools with, e.g.
	// "link show" — not a goip-specific spelling, because the whole point is
	// that both tools got the same arguments.
	Command string `json:"command"`
	// Locus names the asserted quantity, e.g.
	// "request:RTM_GETLINK:IFLA_EXT_MASK:get". It is deliberately specific: a
	// divergence that moves to a different locus stops matching, and the check
	// goes red again.
	//
	// It must be spelled exactly as the differ derives it — Divergence.Locus,
	// not a human's name for the same thing. The trailing ":get" above is the
	// request role, which comes from NLM_F_DUMP rather than from iproute2's
	// ll_link_get, because a comparator cannot see a C function name.
	Locus string `json:"locus"`
	// Kind is why it is accepted. Closed set; see Kind.
	Kind Kind `json:"kind"`
	// IPVersion is required on version-skew and forbidden elsewhere, so an
	// entry either is about one release or is not about releases at all.
	IPVersion string `json:"ip_version,omitempty"`
	// Reason is mandatory prose. An entry that cannot be explained is an entry
	// that should not exist.
	Reason string `json:"reason"`
}

// key is how an entry is matched, and the duplicate check's unit.
func (e Entry) key() string { return e.Command + "\x00" + e.Locus }

// Allowlist is the loaded file plus the lookup index built from it.
type Allowlist struct {
	// Comment is the `_comment` header, retained rather than discarded so a
	// tool that rewrites the file cannot drop it. Unused by lookups.
	Comment []string `json:"_comment"`
	// Entries in file order. Order carries no meaning — duplicates are an
	// error precisely so that it does not have to.
	Entries []Entry `json:"entries"`
	// GatedCommands is the subset whose findings fail a build. Empty is the
	// honest state until a command's entries have reasons.
	GatedCommands []string `json:"gated_commands"`

	byKey map[string]Entry
	gated map[string]bool
}

// LoadAllowlist parses and validates. It returns a fully indexed Allowlist or
// an error; there is no partially-valid result, because a caller that got one
// would have to decide which half to trust.
func LoadAllowlist(data []byte) (*Allowlist, error) {
	var a Allowlist
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAllowlistBadJSON, err)
	}

	a.byKey = make(map[string]Entry, len(a.Entries))
	for i := range a.Entries {
		e := a.Entries[i]
		switch {
		case e.Command == "":
			return nil, fmt.Errorf("%w: entry %d", ErrAllowlistNoCommand, i)
		case e.Locus == "":
			return nil, fmt.Errorf("%w: entry %d (command %q)", ErrAllowlistNoLocus, i, e.Command)
		case e.Reason == "":
			return nil, fmt.Errorf("%w: entry %d (%s / %s)", ErrAllowlistNoReason, i, e.Command, e.Locus)
		case !validKinds[e.Kind]:
			return nil, fmt.Errorf("%w: entry %d (%s / %s) has kind %q", ErrAllowlistBadKind, i, e.Command, e.Locus, e.Kind)
		case e.Kind == KindVersionSkew && e.IPVersion == "":
			return nil, fmt.Errorf("%w: entry %d (%s / %s)", ErrAllowlistNoIPVersion, i, e.Command, e.Locus)
		case e.Kind != KindVersionSkew && e.IPVersion != "":
			return nil, fmt.Errorf("%w: entry %d (%s / %s) has kind %q", ErrAllowlistIPVersionUnked, i, e.Command, e.Locus, e.Kind)
		}
		if _, dup := a.byKey[e.key()]; dup {
			return nil, fmt.Errorf("%w: %s / %s", ErrAllowlistDuplicate, e.Command, e.Locus)
		}
		a.byKey[e.key()] = e
	}

	a.gated = make(map[string]bool, len(a.GatedCommands))
	for _, c := range a.GatedCommands {
		a.gated[c] = true
	}
	return &a, nil
}

// EmbeddedAllowlist loads the committed file. Two consumers read it — a Go
// test and the in-guest comparator — so it is embedded rather than found by
// path: the in-guest binary has no repo to look in.
func EmbeddedAllowlist() (*Allowlist, error) {
	data, err := allowlistFS.ReadFile(AllowlistFileCst)
	if err != nil {
		// Unreachable short of a build-tag mistake, since go:embed fails at
		// compile time on a missing file. Wrapped rather than panicked so a
		// caller in a check gets a message instead of a stack.
		return nil, fmt.Errorf("nlparity: reading embedded %s: %w", AllowlistFileCst, err)
	}
	return LoadAllowlist(data)
}

// Lookup returns the entry for a command and locus. It is deliberately NOT the
// suppression decision — see Suppresses — so that a report can name the entry
// at a locus whose divergence it is still going to fail on.
func (a *Allowlist) Lookup(command, locus string) (Entry, bool) {
	e, ok := a.byKey[Entry{Command: command, Locus: locus}.key()]
	return e, ok
}

// Suppresses is the only suppression decision in the comparator.
//
// It returns false for every class but DivergenceValue regardless of what the
// file says, which is the header's rule made unbypassable. A caller that wants
// to know whether an entry merely EXISTS wants Lookup.
func (a *Allowlist) Suppresses(command, locus string, class DivergenceClass) bool {
	if !class.Suppressible() {
		return false
	}
	_, ok := a.Lookup(command, locus)
	return ok
}

// IsGated reports whether findings for a command fail the build. A command
// absent from gated_commands is still compared and still reported; the list
// governs the exit code, not the work.
func (a *Allowlist) IsGated(command string) bool { return a.gated[command] }

// EntriesFor returns the entries for one command, in file order. Used by the
// gating report to print what was accepted for a command alongside what was
// not, so a clean run still shows its accepted divergences.
func (a *Allowlist) EntriesFor(command string) []Entry {
	var out []Entry
	for i := range a.Entries {
		if a.Entries[i].Command == command {
			out = append(out, a.Entries[i])
		}
	}
	return out
}
