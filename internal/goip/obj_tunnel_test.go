package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fakeTnlSys is a tnlsys.Sys from maps: an ioctl answer by
// "family/req/name", a property by "dev/prop", and the descriptors. Every
// ioctl's input is kept, to hold goip to the bytes ip passes.
type fakeTnlSys struct {
	ioctls map[string]fakeIoctl
	props  map[string]fakeProp
	fds    []tnlsys.TunFD
	fdsErr error
	inputs []string
}

type fakeIoctl struct {
	out []byte
	err error
}

type fakeProp struct {
	v   string
	err error
}

func (f *fakeTnlSys) Ioctl(family int, req uint32, name string, arg []byte) ([]byte, error) {
	k := fmt.Sprintf("%d/%x/%s", family, req, name)
	f.inputs = append(f.inputs, fmt.Sprintf("%s=%x", k, arg))
	a, ok := f.ioctls[k]
	if !ok {
		return nil, fmt.Errorf("%w: %s", tnlsys.ErrNotRecorded, k)
	}
	copy(arg, a.out)
	return nil, a.err
}

func (f *fakeTnlSys) ReadProp(dev, prop string) (string, error) {
	p, ok := f.props[dev+"/"+prop]
	if !ok {
		return "", fmt.Errorf("%w: %s/%s", tnlsys.ErrNotRecorded, dev, prop)
	}
	return p.v, p.err
}

func (f *fakeTnlSys) TunFDs() ([]tnlsys.TunFD, error) { return f.fds, f.fdsErr }

// fakeLinkDump answers every dump with its bodies.
type fakeLinkDump struct{ bodies [][]byte }

func (s *fakeLinkDump) Dump([]byte, uint16) ([][]byte, error) { return s.bodies, nil }

// tnlLink is an RTM_NEWLINK body: ifinfomsg of the ARPHRD, IFLA_IFNAME,
// then the kind and IFLA_STATS64 when given.
func tnlLink(index int32, arphrd uint16, name, kind string, stats bool) []byte {
	b := ifinfomsg(index, 0, 0)
	binary.LittleEndian.PutUint16(b[2:4], arphrd)
	b = append(b, nla(unix.IFLA_IFNAME, strz(name))...)
	if kind != "" {
		b = append(b, nla(unix.IFLA_LINKINFO, nla(unix.IFLA_INFO_KIND, []byte(kind)))...)
	}
	if stats {
		s := make([]byte, xtcpnl.RtnlLinkStats64SizeCst)
		s[0], s[16] = 7, 9 // rx_packets 7, rx_bytes 9
		b = append(b, nla(unix.IFLA_STATS64, s)...)
	}
	return b
}

var (
	tnlA4 = netip.MustParseAddr
	// tnlGre1 is a gre tunnel with key 5 both ways and seq in.
	tnlGre1 = xtcpnl.IPTunnelParm{TunnelName: xtcpnl.TunnelName{Name: "gre1"}, IFlags: xtcpnl.GreFlagKey | xtcpnl.GreFlagSeq,
		OFlags: xtcpnl.GreFlagKey, IKey: 5, OKey: 5, VersionIHL: 0x45, FragOff: 0x4000, TTL: 32, Protocol: 47,
		Saddr: tnlA4("192.0.2.1"), Daddr: tnlA4("198.51.100.2")}
	tnlIpip1 = xtcpnl.IPTunnelParm{TunnelName: xtcpnl.TunnelName{Name: "ipip1"}, VersionIHL: 0x45, FragOff: 0x4000,
		Protocol: 4, Saddr: tnlA4("0.0.0.0"), Daddr: tnlA4("198.51.100.1")}
	tnlIsa1 = xtcpnl.IPTunnelParm{TunnelName: xtcpnl.TunnelName{Name: "isa1"}, IFlags: xtcpnl.SitIsatap,
		VersionIHL: 0x45, FragOff: 0x4000, Protocol: 41, Saddr: tnlA4("192.0.2.1"), Daddr: tnlA4("0.0.0.0")}
	tnlIP6t1 = xtcpnl.IP6TnlParm2{TunnelName: xtcpnl.TunnelName{Name: "ip6t1"}, Proto: 41, EncapLimit: 4, HopLimit: 1,
		FlowInfo: 0x12345, Flags: xtcpnl.IP6TnlFIgnEncapLimit | xtcpnl.IP6TnlFUseOrigTclass,
		Laddr: tnlA4("2001:db8::1"), Raddr: tnlA4("2001:db8::2")}
)

