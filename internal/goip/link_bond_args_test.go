package goip

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The expected bytes below are built by hand from rtattr headers, not by the
// AttrBuilder under test: a length, a type, the payload, zero padding to 4.

func nla(atype uint16, payload ...[]byte) []byte {
	var body []byte
	for _, p := range payload {
		body = append(body, p...)
	}
	b := make([]byte, 4, 4+len(body)+3)
	binary.LittleEndian.PutUint16(b[0:2], uint16(4+len(body))) //nolint:gosec // test sizes are small
	binary.LittleEndian.PutUint16(b[2:4], atype)
	b = append(b, body...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func le16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func catBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// bondArgsResolver answers ll_name_to_index for the tests: eth0 is 3, and
// every other name is 0, no such device.
func bondArgsResolver(name string) (uint32, error) {
	if name == "eth0" {
		return 3, nil
	}
	return 0, nil
}

// bondArgsExpected is a row's outcome: the IFLA_INFO_DATA payload, or the
// error class and a fragment of iproute2's message.
type bondArgsExpected struct {
	wire   []byte
	err    error
	errMsg string
}

func checkBondArgs(t *testing.T, got []byte, err error, want bondArgsExpected) {
	t.Helper()
	if want.err != nil {
		if !errors.Is(err, want.err) {
			t.Fatalf("err = %v, want %v", err, want.err)
		}
		if !strings.Contains(err.Error(), want.errMsg) {
			t.Fatalf("err = %q, want it to contain %q", err, want.errMsg)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(got, want.wire) {
		t.Fatalf("wire mismatch\n got  %s\n want %s", hex.EncodeToString(got), hex.EncodeToString(want.wire))
	}
}

func targets16(n int) string {
	var parts []string
	for i := 1; i <= n; i++ {
		parts = append(parts, "10.0.0."+strconv.Itoa(i))
	}
	return strings.Join(parts, ",")
}

func targetNest16() []byte {
	var subs [][]byte
	for i := 1; i <= 16; i++ {
		subs = append(subs, nla(uint16(i-1), []byte{10, 0, 0, byte(i)})) //nolint:gosec // i <= 16
	}
	return nla(unix.IFLA_BOND_ARP_IP_TARGET, subs...)
}

// TestEncodeBondOptions is argv after `type bond` through bond_parse_opt and
// into IFLA_INFO_DATA: link-write-path.md table 3, with row 17 corrected. A
// short ad_actor_system is not a parse error; ll_addr_a2n returns two bytes,
// `ip` sends them, and the kernel is what refuses them.
//
// go test ./internal/goip/ -run TestEncodeBondOptions
func TestEncodeBondOptions(t *testing.T) {
	v6 := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	tests := []struct {
		description string
		args        []string
		expected    bondArgsExpected
	}{
		{description: "boundary: no arguments, an empty data nest payload", args: nil, expected: bondArgsExpected{wire: nil}},
		{description: "positive: mode by name", args: []string{"mode", "802.3ad"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_MODE, []byte{4})}},
		{description: "positive: mode by index, get_index's integer arm", args: []string{"mode", "4"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_MODE, []byte{4})}},
		{description: "boundary: mode 6 is the last index", args: []string{"mode", "6"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_MODE, []byte{6})}},
		{description: "boundary: mode 7 is past the table", args: []string{"mode", "7"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `argument "7" is wrong: invalid mode`}},
		{description: "negative: mode names are strcmp", args: []string{"mode", "802.3AD"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "invalid mode"}},
		{description: "corner: m is a prefix of mode", args: []string{"m", "balance-rr"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_MODE, []byte{0})}},
		{description: "positive: lacp_r is a prefix of lacp_rate", args: []string{"lacp_r", "fast"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_AD_LACP_RATE, []byte{1})}},
		{description: "positive: lacp_active on", args: []string{"lacp_active", "on"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_AD_LACP_ACTIVE, []byte{1})}},
		{description: "corner: lacp_active is strcmp, so lacp_act is unknown", args: []string{"lacp_act", "on"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `bond: unknown command "lacp_act"?`}},
		{description: "positive: arp_missed_max", args: []string{"arp_missed_max", "2"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_MISSED_MAX, []byte{2})}},
		{description: "corner: arp_missed_max is strcmp, so arp_missed is unknown", args: []string{"arp_missed", "2"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `unknown command "arp_missed"`}},
		{description: "negative: lacp_strict is not in the 7.1.0 parser", args: []string{"lacp_strict", "on"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `unknown command "lacp_strict"`}},
		{description: "corner: p is peer_notify_delay, tried before primary", args: []string{"p", "100"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_PEER_NOTIF_DELAY, le32(100))}},
		{description: "corner: a is active_slave", args: []string{"a", "eth0"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_ACTIVE_SLAVE, le32(3))}},
		{description: "corner: prim is primary, tried before primary_reselect", args: []string{"prim", "eth0"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_PRIMARY, le32(3))}},
		{description: "corner: primary_r is only primary_reselect", args: []string{"primary_r", "better"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_PRIMARY_RESELECT, []byte{1})}},
		{description: "corner: ad_actor_sys is ad_actor_sys_prio, tried before ad_actor_system", args: []string{"ad_actor_sys", "65535"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_AD_ACTOR_SYS_PRIO, le16(65535))}},
		{description: "boundary: ad_actor_sys_prio 65536 is past u16", args: []string{"ad_actor_sys_prio", "65536"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "invalid ad_actor_sys_prio"}},
		{description: "positive: ad_actor_system, six bytes", args: []string{"ad_actor_system", "02:00:00:00:00:0a"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_AD_ACTOR_SYSTEM, []byte{2, 0, 0, 0, 0, 0x0a})}},
		{description: "corner: a short ad_actor_system encodes its two bytes (table 3 row 17, corrected)", args: []string{"ad_actor_system", "02:00"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_AD_ACTOR_SYSTEM, []byte{2, 0})}},
		{description: "negative: ad_actor_system with a non-hex part", args: []string{"ad_actor_system", "zz:00"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "invalid lladdr"}},
		{description: "boundary: use_carrier 0", args: []string{"use_carrier", "0"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_USE_CARRIER, []byte{0})}},
		{description: "boundary: use_carrier 256 is past u8", args: []string{"use_carrier", "256"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "invalid use_carrier"}},
		{description: "positive: ad_select actor_port_prio, the fourth name", args: []string{"ad_select", "actor_port_prio"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_AD_SELECT, []byte{3})}},
		{description: "positive: xmit_hash_policy names are value order, not help order", args: []string{"xmit", "layer2+3"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_XMIT_HASH_POLICY, []byte{2})}},
		{description: "positive: num_unsol_na sets num_grat_arp's attribute", args: []string{"num_unsol_na", "3"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_NUM_PEER_NOTIF, []byte{3})}},
		{description: "corner: num is num_grat_arp", args: []string{"num", "3"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_NUM_PEER_NOTIF, []byte{3})}},
		{description: "positive: miimon base 0 hex", args: []string{"miimon", "0x64"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_MIIMON, le32(100))}},
		{description: "positive: argv order is wire order", args: []string{"miimon", "100", "mode", "active-backup", "tlb_dynamic_lb", "1"},
			expected: bondArgsExpected{wire: catBytes(
				nla(unix.IFLA_BOND_MIIMON, le32(100)),
				nla(unix.IFLA_BOND_MODE, []byte{1}),
				nla(unix.IFLA_BOND_TLB_DYNAMIC_LB, []byte{1}))}},
		{description: "corner: a repeated keyword is sent twice", args: []string{"miimon", "1", "miimon", "2"},
			expected: bondArgsExpected{wire: catBytes(nla(unix.IFLA_BOND_MIIMON, le32(1)), nla(unix.IFLA_BOND_MIIMON, le32(2)))}},
		{description: "positive: clear_active_slave takes no value and sends 0", args: []string{"clear_active_slave", "miimon", "1"},
			expected: bondArgsExpected{wire: catBytes(nla(unix.IFLA_BOND_ACTIVE_SLAVE, le32(0)), nla(unix.IFLA_BOND_MIIMON, le32(1)))}},
		{description: "negative: active_slave names no device", args: []string{"active_slave", "nope"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `Cannot find device "nope"`}},
		{description: "positive: coupled_control off", args: []string{"coupled_control", "off"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_COUPLED_CONTROL, []byte{0})}},
		{description: "positive: broadcast_neighbor on", args: []string{"broadcast_neighbor", "on"},
			expected: bondArgsExpected{wire: nla(xtcpnl.IflaBondBroadcastNeigh, []byte{1})}},
		{description: "negative: parse_on_off takes no numbers", args: []string{"coupled_control", "1"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `must be "on" or "off"`}},
		{description: "positive: two arp targets, sub-attribute type is the position from 0", args: []string{"arp_ip_target", "10.0.0.1,10.0.0.2"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_ARP_IP_TARGET,
				nla(0, []byte{10, 0, 0, 1}), nla(1, []byte{10, 0, 0, 2}))}},
		{description: "corner: strtok drops empty fields", args: []string{"arp_ip_target", ",10.0.0.1,,10.0.0.2,"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_ARP_IP_TARGET,
				nla(0, []byte{10, 0, 0, 1}), nla(1, []byte{10, 0, 0, 2}))}},
		{description: "boundary: the keyword alone is an empty nest", args: []string{"arp_ip_target"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_ARP_IP_TARGET)}},
		{description: "corner: only commas is an empty nest too", args: []string{"arp_ip_target", ",,"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_ARP_IP_TARGET)}},
		{description: "boundary: sixteen targets", args: []string{"arp_ip_target", targets16(16)},
			expected: bondArgsExpected{wire: targetNest16()}},
		{description: "boundary: a seventeenth target is dropped without a word", args: []string{"arp_ip_target", targets16(17)},
			expected: bondArgsExpected{wire: targetNest16()}},
		{description: "corner: NEXT_ARG_OK takes the next argument even when it is a keyword", args: []string{"arp_ip_target", "miimon", "100"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `an IP address is expected rather than "miimon"`}},
		{description: "corner: a target part is strtoul base 0, so 08 is not octal and is refused", args: []string{"arp_ip_target", "10.0.0.08"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `an IP address is expected rather than "10.0.0.08"`}},
		{description: "corner: and 010 is octal 8", args: []string{"arp_ip_target", "10.0.0.010"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_ARP_IP_TARGET, nla(0, []byte{10, 0, 0, 8}))}},
		{description: "positive: ns_ip6_target", args: []string{"ns_ip6_target", "2001:db8::1"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_NS_IP6_TARGET, nla(0, v6))}},
		{description: "corner: ns_ip6_target is strcmp", args: []string{"ns_ip6", "2001:db8::1"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `unknown command "ns_ip6"`}},
		{description: "negative: an IPv4 address in ns_ip6_target", args: []string{"ns_ip6_target", "10.0.0.1"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "inet6 address is expected"}},
		{description: "negative: a value is missing", args: []string{"miimon"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "Command line is not complete"}},
		{description: "negative: an unknown keyword after a good one fails the whole command", args: []string{"miimon", "1", "bogus"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `unknown command "bogus"`}},
		{description: "negative: help is not implemented", args: []string{"help"},
			expected: bondArgsExpected{err: ErrNotImplemented, errMsg: "help"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []byte
			settings, err := parseBondArgs(tc.args, bondArgsResolver)
			if err == nil {
				got, err = xtcpnl.EncodeBondInfoData(settings)
			}
			checkBondArgs(t, got, err, tc.expected)
		})
	}
}

