//go:build linux

package nicinfo

import "testing"

func TestModelFor(t *testing.T) {
	tests := []struct {
		name   string
		vendor uint32
		device uint32
		want   string
	}{
		{"positive mlx ConnectX-7", 0x15b3, 0xa2dc, "ConnectX-7"},
		{"positive mlx ConnectX-6", 0x15b3, 0x1021, "ConnectX-6"},
		{"positive mlx ConnectX-6 Dx", 0x15b3, 0x101d, "ConnectX-6 Dx"},
		{"positive mlx ConnectX-6 Lx", 0x15b3, 0x101b, "ConnectX-6 Lx"},
		{"positive intel E810", 0x8086, 0x159b, "E810"},
		{"positive intel X710", 0x8086, 0x15ff, "X710"},
		{"positive intel I350", 0x8086, 0x1521, "I350"},
		{"positive broadcom BCM5720", 0x14e4, 0x165f, "BCM5720"},
		{"corner unknown device known vendor", 0x8086, 0xffff, ""},
		{"corner unknown vendor", 0xdead, 0xbeef, ""},
		{"boundary zero:zero", 0, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modelFor(tt.vendor, tt.device); got != tt.want {
				t.Fatalf("modelFor(%#x, %#x) = %q, want %q", tt.vendor, tt.device, got, tt.want)
			}
		})
	}
}

func TestParsePCIIDs(t *testing.T) {
	t.Run("positive parses embedded file", func(t *testing.T) {
		m := parsePCIIDs(pciIDsData)
		if len(m) != 8 {
			t.Fatalf("parsed %d entries, want 8", len(m))
		}
		if got := m[pciKey{0x15b3, 0xa2dc}]; got != "ConnectX-7" {
			t.Fatalf("15b3:a2dc = %q, want ConnectX-7", got)
		}
	})

	t.Run("positive multi-word model kept intact", func(t *testing.T) {
		m := parsePCIIDs([]byte("15b3:101d ConnectX-6 Dx\n"))
		if got := m[pciKey{0x15b3, 0x101d}]; got != "ConnectX-6 Dx" {
			t.Fatalf("model = %q, want %q", got, "ConnectX-6 Dx")
		}
	})

	t.Run("negative comments and blank lines ignored", func(t *testing.T) {
		in := "# comment\n\n   \n# 8086:159b NotReal\n8086:1521 I350\n"
		m := parsePCIIDs([]byte(in))
		if len(m) != 1 {
			t.Fatalf("parsed %d entries, want 1", len(m))
		}
		if _, ok := m[pciKey{0x8086, 0x1521}]; !ok {
			t.Fatalf("expected 8086:1521 present")
		}
	})

	t.Run("corner malformed lines skipped", func(t *testing.T) {
		in := "notanid\n" + // no colon, no space
			"zz:zz Bad Vendor Hex\n" + // bad hex
			"8086:zz Bad Device Hex\n" + // bad device hex
			"8086:1521\n" + // id with no model
			"8086:1521 \n" + // id with blank model
			"8086:159b E810\n" // the only valid line
		m := parsePCIIDs([]byte(in))
		if len(m) != 1 {
			t.Fatalf("parsed %d entries, want 1 (%v)", len(m), m)
		}
		if got := m[pciKey{0x8086, 0x159b}]; got != "E810" {
			t.Fatalf("8086:159b = %q, want E810", got)
		}
	})

	t.Run("boundary empty input", func(t *testing.T) {
		if m := parsePCIIDs(nil); len(m) != 0 {
			t.Fatalf("parsePCIIDs(nil) = %v, want empty", m)
		}
	})
}
