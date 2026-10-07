package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// Crafted bytes below are boundary/corner/malformed inputs, NOT captured
// hardware evidence. Positive kernel-wire cases live in the fixture tables.
func ethtoolTestAttr(id uint16, data ...byte) []byte {
	out := make([]byte, (len(data)+7)&^3)
	binary.LittleEndian.PutUint16(out, uint16(len(data)+4))
	binary.LittleEndian.PutUint16(out[2:], id)
	copy(out[4:], data)
	return out
}
func ethtoolTestU32(v uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, v)
	return out
}
func ethtoolTestBody(command byte, attrs ...[]byte) []byte {
	out := []byte{command, 1, 0, 0}
	for _, a := range attrs {
		out = append(out, a...)
	}
	return out
}
func ethtoolTestJoin(attrs ...[]byte) []byte {
	var out []byte
	for _, a := range attrs {
		out = append(out, a...)
	}
	return out
}

func TestEthtoolScalarBoundaries(t *testing.T) {
	for _, speed := range []uint32{0, 65534, 65535, 65536, 100000, 200000, 400000, 800000, 0xfffffffe, 0xffffffff} {
		t.Run(fmt.Sprint(speed), func(t *testing.T) {
			t.Log("boundary: preserve raw uint32 Mbps without uint16 narrowing or legacy unknown conversion")
			m, err := ParseEthtool(ethtoolTestBody(4, ethtoolTestAttr(5, ethtoolTestU32(speed)...)), 0)
			if err != nil || m.LinkModes == nil || !equalScalar(m.LinkModes.Speed, speed) {
				t.Fatalf("speed=%d: %+v, %v", speed, m, err)
			}
			if m.LinkModes.Duplex != nil || m.LinkModes.Autoneg != nil {
				t.Fatal("absent fields became present zeroes")
			}
			if speed == 800000 && uint64(*m.LinkModes.Speed)*1000000 != 800000000000 {
				t.Fatal("bits/s overflow")
			}
		})
	}
	for _, duplex := range []byte{0, 1, 2, 254, 255} {
		t.Run(fmt.Sprintf("duplex-%d", duplex), func(t *testing.T) {
			t.Log("corner: preserve half/full/unknown and future enum values without coercion")
			m, err := ParseEthtool(ethtoolTestBody(4, ethtoolTestAttr(6, duplex)), 0)
			if err != nil || !equalScalar(m.LinkModes.Duplex, duplex) || m.LinkModes.Speed != nil {
				t.Fatalf("%+v, %v", m, err)
			}
		})
	}
}

