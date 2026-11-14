//go:build linux

package nicinfo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const routeV4Header = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

// routeV4 is one /proc/net/route line.
func routeV4(iface, dest, flags, mask string) string {
	return iface + "\t" + dest + "\t0100A8C0\t" + flags + "\t0\t0\t0\t" + mask + "\t0\t0\t0\n"
}

// routeV6 is one /proc/net/ipv6_route line.
func routeV6(dest, prefix, flags, iface string) string {
	zero := strings.Repeat("0", 32)
	return dest + " " + prefix + " " + zero + " 00 " + zero + " 00000400 00000001 00000000 " + flags + " " + iface + "\n"
}

var (
	zeroV6 = strings.Repeat("0", 32)
	// The kernel's "unreachable ::/0 dev lo" (RTF_NONEXTHOP|RTF_REJECT, not
	// RTF_UP): in every ipv6_route, and what made UPLINK2 lo on prod.
	unreachableV6 = zeroV6 + " 00 " + zeroV6 + " 00 " + zeroV6 + " ffffffff 00000001 00000000 00200200       lo\n"
)

func TestParseDefaultRoutesV4(t *testing.T) {
	for _, tc := range []struct {
		name, category, description string
		input                       string
		expected                    []string
	}{
		{"gateway", "positive", "default via a gateway on eth0 (UG)", routeV4Header + routeV4("eth0", "00000000", "0003", "00000000"), []string{"eth0"}},
		{"bond", "positive", "default on bond0, the prod shape", routeV4Header + routeV4("bond0", "00000000", "0003", "00000000"), []string{"bond0"}},
		{"unreachable", "negative", "unreachable default: no device, RTF_REJECT", routeV4Header + routeV4("*", "00000000", "0201", "00000000"), nil},
		{"blackhole", "negative", "blackhole default: no device (*), up, not reject", routeV4Header + routeV4("*", "00000000", "0001", "00000000"), nil},
		{"reject-on-dev", "negative", "reject flag on a real device", routeV4Header + routeV4("eth0", "00000000", "0203", "00000000"), nil},
		{"not-up", "negative", "default route without RTF_UP", routeV4Header + routeV4("eth0", "00000000", "0002", "00000000"), nil},
		{"lo", "negative", "an up default route on lo", routeV4Header + routeV4("lo", "00000000", "0001", "00000000"), nil},
		{"subnet", "negative", "a subnet route, not the default", routeV4Header + routeV4("eth0", "0000A8C0", "0001", "00FFFFFF"), nil},
		{"mask", "boundary", "destination 0 with a /8 mask is 0.0.0.0/8, not default", routeV4Header + routeV4("eth0", "00000000", "0001", "000000FF"), nil},
		{"header-only", "boundary", "a table with only the header", routeV4Header, nil},
		{"two", "corner", "two defaults by metric, kept in table order", routeV4Header + routeV4("eth1", "00000000", "0003", "00000000") + routeV4("eth0", "00000000", "0003", "00000000"), []string{"eth1", "eth0"}},
		{"garbage", "corner", "short lines and non-hex flags", routeV4Header + "eth0 00000000\n" + routeV4("eth0", "00000000", "zz", "00000000"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s", tc.category, tc.description)
			if got := parseDefaultRoutesV4([]byte(tc.input)); !slices.Equal(got, tc.expected) {
				t.Fatalf("got %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestParseDefaultRoutesV6(t *testing.T) {
	for _, tc := range []struct {
		name, category, description string
		input                       string
		expected                    []string
	}{
		{"static", "positive", "static default via eth0 (UP|GATEWAY)", routeV6(zeroV6, "00", "00000003", "eth0"), []string{"eth0"}},
		{"ra", "positive", "RA-learned default (UP|GATEWAY|DEFAULT|ADDRCONF|EXPIRES)", routeV6(zeroV6, "00", "00450003", "bond0"), []string{"bond0"}},
		{"unreachable-lo", "negative", "the kernel's unreachable ::/0 dev lo, the UPLINK2=lo bug", unreachableV6, nil},
		{"up-reject", "negative", "an up reject route on a real device", routeV6(zeroV6, "00", "00000201", "eth0"), nil},
		{"lo-up", "negative", "an up default on lo", routeV6(zeroV6, "00", "00000001", "lo"), nil},
		{"prefix", "boundary", "dest :: with a /64 prefix, not default", routeV6(zeroV6, "40", "00000001", "eth0"), nil},
		{"empty", "boundary", "an empty table", "", nil},
		{"with-unreachable", "corner", "a real default beside the unreachable one", unreachableV6 + routeV6(zeroV6, "00", "00000003", "eth0"), []string{"eth0"}},
		{"garbage", "corner", "nine fields, and non-hex flags", zeroV6 + " 00 x x x x x x x\n" + routeV6(zeroV6, "00", "zz", "eth0"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s", tc.category, tc.description)
			if got := parseDefaultRoutesV6([]byte(tc.input)); !slices.Equal(got, tc.expected) {
				t.Fatalf("got %v, expected %v", got, tc.expected)
			}
		})
	}
}

// sysNet builds <root>/class/net: nics get a device entry, uppers get one
// lower_<if> link per lower (a symlink, as on sysfs), virtual ifaces get
// neither.
func sysNet(t *testing.T, nics []string, uppers map[string][]string, virtual []string) string {
	t.Helper()
	root := t.TempDir()
	net := filepath.Join(root, "class", "net")
	mkdir := func(p string) {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range nics {
		mkdir(filepath.Join(net, n, "device"))
	}
	for _, v := range virtual {
		mkdir(filepath.Join(net, v))
	}
	for upper, lowers := range uppers {
		mkdir(filepath.Join(net, upper))
		for _, l := range lowers {
			if err := os.Symlink(filepath.Join("..", l), filepath.Join(net, upper, "lower_"+l)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func TestPhysicalLowers(t *testing.T) {
	for _, tc := range []struct {
		name, category, description string
		nics                        []string
		uppers                      map[string][]string
		virtual                     []string
		input                       string
		expected                    []string
	}{
		{"nic", "positive", "a NIC is its own answer", []string{"eth0"}, nil, nil, "eth0", []string{"eth0"}},
		{"bond", "positive", "bond0 over two NICs, the prod shape", []string{"eth0", "eth1"}, map[string][]string{"bond0": {"eth1", "eth0"}}, nil, "bond0", []string{"eth0", "eth1"}},
		{"vlan-on-bond", "positive", "a vlan on a bond on two NICs", []string{"eth0", "eth1"}, map[string][]string{"bond0.100": {"bond0"}, "bond0": {"eth0", "eth1"}}, nil, "bond0.100", []string{"eth0", "eth1"}},
		{"bridge-veths", "negative", "a bridge of veths has no NIC beneath it", nil, map[string][]string{"br0": {"veth1"}}, []string{"veth1"}, "br0", []string{"br0"}},
		{"missing", "negative", "an interface not in sysfs", nil, nil, nil, "gone0", []string{"gone0"}},
		{"empty-bond", "boundary", "a bond with no members", nil, map[string][]string{"bond0": nil}, nil, "bond0", []string{"bond0"}},
		{"bridge-mixed", "corner", "a bridge over a NIC and a veth: only the NIC", []string{"eth2"}, map[string][]string{"br0": {"eth2", "veth1"}}, []string{"veth1"}, "br0", []string{"eth2"}},
		{"cycle", "corner", "a lower_ loop terminates", nil, map[string][]string{"a": {"b"}, "b": {"a"}}, nil, "a", []string{"a"}},
		{"shared-lower", "corner", "two paths to one NIC count it once", []string{"eth0"}, map[string][]string{"br0": {"bond0", "vlan5"}, "bond0": {"eth0"}, "vlan5": {"eth0"}}, nil, "br0", []string{"eth0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s", tc.category, tc.description)
			root := sysNet(t, tc.nics, tc.uppers, tc.virtual)
			if got := physicalLowers(root, tc.input); !slices.Equal(got, tc.expected) {
				t.Fatalf("got %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestDefaultUplinksFixture(t *testing.T) {
	bond := map[string][]string{"bond0": {"eth0", "eth1"}}
	for _, tc := range []struct {
		name, category, description string
		v4, v6                      string
		max                         int
		expected                    []string
	}{
		{"prod", "positive", "v4 default on bond0, v6 only the unreachable lo route: was [bond0 lo]", routeV4Header + routeV4("bond0", "00000000", "0003", "00000000"), unreachableV6, 2, []string{"eth0", "eth1"}},
		{"dual-stack", "positive", "v4 and v6 defaults on one bond: members once", routeV4Header + routeV4("bond0", "00000000", "0003", "00000000"), routeV6(zeroV6, "00", "00450003", "bond0"), 2, []string{"eth0", "eth1"}},
		{"only-lo", "negative", "no usable default route at all", routeV4Header, unreachableV6, 2, nil},
		{"no-proc", "negative", "route files unreadable", "", "", 2, nil},
		{"cap-one", "boundary", "max 1 with a two-member bond", routeV4Header + routeV4("bond0", "00000000", "0003", "00000000"), "", 1, []string{"eth0"}},
		{"cap-zero", "boundary", "max 0", routeV4Header + routeV4("bond0", "00000000", "0003", "00000000"), "", 0, nil},
		{"v6-member", "corner", "v4 on eth2, v6 on bond0: v4 first, then the members", routeV4Header + routeV4("eth2", "00000000", "0003", "00000000"), routeV6(zeroV6, "00", "00000003", "bond0"), 4, []string{"eth2", "eth0", "eth1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s", tc.category, tc.description)
			sys := sysNet(t, []string{"eth0", "eth1", "eth2"}, bond, nil)
			proc := t.TempDir()
			if tc.v4 != "" || tc.v6 != "" {
				if err := os.MkdirAll(filepath.Join(proc, "net"), 0o750); err != nil {
					t.Fatal(err)
				}
				for file, body := range map[string]string{"route": tc.v4, "ipv6_route": tc.v6} {
					if err := os.WriteFile(filepath.Join(proc, "net", file), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if got := defaultUplinks(proc, sys, tc.max); !slices.Equal(got, tc.expected) {
				t.Fatalf("got %v, expected %v", got, tc.expected)
			}
		})
	}
}
