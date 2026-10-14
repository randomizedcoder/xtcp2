package linkmonitor

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

// BenchmarkRDMACounterRead polls a fixture port with every cataloged counter
// on tmpfs: open-read-close is the reader before descriptors were held, and
// the pool rows choose rdmaReadParallelism. On tmpfs the pool sizes should be
// about equal; the host rows below show the driver's cost.
func BenchmarkRDMACounterRead(b *testing.B) {
	for _, tc := range []struct {
		name, description, expected string
		limit, parallel             int
	}{
		{"open-read-close", "descriptors opened, read and closed every poll, under the identity guard", "baseline", 0, 1},
		{"held-pool-1", "held descriptors read in order", "fewer allocations and less time than the baseline", rdmaHandleLimit, 1},
		{"held-pool-2", "held descriptors, 2 readers", "about equal to held-pool-1 on tmpfs", rdmaHandleLimit, 2},
		{"held-pool-4", "held descriptors, 4 readers (the default)", "about equal to held-pool-1 on tmpfs", rdmaHandleLimit, 4},
		{"held-pool-8", "held descriptors, 8 readers", "about equal to held-pool-1 on tmpfs", rdmaHandleLimit, 8},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s; expected: %s", tc.description, tc.expected)
			c, job, path := optionalRDMAFixture(b)
			for name := range rdmaCounterCatalog {
				rdmaWrite(b, filepath.Join(path, name), strconv.Itoa(42))
			}
			c.handles.limit, c.parallel = int64(tc.limit), tc.parallel
			first := c.Collect(b.Context(), job)
			if first.Err != nil {
				b.Fatal(first.Err)
			}
			job.StatisticSchema = first.StatisticSchema
			b.ReportAllocs()
			for b.Loop() {
				if r := c.Collect(b.Context(), job); r.Err != nil {
					b.Fatal(r.Err)
				}
			}
			if err := c.Close(); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// hostRDMAPorts lists the host's RDMA ports with hardware counters, as the
// inventory reports them, or skips without any.
func hostRDMAPorts(b *testing.B) (rdmaFilesystem, []model.RDMAPort) {
	b.Helper()
	files := rdmaFilesystem{root: defaultRDMARoot, netRoot: defaultNetRoot}
	client, err := linuxio.NewClient(unix.NETLINK_RDMA)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	devices, err := client.DumpRDMADevices(b.Context())
	if err != nil || len(devices.Values) == 0 {
		b.Skipf("no RDMA devices: %v", err)
	}
	var ports []model.RDMAPort
	for _, d := range devices.Values {
		for port := uint32(1); port <= d.Port; port++ {
			p := model.RDMAPort{Device: d.Name, Index: d.Index, Port: port}
			if err := files.metadata(b.Context(), &p); err != nil || p.Hardware == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(defaultRDMARoot, d.Name, "ports", strconv.FormatUint(uint64(port), 10), "hw_counters")); err == nil {
				ports = append(ports, p)
			}
		}
	}
	if len(ports) == 0 {
		b.Skip("no RDMA port with hardware counters")
	}
	return files, ports
}

// hostHWCounterFiles lists a port's hw_counters counter files in name order.
func hostHWCounterFiles(b *testing.B, p model.RDMAPort) []string {
	b.Helper()
	dir := filepath.Join(defaultRDMARoot, p.Device, "ports", strconv.FormatUint(uint64(p.Port), 10), "hw_counters")
	entries, err := os.ReadDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.Name() != "lifespan" {
			names = append(names, filepath.Join(dir, e.Name()))
		}
	}
	return names
}

// BenchmarkRDMAHostCounters reads the host's hw_counters each way, 11 ms
// apart so the driver's 10 ms cache expires between rounds, as it does
// between polls. Each op is one round over every port.
func BenchmarkRDMAHostCounters(b *testing.B) {
	files, ports := hostRDMAPorts(b)
	sysfs := func(indexZeroFirst bool) func(*testing.B) {
		return func(b *testing.B) {
			paths := make([][]string, len(ports))
			for i := range ports {
				paths[i] = hostHWCounterFiles(b, ports[i])
				if first := slices.Index(paths[i], filepath.Join(filepath.Dir(paths[i][0]), "rx_write_requests")); indexZeroFirst && first > 0 {
					zero := paths[i][first]
					paths[i] = slices.Insert(slices.Delete(paths[i], first, first+1), 0, zero)
				}
			}
			for b.Loop() {
				for _, port := range paths {
					for _, path := range port {
						if _, err := rdmaScalar(b.Context(), path); err != nil {
							b.Fatal(err)
						}
					}
				}
				b.StopTimer()
				time.Sleep(11 * time.Millisecond)
				b.StartTimer()
			}
		}
	}
	collector := func(netlink bool) func(*testing.B) {
		return func(b *testing.B) {
			var stats rdmaStatSource
			if netlink {
				client, err := linuxio.NewClient(unix.NETLINK_RDMA)
				if err != nil {
					b.Fatal(err)
				}
				stats = client
			}
			c := newSharedRDMACollector(nil, files, newRDMAHandles(sysfsFiles{}).retain(), stats)
			jobs := make([]model.Job, len(ports))
			for i := range ports {
				jobs[i] = model.Job{Key: model.JobKey{Collector: model.CollectorRDMACounters}, Device: model.Device{Eligibility: model.Eligible},
					RDMA: &model.RDMARequest{Ports: []model.RDMAPort{ports[i]}}}
				r := c.Collect(b.Context(), jobs[i])
				if r.Err != nil {
					b.Fatal(r.Err)
				}
				jobs[i].StatisticSchema = r.StatisticSchema
			}
			for b.Loop() {
				for i := range jobs {
					if r := c.Collect(b.Context(), jobs[i]); r.Err != nil {
						b.Fatal(r.Err)
					}
				}
				b.StopTimer()
				time.Sleep(11 * time.Millisecond)
				b.StartTimer()
			}
			if err := c.Close(); err != nil {
				b.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name, description, expected string
		run                         func(*testing.B)
	}{
		{"sysfs-name-order", "hw_counters opened and read in name order, as before", "about 8 ms per mlx5 port", sysfs(false)},
		{"sysfs-index0-first", "hw_counters opened and read with rx_write_requests first", "about 0.33 ms per mlx5 port", sysfs(true)},
		{"netlink", "one STAT_GET per port", "about 0.3 ms per port", func(b *testing.B) {
			client, err := linuxio.NewClient(unix.NETLINK_RDMA)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			for b.Loop() {
				for i := range ports {
					if _, err := client.GetRDMAStat(b.Context(), ports[i].Index, ports[i].Port); err != nil && !errors.Is(err, unix.EINVAL) {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				time.Sleep(11 * time.Millisecond)
				b.StartTimer()
			}
		}},
		{"collector-sysfs", "the collector reading every file from held descriptors, index 0 first", "PMA files plus about 0.33 ms per mlx5 port", collector(false)},
		{"collector-netlink", "the collector with STAT_GET and held PMA descriptors", "the fastest full poll", collector(true)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s; expected: %s", tc.description, tc.expected)
			tc.run(b)
		})
	}
}
