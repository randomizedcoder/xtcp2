package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// chardevAttr is one nldev attribute, padded as the kernel pads it.
func chardevAttr(atype uint16, v []byte) []byte {
	b := binary.LittleEndian.AppendUint16(nil, uint16(RTAttrSizeCst+len(v)))
	b = binary.LittleEndian.AppendUint16(b, atype)
	b = append(b, v...)
	return append(b, make([]byte, FourByteAlignPadding(len(v)))...)
}

func chardevU64(atype uint16, v uint64) []byte {
	return chardevAttr(atype, binary.LittleEndian.AppendUint64(nil, v))
}

func chardevU32(atype uint16, v uint32) []byte {
	return chardevAttr(atype, binary.LittleEndian.AppendUint32(nil, v))
}

func chardevStr(atype uint16, s string) []byte { return chardevAttr(atype, []byte(s+"\x00")) }

// capturedMlx5Chardev is the GET_CHARDEV reply body ibv_devinfo got for mlx5_1
// on host l (rdmanative/testdata/7_2_9/l-mlx5/nldev/devinfo-mlx5_0.strace):
// driver id 1 (RDMA_DRIVER_MLX5), dev_t 231:193, driver ABI 1, uverbs1.
var capturedMlx5Chardev = []byte{
	0x08, 0x00, 0x49, 0x00, 0x01, 0x00, 0x00, 0x00,
	0x0c, 0x00, 0x48, 0x00, 0xc1, 0xe7, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x0c, 0x00, 0x47, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x0c, 0x00, 0x46, 0x00, 'u', 'v', 'e', 'r', 'b', 's', '1', 0x00,
}

// capturedMlx5ChardevRequest is the request ibv_devinfo sent for it, with
// nlmsg_seq and nlmsg_pid zeroed.
var capturedMlx5ChardevRequest = []byte{
	0x24, 0x00, 0x00, 0x00, 0x0f, 0x14, 0x01, 0x00, 0, 0, 0, 0, 0, 0, 0, 0,
	0x08, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00,
	0x0b, 0x00, 0x45, 0x00, 'u', 'v', 'e', 'r', 'b', 's', 0x00, 0x00,
}

// hugeEncodeDev is the kernel's huge_encode_dev.
func hugeEncodeDev(major, minor uint32) uint64 {
	return uint64(minor&0xff) | uint64(major)<<8 | uint64(minor&^0xff)<<12
}

func TestBuildRdmaGetChardev(t *testing.T) {
	umadAttrs := bytes.Join([][]byte{chardevU32(RdmaNldevAttrDevIndex, 2), chardevU32(RdmaNldevAttrPortIndex, 1),
		chardevStr(RdmaNldevAttrChardevType, "umad")}, nil)
	tests := []struct {
		name, category, description string
		prefix                      []byte
		seq, dev, port              uint32
		kind                        ChardevKind
		expected                    []byte
		err                         error
	}{
		{"captured_uverbs", "positive", "mlx5_1's uverbs request, as ibv_devinfo sent it",
			nil, 0, 1, 0, ChardevUverbs, capturedMlx5ChardevRequest, nil},
		{"umad_port", "positive", "a umad request names the port between the device and the type",
			nil, 9, 2, 1, ChardevUmad,
			append([]byte{0x2c, 0, 0, 0, 0x0f, 0x14, 0x01, 0x00, 9, 0, 0, 0, 0, 0, 0, 0}, umadAttrs...), nil},
		{"append", "corner", "the request is appended after what buf holds",
			[]byte{0xaa, 0xbb}, 0, 1, 0, ChardevUverbs, append([]byte{0xaa, 0xbb}, capturedMlx5ChardevRequest...), nil},
		{"uverbs_port", "negative", "uverbs is per device, so a port is refused", nil, 1, 1, 1, ChardevUverbs, nil, ErrRdmaChardevKind},
		{"umad_no_port", "negative", "umad is per port, so port 0 is refused", nil, 1, 1, 0, ChardevUmad, nil, ErrRdmaChardevKind},
		{"kind_zero", "boundary", "the zero ChardevKind", nil, 1, 1, 0, 0, nil, ErrRdmaChardevKind},
		{"kind_unknown", "boundary", "a kind past ChardevUmad", nil, 1, 1, 1, ChardevUmad + 1, nil, ErrRdmaChardevKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %x err %v", tt.category, tt.description, tt.expected, tt.err)
			got, err := BuildRdmaGetChardev(tt.prefix, tt.seq, tt.dev, tt.port, tt.kind)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err %v, want %v", err, tt.err)
			}
			if err != nil {
				if !bytes.Equal(got, tt.prefix) {
					t.Fatalf("buf changed on error: %x", got)
				}
				return
			}
			if !bytes.Equal(got, tt.expected) {
				t.Fatalf("\n got %x\nwant %x", got, tt.expected)
			}
		})
	}
}

