package rdmacaps

import (
	"context"
	"encoding/binary"
	"syscall"
)

// Layouts from include/uapi/rdma/ib_user_mad.h and rdma_user_ioctl.h.
// TestABILayout compares every constant in this file with
// nix/tests/linkmonitor-abi.c output.
const (
	umadHdrLen             = 64
	umadHdrOffID           = 0
	umadHdrOffStatus       = 4
	umadHdrOffTimeout      = 8
	umadHdrOffRetries      = 12
	umadHdrOffLength       = 16
	umadHdrOffQPN          = 20
	umadHdrOffQKey         = 24
	umadHdrOffLID          = 28
	umadHdrOffSL           = 30
	umadHdrOffPathBits     = 31
	umadHdrOffGRHPresent   = 32
	umadHdrOffGIDIndex     = 33
	umadHdrOffHopLimit     = 34
	umadHdrOffTrafficClass = 35
	umadHdrOffGID          = 36
	umadHdrOffFlowLabel    = 52
	umadHdrOffPKeyIndex    = 56

	umadRegReq2Len           = 40
	umadRegReq2OffID         = 0
	umadRegReq2OffQPN        = 4
	umadRegReq2OffMgmtClass  = 8
	umadRegReq2OffVersion    = 9
	umadRegReq2OffRes        = 10
	umadRegReq2OffFlags      = 12
	umadRegReq2OffMethodMask = 16
	umadRegReq2OffOUI        = 32
	umadRegReq2OffRMPP       = 36

	umadRegisterAgent2  = 0xC0281B04
	umadUnregisterAgent = 0x40041B02
	// umadEnablePKey selects the 64-byte header above; without it the kernel
	// reads and writes the 56-byte header of ABI 5's first form. It must
	// precede the first registration.
	umadEnablePKey = 0x1B03
	// umadABIVersion is IB_USER_MAD_ABI_VERSION, the lowest umad ABI the
	// exchange accepts, as libibumad's umad_open_port requires.
	umadABIVersion = 5
)

// umadPacket is what one exchange writes and reads: the header, then one MAD.
type umadPacket = [umadHdrLen + 256]byte

// umadHeader is ib_user_mad_hdr. The first five fields are in host order; the
// address fields are big-endian, as the kernel keeps them, except the P_Key
// index.
type umadHeader struct {
	Agent, Status, TimeoutMs, Retries, Length uint32
	QPN, QKey                                 uint32
	LID                                       uint16
	SL, PathBits, GRHPresent, GIDIndex        uint8
	HopLimit, TrafficClass                    uint8
	GID                                       [16]byte
	FlowLabel                                 uint32
	PKeyIndex                                 uint16
}

func (h *umadHeader) encode(b *[umadHdrLen]byte) {
	ne, be := binary.NativeEndian, binary.BigEndian
	ne.PutUint32(b[umadHdrOffID:], h.Agent)
	ne.PutUint32(b[umadHdrOffStatus:], h.Status)
	ne.PutUint32(b[umadHdrOffTimeout:], h.TimeoutMs)
	ne.PutUint32(b[umadHdrOffRetries:], h.Retries)
	ne.PutUint32(b[umadHdrOffLength:], h.Length)
	be.PutUint32(b[umadHdrOffQPN:], h.QPN)
	be.PutUint32(b[umadHdrOffQKey:], h.QKey)
	be.PutUint16(b[umadHdrOffLID:], h.LID)
	b[umadHdrOffSL], b[umadHdrOffPathBits] = h.SL, h.PathBits
	b[umadHdrOffGRHPresent], b[umadHdrOffGIDIndex] = h.GRHPresent, h.GIDIndex
	b[umadHdrOffHopLimit], b[umadHdrOffTrafficClass] = h.HopLimit, h.TrafficClass
	copy(b[umadHdrOffGID:umadHdrOffFlowLabel], h.GID[:])
	be.PutUint32(b[umadHdrOffFlowLabel:], h.FlowLabel)
	ne.PutUint16(b[umadHdrOffPKeyIndex:], h.PKeyIndex)
	clear(b[umadHdrOffPKeyIndex+2:])
}

