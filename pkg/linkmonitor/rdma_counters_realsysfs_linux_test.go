package linkmonitor

import (
	"bufio"
	"errors"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// rdmaSysfsSnapshot is one file written by testdata/rdmasysfs/capture.sh or
// fleet-export.py: paths relative to /sys/class/infiniband/<device>, in
// capture order. Fleet files add "t1:path=value" lines for the counters that
// moved by the second sample, header "elapsed" seconds later.
type rdmaSysfsSnapshot struct {
	header map[string]string
	paths  []string
	values map[string]string
	later  map[string]string
}

// rdmaReadError is the fleet capture's value for a file whose read failed.
const rdmaReadError = "READ_ERROR"

func loadRDMASysfsSnapshot(t testing.TB, name string) rdmaSysfsSnapshot {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "rdmasysfs", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	s := rdmaSysfsSnapshot{header: map[string]string{}, values: map[string]string{}, later: map[string]string{}}
	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		if key, value, ok := strings.Cut(strings.TrimPrefix(text, "# "), ": "); ok && strings.HasPrefix(text, "# ") {
			s.header[key] = value
			continue
		}
		text, later := strings.CutPrefix(text, "t1:")
		path, value, ok := strings.Cut(text, "=")
		if later {
			if _, known := s.values[path]; !known || !ok {
				t.Fatalf("%s:%d: t1 line for unknown %q", name, line, path)
			}
			if _, dup := s.later[path]; dup {
				t.Fatalf("%s:%d: duplicate t1 %s", name, line, path)
			}
			s.later[path] = value
			continue
		}
		if !ok || path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			t.Fatalf("%s:%d: not a path=value line: %q", name, line, text)
		}
		if _, dup := s.values[path]; dup {
			t.Fatalf("%s:%d: duplicate %s", name, line, path)
		}
		s.paths = append(s.paths, path)
		s.values[path] = value
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return s
}

// counterFiles returns the snapshot's counter-group files as group/name.
func (s rdmaSysfsSnapshot) counterFiles() []string {
	var names []string
	for _, path := range s.paths {
		rest, ok := strings.CutPrefix(path, "ports/1/")
		if !ok {
			continue
		}
		if group, _, _ := strings.Cut(rest, "/"); group == "counters" || group == "counters_ext" || group == "hw_counters" {
			names = append(names, rest)
		}
	}
	return names
}

// matching returns the snapshot's counter files whose value satisfies keep.
func (s rdmaSysfsSnapshot) matching(keep func(string) bool) []string {
	var names []string
	for _, name := range s.counterFiles() {
		if keep(s.values["ports/1/"+name]) {
			names = append(names, name)
		}
	}
	return names
}

// install writes the snapshot over the rdmaFixture tree. Real sysfs
// attributes end in a newline, so each file does too.
func (s rdmaSysfsSnapshot) install(t testing.TB, port string) {
	t.Helper()
	device := filepath.Dir(filepath.Dir(port))
	for _, path := range s.paths {
		rdmaWrite(t, filepath.Join(device, path), s.values[path]+"\n")
	}
}

// snapshotRDMAFixture is optionalRDMAFixture discovered with the snapshot's
// own link layer, so a RoCE capture takes the RoCE association path and the
// collector's identity check sees the layer it was discovered with.
func snapshotRDMAFixture(t testing.TB, s rdmaSysfsSnapshot) (*rdmaOptionalCollector, model.Job, string) {
	t.Helper()
	layer, ok := s.values["ports/1/link_layer"]
	if !ok {
		t.Fatal("snapshot has no ports/1/link_layer")
	}
	i, _, path := rdmaFixture(t, layer)
	s.install(t, path)
	candidate, err := i.Dump(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.RDMAPorts) != 1 || candidate.RDMAPorts[0].Layer != layer {
		t.Fatalf("discovered %+v, want one %s port", candidate.RDMAPorts, layer)
	}
	p := candidate.RDMAPorts[0]
	job := model.Job{Key: model.JobKey{Namespace: 1, Device: p.Canonical, Collector: model.CollectorRDMACounters}, Token: model.Token{Generation: 1},
		Device: model.Device{Key: p.Canonical, Eligibility: model.Eligible, Up: presentValue(true)}, RDMA: &model.RDMARequest{Ports: []model.RDMAPort{p}}}
	return newRDMAOptionalCollector(nil, i.files), job, path
}

