package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
)

// GenlControllerID is the reserved generic-netlink controller family ID.
// These decoders, like the rtnetlink decoders, target little-endian Linux.
// Callers must dispatch NETLINK_GENERIC separately from NETLINK_ROUTE and
// resolve dynamic family IDs through the controller before decoding a body.
const GenlControllerID uint16 = 16

var ErrGenericNetlink = errors.New("xtcpnl: malformed generic netlink message")

// NetlinkAttribute retains the complete type (including NLA flags) and an
// owned copy of the payload. Unknown attributes remain available to callers.
type NetlinkAttribute struct {
	Type uint16
	Data []byte
}

func (a NetlinkAttribute) ID() uint16 { return a.Type & 0x3fff }

// ParseNetlinkAttributes validates framing, including alignment. The final
// attribute may omit its alignment padding. Trailing partial headers fail.
func ParseNetlinkAttributes(data []byte) ([]NetlinkAttribute, error) {
	var attrs []NetlinkAttribute
	for len(data) != 0 {
		if len(data) < 4 {
			return nil, fmt.Errorf("%w: short attribute header", ErrGenericNetlink)
		}
		n := int(binary.LittleEndian.Uint16(data))
		if n < 4 || n > len(data) {
			return nil, fmt.Errorf("%w: attribute length %d", ErrGenericNetlink, n)
		}
		attrs = append(attrs, NetlinkAttribute{binary.LittleEndian.Uint16(data[2:]), append([]byte{}, data[4:n]...)})
		if n == len(data) {
			break
		}
		aligned := (n + 3) &^ 3
		if aligned > len(data) {
			return nil, fmt.Errorf("%w: partial attribute padding", ErrGenericNetlink)
		}
		data = data[aligned:]
	}
	return attrs, nil
}

type GenericNetlinkMessage struct {
	Command, Version uint8
	Attributes       []NetlinkAttribute
}

func ParseGenericNetlink(body []byte) (GenericNetlinkMessage, error) {
	if len(body) < 4 {
		return GenericNetlinkMessage{}, fmt.Errorf("%w: short genlmsghdr", ErrGenericNetlink)
	}
	a, err := ParseNetlinkAttributes(body[4:])
	return GenericNetlinkMessage{body[0], body[1], a}, err
}

// attribute selects singleton fields; repeated list entries and unknown
// attributes are not subject to this duplicate check.
func attribute(attrs []NetlinkAttribute, id uint16) (*NetlinkAttribute, error) {
	var found *NetlinkAttribute
	for i := range attrs {
		if attrs[i].ID() == id {
			if found != nil {
				return nil, fmt.Errorf("%w: duplicate attribute %d", ErrGenericNetlink, id)
			}
			found = &attrs[i]
		}
	}
	return found, nil
}

func scalarAttribute(attrs []NetlinkAttribute, id uint16, width int) (*NetlinkAttribute, error) {
	a, err := attribute(attrs, id)
	if err == nil && a != nil && (len(a.Data) != width || a.Type&0xc000 != 0) {
		err = fmt.Errorf("%w: attribute %d needs an unflagged %d-byte scalar", ErrGenericNetlink, id, width)
	}
	return a, err
}

func genlAttrU32(attrs []NetlinkAttribute, id uint16) (*uint32, error) {
	a, err := scalarAttribute(attrs, id, 4)
	if err != nil || a == nil {
		return nil, err
	}
	v := binary.LittleEndian.Uint32(a.Data)
	return &v, nil
}

func genlAttrU16(attrs []NetlinkAttribute, id uint16) (*uint16, error) {
	a, err := scalarAttribute(attrs, id, 2)
	if err != nil || a == nil {
		return nil, err
	}
	v := binary.LittleEndian.Uint16(a.Data)
	return &v, nil
}

