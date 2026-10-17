package xtcpnl

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A1-T01 (docs/netlink/addr-write-path.md, table 6): ParseNewAddr against
// the readback states the link-write capture commits, and against the
// RTM_GETMULTICAST and RTM_GETANYCAST dumps in addr-mcdump/mc_any, which are
// the only replies that carry IFA_MULTICAST and IFA_ANYCAST.
//
// The oracle for a readback state is the `ip` sidecar captured by the same
// invocation as the pcap: getaddr_json.pcap against ip_addr_json, and
// getaddr.pcap against ip_addr. Plain and -j are two `ip` runs a second apart,
// so a lifetime that counts down differs between them by design.
//
// The oracle for a multicast or anycast dump is the /proc file the kernel
// renders from the same lists with no netlink in between: igmp, igmp6 and
// anycast6.

const addrMcdumpDir = "addr-mcdump/mc_any"

// addrJSONFlags is ifa_flag_data (ip/ipaddress.c:1380-1398) in its order,
// for print_ifa_flags (:1411-1447): the JSON key each bit prints as.
// PERMANENT prints "dynamic" when it is CLEAR, and SECONDARY prints
// "temporary" on IPv6.
var addrJSONFlags = []struct {
	bit  uint32
	name string
}{
	{bit: unix.IFA_F_SECONDARY, name: "secondary"},
	{bit: unix.IFA_F_NODAD, name: "nodad"},
	{bit: unix.IFA_F_OPTIMISTIC, name: "optimistic"},
	{bit: unix.IFA_F_DADFAILED, name: "dadfailed"},
	{bit: unix.IFA_F_HOMEADDRESS, name: "home"},
	{bit: unix.IFA_F_DEPRECATED, name: "deprecated"},
	{bit: unix.IFA_F_TENTATIVE, name: "tentative"},
	{bit: unix.IFA_F_PERMANENT, name: "dynamic"},
	{bit: unix.IFA_F_MANAGETEMPADDR, name: "mngtmpaddr"},
	{bit: unix.IFA_F_NOPREFIXROUTE, name: "noprefixroute"},
	{bit: unix.IFA_F_MCAUTOJOIN, name: "autojoin"},
	{bit: unix.IFA_F_STABLE_PRIVACY, name: "stable-privacy"},
}

// addrJSONScopes is rtnl_rtscope_n2a (lib/rt_names.c) without
// /etc/iproute2/rt_scopes, which the capture guest does not have.
var addrJSONScopes = map[uint8]string{0: "global", 200: "site", 253: "link", 254: "host", 255: "nowhere"}

// addrJSONProtos is rtnl_addrprot_n2a's built-in names (lib/rt_names.c);
// any other value prints as %#x.
var addrJSONProtos = map[uint8]string{
	IfaProtoUnspec: "unspec", IfaProtoKernelLo: "kernel_lo",
	IfaProtoKernelRA: "kernel_ra", IfaProtoKernelLL: "kernel_ll",
}

// addrJSONView is the addr_info object print_addrinfo (ip/ipaddress.c:1590-1715)
// would print for ai, as strings, built only from the decoded fields. Every
// key the sidecar has must be here: a key this cannot produce is an
// attribute the decoder does not carry.
func addrJSONView(ai AddrInfo) map[string]string {
	v := map[string]string{}
	switch ai.Family {
	case unix.AF_INET:
		v["family"] = "inet"
	case unix.AF_INET6:
		v["family"] = "inet6"
	default:
		v["family_index"] = strconv.Itoa(int(ai.Family))
	}
	if ai.Local != nil {
		v["local"] = addrString(ai.Local)
		if !bytes.Equal(ai.Local, ai.Address) {
			v["address"] = addrString(ai.Address)
		}
	}
	v["prefixlen"] = strconv.Itoa(int(ai.Prefixlen))
	if ai.HasRtPriority {
		v["metric"] = strconv.FormatUint(uint64(ai.RtPriority), 10)
	}
	if ai.Broadcast != nil {
		v["broadcast"] = addrString(ai.Broadcast)
	}
	if ai.Anycast != nil {
		v["anycast"] = addrString(ai.Anycast)
	}
	if ai.Multicast != nil {
		v["multicast"] = addrString(ai.Multicast)
	}
	if name, ok := addrJSONScopes[ai.Scope]; ok {
		v["scope"] = name
	} else {
		v["scope"] = strconv.Itoa(int(ai.Scope))
	}
	rest := ai.Flags
	for _, f := range addrJSONFlags {
		switch {
		case f.bit == unix.IFA_F_PERMANENT:
			if rest&f.bit == 0 {
				v[f.name] = "true"
			}
		case rest&f.bit == 0:
		case f.bit == unix.IFA_F_SECONDARY && ai.Family == unix.AF_INET6:
			v["temporary"] = "true"
		default:
			v[f.name] = "true"
		}
		rest &^= f.bit
	}
	if rest != 0 {
		v["ifa_flags"] = fmt.Sprintf("%02x", rest)
	}
	if ai.HasProto {
		if name, ok := addrJSONProtos[ai.Proto]; ok {
			v["protocol"] = name
		} else {
			v["protocol"] = fmt.Sprintf("%#x", ai.Proto) // lib/rt_names.c:335
		}
	}
	if ai.Label != "" {
		v["label"] = ai.Label
	}
	if ai.HasCacheInfo {
		v["valid_life_time"] = strconv.FormatUint(uint64(ai.CacheInfo.Valid), 10)
		v["preferred_life_time"] = strconv.FormatUint(uint64(ai.CacheInfo.Preferred), 10)
	}
	return v
}