func TestParseRdmaChardev(t *testing.T) {
	devt := chardevU64(RdmaNldevAttrChardev, hugeEncodeDev(231, 192))
	abi := chardevU64(RdmaNldevAttrChardevABI, 1)
	cat := func(b ...[]byte) []byte { return bytes.Join(b, nil) }
	type result struct {
		name                string
		major, minor        uint32
		abi                 uint64
		driverID            uint32
		hasDriverID         bool
		err                 error
		attrErr, notChardev bool
	}
	tests := []struct {
		name, category, description string
		body                        []byte
		kind                        ChardevKind
		expected                    result
	}{
		{"captured_uverbs", "positive", "mlx5_1's captured reply", capturedMlx5Chardev, ChardevUverbs,
			result{name: "uverbs1", major: 231, minor: 193, abi: 1, driverID: 1, hasDriverID: true}},
		{"umad", "positive", "a umad reply: no driver id, ABI 5", cat(chardevU64(RdmaNldevAttrChardev, hugeEncodeDev(231, 0)),
			chardevU64(RdmaNldevAttrChardevABI, 5), chardevStr(RdmaNldevAttrChardevName, "umad0")), ChardevUmad,
			result{name: "umad0", major: 231, abi: 5}},
		{"large_minor", "positive", "a minor above 255 takes huge_encode_dev's high bits", cat(chardevU64(RdmaNldevAttrChardev, hugeEncodeDev(511, 0x12345)),
			abi, chardevStr(RdmaNldevAttrChardevName, "uverbs4660")), ChardevUverbs,
			result{name: "uverbs4660", major: 511, minor: 0x12345, abi: 1}},
		{"pad_ignored", "corner", "a PAD (UNSPEC) attribute, as nla_put_u64_64bit adds where alignment needs it", cat(chardevAttr(RdmaNldevAttrUnspec, nil),
			devt, abi, chardevStr(RdmaNldevAttrChardevName, "uverbs0")), ChardevUverbs,
			result{name: "uverbs0", major: 231, minor: 192, abi: 1}},
		{"last_wins", "corner", "a repeated name is last-wins, as rd_attr_cb stores it", cat(devt, abi,
			chardevStr(RdmaNldevAttrChardevName, "uverbs9"), chardevStr(RdmaNldevAttrChardevName, "uverbs0")), ChardevUverbs,
			result{name: "uverbs0", major: 231, minor: 192, abi: 1}},
		{"missing_name", "negative", "a reply without CHARDEV_NAME", cat(devt, abi), ChardevUverbs, result{err: ErrRdmaChardev}},
		{"missing_dev", "negative", "a reply without CHARDEV", cat(abi, chardevStr(RdmaNldevAttrChardevName, "uverbs0")), ChardevUverbs, result{err: ErrRdmaChardev}},
		{"missing_abi", "negative", "a reply without CHARDEV_ABI", cat(devt, chardevStr(RdmaNldevAttrChardevName, "uverbs0")), ChardevUverbs, result{err: ErrRdmaChardev}},
		{"wrong_kind", "negative", "a uverbs name in reply to a umad request", capturedMlx5Chardev, ChardevUmad, result{err: ErrRdmaChardev}},
		{"umad_driver_id", "negative", "a driver id, which only uverbs sends, on a umad reply", cat(chardevU32(RdmaNldevAttrUverbsDriverID, 1),
			devt, chardevU64(RdmaNldevAttrChardevABI, 5), chardevStr(RdmaNldevAttrChardevName, "umad0")), ChardevUmad, result{err: ErrRdmaChardev}},
		{"slash", "negative", "a name with a path separator", cat(devt, abi, chardevStr(RdmaNldevAttrChardevName, "uverbs/0")), ChardevUverbs, result{err: ErrRdmaChardev}},
		{"dotdot", "negative", "a name that is not the prefix", cat(devt, abi, chardevStr(RdmaNldevAttrChardevName, "..")), ChardevUverbs, result{err: ErrRdmaChardev}},
		{"no_number", "negative", "the prefix alone", cat(devt, abi, chardevStr(RdmaNldevAttrChardevName, "uverbs")), ChardevUverbs, result{err: ErrRdmaChardev}},
		{"leading_zero", "negative", "a number with a leading zero, which %d never prints", cat(devt, abi, chardevStr(RdmaNldevAttrChardevName, "uverbs01")), ChardevUverbs, result{err: ErrRdmaChardev}},
		{"no_nul", "negative", "a name without its terminator", cat(devt, abi, chardevAttr(RdmaNldevAttrChardevName, []byte("uverbs0"))), ChardevUverbs, result{attrErr: true}},
		{"dev_width", "negative", "a 4-byte CHARDEV", cat(chardevU32(RdmaNldevAttrChardev, 1), abi, chardevStr(RdmaNldevAttrChardevName, "uverbs0")), ChardevUverbs, result{attrErr: true}},
		{"driver_width", "negative", "an 8-byte UVERBS_DRIVER_ID", cat(chardevU64(RdmaNldevAttrUverbsDriverID, 1), devt, abi,
			chardevStr(RdmaNldevAttrChardevName, "uverbs0")), ChardevUverbs, result{attrErr: true}},
		{"truncated", "negative", "the captured reply one byte short", capturedMlx5Chardev[:len(capturedMlx5Chardev)-1], ChardevUverbs, result{notChardev: true}},
		{"kind_zero", "negative", "the zero ChardevKind", capturedMlx5Chardev, 0, result{err: ErrRdmaChardevKind}},
		{"long_name_15", "boundary", "a 15-byte name, RdmaChardevNameMaxCst", cat(devt, abi, chardevStr(RdmaNldevAttrChardevName, "uverbs"+strings.Repeat("9", 9))), ChardevUverbs,
			result{name: "uverbs999999999", major: 231, minor: 192, abi: 1}},
		{"long_name_16", "boundary", "a 16-byte name", cat(devt, abi, chardevStr(RdmaNldevAttrChardevName, "uverbs"+strings.Repeat("9", 10))), ChardevUverbs, result{err: ErrRdmaChardev}},
		{"empty", "boundary", "an empty body", nil, ChardevUverbs, result{err: ErrRdmaChardev}},
		{"driver_abi", "corner", "uverbs ABI 1: the driver's ABI is reported, not the global 6, and is not range-checked", capturedMlx5Chardev, ChardevUverbs,
			result{name: "uverbs1", major: 231, minor: 193, abi: 1, driverID: 1, hasDriverID: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %+v", tt.category, tt.description, tt.expected)
			out := RdmaChardev{Dev: 99, NameLen: 3}
			err := ParseRdmaChardev(tt.body, tt.kind, &out)
			e := tt.expected
			switch {
			case e.attrErr:
				if !errors.Is(err, ErrRdmaAttrLen) && !errors.Is(err, ErrRdmaAttrString) {
					t.Fatalf("err %v, want an attribute error", err)
				}
			case e.notChardev:
				if err == nil || errors.Is(err, ErrRdmaChardev) {
					t.Fatalf("err %v, want a walk error", err)
				}
			case !errors.Is(err, e.err):
				t.Fatalf("err %v, want %v", err, e.err)
			}
			if err != nil {
				if out != (RdmaChardev{}) {
					t.Fatalf("out not cleared on error: %+v", out)
				}
				return
			}
			if string(out.NameBytes()) != e.name || out.Major() != e.major || out.Minor() != e.minor ||
				out.ABI != e.abi || out.DriverID != e.driverID || out.HasDriverID != e.hasDriverID {
				t.Fatalf("got %q %d:%d abi %d driver %d/%v", out.NameBytes(), out.Major(), out.Minor(), out.ABI, out.DriverID, out.HasDriverID)
			}
		})
	}
}

