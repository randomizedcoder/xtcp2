package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The committed capture this file is driven from, and the two portids inside
// it that are `ip` runs.
//
// # This capture is polluted, and the portids are how it is made usable
//
// netlink_route_getaddr.pcap holds 251 messages. `gen_addr()` in
// nix/capture-netlink-fixtures.nix runs `ip -4 addr show` and then
// `ip -6 addr show` into the same pcap, and nlmon mirrored everything else the
// host did during the window: a third-party RTM_NEWADDR (a real write, from a
// DHCPv6 client adding the 2603:…:6adf address), a 110-reply RTM_GETNEIGH
// dump, nine RTM_GETLINK single-gets, an AF_UNSPEC link dump with no ext mask,
// and two RTM_GETADDR dumps built on rtgenmsg rather than ifaddrmsg — 20 bytes
// where iproute2 sends 24, so not iproute2 at all.
//
// The two `ip` runs are portids 106900 and 106901, and the reply counts are
// what identify them rather than a guess:
//
//	106900:  11 RTM_NEWLINK + 9 RTM_NEWADDR (all AF_INET)  + 2 NLMSG_DONE = 22
//	106901:  11 RTM_NEWLINK + 15 RTM_NEWADDR (all AF_INET6) + 2 NLMSG_DONE = 28
//
// Two dumps per run, two DONEs per run — which is itself the L1 transaction
// count for this command, observed rather than assumed. And 9 and 15 are
// exactly the number of `inet ` and `inet6 ` lines in ip_addr_n.
const (
	addrBulkPcap = "../../pkg/xtcpnl/testdata/7_1_8/netlink_route_getaddr.pcap"
	addrSidecar  = "../../pkg/xtcpnl/testdata/7_1_8/ip_addr_n"

	// portidV4Run is the `ip -4 addr show` socket, portidV6Run the
	// `ip -6 addr show` one.
	portidV4Run uint32 = 106900
	portidV6Run uint32 = 106901
)

const (
	// wantV4Addrs and wantV6Addrs are the RTM_NEWADDR reply counts of the two
	// runs, cross-checked against the sidecar's line counts.
	wantV4Addrs = 9
	wantV6Addrs = 15

	// wantAddrStanzas is how many stanzas a plain `ip addr show` prints for
	// this host: every link, because ipaddr_filter's missing_net_address
	// rescue keeps the ones with no addresses when no family was asked for.
	wantAddrStanzas = wantLinkStanzas

	// wantV4Stanzas is how many `-4 addr show` prints. Two of the eleven
	// links drop out and the two reasons are different, which is the whole
	// point of filterLinksWithAddrs: veth179a698 has an IPv6 address and no
	// IPv4 one, and nlmon0 has no addresses at all and so is not rescued
	// either, because the rescue does not apply when a family was requested.
	wantV4Stanzas = 9

	// wantV6Stanzas is how many `-6 addr show` prints. Two links drop out
	// here as well, and the second one is not the one the instinct picks:
	// nlmon0 has no addresses at all, and **virbr0 has an IPv4 address and no
	// IPv6 one**. "every link has a link-local" is not true — this libvirt
	// bridge has IPv6 disabled, so the kernel never assigned one. Measured
	// from the capture (the 15 AF_INET6 replies cover ifindexes 1, 2, 3, 4, 8,
	// 9, 58, 59 and 60, and 7 is absent) and cross-checked against ip_addr_n,
	// whose virbr0 stanza at :38-42 has one `inet` line and no `inet6`.
	//
	// This was 10 when it was reasoned about instead of counted, and the row
	// below caught it.
	wantV6Stanzas = 9
)

// compositeSource answers RTM_NEWLINK from one portid and RTM_NEWADDR from
// the union of two, which is what reconstructs a plain `ip addr show` out of a
// capture that only ever recorded `-4` and `-6`.
//
// # Why this is sound, and the one way in which it is not
//
// A plain `ip addr show` sends AF_UNSPEC for both dumps. The kernel answers an
// AF_UNSPEC RTM_GETADDR dump by walking the families in turn — all AF_INET
// addresses, then all AF_INET6 — so concatenating the v4 run's replies with
// the v6 run's reproduces the reply *sequence*, not merely the set. ip_addr_n
// confirms the ordering independently: every link's `inet` lines precede its
// `inet6` lines.
//
// For links, AF_UNSPEC and AF_INET are answered by the same kernel function
// (see xtcpnl.BuildDumpLinkRequestFamily), so the v4 run's 11 RTM_NEWLINK
// replies carry every attribute an AF_UNSPEC dump would. The AF_INET6 run's do
// not, which is why this takes links from portidV4Run specifically and why
// TestAddrShowLinkRepliesDifferByFamily asserts that the choice matters.
//
// Where it is not sound: the `-4` link dump carries no IFLA_EXT_MASK, so
// RTEXT_FILTER_SKIP_STATS is clear and its replies carry IFLA_STATS and
// IFLA_STATS64 that a real plain `addr show` would not. goip decodes neither,
// so no rendered byte depends on it — but this is a reconstruction of a
// command that was never captured, and a matched plain `addr show` capture is
// on the plan's Item 7 list.
type compositeSource struct {
	links *ReplaySource
	addrs []*ReplaySource
}

func newCompositeSource(t *testing.T) *compositeSource {
	t.Helper()
	links, err := OpenReplayPortid(addrBulkPcap, portidV4Run)
	if err != nil {
		t.Fatalf("open link replay: %v", err)
	}
	v4, err := OpenReplayPortid(addrBulkPcap, portidV4Run)
	if err != nil {
		t.Fatalf("open v4 replay: %v", err)
	}
	v6, err := OpenReplayPortid(addrBulkPcap, portidV6Run)
	if err != nil {
		t.Fatalf("open v6 replay: %v", err)
	}
	return &compositeSource{links: links, addrs: []*ReplaySource{v4, v6}}
}

// Dump satisfies Source. It also records nothing: request recording is
// recordingSource's job, kept separate so a test asserting on output cannot
// accidentally also be the test asserting on requests.
func (c *compositeSource) Dump(request []byte, msgType uint16) ([][]byte, error) {
	if msgType == uint16(unix.RTM_NEWLINK) {
		return c.links.Dump(request, msgType)
	}
	var out [][]byte
	for _, s := range c.addrs {
		bodies, err := s.Dump(request, msgType)
		if err != nil {
			return nil, err
		}
		out = append(out, bodies...)
	}
	return out, nil
}

// recordingSource wraps a Source and keeps every request it was asked to
// send, in order.
//
// This is the harness's L1 finding — positional transaction count — brought
// inside `go test`. Tier A proves each individual request is byte-correct;
// nothing there proves goip sends *two* of them, in the right order, and
// `addr show` is the first command where that can be wrong.
type recordingSource struct {
	inner    Source
	requests [][]byte
	types    []uint16
}

func (r *recordingSource) Dump(request []byte, msgType uint16) ([][]byte, error) {
	r.requests = append(r.requests, append([]byte(nil), request...))
	r.types = append(r.types, msgType)
	return r.inner.Dump(request, msgType)
}

// runAddrShow drives the handler directly with a chosen source and family.
//
// Run() cannot be used for the composite cases: it builds its own source from
// GOIP_REPLAY, and a single pcap path cannot express "links from this portid,
// addresses from those two". TestRunAddrArgs below covers the Run() path,
// where the argv parsing lives, so both halves are exercised — this one just
// does not pretend to exercise the half it skips.
func runAddrShow(t *testing.T, src Source, family uint8, wantJSON bool) string {
	t.Helper()
	var out bytes.Buffer
	c := &runCtx{
		src:    src,
		lltab:  NewLLTab(),
		out:    &out,
		errOut: &bytes.Buffer{},
		json:   wantJSON,
		family: family,
	}
	if err := addrShow(c, nil); err != nil {
		t.Fatalf("addrShow(family=%d): %v", family, err)
	}
	return out.String()
}

// secRe matches the lifetime values that legitimately differ between the pcap
// and the sidecar. See TestAddrShowMatchesCapturedOutput.
var secRe = regexp.MustCompile(`[0-9]+sec`)

// TestAddrShowMatchesCapturedOutput is §8.7's addr row: a plain `ip addr show`
// rendered from committed replies, compared line by line against the
// reconstructed sidecar.
//
// # Four lines cannot be byte-compared, and the reason is a clock
//
// `cap netlink_route_getaddr` ran first in the capture script and the sidecars
// were written after all three captures (nix/capture-netlink-fixtures.nix:
// :109-111 vs :128-131), so a few seconds of wall clock separate the pcap from
// `ip_addr_n`. Every address with a finite lifetime therefore reads differently
// in the two:
//
//	sidecar ip_addr_n:11   valid_lft 47871sec preferred_lft 47871sec
//	pcap    RTM_NEWADDR    valid 47876          preferred 47876
//
// That is 5 seconds of DHCP lease, not a rendering bug. Four lines are
// affected — the lifetime lines of the DHCP v4 address and the three dynamic
// v6 addresses on enp1s0 — and they are compared with the digit runs before
// "sec" normalized away.
//
// The relaxation is dangerous exactly to the extent that it can spread, so the
// number of lines that needed it is asserted as its own row. A change that
// made ten lines need it would fail there rather than passing quietly.
//
// go test ./internal/goip/ -run TestAddrShowMatchesCapturedOutput
func TestAddrShowMatchesCapturedOutput(t *testing.T) {
	want := plainFromSidecar(t, addrSidecar)
	got := strings.Split(strings.TrimSuffix(runAddrShow(t, newCompositeSource(t), unix.AF_UNSPEC, false), "\n"), "\n")
	lineNos := sidecarLineNumbers(t, addrSidecar)

	t.Run(fmt.Sprintf("positive: output has %d stanzas, one per link", wantAddrStanzas), func(t *testing.T) {
		n := 0
		for _, l := range got {
			if !strings.HasPrefix(l, " ") {
				n++
			}
		}
		if n != wantAddrStanzas {
			t.Errorf("got %d stanzas, want %d", n, wantAddrStanzas)
		}
	})

	t.Run("positive: line count matches the reconstruction", func(t *testing.T) {
		if len(got) != len(want) {
			t.Fatalf("got %d lines, want %d\n got: %q\nwant: %q", len(got), len(want), got, want)
		}
	})
	if len(got) != len(want) {
		return
	}

	relaxed := 0
	for i := range want {
		i := i
		name := fmt.Sprintf("positive: output line %d matches ip_addr_n:%d", i+1, lineNos[i])
		if want[i] != got[i] && secRe.ReplaceAllString(want[i], "Nsec") == secRe.ReplaceAllString(got[i], "Nsec") {
			relaxed++
			name = fmt.Sprintf("boundary: output line %d matches ip_addr_n:%d up to the lifetime clock", i+1, lineNos[i])
		}
		t.Run(name, func(t *testing.T) {
			if want[i] == got[i] {
				return
			}
			if secRe.ReplaceAllString(want[i], "Nsec") == secRe.ReplaceAllString(got[i], "Nsec") {
				return
			}
			t.Errorf("line %d mismatch\n got: %q\nwant: %q", i+1, got[i], want[i])
		})
	}

	// The guard on the relaxation. 4 is measured: the DHCP v4 lease on
	// enp1s0 (ip_addr_n:11) and the three dynamic v6 addresses on the same
	// link (:13, :15, :17).
	t.Run("boundary: exactly 4 lines needed the lifetime relaxation", func(t *testing.T) {
		if relaxed != 4 {
			t.Errorf("%d lines needed the lifetime relaxation, want 4 — "+
				"a change in this number means either the fixture was re-taken or "+
				"the relaxation is now hiding a real divergence", relaxed)
		}
	})
}