func TestRDMACountersRealSysfs(t *testing.T) {
	const hostL = "host-l-mlx5_0-7.1.8.txt"
	const ionic = "ionic-vm-ionic_0-7.1.4.txt"
	const rxe = "rxe-vm-lm_rxe-7.1.4.txt"
	// rxe's own names; its link_downed is under hw_counters, not counters.
	rxeUncataloged := []string{"hw_counters/ack_deferred", "hw_counters/completer_retry_err", "hw_counters/link_downed", "hw_counters/out_of_seq_request",
		"hw_counters/rcvd_pkts", "hw_counters/rcvd_rnr_err", "hw_counters/rcvd_seq_err", "hw_counters/rdma_recvs", "hw_counters/rdma_sends",
		"hw_counters/retry_exceeded_err", "hw_counters/retry_rnr_exceeded_err", "hw_counters/send_err", "hw_counters/send_rnr_err", "hw_counters/sent_pkts"}
	// rocm-ernic's STATS_HDRS names; ionic_rdma has no counters group.
	ionicUncataloged := []string{"hw_counters/commands", "hw_counters/total_bytes_rdma_read", "hw_counters/total_bytes_rdma_write", "hw_counters/total_bytes_received", "hw_counters/total_bytes_sent"}
	// mlx5 counters node_exporter does not map; the catalog drops them.
	mlx5Uncataloged := []string{"hw_counters/req_rnr_retries_exceeded", "hw_counters/req_transport_retries_exceeded"}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		snapshot                                     string
		// mutate edits the installed port directory before collection.
		mutate func(t *testing.T, port string)
		// info asks for the device metadata sample as well.
		info bool

		support     model.Support
		err         error
		uncataloged []string
		// rate is the expected rate gauge in bytes per second, 0 for none.
		rate uint64
		// values pins samples by catalog path; unlisted ones must equal
		// the snapshot value times the catalog scale.
		values map[string]uint64
	}{
		{
			name: "mlx5", category: "positive", description: "host l ConnectX-4 Lx RoCE port as captured",
			expectedOutcome: "every cataloged counters and hw_counters file is one sample at its scale, plus the 25 Gb/s rate",
			snapshot:        hostL, support: model.Supported, uncataloged: mlx5Uncataloged, rate: 3125000000,
		},
		{
			name: "mlx5-info", category: "positive", description: "host l port that owns the device metadata",
			expectedOutcome: "the same counters plus one info sample labeled with board_id, firmware_version and hca_type",
			snapshot:        hostL, info: true, support: model.Supported, uncataloged: mlx5Uncataloged, rate: 3125000000,
		},
		{
			name: "mlx5-traffic", category: "positive", description: "host l port after traffic: nonzero four-octet data registers",
			expectedOutcome: "data counters are reported in bytes, four times the register",
			snapshot:        hostL, support: model.Supported, uncataloged: mlx5Uncataloged, rate: 3125000000,
			mutate: func(t *testing.T, port string) {
				rdmaWrite(t, filepath.Join(port, "counters", "port_xmit_data"), "1000\n")
				rdmaWrite(t, filepath.Join(port, "hw_counters", "out_of_sequence"), "7\n")
			},
			values: map[string]uint64{"counters/port_xmit_data": 4000, "hw_counters/out_of_sequence": 7},
		},
		{
			name: "ionic", category: "positive", description: "real ionic_rdma port on rocm-ernic, hw_counters only",
			expectedOutcome: "lifespan is the one cataloged counter; the emulator's names are catch-all samples; rate 100 Gb/s",
			snapshot:        ionic, support: model.Supported, uncataloged: ionicUncataloged, rate: 12500000000,
		},
		{
			name: "ionic-info", category: "corner", description: "ionic has no board_id file",
			expectedOutcome: "the info sample still carries firmware_version and hca_type, with an empty board_id",
			snapshot:        ionic, info: true, support: model.Supported, uncataloged: ionicUncataloged, rate: 12500000000,
		},
		{
			name: "rxe", category: "positive", description: "soft-RoCE port, hw_counters only",
			expectedOutcome: "duplicate_request and lifespan are cataloged, the other rxe names are catch-all samples; rate 10 Gb/s",
			snapshot:        rxe, support: model.Supported, uncataloged: rxeUncataloged, rate: 1250000000,
		},
		{
			name: "no-groups", category: "negative", description: "host l port with counters and hw_counters removed",
			expectedOutcome: "unsupported, with no samples and no error",
			snapshot:        hostL, support: model.Unsupported,
			mutate: func(t *testing.T, port string) {
				for _, group := range []string{"counters", "hw_counters"} {
					if err := os.RemoveAll(filepath.Join(port, group)); err != nil {
						t.Fatal(err)
					}
				}
			},
		},
		{
			name: "not-a-number", category: "negative", description: "a real counter file reads N/A",
			expectedOutcome: "the whole set is rejected as malformed",
			snapshot:        hostL, support: model.SupportUnknown, err: linuxio.ErrReply,
			mutate: func(t *testing.T, port string) {
				rdmaWrite(t, filepath.Join(port, "hw_counters", "out_of_buffer"), "N/A\n")
			},
		},
		{
			name: "empty", category: "boundary", description: "a real counter file is empty",
			expectedOutcome: "malformed, not a fabricated zero",
			snapshot:        hostL, support: model.SupportUnknown, err: linuxio.ErrReply,
			mutate: func(t *testing.T, port string) {
				rdmaWrite(t, filepath.Join(port, "counters", "symbol_error"), "")
			},
		},
		{
			name: "max-u64", category: "boundary", description: "a unit-scale counter at the largest u64",
			expectedOutcome: "reported exactly",
			snapshot:        hostL, support: model.Supported, uncataloged: mlx5Uncataloged, rate: 3125000000,
			mutate: func(t *testing.T, port string) {
				rdmaWrite(t, filepath.Join(port, "hw_counters", "rx_write_requests"), "18446744073709551615\n")
			},
			values: map[string]uint64{"hw_counters/rx_write_requests": math.MaxUint64},
		},
		{
			name: "data-limit", category: "boundary", description: "the largest four-octet data register that fits in bytes",
			expectedOutcome: "exactly four times the register",
			snapshot:        hostL, support: model.Supported, uncataloged: mlx5Uncataloged, rate: 3125000000,
			mutate: func(t *testing.T, port string) {
				rdmaWrite(t, filepath.Join(port, "counters", "port_rcv_data"), strconv.FormatUint(math.MaxUint64/4, 10)+"\n")
			},
			values: map[string]uint64{"counters/port_rcv_data": math.MaxUint64 / 4 * 4},
		},
		{
			name: "data-overflow", category: "boundary", description: "one past the largest four-octet data register",
			expectedOutcome: "the whole set is rejected rather than wrapped",
			snapshot:        hostL, support: model.SupportUnknown, err: linuxio.ErrLimit,
			mutate: func(t *testing.T, port string) {
				rdmaWrite(t, filepath.Join(port, "counters", "port_rcv_data"), strconv.FormatUint(math.MaxUint64/4+1, 10)+"\n")
			},
		},
		{
			name: "only-uncataloged", category: "corner", description: "hw_counters holds only names the catalog does not know",
			expectedOutcome: "supported with the rate sample and one catch-all sample per file: the group exists, so this is not unsupported",
			snapshot:        hostL, support: model.Supported, uncataloged: mlx5Uncataloged, rate: 3125000000,
			mutate: func(t *testing.T, port string) {
				if err := os.RemoveAll(filepath.Join(port, "counters")); err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(filepath.Join(port, "hw_counters"))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if !slices.Contains(mlx5Uncataloged, "hw_counters/"+entry.Name()) {
						if err := os.Remove(filepath.Join(port, "hw_counters", entry.Name())); err != nil {
							t.Fatal(err)
						}
					}
				}
			},
		},
		{
			name: "lifespan", category: "corner", description: "the core's hw_counters lifespan file is the cache age in milliseconds",
			expectedOutcome: "an integer-seconds gauge, not a counter",
			snapshot:        hostL, support: model.Supported, uncataloged: mlx5Uncataloged, rate: 3125000000,
			mutate: func(t *testing.T, port string) {
				rdmaWrite(t, filepath.Join(port, "hw_counters", "lifespan"), "2500\n")
			},
			values: map[string]uint64{"hw_counters/lifespan": 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			snapshot := loadRDMASysfsSnapshot(t, tc.snapshot)
			c, job, port := snapshotRDMAFixture(t, snapshot)
			if tc.mutate != nil {
				tc.mutate(t, port)
			}
			if tc.info {
				job.RDMA.InfoDevices = []string{rdmaSelector(job.RDMA.Ports[0])}
			}
			r := c.Collect(t.Context(), job)
			if r.Support != tc.support || !errors.Is(r.Err, tc.err) {
				t.Fatalf("%s: support %v err %v", tc.expectedOutcome, r.Support, r.Err)
			}
			if tc.support != model.Supported {
				if len(r.Samples) != 0 {
					t.Fatalf("%s: %d samples published", tc.expectedOutcome, len(r.Samples))
				}
				return
			}
			checkRealSysfsSamples(t, tc.expectedOutcome, port, r.Samples, tc.uncataloged, nil, tc.rate, tc.values, tc.info)
		})
	}
}

