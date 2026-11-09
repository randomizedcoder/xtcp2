package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The SOCK_DIAG_BY_FAMILY request `ss` sends for the inet families, and the
// filter bytecode it carries. A request is
//
//	struct inet_diag_req_v2                        (56 bytes)
//	[INET_DIAG_REQ_BYTECODE]  inet_diag_bc_op ...
//	[INET_DIAG_REQ_PROTOCOL]  u32                  (protocols above 255)
//
// The bytecode is a list of 4-byte ops, each a conditional forward jump:
//
//	struct inet_diag_bc_op { u8 code; u8 yes; u16 no; }
//
// followed by the op's operand: a second op whose `no` is the port (the
// port comparisons), struct inet_diag_hostcond plus the address (S_COND,
// D_COND), a u32 ifindex (DEV_COND), struct inet_diag_markcond (MARK_COND)
// or a u64 cgroup id (CGROUP_COND). A match moves on by `yes`, a miss by
// `no`; the socket is reported when the walk lands exactly on the end.
//
// Every operand field is host order (little-endian on the capture hosts),
// except the addresses, which are network order. Reference:
// include/uapi/linux/inet_diag.h and net/ipv4/inet_diag.c (Linux 6b4b543fa187).

const (
	InetDiagReqBytecode      uint16 = 1 // INET_DIAG_REQ_BYTECODE
	InetDiagReqSkBpfStorages uint16 = 2 // INET_DIAG_REQ_SK_BPF_STORAGES
	InetDiagReqProtocol      uint16 = 3 // INET_DIAG_REQ_PROTOCOL

	InetDiagBcNop        uint8 = 0  // INET_DIAG_BC_NOP
	InetDiagBcJmp        uint8 = 1  // INET_DIAG_BC_JMP
	InetDiagBcSGe        uint8 = 2  // INET_DIAG_BC_S_GE
	InetDiagBcSLe        uint8 = 3  // INET_DIAG_BC_S_LE
	InetDiagBcDGe        uint8 = 4  // INET_DIAG_BC_D_GE
	InetDiagBcDLe        uint8 = 5  // INET_DIAG_BC_D_LE
	InetDiagBcAuto       uint8 = 6  // INET_DIAG_BC_AUTO
	InetDiagBcSCond      uint8 = 7  // INET_DIAG_BC_S_COND
	InetDiagBcDCond      uint8 = 8  // INET_DIAG_BC_D_COND
	InetDiagBcDevCond    uint8 = 9  // INET_DIAG_BC_DEV_COND
	InetDiagBcMarkCond   uint8 = 10 // INET_DIAG_BC_MARK_COND
	InetDiagBcSEq        uint8 = 11 // INET_DIAG_BC_S_EQ
	InetDiagBcDEq        uint8 = 12 // INET_DIAG_BC_D_EQ
	InetDiagBcCgroupCond uint8 = 13 // INET_DIAG_BC_CGROUP_COND

	// InetDiagBcOpSizeCst is struct inet_diag_bc_op.
	InetDiagBcOpSizeCst = 4
	// InetDiagHostcondSizeCst is struct inet_diag_hostcond without its
	// address: u8 family, u8 prefix_len, 2 bytes of padding, int port.
	InetDiagHostcondSizeCst = 8
	// InetDiagMarkcondSizeCst is struct inet_diag_markcond: u32 mark, mask.
	InetDiagMarkcondSizeCst = 8

	// sockBindportLock is SOCK_BINDPORT_LOCK (include/net/sock.h:1532),
	// the userlocks bit INET_DIAG_BC_AUTO tests.
	sockBindportLock = 8

	afUnspec = 0
	afInet   = 2
	afInet6  = 10
)

var (
	ErrInetDiagRequestSmall = errors.New("data too small for an inet_diag request")
	ErrInetDiagProtocolLen  = errors.New("INET_DIAG_REQ_PROTOCOL is not 4 bytes")
	// ErrInetDiagBcInvalid and ErrInetDiagBcPerm are the kernel audit's
	// EINVAL and EPERM.
	ErrInetDiagBcInvalid = errors.New("inet_diag bytecode rejected (EINVAL)")
	ErrInetDiagBcPerm    = errors.New("inet_diag bytecode needs CAP_NET_ADMIN (EPERM)")
)