// TestAddrShowLinkRepliesDifferByFamily is the measurement the whole
// family-threading design rests on, asserted rather than trusted.
//
// The same host, the same moment, two RTM_GETLINK dumps differing only in
// ifi_family, and two completely different reply shapes — because the kernel
// dispatches RTM_GETLINK on the family byte and net/ipv6 registers its own
// dumpit. If this ever stops being true, LinkInfo.HasTxQLen, HasGroup,
// LinkViewForAddr and half of req.AddrShowLinkDump's doc comment are all
// solving a problem that no longer exists, and this row is where that shows up.
//
// go test ./internal/goip/ -run TestAddrShowLinkRepliesDifferByFamily
func TestAddrShowLinkRepliesDifferByFamily(t *testing.T) {
	v4, err := OpenReplayPortid(addrBulkPcap, portidV4Run)
	if err != nil {
		t.Fatalf("open v4 replay: %v", err)
	}
	v6, err := OpenReplayPortid(addrBulkPcap, portidV6Run)
	if err != nil {
		t.Fatalf("open v6 replay: %v", err)
	}
	v4Links, err := v4.Dump(nil, uint16(unix.RTM_NEWLINK))
	if err != nil {
		t.Fatalf("v4 link dump: %v", err)
	}
	v6Links, err := v6.Dump(nil, uint16(unix.RTM_NEWLINK))
	if err != nil {
		t.Fatalf("v6 link dump: %v", err)
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: both runs enumerated the same 11 links",
			check: func(t *testing.T) {
				if len(v4Links) != wantLinkStanzas || len(v6Links) != wantLinkStanzas {
					t.Errorf("got %d v4 and %d v6 link replies, want %d each",
						len(v4Links), len(v6Links), wantLinkStanzas)
				}
			},
		},
		{
			description: "positive: the AF_INET6 replies are materially shorter, not merely reordered",
			check: func(t *testing.T) {
				for i := range v6Links {
					if len(v6Links[i]) >= len(v4Links[i]) {
						t.Errorf("link %d: v6 body %d bytes, v4 %d — expected the "+
							"inet6_fill_ifinfo reply to be the smaller one",
							i, len(v6Links[i]), len(v4Links[i]))
					}
				}
			},
		},
		{
			description: "positive: the AF_INET reply carries qdisc, txqlen and group; the AF_INET6 one carries none of them",
			check: func(t *testing.T) {
				v4lo := parseLink(t, v4Links[0])
				v6lo := parseLink(t, v6Links[0])
				if v4lo.Qdisc == "" || !v4lo.HasTxQLen || !v4lo.HasGroup {
					t.Errorf("AF_INET lo: qdisc=%q HasTxQLen=%v HasGroup=%v, want all present",
						v4lo.Qdisc, v4lo.HasTxQLen, v4lo.HasGroup)
				}
				if v6lo.Qdisc != "" || v6lo.HasTxQLen || v6lo.HasGroup {
					t.Errorf("AF_INET6 lo: qdisc=%q HasTxQLen=%v HasGroup=%v, want all absent",
						v6lo.Qdisc, v6lo.HasTxQLen, v6lo.HasGroup)
				}
			},
		},
		{
			// The coupling from the link work, reached a second way. All three
			// veths carry IFLA_LINK_NETNSID in the AF_INET reply and none of
			// them do in the AF_INET6 one, so `-4 addr show` prints `@if2` and
			// `-6 addr show` resolves the peer and prints `@enp1s0`. Same host,
			// same instant, same link — different suffix, because of an
			// attribute the kernel chose not to send.
			description: "corner: IFLA_LINK survives into the AF_INET6 reply but IFLA_LINK_NETNSID does not",
			check: func(t *testing.T) {
				v4n, v6n := 0, 0
				for i := range v4Links {
					if parseLink(t, v4Links[i]).HasLinkNetnsID {
						v4n++
					}
					if l := parseLink(t, v6Links[i]); l.HasLinkNetnsID {
						v6n++
					} else if l.Link != 0 {
						// IFLA_LINK present without the netnsid: the
						// resolving path.
						continue
					}
				}
				if v4n != 3 || v6n != 0 {
					t.Errorf("links carrying IFLA_LINK_NETNSID: %d under AF_INET, %d under AF_INET6; want 3 and 0", v4n, v6n)
				}
			},
		},
		{
			description: "boundary: the AF_INET6 replies still carry a name, an MTU and an operstate",
			check: func(t *testing.T) {
				for i := range v6Links {
					l := parseLink(t, v6Links[i])
					if l.Name == "" || l.MTU == 0 {
						t.Errorf("AF_INET6 link %d: name=%q mtu=%d, want both set (inet6_fill_ifinfo emits IFLA_IFNAME and IFLA_MTU)", i, l.Name, l.MTU)
					}
				}
			},
		},
		{
			description: "negative: no AF_INET6 reply carries an altname, so no altname line can be printed",
			check: func(t *testing.T) {
				for i := range v6Links {
					if alt := parseLink(t, v6Links[i]).AltNames; len(alt) != 0 {
						t.Errorf("AF_INET6 link %d carries altnames %q, want none", i, alt)
					}
				}
				// Non-vacuity: the AF_INET side does carry them.
				n := 0
				for i := range v4Links {
					n += len(parseLink(t, v4Links[i]).AltNames)
				}
				if n != 4 {
					t.Errorf("AF_INET replies carry %d altnames, want 4 — if this is 0 the row above proves nothing", n)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, tc.check)
	}
}

// TestAddrShowFamilyRendering asserts the three structurally different
// renderings the one command has, over the committed replies.
//
// # Why these are structural rows and not a sidecar comparison
//
// Only the AF_UNSPEC form has a sidecar: `ip_addr_n` was written by
// `ip -d addr show`. Deriving a `-4` expectation from it would mean
// reconstructing three transformations at once — drop the link/ line, move
// link-netnsid up to the stanza line, drop the v6 stanzas and the links that
// have no v4 address — and an expectation built from three of my own
// transformations is not an independent witness. So the AF_UNSPEC row keeps
// the byte comparison (in the test above) and these rows assert the specific
// differences, each cited to the guard in print_linkinfo that causes it.
//
// go test ./internal/goip/ -run TestAddrShowFamilyRendering
func TestAddrShowFamilyRendering(t *testing.T) {
	v4Src, err := OpenReplayPortid(addrBulkPcap, portidV4Run)
	if err != nil {
		t.Fatalf("open v4 replay: %v", err)
	}
	v6Src, err := OpenReplayPortid(addrBulkPcap, portidV6Run)
	if err != nil {
		t.Fatalf("open v6 replay: %v", err)
	}

	unspec := runAddrShow(t, newCompositeSource(t), unix.AF_UNSPEC, false)
	v4 := runAddrShow(t, v4Src, unix.AF_INET, false)
	v6 := runAddrShow(t, v6Src, unix.AF_INET6, false)

	stanzas := func(out string) []string {
		var s []string
		for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
			if !strings.HasPrefix(l, " ") {
				s = append(s, l)
			}
		}
		return s
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			// ip_addr_n:1-2. The guard is AF_UNSPEC-or-AF_PACKET-or-detail, so
			// only the plain form opens a second line.
			description: "positive: AF_UNSPEC prints the `link/` line",
			check: func(t *testing.T) {
				if !strings.Contains(unspec, "\n    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00\n") {
					t.Errorf("AF_UNSPEC output has no lo link/ line:\n%s", firstLines(unspec, 4))
				}
			},
		},
		{
			description: "positive: AF_INET omits the `link/` line entirely",
			check: func(t *testing.T) {
				if strings.Contains(v4, "    link/") {
					t.Errorf("AF_INET output contains a link/ line:\n%s", firstLines(v4, 4))
				}
			},
		},
		{
			description: "positive: AF_INET6 omits the `link/` line entirely",
			check: func(t *testing.T) {
				if strings.Contains(v6, "    link/") {
					t.Errorf("AF_INET6 output contains a link/ line:\n%s", firstLines(v6, 4))
				}
			},
		},
		{
			// **The row for the print that moved.** IFLA_LINK_NETNSID is
			// printed after the guard closes (ip/ipaddress.c:1114), so with no
			// link/ line to land on it appends to the stanza. Verified against
			// the pinned `ip`, whose `-4` stanza for a veth ends
			// "qlen 1000 link-netnsid 0".
			description: "corner: under AF_INET, link-netnsid moves onto the stanza line",
			check: func(t *testing.T) {
				var found string
				for _, s := range stanzas(v4) {
					if strings.HasPrefix(s, "58: ve-nfb-vpn@if2:") {
						found = s
					}
				}
				if found == "" {
					t.Fatalf("no ve-nfb-vpn stanza in AF_INET output:\n%s", v4)
				}
				if !strings.HasSuffix(found, " link-netnsid 1") {
					t.Errorf("AF_INET stanza = %q, want it to end in ` link-netnsid 1`", found)
				}
			},
		},
		{
			description: "corner: under AF_UNSPEC the same link-netnsid stays on the `link/` line",
			check: func(t *testing.T) {
				for _, s := range stanzas(unspec) {
					if strings.HasPrefix(s, "58: ve-nfb-vpn@if2:") && strings.Contains(s, "link-netnsid") {
						t.Errorf("AF_UNSPEC stanza carries link-netnsid, want it on the link/ line: %q", s)
					}
				}
				if !strings.Contains(unspec, "brd ff:ff:ff:ff:ff:ff link-netnsid 1\n") {
					t.Errorf("AF_UNSPEC output has no link/ line ending in link-netnsid 1")
				}
			},
		},
		{
			// The stanza line under -6 loses qdisc and group because the
			// attributes are not in the reply, not because of any guard.
			description: "positive: AF_INET6 stanzas carry neither qdisc nor group",
			check: func(t *testing.T) {
				for _, s := range stanzas(v6) {
					if strings.Contains(s, "qdisc ") || strings.Contains(s, "group ") {
						t.Errorf("AF_INET6 stanza %q carries qdisc or group, but inet6_fill_ifinfo sends neither attribute", s)
					}
				}
			},
		},
		{
			// **The known divergence, asserted as a divergence.** The pinned
			// `ip` prints "qlen 1000" here from a SIOCGIFTXQLEN ioctl; goip
			// prints nothing, and the fork deleted the ioctl. Written as a
			// test so the divergence is a fact with a row rather than a note
			// in a doc comment.
			description: "negative: AF_INET6 stanzas carry no qlen either, which is the faceb326 ioctl divergence",
			check: func(t *testing.T) {
				for _, s := range stanzas(v6) {
					if strings.Contains(s, "qlen ") {
						t.Errorf("AF_INET6 stanza %q carries qlen; goip does not do the SIOCGIFTXQLEN fallback", s)
					}
				}
				// Non-vacuity: the AF_UNSPEC form does print qlen.
				if !strings.Contains(unspec, "qlen 1000") {
					t.Error("AF_UNSPEC output has no qlen at all, so the row above proves nothing")
				}
			},
		},
		{
			// print_linkmode is do_link-gated, so no addr form prints mode.
			description: "negative: no addr form prints `mode DEFAULT`, which only `link show` does",
			check: func(t *testing.T) {
				for name, out := range map[string]string{"AF_UNSPEC": unspec, "AF_INET": v4, "AF_INET6": v6} {
					if strings.Contains(out, "mode DEFAULT") {
						t.Errorf("%s output contains `mode DEFAULT`", name)
					}
				}
			},
		},
		{
			// The coupling reached through rendering rather than through the
			// decoder: with no IFLA_LINK_NETNSID, print_name_and_link resolves
			// IFLA_LINK through the index cache instead of printing if%u.
			description: "corner: AF_INET6 renders the veth peer by name, AF_INET renders it as if%u",
			check: func(t *testing.T) {
				if !strings.Contains(v6, "58: ve-nfb-vpn@enp1s0:") {
					t.Errorf("AF_INET6 output has no `ve-nfb-vpn@enp1s0` stanza:\n%s", firstLines(v6, 30))
				}
				if !strings.Contains(v4, "58: ve-nfb-vpn@if2:") {
					t.Errorf("AF_INET output has no `ve-nfb-vpn@if2` stanza")
				}
			},
		},
		{
			// ipaddr_filter, the two-variable condition. Three different
			// outcomes from one rule, all on real fixture data.
			description: "boundary: the three families print 11, 9 and 9 stanzas",
			check: func(t *testing.T) {
				for _, c := range []struct {
					name string
					out  string
					want int
				}{
					{"AF_UNSPEC", unspec, wantAddrStanzas},
					{"AF_INET", v4, wantV4Stanzas},
					{"AF_INET6", v6, wantV6Stanzas},
				} {
					if n := len(stanzas(c.out)); n != c.want {
						t.Errorf("%s printed %d stanzas, want %d", c.name, n, c.want)
					}
				}
			},
		},
		{
			// The rescue clause, isolated. nlmon0 has no addresses of any
			// family, so `missing_net_address` stays set and it survives only
			// when no family was asked for.
			description: "corner: nlmon0 appears under AF_UNSPEC and under neither -4 nor -6",
			check: func(t *testing.T) {
				if !strings.Contains(unspec, "161: nlmon0:") {
					t.Error("AF_UNSPEC output is missing nlmon0, which ipaddr_filter's missing_net_address rescue keeps")
				}
				if strings.Contains(v4, "nlmon0") || strings.Contains(v6, "nlmon0") {
					t.Error("a family-filtered output contains nlmon0, which has no addresses to match")
				}
			},
		},
		{
			// The other half of the same condition, and the one a
			// single-variable implementation gets wrong: this link *has*
			// addresses, just none of the requested family, so the rescue must
			// not apply.
			description: "corner: veth179a698 has only an IPv6 address, so -4 drops it and -6 keeps it",
			check: func(t *testing.T) {
				if strings.Contains(v4, "veth179a698") {
					t.Error("AF_INET output contains veth179a698, which has no IPv4 address")
				}
				if !strings.Contains(v6, "veth179a698") {
					t.Error("AF_INET6 output is missing veth179a698, which has a link-local address")
				}
			},
		},
		{
			// The mirror image, and the one that disproves "every link has a
			// link-local": virbr0 has an IPv4 address and no IPv6 one at all,
			// so it is `-4`-only for the same reason veth179a698 is
			// `-6`-only. Having both directions in the table is what stops
			// the filter being right by accident.
			description: "corner: virbr0 has only an IPv4 address, so -6 drops it and -4 keeps it",
			check: func(t *testing.T) {
				if !strings.Contains(v4, "virbr0") {
					t.Error("AF_INET output is missing virbr0, which has 192.168.122.1/24")
				}
				if strings.Contains(v6, "virbr0") {
					t.Error("AF_INET6 output contains virbr0, which has no IPv6 address")
				}
			},
		},
		{
			description: "negative: no AF_INET output line is an inet6 address, and vice versa",
			check: func(t *testing.T) {
				if strings.Contains(v4, "    inet6 ") {
					t.Error("AF_INET output contains an inet6 line")
				}
				if strings.Contains(v6, "    inet ") {
					t.Error("AF_INET6 output contains an inet line")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, tc.check)
	}
}

// TestRunAddrArgs covers the Run() path: option and verb parsing, exit codes,
// and the refusals.
//
// go test ./internal/goip/ -run TestRunAddrArgs
func TestRunAddrArgs(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantExit    int
		wantOutHas  string
		wantErrHas  string
	}{
		{
			description: "positive: `addr show` renders from the capture",
			args:        []string{"-4", "addr", "show"},
			wantExit:    ExitOK,
			wantOutHas:  "    inet 127.0.0.1/8 scope host lo\n",
		},
		{
			description: "positive: a bare `address` is a show, as do_ipaddr's argc<1 branch is",
			args:        []string{"-4", "address"},
			wantExit:    ExitOK,
			wantOutHas:  "1: lo: ",
		},
		{
			// matches() again: "a" is the first table entry and "s" resolves
			// to show before save.
			description: "positive: `a s` abbreviates to `address show`",
			args:        []string{"-4", "a", "s"},
			wantExit:    ExitOK,
			wantOutHas:  "1: lo: ",
		},
		{
			description: "positive: `addr lst` and `addr list` are both shows",
			args:        []string{"-4", "addr", "lst"},
			wantExit:    ExitOK,
			wantOutHas:  "1: lo: ",
		},
		{
			// **The row that matters most in this table.** `ip a add` is a
			// write. goip must refuse it by name rather than fall through to
			// anything, and the exit code has to differ from a successful
			// show.
			description: "negative: `addr add` is refused as read-only, not run",
			args:        []string{"addr", "add"},
			wantExit:    ExitUsage,
			wantErrHas:  "read-only",
		},
		{
			description: "negative: `addr delete` is refused as read-only",
			args:        []string{"addr", "del"},
			wantExit:    ExitUsage,
			wantErrHas:  "read-only",
		},
		{
			description: "negative: `addr flush` is refused as read-only",
			args:        []string{"addr", "flush"},
			wantExit:    ExitUsage,
			wantErrHas:  "read-only",
		},
		{
			// save/showdump/restore are reads, so they get the plain
			// not-implemented message rather than the read-only one — the
			// distinction is the point.
			description: "negative: `addr save` is not implemented, but not called read-only",
			args:        []string{"addr", "save"},
			wantExit:    ExitUsage,
			wantErrHas:  "not implemented",
		},
		{
			// This row used to assert the opposite — that `dev` was refused
			// along with every other filter. The refusal was right while the
			// request was unfiltered, because answering a filtered query with
			// an unfiltered dump is a wrong answer; now that the three
			// transactions exist, the refusal would be the wrong answer.
			description: "positive: `dev NAME` is served rather than refused, and renders that link",
			args:        []string{"-4", "addr", "show", "dev", "lo"},
			wantExit:    ExitOK,
			wantOutHas:  "1: lo: <LOOPBACK,UP,LOWER_UP>",
		},
		{
			description: "negative: a filter argument other than `dev` is still refused rather than silently ignored",
			args:        []string{"-4", "addr", "show", "scope", "host"},
			wantExit:    ExitUsage,
			wantErrHas:  "not implemented",
		},
		{
			// len(args) == 1, so the dev arm does not match and the generic
			// refusal names the argument. `ip` fails here too, with
			// "Command line is not complete" out of NEXT_ARG().
			description: "negative: `dev` with no name is refused, not read as a device called \"dev\"",
			args:        []string{"addr", "show", "dev"},
			wantExit:    ExitUsage,
			wantErrHas:  "not implemented",
		},
		{
			// `ip` compares this keyword with strcmp and not matches()
			// (ip/ipaddress.c:2241), so `d` is NOT an abbreviation of `dev`:
			// it falls through to the else-arm as a device NAME, and the
			// following `lo` then trips duparg2. goip must not accept as a
			// keyword what `ip` reads as a device.
			description: "negative: `d lo` is not `dev lo`, because iproute2 matches this keyword with strcmp",
			args:        []string{"addr", "show", "d", "lo"},
			wantExit:    ExitUsage,
			wantErrHas:  "not implemented",
		},
		{
			// `ip addr show lo` works — the else-arm takes a bare name. goip
			// refuses it deliberately: that arm is reached only after every
			// keyword test above it has failed, so implementing it before
			// `scope`, `up`, `label` and the rest would turn
			// `goip addr show up` from an honest refusal into
			// `Device "up" does not exist`.
			description: "negative: a bare device name is refused, because the keywords it must not shadow are not implemented",
			args:        []string{"addr", "show", "lo"},
			wantExit:    ExitUsage,
			wantErrHas:  "not implemented",
		},
		{
			description: "negative: an unknown verb is refused",
			args:        []string{"addr", "zzz"},
			wantExit:    ExitUsage,
			wantErrHas:  "not implemented",
		},
		{
			// `ip a r` is replace — a write — so this must not be read as
			// anything goip runs.
			description: "corner: `a r` resolves to replace and is refused, not treated as a show",
			args:        []string{"addr", "r"},
			wantExit:    ExitUsage,
			wantErrHas:  "read-only",
		},
		{
			// maddress precedes nothing that "m" would otherwise hit, but
			// `address` is first in the object table so "a" can never mean
			// addrlabel. Included because the object table's order is what
			// makes that true.
			description: "corner: `a` is address, never addrlabel, because address is first in the table",
			args:        []string{"-4", "a", "s"},
			wantExit:    ExitOK,
			wantOutHas:  "    inet ",
		},
		{
			description: "boundary: -6 selects the v6 run and renders inet6 lines only",
			args:        []string{"-6", "addr", "show"},
			wantExit:    ExitOK,
			wantOutHas:  "    inet6 ::1/128 scope host noprefixroute \n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			// The portid is chosen per row by the family flag in args, because
			// each portid recorded exactly one of the two runs. A row that
			// asked for -4 against the v6 portid would be testing a mismatch
			// nothing produces.
			portid := portidV4Run
			for _, a := range tc.args {
				if a == "-6" {
					portid = portidV6Run
				}
			}
			t.Setenv("GOIP_REPLAY", addrBulkPcap)
			t.Setenv("GOIP_REPLAY_PORTID", fmt.Sprintf("%d", portid))

			var out, errOut bytes.Buffer
			code := Run(tc.args, &out, &errOut)
			if code != tc.wantExit {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tc.wantExit, errOut.String())
			}
			if tc.wantOutHas != "" && !strings.Contains(out.String(), tc.wantOutHas) {
				t.Errorf("stdout does not contain %q\ngot:\n%s", tc.wantOutHas, firstLines(out.String(), 6))
			}
			if tc.wantErrHas != "" && !strings.Contains(errOut.String(), tc.wantErrHas) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), tc.wantErrHas)
			}
		})
	}
}

