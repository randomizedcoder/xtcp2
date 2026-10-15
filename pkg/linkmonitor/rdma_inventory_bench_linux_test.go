package linkmonitor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

// rdmaScalarOSOpen is the attribute reader before raw descriptors, kept as
// the benchmarks' baseline: os.Open registers the file with the runtime
// poller and io.ReadAll reads again for EOF.
func rdmaScalarOSOpen(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	b, readErr := io.ReadAll(io.LimitReader(f, rdmaScalarLimit+1))
	if err := errors.Join(readErr, f.Close(), ctx.Err()); err != nil {
		return "", err
	}
	if len(b) > rdmaScalarLimit {
		return "", linuxio.ErrLimit
	}
	return strings.TrimSpace(string(b)), nil
}

var rdmaScalarReaders = []struct {
	name, description, expected string
	read                        func(context.Context, string) (string, error)
}{
	{"os-open", "os.Open and io.ReadAll, as before", "baseline", rdmaScalarOSOpen},
	{"raw", "unix.Open, one pread, close", "fewer allocations and less time than the baseline", rdmaScalar},
}

// BenchmarkRDMAGIDScan reads a 256-slot GID table on tmpfs, as a resync does
// for every port, with each reader. Each op is one table.
func BenchmarkRDMAGIDScan(b *testing.B) {
	port := b.TempDir()
	paths := make([]string, 0, 256)
	for i := range 256 {
		path := filepath.Join(port, "gid_attrs", "types", strconv.Itoa(i))
		rdmaWrite(b, path, "RoCE v2")
		paths = append(paths, path)
	}
	for _, tc := range rdmaScalarReaders {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s; expected: %s", tc.description, tc.expected)
			b.ReportAllocs()
			for b.Loop() {
				for _, path := range paths {
					if _, err := tc.read(b.Context(), path); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
	b.Run("gids", func(b *testing.B) {
		b.Log("the inventory's whole GID scan, raw reader; expected: about the raw row plus the directory read")
		b.ReportAllocs()
		for b.Loop() {
			var p model.RDMAPort
			if err := (rdmaFilesystem{}).gids(b.Context(), port, &p); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkRDMAHostGIDs reads the host's GID tables, the bulk of a resync's
// RDMA cost: every slot's type file, then each populated slot's ndev. Each op
// is one scan over every port.
func BenchmarkRDMAHostGIDs(b *testing.B) {
	files, ports := hostRDMAPorts(b)
	var slots []string
	for i := range ports {
		dir := filepath.Join(files.root, ports[i].Device, "ports", strconv.FormatUint(uint64(ports[i].Port), 10), "gid_attrs", "types")
		entries, err := os.ReadDir(dir)
		if err != nil {
			b.Skip(err)
		}
		for _, e := range entries {
			slots = append(slots, filepath.Join(dir, e.Name()))
		}
	}
	for _, tc := range rdmaScalarReaders {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s, %d slots; expected: %s", tc.description, len(slots), tc.expected)
			b.ReportAllocs()
			for b.Loop() {
				for _, path := range slots {
					if _, err := tc.read(b.Context(), path); err != nil && !emptyGIDSlot(err) {
						b.Fatal(err)
					}
				}
			}
		})
	}
	b.Run("metadata", func(b *testing.B) {
		b.Log("the inventory's whole per-port read: identity, link_layer and GIDs; expected: about the raw row")
		b.ReportAllocs()
		for b.Loop() {
			for i := range ports {
				p := model.RDMAPort{Device: ports[i].Device, Index: ports[i].Index, Port: ports[i].Port}
				if err := files.metadata(b.Context(), &p); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// BenchmarkRDMAHostState reads each host port's state as the RDMA state
// collector does every poll. Each op is one read of every port.
func BenchmarkRDMAHostState(b *testing.B) {
	files, ports := hostRDMAPorts(b)
	client, err := linuxio.NewClient(unix.NETLINK_RDMA)
	if err != nil {
		b.Fatal(err)
	}
	reader := &rdmaStateReader{client: client, files: files}
	b.Cleanup(func() { _ = reader.Close() })
	// The inventory records each port's netdev; take it from a first reply.
	for i := range ports {
		result, err := client.GetRDMAPort(b.Context(), ports[i].Index, ports[i].Port)
		if err != nil || len(result.Values) != 1 {
			b.Fatal(err)
		}
		v := result.Values[0]
		ports[i].Netdev, ports[i].NetdevName = model.Optional[uint32]{Value: v.Netdev, Present: v.HasNetdev}, v.NetdevName
	}
	for _, tc := range []struct {
		name, description, expected string
		read                        func(model.RDMAPort) error
	}{
		{"identity-each-read", "the sysfs identity before and after every GET_PORT, as before", "baseline", func(p model.RDMAPort) error {
			if _, err := reader.identity(p); err != nil {
				return err
			}
			_, err := reader.Read(b.Context(), p)
			if err == nil {
				_, err = filepath.EvalSymlinks(filepath.Join(files.root, p.Device, "device"))
			}
			return err
		}},
		{"netlink-matched", "GET_PORT alone, its reply matched to the port", "a fraction of the baseline: no sysfs access", func(p model.RDMAPort) error {
			_, err := reader.Read(b.Context(), p)
			return err
		}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s; expected: %s", tc.description, tc.expected)
			b.ReportAllocs()
			for b.Loop() {
				for i := range ports {
					if err := tc.read(ports[i]); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
