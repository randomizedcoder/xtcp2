package goip

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// NX3's writes (goip-parity-next.md): AF_MPLS routes, `as` on the route
// and in a hop, and `ttl-propagate`. O5 holds the 109 captured mpls rows to
// ip's bytes; these rows are the edges no capture has.

// mplsLabels is a label stack as mpls_pton writes it: each label << 12,
// big-endian, the bottom-of-stack bit on the last.
func mplsLabels(labels ...uint32) []byte {
	var b []byte
	for i, l := range labels {
		e := l << 12
		if i == len(labels)-1 {
			e |= 1 << 8
		}
		b = binary.BigEndian.AppendUint32(b, e)
	}
	return b
}

// mplsVia is RTA_VIA's payload: the family, host order, then the address.
func mplsVia(family uint16, addr ...byte) xtcpnl.RouteAttr {
	return xtcpnl.RouteAttrBytes(unix.RTA_VIA, append(binary.LittleEndian.AppendUint16(nil, family), addr...))
}

// TestRouteWriteMpls is iproute_modify under -f mpls, and `as` on an IPv4
// route. d0 is ifindex 2.
//
// go test ./internal/goip/ -run TestRouteWriteMpls
func TestRouteWriteMpls(t *testing.T) {
	const create, excl = unix.NLM_F_CREATE, unix.NLM_F_EXCL
	dst := func(l ...uint32) xtcpnl.RouteAttr { return xtcpnl.RouteAttrBytes(unix.RTA_DST, mplsLabels(l...)) }
	newdst := func(l ...uint32) xtcpnl.RouteAttr { return xtcpnl.RouteAttrBytes(unix.RTA_NEWDST, mplsLabels(l...)) }
	ttl := func(v byte) xtcpnl.RouteAttr { return xtcpnl.RouteAttrBytes(unix.RTA_TTL_PROPAGATE, []byte{v}) }
	oif := rtaOif(2)
	inet := mplsVia(unix.AF_INET, 10, 0, 0, 2)
	// mpls is an AF_MPLS add with iproute_modify's defaults: table main,
	// proto boot, scope universe and, with no type given, unicast.
	mpls := func(dstLen uint8, attrs ...xtcpnl.RouteAttr) xtcpnl.RouteWrite {
		w := newRouteWrite(create|excl, dstLen, attrs...)
		w.Family, w.Scope = unix.AF_MPLS, unix.RT_SCOPE_UNIVERSE
		return w
	}
	ok := func(w xtcpnl.RouteWrite) routeWriteExpected {
		return routeWriteExpected{write: w, resolved: []string{"d0"}}
	}
	invalid := func(msg string) routeWriteExpected {
		return routeWriteExpected{err: ErrInvalidArgument, errMsg: msg}
	}
	stack := func(n int) string {
		l := make([]string, n)
		for i := range l {
			l[i] = strconv.Itoa(16 + i)
		}
		return strings.Join(l, "/")
	}
	labels64 := make([]uint32, 64)
	for i := range labels64 {
		labels64[i] = uint32(16 + i)
	}
	rows := []routeWriteRow{
		{description: "positive: a label to a device", verb: "add", args: "100 dev d0",
			expected: ok(mpls(20, dst(100), oif))},
		{description: "positive: a swap to a stack via an IPv4 neighbor, in argv order", verb: "add",
			args: "100 as 200/300 via inet 10.0.0.2 dev d0", expected: ok(mpls(20, dst(100), newdst(200, 300), inet, oif))},
		{description: "positive: as to skips the to", verb: "add", args: "100 as to 200 dev d0",
			expected: ok(mpls(20, dst(100), newdst(200), oif))},
		{description: "positive: via an IPv6 neighbor", verb: "add", args: "100 via inet6 2001:db8::2 dev d0",
			expected: ok(mpls(20, dst(100), mplsVia(unix.AF_INET6, 0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2), oif))},
		{description: "positive: ttl-propagate en is enabled, at its place", verb: "add", args: "100 ttl-propagate en dev d0",
			expected: ok(mpls(20, dst(100), ttl(1), oif))},
		{description: "positive: ttl-propagate d is disabled", verb: "add", args: "100 ttl-propagate d dev d0",
			expected: ok(mpls(20, dst(100), ttl(0), oif))},
		{description: "positive: a hop's as is in the hop, before its via", verb: "add",
			args: "100 nexthop as 300 via inet 10.0.0.2 dev d0",
			expected: ok(routeWith(mpls(20, dst(100)), func(w *xtcpnl.RouteWrite) {
				w.Nexthops = []xtcpnl.RouteNexthop{{Ifindex: 2, Attrs: []xtcpnl.RouteAttr{newdst(300), inet}}}
			}))},
		{description: "positive: a delete is unicast, scope universe, with no protocol", verb: "del", args: "100",
			expected: routeWriteExpected{write: xtcpnl.RouteWrite{MsgType: unix.RTM_DELROUTE, Flags: unix.NLM_F_ACK,
				Family: unix.AF_MPLS, DstLen: 20, Table: unix.RT_TABLE_MAIN, Scope: unix.RT_SCOPE_UNIVERSE,
				Type: unix.RTN_UNICAST, Attrs: []xtcpnl.RouteAttr{dst(100)}}}},
		{description: "boundary: the largest label", verb: "add", args: "1048575 dev d0",
			expected: ok(mpls(20, dst(1048575), oif))},
		{description: "boundary: a reserved label is sent, for the kernel to refuse", verb: "add", args: "15 dev d0",
			expected: ok(mpls(20, dst(15), oif))},
		{description: "boundary: a 64-label stack, inet_prefix's whole buffer", verb: "add", args: "100 as " + stack(64) + " dev d0",
			expected: ok(mpls(20, dst(100), newdst(labels64...), oif))},
		{description: "boundary: a prefix length of 0", verb: "add", args: "100/0 dev d0",
			expected: ok(mpls(0, dst(100), oif))},
		{description: "corner: a label in hex, strtoul's base 0", verb: "add", args: "0x64 dev d0",
			expected: ok(mpls(20, dst(100), oif))},
		{description: "corner: as twice is two RTA_NEWDST", verb: "add", args: "100 as 200 as 300 dev d0",
			expected: ok(mpls(20, dst(100), newdst(200), newdst(300), oif))},
		{description: "corner: ttl-propagate twice is sent twice", verb: "add", args: "100 ttl-propagate disabled ttl-propagate enabled dev d0",
			expected: ok(mpls(20, dst(100), ttl(0), ttl(1), oif))},
		{description: "corner: a type given keeps it, not unicast", verb: "add", args: "blackhole 100",
			expected: routeWriteExpected{write: routeWith(mpls(20, dst(100)), func(w *xtcpnl.RouteWrite) { w.Type = unix.RTN_BLACKHOLE })}},
		{description: "negative: a label past 20 bits", verb: "add", args: "1048576 dev d0",
			expected: invalid(`mpls prefix is expected rather than "1048576"`)},
		{description: "negative: a prefix length past 20", verb: "add", args: "100/21 dev d0",
			expected: invalid(`mpls prefix is expected rather than "100/21"`)},
		{description: "negative: default is no label", verb: "add", args: "default dev d0",
			expected: invalid(`mpls prefix is expected rather than "default"`)},
		{description: "negative: any is no label either", verb: "add", args: "100 as any dev d0",
			expected: invalid(`mpls address is expected rather than "any"`)},
		{description: "negative: an as label past 20 bits", verb: "add", args: "100 as 1048576 dev d0",
			expected: invalid(`mpls address is expected rather than "1048576"`)},
		{description: "negative: a 65-label stack, after mpls_pton1's own line", verb: "add", args: "100 as " + stack(65) + " dev d0",
			expected: invalid("Error: too many labels.\nError: mpls address is expected rather than")},
		{description: "corner: a long stack with a ':' is an IPv6 address, so no \"too many labels\"", verb: "add",
			args: "100 as " + stack(65) + ": dev d0", expected: invalid("invalid argument: Error: mpls address is expected rather than")},
		{description: "negative: a via with no family is read as a label", verb: "add", args: "100 via 10.0.0.2 dev d0",
			expected: invalid(`mpls address is expected rather than "10.0.0.2"`)},
		{description: "negative: an IPv6 via with no family is not a label", verb: "add", args: "100 via 2001:db8::2 dev d0",
			expected: invalid(`mpls address is expected rather than "2001:db8::2"`)},
		{description: "negative: a ttl-propagate word that is neither", verb: "add", args: "100 ttl-propagate maybe dev d0",
			expected: invalid(`Error: argument "maybe" is wrong: "ttl-propagate" value is invalid`)},
		{description: "negative: ttl-propagate as the last word", verb: "add", args: "100 ttl-propagate",
			expected: invalid("Command line is not complete")},
		{description: "negative: as to as the last words", verb: "add", args: "100 as to",
			expected: invalid("Command line is not complete")},
		{description: "negative: a hop's as label past 20 bits", verb: "add", args: "100 nexthop as 1048576 dev d0",
			expected: invalid(`mpls address is expected rather than "1048576"`)},
	}
	for i := range rows {
		rows[i].family = unix.AF_MPLS
	}
	runRouteWriteRows(t, rows)
	runRouteWriteRows(t, []routeWriteRow{
		{description: "positive: as on an IPv4 route is an IPv4 RTA_NEWDST", verb: "add", args: "10.1.0.0/24 as 200 via 10.0.0.2",
			expected: routeWriteExpected{write: scopeUniverse(newRouteWrite(create|excl, 24, rtaDst("10.1.0.0"),
				xtcpnl.RouteAttrBytes(unix.RTA_NEWDST, []byte{200, 0, 0, 0}), rtaAddr(unix.RTA_GATEWAY, "10.0.0.2")))}},
		{description: "negative: an IPv6 as on an IPv4 route", verb: "add", args: "10.1.0.0/24 as 2001:db8::1 via 10.0.0.2",
			expected: invalid(`inet address is expected rather than "2001:db8::1"`)},
		{description: "corner: an as before the destination sets the family, as the other address arms do", verb: "add",
			args: "as 2001:db8::1 10.1.0.0/24", expected: invalid(`inet6 prefix is expected rather than "10.1.0.0/24"`)},
	})
}

