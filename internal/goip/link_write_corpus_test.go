package goip

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// linkWriteCorpusDir holds the write rows capture-link-writes.exp recorded:
// one `ip link …` run per directory, in a fresh namespace.
const linkWriteCorpusDir = "../../pkg/xtcpnl/testdata/7_1_4/link-write/write"

// wireBytesOf rebuilds a captured message as the datagram `ip` sent, with
// nlmsg_seq and nlmsg_pid zero, so it compares with zeroSeqPidCopy output.
func wireBytesOf(m nlparity.Msg) []byte {
	out := make([]byte, xtcpnl.NlMsgHdrSizeCst+len(m.Body))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out))) //nolint:gosec // a captured message
	binary.LittleEndian.PutUint16(out[4:6], m.Hdr.Type)
	binary.LittleEndian.PutUint16(out[6:8], m.Hdr.Flags)
	copy(out[xtcpnl.NlMsgHdrSizeCst:], m.Body)
	return out
}

// TestLinkWriteMatchesIpBytes is Phase 6's exit evidence: for every write
// row of the corpus, goip sends the same name lookups `ip` sent, in the same
// order, and builds the same write request, byte for byte apart from
// nlmsg_seq and nlmsg_pid.
//
// The lookups are answered from the row's own pcap, so the ifindexes are the
// ones the kernel gave `ip`. The kernel's answer to the write is not part of
// the comparison: goip never sends it. The refused rows (enslave_up,
// set_lacp_rate_up, …) are here for their request; their NLMSG_ERROR is
// TestDecodeNetlinkErrorExtack's subject.
//
// Rows whose args differ from the captured argv check that two spellings
// build one message: an abbreviated verb, and a numeric bond mode.
//
// go test ./internal/goip/ -run TestLinkWriteMatchesIpBytes
func TestLinkWriteMatchesIpBytes(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		args        string // after `ip link`, as the row ran it unless the description says otherwise
		expected    int    // name lookups `ip` sent before the write
	}{
		{description: "positive: add dummy", slug: "add_dummy",
			args: "add d0 type dummy", expected: 0},
		{description: "positive: add veth with a peer", slug: "add_veth",
			args: "add v0 type veth peer name v1", expected: 0},
		{description: "positive: add a bare bond, INFO_KIND only", slug: "add_bond_bare",
			args: "add bond0 type bond", expected: 0},
		{description: "positive: worked example 1", slug: "add_bond_8023ad",
			args: "add bond0 type bond mode 802.3ad lacp_rate fast miimon 100 xmit_hash_policy layer3+4", expected: 0},
		{description: "positive: every 802.3ad option", slug: "add_bond_all_lacp",
			args: "add bond0 type bond mode 802.3ad lacp_rate fast lacp_active on ad_select bandwidth ad_actor_sys_prio 100 ad_user_port_key 7 ad_actor_system 02:00:00:00:00:0a min_links 1", expected: 0},
		{description: "positive: active-backup with an ARP target nest", slug: "add_bond_ab_arp",
			args: "add bond0 type bond mode active-backup arp_interval 100 arp_ip_target 10.0.0.1,10.0.0.2 arp_validate all", expected: 0},
		{description: "positive: a numeric mode", slug: "add_bond_numeric_mode",
			args: "add bond0 type bond mode 4", expected: 0},
		{description: "positive: enslave while down, master resolved before dev", slug: "enslave_down",
			args: "set la1 down master bond0", expected: 2},
		{description: "positive: enslave while up, a request the kernel refuses", slug: "enslave_up",
			args: "set la1 master bond0", expected: 2},
		{description: "positive: nomaster is master 0", slug: "nomaster",
			args: "set la1 nomaster", expected: 1},
		{description: "positive: bond_slave queue_id", slug: "slave_queue_id",
			args: "set la1 type bond_slave queue_id 1", expected: 1},
		{description: "positive: bond_slave actor_port_prio", slug: "slave_actor_port_prio",
			args: "set la1 type bond_slave actor_port_prio 200", expected: 1},
		{description: "positive: lacp_rate on an up bond, refused EBUSY", slug: "set_lacp_rate_up",
			args: "set bond0 type bond lacp_rate slow", expected: 1},
		{description: "positive: mode with ports, refused ENOTEMPTY", slug: "set_mode_with_ports",
			args: "set bond0 type bond mode balance-rr", expected: 1},
		{description: "positive: lacp_rate in balance-rr, refused EACCES", slug: "set_lacp_rate_rr",
			args: "set bond0 type bond lacp_rate fast", expected: 1},
		{description: "positive: arp_interval with miimon, refused EINVAL", slug: "arp_and_miimon",
			args: "add bond0 type bond arp_interval 100 miimon 100", expected: 0},
		{description: "positive: replace an existing bond, refused EOPNOTSUPP", slug: "replace_existing",
			args: "replace bond0 type bond", expected: 0},
		{description: "positive: set up", slug: "set_up",
			args: "set bond0 up", expected: 1},
		{description: "positive: set down", slug: "set_down",
			args: "set bond0 down", expected: 1},
		{description: "positive: rename, mtu and alias in one set", slug: "rename_mtu_alias",
			args: "set d0 name d1 mtu 9000 alias uplink", expected: 1},
		{description: "positive: delete by index", slug: "del_bond",
			args: "del bond0", expected: 1},
		{description: "corner: a is add", slug: "add_bond_bare", args: "a bond0 type bond", expected: 0},
		{description: "corner: s is set, not show", slug: "enslave_down", args: "s la1 down master bond0", expected: 2},
		{description: "corner: mode 802.3ad builds what mode 4 built", slug: "add_bond_numeric_mode",
			args: "add bond0 type bond mode 802.3ad", expected: 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pcap := filepath.Join(linkWriteCorpusDir, tc.slug, "ip.pcap")
			c, err := nlparity.ParseRouteCaptureFile(pcap)
			if err != nil {
				t.Fatal(err)
			}
			reqs := c.Requests()
			if len(reqs) != tc.expected+1 {
				t.Fatalf("capture holds %d requests, want %d lookups and the write", len(reqs), tc.expected)
			}

			args := tc.args
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newLinkReplay(t, pcap)
			if _, err := runLinkWith(t, src, unix.AF_UNSPEC, strings.Fields(args)); err != nil {
				t.Fatalf("goip link %s: %v", args, err)
			}

			if len(src.gets) != tc.expected {
				t.Fatalf("goip sent %d lookups, want %d", len(src.gets), tc.expected)
			}
			for i, g := range src.gets {
				if want := wireBytesOf(reqs[i]); !bytes.Equal(g, want) {
					t.Errorf("lookup %d:\n got %x\nwant %x", i+1, g, want)
				}
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if want := wireBytesOf(reqs[tc.expected]); !bytes.Equal(zeroSeqPidCopy(got), want) {
				t.Errorf("write:\n got %x\nwant %x", zeroSeqPidCopy(got), want)
			}
		})
	}
}
