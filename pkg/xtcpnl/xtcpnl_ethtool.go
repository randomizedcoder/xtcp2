package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

var ErrUnsupportedEthtool = errors.New("xtcpnl: unsupported ethtool command or version")

type EthtoolKind string

const (
	EthtoolLinkInfoKind  EthtoolKind = "linkinfo"
	EthtoolLinkModesKind EthtoolKind = "linkmodes"
	EthtoolLinkStateKind EthtoolKind = "linkstate"
	EthtoolRingsKind     EthtoolKind = "rings"
	EthtoolChannelsKind  EthtoolKind = "channels"
	EthtoolPauseKind     EthtoolKind = "pause"
	EthtoolFECKind       EthtoolKind = "fec"
	EthtoolSpeedUnknown  uint32      = 0xffffffff
	EthtoolDuplexUnknown uint8       = 0xff
)

// EthtoolHeader holds optional device identity and request flags.
// Optional scalars are pointers: nil means absent, not zero or unknown.
// Speeds are raw Mbps, with no uint16 narrowing or legacy sentinel conversion.
// For bits/s consumers must widen before multiplying: uint64(speed) * 1000000.
type EthtoolHeader struct {
	DeviceIndex, Flags, PHYIndex *uint32
	DeviceName                   string
	Attributes                   []NetlinkAttribute
}

type EthtoolLinkInfo struct {
	Port, PHYAddress, MDIX, MDIXControl, Transceiver *uint8
}

type EthtoolLinkModes struct {
	Autoneg, Duplex, MasterSlaveConfig, MasterSlaveState, RateMatching *uint8
	Speed, Lanes                                                       *uint32
	Ours, Peer                                                         *EthtoolBitset
}

type EthtoolLinkState struct {
	Link, ExtendedState, ExtendedSubstate *uint8
	SQI, SQIMax, ExtendedDownCount        *uint32
}

// EthtoolRings preserves presence for ring sizes and options through Linux 7.0.
// Unknown future attributes remain available in EthtoolMessage.Attributes.
type EthtoolRings struct {
	RXMax, RXMiniMax, RXJumboMax, TXMax, RX, RXMini, RXJumbo, TX              *uint32
	RXBufLen, CQESize, TXPushBufLen, TXPushBufLenMax, HDSThresh, HDSThreshMax *uint32
	TCPDataSplit, TXPush, RXPush                                              *uint8
}

// EthtoolChannels contains maximum and current hardware channel counts.
type EthtoolChannels struct {
	RXMax, TXMax, OtherMax, CombinedMax, RX, TX, Other, Combined *uint32
}
type EthtoolPause struct{ Autoneg, RX, TX *uint8 }
type EthtoolFEC struct {
	Modes  *EthtoolBitset
	Auto   *uint8
	Active *uint32 // link-mode bit index, not a bitmap
}

type EthtoolMessage struct {
	GenericNetlinkMessage
	Kind                  EthtoolKind
	Request, Notification bool
	Header                EthtoolHeader
	LinkInfo              *EthtoolLinkInfo
	LinkModes             *EthtoolLinkModes
	LinkState             *EthtoolLinkState
	Rings                 *EthtoolRings
	Channels              *EthtoolChannels
	Pause                 *EthtoolPause
	FEC                   *EthtoolFEC
}

// ParseEthtool decodes a generic-netlink body AFTER the caller has resolved
// its family as "ethtool". Flags are the outer nlmsg_flags: user and kernel
// commands have different enums (e.g. user RINGS_SET=16, kernel GET_REPLY=16).
// This is stateless decoding, not request matching or sender authentication.
// Unknown commands return ErrUnsupportedEthtool and retain the generic body.
func ParseEthtool(body []byte, flags uint16) (EthtoolMessage, error) {
	var m EthtoolMessage
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	m.Request = flags&unix.NLM_F_REQUEST != 0
	if g.Version != 1 {
		return m, ErrUnsupportedEthtool
	}
	m.Kind, m.Notification = classifyEthtoolCommand(g.Command, m.Request)
	if m.Kind == "" {
		return m, ErrUnsupportedEthtool
	}
	if err := decodeEthtoolHeader(&m.Header, g.Attributes); err != nil {
		return m, err
	}
	err = decodeEthtoolPayload(&m)
	return m, err
}

func classifyEthtoolCommand(command uint8, request bool) (EthtoolKind, bool) {
	// Linux include/uapi/linux/ethtool_netlink.h: USER vs KERNEL enums.
	commands := []struct {
		kind                    EthtoolKind
		get, set, reply, notify uint8
	}{
		{EthtoolLinkInfoKind, 2, 3, 2, 3},
		{EthtoolLinkModesKind, 4, 5, 4, 5},
		{EthtoolLinkStateKind, 6, 0, 6, 0},
		{EthtoolRingsKind, 15, 16, 16, 17},
		{EthtoolChannelsKind, 17, 18, 18, 19},
		{EthtoolPauseKind, 21, 22, 22, 23},
		{EthtoolFECKind, 29, 30, 30, 31},
	}
	for _, c := range commands {
		if command == 0 {
			break
		}
		if (request && (command == c.get || command == c.set)) ||
			(!request && (command == c.reply || command == c.notify)) {
			return c.kind, !request && command == c.notify
		}
	}
	return "", false
}

