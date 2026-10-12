package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// rdSockaddr is a 128-byte sockaddr_storage: family in host order, port in
// network order, and for AF_INET and AF_INET6 the address at its offset.
func rdSockaddr(family, port uint16, addr netip.Addr) []byte {
	b := make([]byte, rdmaSockaddrLenCst)
	binary.LittleEndian.PutUint16(b[0:2], family)
	binary.BigEndian.PutUint16(b[2:4], port)
	switch {
	case addr.Is4():
		a := addr.As4()
		copy(b[rdmaSinAddrOffCst:], a[:])
	case addr.Is6():
		a := addr.As16()
		copy(b[rdmaSin6AddrOffCst:], a[:])
	}
	return b
}

func TestParseRdmaResObjects(t *testing.T) {
	op := func(cmd uint16) uint16 { return RdmaNlGetType(RdmaNlNldev, cmd) }
	handle := rdBody(rdU32(RdmaNldevAttrDevIndex, 0), rdStr(RdmaNldevAttrDevName, "rxe0"))
	rxe0 := RdmaHandle{HasDevIndex: true, DevName: "rxe0", HasDevName: true}
	rxe0p1 := rxe0
	rxe0p1.PortIndex, rxe0p1.HasPortIndex = 1, true
	ibCore := RdmaResOwner{KernName: "ib_core", HasKernName: true}
	pid100 := RdmaResOwner{PID: 100, HasPID: true}
	gsi := func(extra ...[]byte) []byte {
		return rdNest(RdmaNldevAttrResQPEntry, append([][]byte{rdU32(RdmaNldevAttrPortIndex, 1),
			rdU32(RdmaNldevAttrResLQPN, 1), rdU32(RdmaNldevAttrResSqPSN, 0), rdU8(RdmaNldevAttrResType, 1),
			rdU8(RdmaNldevAttrResState, 3), rdStr(RdmaNldevAttrResKernName, "ib_core")}, extra...)...)
	}
	gsiQP := RdmaResQP{PortIndex: 1, HasPortIndex: true, LQPN: 1, HasLQPN: true, HasSQPSN: true,
		Type: 1, HasType: true, State: 3, HasState: true, RdmaResOwner: ibCore}
	listen := rdSockaddr(rdmaAfInetCst, 7174, netip.MustParseAddr("10.99.0.1"))
	cmidEntry := func(src []byte) []byte {
		return rdNest(RdmaNldevAttrResCMIDEntry, rdU32(RdmaNldevAttrPortIndex, 1), rdU32(RdmaNldevAttrResPS, 0x0106),
			rdU8(RdmaNldevAttrResState, 7), rtattr(RdmaNldevAttrResSrcAddr, src), rdU32(RdmaNldevAttrResCMIDN, 0),
			rdU32(RdmaNldevAttrResPID, 100))
	}
	srqQPs := func(listType uint16) []byte {
		return rtattr(listType, rdBody(rdNest(RdmaNldevAttrResQPEntry, rdU32(RdmaNldevAttrResLQPN, 5)),
			rdNest(RdmaNldevAttrResQPEntry, rdU32(RdmaNldevAttrMinRange, 7), rdU32(RdmaNldevAttrMaxRange, 22))))
	}
	srqEntry := func(listType uint16) []byte {
		return rdNest(RdmaNldevAttrResSRQ, rdNest(RdmaNldevAttrResSRQEntry, rdU32(RdmaNldevAttrResSRQN, 1),
			rdU8(RdmaNldevAttrResType, 0), rdU32(RdmaNldevAttrResPDN, 2), srqQPs(listType), rdU32(RdmaNldevAttrResPID, 100)))
	}
	srq := RdmaResSRQs{RdmaHandle: rxe0, Entries: []RdmaResSRQ{{SRQN: 1, HasSRQN: true, HasType: true, PDN: 2, HasPDN: true,
		QPs: []RdmaQPRange{{5, 5}, {7, 22}}, RdmaResOwner: pid100}}}
	tests := []struct {
		description string
		msgType     uint16
		body        []byte
		expected    any
		expectedErr error
	}{
		{description: "positive: QP dump with the kernel GSI QP", msgType: op(RdmaNldevCmdResQPGet),
			body:     rdBody(handle, rdNest(RdmaNldevAttrResQP, gsi())),
			expected: RdmaResQPs{RdmaHandle: rxe0, Entries: []RdmaResQP{gsiQP}}},
		{description: "positive: QP doit, flat", msgType: op(RdmaNldevCmdResQPGet),
			body: rdBody(handle, rdU32(RdmaNldevAttrPortIndex, 1), rdU32(RdmaNldevAttrResLQPN, 7),
				rdU32(RdmaNldevAttrResPDN, 2), rdU32(RdmaNldevAttrResPID, 100), rdU8(RdmaNldevAttrResType, 2),
				rdU8(RdmaNldevAttrResState, 2), rdU32(RdmaNldevAttrResRQPN, 8), rdU32(RdmaNldevAttrResRqPSN, 9),
				rdU32(RdmaNldevAttrResSqPSN, 10), rdU8(RdmaNldevAttrResPathMigState, 0)),
			expected: RdmaResQPs{RdmaHandle: rxe0p1, Flat: true, Entries: []RdmaResQP{{PortIndex: 1, HasPortIndex: true,
				LQPN: 7, HasLQPN: true, PDN: 2, HasPDN: true, RQPN: 8, HasRQPN: true, RQPSN: 9, HasRQPSN: true,
				SQPSN: 10, HasSQPSN: true, HasPathMigState: true, Type: 2, HasType: true, State: 2, HasState: true,
				RdmaResOwner: pid100}}}},
		{description: "positive: CQ kernel entry", msgType: op(RdmaNldevCmdResCQGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResCQ, rdNest(RdmaNldevAttrResCQEntry, rdU32(RdmaNldevAttrResCQE, 512),
				rdU64(RdmaNldevAttrResUsecnt, 2), rdU8(RdmaNldevAttrResPollCtx, 3), rdU8(RdmaNldevAttrDevDIM, 0),
				rdU32(RdmaNldevAttrResCQN, 1), rdStr(RdmaNldevAttrResKernName, "ib_core")))),
			expected: RdmaResCQs{RdmaHandle: rxe0, Entries: []RdmaResCQ{{CQE: 512, HasCQE: true, UseCnt: 2, HasUseCnt: true,
				PollCtx: 3, HasPollCtx: true, HasDIM: true, CQN: 1, HasCQN: true, RdmaResOwner: ibCore}}}},
		{description: "positive: MR with keys", msgType: op(RdmaNldevCmdResMRGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResMR, rdNest(RdmaNldevAttrResMREntry, rdU32(RdmaNldevAttrResRKey, 0x1234),
				rdU32(RdmaNldevAttrResLKey, 0x1234), rdU64(RdmaNldevAttrResMrlen, 4096), rdU32(RdmaNldevAttrResMRN, 1),
				rdU32(RdmaNldevAttrResPDN, 2), rdU32(RdmaNldevAttrResPID, 100)))),
			expected: RdmaResMRs{RdmaHandle: rxe0, Entries: []RdmaResMR{{RKey: 0x1234, HasRKey: true, LKey: 0x1234, HasLKey: true,
				MRLen: 4096, HasMRLen: true, MRN: 1, HasMRN: true, PDN: 2, HasPDN: true, RdmaResOwner: pid100}}}},
		{description: "positive: PD user entry", msgType: op(RdmaNldevCmdResPDGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResPD, rdNest(RdmaNldevAttrResPDEntry, rdU32(RdmaNldevAttrResLocalDMALKey, 0),
				rdU64(RdmaNldevAttrResUsecnt, 3), rdU32(RdmaNldevAttrResPDN, 2), rdU32(RdmaNldevAttrResCtxn, 1),
				rdU32(RdmaNldevAttrResPID, 100)))),
			expected: RdmaResPDs{RdmaHandle: rxe0, Entries: []RdmaResPD{{HasLocalDMALKey: true, UseCnt: 3, HasUseCnt: true,
				PDN: 2, HasPDN: true, CTXN: 1, HasCTXN: true, RdmaResOwner: pid100}}}},
		{description: "positive: SRQ with a single QP and a range", msgType: op(RdmaNldevCmdResSRQGet),
			body: rdBody(handle, srqEntry(RdmaNldevAttrResQP)), expected: srq},
		{description: "positive: CM_ID listening on AF_INET", msgType: op(RdmaNldevCmdResCMIDGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResCMID, cmidEntry(listen))),
			expected: RdmaResCMIDs{RdmaHandle: rxe0, Entries: []RdmaResCMID{{PortIndex: 1, HasPortIndex: true,
				PS: 0x0106, HasPS: true, State: 7, HasState: true, HasSrc: true,
				Src:      RdmaSockaddr{Family: rdmaAfInetCst, Port: 7174, Addr: netip.MustParseAddr("10.99.0.1")},
				HasCMIDN: true, RdmaResOwner: pid100}}}},
		{description: "positive: CTX user entry", msgType: op(RdmaNldevCmdResCtxGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResCtx, rdNest(RdmaNldevAttrResCtxEntry,
				rdU32(RdmaNldevAttrResCtxn, 1), rdU32(RdmaNldevAttrResPID, 100)))),
			expected: RdmaResCtxs{RdmaHandle: rxe0, Entries: []RdmaResCtx{{CTXN: 1, HasCTXN: true, RdmaResOwner: pid100}}}},
		{description: "negative: SRC_ADDR of 16 bytes", msgType: op(RdmaNldevCmdResCMIDGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResCMID, cmidEntry(listen[:16]))), expectedErr: ErrRdmaAttrLen},
		{description: "negative: LQPN of 2 bytes", msgType: op(RdmaNldevCmdResQPGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResQP, rdNest(RdmaNldevAttrResQPEntry,
				rtattr(RdmaNldevAttrResLQPN, []byte{1, 0})))), expectedErr: ErrRdmaAttrLen},
		{description: "negative: USECNT of 4 bytes", msgType: op(RdmaNldevCmdResCQGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResCQ, rdNest(RdmaNldevAttrResCQEntry,
				rdU32(RdmaNldevAttrResUsecnt, 2)))), expectedErr: ErrRdmaAttrLen},
		{description: "negative: KERN_NAME without its NUL", msgType: op(RdmaNldevCmdResQPGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResQP, rdNest(RdmaNldevAttrResQPEntry,
				rtattr(RdmaNldevAttrResKernName, []byte("ib_core"))))), expectedErr: ErrRdmaAttrString},
		{description: "negative: an entry nest of 2 bytes", msgType: op(RdmaNldevCmdResQPGet),
			body:        rdBody(handle, rdNest(RdmaNldevAttrResQP, rtattr(RdmaNldevAttrResQPEntry, []byte{0, 0}))),
			expectedErr: ErrRdmaAttrLen},
		{description: "negative: a table whose rta_len runs past the body", msgType: op(RdmaNldevCmdResQPGet),
			body: rdBody(handle, rtattrOverrunning(RdmaNldevAttrResQP, 64)), expectedErr: ErrRTAttrSmall},
		{description: "boundary: an empty table nest", msgType: op(RdmaNldevCmdResQPGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResQP)), expected: RdmaResQPs{RdmaHandle: rxe0, Entries: []RdmaResQP{}}},
		{description: "boundary: a CTX entry with no attributes", msgType: op(RdmaNldevCmdResCtxGet),
			body:     rdBody(handle, rdNest(RdmaNldevAttrResCtx, rdNest(RdmaNldevAttrResCtxEntry))),
			expected: RdmaResCtxs{RdmaHandle: rxe0, Entries: []RdmaResCtx{{}}}},
		{description: "boundary: an empty body", msgType: op(RdmaNldevCmdResMRGet), expected: RdmaResMRs{Flat: true}},
		{description: "boundary: LQPN 0xffffff, the 24-bit maximum", msgType: op(RdmaNldevCmdResQPGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResQP, rdNest(RdmaNldevAttrResQPEntry,
				rdU32(RdmaNldevAttrResLQPN, 0xffffff)))),
			expected: RdmaResQPs{RdmaHandle: rxe0, Entries: []RdmaResQP{{LQPN: 16777215, HasLQPN: true}}}},
		{description: "corner: the last duplicate inside an entry wins", msgType: op(RdmaNldevCmdResQPGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResQP, rdNest(RdmaNldevAttrResQPEntry,
				rdU32(RdmaNldevAttrResLQPN, 1), rdU32(RdmaNldevAttrResLQPN, 9)))),
			expected: RdmaResQPs{RdmaHandle: rxe0, Entries: []RdmaResQP{{LQPN: 9, HasLQPN: true}}}},
		{description: "corner: an unknown attribute inside an entry is the entry's", msgType: op(RdmaNldevCmdResQPGet),
			body: rdBody(handle, rdNest(RdmaNldevAttrResQP, gsi(rdU64(119, 5)))),
			expected: RdmaResQPs{RdmaHandle: rxe0, Entries: []RdmaResQP{func() RdmaResQP {
				q := gsiQP
				q.Unknown = []uint16{119}
				return q
			}()}}},
		{description: "corner: NLA_F_NESTED on the SRQ's inner QP list", msgType: op(RdmaNldevCmdResSRQGet),
			body: rdBody(handle, srqEntry(RdmaNldevAttrResQP|unix.NLA_F_NESTED)), expected: srq},
		{description: "corner: a foreign entry type in the table is the table's Unknown", msgType: op(RdmaNldevCmdResQPGet),
			body:     rdBody(handle, rdNest(RdmaNldevAttrResQP, rdNest(RdmaNldevAttrResCQEntry, rdU32(RdmaNldevAttrResCQN, 1)))),
			expected: RdmaResQPs{RdmaHandle: rxe0, Entries: []RdmaResQP{}, Unknown: []uint16{RdmaNldevAttrResCQEntry}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseRdmaMsg(tc.msgType, tc.body)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

func TestParseRdmaSockaddr(t *testing.T) {
	const afPacket = 17
	tests := []struct {
		description string
		input       []byte
		expected    RdmaSockaddr
	}{
		{description: "positive: AF_INET", input: rdSockaddr(rdmaAfInetCst, 7174, netip.MustParseAddr("10.99.0.1")),
			expected: RdmaSockaddr{Family: rdmaAfInetCst, Port: 7174, Addr: netip.MustParseAddr("10.99.0.1")}},
		{description: "positive: AF_INET6", input: rdSockaddr(rdmaAfInet6Cst, 1, netip.MustParseAddr("::1")),
			expected: RdmaSockaddr{Family: rdmaAfInet6Cst, Port: 1, Addr: netip.MustParseAddr("::1")}},
		{description: "negative: family 0, unset", input: make([]byte, rdmaSockaddrLenCst), expected: RdmaSockaddr{}},
		{description: "negative: an AF_INET buffer one byte short is the zero value",
			input: rdSockaddr(rdmaAfInetCst, 7174, netip.MustParseAddr("10.99.0.1"))[:rdmaSockaddrLenCst-1], expected: RdmaSockaddr{}},
		{description: "boundary: port 65535", input: rdSockaddr(rdmaAfInetCst, 65535, netip.MustParseAddr("10.99.0.1")),
			expected: RdmaSockaddr{Family: rdmaAfInetCst, Port: 65535, Addr: netip.MustParseAddr("10.99.0.1")}},
		{description: "corner: a v4-mapped AF_INET6 address stays mapped",
			input:    rdSockaddr(rdmaAfInet6Cst, 0, netip.MustParseAddr("::ffff:10.99.0.1")),
			expected: RdmaSockaddr{Family: rdmaAfInet6Cst, Addr: netip.MustParseAddr("::ffff:10.99.0.1")}},
		{description: "corner: an unknown family sets Family only",
			input: func() []byte {
				b := rdSockaddr(afPacket, 0, netip.Addr{})
				copy(b[2:], []byte{1, 2, 3, 4, 5, 6})
				return b
			}(),
			expected: RdmaSockaddr{Family: afPacket}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := parseRdmaSockaddr(tc.input); got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

func TestBuildRdmaResRequests(t *testing.T) {
	dd := rdmaDriverDetails(nil)
	u32 := func(atype uint16, v uint32) []byte { return rdmaIndexAttrs(uint32(atype), v) }
	tests := []struct {
		description string
		build       func() ([]byte, error)
		expected    []byte
		expectedErr error
	}{
		{description: "positive: qp dump, port 0, is 32 bytes of flags 0x305",
			build:    func() ([]byte, error) { return BuildRdmaResDumpRequest(RdmaNldevCmdResQPGet, 0, 0, 0) },
			expected: layoutRequest(0x140a, 0x305, 0, nil, rdBody(dd, u32(RdmaNldevAttrDevIndex, 0)))},
		{description: "positive: qp dump, port 1, adds PORT_INDEX",
			build: func() ([]byte, error) { return BuildRdmaResDumpRequest(RdmaNldevCmdResQPGet, 0, 1, 0) },
			expected: layoutRequest(0x140a, 0x305, 0, nil,
				rdBody(dd, u32(RdmaNldevAttrDevIndex, 0), u32(RdmaNldevAttrPortIndex, 1)))},
		{description: "positive: cq doit by cqn, flags 0x5, DRIVER_DETAILS last",
			build: func() ([]byte, error) {
				return BuildRdmaResDoitRequest(RdmaNldevCmdResCQGet, 0, 0, RdmaNldevAttrResCQN, 3, 0)
			},
			expected: layoutRequest(0x140c, 0x5, 0, nil, rdBody(u32(RdmaNldevAttrDevIndex, 0), u32(RdmaNldevAttrResCQN, 3), dd))},
		{description: "positive: qp doit by lqpn carries the port",
			build: func() ([]byte, error) {
				return BuildRdmaResDoitRequest(RdmaNldevCmdResQPGet, 0, 1, RdmaNldevAttrResLQPN, 1, 0)
			},
			expected: layoutRequest(0x140a, 0x5, 0, nil, rdBody(u32(RdmaNldevAttrDevIndex, 0),
				u32(RdmaNldevAttrPortIndex, 1), u32(RdmaNldevAttrResLQPN, 1), dd))},
		{description: "negative: the summary command",
			build:       func() ([]byte, error) { return BuildRdmaResDumpRequest(RdmaNldevCmdResGet, 0, 0, 0) },
			expectedErr: ErrRdmaCmd},
		{description: "negative: a RAW dump",
			build:       func() ([]byte, error) { return BuildRdmaResDumpRequest(RdmaNldevCmdResQPGetRaw, 0, 0, 0) },
			expectedErr: ErrRdmaCmd},
		{description: "negative: a write command",
			build: func() ([]byte, error) {
				return BuildRdmaResDoitRequest(RdmaNldevCmdDellink, 0, 0, RdmaNldevAttrResLQPN, 1, 0)
			},
			expectedErr: ErrRdmaCmd},
		{description: "boundary: dev 0xffffffff is encoded as is",
			build:    func() ([]byte, error) { return BuildRdmaResDumpRequest(RdmaNldevCmdResMRGet, 0xffffffff, 0, 7) },
			expected: layoutRequest(0x140d, 0x305, 7, nil, rdBody(dd, u32(RdmaNldevAttrDevIndex, 0xffffffff)))},
		{description: "boundary: a doit with port 0 has no PORT_INDEX",
			build: func() ([]byte, error) {
				return BuildRdmaResDoitRequest(RdmaNldevCmdResSRQGet, 2, 0, RdmaNldevAttrResSRQN, 0, 0)
			},
			expected: layoutRequest(0x1417, 0x5, 0, nil, rdBody(u32(RdmaNldevAttrDevIndex, 2), u32(RdmaNldevAttrResSRQN, 0), dd))},
		{description: "corner: every object GET builds; CM_ID's type is 0x140b",
			build:    func() ([]byte, error) { return BuildRdmaResDumpRequest(RdmaNldevCmdResCMIDGet, 0, 0, 0) },
			expected: layoutRequest(0x140b, 0x305, 0, nil, rdBody(dd, u32(RdmaNldevAttrDevIndex, 0)))},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.build()
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("got\n%x\nwant\n%x", got, tc.expected)
			}
		})
	}
}
