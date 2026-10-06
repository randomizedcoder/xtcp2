package xtcpnl

// Tests for the setRuleAttr cascade, the five functions that between them
// decode every FRA_* attribute of a rule.
//
// # Why these helpers are driven directly
//
// setRuleAttr was one switch with twenty-six cases and twenty-one length
// guards: cyclomatic complexity 48, against a gocyclo ceiling of 30. It is now
// five functions, each reached through the default arm of the level above and
// each named for a property true of every arm it holds:
//
//	setRuleAttr            6   no byte order at all — raw copies and strings
//	setRuleU32Attr        17   the little-endian u32s
//	setRuleU8Attr         11   the single bytes
//	setRuleRangeAttr      11   the intervals and the masks that narrow them
//	setRuleBigEndianAttr   7   the big-endian values, and terminal
//
// ParseRule cannot see that split. It reports only the end state, so a constant
// that moved to the wrong level, or that was dropped from every level, decodes
// to the same RuleInfo shape as one that is simply absent — and in the
// byte-order case, to a plausible wrong number rather than to an error. Which
// level handled an attribute is therefore observable on these helpers and
// nowhere else, which is the same argument TestSetRouteAttr makes for
// setRouteAttr (xtcpnl_rtmsg_test.go) and the reason that test exists too.
//
// # Field naming
//
// These tables use `description` + `expected`, the repo-wide standard, while
// the six tables in xtcpnl_fib_rule_hdr_test.go and the rest of this package
// use `want`/`wantErr`. That is a deliberate deviation rather than drift: the
// standard is `expected`, and new tables follow it. The older tables are not
// churned to match, so this package's test binary carries both conventions —
// worth knowing before grepping for one of them.
//
// # Fixtures
//
// The committed 7_1_4 rule captures carry **21 of the 26** attributes between
// them — measured, by enumerating every rtattr in the root, mesh and tunnel
// getrule/getrule6 dumps, not assumed. The mesh and tunnel topologies are much
// richer than the root one: between them they supply FRA_UID_RANGE,
// FRA_TUN_ID, FRA_FLOWLABEL, FRA_FLOWLABEL_MASK, FRA_SPORT_RANGE,
// FRA_DPORT_RANGE, FRA_DPORT_MASK, FRA_GOTO, FRA_FLOW, FRA_L3MDEV,
// FRA_OIFNAME, FRA_DST, FRA_FWMARK, FRA_FWMASK and FRA_SUPPRESS_IFGROUP on top
// of the root dump's six.
//
// So real kernel bytes drive the positive assertions, per this repo's fixture
// rule, and that is what TestSetRuleAttrFromCapture does: it rediscovers the
// covered set at run time and routes each attribute's real payload through the
// cascade. It also fails if coverage DROPS below 21, so a re-capture that
// quietly loses a rule is a test failure rather than a silent loss of fixture
// strength.
//
// Only **5** of the 26 are genuinely absent from every capture —
// FRA_IP_PROTO, FRA_SPORT_MASK, RTA_GATEWAY, FRA_DSCP and FRA_DSCP_MASK — and
// those are the only arms whose positive coverage is necessarily constructed.
//
// The constructed table below still covers all 26, because a capture gives real
// bytes but not a known-in-advance value: pinning the exact decoded number for
// each arm needs a payload this file chose. The two tests answer different
// questions, which is why both exist — "does this arm decode this exact value
// correctly" and "does this arm handle what the kernel really sends".
//
// An earlier version of this comment claimed the captures carried only six of
// the twenty-six. That was wrong, and it had made fifteen row descriptions
// below claim "no committed rule carries this" about attributes that three
// committed rules do carry.
//
// go test ./pkg/xtcpnl/ -run TestSetRuleAttr

