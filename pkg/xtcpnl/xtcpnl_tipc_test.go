package xtcpnl

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Crafted bytes below are modeled on the captured TIPC replies
// (testdata/7_3_1/ws/tipc), NOT raw kernel evidence; the O3/O4 corpus test
// (xtcpnl_ws_tipc_test.go) holds the parser to the real captures. They
// reuse the devlink tests' attribute builders (dlAttr, dlNest, dlBody, ...).

func u32p(p *uint32) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprint(*p)
}

func strp(p *string) string {
	if p == nil {
		return "-"
	}
	return *p
}

func tipcPropSum(p *TIPCProp) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("{%s %s %s %s %s %s}", u32p(p.Prio), u32p(p.Tol), u32p(p.Win), u32p(p.MTU),
		u32p(p.Broadcast), u32p(p.BroadcastRatio))
}

func tipcPublSum(p *TIPCPubl) string {
	return fmt.Sprintf("{%s %s %s %s %s %s}", u32p(p.Type), u32p(p.Lower), u32p(p.Upper), u32p(p.Scope),
		u32p(p.Node), u32p(p.Ref))
}

// tipcSum is a message's sections, in a fixed order, as one line: what a
// row's expected holds. A parse error appends " !err".
func tipcSum(m TIPCMessage, err error) string {
	var p []string
	if b := m.Bearer; b != nil {
		p = append(p, fmt.Sprintf("bearer %s prop=%s domain=%s udp=%d/%d", strp(b.Name), tipcPropSum(b.Prop),
			u32p(b.Domain), len(b.UDPLocal), len(b.UDPRemote)))
	}
	if s := m.Sock; s != nil {
		con := "-"
		if s.Con != nil {
			con = fmt.Sprintf("{%t %s %s %s %s}", s.Con.Flag, u32p(s.Con.Node), u32p(s.Con.Sock),
				u32p(s.Con.Type), u32p(s.Con.Inst))
		}
		p = append(p, fmt.Sprintf("sock ref=%s addr=%s publ=%t con=%s", u32p(s.Ref), u32p(s.Addr), s.HasPubl, con))
	}
	if m.Publ != nil {
		p = append(p, "publ "+tipcPublSum(m.Publ))
	}
	if l := m.Link; l != nil {
		p = append(p, fmt.Sprintf("link %s dest=%s mtu=%s bc=%t up=%t active=%t prop=%s stats=%d", strp(l.Name),
			u32p(l.Dest), u32p(l.MTU), l.Broadcast, l.Up, l.Active, tipcPropSum(l.Prop), len(l.Stats)))
	}
	if md := m.Media; md != nil {
		p = append(p, fmt.Sprintf("media %s prop=%s", strp(md.Name), tipcPropSum(md.Prop)))
	}
	if n := m.Node; n != nil {
		p = append(p, fmt.Sprintf("node addr=%s up=%t id=%q", u32p(n.Addr), n.Up, n.ID))
	}
	if n := m.Net; n != nil {
		w0, w1 := "-", "-"
		if n.NodeID != nil {
			w0 = fmt.Sprintf("%#x", *n.NodeID)
		}
		if n.NodeIDW1 != nil {
			w1 = fmt.Sprintf("%#x", *n.NodeIDW1)
		}
		p = append(p, fmt.Sprintf("net id=%s addr=%s nodeid=%s/%s", u32p(n.ID), u32p(n.Addr), w0, w1))
	}
	if m.NameTable != nil {
		p = append(p, "nametable "+tipcPublSum(m.NameTable))
	}
	if mo := m.Mon; mo != nil {
		p = append(p, fmt.Sprintf("mon ref=%s thr=%s bearer=%s peers=%s gen=%s active=%t", u32p(mo.Ref),
			u32p(mo.ActivationThreshold), strp(mo.BearerName), u32p(mo.PeerCnt), u32p(mo.ListGen), mo.Active))
	}
	if mp := m.MonPeer; mp != nil {
		p = append(p, fmt.Sprintf("monpeer addr=%s members=%v up=%t head=%t local=%t", u32p(mp.Addr), mp.Members,
			mp.Up, mp.Head, mp.Local))
	}
	s := strings.Join(p, "; ")
	if s == "" {
		s = "none"
	}
	if err != nil {
		s += " !err"
	}
	return s
}