func TestEthtoolMalformedAndCornerCases(t *testing.T) {
	rows := []struct {
		name, description, outcome string
		body                       []byte
		flags                      uint16
		wantErr                    error
		check                      func(EthtoolMessage) bool
	}{
		{"short-header", "three-byte genlmsghdr", "framing error", []byte{4, 1, 0}, 0, ErrGenericNetlink, nil},
		{"short-attribute", "trailing partial NLA", "framing error", []byte{4, 1, 0, 0, 4}, 0, ErrGenericNetlink, nil},
		{"zero-length", "NLA length zero", "framing error", []byte{4, 1, 0, 0, 0, 0, 5, 0}, 0, ErrGenericNetlink, nil},
		{"overrun", "NLA exceeds message", "framing error", []byte{4, 1, 0, 0, 12, 0, 5, 0}, 0, ErrGenericNetlink, nil},
		{"short-speed", "speed encoded as uint16", "width error", ethtoolTestBody(4, ethtoolTestAttr(5, 1, 0)), 0, ErrGenericNetlink, nil},
		{"long-duplex", "duplex encoded as uint32", "width error", ethtoolTestBody(4, ethtoolTestAttr(6, 1, 0, 0, 0)), 0, ErrGenericNetlink, nil},
		{"duplicate-speed", "ambiguous singleton values", "duplicate error", ethtoolTestBody(4, ethtoolTestAttr(5, 0, 0, 0, 0), ethtoolTestAttr(5, 1, 0, 0, 0)), 0, ErrGenericNetlink, nil},
		{"network-order", "flagged speed unsupported by this UAPI", "error rather than byte-swap silently", ethtoolTestBody(4, ethtoolTestAttr(0x4005, 0, 0, 0, 1)), 0, ErrGenericNetlink, nil},
		{"bad-nesting", "truncated device header child", "framing error", ethtoolTestBody(4, ethtoolTestAttr(0x8001, 1)), 0, ErrGenericNetlink, nil},
		{"bad-name", "unterminated device name", "string error", ethtoolTestBody(4, ethtoolTestAttr(0x8001, ethtoolTestAttr(2, 'e', 't', 'h')...)), 0, ErrGenericNetlink, nil},
		{"unknown-command", "future command", "explicit unsupported, retain generic attributes", ethtoolTestBody(250, ethtoolTestAttr(999, 1)), 0, ErrUnsupportedEthtool, func(m EthtoolMessage) bool {
			return m.Command == 250 && len(m.Attributes) == 1 && m.Attributes[0].ID() == 999
		}},
		{"future-version", "unimplemented version", "explicit unsupported", []byte{4, 2, 0, 0}, 0, ErrUnsupportedEthtool, nil},
		{"absent", "header-only GET dump", "nil fields, not known zero", ethtoolTestBody(4), unix.NLM_F_REQUEST, nil, func(m EthtoolMessage) bool {
			return m.Request && m.LinkModes.Speed == nil && m.Header.DeviceIndex == nil
		}},
		{"unknown-attribute", "unknown nested NLA with opaque payload", "retained without interpreting its schema", ethtoolTestBody(4, ethtoolTestAttr(0x83ff, 9, 8, 7)), 0, nil, func(m EthtoolMessage) bool {
			return len(m.Attributes) == 1 && m.Attributes[0].Type == 0x83ff && reflect.DeepEqual(m.Attributes[0].Data, []byte{9, 8, 7})
		}},
		{"rings-request", "command 16 in user enum", "RINGS_SET request, not reply", ethtoolTestBody(16), unix.NLM_F_REQUEST, nil, func(m EthtoolMessage) bool { return m.Kind == EthtoolRingsKind && m.Request && !m.Notification }},
		{"rings-reply", "same command 16 in kernel enum", "RINGS_GET_REPLY, not SET", ethtoolTestBody(16), 0, nil, func(m EthtoolMessage) bool { return m.Kind == EthtoolRingsKind && !m.Request && !m.Notification }},
		{"rings-notification", "kernel command 17", "notification, not GET reply", ethtoolTestBody(17), 0, nil, func(m EthtoolMessage) bool { return m.Kind == EthtoolRingsKind && m.Notification }},
		{"state-zero", "present link/SQI zero and future diagnostic enums", "zero stays present; missing SQI_MAX stays absent", ethtoolTestBody(6, ethtoolTestAttr(2, 0), ethtoolTestAttr(3, 0, 0, 0, 0), ethtoolTestAttr(5, 255), ethtoolTestAttr(6, 254), ethtoolTestAttr(7, 255, 255, 255, 255)), 0, nil, func(m EthtoolMessage) bool {
			v := m.LinkState
			return equalScalar(v.Link, uint8(0)) && equalScalar(v.SQI, uint32(0)) && v.SQIMax == nil && equalScalar(v.ExtendedState, uint8(255)) && equalScalar(v.ExtendedSubstate, uint8(254)) && equalScalar(v.ExtendedDownCount, uint32(0xffffffff))
		}},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.outcome)
			m, err := ParseEthtool(tc.body, tc.flags)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
			if tc.check != nil && !tc.check(m) {
				t.Fatalf("unexpected output: %+v", m)
			}
		})
	}
}

