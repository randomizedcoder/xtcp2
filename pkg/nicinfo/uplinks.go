//go:build linux

package nicinfo

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultUplinks returns up to maxN physical interface names that carry the
// default IPv4/IPv6 routes (best-effort; reads /proc/net/route +
// /proc/net/ipv6_route). Used when the operator doesn't pin uplink interfaces
// explicitly. On any read error it returns what it has so far (possibly empty);
// it never fails.
func DefaultUplinks(maxN int) []string {
	if maxN <= 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var out []string
	add := func(name string) bool {
		if name == "" {
			return true
		}
		if _, dup := seen[name]; dup {
			return true
		}
		seen[name] = struct{}{}
		out = append(out, name)
		return len(out) < maxN
	}

	for _, name := range defaultRouteIfacesV4() {
		if !add(name) {
			return out
		}
	}
	for _, name := range defaultRouteIfacesV6() {
		if !add(name) {
			return out
		}
	}
	return out
}

// defaultRouteIfacesV4 parses /proc/net/route for interfaces holding the
// default route (destination 0.0.0.0). The file is whitespace-separated with a
// header line; fields are: Iface Destination Gateway Flags ...
func defaultRouteIfacesV4() []string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return nil
	}
	var out []string
	for i, line := range strings.Split(string(b), "\n") {
		if i == 0 { // header
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		// Destination "00000000" is the default route (0.0.0.0).
		if f[1] == "00000000" {
			out = append(out, f[0])
		}
	}
	return out
}

// PhysicalInterfaces returns up to maxN physical interface names discovered by
// scanning <sysfsRoot>/class/net for entries that have a "device" symlink (a
// real NIC — virtio_net, mlx5, ice, … — as opposed to lo/veth*/docker0/bridges,
// which have none). Names are returned sorted for determinism. This is the
// fallback used when neither an explicit override nor default-route detection
// yields an uplink (e.g. a host whose default route lives on a separate
// management NIC, or a VM with no default route configured). Best-effort: any
// read error yields what it has so far (possibly empty); it never fails.
func PhysicalInterfaces(sysfsRoot string, maxN int) []string {
	if maxN <= 0 {
		return nil
	}
	netDir := filepath.Join(sysfsRoot, "class", "net")
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return nil
	}

	var names []string
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		// A "device" entry (symlink on real sysfs, dir in testdata) marks a
		// real NIC; its absence marks a virtual interface. Mirrors Collect's
		// own skip rule so the two stay consistent.
		if _, err := os.Lstat(filepath.Join(netDir, name, "device")); err != nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxN {
		names = names[:maxN]
	}
	return names
}

// defaultRouteIfacesV6 parses /proc/net/ipv6_route for interfaces holding the
// default route (::/0). Each line has: dest_net(32 hex) dest_prefixlen ...
// with the interface name as the last field.
func defaultRouteIfacesV6() []string {
	b, err := os.ReadFile("/proc/net/ipv6_route")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		// dest_net all zeros and prefix length 0 => default route (::/0).
		if f[0] == "00000000000000000000000000000000" && f[1] == "00" {
			out = append(out, f[len(f)-1])
		}
	}
	return out
}
