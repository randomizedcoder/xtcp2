package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// genlWant is a captured request: its header fields and body, as
// link-write/genl/<row>/ip.pcap holds it, the pid zero.
func genlWant(t *testing.T, typ, flags uint16, seq uint32, bodyHex string) []byte {
	t.Helper()
	body, err := hex.DecodeString(bodyHex)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, NlMsgHdrSizeCst, NlMsgHdrSizeCst+len(body))
	binary.LittleEndian.PutUint32(b[0:4], uint32(NlMsgHdrSizeCst+len(body)))
	binary.LittleEndian.PutUint16(b[4:6], typ)
	binary.LittleEndian.PutUint16(b[6:8], flags)
	binary.LittleEndian.PutUint32(b[8:12], seq)
	return append(b, body...)
}

// TestBuildGenlRequest checks the generic netlink builders against ip's
// tcp_metrics requests in the C2-T01 captures (tcp_metrics' id 31 there), and
// the write gate between them.
//
// go test ./pkg/xtcpnl/ -run TestBuildGenlRequest
func TestBuildGenlRequest(t *testing.T) {
	const id = 31
	addr4 := func(typ uint16, a, b, c, d byte) []byte {
		return []byte{8, 0, byte(typ), 0, a, b, c, d}
	}
	srcPair := append(addr4(TCPMetricsAttrAddrIPv4, 192, 0, 2, 1), addr4(TCPMetricsAttrSaddrIPv4, 192, 0, 2, 2)...)
	tests := []struct {
		description string
		write       bool
		family      string
		id          uint16
		cmd         uint8
		flags       uint16
		attrs       []byte
		expected    []byte // nil when refused
		err         error
	}{
		{
			description: "positive: tcpm_show's dump, GET with NLM_F_DUMP and no attributes",
			family:      TCPMetricsGenlName, id: id, cmd: TCPMetricsCmdGet, flags: unix.NLM_F_DUMP,
			expected: genlWant(t, id, 0x301, 7, "01010000"),
		},
		{
			description: "positive: tcpm_get_src's GET, the address then the source",
			family:      TCPMetricsGenlName, id: id, cmd: TCPMetricsCmdGet, attrs: srcPair,
			expected: genlWant(t, id, 0x1, 7, "0101000008000100c000020108000b00c0000202"),
		},
		{
			description: "positive: tcpm_delete_src's DEL, with NLM_F_ACK, as a write",
			write:       true, family: TCPMetricsGenlName, id: id, cmd: TCPMetricsCmdDel, flags: unix.NLM_F_ACK,
			attrs:    srcPair,
			expected: genlWant(t, id, 0x5, 7, "0201000008000100c000020108000b00c0000202"),
		},
		{
			description: "positive: a flush's DEL, NLM_F_REQUEST alone",
			write:       true, family: TCPMetricsGenlName, id: id, cmd: TCPMetricsCmdDel, attrs: srcPair,
			expected: genlWant(t, id, 0x1, 7, "0201000008000100c000020108000b00c0000202"),
		},
		{
			description: "negative: a DEL asked of the read builder",
			family:      TCPMetricsGenlName, id: id, cmd: TCPMetricsCmdDel, err: ErrGenlWrite,
		},
		{
			description: "negative: a GET asked of the write builder",
			write:       true, family: TCPMetricsGenlName, id: id, cmd: TCPMetricsCmdGet, err: ErrGenlWrite,
		},
		{
			description: "negative: another family's command 2 is not a tcp_metrics DEL",
			write:       true, family: "nlctrl", id: id, cmd: TCPMetricsCmdDel, err: ErrGenlWrite,
		},
		{
			description: "boundary: GENL_ID_CTRL, the lowest family id",
			family:      "nlctrl", id: unix.GENL_ID_CTRL, cmd: 3,
			expected: genlWant(t, unix.GENL_ID_CTRL, 0x1, 7, "03010000"),
		},
		{
			description: "boundary: one below it is NLMSG_MIN_TYPE - 1, a control type",
			family:      TCPMetricsGenlName, id: unix.GENL_ID_CTRL - 1, cmd: TCPMetricsCmdGet, err: ErrGenlFamilyID,
		},
		{
			description: "corner: a write to id 0 fails on the id, after the gate",
			write:       true, family: TCPMetricsGenlName, cmd: TCPMetricsCmdDel, err: ErrGenlFamilyID,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			build := BuildGenlRequest
			if tc.write {
				build = BuildGenlWriteRequest
			}
			got, err := build(tc.family, tc.id, tc.cmd, TCPMetricsGenlVersion, tc.flags, 7, tc.attrs)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err %v, want %v", err, tc.err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("\n got  %x\n want %x", got, tc.expected)
			}
		})
	}
}