func TestEthtoolBitsetBoundaries(t *testing.T) {
	size := func(n uint32) []byte { return ethtoolTestAttr(2, ethtoolTestU32(n)...) }
	value := func(words ...uint32) []byte {
		data := make([]byte, 0, 4*len(words))
		for _, w := range words {
			data = append(data, ethtoolTestU32(w)...)
		}
		return ethtoolTestAttr(4, data...)
	}
	mask := func(words ...uint32) []byte { a := value(words...); a[2] = 5; return a }
	noMask := ethtoolTestAttr(1)
	entry := func(index uint32, set bool) []byte {
		fields := ethtoolTestAttr(1, ethtoolTestU32(index)...)
		if set {
			fields = append(fields, ethtoolTestAttr(3)...)
		}
		return ethtoolTestAttr(0x8001, fields...)
	}
	verbose := func(entries ...[]byte) []byte { return ethtoolTestAttr(0x8003, ethtoolTestJoin(entries...)...) }
	rows := []struct {
		name, description, outcome string
		attrs                      []byte
		bad                        bool
		check                      func(*EthtoolBitset) bool
	}{
		{"word-boundary", "indices 31,32,63,64,127", "all high bits survive compact decoding", ethtoolTestJoin(noMask, size(128), value(0x80000000, 0x80000001, 1, 0x80000000)), false, func(b *EthtoolBitset) bool {
			return bitsetValue(b, 31) && bitsetValue(b, 32) && bitsetValue(b, 63) && bitsetValue(b, 64) && bitsetValue(b, 127)
		}},
		{"mask", "value and selection mask differ", "preserve separate arrays", ethtoolTestJoin(size(33), value(1, 0), mask(3, 1)), false, func(b *EthtoolBitset) bool { return !b.NoMask && reflect.DeepEqual(b.Mask, []uint32{3, 1}) }},
		{"zero-size", "zero bits with empty compact value", "valid empty list", ethtoolTestJoin(noMask, size(0), value()), false, func(b *EthtoolBitset) bool { return b.Compact && len(b.Value) == 0 }},
		{"verbose-nomask", "BIT_VALUE omitted under NOMASK", "listed bit is true", ethtoolTestJoin(noMask, size(33), verbose(entry(32, false))), false, func(b *EthtoolBitset) bool { return bitsetValue(b, 32) }},
		{"verbose-mask", "BIT_VALUE omitted with mask semantics", "listed bit selected but false", ethtoolTestJoin(size(33), verbose(entry(32, false))), false, func(b *EthtoolBitset) bool { return len(b.Bits) == 1 && !b.Bits[0].Value }},
		{"name-only", "verbose bit identified only by name", "retain name without inventing index", ethtoolTestJoin(noMask, verbose(ethtoolTestAttr(0x8001, ethtoolTestAttr(2, 'x', 0)...))), false, func(b *EthtoolBitset) bool { return b.Bits[0].Index == nil && b.Bits[0].Name == "x" && b.Bits[0].Value }},
		{"missing-size", "compact value without size", "error", ethtoolTestJoin(noMask, value(1)), true, nil},
		{"missing-mask", "compact mask semantics without mask", "error", ethtoolTestJoin(size(32), value(1)), true, nil},
		{"forbidden-mask", "NOMASK with mask", "error", ethtoolTestJoin(noMask, size(32), value(1), mask(1)), true, nil},
		{"word-truncated", "33 bits require two words", "error", ethtoolTestJoin(noMask, size(33), value(1)), true, nil},
		{"huge-size", "uint32 max with tiny payload", "bounded error without size-based allocation", ethtoolTestJoin(noMask, size(0xffffffff), value(1)), true, nil},
		{"mixed", "compact and verbose encodings together", "error", ethtoolTestJoin(noMask, size(0), value(), verbose()), true, nil},
		{"index-outside", "index equals size", "error", ethtoolTestJoin(size(32), verbose(entry(32, true))), true, nil},
		{"duplicate-index", "repeated singleton bit index", "error", ethtoolTestJoin(size(32), verbose(entry(1, true), entry(1, false))), true, nil},
		{"missing-identity", "empty verbose bit", "error", ethtoolTestJoin(verbose(ethtoolTestAttr(0x8001))), true, nil},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.outcome)
			m, err := ParseEthtool(ethtoolTestBody(4, ethtoolTestAttr(0x8003, tc.attrs...)), 0)
			if tc.bad {
				if !errors.Is(err, ErrGenericNetlink) {
					t.Fatalf("expected malformed error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !tc.check(m.LinkModes.Ours) {
				t.Fatalf("unexpected bitset %+v", m.LinkModes.Ours)
			}
		})
	}
}

