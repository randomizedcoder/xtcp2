package nlparity

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The differ's table — §8.9's second half, and the one the allowlist's
// trustworthiness rests on.
//
// # Why every row starts from a real capture
//
// A differ's positives are the hard ones to fake honestly. Two hand-built
// segmentations that compare clean prove only that the author built them the
// same way; two that diverge prove only that the author introduced the
// difference the differ then found. So every row here takes a committed guest
// capture as the REFERENCE side and derives the subject side from it by one
// named mutation — drop an attribute, change a value, reorder replies, remove a
// transaction. The mutation is the row's hypothesis about a goip bug, and the
// expectation is the finding it must produce.
//
// That also makes the rows falsifiable in the direction that matters. The plan
// says a parity gate that cannot be made to fail is not a gate, and its
// Verification item 4 is exactly the mutation below named
// "drop IFLA_EXT_MASK from the request": the fixture proves `ip` sends it, and
// the row proves the comparator names it when goip does not.
//
// # The four mutations that are not symmetric
//
// Three rows exist because a value divergence and a presence divergence must
// NOT behave the same way:
//
//   - a value divergence at an allowlisted locus is suppressed;
//   - a PRESENCE divergence at the SAME locus is still reported;
//   - a value divergence whose key appears in D_control is subtracted;
//   - a presence divergence at a locus whose VALUE is noisy in D_control is
//     still reported, because Key includes the class.
//
// The last is the executable form of "suppression applies to value divergences
// only", and it is the one that stops a goip which omits an attribute from
// being covered by that attribute's value being unstable.

// diffRow is one comparison case.
//
// ref is always a real capture. mutate derives the subject from a deep copy of
// it, and naming the mutation in the row description is what keeps the table
// readable as a list of hypotheses rather than of fixtures.
type diffRow struct {
	description string

	command  string
	filename string
	// mutate turns the reference segmentation into the subject one. nil means
	// the two sides are identical, which is the one row that needs no
	// mutation.
	mutate func(s Segmentation) Segmentation
	// refMutate perturbs the REFERENCE side, for the rows whose subject is
	// the ip capture's own quality rather than a goip bug. Applied after the
	// subject is cloned, so it does not leak into it.
	refMutate func(s Segmentation) Segmentation
	// control is D_control, derived the same way. nil means no control was
	// taken.
	controlMutate func(s Segmentation) Segmentation
	// allowlist, when non-nil, is loaded from this JSON instead of the
	// embedded file, so a row can state the entry it depends on inline.
	allowlist string

	// wantFindings is the exact set of Divergence.Key() values expected to
	// survive control subtraction and allowlisting, in no particular order.
	// An empty non-nil slice asserts a clean comparison; nil means the row
	// does not assert the set and uses check instead.
	wantFindings []string
	// wantControlSuppressed and wantAllowSuppressed are the keys expected in
	// each suppression bucket. Asserted because a report that hides what it
	// dropped is a report that cannot be reviewed.
	wantControlSuppressed []string
	wantAllowSuppressed   []string

	wantHygiene int
	wantFailed  bool
	wantErr     error

	check func(t *testing.T, r Report)
}

// cloneSegmentation deep-copies a segmentation so a mutation cannot reach the
// reference side through a shared body. The differ itself never mutates, but a
// TEST that aliased would report a divergence of its own making.
func cloneSegmentation(s Segmentation) Segmentation {
	out := Segmentation{
		Notifications: cloneMsgs(s.Notifications),
		Orphans:       cloneMsgs(s.Orphans),
	}
	if s.Txns != nil {
		out.Txns = make([]Txn, len(s.Txns))
		for i := range s.Txns {
			out.Txns[i] = s.Txns[i]
			out.Txns[i].Request = cloneMsg(s.Txns[i].Request)
			out.Txns[i].Replies = cloneMsgs(s.Txns[i].Replies)
		}
	}
	return out
}

func cloneMsg(m Msg) Msg {
	if m.Body == nil {
		return m
	}
	b := make([]byte, len(m.Body))
	copy(b, m.Body)
	m.Body = b
	return m
}

func cloneMsgs(msgs []Msg) []Msg {
	if msgs == nil {
		return nil
	}
	out := make([]Msg, len(msgs))
	for i := range msgs {
		out[i] = cloneMsg(msgs[i])
	}
	return out
}

// dropAttrs truncates a message's body to its family header, removing every
// attribute. On an `ip link show` request that removes IFLA_EXT_MASK, which is
// the plan's negative verification: the mask changes the REPLY set, so a goip
// that omits it gets IFLA_STATS and IFLA_STATS64 appended by the kernel and
// diverges by the most volatile attribute in the protocol.
func dropAttrs(m Msg) Msg {
	hdrLen := FamilyHdrLen(m.Hdr.Type)
	if hdrLen < 0 || hdrLen > len(m.Body) {
		return m
	}
	m.Body = m.Body[:hdrLen]
	m.Hdr.Len = uint32(xtcpnl.NlMsgHdrSizeCst + hdrLen)
	return m
}

// setAttrU32 overwrites the first four bytes of a bare attribute's payload,
// in place through the aliasing Val slice. Returns false when the attribute is
// absent or too short, so a row cannot silently assert nothing.
func setAttrU32(m Msg, bare uint16, v uint32) bool {
	_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
	for _, a := range attrs {
		if a.BareType() != bare || len(a.Val) < 4 {
			continue
		}
		a.Val[0] = byte(v)
		a.Val[1] = byte(v >> 8)
		a.Val[2] = byte(v >> 16)
		a.Val[3] = byte(v >> 24)
		return true
	}
	return false
}

// mustSetAttrU32 wraps setAttrU32 for use inside a mutate closure, where there
// is no *testing.T. A panic here is a broken row, not a failing assertion, and
// it names itself.
func mustSetAttrU32(m Msg, bare uint16, v uint32) Msg {
	if !setAttrU32(m, bare, v) {
		panic("diff row mutation: attribute not present or too short to set")
	}
	return m
}

// inet6CacheinfoVal returns the IFLA_INET6_CACHEINFO payload of a message,
// two nests down, aliasing the body so a caller can edit it in place. nil when
// the message does not carry one.
func inet6CacheinfoVal(m Msg) []byte {
	_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
	for _, a := range attrs {
		if a.BareType() != uint16(unix.IFLA_AF_SPEC) {
			continue
		}
		fams, _ := SubAttrs(a.Val)
		for _, fam := range fams {
			if fam.BareType() != unix.AF_INET6 {
				continue
			}
			inner, _ := SubAttrs(fam.Val)
			for _, in := range inner {
				if in.BareType() == uint16(unix.IFLA_INET6_CACHEINFO) && len(in.Val) >= 16 {
					return in.Val
				}
			}
		}
	}
	return nil
}

// findingKeys collects the Key() of each divergence, for set comparison.
func findingKeys(ds []Divergence) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Key())
	}
	return out
}

// assertKeySet compares two key sets as multisets and reports both directions,
// because "an extra finding" and "a missing finding" are different bugs.
func assertKeySet(t *testing.T, label string, got, want []string) {
	t.Helper()
	if want == nil {
		return
	}
	if sameStrings(got, want) {
		return
	}
	t.Errorf("%s:\n  got  %v\n  want %v", label, got, want)
}