// TestRdmaChardevAllocs: building into a buffer with room and parsing into a
// caller's RdmaChardev allocate nothing.
func TestRdmaChardevAllocs(t *testing.T) {
	buf := make([]byte, 0, 64)
	var out RdmaChardev
	tests := []struct {
		name, category, description string
		fn                          func()
		expected                    float64
	}{
		{"parse", "positive", "ParseRdmaChardev of the captured reply", func() {
			if err := ParseRdmaChardev(capturedMlx5Chardev, ChardevUverbs, &out); err != nil {
				t.Fatal(err)
			}
		}, 0},
		{"parse_error", "negative", "ParseRdmaChardev of a reply without a name", func() {
			_ = ParseRdmaChardev(capturedMlx5Chardev[:32], ChardevUverbs, &out)
		}, 0},
		{"build", "positive", "BuildRdmaGetChardev into a buffer with room", func() {
			if _, err := BuildRdmaGetChardev(buf[:0], 1, 2, 1, ChardevUmad); err != nil {
				t.Fatal(err)
			}
		}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %v allocs", tt.category, tt.description, tt.expected)
			if got := testing.AllocsPerRun(1000, tt.fn); got != tt.expected {
				t.Fatalf("%v allocs, want %v", got, tt.expected)
			}
		})
	}
}