func TestGenericNetlinkFramingAndOwnership(t *testing.T) {
	rows := []struct {
		name, outcome string
		data          []byte
		bad           bool
	}{
		{"empty", "empty attribute list accepted", nil, false},
		{"unpadded-final", "final padding may be absent", []byte{5, 0, 99, 0, 42}, false},
		{"partial-padding", "partial padding rejected", []byte{5, 0, 99, 0, 42, 0}, true},
		{"trailing-header", "one-byte trailing header rejected", []byte{4, 0, 99, 0, 1}, true},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.outcome)
			_, err := ParseNetlinkAttributes(tc.data)
			if (err != nil) != tc.bad {
				t.Fatalf("got %v", err)
			}
		})
	}
	body := ethtoolTestBody(4, ethtoolTestAttr(5, 1, 0, 0, 0), ethtoolTestAttr(999, 42))
	m, err := ParseEthtool(body, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range body {
		body[i] = 0
	}
	if !equalScalar(m.LinkModes.Speed, uint32(1)) || m.Attributes[1].Data[0] != 42 {
		t.Fatal("retained data aliases receive buffer")
	}
}

func TestGenericNetlinkFamilyBoundaries(t *testing.T) {
	rows := []struct {
		name, outcome string
		id            uint16
	}{
		{"low-dynamic", "resolve ID 32 without hardcoded ethtool ID", 32},
		{"high-dynamic", "preserve full uint16 family ID", 65535},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.outcome)
			body := ethtoolTestBody(1, ethtoolTestAttr(1, byte(tc.id), byte(tc.id>>8)), ethtoolTestAttr(2, []byte("ethtool\x00")...))
			f, err := ParseGenericNetlinkFamily(body)
			if err != nil || f.ID != tc.id || f.Name != "ethtool" {
				t.Fatalf("%+v, %v", f, err)
			}
			if _, err := ParseGenericNetlinkFamily(ethtoolTestBody(1)); !errors.Is(err, ErrGenericNetlink) {
				t.Fatalf("missing identity accepted: %v", err)
			}
		})
	}
}

func FuzzParseEthtool(f *testing.F) {
	f.Add([]byte{}, uint16(0))
	f.Add(ethtoolTestBody(4, ethtoolTestAttr(5, 0, 0, 0, 0)), uint16(0))
	for _, kernel := range []string{"6_8_12", "7_1_4"} {
		for _, scenario := range []string{"modes-compact", "modes-verbose", "sim-fec"} {
			var family uint16
			for _, packet := range readLinkCapture(f, "testdata/"+kernel+"/link-state/matrix-vm-all-20261005/"+scenario+"/generic.pcap") {
				if packet.header.Type == GenlControllerID && packet.header.Flags&unix.NLM_F_REQUEST == 0 {
					if info, err := ParseGenericNetlinkFamily(packet.body); err == nil && info.Name == "ethtool" {
						family = info.ID
					}
				}
				if family != 0 && packet.header.Type == family {
					f.Add(packet.body, packet.header.Flags)
				}
			}
		}
	}
	f.Fuzz(func(t *testing.T, data []byte, flags uint16) {
		// Any bytes/flags must produce a bounded result or error, never panic.
		_, _ = ParseEthtool(data, flags)
		_, _ = ParseGenericNetlinkFamily(data)
	})
}