// checkRealSysfsSamples derives the expected samples from the files left in
// the port directory, so each row only states what is special about it.
// Cataloged files named in unreadable must have no sample.
func checkRealSysfsSamples(t *testing.T, expected, port string, samples []model.Sample, uncataloged, unreadable []string, rate uint64, values map[string]uint64, info bool) {
	t.Helper()
	byMetric := map[string]model.Sample{}
	byCounter := map[string]model.Sample{}
	for _, s := range samples {
		if s.Descriptor == rdmaHWCounterDescriptor {
			counter := ""
			for _, l := range s.Labels {
				if l.Name == "counter" {
					counter = l.Value
				}
			}
			if _, dup := byCounter[counter]; dup {
				t.Fatalf("%s: duplicate catch-all counter %q", expected, counter)
			}
			byCounter[counter] = s
			continue
		}
		if _, dup := byMetric[s.Descriptor]; dup {
			t.Fatalf("%s: duplicate %s", expected, s.Descriptor)
		}
		byMetric[s.Descriptor] = s
	}
	want := 0
	var dropped []string
	for _, group := range []string{"counters", "counters_ext", "hw_counters"} {
		entries, err := os.ReadDir(filepath.Join(port, group))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := group + "/" + entry.Name()
			field, ok := rdmaCounterCatalog[name]
			if !ok {
				dropped = append(dropped, name)
				// Uncataloged hw_counters files are each one untyped
				// catch-all sample with the raw value; other groups drop them.
				s, published := byCounter[entry.Name()]
				if group != "hw_counters" || slices.Contains(unreadable, name) {
					if published && group == "hw_counters" {
						t.Fatalf("%s: unreadable %s published %+v", expected, name, s)
					}
					continue
				}
				want++
				text, err := os.ReadFile(filepath.Join(port, name))
				if err != nil {
					t.Fatal(err)
				}
				raw, err := strconv.ParseUint(strings.TrimSpace(string(text)), 10, 64)
				if err != nil {
					t.Fatalf("%s: %s is not decimal: %v", expected, name, err)
				}
				if got, _ := s.Number.Uint64(); !published || got != raw || s.Kind != model.SampleUntyped {
					t.Fatalf("%s: catch-all %s = %+v, want %d untyped", expected, name, s, raw)
				}
				continue
			}
			s, ok := byMetric["go_link_monitor_infiniband_"+field.metric]
			if slices.Contains(unreadable, name) {
				if ok {
					t.Fatalf("%s: unreadable %s published %+v", expected, name, s)
				}
				continue
			}
			want++
			if !ok {
				t.Fatalf("%s: no sample for %s", expected, name)
			}
			got, _ := s.Number.Uint64()
			if v, pinned := values[name]; pinned {
				if got != v {
					t.Fatalf("%s: %s = %d, want %d", expected, name, got, v)
				}
				continue
			}
			text, err := os.ReadFile(filepath.Join(port, name))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := strconv.ParseUint(strings.TrimSpace(string(text)), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			wantValue, wantKind := raw*field.scale, model.SampleCounter
			if field.scale == 0 {
				wantValue, wantKind = raw/1000, model.SampleGauge
			}
			if got != wantValue || s.Kind != wantKind {
				t.Fatalf("%s: %s = %d (%v), want %d (%v)", expected, name, got, s.Kind, wantValue, wantKind)
			}
		}
	}
	slices.Sort(dropped)
	if !slices.Equal(dropped, uncataloged) {
		t.Fatalf("%s: uncataloged files %v, want %v", expected, dropped, uncataloged)
	}
	if rate != 0 {
		want++
		s, ok := byMetric["go_link_monitor_infiniband_rate_bytes_per_second"]
		if got, _ := s.Number.Uint64(); !ok || got != rate {
			t.Fatalf("%s: rate %d, want %d", expected, got, rate)
		}
	}
	if info {
		want++
		s, ok := byMetric["go_link_monitor_infiniband_info"]
		if !ok {
			t.Fatalf("%s: no info sample in %v", expected, slices.Sorted(maps.Keys(byMetric)))
		}
		device := filepath.Dir(filepath.Dir(port))
		for _, field := range []struct{ file, label string }{{"board_id", "board_id"}, {"fw_ver", "firmware_version"}, {"hca_type", "hca_type"}} {
			text, err := os.ReadFile(filepath.Join(device, field.file))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if !slices.Contains(s.Labels, model.Label{Name: field.label, Value: strings.TrimSpace(string(text))}) {
				t.Fatalf("%s: info labels %v lack %s", expected, s.Labels, field.label)
			}
		}
	}
	if len(samples) != want {
		t.Fatalf("%s: %d samples, want %d", expected, len(samples), want)
	}
}