// InetDiagRequest is a decoded inet SOCK_DIAG_BY_FAMILY request.
type InetDiagRequest struct {
	Family   uint8
	Protocol uint8
	Ext      uint8
	// States is idiag_states in host order, as the kernel reads it. (The
	// collector's own requests, pkg/xtcp/init.go, store a byte-swapped
	// constant that SerializeNetlinkDiagRequest writes big-endian, which is
	// why DeserializeInetDiagReqV2 reads this field big-endian.)
	States          uint32
	ID              InetDiagSockID
	Bytecode        []byte
	HasBytecode     bool
	ProtocolAttr    uint32
	HasProtocolAttr bool
	HasBpfStorages  bool
}

// ParseInetDiagRequest decodes an inet SOCK_DIAG_BY_FAMILY request body
// (the bytes after the nlmsghdr). The kernel rejects a PROTOCOL attribute
// that isn't exactly 4 bytes (inet_diag.c:190), and so does this.
func ParseInetDiagRequest(body []byte) (InetDiagRequest, error) {
	var r InetDiagRequest
	if len(body) < InetDiagReqV2SizeCst {
		return r, ErrInetDiagRequestSmall
	}
	r.Family, r.Protocol, r.Ext = body[0], body[1], body[2]
	r.States = binary.LittleEndian.Uint32(body[4:8])
	if _, err := DeserializeInetDiagSockID(body[8:InetDiagReqV2SizeCst], &r.ID); err != nil {
		return r, err
	}
	var protoErr error
	err := WalkRTAttrs(body[InetDiagReqV2SizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case InetDiagReqBytecode:
			r.Bytecode, r.HasBytecode = CopyBytes(val), true
		case InetDiagReqSkBpfStorages:
			r.HasBpfStorages = true
		case InetDiagReqProtocol:
			if len(val) != 4 {
				protoErr = ErrInetDiagProtocolLen
				return
			}
			r.ProtocolAttr, r.HasProtocolAttr = binary.LittleEndian.Uint32(val), true
		}
	})
	if err != nil {
		return r, err
	}
	return r, protoErr
}

// EffectiveProtocol is the protocol the kernel dumps: the PROTOCOL
// attribute when present, else sdiag_protocol (inet_diag.c:202).
func (r *InetDiagRequest) EffectiveProtocol() uint32 {
	if r.HasProtocolAttr {
		return r.ProtocolAttr
	}
	return uint32(r.Protocol)
}

func bcErr(off int, what string) error {
	return fmt.Errorf("%w: op at %d: %s", ErrInetDiagBcInvalid, off, what)
}

// InetDiagBcAudit is the kernel's inet_diag_bc_audit (inet_diag.c:728): it
// returns nil for bytecode the kernel accepts, ErrInetDiagBcPerm for a
// MARK_COND without netAdmin, and ErrInetDiagBcInvalid (wrapped, with the
// op offset) for the rest. Empty bytecode is EINVAL, as an empty
// INET_DIAG_REQ_BYTECODE attribute is.
func InetDiagBcAudit(bc []byte, netAdmin bool) error {
	n := len(bc)
	if n < InetDiagBcOpSizeCst {
		return bcErr(0, "shorter than one op")
	}
	off := 0
	for off < n {
		rem := n - off
		if rem < InetDiagBcOpSizeCst {
			// The kernel reads the op past the attribute; any yes it
			// can then accept (4..rem+4) leaves len != 0.
			return bcErr(off, "truncated op")
		}
		code, yes := bc[off], int(bc[off+1])
		no := int(binary.LittleEndian.Uint16(bc[off+2:]))
		minLen, err := inetDiagBcMinLen(bc[off:], code, netAdmin)
		if err != nil {
			return fmt.Errorf("%w: op at %d", err, off)
		}
		if minLen > rem {
			return bcErr(off, "operand runs past the end")
		}
		if code != InetDiagBcNop {
			if no < minLen || no > rem+4 || no&3 != 0 {
				return bcErr(off, fmt.Sprintf("bad no %d", no))
			}
			if no < rem && !inetDiagBcValidCC(bc, rem-no) {
				return bcErr(off, fmt.Sprintf("no %d lands inside an op", no))
			}
		}
		if yes < minLen || yes > rem+4 || yes&3 != 0 {
			return bcErr(off, fmt.Sprintf("bad yes %d", yes))
		}
		off += yes
	}
	if off != n {
		// The last yes jumped past the end: the kernel's len < 0.
		return bcErr(n, "last op runs past the end")
	}
	return nil
}