// TestDiff is the differ's table.
//
// go test ./pkg/nlparity/ -run TestDiff$
func TestDiff(t *testing.T) {
	// The locus the committed allowlist names, spelled as diffRequest derives
	// it. Written out once so every row that depends on it depends on the same
	// string, and so a change to requestRole breaks one constant rather than
	// five rows in five different ways.
	const extMaskDumpLocus = "request:RTM_GETLINK:IFLA_EXT_MASK:dump"

	// A minimal allowlist with one value entry at that locus, inline so the
	// rows that need it state their premise rather than inheriting it from the
	// committed file. It is deliberately shaped like the real entry, including
	// the ip_version a version-skew entry must carry.
	const oneValueEntry = `{
	  "_comment": ["test fixture"],
	  "entries": [
	    {
	      "command": "addr show",
	      "locus": "request:RTM_GETLINK:IFLA_EXT_MASK:dump",
	      "kind": "version-skew",
	      "ip_version": "7.1.0",
	      "reason": "test fixture: the de91e928 mask skew, at the locus the differ derives"
	    }
	  ],
	  "gated_commands": ["addr show"]
	}`

	tests := []diffRow{
		{
			// The baseline. `ip addr show` in the clean guest namespace is the
			// ll_init_map link dump then the address dump: two transactions,
			// one socket, eleven replies. Comparing it against itself has to
			// produce nothing, or every row below is measuring the differ's
			// own noise.
			description:  "positive: a capture compared against itself produces an empty diff",
			command:      "addr show",
			filename:     tdGuestGetAddr,
			mutate:       nil,
			wantFindings: []string{},
		},
		{
			// The clean namespace's link dump, alone: one transaction, twelve
			// replies. A second baseline on a different shape, because a
			// differ that only self-compares two-transaction captures cleanly
			// has not been shown to handle one.
			description:  "positive: the single-transaction link dump also self-compares clean",
			command:      "link show",
			filename:     tdGuest + "/netlink_route_getlink.pcap",
			wantFindings: []string{},
		},
		{
			// The plan's Verification item 4, made executable. The fixture
			// proves `ip` sends IFLA_EXT_MASK on this request; the mutation is
			// a goip that does not. It must be reported as PRESENCE, not as a
			// value, because presence is the unsuppressible class — and the
			// locus must be the one the allowlist names, or the two would not
			// interact at all.
			description: "positive: dropping IFLA_EXT_MASK from the request is reported as a presence divergence at the named locus",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[0].Request = dropAttrs(s.Txns[0].Request)
				return s
			},
			wantFindings: []string{
				"L2|presence|" + extMaskDumpLocus,
			},
			check: func(t *testing.T, r Report) {
				for _, d := range r.Findings {
					if d.Class == DivergencePresence && d.Sub != "" {
						t.Errorf("the goip side is named as present: %s", d)
					}
				}
			},
		},
		{
			// de91e928's one-bit mask change, which is what the committed
			// version-skew entry exists for: 0x09 -> 0x109. With the entry
			// present it is suppressed, and the report still NAMES it in
			// AllowSuppressed so a clean run shows what it accepted.
			description: "positive: a value-only divergence at an allowlisted locus is suppressed and still reported",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			allowlist:   oneValueEntry,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[0].Request = mustSetAttrU32(s.Txns[0].Request,
					uint16(unix.IFLA_EXT_MASK), 0x109)
				return s
			},
			wantFindings:        []string{},
			wantAllowSuppressed: []string{"L2|value|" + extMaskDumpLocus},
		},
		{
			// The control triple's mechanism. D_control is built by making the
			// SAME value change between the two reference captures, so the key
			// matches and the test finding is subtracted. Nothing is
			// allowlisted here: this is the self-adapting half.
			description: "positive: a value divergence whose key is also in D_control is subtracted",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[0].Request = mustSetAttrU32(s.Txns[0].Request,
					uint16(unix.IFLA_EXT_MASK), 0x109)
				return s
			},
			controlMutate: func(s Segmentation) Segmentation {
				// A DIFFERENT value at the same locus, which is the realistic
				// case: a control diff's values differ from a test diff's by
				// definition, which is why Subtract keys on Level|Class|Locus
				// and excludes the values.
				s.Txns[0].Request = mustSetAttrU32(s.Txns[0].Request,
					uint16(unix.IFLA_EXT_MASK), 0x209)
				return s
			},
			wantFindings:          []string{},
			wantControlSuppressed: []string{"L2|value|" + extMaskDumpLocus},
			check: func(t *testing.T, r Report) {
				if r.ControlSize == 0 {
					t.Error("ControlSize = 0, so the row's control diff was empty and " +
						"the subtraction it is testing did not happen")
				}
			},
		},
		{
			// §8.9's negative row, and the reason Key includes the class. The
			// attribute is MISSING in goip, and its VALUE is noisy in
			// D_control. It must still be reported: otherwise a goip that
			// omits an attribute is covered by that attribute's value being
			// unstable, and the bug arrives with its own cover story.
			description: "negative: an attribute missing in goip is reported even when its value is noisy in D_control",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[0].Request = dropAttrs(s.Txns[0].Request)
				return s
			},
			controlMutate: func(s Segmentation) Segmentation {
				s.Txns[0].Request = mustSetAttrU32(s.Txns[0].Request,
					uint16(unix.IFLA_EXT_MASK), 0x209)
				return s
			},
			wantFindings:          []string{"L2|presence|" + extMaskDumpLocus},
			wantControlSuppressed: []string{},
			check: func(t *testing.T, r Report) {
				if r.ControlSize == 0 {
					t.Error("the control diff was empty, so this row does not demonstrate " +
						"that a noisy value failed to cancel a presence finding")
				}
			},
		},
		{
			// The same asymmetry through the allowlist rather than through
			// D_control. Allowlist.Suppresses refuses every class but
			// DivergenceValue whatever the file says, so the entry that
			// suppressed the value row above does nothing here.
			description: "negative: an allowlist entry for a value does not suppress a presence divergence at the same locus",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			allowlist:   oneValueEntry,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[0].Request = dropAttrs(s.Txns[0].Request)
				return s
			},
			wantFindings:        []string{"L2|presence|" + extMaskDumpLocus},
			wantAllowSuppressed: []string{},
			wantFailed:          true, // gated_commands names "addr show"
			check: func(t *testing.T, r Report) {
				// The entry EXISTS at this locus — Lookup finds it — and the
				// finding is reported anyway. That distinction is why Lookup
				// and Suppresses are separate methods.
				al, err := LoadAllowlist([]byte(oneValueEntry))
				if err != nil {
					t.Fatalf("LoadAllowlist: %v", err)
				}
				if _, ok := al.Lookup("addr show", extMaskDumpLocus); !ok {
					t.Fatal("the fixture allowlist has no entry at the locus, so the row asserts nothing")
				}
			},
		},
		{
			// L1. Dropping the ll_init_map link dump is the single highest
			// value assertion in the plan: it is how "goip forgot the
			// RTM_GETLINK before RTM_GETADDR" looks, and it is a count
			// finding, which is unsuppressible.
			description: "negative: a missing transaction is an L1 count failure",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Txns = s.Txns[1:]
				return s
			},
			check: func(t *testing.T, r Report) {
				if r.RefTxns != 2 || r.SubTxns != 1 {
					t.Fatalf("RefTxns/SubTxns = %d/%d, want 2/1", r.RefTxns, r.SubTxns)
				}
				found := false
				for _, d := range r.Findings {
					if d.Level == LevelTxnCount && d.Class == DivergenceTransactionCount {
						found = true
						if d.Ref != "2" || d.Sub != "1" {
							t.Errorf("count finding says ip=%s goip=%s, want 2 and 1", d.Ref, d.Sub)
						}
						if d.Txn != -1 {
							t.Errorf("Txn = %d on a capture-wide finding, want -1", d.Txn)
						}
					}
				}
				if !found {
					t.Errorf("no L1 transaction-count finding in %v", findingKeys(r.Findings))
				}
			},
		},
		{
			// An EXTRA transaction, the other direction of the same finding —
			// §8.9 names this one specifically. The tiers below still run over
			// the transactions that do line up, so the count finding must not
			// be the only one reported when the surplus also shifts the
			// pairing.
			description: "negative: an extra transaction in goip is an L1 count failure too",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Txns = append(s.Txns, s.Txns[0])
				return s
			},
			check: func(t *testing.T, r Report) {
				if r.RefTxns != 2 || r.SubTxns != 3 {
					t.Fatalf("RefTxns/SubTxns = %d/%d, want 2/3", r.RefTxns, r.SubTxns)
				}
				keys := findingKeys(r.Findings)
				want := "L1|transaction-count|txn-count"
				for _, k := range keys {
					if k == want {
						return
					}
				}
				t.Errorf("no %s in %v", want, keys)
			},
		},
		{
			// L2 positional message-type mismatch. Swapping the two
			// transactions makes goip send RTM_GETADDR first, which is a
			// presence finding — the request that should be there is absent —
			// and the tiers below are skipped so the report does not hold a
			// second pile of findings all saying the same thing.
			description: "negative: transactions in the wrong order make the request type mismatch a presence finding",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[0], s.Txns[1] = s.Txns[1], s.Txns[0]
				return s
			},
			check: func(t *testing.T, r Report) {
				var presence int
				for _, d := range r.Findings {
					if d.Level == LevelRequest && d.Class == DivergencePresence {
						presence++
						if !strings.Contains(d.Ref, "RTM_GET") || !strings.Contains(d.Sub, "RTM_GET") {
							t.Errorf("finding does not name both message types: %s", d)
						}
					}
				}
				if presence != 2 {
					t.Errorf("got %d L2 presence findings, want 2 (both positions mismatch): %v",
						presence, findingKeys(r.Findings))
				}
				// And no L2 value findings, because diffRequest returns early
				// on a type mismatch rather than comparing an RTM_GETADDR's
				// body against an RTM_GETLINK's.
				for _, d := range r.Findings {
					if d.Level == LevelRequest && d.Class == DivergenceValue {
						t.Errorf("a value finding survived a type mismatch: %s", d)
					}
				}
			},
		},
		{
			// L3 membership. Dropping one reply is how "goip rendered 3 of 4
			// links" looks, and it is a key-set finding: unsuppressible, and
			// reported with the object key so a reader knows WHICH link.
			description: "negative: a missing reply is an L3 key-set finding naming the object",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[0].Replies = s.Txns[0].Replies[1:]
				return s
			},
			check: func(t *testing.T, r Report) {
				var keySet int
				for _, d := range r.Findings {
					if d.Class != DivergenceKeySet {
						continue
					}
					keySet++
					if d.Object == "" {
						t.Errorf("key-set finding carries no Object: %s", d)
					}
					if d.Sub != "" {
						t.Errorf("key-set finding names the goip side for a reply goip did not send: %s", d)
					}
				}
				if keySet != 1 {
					t.Errorf("got %d key-set findings, want 1: %v", keySet, findingKeys(r.Findings))
				}
			},
		},
		{
			// §8.9's boundary row. Reversing the replies leaves the key SET
			// identical and the key ORDER different, so exactly one ordering
			// finding must appear and membership must stay clean. The two
			// checks are separate tiers precisely so an ordering problem
			// cannot be reported as a membership problem or the reverse.
			description: "boundary: the same reply key set in a different order is an ordering finding only",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				r := s.Txns[1].Replies
				rev := make([]Msg, len(r))
				for i := range r {
					rev[i] = r[len(r)-1-i]
				}
				s.Txns[1].Replies = rev
				return s
			},
			check: func(t *testing.T, r Report) {
				var order, keySet int
				for _, d := range r.Findings {
					switch d.Class {
					case DivergenceKeyOrder:
						order++
					case DivergenceKeySet:
						keySet++
					case DivergenceValue, DivergencePresence, DivergenceTransactionCount,
						DivergenceAttrOrder, DivergenceHygiene:
					}
				}
				if order != 1 {
					t.Errorf("got %d key-order findings, want exactly 1: %v",
						order, findingKeys(r.Findings))
				}
				if keySet != 0 {
					t.Errorf("got %d key-set findings, want 0 — membership is unchanged by a "+
						"reordering, and reporting it as a membership problem is the masking "+
						"the three tiers are split to prevent", keySet)
				}
			},
		},
		{
			// The attribute-order tier, from a REAL pair. In
			// netlink_route_getlink_dev.pcap the two RTM_GETLINK single-gets
			// carry the same two attributes in opposite order:
			// ll_link_get emits IFLA_EXT_MASK then IFLA_IFNAME, and
			// `ip link show dev goip0`'s own get emits IFLA_IFNAME then
			// IFLA_EXT_MASK. Swapping the two transactions therefore compares
			// one real ordering against the other, with no constructed bytes.
			description: "boundary: two real requests with the same attributes in opposite order give an attr-order finding",
			command:     "link show dev",
			filename:    tdGuestLinkDev,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[0], s.Txns[1] = s.Txns[1], s.Txns[0]
				return s
			},
			check: func(t *testing.T, r Report) {
				var attrOrder, presence int
				for _, d := range r.Findings {
					switch d.Class {
					case DivergenceAttrOrder:
						attrOrder++
						if !strings.Contains(d.Ref, "IFLA_EXT_MASK") ||
							!strings.Contains(d.Sub, "IFLA_EXT_MASK") {
							t.Errorf("attr-order finding does not print both type lists: %s", d)
						}
					case DivergencePresence:
						presence++
					case DivergenceValue, DivergenceTransactionCount, DivergenceKeySet,
						DivergenceKeyOrder, DivergenceHygiene:
					}
				}
				if attrOrder == 0 {
					t.Errorf("no attr-order finding; the two transactions really do carry "+
						"IFLA_EXT_MASK and IFLA_IFNAME in opposite orders: %v",
						findingKeys(r.Findings))
				}
				if presence != 0 {
					t.Errorf("got %d presence findings from a pure reordering, want 0: %v",
						presence, findingKeys(r.Findings))
				}
				// And the measured fact that made the port-id check a
				// sentinel: this capture is of TWO sockets, because
				// ll_link_get opened its own. Zero hygiene findings all the
				// same, because every message in it is attributed.
				if len(r.RefPids) != 2 {
					t.Errorf("RefPids = %v, want two port ids — ll_link_get opens its own "+
						"socket, and that is why this is a sentinel rather than a finding",
						r.RefPids)
				}
			},
		},
		{
			// §8.9's corner row: the allowlist-integrity test. The entry is at
			// the `:get` locus and the divergence is at `:dump`, which is what
			// "a divergence that MOVED" looks like — and the check goes red.
			// A locus is specific on purpose, and this is the row that proves
			// the specificity is doing work rather than being decoration.
			description: "corner: an allowlist entry whose locus has moved no longer matches, and the check goes red",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			allowlist: `{
			  "_comment": ["test fixture"],
			  "entries": [
			    {
			      "command": "addr show",
			      "locus": "request:RTM_GETLINK:IFLA_EXT_MASK:get",
			      "kind": "version-skew",
			      "ip_version": "7.1.0",
			      "reason": "test fixture: the right attribute at the WRONG role, which is what a moved locus looks like"
			    }
			  ],
			  "gated_commands": ["addr show"]
			}`,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[0].Request = mustSetAttrU32(s.Txns[0].Request,
					uint16(unix.IFLA_EXT_MASK), 0x109)
				return s
			},
			wantFindings:        []string{"L2|value|" + extMaskDumpLocus},
			wantAllowSuppressed: []string{},
			wantFailed:          true,
		},
		{
			// A reply value divergence, to show the same suppression rule
			// applies three tiers down and keyed on the reply locus rather
			// than the request one. IFA_FLAGS is a u32 on every address in the
			// capture and is not normalized, so changing it must surface.
			description: "positive: a reply attribute value divergence is reported at the reply locus with its object key",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Txns[1].Replies[0] = mustSetAttrU32(s.Txns[1].Replies[0],
					uint16(unix.IFA_FLAGS), 0xDEAD)
				return s
			},
			wantFindings: []string{"L3|value|reply:RTM_NEWADDR:IFA_FLAGS"},
			check: func(t *testing.T, r Report) {
				for _, d := range r.Findings {
					if d.Object == "" {
						t.Errorf("reply finding carries no Object, so the report cannot say "+
							"which address diverged: %s", d)
					}
					if d.Txn < 0 {
						t.Errorf("reply finding carries no Txn: %s", d)
					}
				}
			},
		},
		{
			// Normalization is inside Diff, so a divergence in a volatile
			// field must NOT be reported. This is the row that keeps the
			// normalizer honest from the differ's side: if NormalizeMsg stopped
			// being called, every reply carrying IFA_CACHEINFO would diverge.
			description: "positive: a divergence confined to a normalized field is not reported",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				n := 0
				for i := range s.Txns[1].Replies {
					_, attrs, _ := DecodeAttrs(s.Txns[1].Replies[i].Hdr.Type, s.Txns[1].Replies[i].Body)
					for _, a := range attrs {
						if a.BareType() != uint16(unix.IFA_CACHEINFO) || len(a.Val) < 16 {
							continue
						}
						// cstamp and tstamp only: bytes 8:16, the exact span
						// normalization zeroes. The lifetimes at 0:8 are left
						// alone, because changing them SHOULD be reported.
						for j := 8; j < 16; j++ {
							a.Val[j] ^= 0xFF
						}
						n++
					}
				}
				if n == 0 {
					panic("diff row mutation: no IFA_CACHEINFO to perturb")
				}
				return s
			},
			wantFindings: []string{},
		},
		{
			// And the other half of that claim: the same attribute's STABLE
			// half is compared. A row that only asserted the suppression would
			// pass for a differ that dropped IFA_CACHEINFO entirely.
			description: "negative: a divergence in the COMPARED half of a normalized attribute is reported",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				n := 0
				for i := range s.Txns[1].Replies {
					_, attrs, _ := DecodeAttrs(s.Txns[1].Replies[i].Hdr.Type, s.Txns[1].Replies[i].Body)
					for _, a := range attrs {
						if a.BareType() != uint16(unix.IFA_CACHEINFO) || len(a.Val) < 16 {
							continue
						}
						// preferred, the first u32: INFINITY_LIFE_TIME on
						// every address, so a goip that synthesized "forever"
						// wrongly would show up exactly here.
						a.Val[0] = 0
						n++
						break
					}
					if n > 0 {
						break
					}
				}
				if n == 0 {
					panic("diff row mutation: no IFA_CACHEINFO to perturb")
				}
				return s
			},
			wantFindings: []string{"L3|value|reply:RTM_NEWADDR:IFA_CACHEINFO"},
		},
		{
			// THE motivating case for descending into IFLA_AF_SPEC.
			// reachable_time is deliberately left compared by the normalizer —
			// the kernel recomputes it every few minutes, so two adjacent
			// captures can straddle a recompute — with the stated intent that
			// it surface as a volatile-fallback allowlist entry. That only
			// works if the locus is narrow enough to allowlist: an entry at
			// `reply:RTM_NEWLINK:IFLA_AF_SPEC` would suppress every value
			// difference anywhere under AF_SPEC to get this one.
			description: "positive: a divergence two nests deep gets a locus naming the family and the attribute",
			command:     "link show",
			filename:    tdGuest + "/netlink_route_getlink.pcap",
			mutate: func(s Segmentation) Segmentation {
				n := 0
				for i := range s.Txns[0].Replies {
					v := inet6CacheinfoVal(s.Txns[0].Replies[i])
					if v == nil {
						continue
					}
					// reachable_time, the third u32: bytes 8:12. NOT tstamp at
					// 4:8, which normalization zeroes and which the row above
					// covers.
					v[8] ^= 0xFF
					n++
					break
				}
				if n == 0 {
					panic("diff row mutation: no IFLA_INET6_CACHEINFO to perturb")
				}
				return s
			},
			wantFindings: []string{
				"L3|value|reply:RTM_NEWLINK:IFLA_AF_SPEC:AF_INET6:IFLA_INET6_CACHEINFO",
			},
			check: func(t *testing.T, r Report) {
				// And the entry that would be written for it is a VALUE entry,
				// so it is suppressible — which is the whole point of getting
				// the locus right rather than reporting the blob.
				if !r.Findings[0].Class.Suppressible() {
					t.Error("the nested finding is not suppressible, so no volatile-fallback " +
						"entry could ever be written for reachable_time")
				}
			},
		},
		{
			// tstamp, one field earlier in the same payload, is zeroed by
			// normalization. The pair of rows is what shows the descent did
			// not defeat the normalizer by comparing the nest byte-for-byte.
			description: "positive: the normalized field inside the same nested attribute is still not reported",
			command:     "link show",
			filename:    tdGuest + "/netlink_route_getlink.pcap",
			mutate: func(s Segmentation) Segmentation {
				n := 0
				for i := range s.Txns[0].Replies {
					v := inet6CacheinfoVal(s.Txns[0].Replies[i])
					if v == nil {
						continue
					}
					for j := 4; j < 8; j++ { // tstamp
						v[j] ^= 0xFF
					}
					n++
				}
				if n == 0 {
					panic("diff row mutation: no IFLA_INET6_CACHEINFO to perturb")
				}
				return s
			},
			wantFindings: []string{},
		},
		{
			// A capture with no transactions cannot be compared at all, and
			// Compare must say so rather than reporting a clean run over
			// nothing. Failed() is true regardless of gating.
			description: "negative: a subject capture with no transactions is an error, not a clean comparison",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Txns = nil
				return s
			},
			wantErr:    ErrNoTransactions,
			wantFailed: true,
			check: func(t *testing.T, r Report) {
				if len(r.Findings) != 0 {
					t.Errorf("Findings = %v on an unusable capture; nothing was compared",
						findingKeys(r.Findings))
				}
			},
		},
		{
			// Hygiene is a fact about the CAPTURE, so it is reported from both
			// sides, kept out of Findings, and fails regardless of gating —
			// "addr show" is not in gated_commands here.
			description: "negative: a multicast notification on the goip side is a hygiene failure and fails an ungated command",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate: func(s Segmentation) Segmentation {
				s.Notifications = append(s.Notifications, Msg{
					Hdr: xtcpnl.NlMsgHdr{
						Len:  uint32(xtcpnl.NlMsgHdrSizeCst),
						Type: uint16(unix.RTM_NEWADDR),
					},
				})
				return s
			},
			wantFindings: []string{},
			wantHygiene:  1,
			wantFailed:   true,
			check: func(t *testing.T, r Report) {
				if r.Gated {
					t.Fatal("the row depends on the command being UNGATED, and it is gated")
				}
				for _, d := range r.Hygiene {
					if d.Class != DivergenceHygiene {
						t.Errorf("hygiene finding has class %s", d.Class)
					}
					if d.Class.Suppressible() {
						t.Error("a hygiene finding is suppressible, which would let an " +
							"allowlist entry make every other finding in the run meaningless")
					}
					if !strings.Contains(d.Locus, "goip") {
						t.Errorf("hygiene locus %q does not say which side it came from", d.Locus)
					}
				}
			},
		},
		{
			// The other hygiene shape, and the one that actually says the
			// capture caught someone else's traffic: a reply with no request
			// in the window. It is reported from the ip side, so the report
			// says WHICH capture was dirty rather than only that one was.
			description: "negative: an orphaned reply on the ip side is a hygiene failure naming that side",
			command:     "addr show",
			filename:    tdGuestGetAddr,
			mutate:      func(s Segmentation) Segmentation { return s },
			refMutate: func(s Segmentation) Segmentation {
				s.Orphans = append(s.Orphans,
					segReplySingle(uint16(unix.RTM_NEWLINK), 12345, 999))
				return s
			},
			wantFindings: []string{},
			wantHygiene:  1,
			wantFailed:   true,
			check: func(t *testing.T, r Report) {
				if !strings.Contains(r.Hygiene[0].Locus, ":ip:") {
					t.Errorf("hygiene locus %q does not name the ip side", r.Hygiene[0].Locus)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c, err := ParseRouteCaptureFile(tc.filename)
			if err != nil {
				t.Fatalf("%s: %v", tc.filename, err)
			}
			ref := SegmentCapture(c)
			if len(ref.Txns) == 0 {
				t.Fatalf("%s segments to no transactions, so the row has no reference side",
					tc.filename)
			}

			sub := cloneSegmentation(ref)
			if tc.mutate != nil {
				sub = tc.mutate(sub)
			}
			if tc.refMutate != nil {
				ref = tc.refMutate(ref)
			}

			var control []Divergence
			if tc.controlMutate != nil {
				control = Diff(tc.command, ref, tc.controlMutate(cloneSegmentation(ref)))
			}

			var al *Allowlist
			if tc.allowlist != "" {
				al, err = LoadAllowlist([]byte(tc.allowlist))
				if err != nil {
					t.Fatalf("LoadAllowlist: %v", err)
				}
			}

			r := Compare(tc.command, ref, sub, control, al)

			switch {
			case tc.wantErr != nil:
				if !errors.Is(r.Err, tc.wantErr) {
					t.Fatalf("Err = %v, want %v", r.Err, tc.wantErr)
				}
			case r.Err != nil:
				t.Fatalf("unexpected Err = %v", r.Err)
			}

			assertKeySet(t, "Findings", findingKeys(r.Findings), tc.wantFindings)
			assertKeySet(t, "ControlSuppressed", findingKeys(r.ControlSuppressed), tc.wantControlSuppressed)
			assertKeySet(t, "AllowSuppressed", findingKeys(r.AllowSuppressed), tc.wantAllowSuppressed)

			if len(r.Hygiene) != tc.wantHygiene {
				t.Errorf("Hygiene = %d findings, want %d: %v", len(r.Hygiene), tc.wantHygiene, r.Hygiene)
			}
			if got := r.Failed(); got != tc.wantFailed {
				t.Errorf("Failed() = %v, want %v (gated=%v, %d findings, %d hygiene, err=%v)",
					got, tc.wantFailed, r.Gated, len(r.Findings), len(r.Hygiene), r.Err)
			}
			if tc.check != nil {
				tc.check(t, r)
			}
		})
	}
}

