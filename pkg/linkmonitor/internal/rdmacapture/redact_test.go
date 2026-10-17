package rdmacapture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testValues are a ConnectX-style port: a MAC, a node GUID that is not the
// MAC's EUI-64, a link-local GID that is, an IPv4-mapped GID and a global
// IPv6 address.
const testValues = `
mac 98:03:9b:12:34:56
guid 9803:9b03:0012:3456
gid fe80:0000:0000:0000:9a03:9bff:fe12:3456
gid ::ffff:10.1.2.3
ipv4 10.1.2.3
ipv6 2400:cb00::5
mac 00:00:5e:00:53:07
`

func testRedactor(t *testing.T) *Redactor {
	t.Helper()
	values, err := ReadValues(strings.NewReader(testValues))
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRedactor(values)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func hexdump(data []byte) string {
	var b strings.Builder
	for off := 0; off < len(data); off += 16 {
		end := min(off+16, len(data))
		b.Write(renderHexdumpLine(fmt.Sprintf("%05x", off), data[off:end]))
		b.WriteByte('\n')
	}
	return b.String()
}

func pcap(records ...[]byte) []byte {
	out := make([]byte, 24)
	binary.LittleEndian.PutUint32(out, 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(out[4:], 2)
	binary.LittleEndian.PutUint16(out[6:], 4)
	binary.LittleEndian.PutUint32(out[16:], 65535)
	binary.LittleEndian.PutUint32(out[20:], 253)
	for _, r := range records {
		h := make([]byte, 16)
		binary.LittleEndian.PutUint32(h[8:], uint32(len(r)))
		binary.LittleEndian.PutUint32(h[12:], uint32(len(r)))
		out = append(append(out, h...), r...)
	}
	return out
}

var (
	realMAC  = []byte{0x98, 0x03, 0x9b, 0x12, 0x34, 0x56}
	docMAC   = []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x01}
	realGUID = []byte{0x98, 0x03, 0x9b, 0x03, 0x00, 0x12, 0x34, 0x56}
	docGUID  = []byte{0x02, 0x00, 0x5e, 0x10, 0x00, 0x00, 0x00, 0x01}
	realLL   = []byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0x9a, 0x03, 0x9b, 0xff, 0xfe, 0x12, 0x34, 0x56}
	docLL    = []byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0x02, 0x00, 0x5e, 0xff, 0xfe, 0x00, 0x53, 0x01}
)

func reversed(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[len(b)-1-i] = b[i]
	}
	return out
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func TestRedact(t *testing.T) {
	r := testRedactor(t)
	tests := []struct {
		name        string
		category    string
		description string
		format      Format
		input       []byte
		expected    []byte // nil: an error
	}{
		{"mac_binary", "positive", "MAC bytes inside a buffer", FormatBinary,
			cat([]byte{0xaa}, realMAC, []byte{0xbb}), cat([]byte{0xaa}, docMAC, []byte{0xbb})},
		{"guid_be", "positive", "GUID in network order", FormatBinary, realGUID, docGUID},
		{"guid_le", "positive", "GUID as NLDEV's host-order u64 carries it", FormatBinary, reversed(realGUID), reversed(docGUID)},
		{"gid_link_local", "positive", "link-local GID becomes fe80:: and the EUI-64 of the MAC's replacement", FormatBinary, realLL, docLL},
		{"gid_mapped", "corner", "IPv4-mapped GID keeps ::ffff: and maps the IPv4 address", FormatBinary,
			[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 10, 1, 2, 3}, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 192, 0, 2, 1}},
		{"doc_value_kept", "boundary", "a MAC already in 00:00:5e:00:53:00/24 is not rewritten", FormatBinary,
			[]byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x07}, []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x07}},
		{"text_sysfs_guid", "positive", "sysfs node_guid", FormatText, []byte("9803:9b03:0012:3456\n"), []byte("0200:5e10:0000:0001\n")},
		{"text_gid_full", "positive", "sysfs gids/0", FormatText,
			[]byte("fe80:0000:0000:0000:9a03:9bff:fe12:3456\n"), []byte("fe80:0000:0000:0000:0200:5eff:fe00:5301\n")},
		{"text_hex_guid", "positive", "infiniband-diags 0x GUID", FormatText, []byte("Node GUID: 0x98039b0300123456\n"), []byte("Node GUID: 0x02005e1000000001\n")},
		{"text_mac_upper", "corner", "upper-case MAC stays upper-case", FormatText, []byte("98:03:9B:12:34:56"), []byte("00:00:5E:00:53:01")},
		{"text_compressed_ipv6", "corner", "compressed IPv6 may change length in text", FormatText, []byte("inet6 2400:cb00::5/64"), []byte("inet6 2001:db8::1/64")},
		{"text_ipv4", "positive", "IPv4 with a prefix length", FormatText, []byte("inet 10.1.2.3/24"), []byte("inet 192.0.2.1/24")},
		{"text_ipv4_boundary", "negative", "an address that only contains the value is left alone", FormatText,
			[]byte("110.1.2.34 10.1.2.30"), []byte("110.1.2.34 10.1.2.30")},
		{"strace_quoted", "positive", "an -xx read buffer", FormatStrace,
			[]byte(`read(3, "\x98\x03\x9b\x12\x34\x56", 6) = 6` + "\n"), []byte(`read(3, "\x00\x00\x5e\x00\x53\x01", 6) = 6` + "\n")},
		{"strace_printable", "corner", "strace printed some MAC bytes as characters", FormatStrace,
			[]byte(`write(3, "\x98\x03\x9b\x124V", 6) = 6`), []byte(`write(3, "\x00\x00\x5e\x00S\x01", 6) = 6`)},
		{"strace_truncated", "corner", "a string cut short inside a value; the longest value it could start wins", FormatStrace,
			[]byte(`sendmsg(3, "\x00\x98\x03\x9b"..., 64) = 64`), []byte(`sendmsg(3, "\x00\x02\x00\x5e"..., 64) = 64`)},
		{"strace_hexdump_split", "boundary", "a MAC split across two dump lines", FormatStrace,
			[]byte("x\n" + hexdump(cat(make([]byte, 13), realMAC, make([]byte, 5)))),
			[]byte("x\n" + hexdump(cat(make([]byte, 13), docMAC, make([]byte, 5))))},
		{"pcap_record", "positive", "a pcap record holding a GID", FormatPcap,
			pcap(cat([]byte{1, 2}, realLL)), pcap(cat([]byte{1, 2}, docLL))},
		{"pcap_truncated", "negative", "a record claiming more bytes than remain", FormatPcap,
			pcap(realMAC)[:24+16+3], nil},
		{"pcap_bad_magic", "negative", "not a pcap file", FormatPcap, make([]byte, 24), nil},
		{"pcap_empty", "boundary", "a pcap with a header and no records", FormatPcap, pcap(), pcap()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %q", tt.category, tt.description, tt.expected)
			got, err := r.Redact(tt.input, tt.format)
			if tt.expected == nil {
				if !errors.Is(err, ErrPcap) {
					t.Fatalf("got %q, %v; want ErrPcap", got, err)
				}
				return
			}
			if err != nil || !bytes.Equal(got, tt.expected) {
				t.Fatalf("got %q, %v\nwant %q", got, err, tt.expected)
			}
			if (tt.format == FormatBinary || tt.format == FormatPcap) && len(got) != len(tt.input) {
				t.Fatalf("length %d, want %d: redaction must keep lengths", len(got), len(tt.input))
			}
			// redaction_stable: a second pass over redacted bytes changes nothing.
			again, err := r.Redact(got, tt.format)
			if err != nil || !bytes.Equal(again, got) {
				t.Fatalf("redaction_stable: second pass gave %q, %v", again, err)
			}
		})
	}
}

func TestRedactorBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		values      string
		expected    string // "" means success; otherwise an error substring
	}{
		{"no_values", "boundary", "no values: every format is unchanged", "", ""},
		{"too_many", "negative", "more than 255 MACs cannot be numbered", manyMACs(256), "more than 255"},
		{"max", "boundary", "255 MACs can", manyMACs(255), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %q", tt.category, tt.description, tt.expected)
			values, err := ReadValues(strings.NewReader(tt.values))
			if err != nil {
				t.Fatal(err)
			}
			r, err := NewRedactor(values)
			if tt.expected != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expected) {
					t.Fatalf("got %v, want %q", err, tt.expected)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(values) == 0 {
				in := cat(realMAC, []byte(" 10.1.2.3"))
				for _, f := range []Format{FormatText, FormatBinary, FormatStrace} {
					if got, _ := r.Redact(in, f); !bytes.Equal(got, in) {
						t.Fatalf("format %d changed %q to %q", f, in, got)
					}
				}
			}
		})
	}
}

func manyMACs(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("mac 98:03:9b:00:")
		b.WriteString(string([]byte{"0123456789abcdef"[i>>4], "0123456789abcdef"[i&0xf]}))
		b.WriteString(":01\n")
	}
	return b.String()
}

func TestRedactionMap(t *testing.T) {
	t.Log("positive: the map holds each real value's sha256 and its documentation value; expected: no real value appears in it")
	r := testRedactor(t)
	m := r.Map()
	// 2 MACs, 1 IPv4, 2 GUIDs (one derived from the link-local GID), 3 IPv6.
	if len(m) != 8 {
		t.Fatalf("got %d entries, want 8: %v", len(m), m)
	}
	for k, v := range m {
		for _, real := range []string{"98:03:9b", "9803:9b03", "10.1.2.3", "2400:cb00", "9a03:9bff"} {
			if strings.Contains(k, real) || strings.Contains(v, real) {
				t.Fatalf("map entry %s -> %s holds real value %s", k, v, real)
			}
		}
	}
}

// TestStraceCorpusRoundTrip re-renders every committed RDMA strace capture.
// None holds a test value, so each must come back byte for byte; any
// difference is a hex dump or quoted string the redactor mis-parses.
func TestStraceCorpusRoundTrip(t *testing.T) {
	t.Log("positive: every pkg/xtcpnl RDMA strace capture; expected: byte-identical after redaction")
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "xtcpnl", "testdata", "*", "rdma", "*.strace"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no corpus: %v", err)
	}
	r := testRedactor(t)
	for _, p := range paths {
		data, err := os.ReadFile(p) // #nosec G304 -- committed testdata
		if err != nil {
			t.Fatal(err)
		}
		got, err := r.Redact(data, FormatStrace)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("%s: round trip changed the file (%v)", p, err)
		}
	}
}