// TestEncodeBondSlaveOptions is argv after `type bond_slave` through
// bond_slave_parse_opt into IFLA_INFO_SLAVE_DATA: link-write-path.md table 4.
//
// go test ./internal/goip/ -run TestEncodeBondSlaveOptions
func TestEncodeBondSlaveOptions(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    bondArgsExpected
	}{
		{description: "positive: queue_id", args: []string{"queue_id", "3"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_SLAVE_QUEUE_ID, le16(3))}},
		{description: "corner: q is a prefix of queue_id", args: []string{"q", "3"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_SLAVE_QUEUE_ID, le16(3))}},
		{description: "boundary: queue_id 65536 is past u16", args: []string{"queue_id", "65536"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "queue_id is invalid"}},
		{description: "positive: prio -1 is a signed 32-bit value", args: []string{"prio", "-1"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_SLAVE_PRIO, le32(0xffffffff))}},
		{description: "boundary: prio INT32_MIN", args: []string{"prio", "-2147483648"},
			expected: bondArgsExpected{wire: nla(unix.IFLA_BOND_SLAVE_PRIO, le32(0x80000000))}},
		{description: "boundary: prio past INT32_MAX", args: []string{"prio", "2147483648"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "prio is invalid"}},
		{description: "corner: prio is strcmp, so pri is unknown", args: []string{"pri", "1"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `bond_slave: unknown option "pri"?`}},
		{description: "positive: actor_port_prio", args: []string{"actor_port_prio", "65535"},
			expected: bondArgsExpected{wire: nla(xtcpnl.IflaBondSlaveActorPortPrio, le16(65535))}},
		{description: "boundary: actor_port_prio 65536", args: []string{"actor_port_prio", "65536"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "actor prio is invalid"}},
		{description: "corner: actor_port_prio is strcmp", args: []string{"actor", "1"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `unknown option "actor"`}},
		{description: "positive: argv order is wire order", args: []string{"prio", "5", "queue_id", "1"},
			expected: bondArgsExpected{wire: catBytes(nla(unix.IFLA_BOND_SLAVE_PRIO, le32(5)), nla(unix.IFLA_BOND_SLAVE_QUEUE_ID, le16(1)))}},
		{description: "negative: a bond keyword is not a port keyword", args: []string{"miimon", "1"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: `unknown option "miimon"`}},
		{description: "negative: a value is missing", args: []string{"queue_id"},
			expected: bondArgsExpected{err: ErrInvalidArgument, errMsg: "Command line is not complete"}},
		{description: "negative: h is help, not implemented", args: []string{"h"},
			expected: bondArgsExpected{err: ErrNotImplemented, errMsg: "help"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []byte
			settings, err := parseBondSlaveArgs(tc.args)
			if err == nil {
				got, err = xtcpnl.EncodeBondSlaveData(settings)
			}
			checkBondArgs(t, got, err, tc.expected)
		})
	}
}

