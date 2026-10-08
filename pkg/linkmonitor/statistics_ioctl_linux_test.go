package linkmonitor

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

type statisticFixture struct {
	names  []string
	values []uint64
	calls  map[uint32]int
	mutate func(uint32, []byte) error
}

func (f *statisticFixture) invoke(_ string, data []byte) error {
	command := binary.NativeEndian.Uint32(data)
	f.calls[command]++
	switch command {
	case unix.ETHTOOL_GDRVINFO:
		binary.NativeEndian.PutUint32(data[180:], uint32(len(f.names)))
	case unix.ETHTOOL_GSSET_INFO:
		binary.NativeEndian.PutUint32(data[16:], uint32(len(f.names)))
	case unix.ETHTOOL_GSTRINGS:
		copy(data[12:], statisticNames(f.names...))
	case unix.ETHTOOL_GSTATS, unix.ETHTOOL_GPHYSTATS:
		for i, value := range f.values {
			binary.NativeEndian.PutUint64(data[8+i*8:], value)
		}
	default:
		return unix.EOPNOTSUPP
	}
	if f.mutate != nil {
		return f.mutate(command, data)
	}
	return nil
}

func statisticTestSource(t testing.TB, f *statisticFixture) *statisticsIoctl {
	t.Helper()
	f.calls = make(map[uint32]int)
	c := &statisticsIoctl{ioctl: &ethtoolIoctl{fd: -1, invoke: f.invoke, validate: func(ctx context.Context, _ string, _ uint32) error { return ctx.Err() }},
		include: regexp.MustCompile(".*"), exclude: regexp.MustCompile("^$")}
	t.Cleanup(func() {
		if err := c.buffer.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}

func statisticTestJob(kind model.CollectorKind) model.Job {
	key := model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: 1}
	return model.Job{Key: model.JobKey{Namespace: 1, Device: key, Collector: kind}, Device: model.Device{Key: key, Name: "eth0", Eligibility: model.Eligible}}
}

func TestStatisticIoctl(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		kind                                         model.CollectorKind
		command                                      uint32
		mutate                                       func(uint32, []byte) error
		wantErr                                      error
		invalid                                      bool
	}{
		{"driver", "positive", "driver string set and values", "raw untyped driver values", model.CollectorDriver, unix.ETHTOOL_GSTATS, nil, nil, false},
		{"phy", "positive", "independent PHY string set", "raw untyped PHY values", model.CollectorPHY, unix.ETHTOOL_GPHYSTATS, nil, nil, false},
		{"unsupported", "negative", "PHY command unsupported", "unsupported without driver fallback", model.CollectorPHY, unix.ETHTOOL_GPHYSTATS, func(uint32, []byte) error { return unix.EOPNOTSUPP }, unix.EOPNOTSUPP, true},
		{"permission", "negative", "driver query denied", "error preserved", model.CollectorDriver, unix.ETHTOOL_GSTATS, func(uint32, []byte) error { return unix.EPERM }, unix.EPERM, true},
		{"oversize", "boundary", "count exceeds allocation limit", "reject before strings or values", model.CollectorDriver, unix.ETHTOOL_GSTATS, func(cmd uint32, b []byte) error {
			if cmd == unix.ETHTOOL_GDRVINFO {
				binary.NativeEndian.PutUint32(b[180:], maximumSamples+1)
			}
			return nil
		}, nil, true},
		{"wrong command", "negative", "kernel overwrites command", "reject malformed reply", model.CollectorDriver, unix.ETHTOOL_GSTATS, func(_ uint32, b []byte) error { binary.NativeEndian.PutUint32(b, 0); return nil }, nil, true},
		{"wrong set", "negative", "strings belong to another set", "reject malformed reply", model.CollectorPHY, unix.ETHTOOL_GPHYSTATS, func(cmd uint32, b []byte) error {
			if cmd == unix.ETHTOOL_GSTRINGS {
				binary.NativeEndian.PutUint32(b[4:], 99)
			}
			return nil
		}, nil, true},
		{"absent PHY set", "corner", "GSSET_INFO omits requested bit", "unsupported", model.CollectorPHY, unix.ETHTOOL_GPHYSTATS, func(cmd uint32, b []byte) error {
			if cmd == unix.ETHTOOL_GSSET_INFO {
				binary.NativeEndian.PutUint64(b[8:], 0)
			}
			return nil
		}, unix.EOPNOTSUPP, true},
		{"extra PHY set", "negative", "unexpected response mask", "reject malformed reply", model.CollectorPHY, unix.ETHTOOL_GPHYSTATS, func(cmd uint32, b []byte) error {
			if cmd == unix.ETHTOOL_GSSET_INFO {
				binary.NativeEndian.PutUint64(b[8:], 129)
			}
			return nil
		}, nil, true},
		{"unstable", "corner", "values repeatedly report changed count", "two attempts then failure", model.CollectorDriver, unix.ETHTOOL_GSTATS, func(cmd uint32, b []byte) error {
			if cmd == unix.ETHTOOL_GSTATS {
				binary.NativeEndian.PutUint32(b[4:], 0)
			}
			return nil
		}, errStatisticShape, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := &statisticFixture{names: []string{"rx", "tx"}, values: []uint64{1<<53 + 1, ^uint64(0)}, mutate: tc.mutate}
			c := statisticTestSource(t, f)
			samples, schema, err := c.read(t.Context(), statisticTestJob(tc.kind))
			if (err != nil) != tc.invalid || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if f.calls[unix.ETHTOOL_GSTRINGS] > 2 || f.calls[tc.command] > 2 {
				t.Fatal("unbounded retry")
			}
			if err != nil {
				return
			}
			if len(samples) != 2 || len(schema.Names) != 2 || f.calls[tc.command] != 1 {
				t.Fatal(tc.expectedOutcome)
			}
			for i, sample := range samples {
				value, ok := sample.Number.Uint64()
				if !ok || value != f.values[i] || sample.Kind != model.SampleUntyped {
					t.Fatal("value changed")
				}
			}
		})
	}
}