// TestIsAdmittedGenlWrite pins the genl write gate.
//
// go test ./pkg/xtcpnl/ -run TestIsAdmittedGenlWrite
func TestIsAdmittedGenlWrite(t *testing.T) {
	tests := []struct {
		description string
		family      string
		cmd         uint8
		expected    bool
	}{
		{"positive: tcp_metrics DEL", TCPMetricsGenlName, TCPMetricsCmdDel, true},
		{"negative: tcp_metrics GET", TCPMetricsGenlName, TCPMetricsCmdGet, false},
		{"negative: nlctrl's command 2, CTRL_CMD_DELFAMILY, a write goip never sends", "nlctrl", 2, false},
		{"boundary: command 0, UNSPEC", TCPMetricsGenlName, 0, false},
		{"corner: the name is case-sensitive, as the controller's is", "TCP_METRICS", TCPMetricsCmdDel, false},
		{"corner: an unresolved family has no name", "", TCPMetricsCmdDel, false},
		{"positive: SEG6 SETHMAC, `sr hmac set`", Seg6GenlName, Seg6CmdSetHmac, true},
		{"positive: SEG6 SET_TUNSRC, `sr tunsrc set`", Seg6GenlName, Seg6CmdSetTunsrc, true},
		{"negative: SEG6 DUMPHMAC, `sr hmac show`", Seg6GenlName, Seg6CmdDumpHmac, false},
		{"negative: SEG6 GET_TUNSRC, `sr tunsrc show`", Seg6GenlName, Seg6CmdGetTunsrc, false},
		{"boundary: SEG6 command 5, past the last", Seg6GenlName, Seg6CmdGetTunsrc + 1, false},
		{"corner: SEG6's SETHMAC number is not a tcp_metrics write", TCPMetricsGenlName, Seg6CmdSetHmac, false},
		{"positive: IOAM6 ADD_NAMESPACE, `ioam namespace add`", IOAM6GenlName, IOAM6CmdAddNamespace, true},
		{"positive: IOAM6 DEL_NAMESPACE, `ioam namespace del`", IOAM6GenlName, IOAM6CmdDelNamespace, true},
		{"positive: IOAM6 ADD_SCHEMA, `ioam schema add`", IOAM6GenlName, IOAM6CmdAddSchema, true},
		{"positive: IOAM6 DEL_SCHEMA, `ioam schema del`", IOAM6GenlName, IOAM6CmdDelSchema, true},
		{"positive: IOAM6 NS_SET_SCHEMA, `ioam namespace set`", IOAM6GenlName, IOAM6CmdNsSetSchema, true},
		{"negative: IOAM6 DUMP_NAMESPACES, `ioam namespace show`", IOAM6GenlName, IOAM6CmdDumpNamespaces, false},
		{"negative: IOAM6 DUMP_SCHEMAS, `ioam schema show`", IOAM6GenlName, IOAM6CmdDumpSchemas, false},
		{"boundary: IOAM6 command 0, UNSPEC", IOAM6GenlName, 0, false},
		{"boundary: IOAM6 command 8, past the last", IOAM6GenlName, IOAM6CmdNsSetSchema + 1, false},
		{"positive: mptcp_pm ADD_ADDR, `mptcp endpoint add`", MPTCPPMGenlName, MPTCPPMCmdAddAddr, true},
		{"positive: mptcp_pm DEL_ADDR, `mptcp endpoint delete`", MPTCPPMGenlName, MPTCPPMCmdDelAddr, true},
		{"positive: mptcp_pm FLUSH_ADDRS, `mptcp endpoint flush`", MPTCPPMGenlName, MPTCPPMCmdFlushAddrs, true},
		{"positive: mptcp_pm SET_LIMITS, `mptcp limits set`", MPTCPPMGenlName, MPTCPPMCmdSetLimits, true},
		{"positive: mptcp_pm SET_FLAGS, `mptcp endpoint change`", MPTCPPMGenlName, MPTCPPMCmdSetFlags, true},
		{"negative: mptcp_pm GET_ADDR, `mptcp endpoint show`", MPTCPPMGenlName, MPTCPPMCmdGetAddr, false},
		{"negative: mptcp_pm GET_LIMITS, `mptcp limits show`", MPTCPPMGenlName, MPTCPPMCmdGetLimits, false},
		{"negative: mptcp_pm ANNOUNCE, the userspace path manager's, which ip does not send", MPTCPPMGenlName,
			MPTCPPMCmdAnnounce, false},
		{"boundary: mptcp_pm command 0, UNSPEC", MPTCPPMGenlName, 0, false},
		{"corner: mptcp_pm's SET_FLAGS number is not a SEG6 write", Seg6GenlName, MPTCPPMCmdSetFlags, false},
		{"positive: fou ADD, `fou add`", FOUGenlName, FOUCmdAdd, true},
		{"positive: fou DEL, `fou del`", FOUGenlName, FOUCmdDel, true},
		{"negative: fou GET, `fou show`", FOUGenlName, FOUCmdGet, false},
		{"boundary: fou command 0, UNSPEC", FOUGenlName, 0, false},
		{"corner: fou's ADD number is not a tcp_metrics write", TCPMetricsGenlName, FOUCmdAdd, false},
		{"positive: ila ADD, `ila add`", ILAGenlName, ILACmdAdd, true},
		{"positive: ila DEL, `ila del`", ILAGenlName, ILACmdDel, true},
		{"negative: ila GET, `ila list`", ILAGenlName, ILACmdGet, false},
		{"negative: ila FLUSH, which ip never sends", ILAGenlName, ILACmdFlush, false},
		{"boundary: ila command 0, UNSPEC", ILAGenlName, 0, false},
		{"positive: l2tp TUNNEL_CREATE, `l2tp add tunnel`", L2TPGenlName, L2TPCmdTunnelCreate, true},
		{"positive: l2tp TUNNEL_DELETE, `l2tp del tunnel`", L2TPGenlName, L2TPCmdTunnelDelete, true},
		{"positive: l2tp SESSION_CREATE, `l2tp add session`", L2TPGenlName, L2TPCmdSessionCreate, true},
		{"positive: l2tp SESSION_DELETE, `l2tp del session`", L2TPGenlName, L2TPCmdSessionDelete, true},
		{"negative: l2tp TUNNEL_GET, `l2tp show tunnel`", L2TPGenlName, L2TPCmdTunnelGet, false},
		{"negative: l2tp SESSION_GET, `l2tp show session`", L2TPGenlName, L2TPCmdSessionGet, false},
		{"negative: l2tp TUNNEL_MODIFY, which ip never sends", L2TPGenlName, L2TPCmdTunnelModify, false},
		{"negative: l2tp SESSION_MODIFY, which ip never sends", L2TPGenlName, L2TPCmdSessionModify, false},
		{"boundary: l2tp command 0, UNSPEC", L2TPGenlName, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsAdmittedGenlWrite(tc.family, tc.cmd); got != tc.expected {
				t.Errorf("IsAdmittedGenlWrite(%q, %d) = %v, want %v", tc.family, tc.cmd, got, tc.expected)
			}
		})
	}
}
