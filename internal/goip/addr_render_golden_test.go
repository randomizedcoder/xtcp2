package goip

import (
	"bytes"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is goip's `addr show` against `ip`'s, byte for byte (A1-T02;
// docs/netlink/addr-write-path.md, table 6): each addr-show state's
// committed pcap replayed through goip, in text and in `-j`, and compared
// with the sidecar `ip` printed from the same capture window.
//
// Byte for byte, not key by key: the JSON members are compared in order, so
// AddrView.MarshalJSON writing print_addrinfo's order is what is under test,
// as well as the key set and the values.

const addrShowDir = linkWriteDir + "addr-show/"

// firstByteDiff fails t with the first line on which got and want differ,
// or with their lengths when one is a prefix of the other.
func firstByteDiff(t *testing.T, got, want []byte) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	g := strings.SplitAfter(string(got), "\n")
	w := strings.SplitAfter(string(want), "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			t.Fatalf("line %d differs\n got: %q\nwant: %q", i+1, g[i], w[i])
		}
	}
	t.Fatalf("got %d bytes, want %d; one is a prefix of the other", len(got), len(want))
}

// TestAddrShowReadbackGoldens is `goip addr show` and `goip -j addr show`
// against ip_addr and ip_addr_json in every addr-show state.
//
// go test ./internal/goip/ -run TestAddrShowReadbackGoldens
func TestAddrShowReadbackGoldens(t *testing.T) {
	tests := []struct {
		description string
		state       string
		// netns is the peer namespace name the text sidecar resolved and
		// goip prints as link-netnsid 0; empty when there is none.
		netns    string
		expected string
	}{
		{description: "positive: several links, both families", state: "mixed_links", expected: "byte-identical"},
		{description: "positive: autojoin is a flag on a local address", state: "v4_autojoin", expected: "byte-identical"},
		{description: "positive: brd after the local block", state: "v4_brd", expected: "byte-identical"},
		{description: "boundary: finite lifetimes on a deprecated address, frozen bytes", state: "v4_deprecated", expected: "byte-identical"},
		{description: "positive: scope host", state: "v4_host_scope", expected: "byte-identical"},
		{description: "positive: flags between scope and label", state: "v4_label", expected: "byte-identical"},
		{description: "boundary: finite lifetimes, frozen bytes", state: "v4_lifetimes", expected: "byte-identical"},
		{description: "positive: metric 50 follows the prefix length; \"metric\":50 follows prefixlen", state: "v4_metric", expected: "byte-identical"},
		{description: "positive: v4 peer, local then address then prefixlen", state: "v4_peer", expected: "byte-identical"},
		{description: "positive: proto before the label", state: "v4_proto", expected: "byte-identical"},
		{description: "positive: a secondary address", state: "v4_secondary", expected: "byte-identical"},
		{description: "positive: an address on a VRF port", state: "v4_vrf", expected: "byte-identical"},
		{description: "corner: the netns name goip does not resolve, rewritten in text only", state: "v6_dadfailed", netns: "addrb", expected: "byte-identical"},
		{description: "positive: v6 flags between scope and the lifetime line", state: "v6_flags", expected: "byte-identical"},
		{description: "positive: optimistic", state: "v6_optimistic", expected: "byte-identical"},
		{description: "positive: v6 peer, local then address then prefixlen", state: "v6_peer", expected: "byte-identical"},
		{description: "positive: stable-privacy", state: "v6_stable_privacy", expected: "byte-identical"},
		{description: "positive: temporary", state: "v6_temporary", expected: "byte-identical"},
		{description: "positive: tentative", state: "v6_tentative", expected: "byte-identical"},
	}

	t.Run("corner: every addr-show state has a row", func(t *testing.T) {
		entries, err := os.ReadDir(addrShowDir)
		if err != nil {
			t.Fatal(err)
		}
		var dirs []string
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, e.Name())
			}
		}
		rows := make([]string, 0, len(tests))
		for _, tc := range tests {
			rows = append(rows, tc.state)
		}
		sort.Strings(rows)
		if strings.Join(dirs, " ") != strings.Join(rows, " ") {
			t.Fatalf("states on disk %v, rows %v", dirs, rows)
		}
	})

	for _, tc := range tests {
		for _, form := range []struct {
			name, pcap, sidecar string
			args                []string
		}{
			{"text", "netlink_route_getaddr.pcap", "ip_addr", []string{"addr", "show"}},
			{"json", "netlink_route_getaddr_json.pcap", "ip_addr_json", []string{"-j", "addr", "show"}},
		} {
			t.Run(tc.description+"/"+form.name, func(t *testing.T) {
				dir := addrShowDir + tc.state + "/"
				t.Setenv("GOIP_REPLAY", dir+form.pcap)
				var stdout, stderr bytes.Buffer
				if code := Run(form.args, &stdout, &stderr); code != ExitOK {
					t.Fatalf("Run(%q) = %d, stderr=%s", form.args, code, stderr.String())
				}
				var want []byte
				if tc.netns != "" && form.name == "text" {
					want = []byte(sidecarWithNetnsID(t, dir+form.sidecar, tc.netns))
				} else {
					raw, err := os.ReadFile(dir + form.sidecar)
					if err != nil {
						t.Fatal(err)
					}
					want = raw
				}
				firstByteDiff(t, stdout.Bytes(), want)
			})
		}
	}
}