func tipcProp(attrs ...[]byte) []byte { return dlNest(TIPCBearerProp, attrs...) }

// tipcEthBearer is the eth fixture's BEARER_GET answer: name, prop
// (priority 10, tolerance 1500, window 8191) and domain.
func tipcEthBearer() []byte {
	return dlNest(TIPCAttrBearer,
		dlAttr(TIPCBearerName, dlStr("eth:d0")...),
		tipcProp(dlAttr(TIPCPropPrio, dlU32(10)...), dlAttr(TIPCPropTol, dlU32(1500)...),
			dlAttr(TIPCPropWin, dlU32(8191)...)),
		dlAttr(TIPCBearerDomain, dlU32(0)...))
}

// tipcPeerLinkNest is the peer fixture's unicast link: PROP holds PRIO
// twice, as the kernel puts it (net/tipc/link.c:2708, 2715).
func tipcPeerLinkNest(prio2 uint32, extra ...[]byte) []byte {
	children := append([][]byte{
		dlAttr(TIPCLinkName, dlStr("1001:v0-1002:v1")...),
		dlAttr(tipcLinkDest, dlU32(0x31303032)...),
		dlAttr(tipcLinkMTU, dlU32(1500)...),
		dlAttr(tipcLinkUp), dlAttr(tipcLinkActive),
		dlNest(tipcLinkProp, dlAttr(TIPCPropPrio, dlU32(10)...), dlAttr(TIPCPropTol, dlU32(1500)...),
			dlAttr(TIPCPropWin, dlU32(50)...), dlAttr(TIPCPropPrio, dlU32(prio2)...)),
		dlNest(tipcLinkStats, dlAttr(TIPCStatsRxInfo, dlU32(2)...), dlAttr(TIPCStatsTxInfo, dlU32(2)...)),
	}, extra...)
	return dlNest(TIPCAttrLink, children...)
}

func tipcNameTablePublNest(scope uint32) []byte {
	return dlNest(TIPCAttrNameTable, dlNest(tipcNameTablePubl,
		dlAttr(tipcPublType, dlU32(1000)...), dlAttr(tipcPublLower, dlU32(1)...), dlAttr(tipcPublUpper, dlU32(1)...),
		dlAttr(tipcPublScope, dlU32(scope)...), dlAttr(tipcPublNode, dlU32(0x31303031)...),
		dlAttr(tipcPublRef, dlU32(1639285828)...), dlAttr(tipcPublKey, dlU32(7)...)))
}