func BenchmarkParseChardev(b *testing.B) {
	var out RdmaChardev
	b.ReportAllocs()
	b.SetBytes(int64(len(capturedMlx5Chardev)))
	for b.Loop() {
		if err := ParseRdmaChardev(capturedMlx5Chardev, ChardevUverbs, &out); err != nil {
			b.Fatal(err)
		}
	}
}

// FuzzParseChardev: no input panics; an accepted reply has a name that is
// the kind's prefix and digits within the bound, and a driver id only for
// uverbs.
func FuzzParseChardev(f *testing.F) {
	f.Add(capturedMlx5Chardev, uint8(ChardevUverbs))
	f.Add(bytes.Join([][]byte{chardevU64(RdmaNldevAttrChardev, 1), chardevU64(RdmaNldevAttrChardevABI, 5),
		chardevStr(RdmaNldevAttrChardevName, "umad0")}, nil), uint8(ChardevUmad))
	f.Add([]byte{}, uint8(0))
	f.Fuzz(func(t *testing.T, body []byte, kind uint8) {
		var out RdmaChardev
		if err := ParseRdmaChardev(body, ChardevKind(kind), &out); err != nil {
			return
		}
		k := ChardevKind(kind)
		if !chardevName(out.NameBytes(), k.prefix()) || int(out.NameLen) > RdmaChardevNameMaxCst ||
			(out.HasDriverID && k != ChardevUverbs) {
			t.Fatalf("accepted %+v for kind %d", out, kind)
		}
	})
}
