package godevlink

import (
	"encoding/binary"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// Extra DEVLINK_ATTR_* ids and the param value type codes the renderers read
// straight from the wire (include/uapi/linux/devlink.h), beyond the ones
// ParseDevlink exposes.
const (
	attrPortSplittable   uint16 = 148
	attrParam            uint16 = 80
	attrParamName        uint16 = 81
	attrParamGeneric     uint16 = 82
	attrParamType        uint16 = 83
	attrParamValuesList  uint16 = 84
	attrParamValue       uint16 = 85
	attrParamValueData   uint16 = 86
	attrParamValueCmode  uint16 = 87
	attrParamValueDflt   uint16 = 182
	attrRegionSnapshots  uint16 = 90
	attrRegionSnapshot   uint16 = 91
	attrRegionSnapshotID uint16 = 92

	// DEVLINK_VAR_ATTR_TYPE_* (the param value's type).
	varTypeU8     uint8 = 1
	varTypeU16    uint8 = 2
	varTypeU32    uint8 = 3
	varTypeU64    uint8 = 4
	varTypeString uint8 = 5
	varTypeFlag   uint8 = 6
)

// unknownType is devlink.c's "<unknown type>" fallback, shared by the name
// tables (port type, param cmode, trap type) that spell it the same.
const unknownType = "<unknown type>"

// --- devlink.c name tables ---

func portTypeName(t uint16) string {
	switch t {
	case 0:
		return "notset"
	case 1:
		return "auto"
	case 2:
		return "eth"
	case 3:
		return "ib"
	default:
		return unknownType
	}
}

// portFlavourName is devlink.c's port_flavour_map.
func portFlavourName(f uint16) string {
	switch f {
	case 0:
		return "physical"
	case 1:
		return "cpu"
	case 2:
		return "dsa"
	case 3:
		return "pcipf"
	case 4:
		return "pcivf"
	case 5:
		return "pcisf"
	case 6:
		return "virtual"
	case 7:
		return "unused"
	default:
		return "<unknown flavour>"
	}
}

func paramCmodeName(c uint8) string {
	switch c {
	case 0:
		return "runtime"
	case 1:
		return "driverinit"
	case 2:
		return "permanent"
	default:
		return unknownType
	}
}

func healthStateName(s uint8) string {
	switch s {
	case 0:
		return "healthy"
	case 1:
		return "error"
	default:
		return "<unknown state>"
	}
}

func trapTypeName(t uint8) string {
	switch t {
	case 0:
		return "drop"
	case 1:
		return "exception"
	case 2:
		return "control"
	default:
		return unknownType
	}
}

func trapActionName(a uint8) string {
	switch a {
	case 0:
		return "drop"
	case 1:
		return "trap"
	case 2:
		return "mirror"
	default:
		return "<unknown action>"
	}
}

func resourceUnitName(u uint8) string {
	if u == 0 {
		return "entry"
	}
	return "<unknown unit>"
}

// --- raw attribute helpers (over xtcpnl.ParseNetlinkAttributes output) ---

func rawAttr(attrs []xtcpnl.NetlinkAttribute, id uint16) (*xtcpnl.NetlinkAttribute, bool) {
	for i := range attrs {
		if attrs[i].ID() == id {
			return &attrs[i], true
		}
	}
	return nil, false
}

func rawPresent(attrs []xtcpnl.NetlinkAttribute, id uint16) bool {
	_, ok := rawAttr(attrs, id)
	return ok
}

func rawU8(attrs []xtcpnl.NetlinkAttribute, id uint16) (uint8, bool) {
	if a, ok := rawAttr(attrs, id); ok && len(a.Data) >= 1 {
		return a.Data[0], true
	}
	return 0, false
}

func rawU32(attrs []xtcpnl.NetlinkAttribute, id uint16) (uint32, bool) {
	if a, ok := rawAttr(attrs, id); ok && len(a.Data) >= 4 {
		return binary.LittleEndian.Uint32(a.Data), true
	}
	return 0, false
}

func rawStr(attrs []xtcpnl.NetlinkAttribute, id uint16) (string, bool) {
	if a, ok := rawAttr(attrs, id); ok {
		s := a.Data
		if n := len(s); n > 0 && s[n-1] == 0 {
			s = s[:n-1]
		}
		return string(s), true
	}
	return "", false
}

// rawNested parses a nested attribute's children; an empty nest is present
// with no children.
func rawNested(attrs []xtcpnl.NetlinkAttribute, id uint16) ([]xtcpnl.NetlinkAttribute, bool) {
	a, ok := rawAttr(attrs, id)
	if !ok {
		return nil, false
	}
	sub, err := xtcpnl.ParseNetlinkAttributes(a.Data)
	if err != nil {
		return nil, false
	}
	return sub, true
}
