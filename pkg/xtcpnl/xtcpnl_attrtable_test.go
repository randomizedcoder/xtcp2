package xtcpnl

import (
	"errors"
	"fmt"
	"testing"
)

// rta is one attribute: header, value, padding to four bytes.
func rta(typ uint16, val ...byte) []byte {
	l := RTAttrSizeCst + len(val)
	b := []byte{byte(l), byte(l >> 8), byte(typ), byte(typ >> 8)}
	b = append(b, val...)
	return append(b, make([]byte, FourByteAlignPadding(l))...)
}

func joinAttrs(as ...[]byte) []byte {
	var out []byte
	for _, a := range as {
		out = append(out, a...)
	}
	return out
}

// TestAttrTable pins AttrTable against parse_rtattr_flags and the
// rta_getattr_* reads it stands for.
func TestAttrTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		data        []byte
		max         uint16
		query       func(AttrTable) string
		expected    string // the query's result, or "error"
	}{
		{"positive: u8, u16, u32, u64 at their widths",
			joinAttrs(rta(1, 7), rta(2, 0x34, 0x12), rta(3, 1, 0, 0, 0x80), rta(4, 1, 2, 3, 4, 5, 6, 7, 8)), 4,
			func(a AttrTable) string {
				u8, _ := a.U8(1)
				u16, _ := a.U16(2)
				u32, _ := a.U32(3)
				u64, _ := a.U64(4)
				return fmt.Sprintf("%d %#x %#x %#x", u8, u16, u32, u64)
			}, "7 0x1234 0x80000001 0x807060504030201"},
		{"positive: a string up to its NUL", rta(3, 'd', '1', 0, 'x'), 3,
			func(a AttrTable) string { s, ok := a.Str(3); return fmt.Sprintf("%s %t", s, ok) }, "d1 true"},
		{"positive: a string without a NUL", rta(3, 'b', 'r'), 3,
			func(a AttrTable) string { s, ok := a.Str(3); return fmt.Sprintf("%s %t", s, ok) }, "br true"},
		{"negative: an attribute longer than the data", []byte{0x20, 0, 1, 0, 1, 2}, 4,
			nil, "error"},
		{"negative: an attribute shorter than its header", []byte{2, 0, 1, 0}, 4,
			nil, "error"},
		{"negative: a max beyond the bound", nil, 2000, nil, "error"},
		{"negative: absent is not present", rta(1, 1), 4,
			func(a AttrTable) string { _, ok := a.U32(2); return fmt.Sprint(a.Has(2), ok, a.Bytes(2) == nil) }, "false false true"},
		{"boundary: a short attribute is present but no u32", rta(2, 1, 2), 4,
			func(a AttrTable) string {
				_, ok := a.U32(2)
				v, ok16 := a.U16(2)
				return fmt.Sprint(a.Has(2), ok, v, ok16)
			}, "true false 513 true"},
		{"boundary: a zero-length attribute is present", rta(2), 4,
			func(a AttrTable) string { _, ok := a.U8(2); return fmt.Sprint(a.Has(2), ok, len(a.Bytes(2))) }, "true false 0"},
		{"boundary: type max is kept, max+1 dropped", joinAttrs(rta(4, 1), rta(5, 2)), 4,
			func(a AttrTable) string { return fmt.Sprint(a.Has(4), a.Has(5), a.Len()) }, "true false 5"},
		{"boundary: empty data, an empty table", nil, 3,
			func(a AttrTable) string { return fmt.Sprint(a.Has(0), a.Len()) }, "false 4"},
		{"corner: the first occurrence wins", joinAttrs(rta(1, 1), rta(1, 2)), 4,
			func(a AttrTable) string { v, _ := a.U8(1); return fmt.Sprint(v) }, "1"},
		{"corner: NLA_F_NESTED is masked off the type", rta(1|0x8000, 9), 4,
			func(a AttrTable) string { v, ok := a.U8(1); return fmt.Sprint(v, ok) }, "9 true"},
		{"corner: a short trailing remainder is tolerated", append(rta(1, 3), 0, 0), 4,
			func(a AttrTable) string { v, _ := a.U8(1); return fmt.Sprint(v) }, "3"},
		{"corner: the zero table has nothing", nil, 0,
			func(AttrTable) string { var z AttrTable; _, ok := z.U8(0); return fmt.Sprint(z.Has(0), ok, z.Len()) }, "false false 0"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			a, err := ParseAttrTable(tc.data, tc.max)
			if tc.expected == "error" {
				if err == nil {
					t.Fatal("no error")
				}
				if tc.max > attrTableMaxCst && !errors.Is(err, ErrAttrTableMax) {
					t.Fatalf("error %v, want ErrAttrTableMax", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := tc.query(a); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestAttrTableRaw pins the raw-type helpers against parse_rtattr,
// parse_rtattr_flags and parse_rtattr_one.
func TestAttrTableRaw(t *testing.T) {
	t.Parallel()
	nested := uint16(0x8000)
	tests := []struct {
		description string
		run         func() string
		expected    string
	}{
		{"positive: parse_rtattr (flags 0) drops a flagged nest", func() string {
			a, err := ParseAttrTableFlags(rta(2|nested, 1), 4, 0)
			return fmt.Sprint(a.Has(2), err)
		}, "false <nil>"},
		{"positive: parse_rtattr_flags NLA_F_NESTED keeps it", func() string {
			a, err := ParseAttrTableFlags(rta(2|nested, 1), 4, nested)
			return fmt.Sprint(a.Has(2), err)
		}, "true <nil>"},
		{"positive: FindRawAttr matches the raw type, flags included", func() string {
			v, ok := FindRawAttr(joinAttrs(rta(6, 1), rta(6|nested, 2)), 6|nested)
			return fmt.Sprintf("%d %t", v, ok)
		}, "[2] true"},
		{"negative: FindRawAttr finds no unflagged match for a flagged one", func() string {
			_, ok := FindRawAttr(rta(6|nested, 2), 6)
			return fmt.Sprint(ok)
		}, "false"},
		{"negative: WalkRawAttrs refuses an overlong attribute", func() string {
			return fmt.Sprint(WalkRawAttrs([]byte{0x20, 0, 1, 0}, func(uint16, []byte) {}) != nil)
		}, "true"},
		{"boundary: FindRawAttr on empty data", func() string {
			_, ok := FindRawAttr(nil, 1)
			return fmt.Sprint(ok)
		}, "false"},
		{"corner: FindRawAttr returns a match before a malformed tail", func() string {
			v, ok := FindRawAttr(append(rta(1, 9), 0x20, 0, 2, 0), 1)
			return fmt.Sprintf("%d %t", v, ok)
		}, "[9] true"},
		{"corner: WalkRawAttrs keeps NET_BYTEORDER in the type", func() string {
			var got uint16
			if err := WalkRawAttrs(rta(3|0x4000, 1), func(r uint16, _ []byte) { got = r }); err != nil {
				return err.Error()
			}
			return fmt.Sprintf("%#x", got)
		}, "0x4003"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			if got := tc.run(); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
