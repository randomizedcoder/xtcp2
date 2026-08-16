//go:build linux

package nicinfo

import (
	"testing"
)

// sysfsRoot points at the committed fake sysfs tree. The Go toolchain ignores
// testdata/, so it never affects builds. See testdata/sys/class/net/... for the
// fleet driver/PCI matrix and the virtual interfaces that must be skipped.
const sysfsRoot = "./testdata/sys"

// findNIC returns the NIC with the given name from a Collect result, or false.
func findNIC(nics []NIC, name string) (NIC, bool) {
	for _, n := range nics {
		if n.Ifname == name {
			return n, true
		}
	}
	return NIC{}, false
}

func TestCollect(t *testing.T) {
	// Ask for every interface in the fixture at once; virtual ones must drop
	// out and physical ones must be populated from sysfs.
	all := []string{
		"eth-mlx", "eth-ice", "eth-i40e", "eth-igb", "eth-tg3", "eth-cdc",
		"lo", "veth0", "docker0", "does-not-exist",
	}
	nics := Collect(sysfsRoot, all)

	tests := []struct {
		name      string
		ifname    string
		present   bool // want the interface in the result?
		driver    string
		model     string
		vendor    uint32
		device    uint32
		busInfo   string
		speed     uint32
		operstate string
	}{
		{
			name:   "positive mlx5_core ConnectX-7 up 200G",
			ifname: "eth-mlx", present: true,
			driver: "mlx5_core", model: "ConnectX-7",
			vendor: 0x15b3, device: 0xa2dc,
			busInfo: "0000:01:00.0", speed: 200000, operstate: "up",
		},
		{
			name:   "positive ice E810 up 100G",
			ifname: "eth-ice", present: true,
			driver: "ice", model: "E810",
			vendor: 0x8086, device: 0x159b,
			busInfo: "0000:03:00.0", speed: 100000, operstate: "up",
		},
		{
			name:   "positive i40e X710 up 40G",
			ifname: "eth-i40e", present: true,
			driver: "i40e", model: "X710",
			vendor: 0x8086, device: 0x15ff,
			busInfo: "0000:04:00.0", speed: 40000, operstate: "up",
		},
		{
			name:   "boundary igb speed -1 and operstate down => 0",
			ifname: "eth-igb", present: true,
			driver: "igb", model: "I350",
			vendor: 0x8086, device: 0x1521,
			busInfo: "0000:05:00.0", speed: 0, operstate: "down",
		},
		{
			name:   "positive tg3 BCM5720 1G",
			ifname: "eth-tg3", present: true,
			driver: "tg3", model: "BCM5720",
			vendor: 0x14e4, device: 0x165f,
			busInfo: "0000:06:00.0", speed: 1000, operstate: "up",
		},
		{
			name:   "corner cdc_ether unknown pci id => empty model, boundary speed file absent => 0",
			ifname: "eth-cdc", present: true,
			driver: "cdc_ether", model: "",
			vendor: 0x0bda, device: 0x8153,
			busInfo: "usb-0000:00:14.0-1", speed: 0, operstate: "up",
		},
		{name: "negative virtual lo skipped", ifname: "lo", present: false},
		{name: "negative virtual veth0 skipped", ifname: "veth0", present: false},
		{name: "negative virtual docker0 skipped", ifname: "docker0", present: false},
		{name: "negative nonexistent iface skipped", ifname: "does-not-exist", present: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := findNIC(nics, tt.ifname)
			if ok != tt.present {
				t.Fatalf("presence of %q = %v, want %v", tt.ifname, ok, tt.present)
			}
			if !tt.present {
				return
			}
			if got.Driver != tt.driver {
				t.Errorf("Driver = %q, want %q", got.Driver, tt.driver)
			}
			if got.Model != tt.model {
				t.Errorf("Model = %q, want %q", got.Model, tt.model)
			}
			if got.PCIVendor != tt.vendor {
				t.Errorf("PCIVendor = %#x, want %#x", got.PCIVendor, tt.vendor)
			}
			if got.PCIDevice != tt.device {
				t.Errorf("PCIDevice = %#x, want %#x", got.PCIDevice, tt.device)
			}
			if got.BusInfo != tt.busInfo {
				t.Errorf("BusInfo = %q, want %q", got.BusInfo, tt.busInfo)
			}
			if got.SpeedMbps != tt.speed {
				t.Errorf("SpeedMbps = %d, want %d", got.SpeedMbps, tt.speed)
			}
			if got.Operstate != tt.operstate {
				t.Errorf("Operstate = %q, want %q", got.Operstate, tt.operstate)
			}
			// FwVersion is only populated by the production ioctl; with an
			// injected sysfs root it must stay empty.
			if got.FwVersion != "" {
				t.Errorf("FwVersion = %q, want empty (ioctl skipped for fake sysfs)", got.FwVersion)
			}
		})
	}
}

func TestCollectCornerCases(t *testing.T) {
	t.Run("corner empty ifnames returns nil", func(t *testing.T) {
		if got := Collect(sysfsRoot, nil); got != nil {
			t.Fatalf("Collect(_, nil) = %v, want nil", got)
		}
	})
	t.Run("negative missing sysfs root returns nil", func(t *testing.T) {
		if got := Collect("./testdata/does-not-exist", []string{"eth-mlx"}); got != nil {
			t.Fatalf("Collect(bad root) = %v, want nil", got)
		}
	})
}

func TestDefaultUplinks(t *testing.T) {
	tests := []struct {
		name string
		max  int
		zero bool // want empty/nil result regardless of host
	}{
		{"corner max zero", 0, true},
		{"negative max", -3, true},
		{"positive small max", 2, false},
		{"boundary max one", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultUplinks(tt.max)
			if tt.zero {
				if len(got) != 0 {
					t.Fatalf("DefaultUplinks(%d) = %v, want empty", tt.max, got)
				}
				return
			}
			// Best-effort on the host: can't assert exact names, but the
			// contract (cap + dedup + non-panic) must hold.
			if len(got) > tt.max {
				t.Fatalf("DefaultUplinks(%d) returned %d names, over cap", tt.max, len(got))
			}
			seen := make(map[string]struct{})
			for _, n := range got {
				if n == "" {
					t.Fatalf("DefaultUplinks returned empty name")
				}
				if _, dup := seen[n]; dup {
					t.Fatalf("DefaultUplinks returned duplicate %q", n)
				}
				seen[n] = struct{}{}
			}
		})
	}
}

func TestPhysicalInterfaces(t *testing.T) {
	// The fake sysfs tree has six eth-* NICs with a device/ entry plus
	// docker0/lo/veth0 with none; only the former are physical.
	allPhysical := []string{"eth-cdc", "eth-i40e", "eth-ice", "eth-igb", "eth-mlx", "eth-tg3"}

	tests := []struct {
		name string
		root string
		max  int
		want []string
	}{
		{"corner max zero", sysfsRoot, 0, nil},
		{"negative max", sysfsRoot, -1, nil},
		{"boundary max one", sysfsRoot, 1, allPhysical[:1]},
		{"boundary max two", sysfsRoot, 2, allPhysical[:2]},
		{"all physical, cap above count", sysfsRoot, 99, allPhysical},
		{"negative: missing sysfs root", "./testdata/does-not-exist", 2, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PhysicalInterfaces(tt.root, tt.max)
			if len(got) != len(tt.want) {
				t.Fatalf("PhysicalInterfaces(%q, %d) = %v, want %v", tt.root, tt.max, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("PhysicalInterfaces(%q, %d) = %v, want %v", tt.root, tt.max, got, tt.want)
				}
			}
		})
	}
}
