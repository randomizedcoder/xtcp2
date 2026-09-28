package render

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The expectations in this file cite pkg/xtcpnl/testdata/7_1_8/ip_addr_n by
// line number wherever one exists, which is the convention
// xtcpnl_rtnetlink_realfixtures_test.go:26-40 set. A citation is a claim that
// the pinned `ip` 7.1.0 printed exactly that string for exactly those bytes,
// so the byte inputs below are the ones the capture carries and not
// convenient round numbers.
//
// The one thing a unit table cannot cite is a line iproute2 never printed on
// this host. Those rows are marked corner and say what they are reasoning
// from — the C source, not observation.

// v4 builds a four-byte IFA payload.
func v4(a, b, c, d byte) []byte { return []byte{a, b, c, d} }

// v6 parses a colon-hex literal into the sixteen bytes the wire carries, so
// the test inputs read as addresses rather than as byte soup.
//
// It is hand-written rather than netip.ParseAddr because the function under
// test formats with netip: parsing the input with the same library would make
// a round trip out of what is meant to be an independent expectation.
func v6(t *testing.T, s string) []byte {
	t.Helper()
	head, tail, hasRun := strings.Cut(s, "::")
	groups := func(part string) []string {
		if part == "" {
			return nil
		}
		return strings.Split(part, ":")
	}
	left, right := groups(head), groups(tail)
	if !hasRun {
		left, right = groups(s), nil
	}
	zeros := 8 - len(left) - len(right)
	if zeros < 0 || (!hasRun && zeros != 0) {
		t.Fatalf("v6(%q): %d groups, want 8", s, len(left)+len(right))
	}

	var out []byte
	emit := func(part []string) {
		for _, g := range part {
			var n uint32
			for _, c := range g {
				var d uint32
				switch {
				case c >= '0' && c <= '9':
					d = uint32(c - '0')
				case c >= 'a' && c <= 'f':
					d = uint32(c-'a') + 10
				default:
					t.Fatalf("v6(%q): bad hex digit %q", s, c)
				}
				n = n<<4 | d
			}
			out = append(out, byte(n>>8), byte(n))
		}
	}
	emit(left)
	for i := 0; i < zeros; i++ {
		out = append(out, 0, 0)
	}
	emit(right)

	if len(out) != 16 {
		t.Fatalf("v6(%q): produced %d bytes, want 16", s, len(out))
	}
	return out
}

// go test ./internal/goip/render/ -run TestV6Helper
func TestV6Helper(t *testing.T) {
	tests := []struct {
		description string
		in          string
		want        string // lowercase hex of all sixteen bytes
	}{
		{
			description: "positive: eight explicit groups are laid down in order",
			in:          "2001:0db8:0000:0001:0002:0003:0004:0005",
			want:        "20010db8000000010002000300040005",
		},
		{
			description: "positive: a trailing run expands to the bytes it stands for",
			in:          "fd10:10:4::2",
			want:        "fd100010000400000000000000000002",
		},
		{
			description: "boundary: a leading run with one group after it is the loopback",
			in:          "::1",
			want:        "00000000000000000000000000000001",
		},
		{
			description: "boundary: a bare run is the all-zero address",
			in:          "::",
			want:        "00000000000000000000000000000000",
		},
		{
			description: "corner: a run in the middle expands between the two halves",
			in:          "fe80::609:73ff:fecf:d8d0",
			want:        "fe80000000000000060973fffecfd8d0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var sb strings.Builder
			for _, c := range v6(t, tt.in) {
				sb.WriteString(hexByte(c))
			}
			if sb.String() != tt.want {
				t.Errorf("v6(%q) = %s, want %s", tt.in, sb.String(), tt.want)
			}
		})
	}
}

func hexByte(c byte) string {
	const d = "0123456789abcdef"
	return string([]byte{d[c>>4], d[c&0xf]})
}

// go test ./internal/goip/render/ -run TestScopeName
func TestScopeName(t *testing.T) {
	tests := []struct {
		description string
		scope       uint8
		want        string
	}{
		{
			// ip_addr_n:10 — "scope global" on the DHCP address.
			description: "positive: RT_SCOPE_UNIVERSE is global, not universe",
			scope:       unix.RT_SCOPE_UNIVERSE,
			want:        "global",
		},
		{
			// ip_addr_n:3 — "scope host lo".
			description: "positive: RT_SCOPE_HOST is host",
			scope:       unix.RT_SCOPE_HOST,
			want:        "host",
		},
		{
			// ip_addr_n:18 — "scope link" on the v6 link-local.
			description: "positive: RT_SCOPE_LINK is link",
			scope:       unix.RT_SCOPE_LINK,
			want:        "link",
		},
		{
			description: "boundary: RT_SCOPE_SITE is 200, the one non-contiguous id",
			scope:       unix.RT_SCOPE_SITE,
			want:        "site",
		},
		{
			description: "boundary: RT_SCOPE_NOWHERE is 255, the top of the byte",
			scope:       unix.RT_SCOPE_NOWHERE,
			want:        "nowhere",
		},
		{
			// rt_scopes ships five entries and nothing fills the gaps, so
			// every id in 1..199 and 201..252 falls through to the number.
			description: "negative: an id rt_scopes does not name renders as its decimal value",
			scope:       42,
			want:        "42",
		},
		{
			description: "boundary: id 1, immediately above global, is already unnamed",
			scope:       1,
			want:        "1",
		},
		{
			description: "corner: id 252, immediately below link, is unnamed and decimal",
			scope:       252,
			want:        "252",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := scopeName(tt.scope); got != tt.want {
				t.Errorf("scopeName(%d) = %q, want %q", tt.scope, got, tt.want)
			}
		})
	}
}

// go test ./internal/goip/render/ -run TestAddrProtoName
func TestAddrProtoName(t *testing.T) {
	tests := []struct {
		description string
		proto       uint8
		want        string
	}{
		{
			description: "positive: IFAPROT_UNSPEC is unspec",
			proto:       0,
			want:        "unspec",
		},
		{
			description: "positive: IFAPROT_KERNEL_LO is kernel_lo",
			proto:       1,
			want:        "kernel_lo",
		},
		{
			description: "positive: IFAPROT_KERNEL_RA is kernel_ra",
			proto:       2,
			want:        "kernel_ra",
		},
		{
			// ip_addr_n:27 — "proto kernel_ll" on enp35s0f0np0's link-local.
			description: "boundary: IFAPROT_KERNEL_LL is the last named entry",
			proto:       3,
			want:        "kernel_ll",
		},
		{
			// The fallback here is %#x, not decimal — the only n2a in this
			// file that is, and transcribed rather than harmonized.
			description: "negative: the first unnamed proto takes the %#x fallback, not decimal",
			proto:       4,
			want:        "0x4",
		},
		{
			description: "corner: proto 255 is hex too, and %#x gives no zero padding",
			proto:       255,
			want:        "0xff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := addrProtoName(tt.proto); got != tt.want {
				t.Errorf("addrProtoName(%d) = %q, want %q", tt.proto, got, tt.want)
			}
		})
	}
}

// go test ./internal/goip/render/ -run TestFamilyName
func TestFamilyName(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		want        string
	}{
		{
			description: "positive: AF_INET is inet",
			family:      unix.AF_INET,
			want:        "inet",
		},
		{
			description: "positive: AF_INET6 is inet6",
			family:      unix.AF_INET6,
			want:        "inet6",
		},
		{
			// family_name's AF_PACKET case is "link", which is what makes
			// `ip -f link addr show` print "link/ether" lines as addresses.
			description: "positive: AF_PACKET is link, not packet",
			family:      unix.AF_PACKET,
			want:        "link",
		},
		{
			description: "positive: AF_MPLS is mpls",
			family:      unix.AF_MPLS,
			want:        "mpls",
		},
		{
			// The C returns "???" here. Returning "" instead is what forces
			// print_addrinfo's family_index branch in the caller rather than
			// letting the sentinel reach output; see the Text() table.
			description: "negative: AF_UNSPEC has no name and returns the empty sentinel",
			family:      unix.AF_UNSPEC,
			want:        "",
		},
		{
			description: "corner: a family above every AF_* constant returns the sentinel, not a panic",
			family:      200,
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := familyName(tt.family); got != tt.want {
				t.Errorf("familyName(%d) = %q, want %q", tt.family, got, tt.want)
			}
		})
	}
}

