package linkmonitor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

type hostTestFile struct {
	io.Reader
	close func() error
}

func (f hostTestFile) Close() error { return f.close() }

func hostTestCollector(t testing.TB, files map[string]string) *hostCollector {
	t.Helper()
	cfg, err := validateConfig(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	c := newHostCollector(nil, "/fixture", cfg)
	c.open = func(path string) (io.ReadCloser, error) {
		data, exists := files[filepath.Base(path)]
		if !exists {
			return nil, os.ErrNotExist
		}
		return io.NopCloser(strings.NewReader(data)), nil
	}
	return c
}

func hostTestJob() model.Job {
	return model.Job{Key: model.JobKey{Namespace: 1, Collector: model.CollectorNetstat}}
}

func TestHostReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		file                                         string
		openErr, readErr, closeErr                   error
		data                                         string
		want                                         model.ErrorReason
	}{
		{"optional", "corner", "missing snmp6", "supported success", "snmp6", os.ErrNotExist, nil, nil, "", model.ErrorNone},
		{"required", "negative", "missing snmp", "I/O failure", "snmp", os.ErrNotExist, nil, nil, "", model.ErrorIO},
		{"netstat", "negative", "missing netstat after snmp", "atomic failure", "netstat", os.ErrNotExist, nil, nil, "", model.ErrorIO},
		{"permission", "negative", "optional IPv6 unreadable", "permission failure", "snmp6", syscall.EACCES, nil, nil, "", model.ErrorPermission},
		{"partial", "negative", "partial file then read error", "I/O failure", "netstat", nil, syscall.EIO, nil, "", model.ErrorIO},
		{"read ENOENT", "negative", "snmp6 disappears after open", "I/O failure", "snmp6", nil, os.ErrNotExist, nil, "", model.ErrorIO},
		{"close", "negative", "close fails after valid read", "I/O failure", "netstat", nil, nil, syscall.EIO, "", model.ErrorIO},
		{"bad", "negative", "malformed second file", "malformed with no partial samples", "netstat", nil, nil, nil, "Tcp: A\nTcp: bad", model.ErrorMalformed},
		{"cross duplicate", "negative", "same key in snmp and netstat", "malformed", "netstat", nil, nil, nil, "Tcp: A\nTcp: 2", model.ErrorMalformed},
		{"IPv6 collision", "negative", "IPv6 duplicate of paired protocol", "malformed", "snmp6", nil, nil, nil, "Ip6A 3", model.ErrorMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c := hostTestCollector(t, map[string]string{"snmp": "Tcp: A\nTcp: 1\nIp6: A\nIp6: 2", "netstat": "", "snmp6": ""})
			original := c.open
			opened, closed := 0, 0
			c.open = func(path string) (io.ReadCloser, error) {
				if filepath.Base(path) != tc.file {
					return original(path)
				}
				if tc.openErr != nil {
					return nil, tc.openErr
				}
				opened++
				return hostTestFile{Reader: io.MultiReader(strings.NewReader(tc.data), hostErrorReader{tc.readErr}), close: func() error { closed++; return tc.closeErr }}, nil
			}
			result := c.Collect(t.Context(), hostTestJob())
			if result.Reason != tc.want || (result.Err != nil) != (tc.want != model.ErrorNone) || opened != closed {
				t.Fatalf("%s: %+v closes=%d/%d", tc.expectedOutcome, result, closed, opened)
			}
			if result.Err != nil && len(result.Samples) != 0 {
				t.Fatal("partial samples escaped")
			}
		})
	}
}

type hostErrorReader struct{ err error }

func (r hostErrorReader) Read([]byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	return 0, io.EOF
}

