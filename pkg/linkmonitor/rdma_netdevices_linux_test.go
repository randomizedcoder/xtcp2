package linkmonitor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
)

// netdevFixture writes a /sys/class/net/<name> directory with the four
// attributes netdevices reads.
func netdevFixture(t *testing.T, dir, ifindex string) {
	t.Helper()
	for name, value := range map[string]string{"ifindex": ifindex, "iflink": ifindex, "type": "1", "dev_port": "0"} {
		rdmaWrite(t, filepath.Join(dir, name), value)
	}
}

func netdevSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// TestRDMANetdevicesTable covers the /sys/class/net walk the RDMA association
// does. The bonding module adds bonding_masters there, a regular file; before
// the fix it failed the whole walk with ENOTDIR on 52 prod hosts.
func TestRDMANetdevicesTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		setup                                        func(t *testing.T, root string)
		names                                        []string
		err                                          error
	}{
		{"netdev", "positive", "one netdev directory", "the netdev", func(t *testing.T, root string) {
			netdevFixture(t, filepath.Join(root, "eth1"), "2")
		}, []string{"eth1"}, nil},
		{"bonding-masters", "positive", "a netdev beside the bonding module's bonding_masters file", "the netdev; the file is skipped", func(t *testing.T, root string) {
			netdevFixture(t, filepath.Join(root, "eth1"), "2")
			rdmaWrite(t, filepath.Join(root, "bonding_masters"), "bond0\n")
		}, []string{"eth1"}, nil},
		{"sysfs-symlinks", "positive", "netdevs as symlinks into /sys/devices, as sysfs lays them out, plus bonding_masters", "both netdevs", func(t *testing.T, root string) {
			devices := filepath.Join(filepath.Dir(root), "devices")
			netdevFixture(t, filepath.Join(devices, "eth1"), "2")
			netdevFixture(t, filepath.Join(devices, "bond0"), "3")
			netdevSymlink(t, filepath.Join(devices, "eth1"), filepath.Join(root, "eth1"))
			netdevSymlink(t, filepath.Join(devices, "bond0"), filepath.Join(root, "bond0"))
			rdmaWrite(t, filepath.Join(root, "bonding_masters"), "bond0\n")
		}, []string{"bond0", "eth1"}, nil},
		{"empty", "boundary", "no entries at all", "an empty set, no error", func(*testing.T, string) {}, nil, nil},
		{"only-bonding-masters", "boundary", "bonding_masters and no netdev", "an empty set, no error", func(t *testing.T, root string) {
			rdmaWrite(t, filepath.Join(root, "bonding_masters"), "")
		}, nil, nil},
		{"symlink-to-file", "corner", "a symlink resolving to a regular file", "skipped like bonding_masters", func(t *testing.T, root string) {
			netdevFixture(t, filepath.Join(root, "eth1"), "2")
			file := filepath.Join(filepath.Dir(root), "plain")
			rdmaWrite(t, file, "x")
			netdevSymlink(t, file, filepath.Join(root, "eth9"))
		}, []string{"eth1"}, nil},
		{"missing-ifindex", "negative", "a netdev directory without ifindex", "the walk fails; a real netdev is never skipped", func(t *testing.T, root string) {
			rdmaWrite(t, filepath.Join(root, "eth1", "type"), "1")
		}, nil, fs.ErrNotExist},
		{"dangling-symlink", "negative", "a netdev removed mid-walk (dangling symlink)", "the walk fails and the reconcile retries", func(t *testing.T, root string) {
			netdevSymlink(t, filepath.Join(root, "gone"), filepath.Join(root, "eth2"))
		}, nil, fs.ErrNotExist},
		{"duplicate-ifindex", "negative", "two netdevs with one ifindex", "malformed reply", func(t *testing.T, root string) {
			netdevFixture(t, filepath.Join(root, "eth1"), "2")
			netdevFixture(t, filepath.Join(root, "eth2"), "2")
		}, nil, linuxio.ErrReply},
		{"bad-name", "negative", "an entry name with a colon", "malformed reply, checked before the type", func(t *testing.T, root string) {
			rdmaWrite(t, filepath.Join(root, "a:b"), "")
		}, nil, linuxio.ErrReply},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			root := filepath.Join(t.TempDir(), "net")
			if err := os.MkdirAll(root, 0750); err != nil {
				t.Fatal(err)
			}
			tc.setup(t, root)
			got, err := rdmaFilesystem{netRoot: root}.netdevices(t.Context())
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("%s: err=%v, want %v", tc.expectedOutcome, err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			names := make([]string, 0, len(got))
			for name := range got {
				names = append(names, name)
			}
			slices.Sort(names)
			if !slices.Equal(names, tc.names) {
				t.Fatalf("%s: got %v, want %v", tc.expectedOutcome, names, tc.names)
			}
		})
	}
}