import (
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestSetRuleAttrEveryArm drives all twenty-six attributes through the cascade
// one at a time, from a zero RuleInfo, and asserts the WHOLE resulting struct.
//
// This is the table that holds the split together, and it is exhaustive on
// purpose. Asserting the whole struct rather than one field is what makes it
// catch an arm that writes a second field, and starting from the zero value is
// what makes it catch an arm that writes nothing: a constant dropped from every
// level falls off the terminal function and leaves RuleInfo{}, which no
// single-field assertion would distinguish from a decode of zero.
//
// Note that a constant present in TWO levels is caught here as well, but
// indirectly: the upper level matches first and the lower never runs, so the
// symptom is the upper level's decoding — which for the big-endian three means
// a byte-swapped number, not an absent field. That is why those rows assert the
// exact value and not merely the presence bit.
//
// Every payload here is constructed, and that is not a gap. A capture supplies
// real bytes but not a value known in advance, so pinning each arm's exact
// decoded number — which is what catches a byte swap — needs a payload chosen
// here. TestSetRuleAttrFromCapture below answers the other half of the question
// with the 21 attributes the committed captures really carry.
func TestSetRuleAttrEveryArm(t *testing.T) {
	tests := ruleArmCases()

	if len(tests) != 26 {
		t.Fatalf("this table must cover all 26 arms of the cascade exactly once, got %d rows", len(tests))
	}

	seen := make(map[uint16]string, len(tests))
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var ri RuleInfo
			setRuleAttr(&ri, tc.atype, tc.val)
			if !reflect.DeepEqual(ri, tc.expected) {
				t.Errorf("setRuleAttr(%d) = %+v, expected %+v", tc.atype, ri, tc.expected)
			}
		})
		if prev, dup := seen[tc.atype]; dup {
			t.Errorf("attribute %d appears twice in this table: %q and %q", tc.atype, prev, tc.description)
		}
		seen[tc.atype] = tc.description
	}
}

// ruleCaptureAttrs returns the first real payload seen for each attribute
// across the committed rule captures, keyed by attribute number.
//
// All three dumps are read rather than just the root one, because the mesh and
// tunnel topologies are where the interesting rules live: the root dump has the
// six attributes a default namespace produces, and those two supply fifteen
// more between them.
func ruleCaptureAttrs(t *testing.T) map[uint16][]byte {
	t.Helper()
	found := make(map[uint16][]byte, 26)
	for _, path := range []string{
		tdDumpGetRule_7_1_4,
		tdDumpGetRule6_7_1_4,
		tdDumpMeshGetRule_7_1_4,
	} {
		bodies, _ := readDumpSetReplies(t, path, uint16(unix.RTM_NEWRULE))
		for _, body := range bodies {
			if err := WalkRTAttrs(body[FibRuleHdrSizeCst:], func(a uint16, val []byte) {
				if _, dup := found[a]; !dup {
					found[a] = CopyBytes(val)
				}
			}); err != nil {
				t.Fatalf("%s: WalkRTAttrs: %v", path, err)
			}
		}
	}
	return found
}

// nonZeroRuleFields returns the names of the RuleInfo fields that are not at
// their zero value, which is how a capture-driven row says "this arm touched
// exactly these and nothing else".
func nonZeroRuleFields(ri RuleInfo) map[string]bool {
	out := map[string]bool{}
	v := reflect.ValueOf(ri)
	tp := v.Type()
	for i := 0; i < v.NumField(); i++ {
		if !v.Field(i).IsZero() {
			out[tp.Field(i).Name] = true
		}
	}
	return out
}

// ruleCaptureCoverageCst is how many of the cascade's 26 arms the committed
// captures exercise with real kernel bytes, measured 2026-10-06 by enumerating
// every rtattr in the three rule dumps.
//
// It is asserted as a floor rather than only written down, so that a re-capture
// which quietly drops a rule fails this test instead of silently weakening
// every row below it. The five arms it does not reach are FRA_IP_PROTO,
// FRA_SPORT_MASK, RTA_GATEWAY, FRA_DSCP and FRA_DSCP_MASK; adding a rule that
// sets any of them to the capture script should raise this number.
const ruleCaptureCoverageCst = 21

