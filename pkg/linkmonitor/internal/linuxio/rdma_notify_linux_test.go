package linuxio

import (
	"encoding/binary"
	"testing"
)

func notifyAttribute(id uint16, value []byte) []byte {
	b := make([]byte, (4+len(value)+3)&^3)
	binary.LittleEndian.PutUint16(b, uint16(4+len(value)))
	binary.LittleEndian.PutUint16(b[2:], id)
	copy(b[4:], value)
	return b
}

func notificationFixture() []byte {
	b := make([]byte, 16, 44)
	binary.LittleEndian.PutUint16(b[4:], rdmaMonitor)
	b = append(b, notifyAttribute(1, []byte{0, 0, 0, 0})...)
	b = append(b, notifyAttribute(2, []byte("mlx5_0\x00"))...)
	b = append(b, notifyAttribute(rdmaEventType, []byte{0})...)
	binary.LittleEndian.PutUint32(b, uint32(len(b)))
	return b
}

func TestRDMANotifications(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		mutate                                       func([]byte) []byte
		ok                                           bool
	}{
		{"register", "positive", "register event for index zero", "one topology hint", func(b []byte) []byte { return b }, true},
		{"future", "corner", "future event value", "conservative topology hint", func(b []byte) []byte { b[len(b)-4] = 255; return b }, true},
		{"sequence", "negative", "request reply on event socket", "reject before delivery", func(b []byte) []byte { b[8] = 1; return b }, false},
		{"sender", "negative", "nonzero header sender", "reject before delivery", func(b []byte) []byte { b[12] = 1; return b }, false},
		{"truncated", "boundary", "one byte missing", "reject malformed envelope", func(b []byte) []byte { return b[:len(b)-1] }, false},
		{"duplicate", "negative", "event attribute repeated", "reject duplicate", func(b []byte) []byte {
			b = append(b, notifyAttribute(rdmaEventType, []byte{1})...)
			binary.LittleEndian.PutUint32(b, uint32(len(b)))
			return b
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			n := 0
			err := DecodeRDMANotifications(tc.mutate(notificationFixture()), func() bool { n++; return true })
			if (err == nil) != tc.ok || (tc.ok && n != 1) || (!tc.ok && n != 0) {
				t.Fatal(tc.expectedOutcome, n, err)
			}
		})
	}
}

func TestRDMAMonitorMode(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		body                                         []byte
		enabled, valid                               bool
	}{
		{"on", "positive", "monitor mode one", "coverage supported", notifyAttribute(rdmaMonitorMode, []byte{1}), true, true},
		{"off", "negative", "monitor mode zero", "coverage unavailable", notifyAttribute(rdmaMonitorMode, []byte{0}), false, true},
		{"absent", "boundary", "old kernel without mode", "coverage unavailable", nil, false, true},
		{"future", "corner", "unknown mode value", "malformed mode rejected", notifyAttribute(rdmaMonitorMode, []byte{2}), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			enabled, err := decodeMonitorMode(tc.body)
			if enabled != tc.enabled || (err == nil) != tc.valid {
				t.Fatal(enabled, err)
			}
		})
	}
}

func FuzzRDMANotifications(f *testing.F) {
	f.Add(notificationFixture())
	f.Add([]byte{})
	f.Fuzz(func(_ *testing.T, b []byte) { _ = DecodeRDMANotifications(b, func() bool { return true }) })
}
