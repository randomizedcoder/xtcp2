package rdmacapture

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseValue(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		kind, text  string
		expected    []byte // nil: ErrValue
		wantKind    Kind
	}{
		{"mac", "positive", "MAC as sysfs prints it", "mac", "98:03:9b:12:34:56", []byte{0x98, 0x03, 0x9b, 0x12, 0x34, 0x56}, KindMAC},
		{"mac_upper", "corner", "upper-case MAC", "mac", "98:03:9B:12:34:56", []byte{0x98, 0x03, 0x9b, 0x12, 0x34, 0x56}, KindMAC},
		{"mac_eui64", "negative", "an 8-byte EUI-64 is not a MAC", "mac", "98:03:9b:ff:fe:12:34:56", nil, 0},
		{"guid_sysfs", "positive", "GUID in sysfs node_guid form", "guid", "9803:9b03:0012:3456", []byte{0x98, 0x03, 0x9b, 0x03, 0x00, 0x12, 0x34, 0x56}, KindGUID},
		{"guid_hex", "boundary", "GUID as 0x and 16 hex digits", "guid", "0x98039b0300123456", []byte{0x98, 0x03, 0x9b, 0x03, 0x00, 0x12, 0x34, 0x56}, KindGUID},
		{"guid_short", "negative", "15 hex digits", "guid", "0x98039b030012345", nil, 0},
		{"guid_bad_hex", "negative", "non-hex digit in sysfs form", "guid", "9803:9b03:0012:345g", nil, 0},
		{"ipv4", "positive", "dotted IPv4", "ipv4", "10.1.2.3", []byte{10, 1, 2, 3}, KindIPv4},
		{"ipv4_mapped_as_ipv4", "negative", "an IPv4-mapped IPv6 address is not an ipv4 value", "ipv4", "::ffff:10.1.2.3", nil, 0},
		{"gid_full", "positive", "GID in sysfs's uncompressed form", "gid", "fe80:0000:0000:0000:9a03:9bff:fe12:3456",
			[]byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0x9a, 0x03, 0x9b, 0xff, 0xfe, 0x12, 0x34, 0x56}, KindIPv6},
		{"gid_mapped", "corner", "IPv4-mapped GID", "gid", "::ffff:10.1.2.3",
			[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 10, 1, 2, 3}, KindIPv6},
		{"ipv6_zone", "negative", "a zone is not part of an identifier", "ipv6", "fe80::1%eth0", nil, 0},
		{"unknown_kind", "negative", "kind outside the list", "uuid", "x", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %x", tt.category, tt.description, tt.expected)
			v, err := ParseValue(tt.kind, tt.text)
			if tt.expected == nil {
				if !errors.Is(err, ErrValue) {
					t.Fatalf("got %v, %v; want ErrValue", v, err)
				}
				return
			}
			if err != nil || v.Kind != tt.wantKind || !bytes.Equal(v.Bytes, tt.expected) {
				t.Fatalf("got %v %x, %v; want %v %x", v.Kind, v.Bytes, err, tt.wantKind, tt.expected)
			}
		})
	}
}

func TestReadValues(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       string
		expected    int // values read; -1: error
	}{
		{"lines", "positive", "one value per line", "mac 98:03:9b:12:34:56\nguid 9803:9b03:0012:3456\n", 2},
		{"comments", "boundary", "blank lines and comments only", "\n# none\n   \n", 0},
		{"three_fields", "negative", "a line with a third field", "mac 98:03:9b:12:34:56 extra\n", -1},
		{"bad_value", "negative", "a value that does not parse names its line", "mac 98:03:9b:12:34:56\nipv4 300.1.1.1\n", -1},
		{"indented", "corner", "leading and trailing space is ignored", "  ipv4 10.1.2.3  \n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %d", tt.category, tt.description, tt.expected)
			values, err := ReadValues(strings.NewReader(tt.input))
			if tt.expected < 0 {
				if err == nil {
					t.Fatalf("got %d values, want an error", len(values))
				}
				return
			}
			if err != nil || len(values) != tt.expected {
				t.Fatalf("got %d values, %v; want %d", len(values), err, tt.expected)
			}
		})
	}
}
