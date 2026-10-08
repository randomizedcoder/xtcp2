package linkmonitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func TestIdentityEligibility(t *testing.T) {
	physical := &linuxio.DevlinkPort{HasFlavor: true, Flavor: 0}
	vf := &linuxio.DevlinkPort{Bus: "pci", Device: "0000:01:00.1", HasFlavor: true, Flavor: 4}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		kind, path                                   string
		wireless, ownVF, switchID                    bool
		port                                         *linuxio.DevlinkPort
		want                                         model.Eligibility
	}{
		{"physical", "positive", "physical PCI ancestry", "eligible", "", "/sys/devices/pci/0000:01:00.0", false, false, false, physical, model.Eligible},
		{"bond member", "positive", "physical interface enslaved to a bond", "member remains eligible", "", "/sys/devices/pci/0000:01:00.0", false, false, false, physical, model.Eligible},
		{"RoCE", "positive", "RoCE uses the physical Ethernet netdevice", "one eligible Ethernet identity", "", "/sys/devices/pci/0000:01:00.0", false, false, false, physical, model.Eligible},
		{"usb", "positive", "USB hardware ancestry", "eligible", "", "/sys/devices/usb/1-1", false, false, false, nil, model.Eligible},
		{"guest", "positive", "guest virtio hardware ancestry", "eligible", "", "/sys/devices/pci/virtio0", false, false, false, nil, model.Eligible},
		{"own VF", "corner", "VF association matches its own PCI device", "eligible, not PF representor", "", "/sys/devices/pci/0000:01:00.1", false, true, true, vf, model.Eligible},
		{"representor", "negative", "VF eswitch port under parent PF", "excluded", "", "/sys/devices/pci/0000:01:00.0", false, false, true, vf, model.Excluded},
		{"ambiguous", "corner", "switch ID without devlink evidence", "unknown, block learning", "", "/sys/devices/pci/0000:01:00.0", false, false, true, nil, model.EligibilityUnknown},
		{"conflicting", "negative", "devlink physical port lacks hardware ancestry", "unknown, not guessed physical", "", "", false, false, false, physical, model.EligibilityUnknown},
		{"wireless", "negative", "wireless directory present", "excluded", "", "/sys/devices/pci/0000:01:00.0", true, false, false, nil, model.Excluded},
		{"virtual", "negative", "veth kind despite parent evidence", "excluded", "veth", "/sys/devices/pci/0000:01:00.0", false, false, false, nil, model.Excluded},
		{"bond master", "negative", "bond virtual interface", "excluded", "bond", "", false, false, false, nil, model.Excluded},
		{"unknown kind", "corner", "future unclassified link kind", "unknown", "future", "/sys/devices/pci/0000:01:00.0", false, false, false, nil, model.EligibilityUnknown},
		{"oversize", "boundary", "identity exceeds maximum", "unknown without oversized identity", "", strings.Repeat("x", maxHardwareIdentity+1), false, false, false, nil, model.EligibilityUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			link := &xtcpnl.LinkInfo{Type: unix.ARPHRD_ETHER, Kind: tc.kind}
			if tc.name == "bond member" {
				link.SlaveKind = "bond"
			}
			if tc.switchID {
				link.Detail.PhysSwitchID = []byte{1}
			}
			got, identity := classifyHardware(link, hardwareMetadata{path: tc.path, wireless: tc.wireless, ownVF: tc.ownVF}, tc.port)
			if got != tc.want || len(identity) > maxHardwareIdentity {
				t.Fatalf("%s: eligibility %v identity length %d", tc.expectedOutcome, got, len(identity))
			}
		})
	}
}

func TestIdentityFilesystem(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		index                                        uint32
		wireless, broken, wantError                  bool
	}{
		{"physical", "positive", "valid sysfs device symlink", "return stable ancestry and port", 1, false, false, false},
		{"wireless", "positive", "wireless child directory", "wireless evidence true", 1, true, false, false},
		{"replaced", "corner", "sysfs index differs from query", "reject identity", 2, false, false, true},
		{"component file", "negative", "device component is a regular file", "metadata error", 1, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			root := t.TempDir()
			path := filepath.Join(root, "eth0")
			if err := os.MkdirAll(filepath.Join(root, "hardware"), 0750); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0750); err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]string{"ifindex": "1\n", "dev_port": "0\n"} {
				if err := os.WriteFile(filepath.Join(path, name), []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.broken {
				if err := os.WriteFile(filepath.Join(path, "device"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(filepath.Join(root, "hardware"), filepath.Join(path, "device")); err != nil {
				t.Fatal(err)
			}
			if tc.wireless {
				if err := os.Mkdir(filepath.Join(path, "wireless"), 0750); err != nil {
					t.Fatal(err)
				}
			}
			m, err := (identityFilesystem{root: root}).read(t.Context(), "eth0", tc.index)
			if (err != nil) != tc.wantError || (err == nil && (m.wireless != tc.wireless || m.path == "")) {
				t.Fatalf("%s: metadata %+v err %v", tc.expectedOutcome, m, err)
			}
		})
	}
}
