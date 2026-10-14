package linkmonitor

import (
	"context"
	"errors"
	"maps"
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// fakeStatClient scripts STAT_GET: reply answers each call, numbered from 1.
type fakeStatClient struct {
	mu     sync.Mutex
	calls  int
	closed bool
	reply  func(call int, index, port uint32) (linuxio.RDMAStat, error)
}

func (f *fakeStatClient) GetRDMAStat(_ context.Context, index, port uint32) (linuxio.RDMAStat, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()
	return f.reply(call, index, port)
}

func (f *fakeStatClient) Close() error {
	f.closed = true
	return nil
}

func (f *fakeStatClient) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// statReply answers for the port asked with counters, keyed by file name.
func statReply(device string, counters map[string]uint64) func(int, uint32, uint32) (linuxio.RDMAStat, error) {
	return func(_ int, index, port uint32) (linuxio.RDMAStat, error) {
		s := linuxio.RDMAStat{Name: device, Index: index, Port: port}
		for _, name := range slices.Sorted(maps.Keys(counters)) {
			s.Counters = append(s.Counters, linuxio.RDMACounter{Name: name, Value: counters[name]})
		}
		return s, nil
	}
}

func statError(err error) func(int, uint32, uint32) (linuxio.RDMAStat, error) {
	return func(int, uint32, uint32) (linuxio.RDMAStat, error) { return linuxio.RDMAStat{}, err }
}

// rdmaHarness is optionalRDMAFixture with counter files written, held
// descriptors counted through fakeRDMAFiles, and STAT_GET scripted when stats
// is set. The port has netlink index 3.
type rdmaHarness struct {
	c     *rdmaOptionalCollector
	job   model.Job
	path  string
	files *fakeRDMAFiles
	stats *fakeStatClient
}

const rdmaHarnessIndex = 3

func newRDMAHarness(t testing.TB, files map[string]string, stats *fakeStatClient) *rdmaHarness {
	t.Helper()
	c, job, path := optionalRDMAFixture(t)
	for name, value := range files {
		rdmaWrite(t, filepath.Join(path, name), value+"\n")
	}
	h := &rdmaHarness{c: c, job: job, path: path, files: newFakeRDMAFiles(), stats: stats}
	useRDMAFiles(t, c, h.files)
	h.job.RDMA.Ports[0].Index = rdmaHarnessIndex
	if stats != nil {
		c.stats = stats
	}
	return h
}

// addPort adds port 2 of the same device with files of its own.
func (h *rdmaHarness) addPort(t testing.TB, files map[string]string) {
	t.Helper()
	port := filepath.Join(filepath.Dir(h.path), "2")
	rdmaWrite(t, filepath.Join(port, "link_layer"), h.job.RDMA.Ports[0].Layer)
	for name, value := range files {
		rdmaWrite(t, filepath.Join(port, name), value+"\n")
	}
	p := h.job.RDMA.Ports[0]
	p.Port = 2
	h.job.RDMA.Ports = append(h.job.RDMA.Ports, p)
}

// poll collects as the reducer does: a success keeps the returned schema
// for the next poll and a failure discards it.
func (h *rdmaHarness) poll(t testing.TB) model.Result {
	t.Helper()
	return h.pollContext(t.Context())
}

func (h *rdmaHarness) pollContext(ctx context.Context) model.Result {
	r := h.c.Collect(ctx, h.job)
	h.job.StatisticSchema = r.StatisticSchema
	return r
}

func mustPoll(t testing.TB, h *rdmaHarness) model.Result {
	t.Helper()
	r := h.poll(t)
	if r.Err != nil || r.Support != model.Supported {
		t.Fatalf("poll: support %v err %v", r.Support, r.Err)
	}
	return r
}

// rdmaSampleIs reports whether s is the sample of schema entry name.
func rdmaSampleIs(s model.Sample, name string) bool {
	if field, cataloged := rdmaCounterCatalog[name]; cataloged {
		return s.Descriptor == "go_link_monitor_infiniband_"+field.metric
	}
	return s.Descriptor == rdmaHWCounterDescriptor && slices.Contains(s.Labels, model.Label{Name: "counter", Value: path.Base(name)})
}

// rdmaSampleValue finds the sample of schema entry name.
func rdmaSampleValue(samples []model.Sample, name string) (uint64, bool) {
	for _, s := range samples {
		if rdmaSampleIs(s, name) {
			n, ok := s.Number.Uint64()
			return n, ok
		}
	}
	return 0, false
}

func wantRDMAValues(t testing.TB, expected string, samples []model.Sample, want map[string]uint64) {
	t.Helper()
	for name, value := range want {
		if got, ok := rdmaSampleValue(samples, name); !ok || got != value {
			t.Fatalf("%s: %s = %d (present %v), want %d", expected, name, got, ok, value)
		}
	}
}

// rdmaPMANames returns n cataloged PortCounters names in schema order.
func rdmaPMANames(t testing.TB, n int) []string {
	t.Helper()
	var names []string
	for name := range rdmaCounterCatalog {
		if rdmaPMACounter(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if len(names) < n {
		t.Fatalf("catalog has %d PortCounters names, need %d", len(names), n)
	}
	return names[:n]
}

// rdmaPMAFilesFixed is rdmaPMAFiles for a table built before t exists.
func rdmaPMAFilesFixed(n int) map[string]string {
	var names []string
	for name := range rdmaCounterCatalog {
		if rdmaPMACounter(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	files := map[string]string{}
	for i, name := range names[:n] {
		files[name] = strconv.Itoa(i + 1)
	}
	return files
}

func rdmaPMAFiles(t testing.TB, n int) map[string]string {
	t.Helper()
	files := map[string]string{}
	for i, name := range rdmaPMANames(t, n) {
		files[name] = strconv.Itoa(i + 1)
	}
	return files
}

func joinRDMAFiles(sets ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, set := range sets {
		maps.Copy(out, set)
	}
	return out
}

// rdmaHWFiles is a small mlx5-like hw_counters directory: sysfs values 100
// and 200, and the lifespan file.
var rdmaHWFiles = map[string]string{
	"hw_counters/rx_write_requests": "100",
	"hw_counters/out_of_sequence":   "200",
	"hw_counters/lifespan":          "10",
	"counters/link_downed":          "1",
}

func schemaHas(r model.Result, name string) bool {
	return r.StatisticSchema != nil && slices.Contains(r.StatisticSchema.Names, name)
}

func TestRDMAHWCountersNetlinkTable(t *testing.T) {
	netlinkValues := map[string]uint64{"rx_write_requests": 7, "out_of_sequence": 8}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		files                                        map[string]string
		reply                                        func(int, uint32, uint32) (linuxio.RDMAStat, error)
		check                                        func(t *testing.T, h *rdmaHarness, expected string)
	}{
		{"match", "positive", "reply names equal the schema's hw_counters names",
			"one sample per name with the netlink values; no hw_counters counter read from sysfs",
			rdmaHWFiles, statReply("mlx5_0", netlinkValues), func(t *testing.T, h *rdmaHarness, expected string) {
				r := mustPoll(t, h)
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/rx_write_requests": 7, "hw_counters/out_of_sequence": 8, "counters/link_downed": 1})
				if n := len(h.files.sequence("read", "hw_counters/")); n != 1 || h.stats.count() != 1 {
					t.Fatalf("%s: %d hw_counters reads (lifespan only expected), %d requests", expected, n, h.stats.count())
				}
			}},
		{"same-as-sysfs", "positive", "netlink and sysfs serve the same values",
			"samples equal field for field, including Source and labels",
			rdmaHWFiles, statReply("mlx5_0", map[string]uint64{"rx_write_requests": 100, "out_of_sequence": 200}), func(t *testing.T, h *rdmaHarness, expected string) {
				netlink := mustPoll(t, h)
				h.c.stats = nil
				sysfs := mustPoll(t, h)
				if !reflect.DeepEqual(netlink.Samples, sysfs.Samples) {
					t.Fatalf("%s:\nnetlink %+v\nsysfs   %+v", expected, netlink.Samples, sysfs.Samples)
				}
			}},
		{"lifespan", "positive", "schema has hw_counters/lifespan",
			"lifespan read from its held descriptor each poll; the counters from netlink",
			rdmaHWFiles, statReply("mlx5_0", netlinkValues), func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				h.files.reset()
				r := mustPoll(t, h)
				if got := h.files.sequence("read", "hw_counters/"); !slices.Equal(got, []string{"hw_counters/lifespan"}) {
					t.Fatalf("%s: hw_counters reads %v", expected, got)
				}
				if _, ok := rdmaSampleValue(r.Samples, "hw_counters/lifespan"); !ok || h.files.count(h.files.opens, "") != 0 {
					t.Fatalf("%s: lifespan sample %v, opens %v", expected, ok, h.files.opens)
				}
			}},
		{"one-request-per-port", "positive", "device with 2 ports",
			"exactly 2 STAT_GET requests per poll",
			rdmaHWFiles, statReply("mlx5_0", netlinkValues), func(t *testing.T, h *rdmaHarness, expected string) {
				h.addPort(t, map[string]string{"hw_counters/rx_write_requests": "1"})
				r := mustPoll(t, h)
				if h.stats.count() != 2 || len(r.Samples) == 0 {
					t.Fatalf("%s: %d requests", expected, h.stats.count())
				}
			}},
		{"extra-optional", "corner", "reply has an enabled optional counter sysfs does not list",
			"ignored: no sample and no error",
			rdmaHWFiles, statReply("mlx5_0", joinStat(netlinkValues, map[string]uint64{"cc_rx_ce_pkts": 9})), func(t *testing.T, h *rdmaHarness, expected string) {
				r := mustPoll(t, h)
				if _, ok := rdmaSampleValue(r.Samples, "hw_counters/cc_rx_ce_pkts"); ok {
					t.Fatal(expected)
				}
			}},
		{"missing-name", "negative", "a schema name is absent from the reply",
			"that entry pruned from the returned schema; the others emitted",
			rdmaHWFiles, statReply("mlx5_0", map[string]uint64{"rx_write_requests": 7}), func(t *testing.T, h *rdmaHarness, expected string) {
				r := mustPoll(t, h)
				if schemaHas(r, "hw_counters/out_of_sequence") || !schemaHas(r, "hw_counters/rx_write_requests") {
					t.Fatalf("%s: schema %v", expected, r.StatisticSchema.Names)
				}
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/rx_write_requests": 7})
			}},
		{"all-missing", "boundary", "empty reply for a non-empty schema",
			"every hw_counters counter pruned; lifespan and the counters group still emitted",
			rdmaHWFiles, statReply("mlx5_0", nil), func(t *testing.T, h *rdmaHarness, expected string) {
				r := mustPoll(t, h)
				if schemaHas(r, "hw_counters/out_of_sequence") || schemaHas(r, "hw_counters/rx_write_requests") || !schemaHas(r, "hw_counters/lifespan") {
					t.Fatalf("%s: schema %v", expected, r.StatisticSchema.Names)
				}
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"counters/link_downed": 1})
			}},
		{"no-hw-group", "boundary", "schema has no hw_counters",
			"no STAT_GET sent",
			map[string]string{"counters/link_downed": "1"}, statReply("mlx5_0", netlinkValues), func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				if h.stats.count() != 0 {
					t.Fatal(expected)
				}
			}},
		{"sysfs-discovered", "boundary", "port discovered through sysfs, without a netlink index",
			"no STAT_GET; the counters read from sysfs",
			rdmaHWFiles, statReply("mlx5_0", netlinkValues), func(t *testing.T, h *rdmaHarness, expected string) {
				h.job.RDMA.Ports[0].Index, h.job.RDMA.Ports[0].Sysfs = 0, true
				r := mustPoll(t, h)
				if h.stats.count() != 0 {
					t.Fatal(expected)
				}
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/rx_write_requests": 100})
			}},
		{"index-zero", "boundary", "netlink device index 0, the first device registered",
			"STAT_GET sent for index 0 and its values used",
			rdmaHWFiles, statReply("mlx5_0", netlinkValues), func(t *testing.T, h *rdmaHarness, expected string) {
				h.job.RDMA.Ports[0].Index = 0
				r := mustPoll(t, h)
				if h.stats.count() != 1 {
					t.Fatal(expected)
				}
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/rx_write_requests": 7})
			}},
		{"wrong-device-name", "negative", "reply names another device",
			"ErrEpoch, schema invalidated, no samples, nothing held",
			rdmaHWFiles, statReply("mlx5_9", netlinkValues), func(t *testing.T, h *rdmaHarness, expected string) {
				r := h.poll(t)
				if !errors.Is(r.Err, linuxio.ErrEpoch) || !r.InvalidateSchema || len(r.Samples) != 0 || h.files.held() != 0 {
					t.Fatalf("%s: %+v held %d", expected, r, h.files.held())
				}
			}},
		{"wrong-port", "negative", "reply is for port 2",
			"ErrEpoch, no samples",
			rdmaHWFiles, func(call int, index, _ uint32) (linuxio.RDMAStat, error) {
				return statReply("mlx5_0", netlinkValues)(call, index, 2)
			}, func(t *testing.T, h *rdmaHarness, expected string) {
				if r := h.poll(t); !errors.Is(r.Err, linuxio.ErrEpoch) || len(r.Samples) != 0 {
					t.Fatal(expected, r.Err)
				}
			}},
		{"enodev", "negative", "STAT_GET returns ENODEV",
			"set fails and the schema is invalidated; not sticky, the next poll sends STAT_GET again",
			rdmaHWFiles, statError(syscall.ENODEV), func(t *testing.T, h *rdmaHarness, expected string) {
				if r := h.poll(t); !errors.Is(r.Err, syscall.ENODEV) || !r.InvalidateSchema {
					t.Fatal(expected, r.Err)
				}
				h.poll(t)
				if h.stats.count() != 2 {
					t.Fatalf("%s: %d requests", expected, h.stats.count())
				}
			}},
		{"timeout", "negative", "STAT_GET times out with the poll's context still live",
			"this poll reads sysfs; not sticky, the next poll sends STAT_GET again",
			rdmaHWFiles, statError(syscall.ETIMEDOUT), func(t *testing.T, h *rdmaHarness, expected string) {
				r := mustPoll(t, h)
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/rx_write_requests": 100})
				mustPoll(t, h)
				if h.stats.count() != 2 {
					t.Fatalf("%s: %d requests", expected, h.stats.count())
				}
			}},
		{"counter-value-max", "boundary", "vendor counter at 2^64-1",
			"exported exactly",
			joinRDMAFiles(rdmaHWFiles, map[string]string{"hw_counters/vendor_x": "0"}), statReply("mlx5_0", joinStat(netlinkValues, map[string]uint64{"vendor_x": math.MaxUint64})), func(t *testing.T, h *rdmaHarness, expected string) {
				r := mustPoll(t, h)
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/vendor_x": math.MaxUint64})
			}},
		{"duplicate-name", "corner", "same counter name twice in a reply",
			"ErrReply, no samples",
			rdmaHWFiles, func(call int, index, port uint32) (linuxio.RDMAStat, error) {
				s, err := statReply("mlx5_0", netlinkValues)(call, index, port)
				s.Counters = append(s.Counters, s.Counters[0])
				return s, err
			}, func(t *testing.T, h *rdmaHarness, expected string) {
				if r := h.poll(t); !errors.Is(r.Err, linuxio.ErrReply) || len(r.Samples) != 0 {
					t.Fatal(expected, r.Err)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			h := newRDMAHarness(t, tc.files, &fakeStatClient{reply: tc.reply})
			tc.check(t, h, tc.expectedOutcome)
		})
	}
}

func joinStat(sets ...map[string]uint64) map[string]uint64 {
	out := map[string]uint64{}
	for _, set := range sets {
		maps.Copy(out, set)
	}
	return out
}

func TestRDMAHWCountersFallbackTable(t *testing.T) {
	ordered := map[string]string{
		"hw_counters/duplicate_request": "1", "hw_counters/lifespan": "10", "hw_counters/out_of_sequence": "2",
		"hw_counters/rnr_nak_retry_err": "3", "hw_counters/rx_write_requests": "4",
	}
	withoutIndexZero := maps.Clone(ordered)
	delete(withoutIndexZero, "hw_counters/rx_write_requests")
	sticky := func(err error) func(*testing.T, *rdmaHarness, string) {
		return func(t *testing.T, h *rdmaHarness, expected string) {
			for range 2 {
				r := mustPoll(t, h)
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/rx_write_requests": 100})
			}
			if h.stats.count() != 1 {
				t.Fatalf("%s: %d requests after %v", expected, h.stats.count(), err)
			}
		}
	}
	order := func(want ...string) func(*testing.T, *rdmaHarness, string) {
		return func(t *testing.T, h *rdmaHarness, expected string) {
			for range 2 {
				h.files.reset()
				mustPoll(t, h)
				if got := h.files.sequence("read", "hw_counters/"); !slices.Equal(got, want) {
					t.Fatalf("%s: reads %v, want %v", expected, got, want)
				}
			}
		}
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		files                                        map[string]string
		stats                                        *fakeStatClient
		check                                        func(t *testing.T, h *rdmaHarness, expected string)
	}{
		{"eopnotsupp-sticky", "positive", "STAT_GET returns EOPNOTSUPP; poll twice",
			"both polls read sysfs; the second sends no STAT_GET",
			rdmaHWFiles, &fakeStatClient{reply: statError(syscall.EOPNOTSUPP)}, sticky(syscall.EOPNOTSUPP)},
		{"eperm-sticky", "positive", "STAT_GET returns EPERM",
			"falls back to sysfs; sticky",
			rdmaHWFiles, &fakeStatClient{reply: statError(syscall.EPERM)}, sticky(syscall.EPERM)},
		{"socket-open-fails", "negative", "opening the netlink socket fails with EPROTONOSUPPORT",
			"falls back to sysfs; sticky",
			rdmaHWFiles, &fakeStatClient{reply: statError(syscall.EPROTONOSUPPORT)}, sticky(syscall.EPROTONOSUPPORT)},
		{"sticky-until-schema", "boundary", "fallback set, then the schema is invalidated",
			"the next poll sends STAT_GET again and takes its values",
			rdmaHWFiles, &fakeStatClient{reply: func(call int, index, port uint32) (linuxio.RDMAStat, error) {
				if call == 1 {
					return linuxio.RDMAStat{}, syscall.EOPNOTSUPP
				}
				return statReply("mlx5_0", map[string]uint64{"rx_write_requests": 7, "out_of_sequence": 8})(call, index, port)
			}}, func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				mustPoll(t, h)
				h.job.StatisticSchema = nil
				r := mustPoll(t, h)
				if h.stats.count() != 2 {
					t.Fatalf("%s: %d requests", expected, h.stats.count())
				}
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/rx_write_requests": 7})
			}},
		{"rx-write-first", "positive", "sysfs read with rx_write_requests in the schema",
			"rx_write_requests read first, the rest in name order, every poll",
			ordered, nil, order("hw_counters/rx_write_requests", "hw_counters/duplicate_request", "hw_counters/lifespan", "hw_counters/out_of_sequence", "hw_counters/rnr_nak_retry_err")},
		{"rx-write-absent", "boundary", "schema without rx_write_requests (rxe, ionic)",
			"pure name order",
			withoutIndexZero, nil, order("hw_counters/duplicate_request", "hw_counters/lifespan", "hw_counters/out_of_sequence", "hw_counters/rnr_nak_retry_err")},
		{"rx-write-only", "boundary", "schema with only rx_write_requests",
			"one read",
			map[string]string{"hw_counters/rx_write_requests": "4"}, nil, order("hw_counters/rx_write_requests")},
		{"order-not-in-output", "corner", "sysfs read in index-0-first order",
			"samples still in schema order, so publication is unchanged",
			ordered, nil, func(t *testing.T, h *rdmaHarness, expected string) {
				r := mustPoll(t, h)
				var got []string
				for _, s := range r.Samples {
					for _, name := range r.StatisticSchema.Names {
						if rdmaSampleIs(s, name) {
							got = append(got, name)
						}
					}
				}
				if !slices.Equal(got, r.StatisticSchema.Names) {
					t.Fatalf("%s: samples in order %v, schema %v", expected, got, r.StatisticSchema.Names)
				}
			}},
		{"fallback-beside-pool", "corner", "STAT_GET fails while the pool reads 8 counters files",
			"hw_counters opened and read from sysfs beside the pool; every value correct",
			joinRDMAFiles(rdmaHWFiles, rdmaPMAFilesFixed(8)), &fakeStatClient{reply: statError(syscall.EOPNOTSUPP)}, func(t *testing.T, h *rdmaHarness, expected string) {
				for range 2 {
					r := mustPoll(t, h)
					wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/rx_write_requests": 100, "hw_counters/out_of_sequence": 200})
					if got := len(h.files.sequence("read", "counters")); got < 8 {
						t.Fatalf("%s: %d counters reads", expected, got)
					}
				}
			}},
		{"fallback-read-error", "negative", "sysfs fallback and EIO on one file",
			"set fails as before; the failure invalidates the schema, so the next poll sends STAT_GET again",
			rdmaHWFiles, &fakeStatClient{reply: statError(syscall.EOPNOTSUPP)}, func(t *testing.T, h *rdmaHarness, expected string) {
				h.files.readErr["hw_counters/out_of_sequence"] = syscall.EIO
				if r := h.poll(t); !errors.Is(r.Err, syscall.EIO) || len(r.Samples) != 0 {
					t.Fatal(expected, r.Err)
				}
				delete(h.files.readErr, "hw_counters/out_of_sequence")
				mustPoll(t, h)
				if h.stats.count() != 2 {
					t.Fatalf("%s: %d requests", expected, h.stats.count())
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			h := newRDMAHarness(t, tc.files, tc.stats)
			if tc.stats == nil {
				h.stats = &fakeStatClient{}
			}
			tc.check(t, h, tc.expectedOutcome)
		})
	}
}

func TestRDMAHandleCacheTable(t *testing.T) {
	const n = 21
	pma := func(t *testing.T) map[string]string { return rdmaPMAFiles(t, n) }
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		files                                        func(t *testing.T) map[string]string
		check                                        func(t *testing.T, h *rdmaHarness, expected string)
	}{
		{"first-poll", "positive", "cold cache, port with 21 counters files",
			"21 opens and 21 reads",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				if h.files.count(h.files.opens, "counters") != n || h.files.count(h.files.reads, "counters") != n {
					t.Fatalf("%s: opens %v reads %v", expected, h.files.opens, h.files.reads)
				}
			}},
		{"steady", "positive", "second poll, same schema and generation",
			"0 opens, 21 reads, 0 closes",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				h.files.reset()
				mustPoll(t, h)
				if h.files.count(h.files.opens, "") != 0 || h.files.count(h.files.reads, "counters") != n || h.files.count(h.files.closes, "") != 0 {
					t.Fatalf("%s: opens %v reads %d closes %v", expected, h.files.opens, h.files.count(h.files.reads, ""), h.files.closes)
				}
			}},
		{"schema-same-names", "positive", "rediscovered schema, a new pointer with the same names",
			"descriptors kept by name: 0 opens, 0 closes",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				h.files.reset()
				h.job.StatisticSchema = nil
				mustPoll(t, h)
				if h.files.count(h.files.opens, "") != 0 || h.files.count(h.files.closes, "") != 0 {
					t.Fatalf("%s: opens %v closes %v", expected, h.files.opens, h.files.closes)
				}
			}},
		{"generation-change", "positive", "the port's inventory generation increases",
			"the older entry's 21 descriptors closed, 21 opened for the new one",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				h.files.reset()
				h.job.RDMA.Ports[0].Generation++
				mustPoll(t, h)
				if h.files.count(h.files.closes, "counters") != n || h.files.count(h.files.opens, "counters") != n || h.files.held() != n {
					t.Fatalf("%s: opens %v closes %v held %d", expected, h.files.opens, h.files.closes, h.files.held())
				}
			}},
		{"older-generation", "boundary", "a job for a generation older than one already seen",
			"read through a transient entry: 21 opens and 21 closes; the newer entry stays held",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				h.job.RDMA.Ports[0].Generation = 2
				mustPoll(t, h)
				h.files.reset()
				h.job.RDMA.Ports[0].Generation = 1
				mustPoll(t, h)
				if h.files.count(h.files.opens, "counters") != n || h.files.count(h.files.closes, "counters") != n || h.files.held() != n {
					t.Fatalf("%s: opens %v closes %v held %d", expected, h.files.opens, h.files.closes, h.files.held())
				}
			}},
		{"read-error-eio", "negative", "EIO on file 7 of a held entry",
			"set fails, all descriptors closed, the next poll reopens all 21",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				key := rdmaPMANames(t, n)[6]
				h.files.readErr[key] = syscall.EIO
				if r := h.poll(t); !errors.Is(r.Err, syscall.EIO) || h.files.held() != 0 {
					t.Fatal(expected, r.Err, h.files.held())
				}
				delete(h.files.readErr, key)
				h.files.reset()
				mustPoll(t, h)
				if h.files.count(h.files.opens, "counters") != n {
					t.Fatalf("%s: opens %v", expected, h.files.opens)
				}
			}},
		{"read-error-enodev", "negative", "ENODEV: the kernfs node was removed under a held descriptor",
			"set fails with the I/O reason, nothing held, no samples",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				h.files.readErr[rdmaPMANames(t, n)[0]] = syscall.ENODEV
				r := h.poll(t)
				if !errors.Is(r.Err, syscall.ENODEV) || r.Reason != rdmaError(syscall.ENODEV) || len(r.Samples) != 0 || h.files.held() != 0 {
					t.Fatal(expected, r.Err, r.Reason, h.files.held())
				}
			}},
		{"einval-prune", "negative", "EINVAL on one file",
			"that file pruned and its descriptor closed on the next poll; the other 20 stay held",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				key := rdmaPMANames(t, n)[4]
				h.files.readErr[key] = syscall.EINVAL
				if r := mustPoll(t, h); schemaHas(r, key) {
					t.Fatal(expected)
				}
				h.files.reset()
				mustPoll(t, h)
				if h.files.closes[key] != 1 || h.files.reads[key] != 0 || h.files.count(h.files.opens, "") != 0 || h.files.held() != n-1 {
					t.Fatalf("%s: closes %v reads %d opens %v held %d", expected, h.files.closes, h.files.reads[key], h.files.opens, h.files.held())
				}
			}},
		{"open-fails", "negative", "ENOENT opening one counters file",
			"set fails with nothing left open",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				h.files.openErr[rdmaPMANames(t, n)[9]] = syscall.ENOENT
				if r := h.poll(t); !errors.Is(r.Err, syscall.ENOENT) || h.files.held() != 0 {
					t.Fatal(expected, r.Err, h.files.held())
				}
			}},
		{"cap-exact", "boundary", "the limit equals the slots needed",
			"all 21 held",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				h.c.handles.limit = n + 1 // the counters and rate
				mustPoll(t, h)
				if h.files.held() != n {
					t.Fatalf("%s: held %d", expected, h.files.held())
				}
			}},
		{"cap-over", "boundary", "one slot more than the limit",
			"the port opens, reads and closes each poll; the samples are identical",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				h.c.handles.limit = n
				transient := mustPoll(t, h)
				if h.files.held() != 0 {
					t.Fatalf("%s: held %d", expected, h.files.held())
				}
				h.c.handles.limit = rdmaHandleLimit
				if held := mustPoll(t, h); !reflect.DeepEqual(held.Samples, transient.Samples) {
					t.Fatalf("%s:\nheld      %+v\ntransient %+v", expected, held.Samples, transient.Samples)
				}
			}},
		{"size-4096", "boundary", "a file of exactly 4,096 bytes",
			"read and parsed",
			func(*testing.T) map[string]string {
				return map[string]string{"hw_counters/vendor_big": strings.Repeat("0", rdmaScalarLimit-3) + "42"}
			}, func(t *testing.T, h *rdmaHarness, expected string) {
				r := mustPoll(t, h)
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/vendor_big": 42})
			}},
		{"size-4097", "boundary", "a file of 4,097 bytes",
			"ErrLimit, oversize reason",
			func(*testing.T) map[string]string {
				return map[string]string{"hw_counters/vendor_big": strings.Repeat("0", rdmaScalarLimit-2) + "42"}
			}, func(t *testing.T, h *rdmaHarness, expected string) {
				if r := h.poll(t); !errors.Is(r.Err, linuxio.ErrLimit) || r.Reason != model.ErrorOversize {
					t.Fatal(expected, r.Err, r.Reason)
				}
			}},
		{"empty-file", "boundary", "an uncataloged file of 0 bytes",
			"skipped like any unparsable vendor file",
			func(*testing.T) map[string]string {
				return map[string]string{"hw_counters/vendor_empty": "", "counters/link_downed": "1"}
			}, func(t *testing.T, h *rdmaHarness, expected string) {
				r := mustPoll(t, h)
				if _, ok := rdmaSampleValue(r.Samples, "hw_counters/vendor_empty"); ok {
					t.Fatal(expected)
				}
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"counters/link_downed": 1})
			}},
		{"pread-fresh", "corner", "file rewritten in place between polls",
			"the second poll sees the new value without reopening",
			func(*testing.T) map[string]string { return map[string]string{"counters/link_downed": "1"} },
			func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				rdmaWrite(t, filepath.Join(h.path, "counters", "link_downed"), "5\n")
				h.files.reset()
				r := mustPoll(t, h)
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"counters/link_downed": 5})
				if h.files.count(h.files.opens, "") != 0 {
					t.Fatal(expected)
				}
			}},
		{"same-device-two-ports", "corner", "ports 1 and 2 of one device, polled as separate jobs",
			"separate entries: failing port 1 closes only its descriptors",
			pma, func(t *testing.T, h *rdmaHarness, expected string) {
				h.addPort(t, map[string]string{"counters/link_downed": "1"})
				one, two := h.job, h.job
				one.RDMA = &model.RDMARequest{Ports: h.job.RDMA.Ports[:1]}
				two.RDMA = &model.RDMARequest{Ports: []model.RDMAPort{h.job.RDMA.Ports[1]}}
				for _, job := range []model.Job{one, two} {
					if r := h.c.Collect(t.Context(), job); r.Err != nil {
						t.Fatal(r.Err)
					}
				}
				h.files.readErr[rdmaPMANames(t, n)[0]] = syscall.EIO
				if r := h.c.Collect(t.Context(), one); r.Err == nil || h.files.held() != 1 {
					t.Fatalf("%s: err %v held %d", expected, r.Err, h.files.held())
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			tc.check(t, newRDMAHarness(t, tc.files(t), nil), tc.expectedOutcome)
		})
	}
}