func genlAttrU64(attrs []NetlinkAttribute, id uint16) (*uint64, error) {
	a, err := scalarAttribute(attrs, id, 8)
	if err != nil || a == nil {
		return nil, err
	}
	v := binary.LittleEndian.Uint64(a.Data)
	return &v, nil
}

func genlAttrU8(attrs []NetlinkAttribute, id uint16) (*uint8, error) {
	a, err := scalarAttribute(attrs, id, 1)
	if err != nil || a == nil {
		return nil, err
	}
	v := a.Data[0]
	return &v, nil
}

func attrString(attrs []NetlinkAttribute, id uint16) (string, error) {
	a, err := attribute(attrs, id)
	if err != nil || a == nil {
		return "", err
	}
	if len(a.Data) == 0 || a.Data[len(a.Data)-1] != 0 || a.Type&0xc000 != 0 {
		return "", fmt.Errorf("%w: attribute %d needs a NUL-terminated string", ErrGenericNetlink, id)
	}
	return string(a.Data[:len(a.Data)-1]), nil
}

func attrNested(attrs []NetlinkAttribute, id uint16) ([]NetlinkAttribute, bool, error) {
	a, err := attribute(attrs, id)
	if err != nil || a == nil {
		return nil, false, err
	}
	// Some older families omit NLA_F_NESTED; framing still determines validity.
	if a.Type&0x4000 != 0 {
		return nil, true, fmt.Errorf("%w: network-order nested attribute", ErrGenericNetlink)
	}
	v, err := ParseNetlinkAttributes(a.Data)
	return v, true, err
}

// genlDecoder reads a genl family's singleton attributes into optional
// fields. It retains the first malformed-field error, as
// ethtoolPayloadDecoder does, so the fields before it are still returned.
type genlDecoder struct {
	attrs []NetlinkAttribute
	err   error
}

func (d *genlDecoder) u8(id uint16, dst **uint8) {
	if d.err == nil {
		*dst, d.err = genlAttrU8(d.attrs, id)
	}
}

func (d *genlDecoder) u16(id uint16, dst **uint16) {
	if d.err == nil {
		*dst, d.err = genlAttrU16(d.attrs, id)
	}
}

func (d *genlDecoder) u32(id uint16, dst **uint32) {
	if d.err == nil {
		*dst, d.err = genlAttrU32(d.attrs, id)
	}
}

// be16 reads a u16 the kernel puts in network order, without the
// NLA_F_NET_BYTEORDER flag, as nla_put_be16 does.
func (d *genlDecoder) be16(id uint16, dst **uint16) {
	if d.err != nil {
		return
	}
	var a *NetlinkAttribute
	a, d.err = scalarAttribute(d.attrs, id, 2)
	if d.err == nil && a != nil {
		v := binary.BigEndian.Uint16(a.Data)
		*dst = &v
	}
}

// be64 reads a u64 whose bytes are in network order, as ILA's locators
// are, so its value prints as ip writes it.
func (d *genlDecoder) be64(id uint16, dst **uint64) {
	if d.err != nil {
		return
	}
	var a *NetlinkAttribute
	a, d.err = scalarAttribute(d.attrs, id, 8)
	if d.err == nil && a != nil {
		v := binary.BigEndian.Uint64(a.Data)
		*dst = &v
	}
}

// s32 reads a u32-width attribute as signed.
func (d *genlDecoder) s32(id uint16, dst **int32) {
	var v *uint32
	d.u32(id, &v)
	if v != nil {
		s := int32(*v)
		*dst = &s
	}
}

func (d *genlDecoder) u64(id uint16, dst **uint64) {
	if d.err == nil {
		*dst, d.err = genlAttrU64(d.attrs, id)
	}
}

// bytes reads a binary attribute: nil when absent, non-nil (perhaps empty)
// when present.
func (d *genlDecoder) bytes(id uint16) []byte {
	if d.err != nil {
		return nil
	}
	a, err := attribute(d.attrs, id)
	d.err = err
	if err != nil || a == nil {
		return nil
	}
	return a.Data
}