func addrString(b []byte) string {
	a, ok := netip.AddrFromSlice(b)
	if !ok {
		return hex.EncodeToString(b)
	}
	return a.String()
}

// sidecarAddrInfo reads an `ip -j addr show` sidecar into ifindex -> its
// addr_info objects, each value rendered as the string the view uses.
func sidecarAddrInfo(t *testing.T, parts ...string) map[uint32][]map[string]string {
	t.Helper()
	var links []struct {
		Ifindex  uint32           `json:"ifindex"`
		AddrInfo []map[string]any `json:"addr_info"`
	}
	dec := json.NewDecoder(bytes.NewReader(readFixture(t, parts...)))
	dec.UseNumber()
	if err := dec.Decode(&links); err != nil {
		t.Fatalf("%v: %v", parts, err)
	}
	out := map[uint32][]map[string]string{}
	for _, l := range links {
		for _, obj := range l.AddrInfo {
			m := map[string]string{}
			for k, val := range obj {
				m[k] = fmt.Sprint(val)
			}
			out[l.Ifindex] = append(out[l.Ifindex], m)
		}
	}
	return out
}

// decodeAddrReplies decodes every reply of one type in a dump-set pcap.
func decodeAddrReplies(t *testing.T, path string, msgType uint16) []AddrInfo {
	t.Helper()
	bodies, sawDone := readDumpSetReplies(t, path, msgType)
	if !sawDone {
		t.Fatalf("%s: no NLMSG_DONE", path)
	}
	out := make([]AddrInfo, 0, len(bodies))
	for _, b := range bodies {
		ai, err := ParseNewAddr(b)
		if err != nil {
			t.Fatalf("%s: ParseNewAddr: %v", path, err)
		}
		out = append(out, ai)
	}
	return out
}

// addrReply is the first decoded reply in a readback pcap for one family and
// index.
func addrReply(t *testing.T, path string, family uint8, index uint32) AddrInfo {
	t.Helper()
	ai, err := ParseNewAddr(addrReplyBody(t, path, family, index))
	if err != nil {
		t.Fatal(err)
	}
	return ai
}

