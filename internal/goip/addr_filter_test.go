package goip

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// fakeAddrLookups records each lookup in call order, "index NAME" or
// "isVRF NAME", so a row can pin when a lookup happens as well as its result.
type fakeAddrLookups struct {
	idx   map[string]int32
	vrfs  map[string]bool
	calls []string
}

func newFakeAddrLookups() *fakeAddrLookups {
	return &fakeAddrLookups{
		idx:  map[string]int32{"d0": 3, "d1": 4, "bond0": 5, "vrf0": 7, "-": 8},
		vrfs: map[string]bool{"vrf0": true},
	}
}

func (f *fakeAddrLookups) lookups() addrLookups {
	return addrLookups{
		index: func(name string) (int32, error) {
			f.calls = append(f.calls, "index "+name)
			if name == "boom" {
				return 0, errFakeResolve
			}
			return f.idx[name], nil
		},
		isVRF: func(name string) (bool, error) {
			f.calls = append(f.calls, "isVRF "+name)
			return f.vrfs[name], nil
		},
	}
}

// addrFilterExpected is a row's outcome: the filter and the lookups made, or
// an error class and a fragment of iproute2's message.
type addrFilterExpected struct {
	filter  addrFilter
	lookups []string
	err     error
	errMsg  string
}

// TestParseAddrShowArgs is argv after `show` through the keyword loop of
// ipaddr_list_flush_or_save (ip/ipaddress.c:2170-2264) and get_filter
// (:1468-1496). The design's table 5 rows that need the filter applied or a
// pcap are A2-T02's; this is the parse.
//
// go test ./internal/goip/ -run TestParseAddrShowArgs
func TestParseAddrShowArgs(t *testing.T) {
	// base is the filter an empty argv gives under family fam.
	base := func(fam uint8) addrFilter {
		return addrFilter{family: fam, group: -1, vfinfo: true}
	}
	with := func(fam uint8, edit func(*addrFilter)) addrFilterExpected {
		f := base(fam)
		edit(&f)
		return addrFilterExpected{filter: f}
	}
	withLookups := func(fam uint8, lookups []string, edit func(*addrFilter)) addrFilterExpected {
		e := with(fam, edit)
		e.lookups = lookups
		return e
	}
	invalid := func(msg string, lookups ...string) addrFilterExpected {
		return addrFilterExpected{err: ErrInvalidArgument, errMsg: msg, lookups: lookups}
	}
	prefix := func(s string) inetPrefix {
		p := netip.MustParsePrefix(s)
		fam := uint8(unix.AF_INET)
		if p.Addr().Is6() {
			fam = unix.AF_INET6
		}
		return inetPrefix{family: fam, data: p.Addr().AsSlice(), bitlen: p.Bits()}
	}
	flag := func(fam uint8, flags, mask uint32) addrFilterExpected {
		return with(fam, func(f *addrFilter) { f.flags, f.flagmask = flags, mask })
	}
	const (
		unspec = unix.AF_UNSPEC
		inet   = unix.AF_INET
	)

	tests := []struct {
		description string
		args        []string
		family      uint8
		expected    addrFilterExpected
	}{
		// positive: one keyword each
		{
			description: "positive: an empty argv is the default filter",
			expected:    with(unspec, func(*addrFilter) {}),
		},
		{
			description: "positive: to sets the prefix, and the family from it",
			args:        []string{"to", "192.0.2.0/24"},
			expected: with(inet, func(f *addrFilter) {
				f.pfx, f.havePfx = prefix("192.0.2.0/24"), true
			}),
		},
		{
			description: "positive: scope host",
			args:        []string{"scope", "host"},
			expected:    with(unspec, func(f *addrFilter) { f.scope, f.scopemask = unix.RT_SCOPE_HOST, -1 }),
		},
		{
			description: "positive: up",
			args:        []string{"up"},
			expected:    with(unspec, func(f *addrFilter) { f.up = true }),
		},
		{
			description: "positive: down",
			args:        []string{"down"},
			expected:    with(unspec, func(f *addrFilter) { f.down = true }),
		},
		{
			description: "positive: label keeps its glob",
			args:        []string{"label", "d0:*"},
			expected:    with(unspec, func(f *addrFilter) { f.label, f.haveLabel = "d0:*", true }),
		},
		{
			description: "positive: group default is the built-in 0",
			args:        []string{"group", "default"},
			expected:    with(unspec, func(f *addrFilter) { f.group = 0 }),
		},
		{
			description: "positive: proto 99",
			args:        []string{"proto", "99"},
			expected:    with(unspec, func(f *addrFilter) { f.proto, f.haveProto = 99, true }),
		},
		{
			description: "positive: a flag sets its bit in flags and flagmask",
			args:        []string{"tentative"},
			expected:    flag(unspec, unix.IFA_F_TENTATIVE, unix.IFA_F_TENTATIVE),
		},
		{
			description: "positive: dynamic is permanent inverted",
			args:        []string{"dynamic"},
			expected:    flag(unspec, 0, unix.IFA_F_PERMANENT),
		},
		{
			description: "positive: primary is secondary inverted",
			args:        []string{"primary"},
			expected:    flag(unspec, 0, unix.IFA_F_SECONDARY),
		},
		{
			description: "positive: temporary is SECONDARY",
			args:        []string{"temporary"},
			expected:    flag(unspec, unix.IFA_F_SECONDARY, unix.IFA_F_SECONDARY),
		},
		{
			description: "positive: type bond_slave is a slave kind",
			args:        []string{"type", "bond_slave"},
			expected:    with(unspec, func(f *addrFilter) { f.slaveKind, f.haveSlaveKind = "bond", true }),
		},
		{
			description: "positive: type dummy is a kind",
			args:        []string{"type", "dummy"},
			expected:    with(unspec, func(f *addrFilter) { f.kind, f.haveKind = "dummy", true }),
		},
		{
			description: "positive: master resolves inline",
			args:        []string{"master", "bond0"},
			expected:    withLookups(unspec, []string{"index bond0"}, func(f *addrFilter) { f.master = 5 }),
		},
		{
			description: "positive: vrf resolves, then checks the kind",
			args:        []string{"vrf", "vrf0"},
			expected:    withLookups(unspec, []string{"index vrf0", "isVRF vrf0"}, func(f *addrFilter) { f.master = 7 }),
		},
		{
			description: "positive: nomaster is -1",
			args:        []string{"nomaster"},
			expected:    with(unspec, func(f *addrFilter) { f.master = -1 }),
		},
		{
			description: "positive: novf clears vfinfo",
			args:        []string{"novf"},
			expected:    with(unspec, func(f *addrFilter) { f.vfinfo = false }),
		},
		{
			description: "positive: dev d0",
			args:        []string{"dev", "d0"},
			expected:    withLookups(unspec, []string{"index d0"}, func(f *addrFilter) { f.ifindex = 3 }),
		},
		{
			description: "positive: a bare word is the device",
			args:        []string{"d0"},
			expected:    withLookups(unspec, []string{"index d0"}, func(f *addrFilter) { f.ifindex = 3 }),
		},

		// boundary
		{
			description: "boundary: scope all matches every scope",
			args:        []string{"scope", "all"},
			expected:    with(unspec, func(f *addrFilter) { f.scope, f.scopemask = unix.RT_SCOPE_NOWHERE, 0 }),
		},
		{
			description: "boundary: scope 0 by number",
			args:        []string{"scope", "0"},
			expected:    with(unspec, func(f *addrFilter) { f.scope, f.scopemask = 0, -1 }),
		},
		{
			description: "boundary: scope 255 by number",
			args:        []string{"scope", "255"},
			expected:    with(unspec, func(f *addrFilter) { f.scope, f.scopemask = 255, -1 }),
		},
		{
			description: "boundary: scope 256 is past rtnl_rtscope_a2n's range",
			args:        []string{"scope", "256"},
			expected:    invalid(`argument "256" is wrong: invalid "scope"`),
		},
		{
			description: "boundary: proto 255 is get_u8's maximum",
			args:        []string{"proto", "255"},
			expected:    with(unspec, func(f *addrFilter) { f.proto, f.haveProto = 255, true }),
		},
		{
			description: "boundary: proto 256 is past get_u8",
			args:        []string{"proto", "256"},
			expected:    invalid(`argument "256" is wrong: "proto" value is invalid`),
		},
		{
			description: "boundary: group takes strtol base 0",
			args:        []string{"group", "0x10"},
			expected:    with(unspec, func(f *addrFilter) { f.group = 16 }),
		},
		{
			description: "boundary: group 2147483647 is int's maximum",
			args:        []string{"group", "2147483647"},
			expected:    with(unspec, func(f *addrFilter) { f.group = 2147483647 }),
		},
		{
			description: "boundary: group -1 is refused",
			args:        []string{"group", "-1"},
			expected:    invalid(`argument "-1" is wrong: Invalid "group" value`),
		},
		{
			description: "boundary: group 2147483648 wraps negative in the int and is refused",
			args:        []string{"group", "2147483648"},
			expected:    invalid(`Invalid "group" value`),
		},

		// negative
		{
			description: "negative: a bad to is get_prefix's message, not the unreachable invalid \"to\"",
			args:        []string{"to", "bogus"},
			expected:    invalid(`Error: any valid prefix is expected rather than "bogus"`),
		},
		{
			description: "negative: an unknown scope name",
			args:        []string{"scope", "foo"},
			expected:    invalid(`argument "foo" is wrong: invalid "scope"`),
		},
		{
			description: "negative: an unknown master, after its one lookup",
			args:        []string{"master", "nosuch"},
			expected:    invalid(`argument "nosuch" is wrong: Device does not exist`, "index nosuch"),
		},
		{
			description: "negative: vrf on a device that is not a vrf, after both lookups",
			args:        []string{"vrf", "d0"},
			expected:    invalid(`argument "d0" is wrong: Not a valid VRF name`, "index d0", "isVRF d0"),
		},
		{
			description: "negative: vrf on no device stops at the index lookup",
			args:        []string{"vrf", "nosuch"},
			expected:    invalid(`argument "nosuch" is wrong: Not a valid VRF name`, "index nosuch"),
		},
		{
			description: "negative: proto names are not accepted, get_u8 only",
			args:        []string{"proto", "kernel_lo"},
			expected:    invalid(`argument "kernel_lo" is wrong: "proto" value is invalid`),
		},
		{
			description: "negative: two devices",
			args:        []string{"d0", "d1"},
			expected:    invalid(`either "dev" is duplicate, or "d1" is garbage`),
		},
		{
			description: "negative: dev twice",
			args:        []string{"dev", "d0", "dev", "d1"},
			expected:    invalid(`either "dev" is duplicate, or "d1" is garbage`),
		},
		{
			description: "negative: an unknown device, looked up after the loop",
			args:        []string{"dev", "nosuch"},
			expected:    invalid(`Device "nosuch" does not exist`, "index nosuch"),
		},
		{
			description: "negative: a keyword with no value",
			args:        []string{"scope"},
			expected:    invalid(`Command line is not complete`),
		},
		{
			description: "negative: dev with no value",
			args:        []string{"dev"},
			expected:    invalid(`Command line is not complete`),
		},
		{
			description: "negative: a group name goip cannot resolve, with no /etc/iproute2/group",
			args:        []string{"group", "mygroup"},
			expected:    invalid(`argument "mygroup" is wrong: Invalid "group" value`),
		},
		{
			description: "negative: a lookup error is returned as it is",
			args:        []string{"master", "boom"},
			expected:    addrFilterExpected{err: errFakeResolve, lookups: []string{"index boom"}},
		},

		// corner
		{
			description: "corner: to under -4 keeps the family",
			args:        []string{"to", "192.0.2.0/24"},
			family:      inet,
			expected: with(inet, func(f *addrFilter) {
				f.pfx, f.havePfx = prefix("192.0.2.0/24"), true
			}),
		},
		{
			description: "corner: an IPv6 to sets AF_INET6",
			args:        []string{"to", "2001:db8::/32"},
			expected: with(unix.AF_INET6, func(f *addrFilter) {
				f.pfx, f.havePfx = prefix("2001:db8::/32"), true
			}),
		},
		{
			description: "corner: an IPv6 to under -4 is get_prefix's refusal",
			args:        []string{"to", "2001:db8::/32"},
			family:      inet,
			expected:    invalid(`prefix is expected rather than "2001:db8::/32"`),
		},
		{
			description: "corner: -dynamic is the same as permanent",
			args:        []string{"-dynamic"},
			expected:    flag(unspec, unix.IFA_F_PERMANENT, unix.IFA_F_PERMANENT),
		},
		{
			description: "corner: -tentative clears the bit and keeps it in the mask",
			args:        []string{"-tentative"},
			expected:    flag(unspec, 0, unix.IFA_F_TENTATIVE),
		},
		{
			description: "corner: a later flag inversion clears an earlier one",
			args:        []string{"permanent", "dynamic"},
			expected:    flag(unspec, 0, unix.IFA_F_PERMANENT),
		},
		{
			description: "corner: up and down together",
			args:        []string{"up", "down"},
			expected:    with(unspec, func(f *addrFilter) { f.up, f.down = true, true }),
		},
		{
			description: "corner: keywords are exact, so sc host is two devices",
			args:        []string{"sc", "host"},
			expected:    invalid(`either "dev" is duplicate, or "host" is garbage`),
		},
		{
			description: "corner: help is matched by prefix, before any lookup",
			args:        []string{"he"},
			expected:    addrFilterExpected{err: ErrNotImplemented, errMsg: "help"},
		},
		{
			description: "corner: an empty word is a device, as matches() refuses an empty prefix",
			args:        []string{""},
			expected:    invalid(`Device "" does not exist`, "index "),
		},
		{
			description: "corner: help after dev is a device name",
			args:        []string{"dev", "help"},
			expected:    invalid(`Device "help" does not exist`, "index help"),
		},
		{
			description: "corner: a flag name is never a device",
			args:        []string{"permanent"},
			expected:    flag(unspec, unix.IFA_F_PERMANENT, unix.IFA_F_PERMANENT),
		},
		{
			description: "corner: a lone - fails the flag lookup and is a device",
			args:        []string{"-"},
			expected:    withLookups(unspec, []string{"index -"}, func(f *addrFilter) { f.ifindex = 8 }),
		},
		{
			description: "corner: type _slave leaves an empty, but set, slave kind",
			args:        []string{"type", "_slave"},
			expected:    with(unspec, func(f *addrFilter) { f.slaveKind, f.haveSlaveKind = "", true }),
		},
		{
			description: "corner: type br is shorter than the suffix and is a kind",
			args:        []string{"type", "br"},
			expected:    with(unspec, func(f *addrFilter) { f.kind, f.haveKind = "br", true }),
		},
		{
			description: "corner: an empty label is set, and matches nothing",
			args:        []string{"label", ""},
			expected:    with(unspec, func(f *addrFilter) { f.haveLabel = true }),
		},
		{
			description: "corner: an argument error comes before the device lookup",
			args:        []string{"nosuch", "scope", "foo"},
			expected:    invalid(`invalid "scope"`),
		},
		{
			description: "corner: an inline master lookup comes before a later argument error",
			args:        []string{"master", "nosuch", "scope", "foo"},
			expected:    invalid(`Device does not exist`, "index nosuch"),
		},
		{
			description: "corner: the last master wins",
			args:        []string{"nomaster", "master", "bond0"},
			expected:    withLookups(unspec, []string{"index bond0"}, func(f *addrFilter) { f.master = 5 }),
		},
		{
			description: "corner: group 4294967296 wraps to 0 in the int",
			args:        []string{"group", "4294967296"},
			expected:    with(unspec, func(f *addrFilter) { f.group = 0 }),
		},
		{
			description: "corner: a strtol underflow clamps to LONG_MIN, which is 0 in the int",
			args:        []string{"group", "-99999999999999999999"},
			expected:    with(unspec, func(f *addrFilter) { f.group = 0 }),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			lk := newFakeAddrLookups()
			got, err := parseAddrShowArgs(tc.args, tc.family, lk.lookups())
			if !equalStrings(lk.calls, tc.expected.lookups) {
				t.Errorf("lookups %q, want %q", lk.calls, tc.expected.lookups)
			}
			if tc.expected.err != nil {
				if !errors.Is(err, tc.expected.err) || !strings.Contains(err.Error(), tc.expected.errMsg) {
					t.Fatalf("err = %v, want %v containing %q", err, tc.expected.err, tc.expected.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, tc.expected.filter) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected.filter)
			}
		})
	}
}

// TestAddrShowKeywordsAreNotFlags is why addrShowKeywords may be tried
// before get_filter although iproute2 tries get_filter after `down`: no word
// that is a keyword is also a flag name, with or without get_filter's `-`.
//
// go test ./internal/goip/ -run TestAddrShowKeywordsAreNotFlags
func TestAddrShowKeywordsAreNotFlags(t *testing.T) {
	for _, kw := range addrShowKeywords {
		t.Run("corner: "+kw.name+" is not a get_filter word", func(t *testing.T) {
			var f addrFilter
			if f.getFilter(kw.name) {
				t.Errorf("%q is both a keyword and a flag", kw.name)
			}
		})
	}
	t.Run("corner: no keyword starts with -, so a stripped - cannot reach one", func(t *testing.T) {
		for _, kw := range addrShowKeywords {
			if strings.HasPrefix(kw.name, "-") {
				t.Errorf("%q starts with -", kw.name)
			}
		}
	})
}
