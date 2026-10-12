package goip

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
)

// routeWriteCorpusDir holds the route rows capture-link-writes.exp
// recorded: one `ip route …` run per directory, in a fresh namespace.
const routeWriteCorpusDir = "../../pkg/xtcpnl/testdata/7_1_4/link-write/route"

// routeEventsPcap is the root-namespace capture of mkVm.nix's route phase,
// which ran ten `ip route add|del` commands against nlcap0 (ifindex 5).
const routeEventsPcap = "../../pkg/xtcpnl/testdata/7_1_4/netlink_route_events.pcap"

// replayRouteWrite runs one route write over pcap and checks it against the
// requests `ip` sent: the lookups first, byte for byte, then the write.
// write is the index of ip's write among the capture's requests, and
// lookups how many requests before it goip must reproduce.
func replayRouteWrite(t *testing.T, pcap string, family uint8, args string, write, lookups int) {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(pcap)
	if err != nil {
		t.Fatal(err)
	}
	reqs := c.Requests()
	if write >= len(reqs) || write < lookups {
		t.Fatalf("capture holds %d requests; write %d after %d lookups is not in it", len(reqs), write, lookups)
	}

	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := newLinkReplay(t, pcap)
	if _, err := runRouteWith(t, src, family, strings.Fields(args)); err != nil {
		t.Fatalf("goip route %s: %v", args, err)
	}

	if len(src.gets) != lookups {
		t.Fatalf("goip sent %d lookups, want %d", len(src.gets), lookups)
	}
	for i, g := range src.gets {
		if want := wireBytesOf(reqs[write-lookups+i]); !bytes.Equal(g, want) {
			t.Errorf("lookup %d:\n got %x\nwant %x", i+1, g, want)
		}
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := wireBytesOf(reqs[write]); !bytes.Equal(zeroSeqPidCopy(got), want) {
		t.Errorf("write:\n got %x\nwant %x", zeroSeqPidCopy(got), want)
	}
}

// TestRouteWriteMatchesIpBytes is phase 4's exit evidence: for every row of
// the route corpus, goip sends the lookup `ip` sent, if any, and builds the
// same write request, byte for byte apart from nlmsg_seq and nlmsg_pid.
//
// The lookup is answered from the row's own pcap, so RTA_OIF carries the
// ifindex the kernel gave `ip`. The kernel's answer to the write is not part
// of the comparison: goip never sends it. Rows whose args differ from the
// captured argv check that two spellings build one message.
//
// go test ./internal/goip/ -run TestRouteWriteMatchesIpBytes
func TestRouteWriteMatchesIpBytes(t *testing.T) {
	const p24 = "198.51.100.0/24"
	tests := []struct {
		description string
		slug        string
		family      uint8  // -4/-6, as the row ran it
		args        string // after `ip route`, as the row ran it unless the description says otherwise
		expected    int    // name lookups `ip` sent before the write
	}{
		{description: "positive: the NFS replace, worked example", slug: "replace_nfs",
			args: "replace 192.0.2.50/32 dev d0 congctl lock cubic rto_min 3ms initcwnd 32 initrwnd 32 quickack 1", expected: 1},
		{description: "positive: replace a dev route", slug: "replace_dev", args: "replace " + p24 + " dev d0", expected: 1},
		{description: "positive: add a dev route", slug: "add_dev", args: "add " + p24 + " dev d0", expected: 1},
		{description: "positive: add via and dev", slug: "add_via_dev", args: "add " + p24 + " via 192.0.2.254 dev d0", expected: 1},
		{description: "boundary: add via alone sends no lookup", slug: "add_via_nodev", args: "add " + p24 + " via 192.0.2.254"},
		{description: "boundary: default has no RTA_DST", slug: "replace_default", args: "replace default via 192.0.2.254"},
		{description: "positive: change", slug: "change_metric", args: "change " + p24 + " dev d0 metric 100", expected: 1},
		{description: "positive: append", slug: "append_dev", args: "append " + p24 + " dev d0 metric 200", expected: 1},
		{description: "positive: prepend", slug: "prepend_dev", args: "prepend " + p24 + " dev d0 metric 300", expected: 1},
		{description: "positive: test, refused EEXIST", slug: "test_dev", args: "test " + p24 + " dev d0", expected: 1},
		{description: "positive: delete by dev", slug: "del_dev", args: "del " + p24 + " dev d0", expected: 1},
		{description: "boundary: delete with no dev sends no lookup", slug: "del_nodev", args: "del " + p24},
		{description: "positive: delete a missing route, refused ESRCH", slug: "del_missing", args: "del 203.0.113.99/32"},
		{description: "positive: table 100 fits rtm_table", slug: "add_table_100", args: "add " + p24 + " dev d0 table 100", expected: 1},
		{description: "boundary: table 300 spills to RTA_TABLE", slug: "add_table_300", args: "add " + p24 + " dev d0 table 300", expected: 1},
		{description: "positive: blackhole", slug: "add_blackhole", args: "add blackhole " + p24},
		{description: "positive: unreachable", slug: "add_unreachable", args: "add unreachable " + p24},
		{description: "positive: local goes to the local table", slug: "add_local", args: "add local 192.0.2.77 dev d0", expected: 1},
		{description: "positive: src, proto, scope and metric", slug: "add_src_proto_scope",
			args: "add " + p24 + " dev d0 src 192.0.2.1 proto static scope link metric 50", expected: 1},
		{description: "positive: onlink", slug: "add_onlink", args: "add " + p24 + " via 203.0.113.1 dev d0 onlink", expected: 1},
		{description: "positive: a v6 gateway on a v4 route is RTA_VIA", slug: "add_via_inet6",
			args: "add " + p24 + " via inet6 2001:db8::2 dev d0", expected: 1},
		{description: "positive: -6 replace via", slug: "v6_replace_via", family: unix.AF_INET6,
			args: "replace 2001:db8:1::/64 via 2001:db8::2 dev d0", expected: 1},
		{description: "positive: -6 delete", slug: "v6_del", family: unix.AF_INET6, args: "del 2001:db8:1::/64 dev d0", expected: 1},
		{description: "positive: every metric keyword", slug: "metrics_all",
			args:     "replace " + p24 + " dev d0 mtu lock 1400 advmss 1360 window 65535 rtt 3ms rttvar 10ms ssthresh 100 cwnd 10 reordering 5 hoplimit 64 initcwnd 10 initrwnd 20 features ecn tcp_usec_ts fastopen_no_cookie 1",
			expected: 1},
		{description: "boundary: raw rtt, rttvar and rto_min", slug: "metrics_raw",
			args: "replace " + p24 + " dev d0 rtt 24 rttvar 40 rto_min 200", expected: 1},
		{description: "corner: a repeated metric is sent twice", slug: "metric_repeated",
			args: "replace " + p24 + " dev d0 initcwnd 10 initcwnd 20", expected: 1},
		{description: "corner: repl is replace", slug: "replace_dev", args: "repl " + p24 + " dev d0", expected: 1},
		{description: "corner: chg is change", slug: "change_metric", args: "chg " + p24 + " dev d0 metric 100", expected: 1},
		{description: "corner: an explicit -4 builds what no family built", slug: "add_dev", family: unix.AF_INET,
			args: "add " + p24 + " dev d0", expected: 1},
		{description: "corner: to, oif and pri are the same request", slug: "change_metric",
			args: "change to " + p24 + " oif d0 pri 100", expected: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pcap := filepath.Join(routeWriteCorpusDir, tc.slug, "ip.pcap")
			c, err := nlparity.ParseRouteCaptureFile(pcap)
			if err != nil {
				t.Fatal(err)
			}
			if n := len(c.Requests()); n != tc.expected+1 {
				t.Fatalf("capture holds %d requests, want %d lookups and the write", n, tc.expected)
			}
			replayRouteWrite(t, pcap, tc.family, tc.args, tc.expected, tc.expected)
		})
	}
}

