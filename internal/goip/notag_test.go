//go:build !goip_write

package goip

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// errNoSocket is noSocket's answer to a request: a write verb must be
// refused before goip sends one.
var errNoSocket = errors.New("noSocket: a request")

// noSocket is a source of every kind that answers each dump with nothing,
// the reads ip makes ahead of a write (ll_init_map), and counts and refuses
// every request.
type noSocket struct{ requests int }

func (s *noSocket) request() error { s.requests++; return errNoSocket }

func (s *noSocket) Dump([]byte, uint16) ([][]byte, error)     { return nil, nil }
func (s *noSocket) DumpMsgs([]byte, uint16) ([][]byte, error) { return nil, nil }
func (s *noSocket) GenlDump([]byte) ([][]byte, error)         { return nil, nil }
func (s *noSocket) XfrmDump([]byte) ([]xfrmMsg, error)        { return nil, nil }
func (s *noSocket) Talk([]byte, uint16) ([]byte, error)       { return nil, s.request() }
func (s *noSocket) GenlTalk([]byte) (uint16, []byte, error)   { return 0, nil, s.request() }
func (s *noSocket) XfrmTalk([]byte) (uint16, []byte, error)   { return 0, nil, s.request() }

// genlResolved is every generic netlink family a write verb names, as if
// resolved, so the run reaches its verb.
func genlResolved() map[string]uint16 {
	m := map[string]uint16{}
	for i, n := range []string{xtcpnl.FOUGenlName, xtcpnl.ILAGenlName, xtcpnl.MPTCPPMGenlName, xtcpnl.IOAM6GenlName,
		xtcpnl.L2TPGenlName, xtcpnl.MacsecGenlName, xtcpnl.Seg6GenlName, xtcpnl.TCPMetricsGenlName} {
		m[n] = uint16(0x20 + i)
	}
	return m
}

