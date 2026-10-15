package linkmonitor_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// interfaceRegistry gathers one interface of the current snapshot through
// collectors, as the HTTP interface view does per request.
func interfaceRegistry(t testing.TB, m *linkmonitor.Monitor, name string, collectors ...client.Collector) (*client.Registry, error) {
	t.Helper()
	c, err := monitorprom.InterfaceCollector(m.Snapshot(), name, collectors...)
	if err != nil {
		return nil, err
	}
	r := client.NewPedanticRegistry()
	if err := r.Register(c); err != nil {
		t.Fatal(err)
	}
	return r, nil
}

func categoryCollector(t testing.TB, m *linkmonitor.Monitor, category string) client.Collector {
	t.Helper()
	match, err := monitorprom.CategoryMatcher(category)
	if err != nil {
		t.Fatal(err)
	}
	return monitorprom.NewCollector(m, monitorprom.WithMetricFilter(match))
}

// allCollectors is the all-path pair: everything plus the sparse problems.
func allCollectors(m *linkmonitor.Monitor, at time.Time) []client.Collector {
	return []client.Collector{monitorprom.NewCollector(m), monitorprom.NewSummaryCollector(m, monitorprom.WithoutScalars(), fixedClock(at))}
}

func familyOf(line string) string { return line[:strings.IndexByte(line, '{')] }

func fleetFixture(t testing.TB) *linkmonitor.PrometheusFixture {
	f := linkmonitor.NewPrometheusFixture(t)
	f.PerformanceRealisticFleet()
	f.Publish(4)
	return f
}

// nativeFixture has two native InfiniBand ports with a counter on port 1.
func nativeFixture(t testing.TB) *linkmonitor.PrometheusFixture {
	f := linkmonitor.NewPrometheusFixture(t)
	f.RDMA(true)
	if err := f.NativeSamples(1, model.CollectorRDMACounters, ibCounter("symbol_error_total", 7)); err != nil {
		t.Fatal(err)
	}
	f.Publish(2)
	return f
}