// TestRDMAHandleSessionClose checks real descriptors against /proc/self/fd:
// closing the collector returns the process to its baseline.
func TestRDMAHandleSessionClose(t *testing.T) {
	t.Logf("corner: close the collector with 21 descriptors held; expected: open descriptors back to the baseline")
	count := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	c, job, path := optionalRDMAFixture(t)
	for name, value := range rdmaPMAFiles(t, 21) {
		rdmaWrite(t, filepath.Join(path, name), value)
	}
	before := count()
	if r := c.Collect(t.Context(), job); r.Err != nil {
		t.Fatal(r.Err)
	}
	if held := count() - before; held != 21 {
		t.Fatalf("%d descriptors held, want 21", held)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if after := count(); after != before {
		t.Fatalf("descriptors %d after close, %d before", after, before)
	}
}

func TestRDMAIdentityOnReopenTable(t *testing.T) {
	files := map[string]string{"counters/link_downed": "1", "counters/symbol_error": "2"}
	repoint := func(t *testing.T, h *rdmaHarness) {
		device := filepath.Join(h.c.files.root, "mlx5_0", "device")
		other := filepath.Join(filepath.Dir(h.job.RDMA.Ports[0].Hardware), "0000:02:00.0")
		if err := os.MkdirAll(other, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(device); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(other, device); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		check                                        func(t *testing.T, h *rdmaHarness, expected string)
	}{
		{"steady-no-guard", "positive", "steady poll after the device symlink is repointed",
			"accepted: a steady poll runs no identity check",
			func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				repoint(t, h)
				mustPoll(t, h)
			}},
		{"reopen-guarded", "positive", "symlink repointed, then a generation change reopens",
			"ErrEpoch from the guard around the reopen",
			func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				repoint(t, h)
				h.job.RDMA.Ports[0].Generation++
				if r := h.poll(t); !errors.Is(r.Err, linuxio.ErrEpoch) || h.files.held() != 0 {
					t.Fatal(expected, r.Err, h.files.held())
				}
			}},
		{"hardware-changed", "negative", "device symlink points elsewhere before the first open",
			"ErrEpoch, nothing held, no samples",
			func(t *testing.T, h *rdmaHarness, expected string) {
				repoint(t, h)
				if r := h.poll(t); !errors.Is(r.Err, linuxio.ErrEpoch) || h.files.held() != 0 || len(r.Samples) != 0 {
					t.Fatal(expected, r.Err)
				}
			}},
		{"layer-changed", "negative", "link_layer changes before a reopen",
			"ErrEpoch",
			func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				rdmaWrite(t, filepath.Join(h.path, "link_layer"), rdmaEthernetLayer)
				h.job.RDMA.Ports[0].Generation++
				if r := h.poll(t); !errors.Is(r.Err, linuxio.ErrEpoch) {
					t.Fatal(expected, r.Err)
				}
			}},
		{"inode-replaced", "negative", "port directory replaced while its files open",
			"ErrEpoch, nothing held",
			func(t *testing.T, h *rdmaHarness, expected string) {
				var once sync.Once
				h.files.onOpen = func(string) {
					once.Do(func() {
						if err := os.Rename(h.path, h.path+"-old"); err != nil {
							t.Error(err)
						}
						rdmaWrite(t, filepath.Join(h.path, "link_layer"), rdmaNativeLayer)
					})
				}
				if r := h.poll(t); !errors.Is(r.Err, linuxio.ErrEpoch) || h.files.held() != 0 {
					t.Fatal(expected, r.Err, h.files.held())
				}
			}},
		{"removed-while-held", "corner", "port directory removed while descriptors are held",
			"the read fails and closes them; the reopen reports ENOENT; no stale values",
			func(t *testing.T, h *rdmaHarness, expected string) {
				mustPoll(t, h)
				if err := os.RemoveAll(h.path); err != nil {
					t.Fatal(err)
				}
				for name := range files {
					h.files.readErr[name] = syscall.ENODEV
				}
				if r := h.poll(t); !errors.Is(r.Err, syscall.ENODEV) || len(r.Samples) != 0 || h.files.held() != 0 {
					t.Fatal(expected, r.Err)
				}
				if r := h.poll(t); !errors.Is(r.Err, os.ErrNotExist) || len(r.Samples) != 0 {
					t.Fatal(expected, r.Err)
				}
			}},
		{"cancel", "corner", "context canceled during a reopen",
			"context error, nothing held",
			func(t *testing.T, h *rdmaHarness, expected string) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				h.files.onOpen = func(string) { cancel() }
				if r := h.pollContext(ctx); !errors.Is(r.Err, context.Canceled) || h.files.held() != 0 {
					t.Fatal(expected, r.Err, h.files.held())
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			tc.check(t, newRDMAHarness(t, files, nil), tc.expectedOutcome)
		})
	}
}