// go test ./pkg/xtcpnl/ -run TestParseNewAddrRichFixtures
func TestParseNewAddrRichFixtures(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		for _, state := range slices.Sorted(maps.Keys(addrStateSelectors)) {
			t.Run("positive: "+state+"'s decoded fields equal its -j sidecar", func(t *testing.T) {
				pcap := addrFixture("addr-show", state, "netlink_route_getaddr_json.pcap")
				decoded := map[uint32][]map[string]string{}
				replies := decodeAddrReplies(t, pcap, unix.RTM_NEWADDR)
				for i := range replies {
					decoded[replies[i].Index] = append(decoded[replies[i].Index], addrJSONView(replies[i]))
				}
				expected := sidecarAddrInfo(t, "addr-show", state, "ip_addr_json")
				if len(decoded) == 0 {
					t.Fatal("no addresses decoded")
				}
				for idx, want := range expected {
					got := decoded[idx]
					if len(got) != len(want) {
						t.Fatalf("ifindex %d: %d addresses decoded, the sidecar has %d", idx, len(got), len(want))
					}
					for i := range want {
						if !maps.Equal(got[i], want[i]) {
							t.Errorf("ifindex %d address %d:\n got  %v\n want %v", idx, i, got[i], want[i])
						}
					}
				}
				for idx := range decoded {
					if _, ok := expected[idx]; !ok {
						t.Errorf("ifindex %d decoded but absent from the sidecar", idx)
					}
				}
			})
		}
	})

	showPcap := func(state string) string {
		return addrFixture("addr-show", state, "netlink_route_getaddr.pcap")
	}
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{
			description: "positive: metric decoded, and the plain sidecar renders it",
			got: func(t *testing.T) string {
				ai := addrReply(t, showPcap("v4_metric"), unix.AF_INET, 3)
				line := fixtureLine(t, "inet 192.0.2.1/24", "addr-show", "v4_metric", "ip_addr")
				return fmt.Sprintf("%v %d %q", ai.HasRtPriority, ai.RtPriority, line)
			},
			expected: `true 50 "inet 192.0.2.1/24 metric 50 scope global d0"`,
		},
		{
			description: "positive: v6 peer decoded, local and address differ",
			got: func(t *testing.T) string {
				ai := addrReply(t, showPcap("v6_peer"), unix.AF_INET6, 3)
				return addrString(ai.Local) + " " + addrString(ai.Address)
			},
			expected: "2001:db8::1 2001:db8::2",
		},
		{
			description: "boundary: metric absent from the build_clean dump, and from its sidecar",
			got: func(t *testing.T) string {
				ai := addrReply(t, tdDumpGetAddrV4_7_1_4, unix.AF_INET, 3)
				side, err := os.ReadFile(tdDumps_7_1_4 + "/ip_addr_v4")
				if err != nil {
					t.Fatal(err)
				}
				return fmt.Sprintf("%v %v", ai.HasRtPriority, bytes.Contains(side, []byte("metric")))
			},
			expected: "false false",
		},
		{
			description: "boundary: infinite lifetimes in build_clean, rendered forever",
			got: func(t *testing.T) string {
				ai := addrReply(t, tdDumpGetAddrV4_7_1_4, unix.AF_INET, 3)
				return fmt.Sprintf("%#x %#x %v", ai.CacheInfo.Valid, ai.CacheInfo.Preferred, ai.IsPermanent())
			},
			expected: "0xffffffff 0xffffffff true",
		},
		{
			description: "boundary: finite lifetimes, the plain pcap against its own sidecar",
			got: func(t *testing.T) string {
				ai := addrReply(t, showPcap("v4_lifetimes"), unix.AF_INET, 3)
				want := fmt.Sprintf("valid_lft %dsec preferred_lft %dsec", ai.CacheInfo.Valid, ai.CacheInfo.Preferred)
				return fmt.Sprintf("%d %d %q", ai.CacheInfo.Valid, ai.CacheInfo.Preferred,
					fixtureLine(t, want, "addr-show", "v4_lifetimes", "ip_addr"))
			},
			expected: `3600 1800 "valid_lft 3600sec preferred_lft 1800sec"`,
		},
		{
			description: "boundary: finite lifetimes, the -j pcap a second later",
			got: func(t *testing.T) string {
				ai := addrReply(t, addrFixture("addr-show", "v4_lifetimes", "netlink_route_getaddr_json.pcap"), unix.AF_INET, 3)
				return fmt.Sprintf("%d %d", ai.CacheInfo.Valid, ai.CacheInfo.Preferred)
			},
			expected: "3599 1799",
		},
		{
			description: "corner: IPv6 with no IFA_LOCAL aliases Local from Address",
			got: func(t *testing.T) string {
				ai := addrReply(t, showPcap("v6_flags"), unix.AF_INET6, 3)
				return fmt.Sprintf("%v %s", bytes.Equal(ai.Local, ai.Address), addrString(ai.Local))
			},
			expected: "true 2001:db8:3::1",
		},
		{
			description: "corner: IPv4 deprecated sets the bit and reads as deprecated",
			got: func(t *testing.T) string {
				ai := addrReply(t, showPcap("v4_deprecated"), unix.AF_INET, 3)
				return fmt.Sprintf("%v %v", ai.Flags&unix.IFA_F_DEPRECATED != 0, ai.IsDeprecated())
			},
			expected: "true true",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %s, expected %s", got, tc.expected)
			}
		})
	}
}