// TestAddrShowJSONMatchesCapturedSidecars compares `goip -json addr show`
// against the `ip -j -p addr show` sidecar captured from the same guest
// namespace at the same moment as the pcap it replays.
//
// # This is the comparison that found the flags divergence
//
// dumps/ip_addr_json has been committed since the in-guest capture was
// written (capture-netlink-dumps.exp:207) and nothing read it. The first run
// of this test failed on two v6 addresses: the sidecar has
//
//	"nodad": true, "mngtmpaddr": true, "noprefixroute": true
//
// where goip had `"flags": ["nodad","mngtmpaddr","noprefixroute"]`. The array
// was deliberate and documented as such in render/addr.go — see the
// AddrView.Flags comment for why the justification did not survive contact
// with the golden. print_ifa_flags emits print_bool(PRINT_JSON, name, NULL,
// true) per flag (ip/ipaddress.c:1434-1435).
//
// Everything else matched on the first run, which is what makes the one
// failure a finding rather than a sign the comparison is too strict.
//
// go test ./internal/goip/ -run TestAddrShowJSONMatchesCapturedSidecars
func TestAddrShowJSONMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		sidecar     string
		// args defaults to the plain `-json addr show`. The `-s` rows set it,
		// because the stats block is the only thing a global option adds to
		// this command's JSON.
		args []string
		// zeroCounters replaces every number under a `stats`/`stats64` key
		// with 0 on both sides. Set on the `-s` rows alone, whose sidecar is
		// a `side` capture — a second invocation, so its counters have
		// ticked. See zeroLinkStats.
		zeroCounters bool
	}{
		{
			// Five links, and the topology that has a link with no address
			// at all — the case where addr_info is an empty array rather
			// than an absent key.
			description: "positive: the clean topology reproduces ip_addr_json key for key",
			pcap:        guestDumpsDir + "netlink_route_getaddr.pcap",
			sidecar:     "ip_addr_json",
		},
		{
			// The mesh namespace is where the hand-configured 2001:db8::
			// addresses live, and they are the only ones in either corpus
			// carrying IFA_F_NODAD or IFA_F_MANAGETEMPADDR — so this is the
			// row that fails when the flag keys are wrong, and the row above
			// is the one that shows everything else still agrees.
			description: "positive: the mesh topology reproduces mesh/ip_addr_json, nodad and mngtmpaddr included",
			pcap:        guestDumpsDir + "mesh/netlink_route_getaddr.pcap",
			sidecar:     "mesh/ip_addr_json",
		},
		{
			// This row exists to falsify a design decision, not to confirm
			// one. `ip addr show` prints the SAME link header as `ip link
			// show`, so all thirteen tunnel devices carry "link": null here
			// too — and render.LinkTarget is a field type rather than a
			// MarshalJSON on LinkView precisely because AddrGroupView embeds
			// LinkView and encoding/json gives an embedded type's MarshalJSON
			// precedence over field promotion (render/link.go:101-111).
			//
			// If that reasoning were wrong, this is where it shows: the whole
			// addr_info array would vanish and every entry would marshal to a
			// bare link object. gre1 is the device that makes the check bite,
			// because it is the only UP tunnel and carries three addresses —
			// a v4, a nodad v6, and the kernel_ll fe80::5efe:c000:203 that
			// only a SIT-style device generates.
			description: "positive: the tunnel topology reproduces tunnel/ip_addr_json, so null link and addr_info coexist",
			pcap:        guestDumpsDir + "tunnel/netlink_route_getaddr.pcap",
			sidecar:     "tunnel/ip_addr_json",
		},
		{
			// The `-s` form, and what it asserts beyond the text goldens is
			// the KEY: `stats64`, because IFLA_STATS64 was on the wire. The
			// text block is identical whichever attribute it came from, so
			// which of the two keys a document carries is invisible there and
			// is the whole of PR A's StatsIs64 trap.
			//
			// Values are zeroed, so this is a claim about shape. They are
			// pinned exactly by TestAddrShowStatsMatchesCapturedSidecars,
			// whose sidecar came from the same invocation as its pcap.
			description:  "positive: -s -json addr show reproduces ip_addr_stats_json's shape, keyed stats64",
			pcap:         addrStatsPcap,
			sidecar:      "ip_addr_stats_json",
			args:         []string{"-s", "-json", "addr", "show"},
			zeroCounters: true,
		},
		{
			description:  "positive: the mesh topology reproduces mesh/ip_addr_stats_json, including the link with no addresses",
			pcap:         guestDumpsDir + "mesh/netlink_route_getaddr_stats.pcap",
			sidecar:      "mesh/ip_addr_stats_json",
			args:         []string{"-s", "-json", "addr", "show"},
			zeroCounters: true,
		},
		{
			description:  "positive: the tunnel topology reproduces tunnel/ip_addr_stats_json over thirteen devices",
			pcap:         guestDumpsDir + "tunnel/netlink_route_getaddr_stats.pcap",
			sidecar:      "tunnel/ip_addr_stats_json",
			args:         []string{"-s", "-json", "addr", "show"},
			zeroCounters: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(guestDumpsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			args := tc.args
			if args == nil {
				args = []string{"-json", "addr", "show"}
			}
			if code := Run(args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
			}
			got := stdout.Bytes()
			if tc.zeroCounters {
				got, want = zeroLinkStats(t, got), zeroLinkStats(t, want)
			}
			assertJSONEntriesEqual(t, got, want, false)
		})
	}
}