func TestInterfaceCollector(t *testing.T) {
	type check struct {
		contains, absent []string
		// only, when set, is the single interface label value allowed.
		only string
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		fixture                                      func(testing.TB) *linkmonitor.PrometheusFixture
		collectors                                   func(*linkmonitor.PrometheusFixture) []client.Collector
		iface                                        string
		wantErr                                      error
		want                                         check
	}{
		{"link-one-interface", "positive", "link category, eth2 of three ports", "only eth2 link series and shared families",
			func(t testing.TB) *linkmonitor.PrometheusFixture { return healthyFixture(t, 3) },
			func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return []client.Collector{categoryCollector(t, f.Monitor, monitorprom.CategoryLink)}
			}, "eth2", nil,
			check{contains: []string{"go_link_monitor_interface_check{check=max_speed,interface=eth2,status=pass} 1", "go_link_monitor_interface_up{interface=eth2} 1",
				"go_link_monitor_collection_healthy{} 0"}, absent: []string{"go_link_monitor_collector_support"}, only: "eth2"}},
		{"all-one-interface", "positive", "all form with a CRC problem on eth2", "every category's eth2 series plus its problem",
			func(t testing.TB) *linkmonitor.PrometheusFixture {
				f := healthyFixture(t, 3)
				crc(f, 2, 1)
				crc(f, 2, 2)
				f.Publish(3)
				return f
			},
			func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return allCollectors(f.Monitor, f.ScrapeTime(0))
			}, "eth2", nil,
			check{contains: []string{"go_link_monitor_interface_problem{interface=eth2,reason=errors} 1", "go_link_monitor_interface_receive_crc_errors_total{interface=eth2} 2",
				"go_link_monitor_collector_support{collector=standard,interface=eth2,status=supported} 1", "go_link_monitor_interface_up{interface=eth2} 1"},
				absent: []string{"go_link_monitor_problems{", "go_link_monitor_up_links{"}, only: "eth2"}},
		{"healthy-no-problem", "negative", "all form on a healthy port", "no interface_problem series",
			func(t testing.TB) *linkmonitor.PrometheusFixture { return healthyFixture(t, 3) },
			func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return allCollectors(f.Monitor, f.ScrapeTime(0))
			}, "eth1", nil,
			check{absent: []string{"go_link_monitor_interface_problem{"}, only: "eth1"}},
		{"roce-under-ethernet", "positive", "rdma category for RoCE port eth3", "its RDMA series, not eth4's",
			fleetFixture, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return []client.Collector{categoryCollector(t, f.Monitor, monitorprom.CategoryRDMA)}
			}, "eth3", nil, check{contains: []string{"go_link_monitor_rdma_port_up{device=mlx5_0,interface=eth3,port=1} 1"}, absent: []string{"mlx5_1"}, only: "eth3"}},
		{"native-ib", "positive", "rdma category for native rdma:mlx5_0:1", "that port's state and counters, attributed without an interface label",
			nativeFixture, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return []client.Collector{categoryCollector(t, f.Monitor, monitorprom.CategoryRDMA)}
			}, "rdma:mlx5_0:1", nil, check{contains: []string{"go_link_monitor_rdma_port_up{device=mlx5_0,interface=rdma:mlx5_0:1,port=1} 1",
				"go_link_monitor_infiniband_symbol_error_total{device=mlx5_0,port=1} 7"}, absent: []string{"port=2"}, only: "rdma:mlx5_0:1"}},
		{"native-ib-other-port", "negative", "port 2 of the same HCA", "port 1 counters absent",
			nativeFixture, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return []client.Collector{categoryCollector(t, f.Monitor, monitorprom.CategoryRDMA)}
			}, "rdma:mlx5_0:2", nil, check{absent: []string{"infiniband_symbol_error_total"}, only: "rdma:mlx5_0:2"}},
		{"collection-interface", "positive", "collection category for eth1", "only eth1 collector diagnostics",
			func(t testing.TB) *linkmonitor.PrometheusFixture { return healthyFixture(t, 3) },
			func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return []client.Collector{categoryCollector(t, f.Monitor, monitorprom.CategoryCollection)}
			}, "eth1", nil, check{contains: []string{"go_link_monitor_collector_support{collector=settings,interface=eth1,status=supported} 1"},
				absent: []string{"resyncs_total", "baseline_write_errors_total", "interface_check"}, only: "eth1"}},
		{"driver-interface", "positive", "driver category for one deployed port", "only that port's driver statistics",
			func(t testing.TB) *linkmonitor.PrometheusFixture {
				f := linkmonitor.NewPrometheusFixture(t)
				f.PerformanceDeployedHost()
				f.Publish(3)
				return f
			}, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return []client.Collector{categoryCollector(t, f.Monitor, monitorprom.CategoryDriver)}
			}, "enp1s0", nil, check{contains: []string{"go_link_monitor_ethtool_statistic{encoding=utf8,interface=enp1s0,statistic=queue_7} 7"}, absent: []string{"netstat_"}, only: "enp1s0"}},
		{"host-scope-absent", "corner", "link category on eth1 with an exception", "no up_links, baseline or exception match series",
			func(t testing.TB) *linkmonitor.PrometheusFixture {
				f := healthyFixture(t, 3)
				f.Exceptions("eth1")
				f.Publish(3)
				return f
			}, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return []client.Collector{categoryCollector(t, f.Monitor, monitorprom.CategoryLink)}
			}, "eth1", nil, check{contains: []string{"go_link_monitor_interface_max_speed_exception{interface=eth1} 1"},
				absent: []string{"go_link_monitor_up_links{", "baseline_up_links", "max_speed_exception_match"}, only: "eth1"}},
		{"excluded-interface", "corner", "link category on excluded docker0", "classification series only",
			fleetFixture, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return []client.Collector{categoryCollector(t, f.Monitor, monitorprom.CategoryLink)}
			}, "docker0", nil, check{contains: []string{"go_link_monitor_interface_classification{interface=docker0,status=excluded} 1"},
				absent: []string{"interface_up", "interface_check"}, only: "docker0"}},
		{"excluded-no-problem", "corner", "all form on excluded docker0", "never an interface_problem",
			fleetFixture, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return allCollectors(f.Monitor, f.ScrapeTime(0))
			},
			"docker0", nil, check{absent: []string{"interface_problem"}, only: "docker0"}},
		{"unknown-interface", "negative", "eth9 on a three-port host", "ErrUnknownInterface",
			func(t testing.TB) *linkmonitor.PrometheusFixture { return healthyFixture(t, 3) },
			func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return allCollectors(f.Monitor, f.ScrapeTime(0))
			},
			"eth9", monitorprom.ErrUnknownInterface, check{}},
		{"empty-name", "boundary", "empty interface name", "ErrUnknownInterface",
			func(t testing.TB) *linkmonitor.PrometheusFixture { return healthyFixture(t, 3) },
			func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return allCollectors(f.Monitor, f.ScrapeTime(0))
			},
			"", monitorprom.ErrUnknownInterface, check{}},
		{"case-sensitive", "corner", "ETH1 for eth1", "ErrUnknownInterface: names are exact",
			func(t testing.TB) *linkmonitor.PrometheusFixture { return healthyFixture(t, 3) },
			func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return allCollectors(f.Monitor, f.ScrapeTime(0))
			},
			"ETH1", monitorprom.ErrUnknownInterface, check{}},
		{"renamed", "corner", "eth1 renamed to wan0 and published", "old name unknown",
			func(t testing.TB) *linkmonitor.PrometheusFixture {
				f := healthyFixture(t, 3)
				f.Device(1, "wan0", true)
				f.Publish(3)
				return f
			}, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return allCollectors(f.Monitor, f.ScrapeTime(0))
			},
			"eth1", monitorprom.ErrUnknownInterface, check{}},
		{"renamed-new", "corner", "eth1 renamed to wan0 and published", "new name serves the same device",
			func(t testing.TB) *linkmonitor.PrometheusFixture {
				f := healthyFixture(t, 3)
				f.Device(1, "wan0", true)
				f.Publish(3)
				return f
			}, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return allCollectors(f.Monitor, f.ScrapeTime(0))
			},
			"wan0", nil, check{contains: []string{"go_link_monitor_interface_up{interface=wan0} 1"}, only: "wan0"}},
		{"removed", "corner", "eth2 removed and published", "ErrUnknownInterface",
			func(t testing.TB) *linkmonitor.PrometheusFixture {
				f := healthyFixture(t, 3)
				f.Remove(2)
				f.Publish(2)
				return f
			}, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return allCollectors(f.Monitor, f.ScrapeTime(0))
			},
			"eth2", monitorprom.ErrUnknownInterface, check{}},
		{"category-named-interface", "corner", "an interface named link", "served as the interface",
			func(t testing.TB) *linkmonitor.PrometheusFixture {
				f := linkmonitor.NewPrometheusFixture(t)
				f.Device(1, "link", true)
				f.Publish(1)
				return f
			}, func(f *linkmonitor.PrometheusFixture) []client.Collector {
				return []client.Collector{categoryCollector(t, f.Monitor, monitorprom.CategoryLink)}
			}, "link", nil, check{contains: []string{"go_link_monitor_interface_up{interface=link} 1"}, only: "link"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := tc.fixture(t)
			r, err := interfaceRegistry(t, f.Monitor, tc.iface, tc.collectors(f)...)
			if tc.wantErr != nil || err != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			lines := series(t, r)
			got := strings.Join(lines, "\n")
			for _, want := range tc.want.contains {
				if !slices.Contains(lines, want) {
					t.Errorf("missing %s in\n%s", want, got)
				}
			}
			for _, unwanted := range tc.want.absent {
				if strings.Contains(got, unwanted) {
					t.Errorf("unexpected %s in\n%s", unwanted, got)
				}
			}
			for _, line := range lines {
				if strings.Contains(line, "interface=") && !strings.Contains(line, "interface="+tc.want.only+",") && !strings.Contains(line, "interface="+tc.want.only+"}") {
					t.Errorf("foreign interface: %s", line)
				}
			}
		})
	}
}