// rdmaConcurrency records the most reads in flight. With wait set, a read
// waits up to a second for a second reader, so a pool is observed even on a
// fast filesystem; inline reads time out and stay at 1.
type rdmaConcurrency struct {
	mu             sync.Mutex
	inflight, most int
	arrived        chan struct{}
}

func (r *rdmaConcurrency) hook(wait time.Duration) func(string) {
	r.arrived = make(chan struct{})
	var once sync.Once
	return func(string) {
		r.mu.Lock()
		r.inflight++
		r.most = max(r.most, r.inflight)
		if r.inflight >= 2 {
			once.Do(func() { close(r.arrived) })
		}
		r.mu.Unlock()
		select {
		case <-r.arrived:
		case <-time.After(wait):
		}
		r.mu.Lock()
		r.inflight--
		r.mu.Unlock()
	}
}

func TestRDMAParallelReadsTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		files                                        int
		check                                        func(t *testing.T, h *rdmaHarness, expected string)
	}{
		{"twenty-one", "positive", "21 counters files, 4 readers",
			"reads overlap; samples equal the sequential path, in order",
			21, func(t *testing.T, h *rdmaHarness, expected string) {
				var seen rdmaConcurrency
				h.files.onRead = seen.hook(time.Second)
				pool := mustPoll(t, h)
				h.files.onRead = nil
				h.c.parallel = 1
				inline := mustPoll(t, h)
				if seen.most < 2 || !reflect.DeepEqual(pool.Samples, inline.Samples) {
					t.Fatalf("%s: %d in flight", expected, seen.most)
				}
			}},
		{"zero", "boundary", "no counters files, hw_counters only",
			"no pool; the hw_counters samples emitted",
			0, func(t *testing.T, h *rdmaHarness, expected string) {
				rdmaWrite(t, filepath.Join(h.path, "hw_counters", "out_of_sequence"), "3\n")
				var seen rdmaConcurrency
				h.files.onRead = seen.hook(time.Millisecond)
				r := mustPoll(t, h)
				if seen.most != 1 {
					t.Fatalf("%s: %d in flight", expected, seen.most)
				}
				wantRDMAValues(t, expected, r.Samples, map[string]uint64{"hw_counters/out_of_sequence": 3})
			}},
		{"one", "boundary", "1 counters file", "read inline", 1, inlineReads},
		{"three", "boundary", "3 counters files, below the pool threshold of 4", "read inline, in name order", 3, inlineReads},
		{"four", "boundary", "4 counters files", "pool used; same samples as inline",
			4, func(t *testing.T, h *rdmaHarness, expected string) {
				var seen rdmaConcurrency
				h.files.onRead = seen.hook(time.Second)
				mustPoll(t, h)
				if seen.most < 2 {
					t.Fatalf("%s: %d in flight", expected, seen.most)
				}
			}},
		{"one-error", "negative", "EIO on file 10",
			"set fails with that file's error, as the sequential path did",
			21, func(t *testing.T, h *rdmaHarness, expected string) {
				name := rdmaPMANames(t, 21)[9]
				h.files.readErr[name] = syscall.EIO
				if r := h.poll(t); !errors.Is(r.Err, syscall.EIO) || !strings.Contains(r.Err.Error(), name) {
					t.Fatal(expected, r.Err)
				}
			}},
		{"two-errors", "negative", "EIO on files 3 and 17",
			"the error of file 3, the lowest schema index",
			21, func(t *testing.T, h *rdmaHarness, expected string) {
				names := rdmaPMANames(t, 21)
				h.files.readErr[names[2]], h.files.readErr[names[16]] = syscall.EIO, syscall.EIO
				if r := h.poll(t); r.Err == nil || !strings.Contains(r.Err.Error(), names[2]) || strings.Contains(r.Err.Error(), names[16]) {
					t.Fatal(expected, r.Err)
				}
			}},
		{"prune-middle", "corner", "EINVAL on file 11",
			"that entry pruned; the other 20 emitted in schema order",
			21, func(t *testing.T, h *rdmaHarness, expected string) {
				names := rdmaPMANames(t, 21)
				h.files.readErr[names[10]] = syscall.EINVAL
				r := mustPoll(t, h)
				want := slices.Delete(slices.Clone(names), 10, 11)
				if !slices.Equal(r.StatisticSchema.Names, want) || len(r.Samples) != 20 {
					t.Fatalf("%s: schema %v, %d samples", expected, r.StatisticSchema.Names, len(r.Samples))
				}
			}},
		{"slow-file", "corner", "one read blocks until the poll's deadline",
			"timeout reason; every reader exits",
			21, func(t *testing.T, h *rdmaHarness, expected string) {
				before := runtime.NumGoroutine()
				ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer cancel()
				slow := rdmaPMANames(t, 21)[4]
				h.files.onRead = func(key string) {
					if key == slow {
						<-ctx.Done()
					}
				}
				if r := h.pollContext(ctx); r.Reason != model.ErrorTimeout {
					t.Fatal(expected, r.Err, r.Reason)
				}
				for deadline := time.Now().Add(time.Second); runtime.NumGoroutine() > before && time.Now().Before(deadline); {
					time.Sleep(time.Millisecond)
				}
				if after := runtime.NumGoroutine(); after > before {
					t.Fatalf("%s: %d goroutines, %d before", expected, after, before)
				}
			}},
		{"hw-sequential", "corner", "sysfs hw_counters beside 8 counters files on the pool",
			"hw_counters read in order on one reader, rx_write_requests first",
			8, func(t *testing.T, h *rdmaHarness, expected string) {
				for _, name := range []string{"duplicate_request", "out_of_sequence", "rx_write_requests"} {
					rdmaWrite(t, filepath.Join(h.path, "hw_counters", name), "1\n")
				}
				mustPoll(t, h)
				want := []string{"hw_counters/rx_write_requests", "hw_counters/duplicate_request", "hw_counters/out_of_sequence"}
				if got := h.files.sequence("read", "hw_counters/"); !slices.Equal(got, want) {
					t.Fatalf("%s: %v", expected, got)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			tc.check(t, newRDMAHarness(t, rdmaPMAFiles(t, tc.files), nil), tc.expectedOutcome)
		})
	}
}

