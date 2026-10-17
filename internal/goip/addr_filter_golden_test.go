package goip

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is `goip addr show SELECTORS` against `ip`'s, byte for byte
// (A2-T02; docs/netlink/addr-write-path.md, table 5): each selector row's
// committed pcap replayed through goip with the argv addr-corpus.exp gave
// `ip`, and compared with the sidecar `ip` printed.

// addrFilterGoldens are the selector rows of addr-corpus.exp's `selectors`
// table, minus the `-o` form, which is A3's.
var addrFilterGoldens = []struct {
	description string
	state       string
	suffix      string
	args        []string
	// netns is the peer namespace name the sidecar resolved and goip prints
	// as link-netnsid 0; empty when there is none.
	netns string
	// dropIoctlQlen removes the `qlen 1000` 7.1.0's print_queuelen reads
	// from ioctl(SIOCGIFTXQLEN) when an AF_INET6 link reply has no
	// IFLA_TXQLEN; see sidecarWithoutIoctlQlen.
	dropIoctlQlen bool
	expected      string
}{
	{description: "positive: secondary", state: "v4_secondary", suffix: "_secondary",
		args: []string{"addr", "show", "secondary"}, expected: "only the secondary address under d0"},
	{description: "positive: primary is -secondary inverted", state: "v4_secondary", suffix: "_primary",
		args: []string{"addr", "show", "primary"}, expected: "only the primary address"},
	{description: "positive: a negated flag", state: "v4_secondary", suffix: "_not_secondary",
		args: []string{"addr", "show", "-secondary"}, expected: "byte-identical"},
	{description: "positive: to PREFIX, v4: filter.family drops the link/ line", state: "v4_secondary", suffix: "_to",
		args: []string{"addr", "show", "to", "192.0.2.0/24"}, expected: "d0 header with no link/ether line, both addresses"},
	{description: "corner: label falls back to the device name", state: "v4_secondary", suffix: "_label_dev",
		args: []string{"addr", "show", "label", "d*"}, expected: "the d0 addresses, which carry no IFA_LABEL; no header"},
	{description: "positive: a bare device name is dev NAME", state: "v4_secondary", suffix: "_d0",
		args: []string{"addr", "show", "d0"}, expected: "byte-identical"},
	{description: "positive: label glob prints no header", state: "v4_label", suffix: "_label",
		args: []string{"addr", "show", "label", "d0:*"}, expected: "address lines only"},
	{description: "positive: label in JSON is {\"addr_info\":[...]} per link", state: "v4_label", suffix: "_label_json",
		args: []string{"-j", "addr", "show", "label", "d0:*"}, expected: "byte-identical to the captured -j output"},
	{description: "positive: proto keeps addresses with no IFA_PROTO and rescues address-less links", state: "v4_proto", suffix: "_proto",
		args: []string{"addr", "show", "proto", "99"}, expected: "lo keeps 127.0.0.1, drops ::1; nlmon0 header; d0's 0x63 address"},
	{description: "positive: dynamic", state: "v4_lifetimes", suffix: "_dynamic",
		args: []string{"addr", "show", "dynamic"}, expected: "byte-identical"},
	{description: "positive: permanent", state: "v4_lifetimes", suffix: "_permanent",
		args: []string{"addr", "show", "permanent"}, expected: "byte-identical"},
	{description: "positive: -dynamic", state: "v4_lifetimes", suffix: "_not_dynamic",
		args: []string{"addr", "show", "-dynamic"}, expected: "byte-identical"},
	{description: "positive: scope host", state: "v4_host_scope", suffix: "_scope_host",
		args: []string{"addr", "show", "scope", "host"}, expected: "byte-identical"},
	{description: "boundary: scope all masks nothing", state: "v4_host_scope", suffix: "_scope_all",
		args: []string{"addr", "show", "scope", "all"}, expected: "byte-identical"},
	{description: "positive: -6", state: "v6_flags", suffix: "_v6",
		args: []string{"-6", "addr", "show"}, dropIoctlQlen: true, expected: "byte-identical once the ioctl's qlen is removed"},
	{description: "positive: to PREFIX, v6", state: "v6_flags", suffix: "_to",
		args: []string{"addr", "show", "to", "2001:db8::/32"}, expected: "byte-identical"},
	{description: "positive: tentative", state: "v6_tentative", suffix: "_tentative",
		args: []string{"addr", "show", "tentative"}, expected: "byte-identical"},
	{description: "positive: -tentative", state: "v6_tentative", suffix: "_not_tentative",
		args: []string{"addr", "show", "-tentative"}, expected: "byte-identical"},
	{description: "positive: scope link", state: "v6_stable_privacy", suffix: "_scope_link",
		args: []string{"addr", "show", "scope", "link"}, expected: "byte-identical"},
	{description: "positive: dadfailed", state: "v6_dadfailed", suffix: "_dadfailed", netns: "addrb",
		args: []string{"addr", "show", "dadfailed"}, expected: "byte-identical after the netns rewrite"},
	{description: "positive: optimistic", state: "v6_optimistic", suffix: "_optimistic",
		args: []string{"addr", "show", "optimistic"}, expected: "byte-identical"},
	{description: "positive: temporary", state: "v6_temporary", suffix: "_temporary",
		args: []string{"addr", "show", "temporary"}, expected: "byte-identical"},
	{description: "positive: vrf", state: "v4_vrf", suffix: "_vrf",
		args: []string{"addr", "show", "vrf", "vrf0"}, expected: "byte-identical"},
	{description: "positive: master on a vrf", state: "v4_vrf", suffix: "_master",
		args: []string{"addr", "show", "master", "vrf0"}, expected: "byte-identical"},
	{description: "positive: up", state: "mixed_links", suffix: "_up",
		args: []string{"addr", "show", "up"}, expected: "byte-identical"},
	{description: "positive: down", state: "mixed_links", suffix: "_down",
		args: []string{"addr", "show", "down"}, expected: "byte-identical"},
	{description: "corner: up down matches nothing", state: "mixed_links", suffix: "_up_down",
		args: []string{"addr", "show", "up", "down"}, expected: "empty stdout, exit 0"},
	{description: "positive: master", state: "mixed_links", suffix: "_master",
		args: []string{"addr", "show", "master", "bond0"}, expected: "only d3"},
	{description: "corner: -4 master filters client-side", state: "mixed_links", suffix: "_v4_master",
		args: []string{"-4", "addr", "show", "master", "bond0"}, expected: "only d3, from a request with no IFLA_MASTER"},
	{description: "positive: nomaster", state: "mixed_links", suffix: "_nomaster",
		args: []string{"addr", "show", "nomaster"}, expected: "every link but d3"},
	{description: "positive: type", state: "mixed_links", suffix: "_type_dummy",
		args: []string{"addr", "show", "type", "dummy"}, expected: "byte-identical"},
	{description: "corner: the slave kind is client-side only", state: "mixed_links", suffix: "_type_bond_slave",
		args: []string{"addr", "show", "type", "bond_slave"}, expected: "only d3"},
	{description: "positive: -4 drops address-less links", state: "mixed_links", suffix: "_v4",
		args: []string{"-4", "addr", "show"}, expected: "no d2: the rescue needs AF_UNSPEC or AF_PACKET"},
	{description: "positive: group", state: "mixed_links", suffix: "_group",
		args: []string{"addr", "show", "group", "default"}, expected: "byte-identical"},
	{description: "positive: novf", state: "mixed_links", suffix: "_novf",
		args: []string{"addr", "show", "novf"}, expected: "byte-identical"},
}