func TestInterfaceCollectorUnsupported(t *testing.T) {
	t.Log("negative: a collector without an interface view; expected: construction error, not a silent omission")
	f := healthyFixture(t, 1)
	gauge := client.NewGauge(client.GaugeOpts{Name: "other", Help: "Other."})
	if _, err := monitorprom.InterfaceCollector(f.Monitor.Snapshot(), "eth1", gauge); err == nil || errors.Is(err, monitorprom.ErrUnknownInterface) {
		t.Fatalf("err = %v, want an unsupported-collector error", err)
	}
	if _, err := monitorprom.InterfaceCollector(linkmonitor.Snapshot{}, "eth1"); !errors.Is(err, monitorprom.ErrUnknownInterface) {
		t.Fatalf("empty snapshot err = %v, want ErrUnknownInterface", err)
	}
}

// TestInterfacePartition requires the interface views to partition each
// path: every view line is on the path, no line is in two views, and every
// path line in no view is host scope.
func TestInterfacePartition(t *testing.T) {
	for _, fixture := range []struct {
		name, category, description string
		make                        func(testing.TB) *linkmonitor.PrometheusFixture
	}{
		{"fleet", "positive", "Ethernet, RoCE and excluded devices with a CRC problem", func(t testing.TB) *linkmonitor.PrometheusFixture {
			f := fleetFixture(t)
			crc(f, 1, 1)
			crc(f, 1, 2)
			f.Publish(4)
			return f
		}},
		{"native", "corner", "native InfiniBand ports with unlabelled counters", nativeFixture},
		{"deployed", "boundary", "deployed host with host-scope netstat", func(t testing.TB) *linkmonitor.PrometheusFixture {
			f := linkmonitor.NewPrometheusFixture(t)
			f.PerformanceDeployedHost()
			f.Publish(3)
			return f
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Logf("%s: %s", fixture.category, fixture.description)
			testInterfacePartition(t, fixture.make(t))
		})
	}
}