// TestObjectKey is the pairing table. A wrong key here does not produce a
// wrong answer — it produces a report full of key-set findings that all say
// "these two captures describe different objects", which is the least useful
// possible failure.
//
// go test ./pkg/nlparity/ -run TestObjectKey
func TestObjectKey(t *testing.T) {
	// Real messages, pulled out of the captures by type, so the positives are
	// keys the differ will actually compute on the gated corpus.
	first := func(t *testing.T, path string, msgType uint16) Msg {
		t.Helper()
		c, err := ParseRouteCaptureFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, m := range c.Msgs() {
			if m.Hdr.Type == msgType {
				return m
			}
		}
		t.Fatalf("%s holds no %s", path, MsgTypeName(msgType))
		return Msg{}
	}

	tests := []struct {
		description string
		msg         func(t *testing.T) Msg
		want        string
		wantPrefix  string
	}{
		{
			description: "positive: a link keys on ifi_index alone, because that is what names a link",
			msg: func(t *testing.T) Msg {
				return first(t, tdGuest+"/netlink_route_getlink.pcap", uint16(unix.RTM_NEWLINK))
			},
			want: "link/ifindex=1",
		},
		{
			description: "positive: an address keys on family, index, prefixlen and IFA_ADDRESS",
			msg:         func(t *testing.T) Msg { return first(t, tdGuestGetAddr, uint16(unix.RTM_NEWADDR)) },
			want:        "addr/family=2/index=1/prefixlen=8/addr=7f000001",
		},
		{
			// c0000232 is 192.0.2.50, and WHICH entry that is depends on the
			// capture rather than the topology: `first` takes the first
			// RTM_NEWNEIGH in the pcap and the kernel walks its hash table
			// in hash order. The claim under test is the key's SHAPE — that
			// NDA_DST is hex-encoded and joins family and ifindex — so a
			// re-capture that reorders the dump moves this literal without
			// anything being wrong. It read c0000234 (192.0.2.52) under the
			// previous clean capture.
			description: "positive: a neighbor keys on family, ifindex and NDA_DST",
			msg:         func(t *testing.T) Msg { return first(t, tdGuestGetNeigh, uint16(unix.RTM_NEWNEIGH)) },
			want:        "neigh/family=2/ifindex=3/dst=c0000232",
		},
		{
			description: "positive: a route keys on family, table, dst_len, RTA_DST, RTA_OIF and RTA_PRIORITY",
			msg: func(t *testing.T) Msg {
				return first(t, tdGuest+"/netlink_route_getroute_table_all.pcap", uint16(unix.RTM_NEWROUTE))
			},
			wantPrefix: "route/family=",
		},
		{
			// A terminator has no object, and keying it on its type alone is
			// sufficient: a transaction has at most one, and keeping it in the
			// key sequence is what lets the ordering check see whether it
			// arrived last.
			description: "boundary: NLMSG_DONE keys on its type alone",
			msg: func(t *testing.T) Msg {
				return Msg{Hdr: xtcpnl.NlMsgHdr{Type: uint16(unix.NLMSG_DONE)}, Body: []byte{0, 0, 0, 0}}
			},
			want: "NLMSG_DONE",
		},
		{
			description: "boundary: NLMSG_ERROR keys on its type alone too",
			msg: func(t *testing.T) Msg {
				return Msg{Hdr: xtcpnl.NlMsgHdr{Type: uint16(unix.NLMSG_ERROR)}, Body: make([]byte, 20)}
			},
			want: "NLMSG_ERROR",
		},
		{
			// A body too short for its family header cannot be keyed on the
			// object, and the length goes into the key so two truncated
			// messages of different lengths do not pair — which would turn a
			// truncation into a value divergence.
			description: "negative: a body too short for its ifinfomsg keys on the type and its length",
			msg: func(t *testing.T) Msg {
				return Msg{Hdr: xtcpnl.NlMsgHdr{Type: uint16(unix.RTM_NEWLINK)}, Body: []byte{1, 2, 3}}
			},
			want: "RTM_NEWLINK/short=3",
		},
		{
			description: "negative: an empty body on a modeled type keys short with length 0",
			msg: func(t *testing.T) Msg {
				return Msg{Hdr: xtcpnl.NlMsgHdr{Type: uint16(unix.RTM_NEWADDR)}}
			},
			want: "RTM_NEWADDR/short=0",
		},
		{
			// An address with no IFA_ADDRESS keys with an empty addr rather
			// than being unkeyable, so it still pairs with its counterpart.
			description: "boundary: an address with no IFA_ADDRESS keys with an empty address field",
			msg: func(t *testing.T) Msg {
				return Msg{
					Hdr:  xtcpnl.NlMsgHdr{Type: uint16(unix.RTM_NEWADDR)},
					Body: []byte{unix.AF_INET, 24, 0, 0, 7, 0, 0, 0},
				}
			},
			want: "addr/family=2/index=7/prefixlen=24/addr=",
		},
		{
			// RTA_TABLE overrides the 8-bit rtm_table, because a table id
			// above 255 does not fit in the header field and lives in the
			// attribute instead.
			description: "corner: RTA_TABLE overrides the 8-bit rtm_table when both are present",
			msg: func(t *testing.T) Msg {
				m := attrMsg(uint16(unix.RTM_NEWROUTE), xtcpnl.RtMsgSizeCst,
					uint16(unix.RTA_TABLE), []byte{0x10, 0x27, 0, 0}) // 10000
				m.Body[4] = 254 // rtm_table = RT_TABLE_MAIN
				return m
			},
			wantPrefix: "route/family=0/table=10000/",
		},
		{
			description: "corner: an unmodeled message type keys on its numeric name",
			msg: func(t *testing.T) Msg {
				return Msg{Hdr: xtcpnl.NlMsgHdr{Type: uint16(unix.RTM_NEWRULE)}}
			},
			want: "type(32)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := ObjectKey(tc.msg(t))
			switch {
			case tc.wantPrefix != "":
				if !strings.HasPrefix(got, tc.wantPrefix) {
					t.Errorf("ObjectKey = %q, want prefix %q", got, tc.wantPrefix)
				}
			default:
				if got != tc.want {
					t.Errorf("ObjectKey = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// TestHygieneDiff covers the capture-quality tier, which is the one that must
// never be suppressible.
//
// go test ./pkg/nlparity/ -run TestHygieneDiff
func TestHygieneDiff(t *testing.T) {
	// A bound, closed, unambiguous transaction on one pid: the shape the clean
	// guest captures have, so a row can add exactly one defect to it.
	clean := func() Segmentation {
		return Segmentation{Txns: []Txn{{
			Request: segReq(uint16(unix.RTM_GETLINK), 100),
			Replies: []Msg{segReplyMulti(uint16(unix.RTM_NEWLINK), 100, 7)},
			Seq:     100, Pid: 7, State: TxnClosedByDone,
		}}}
	}

	tests := []struct {
		description string
		seg         func() Segmentation
		wantLoci    []string
	}{
		{
			description: "positive: a bound, closed, single-pid segmentation has no hygiene findings",
			seg:         clean,
			wantLoci:    nil,
		},
		{
			// A read-only cut asks and is answered; a multicast event means the
			// capture window caught traffic nobody in it requested. Reported
			// and failed, not dropped.
			description: "negative: a multicast notification is reported rather than dropped",
			seg: func() Segmentation {
				s := clean()
				s.Notifications = []Msg{segNotification(uint16(unix.RTM_NEWADDR))}
				return s
			},
			wantLoci: []string{"hygiene:ip:notifications"},
		},
		{
			description: "negative: an orphaned reply is reported",
			seg: func() Segmentation {
				s := clean()
				s.Orphans = []Msg{segReplySingle(uint16(unix.RTM_NEWLINK), 999, 12345)}
				return s
			},
			wantLoci: []string{"hygiene:ip:orphans"},
		},
		{
			description: "negative: an ambiguous transaction is reported",
			seg: func() Segmentation {
				s := clean()
				s.Txns[0].Ambiguous = true
				return s
			},
			wantLoci: []string{"hygiene:ip:ambiguous"},
		},
		{
			description: "negative: an unterminated transaction is reported",
			seg: func() Segmentation {
				s := clean()
				s.Txns[0].State = TxnOpen
				return s
			},
			wantLoci: []string{"hygiene:ip:open"},
		},
		{
			// MEASURED, and the reason this is a negative row rather than a
			// positive one. The plan called a second port id a hygiene
			// failure; ll_link_get calls rtnl_open(&rth, 0) per invocation, so
			// one `ip` process answers on one port id per name it resolves.
			// Eight of the eighteen committed guest captures do this in the
			// CLEAN namespace with zero orphans and zero notifications, so a
			// hygiene arm here would make `route show` and `link show dev`
			// permanently red — and unfixably, since DivergenceHygiene is
			// unsuppressible by construction.
			description: "negative: two port ids with everything attributed is NOT a hygiene failure",
			seg: func() Segmentation {
				s := clean()
				s.Txns = append(s.Txns, Txn{
					Request: segReq(uint16(unix.RTM_GETADDR), 200),
					Replies: []Msg{segReplyMulti(uint16(unix.RTM_NEWADDR), 200, 9)},
					Seq:     200, Pid: 9, State: TxnClosedByDone,
				})
				return s
			},
			wantLoci: nil,
		},
		{
			// One finding per defect KIND, not per occurrence: a capture with
			// four ambiguous transactions is one problem, and four identical
			// lines would bury the others.
			description: "boundary: two ambiguous transactions produce one finding, not two",
			seg: func() Segmentation {
				s := clean()
				s.Txns[0].Ambiguous = true
				s.Txns = append(s.Txns, Txn{
					Request: segReq(uint16(unix.RTM_GETADDR), 200),
					Replies: []Msg{segReplyMulti(uint16(unix.RTM_NEWADDR), 200, 7)},
					Seq:     200, Pid: 7, State: TxnClosedByDone, Ambiguous: true,
				})
				return s
			},
			wantLoci: []string{"hygiene:ip:ambiguous"},
		},
		{
			description: "boundary: an empty segmentation has nothing to report, which CheckUsable catches instead",
			seg:         func() Segmentation { return Segmentation{} },
			wantLoci:    nil,
		},
		{
			description: "corner: every defect at once produces one finding each",
			seg: func() Segmentation {
				s := clean()
				s.Notifications = []Msg{segNotification(uint16(unix.RTM_NEWADDR))}
				s.Orphans = []Msg{segReplySingle(uint16(unix.RTM_NEWLINK), 999, 12345)}
				s.Txns[0].Ambiguous = true
				s.Txns[0].State = TxnOpen
				return s
			},
			wantLoci: []string{
				"hygiene:ip:notifications",
				"hygiene:ip:orphans",
				"hygiene:ip:ambiguous",
				"hygiene:ip:open",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := HygieneDiff("link show", "ip", tc.seg())

			var loci []string
			for _, d := range got {
				loci = append(loci, d.Locus)
				if d.Level != LevelHygiene {
					t.Errorf("finding %s has level %s, want %s", d.Locus, d.Level, LevelHygiene)
				}
				if d.Class != DivergenceHygiene {
					t.Errorf("finding %s has class %s, want %s", d.Locus, d.Class, DivergenceHygiene)
				}
				if d.Txn != -1 {
					t.Errorf("finding %s has Txn %d, want -1: a hygiene fact is capture-wide",
						d.Locus, d.Txn)
				}
				if d.Sub == "" {
					t.Errorf("finding %s carries no detail", d.Locus)
				}
			}
			if !sameStrings(loci, tc.wantLoci) {
				t.Errorf("loci:\n  got  %v\n  want %v", loci, tc.wantLoci)
			}
		})
	}
}

// TestCheckUsable covers the one precondition Compare refuses to work without.
//
// go test ./pkg/nlparity/ -run TestCheckUsable
func TestCheckUsable(t *testing.T) {
	tests := []struct {
		description string
		seg         Segmentation
		wantErr     error
	}{
		{
			description: "positive: one transaction is enough to compare",
			seg:         Segmentation{Txns: []Txn{{Seq: 1}}},
		},
		{
			description: "negative: no transactions is an error, because a clean report over nothing is a lie",
			seg:         Segmentation{},
			wantErr:     ErrNoTransactions,
		},
		{
			// The error names the notification and orphan counts, so a reader
			// can tell "the capture window missed the command" from "the
			// capture caught only someone else's traffic".
			description: "negative: notifications without transactions is still unusable, and the error says so",
			seg: Segmentation{
				Notifications: []Msg{segNotification(uint16(unix.RTM_NEWADDR))},
				Orphans:       []Msg{segReplySingle(uint16(unix.RTM_NEWLINK), 1, 2)},
			},
			wantErr: ErrNoTransactions,
		},
		{
			description: "boundary: an empty non-nil transaction slice is the same as nil",
			seg:         Segmentation{Txns: []Txn{}},
			wantErr:     ErrNoTransactions,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := CheckUsable(tc.seg)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CheckUsable = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil {
				return
			}
			if n := len(tc.seg.Notifications); n != 0 && !strings.Contains(err.Error(), "notification") {
				t.Errorf("error %q does not report the %d notifications", err, n)
			}
		})
	}
}

// TestSubtract covers the control-triple mechanism on its own, where the
// class-in-the-key property can be stated without building two captures.
//
// go test ./pkg/nlparity/ -run TestSubtract
func TestSubtract(t *testing.T) {
	d := func(level Level, class DivergenceClass, locus, ref, sub string) Divergence {
		return Divergence{Command: "link show", Level: level, Class: class, Locus: locus,
			Txn: 0, Ref: ref, Sub: sub}
	}

	tests := []struct {
		description   string
		test, control []Divergence
		want          []string
	}{
		{
			description: "positive: a finding whose key is in the control is removed",
			test:        []Divergence{d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "a", "b")},
			control:     []Divergence{d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "c", "d")},
			want:        nil,
		},
		{
			// The values differing is the NORMAL case, not an edge one: a
			// control diff's values differ from a test diff's by definition,
			// which is why Key excludes them.
			description: "positive: the values are excluded from the key, so different values still cancel",
			test:        []Divergence{d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "05dc", "0500")},
			control:     []Divergence{d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "ffff", "0000")},
			want:        nil,
		},
		{
			// THE row. Key includes the class, so a noisy VALUE cannot cancel
			// a missing ATTRIBUTE at the same locus. This is the structural
			// half of "suppression applies to value divergences only".
			description: "negative: a noisy value does not cancel a presence divergence at the same locus",
			test:        []Divergence{d(LevelReply, DivergencePresence, "reply:RTM_NEWLINK:IFLA_MTU", "4 bytes", "")},
			control:     []Divergence{d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "a", "b")},
			want:        []string{"L3|presence|reply:RTM_NEWLINK:IFLA_MTU"},
		},
		{
			description: "negative: a noisy locus does not cancel a finding at a different level",
			test:        []Divergence{d(LevelRequest, DivergenceValue, "request:RTM_GETLINK:IFLA_EXT_MASK:dump", "a", "b")},
			control:     []Divergence{d(LevelReply, DivergenceValue, "request:RTM_GETLINK:IFLA_EXT_MASK:dump", "a", "b")},
			want:        []string{"L2|value|request:RTM_GETLINK:IFLA_EXT_MASK:dump"},
		},
		{
			description: "negative: a transaction count finding is never canceled by a value one",
			test:        []Divergence{d(LevelTxnCount, DivergenceTransactionCount, "txn-count", "2", "1")},
			control:     []Divergence{d(LevelTxnCount, DivergenceValue, "txn-count", "2", "2")},
			want:        []string{"L1|transaction-count|txn-count"},
		},
		{
			description: "boundary: an empty control returns the test findings unchanged",
			test:        []Divergence{d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "a", "b")},
			control:     nil,
			want:        []string{"L3|value|reply:RTM_NEWLINK:IFLA_MTU"},
		},
		{
			description: "boundary: an empty test set stays empty however noisy the control",
			test:        nil,
			control:     []Divergence{d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "a", "b")},
			want:        nil,
		},
		{
			// Two findings at one key both go, because the control says that
			// key is noisy — not that one occurrence of it is.
			description: "corner: two test findings sharing a key are both removed by one control entry",
			test: []Divergence{
				d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "a", "b"),
				d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "c", "d"),
			},
			control: []Divergence{d(LevelReply, DivergenceValue, "reply:RTM_NEWLINK:IFLA_MTU", "e", "f")},
			want:    nil,
		},
		{
			// The diagnostics are excluded from the key on purpose: an ifindex
			// is boot-assigned, so a key containing txn or object would stop
			// canceling after a reboot in a way that mimics working.
			description: "corner: Txn and Object are excluded from the key, so a finding cancels across transactions",
			test: []Divergence{{Command: "link show", Level: LevelReply, Class: DivergenceValue,
				Locus: "reply:RTM_NEWLINK:IFLA_MTU", Txn: 5, Object: "link/ifindex=9"}},
			control: []Divergence{{Command: "link show", Level: LevelReply, Class: DivergenceValue,
				Locus: "reply:RTM_NEWLINK:IFLA_MTU", Txn: 0, Object: "link/ifindex=3"}},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := findingKeys(Subtract(tc.test, tc.control))
			if !sameStrings(got, tc.want) {
				t.Errorf("Subtract:\n  got  %v\n  want %v", got, tc.want)
			}
		})
	}
}