// go test ./pkg/xtcpnl/ -run TestParseNewAddrMalformed
//
// Hand bytes, which the conventions allow only for malformed input: the
// kernel's policy never emits a short u32 (net/ipv4/devinet.c:106,
// net/ipv6/addrconf.c:4804), so no capture can carry one.
func TestParseNewAddrMalformed(t *testing.T) {
	hdr := ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 3)
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "negative: a 2-byte IFA_RT_PRIORITY is ignored, not an error",
			body:        concat(hdr, rtattr(unix.IFA_RT_PRIORITY, []byte{50, 0})),
			expected:    "err=<nil> rt=false/0 mc=false/0",
		},
		{
			description: "negative: a 3-byte IFA_MC_USERS is ignored, not an error",
			body:        concat(hdr, rtattr(IfaMcUsers, []byte{2, 0, 0})),
			expected:    "err=<nil> rt=false/0 mc=false/0",
		},
		{
			description: "negative: a 7-byte ifaddrmsg is too short",
			body:        make([]byte, IfAddrmsgSizeCst-1),
			expected:    "err=" + ErrIfAddrmsgSmall.Error() + " rt=false/0 mc=false/0",
		},
		{
			description: "boundary: an 8-byte IFA_RT_PRIORITY decodes its first four",
			body:        concat(hdr, rtattr(unix.IFA_RT_PRIORITY, []byte{50, 0, 0, 0, 9, 9, 9, 9})),
			expected:    "err=<nil> rt=true/50 mc=false/0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ai, err := ParseNewAddr(tc.body)
			got := fmt.Sprintf("err=%v rt=%v/%d mc=%v/%d", err, ai.HasRtPriority, ai.RtPriority, ai.HasMcUsers, ai.McUsers)
			if got != tc.expected {
				t.Errorf("got %s, expected %s", got, tc.expected)
			}
		})
	}
}

// procGroups reads one of /proc/net/igmp6 or /proc/net/anycast6 into
// "ifindex addr" strings: both print the index first and the address as 32
// hex digits third (net/ipv6/mcast.c igmp6_mc_seq_show,
// net/ipv6/anycast.c ac6_seq_show).
func procGroups6(t *testing.T, name string) []string {
	t.Helper()
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(readFixture(t, addrMcdumpDir, name)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		b, err := hex.DecodeString(f[2])
		if err != nil || len(b) != 16 {
			t.Fatalf("%s: %q", name, sc.Text())
		}
		out = append(out, f[0]+" "+addrString(b))
	}
	return out
}

// procGroups4 reads /proc/net/igmp: a device line "Idx Device : Count
// Querier", then one tab-indented line per group whose first field is the
// __be32 printed as a host-order %08X (net/ipv4/igmp.c igmp_mc_seq_show), so
// on a little-endian guest the bytes are reversed.
func procGroups4(t *testing.T) []string {
	t.Helper()
	var out []string
	idx := ""
	sc := bufio.NewScanner(bytes.NewReader(readFixture(t, addrMcdumpDir, "proc_net_igmp")))
	for sc.Scan() {
		line := sc.Text()
		f := strings.Fields(line)
		switch {
		case len(f) == 0 || f[0] == "Idx":
		case !strings.HasPrefix(line, "\t"):
			idx = f[0]
		default:
			v, err := strconv.ParseUint(f[0], 16, 32)
			if err != nil {
				t.Fatalf("proc_net_igmp: %q", line)
			}
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], uint32(v))
			out = append(out, idx+" "+addrString(b[:]))
		}
	}
	return out
}

// mcdumpGroups decodes a dump into "ifindex addr" strings from one field.
func mcdumpGroups(t *testing.T, name string, msgType uint16, field func(AddrInfo) []byte) []string {
	t.Helper()
	replies := decodeAddrReplies(t, addrFixture(addrMcdumpDir, name+".pcap"), msgType)
	out := make([]string, 0, len(replies))
	for i := range replies {
		out = append(out, fmt.Sprintf("%d %s", replies[i].Index, addrString(field(replies[i]))))
	}
	return out
}

func multicastOf(ai AddrInfo) []byte { return ai.Multicast }
func anycastOf(ai AddrInfo) []byte   { return ai.Anycast }