func testInterfacePartition(t *testing.T, f *linkmonitor.PrometheusFixture) {
	var names []string
	f.Monitor.Snapshot().RangeDevices(func(d linkmonitor.DeviceView) bool { names = append(names, d.Name()); return true })
	at := f.ScrapeTime(0)
	paths := map[string]func() []client.Collector{"all": func() []client.Collector { return allCollectors(f.Monitor, at) }}
	for _, category := range monitorprom.Categories() {
		if category != monitorprom.CategoryNetstat {
			paths[category] = func() []client.Collector { return []client.Collector{categoryCollector(t, f.Monitor, category)} }
		}
	}
	for path, collectors := range paths {
		t.Run(path, func(t *testing.T) {
			t.Logf("corner: union of %s interface views over %d interfaces; expected: equals the path minus host-scope series", path, len(names))
			full := client.NewPedanticRegistry()
			full.MustRegister(collectors()...)
			all := series(t, full)
			owner := make(map[string]string)
			parts := collectors()
			for _, name := range names {
				r, err := interfaceRegistry(t, f.Monitor, name, parts...)
				if err != nil {
					t.Fatal(name, err)
				}
				for _, line := range series(t, r) {
					if monitorprom.CategoryOf(familyOf(line)) == monitorprom.CategoryShared {
						continue
					}
					if !slices.Contains(all, line) {
						t.Errorf("%s: %s not on the path", name, line)
					}
					if previous, ok := owner[line]; ok {
						t.Errorf("%s: %s also in %s", name, line, previous)
					}
					owner[line] = name
				}
			}
			for _, line := range all {
				// Host diagnostics carry an empty interface label.
				hostScope := !strings.Contains(line, "interface=") || strings.Contains(line, "interface=,") || strings.Contains(line, "interface=}")
				if _, ok := owner[line]; !ok && !hostScope {
					t.Errorf("interface series in no view: %s", line)
				}
			}
		})
	}
}

func TestInterfaceAllocationGuards(t *testing.T) {
	collect := func(m *linkmonitor.Monitor, name string) float64 {
		parts := allCollectors(m, time.Unix(10, 500000000))
		out := make(chan client.Metric, 4096)
		drainOnce := func() {
			c, err := monitorprom.InterfaceCollector(m.Snapshot(), name, parts...)
			if err != nil {
				t.Fatal(err)
			}
			c.Collect(out)
			for len(out) > 0 {
				<-out
			}
		}
		drainOnce() // build the catalogs outside measurement
		return testing.AllocsPerRun(50, drainOnce)
	}
	small := collect(speedFixture(t, 3, 0), "eth2")
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		ports                                        int
		iface                                        string
	}{
		{"port-independent-64", "positive", "eth2 of 64 ports", "same allocations as eth2 of 3", 64, "eth2"},
		{"port-independent-1024", "positive", "eth2 of 1,024 ports", "same allocations as eth2 of 3", 1024, "eth2"},
		{"last-port-1024", "boundary", "eth1024, last page of 1,024 ports", "same allocations as eth2 of 3", 1024, "eth1024"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			if got := collect(speedFixture(t, tc.ports, 0), tc.iface); got != small {
				t.Fatalf("allocations = %v, eth2 of 3 ports = %v", got, small)
			}
		})
	}
}

func TestInterfaceConcurrentRename(t *testing.T) {
	t.Log("race: interface views during renames, inserts and removals; expected: no race and every response names only its interface")
	f := healthyFixture(t, 4)
	parts := allCollectors(f.Monitor, f.ScrapeTime(0))
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for reader := range 8 {
		name := fmt.Sprintf("eth%d", reader%4+1)
		if reader >= 4 {
			name = fmt.Sprintf("wan%d", reader%4+1)
		}
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				r, err := interfaceRegistry(t, f.Monitor, name, parts...)
				if errors.Is(err, monitorprom.ErrUnknownInterface) {
					continue
				}
				if err != nil {
					t.Error(err)
					return
				}
				for _, line := range series(t, r) {
					if strings.Contains(line, "interface=") && !strings.Contains(line, "interface="+name+",") && !strings.Contains(line, "interface="+name+"}") {
						t.Errorf("%s view served %s", name, line)
						return
					}
				}
			}
		})
	}
	for i := range 200 {
		port := uint32(i%4 + 1)
		name := fmt.Sprintf("eth%d", port)
		if i%8 >= 4 {
			name = fmt.Sprintf("wan%d", port)
		}
		f.Device(port, name, true)
		if i%5 == 0 {
			f.Remove(5)
		} else {
			f.Device(5, "eth5", true)
		}
		_ = f.Samples(port, model.CollectorDriver, linkmonitor.PerformanceSamples(i%7+1)...)
		f.Publish(4)
	}
	close(stop)
	wg.Wait()
}