// addrFilterNotSelectors are the per-state forms TestAddrShowReadbackGoldens
// and A3 own, and the write row TestAddrFilterRequestShape owns.
var addrFilterNotSelectors = []string{"", "_json", "_br", "_br_json", "_o", "_o_v6", "_d", "_stats", "_vrf_d1"}

// TestAddrFilter is `goip addr show SELECTORS` against ip_addr<suffix> in
// every selector row.
//
// go test ./internal/goip/ -run TestAddrFilter$
func TestAddrFilter(t *testing.T) {
	t.Run("corner: every selector fixture has a row", func(t *testing.T) {
		entries, err := os.ReadDir(addrShowDir)
		if err != nil {
			t.Fatal(err)
		}
		var onDisk []string
		for _, e := range entries {
			pcaps, gerr := os.ReadDir(addrShowDir + e.Name())
			if gerr != nil {
				t.Fatal(gerr)
			}
			for _, p := range pcaps {
				suffix, ok := strings.CutPrefix(p.Name(), "netlink_route_getaddr")
				if !ok {
					suffix, ok = strings.CutPrefix(p.Name(), "ip_addr")
					if !ok || !strings.HasSuffix(suffix, ".pcap") {
						continue
					}
				}
				suffix = strings.TrimSuffix(suffix, ".pcap")
				skip := false
				for _, n := range addrFilterNotSelectors {
					skip = skip || suffix == n
				}
				if !skip && strings.HasSuffix(p.Name(), ".pcap") {
					onDisk = append(onDisk, e.Name()+"/"+suffix)
				}
			}
		}
		rows := make([]string, 0, len(addrFilterGoldens))
		for _, tc := range addrFilterGoldens {
			rows = append(rows, tc.state+"/"+tc.suffix)
		}
		sort.Strings(onDisk)
		sort.Strings(rows)
		if strings.Join(onDisk, " ") != strings.Join(rows, " ") {
			t.Fatalf("selector pcaps on disk %v\nrows %v", onDisk, rows)
		}
	})

	for _, tc := range addrFilterGoldens {
		t.Run(tc.description, func(t *testing.T) {
			dir := addrShowDir + tc.state + "/"
			t.Setenv("GOIP_REPLAY", dir+"netlink_route_getaddr"+tc.suffix+".pcap")
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			var want []byte
			if tc.netns != "" {
				want = []byte(sidecarWithNetnsID(t, dir+"ip_addr"+tc.suffix, tc.netns))
			} else {
				raw, err := os.ReadFile(dir + "ip_addr" + tc.suffix)
				if err != nil {
					t.Fatal(err)
				}
				want = raw
			}
			if tc.dropIoctlQlen {
				want = []byte(sidecarWithoutIoctlQlen(t, string(want)))
			}
			firstByteDiff(t, stdout.Bytes(), want)
		})
	}
}