// TestParseTIPC decodes one crafted message per row, held to its summary:
// each section that decoded, and " !err" when the parse failed.
//
// go test ./pkg/xtcpnl/ -run TestParseTIPC
func TestParseTIPC(t *testing.T) {
	longName := strings.Repeat("b", 31) // TIPC_MAX_BEARER_NAME is 32 with its NUL
	tests := []struct {
		description string
		body        []byte
		flags       uint16
		expected    string
	}{
		{description: "positive: an eth bearer's name, properties and domain",
			body:     dlBody(TIPCCmdBearerGet, tipcEthBearer()),
			expected: "bearer eth:d0 prop={10 1500 8191 - - -} domain=0 udp=0/0"},
		{description: "positive: a udp bearer's local sockaddr",
			body: dlBody(TIPCCmdBearerGet, dlNest(TIPCAttrBearer, dlAttr(TIPCBearerName, dlStr("udp:u0")...),
				dlNest(TIPCBearerUDPOpts, dlAttr(tipcUDPLocal, make([]byte, 128)...)))),
			expected: "bearer udp:u0 prop=- domain=- udp=128/0"},
		{description: "positive: the broadcast link: window and mode, no priority",
			body: dlBody(TIPCCmdLinkGet, dlNest(TIPCAttrLink, dlAttr(TIPCLinkName, dlStr("broadcast-link")...),
				dlAttr(TIPCLinkBroadcast), dlAttr(tipcLinkUp),
				dlNest(tipcLinkProp, dlAttr(TIPCPropWin, dlU32(50)...), dlAttr(TIPCPropBroadcast, dlU32(0)...)),
				dlNest(tipcLinkStats, dlAttr(TIPCStatsRxInfo, dlU32(0)...)))),
			expected: "link broadcast-link dest=- mtu=- bc=true up=true active=false prop={- - 50 - 0 -} stats=1"},
		{description: "positive: a unicast link whose PROP repeats PRIO with the same value, as the kernel sends it",
			body:     dlBody(TIPCCmdLinkGet, tipcPeerLinkNest(10)),
			expected: "link 1001:v0-1002:v1 dest=825241650 mtu=1500 bc=false up=true active=true prop={10 1500 50 - - -} stats=2"},
		{description: "positive: net get, the default netid and the identity's hash",
			body: dlBody(TIPCCmdNetGet, dlNest(TIPCAttrNet, dlAttr(tipcNetID, dlU32(4711)...),
				dlAttr(tipcNetAddr, dlU32(0x31303031)...), dlAttr(tipcNetNodeID, dlU64(0x31303031)...),
				dlAttr(tipcNetNodeIDW1, dlU64(0)...))),
			expected: "net id=4711 addr=825241649 nodeid=0x31303031/0x0"},
		{description: "positive: a name-table publication",
			body:     dlBody(TIPCCmdNameTableGet, tipcNameTablePublNest(3)),
			expected: "nametable {1000 1 1 3 825241649 1639285828}"},
		{description: "positive: a connected socket, its peer and the name it connected by",
			body: dlBody(TIPCCmdSockGet, dlNest(TIPCAttrSock, dlAttr(TIPCSockAddr, dlU32(0)...),
				dlAttr(TIPCSockRef, dlU32(274500323)...),
				dlNest(TIPCSockCon, dlAttr(tipcConFlag), dlAttr(tipcConNode, dlU32(0)...),
					dlAttr(tipcConSock, dlU32(1590099062)...), dlAttr(tipcConType, dlU32(2000)...),
					dlAttr(tipcConInst, dlU32(1)...)))),
			expected: "sock ref=274500323 addr=0 publ=false con={true 0 1590099062 2000 1}"},
		{description: "positive: a bound socket has HAS_PUBL",
			body: dlBody(TIPCCmdSockGet, dlNest(TIPCAttrSock, dlAttr(TIPCSockRef, dlU32(9)...),
				dlAttr(TIPCSockHasPubl))),
			expected: "sock ref=9 addr=- publ=true con=-"},
		{description: "positive: a PUBL_GET request names its socket by ref",
			body:     dlBody(TIPCCmdPublGet, dlNest(TIPCAttrSock|unix.NLA_F_NESTED, dlAttr(TIPCSockRef, dlU32(9)...))),
			flags:    unix.NLM_F_REQUEST | unix.NLM_F_DUMP,
			expected: "sock ref=9 addr=- publ=false con=-"},
		{description: "positive: a monitor peer and its domain members",
			body: dlBody(TIPCCmdMonPeerGet, dlNest(TIPCAttrMonPeer, dlAttr(tipcMonPeerAddr, dlU32(0x31303031)...),
				dlAttr(tipcMonPeerMembers, join(dlU32(0x31303032), dlU32(0x31303033))...),
				dlAttr(tipcMonPeerUp), dlAttr(tipcMonPeerHead))),
			expected: "monpeer addr=825241649 members=[825241650 825241651] up=true head=true local=false"},
		{description: "positive: the local monitor peer: up, not head, no domain (ec-037)",
			body: dlBody(TIPCCmdMonPeerGet, dlNest(TIPCAttrMonPeer, dlAttr(tipcMonPeerAddr, dlU32(0x31303031)...),
				dlAttr(tipcMonPeerApplied, dlU32(0)...), dlAttr(tipcMonPeerUp), dlAttr(tipcMonPeerLocal))),
			expected: "monpeer addr=825241649 members=[] up=true head=false local=true"},
		{description: "positive: monitor summary for one bearer",
			body: dlBody(TIPCCmdMonGet, dlNest(TIPCAttrMon, dlAttr(TIPCMonRef, dlU32(0)...),
				dlAttr(tipcMonBearerName, dlStr("eth:d0")...), dlAttr(tipcMonPeerCnt, dlU32(1)...),
				dlAttr(tipcMonListGen, dlU32(0)...))),
			expected: "mon ref=0 thr=- bearer=eth:d0 peers=1 gen=0 active=false"},
		{description: "negative: a body shorter than its genlmsghdr",
			body:     []byte{TIPCCmdLinkGet, 1, 0},
			expected: "none !err"},
		{description: "negative: a 3-byte tail after a valid nest fails the framing",
			body:     append(dlBody(TIPCCmdBearerGet, tipcEthBearer()), 1, 2, 3),
			expected: "none !err"},
		{description: "negative: a 2-byte MTU, the fields before it kept",
			body: dlBody(TIPCCmdLinkGet, dlNest(TIPCAttrLink, dlAttr(TIPCLinkName, dlStr("l")...),
				dlAttr(tipcLinkMTU, dlU16(1500)...))),
			expected: "link l dest=- mtu=- bc=false up=false active=false prop=- stats=0 !err"},
		{description: "negative: a repeated property that disagrees",
			body:     dlBody(TIPCCmdLinkGet, tipcPeerLinkNest(11)),
			expected: "link 1001:v0-1002:v1 dest=825241650 mtu=1500 bc=false up=true active=true prop={10 1500 50 - - -} stats=0 !err"},
		{description: "negative: a repeated bearer name",
			body: dlBody(TIPCCmdBearerGet, dlNest(TIPCAttrBearer, dlAttr(TIPCBearerName, dlStr("a")...),
				dlAttr(TIPCBearerName, dlStr("b")...))),
			expected: "bearer - prop=- domain=- udp=0/0 !err"},
		{description: "negative: a top-level BEARER that is a u32, not a nest",
			body:     dlBody(TIPCCmdBearerGet, dlAttr(TIPCAttrBearer, dlU32(7)...)),
			expected: "none !err"},
		{description: "negative: monitor members not a whole number of u32s",
			body: dlBody(TIPCCmdMonPeerGet, dlNest(TIPCAttrMonPeer, dlAttr(tipcMonPeerAddr, dlU32(1)...),
				dlAttr(tipcMonPeerMembers, 1, 2, 3))),
			expected: "monpeer addr=1 members=[] up=false head=false local=false !err"},
		{description: "boundary: a reply with no attributes decodes to no section",
			body:     dlBody(TIPCCmdLinkGet),
			expected: "none"},
		{description: "boundary: publication scope 3, node, the largest scope",
			body:     dlBody(TIPCCmdNameTableGet, tipcNameTablePublNest(3)),
			expected: "nametable {1000 1 1 3 825241649 1639285828}"},
		{description: "boundary: a bearer name of TIPC_MAX_BEARER_NAME-1 bytes is kept whole",
			body:     dlBody(TIPCCmdBearerGet, dlNest(TIPCAttrBearer, dlAttr(TIPCBearerName, dlStr(longName)...))),
			expected: "bearer " + longName + " prop=- domain=- udp=0/0"},
		{description: "boundary: an empty monitor domain is an empty member list",
			body: dlBody(TIPCCmdMonPeerGet, dlNest(TIPCAttrMonPeer, dlAttr(tipcMonPeerAddr, dlU32(1)...),
				dlAttr(tipcMonPeerMembers), dlAttr(tipcMonPeerLocal))),
			expected: "monpeer addr=1 members=[] up=false head=false local=true"},
		{description: "corner: scope 4, out of tipc's table, is decoded as sent",
			body:     dlBody(TIPCCmdNameTableGet, tipcNameTablePublNest(4)),
			expected: "nametable {1000 1 1 4 825241649 1639285828}"},
		{description: "corner: a top-level attribute past TIPC_NLA_MAX is skipped, its siblings decoded",
			body:     dlBody(TIPCCmdBearerGet, dlAttr(TIPCAttrMonPeer+5, dlU32(1)...), tipcEthBearer()),
			expected: "bearer eth:d0 prop={10 1500 8191 - - -} domain=0 udp=0/0"},
		{description: "corner: a stats id past TIPC_NLA_STATS_MAX is skipped",
			body: dlBody(TIPCCmdLinkGet, dlNest(TIPCAttrLink, dlAttr(TIPCLinkName, dlStr("l")...),
				dlNest(tipcLinkStats, dlAttr(TIPCStatsRxInfo, dlU32(1)...), dlAttr(TIPCStatsMax+1, dlU32(9)...)))),
			expected: "link l dest=- mtu=- bc=false up=false active=false prop=- stats=1"},
		{description: "corner: NLA_F_NESTED on a scalar is refused, as every genl scalar here",
			body: dlBody(TIPCCmdLinkGet, dlNest(TIPCAttrLink, dlAttr(TIPCLinkName, dlStr("l")...),
				dlAttr(tipcLinkMTU|unix.NLA_F_NESTED, dlU32(1500)...))),
			expected: "link l dest=- mtu=- bc=false up=false active=false prop=- stats=0 !err"},
		{description: "corner: a nest with NLA_F_NESTED set decodes as one without",
			body:     dlBody(TIPCCmdMediaGet, dlNest(TIPCAttrMedia|unix.NLA_F_NESTED, dlAttr(TIPCMediaName, dlStr("udp")...))),
			expected: "media udp prop=-"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			m, err := ParseTIPC(tc.body, tc.flags)
			if err != nil && !errors.Is(err, ErrGenericNetlink) {
				t.Fatalf("error %v is not ErrGenericNetlink", err)
			}
			if got := tipcSum(m, err); got != tc.expected {
				t.Errorf("got  %q\nwant %q (err %v)", got, tc.expected, err)
			}
			if m.Request != (tc.flags&unix.NLM_F_REQUEST != 0) {
				t.Errorf("request %t, flags %#x", m.Request, tc.flags)
			}
		})
	}
}