// TestBondParseKeywordsCoverTable ties the parse chains to the option
// tables: every keyword names a row, matches the way the row says, and is
// its Keyword or one of its Aliases; every settable row has a keyword,
// except lacp_strict, which 7.1.0 does not parse.
//
// go test ./internal/goip/ -run TestBondParseKeywordsCoverTable
func TestBondParseKeywordsCoverTable(t *testing.T) {
	tests := []struct {
		description string
		chain       []bondKeyword
		table       []xtcpnl.BondOptionSpec
		unparsed    []string
		expected    int // keywords, help included
	}{
		{description: "positive: bond", chain: bondParseKeywords, table: xtcpnl.BondOptions, unparsed: []string{"lacp_strict"}, expected: 35},
		{description: "positive: bond_slave", chain: bondSlaveParseKeywords, table: xtcpnl.BondSlaveOptions, expected: 4},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if len(tc.chain) != tc.expected {
				t.Errorf("%d keywords, want %d", len(tc.chain), tc.expected)
			}
			covered := map[uint16]bool{}
			for i := range tc.chain {
				kw := &tc.chain[i]
				if kw.arg == bondArgHelp {
					if i != len(tc.chain)-1 {
						t.Errorf("help is keyword %d, want it last", i)
					}
					continue
				}
				spec := specByID(tc.table, kw.id)
				if spec == nil {
					t.Errorf("%s: id %d has no row", kw.keyword, kw.id)
					continue
				}
				if kw.match != spec.Match {
					t.Errorf("%s: match %d, row says %d", kw.keyword, kw.match, spec.Match)
				}
				if kw.keyword != spec.Keyword && !slices.Contains(spec.Aliases, kw.keyword) {
					t.Errorf("%s: not the row's keyword %q or an alias %v", kw.keyword, spec.Keyword, spec.Aliases)
				}
				covered[kw.id] = true
			}
			for i := range tc.table {
				s := &tc.table[i]
				if s.ReadOnly || slices.Contains(tc.unparsed, s.Keyword) {
					if covered[s.ID] {
						t.Errorf("%s: has a keyword but should not", s.Keyword)
					}
					continue
				}
				if !covered[s.ID] {
					t.Errorf("%s: settable row has no keyword", s.Keyword)
				}
			}
		})
	}
}
