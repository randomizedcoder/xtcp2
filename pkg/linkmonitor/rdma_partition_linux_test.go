package linkmonitor

import (
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func rdmaTestSchema(names ...string) *model.StatisticSchema {
	s := &model.StatisticSchema{}
	for _, name := range names {
		s.Names = append(s.Names, name)
		s.Selected = append(s.Selected, 0)
		s.Labels = append(s.Labels, []model.Label{{Name: "device", Value: "mlx5_0"}, {Name: "port", Value: "1"}})
	}
	return s
}

func TestRDMAWatchPartition(t *testing.T) {
	mixed := []string{"counters/link_downed", "counters/port_rcv_data", "counters/port_rcv_errors", "counters/symbol_error",
		"hw_counters/local_ack_timeout_err", "hw_counters/out_of_sequence", "hw_counters/rx_write_requests", "hw_counters/vendor_x"}
	job := func(kind model.CollectorKind) model.Job {
		return model.Job{Key: model.JobKey{Collector: kind}, Device: model.Device{Name: "ib0", Key: model.DeviceKey{Kind: model.DeviceEthernet}}}
	}
	for _, tc := range []struct {
		name, category, description, expected string
		mutate                                func(*Config)
		names                                 []string
		watch                                 []string
	}{
		{"mlx5-roce", "positive", "PMA, flap and RoCE hw counters", "error and flap counters watched; data, uncataloged and unclassified counters rest",
			nil, mixed, []string{"counters/link_downed", "counters/port_rcv_errors", "counters/symbol_error", "hw_counters/local_ack_timeout_err"}},
		{"rdma-class-off", "negative", "error classes link only", "only the flap counter watched", func(c *Config) { c.ErrorClasses = []string{"link"} }, mixed, []string{"counters/link_downed"}},
		{"flap-off", "negative", "flap window 0", "link_downed rests", func(c *Config) { c.FlapWindow = 0 }, mixed, []string{"counters/port_rcv_errors", "counters/symbol_error", "hw_counters/local_ack_timeout_err"}},
		{"excluded", "negative", "symbol_error on ib0 excluded", "symbol_error rests", func(c *Config) { c.ErrorExclude = `go_link_monitor_infiniband_symbol_error_total@ib0` }, mixed,
			[]string{"counters/link_downed", "counters/port_rcv_errors", "hw_counters/local_ack_timeout_err"}},
		{"excluded-other", "negative", "symbol_error excluded on another interface", "still watched", func(c *Config) { c.ErrorExclude = `.*@ib1` }, mixed,
			[]string{"counters/link_downed", "counters/port_rcv_errors", "counters/symbol_error", "hw_counters/local_ack_timeout_err"}},
		{"empty-schema", "boundary", "no counters", "both partitions empty", nil, nil, nil},
		{"all-error", "boundary", "only error files", "all watched, rest empty", nil, []string{"counters/port_rcv_errors", "counters/symbol_error"}, []string{"counters/port_rcv_errors", "counters/symbol_error"}},
		{"uncataloged-hw", "corner", "an unknown hw_counters name", "rests: never evidence", nil, []string{"hw_counters/vendor_err"}, nil},
		{"out-of-sequence", "corner", "out_of_sequence, which the classifier does not count", "rests: a classification gap, documented", nil, []string{"hw_counters/out_of_sequence"}, nil},
		{"lifespan", "corner", "the hw_counters cache setting", "rests with the metadata", nil, []string{"hw_counters/lifespan"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			cfg := DefaultConfig()
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			policy, err := newProblemPolicy(cfg)
			if err != nil {
				t.Fatal(err)
			}
			schema := rdmaTestSchema(tc.names...)
			watch := rdmaPartition(schema, job(model.CollectorRDMAErrors), &policy)
			rest := rdmaPartition(schema, job(model.CollectorRDMACounters), &policy)
			if !slices.Equal(watch.Names, tc.watch) {
				t.Fatalf("watch %v, want %v", watch.Names, tc.watch)
			}
			// Disjoint, covering, and each keeping schema order and labels.
			merged := append(slices.Clone(watch.Names), rest.Names...)
			slices.Sort(merged)
			if !slices.Equal(merged, slices.Sorted(slices.Values(tc.names))) || len(watch.Names)+len(rest.Names) != len(tc.names) {
				t.Fatalf("watch %v and rest %v do not partition %v", watch.Names, rest.Names, tc.names)
			}
			for _, part := range []*model.StatisticSchema{watch, rest} {
				if !slices.IsSortedFunc(part.Names, func(a, b string) int { return slices.Index(tc.names, a) - slices.Index(tc.names, b) }) ||
					len(part.Labels) != len(part.Names) || len(part.Selected) != len(part.Names) {
					t.Fatalf("partition %v lost order or labels", part.Names)
				}
			}
		})
	}
}

// TestRDMAWatchPartitionFleet splits every fleet host's counters between the
// on-demand jobs and checks the two publish exactly what one poll-mode job does.
func TestRDMAWatchPartitionFleet(t *testing.T) {
	policy, err := newProblemPolicy(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	key := func(s model.Sample) string {
		labels := make([]string, 0, len(s.Labels))
		for _, l := range s.Labels {
			labels = append(labels, l.Name+"="+l.Value)
		}
		v, _ := s.Number.Uint64()
		return s.Descriptor + "{" + strings.Join(labels, ",") + "}" + s.Counter.Source + "=" + strconv.FormatUint(v, 10)
	}
	for _, tc := range rdmaFleetFixtures {
		t.Run(strings.TrimSuffix(tc.file, ".txt"), func(t *testing.T) {
			t.Logf("positive: %s split between rdma_errors and rdma_counters; expected: the same samples as one poll-mode job, none twice", tc.description)
			snapshot := loadRDMASysfsSnapshot(t, filepath.Join(rdmaFleetDir, tc.file))
			c, job, _ := snapshotRDMAFixture(t, snapshot)
			installFleetReads(t, c, syscall.EINVAL)
			whole := c.Collect(t.Context(), job)
			c.split = &policy
			var split []model.Sample
			watched := 0
			for _, kind := range [...]model.CollectorKind{model.CollectorRDMAErrors, model.CollectorRDMACounters} {
				job.Key.Collector, job.StatisticSchema = kind, nil
				r := c.Collect(t.Context(), job)
				if r.Err != nil || r.Support != model.Supported {
					t.Fatalf("%v: support %v err %v", kind, r.Support, r.Err)
				}
				if kind == model.CollectorRDMAErrors {
					watched = len(r.Samples)
					for _, s := range r.Samples {
						if !policy.enabled(classifyEvidence(s.Descriptor, s.Labels)) {
							t.Fatalf("watched %s is not evidence", s.Descriptor)
						}
					}
				}
				split = append(split, r.Samples...)
			}
			want, got := make([]string, 0, len(whole.Samples)), make([]string, 0, len(split))
			for _, s := range whole.Samples {
				want = append(want, key(s))
			}
			for _, s := range split {
				got = append(got, key(s))
			}
			slices.Sort(want)
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Fatalf("split publishes %d samples, one job %d", len(got), len(want))
			}
			if watched == 0 && slices.ContainsFunc(whole.Samples, func(s model.Sample) bool { return policy.enabled(classifyEvidence(s.Descriptor, s.Labels)) }) {
				t.Fatal("error counters present but none watched")
			}
		})
	}
}

func TestRDMAPartitionHandles(t *testing.T) {
	pma := map[string]string{"counters/symbol_error": "1", "counters/port_rcv_errors": "2", "counters/port_rcv_data": "3", "counters/port_xmit_data": "4"}
	hw := map[string]uint64{"local_ack_timeout_err": 5, "rx_write_requests": 6}
	hwFiles := map[string]string{"hw_counters/local_ack_timeout_err": "5", "hw_counters/rx_write_requests": "6"}
	for _, tc := range []struct {
		name, category, description, expected string
		files                                 map[string]string
		stat                                  bool
		classes                               []string
		failWatch                             error // a pread error on the watch job's symbol_error
		statErr                               error
		check                                 func(t *testing.T, h *rdmaHarness, watch, rest *model.Job)
	}{
		{"alternate", "positive", "watch, rest, watch, rest polls", "each opens once; no reopen as they alternate", pma, false, nil, nil, nil, func(t *testing.T, h *rdmaHarness, watch, rest *model.Job) {
			pollJob(t, h, watch)
			pollJob(t, h, rest)
			opens := h.files.count(h.files.opens, "counters/")
			for range 2 {
				pollJob(t, h, watch)
				pollJob(t, h, rest)
			}
			if got := h.files.count(h.files.opens, "counters/"); got != opens {
				t.Fatalf("%d counter opens after alternating, want %d", got, opens)
			}
		}},
		{"one-stat-each", "positive", "both jobs with hw counters", "one STAT_GET each; each keeps its own names", joinRDMAFiles(pma, hwFiles), true, nil, nil, nil, func(t *testing.T, h *rdmaHarness, watch, rest *model.Job) {
			before := h.stats.count()
			w, r := pollJob(t, h, watch), pollJob(t, h, rest)
			if h.stats.count()-before != 2 {
				t.Fatalf("%d STAT_GETs, want 2", h.stats.count()-before)
			}
			if _, ok := rdmaSampleValue(w.Samples, "hw_counters/local_ack_timeout_err"); !ok {
				t.Fatal("watch lacks local_ack_timeout_err")
			}
			if _, ok := rdmaSampleValue(r.Samples, "hw_counters/rx_write_requests"); !ok {
				t.Fatal("rest lacks rx_write_requests")
			}
		}},
		{"metadata-rest", "positive", "rate and info series", "published by the rest job only", joinRDMAFiles(pma, map[string]string{"rate": "100 Gb/sec (4X EDR)"}), false, nil, nil, nil, func(t *testing.T, h *rdmaHarness, watch, rest *model.Job) {
			w, r := pollJob(t, h, watch), pollJob(t, h, rest)
			isRate := func(s model.Sample) bool { return s.Descriptor == "go_link_monitor_infiniband_rate_bytes_per_second" }
			if slices.ContainsFunc(w.Samples, isRate) || !slices.ContainsFunc(r.Samples, isRate) {
				t.Fatal("rate not on the rest job alone")
			}
		}},
		{"rest-no-hw", "boundary", "every hw counter is watched", "the rest job sends no STAT_GET", joinRDMAFiles(pma, map[string]string{"hw_counters/local_ack_timeout_err": "5"}), true, nil, nil, nil, func(t *testing.T, h *rdmaHarness, watch, rest *model.Job) {
			pollJob(t, h, watch)
			before := h.stats.count()
			pollJob(t, h, rest)
			if h.stats.count() != before {
				t.Fatal("rest job sent STAT_GET with no hw counters")
			}
		}},
		{"watch-empty", "boundary", "every error class off and no flap window", "the watch job reads nothing and publishes nothing", pma, false, []string{}, nil, nil, func(t *testing.T, h *rdmaHarness, watch, _ *model.Job) {
			reads := h.files.count(h.files.reads, "")
			if w := pollJob(t, h, watch); len(w.Samples) != 0 || h.files.count(h.files.reads, "") != reads {
				t.Fatalf("empty watch published %d samples", len(w.Samples))
			}
		}},
		{"watch-read-error", "negative", "EIO on a watch file", "the watch set fails and closes its descriptors; the rest stays held", pma, false, nil, syscall.EIO, nil, func(t *testing.T, h *rdmaHarness, watch, rest *model.Job) {
			pollJob(t, h, rest)
			held := h.files.held()
			h.files.readErr["counters/symbol_error"] = syscall.EIO
			if r := h.c.Collect(t.Context(), *watch); !errors.Is(r.Err, syscall.EIO) {
				t.Fatalf("watch error %v, want EIO", r.Err)
			}
			opens := h.files.count(h.files.opens, "")
			pollJob(t, h, rest)
			if h.files.count(h.files.opens, "") != opens || h.files.held() > held {
				t.Fatal("the rest job reopened after the watch job failed")
			}
		}},
		{"stat-enodev", "negative", "STAT_GET ENODEV in the watch job", "the watch set fails with the schema invalidated", joinRDMAFiles(pma, hwFiles), true, nil, nil, syscall.ENODEV, func(t *testing.T, h *rdmaHarness, watch, _ *model.Job) {
			h.stats.reply = statError(syscall.ENODEV)
			if r := h.c.Collect(t.Context(), *watch); !errors.Is(r.Err, syscall.ENODEV) || !r.InvalidateSchema {
				t.Fatalf("watch %v invalidate %v", r.Err, r.InvalidateSchema)
			}
		}},
		{"parallel-race", "corner", "both jobs at once on one port, as two workers run them", "no race; each publishes its own samples", pma, false, nil, nil, nil, func(t *testing.T, h *rdmaHarness, watch, rest *model.Job) {
			other := newSharedRDMACollector(nil, h.c.files, h.c.handles.retain(), nil)
			other.split = h.c.split
			t.Cleanup(func() { _ = other.Close() })
			done := make(chan model.Result, 1)
			go func() { done <- other.Collect(t.Context(), *rest) }()
			w := h.c.Collect(t.Context(), *watch)
			r := <-done
			if w.Err != nil || r.Err != nil || len(w.Samples) == 0 || len(r.Samples) == 0 {
				t.Fatalf("watch %v %d, rest %v %d", w.Err, len(w.Samples), r.Err, len(r.Samples))
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			var stats *fakeStatClient
			if tc.stat {
				stats = &fakeStatClient{reply: statReply("mlx5_0", hw)}
			}
			h := newRDMAHarness(t, tc.files, stats)
			cfg := DefaultConfig()
			if tc.classes != nil {
				cfg.ErrorClasses, cfg.FlapWindow = tc.classes, 0
			}
			policy, err := newProblemPolicy(cfg)
			if err != nil {
				t.Fatal(err)
			}
			h.c.split = &policy
			watch, rest := h.job, h.job
			watch.Key.Collector, rest.Key.Collector = model.CollectorRDMAErrors, model.CollectorRDMACounters
			tc.check(t, h, &watch, &rest)
		})
	}
}

// pollJob collects job and keeps its schema for the next poll, as the reducer
// does per collector kind.
func pollJob(t *testing.T, h *rdmaHarness, job *model.Job) model.Result {
	t.Helper()
	r := h.c.Collect(t.Context(), *job)
	if r.Err != nil || r.Support != model.Supported {
		t.Fatalf("%v poll: support %v err %v", job.Key.Collector, r.Support, r.Err)
	}
	job.StatisticSchema = r.StatisticSchema
	return r
}