// TestDivergenceRendering covers Key and String, which are the two things a
// reviewer of a failed check actually reads.
//
// go test ./pkg/nlparity/ -run TestDivergenceRendering
func TestDivergenceRendering(t *testing.T) {
	tests := []struct {
		description string
		d           Divergence
		wantKey     string
		wantIn      []string
		// wantNotIn is asserted against Key, wantNotInStr against String. Two
		// fields rather than one because the two have different jobs: Key must
		// not carry a diagnostic that moves between runs, and String must not
		// print the same word twice.
		wantNotIn    []string
		wantNotInStr []string
	}{
		{
			description: "positive: a reply value divergence keys on level, class and locus, and prints its diagnostics",
			d: Divergence{Level: LevelReply, Class: DivergenceValue,
				Locus: "reply:RTM_NEWLINK:IFLA_MTU", Txn: 1, Object: "link/ifindex=3",
				Ref: "dc05", Sub: "0005"},
			wantKey: "L3|value|reply:RTM_NEWLINK:IFLA_MTU",
			wantIn:  []string{"txn[1]", "link/ifindex=3", "ip=dc05", "goip=0005"},
		},
		{
			// The diagnostics are in String and NOT in Key. A locus containing
			// a boot-assigned ifindex would silently stop matching an
			// allowlist entry after a reboot, which looks exactly like the
			// allowlist working.
			description: "positive: the object key is printed but excluded from the key",
			d: Divergence{Level: LevelReply, Class: DivergenceValue,
				Locus: "reply:RTM_NEWLINK:IFLA_MTU", Txn: 1, Object: "link/ifindex=3"},
			wantKey:   "L3|value|reply:RTM_NEWLINK:IFLA_MTU",
			wantNotIn: []string{"ifindex=3|", "|1|"},
		},
		{
			description: "boundary: an absent goip side renders as <absent> rather than as an empty value",
			d: Divergence{Level: LevelRequest, Class: DivergencePresence,
				Locus: "request:RTM_GETLINK:IFLA_EXT_MASK:dump", Txn: 0, Ref: "4 bytes"},
			wantKey: "L2|presence|request:RTM_GETLINK:IFLA_EXT_MASK:dump",
			wantIn:  []string{"goip=<absent>"},
		},
		{
			description: "boundary: an absent ip side renders as <absent> too",
			d: Divergence{Level: LevelReply, Class: DivergenceKeySet,
				Locus: "reply:key-set", Txn: 0, Sub: "link/ifindex=9"},
			wantKey: "L3|key-set|reply:key-set",
			wantIn:  []string{"ip=<absent>"},
		},
		{
			// Txn -1 is the capture-wide sentinel, and printing "txn[-1]"
			// would invite a reader to look for transaction -1.
			description: "corner: a capture-wide finding omits the transaction index entirely",
			d: Divergence{Level: LevelTxnCount, Class: DivergenceTransactionCount,
				Locus: "txn-count", Txn: -1, Ref: "2", Sub: "1"},
			wantKey:   "L1|transaction-count|txn-count",
			wantNotIn: []string{"txn["},
		},
		{
			description: "corner: a hygiene finding renders its level as a word rather than a number",
			d: Divergence{Level: LevelHygiene, Class: DivergenceHygiene,
				Locus: "hygiene:goip:orphans", Txn: -1, Sub: "3 replies with no open transaction"},
			wantKey: "hygiene|hygiene|hygiene:goip:orphans",
			wantIn:  []string{"3 replies"},
		},
		{
			// LevelHygiene with DivergenceHygiene is the only Level/Class pair
			// that renders as the same word, and printing both put "hygiene"
			// three times in a row in the harness's report, since the locus
			// carries the prefix as well. Key is unchanged: it is the persisted
			// identity, and shortening it would repoint every hygiene key.
			description: "corner: String prints the level once when the class is the same word",
			d: Divergence{Level: LevelHygiene, Class: DivergenceHygiene,
				Locus: "hygiene:goip:orphans", Txn: -1, Sub: "3 replies with no open transaction"},
			wantKey:      "hygiene|hygiene|hygiene:goip:orphans",
			wantIn:       []string{"hygiene hygiene:goip:orphans"},
			wantNotInStr: []string{"hygiene hygiene hygiene"},
		},
		{
			// The negative half of the row above. A Level and Class that differ
			// must both still print, or every other finding loses the word that
			// says whether it is a value, a presence or an ordering.
			description: "negative: a finding whose level and class differ prints both",
			d: Divergence{Level: LevelReply, Class: DivergenceAttrOrder,
				Locus: "reply:RTM_NEWLINK:attr-order", Txn: 0, Ref: "1,2,3", Sub: "1,3,2"},
			wantKey:      "L3|attr-order|reply:RTM_NEWLINK:attr-order",
			wantIn:       []string{"L3 attr-order reply:RTM_NEWLINK:attr-order"},
			wantNotInStr: []string{"L3 reply:RTM_NEWLINK:attr-order"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.d.Key(); got != tc.wantKey {
				t.Errorf("Key = %q, want %q", got, tc.wantKey)
			}
			s := tc.d.String()
			for _, want := range tc.wantIn {
				if !strings.Contains(s, want) {
					t.Errorf("String = %q, does not contain %q", s, want)
				}
			}
			for _, notWant := range tc.wantNotIn {
				if strings.Contains(tc.d.Key(), notWant) {
					t.Errorf("Key = %q, must not contain %q", tc.d.Key(), notWant)
				}
			}
			for _, notWant := range tc.wantNotInStr {
				if strings.Contains(s, notWant) {
					t.Errorf("String = %q, must not contain %q", s, notWant)
				}
			}
		})
	}
}

