package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// ErrMonitorLink means a required or consumed monitor field is malformed.
var ErrMonitorLink = errors.New("xtcpnl: invalid monitor link record")

// MonitorLinkRequirements selects fields required for this operation. Inventory
// requires a name; deletion can use only a positive index. Traffic collection
// requires direct device statistics, never the IPv6 MIB rendering fallback.
type MonitorLinkRequirements struct {
	Name, Stats bool
}

// ParseMonitorLink validates monitor-consumed fields before using ParseNewLink.
// Unknown attributes remain tolerated. Known fields reject duplicates, invalid
// flags, partial scalars and malformed nests; no requirement invents carrier or
// operstate when an old kernel omits them. The tolerant public parser is unchanged.
func ParseMonitorLink(body []byte, required MonitorLinkRequirements) (LinkInfo, error) {
	var header IfInfomsg
	if _, err := DeserializeIfInfomsg(body, &header); err != nil {
		return LinkInfo{}, fmt.Errorf("%w: %w", ErrMonitorLink, err)
	}
	if header.Index <= 0 {
		return LinkInfo{}, ErrMonitorLink
	}
	var seen attrSeen
	err := walkMonitorAttrs(body[IfInfomsgSizeCst:], func(typ uint16, value []byte) error {
		id := typ & NlaTypeMaskCst
		if !monitorLinkAttribute(id) {
			return nil
		}
		if !seen.first(id) || typ&unix.NLA_F_NET_BYTEORDER != 0 {
			return ErrMonitorLink
		}
		if id != unix.IFLA_LINKINFO && typ&unix.NLA_F_NESTED != 0 {
			return ErrMonitorLink
		}
		return validateMonitorAttribute(id, value)
	})
	if err != nil {
		return LinkInfo{}, fmt.Errorf("%w: %w", ErrMonitorLink, err)
	}
	link, err := ParseNewLink(body)
	if err != nil {
		return LinkInfo{}, fmt.Errorf("%w: %w", ErrMonitorLink, err)
	}
	if (required.Name && link.Name == "") || (required.Stats && link.StatsFields == 0) {
		return LinkInfo{}, ErrMonitorLink
	}
	return link, nil
}

func monitorLinkAttribute(id uint16) bool {
	switch id {
	case unix.IFLA_IFNAME, unix.IFLA_OPERSTATE, unix.IFLA_CARRIER,
		unix.IFLA_MTU, unix.IFLA_LINK, unix.IFLA_MASTER, unix.IFLA_LINK_NETNSID,
		unix.IFLA_ADDRESS, unix.IFLA_PERM_ADDRESS, unix.IFLA_LINKINFO,
		unix.IFLA_STATS, unix.IFLA_STATS64, unix.IFLA_CARRIER_CHANGES,
		unix.IFLA_CARRIER_UP_COUNT, unix.IFLA_CARRIER_DOWN_COUNT:
		return true
	default:
		return false
	}
}

func validateMonitorAttribute(id uint16, value []byte) error {
	switch id {
	case unix.IFLA_IFNAME:
		if !monitorString(value) || validIfName(string(value[:len(value)-1])) != nil {
			return ErrMonitorLink
		}
	case unix.IFLA_OPERSTATE, unix.IFLA_CARRIER:
		if len(value) != 1 {
			return ErrMonitorLink
		}
		if id == unix.IFLA_CARRIER && value[0] > 1 {
			return ErrMonitorLink
		}
	case unix.IFLA_MTU, unix.IFLA_LINK, unix.IFLA_MASTER, unix.IFLA_LINK_NETNSID,
		unix.IFLA_CARRIER_CHANGES, unix.IFLA_CARRIER_UP_COUNT, unix.IFLA_CARRIER_DOWN_COUNT:
		if len(value) != 4 {
			return ErrMonitorLink
		}
	case unix.IFLA_STATS, unix.IFLA_STATS64:
		width := 4
		if id == unix.IFLA_STATS64 {
			width = 8
		}
		// Original UAPI has 23 counters. Later whole counters are compatible;
		// bytes of a partial counter must never become a valid observation.
		if len(value) < 23*width || len(value)%width != 0 {
			return ErrMonitorLink
		}
	case unix.IFLA_LINKINFO:
		return validateMonitorLinkInfo(value)
	case unix.IFLA_ADDRESS, unix.IFLA_PERM_ADDRESS:
		// Variable-width hardware addresses, including native InfiniBand.
		return nil
	}
	return nil
}

func validateMonitorLinkInfo(data []byte) error {
	var seen attrSeen
	return walkMonitorAttrs(data, func(typ uint16, value []byte) error {
		id := typ & NlaTypeMaskCst
		if id != unix.IFLA_INFO_KIND && id != unix.IFLA_INFO_SLAVE_KIND {
			return nil
		}
		if !seen.first(id) || typ != id || !monitorString(value) {
			return ErrMonitorLink
		}
		return nil
	})
}

func monitorString(value []byte) bool {
	return len(value) > 1 && value[len(value)-1] == 0 && !bytes.ContainsRune(value[:len(value)-1], 0)
}

// This validation walk borrows values and retains flags, with no payload copies.
// ParseNewLink remains responsible for ownership of the returned record.
func walkMonitorAttrs(data []byte, visit func(uint16, []byte) error) error {
	for len(data) != 0 {
		if len(data) < 4 {
			return ErrRTAttrSmall
		}
		n := int(binary.LittleEndian.Uint16(data[:2]))
		if n < 4 || n > len(data) {
			return ErrRTAttrSmall
		}
		if err := visit(binary.LittleEndian.Uint16(data[2:4]), data[4:n]); err != nil {
			return err
		}
		advance := n + FourByteAlignPadding(n)
		if n == len(data) {
			advance = n
		} else if advance > len(data) {
			return ErrRTAttrSmall
		}
		data = data[advance:]
	}
	return nil
}