// fatalRecorder is a testing.TB whose Fatal and Fatalf record the failure
// and stop the helper under test, without failing the test running it.
type fatalRecorder struct {
	testing.TB
	failed bool
}

type fatalStop struct{}

func (r *fatalRecorder) Helper() {}

func (r *fatalRecorder) Fatal(...any) {
	r.failed = true
	panic(fatalStop{})
}

func (r *fatalRecorder) Fatalf(string, ...any) {
	r.failed = true
	panic(fatalStop{})
}

// TestSidecarWithNetnsIDMustRewrite is the control for the corner row above:
// the rewrite is required to apply, so a sidecar without the name fails
// instead of passing a comparison it did not touch.
//
// go test ./internal/goip/ -run TestSidecarWithNetnsIDMustRewrite
func TestSidecarWithNetnsIDMustRewrite(t *testing.T) {
	tests := []struct {
		description string
		state       string
		netns       string
		expected    bool // whether the helper fails
	}{
		{
			description: "positive: v6_dadfailed names addrb",
			state:       "v6_dadfailed",
			netns:       "addrb",
			expected:    false,
		},
		{
			description: "negative: v6_dadfailed does not name lacpb",
			state:       "v6_dadfailed",
			netns:       "lacpb",
			expected:    true,
		},
		{
			description: "negative: v4_metric has no veth peer to rewrite",
			state:       "v4_metric",
			netns:       "addrb",
			expected:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := &fatalRecorder{TB: t}
			func() {
				defer func() {
					if p := recover(); p != nil {
						if _, ok := p.(fatalStop); !ok {
							panic(p)
						}
					}
				}()
				sidecarWithNetnsID(r, addrShowDir+tc.state+"/ip_addr", tc.netns)
			}()
			if r.failed != tc.expected {
				t.Fatalf("helper failed = %v, want %v", r.failed, tc.expected)
			}
		})
	}
}

const addrMonitorDir = linkWriteDir + "addr-monitor/mc_any/"

// monitorEvent is one RTM_{NEW,DEL}{MULTICAST,ANYCAST} notification in a
// monitor pcap: its type and decoded body.
type monitorEvent struct {
	msgType uint16
	ai      xtcpnl.AddrInfo
}

// monitorEvents decodes every kernel-originated multicast and anycast
// notification in a pcap, in capture order.
func monitorEvents(t *testing.T, path string) []monitorEvent {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, records, err := xtcpnl.ParseNetlinkPcap(raw)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var out []monitorEvent
	for _, r := range records {
		_, dgram, err := r.NetlinkPayload()
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for len(dgram) >= xtcpnl.NlMsgHdrSizeCst {
			var h xtcpnl.NlMsgHdr
			if _, err := xtcpnl.DeserializeNlMsgHdr(dgram[:xtcpnl.NlMsgHdrSizeCst], &h); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			mlen := int(h.Len)
			if mlen < xtcpnl.NlMsgHdrSizeCst || mlen > len(dgram) {
				t.Fatalf("%s: nlmsg_len %d in a %d-byte datagram", path, mlen, len(dgram))
			}
			switch h.Type {
			case unix.RTM_NEWMULTICAST, unix.RTM_DELMULTICAST, unix.RTM_NEWANYCAST, unix.RTM_DELANYCAST:
				ai, err := xtcpnl.ParseNewAddr(dgram[xtcpnl.NlMsgHdrSizeCst:mlen])
				if err != nil {
					t.Fatalf("%s: ParseNewAddr: %v", path, err)
				}
				out = append(out, monitorEvent{msgType: h.Type, ai: ai})
			}
			adv := mlen + xtcpnl.FourByteAlignPadding(mlen)
			if adv > len(dgram) {
				break
			}
			dgram = dgram[adv:]
		}
	}
	return out
}

// monitorBlock is one event as `ip monitor` printed it: the `Deleted ` and
// `N: dev` prefix apart from the print_addrinfo body after it.
type monitorBlock struct {
	deleted bool
	index   uint32
	dev     string
	body    string
}