// TestRequestRole covers the derivation the committed allowlist's loci depend
// on. A comparator cannot see iproute2's ll_init_map or ll_link_get, but it can
// see NLM_F_DUMP, and that is exactly the distinction between them.
//
// go test ./pkg/nlparity/ -run TestRequestRole
func TestRequestRole(t *testing.T) {
	tests := []struct {
		description string
		flags       uint16
		want        string
	}{
		{
			// 0x0301 = NLM_F_REQUEST|NLM_F_ROOT|NLM_F_MATCH, i.e.
			// NLM_F_REQUEST|NLM_F_DUMP, which is what every dump in the corpus
			// carries.
			description: "positive: the captured dump flags 0x0301 are a dump",
			flags:       0x0301,
			want:        "dump",
		},
		{
			description: "positive: the captured single-get flags 0x0001 are a get",
			flags:       0x0001,
			want:        "get",
		},
		{
			// NLM_F_DUMP is ROOT|MATCH, so ROOT alone is not a dump. That
			// matters: a goip that set only ROOT would be a different request,
			// and calling it a dump would put its divergence at the dump
			// locus where an allowlist entry might be waiting.
			description: "boundary: NLM_F_ROOT without NLM_F_MATCH is not a dump",
			flags:       uint16(unix.NLM_F_REQUEST | unix.NLM_F_ROOT),
			want:        "get",
		},
		{
			description: "boundary: NLM_F_MATCH without NLM_F_ROOT is not a dump either",
			flags:       uint16(unix.NLM_F_REQUEST | unix.NLM_F_MATCH),
			want:        "get",
		},
		{
			description: "negative: no flags at all is a get, not an error — the role has only two values",
			flags:       0,
			want:        "get",
		},
		{
			description: "corner: every flag set is still a dump, because the test is a mask and not equality",
			flags:       0xFFFF,
			want:        "dump",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := requestRole(Msg{Hdr: xtcpnl.NlMsgHdr{Flags: tc.flags}})
			if got != tc.want {
				t.Errorf("requestRole(flags=0x%04x) = %q, want %q", tc.flags, got, tc.want)
			}
		})
	}
}