func TestStatisticSchemaCache(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		refresh, grow                                bool
	}{
		{"reuse", "positive", "new worker receives accepted immutable schema", "no second name query", false, false},
		{"resync", "corner", "same count names change after invalidation", "new names replace schema", true, false},
		{"growth", "boundary", "count grows during later poll", "new schema and all values", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := &statisticFixture{names: []string{"rx"}, values: []uint64{7}}
			first := statisticTestSource(t, f)
			job := statisticTestJob(model.CollectorDriver)
			old, schema, err := first.read(t.Context(), job)
			if err != nil {
				t.Fatal(err)
			}
			job.StatisticSchema = schema
			if tc.refresh {
				job.StatisticSchema = nil
				f.names[0] = "changed"
			}
			if tc.grow {
				f.names = append(f.names, "tx")
				f.values = append(f.values, 8)
			}
			f.values[0] = 9
			second := statisticTestSource(t, f)
			next, nextSchema, err := second.read(t.Context(), job)
			if err != nil {
				t.Fatal(err)
			}
			refresh := tc.refresh || tc.grow
			if (nextSchema != schema) != refresh || (f.calls[unix.ETHTOOL_GSTRINGS] == 1) != refresh {
				t.Fatal(tc.expectedOutcome)
			}
			if !slices.Equal(nextSchema.Names, f.names) {
				t.Fatal("new values paired with old names")
			}
			before, _ := old[0].Number.Uint64()
			after, _ := next[0].Number.Uint64()
			if before != 7 || after != 9 || schema.Names[0] != "rx" {
				t.Fatal("immutable values or names mutated")
			}
		})
	}
}

func TestStatisticCountRace(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		command                                      uint32
		offset                                       int
	}{
		{"strings", "corner", "first names request has stale count", "rediscovery succeeds once", unix.ETHTOOL_GSTRINGS, 8},
		{"values", "corner", "first values request has stale count", "rediscovery succeeds once", unix.ETHTOOL_GSTATS, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := &statisticFixture{names: []string{"rx"}, values: []uint64{8}}
			f.mutate = func(cmd uint32, b []byte) error {
				if cmd == tc.command && f.calls[cmd] == 1 {
					binary.NativeEndian.PutUint32(b[tc.offset:], 0)
				}
				return nil
			}
			c := statisticTestSource(t, f)
			samples, _, err := c.read(t.Context(), statisticTestJob(model.CollectorDriver))
			if err != nil || len(samples) != 1 || f.calls[unix.ETHTOOL_GDRVINFO] != 2 {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
		})
	}
}

func BenchmarkStatisticCached(b *testing.B) {
	for _, count := range []int{0, 64, 1024, 8192, maximumSamples} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			f := &statisticFixture{names: make([]string, count), values: make([]uint64, count)}
			for i := range count {
				f.names[i] = fmt.Sprintf("queue_%d", i)
			}
			c := statisticTestSource(b, f)
			job := statisticTestJob(model.CollectorDriver)
			_, schema, err := c.read(b.Context(), job)
			if err != nil {
				b.Fatal(err)
			}
			job.StatisticSchema = schema
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, _, err := c.read(b.Context(), job); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestStatisticIoctlLimits(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		count                                        int
	}{
		{"zero", "boundary", "empty supported source", "no variable request", 0},
		{"one", "positive", "one source register", "complete publication", 1},
		{"maximum", "boundary", "65536 source registers", "complete untruncated publication", maximumSamples},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := &statisticFixture{names: make([]string, tc.count), values: make([]uint64, tc.count)}
			for i := range tc.count {
				f.names[i], f.values[i] = fmt.Sprint(i), uint64(i)
			}
			c := statisticTestSource(t, f)
			for _, kind := range statisticCollectors {
				samples, schema, err := c.read(t.Context(), statisticTestJob(kind))
				if err != nil || len(samples) != tc.count || len(schema.Names) != tc.count {
					t.Fatalf("%s: %v", tc.expectedOutcome, err)
				}
				if tc.count > 0 {
					n, ok := samples[tc.count-1].Number.Uint64()
					if !ok || n != uint64(tc.count-1) {
						t.Fatal("last value changed")
					}
				}
			}
			if tc.count == 0 && len(f.calls) != 2 {
				t.Fatal("zero count issued variable requests")
			}
		})
	}
}