// TestTIPCStatsIDs pins the stats ids the clone prints by against the
// uapi enum's order: the iota block must end at TIPC_NLA_STATS_MAX.
//
// go test ./pkg/xtcpnl/ -run TestTIPCStatsIDs
func TestTIPCStatsIDs(t *testing.T) {
	tests := []struct {
		description string
		got         uint16
		expected    uint16
	}{
		{description: "boundary: the first id is RX_INFO, 1", got: TIPCStatsRxInfo, expected: 1},
		{description: "positive: MSG_PROF_TOT is 11", got: TIPCStatsMsgProfTot, expected: 11},
		{description: "positive: RX_STATES is 21", got: TIPCStatsRxStates, expected: 21},
		{description: "boundary: the last id is AVG_QUEUE, TIPC_NLA_STATS_MAX", got: TIPCStatsAvgQueue, expected: TIPCStatsMax},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %d, want %d", tc.got, tc.expected)
			}
		})
	}
}

// FuzzParseTIPC holds ParseTIPC to its invariants on any input: no panic,
// every error is ErrGenericNetlink, a successful parse keeps every
// top-level attribute it was given, and stats ids stay in the enum.
//
// go test ./pkg/xtcpnl/ -run '^$' -fuzz FuzzParseTIPC
func FuzzParseTIPC(f *testing.F) {
	f.Add(dlBody(TIPCCmdBearerGet, tipcEthBearer()), uint16(0))
	f.Add(dlBody(TIPCCmdLinkGet, tipcPeerLinkNest(10)), uint16(0))
	f.Add(dlBody(TIPCCmdNameTableGet, tipcNameTablePublNest(3)), uint16(0))
	f.Add(dlBody(TIPCCmdPublGet, dlNest(TIPCAttrSock, dlAttr(TIPCSockRef, dlU32(9)...))), uint16(unix.NLM_F_REQUEST))
	f.Add(dlBody(TIPCCmdMonPeerGet, dlNest(TIPCAttrMonPeer, dlAttr(tipcMonPeerMembers, dlU32(1)...))), uint16(0))
	f.Add(dlBody(TIPCCmdNetGet, dlNest(TIPCAttrNet, dlAttr(tipcNetID, dlU32(4711)...))), uint16(0))
	f.Fuzz(func(t *testing.T, body []byte, flags uint16) {
		m, err := ParseTIPC(body, flags)
		if err != nil {
			if !errors.Is(err, ErrGenericNetlink) {
				t.Fatalf("error %v is not ErrGenericNetlink", err)
			}
			return
		}
		g, gerr := ParseGenericNetlink(body)
		if gerr != nil || len(g.Attributes) != len(m.Attributes) || g.Command != m.Command {
			t.Fatalf("message %+v differs from its generic parse %+v (%v)", m.GenericNetlinkMessage, g, gerr)
		}
		if l := m.Link; l != nil && l.Stats != nil {
			for id := range l.Stats {
				if id == 0 || id > TIPCStatsMax {
					t.Fatalf("stats id %d out of range", id)
				}
			}
		}
	})
}