// TestCommittedAllowlistLociAreDerivable is the reconciliation guard.
//
// Every non-stdout locus in the committed allowlist must be a string the differ
// can actually emit. An entry whose locus is a human's name for a call site —
// "request:RTM_GETLINK:IFLA_EXT_MASK:ll_init_map", as these two read before the
// differ existed — loads and validates and looks reviewed, and matches nothing
// forever. That is a worse failure than a missing entry, because it presents as
// a check that is passing.
//
// The construction here is the real one: take the capture the entry is about,
// mutate the attribute it names, and assert the divergence comes out at exactly
// that locus.
//
// go test ./pkg/nlparity/ -run TestCommittedAllowlistLociAreDerivable
func TestCommittedAllowlistLociAreDerivable(t *testing.T) {
	al, err := EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}

	tests := []struct {
		description string
		command     string
		locus       string
		filename    string
		// txn is the transaction whose request carries the attribute, and
		// wantMask is the value the pinned ip was recorded sending — asserted
		// so a row cannot pass against a capture that does not hold what the
		// entry's reason claims.
		txn      int
		wantMask uint32
	}{
		{
			// MEASURED: 0x01, not 0x09. 7.1.0's ll_init_map calls
			// rtnl_linkdump_req(rth, AF_UNSPEC), which forwards
			// RTEXT_FILTER_VF alone — so this is the one place in the corpus
			// where the mask is not 0x09, and the entry's reason says so.
			description: "positive: the ll_init_map dump locus is derivable, and its pinned mask is 0x01",
			command:     "neigh show",
			locus:       "request:RTM_GETLINK:IFLA_EXT_MASK:dump",
			filename:    tdGuestGetNeigh,
			txn:         0,
			wantMask:    0x01,
		},
		{
			// The same locus on a second command, because Entry.key() is
			// command plus locus and the `neigh show` row above therefore
			// does not reach this one. The mask is 0x01 here for the
			// identical reason — ip/ipneigh.c:597 calls ll_init_map before
			// it looks at `dev` — so a row that read 0x09 would be evidence
			// the selector had changed the dump, which is precisely what
			// this command asserts it does not do.
			description: "positive: the ll_init_map dump locus is derivable on `neigh show dev` too, at the same 0x01",
			command:     "neigh show dev",
			locus:       "request:RTM_GETLINK:IFLA_EXT_MASK:dump",
			filename:    tdGuestGetNeighDev,
			txn:         0,
			wantMask:    0x01,
		},
		{
			// And a third, for the third command on this object. The
			// repetition is Entry.key() again, but the row earns its place
			// on evidence rather than on symmetry: `proxy` is the ONE
			// selector in the corpus that changes which kernel table is
			// walked, so "it still did not touch txn 0" is a claim about a
			// materially different command than the `dev` row makes. The
			// mask is 0x01 because ip/ipneigh.c:597 runs ll_init_map before
			// the argument loop's effects are consulted at all — the
			// selector is parsed before, but acted on only in the dump at
			// :490.
			description: "positive: the ll_init_map dump locus is derivable on `neigh show proxy` too, at the same 0x01",
			command:     "neigh show proxy",
			locus:       "request:RTM_GETLINK:IFLA_EXT_MASK:dump",
			filename:    tdGuestGetNeighProxy,
			txn:         0,
			wantMask:    0x01,
		},
		{
			// ll_link_get was already RTEXT_FILTER_VF|SKIP_STATS at 7.1.0, so
			// this locus's pinned value is 0x09 and only de91e928 moves it.
			description: "positive: the ll_link_get single-get locus is derivable, and its pinned mask is 0x09",
			command:     "link show dev",
			locus:       "request:RTM_GETLINK:IFLA_EXT_MASK:get",
			filename:    tdGuestLinkDev,
			txn:         0,
			wantMask:    0x09,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if _, ok := al.Lookup(tc.command, tc.locus); !ok {
				t.Fatalf("the committed allowlist has no %q entry at %q", tc.command, tc.locus)
			}

			c, err := ParseRouteCaptureFile(tc.filename)
			if err != nil {
				t.Fatalf("%s: %v", tc.filename, err)
			}
			ref := SegmentCapture(c)
			if len(ref.Txns) <= tc.txn {
				t.Fatalf("%s has %d transactions, need at least %d", tc.filename, len(ref.Txns), tc.txn+1)
			}

			// The pinned value, read out of the capture rather than asserted
			// from the reason's prose.
			_, attrs, _ := DecodeAttrs(ref.Txns[tc.txn].Request.Hdr.Type, ref.Txns[tc.txn].Request.Body)
			var mask []byte
			for _, a := range attrs {
				if a.BareType() == uint16(unix.IFLA_EXT_MASK) {
					mask = a.Val
				}
			}
			if len(mask) != 4 {
				t.Fatalf("transaction %d's request carries no 4-byte IFLA_EXT_MASK", tc.txn)
			}
			if got := le32(mask); got != tc.wantMask {
				t.Fatalf("pinned IFLA_EXT_MASK = 0x%02x, want 0x%02x — the allowlist entry's "+
					"reason describes a value this capture does not hold", got, tc.wantMask)
			}

			// Now the divergence: de91e928's one extra bit.
			sub := cloneSegmentation(ref)
			sub.Txns[tc.txn].Request = mustSetAttrU32(sub.Txns[tc.txn].Request,
				uint16(unix.IFLA_EXT_MASK), tc.wantMask|0x100)

			found := false
			for _, d := range Diff(tc.command, ref, sub) {
				if d.Locus != tc.locus {
					continue
				}
				found = true
				if d.Class != DivergenceValue {
					t.Errorf("divergence at %q has class %s, want %s — a version-skew entry "+
						"can only ever suppress a value", tc.locus, d.Class, DivergenceValue)
				}
				if !al.Suppresses(tc.command, tc.locus, d.Class) {
					t.Errorf("the committed entry does not suppress the divergence it exists for")
				}
			}
			if !found {
				t.Fatalf("no divergence at %q; the committed locus is not one the differ emits, "+
					"which makes the entry unreachable", tc.locus)
			}
		})
	}

	// The rows above check the entries they name. This checks that they name
	// all of them — otherwise a new entry could be committed at a prose locus
	// and this test would stay green by simply not looking at it, which is
	// the failure mode it exists to prevent, one level up.
	//
	// `stdout:` loci are excluded because nlparity cannot derive them: it is
	// netlink-only, and rendered text is internal/goipparity's half of the
	// harness. TestStdoutLociAreEnumerable checks those against
	// goipparity.StdoutLoci(), so between the two every committed locus is
	// covered by something that can actually produce it.
	t.Run("corner: every non-stdout committed locus has a row above", func(t *testing.T) {
		covered := make(map[string]bool, len(tests))
		for _, tc := range tests {
			covered[tc.command+"\x00"+tc.locus] = true
		}
		for _, e := range al.Entries {
			if strings.HasPrefix(e.Locus, "stdout:") {
				continue
			}
			if !covered[e.Command+"\x00"+e.Locus] {
				t.Errorf("the committed allowlist has an entry for %q at %q with no "+
					"derivability row; add one, or the entry is unverified",
					e.Command, e.Locus)
			}
		}
	})
}