func decodeEthtoolHeader(header *EthtoolHeader, attrs []NetlinkAttribute) error {
	h, _, err := attrNested(attrs, 1)
	if err != nil {
		return err
	}
	header.Attributes = h
	if header.DeviceIndex, err = genlAttrU32(h, 1); err != nil {
		return err
	}
	if header.DeviceName, err = attrString(h, 2); err != nil {
		return err
	}
	if header.Flags, err = genlAttrU32(h, 3); err != nil {
		return err
	}
	if header.PHYIndex, err = genlAttrU32(h, 4); err != nil {
		return err
	}
	// Capture requests can be intentionally invalid and dump headers can omit
	// a device. Do not invent identity or enforce request policy in a decoder.
	return nil
}

// ethtoolPayloadDecoder retains the first malformed-field error so fields are
// decoded in wire-schema order and partial results match ParseEthtool's contract.
type ethtoolPayloadDecoder struct {
	attrs []NetlinkAttribute
	err   error
}

func (d *ethtoolPayloadDecoder) u8(id uint16, dst **uint8) {
	if d.err == nil {
		*dst, d.err = genlAttrU8(d.attrs, id)
	}
}

func (d *ethtoolPayloadDecoder) u32(id uint16, dst **uint32) {
	if d.err == nil {
		*dst, d.err = genlAttrU32(d.attrs, id)
	}
}

func (d *ethtoolPayloadDecoder) bits(id uint16, dst **EthtoolBitset) {
	if d.err != nil {
		return
	}
	attrs, present, err := attrNested(d.attrs, id)
	d.err = err
	if err == nil && present {
		*dst, d.err = parseEthtoolBitset(attrs)
	}
}

func decodeEthtoolPayload(m *EthtoolMessage) error {
	d := ethtoolPayloadDecoder{attrs: m.Attributes}
	switch m.Kind {
	case EthtoolLinkInfoKind:
		m.LinkInfo = d.decodeLinkInfo()
	case EthtoolLinkModesKind:
		m.LinkModes = d.decodeLinkModes()
	case EthtoolLinkStateKind:
		m.LinkState = d.decodeLinkState()
	case EthtoolRingsKind:
		m.Rings = d.decodeRings()
	case EthtoolChannelsKind:
		m.Channels = d.decodeChannels()
	case EthtoolPauseKind:
		m.Pause = d.decodePause()
	case EthtoolFECKind:
		m.FEC = d.decodeFEC()
	}
	return d.err
}

func (d *ethtoolPayloadDecoder) decodeLinkInfo() *EthtoolLinkInfo {
	v := &EthtoolLinkInfo{}
	d.u8(2, &v.Port)
	d.u8(3, &v.PHYAddress)
	d.u8(4, &v.MDIX)
	d.u8(5, &v.MDIXControl)
	d.u8(6, &v.Transceiver)
	return v
}

func (d *ethtoolPayloadDecoder) decodeLinkModes() *EthtoolLinkModes {
	v := &EthtoolLinkModes{}
	d.u8(2, &v.Autoneg)
	d.bits(3, &v.Ours)
	d.bits(4, &v.Peer)
	d.u32(5, &v.Speed)
	d.u8(6, &v.Duplex)
	d.u8(7, &v.MasterSlaveConfig)
	d.u8(8, &v.MasterSlaveState)
	d.u32(9, &v.Lanes)
	d.u8(10, &v.RateMatching)
	return v
}

func (d *ethtoolPayloadDecoder) decodeLinkState() *EthtoolLinkState {
	v := &EthtoolLinkState{}
	d.u8(2, &v.Link)
	d.u32(3, &v.SQI)
	d.u32(4, &v.SQIMax)
	d.u8(5, &v.ExtendedState)
	d.u8(6, &v.ExtendedSubstate)
	d.u32(7, &v.ExtendedDownCount)
	return v
}

func (d *ethtoolPayloadDecoder) decodeRings() *EthtoolRings {
	v := &EthtoolRings{}
	d.u32(2, &v.RXMax)
	d.u32(3, &v.RXMiniMax)
	d.u32(4, &v.RXJumboMax)
	d.u32(5, &v.TXMax)
	d.u32(6, &v.RX)
	d.u32(7, &v.RXMini)
	d.u32(8, &v.RXJumbo)
	d.u32(9, &v.TX)
	d.u32(10, &v.RXBufLen)
	d.u8(11, &v.TCPDataSplit)
	d.u32(12, &v.CQESize)
	d.u8(13, &v.TXPush)
	d.u8(14, &v.RXPush)
	d.u32(15, &v.TXPushBufLen)
	d.u32(16, &v.TXPushBufLenMax)
	d.u32(17, &v.HDSThresh)
	d.u32(18, &v.HDSThreshMax)
	return v
}