// TestAddrShowTextMatchesCapturedSidecars diffs `goip addr show`'s TEXT output
// against the plain `ip addr show` sidecar from the same capture, line by line.
//
// # Why this did not exist before
//
// dumps/ip_addr, mesh/ip_addr and tunnel/ip_addr have all been committed since
// their captures were written and nothing read any of them — the addr object
// had a JSON sidecar comparison and no text one, which is the gap that let the
// flags divergence live in the JSON half undetected until
// TestAddrShowJSONMatchesCapturedSidecars was written. This is the same debt on
// the other encoding.
//
// # What the text form asserts that the JSON form cannot
//
// `ip addr show` prints the link header through the same code as `ip link
// show` and then indents an addr stanza under it, so the text is the only
// place two layout facts are visible:
//
//   - the `inet`/`inet6` continuation lines' indentation and their trailing
//     `valid_lft forever preferred_lft forever`, which in JSON are separate
//     numeric keys and carry no layout at all;
//   - `@NONE` and ` permaddr `, which are text-only for the reasons recorded on
//     TestLinkShowTextMatchesCapturedSidecars, now asserted a second time on a
//     code path that reaches them through AddrGroupView rather than LinkView.
//
// The clean and mesh rows are controls on the same argument as the link test:
// with the tunnel row alone a failure cannot be attributed between "the tunnel
// work is wrong" and "goip's plain addr text has always differed".
//
// # The per-family rows, and why they are not duplicates of the AF_UNSPEC ones
//
// `ip -4 addr show` and `ip -6 addr show` are answered by DIFFERENT kernel
// functions than the bare form: the v6 link dump goes to inet6_dump_ifinfo
// (net/ipv6/addrconf.c), which emits six attribute types and no IFLA_TXQLEN,
// no IFLA_LINKINFO, no IFLA_STATS64. So the same three topologies rendered
// per-family exercise a reply shape the AF_UNSPEC rows never see, and the
// mesh and tunnel sidecars for it were committed unread.
//
// The v6 rows need sidecarWithoutIoctlQlen for the reason recorded on that
// helper: iproute2 7.1.0 fills the absent IFLA_TXQLEN in from
// ioctl(SIOCGIFTXQLEN), which a netlink-only goip cannot see. That is the
// pre-existing `stdout:keyword:qlen` allowlist entry, not a new divergence,
// and the helper fails if there is nothing to remove.
//
// go test ./internal/goip/ -run TestAddrShowTextMatchesCapturedSidecars
func TestAddrShowTextMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		sidecar     string
		// args defaults to the AF_UNSPEC form.
		args []string
		// dropIoctlQlen removes the `qlen 1000` that 7.1.0's print_queuelen
		// supplies from an ioctl when IFLA_TXQLEN is absent from the reply.
		dropIoctlQlen bool
	}{
		{
			description: "control: the clean topology reproduces ip_addr line for line",
			pcap:        guestDumpsDir + "netlink_route_getaddr.pcap",
			sidecar:     "ip_addr",
		},
		{
			description: "control: the mesh topology reproduces mesh/ip_addr line for line",
			pcap:        guestDumpsDir + "mesh/netlink_route_getaddr.pcap",
			sidecar:     "mesh/ip_addr",
		},
		{
			// gre1 is the only tunnel carrying addresses, so it is the only
			// device here whose link header and addr stanzas must both be
			// right at once — @NONE and `peer 198.51.100.3` on the first
			// line, three indented stanzas under it.
			description: "positive: the tunnel topology reproduces tunnel/ip_addr, @NONE header over gre1's stanzas",
			pcap:        guestDumpsDir + "tunnel/netlink_route_getaddr.pcap",
			sidecar:     "tunnel/ip_addr",
		},
		{
			// The -4 arm suppresses the `link/` line entirely — the guard at
			// ip/ipaddress.c:1060 wants AF_UNSPEC or -d, and this is
			// neither — so these rows are the only committed evidence of the
			// header without it.
			description: "boundary: -4 addr show on the mesh topology, where the link/ line is suppressed",
			pcap:        guestDumpsDir + "mesh/netlink_route_getaddr_v4.pcap",
			sidecar:     "mesh/ip_addr_v4",
			args:        []string{"-4", "addr", "show"},
		},
		{
			description: "boundary: -4 addr show on the tunnel topology, fourteen devices with one address between them",
			pcap:        guestDumpsDir + "tunnel/netlink_route_getaddr_v4.pcap",
			sidecar:     "tunnel/ip_addr_v4",
			args:        []string{"-4", "addr", "show"},
		},
		{
			// And the -6 arm, whose replies come from inet6_dump_ifinfo.
			description:   "corner: -6 addr show on the mesh topology, off a reply with six attribute types",
			pcap:          guestDumpsDir + "mesh/netlink_route_getaddr_v6.pcap",
			sidecar:       "mesh/ip_addr_v6",
			args:          []string{"-6", "addr", "show"},
			dropIoctlQlen: true,
		},
		{
			description:   "corner: -6 addr show on the tunnel topology, where ip6tnl1 and ip6gre1 carry v6 addresses",
			pcap:          guestDumpsDir + "tunnel/netlink_route_getaddr_v6.pcap",
			sidecar:       "tunnel/ip_addr_v6",
			args:          []string{"-6", "addr", "show"},
			dropIoctlQlen: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			raw, err := os.ReadFile(guestDumpsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			want := string(raw)
			if tc.dropIoctlQlen {
				want = sidecarWithoutIoctlQlen(t, want)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			args := tc.args
			if args == nil {
				args = []string{"addr", "show"}
			}
			if code := Run(args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
			}
			assertLinesEqual(t, stdout.String(), want)
		})
	}
}

// TestAddrShowJSON asserts the `-j` form against the text form over the same
// replies, so the two cannot drift.
//
// go test ./internal/goip/ -run TestAddrShowJSON
func TestAddrShowJSON(t *testing.T) {
	text := runAddrShow(t, newCompositeSource(t), unix.AF_UNSPEC, false)
	raw := runAddrShow(t, newCompositeSource(t), unix.AF_UNSPEC, true)

	var groups []map[string]any
	if err := json.Unmarshal([]byte(raw), &groups); err != nil {
		t.Fatalf("unmarshal -j output: %v\n%s", err, firstLines(raw, 3))
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: the JSON array holds one object per link",
			check: func(t *testing.T) {
				if len(groups) != wantAddrStanzas {
					t.Errorf("got %d JSON objects, want %d", len(groups), wantAddrStanzas)
				}
			},
		},
		{
			// The comparison is over the ordered *lists*, not by searching the
			// text for each name. Searching was the first attempt and it was
			// wrong: a veth's text stanza reads `58: ve-nfb-vpn@if2:`, so a
			// `": " + name + ":"` probe misses exactly the three links whose
			// naming is most interesting. iproute2 puts the bare ifname in
			// JSON and only concatenates the peer suffix for the text form
			// (print_name_and_link), which is the difference this row has to
			// respect rather than trip over.
			description: "positive: the JSON ifnames are the text stanza names, in the same order",
			check: func(t *testing.T) {
				// fromJSON is exactly one name per group; fromText's length
				// is whatever the stanza scan below finds, which is the
				// quantity the two lists are compared on.
				fromJSON := make([]string, 0, len(groups))
				var fromText []string
				for _, g := range groups {
					name, _ := g["ifname"].(string)
					if name == "" {
						t.Fatalf("a JSON object has no ifname: %v", g)
					}
					fromJSON = append(fromJSON, name)
				}
				for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
					if strings.HasPrefix(l, " ") {
						continue
					}
					// "58: ve-nfb-vpn@if2: <FLAGS> ..." -> "ve-nfb-vpn"
					_, rest, ok := strings.Cut(l, ": ")
					if !ok {
						t.Fatalf("unparseable stanza line %q", l)
					}
					name, _, ok := strings.Cut(rest, ": ")
					if !ok {
						t.Fatalf("unparseable stanza line %q", l)
					}
					fromText = append(fromText, strings.SplitN(name, "@", 2)[0])
				}
				if len(fromJSON) != len(fromText) {
					t.Fatalf("JSON has %d names %q, text has %d %q", len(fromJSON), fromJSON, len(fromText), fromText)
				}
				for i := range fromJSON {
					if fromJSON[i] != fromText[i] {
						t.Errorf("position %d: JSON %q, text %q", i, fromJSON[i], fromText[i])
					}
				}
			},
		},
		{
			// addr_info is opened unconditionally by
			// print_selected_addrinfo, so a link with no addresses carries an
			// empty array rather than a missing key or a null.
			description: "boundary: nlmon0 carries an empty addr_info array, not a missing key",
			check: func(t *testing.T) {
				for _, g := range groups {
					if g["ifname"] != "nlmon0" {
						continue
					}
					ai, ok := g["addr_info"]
					if !ok {
						t.Fatal("nlmon0 has no addr_info key")
					}
					arr, ok := ai.([]any)
					if !ok {
						t.Fatalf("nlmon0 addr_info is %T, want an array", ai)
					}
					if len(arr) != 0 {
						t.Errorf("nlmon0 addr_info has %d entries, want 0", len(arr))
					}
					return
				}
				t.Error("no nlmon0 object in the JSON")
			},
		},
		{
			description: "positive: the addr_info entries total 24, the 9 v4 plus the 15 v6",
			check: func(t *testing.T) {
				n := 0
				for _, g := range groups {
					if arr, ok := g["addr_info"].([]any); ok {
						n += len(arr)
					}
				}
				if n != wantV4Addrs+wantV6Addrs {
					t.Errorf("got %d addr_info entries, want %d", n, wantV4Addrs+wantV6Addrs)
				}
			},
		},
		{
			// print_linkmode is the only emitter of the key, and it is
			// do_link-gated, so `ip -j addr show` has no linkmode at all.
			description: "negative: no object carries a linkmode key",
			check: func(t *testing.T) {
				for _, g := range groups {
					if _, ok := g["linkmode"]; ok {
						t.Errorf("%v carries a linkmode key, which only `link show` emits", g["ifname"])
					}
				}
			},
		},
		{
			// The lifetime keys are numbers in JSON even where the text says
			// "forever": print_uint(PRINT_JSON, …, INFINITY_LIFE_TIME) emits
			// the sentinel rather than the word.
			description: "corner: a forever lifetime is the numeric sentinel in JSON, not the word",
			check: func(t *testing.T) {
				for _, g := range groups {
					if g["ifname"] != "lo" {
						continue
					}
					arr, _ := g["addr_info"].([]any)
					if len(arr) == 0 {
						t.Fatal("lo has no addr_info entries")
					}
					first, _ := arr[0].(map[string]any)
					v, ok := first["valid_life_time"].(float64)
					if !ok {
						t.Fatalf("lo's first address has valid_life_time %v (%T), want a number", first["valid_life_time"], first["valid_life_time"])
					}
					if uint32(v) != 0xFFFFFFFF {
						t.Errorf("valid_life_time = %v, want 4294967295", v)
					}
					return
				}
				t.Error("no lo object in the JSON")
			},
		},
		{
			description: "boundary: a v6 address carries no label key and a v4 one does",
			check: func(t *testing.T) {
				var v4, v6 map[string]any
				for _, g := range groups {
					if g["ifname"] != "lo" {
						continue
					}
					arr, _ := g["addr_info"].([]any)
					for _, e := range arr {
						m, _ := e.(map[string]any)
						switch m["family"] {
						case "inet":
							v4 = m
						case "inet6":
							v6 = m
						}
					}
				}
				if v4 == nil || v6 == nil {
					t.Fatal("lo is missing one of its two addresses in the JSON")
				}
				if v4["label"] != "lo" {
					t.Errorf("v4 label = %v, want \"lo\"", v4["label"])
				}
				if _, ok := v6["label"]; ok {
					t.Errorf("v6 address carries a label key %v, but IPv6 replies have no IFA_LABEL", v6["label"])
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, tc.check)
	}
}

// TestAddrShowTransactions is the L1 finding as a unit test: the number of
// requests, their order and their types.
//
// go test ./internal/goip/ -run TestAddrShowTransactions
func TestAddrShowTransactions(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		wantTypes   []uint16
		wantLens    []int
	}{
		{
			// Two requests, link dump then address dump. The order is
			// iproute2's and it is not arbitrary: the output loop is over
			// links, so the link dump has to complete first.
			description: "positive: AF_UNSPEC sends a 40-byte RTM_GETLINK then a 24-byte RTM_GETADDR",
			family:      unix.AF_UNSPEC,
			wantTypes:   []uint16{unix.RTM_GETLINK, unix.RTM_GETADDR},
			wantLens:    []int{40, 24},
		},
		{
			// **The length is the assertion.** 32 not 40, because
			// rtnl_linkdump_req_filter_fn skips the filter_fn — and so the
			// ext mask — for any family other than AF_UNSPEC and AF_PACKET.
			description: "positive: AF_INET sends a 32-byte RTM_GETLINK, with no ext mask",
			family:      unix.AF_INET,
			wantTypes:   []uint16{unix.RTM_GETLINK, unix.RTM_GETADDR},
			wantLens:    []int{32, 24},
		},
		{
			description: "positive: AF_INET6 sends the same 32-byte form",
			family:      unix.AF_INET6,
			wantTypes:   []uint16{unix.RTM_GETLINK, unix.RTM_GETADDR},
			wantLens:    []int{32, 24},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			rec := &recordingSource{inner: newCompositeSource(t)}
			var out bytes.Buffer
			c := &runCtx{
				src: rec, lltab: NewLLTab(), out: &out, errOut: &bytes.Buffer{},
				family: tc.family,
			}
			if err := addrShow(c, nil); err != nil {
				t.Fatalf("addrShow: %v", err)
			}
			if len(rec.requests) != len(tc.wantTypes) {
				t.Fatalf("sent %d requests, want %d", len(rec.requests), len(tc.wantTypes))
			}
			for i := range tc.wantTypes {
				gotType := uint16(rec.requests[i][4]) | uint16(rec.requests[i][5])<<8
				if gotType != tc.wantTypes[i] {
					t.Errorf("request %d nlmsg_type = %d, want %d", i, gotType, tc.wantTypes[i])
				}
				if len(rec.requests[i]) != tc.wantLens[i] {
					t.Errorf("request %d is %d bytes, want %d", i, len(rec.requests[i]), tc.wantLens[i])
				}
			}
			// The seq numbers must differ: the comparator pairs a reply to a
			// transaction by seq, and two requests sharing one would be
			// unattributable.
			s0 := uint32(rec.requests[0][8]) | uint32(rec.requests[0][9])<<8
			s1 := uint32(rec.requests[1][8]) | uint32(rec.requests[1][9])<<8
			if s0 == s1 {
				t.Errorf("both requests carry nlmsg_seq %d; they must differ", s0)
			}
		})
	}
}

// The `addr show dev` fixture and the interface it was taken on.
//
// devIndexCst is 3 and not 1 or 2: the capture namespace holds lo, then the
// nlmon0 the capture tooling itself creates, then the dummy. It is spelled out
// rather than read off the reply, because the ifindex is what request three
// carries and a test that derived it from the reply could not tell a
// correctly-filtered dump from an unfiltered one.
const (
	addrDevPcap    = guestDumpsDir + "netlink_route_getaddr_dev.pcap"
	addrDevSidecar = guestDumpsDir + "ip_addr_dev"
	devNameCst     = "goip0"
	devIndexCst    = 3
)

// runAddressWith drives runAddress over a chosen source, bypassing Run so the
// test can supply a TalkSource and read its counters back — the addr-side
// twin of runLinkWith.
func runAddressWith(t *testing.T, src Source, family uint8, args []string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := &runCtx{
		src:    src,
		lltab:  NewLLTab(),
		out:    &out,
		errOut: io.Discard,
		family: family,
	}
	err := runAddress(c, args)
	return out.String(), err
}