// TestCompareControlAndAllowlistInteraction pins the order of the two
// suppression stages, which is the one thing about Compare that is not visible
// from its signature.
//
// go test ./pkg/nlparity/ -run TestCompareControlAndAllowlistInteraction
func TestCompareControlAndAllowlistInteraction(t *testing.T) {
	const locus = "request:RTM_GETLINK:IFLA_EXT_MASK:dump"
	al, err := LoadAllowlist([]byte(`{
	  "_comment": ["test fixture"],
	  "entries": [
	    {
	      "command": "addr show",
	      "locus": "request:RTM_GETLINK:IFLA_EXT_MASK:dump",
	      "kind": "accepted-divergence",
	      "reason": "test fixture: an entry at a locus the control also flags"
	    }
	  ],
	  "gated_commands": ["addr show"]
	}`))
	if err != nil {
		t.Fatalf("LoadAllowlist: %v", err)
	}

	c, err := ParseRouteCaptureFile(tdGuestGetAddr)
	if err != nil {
		t.Fatalf("%s: %v", tdGuestGetAddr, err)
	}
	ref := SegmentCapture(c)

	valueDiff := func(v uint32) Segmentation {
		s := cloneSegmentation(ref)
		s.Txns[0].Request = mustSetAttrU32(s.Txns[0].Request, uint16(unix.IFLA_EXT_MASK), v)
		return s
	}

	tests := []struct {
		description        string
		control            []Divergence
		al                 *Allowlist
		wantFindings       int
		wantControlDropped int
		wantAllowDropped   int
	}{
		{
			description:  "positive: neither stage configured, so the divergence is reported",
			wantFindings: 1,
		},
		{
			description:        "positive: the control alone subtracts it, and names it in ControlSuppressed",
			control:            Diff("addr show", ref, valueDiff(0x209)),
			wantControlDropped: 1,
		},
		{
			description:      "positive: the allowlist alone suppresses it, and names it in AllowSuppressed",
			al:               al,
			wantAllowDropped: 1,
		},
		{
			// Control subtraction runs FIRST, so a divergence both stages
			// would have dropped is attributed to the control and does NOT
			// appear in AllowSuppressed. The order is what makes the two
			// buckets add up to the diff, which is what makes a report
			// reviewable: a finding counted twice would make |D_control| look
			// smaller than it is.
			description:        "corner: with both configured the control wins, so the finding is counted once",
			control:            Diff("addr show", ref, valueDiff(0x209)),
			al:                 al,
			wantControlDropped: 1,
			wantAllowDropped:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := Compare("addr show", ref, valueDiff(0x109), tc.control, tc.al)

			if len(r.Findings) != tc.wantFindings {
				t.Errorf("Findings = %d, want %d: %v", len(r.Findings), tc.wantFindings, r.Findings)
			}
			if len(r.ControlSuppressed) != tc.wantControlDropped {
				t.Errorf("ControlSuppressed = %d, want %d: %v",
					len(r.ControlSuppressed), tc.wantControlDropped, r.ControlSuppressed)
			}
			if len(r.AllowSuppressed) != tc.wantAllowDropped {
				t.Errorf("AllowSuppressed = %d, want %d: %v",
					len(r.AllowSuppressed), tc.wantAllowDropped, r.AllowSuppressed)
			}

			total := len(r.Findings) + len(r.ControlSuppressed) + len(r.AllowSuppressed)
			if total != 1 {
				t.Errorf("the one divergence appears %d times across the three buckets; "+
					"a finding counted twice makes |D_control| look smaller than it is", total)
			}
			for _, d := range append(append([]Divergence{}, r.Findings...), r.AllowSuppressed...) {
				if d.Locus != locus {
					t.Errorf("unexpected locus %q", d.Locus)
				}
			}
		})
	}
}