// exactReplay is statsReplay with dumps answered the way gets are: only by
// the replies that followed the byte-identical request `ip` sent. A plain
// replay answers every dump with every reply of the type in the capture,
// which would hand goip the link a later side-get fetched and hide that
// side-get.
type exactReplay struct{ *statsReplay }

func (s exactReplay) Dump(request []byte, msgType uint16) ([][]byte, error) {
	want := zeroSeqPidCopy(request)
	s.sent = append(s.sent, want)
	s.dumps++
	msgs := s.inner.cap.Msgs()
	for i, m := range msgs {
		if !m.IsRequest() || !bytes.Equal(capturedRequest(m), want) {
			continue
		}
		var out [][]byte
		for _, r := range msgs[i+1:] {
			if r.IsRequest() {
				break
			}
			if r.Hdr.Type == msgType {
				out = append(out, xtcpnl.CopyBytes(r.Body))
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: no recorded request matches %x", ErrNoReplay, want)
}

// enodevSource answers every get with ENODEV and every dump with nothing,
// recording what it was sent: a kernel with no device by any name.
type enodevSource struct{ sent [][]byte }

func (s *enodevSource) Dump(request []byte, _ uint16) ([][]byte, error) {
	s.sent = append(s.sent, zeroSeqPidCopy(request))
	return nil, nil
}

func (s *enodevSource) Talk(request []byte, _ uint16) ([]byte, error) {
	s.sent = append(s.sent, zeroSeqPidCopy(request))
	return nil, fmt.Errorf("goip: talk: %w", unix.ENODEV)
}

// TestAddrFilterRequestShape is every request goip sends for a selector row
// against the requests `ip` sent in the same capture, byte for byte with seq
// and pid cleared, in count and order. statsReplay answers a get only when
// it is byte-identical to one `ip` sent, so a wrong lookup fails the run
// before the comparison does.
//
// go test ./internal/goip/ -run TestAddrFilterRequestShape
func TestAddrFilterRequestShape(t *testing.T) {
	const (
		getlink = unix.RTM_GETLINK
		getaddr = unix.RTM_GETADDR
	)
	tests := []struct {
		description string
		state       string
		pcap        string
		family      uint8
		args        string
		// types is the request sequence both must have.
		types []uint16
		// errHas is the error goip must return, "" for success; the pcap's
		// .err sidecar must hold it too.
		errHas   string
		expected string
	}{
		{"positive: master: a get by name, then IFLA_MASTER on the dump", "mixed_links", "netlink_route_getaddr_master.pcap",
			unix.AF_UNSPEC, "show master bond0", []uint16{getlink, getlink, getaddr}, "", "52B get, 48B dump with ext 0x09 and IFLA_MASTER=6"},
		{"boundary: -4 sends a bare dump", "mixed_links", "netlink_route_getaddr_v4_master.pcap",
			unix.AF_INET, "show master bond0", []uint16{getlink, getlink, getaddr}, "", "32B AF_INET dump, no attributes"},
		{"boundary: nomaster is IFLA_MASTER 0xffffffff", "mixed_links", "netlink_route_getaddr_nomaster.pcap",
			unix.AF_UNSPEC, "show nomaster", []uint16{getlink, getaddr}, "", "48B dump"},
		{"positive: type is a LINKINFO kind nest", "mixed_links", "netlink_route_getaddr_type_dummy.pcap",
			unix.AF_UNSPEC, "show type dummy", []uint16{getlink, getaddr, getlink}, "", "56B dump, kind \"dummy\" with no NUL; then d3's master, which the filtered dump left out, by index"},
		{"corner: the slave kind is not sent", "mixed_links", "netlink_route_getaddr_type_bond_slave.pcap",
			unix.AF_UNSPEC, "show type bond_slave", []uint16{getlink, getaddr}, "", "40B dump, ext mask only"},
		{"boundary: novf clears the VF bit", "mixed_links", "netlink_route_getaddr_novf.pcap",
			unix.AF_UNSPEC, "show novf", []uint16{getlink, getaddr}, "", "ext mask 0x08"},
		{"corner: group stays client-side", "mixed_links", "netlink_route_getaddr_group.pcap",
			unix.AF_UNSPEC, "show group default", []uint16{getlink, getaddr}, "", "plain 40B dump"},
		{"corner: to keeps the link dump AF_UNSPEC", "v4_secondary", "netlink_route_getaddr_to.pcap",
			unix.AF_UNSPEC, "show to 192.0.2.0/24", []uint16{getlink, getaddr}, "", "AF_UNSPEC link dump with ext mask, AF_INET address dump"},
		{"positive: a bare device takes the three-request dev path", "v4_secondary", "netlink_route_getaddr_d0.pcap",
			unix.AF_UNSPEC, "show d0", []uint16{getlink, getlink, getaddr}, "", "by name, by index, address dump"},
		{"positive: vrf: ll_name_to_index, then name_is_vrf, then the dump", "v4_vrf", "netlink_route_getaddr_vrf.pcap",
			unix.AF_UNSPEC, "show vrf vrf0", []uint16{getlink, getlink, getlink, getaddr}, "", "52B get, 44B get, dump with IFLA_MASTER"},
		{"negative: not a vrf: the same two gets and no dump", "v4_vrf", "ip_addr_vrf_d1.pcap",
			unix.AF_UNSPEC, "show vrf d1", []uint16{getlink, getlink}, `Error: argument "d1" is wrong: Not a valid VRF name`, "the error ip printed"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := addrShowDir + tc.state + "/" + tc.pcap
			src := exactReplay{newStatsReplay(t, path)}
			_, err := runAddressWith(t, src, tc.family, strings.Fields(tc.args))
			switch {
			case tc.errHas == "" && err != nil:
				t.Fatalf("runAddress(%q): %v", tc.args, err)
			case tc.errHas != "":
				if err == nil || !strings.Contains(err.Error(), tc.errHas) {
					t.Fatalf("runAddress(%q) = %v, want %q", tc.args, err, tc.errHas)
				}
				raw, rerr := os.ReadFile(strings.TrimSuffix(path, ".pcap") + ".err")
				if rerr != nil {
					t.Fatal(rerr)
				}
				if strings.TrimSpace(string(raw)) != tc.errHas {
					t.Fatalf("ip printed %q, want %q", raw, tc.errHas)
				}
			}
			c, err := nlparity.ParseRouteCaptureFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var ip [][]byte
			for _, m := range c.Requests() {
				ip = append(ip, capturedRequest(m))
			}
			if len(src.sent) != len(tc.types) || len(ip) != len(tc.types) {
				t.Fatalf("goip sent %d requests, ip %d; want %d each\ngoip %x\nip   %x", len(src.sent), len(ip), len(tc.types), src.sent, ip)
			}
			for i := range ip {
				if typ := requestTypeOf(src.sent[i]); typ != tc.types[i] {
					t.Errorf("request %d type %d, want %d", i+1, typ, tc.types[i])
				}
				if !bytes.Equal(src.sent[i], ip[i]) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], ip[i])
				}
			}
		})
	}

	// No pcap has a kernel without the device, so these two run against
	// one: every get is ENODEV.
	for _, tc := range []struct {
		description string
		args        string
		expected    string
	}{
		{"negative: no such master: one get, no dump", "show master nosuch", `Error: argument "nosuch" is wrong: Device does not exist`},
		{"negative: no such device: one get, no dump", "show dev nosuch", `Device "nosuch" does not exist`},
	} {
		t.Run(tc.description, func(t *testing.T) {
			src := &enodevSource{}
			_, err := runAddressWith(t, src, unix.AF_UNSPEC, strings.Fields(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("runAddress(%q) = %v, want %q", tc.args, err, tc.expected)
			}
			if !errors.Is(err, ErrNoSuchDevice) {
				t.Errorf("errors.Is(%v, ErrNoSuchDevice) = false", err)
			}
			if len(src.sent) != 1 || requestTypeOf(src.sent[0]) != getlink {
				t.Fatalf("sent %x, want one RTM_GETLINK", src.sent)
			}
		})
	}
}