// go test ./pkg/xtcpnl/ -run TestParseMcastAnycastFixtures
func TestParseMcastAnycastFixtures(t *testing.T) {
	sorted := func(s []string) string {
		s = slices.Clone(s)
		slices.Sort(s)
		return strings.Join(s, ", ")
	}
	contains := func(s []string, want string) string {
		return fmt.Sprintf("%v", slices.Contains(s, want))
	}
	dumps := []struct {
		name    string
		msgType uint16
	}{
		{name: "netlink_route_getmulticast_v4", msgType: unix.RTM_GETMULTICAST},
		{name: "netlink_route_getmulticast_v6", msgType: unix.RTM_GETMULTICAST},
		{name: "netlink_route_getanycast_v6", msgType: unix.RTM_GETANYCAST},
	}
	allReplies := func(t *testing.T) []AddrInfo {
		parts := make([][]AddrInfo, 0, len(dumps))
		for _, d := range dumps {
			parts = append(parts, decodeAddrReplies(t, addrFixture(addrMcdumpDir, d.name+".pcap"), d.msgType))
		}
		return slices.Concat(parts...)
	}

	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    func(t *testing.T) string
	}{
		{
			description: "positive: v6 multicast groups match /proc/net/igmp6",
			got: func(t *testing.T) string {
				return sorted(mcdumpGroups(t, "netlink_route_getmulticast_v6", unix.RTM_GETMULTICAST, multicastOf))
			},
			expected: func(t *testing.T) string { return sorted(procGroups6(t, "proc_net_igmp6")) },
		},
		{
			description: "positive: ff02::1 on d0 is among them",
			got: func(t *testing.T) string {
				return contains(mcdumpGroups(t, "netlink_route_getmulticast_v6", unix.RTM_GETMULTICAST, multicastOf), "3 ff02::1")
			},
			expected: func(*testing.T) string { return "true" },
		},
		{
			description: "positive: v4 multicast groups match /proc/net/igmp",
			got: func(t *testing.T) string {
				return sorted(mcdumpGroups(t, "netlink_route_getmulticast_v4", unix.RTM_GETMULTICAST, multicastOf))
			},
			expected: func(t *testing.T) string { return sorted(procGroups4(t)) },
		},
		{
			description: "positive: the autojoin group 224.0.0.251 on d0 is among them",
			got: func(t *testing.T) string {
				return contains(mcdumpGroups(t, "netlink_route_getmulticast_v4", unix.RTM_GETMULTICAST, multicastOf), "3 224.0.0.251")
			},
			expected: func(*testing.T) string { return "true" },
		},
		{
			description: "positive: v6 anycast matches /proc/net/anycast6",
			got: func(t *testing.T) string {
				return sorted(mcdumpGroups(t, "netlink_route_getanycast_v6", unix.RTM_GETANYCAST, anycastOf))
			},
			expected: func(t *testing.T) string { return sorted(procGroups6(t, "proc_net_anycast6")) },
		},
		{
			description: "positive: the subnet-router anycast 2001:db8:1:: on d0 is among them",
			got: func(t *testing.T) string {
				return contains(mcdumpGroups(t, "netlink_route_getanycast_v6", unix.RTM_GETANYCAST, anycastOf), "3 2001:db8:1::")
			},
			expected: func(*testing.T) string { return "true" },
		},
		{
			// IFA_MC_USERS landed after Linux 7.1 (7cb81987, e1d0f3f0). When
			// the corpus moves to a kernel that sends it, this row fails, and
			// the positive row it is waiting for can be written.
			description: "boundary: the 7.1 corpus kernel's multicast dumps carry no IFA_MC_USERS",
			got: func(t *testing.T) string {
				uname := string(readFixture(t, "uname"))
				if !strings.Contains(uname, " 7.1.") {
					t.Fatalf("the corpus kernel is no longer 7.1 (%q): replace this row with a positive IFA_MC_USERS row", strings.TrimSpace(uname))
				}
				n := 0
				replies := allReplies(t)
				for i := range replies {
					if replies[i].HasMcUsers {
						n++
					}
				}
				return strconv.Itoa(n)
			},
			expected: func(*testing.T) string { return "0" },
		},
		{
			description: "corner: no IFA_ADDRESS or IFA_LOCAL, so nothing is aliased",
			got: func(t *testing.T) string {
				n := 0
				replies := allReplies(t)
				for i := range replies {
					if replies[i].Address != nil || replies[i].Local != nil {
						n++
					}
				}
				return strconv.Itoa(n)
			},
			expected: func(*testing.T) string { return "0" },
		},
		{
			// put_ifaddrmsg(nlh, 128, IFA_F_PERMANENT, ...) for IPv6
			// (net/ipv6/addrconf.c:5266), and the IPv4 equivalent with 32.
			description: "corner: every reply is a full-length permanent prefix",
			got: func(t *testing.T) string {
				var bad []string
				replies := allReplies(t)
				for i := range replies {
					ai := &replies[i]
					full := map[uint8]uint8{unix.AF_INET: 32, unix.AF_INET6: 128}[ai.Family]
					if ai.Prefixlen != full || ai.Flags&unix.IFA_F_PERMANENT == 0 {
						bad = append(bad, fmt.Sprintf("%d/%d flags %#x", ai.Family, ai.Prefixlen, ai.Flags))
					}
				}
				return strings.Join(bad, "; ")
			},
			expected: func(*testing.T) string { return "" },
		},
		{
			description: "negative: an AF_UNSPEC multicast dump is EOPNOTSUPP",
			got: func(t *testing.T) string {
				return mcdumpAnswer(t, "netlink_route_getmulticast_unspec")
			},
			expected: func(*testing.T) string { return "ENOTSUP rc=1 NLMSG_ERROR -95" },
		},
		{
			description: "negative: an AF_INET anycast dump is EOPNOTSUPP",
			got: func(t *testing.T) string {
				return mcdumpAnswer(t, "netlink_route_getanycast_v4")
			},
			expected: func(*testing.T) string { return "ENOTSUP rc=1 NLMSG_ERROR -95" },
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, expected := tc.got(t), tc.expected(t)
			if got != expected {
				t.Errorf("got      %s\nexpected %s", got, expected)
			}
		})
	}
}