func tnlOut(p xtcpnl.IPTunnelParm) fakeIoctl { return fakeIoctl{out: p.Marshal()} }

// tnlPRL is a SIOCGETPRL answer: the header, then each router.
func tnlPRL(routers ...xtcpnl.TunnelPRL) fakeIoctl {
	b := xtcpnl.TunnelPRLRequest(16)
	for i, r := range routers {
		e := b[(i+1)*xtcpnl.TunnelPRLSize:]
		a := r.Addr.As4()
		copy(e, a[:])
		binary.NativeEndian.PutUint16(e[4:6], r.Flags)
	}
	return fakeIoctl{out: b}
}

// tnl6RD is a SIOCGET6RD answer.
func tnl6RD(prefix string, plen uint16, relay string, rlen uint16) fakeIoctl {
	b := make([]byte, xtcpnl.Tunnel6RDSize)
	copy(b, tnlA4(prefix).AsSlice())
	copy(b[16:], tnlA4(relay).AsSlice())
	binary.NativeEndian.PutUint16(b[20:22], plen)
	binary.NativeEndian.PutUint16(b[22:24], rlen)
	return fakeIoctl{out: b}
}

// tnlKey is fakeTnlSys's ioctl key.
func tnlKey(family int, req uint32, name string) string {
	return fmt.Sprintf("%d/%x/%s", family, req, name)
}

type tnlUnitCase struct {
	description string
	args        string
	opts        string // j s d o
	links       [][]byte
	ioctls      map[string]fakeIoctl
	props       map[string]fakeProp
	fds         []tnlsys.TunFD
	// expected is "rc|stdout|a fragment of stderr".
	expected string
}

func runTnlUnit(t *testing.T, tc *tnlUnitCase) (string, *fakeTnlSys) {
	t.Helper()
	var out, errOut bytes.Buffer
	c := newRunCtx(nil, &out, &errOut)
	sys := &fakeTnlSys{ioctls: tc.ioctls, props: tc.props, fds: tc.fds}
	c.src, c.sys = &fakeLinkDump{bodies: tc.links}, sys
	c.json = strings.Contains(tc.opts, "j")
	c.oneline = strings.Contains(tc.opts, "o")
	if strings.Contains(tc.opts, "s") {
		c.showStats = 1
	}
	if strings.Contains(tc.opts, "d") {
		c.showDetails = 1
	}
	if strings.Contains(tc.opts, "6") {
		c.family = unix.AF_INET6
	}
	if strings.Contains(tc.opts, "f") {
		c.family = unix.AF_PACKET
	}
	args := strings.Fields(tc.args)
	run := runTunnel
	if args[0] == "tuntap" {
		run = runTuntap
	}
	err := run(c, args[1:])
	rc := 0
	if err != nil {
		rc = exitCodeOf(err)
		errOut.WriteString("goip: " + err.Error() + "\n")
	}
	return fmt.Sprintf("%d|%s|%s", rc, out.String(), errOut.String()), sys
}

func checkTnlUnit(t *testing.T, tests []tnlUnitCase) {
	t.Helper()
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			got, _ := runTnlUnit(t, tc)
			rc, rest, _ := strings.Cut(tc.expected, "|")
			stdout, frag, _ := strings.Cut(rest, "|")
			grc, grest, _ := strings.Cut(got, "|")
			gout, gerr, _ := strings.Cut(grest, "|")
			if grc != rc || gout != stdout || !strings.Contains(gerr, frag) {
				t.Errorf("got rc %s stdout %q stderr %q\nwant rc %s stdout %q stderr containing %q",
					grc, gout, gerr, rc, stdout, frag)
			}
			if frag == "" && gerr != "" {
				t.Errorf("stderr %q, want none", gerr)
			}
		})
	}
}