// TestAddrShowDevTransactionShape is the assertion no output can make: that
// `ip addr show dev NAME` is two single-gets AND a dump, in that order, and
// that its two single-gets are not the two `link show dev` sends.
//
// # The three requests, and what each of them is not
//
//  1. ll_link_get(name, 0) — ip/ipaddress.c:2253 into lib/ll_map.c:264. By
//     NAME, ifi_family AF_UNSPEC because the request struct is a designated
//     initializer that never names it, ext-mask attribute first. Identical to
//     `link show dev`'s first request, because it is the same function.
//  2. ipaddr_link_get(index) — :2302 into :2052. By INDEX, and ifi_family is
//     filter.family. This is where the two commands part: `link show dev`
//     sends iplink_get here, by NAME with ifi_family AF_PACKET and the
//     attributes in the other order.
//  3. ip_addr_list — :2314, the RTM_GETADDR dump, with the resolved index in
//     ifa_index.
//
// The wantDumps column is the half that a transaction count alone would miss.
// A goip that dumped the links instead of getting them would send three
// requests too, and print the same stanza, and be wrong at L2 twice over.
//
// go test ./internal/goip/ -run TestAddrShowDevTransactionShape
func TestAddrShowDevTransactionShape(t *testing.T) {
	llLinkGet := func(name string) getShape {
		return getShape{
			flags:     uint16(unix.NLM_F_REQUEST),
			family:    unix.AF_UNSPEC,
			firstAttr: uint16(unix.IFLA_EXT_MASK),
			name:      name,
		}
	}
	addrLinkGet := func(family uint8, index int32) getShape {
		return getShape{
			flags:     uint16(unix.NLM_F_REQUEST),
			family:    family,
			firstAttr: uint16(unix.IFLA_EXT_MASK),
			index:     index,
		}
	}

	tests := []struct {
		description      string
		family           uint8
		args             []string
		wantDumps        int
		wantGets         []getShape
		wantStdoutPrefix string
		wantNoStdout     string
	}{
		{
			description: "positive: `addr show dev goip0` is ll_link_get, then a by-index get, then one dump",
			family:      unix.AF_UNSPEC,
			args:        []string{"show", "dev", devNameCst},
			wantDumps:   1,
			wantGets: []getShape{
				llLinkGet(devNameCst),
				addrLinkGet(unix.AF_UNSPEC, devIndexCst),
			},
			wantStdoutPrefix: "3: goip0: ",
		},
		{
			// The family DOES reach request two here, where `link show dev`
			// has it overwritten with AF_PACKET before argument parsing
			// (ip/ipaddress.c:2416). Request one is unmoved, because
			// ll_link_get has no family knob at all — so this row is the one
			// that shows the two gets disagreeing about the family inside a
			// single command.
			description: "positive: -4 reaches the second get and not the first",
			family:      unix.AF_INET,
			args:        []string{"show", "dev", devNameCst},
			wantDumps:   1,
			wantGets: []getShape{
				llLinkGet(devNameCst),
				addrLinkGet(unix.AF_INET, devIndexCst),
			},
			wantStdoutPrefix: "3: goip0: ",
			wantNoStdout:     "inet6 ",
		},
		{
			description: "positive: -6 does the same with the other family, so the row above is not passing on AF_INET's value",
			family:      unix.AF_INET6,
			args:        []string{"show", "dev", devNameCst},
			wantDumps:   1,
			wantGets: []getShape{
				llLinkGet(devNameCst),
				addrLinkGet(unix.AF_INET6, devIndexCst),
			},
			wantStdoutPrefix: "3: goip0: ",
			wantNoStdout:     "    inet ",
		},
		{
			// AF_PACKET skips the address dump entirely — ip/ipaddress.c:2310
			// guards it — so the command degenerates to two single-gets and
			// no dump at all. That makes this the only `dev` form whose
			// transaction count matches `link show dev`'s while its requests
			// differ from it.
			description: "boundary: -0 drops the third transaction, because AF_PACKET skips the address dump",
			family:      unix.AF_PACKET,
			args:        []string{"show", "dev", devNameCst},
			wantDumps:   0,
			wantGets: []getShape{
				llLinkGet(devNameCst),
				addrLinkGet(unix.AF_PACKET, devIndexCst),
			},
			wantStdoutPrefix: "3: goip0: ",
			wantNoStdout:     "inet",
		},
		{
			description: "boundary: `addr lst dev goip0` abbreviates to the same three requests",
			family:      unix.AF_UNSPEC,
			args:        []string{"lst", "dev", devNameCst},
			wantDumps:   1,
			wantGets: []getShape{
				llLinkGet(devNameCst),
				addrLinkGet(unix.AF_UNSPEC, devIndexCst),
			},
			wantStdoutPrefix: "3: goip0: ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newLinkReplay(t, addrDevPcap)
			out, err := runAddressWith(t, src, tc.family, tc.args)
			if err != nil {
				t.Fatalf("runAddress(%q): %v", tc.args, err)
			}
			if src.dumps != tc.wantDumps {
				t.Errorf("sent %d dumps, want %d", src.dumps, tc.wantDumps)
			}
			if len(src.gets) != len(tc.wantGets) {
				t.Fatalf("sent %d single-gets, want %d", len(src.gets), len(tc.wantGets))
			}
			for i, want := range tc.wantGets {
				if got := selectorOf(src.gets[i]); got != want {
					t.Errorf("single-get %d = %+v, want %+v", i+1, got, want)
				}
			}
			if !strings.HasPrefix(out, tc.wantStdoutPrefix) {
				t.Errorf("stdout does not start with %q\ngot:\n%s", tc.wantStdoutPrefix, firstLines(out, 6))
			}
			if tc.wantNoStdout != "" && strings.Contains(out, tc.wantNoStdout) {
				t.Errorf("stdout contains %q, which this family must not print\ngot:\n%s",
					tc.wantNoStdout, firstLines(out, 8))
			}
			// One stanza, always: the selector resolved to one link and
			// ipaddr_filter cannot add any.
			for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
				if i > 0 && !strings.HasPrefix(line, " ") {
					t.Errorf("line %d starts a second stanza: %q", i+1, line)
				}
			}
		})
	}

	// The negative the rows above cannot state. getShape keeps five fields;
	// two requests agreeing on all five can still differ in the ext-mask
	// value, in a trailing attribute, or in nlmsg_len. Comparing the raw
	// bytes is what closes that gap, and it is cheap here because both
	// requests came out of the same run moments apart.
	t.Run("negative: the two single-gets differ in full, not merely in the fields getShape keeps", func(t *testing.T) {
		src := newLinkReplay(t, addrDevPcap)
		if _, err := runAddressWith(t, src, unix.AF_UNSPEC, []string{"show", "dev", devNameCst}); err != nil {
			t.Fatalf("runAddress: %v", err)
		}
		if len(src.gets) != 2 {
			t.Fatalf("sent %d single-gets, want 2", len(src.gets))
		}
		if bytes.Equal(src.gets[0], src.gets[1]) {
			t.Errorf("both single-gets are %x; ll_link_get and ipaddr_link_get have collapsed into one", src.gets[0])
		}
	})

	// And the one `link show dev` asserts from the other side: this command's
	// second request is NOT iplink_get's. Same interface, same moment, two
	// commands, two different bytes.
	t.Run("corner: the second get is not the iplink_get `link show dev` sends", func(t *testing.T) {
		addrSrc := newLinkReplay(t, addrDevPcap)
		if _, err := runAddressWith(t, addrSrc, unix.AF_UNSPEC, []string{"show", "dev", devNameCst}); err != nil {
			t.Fatalf("runAddress: %v", err)
		}
		linkSrc := newLinkReplay(t, addrDevPcap)
		if _, err := runLinkWith(t, linkSrc, unix.AF_PACKET, []string{"show", "dev", devNameCst}); err != nil {
			t.Fatalf("runLink: %v", err)
		}
		if len(addrSrc.gets) != 2 || len(linkSrc.gets) != 2 {
			t.Fatalf("gets = %d and %d, want 2 each", len(addrSrc.gets), len(linkSrc.gets))
		}
		if !bytes.Equal(addrSrc.gets[0], linkSrc.gets[0]) {
			t.Errorf("the two commands' FIRST requests differ, and they must not — both are ll_link_get\n"+
				"addr: %x\nlink: %x", addrSrc.gets[0], linkSrc.gets[0])
		}
		if bytes.Equal(addrSrc.gets[1], linkSrc.gets[1]) {
			t.Errorf("the two commands' SECOND requests are identical; ipaddr_link_get and iplink_get "+
				"are different functions with different selectors and families: %x", addrSrc.gets[1])
		}
	})
}

// TestAddrShowDevMatchesCapturedOutput diffs `goip addr show dev goip0`
// against the `ip addr show dev goip0` sidecar written in the same guest
// namespace, in the same window, as the pcap it replays.
//
// A matched pair, so this is a verbatim comparison rather than the
// reconstruction the 7_1_8 corpus needs — ip_addr_dev was written by a plain
// `ip`, not by `ip -d`.
//
// What it certifies beyond `addr show`'s own golden: the stanza here comes
// from a single-get reply rather than from a dump reply, and `do_link` is
// still 0, so it must have the address lines AND no `mode DEFAULT`. Those two
// facts pull in opposite directions — the reply looks like `link show dev`'s
// and the rendering must look like `addr show`'s — and only a golden taken
// from this exact command can hold both.
//
// # Three namespaces, and the two new ones carry what the dummy cannot
//
// The clean row is the control. The other two were committed unread, and each
// adds a device shape a dummy cannot express:
//
//   - mesh/ip_addr_dev is veth0, so the single-get must resolve IFLA_MASTER
//     to `master br0` and IFLA_LINK to `@veth1` through SIDE transactions,
//     off a reply that carries only the two indexes. It is also the only
//     `dev` form in the corpus with no address at all, which makes it a
//     negative: the stanza is one link header with nothing indented under it,
//     where a renderer that rendered an empty address list as a zero stanza
//     would print an `inet` line.
//   - tunnel/ip_addr_dev is gre1, whose header carries both `@NONE` and
//     `link/gre 192.0.2.3 peer 198.51.100.3` — ll_addr_n2a's 4-byte special
//     case, reached here through the single-get path rather than through a
//     dump.
//
// go test ./internal/goip/ -run TestAddrShowDevMatchesCapturedOutput
func TestAddrShowDevMatchesCapturedOutput(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		sidecar     string
		dev         string
		// wantAddrLines is the number of indented inet/inet6 lines, asserted
		// separately from the line-for-line diff so that "the header is right
		// and the stanza is empty" is a stated expectation rather than
		// something inferred from a passing diff.
		wantAddrLines int
	}{
		{
			description:   "control: the clean topology reproduces ip_addr_dev, four addresses under one header",
			pcap:          addrDevPcap,
			sidecar:       addrDevSidecar,
			dev:           devNameCst,
			wantAddrLines: 4,
		},
		{
			description:   "negative: mesh/ip_addr_dev is veth0, which carries NO address — one header, nothing indented",
			pcap:          guestDumpsDir + "mesh/netlink_route_getaddr_dev.pcap",
			sidecar:       guestDumpsDir + "mesh/ip_addr_dev",
			dev:           "veth0",
			wantAddrLines: 0,
		},
		{
			description:   "positive: tunnel/ip_addr_dev is gre1, @NONE and a dotted-quad link/gre over three stanzas",
			pcap:          guestDumpsDir + "tunnel/netlink_route_getaddr_dev.pcap",
			sidecar:       guestDumpsDir + "tunnel/ip_addr_dev",
			dev:           "gre1",
			wantAddrLines: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			raw, err := os.ReadFile(tc.sidecar)
			if err != nil {
				t.Fatalf("read %s: %v", tc.sidecar, err)
			}
			want := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")

			out, err := runAddressWith(t, newLinkReplay(t, tc.pcap), unix.AF_UNSPEC,
				[]string{"show", "dev", tc.dev})
			if err != nil {
				t.Fatalf("runAddress: %v", err)
			}
			got := strings.Split(strings.TrimRight(out, "\n"), "\n")

			if len(got) != len(want) {
				t.Errorf("rendered %d lines, want %d\ngot:\n%s\nwant:\n%s",
					len(got), len(want), out, string(raw))
			}

			// Asserted on its own because it is the one token that separates
			// this output from `ip link show dev NAME`'s, and a line-by-line
			// diff reports it as "line 1 differs" with no hint of why.
			if strings.Contains(out, "mode DEFAULT") {
				t.Errorf("output carries `mode DEFAULT`, which print_linkmode emits only under do_link "+
					"(ip/ipaddress.c:1043):\n%s", firstLines(out, 3))
			}

			addrLines := 0
			for _, l := range got {
				if strings.HasPrefix(strings.TrimLeft(l, " "), "inet") {
					addrLines++
				}
			}
			if addrLines != tc.wantAddrLines {
				t.Errorf("rendered %d inet/inet6 lines, want %d\n%s",
					addrLines, tc.wantAddrLines, out)
			}

			for i, wantLine := range want {
				if i >= len(got) {
					t.Errorf("missing output line %d; want %q", i+1, wantLine)
					continue
				}
				if got[i] != wantLine {
					t.Errorf("line %d mismatch\n got: %q\nwant: %q", i+1, got[i], wantLine)
				}
			}
		})
	}
}

// firstLines is a failure-message helper: long outputs make a test log
// unreadable, and a diff is only useful if it can be seen.
func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}

// parseLink is a decode-or-fail helper, so the rows above read as assertions
// rather than as error handling.
func parseLink(t *testing.T, body []byte) xtcpnl.LinkInfo {
	t.Helper()
	li, err := xtcpnl.ParseNewLink(body)
	if err != nil {
		t.Fatalf("ParseNewLink: %v", err)
	}
	return li
}