func inlineReads(t *testing.T, h *rdmaHarness, expected string) {
	t.Helper()
	var seen rdmaConcurrency
	h.files.onRead = seen.hook(time.Millisecond)
	mustPoll(t, h)
	want := slices.Clone(h.job.StatisticSchema.Names)
	if got := h.files.sequence("read", "counters"); seen.most != 1 || !slices.Equal(got, want) {
		t.Fatalf("%s: %d in flight, reads %v", expected, seen.most, got)
	}
}

// TestRDMACollectorsSharedCacheRace runs four collectors on one handle cache,
// as the scheduler's workers do, with jobs for both ports of a device,
// schema invalidation during reads, and STAT_GET alternating between a
// reply and a timeout that falls back to sysfs. Run it under -race.
func TestRDMACollectorsSharedCacheRace(t *testing.T) {
	t.Logf("corner: 4 collectors share descriptors across two ports with schema churn; expected: no race, no leaked descriptor")
	hw := map[string]string{"hw_counters/rx_write_requests": "1", "hw_counters/out_of_sequence": "2"}
	h := newRDMAHarness(t, joinRDMAFiles(rdmaPMAFiles(t, 8), hw), nil)
	h.addPort(t, joinRDMAFiles(rdmaPMAFiles(t, 6), hw))
	reply := statReply("mlx5_0", map[string]uint64{"rx_write_requests": 1, "out_of_sequence": 2})
	shared := newRDMAHandles(h.files)
	jobs := make([]model.Job, 2)
	for i := range jobs {
		jobs[i] = h.job
		jobs[i].RDMA = &model.RDMARequest{Ports: []model.RDMAPort{h.job.RDMA.Ports[i]}}
	}
	var wg sync.WaitGroup
	collectors := make([]*rdmaOptionalCollector, collectorWorkers)
	for w := range collectors {
		collectors[w] = newSharedRDMACollector(nil, h.c.files, shared.retain(), &fakeStatClient{reply: func(call int, index, port uint32) (linuxio.RDMAStat, error) {
			if call%2 == 0 {
				return linuxio.RDMAStat{}, syscall.ETIMEDOUT
			}
			return reply(call, index, port)
		}})
		wg.Go(func() {
			local := slices.Clone(jobs)
			for i := range 50 {
				job := &local[i%len(local)]
				if w == 0 && i%10 == 0 {
					job.StatisticSchema = nil
				}
				r := collectors[w].Collect(t.Context(), *job)
				if r.Err != nil {
					t.Error(r.Err)
					return
				}
				job.StatisticSchema = r.StatisticSchema
			}
		})
	}
	wg.Wait()
	for _, c := range collectors {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if n := h.files.held(); n != 0 {
		t.Fatalf("%d descriptors held after every collector closed", n)
	}
}

// rdmaShape is one driver's counter directory for the steady-state rows.
type rdmaShape struct {
	files map[string]string
	stat  map[string]uint64
}

func rdmaShapeOf(t testing.TB, pma int, hw []string) rdmaShape {
	t.Helper()
	s := rdmaShape{files: rdmaPMAFiles(t, pma), stat: map[string]uint64{}}
	s.files["hw_counters/lifespan"] = "10"
	for i, name := range hw {
		s.files["hw_counters/"+name] = strconv.Itoa(i)
		s.stat[name] = uint64(i)
	}
	return s
}

// mlx5HWCounters is the mlx5 RoCE hw_counters directory without lifespan:
// 30 counters, rx_write_requests (index 0) last by name.
var mlx5HWCounters = []string{"duplicate_request", "implied_nak_seq_err", "local_ack_timeout_err", "np_cnp_sent",
	"np_ecn_marked_roce_packets", "out_of_buffer", "out_of_sequence", "packet_seq_err", "req_cqe_error",
	"req_cqe_flush_error", "req_remote_access_errors", "req_remote_invalid_request", "req_rnr_retries_exceeded",
	"req_transport_retries_exceeded", "resp_cqe_error", "resp_cqe_flush_error", "resp_local_length_error",
	"resp_remote_access_errors", "rnr_nak_retry_err", "roce_adp_retrans", "roce_adp_retrans_to",
	"roce_slow_restart", "roce_slow_restart_cnps", "roce_slow_restart_trans", "rp_cnp_handled", "rp_cnp_ignored",
	"rx_atomic_requests", "rx_icrc_encapsulated", "rx_read_requests", "rx_write_requests"}

// TestRDMASteadyStateCostTable pins a steady poll's work per driver shape:
// no opens and no identity checks, one read per sysfs file still needed,
// and one STAT_GET per port, or the fallback reads with index 0 first.
func TestRDMASteadyStateCostTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		shape                                        rdmaShape
		sysfs                                        bool
		// reads is the sysfs reads per steady poll; requests the STAT_GETs.
		reads, requests int
		// allocs bounds a steady poll's allocations, measured on real files.
		allocs float64
	}{
		{"mlx5-roce", "positive", "21 PMA files and 31 hw_counters, STAT_GET available",
			"21 PMA reads plus lifespan, one STAT_GET, no opens", rdmaShapeOf(t, 21, mlx5HWCounters), false, 22, 1, 140},
		{"rxe", "positive", "hw_counters only",
			"lifespan read, one STAT_GET", rdmaShapeOf(t, 0, []string{"duplicate_request", "out_of_sequence", "rcvd_seq_err", "send_retry_exceeded_err"}), false, 1, 1, 25},
		{"ionic", "positive", "hw_counters with ionic names",
			"lifespan read, one STAT_GET", rdmaShapeOf(t, 0, []string{"req_rx_cqe_err", "req_rx_dup_response", "resp_rx_dup_request", "tx_rdma_ucast_pkts"}), false, 1, 1, 20},
		{"sysfs-discovered", "boundary", "mlx5 RoCE port found through sysfs, without a netlink index",
			"every file read, rx_write_requests first among hw_counters, no STAT_GET", rdmaShapeOf(t, 21, mlx5HWCounters), true, 52, 0, 125},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			stats := &fakeStatClient{reply: statReply("mlx5_0", tc.shape.stat)}
			h := newRDMAHarness(t, tc.shape.files, stats)
			h.job.RDMA.Ports[0].Sysfs = tc.sysfs
			mustPoll(t, h)
			repoint := filepath.Join(h.c.files.root, "mlx5_0", "device")
			if err := os.Remove(repoint); err != nil { // a steady poll must not check identity
				t.Fatal(err)
			}
			h.files.reset()
			before := stats.count()
			mustPoll(t, h)
			if opens, reads := h.files.count(h.files.opens, ""), h.files.count(h.files.reads, ""); opens != 0 || reads != tc.reads || stats.count()-before != tc.requests {
				t.Fatalf("%s: opens %d reads %d requests %d", tc.expectedOutcome, opens, reads, stats.count()-before)
			}
			if tc.sysfs {
				if hw := h.files.sequence("read", "hw_counters/"); hw[0] != "hw_counters/rx_write_requests" {
					t.Fatalf("%s: first hw_counters read %s", tc.expectedOutcome, hw[0])
				}
			}

			c, job, path := optionalRDMAFixture(t)
			for name, value := range tc.shape.files {
				rdmaWrite(t, filepath.Join(path, name), value+"\n")
			}
			job.RDMA.Ports[0].Index, job.RDMA.Ports[0].Sysfs = rdmaHarnessIndex, tc.sysfs
			c.stats = stats
			job.StatisticSchema = c.Collect(t.Context(), job).StatisticSchema
			allocs := testing.AllocsPerRun(20, func() {
				if r := c.Collect(t.Context(), job); r.Err != nil {
					t.Fatal(r.Err)
				}
			})
			t.Logf("%s: %.0f allocations per steady poll, bound %.0f", tc.name, allocs, tc.allocs)
			if allocs > tc.allocs {
				t.Fatalf("%s: %.0f allocations per poll, bound %.0f", tc.expectedOutcome, allocs, tc.allocs)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