// TestGoipTunnelUnit drives ip tunnel's show paths over a fake link dump
// and a fake tnlsys.Sys.
//
// go test ./internal/goip/ -run TestGoipTunnelUnit
func TestGoipTunnelUnit(t *testing.T) {
	gre1 := tnlLink(5, unix.ARPHRD_IPGRE, "gre1", "", true)
	ipip1 := tnlLink(6, unix.ARPHRD_TUNNEL, "ipip1", "", false)
	isa1 := tnlLink(7, unix.ARPHRD_SIT, "isa1", "", false)
	d0 := tnlLink(2, unix.ARPHRD_ETHER, "d0", "dummy", false)
	gre1Out := map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "gre1"): tnlOut(tnlGre1)}
	sit1 := tnlIsa1
	sit1.Name, sit1.IFlags = "sit1", 0
	vti := tnlIpip1
	vti.Name, vti.IFlags, vti.Protocol = "vti1", xtcpnl.VtiIsVti, 4
	tos := tnlIpip1
	tos.TOS = 0x11
	isaIoctls := map[string]fakeIoctl{
		tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "isa1"): tnlOut(tnlIsa1),
		tnlKey(unix.AF_INET, tnlsys.SIOCGETPRL, "isa1"): tnlPRL(xtcpnl.TunnelPRL{Addr: tnlA4("192.0.2.11")},
			xtcpnl.TunnelPRL{Addr: tnlA4("192.0.2.10"), Flags: xtcpnl.PRLDefault}),
		tnlKey(unix.AF_INET, tnlsys.SIOCGET6RD, "isa1"): {err: unix.EINVAL},
	}
	checkTnlUnit(t, []tnlUnitCase{
		// Positive.
		{description: "positive: gre with key 5 and seq in, its flag line after the line",
			args: "tunnel show", links: [][]byte{d0, gre1}, ioctls: gre1Out,
			expected: "0|gre1: gre/ip remote 198.51.100.2 local 192.0.2.1 ttl 32 key 5\n  Drop packets out of sequence.\n|"},
		{description: "positive: -j ipip: remote and local as addresses, no ttl member for inherit (T7)",
			args: "tunnel", opts: "j", links: [][]byte{ipip1},
			ioctls:   map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "ipip1"): tnlOut(tnlIpip1)},
			expected: `0|[{"ifname":"ipip1","mode":"ip/ip","remote":"198.51.100.1","local":"0.0.0.0"}` + "\n]\n|"},
		{description: "positive: isatap routers, pr then pdr in the kernel's order; 6rd EINVAL is silent",
			args: "tunnel show", links: [][]byte{isa1}, ioctls: isaIoctls,
			expected: "0|isa1: ipv6/ip remote any local 192.0.2.1 pr 192.0.2.11 pdr 192.0.2.10 ttl inherit\n|"},
		{description: "positive: -j isatap: pr and pdr are members of one object (T7)",
			args: "tunnel show", opts: "j", links: [][]byte{isa1}, ioctls: isaIoctls,
			expected: `0|[{"ifname":"isa1","mode":"ipv6/ip","remote":"0.0.0.0","local":"192.0.2.1","pr":"192.0.2.11","pdr":"192.0.2.10"}` + "\n]\n|"},
		{description: "positive: sit with a 6rd prefix and relay",
			args: "tunnel show", links: [][]byte{tnlLink(8, unix.ARPHRD_SIT, "sit1", "", false)},
			ioctls: map[string]fakeIoctl{
				tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "sit1"): tnlOut(sit1),
				tnlKey(unix.AF_INET, tnlsys.SIOCGET6RD, "sit1"):    tnl6RD("2001:db8::", 32, "192.0.2.0", 24),
			},
			expected: "0|sit1: ipv6/ip remote any local 192.0.2.1 ttl inherit 6rd-prefix 2001:db8::/32 6rd-relay_prefix 192.0.2.0/24\n|"},
		{description: "positive: v6 named: text, no final newline (T9)",
			args: "tunnel show ip6t1", opts: "6",
			ioctls:   map[string]fakeIoctl{tnlKey(unix.AF_INET6, tnlsys.SIOCGETTUNNEL, "ip6t1"): {out: tnlIP6t1.Marshal()}},
			expected: "0|ip6t1: ipv6/ipv6 remote 2001:db8::2 local 2001:db8::1 encaplimit none hoplimit 1 tclass inherit flowlabel 0x12345 (flowinfo 0x00012345)|"},
		{description: "positive: tos inherit/ with the rest named in text",
			args: "tunnel show", links: [][]byte{ipip1},
			ioctls:   map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "ipip1"): tnlOut(tos)},
			expected: "0|ipip1: ip/ip remote 198.51.100.1 local any ttl inherit tos inherit/0x10\n|"},
		{description: "positive: -s: the stats block after the line",
			args: "tunnel show", opts: "s", links: [][]byte{gre1}, ioctls: gre1Out,
			expected: "0|gre1: gre/ip remote 198.51.100.2 local 192.0.2.1 ttl 32 key 5\n  Drop packets out of sequence.\n" +
				"RX: Packets    Bytes        Errors CsumErrs OutOfSeq Mcasts\n" +
				"    7          9            0      0        0        0       \n" +
				"TX: Packets    Bytes        Errors DeadLoop NoRoute  NoBufs\n" +
				"    0          0            0      0        0        0     \n|"},
		// Negative.
		{description: "negative: a refused ioctl in the list is a stderr line and a skip, rc 0 (T3)",
			args: "tunnel show", links: [][]byte{gre1, ipip1},
			ioctls: map[string]fakeIoctl{
				tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "gre1"):  {err: unix.ENODEV},
				tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "ipip1"): tnlOut(tnlIpip1),
			},
			expected: "0|ipip1: ip/ip remote 198.51.100.1 local any ttl inherit\n|get tunnel \"gre1\" failed: No such device\n"},
		{description: "negative: the named path's refusal is rc 1 (T5)",
			args:     "tunnel show gre9",
			ioctls:   map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "gre9"): {err: unix.ENODEV}},
			expected: "1||get tunnel \"gre9\" failed: No such device"},
		{description: "negative: keys without a gre or vti mode (T6)",
			args: "tunnel show key 5", expected: "1||Keys are not allowed with ipip and sit tunnels"},
		{description: "negative: an unknown mode exits -1",
			args: "tunnel show mode bogus", expected: "255||Unknown tunnel mode \"bogus\""},
		{description: "negative: two modes exit -1",
			args: "tunnel show mode gre mode sit", expected: "255||You managed to ask for more than one tunnel mode."},
		{description: "negative: add is a write goip does not make",
			args: "tunnel add name t1 mode gre", expected: "1||not implemented"},
		{description: "negative: a second name is duparg2",
			args: "tunnel show a b", expected: "255||either \"name\" is duplicate, or \"b\" is garbage"},
		{description: "negative: a broadcast tunnel needs a source",
			args: "tunnel show mode gre remote 239.1.1.1", expected: "1||A broadcast tunnel requires a source address"},
		{description: "negative: a replay gap is an error, not a kernel answer",
			args: "tunnel show", links: [][]byte{gre1}, expected: "2||not in the snapshot"},
		{description: "negative: a socket(2) failure is ip's create socket line",
			args:     "tunnel show gre1",
			ioctls:   map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "gre1"): {err: &tnlsys.SocketError{Err: unix.EAFNOSUPPORT}}},
			expected: "1||create socket failed: Address family not supported by protocol\n"},
		{description: "negative: a refused SIOCGETPRL prints tnl_gen_ioctl's line and no routers",
			args: "tunnel show", links: [][]byte{isa1},
			ioctls: map[string]fakeIoctl{
				tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "isa1"): tnlOut(tnlIsa1),
				tnlKey(unix.AF_INET, tnlsys.SIOCGETPRL, "isa1"):    {err: unix.EPERM},
				tnlKey(unix.AF_INET, tnlsys.SIOCGET6RD, "isa1"):    {err: unix.EOPNOTSUPP},
			},
			expected: "0|isa1: ipv6/ip remote any local 192.0.2.1 ttl inherit\n|isa1: ioctl 89f4 failed: Operation not permitted\nisa1: ioctl 89f8 failed: Operation not supported\n"},
		{description: "negative: an unknown verb exits -1",
			args: "tunnel bogus", expected: "255||Command \"bogus\" is unknown, try \"ip tunnel help\""},
		{description: "negative: -0 is no tunnel family",
			args: "tunnel show", opts: "f", expected: "255||Unsupported protocol family: 17"},
		// Boundary.
		{description: "boundary: no tunnels is nothing in text",
			args: "tunnel show", links: [][]byte{d0}, expected: "0||"},
		{description: "boundary: no tunnels is an empty array under -j",
			args: "tunnel show", opts: "j", expected: "0|[]\n|"},
		{description: "boundary: -s with no stats ends the dump after the line (T2)",
			args: "tunnel show", opts: "s", links: [][]byte{ipip1},
			ioctls:   map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "ipip1"): tnlOut(tnlIpip1)},
			expected: "1|ipip1: ip/ip remote 198.51.100.1 local any ttl inherit|Dump terminated"},
		{description: "boundary: ttl 300 is invarg with its own newline (T11)",
			args: "tunnel show ttl 300", expected: "255||invalid TTL\n\n"},
		{description: "boundary: a 21-bit flowlabel",
			args: "tunnel show flowlabel 0x100000", opts: "6", expected: "255||invalid Flowlabel"},
		{description: "boundary: help is usage",
			args: "tunnel help", expected: "255||Usage: ip tunnel { add | change | del | show | prl | 6rd } [ NAME ]"},
		// Corner.
		{description: "corner: mode any is the v6 code even under -4 (T10)",
			args: "tunnel show mode any", opts: "j", links: [][]byte{tnlLink(9, unix.ARPHRD_TUNNEL6, "ip6t1", "", false)},
			ioctls:   map[string]fakeIoctl{tnlKey(unix.AF_INET6, tnlsys.SIOCGETTUNNEL, "ip6t1"): {out: tnlIP6t1.Marshal()}},
			expected: `0|[{"ifname":"ip6t1","mode":"ipv6/ipv6","remote":"2001:db8::2","local":"2001:db8::1","ip6_tnl_f_ign_encap_limit":null,"hoplimit":1,"ip6_tnl_f_use_orig_tclass":null,"flowlabel":"0x12345","flowinfo":"0x00012345"}` + "\n]\n|"},
		{description: "corner: vti prints ip/ip (T7)",
			args: "tunnel show vti1", ioctls: map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "vti1"): tnlOut(vti)},
			expected: "0|vti1: ip/ip remote 198.51.100.1 local any ttl inherit\n|"},
		{description: "corner: the named path prints text under -j",
			args: "tunnel show gre1", opts: "j", ioctls: gre1Out,
			expected: "0|gre1: gre/ip remote 198.51.100.2 local 192.0.2.1 ttl 32 key 5\n  Drop packets out of sequence.\n|"},
		{description: "corner: -o puts the flag line after a backslash, the newline stays",
			args: "tunnel show", opts: "o", links: [][]byte{gre1}, ioctls: gre1Out,
			expected: "0|gre1: gre/ip remote 198.51.100.2 local 192.0.2.1 ttl 32 key 5\\  Drop packets out of sequence.\n|"},
		{description: "corner: -j -s puts the stats text inside the document (T1)",
			args: "tunnel show", opts: "js", links: [][]byte{gre1}, ioctls: gre1Out,
			expected: `0|[{"ifname":"gre1","mode":"gre/ip","remote":"198.51.100.2","local":"192.0.2.1","ttl":32,"key":5,"flags":["rx_drop_ooseq"]}` +
				"\nRX: Packets    Bytes        Errors CsumErrs OutOfSeq Mcasts\n" +
				"    7          9            0      0        0        0       \n" +
				"TX: Packets    Bytes        Errors DeadLoop NoRoute  NoBufs\n" +
				"    0          0            0      0        0        0     \n]\n|"},
	})
}