// str reads a NUL-terminated string attribute, nil when absent.
func (d *genlDecoder) str(id uint16) *string {
	if d.err != nil {
		return nil
	}
	a, err := attribute(d.attrs, id)
	if err != nil || a == nil {
		d.err = err
		return nil
	}
	var s string
	s, d.err = attrString(d.attrs, id)
	if d.err != nil {
		return nil
	}
	return &s
}

// flag reads an NLA_FLAG attribute: present or not. As in the kernel's
// policy, a flag carries no payload.
func (d *genlDecoder) flag(id uint16) bool {
	if d.err != nil {
		return false
	}
	a, err := attribute(d.attrs, id)
	if err == nil && a != nil && (len(a.Data) != 0 || a.Type&0xc000 != 0) {
		err = fmt.Errorf("%w: attribute %d needs an empty, unflagged flag", ErrGenericNetlink, id)
	}
	d.err = err
	return err == nil && a != nil
}

// addr reads an address pair: v4 as 4 bytes, else v6 as 16. Either id may
// be 0 for a family with only one of them.
func (d *genlDecoder) addr(v4, v6 uint16) netip.Addr {
	if d.err != nil {
		return netip.Addr{}
	}
	var a *NetlinkAttribute
	var err error
	if v4 != 0 {
		a, err = scalarAttribute(d.attrs, v4, 4)
		if err == nil && a != nil {
			return netip.AddrFrom4([4]byte(a.Data))
		}
	}
	if err == nil && v6 != 0 {
		a, err = scalarAttribute(d.attrs, v6, 16)
	}
	d.err = err
	if err == nil && a != nil {
		return netip.AddrFrom16([16]byte(a.Data))
	}
	return netip.Addr{}
}

type GenericNetlinkFamily struct {
	ID              uint16
	Name            string
	Version         *uint32
	MulticastGroups map[string]uint32
	Attributes      []NetlinkAttribute
}

// ParseGenericNetlinkFamily decodes CTRL_CMD_NEWFAMILY (including GETFAMILY
// replies). Family and multicast IDs are runtime values, never constants.
func ParseGenericNetlinkFamily(body []byte) (GenericNetlinkFamily, error) {
	var f GenericNetlinkFamily
	m, err := ParseGenericNetlink(body)
	if err != nil {
		return f, err
	}
	if m.Command != 1 {
		return f, fmt.Errorf("%w: expected CTRL_CMD_NEWFAMILY", ErrGenericNetlink)
	}
	f.Attributes = m.Attributes
	id, err := scalarAttribute(m.Attributes, 1, 2)
	if err != nil {
		return f, err
	}
	if id == nil {
		return f, fmt.Errorf("%w: missing family ID", ErrGenericNetlink)
	}
	f.ID = binary.LittleEndian.Uint16(id.Data)
	f.Name, err = attrString(m.Attributes, 2)
	if err != nil {
		return f, err
	}
	if f.Name == "" || f.ID < GenlControllerID {
		return f, fmt.Errorf("%w: invalid family identity", ErrGenericNetlink)
	}
	if f.Version, err = genlAttrU32(m.Attributes, 3); err != nil {
		return f, err
	}
	groups, _, err := attrNested(m.Attributes, 7)
	if err != nil {
		return f, err
	}
	f.MulticastGroups = make(map[string]uint32)
	for _, group := range groups {
		fields, err := ParseNetlinkAttributes(group.Data)
		if err != nil {
			return f, err
		}
		name, err := attrString(fields, 1)
		if err != nil {
			return f, err
		}
		id, err := genlAttrU32(fields, 2)
		if err != nil {
			return f, err
		}
		if _, exists := f.MulticastGroups[name]; exists || name == "" || id == nil {
			return f, fmt.Errorf("%w: invalid multicast group", ErrGenericNetlink)
		}
		f.MulticastGroups[name] = *id
	}
	return f, nil
}