// monitorWantBodies are the events the capture's trigger must produce, as
// "dev body": the canary on c0, then on d0 the all-hosts join on up, the
// subnet-router anycast, and the two autojoin groups. Every group header
// carries IFA_F_PERMANENT and IFA_CACHEINFO forever, and the kernel forces
// scope universe (inet6_fill_ifmcaddr's force_rt_scope_universe), so each
// is one line plus the lifetime line.
var monitorWantBodies = func() []string {
	const life = "       valid_lft forever preferred_lft forever\n"
	events := []string{
		"c0 inet mcast 224.0.0.250",
		"d0 inet mcast 224.0.0.1",
		"d0 inet6 any 2001:db8:1::",
		"d0 inet6 mcast ff02::fb",
		"d0 inet mcast 224.0.0.251",
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		dev, rest, _ := strings.Cut(e, " ")
		out = append(out, dev+"     "+rest+" scope global \n"+life)
	}
	return out
}()

var monitorPrefix = regexp.MustCompile(`^(Deleted )?(\d+): (\S+?)    `)

// monitorBlocks splits a monitor sidecar into its events. An event starts on
// a line with the prefix and runs to the next one, so a lifetime line stays
// with its address line.
func monitorBlocks(t *testing.T, path string) []monitorBlock {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []monitorBlock
	for _, line := range strings.SplitAfter(string(raw), "\n") {
		if line == "" {
			continue
		}
		m := monitorPrefix.FindStringSubmatch(line)
		if m == nil {
			if len(out) == 0 {
				t.Fatalf("%s: %q precedes the first event", path, line)
			}
			out[len(out)-1].body += line
			continue
		}
		idx, err := strconv.ParseUint(m[2], 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, monitorBlock{
			deleted: m[1] != "",
			index:   uint32(idx),
			dev:     m[3],
			body:    "    " + line[len(m[0]):],
		})
	}
	return out
}

// TestAddrMonitorMcastGoldens is goip's rendering of each multicast and
// anycast notification against what `ip monitor maddress acaddress` printed
// for it, the only `ip` that prints one (ip/ipmonitor.c:156-160).
//
// go test ./internal/goip/ -run TestAddrMonitorMcastGoldens
func TestAddrMonitorMcastGoldens(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		sidecar     string
		// contains are bodies, prefix stripped, the sidecar must have
		// among its events.
		contains []string
		expected string
	}{
		{
			description: "positive: monitor maddress acaddress, one block per notification, byte-identical bodies",
			pcap:        "netlink_route_monitor_maddr.pcap",
			sidecar:     "netlink_route_monitor_maddr.out",
			contains:    monitorWantBodies,
			expected:    "byte-identical",
		},
		{
			description: "boundary: -j monitor prints text in 7.1.0, and the same bodies",
			pcap:        "netlink_route_monitor_maddr_json.pcap",
			sidecar:     "netlink_route_monitor_maddr_json.out",
			contains:    monitorWantBodies,
			expected:    "byte-identical, and no '{' in the sidecar",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			events := monitorEvents(t, addrMonitorDir+tc.pcap)
			blocks := monitorBlocks(t, addrMonitorDir+tc.sidecar)
			if len(events) == 0 {
				t.Fatal("no multicast or anycast notification in the pcap")
			}
			if len(events) != len(blocks) {
				t.Fatalf("%d notifications, %d monitor events", len(events), len(blocks))
			}
			raw, err := os.ReadFile(addrMonitorDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.ContainsRune(raw, '{') {
				t.Errorf("the sidecar has JSON in it; 7.1.0's monitor has no JSON context")
			}
			bodies := map[string]bool{}
			for i, ev := range events {
				b := blocks[i]
				del := ev.msgType == unix.RTM_DELMULTICAST || ev.msgType == unix.RTM_DELANYCAST
				if b.deleted != del {
					t.Errorf("event %d: Deleted = %v, message type %d", i, b.deleted, ev.msgType)
				}
				if b.index != ev.ai.Index {
					t.Errorf("event %d: prefix index %d, ifa_index %d", i, b.index, ev.ai.Index)
				}
				if b.dev != "c0" && b.dev != "d0" {
					t.Errorf("event %d: dev %q, want c0 or d0", i, b.dev)
				}
				if ev.ai.Local != nil || ev.ai.Address != nil {
					t.Errorf("event %d: has IFA_LOCAL or IFA_ADDRESS, so a local block", i)
				}
				got := render.AddrViewOf(ev.ai).Text()
				if got != b.body {
					t.Errorf("event %d (type %d):\n got: %q\nwant: %q", i, ev.msgType, got, b.body)
				}
				bodies[b.dev+" "+b.body] = true
			}
			for _, group := range []string{"ff02::fb", "224.0.0.251"} {
				sawLeave := false
				for i, ev := range events {
					if blocks[i].deleted && ev.msgType == unix.RTM_DELMULTICAST &&
						strings.Contains(blocks[i].body, "mcast "+group+" ") {
						sawLeave = true
					}
				}
				if !sawLeave {
					t.Errorf("no Deleted block for the RTM_DELMULTICAST (%d) of %s", unix.RTM_DELMULTICAST, group)
				}
			}
			for _, w := range tc.contains {
				if !bodies[w] {
					t.Errorf("no event %q in %v", w, bodies)
				}
			}
		})
	}
}