// TestIfaFlagTokens covers print_ifa_flags, including the two rules that make
// it more than a bit-to-name map: IFA_F_PERMANENT prints on absence, and
// IFA_F_SECONDARY renames itself on AF_INET6.
//
// go test ./internal/goip/render/ -run TestIfaFlagTokens
func TestIfaFlagTokens(t *testing.T) {
	tests := []struct {
		description string
		flags       uint32
		family      uint8
		want        []string
	}{
		{
			// ip_addr_n:3 — "inet 127.0.0.1/8 scope host lo" has no flag
			// tokens at all, which only happens because PERMANENT is set.
			description: "positive: a permanent address with no other bits prints no tokens",
			flags:       unix.IFA_F_PERMANENT,
			family:      unix.AF_INET,
			want:        nil,
		},
		{
			// ip_addr_n:10 — "scope global dynamic noprefixroute enp1s0".
			description: "positive: the DHCP address prints dynamic then noprefixroute",
			flags:       unix.IFA_F_NOPREFIXROUTE,
			family:      unix.AF_INET,
			want:        []string{"dynamic", "noprefixroute"},
		},
		{
			// ip_addr_n:12 — "scope global temporary dynamic".
			description: "positive: an IPv6 temporary prints temporary then dynamic",
			flags:       unix.IFA_F_SECONDARY,
			family:      unix.AF_INET6,
			want:        []string{"temporary", "dynamic"},
		},
		{
			// ip_addr_n:14 — "temporary deprecated dynamic", table indices
			// 0, 6 and 8, which is what pins the ordering rather than the
			// bit values.
			description: "positive: token order follows table position, so deprecated lands between temporary and dynamic",
			flags:       unix.IFA_F_SECONDARY | unix.IFA_F_DEPRECATED,
			family:      unix.AF_INET6,
			want:        []string{"temporary", "deprecated", "dynamic"},
		},
		{
			// ip_addr_n:16 — "scope global dynamic mngtmpaddr noprefixroute".
			// Both bits are above the 8-bit header field, so this row is only
			// reachable at all because IFA_FLAGS is decoded.
			description: "positive: mngtmpaddr and noprefixroute, the two bits the 8-bit header cannot carry",
			flags:       unix.IFA_F_MANAGETEMPADDR | unix.IFA_F_NOPREFIXROUTE,
			family:      unix.AF_INET6,
			want:        []string{"dynamic", "mngtmpaddr", "noprefixroute"},
		},
		{
			// ip_addr_n:25 — "scope global nodad", with valid_lft forever, so
			// PERMANENT is set and suppresses "dynamic".
			description: "positive: nodad on a permanent address prints nodad alone",
			flags:       unix.IFA_F_NODAD | unix.IFA_F_PERMANENT,
			family:      unix.AF_INET6,
			want:        []string{"nodad"},
		},
		{
			// ip_addr_n:5 — "inet6 ::1/128 scope host noprefixroute".
			description: "positive: the loopback v6 address prints noprefixroute alone",
			flags:       unix.IFA_F_PERMANENT | unix.IFA_F_NOPREFIXROUTE,
			family:      unix.AF_INET6,
			want:        []string{"noprefixroute"},
		},
		{
			// The inversion, isolated: no bits at all still prints one token.
			description: "negative: zero flags print dynamic, because PERMANENT prints when clear",
			flags:       0,
			family:      unix.AF_INET,
			want:        []string{"dynamic"},
		},
		{
			// Row 1 of ifaFlagNames carries IFA_F_SECONDARY a second time
			// under the name "temporary". print_ifa_flags clears the mask at
			// the end of every iteration, so row 0 has already consumed the
			// bit — the name can only ever come from row 0's AF_INET6 special
			// case.
			//
			// Deleting the `unreachable` skip does NOT break this row, which
			// was measured rather than assumed: the bit-clearing already makes
			// row 1 dead, so the flag is documentation and not mechanism. What
			// this row detects is the clearing going away — removing
			// `rest &= ^f.bit` makes it print "temporary temporary", and ten
			// other rows in this table go red with it.
			description: "negative: the duplicate SECONDARY row is dead, so temporary is never printed twice",
			flags:       unix.IFA_F_SECONDARY | unix.IFA_F_PERMANENT,
			family:      unix.AF_INET6,
			want:        []string{"temporary"},
		},
		{
			description: "boundary: the same SECONDARY bit is secondary, not temporary, on AF_INET",
			flags:       unix.IFA_F_SECONDARY | unix.IFA_F_PERMANENT,
			family:      unix.AF_INET,
			want:        []string{"secondary"},
		},
		{
			description: "boundary: STABLE_PRIVACY is the last row and still named, not hex",
			flags:       unix.IFA_F_STABLE_PRIVACY | unix.IFA_F_PERMANENT,
			family:      unix.AF_INET6,
			want:        []string{"stable-privacy"},
		},
		{
			// 0x1000 is one bit above IFA_F_STABLE_PRIVACY (0x800).
			description: "negative: the first bit above the table becomes a single flags token",
			flags:       0x1000 | unix.IFA_F_PERMANENT,
			family:      unix.AF_INET6,
			want:        []string{"flags 1000"},
		},
		{
			// %02x is a minimum width, so a wide residue is not truncated and
			// a narrow one is padded; both halves need a row.
			description: "corner: unrecognized bits are accumulated into one token, not one per bit",
			flags:       0x1000 | 0x8000 | unix.IFA_F_PERMANENT,
			family:      unix.AF_INET6,
			want:        []string{"flags 9000"},
		},
		{
			// There is no bit between 0x800 and 0x1000, so this residue is
			// only constructible, never captured. Reasoned from the %02x in
			// print_ifa_flags.
			description: "corner: a residue below 0x10 is zero-padded to two digits by %02x",
			flags:       0x1000_0000 | unix.IFA_F_PERMANENT,
			family:      unix.AF_INET6,
			want:        []string{"flags 10000000"},
		},
		{
			description: "corner: every named bit at once, in table order, with no hex residue",
			flags: unix.IFA_F_SECONDARY | unix.IFA_F_NODAD | unix.IFA_F_OPTIMISTIC |
				unix.IFA_F_DADFAILED | unix.IFA_F_HOMEADDRESS | unix.IFA_F_DEPRECATED |
				unix.IFA_F_TENTATIVE | unix.IFA_F_PERMANENT | unix.IFA_F_MANAGETEMPADDR |
				unix.IFA_F_NOPREFIXROUTE | unix.IFA_F_MCAUTOJOIN | unix.IFA_F_STABLE_PRIVACY,
			family: unix.AF_INET,
			want: []string{
				"secondary", "nodad", "optimistic", "dadfailed", "home",
				"deprecated", "tentative", "mngtmpaddr", "noprefixroute",
				"autojoin", "stable-privacy",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := IfaFlagTokens(tt.flags, tt.family)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IfaFlagTokens(%#x, %d) = %q, want %q",
					tt.flags, tt.family, got, tt.want)
			}
		})
	}
}