// TestDiffNormalizesRatherThanRequiringIt asserts that Diff's callers hand over
// RAW segmentations.
//
// The ordering rule is that attribution happens before normalization, because
// seq and pid are the only evidence of who sent what. That makes it tempting
// for a caller to normalize defensively on the way in, and this test states
// that doing so changes nothing — so a caller who forgets is not penalized and
// a caller who does it twice is not either.
//
// go test ./pkg/nlparity/ -run TestDiffNormalizesRatherThanRequiringIt
func TestDiffNormalizesRatherThanRequiringIt(t *testing.T) {
	c, err := ParseRouteCaptureFile(tdGuestGetAddr)
	if err != nil {
		t.Fatalf("%s: %v", tdGuestGetAddr, err)
	}
	ref := SegmentCapture(c)

	prenormalized := cloneSegmentation(ref)
	for i := range prenormalized.Txns {
		prenormalized.Txns[i].Request = NormalizeMsg(prenormalized.Txns[i].Request)
		prenormalized.Txns[i].Replies = NormalizeMsgs(prenormalized.Txns[i].Replies)
	}

	tests := []struct {
		description string
		ref, sub    Segmentation
		want        int
	}{
		{
			description: "positive: raw against raw is clean",
			ref:         ref,
			sub:         cloneSegmentation(ref),
			want:        0,
		},
		{
			description: "boundary: pre-normalized against raw is clean, so normalization is idempotent across the boundary",
			ref:         prenormalized,
			sub:         cloneSegmentation(ref),
			want:        0,
		},
		{
			description: "boundary: pre-normalized against pre-normalized is clean",
			ref:         prenormalized,
			sub:         cloneSegmentation(prenormalized),
			want:        0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := Diff("addr show", tc.ref, tc.sub)
			if len(got) != tc.want {
				t.Errorf("Diff = %d findings, want %d:", len(got), tc.want)
				for _, d := range got {
					t.Errorf("  %s", d)
				}
			}
		})
	}
}

// TestLevelString covers the report vocabulary. Not cosmetic: the level is the
// first field of every allowlist key, so "L3" versus "3" is the difference
// between an entry that matches and one that does not.
//
// go test ./pkg/nlparity/ -run TestLevelString
func TestLevelString(t *testing.T) {
	tests := []struct {
		description string
		level       Level
		want        string
	}{
		{"positive: L1 is the transaction count", LevelTxnCount, "L1"},
		{"positive: L2 is the request tier", LevelRequest, "L2"},
		{"positive: L3 is the reply tier", LevelReply, "L3"},
		{"positive: hygiene renders as a word, because it is not a comparison tier", LevelHygiene, "hygiene"},
		{"boundary: the zero value is not a level and says so", Level(0), "Level(0)"},
		{"corner: an out-of-range level renders its number rather than panicking", Level(99), "Level(99)"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.level.String(); got != tc.want {
				t.Errorf("Level(%d).String() = %q, want %q", uint8(tc.level), got, tc.want)
			}
		})
	}
}

// TestDiffDoesNotMutateItsInputs is a single assertion with no table, because
// it has exactly one thing to say: a differ that edited the capture it was
// handed would make the second of two comparisons disagree with the first, and
// the control triple runs three comparisons over overlapping data.
//
// go test ./pkg/nlparity/ -run TestDiffDoesNotMutateItsInputs
func TestDiffDoesNotMutateItsInputs(t *testing.T) {
	c, err := ParseRouteCaptureFile(tdGuestGetAddr)
	if err != nil {
		t.Fatalf("%s: %v", tdGuestGetAddr, err)
	}
	ref := SegmentCapture(c)

	snapshot := make([][]byte, 0, 64)
	for i := range ref.Txns {
		snapshot = append(snapshot, bytes.Clone(ref.Txns[i].Request.Body))
		for j := range ref.Txns[i].Replies {
			snapshot = append(snapshot, bytes.Clone(ref.Txns[i].Replies[j].Body))
		}
	}

	sub := cloneSegmentation(ref)
	sub.Txns[0].Request = mustSetAttrU32(sub.Txns[0].Request, uint16(unix.IFLA_EXT_MASK), 0x109)
	_ = Diff("addr show", ref, sub)

	k := 0
	for i := range ref.Txns {
		if !bytes.Equal(ref.Txns[i].Request.Body, snapshot[k]) {
			t.Errorf("txn %d request body changed:\n got %x\nwant %x",
				i, ref.Txns[i].Request.Body, snapshot[k])
		}
		k++
		for j := range ref.Txns[i].Replies {
			if !bytes.Equal(ref.Txns[i].Replies[j].Body, snapshot[k]) {
				t.Errorf("txn %d reply %d body changed:\n got %x\nwant %x",
					i, j, ref.Txns[i].Replies[j].Body, snapshot[k])
			}
			k++
		}
	}
}
