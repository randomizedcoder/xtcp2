package xtcpnl

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The tunnel ioctl structs against the 8C corpus
// (testdata/7_3_1/ws/tunnel, each row's tnlsys.out sidecar; goip 8P).

// tnlSidecarIoctl is one ioctl record of a tnlsys.out sidecar
// (internal/tnlsys IoctlRec), the fields these tests read.
type tnlSidecarIoctl struct {
	Family int    `json:"family"`
	Req    string `json:"req"`
	Name   string `json:"name"`
	In     string `json:"in"`
	Out    string `json:"out"`
	Errno  int    `json:"errno"`
}

func tnlSidecar(t testing.TB, dir string) []tnlSidecarIoctl {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "tnlsys.out"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Ioctls []tnlSidecarIoctl `json:"ioctls"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	return s.Ioctls
}

func tnlHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// tnlCorpusOut is the output of the first successful req on name in a
// corpus row of the tunnel space.
func tnlCorpusOut(t testing.TB, row, req, name string) []byte {
	t.Helper()
	for _, r := range tnlSidecar(t, filepath.Join("testdata", "7_3_1", "ws", "tunnel", row)) {
		if r.Req == req && r.Name == name && r.Errno == 0 {
			return tnlHex(t, r.Out)
		}
	}
	t.Fatalf("%s: no %s answer for %s", row, req, name)
	return nil
}

func tnlCorpusDirs(t testing.TB) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join("testdata", "7_3_1", "ws", "tunnel", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no ws/tunnel rows")
	}
	slices.Sort(dirs)
	return dirs
}

// TestParseTunnelParm decodes corpus answers and hand-made buffers.
//
// go test ./pkg/xtcpnl/ -run TestParseTunnelParm
func TestParseTunnelParm(t *testing.T) {
	a4 := netip.MustParseAddr
	// ec-003 is `ip tunnel show` over tnl4, ec-039 `-6 tunnel show` over
	// tnl6, ec-038 `tunnel show isa16`.
	tests := []struct {
		description string
		got         func() (string, error)
		expected    string
	}{
		{"positive: gre with key, seq and csum", func() (string, error) {
			p, err := ParseIPTunnelParm(tnlCorpusOut(t, "ec-003", "SIOCGETTUNNEL", "gre1"))
			return fmt.Sprintf("%s %#x %#x %d %d proto %d %s>%s ttl %d", p.Name, p.IFlags, p.OFlags, p.IKey, p.OKey,
				p.Protocol, p.Saddr, p.Daddr, p.TTL), err
		}, "gre1 0xb000 0xb000 5 5 proto 47 192.0.2.1>198.51.100.2 ttl 32"},
		{"positive: vti keys with VTI_ISVTI in i_flags", func() (string, error) {
			p, err := ParseIPTunnelParm(tnlCorpusOut(t, "ec-003", "SIOCGETTUNNEL", "vti1"))
			return fmt.Sprintf("%#x %#x %d %d isvti %t", p.IFlags, p.OFlags, p.IKey, p.OKey, p.IFlags&VtiIsVti != 0), err
		}, "0x2100 0x2000 7 8 isvti true"},
		{"positive: isatap made by ip tunnel add: proto 41 and SIT_ISATAP", func() (string, error) {
			p, err := ParseIPTunnelParm(tnlCorpusOut(t, "ec-003", "SIOCGETTUNNEL", "isa1"))
			return fmt.Sprintf("proto %d isatap %t", p.Protocol, p.IFlags&SitIsatap != 0), err
		}, "proto 41 isatap true"},
		{"positive: isatap PRL, header then two routers", func() (string, error) {
			prl, err := ParseTunnelPRL(tnlCorpusOut(t, "ec-003", "SIOCGETPRL", "isa1"))
			var s []string
			for _, e := range prl {
				if e.Addr != a4("0.0.0.0") {
					s = append(s, fmt.Sprintf("%s/%d", e.Addr, e.Flags))
				}
			}
			return fmt.Sprintf("%d entries, header datalen %d: %s", len(prl), prl[0].DataLen, strings.Join(s, " ")), err
		}, "16 entries, header datalen 32: 192.0.2.11/0 192.0.2.10/1"},
		{"positive: ip6tnl: encaplimit none is IGN_ENCAP_LIMIT (limit stays 4), the kernel CAP bits in flags", func() (string, error) {
			p, err := ParseIP6TnlParm2(tnlCorpusOut(t, "ec-039", "SIOCGETTUNNEL", "ip6t1"))
			return fmt.Sprintf("%s proto %d encap %d hop %d flowinfo %#x flags %#x %s>%s", p.Name, p.Proto, p.EncapLimit,
				p.HopLimit, p.FlowInfo, p.Flags, p.Laddr, p.Raddr), err
		}, "ip6t1 proto 41 encap 4 hop 1 flowinfo 0x12345 flags 0x30043 2001:db8::1>2001:db8::2"},
		{"positive: ip6gre keys", func() (string, error) {
			p, err := ParseIP6TnlParm2(tnlCorpusOut(t, "ec-039", "SIOCGETTUNNEL", "ip6g1"))
			return fmt.Sprintf("%#x %#x %d %d proto %d", p.IFlags, p.OFlags, p.IKey, p.OKey, p.Proto), err
		}, "0xb000 0xb000 9 9 proto 47"},
		{"positive: a 6rd struct", func() (string, error) {
			b := make([]byte, Tunnel6RDSize)
			copy(b, netip.MustParseAddr("2001:db8::").AsSlice())
			copy(b[16:], []byte{192, 0, 2, 0})
			b[20], b[22] = 32, 24 // host-endian u16s on a little-endian host
			r, err := ParseTunnel6RD(b)
			return fmt.Sprintf("%s/%d %s/%d", r.Prefix, r.PrefixLen, r.RelayPrefix, r.RelayPrefixLen), err
		}, "2001:db8::/32 192.0.2.0/24"},
		{"negative: ip_tunnel_parm one byte short", func() (string, error) {
			_, err := ParseIPTunnelParm(make([]byte, IPTunnelParmSize-1))
			return "", err
		}, "error"},
		{"negative: ip6_tnl_parm2 given an ip_tunnel_parm", func() (string, error) {
			_, err := ParseIP6TnlParm2(make([]byte, IPTunnelParmSize))
			return "", err
		}, "error"},
		{"negative: a PRL buffer of a partial entry", func() (string, error) {
			_, err := ParseTunnelPRL(make([]byte, TunnelPRLSize+1))
			return "", err
		}, "error"},
		{"negative: a 6rd struct one byte long", func() (string, error) {
			_, err := ParseTunnel6RD(make([]byte, Tunnel6RDSize+1))
			return "", err
		}, "error"},
		{"boundary: all-zero parm: no name, any/any, ttl 0 (inherit)", func() (string, error) {
			p, err := ParseIPTunnelParm(make([]byte, IPTunnelParmSize))
			return fmt.Sprintf("%q %t proto %d %s %s ttl %d", p.Name, p.Terminated, p.Protocol, p.Saddr, p.Daddr, p.TTL), err
		}, `"" true proto 0 0.0.0.0 0.0.0.0 ttl 0`},
		{"boundary: a name of all 16 bytes has no NUL", func() (string, error) {
			b := make([]byte, IPTunnelParmSize)
			copy(b, "abcdefghijklmnop")
			p, err := ParseIPTunnelParm(b)
			return fmt.Sprintf("%q %t", p.Name, p.Terminated), err
		}, `"abcdefghijklmnop" false`},
		{"boundary: an empty PRL buffer", func() (string, error) {
			prl, err := ParseTunnelPRL(nil)
			return fmt.Sprint(len(prl)), err
		}, "0"},
		{"corner: 16 routers in a 16-entry buffer: the header and 15 (T8)", func() (string, error) {
			prl, err := ParseTunnelPRL(tnlCorpusOut(t, "ec-038", "SIOCGETPRL", "isa16"))
			n := 0
			for _, e := range prl[1:] {
				if e.Addr.IsValid() && e.Addr != a4("0.0.0.0") {
					n++
				}
			}
			return fmt.Sprintf("%d entries, %d routers, first %s last %s", len(prl), n, prl[1].Addr, prl[15].Addr), err
		}, "16 entries, 15 routers, first 192.0.2.25 last 192.0.2.11"},
		{"corner: ip6gre's 76 bytes through the v4 path decode as garbage (T19)", func() (string, error) {
			p, err := ParseIPTunnelParm(tnlCorpusOut(t, "ec-065", "SIOCGETTUNNEL", "gre6x"))
			return fmt.Sprintf("%s proto %d %s>%s", p.Name, p.Protocol, p.Saddr, p.Daddr), err
		}, "gre6x proto 0 0.0.0.1>32.1.13.184"},
		{"corner: a negative link", func() (string, error) {
			b := make([]byte, IPTunnelParmSize)
			copy(b[16:20], []byte{0xff, 0xff, 0xff, 0xff})
			p, err := ParseIPTunnelParm(b)
			return fmt.Sprint(p.Link), err
		}, "-1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.got()
			if tc.expected == "error" {
				if !errors.Is(err, ErrTunnelStructLen) {
					t.Errorf("err %v, want ErrTunnelStructLen", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestParseSysfsLong pins read_prop's parse: fgets, the newline cut, then
// strtol base 0 over the whole line.
//
// go test ./pkg/xtcpnl/ -run TestParseSysfsLong
func TestParseSysfsLong(t *testing.T) {
	tests := []struct {
		description string
		content     string
		expected    string // value, or the error
	}{
		{"positive: tun_flags in hex", "0x1002\n", "4098"},
		{"positive: owner -1", "-1\n", "-1"},
		{"positive: group 100", "100\n", "100"},
		{"positive: no newline", "7", "7"},
		{"negative: empty", "", "not a number"},
		{"negative: a newline only", "\n", "not a number"},
		{"negative: a word", "abc\n", "not a number"},
		{"negative: trailing space", "5 \n", "not a number"},
		{"negative: 0x with no digits", "0x\n", "not a number"},
		{"negative: 08 is a bad octal", "08\n", "not a number"},
		{"boundary: LONG_MAX", "9223372036854775807\n", "9223372036854775807"},
		{"boundary: LONG_MIN", "-9223372036854775808\n", "-9223372036854775808"},
		{"boundary: LONG_MAX + 1 saturates", "9223372036854775808\n", "out of range"},
		{"boundary: LONG_MIN - 1 saturates", "-9223372036854775809\n", "out of range"},
		{"corner: octal", "010\n", "8"},
		{"corner: leading space and a sign", " \t+0X1f\n", "31"},
		{"corner: only the first line", "3\n4\n", "3"},
		{"corner: a NUL ends the string", "12\x0034\n", "12"},
		{"corner: fgets keeps 79 bytes", strings.Repeat("0", 79) + "x\n", "0"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, err := ParseSysfsLong(tc.content)
			got := fmt.Sprint(v)
			switch {
			case errors.Is(err, ErrSysfsNotNumber):
				got = "not a number"
			case errors.Is(err, ErrSysfsRange):
				got = "out of range"
			case err != nil:
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("ParseSysfsLong(%q) = %s, want %s", tc.content, got, tc.expected)
			}
		})
	}
}

// tnlLinks is every RTM_NEWLINK reply in a row's ip.pcap, by name.
func tnlLinks(t *testing.T, dir string) map[string]LinkInfo {
	t.Helper()
	links := map[string]LinkInfo{}
	for _, m := range pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")) {
		if m.h.Flags&unix.NLM_F_REQUEST != 0 || m.h.Type != unix.RTM_NEWLINK {
			continue
		}
		li, err := ParseNewLink(m.body)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		links[li.Name] = li
	}
	return links
}

// tnlAgree compares a SIOCGETTUNNEL answer with the same tunnel's
// IFLA_INFO_DATA: the fields both carry. ok is false for a kind with no
// ioctl struct of that size (the T19 case: an ip6gre asked through the v4
// path).
func tnlAgree(li LinkInfo, out []byte) (diffs []string, ok bool, err error) {
	d := func(field string, a, b any) {
		if a != b {
			diffs = append(diffs, fmt.Sprintf("%s: ioctl %v, netlink %v", field, a, b))
		}
	}
	switch len(out) {
	case IPTunnelParmSize:
		p, err := ParseIPTunnelParm(out)
		if err != nil {
			return nil, false, err
		}
		switch {
		case li.Iptun != nil && li.Kind != ip6tnlKindCst:
			n := li.Iptun
			d("saddr", p.Saddr, n.Local)
			d("daddr", p.Daddr, n.Remote)
			d("ttl", p.TTL, n.TTL)
			d("tos", p.TOS, n.TOS)
			d("protocol", p.Protocol, n.Proto)
			d("isatap", p.IFlags&SitIsatap != 0, n.Flags&IptunFlagSitIsatap != 0)
		case li.Gre != nil && li.Kind == greKindCst:
			n := li.Gre
			d("saddr", p.Saddr, n.Local)
			d("daddr", p.Daddr, n.Remote)
			d("ttl", p.TTL, n.TTL)
			d("tos", p.TOS, n.TOS)
			d("i_flags", p.IFlags, n.IFlags)
			d("o_flags", p.OFlags, n.OFlags)
			d("i_key", p.IKey, n.IKey)
			d("o_key", p.OKey, n.OKey)
		case li.Vti != nil && li.Kind == vtiKindCst:
			n := li.Vti
			d("saddr", p.Saddr, n.Local)
			d("daddr", p.Daddr, n.Remote)
			d("i_key", p.IKey, n.IKey)
			d("o_key", p.OKey, n.OKey)
		default:
			return nil, false, nil
		}
	case IP6TnlParm2Size:
		p, err := ParseIP6TnlParm2(out)
		if err != nil {
			return nil, false, err
		}
		switch {
		case li.Iptun != nil && li.Kind == ip6tnlKindCst:
			n := li.Iptun
			d("laddr", p.Laddr, n.Local)
			d("raddr", p.Raddr, n.Remote)
			d("hop_limit", p.HopLimit, n.TTL)
			d("encap_limit", p.EncapLimit, n.EncapLimit)
			d("flowinfo", p.FlowInfo, n.FlowInfo)
			d("flags", p.Flags, n.Flags)
			d("proto", p.Proto, n.Proto)
		case li.Gre != nil && li.Kind == ip6greKindCst:
			n := li.Gre
			d("laddr", p.Laddr, n.Local)
			d("raddr", p.Raddr, n.Remote)
			d("hop_limit", p.HopLimit, n.TTL)
			d("encap_limit", p.EncapLimit, n.EncapLimit)
			d("flowinfo", p.FlowInfo, n.FlowInfo)
			d("i_flags", p.IFlags, n.IFlags)
			d("o_flags", p.OFlags, n.OFlags)
			d("i_key", p.IKey, n.IKey)
			d("o_key", p.OKey, n.OKey)
		case li.Vti != nil && li.Kind == vti6KindCst:
			n := li.Vti
			d("laddr", p.Laddr, n.Local)
			d("raddr", p.Raddr, n.Remote)
			d("i_key", p.IKey, n.IKey)
			d("o_key", p.OKey, n.OKey)
		default:
			return nil, false, nil
		}
	default:
		return nil, false, nil
	}
	return diffs, true, nil
}

// TestWsTunnelParmAgreesWithLinkDump is the O-check: every SIOCGETTUNNEL
// answer in the corpus decodes, and where the same row's link dump holds
// the tunnel it names, the two agree on every field both carry.
//
// go test ./pkg/xtcpnl/ -run TestWsTunnelParmAgreesWithLinkDump
func TestWsTunnelParmAgreesWithLinkDump(t *testing.T) {
	compared, decoded, prl, sixrd := 0, 0, 0, 0
	kinds := map[string]bool{}
	for _, dir := range tnlCorpusDirs(t) {
		links := tnlLinks(t, dir)
		for _, r := range tnlSidecar(t, dir) {
			if r.Errno != 0 {
				continue
			}
			out := tnlHex(t, r.Out)
			switch r.Req {
			case "SIOCGETPRL":
				if _, err := ParseTunnelPRL(out); err != nil {
					t.Errorf("%s %s: %v", dir, r.Name, err)
				}
				prl++
				continue
			case "SIOCGET6RD":
				if _, err := ParseTunnel6RD(out); err != nil {
					t.Errorf("%s %s: %v", dir, r.Name, err)
				}
				sixrd++
				continue
			}
			var name TunnelName
			switch len(out) {
			case IPTunnelParmSize:
				p, err := ParseIPTunnelParm(out)
				if err != nil {
					t.Fatalf("%s %s: %v", dir, r.Name, err)
				}
				name = p.TunnelName
			case IP6TnlParm2Size:
				p, err := ParseIP6TnlParm2(out)
				if err != nil {
					t.Fatalf("%s %s: %v", dir, r.Name, err)
				}
				name = p.TunnelName
			default:
				t.Fatalf("%s %s: a %d-byte answer", dir, r.Name, len(out))
			}
			decoded++
			if !name.Terminated {
				t.Errorf("%s %s: the kernel's name has no NUL", dir, r.Name)
			}
			li, ok := links[name.Name]
			if !ok {
				continue
			}
			diffs, ok, err := tnlAgree(li, out)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				continue
			}
			compared++
			kinds[li.Kind] = true
			for _, x := range diffs {
				t.Errorf("%s: %s (%s): %s", dir, name.Name, li.Kind, x)
			}
		}
	}
	t.Logf("%d answers decoded, %d compared with the link dump; %d PRL, %d 6rd", decoded, compared, prl, sixrd)
	for _, k := range []string{ipipKindCst, sitKindCst, greKindCst, vtiKindCst, ip6tnlKindCst, ip6greKindCst, vti6KindCst} {
		if !kinds[k] {
			t.Errorf("no %s answer compared", k)
		}
	}
}

// FuzzParseIPTunnelParm: any input decodes or is refused for its length,
// and a decoded name is at most IFNAMSIZ bytes.
func FuzzParseIPTunnelParm(f *testing.F) {
	for _, dir := range tnlCorpusDirs(f) {
		for _, r := range tnlSidecar(f, dir) {
			if r.Errno == 0 && r.Req == "SIOCGETTUNNEL" {
				f.Add(tnlHex(f, r.Out))
			}
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := ParseIPTunnelParm(b)
		if (err == nil) != (len(b) == IPTunnelParmSize) {
			t.Fatalf("len %d: err %v", len(b), err)
		}
		if len(p.Name) > tnlNameSizeCst || p.Terminated && strings.IndexByte(p.Name, 0) >= 0 {
			t.Fatalf("name %q", p.Name)
		}
	})
}

// FuzzParseIP6TnlParm2: as FuzzParseIPTunnelParm, for the v6 struct.
func FuzzParseIP6TnlParm2(f *testing.F) {
	for _, dir := range tnlCorpusDirs(f) {
		for _, r := range tnlSidecar(f, dir) {
			if r.Errno == 0 && r.Req == "SIOCGETTUNNEL" && r.Family == unix.AF_INET6 {
				f.Add(tnlHex(f, r.Out))
			}
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := ParseIP6TnlParm2(b)
		if (err == nil) != (len(b) == IP6TnlParm2Size) {
			t.Fatalf("len %d: err %v", len(b), err)
		}
		if err == nil && (!p.Laddr.Is6() || !p.Raddr.Is6()) {
			t.Fatalf("addresses %v %v", p.Laddr, p.Raddr)
		}
	})
}

// FuzzParseSysfsLong: whatever the content, a value or one of the two
// errors, and the value of a plain decimal round-trips.
func FuzzParseSysfsLong(f *testing.F) {
	for _, s := range []string{"0x1002\n", "-1\n", "100\n", "", "0x", "010", " +7\n", "9223372036854775808"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := ParseSysfsLong(s)
		if err != nil && !errors.Is(err, ErrSysfsNotNumber) && !errors.Is(err, ErrSysfsRange) {
			t.Fatalf("%q: %v", s, err)
		}
		if err == nil && s == fmt.Sprint(v) && v != 0 && s[0] == '0' {
			t.Fatalf("%q read as decimal %d", s, v)
		}
	})
}

func BenchmarkParseIPTunnelParm(b *testing.B) {
	buf := tnlCorpusOut(b, "ec-003", "SIOCGETTUNNEL", "gre1")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseIPTunnelParm(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseIP6TnlParm2(b *testing.B) {
	buf := tnlCorpusOut(b, "ec-039", "SIOCGETTUNNEL", "ip6t1")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseIP6TnlParm2(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseTunnelPRL(b *testing.B) {
	buf := tnlCorpusOut(b, "ec-038", "SIOCGETPRL", "isa16")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseTunnelPRL(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseSysfsLong(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseSysfsLong("0x5101\n"); err != nil {
			b.Fatal(err)
		}
	}
}

// TestTunnelParmMarshal builds the ioctl inputs goip passes: every input
// `ip` passed in the corpus rebuilds from its parse, and hand-made structs
// land at the C offsets.
//
// go test ./pkg/xtcpnl/ -run TestTunnelParmMarshal
func TestTunnelParmMarshal(t *testing.T) {
	inputs := map[string][][]byte{}
	for _, dir := range tnlCorpusDirs(t) {
		for _, r := range tnlSidecar(t, dir) {
			k := fmt.Sprintf("%s/%d", r.Req, r.Family)
			inputs[k] = append(inputs[k], tnlHex(t, r.In))
		}
	}
	roundTrip := func(k string, parse func([]byte) ([]byte, error)) (string, error) {
		n := 0
		for _, in := range inputs[k] {
			out, err := parse(in)
			if err != nil {
				return "", err
			}
			if !slices.Equal(out, in) {
				return "", fmt.Errorf("%x rebuilt as %x", in, out)
			}
			n++
		}
		return fmt.Sprintf("%t", n > 0), nil
	}
	tests := []struct {
		description string
		got         func() (string, error)
		expected    string
	}{
		{"positive: every v4 SIOCGETTUNNEL input in the corpus rebuilds", func() (string, error) {
			return roundTrip("SIOCGETTUNNEL/2", func(b []byte) ([]byte, error) {
				p, err := ParseIPTunnelParm(b)
				return p.Marshal(), err
			})
		}, "true"},
		{"positive: every v6 SIOCGETTUNNEL input in the corpus rebuilds", func() (string, error) {
			return roundTrip("SIOCGETTUNNEL/10", func(b []byte) ([]byte, error) {
				p, err := ParseIP6TnlParm2(b)
				return p.Marshal(), err
			})
		}, "true"},
		{"positive: every SIOCGETPRL input is the 16-entry request", func() (string, error) {
			return roundTrip("SIOCGETPRL/2", func([]byte) ([]byte, error) { return TunnelPRLRequest(16), nil })
		}, "true"},
		{"positive: v4 fields at the C offsets", func() (string, error) {
			p := IPTunnelParm{TunnelName: TunnelName{Name: "gre1"}, Link: 3, IFlags: GreFlagKey, OFlags: GreFlagSeq,
				IKey: 5, OKey: 6, VersionIHL: 0x45, TOS: 0x10, FragOff: 0x4000, TTL: 64, Protocol: 47,
				Saddr: netip.MustParseAddr("192.0.2.1"), Daddr: netip.MustParseAddr("198.51.100.2")}
			b := p.Marshal()
			return fmt.Sprintf("%d %x %x %x %x %x", len(b), b[:4], b[20:32], b[32:34], b[38:42], b[44:52]), nil
		}, "52 67726531 200010000000000500000006 4510 4000402f c0000201c6336402"},
		{"negative: a v4 address in an ip6 struct is left zero", func() (string, error) {
			p := IP6TnlParm2{Laddr: netip.MustParseAddr("192.0.2.1")}
			return fmt.Sprintf("%x", p.Marshal()[32:48]), nil
		}, "00000000000000000000000000000000"},
		{"boundary: the zero struct is all zero bytes", func() (string, error) {
			var p IPTunnelParm
			var q IP6TnlParm2
			return fmt.Sprintf("%t %t", slices.Equal(p.Marshal(), make([]byte, IPTunnelParmSize)),
				slices.Equal(q.Marshal(), make([]byte, IP6TnlParm2Size))), nil
		}, "true true"},
		{"boundary: a PRL request of one entry has datalen 0; none is nil", func() (string, error) {
			return fmt.Sprintf("%x %t", TunnelPRLRequest(1), TunnelPRLRequest(0) == nil), nil
		}, "00000000000000000000000000000000 true"},
		{"corner: a 16-byte name fills name[] with no NUL, as strncpy does", func() (string, error) {
			p := IP6TnlParm2{TunnelName: TunnelName{Name: "abcdefghijklmnopq"}, Link: -1}
			b := p.Marshal()
			return fmt.Sprintf("%s %x", b[:16], b[16:20]), nil
		}, "abcdefghijklmnop ffffffff"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.got()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestSysfsPropLine is the buf read_prop quotes in its messages.
//
// go test ./pkg/xtcpnl/ -run TestSysfsPropLine
func TestSysfsPropLine(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    string // line|ok
	}{
		{"positive: a value and its newline", "0x1002\n", "0x1002|true"},
		{"negative: an empty file is fgets' NULL", "", "|false"},
		{"boundary: 79 bytes kept", strings.Repeat("7", 90), strings.Repeat("7", 79) + "|true"},
		{"boundary: a bare newline is an empty line", "\n", "|true"},
		{"corner: only the first line", "1\n2\n", "1|true"},
		{"corner: a NUL ends the string", "1\x002\n", "1|true"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			line, ok := SysfsPropLine(tc.in)
			if got := fmt.Sprintf("%s|%t", line, ok); got != tc.expected {
				t.Errorf("SysfsPropLine(%q) = %q, want %q", tc.in, got, tc.expected)
			}
		})
	}
}