// go test ./internal/goip/render/ -run TestAddrString
func TestAddrString(t *testing.T) {
	tests := []struct {
		description string
		in          []byte
		family      uint8
		want        string
	}{
		{
			// ip_addr_n:3.
			description: "positive: an IPv4 address is dotted quad",
			in:          v4(127, 0, 0, 1),
			family:      unix.AF_INET,
			want:        "127.0.0.1",
		},
		{
			// ip_addr_n:5 — the shortest possible v6 rendering.
			description: "positive: the v6 loopback compresses to ::1",
			in:          v6(t, "::1"),
			family:      unix.AF_INET6,
			want:        "::1",
		},
		{
			// ip_addr_n:25 — leading zeros stripped inside groups and the
			// longest zero run compressed, both in one address.
			description: "positive: fd10:10:4::2 strips leading zeros and compresses the tail",
			in:          v6(t, "fd10:0010:0004:0000:0000:0000:0000:0002"),
			family:      unix.AF_INET6,
			want:        "fd10:10:4::2",
		},
		{
			// ip_addr_n:27 — a compression at the front rather than the back.
			description: "positive: fe80::609:73ff:fecf:d8d0 compresses the leading run",
			in:          v6(t, "fe80:0000:0000:0000:0609:73ff:fecf:d8d0"),
			family:      unix.AF_INET6,
			want:        "fe80::609:73ff:fecf:d8d0",
		},
		{
			// RFC 5952 §4.2.2, and glibc agrees (measured): a lone zero group
			// is written "0", never "::". Nothing in the sidecar has one.
			description: "boundary: a single zero group is left uncompressed, matching inet_ntop",
			in:          v6(t, "2001:0db8:0000:0001:0002:0003:0004:0005"),
			family:      unix.AF_INET6,
			want:        "2001:db8:0:1:2:3:4:5",
		},
		{
			description: "boundary: an empty attribute renders as the empty string, not as ::",
			in:          nil,
			family:      unix.AF_INET6,
			want:        "",
		},
		{
			description: "boundary: the all-zero v4 address is 0.0.0.0, which is not empty",
			in:          v4(0, 0, 0, 0),
			family:      unix.AF_INET,
			want:        "0.0.0.0",
		},
		{
			// A 16-byte payload on an AF_INET address is a malformed reply.
			// Showing hex makes it visible; dropping it would look like an
			// address with no text.
			description: "negative: a 16-byte payload on AF_INET falls through to hex",
			in:          v6(t, "::1"),
			family:      unix.AF_INET,
			want:        "00000000000000000000000000000001",
		},
		{
			description: "negative: a 6-byte payload matches no family length and renders as hex",
			in:          []byte{0xe0, 0x4f, 0x43, 0xe6, 0x28, 0xef},
			family:      unix.AF_PACKET,
			want:        "e04f43e628ef",
		},
		{
			// Unmap() is deliberately not called: glibc keeps the prefix
			// (measured, '::ffff:192.0.2.1'), and Unmap would print the bare
			// dotted quad. The kernel does not put a mapped address on an
			// interface, so this row is constructed.
			description: "corner: an IPv4-mapped v6 address keeps its ::ffff: prefix, as inet_ntop does",
			in:          v6(t, "::ffff:c000:0201"),
			family:      unix.AF_INET6,
			want:        "::ffff:192.0.2.1",
		},
		{
			description: "corner: the unspecified v6 address is ::, the whole address compressed",
			in:          v6(t, "::"),
			family:      unix.AF_INET6,
			want:        "::",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := addrString(tt.in, tt.family); got != tt.want {
				t.Errorf("addrString(%x, %d) = %q, want %q",
					tt.in, tt.family, got, tt.want)
			}
		})
	}
}

// go test ./internal/goip/render/ -run TestLifetime
func TestLifetime(t *testing.T) {
	tests := []struct {
		description string
		v           uint32
		signed      bool
		want        string
	}{
		{
			// ip_addr_n:4 — "valid_lft forever preferred_lft forever".
			description: "positive: INFINITY_LIFE_TIME is forever, not a number",
			v:           xtcpnl.IfaLifetimeInfinityCst,
			want:        "forever",
		},
		{
			// ip_addr_n:11 — the DHCP lease.
			description: "positive: a finite lifetime gets a sec suffix with no space",
			v:           47871,
			want:        "47871sec",
		},
		{
			// ip_addr_n:15 — preferred_lft 0sec on the deprecated address,
			// which is the one place the signed format string is reached.
			description: "boundary: zero prints 0sec under both format strings",
			v:           0,
			signed:      true,
			want:        "0sec",
		},
		{
			description: "boundary: zero unsigned is 0sec as well, so the branch is invisible here",
			v:           0,
			want:        "0sec",
		},
		{
			// print_addrinfo uses %d for a deprecated address's preferred
			// lifetime. A kernel reporting a negative one would print the
			// negative rather than a value near 4.3 billion.
			description: "corner: the signed form of 0xfffffffe is -2sec, not 4294967294sec",
			v:           0xffff_fffe,
			signed:      true,
			want:        "-2sec",
		},
		{
			description: "corner: the same bytes unsigned are 4294967294sec, which is why the flag exists",
			v:           0xffff_fffe,
			want:        "4294967294sec",
		},
		{
			// 0xffffffff is intercepted before either format string, so the
			// signed path never reaches -1.
			description: "corner: forever wins over the signed form, so -1sec is unreachable",
			v:           xtcpnl.IfaLifetimeInfinityCst,
			signed:      true,
			want:        "forever",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := lifetime(tt.v, tt.signed); got != tt.want {
				t.Errorf("lifetime(%#x, %v) = %q, want %q",
					tt.v, tt.signed, got, tt.want)
			}
		})
	}
}

// TestEqualAddrBytes covers print_addrinfo's memcmp, whose length comes from
// the family and not from the slices.
//
// go test ./internal/goip/render/ -run TestEqualAddrBytes
func TestEqualAddrBytes(t *testing.T) {
	tests := []struct {
		description string
		a, b        []byte
		family      uint8
		want        bool
	}{
		{
			description: "positive: identical v4 addresses compare equal, so no peer is printed",
			a:           v4(172, 16, 50, 219),
			b:           v4(172, 16, 50, 219),
			family:      unix.AF_INET,
			want:        true,
		},
		{
			description: "positive: identical v6 addresses compare equal over all sixteen bytes",
			a:           v6(t, "fd10:10:4::2"),
			b:           v6(t, "fd10:10:4::2"),
			family:      unix.AF_INET6,
			want:        true,
		},
		{
			description: "negative: a v4 peer differing in the last byte is unequal, so peer is printed",
			a:           v4(192, 0, 2, 2),
			b:           v4(192, 0, 2, 1),
			family:      unix.AF_INET,
			want:        false,
		},
		{
			description: "negative: v6 addresses differing only in the sixteenth byte are unequal",
			a:           v6(t, "fd10:10:4::2"),
			b:           v6(t, "fd10:10:4::3"),
			family:      unix.AF_INET6,
			want:        false,
		},
		{
			// The literal `ifa_family == AF_INET ? 4 : 16`: on AF_INET only
			// the first four bytes are looked at, so trailing garbage cannot
			// manufacture a peer.
			description: "boundary: on AF_INET bytes past the fourth are not compared",
			a:           []byte{192, 0, 2, 1, 0xff},
			b:           []byte{192, 0, 2, 1, 0x00},
			family:      unix.AF_INET,
			want:        true,
		},
		{
			description: "boundary: two empty slices are equal, which is the both-attributes-absent case",
			a:           nil,
			b:           nil,
			family:      unix.AF_INET6,
			want:        true,
		},
		{
			// Shorter than the family length on either side, which the C
			// would read past; the Go falls back to comparing what is there.
			description: "negative: a short slice against a full one is unequal rather than a read past the end",
			a:           []byte{192, 0},
			b:           v4(192, 0, 2, 1),
			family:      unix.AF_INET,
			want:        false,
		},
		{
			description: "corner: two equally short slices compare on their own length",
			a:           []byte{192, 0},
			b:           []byte{192, 0},
			family:      unix.AF_INET,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := equalAddrBytes(tt.a, tt.b, tt.family); got != tt.want {
				t.Errorf("equalAddrBytes(%x, %x, %d) = %v, want %v",
					tt.a, tt.b, tt.family, got, tt.want)
			}
		})
	}
}