// TestSetRuleAttrFromCapture drives the cascade with bytes a real kernel sent,
// for every attribute the committed captures carry.
//
// # What this asserts that the constructed table cannot
//
// A capture supplies a payload whose VALUE is not known here, so this cannot
// assert an exact decode — that is TestSetRuleAttrEveryArm's job. What it can
// assert, and what a constructed payload cannot, is that each arm copes with
// the widths and shapes the kernel really emits rather than the ones this file
// imagined. Two concrete cases it has something to say about: the kernel's
// trailing NUL on the interface names, which is what the trim exists for, and
// the real attribute lengths, which are what every guard in the cascade checks.
//
// Each row asserts that the fields the arm touched are a SUBSET of the fields
// its constructed counterpart touches, and that the arm's presence bit came out
// true. Subset rather than equality is deliberate: a real value may legitimately
// BE zero — a pref 0 rule, an all-zero mask — so requiring every field to be
// non-zero would fail on correct data. Cross-contamination between levels, which
// is the actual risk a cascade introduces, shows up as a field OUTSIDE the
// allowed set and is caught either way.
func TestSetRuleAttrFromCapture(t *testing.T) {
	captured := ruleCaptureAttrs(t)

	covered := 0
	for _, tc := range ruleArmCases() {
		raw, ok := captured[tc.atype]
		if !ok {
			continue
		}
		covered++
		t.Run(tc.description, func(t *testing.T) {
			var ri RuleInfo
			setRuleAttr(&ri, tc.atype, raw)

			allowed := nonZeroRuleFields(tc.expected)
			for name := range nonZeroRuleFields(ri) {
				if !allowed[name] {
					t.Errorf("a real %d-byte payload for attribute %d set RuleInfo.%s, which this arm must not touch; got %+v",
						len(raw), tc.atype, name, ri)
				}
			}
			// Where the arm has a presence bit, a payload the kernel actually
			// sent must set it: a real attribute at a real width is precisely
			// the case no length guard may suppress.
			for name := range allowed {
				if !strings.HasPrefix(name, "Has") {
					continue
				}
				if !reflect.ValueOf(ri).FieldByName(name).Bool() {
					t.Errorf("a real %d-byte payload for attribute %d left RuleInfo.%s false, so a guard suppressed bytes the kernel really sent",
						len(raw), tc.atype, name)
				}
			}
		})
	}

	if covered < ruleCaptureCoverageCst {
		t.Errorf("the committed captures now cover %d of the 26 arms, down from %d; a re-capture appears to have dropped a rule, which weakens every row here",
			covered, ruleCaptureCoverageCst)
	}
	t.Logf("real kernel bytes exercised %d of the cascade's 26 arms", covered)
}

