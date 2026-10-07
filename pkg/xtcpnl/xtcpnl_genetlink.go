package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
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