// The other four reply portids in the capture, named so the rows below read as
// statements about known traffic rather than about magic numbers.
const (
	// portidRtgenAddr is a tool that dumps addresses twice — AF_INET6 then
	// AF_INET — and never dumps links. Its requests are 20 bytes on a
	// rtgenmsg, where iproute2 sends 24 on an ifaddrmsg, so it is not
	// iproute2 at all.
	portidRtgenAddr uint32 = 3208

	// portidWriter added an address for real: one RTM_NEWADDR with
	// NLM_F_REQUEST|ACK|REPLACE|CREATE, answered by one NLMSG_ERROR ack.
	portidWriter uint32 = 890450

	// portidNeighTool did an AF_UNSPEC link dump (11 replies), a neighbor
	// dump (110 replies) and then nine RTM_GETLINK single-gets to name the
	// devices those neighbors sit on.
	portidNeighTool uint32 = 890537

	// portidAbsent is a portid no socket in this capture used. It exists to
	// give the stale-constant case an expectation.
	portidAbsent uint32 = 4242
)

// TestAddrBulkPcapAttribution guards this file's portid constants against the
// capture being regenerated, and censuses everything else that is in there.
//
// portidV4Run and portidV6Run are not facts about iproute2. They are the pids
// two `ip` processes happened to hold on the day the pcap was taken. Re-running
// `nix run .#capture-netlink-fixtures` produces the same command, the same
// request bytes and the same rendering — and two different portids, at which
// point every OpenReplayPortid call in this file quietly finds nothing to
// replay and the failures land far from the cause. This table is what turns
// that into one failure that names the constants to re-measure.
//
// The negative and corner rows are not decoration. They are the evidence that
// the portid filter is load-bearing rather than tidy: four other sockets put
// 183 more replies into this file, and one of them renders addresses.
//
// go test ./internal/goip/ -run TestAddrBulkPcapAttribution
func TestAddrBulkPcapAttribution(t *testing.T) {
	capt, err := nlparity.ParseRouteCaptureFile(addrBulkPcap)
	if err != nil {
		t.Fatalf("ParseRouteCaptureFile(%s): %v", addrBulkPcap, err)
	}

	tests := []struct {
		description string
		portid      uint32 // 0 means "no filter", matching ReplaySource
		wantTotal   int
		wantNewLink int
		wantNewAddr int
		wantDone    int
	}{
		{
			// 11 links, 9 AF_INET addresses, and two DONEs because a
			// `-4 addr show` is two dumps. 9 is also exactly the number of
			// `inet ` lines in ip_addr_n, which is what makes this an
			// attribution and not a guess.
			description: "positive: portidV4Run is the `ip -4 addr show` run",
			portid:      portidV4Run,
			wantTotal:   22,
			wantNewLink: 11,
			wantNewAddr: 9,
			wantDone:    2,
		},
		{
			// The same 11 links — the link dump does not depend on the
			// family — and 15 AF_INET6 addresses, matching ip_addr_n's
			// `inet6 ` line count.
			description: "positive: portidV6Run is the `ip -6 addr show` run",
			portid:      portidV6Run,
			wantTotal:   28,
			wantNewLink: 11,
			wantNewAddr: 15,
			wantDone:    2,
		},
		{
			// **Zero RTM_NEWLINK is the discriminator.** This socket
			// received 24 addresses, more than either of our runs, so a
			// filter that only counted addresses would pick it as the
			// better candidate. Nothing that renders `addr show` can skip
			// the link dump, because the output loop is over links.
			description: "negative: portidRtgenAddr dumps addresses but never links, so it is not an `addr show`",
			portid:      portidRtgenAddr,
			wantTotal:   26,
			wantNewLink: 0,
			wantNewAddr: 24,
			wantDone:    2,
		},
		{
			// The row that fires when the capture is regenerated: a portid
			// nothing used yields nothing at all, which is precisely the
			// state the stale constants would put the whole file into.
			description: "negative: a portid no socket used has no replies at all",
			portid:      portidAbsent,
			wantTotal:   0,
			wantNewLink: 0,
			wantNewAddr: 0,
			wantDone:    0,
		},
		{
			// Portid 0 is ReplaySource's "every reply", which is right for
			// a single-tool capture and wrong for this one: 233 replies
			// across six portids, 42 links and 72 addresses, against the
			// 22 and 28 that are ours. Note that 24 of the 233 are not
			// replies to anything — see the notification row below.
			description: "boundary: portid 0 means no filter, and takes in 183 replies that are not ours",
			portid:      0,
			wantTotal:   233,
			wantNewLink: 42,
			wantNewAddr: 72,
			wantDone:    8,
		},
		{
			// 20 RTM_NEWLINK from one socket, which decomposes as 11 from
			// an AF_UNSPEC dump plus 9 single-gets — the side traffic that
			// `ip neigh show` generates while naming the device each
			// neighbor sits on, and the reason the parity harness's
			// topology is a lone dummy with no IFLA_LINK and no
			// IFLA_MASTER.
			//
			// The 110 RTM_NEWNEIGH replies here are worth knowing about
			// for another reason: the corpus is documented as having no
			// RTM_GETNEIGH dump anywhere, and that is true of the REQUEST
			// side only. These are real kernel replies, and a kernel reply
			// does not depend on which tool asked, so NeighInfo's decoder
			// positives are available from this file today.
			description: "corner: portidNeighTool holds a neighbor dump plus nine link single-gets",
			portid:      portidNeighTool,
			wantTotal:   132,
			wantNewLink: 20,
			wantNewAddr: 0,
			wantDone:    2,
		},
		{
			// A write, in a corpus documented as read-only: one
			// RTM_NEWADDR request with NLM_F_CREATE, and the single
			// NLMSG_ERROR ack is all this portid received. It is here so
			// that "the fixtures contain only RTM_GET*" is not believed of
			// the captures as well as of xtcpnl.
			description: "corner: portidWriter received exactly one message, the ack for a real address add",
			portid:      portidWriter,
			wantTotal:   1,
			wantNewLink: 0,
			wantNewAddr: 0,
			wantDone:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var total, newlink, newaddr, done int
			for _, m := range capt.Msgs() {
				if m.IsRequest() {
					continue
				}
				if tc.portid != 0 && m.Hdr.Pid != tc.portid {
					continue
				}
				total++
				switch m.Hdr.Type {
				case unix.RTM_NEWLINK:
					newlink++
				case unix.RTM_NEWADDR:
					newaddr++
				case unix.NLMSG_DONE:
					done++
				}
			}
			if total != tc.wantTotal || newlink != tc.wantNewLink ||
				newaddr != tc.wantNewAddr || done != tc.wantDone {
				t.Errorf("portid %d: %d replies (%d NEWLINK, %d NEWADDR, %d DONE), "+
					"want %d (%d, %d, %d).\n"+
					"If %s was regenerated, the portids changed with it: re-measure\n"+
					"portidV4Run, portidV6Run and the want* counts at the top of this file.",
					tc.portid, total, newlink, newaddr, done,
					tc.wantTotal, tc.wantNewLink, tc.wantNewAddr, tc.wantDone,
					addrBulkPcap)
			}
		})
	}
}