// TestSetRuleAttrRejectsAndIgnores covers what the cascade must NOT do: decode a
// short attribute, and decode an attribute no level handles.
//
// Both are absence rather than error, which is ParseRule's documented policy —
// a truncated fixed-width attribute is a completeness problem, not a meaning
// one, so the rest of the rule still decodes. The distinction worth preserving
// is that a short payload leaves the presence bit FALSE rather than storing a
// fabricated zero, because a zero would claim the kernel said so.
//
// Every payload here is constructed, necessarily: the kernel does not emit a
// short fixed-width attribute, which is the whole reason these rows cannot come
// from a capture.
func TestSetRuleAttrRejectsAndIgnores(t *testing.T) {
	tests := []struct {
		description string
		atype       uint16
		val         []byte
		expected    RuleInfo
	}{
		{
			description: "negative: FRA_PAD reaches no level, falls off the terminal function and leaves the struct untouched (constructed: a padding attribute with a payload, which the kernel would not send)",
			atype:       uint16(unix.FRA_PAD),
			val:         le32(0xDEADBEEF),
			expected:    RuleInfo{},
		},
		{
			description: "negative: an attribute number no level claims is ignored, the property that lets a newer kernel's reply still decode (constructed: 0xFF00 is outside the FRA_* range)",
			atype:       0xFF00,
			val:         le32(1),
			expected:    RuleInfo{},
		},
		{
			description: "negative: FRA_PRIORITY at 3 bytes is suppressed, leaving HasPriority false rather than storing a fabricated zero (constructed: the kernel always emits 4)",
			atype:       uint16(unix.FRA_PRIORITY),
			val:         []byte{1, 2, 3},
			expected:    RuleInfo{},
		},
		{
			description: "boundary: FRA_UID_RANGE one byte short of FibRuleUidRangeSizeCst sets nothing at all, not a half-decoded range (constructed)",
			atype:       uint16(unix.FRA_UID_RANGE),
			val:         []byte{1, 2, 3, 4, 5, 6, 7},
			expected:    RuleInfo{},
		},
		{
			description: "boundary: FRA_SPORT_RANGE one byte short of FibRulePortRangeSizeCst sets nothing (constructed)",
			atype:       uint16(unix.FRA_SPORT_RANGE),
			val:         []byte{1, 2, 3},
			expected:    RuleInfo{},
		},
		{
			description: "boundary: FraSportMask at one byte is suppressed, which is what distinguishes a u16 reader from a u8 one (constructed)",
			atype:       FraSportMask,
			val:         []byte{0xFF},
			expected:    RuleInfo{},
		},
		{
			description: "boundary: FRA_TUN_ID at 7 bytes is suppressed rather than over-reading its u64 (constructed)",
			atype:       uint16(unix.FRA_TUN_ID),
			val:         []byte{1, 2, 3, 4, 5, 6, 7},
			expected:    RuleInfo{},
		},
		{
			description: "boundary: FraFlowlabel at 3 bytes is suppressed (constructed)",
			atype:       FraFlowlabel,
			val:         []byte{1, 2, 3},
			expected:    RuleInfo{},
		},
		{
			description: "boundary: FRA_L3MDEV at zero length is suppressed, the narrowest guard in the cascade (constructed)",
			atype:       uint16(unix.FRA_L3MDEV),
			val:         []byte{},
			expected:    RuleInfo{},
		},
		{
			description: "corner: FRA_IIFNAME with no NUL anywhere keeps the whole value rather than dropping a byte (constructed: the kernel always NUL-terminates, so only a constructed attribute reaches this)",
			atype:       uint16(unix.FRA_IIFNAME),
			val:         []byte("eth0"),
			expected:    RuleInfo{IifName: "eth0", HasIifName: true},
		},
		{
			description: "corner: FRA_IIFNAME of nothing but NULs trims to the empty string but still sets its presence bit, because the attribute WAS present (constructed)",
			atype:       uint16(unix.FRA_IIFNAME),
			val:         []byte{0x00, 0x00},
			expected:    RuleInfo{IifName: "", HasIifName: true},
		},
		{
			// Src has no presence bit, so for this one attribute "present but
			// empty" and "absent" really are the same decoded state. That is
			// CopyBytes normalizing empty to nil, not an arm misbehaving, and
			// it is worth pinning because it is the only place in the cascade
			// where a zero-length attribute is unrecoverable rather than
			// merely suppressed.
			description: "corner: FRA_SRC of zero length decodes to a nil Src, indistinguishable from the attribute being absent (constructed: the kernel does not send a zero-length address)",
			atype:       uint16(unix.FRA_SRC),
			val:         []byte{},
			expected:    RuleInfo{Src: nil},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var ri RuleInfo
			setRuleAttr(&ri, tc.atype, tc.val)
			if !reflect.DeepEqual(ri, tc.expected) {
				t.Errorf("setRuleAttr(%d, %v) = %+v, expected %+v", tc.atype, tc.val, ri, tc.expected)
			}
		})
	}
}

// ruleArmCase is one arm of the cascade: the attribute number, a payload chosen
// to pin that arm's exact decoding, and the whole RuleInfo that must result
// from applying it to a zero value.
//
// It is a named type with a constructor rather than an anonymous struct inside
// one test because two tests need the same 26 rows for different purposes.
// TestSetRuleAttrEveryArm asserts the exact expected struct; the
// capture-driven test reads each row's `expected` only to learn WHICH fields
// that arm is allowed to touch, since a real capture supplies bytes but not a
// value known in advance.
type ruleArmCase struct {
	description string
	atype       uint16
	val         []byte
	expected    RuleInfo
}

