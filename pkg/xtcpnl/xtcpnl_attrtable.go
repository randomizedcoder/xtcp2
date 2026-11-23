package xtcpnl

import (
	"encoding/binary"
	"errors"
	"strings"
)

// AttrTable is iproute2's `struct rtattr *tb[MAX + 1]` after
// parse_rtattr_flags(tb, max, rta, len, NLA_F_NESTED) (lib/libnetlink.c):
// each attribute type up to max, its first occurrence winning, with
// NLA_F_NESTED and NLA_F_NET_BYTEORDER masked off the type. Types above max
// are dropped, as parse_rtattr drops them.
//
// It exists for the clones that mirror an iproute2 printer read for read
// (gobridge): a printer tests `tb[X]` and then calls rta_getattr_u8/u16/
// u32/u64 on it, and Has and the getters are those two steps. A getter's ok
// is false when the attribute is absent or shorter than the width read, which
// is where iproute2 reads past the attribute (bug class C3); the clone
// decides what to print then.
//
// The values are views into the parsed buffer; copy what outlives it.
type AttrTable struct {
	vals [][]byte
}

// ErrAttrTableMax is a max too large for a table.
var ErrAttrTableMax = errors.New("xtcpnl: attribute table max out of range")

// attrTableMaxCst bounds a table's size; every rtnetlink enum is far below
// it.
const attrTableMaxCst = 1024

// ParseAttrTable parses a TLV stream into a table of types 0..maxType,
// with NLA_F_NESTED and NLA_F_NET_BYTEORDER masked off each type. A
// malformed stream (an attribute longer than what remains, or shorter than
// its header) is an error.
func ParseAttrTable(data []byte, maxType uint16) (AttrTable, error) {
	return ParseAttrTableFlags(data, maxType, ^NlaTypeMaskCst)
}

// ParseAttrTableFlags is parse_rtattr_flags(tb, max, rta, len, flags)
// exactly: each type is rta_type & ~flags, so flags 0 is parse_rtattr,
// where a nest flagged NLA_F_NESTED has a type above max and is dropped.
func ParseAttrTableFlags(data []byte, maxType, flags uint16) (AttrTable, error) {
	if maxType > attrTableMaxCst {
		return AttrTable{}, ErrAttrTableMax
	}
	t := AttrTable{vals: make([][]byte, int(maxType)+1)}
	err := WalkRawAttrs(data, func(raw uint16, val []byte) {
		atype := raw &^ flags
		if int(atype) < len(t.vals) && t.vals[atype] == nil {
			// A zero-length attribute is present: keep a non-nil empty
			// slice so Has sees it.
			t.vals[atype] = val[:len(val):len(val)]
		}
	})
	if err != nil {
		return AttrTable{}, err
	}
	return t, nil
}

// WalkRawAttrs is the RTA_OK/RTA_NEXT loop: fn sees each attribute's raw
// type, flags included, and its payload. A malformed attribute is an
// error; a remainder shorter than a header ends the walk, as RTA_OK does.
func WalkRawAttrs(data []byte, fn func(raw uint16, val []byte)) error {
	for len(data) >= RTAttrSizeCst {
		alen := int(binary.LittleEndian.Uint16(data[0:2]))
		if alen < RTAttrSizeCst || alen > len(data) {
			return ErrRTAttrSmall
		}
		fn(binary.LittleEndian.Uint16(data[2:4]), data[RTAttrSizeCst:alen])
		adv := alen + FourByteAlignPadding(alen)
		if adv >= len(data) {
			break
		}
		data = data[adv:]
	}
	return nil
}

// FindRawAttr is parse_rtattr_one(type, rta, len): the payload of the first
// attribute whose raw type, flags included, is raw.
func FindRawAttr(data []byte, raw uint16) ([]byte, bool) {
	var out []byte
	found := false
	err := WalkRawAttrs(data, func(t uint16, val []byte) {
		if !found && t == raw {
			out, found = val, true
		}
	})
	if err != nil && !found {
		return nil, false
	}
	return out, found
}

// Has is `tb[t] != NULL`.
func (a AttrTable) Has(t uint16) bool { return int(t) < len(a.vals) && a.vals[t] != nil }

// Bytes is RTA_DATA(tb[t]) and RTA_PAYLOAD(tb[t]); nil when absent.
func (a AttrTable) Bytes(t uint16) []byte {
	if !a.Has(t) {
		return nil
	}
	return a.vals[t]
}

// U8 is rta_getattr_u8(tb[t]).
func (a AttrTable) U8(t uint16) (uint8, bool) {
	b := a.Bytes(t)
	if len(b) < 1 {
		return 0, false
	}
	return b[0], true
}

// U16 is rta_getattr_u16(tb[t]), host (little-endian) order.
func (a AttrTable) U16(t uint16) (uint16, bool) {
	b := a.Bytes(t)
	if len(b) < 2 {
		return 0, false
	}
	return binary.LittleEndian.Uint16(b), true
}

// U32 is rta_getattr_u32(tb[t]), host (little-endian) order.
func (a AttrTable) U32(t uint16) (uint32, bool) {
	b := a.Bytes(t)
	if len(b) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b), true
}

// U64 is rta_getattr_u64(tb[t]), host (little-endian) order; iproute2
// memcpy's it, so alignment does not matter.
func (a AttrTable) U64(t uint16) (uint64, bool) {
	b := a.Bytes(t)
	if len(b) < 8 {
		return 0, false
	}
	return binary.LittleEndian.Uint64(b), true
}

// Str is rta_getattr_str(tb[t]): the payload up to its first NUL, or all of
// it when there is none.
func (a AttrTable) Str(t uint16) (string, bool) {
	b := a.Bytes(t)
	if b == nil {
		return "", false
	}
	s, _, _ := strings.Cut(string(b), "\x00")
	return s, true
}

// Len is the number of slots, maxType + 1; 0 for the zero table.
func (a AttrTable) Len() int { return len(a.vals) }