// TestAddrViewText drives AddrViewOf and AddrView.Text together, because the
// expectation is a line of `ip addr show` output and splitting them would let
// a resolution bug and a formatting bug cancel.
//
// The want strings include their trailing whitespace and newline, and the
// trailing space is real rather than sloppiness in the table. Every field is
// printed with its own trailing space and the label is printed with a bare
// "%s", so a line ends flush only when there is a label to end it.
// ip_addr_n:10 (with a label) and :5 (without) differ in exactly that way.
//
// What decides it is the label and not the family: the constructed v4 rows
// below that carry no IFA_LABEL end in a space too, which is why their wants
// look inconsistent with :3 and are not.
//
// go test ./internal/goip/render/ -run TestAddrViewText
func TestAddrViewText(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.AddrInfo
		want        string
	}{
		{
			description: "positive: the loopback v4 address, ip_addr_n:3-4",
			in: xtcpnl.AddrInfo{
				Family:    unix.AF_INET,
				Prefixlen: 8,
				Scope:     unix.RT_SCOPE_HOST,
				Index:     1,
				Local:     v4(127, 0, 0, 1),
				Address:   v4(127, 0, 0, 1),
				Label:     "lo",
				Flags:     unix.IFA_F_PERMANENT,
				CacheInfo: xtcpnl.IfaCacheinfo{
					Preferred: xtcpnl.IfaLifetimeInfinityCst,
					Valid:     xtcpnl.IfaLifetimeInfinityCst,
				},
				HasCacheInfo: true,
			},
			want: "    inet 127.0.0.1/8 scope host lo\n" +
				"       valid_lft forever preferred_lft forever\n",
		},
		{
			description: "positive: the loopback v6 address keeps the trailing space its missing label leaves, ip_addr_n:5-6",
			in: xtcpnl.AddrInfo{
				Family:       unix.AF_INET6,
				Prefixlen:    128,
				Scope:        unix.RT_SCOPE_HOST,
				Index:        1,
				Local:        v6(t, "::1"),
				Address:      v6(t, "::1"),
				Flags:        unix.IFA_F_PERMANENT | unix.IFA_F_NOPREFIXROUTE,
				CacheInfo:    xtcpnl.IfaCacheinfo{Preferred: xtcpnl.IfaLifetimeInfinityCst, Valid: xtcpnl.IfaLifetimeInfinityCst},
				HasCacheInfo: true,
			},
			want: "    inet6 ::1/128 scope host noprefixroute \n" +
				"       valid_lft forever preferred_lft forever\n",
		},
		{
			description: "positive: the DHCP address, with brd, two flag tokens and a label, ip_addr_n:10-11",
			in: xtcpnl.AddrInfo{
				Family:       unix.AF_INET,
				Prefixlen:    24,
				Scope:        unix.RT_SCOPE_UNIVERSE,
				Index:        2,
				Local:        v4(172, 16, 50, 219),
				Address:      v4(172, 16, 50, 219),
				Broadcast:    v4(172, 16, 50, 255),
				Label:        "enp1s0",
				Flags:        unix.IFA_F_NOPREFIXROUTE,
				CacheInfo:    xtcpnl.IfaCacheinfo{Preferred: 47871, Valid: 47871},
				HasCacheInfo: true,
			},
			want: "    inet 172.16.50.219/24 brd 172.16.50.255 scope global dynamic noprefixroute enp1s0\n" +
				"       valid_lft 47871sec preferred_lft 47871sec\n",
		},
		{
			description: "positive: the deprecated temporary address takes the signed lifetime format, ip_addr_n:14-15",
			in: xtcpnl.AddrInfo{
				Family:       unix.AF_INET6,
				Prefixlen:    64,
				Scope:        unix.RT_SCOPE_UNIVERSE,
				Index:        2,
				Local:        v6(t, "2603:8002:ea00:6800:827f:e158:2c1c:13b4"),
				Address:      v6(t, "2603:8002:ea00:6800:827f:e158:2c1c:13b4"),
				Flags:        unix.IFA_F_SECONDARY | unix.IFA_F_DEPRECATED,
				CacheInfo:    xtcpnl.IfaCacheinfo{Preferred: 0, Valid: 34941},
				HasCacheInfo: true,
			},
			want: "    inet6 2603:8002:ea00:6800:827f:e158:2c1c:13b4/64 scope global temporary deprecated dynamic \n" +
				"       valid_lft 34941sec preferred_lft 0sec\n",
		},
		{
			description: "positive: IFA_PROTO renders as a proto clause before the label, ip_addr_n:27-28",
			in: xtcpnl.AddrInfo{
				Family:       unix.AF_INET6,
				Prefixlen:    64,
				Scope:        unix.RT_SCOPE_LINK,
				Index:        3,
				Local:        v6(t, "fe80::609:73ff:fecf:d8d0"),
				Address:      v6(t, "fe80::609:73ff:fecf:d8d0"),
				Flags:        unix.IFA_F_PERMANENT,
				Proto:        3,
				CacheInfo:    xtcpnl.IfaCacheinfo{Preferred: xtcpnl.IfaLifetimeInfinityCst, Valid: xtcpnl.IfaLifetimeInfinityCst},
				HasCacheInfo: true,
			},
			want: "    inet6 fe80::609:73ff:fecf:d8d0/64 scope link proto kernel_ll \n" +
				"       valid_lft forever preferred_lft forever\n",
		},
		{
			description: "positive: nodad on a permanent address, ip_addr_n:25-26",
			in: xtcpnl.AddrInfo{
				Family:       unix.AF_INET6,
				Prefixlen:    64,
				Scope:        unix.RT_SCOPE_UNIVERSE,
				Index:        3,
				Local:        v6(t, "fd10:10:4::2"),
				Address:      v6(t, "fd10:10:4::2"),
				Flags:        unix.IFA_F_NODAD | unix.IFA_F_PERMANENT,
				CacheInfo:    xtcpnl.IfaCacheinfo{Preferred: xtcpnl.IfaLifetimeInfinityCst, Valid: xtcpnl.IfaLifetimeInfinityCst},
				HasCacheInfo: true,
			},
			want: "    inet6 fd10:10:4::2/64 scope global nodad \n" +
				"       valid_lft forever preferred_lft forever\n",
		},
		{
			// Every address in the committed dump carries IFA_CACHEINFO, so
			// this row is the constructed complement: absent is not zero, and
			// it means no second line at all rather than "valid_lft 0sec".
			description: "boundary: no IFA_CACHEINFO prints one line, not a zero lifetime",
			in: xtcpnl.AddrInfo{
				Family:    unix.AF_INET,
				Prefixlen: 24,
				Scope:     unix.RT_SCOPE_UNIVERSE,
				Index:     9,
				Local:     v4(192, 0, 2, 1),
				Address:   v4(192, 0, 2, 1),
				Label:     "goip0",
				Flags:     unix.IFA_F_PERMANENT,
			},
			want: "    inet 192.0.2.1/24 scope global goip0\n",
		},
		{
			// IFA_ADDRESS is the peer and IFA_LOCAL the local address, which
			// is the reverse of the key names; the peer clause only appears
			// when the two differ. Nothing in the dump is point-to-point.
			description: "boundary: a differing IFA_ADDRESS becomes a peer clause before the prefix length",
			in: xtcpnl.AddrInfo{
				Family:    unix.AF_INET,
				Prefixlen: 32,
				Scope:     unix.RT_SCOPE_UNIVERSE,
				Index:     9,
				Local:     v4(192, 0, 2, 1),
				Address:   v4(192, 0, 2, 2),
				Label:     "ppp0",
				Flags:     unix.IFA_F_PERMANENT,
			},
			want: "    inet 192.0.2.1 peer 192.0.2.2/32 scope global ppp0\n",
		},
		{
			description: "boundary: a prefix length of 0 still prints, as /0 rather than being omitted",
			in: xtcpnl.AddrInfo{
				Family:    unix.AF_INET,
				Prefixlen: 0,
				Scope:     unix.RT_SCOPE_UNIVERSE,
				Index:     9,
				Local:     v4(192, 0, 2, 1),
				Address:   v4(192, 0, 2, 1),
				Flags:     unix.IFA_F_PERMANENT,
			},
			want: "    inet 192.0.2.1/0 scope global \n",
		},
		{
			// ifa_prefixlen is a byte and the kernel does not validate it
			// against the family, so 33 on AF_INET is decodable. The plan's
			// §8.4 row says the decoder keeps it verbatim; this is the
			// renderer half — it prints it rather than clamping.
			description: "negative: a prefix length of 33 on AF_INET is printed verbatim, not clamped to 32",
			in: xtcpnl.AddrInfo{
				Family:    unix.AF_INET,
				Prefixlen: 33,
				Scope:     unix.RT_SCOPE_UNIVERSE,
				Index:     9,
				Local:     v4(192, 0, 2, 1),
				Address:   v4(192, 0, 2, 1),
				Flags:     unix.IFA_F_PERMANENT,
			},
			want: "    inet 192.0.2.1/33 scope global \n",
		},
		{
			// The family_name "???" branch, which print_addrinfo answers with
			// a different format string. Constructed: an AF_UNSPEC address
			// cannot come back from an RTM_GETADDR dump.
			description: "negative: an unnameable family takes the family %d form instead of a name",
			in: xtcpnl.AddrInfo{
				Family:    unix.AF_UNSPEC,
				Prefixlen: 8,
				Scope:     unix.RT_SCOPE_UNIVERSE,
				Index:     9,
				Local:     []byte{1, 2, 3, 4},
				Address:   []byte{1, 2, 3, 4},
				Flags:     unix.IFA_F_PERMANENT,
			},
			want: "    family 0 01020304/8 scope global \n",
		},
		{
			description: "corner: an unnamed scope prints its number, keeping the field width of a name",
			in: xtcpnl.AddrInfo{
				Family:    unix.AF_INET,
				Prefixlen: 24,
				Scope:     42,
				Index:     9,
				Local:     v4(192, 0, 2, 1),
				Address:   v4(192, 0, 2, 1),
				Flags:     unix.IFA_F_PERMANENT,
			},
			want: "    inet 192.0.2.1/24 scope 42 \n",
		},
		{
			// IFA_PROTO of 0 is IFAPROT_UNSPEC, which is the absent value —
			// so "proto unspec" must never be printed even though the name
			// exists in the table.
			description: "corner: IFA_PROTO of 0 prints no proto clause, though unspec is a named value",
			in: xtcpnl.AddrInfo{
				Family:    unix.AF_INET,
				Prefixlen: 24,
				Scope:     unix.RT_SCOPE_UNIVERSE,
				Index:     9,
				Local:     v4(192, 0, 2, 1),
				Address:   v4(192, 0, 2, 1),
				Proto:     0,
				Flags:     unix.IFA_F_PERMANENT,
			},
			want: "    inet 192.0.2.1/24 scope global \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := AddrViewOf(tt.in).Text()
			if got != tt.want {
				t.Errorf("Text() mismatch\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// go test ./internal/goip/render/ -run TestAddrViewJSON
func TestAddrViewJSON(t *testing.T) {
	permanentV4 := xtcpnl.AddrInfo{
		Family:       unix.AF_INET,
		Prefixlen:    8,
		Scope:        unix.RT_SCOPE_HOST,
		Index:        1,
		Local:        v4(127, 0, 0, 1),
		Address:      v4(127, 0, 0, 1),
		Label:        "lo",
		Flags:        unix.IFA_F_PERMANENT,
		CacheInfo:    xtcpnl.IfaCacheinfo{Preferred: xtcpnl.IfaLifetimeInfinityCst, Valid: xtcpnl.IfaLifetimeInfinityCst},
		HasCacheInfo: true,
	}

	tests := []struct {
		description string
		in          xtcpnl.AddrInfo
		wantKeys    []string
		wantAbsent  []string
		check       func(t *testing.T, m map[string]any)
	}{
		{
			description: "positive: the loopback address carries ip -j's key names",
			in:          permanentV4,
			wantKeys:    []string{"family", "local", "prefixlen", "scope", "label", "valid_life_time", "preferred_life_time"},
			wantAbsent:  []string{"family_index", "address", "broadcast", "flags", "protocol"},
			check: func(t *testing.T, m map[string]any) {
				if m["family"] != "inet" {
					t.Errorf("family = %v, want inet", m["family"])
				}
				if m["prefixlen"] != float64(8) {
					t.Errorf("prefixlen = %v, want 8", m["prefixlen"])
				}
			},
		},
		{
			// prefixlen and local have no omitempty, because a /0 address and
			// a zero-length local are both real and dropping the key would
			// change the shape of the object rather than its value.
			description: "boundary: prefixlen 0 is still emitted, so the key set does not depend on the value",
			in: xtcpnl.AddrInfo{
				Family: unix.AF_INET, Prefixlen: 0, Index: 9,
				Local: v4(0, 0, 0, 0), Address: v4(0, 0, 0, 0),
				Flags: unix.IFA_F_PERMANENT,
			},
			wantKeys: []string{"family", "local", "prefixlen", "scope"},
			check: func(t *testing.T, m map[string]any) {
				if m["prefixlen"] != float64(0) {
					t.Errorf("prefixlen = %v, want 0", m["prefixlen"])
				}
			},
		},
		{
			description: "boundary: no IFA_CACHEINFO drops both lifetime keys rather than emitting zeros",
			in: xtcpnl.AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Index: 9,
				Local: v4(192, 0, 2, 1), Address: v4(192, 0, 2, 1),
				Flags: unix.IFA_F_PERMANENT,
			},
			wantAbsent: []string{"valid_life_time", "preferred_life_time"},
		},
		{
			description: "positive: flags are an array, the one deliberate key-shape divergence from ip -j",
			in: xtcpnl.AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Index: 2,
				Local:   v6(t, "2603:8002:ea00:6800:6adf:8a2f:21ae:d6a7"),
				Address: v6(t, "2603:8002:ea00:6800:6adf:8a2f:21ae:d6a7"),
				Flags:   unix.IFA_F_MANAGETEMPADDR | unix.IFA_F_NOPREFIXROUTE,
			},
			wantKeys: []string{"flags"},
			check: func(t *testing.T, m map[string]any) {
				got, ok := m["flags"].([]any)
				if !ok {
					t.Fatalf("flags is %T, want an array", m["flags"])
				}
				want := []string{"dynamic", "mngtmpaddr", "noprefixroute"}
				if len(got) != len(want) {
					t.Fatalf("flags = %v, want %v", got, want)
				}
				for i, w := range want {
					if got[i] != w {
						t.Errorf("flags[%d] = %v, want %q", i, got[i], w)
					}
				}
			},
		},
		{
			description: "negative: an unnameable family emits family_index and not family",
			in: xtcpnl.AddrInfo{
				Family: unix.AF_UNSPEC, Prefixlen: 8, Index: 9,
				Local: []byte{1, 2, 3, 4}, Address: []byte{1, 2, 3, 4},
				Flags: unix.IFA_F_PERMANENT,
			},
			wantKeys:   []string{"family_index"},
			wantAbsent: []string{"family"},
			check: func(t *testing.T, m map[string]any) {
				if m["family_index"] != float64(0) {
					t.Errorf("family_index = %v, want 0", m["family_index"])
				}
			},
		},
		{
			// The unexported deprecated field selects a format string and is
			// not part of the object; if it ever became exported the JSON
			// would gain a key `ip -j` does not have.
			description: "corner: the deprecated selector is unexported and contributes no key",
			in: xtcpnl.AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Index: 2,
				Local:        v6(t, "2603:8002:ea00:6800:827f:e158:2c1c:13b4"),
				Address:      v6(t, "2603:8002:ea00:6800:827f:e158:2c1c:13b4"),
				Flags:        unix.IFA_F_SECONDARY | unix.IFA_F_DEPRECATED,
				CacheInfo:    xtcpnl.IfaCacheinfo{Preferred: 0, Valid: 34941},
				HasCacheInfo: true,
			},
			wantAbsent: []string{"deprecated"},
			check: func(t *testing.T, m map[string]any) {
				if m["preferred_life_time"] != float64(0) {
					t.Errorf("preferred_life_time = %v, want 0", m["preferred_life_time"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			b, err := json.Marshal(AddrViewOf(tt.in))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			for _, k := range tt.wantKeys {
				if _, ok := m[k]; !ok {
					t.Errorf("key %q missing from %s", k, b)
				}
			}
			for _, k := range tt.wantAbsent {
				if _, ok := m[k]; ok {
					t.Errorf("key %q present in %s and should not be", k, b)
				}
			}
			if tt.check != nil {
				tt.check(t, m)
			}
		})
	}
}

// TestLinkViewForAddr covers the three ways `ip addr show`'s link stanza
// differs from `ip link show`'s, all of which are family-dependent and none of
// which are visible in a plain AF_UNSPEC run.
//
// go test ./internal/goip/render/ -run TestLinkViewForAddr
func TestLinkViewForAddr(t *testing.T) {
	// A veth, because it is the only shape that carries IFLA_LINK and
	// IFLA_LINK_NETNSID together and so exercises the netnsid placement.
	veth := xtcpnl.LinkInfo{
		Index:          58,
		Name:           "ve-nfb-vpn",
		Type:           unix.ARPHRD_ETHER,
		Flags:          unix.IFF_BROADCAST | unix.IFF_MULTICAST | unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_LOWER_UP,
		MTU:            1500,
		Qdisc:          "noqueue",
		OperState:      6,
		LinkMode:       0,
		Group:          0,
		HasGroup:       true,
		TxQLen:         1000,
		HasTxQLen:      true,
		Address:        []byte{0x6e, 0x01, 0x02, 0x03, 0x04, 0x05},
		Broadcast:      []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		Link:           2,
		LinkNetnsID:    0,
		HasLinkNetnsID: true,
	}

	names := fakeNames{
		2:  {name: "enp1s0", flags: unix.IFF_UP | unix.IFF_LOWER_UP},
		58: {name: "ve-nfb-vpn", flags: unix.IFF_UP | unix.IFF_LOWER_UP},
	}

	tests := []struct {
		description string
		family      uint8
		wantLines   int
		check       func(t *testing.T, v LinkView, text string)
	}{
		{
			// A plain `addr show` stanza is two lines: the stanza line and
			// the link/ continuation.
			description: "positive: AF_UNSPEC keeps the link/ line, so the stanza is two lines",
			family:      unix.AF_UNSPEC,
			wantLines:   2,
			check: func(t *testing.T, v LinkView, text string) {
				if !strings.Contains(text, "    link/ether ") {
					t.Errorf("no link/ether line in %q", text)
				}
				// On this path link-netnsid is on line two, with the MAC.
				second := strings.Split(text, "\n")[1]
				if !strings.Contains(second, "link-netnsid 0") {
					t.Errorf("link-netnsid not on the link/ line: %q", second)
				}
			},
		},
		{
			description: "positive: AF_PACKET keeps the link/ line too, since it is the filter.family == AF_PACKET arm of the same guard",
			family:      unix.AF_PACKET,
			wantLines:   2,
			check: func(t *testing.T, v LinkView, text string) {
				if v.LinkType != "ether" {
					t.Errorf("LinkType = %q, want ether", v.LinkType)
				}
			},
		},
		{
			// The guard at ip/ipaddress.c:1060 opens with the print_nl() that
			// starts line two, so suppressing it removes the newline as well
			// as the fields — and the link-netnsid print at :1114 sits
			// outside the guard and lands on the stanza line instead.
			description: "positive: AF_INET drops the link/ line and moves link-netnsid onto the stanza line",
			family:      unix.AF_INET,
			wantLines:   1,
			check: func(t *testing.T, v LinkView, text string) {
				if strings.Contains(text, "link/") {
					t.Errorf("link/ line survived under AF_INET: %q", text)
				}
				if !strings.HasSuffix(strings.TrimRight(text, "\n"), "link-netnsid 0") {
					t.Errorf("link-netnsid did not move to the stanza line: %q", text)
				}
			},
		},
		{
			description: "positive: AF_INET6 drops the link/ line by the same guard",
			family:      unix.AF_INET6,
			wantLines:   1,
			check: func(t *testing.T, v LinkView, text string) {
				if strings.Contains(text, "link/") {
					t.Errorf("link/ line survived under AF_INET6: %q", text)
				}
			},
		},
		{
			// print_linkmode is gated on do_link, which only ipaddr_list_link
			// sets, so `addr show` never prints "mode DEFAULT" for any
			// family — and since print_linkmode is the sole emitter of the
			// JSON key, `ip -j addr show` has no "linkmode" either. The
			// sidecars confirm it: ip_link_n:1 has it, ip_addr_n:1 does not.
			description: "negative: no family prints mode DEFAULT, because do_link is 0 for addr show",
			family:      unix.AF_UNSPEC,
			wantLines:   2,
			check: func(t *testing.T, v LinkView, text string) {
				if v.LinkMode != "" {
					t.Errorf("LinkMode = %q, want empty", v.LinkMode)
				}
				if strings.Contains(text, "mode ") {
					t.Errorf("a mode clause survived: %q", text)
				}
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatalf("Marshal: %v", err)
				}
				if strings.Contains(string(b), "linkmode") {
					t.Errorf("linkmode key in JSON: %s", b)
				}
			},
		},
		{
			// Non-vacuity for the row above: LinkViewOf, which `link show`
			// uses, does set it. If both were empty the row would prove
			// nothing.
			description: "boundary: LinkViewOf does set LinkMode, so the clearing above is a real difference",
			family:      unix.AF_UNSPEC,
			wantLines:   2,
			check: func(t *testing.T, v LinkView, text string) {
				if LinkViewOf(veth, names).LinkMode != "DEFAULT" {
					t.Errorf("LinkViewOf LinkMode = %q, want DEFAULT",
						LinkViewOf(veth, names).LinkMode)
				}
			},
		},
		{
			description: "boundary: the three link/ fields are cleared, not merely hidden, so the JSON loses them with the text",
			family:      unix.AF_INET,
			wantLines:   1,
			check: func(t *testing.T, v LinkView, text string) {
				if v.LinkType != "" || v.Address != "" || v.Broadcast != "" {
					t.Errorf("LinkType/Address/Broadcast = %q/%q/%q, want all empty",
						v.LinkType, v.Address, v.Broadcast)
				}
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatalf("Marshal: %v", err)
				}
				for _, k := range []string{"link_type", "address", "broadcast"} {
					if strings.Contains(string(b), `"`+k+`"`) {
						t.Errorf("key %q still in JSON: %s", k, b)
					}
				}
			},
		},
		{
			// The stanza line itself is family-independent: everything the
			// guard touches is on line two. This row exists so a future
			// change that starts trimming the first line fails here.
			description: "corner: the stanza line is byte-identical across all four families",
			family:      unix.AF_INET6,
			wantLines:   1,
			check: func(t *testing.T, v LinkView, text string) {
				want := firstLine(LinkViewForAddr(veth, names, unix.AF_UNSPEC).Text())
				got := firstLine(text)
				// AF_UNSPEC keeps link-netnsid on line two; the other
				// families append it to line one, so compare up to it.
				got = strings.TrimSuffix(got, " link-netnsid 0")
				if got != want {
					t.Errorf("stanza line differs\n got %q\nwant %q", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			v := LinkViewForAddr(veth, names, tt.family)
			text := v.Text()
			if n := len(strings.Split(strings.TrimRight(text, "\n"), "\n")); n != tt.wantLines {
				t.Errorf("stanza has %d lines, want %d:\n%s", n, tt.wantLines, text)
			}
			if tt.check != nil {
				tt.check(t, v, text)
			}
		})
	}
}

// firstLine is the stanza line of a rendered link, without its newline.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestLinkViewForAddrPresence covers the presence flags, which exist because
// `ip -6 addr show`'s link replies come from inet6_dump_ifinfo rather than
// rtnl_dump_ifinfo and carry neither IFLA_TXQLEN nor IFLA_GROUP. Absent is not
// zero: the plan did not anticipate this asymmetry and a whole-struct zero
// value would have rendered "group 0 qlen 0".
//
// go test ./internal/goip/render/ -run TestLinkViewForAddrPresence
func TestLinkViewForAddrPresence(t *testing.T) {
	names := fakeNames{1: {name: "lo", flags: unix.IFF_UP | unix.IFF_LOWER_UP}}

	base := xtcpnl.LinkInfo{
		Index:     1,
		Name:      "lo",
		Type:      unix.ARPHRD_LOOPBACK,
		Flags:     unix.IFF_LOOPBACK | unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_LOWER_UP,
		MTU:       65536,
		OperState: 0,
	}

	withFull := base
	withFull.Qdisc = "noqueue"
	withFull.Group, withFull.HasGroup = 0, true
	withFull.TxQLen, withFull.HasTxQLen = 1000, true

	tests := []struct {
		description string
		in          xtcpnl.LinkInfo
		family      uint8
		wantContain []string
		wantAbsent  []string
		// qlenZero flips render.RenderQlenZero for the row, so the two sides
		// of the faceb326 first locus each get an expectation instead of one
		// being reasoned about.
		qlenZero bool
		check    func(t *testing.T, v LinkView)
	}{
		{
			// ip_addr_n:1 — "qdisc noqueue state UNKNOWN group default qlen 1000".
			description: "positive: a full rtnl_dump_ifinfo reply prints qdisc, group and qlen",
			in:          withFull,
			family:      unix.AF_UNSPEC,
			wantContain: []string{"qdisc noqueue", "group default", "qlen 1000"},
		},
		{
			// An inet6_dump_ifinfo reply: IFLA_IFNAME, ADDRESS, MTU, LINK,
			// OPERSTATE, PROTINFO and nothing else.
			description: "negative: an inet6_dump_ifinfo reply prints no qdisc, no group and no qlen",
			in:          base,
			family:      unix.AF_INET6,
			wantContain: []string{"state UNKNOWN"},
			wantAbsent:  []string{"qdisc", "group ", "qlen"},
		},
		{
			// The faceb326 divergence's second locus: 7.1.0 falls back to a
			// SIOCGIFTXQLEN ioctl in print_queuelen when IFLA_TXQLEN is
			// absent, so it prints "qlen 1000" here and goip prints nothing.
			// Recorded as a version-skew allowlist entry, not as a bug.
			description: "negative: the missing qlen under AF_INET6 is the faceb326 ioctl divergence, not a decode gap",
			in:          base,
			family:      unix.AF_INET6,
			wantAbsent:  []string{"qlen"},
		},
		{
			// Both suppress the clause, and for unrelated reasons: an
			// IFLA_TXQLEN of 0 is suppressed by print_queuelen's `if (qlen)`
			// guard (the faceb326 first locus, reproduced by
			// RenderQlenZero == false), while an absent one never reaches the
			// print at all. Same output, different cause — so the text cannot
			// distinguish them and the view has to; see the two rows below.
			description: "boundary: an IFLA_TXQLEN of 0 prints no qlen either, but by the RenderQlenZero guard rather than by absence",
			in: func() xtcpnl.LinkInfo {
				li := withFull
				li.TxQLen = 0
				return li
			}(),
			family:     unix.AF_UNSPEC,
			wantAbsent: []string{"qlen"},
			check: func(t *testing.T, v LinkView) {
				// The distinction the text cannot carry: present-and-zero is
				// a non-nil pointer to 0, absent is a nil pointer. Collapsing
				// the two would make the row above and the row two below
				// indistinguishable.
				if v.TxQLen == nil {
					t.Fatalf("TxQLen is nil, want a pointer to 0")
				}
				if *v.TxQLen != 0 {
					t.Errorf("*TxQLen = %d, want 0", *v.TxQLen)
				}
			},
		},
		{
			// The other side of the same knob, so neither behavior is only
			// reasoned about. RenderQlenZero == true is the development
			// branch's unconditional print.
			description: "boundary: with RenderQlenZero set, the same zero txqlen does print qlen 0",
			in: func() xtcpnl.LinkInfo {
				li := withFull
				li.TxQLen = 0
				return li
			}(),
			family:      unix.AF_UNSPEC,
			qlenZero:    true,
			wantContain: []string{"qlen 0"},
		},
		{
			description: "boundary: an IFLA_GROUP of 0 is the default group, which is a name and not a number",
			in:          withFull,
			family:      unix.AF_UNSPEC,
			wantContain: []string{"group default"},
			check: func(t *testing.T, v LinkView) {
				if v.Group != "default" {
					t.Errorf("Group = %q, want default", v.Group)
				}
			},
		},
		{
			// Same bytes, same family, one flag apart — so the flag and not
			// the value is what the rendering turns on.
			description: "corner: clearing only HasTxQLen removes the clause while the value stays 1000",
			in: func() xtcpnl.LinkInfo {
				li := withFull
				li.HasTxQLen = false
				return li
			}(),
			family:      unix.AF_UNSPEC,
			wantContain: []string{"group default"},
			wantAbsent:  []string{"qlen"},
			check: func(t *testing.T, v LinkView) {
				if v.TxQLen != nil {
					t.Errorf("TxQLen = %d, want nil", *v.TxQLen)
				}
			},
		},
		{
			// Absence with the knob set, which is what separates the two
			// faceb326 loci: flipping RenderQlenZero cannot bring back a
			// clause the reply has no attribute for, so the ioctl half needs
			// its own allowlist entry rather than being covered by the knob.
			description: "corner: RenderQlenZero cannot restore an absent txqlen, which is why faceb326 needs two allowlist loci",
			in:          base,
			family:      unix.AF_INET6,
			qlenZero:    true,
			wantAbsent:  []string{"qlen"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.qlenZero {
				defer func(prev bool) { RenderQlenZero = prev }(RenderQlenZero)
				RenderQlenZero = true
			}
			v := LinkViewForAddr(tt.in, names, tt.family)
			text := v.Text()
			if tt.check != nil {
				tt.check(t, v)
			}
			for _, s := range tt.wantContain {
				if !strings.Contains(text, s) {
					t.Errorf("missing %q in %q", s, text)
				}
			}
			for _, s := range tt.wantAbsent {
				if strings.Contains(text, s) {
					t.Errorf("unexpected %q in %q", s, text)
				}
			}
		})
	}
}

// TestAddrGroupViewText covers the nesting, which mirrors iproute2's own loop
// over links rather than over addresses.
//
// go test ./internal/goip/render/ -run TestAddrGroupViewText
func TestAddrGroupViewText(t *testing.T) {
	names := fakeNames{1: {name: "lo", flags: unix.IFF_UP | unix.IFF_LOWER_UP}}

	lo := xtcpnl.LinkInfo{
		Index: 1, Name: "lo", Type: unix.ARPHRD_LOOPBACK,
		Flags:     unix.IFF_LOOPBACK | unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_LOWER_UP,
		MTU:       65536,
		Qdisc:     "noqueue",
		OperState: 0,
		Group:     0, HasGroup: true,
		TxQLen: 1000, HasTxQLen: true,
		Address:   []byte{0, 0, 0, 0, 0, 0},
		Broadcast: []byte{0, 0, 0, 0, 0, 0},
	}

	loV4 := AddrViewOf(xtcpnl.AddrInfo{
		Family: unix.AF_INET, Prefixlen: 8, Scope: unix.RT_SCOPE_HOST, Index: 1,
		Local: v4(127, 0, 0, 1), Address: v4(127, 0, 0, 1), Label: "lo",
		Flags:        unix.IFA_F_PERMANENT,
		CacheInfo:    xtcpnl.IfaCacheinfo{Preferred: xtcpnl.IfaLifetimeInfinityCst, Valid: xtcpnl.IfaLifetimeInfinityCst},
		HasCacheInfo: true,
	})
	loV6 := AddrViewOf(xtcpnl.AddrInfo{
		Family: unix.AF_INET6, Prefixlen: 128, Scope: unix.RT_SCOPE_HOST, Index: 1,
		Local: v6(t, "::1"), Address: v6(t, "::1"),
		Flags:        unix.IFA_F_PERMANENT | unix.IFA_F_NOPREFIXROUTE,
		CacheInfo:    xtcpnl.IfaCacheinfo{Preferred: xtcpnl.IfaLifetimeInfinityCst, Valid: xtcpnl.IfaLifetimeInfinityCst},
		HasCacheInfo: true,
	})

	tests := []struct {
		description string
		addrs       []AddrView
		wantLines   []string
	}{
		{
			description: "positive: lo with both addresses reproduces ip_addr_n:1-6 minus the -d fields",
			addrs:       []AddrView{loV4, loV6},
			wantLines: []string{
				"1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000",
				"    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00",
				"    inet 127.0.0.1/8 scope host lo",
				"       valid_lft forever preferred_lft forever",
				"    inet6 ::1/128 scope host noprefixroute ",
				"       valid_lft forever preferred_lft forever",
			},
		},
		{
			// nlmon0 in the fixture has no addresses at all and still prints,
			// because ipaddr_filter's missing_net_address rescue keeps it
			// under AF_UNSPEC. So an empty AddrInfo is the stanza alone.
			description: "boundary: a link with no addresses prints its stanza and nothing more",
			addrs:       []AddrView{},
			wantLines: []string{
				"1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000",
				"    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00",
			},
		},
		{
			description: "negative: a nil AddrInfo behaves as an empty one rather than panicking",
			addrs:       nil,
			wantLines: []string{
				"1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000",
				"    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00",
			},
		},
		{
			// Order is the order the group was built in, never sorted: an
			// AF_UNSPEC dump walks families in turn, so every v4 line
			// precedes every v6 line for a given link and re-sorting here
			// would break the comparison against the sidecar.
			description: "corner: addresses render in slice order, so a v6-before-v4 group prints that way",
			addrs:       []AddrView{loV6, loV4},
			wantLines: []string{
				"1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000",
				"    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00",
				"    inet6 ::1/128 scope host noprefixroute ",
				"       valid_lft forever preferred_lft forever",
				"    inet 127.0.0.1/8 scope host lo",
				"       valid_lft forever preferred_lft forever",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := AddrGroupView{
				LinkView: LinkViewForAddr(lo, names, unix.AF_UNSPEC),
				AddrInfo: tt.addrs,
			}
			got := strings.Split(strings.TrimRight(g.Text(), "\n"), "\n")
			if len(got) != len(tt.wantLines) {
				t.Fatalf("got %d lines, want %d:\n%s", len(got), len(tt.wantLines), g.Text())
			}
			for i := range got {
				if got[i] != tt.wantLines[i] {
					t.Errorf("line %d\n got %q\nwant %q", i+1, got[i], tt.wantLines[i])
				}
			}
		})
	}
}

// go test ./internal/goip/render/ -run TestAddrGroupViewJSON
func TestAddrGroupViewJSON(t *testing.T) {
	names := fakeNames{1: {name: "lo", flags: unix.IFF_UP}}
	lo := xtcpnl.LinkInfo{
		Index: 1, Name: "lo", Type: unix.ARPHRD_LOOPBACK,
		Flags: unix.IFF_LOOPBACK | unix.IFF_UP | unix.IFF_RUNNING, MTU: 65536,
		Group: 0, HasGroup: true, TxQLen: 1000, HasTxQLen: true,
	}

	tests := []struct {
		description string
		addrs       []AddrView
		check       func(t *testing.T, raw string, m map[string]any)
	}{
		{
			// The embedded LinkView is not a nested object: `ip -j addr show`
			// puts addr_info alongside ifindex and ifname in one object, and
			// Go's embedding is what produces that shape.
			description: "positive: the link fields and addr_info share one flat object",
			addrs:       []AddrView{{Family: "inet", Local: "127.0.0.1", PrefixLen: 8, Scope: "host"}},
			check: func(t *testing.T, raw string, m map[string]any) {
				for _, k := range []string{"ifindex", "ifname", "addr_info"} {
					if _, ok := m[k]; !ok {
						t.Errorf("key %q missing from %s", k, raw)
					}
				}
				if _, ok := m["LinkView"]; ok {
					t.Errorf("LinkView appears as a nested key: %s", raw)
				}
			},
		},
		{
			// addr_info has no omitempty, so a link with no addresses is [].
			// `ip -j` does the same, and a consumer indexing the array must
			// not have to handle null.
			description: "boundary: an empty addr_info marshals as [], not as an absent key",
			addrs:       []AddrView{},
			check: func(t *testing.T, raw string, m map[string]any) {
				v, ok := m["addr_info"]
				if !ok {
					t.Fatalf("addr_info missing from %s", raw)
				}
				if a, ok := v.([]any); !ok || len(a) != 0 {
					t.Errorf("addr_info = %v, want an empty array", v)
				}
			},
		},
		{
			// The one case the caller has to prevent: a nil slice marshals as
			// null, which is why obj_addr.go builds the group with an
			// explicitly empty literal. This row documents what happens if it
			// stops doing so.
			description: "negative: a nil addr_info marshals as null, which is why the caller must pass an empty slice",
			addrs:       nil,
			check: func(t *testing.T, raw string, m map[string]any) {
				if !strings.Contains(raw, `"addr_info":null`) {
					t.Errorf("want addr_info null in %s", raw)
				}
			},
		},
		{
			description: "corner: two addresses stay ordered in the array as they are in the text",
			addrs: []AddrView{
				{Family: "inet", Local: "127.0.0.1", PrefixLen: 8, Scope: "host"},
				{Family: "inet6", Local: "::1", PrefixLen: 128, Scope: "host"},
			},
			check: func(t *testing.T, raw string, m map[string]any) {
				a, ok := m["addr_info"].([]any)
				if !ok || len(a) != 2 {
					t.Fatalf("addr_info = %v, want two entries", m["addr_info"])
				}
				first, _ := a[0].(map[string]any)
				if first["family"] != "inet" {
					t.Errorf("addr_info[0].family = %v, want inet", first["family"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := AddrGroupView{
				LinkView: LinkViewForAddr(lo, names, unix.AF_UNSPEC),
				AddrInfo: tt.addrs,
			}
			b, err := json.Marshal(g)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			tt.check(t, string(b), m)
		})
	}
}