// TestAddrBulkPcapNotifications pins the one thing in this capture that the
// parity harness is specified to treat as a failure rather than as noise.
//
// A netlink message that is not a request and carries nlmsg_pid 0 is a
// multicast notification: replies carry the requester's portid, so pid 0 on a
// non-request means nobody asked. The comparator's segmentation rules call that
// a capture-hygiene failure in a read-only cut — reported, never silently
// dropped — and this file has 24 of them, all RTM_NEWADDR, all with flags 0
// (no NLM_F_MULTI, so not dump replies).
//
// What produced them is not established, and this test deliberately does not
// claim: 24 is also exactly the host's address count and exactly
// portidRtgenAddr's reply count, which is suggestive and not evidence. What
// matters is that the number is pinned, so the hygiene check can be written
// against a known quantity instead of discovering it. The topology namespace
// added to nix/capture-netlink-fixtures.nix sidesteps the whole question:
// netlink taps are per-netns, so a capture taken inside a fresh namespace has
// no third-party traffic to be hygienic about.
//
// go test ./internal/goip/ -run TestAddrBulkPcapNotifications
func TestAddrBulkPcapNotifications(t *testing.T) {
	capt, err := nlparity.ParseRouteCaptureFile(addrBulkPcap)
	if err != nil {
		t.Fatalf("ParseRouteCaptureFile(%s): %v", addrBulkPcap, err)
	}

	tests := []struct {
		description string
		count       func(c nlparity.Capture) int
		want        int
	}{
		{
			description: "positive: 24 non-requests carry nlmsg_pid 0, so nobody asked for them",
			count: func(c nlparity.Capture) int {
				n := 0
				for _, m := range c.Msgs() {
					if !m.IsRequest() && m.Hdr.Pid == 0 {
						n++
					}
				}
				return n
			},
			want: 24,
		},
		{
			// All one type. A mixed set would mean several groups were
			// subscribed and the hygiene finding would need to name them.
			description: "positive: every one of them is an RTM_NEWADDR",
			count: func(c nlparity.Capture) int {
				n := 0
				for _, m := range c.Msgs() {
					if !m.IsRequest() && m.Hdr.Pid == 0 && m.Hdr.Type == unix.RTM_NEWADDR {
						n++
					}
				}
				return n
			},
			want: 24,
		},
		{
			// **This is what makes them notifications rather than dump
			// replies.** A multipart dump reply sets NLM_F_MULTI; these
			// set no flags at all. Without this row the 24 could be read
			// as a dump whose portid was somehow lost.
			description: "boundary: none of them sets NLM_F_MULTI, which is what rules out a dump",
			count: func(c nlparity.Capture) int {
				n := 0
				for _, m := range c.Msgs() {
					if !m.IsRequest() && m.Hdr.Pid == 0 && m.Hdr.Flags != 0 {
						n++
					}
				}
				return n
			},
			want: 0,
		},
		{
			// The hygiene rule is about non-requests. Requests legitimately
			// carry pid 0 — iproute2's four do — so a check written as
			// "pid == 0 is a notification" without the request test would
			// condemn the very traffic under comparison. Six requests here
			// have pid 0.
			description: "negative: six requests also carry pid 0, so pid alone cannot mean notification",
			count: func(c nlparity.Capture) int {
				n := 0
				for _, m := range c.Msgs() {
					if m.IsRequest() && m.Hdr.Pid == 0 {
						n++
					}
				}
				return n
			},
			want: 6,
		},
		{
			// A read-only corpus with a write in it. Flags
			// NLM_F_REQUEST|ACK|REPLACE|CREATE on an RTM_NEWADDR is an
			// address being added by a third party mid-capture, which is
			// also the best available explanation for the notifications
			// above even though it does not account for 24 of them.
			description: "corner: one captured request is an RTM_NEWADDR write, not a GET",
			count: func(c nlparity.Capture) int {
				n := 0
				for _, m := range c.Requests() {
					if m.Hdr.Type == unix.RTM_NEWADDR {
						n++
					}
				}
				return n
			},
			want: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.count(capt); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestAddrBulkPcapSeqCannotAttribute is the executable form of the reason
// ReplaySource filters on portid and never on nlmsg_seq.
//
// rtnl_open seeds seq from time(NULL) (lib/libnetlink.c:249), so two `ip`
// invocations started inside the same second hand out the SAME sequence
// numbers. Measured rather than argued: both runs use 1789012353 for their link
// dump and 1789012354 for their address dump, so grouping replies by seq merges
// the two into one and `addr show` would print every address twice.
//
// go test ./internal/goip/ -run TestAddrBulkPcapSeqCannotAttribute
func TestAddrBulkPcapSeqCannotAttribute(t *testing.T) {
	capt, err := nlparity.ParseRouteCaptureFile(addrBulkPcap)
	if err != nil {
		t.Fatalf("ParseRouteCaptureFile(%s): %v", addrBulkPcap, err)
	}

	const (
		runLinkSeq uint32 = 1789012353 // both runs' RTM_GETLINK
		runAddrSeq uint32 = 1789012354 // both runs' RTM_GETADDR
		dumpFlags  uint16 = unix.NLM_F_REQUEST | unix.NLM_F_DUMP
	)

	// portidsOn reports how many distinct reply portids used one seq.
	portidsOn := func(seq uint32) int {
		seen := map[uint32]bool{}
		for _, m := range capt.Msgs() {
			if !m.IsRequest() && m.Hdr.Seq == seq {
				seen[m.Hdr.Pid] = true
			}
		}
		return len(seen)
	}

	tests := []struct {
		description string
		count       func() int
		want        int
	}{
		{
			// Two dumps per run, so two seq values — and the same two for
			// both runs, which is the next row.
			description: "positive: the two runs between them use exactly two sequence numbers",
			count: func() int {
				seen := map[uint32]bool{}
				for _, m := range capt.Msgs() {
					if m.IsRequest() {
						continue
					}
					if m.Hdr.Pid == portidV4Run || m.Hdr.Pid == portidV6Run {
						seen[m.Hdr.Seq] = true
					}
				}
				return len(seen)
			},
			want: 2,
		},
		{
			// **The whole finding.** Both runs answered on 1789012353, so a
			// seq-keyed attribution has no way to say which run a link
			// reply belongs to.
			description: "negative: the link-dump seq is shared by both runs, so seq cannot separate them",
			count:       func() int { return portidsOn(runLinkSeq) },
			want:        2,
		},
		{
			description: "negative: so is the address-dump seq",
			count:       func() int { return portidsOn(runAddrSeq) },
			want:        2,
		},
		{
			// Why "seq works" is a tempting wrong conclusion: the
			// third-party address dumps used 634337/634338, nowhere near
			// ours, so seq separates every socket in this capture EXCEPT
			// the two that matter. A discriminator that is right five
			// times out of six is not a discriminator.
			description: "boundary: no other socket collides with the runs' seqs, so seq looks like it works",
			count: func() int {
				n := 0
				for _, m := range capt.Msgs() {
					if m.IsRequest() {
						continue
					}
					if m.Hdr.Pid == portidV4Run || m.Hdr.Pid == portidV6Run {
						continue
					}
					if m.Hdr.Seq == runLinkSeq || m.Hdr.Seq == runAddrSeq {
						n++
					}
				}
				return n
			},
			want: 0,
		},
		{
			// The discriminator that does work, on the request side:
			// nlmsg_pid exactly 0 AND flags exactly
			// NLM_F_REQUEST|NLM_F_DUMP. That selects iproute2's four
			// requests — two RTM_GETLINK at 32 bytes and two RTM_GETADDR
			// at 24 — out of the eighteen in the file.
			description: "corner: pid 0 with flags exactly REQUEST|DUMP selects iproute2's four requests",
			count: func() int {
				n := 0
				for _, m := range capt.Requests() {
					if m.Hdr.Pid == 0 && m.Hdr.Flags == dumpFlags {
						n++
					}
				}
				return n
			},
			want: 4,
		},
		{
			// And why the flags half is not redundant: relaxing it to
			// "pid == 0" admits the two 20-byte rtgenmsg RTM_GETADDRs,
			// which also carry pid 0 but set NLM_F_ACK as well, giving
			// 0x0305 rather than 0x0301.
			description: "corner: relaxing that to pid 0 alone admits the two rtgenmsg dumps",
			count: func() int {
				n := 0
				for _, m := range capt.Requests() {
					if m.Hdr.Pid == 0 {
						n++
					}
				}
				return n
			},
			want: 6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.count(); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestAddrShowV6StatsFromMIB drives the third stats arm end to end, from the
// committed AF_INET6 capture to rendered output.
//
// # Why this needs no new capture, which is the whole point of the step
//
// `ip -6 addr show` and `ip -s -6 addr show` exchange identical bytes in both
// directions. The AF_INET6 arm of rtnl_linkdump_req_filter_fn sends no
// IFLA_EXT_MASK, and the kernel's inet6_fill_ifinfo hardcodes ext_filter_mask
// to zero (net/ipv6/addrconf.c:6110), so IFLA_INET6_STATS is on the wire
// whether or not anyone asked for stats. netlink_route_getaddr_v6.pcap has
// carried the attribute since it was captured; `-s` only decides whether goip
// renders it. That is why the negative row below is load-bearing: the gate has
// to be show_stats, and an implementation that gated on attribute presence
// would print the block for both commands and pass every positive row here.
//
// go test ./internal/goip/ -run TestAddrShowV6StatsFromMIB
func TestAddrShowV6StatsFromMIB(t *testing.T) {
	const v6pcap = guestDumpsDir + "netlink_route_getaddr_v6.pcap"

	// run replays the v6 capture and returns stdout.
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		t.Setenv("GOIP_REPLAY", v6pcap)
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != ExitOK {
			t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
		}
		return stdout.String()
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: -s -6 addr show renders one RX/TX block per link, from IFLA_PROTINFO because there is no IFLA_STATS64 to read",
			check: func(t *testing.T) {
				out := run(t, "-s", "-6", "addr", "show")
				links := strings.Count(out, "mtu ")
				if links == 0 {
					t.Fatal("no links rendered; the replay produced nothing to gate")
				}
				if got := strings.Count(out, "RX:  bytes"); got != links {
					t.Errorf("%d RX blocks over %d links, want one each", got, links)
				}
				if got := strings.Count(out, "TX:  bytes"); got != links {
					t.Errorf("%d TX blocks over %d links, want one each", got, links)
				}
			},
		},
		{
			// The row that pins VALUES, per link and in both directions,
			// reading the expectation out of the pcap at literal byte
			// offsets rather than through xtcpnl's IPSTATS_MIB_* constants.
			//
			// The offsets are the point. Going through the constants would
			// compare the mapping against itself — transpose two and both
			// sides move together. Indices 1/2 are INPKTS/INOCTETS and 9/10
			// are OUTPKTS/OUTOCTETS, counted off include/uapi/linux/snmp.h
			// by hand.
			//
			// Both directions, because this capture's only traffic is
			// outbound: goip0 sent 432 octets in 7 packets and received
			// nothing, lo moved nothing either way. An earlier version of
			// this row compared the RX line alone and was therefore VACUOUS
			// — every RX pair in the fixture is "0 0", so transposing INPKTS
			// and INOCTETS left it green.
			//
			// Per link, because a comparison against a SET of pairs cannot
			// tell a correct render from one that prints one link's counters
			// twice.
			//
			// # What this row cannot catch, measured rather than assumed
			//
			// Four single-index transpositions were tried against it. It
			// caught TxBytes <- INOCTETS and missed RxPackets <- INOCTETS
			// and TxPackets <- OUTREQUESTS, both for fixture reasons: the
			// two rx entries are zero on every link here, and OutRequests
			// equals OutTransmits on a host that originates all its traffic.
			// TestInet6StatsSnmpCounters caught all four, because its
			// fixtures set one index at a time with a distinct value.
			//
			// So the division of labor is deliberate: the unit table pins
			// WHICH index each member reads, and this row pins that the
			// numbers survive decode, render and column formatting on their
			// way to the right link. Neither is redundant and neither alone
			// is sufficient.
			description: "positive: each link's rendered RX and TX counters equal that link's own IFLA_INET6_STATS, read at raw offsets rather than through the constants under test",
			check: func(t *testing.T) {
				type pair struct{ rx, tx string }
				want := map[string]pair{}

				capt, err := nlparity.ParseRouteCaptureFile(v6pcap)
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range capt.Msgs() {
					if m.IsRequest() || m.Hdr.Type != uint16(unix.RTM_NEWLINK) {
						continue
					}
					var name string
					var mib []byte
					_ = xtcpnl.WalkRTAttrs(m.Body[xtcpnl.IfInfomsgSizeCst:],
						func(atype uint16, val []byte) {
							switch atype {
							case uint16(unix.IFLA_IFNAME):
								name = string(bytes.TrimRight(val, "\x00"))
							case uint16(unix.IFLA_PROTINFO):
								_ = xtcpnl.WalkRTAttrs(val, func(inner uint16, iv []byte) {
									if inner == uint16(unix.IFLA_INET6_STATS) {
										mib = iv
									}
								})
							}
						})
					if name == "" {
						t.Fatal("a link reply carries no IFLA_IFNAME")
					}
					// 88 bytes reaches index 10, OUTOCTETS, the highest this
					// row reads.
					if len(mib) < 88 {
						t.Fatalf("link %q: IFLA_INET6_STATS is %d bytes, too short to "+
							"reach IPSTATS_MIB_OUTOCTETS", name, len(mib))
					}
					at := func(idx int) uint64 {
						return binary.LittleEndian.Uint64(mib[idx*8 : idx*8+8])
					}
					want[name] = pair{
						// `ip` prints bytes before packets on both lines.
						rx: fmt.Sprintf("%d %d", at(2), at(1)),  // INOCTETS, INPKTS
						tx: fmt.Sprintf("%d %d", at(10), at(9)), // OUTOCTETS, OUTPKTS
					}
				}
				if len(want) == 0 {
					t.Fatal("no RTM_NEWLINK replies in the capture")
				}

				// counters returns the two leading fields of the line after
				// the heading at index i.
				lines := strings.Split(run(t, "-s", "-6", "addr", "show"), "\n")
				counters := func(i int) string {
					if i+1 >= len(lines) {
						t.Fatalf("heading at line %d has no counter line under it", i)
					}
					f := strings.Fields(lines[i+1])
					if len(f) < 2 {
						t.Fatalf("line %d is not a counter line: %q", i+1, lines[i+1])
					}
					return f[0] + " " + f[1]
				}

				stanza := regexp.MustCompile(`^\d+: ([^:@]+)[:@]`)
				var link string
				checked, rendered := 0, 0
				for i, l := range lines {
					if m := stanza.FindStringSubmatch(l); m != nil {
						link = m[1]
						rendered++
						continue
					}
					w, ok := want[link]
					if !ok {
						continue
					}
					switch {
					case strings.Contains(l, "RX:") && strings.Contains(l, "bytes"):
						if got := counters(i); got != w.rx {
							t.Errorf("link %q RX = %q, want %q from "+
								"IFLA_INET6_STATS[INOCTETS]/[INPKTS]", link, got, w.rx)
						}
						checked++
					case strings.Contains(l, "TX:") && strings.Contains(l, "bytes"):
						if got := counters(i); got != w.tx {
							t.Errorf("link %q TX = %q, want %q from "+
								"IFLA_INET6_STATS[OUTOCTETS]/[OUTPKTS]", link, got, w.tx)
						}
						checked++
					}
				}

				// Two lines per rendered link, and fewer rendered links than
				// replies — which is correct rather than a shortfall: under a
				// family filter `ip` drops a link whose address list came back
				// empty, so nlmon0 is in the capture and not in the output.
				// The committed ip_addr_v6 sidecar shows the same two links.
				if checked != 2*rendered {
					t.Errorf("checked %d counter lines over %d rendered links, want %d",
						checked, rendered, 2*rendered)
				}
				if rendered == 0 || rendered > len(want) {
					t.Errorf("%d links rendered against %d in the capture", rendered, len(want))
				}
			},
		},
		{
			description: "negative: -6 addr show renders NO block, even though the same reply carries the same MIB — the gate is show_stats, not attribute presence",
			check: func(t *testing.T) {
				out := run(t, "-6", "addr", "show")
				if strings.Contains(out, "RX:") || strings.Contains(out, "TX:") {
					t.Errorf("a stats block was rendered without -s:\n%s", firstLines(out, 12))
				}
			},
		},
		{
			description: "negative: -6 addr show still matches ip_addr_v6 line for line, so adding the arm changed nothing about the ungated form",
			check: func(t *testing.T) {
				raw, err := os.ReadFile(guestDumpsDir + "ip_addr_v6")
				if err != nil {
					t.Fatal(err)
				}
				// The qlen is an ioctl `ip` makes and goip cannot; it is the
				// pre-existing skew on this command and has nothing to do
				// with stats. See sidecarWithoutIoctlQlen.
				assertLinesEqual(t, run(t, "-6", "addr", "show"),
					sidecarWithoutIoctlQlen(t, string(raw)))
			},
		},
		{
			description: "boundary: -s -6 -j addr show keys the object on stats64, because upstream's third arm returns sizeof(*stats64)",
			check: func(t *testing.T) {
				out := run(t, "-s", "-6", "-j", "addr", "show")
				var groups []map[string]any
				if err := json.Unmarshal([]byte(out), &groups); err != nil {
					t.Fatalf("unmarshal: %v\n%s", err, firstLines(out, 3))
				}
				if len(groups) == 0 {
					t.Fatal("no groups decoded")
				}
				for _, g := range groups {
					if _, ok := g["stats64"]; !ok {
						t.Errorf("link %v has no stats64 key", g["ifname"])
					}
					if _, ok := g["stats"]; ok {
						t.Errorf("link %v carries a stats key; `ip` emits stats64 "+
							"here even though no IFLA_STATS64 was on the wire",
							g["ifname"])
					}
				}
			},
		},
		{
			description: "boundary: -6 -j addr show carries neither key, so the JSON form is gated the same way the text form is",
			check: func(t *testing.T) {
				out := run(t, "-6", "-j", "addr", "show")
				var groups []map[string]any
				if err := json.Unmarshal([]byte(out), &groups); err != nil {
					t.Fatalf("unmarshal: %v\n%s", err, firstLines(out, 3))
				}
				for _, g := range groups {
					if _, ok := g["stats64"]; ok {
						t.Errorf("link %v carries stats64 without -s", g["ifname"])
					}
					if _, ok := g["stats"]; ok {
						t.Errorf("link %v carries stats without -s", g["ifname"])
					}
				}
			},
		},
		{
			description: "corner: the block sits BELOW the inet6 lines, where print_link_stats runs (ipaddress.c:2333) — one line after print_selected_addrinfo, not under the stanza like `ip -s link show`",
			check: func(t *testing.T) {
				out := run(t, "-s", "-6", "addr", "show")
				lines := strings.Split(out, "\n")
				rx := -1
				lastAddr := -1
				for i, l := range lines {
					if strings.HasPrefix(l, "    inet6 ") {
						lastAddr = i
					}
					if strings.Contains(l, "RX:  bytes") && rx == -1 {
						rx = i
						break
					}
				}
				if rx == -1 || lastAddr == -1 {
					t.Fatalf("expected both an inet6 line and an RX block, got rx=%d addr=%d", rx, lastAddr)
				}
				if rx < lastAddr {
					t.Errorf("the first RX block is at line %d, above the inet6 line at %d; "+
						"the addr object puts stats after the addresses", rx, lastAddr)
				}
			},
		},
		{
			description: "corner: -s -6 addr show and -6 addr show differ ONLY by the block — the request is identical, so nothing else may move",
			check: func(t *testing.T) {
				withS := run(t, "-s", "-6", "addr", "show")
				without := run(t, "-6", "addr", "show")
				stripped := []string{}
				for _, l := range strings.Split(withS, "\n") {
					if strings.Contains(l, "RX:  bytes") || strings.Contains(l, "TX:  bytes") {
						continue
					}
					// The counter line follows each heading; it is the only
					// other line made entirely of spaces and digits.
					if regexp.MustCompile(`^\s*\d[\d\s]*$`).MatchString(l) {
						continue
					}
					stripped = append(stripped, l)
				}
				assertLinesEqual(t, strings.Join(stripped, "\n"), without)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) { tc.check(t) })
	}
}

// The three `-s addr` captures, one per namespace, and the devices they were
// taken against.
//
// Each pcap was written by `capio` rather than `cap` — see
// nix/microvms/scripts/capture-netlink-dumps.exp — which records the pcap AND
// the stdout of ONE invocation. That pairing is what makes the comparisons
// below byte-exact without a counter normalizer: the numbers in the sidecar
// came off the same replies that are in the pcap. A `side` sidecar is a second
// invocation, so its counters have ticked, and every earlier stats golden in
// this corpus needed normalizeLinkStatsText or zeroLinkStats for exactly that
// reason.
const (
	addrStatsPcap   = guestDumpsDir + "netlink_route_getaddr_stats.pcap"
	addrStatsV6Pcap = guestDumpsDir + "netlink_route_getaddr_v6_stats.pcap"
)

// TestAddrShowStatsMatchesCapturedSidecars replays the `ip -s addr show` and
// `ip -s -6 addr show` captures and diffs goip's output against the stdout
// each capture recorded alongside its own replies.
//
// # What these rows certify that PR A's unit tables cannot
//
// TestAddrShowV6StatsFromMIB reads its expectation out of the pcap at literal
// MIB byte offsets, which pins WHICH counter each member comes from and is
// deliberately independent of any sidecar. What it cannot see is column
// layout: print_num's width selection, the two heading lines, and where the
// block sits relative to the address stanzas. Only a transcript a real `ip`
// wrote can fail on those, and these are the first goldens in the corpus that
// hold one for the addr object.
//
// # The four-way asymmetry, visible between two files
//
// goip0's TX line reads `530 7` in ip_addr_stats and `432 7` in
// ip_addr_v6_stats. Same link, same boot, same heading — and two different
// counter sets, because the AF_INET6 link dump is answered by
// inet6_fill_ifinfo (net/ipv6/addrconf.c:6073-6117) and carries
// IFLA_INET6_STATS instead of IFLA_STATS64. 530 is the L2 frame total from
// sysfs; 432 is Ip6OutOctets. The corner row below asserts the two disagree,
// because a port that read one source for both families would render
// plausible numbers on every line and be wrong on half of them.
//
// go test ./internal/goip/ -run TestAddrShowStatsMatchesCapturedSidecars
func TestAddrShowStatsMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		sidecar     string
		args        []string
		// dropIoctlQlen removes the one token `ip` gets from a
		// SIOCGIFTXQLEN ioctl rather than from netlink. See
		// sidecarWithoutIoctlQlen, and the row that sets it for why it is
		// the v6 form alone.
		dropIoctlQlen bool
	}{
		{
			// The positive, and the one row in this file that is byte-exact
			// against a stats transcript with nothing subtracted from either
			// side. Three links, two heading lines and two counter lines
			// each, column widths included.
			description: "positive: the clean topology reproduces ip_addr_stats byte for byte",
			pcap:        addrStatsPcap,
			sidecar:     "ip_addr_stats",
			args:        []string{"-s", "addr", "show"},
		},
		{
			// The mesh namespace adds a bridge, a veth pair and a link with
			// no address, so the block must render under a stanza that has
			// no `inet` lines at all — the position claim with the address
			// lines removed from under it.
			description: "positive: the mesh topology reproduces mesh/ip_addr_stats, block included on an address-less link",
			pcap:        guestDumpsDir + "mesh/netlink_route_getaddr_stats.pcap",
			sidecar:     "mesh/ip_addr_stats",
			args:        []string{"-s", "addr", "show"},
		},
		{
			// Thirteen tunnel devices, which is the widest rendering in the
			// corpus and the only one where `@NONE`, ` permaddr ` and a
			// stats block appear in one stanza.
			description: "positive: the tunnel topology reproduces tunnel/ip_addr_stats across thirteen devices",
			pcap:        guestDumpsDir + "tunnel/netlink_route_getaddr_stats.pcap",
			sidecar:     "tunnel/ip_addr_stats",
			args:        []string{"-s", "addr", "show"},
		},
		{
			// The MIB arm against a transcript. PR A verified this form on
			// the host and in a unit table; this is the first committed `ip`
			// stdout for it.
			description: "positive: -s -6 addr show reproduces ip_addr_v6_stats, which is the IFLA_PROTINFO arm rendered",
			pcap:        addrStatsV6Pcap,
			sidecar:     "ip_addr_v6_stats",
			args:        []string{"-s", "-6", "addr", "show"},
			// The v6 rows alone, and not the AF_UNSPEC rows above:
			// inet6_fill_ifinfo emits no IFLA_TXQLEN, so print_linkinfo
			// falls back to a SIOCGIFTXQLEN ioctl that a netlink-only goip
			// has no way to make. Structural, pre-existing, already the
			// `stdout:keyword:qlen` allowlist entry for this command, and
			// nothing to do with `-s` — the non-`-s` row at
			// TestAddrShowV6StatsFromMIB subtracts the same token.
			dropIoctlQlen: true,
		},
		{
			description:   "positive: the mesh topology reproduces mesh/ip_addr_v6_stats on the same arm",
			pcap:          guestDumpsDir + "mesh/netlink_route_getaddr_v6_stats.pcap",
			sidecar:       "mesh/ip_addr_v6_stats",
			args:          []string{"-s", "-6", "addr", "show"},
			dropIoctlQlen: true,
		},
		{
			description:   "positive: the tunnel topology reproduces tunnel/ip_addr_v6_stats on the same arm",
			pcap:          guestDumpsDir + "tunnel/netlink_route_getaddr_v6_stats.pcap",
			sidecar:       "tunnel/ip_addr_v6_stats",
			args:          []string{"-s", "-6", "addr", "show"},
			dropIoctlQlen: true,
		},
		{
			// The negative, and it is a claim about the GATE rather than
			// about the attribute. This pcap's replies carry IFLA_STATS64
			// and IFLA_STATS on every link — the request asked for them with
			// ext_filter_mask 0x01 — so a renderer keyed on attribute
			// presence rather than on show_stats renders a block here and
			// fails against ip_addr, which came from a request that set
			// RTEXT_FILTER_SKIP_STATS instead.
			//
			// Compared line for line, but NOT against the committed
			// ip_addr. That file is from an earlier boot with a different
			// random MAC, so its lladdr and
			// its derived fe80 link-local would differ for a reason that has
			// nothing to do with stats. The assertion is the subtraction
			// instead: the `-s` output minus its RX/TX heading and counter
			// lines IS the non-`-s` output, from the same bytes.
			description: "negative: addr show on the -s capture renders no block, even though every reply carries IFLA_STATS64",
			pcap:        addrStatsPcap,
			sidecar:     "",
			args:        []string{"addr", "show"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}

			// The negative row has no sidecar of its own: there is no
			// committed `ip addr show` transcript from THIS boot to diff
			// against, and the one from the previous boot differs on the MAC.
			// So it asserts against the `-s` form of itself.
			if tc.sidecar == "" {
				assertLinesEqual(t, stdout.String(),
					statsBlockRemoved(t, withStatsFrom(t, tc.pcap)))
				return
			}

			raw, err := os.ReadFile(guestDumpsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			want := string(raw)
			if tc.dropIoctlQlen {
				want = sidecarWithoutIoctlQlen(t, want)
			}
			assertLinesEqual(t, stdout.String(), want)
		})
	}
}

// withStatsFrom is `goip -s addr show` over one capture, for the rows whose
// expectation is goip's own gated output rather than a committed file.
func withStatsFrom(t *testing.T, pcap string) string {
	t.Helper()
	t.Setenv("GOIP_REPLAY", pcap)
	var stdout, stderr bytes.Buffer
	args := []string{"-s", "addr", "show"}
	if code := Run(args, &stdout, &stderr); code != ExitOK {
		t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
	}
	return stdout.String()
}

// statsBlockRemoved drops the four lines print_link_stats64 adds per link: two
// headings and the two counter lines under them.
//
// It is the inverse of the thing under test, which is the only way to compare
// a gated render against an ungated one taken from the same bytes. A heading
// is recognized by its `RX:`/`TX:` prefix and a counter line by being made
// entirely of spaces and digits — the same two shapes
// TestAddrShowV6StatsFromMIB's subtraction row uses, and neither can collide
// with an address stanza, which always carries a `/` and a scope.
func statsBlockRemoved(t *testing.T, s string) string {
	t.Helper()
	counter := regexp.MustCompile(`^\s*\d[\d\s]*$`)
	var out []string
	for _, l := range strings.Split(s, "\n") {
		trimmed := strings.TrimLeft(l, " ")
		if strings.HasPrefix(trimmed, "RX:") || strings.HasPrefix(trimmed, "TX:") {
			continue
		}
		if counter.MatchString(l) {
			continue
		}
		out = append(out, l)
	}
	if len(out) == len(strings.Split(s, "\n")) {
		t.Fatal("statsBlockRemoved removed nothing; the input carries no stats block, " +
			"so the row comparing against it would be vacuous")
	}
	return strings.Join(out, "\n")
}

// runAddressStatsWith is runAddressWith with show_stats set, for the rows that
// must supply a TalkSource and cannot go through Run.
//
// `-s` is a global option parsed in Run's own loop, so a test that drives
// runAddress directly has no way to pass it other than setting the field. The
// dev-path rows need both at once: a TalkSource, because `addr show dev NAME`
// is two single-gets and a dump, and show_stats, because that is the subject.
func runAddressStatsWith(t *testing.T, src Source, family uint8, args []string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := &runCtx{
		src:       src,
		lltab:     NewLLTab(),
		out:       &out,
		errOut:    io.Discard,
		family:    family,
		showStats: 1,
	}
	err := runAddress(c, args)
	return out.String(), err
}

// TestAddrShowDevStatsMatchesCapturedSidecars diffs `goip -s addr show dev
// NAME` against the ip_addr_dev_stats transcript from the same invocation.
//
// # Why this is the only `-s` row in the sweep that needed a harness fix
//
// It is the only one whose capture holds TWO RTM_NEWLINK replies for the same
// link. ll_link_get asks by name on a throwaway socket with its mask hardcoded
// to RTEXT_FILTER_VF | RTEXT_FILTER_SKIP_STATS (lib/ll_map.c:264), purely to
// turn `dev NAME` into an index; ipaddr_link_get then asks again by index
// carrying show_stats' mask, and only that second reply has IFLA_STATS64.
// linkReplay used to answer both from the first reply it found with a matching
// selector, so this command replayed a stats-free reply and rendered no block.
// linkReplay.Talk pairs each request with the reply that follows it now, and
// its comment has the transcript.
//
// The live command was byte-identical to `ip`'s throughout, which is the point
// worth keeping: the failure was in the fixture reader, and the only thing
// that distinguished the two was running `goip` against a real socket.
//
// # Why this capture exists at all, when netlink_route_getaddr_dev.pcap does
//
// It is the corpus's only record of an ext_filter_mask on a NON-DUMP
// RTM_GETLINK. Step 2's corner row could otherwise argue only from source
// that the get path is stats-sensitive for every family; this is the byte.
//
// go test ./internal/goip/ -run TestAddrShowDevStatsMatchesCapturedSidecars
func TestAddrShowDevStatsMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		sidecar     string
		dev         string
		// wantNoStats asserts the negative: no block at all, for the row
		// that drops show_stats.
		wantNoStats bool
	}{
		{
			description: "positive: the clean topology reproduces ip_addr_dev_stats, block under a single-get stanza",
			pcap:        guestDumpsDir + "netlink_route_getaddr_dev_stats.pcap",
			sidecar:     "ip_addr_dev_stats",
			dev:         devNameCst,
		},
		{
			// The row the harness fix was found on, and the one that proves
			// it: veth0 has a master AND a peer, so print_linkinfo issues two
			// more by-index gets for br0 and veth1 on top of the two for
			// veth0 itself. Four gets, four replies, and the right one has to
			// be picked for each. A reply-first match renders `veth0@if4
			// master if3` with no block and no `M-DOWN` — plausible on every
			// token and wrong on four of them.
			description: "positive: the mesh topology reproduces mesh/ip_addr_dev_stats across four single-gets",
			pcap:        guestDumpsDir + "mesh/netlink_route_getaddr_dev_stats.pcap",
			sidecar:     "mesh/ip_addr_dev_stats",
			dev:         "veth0",
		},
		{
			// gre1 is the only UP tunnel and the only device in the corpus
			// that carries addresses, `@NONE`, a permaddr and a stats block
			// in one stanza.
			description: "positive: the tunnel topology reproduces tunnel/ip_addr_dev_stats under an @NONE header",
			pcap:        guestDumpsDir + "tunnel/netlink_route_getaddr_dev_stats.pcap",
			sidecar:     "tunnel/ip_addr_dev_stats",
			dev:         "gre1",
		},
		{
			// The negative, and on this command it is sharper than on the
			// dump forms. The reply goip renders here was fetched BECAUSE
			// show_stats set the mask, so "the attribute is on the wire" and
			// "the user asked for stats" are as close to the same thing as
			// they ever get — and they are still two decisions. Dropping
			// show_stats must drop the block even though the bytes that
			// arrived are unchanged.
			description: "negative: the same capture with show_stats unset renders no block, though the reply still carries IFLA_STATS64",
			pcap:        guestDumpsDir + "netlink_route_getaddr_dev_stats.pcap",
			dev:         devNameCst,
			wantNoStats: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newLinkReplay(t, tc.pcap)
			args := []string{"show", "dev", tc.dev}

			if tc.wantNoStats {
				out, err := runAddressWith(t, src, unix.AF_UNSPEC, args)
				if err != nil {
					t.Fatalf("runAddress: %v", err)
				}
				if strings.Contains(out, "RX:") || strings.Contains(out, "TX:") {
					t.Errorf("a stats block was rendered without show_stats:\n%s", firstLines(out, 12))
				}
				return
			}

			out, err := runAddressStatsWith(t, src, unix.AF_UNSPEC, args)
			if err != nil {
				t.Fatalf("runAddress: %v", err)
			}
			raw, err := os.ReadFile(guestDumpsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			assertLinesEqual(t, out, string(raw))
		})
	}
}
