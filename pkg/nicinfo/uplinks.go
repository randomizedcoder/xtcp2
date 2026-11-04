//go:build linux

package nicinfo

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Route flags (include/uapi/linux/route.h, ipv6_route.h). The same bits in
// /proc/net/route and /proc/net/ipv6_route.
const (
	rtfUp     = 0x0001
	rtfReject = 0x0200
)

// maxLowerDepth bounds the upper->lower walk (vlan on bond on NIC is 2).
const maxLowerDepth = 8

// DefaultUplinks returns up to maxN physical interface names that carry the
// default IPv4/IPv6 routes (best-effort; reads /proc/net/route +
// /proc/net/ipv6_route). Used when the operator doesn't pin uplink interfaces
// explicitly. On any read error it returns what it has so far (possibly empty);
// it never fails.
//
// Only usable routes count: lo and reject routes are skipped (the kernel's
// "unreachable ::/0 dev lo" is in every ipv6_route), as are routes not up. A
// virtual device holding the route (bond0, a vlan, a bridge) is replaced by the
// physical NICs beneath it, which is where the NIC and LLDP columns live.
func DefaultUplinks(maxN int) []string {
	return defaultUplinks("/proc", "/sys", maxN)
}

func defaultUplinks(procRoot, sysfsRoot string, maxN int) []string {
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

	var routes []string
	if b, err := os.ReadFile(filepath.Join(procRoot, "net", "route")); err == nil {
		routes = append(routes, parseDefaultRoutesV4(b)...)
	}
	if b, err := os.ReadFile(filepath.Join(procRoot, "net", "ipv6_route")); err == nil {
		routes = append(routes, parseDefaultRoutesV6(b)...)
	}
	for _, route := range routes {
		for _, name := range physicalLowers(sysfsRoot, route) {
			if !add(name) {
				return out
			}
		}
	}
	return out
}

// usableRoute reports whether a default route's interface and hex flags name
// an uplink: up, not a reject route, and not lo ("*" is v4's no-device).
func usableRoute(iface, flags string) bool {
	if iface == "" || iface == "lo" || iface == "*" {
		return false
	}
	f, err := strconv.ParseUint(flags, 16, 32)
	return err == nil && f&rtfUp != 0 && f&rtfReject == 0
}

// parseDefaultRoutesV4 returns the interfaces holding a usable default route in
// /proc/net/route: a header line, then whitespace-separated fields
// Iface Destination Gateway Flags RefCnt Use Metric Mask ...
func parseDefaultRoutesV4(b []byte) []string {
	var out []string
	for i, line := range strings.Split(string(b), "\n") {
		if i == 0 { // header
			continue
		}
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		// Destination and Mask 0.0.0.0 is the default route.
		if f[1] == "00000000" && f[7] == "00000000" && usableRoute(f[0], f[3]) {
			out = append(out, f[0])
		}
	}
	return out
}

// parseDefaultRoutesV6 returns the interfaces holding a usable default route in
// /proc/net/ipv6_route. Each line has ten fields: dest_net dest_prefixlen
// src_net src_prefixlen next_hop metric refcnt use flags iface.
func parseDefaultRoutesV6(b []byte) []string {
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 10 {
			continue
		}
		// dest_net all zeros and prefix length 0 => default route (::/0).
		if f[0] == "00000000000000000000000000000000" && f[1] == "00" && usableRoute(f[9], f[8]) {
			out = append(out, f[9])
		}
	}
	return out
}

// physicalLowers returns the physical NICs beneath name, following the
// <sysfs>/class/net/<if>/lower_<if> links a bond, team, vlan or bridge has to
// its lower devices, sorted per level. A NIC (it has a "device" entry) is its
// own answer. A virtual device with no physical NIC beneath it (a bridge of
// veths, a tunnel) or with an unreadable sysfs is returned as itself, as
// before this walk existed.
func physicalLowers(sysfsRoot, name string) []string {
	netDir := filepath.Join(sysfsRoot, "class", "net")
	var out []string
	visited := make(map[string]bool)
	var walk func(name string, depth int)
	walk = func(name string, depth int) {
		if visited[name] || depth > maxLowerDepth {
			return
		}
		visited[name] = true
		dir := filepath.Join(netDir, name)
		if _, err := os.Lstat(filepath.Join(dir, "device")); err == nil {
			out = append(out, name)
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries { // ReadDir sorts by name
			if lower, ok := strings.CutPrefix(e.Name(), "lower_"); ok && lower != "" {
				walk(lower, depth+1)
			}
		}
	}
	walk(name, 0)
	if len(out) == 0 {
		return []string{name}
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