// mplsRun is a Run's exit code and a fragment of its stderr.
type mplsRun struct {
	code   int
	stderr string
}

// TestRunMplsFamily is the -M and -family options through Run: route's
// writes, get and show take AF_MPLS, and every verb and object that would read it
// otherwise is refused rather than run in the wrong family.
//
// go test ./internal/goip/ -run TestRunMplsFamily
func TestRunMplsFamily(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    mplsRun
	}{
		{description: "positive: -M reaches route's write, which needs GOIP_WRITE_OUT",
			args: []string{"-M", "route", "add", "100", "via", "inet", "10.0.0.2"}, expected: mplsRun{code: ExitUsage, stderr: "never applies a write"}},
		{description: "positive: -f mpls is -M", args: []string{"-f", "mpls", "route", "del", "100"},
			expected: mplsRun{code: ExitUsage, stderr: "never applies a write"}},
		{description: "negative: route flush in AF_MPLS", args: []string{"-M", "route", "flush"},
			expected: mplsRun{code: ExitUsage, stderr: "route flush in family mpls"}},
		{description: "negative: route save in AF_MPLS", args: []string{"-f", "mpls", "route", "save"},
			expected: mplsRun{code: ExitUsage, stderr: "route save in family mpls"}},
		{description: "negative: another object in AF_MPLS", args: []string{"-M", "address", "show"},
			expected: mplsRun{code: ExitUsage, stderr: "family mpls is only implemented for route"}},
		{description: "negative: AF_BRIDGE on any object", args: []string{"-f", "bridge", "route", "show"},
			expected: mplsRun{code: ExitUsage, stderr: "family bridge is not implemented"}},
		{description: "negative: a word read_family does not know is invarg, exit -1", args: []string{"-f", "ipx", "route"},
			expected: mplsRun{code: ExitMinusOne, stderr: `Error: argument "ipx" is wrong: invalid protocol family`}},
		{description: "corner: -f help is the usage, not a family", args: []string{"-f", "help", "route"},
			expected: mplsRun{code: ExitOK}},
		{description: "corner: read_family is strcmp, so MPLS is not mpls", args: []string{"-f", "MPLS", "route"},
			expected: mplsRun{code: ExitMinusOne, stderr: "invalid protocol family"}},
		{description: "corner: -f inet is -4, which every object takes", args: []string{"-f", "inet", "route", "add", "blackhole", "198.51.100.0/24"},
			expected: mplsRun{code: ExitUsage, stderr: "never applies a write"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", linkDumpPcap)
			t.Setenv(writeOutEnvCst, "")
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != tc.expected.code {
				t.Errorf("code = %d, want %d; stderr %q", code, tc.expected.code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.expected.stderr) {
				t.Errorf("stderr = %q, want %q", stderr.String(), tc.expected.stderr)
			}
		})
	}
}