// inetDiagBcMinLen is the switch in inet_diag_bc_audit plus the valid_*
// helpers: the op's length including its operand, before the space check.
func inetDiagBcMinLen(op []byte, code uint8, netAdmin bool) (int, error) {
	switch code {
	case InetDiagBcSCond, InetDiagBcDCond:
		minLen := InetDiagBcOpSizeCst + InetDiagHostcondSizeCst
		if len(op) < minLen {
			return minLen, nil
		}
		addrLen := 0
		switch op[4] {
		case afUnspec:
		case afInet:
			addrLen = 4
		case afInet6:
			addrLen = 16
		default:
			return 0, fmt.Errorf("%w: hostcond family %d", ErrInetDiagBcInvalid, op[4])
		}
		if int(op[5]) > 8*addrLen {
			return 0, fmt.Errorf("%w: prefix %d over %d bytes", ErrInetDiagBcInvalid, op[5], addrLen)
		}
		return minLen + addrLen, nil
	case InetDiagBcDevCond:
		return InetDiagBcOpSizeCst + 4, nil
	case InetDiagBcSEq, InetDiagBcSGe, InetDiagBcSLe, InetDiagBcDEq, InetDiagBcDGe, InetDiagBcDLe:
		return 2 * InetDiagBcOpSizeCst, nil
	case InetDiagBcMarkCond:
		if !netAdmin {
			return 0, ErrInetDiagBcPerm
		}
		return InetDiagBcOpSizeCst + InetDiagMarkcondSizeCst, nil
	case InetDiagBcCgroupCond:
		return InetDiagBcOpSizeCst + 8, nil
	case InetDiagBcAuto, InetDiagBcJmp, InetDiagBcNop:
		return InetDiagBcOpSizeCst, nil
	default:
		return 0, fmt.Errorf("%w: unknown code %d", ErrInetDiagBcInvalid, code)
	}
}

// inetDiagBcValidCC is the kernel's valid_cc: a jump target, given as the
// bytes left after it (cc), must be reached by walking yes from the start.
func inetDiagBcValidCC(bc []byte, cc int) bool {
	for off, rem := 0, len(bc); rem >= 0; {
		if cc > rem {
			return false
		}
		if cc == rem {
			return true
		}
		if off+1 >= len(bc) {
			return false
		}
		yes := int(bc[off+1])
		if yes < InetDiagBcOpSizeCst || yes&3 != 0 {
			return false
		}
		rem -= yes
		off += yes
	}
	return false
}

// InetDiagEntry is the kernel's struct inet_diag_entry: the socket fields
// the bytecode is run against. Saddr and Daddr are network order, 4 bytes
// for AF_INET and 16 for AF_INET6. Sport and Dport are host order.
type InetDiagEntry struct {
	Family    uint16
	Saddr     []byte
	Daddr     []byte
	Sport     uint16
	Dport     uint16
	Userlocks uint16
	Ifindex   uint32
	Mark      uint32
	CgroupID  uint64
}

// InetDiagBcRun is the kernel's inet_diag_bc_run (inet_diag.c:465): it
// reports whether e passes the filter. It expects audited bytecode; an
// operand that is cut short counts as a miss, and a zero step ends the walk
// as a miss rather than looping.
func InetDiagBcRun(bc []byte, e *InetDiagEntry) bool {
	n, off := len(bc), 0
	for off < n {
		if n-off < InetDiagBcOpSizeCst {
			return false
		}
		step := int(binary.LittleEndian.Uint16(bc[off+2:]))
		if inetDiagBcMatch(bc[off:], e) {
			step = int(bc[off+1])
		}
		if step == 0 {
			return false
		}
		off += step
	}
	return off == n
}