func (d *ethtoolPayloadDecoder) decodePause() *EthtoolPause {
	v := &EthtoolPause{}
	d.u8(2, &v.Autoneg)
	d.u8(3, &v.RX)
	d.u8(4, &v.TX)
	return v
}

func (d *ethtoolPayloadDecoder) decodeFEC() *EthtoolFEC {
	v := &EthtoolFEC{}
	d.bits(2, &v.Modes)
	d.u8(3, &v.Auto)
	d.u32(4, &v.Active)
	return v
}

type EthtoolBit struct {
	Index      *uint32
	Name       string
	Value      bool
	Attributes []NetlinkAttribute
}

// EthtoolBitset preserves compact value/mask words or verbose bit names
// and optional indices. No allocation is based on the untrusted Size alone.
// In a NOMASK list every listed verbose bit is true, even without BIT_VALUE.
type EthtoolBitset struct {
	Size            *uint32
	NoMask, Compact bool
	Value, Mask     []uint32
	Bits            []EthtoolBit
	Attributes      []NetlinkAttribute
}

func parseEthtoolBitset(attrs []NetlinkAttribute) (*EthtoolBitset, error) {
	b := &EthtoolBitset{Attributes: attrs}
	flag, err := scalarAttribute(attrs, 1, 0)
	if err != nil {
		return nil, err
	}
	b.NoMask = flag != nil
	if b.Size, err = genlAttrU32(attrs, 2); err != nil {
		return nil, err
	}
	value, err := attribute(attrs, 4)
	if err != nil {
		return nil, err
	}
	mask, err := attribute(attrs, 5)
	if err != nil {
		return nil, err
	}
	entries, verbose, err := attrNested(attrs, 3)
	if err != nil {
		return nil, err
	}
	if value != nil {
		if err := decodeEthtoolCompactBitset(b, value, mask, verbose); err != nil {
			return nil, err
		}
	} else {
		if !verbose || mask != nil {
			return nil, fmt.Errorf("%w: bitset missing bits or value", ErrGenericNetlink)
		}
		if err := decodeEthtoolVerboseBitset(b, entries); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func decodeEthtoolCompactBitset(b *EthtoolBitset, value, mask *NetlinkAttribute, verbose bool) error {
	b.Compact = true
	if verbose || b.Size == nil || (b.NoMask && mask != nil) || (!b.NoMask && mask == nil) {
		return fmt.Errorf("%w: bitset inconsistent compact fields", ErrGenericNetlink)
	}
	nbytes := ((uint64(*b.Size) + 31) / 32) * 4
	var err error
	if b.Value, err = decodeEthtoolWords(value, nbytes); err != nil {
		return err
	}
	if mask != nil {
		b.Mask, err = decodeEthtoolWords(mask, nbytes)
	}
	return err
}

func decodeEthtoolWords(a *NetlinkAttribute, nbytes uint64) ([]uint32, error) {
	if uint64(len(a.Data)) != nbytes || a.Type&0xc000 != 0 {
		return nil, fmt.Errorf("%w: bitset word length/flags", ErrGenericNetlink)
	}
	out := make([]uint32, len(a.Data)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(a.Data[4*i:])
	}
	return out, nil
}

func decodeEthtoolVerboseBitset(b *EthtoolBitset, entries []NetlinkAttribute) error {
	seen := make(map[uint32]bool)
	for _, entry := range entries {
		if entry.ID() != 1 {
			continue
		} // preserve future list entries in Attributes
		fields, err := ParseNetlinkAttributes(entry.Data)
		if err != nil {
			return err
		}
		bit := EthtoolBit{Attributes: fields}
		if bit.Index, err = genlAttrU32(fields, 1); err != nil {
			return err
		}
		if bit.Name, err = attrString(fields, 2); err != nil {
			return err
		}
		if bit.Index == nil && bit.Name == "" {
			return fmt.Errorf("%w: bitset bit lacks identity", ErrGenericNetlink)
		}
		if bit.Index != nil {
			if (b.Size != nil && *bit.Index >= *b.Size) || seen[*bit.Index] {
				return fmt.Errorf("%w: bitset duplicate or out-of-range bit index", ErrGenericNetlink)
			}
			seen[*bit.Index] = true
		}
		flag, err := scalarAttribute(fields, 3, 0)
		if err != nil {
			return err
		}
		bit.Value = b.NoMask || flag != nil
		b.Bits = append(b.Bits, bit)
	}
	return nil
}

func (d *ethtoolPayloadDecoder) decodeChannels() *EthtoolChannels {
	v := &EthtoolChannels{}
	d.u32(2, &v.RXMax)
	d.u32(3, &v.TXMax)
	d.u32(4, &v.OtherMax)
	d.u32(5, &v.CombinedMax)
	d.u32(6, &v.RX)
	d.u32(7, &v.TX)
	d.u32(8, &v.Other)
	d.u32(9, &v.Combined)
	return v
}