func (h *umadHeader) decode(b *[umadHdrLen]byte) {
	ne, be := binary.NativeEndian, binary.BigEndian
	h.Agent = ne.Uint32(b[umadHdrOffID:])
	h.Status = ne.Uint32(b[umadHdrOffStatus:])
	h.TimeoutMs = ne.Uint32(b[umadHdrOffTimeout:])
	h.Retries = ne.Uint32(b[umadHdrOffRetries:])
	h.Length = ne.Uint32(b[umadHdrOffLength:])
	h.QPN = be.Uint32(b[umadHdrOffQPN:])
	h.QKey = be.Uint32(b[umadHdrOffQKey:])
	h.LID = be.Uint16(b[umadHdrOffLID:])
	h.SL, h.PathBits = b[umadHdrOffSL], b[umadHdrOffPathBits]
	h.GRHPresent, h.GIDIndex = b[umadHdrOffGRHPresent], b[umadHdrOffGIDIndex]
	h.HopLimit, h.TrafficClass = b[umadHdrOffHopLimit], b[umadHdrOffTrafficClass]
	copy(h.GID[:], b[umadHdrOffGID:umadHdrOffFlowLabel])
	h.FlowLabel = be.Uint32(b[umadHdrOffFlowLabel:])
	h.PKeyIndex = ne.Uint16(b[umadHdrOffPKeyIndex:])
}

// encodeUMADRequest writes the packet the cgo exchange sends: libibumad's
// umad_set_addr(0xffff, 0, 0, 0), which addresses the permissive LID on QP0,
// and umad_send's agent and timeout with no retries, then mad.
func encodeUMADRequest(p *umadPacket, agent, timeoutMs uint32, mad *[256]byte) {
	h := umadHeader{Agent: agent, TimeoutMs: timeoutMs, LID: 0xffff}
	h.encode((*[umadHdrLen]byte)(p[:umadHdrLen]))
	copy(p[umadHdrLen:], mad[:])
}

// decodeUMADReply checks a read of n bytes into p for agent and copies its
// MAD to reply. A send that timed out comes back as its own header with
// ETIMEDOUT and only the MAD's common header, so it is DeadlineExceeded
// before the length is checked; any other status is that errno. The upper
// TID half is the kernel's and is not checked here or by validateReply.
func decodeUMADReply(p *umadPacket, n int, agent uint32, reply *[256]byte) error {
	if n < umadHdrLen || n > len(p) {
		return syscall.EPROTO
	}
	var h umadHeader
	h.decode((*[umadHdrLen]byte)(p[:umadHdrLen]))
	switch {
	case h.Agent != agent:
		return syscall.EPROTO
	case h.Status == uint32(syscall.ETIMEDOUT):
		return context.DeadlineExceeded
	case h.Status != 0:
		return syscall.Errno(h.Status)
	case n != len(p):
		return syscall.EPROTO
	}
	*reply = [256]byte(p[umadHdrLen:])
	return nil
}

// encodeRegisterAgent2 writes the REGISTER_AGENT2 request umad_register2
// builds for class and version with no methods, flags, OUI or RMPP: the SMP
// classes register on QP0, every other class on QP1.
func encodeRegisterAgent2(b *[umadRegReq2Len]byte, class, version uint8) {
	*b = [umadRegReq2Len]byte{}
	qpn := uint32(1)
	if class == 0x01 || class == 0x81 {
		qpn = 0
	}
	binary.NativeEndian.PutUint32(b[umadRegReq2OffQPN:], qpn)
	b[umadRegReq2OffMgmtClass], b[umadRegReq2OffVersion] = class, version
}

// registeredAgent is the agent id the kernel writes back into the request.
func registeredAgent(b *[umadRegReq2Len]byte) uint32 {
	return binary.NativeEndian.Uint32(b[umadRegReq2OffID:])
}