// ruleArmCases returns one case per arm of the setRuleAttr cascade: 26 of them,
// each attribute exactly once, which TestSetRuleAttrEveryArm asserts.
func ruleArmCases() []ruleArmCase {
	return []ruleArmCase{
		// ---- setRuleAttr itself: no byte order, so no length guard ----------
		{
			description: "positive: FRA_SRC is copied raw, no byte order and no presence bit of its own (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_SRC),
			val:         v4b(10, 0, 0, 0),
			expected:    RuleInfo{Src: v4b(10, 0, 0, 0)},
		},
		{
			description: "positive: FRA_DST is copied raw (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_DST),
			val:         v4b(172, 16, 0, 0),
			expected:    RuleInfo{Dst: v4b(172, 16, 0, 0)},
		},
		{
			description: "positive: FRA_IIFNAME is NUL-trimmed (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_IIFNAME),
			val:         []byte("eth0\x00"),
			expected:    RuleInfo{IifName: "eth0", HasIifName: true},
		},
		{
			description: "positive: FRA_OIFNAME is NUL-trimmed (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_OIFNAME),
			val:         []byte("eth1\x00"),
			expected:    RuleInfo{OifName: "eth1", HasOifName: true},
		},
		{
			description: "corner: RTA_GATEWAY is decoded here despite not being an FRA_* constant at all (constructed: no committed capture carries RTA_GATEWAY — it needs an RTN_NAT rule)",
			atype:       uint16(unix.RTA_GATEWAY),
			val:         v4b(192, 0, 2, 1),
			expected:    RuleInfo{Gateway: v4b(192, 0, 2, 1), HasGateway: true},
		},

		// ---- setRuleU32Attr: little-endian u32 -----------------------------
		{
			description: "positive: FRA_PRIORITY is little-endian u32 (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_PRIORITY),
			val:         le32(100),
			expected:    RuleInfo{Priority: 100, HasPriority: true},
		},
		{
			description: "positive: FRA_FWMARK is little-endian u32 (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_FWMARK),
			val:         le32(0x11),
			expected:    RuleInfo{Fwmark: 0x11, HasFwmark: true},
		},
		{
			description: "positive: FRA_FWMASK is little-endian u32 (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_FWMASK),
			val:         le32(0xFF),
			expected:    RuleInfo{Fwmask: 0xFF, HasFwmask: true},
		},
		{
			description: "corner: FRA_TABLE sets Table and NO presence bit, because frh_get_table means the header already populated it (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_TABLE),
			val:         le32(1000),
			expected:    RuleInfo{Table: 1000},
		},
		{
			description: "positive: FRA_SUPPRESS_PREFIXLEN is little-endian u32, carrying the all-ones the kernel really sends (constructed because the all-ones value is the interesting one; the capture's own value runs in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_SUPPRESS_PREFIXLEN),
			val:         le32(0xFFFFFFFF),
			expected:    RuleInfo{SuppressPrefixlen: 0xFFFFFFFF, HasSuppressPrefixlen: true},
		},
		{
			description: "positive: FRA_SUPPRESS_IFGROUP is little-endian u32 (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_SUPPRESS_IFGROUP),
			val:         le32(7),
			expected:    RuleInfo{SuppressIfgroup: 7, HasSuppressIfgroup: true},
		},
		{
			description: "positive: FRA_FLOW is the realms pair packed from<<16|to (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_FLOW),
			val:         le32(0x00010002),
			expected:    RuleInfo{Flow: 0x00010002, HasFlow: true},
		},
		{
			description: "positive: FRA_GOTO is little-endian u32 (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_GOTO),
			val:         le32(32000),
			expected:    RuleInfo{Goto: 32000, HasGoto: true},
		},

		// ---- setRuleU8Attr: one byte, so byte order cannot arise -----------
		{
			description: "positive: FRA_L3MDEV is a single byte (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_L3MDEV),
			val:         []byte{1},
			expected:    RuleInfo{L3mdev: 1, HasL3mdev: true},
		},
		{
			description: "positive: FRA_IP_PROTO is a single byte (constructed: no committed capture carries this attribute)",
			atype:       uint16(unix.FRA_IP_PROTO),
			val:         []byte{unix.IPPROTO_TCP},
			expected:    RuleInfo{IPProto: unix.IPPROTO_TCP, HasIPProto: true},
		},
		{
			description: "positive: FRA_PROTOCOL is a single byte (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_PROTOCOL),
			val:         []byte{unix.RTPROT_KERNEL},
			expected:    RuleInfo{Protocol: unix.RTPROT_KERNEL, HasProtocol: true},
		},
		{
			description: "corner: FraDscp sets Dscp and not DscpMask (constructed: no committed capture carries FRA_DSCP; the pairing with the next row is what catches the two being swapped)",
			atype:       FraDscp,
			val:         []byte{0x2E},
			expected:    RuleInfo{Dscp: 0x2E, HasDscp: true},
		},
		{
			description: "corner: FraDscpMask sets DscpMask and not Dscp (constructed: no committed capture carries this attribute)",
			atype:       FraDscpMask,
			val:         []byte{0xFC},
			expected:    RuleInfo{DscpMask: 0xFC, HasDscpMask: true},
		},

		// ---- setRuleRangeAttr: intervals and their masks -------------------
		{
			description: "positive: FRA_UID_RANGE is two little-endian u32s (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_UID_RANGE),
			val:         uidRangeBytes(1000, 2000),
			expected:    RuleInfo{UidRange: FibRuleUidRange{Start: 1000, End: 2000}, HasUidRange: true},
		},
		{
			description: "boundary: FRA_SPORT_RANGE at exactly FibRulePortRangeSizeCst decodes in HOST order, so 1024 stays 1024 and does not become 4 (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_SPORT_RANGE),
			val:         portRangeBytes(1024, 2048),
			expected:    RuleInfo{SportRange: FibRulePortRange{Start: 1024, End: 2048}, HasSportRange: true},
		},
		{
			description: "corner: FraSportMask is a u16, not a byte and not a u32 — the row that catches it being moved to setRuleU8Attr or setRuleU32Attr (constructed: no committed capture carries this attribute)",
			atype:       FraSportMask,
			val:         le16(0xFF00),
			expected:    RuleInfo{SportMask: 0xFF00, HasSportMask: true},
		},
		{
			description: "boundary: FRA_DPORT_RANGE at exactly FibRulePortRangeSizeCst decodes in HOST order (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_DPORT_RANGE),
			val:         portRangeBytes(80, 443),
			expected:    RuleInfo{DportRange: FibRulePortRange{Start: 80, End: 443}, HasDportRange: true},
		},
		{
			description: "corner: FraDportMask is a u16 (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       FraDportMask,
			val:         le16(0x0FF0),
			expected:    RuleInfo{DportMask: 0x0FF0, HasDportMask: true},
		},

		// ---- setRuleBigEndianAttr: terminal, and the byte-order group ------
		{
			description: "corner: FRA_TUN_ID is BIG-endian u64 per ntohll, so be64(42) is 42 and not 0x2A00000000000000 (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       uint16(unix.FRA_TUN_ID),
			val:         be64(42),
			expected:    RuleInfo{TunID: 42, HasTunID: true},
		},
		{
			description: "corner: FraFlowlabel is BIG-endian u32 per rta_getattr_be32 — the row that catches it being moved into setRuleU32Attr, where it would decode byte-swapped rather than fail (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       FraFlowlabel,
			val:         be32(0x00012345),
			expected:    RuleInfo{Flowlabel: 0x00012345, HasFlowlabel: true},
		},
		{
			description: "corner: FraFlowlabelMask is BIG-endian u32 and inherits its justification from FraFlowlabel above (constructed to pin the exact value; the real bytes for this arm run in TestSetRuleAttrFromCapture)",
			atype:       FraFlowlabelMask,
			val:         be32(0x000FFFFF),
			expected:    RuleInfo{FlowlabelMask: 0x000FFFFF, HasFlowlabelMask: true},
		},
	}
}