// TestRouteWriteMatchesIpEvents replays the ten route writes in the events
// capture, which predates the corpus and was taken in the root namespace:
// each dev route is its RTM_GETLINK for nlcap0 and then the write.
//
// go test ./internal/goip/ -run TestRouteWriteMatchesIpEvents
func TestRouteWriteMatchesIpEvents(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		args        string
		write       int // the write's index among the capture's requests
		expected    int // lookups before it
	}{
		{description: "positive: add a dev route", args: "add 198.51.100.0/24 dev nlcap0", write: 54, expected: 1},
		{description: "positive: delete it", args: "del 198.51.100.0/24 dev nlcap0", write: 56, expected: 1},
		{description: "positive: add via and dev", args: "add 203.0.113.0/24 via 192.0.2.254 dev nlcap0", write: 58, expected: 1},
		{description: "positive: delete via and dev", args: "del 203.0.113.0/24 via 192.0.2.254 dev nlcap0", write: 60, expected: 1},
		{description: "positive: add in table 100", args: "add 198.18.0.0/24 dev nlcap0 table 100", write: 62, expected: 1},
		{description: "positive: delete in table 100", args: "del 198.18.0.0/24 dev nlcap0 table 100", write: 64, expected: 1},
		{description: "boundary: add blackhole sends no lookup", args: "add blackhole 198.19.0.0/24", write: 65},
		{description: "corner: delete blackhole keeps the type and nowhere scope", args: "del blackhole 198.19.0.0/24", write: 66},
		{description: "positive: -6 add", family: unix.AF_INET6, args: "add 2001:db8:1::/64 dev nlcap0", write: 68, expected: 1},
		{description: "boundary: -6 delete is universe scope", family: unix.AF_INET6, args: "del 2001:db8:1::/64 dev nlcap0",
			write: 70, expected: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			replayRouteWrite(t, routeEventsPcap, tc.family, tc.args, tc.write, tc.expected)
		})
	}
}

// TestRouteWriteNodevCorpus is the one row whose write `ip` never sent: the
// lookup misses, `ip` says "Cannot find device", and goip must stop at the
// same place, after the same lookup, with no request written.
//
// go test ./internal/goip/ -run TestRouteWriteNodevCorpus
func TestRouteWriteNodevCorpus(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    string // the error fragment
	}{
		{description: "negative: an unknown dev is one lookup and no write",
			args: "add 198.51.100.0/24 dev nosuch", expected: `Cannot find device "nosuch"`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pcap := filepath.Join(routeWriteCorpusDir, "nodev", "ip.pcap")
			c, err := nlparity.ParseRouteCaptureFile(pcap)
			if err != nil {
				t.Fatal(err)
			}
			reqs := c.Requests()
			if len(reqs) != 1 {
				t.Fatalf("capture holds %d requests, want the lookup alone", len(reqs))
			}
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newLinkReplay(t, pcap)
			_, err = runRouteWith(t, src, unix.AF_UNSPEC, strings.Fields(tc.args))
			if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("err = %v, want %q", err, tc.expected)
			}
			if len(src.gets) != 1 || !bytes.Equal(src.gets[0], wireBytesOf(reqs[0])) {
				t.Errorf("lookups %x, want %x", src.gets, wireBytesOf(reqs[0]))
			}
			if _, err := os.Stat(out); err == nil {
				t.Error("a write file was written")
			}
		})
	}
}