// TestGoipTunnelIoctlInputs holds goip to the bytes ip passes each ioctl.
//
// go test ./internal/goip/ -run TestGoipTunnelIoctlInputs
func TestGoipTunnelIoctlInputs(t *testing.T) {
	g6 := tnlIP6t1
	g6.Name, g6.Proto = "ip6g1", 47
	tests := []struct {
		description string
		tc          tnlUnitCase
		expected    string // one of the inputs, "family/req/name=hex"
	}{
		{"positive: the v4 list passes a zeroed struct", tnlUnitCase{args: "tunnel show",
			links:  [][]byte{tnlLink(5, unix.ARPHRD_IPGRE, "gre1", "", false)},
			ioctls: map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "gre1"): tnlOut(tnlGre1)}},
			"2/89f0/gre1=" + strings.Repeat("00", 52)},
		{"positive: the v6 list passes the index, ip6gre's protocol and p1's name", tnlUnitCase{args: "tunnel show remote 2001:db8::4",
			opts:   "6",
			links:  [][]byte{tnlLink(12, unix.ARPHRD_IP6GRE, "ip6g1", "", false)},
			ioctls: map[string]fakeIoctl{tnlKey(unix.AF_INET6, tnlsys.SIOCGETTUNNEL, "ip6g1"): {out: g6.Marshal()}}},
			"10/89f0/ip6g1=" + strings.Repeat("00", 16) + "0c0000002f" + strings.Repeat("00", 55)},
		{"positive: the named path passes the parsed struct: version 4, ihl 5, DF", tnlUnitCase{args: "tunnel show mode gre",
			ioctls: map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "gre0"): tnlOut(tnlGre1)}},
			"2/89f0/gre0=" + strings.Repeat("00", 32) + "4500000000004000002f" + strings.Repeat("00", 10)},
		{"boundary: the PRL request is 16 entries, the header's datalen 240", tnlUnitCase{args: "tunnel show isatap1",
			ioctls: map[string]fakeIoctl{
				tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "isatap1"): tnlOut(tnlIsa1),
				tnlKey(unix.AF_INET, tnlsys.SIOCGETPRL, "isa1"):       tnlPRL(),
				tnlKey(unix.AF_INET, tnlsys.SIOCGET6RD, "isa1"):       {err: unix.EINVAL},
			}},
			"2/89f4/isa1=" + strings.Repeat("00", 8) + "f0000000" + strings.Repeat("00", 244)},
		{"corner: an isatap name prefix sets protocol 41 and SIT_ISATAP in the named struct", tnlUnitCase{args: "tunnel show isatap1",
			ioctls: map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "isatap1"): {err: unix.ENODEV}}},
			"2/89f0/isatap1=" + hexName("isatap1") + "000000000100000000000000000000004500000000004000002900" + strings.Repeat("00", 9)},
		{"positive: iseq and ocsum set one side each", tnlUnitCase{args: "tunnel show mode gre iseq ocsum",
			ioctls: map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "gre0"): tnlOut(tnlGre1)}},
			"2/89f0/gre0=" + strings.Repeat("00", 20) + "10008000" + strings.Repeat("00", 8) + "4500000000004000002f" + strings.Repeat("00", 10)},
		{"corner: a tos with a slash keeps only the part before it", tnlUnitCase{args: "tunnel show mode ipip tos 0x10/0x20",
			ioctls: map[string]fakeIoctl{tnlKey(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "tunl0"): tnlOut(tnlIpip1)}},
			"2/89f0/tunl0=" + strings.Repeat("00", 32) + "4510000000004000000400" + strings.Repeat("00", 9)},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			_, sys := runTnlUnit(t, &tt.tc)
			if !slices.Contains(sys.inputs, tt.expected) {
				t.Errorf("inputs %q\nwant %q among them", sys.inputs, tt.expected)
			}
		})
	}
}

// hexName is a name[IFNAMSIZ] in hex.
func hexName(n string) string {
	b := make([]byte, 16)
	copy(b, n)
	return hex.EncodeToString(b)
}