// TestWriteVerbsNotCompiled is the untagged build's guarantee: every write
// verb of every object is ErrWriteNotCompiled, an ErrWriteVerb, exit 1, and
// goip sends no request on the way.
func TestWriteVerbsNotCompiled(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		args        string
		expected    string // the error names it so
	}{
		{"link_add", "N", "ip link add", "link add d0 type dummy", "ip link add"},
		{"link_set", "N", "ip link set", "link set dev d0 up", "ip link set"},
		{"link_abbrev", "C", "ip link s, set by abbreviation", "link s dev d0 up", "ip link s"},
		{"link_delete", "N", "ip link delete", "link delete dev d0", "ip link delete"},
		{"link_property", "N", "ip link property add", "link property add dev d0 altname a0", "ip link property"},
		{"addr_add", "N", "ip address add", "address add 192.0.2.1/24 dev d0", "ip address add"},
		{"addr_chg", "B", "ip address chg, exact", "address chg 192.0.2.1/24 dev d0", "ip address chg"},
		{"addr_flush", "N", "ip address flush", "address flush dev d0", "ip address flush"},
		{"addr_restore", "N", "ip address restore", "address restore", "ip address restore"},
		{"addrlabel_add", "N", "ip addrlabel add", "addrlabel add prefix ::/0 label 1", "ip addrlabel"},
		{"addrlabel_flush", "N", "ip addrlabel flush", "addrlabel flush", "ip addrlabel flush"},
		{"route_add", "N", "ip route add", "route add 192.0.2.0/24 dev d0", "ip route add"},
		{"route_test", "C", "ip route test, the write verb ip keeps for tests", "route test 192.0.2.0/24 dev d0", "ip route test"},
		{"route_flush", "N", "ip route flush", "route flush dev d0", "ip route flush"},
		{"route_restore", "N", "ip route restore", "route restore", "ip route restore"},
		{"rule_add", "N", "ip rule add", "rule add from all table 1", "ip rule"},
		{"rule_flush", "N", "ip rule flush", "rule flush", "ip rule flush"},
		{"rule_restore", "N", "ip rule restore", "rule restore", "ip rule restore"},
		{"neigh_add", "N", "ip neigh add", "neigh add 192.0.2.2 lladdr 02:00:00:00:00:01 dev d0", "ip neigh add"},
		{"neigh_flush", "N", "ip neigh flush", "neigh flush dev d0", "ip neigh flush"},
		{"ntable_change", "N", "ip ntable change", "ntable change name arp_cache thresh1 1", "ip ntable change"},
		{"nexthop_add", "N", "ip nexthop add", "nexthop add id 1 blackhole", "ip nexthop add"},
		{"nexthop_flush", "N", "ip nexthop flush", "nexthop flush", "ip nexthop flush"},
		{"netns_set", "N", "ip netns set", "netns set n0 1", "ip netns set"},
		{"stats_set", "N", "ip stats set", "stats set dev d0 l3_stats on", "ip stats set"},
		{"token_set", "N", "ip token set", "token set ::1/64 dev d0", "ip token"},
		{"tcp_metrics_delete", "N", "ip tcp_metrics delete", "tcp_metrics delete 192.0.2.2", "ip tcp_metrics delete"},
		{"tcp_metrics_flush", "N", "ip tcp_metrics flush of a prefix, not a DEL as an unfiltered flush is",
			"tcp_metrics flush 192.0.2.0/24", "ip tcp_metrics flush"},
		{"fou_add", "N", "ip fou add", "fou add port 5555 ipproto 4", "ip fou"},
		{"ila_add", "N", "ip ila add", "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4", "ip ila"},
		{"l2tp_add", "N", "ip l2tp add tunnel", "l2tp add tunnel tunnel_id 1 peer_tunnel_id 2 remote 192.0.2.2", "ip l2tp add"},
		{"l2tp_del", "N", "ip l2tp del tunnel", "l2tp del tunnel tunnel_id 1", "ip l2tp delete"},
		{"macsec_add", "N", "ip macsec add", "macsec add macsec0 tx sa 0 pn 1 on key 01 02000000000000000000000000000000", "ip macsec"},
		{"macsec_offload", "N", "ip macsec offload", "macsec offload macsec0 mac", "ip macsec offload"},
		{"ioam_add", "N", "ip ioam namespace add", "ioam namespace add 1", "ip ioam"},
		{"sr_tunsrc", "N", "ip sr tunsrc set", "sr tunsrc set ::1", "ip sr"},
		{"mptcp_add", "N", "ip mptcp endpoint add", "mptcp endpoint add 192.0.2.1 id 1 signal", "ip mptcp"},
		{"mptcp_limits", "N", "ip mptcp limits set", "mptcp limits set subflows 2", "ip mptcp"},
		{"xfrm_state_flush", "N", "ip xfrm state flush", "xfrm state flush", "ip xfrm state flush"},
		{"xfrm_state_add", "N", "ip xfrm state add",
			"xfrm state add src 192.0.2.1 dst 192.0.2.2 proto esp spi 1 enc cbc(aes) 0x00000000000000000000000000000000",
			"ip xfrm state"},
		{"xfrm_policy_flush", "N", "ip xfrm policy flush", "xfrm policy flush", "ip xfrm policy flush"},
		{"xfrm_policy_setdefault", "N", "ip xfrm policy setdefault", "xfrm policy setdefault in block", "ip xfrm policy setdefault"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := strings.Fields(tt.args)
			o, err := lookupObject(args[0])
			if err != nil {
				t.Fatal(err)
			}
			src := &noSocket{}
			c := newRunCtx(nil, &strings.Builder{}, &strings.Builder{})
			c.src, c.genlIDs = src, genlResolved()
			err = o.run(c, args[1:])
			if !errors.Is(err, ErrWriteNotCompiled) || !errors.Is(err, ErrWriteVerb) {
				t.Fatalf("%s: err %v, want ErrWriteNotCompiled", tt.description, err)
			}
			if !strings.Contains(err.Error(), tt.expected) {
				t.Errorf("%s: err %q, want %q in it", tt.description, err, tt.expected)
			}
			if code := exitCodeOf(err); code != ExitUsage {
				t.Errorf("%s: exit %d, want %d", tt.description, code, ExitUsage)
			}
			if src.requests != 0 {
				t.Errorf("%s: %d requests, want none", tt.description, src.requests)
			}
		})
	}
}

// TestWriteOpsUntagged pins writeOps to the stubs: no field is nil, and each
// is the one untaggedWriteOps builds, so nothing swapped a write arm in.
func TestWriteOpsUntagged(t *testing.T) {
	got, want := reflect.ValueOf(writeOps), reflect.ValueOf(untaggedWriteOps())
	for i := range got.NumField() {
		f := got.Type().Field(i).Name
		if got.Field(i).IsNil() {
			t.Errorf("writeOps.%s is nil", f)
			continue
		}
		if got.Field(i).Pointer() != want.Field(i).Pointer() {
			t.Errorf("writeOps.%s is not its stub", f)
		}
	}
}
