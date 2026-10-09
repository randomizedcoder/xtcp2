package rdmacaps

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"syscall"
	"testing"
)

func portReply(req [256]byte) [256]byte {
	b := req
	b[3], b[4] = 0x81, 0x80
	b[64+29], b[64+30], b[64+31] = 2, 2, 2
	b[64+32], b[64+35] = 0x74, 0x44
	return b
}

func TestLocalQueryTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		mutate                                       func(*[256]byte)
		want                                         error
	}{
		{"success", "positive", "local GET receives valid PortInfo", "separate supported/enabled/active retained", nil, nil},
		{"tid", "negative", "wrong lower transaction ID", "reject response", func(b *[256]byte) { b[15]++ }, ErrReply},
		{"attribute", "negative", "wrong attribute", "reject response", func(b *[256]byte) { b[17]++ }, ErrReply},
		{"port", "negative", "different port modifier", "reject response", func(b *[256]byte) { b[23]++ }, ErrReply},
		{"method", "negative", "request echoed instead of response", "reject response", func(b *[256]byte) { b[3] = 1 }, ErrReply},
		{"hops", "boundary", "nonlocal path", "reject response", func(b *[256]byte) { b[7] = 1 }, ErrReply},
		{"direction", "negative", "return direction absent", "reject response", func(b *[256]byte) { b[4] = 0 }, ErrReply},
		{"unsupported", "corner", "unsupported method/attribute status", "explicit unsupported", func(b *[256]byte) { b[5] = 12 }, syscall.EOPNOTSUPP},
		{"redirect", "corner", "redirect status", "error without following redirect", func(b *[256]byte) { b[5] = 2 }, syscall.EIO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			calls := 0
			got, err := query(t.Context(), "mlx5_0", 1, func(_ context.Context, _ string, _ uint32, b [256]byte) ([256]byte, error) {
				calls++
				if b[3] != 1 || b[6] != 0 || b[7] != 0 || binary.BigEndian.Uint64(b[24:32]) != 0 {
					t.Fatal("not a local read-only request")
				}
				for _, v := range b[128:] {
					if v != 0 {
						t.Fatal("nonzero fabric path")
					}
				}
				b = portReply(b)
				if tc.mutate != nil {
					tc.mutate(&b)
				}
				return b, nil
			})
			if !errors.Is(err, tc.want) || calls != 1 {
				t.Fatalf("%s: %v calls=%d", tc.expectedOutcome, err, calls)
			}
			if err == nil && (got.Supported != 7 || got.Enabled != 4 || got.Active != 4 || !got.Complete) {
				t.Fatal(got)
			}
		})
	}
}

func TestCapabilityEncodingTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		ext, ext2, width                             uint8
		want                                         uint32
		complete                                     bool
	}{
		{"SDR", "positive", "base SDR", "SDR plus supported base modes", 0, 0, 1, 7, true},
		{"HDR", "positive", "extended HDR supported/active", "HDR is verbs bit 6", 0x44, 0, 2, 71, true},
		{"NDR", "boundary", "highest first extended bit", "NDR is verbs bit 7", 0x88, 0, 16, 135, true},
		{"XDR", "positive", "second extended speed", "XDR supported independent of enabled", 0, 0x52, 2, 263, true},
		{"future", "negative", "unknown second extended capability bit", "maximum remains unknown", 0, 8, 2, 7, false},
		{"width", "corner", "unknown supported width", "maximum remains unknown", 0, 0, 32, 7, false},
		{"zero", "boundary", "no supported widths", "maximum remains unknown", 0, 0, 0, 7, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			b := portReply(request(1))
			data := b[64:128]
			data[62], data[56], data[30] = tc.ext, tc.ext2, tc.width
			data[63] = 31
			binary.BigEndian.PutUint32(data[20:24], 1<<14|1<<15)
			binary.BigEndian.PutUint16(data[60:62], 1<<11)
			got := decode(data)
			if got.Extended.Enabled != 31 || (tc.ext2 == 0 && got.Enabled&256 != 0) {
				t.Fatal("extended enabled sentinel confused with XDR", got)
			}
			if got.Supported != tc.want || got.Complete != tc.complete {
				t.Fatalf("%s: %+v", tc.expectedOutcome, got)
			}
		})
	}
}

func TestLocalQueryBounds(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		device                                       string
		port                                         uint32
		valid                                        bool
	}{
		{"first", "boundary", "first port", "query allowed", "hca", 1, true},
		{"last", "boundary", "last SMP port", "query allowed", strings.Repeat("a", 63), 255, true},
		{"overflow", "negative", "port outside SMP range", "no I/O", "hca", 256, false},
		{"zero", "boundary", "no port selected", "no I/O", "hca", 0, false},
		{"path", "negative", "path instead of device", "no I/O", "../hca", 1, false},
		{"long", "boundary", "oversized name", "no I/O", strings.Repeat("a", 64), 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			called := false
			_, err := query(t.Context(), tc.device, tc.port, func(_ context.Context, _ string, _ uint32, b [256]byte) ([256]byte, error) {
				called = true
				return portReply(b), nil
			})
			if called != tc.valid || (err == nil) != tc.valid {
				t.Fatal(tc.expectedOutcome, err)
			}
		})
	}
}

func FuzzCapabilityReply(f *testing.F) {
	b := portReply(request(1))
	f.Add(b[:])
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) != 256 {
			return
		}
		var b [256]byte
		copy(b[:], data)
		if validateReply(request(1), b) == nil {
			_ = decode(b[64:128])
		}
	})
}

func TestCapabilityConstraints(t *testing.T) {
	for _, tc := range []struct {
		category, description, expectedOutcome string
		mask                                   uint32
		complete                               bool
	}{
		{"positive", "independent standard masks", "known maximum", 0, true},
		{"negative", "speed/width-pairs table advertised", "unknown rather than invented cross-product", 1 << 27, false},
		{"corner", "unrelated multicast capability", "does not enable extended speed decoding", 1 << 30, true},
	} {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			b := portReply(request(1))
			data := b[64:128]
			binary.BigEndian.PutUint32(data[20:24], tc.mask)
			data[62] = 0xff
			c := decode(data)
			if c.Complete != tc.complete || c.Supported != 7 || c.Active != 4 {
				t.Fatal(tc.expectedOutcome, c)
			}
		})
	}
}
