package xtcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// writeSockstat6 opens a sockstat6-shaped file (sockstat6_seq_show,
// net/ipv6/proc.c) with TCP6 inuse; the line need not be first.
func writeSockstat6(t *testing.T, content string) int {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sockstat6")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return int(f.Fd())
}

// TestGateCountsIPv6 pins the socket-count gate's demand now that a poll
// dumps both families: a namespace's AF_INET6 sockets (sockstat6's TCP6
// inuse; tw in sockstat already covers both families) count toward its
// demand, so an IPv6-heavy namespace is degraded like an IPv4-heavy one.
// Before, v6_heavy and tcp6_line_not_first failed: sockstat6 was not read.
func TestGateCountsIPv6(t *testing.T) {
	type expectation struct {
		demand  float64 // poll_sockets
		sampled bool    // the namespace got a shard request
		unknown float64
	}
	const badFD = 1 << 20 // EBADF
	tests := []struct {
		name        string
		category    string
		description string
		inuse       string // sockstat TCP inuse (tw 0)
		sockstat6   string // "" : no sockstat6 fd
		fd6         int    // overrides the sockstat6 fd when non-zero
		expected    expectation
	}{
		{
			name: "v6_heavy", category: "positive",
			description: "1000 AF_INET and 500000 AF_INET6 sockets over a 200000 budget: sampled",
			inuse:       "1000", sockstat6: "TCP6: inuse 500000\nUDP6: inuse 0\n",
			expected: expectation{demand: 501000, sampled: true},
		},
		{
			name: "no_sockstat6", category: "negative",
			description: "no sockstat6 fd (no IPv6 in the kernel): AF_INET demand only, still known",
			inuse:       "1000",
			expected:    expectation{demand: 1000},
		},
		{
			name: "sockstat6_read_fails", category: "negative",
			description: "an open sockstat6 that can't be read leaves the namespace's count unknown",
			inuse:       "1000", fd6: badFD,
			expected: expectation{demand: float64(262_144), sampled: true, unknown: 1},
		},
		{
			name: "v6_zero", category: "boundary",
			description: "TCP6 inuse 0 adds nothing",
			inuse:       "1000", sockstat6: "TCP6: inuse 0\n",
			expected: expectation{demand: 1000},
		},
		{
			name: "tcp6_line_not_first", category: "corner",
			description: "the TCP6 line after other lines is still found",
			inuse:       "1000", sockstat6: "RAW6: inuse 0\nTCP6: inuse 2500\nUDP6: inuse 3\n",
			expected: expectation{demand: 3500},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := newFamilyPoller(t, nil)
			x.config.MaxRecordsPerPoll = 200_000
			x.config.MaxRecordsPerNetns = 262_144
			x.pPollSockets = prometheus.NewGauge(prometheus.GaugeOpts{Name: "s"})
			x.pPollDegraded = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "d"}, []string{"level"})
			defer x.closeGate()
			h := newStatHandle(writeSockstat(t, tc.inuse, "0"), -1)
			switch {
			case tc.fd6 != 0:
				h.fd6 = tc.fd6
			case tc.sockstat6 != "":
				h.fd6 = writeSockstat6(t, tc.sockstat6)
			}
			x.nsMap.Store(uint64(1), netNSitem{inode: 1, socketFD: 100, stat: h})
			k := newFamilyKernel(t, 0)

			x.pollAllNetlinkSockets(7)
			got := expectation{
				demand:  testutil.ToFloat64(x.pPollSockets),
				unknown: testutil.ToFloat64(x.pC.WithLabelValues("poller", "planPoll", "unknownCount")),
			}
			if sends := k.sends[100]; len(sends) > 0 {
				got.sampled = sends[0].bytecode
			}
			if got != tc.expected {
				t.Errorf("%s (%s): %s\n got      %+v\n expected %+v", tc.name, tc.category, tc.description, got, tc.expected)
			}
		})
	}
}