// inetDiagBcMatch evaluates the op at the start of op (the switch in
// inet_diag_bc_run). JMP never matches; NOP and unknown codes always do.
func inetDiagBcMatch(op []byte, e *InetDiagEntry) bool {
	code := op[0]
	switch code {
	case InetDiagBcJmp:
		return false
	case InetDiagBcSEq, InetDiagBcSGe, InetDiagBcSLe, InetDiagBcDEq, InetDiagBcDGe, InetDiagBcDLe:
		if len(op) < 2*InetDiagBcOpSizeCst {
			return false
		}
		return inetDiagBcPortCmp(code, binary.LittleEndian.Uint16(op[6:8]), e)
	case InetDiagBcAuto:
		return e.Userlocks&sockBindportLock == 0
	case InetDiagBcSCond, InetDiagBcDCond:
		return inetDiagBcHostcond(op, code == InetDiagBcSCond, e)
	case InetDiagBcDevCond:
		return len(op) >= 8 && binary.LittleEndian.Uint32(op[4:8]) == e.Ifindex
	case InetDiagBcMarkCond:
		if len(op) < InetDiagBcOpSizeCst+InetDiagMarkcondSizeCst {
			return false
		}
		mark, mask := binary.LittleEndian.Uint32(op[4:8]), binary.LittleEndian.Uint32(op[8:12])
		return e.Mark&mask == mark
	case InetDiagBcCgroupCond:
		return len(op) >= 12 && binary.LittleEndian.Uint64(op[4:12]) == e.CgroupID
	default:
		return true
	}
}

func inetDiagBcPortCmp(code uint8, port uint16, e *InetDiagEntry) bool {
	switch code {
	case InetDiagBcSEq:
		return e.Sport == port
	case InetDiagBcSGe:
		return e.Sport >= port
	case InetDiagBcSLe:
		return e.Sport <= port
	case InetDiagBcDEq:
		return e.Dport == port
	case InetDiagBcDGe:
		return e.Dport >= port
	default: // InetDiagBcDLe
		return e.Dport <= port
	}
}

// anyPortCst is the hostcond port -1, "any port", read as a u32.
const anyPortCst = 0xffffffff

// v4MappedPrefix is ::ffff:0:0/96.
var v4MappedPrefix = [12]byte{10: 0xff, 11: 0xff}

func inetDiagBcHostcond(op []byte, src bool, e *InetDiagEntry) bool {
	if len(op) < InetDiagBcOpSizeCst+InetDiagHostcondSizeCst {
		return false
	}
	family, prefix := op[4], int(op[5])
	// The kernel's int port, where -1 is "any"; compared unsigned here.
	port := binary.LittleEndian.Uint32(op[8:12])
	condAddr := op[12:]
	entryPort, addr := e.Dport, e.Daddr
	if src {
		entryPort, addr = e.Sport, e.Saddr
	}
	if port != anyPortCst && port != uint32(entryPort) {
		return false
	}
	if family != afUnspec && uint16(family) != e.Family {
		// An AF_INET condition matches a v4-mapped AF_INET6 socket.
		return e.Family == afInet6 && family == afInet && len(addr) == 16 &&
			[12]byte(addr[:12]) == v4MappedPrefix && bitstringMatch(addr[12:], condAddr, prefix)
	}
	return prefix == 0 || bitstringMatch(addr, condAddr, prefix)
}

// bitstringMatch is the kernel's bitstring_match: the first bits bits of a
// and b are equal. Either side too short to hold them is a miss.
func bitstringMatch(a, b []byte, bits int) bool {
	whole, rest := bits/8, bits%8
	need := whole
	if rest != 0 {
		need++
	}
	if len(a) < need || len(b) < need {
		return false
	}
	for i := range whole {
		if a[i] != b[i] {
			return false
		}
	}
	if rest == 0 {
		return true
	}
	mask := byte(0xff) << (8 - rest)
	return (a[whole]^b[whole])&mask == 0
}