func TestHostFileBounds(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		size                                         int
		invalid                                      bool
	}{
		{"empty", "boundary", "zero byte file", "accepted", 0, false},
		{"limit", "boundary", "exactly 4 MiB whitespace", "accepted", maximumHostFile, false},
		{"excess", "negative", "4 MiB plus one byte", "oversize", maximumHostFile + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c := hostTestCollector(t, map[string]string{"snmp": strings.Repeat(" ", tc.size), "netstat": ""})
			result := c.Collect(t.Context(), hostTestJob())
			if errors.Is(result.Err, errHostLimit) != tc.invalid || (result.Err != nil && !tc.invalid) {
				t.Fatalf("%s: %v", tc.expectedOutcome, result.Err)
			}
			if cap(c.scratch) > maximumHostFile+1 {
				t.Fatal("scratch allocation exceeded limit")
			}
		})
	}
}

func TestHostSchemaChanges(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		next                                         string
		ipv6                                         bool
		want                                         []string
	}{
		{"same", "positive", "same schema new values", "reuse descriptors", "Tcp: A B\nTcp: 3 4", false, []string{"netstat_Tcp_A", "netstat_Tcp_B"}},
		{"reorder", "corner", "same size reordered fields", "associate by current order", "Tcp: B A\nTcp: 3 4", false, []string{"netstat_Tcp_B", "netstat_Tcp_A"}},
		{"remove", "corner", "field removed", "old family removed", "Tcp: B\nTcp: 3", false, []string{"netstat_Tcp_B"}},
		{"add IPv6", "corner", "optional file appears", "new family added", "Tcp: B\nTcp: 3", true, []string{"netstat_Tcp_B", "netstat_Ip6_A"}},
		{"empty", "boundary", "all fields removed", "empty replacement", "", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			files := map[string]string{"snmp": "Tcp: A B\nTcp: 1 2", "netstat": ""}
			c := hostTestCollector(t, files)
			first := c.Collect(t.Context(), hostTestJob())
			if first.Err != nil {
				t.Fatal(first.Err)
			}
			files["snmp"] = tc.next
			if tc.ipv6 {
				files["snmp6"] = "Ip6A 5"
			}
			second := c.Collect(t.Context(), hostTestJob())
			if second.Err != nil {
				t.Fatal(second.Err)
			}
			names := make([]string, 0, len(second.Samples))
			for i, sample := range second.Samples {
				names = append(names, sample.Descriptor)
				want := uint64(3 + i)
				if strings.Contains(sample.Descriptor, "Ip6") {
					want = 5
				}
				if sample.Number != model.Unsigned(want) || sample.Kind != model.SampleUntyped || len(sample.Labels) != 0 || sample.Counter != (model.CounterIdentity{}) {
					t.Fatalf("incorrect sample: %+v", sample)
				}
			}
			if !slices.Equal(names, tc.want) {
				t.Fatal(tc.expectedOutcome, names)
			}
			clear(c.scratch)
			if first.Samples[0].Descriptor != "netstat_Tcp_A" || first.Samples[0].Number != model.Unsigned(1) {
				t.Fatal("old result mutated")
			}
			delete(files, "snmp6")
			third := c.Collect(t.Context(), hostTestJob())
			if third.Err != nil || len(third.Samples) != len(second.Samples)-btoi(tc.ipv6) {
				t.Fatal("IPv6 disappearance", third.Err)
			}
		})
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestHostCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		before                                       bool
	}{
		{"before", "negative", "canceled before first open", "no I/O", true},
		{"between", "corner", "canceled on first close", "one file and no samples", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.before {
				cancel()
			}
			c := hostTestCollector(t, nil)
			opens := 0
			c.open = func(string) (io.ReadCloser, error) {
				opens++
				return hostTestFile{Reader: bytes.NewReader(nil), close: func() error { cancel(); return nil }}, nil
			}
			result := c.Collect(ctx, hostTestJob())
			if !errors.Is(result.Err, context.Canceled) || opens != btoi(!tc.before) || len(result.Samples) != 0 {
				t.Fatal(tc.expectedOutcome, result)
			}
		})
	}
}