// TestBuildTIPCRequest pins the requests tipc sends, byte for byte after
// the 16-byte header, against the captured ones (testdata/7_3_1/ws/tipc).
//
// go test ./pkg/xtcpnl/ -run TestBuildTIPCRequest
func TestBuildTIPCRequest(t *testing.T) {
	long := strings.Repeat("x", 300)
	tests := []struct {
		description string
		build       func() ([]byte, error)
		expected    string // flags and the body in hex, or "error"
	}{
		{description: "positive: the family lookup, version 1 with an ack (every row)",
			build:    func() ([]byte, error) { return BuildTIPCFamilyRequest(1) },
			expected: "0x5 03010000 0b0002005449504376320000"},
		{description: "positive: link get broadcast, a nested link name (ec-022)",
			build: func() ([]byte, error) {
				return BuildTIPCRequest(42, TIPCCmdLinkGet, unix.NLM_F_ACK, 1, TIPCAttrLink,
					func(ab *AttrBuilder) error { return ab.PutString(TIPCLinkName, "broadcast-link") })
			},
			expected: "0x5 08010000 180004801300010062726f6164636173742d6c696e6b0000"},
		{description: "positive: a PUBL_GET dump naming its socket (ec-010)",
			build: func() ([]byte, error) {
				return BuildTIPCRequest(42, TIPCCmdPublGet, unix.NLM_F_DUMP, 1, TIPCAttrSock,
					func(ab *AttrBuilder) error { return ab.PutU32(TIPCSockRef, 0xd348c5ae) })
			},
			expected: "0x301 07010000 0c00028008000200aec548d3"},
		{description: "boundary: a dump with no attributes, the genl header alone (ec-005)",
			build:    func() ([]byte, error) { return BuildTIPCRequest(42, TIPCCmdBearerGet, unix.NLM_F_DUMP, 1, 0, nil) },
			expected: "0x301 04010000 "},
		{description: "corner: a nest with nothing in it is a bare header with NLA_F_NESTED",
			build: func() ([]byte, error) {
				return BuildTIPCRequest(42, TIPCCmdMonGet, unix.NLM_F_ACK, 1, TIPCAttrMon, nil)
			},
			expected: "0x5 12010000 04000980"},
		{description: "negative: an id below the controller's is refused",
			build:    func() ([]byte, error) { return BuildTIPCRequest(15, TIPCCmdLinkGet, unix.NLM_F_DUMP, 1, 0, nil) },
			expected: "error"},
		{description: "negative: a name too long for the request buffer is refused",
			build: func() ([]byte, error) {
				return BuildTIPCRequest(42, TIPCCmdLinkGet, unix.NLM_F_ACK, 1, TIPCAttrLink,
					func(ab *AttrBuilder) error { return ab.PutString(TIPCLinkName, long) })
			},
			expected: "error"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := tc.build()
			got := "error"
			if err == nil {
				var h NlMsgHdr
				if _, err := DeserializeNlMsgHdr(b, &h); err != nil {
					t.Fatal(err)
				}
				got = fmt.Sprintf("%#x %x %x", h.Flags, b[NlMsgHdrSizeCst:NlMsgHdrSizeCst+4], b[NlMsgHdrSizeCst+4:])
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