// mcdumpAnswer is a refused dump's errno from its pcap, with the sender's
// exit status and stdout.
func mcdumpAnswer(t *testing.T, name string) string {
	t.Helper()
	flags, body, _ := lastNlMsgErr(t, addrFixture(addrMcdumpDir, name+".pcap"))
	e, err := ParseNlMsgErr(flags, body)
	if err != nil {
		t.Fatal(err)
	}
	// ErrnoName gives 95 its first name, ENOTSUP; on Linux it is the same
	// number as EOPNOTSUPP, which the kernel returns.
	return fmt.Sprintf("%s rc=%s %s", unix.ErrnoName(unix.Errno(-e.Errno)),
		strings.TrimSpace(string(readFixture(t, addrMcdumpDir, name+".rc"))),
		strings.TrimSpace(string(readFixture(t, addrMcdumpDir, name+".out"))))
}

// go test ./pkg/xtcpnl/ -run TestParseNewAddrAllocs
//
// ParseNewAddr runs in the daemon's event path. The ceilings are the counts
// measured on main before A1-T01: the new fixed-size fields cost nothing, and
// a byte attribute costs its copy only when present.
func TestParseNewAddrAllocs(t *testing.T) {
	tests := []struct {
		description string
		body        func(t *testing.T) []byte
		expected    float64
	}{
		{
			description: "boundary: an address with none of the new attributes (goip0: local, address, broadcast, label)",
			body: func(t *testing.T) []byte {
				return addrReplyBody(t, tdDumpGetAddrV4_7_1_4, unix.AF_INET, 3)
			},
			expected: 4,
		},
		{
			description: "positive: IFA_RT_PRIORITY adds no allocation (v4_metric d0: local, address, label)",
			body: func(t *testing.T) []byte {
				return addrReplyBody(t, addrFixture("addr-show", "v4_metric", "netlink_route_getaddr.pcap"), unix.AF_INET, 3)
			},
			expected: 3,
		},
		{
			description: "corner: a multicast reply costs its one copy",
			body: func(t *testing.T) []byte {
				bodies, _ := readDumpSetReplies(t, addrFixture(addrMcdumpDir, "netlink_route_getmulticast_v6.pcap"), unix.RTM_GETMULTICAST)
				if len(bodies) == 0 {
					t.Fatal("no multicast replies")
				}
				return bodies[0]
			},
			expected: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			body := tc.body(t)
			got := testing.AllocsPerRun(1_000, func() {
				if _, err := ParseNewAddr(body); err != nil {
					t.Fatal(err)
				}
			})
			if got > tc.expected {
				t.Fatalf("%v allocations, expected at most %v", got, tc.expected)
			}
		})
	}
}
