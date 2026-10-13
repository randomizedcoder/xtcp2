//go:build ionic_vm

package linkmonitor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// These tests run inside test-linkmonitor-ionic-faults-vm, where rocm-ernic
// injects (nix/microvms/linkmonitor-ionic.nix):
//
//	eth_rx: every 10 effect nobuf   every 10th frame off the TAP is dropped as
//	                                if no Rx buffer were posted
//	eth_tx: every 10 effect drop    every 10th frame the guest sends is
//	                                completed but never sent
//	stats_vals: after 2 effect fail the second STATS_VALS admin command fails
//
// Each pattern counts every frame since boot, so at any published snapshot the
// guest's own counters must satisfy dropped == (delivered + dropped) / 10.
// That turns the monitor's interface counters into an exact check, not a
// "moved" check.

const faultEvery = 10

// sysfsStat reads /sys/class/net/<netdev>/statistics/<name>.
func sysfsStat(netdev, name string) (float64, error) {
	raw, err := os.ReadFile("/sys/class/net/" + netdev + "/statistics/" + name)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
}

func TestIonicFaultStatsVals(t *testing.T) {
	f := ionicEnv(t)
	commands := ionicPort(f) + "/hw_counters/commands"
	// Order matters: event 1 was ionic_rdma's read at registration, so these
	// two reads are events 2 and 3.
	for _, row := range []struct {
		category, description, expected string
		file                            string
		wantErr                         error
	}{
		{"positive", "the guest's first read of hw_counters/commands", "EINVAL: stats_vals fails event 2 with IONIC_RC_EIO",
			commands, unix.EINVAL},
		{"boundary", "the next read of the same file", "a number: after 2 fires once", commands, nil},
		{"corner", "hw_counters/lifespan, which the RDMA core answers without the driver", "a number", ionicPort(f) + "/hw_counters/lifespan", nil},
	} {
		t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
		raw, err := os.ReadFile(row.file)
		switch {
		case row.wantErr != nil && !errors.Is(err, row.wantErr):
			t.Errorf("%s: err=%v (%q), want %v", row.description, err, raw, row.wantErr)
		case row.wantErr == nil && err != nil:
			t.Errorf("%s: %v", row.description, err)
		case row.wantErr == nil:
			if _, perr := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64); perr != nil {
				t.Errorf("%s: %q is not a number", row.description, raw)
			}
		}
	}
}

// pingPeer pings the TAP's host end and returns transmitted and received.
func pingPeer(t *testing.T, peer string, count int) (sent, received int) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "ping", "-n", "-q", "-c", strconv.Itoa(count), "-i", "0.02", "-W", "1", peer).CombinedOutput()
	// ping exits 1 when some replies are missing, which is the point here.
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		t.Fatalf("ping %s: %v: %s", peer, err, out)
	}
	m := regexp.MustCompile(`(\d+) packets transmitted, (\d+) received`).FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("ping summary not found: %s", out)
	}
	sent, _ = strconv.Atoi(m[1])
	received, _ = strconv.Atoi(m[2])
	t.Logf("ping: %d sent, %d received", sent, received)
	return sent, received
}

// queueSum adds the monitor's per-queue ethtool statistics <dir>_<n>_pkts.
func queueSum(samples []metricSample, netdev, dir string) (sum float64, queues int) {
	re := regexp.MustCompile(`^` + dir + `_\d+_pkts$`)
	for _, s := range matching(samples, ionicMetricPrefix+"ethtool_statistic", "interface", netdev) {
		if re.MatchString(s.Labels["statistic"]) {
			sum += s.Value
			queues++
		}
	}
	return sum, queues
}

func TestIonicFaultCounters(t *testing.T) {
	f := ionicEnv(t)
	peer := os.Getenv("LINKMONITOR_IONIC_PEER")
	if peer == "" {
		t.Fatal("LINKMONITOR_IONIC_PEER is required")
	}
	waitMetrics(t, f.url, 120*time.Second, func(s []metricSample) error {
		return errors.Join(want(s, 1, "collection_healthy"), want(s, 1, "baseline_ready"))
	})

	t.Run("positive/ping through both faults", func(t *testing.T) {
		t.Log("positive: 200 pings to the TAP's host end; expected: some but not all replies (each echo crosses eth_tx then eth_rx)")
		sent, received := pingPeer(t, peer, 200)
		if received == 0 || received >= sent {
			t.Errorf("%d of %d replies", received, sent)
		}
	})

	metric := func(s []metricSample, name string) float64 {
		v, err := value(s, name, "interface", f.netdev)
		if err != nil {
			return -1
		}
		return v
	}
	rows := []struct {
		category, description, expected string
		check                           func(s []metricSample) error
	}{
		{"positive", "transmit drops", "interface_transmit_dropped_total equals sysfs tx_dropped and is non-zero",
			func(s []metricSample) error {
				return sysfsEqual(s, f.netdev, "interface_transmit_dropped_total", "tx_dropped", true)
			}},
		{"positive", "receive drops", "interface_receive_dropped_total equals sysfs rx_dropped and is non-zero",
			func(s []metricSample) error {
				return sysfsEqual(s, f.netdev, "interface_receive_dropped_total", "rx_dropped", true)
			}},
		{"positive", "receive over errors", "interface_receive_over_errors_total equals sysfs rx_over_errors and is non-zero (nobuf is rx_queue_empty)",
			func(s []metricSample) error {
				return sysfsEqual(s, f.netdev, "interface_receive_over_errors_total", "rx_over_errors", true)
			}},
		{"positive", "eth_tx every 10 drop", "transmit dropped == (transmit packets + dropped) / 10, frame for frame",
			func(s []metricSample) error {
				return everyN(metric(s, "interface_transmit_packets_total"), metric(s, "interface_transmit_dropped_total"))
			}},
		{"positive", "eth_rx every 10 nobuf", "receive dropped == (receive packets + dropped) / 10, frame for frame",
			func(s []metricSample) error {
				return everyN(metric(s, "interface_receive_packets_total"), metric(s, "interface_receive_dropped_total"))
			}},
		{"corner", "nobuf moves both drop counters", "receive over errors == receive dropped: every Rx drop is an injected nobuf",
			func(s []metricSample) error {
				over, dropped := metric(s, "interface_receive_over_errors_total"), metric(s, "interface_receive_dropped_total")
				if over != dropped {
					return fmt.Errorf("over %v, dropped %v", over, dropped)
				}
				return nil
			}},
		{"negative", "injected loss is not reported as a DMA or descriptor error", "receive missed and transmit errors stay 0",
			func(s []metricSample) error {
				return errors.Join(want(s, 0, "interface_receive_missed_errors_total", "interface", f.netdev),
					want(s, 0, "interface_transmit_errors_total", "interface", f.netdev))
			}},
		{"positive", "the driver's Rx queues against the NIC's delivered count", "the sum of ethtool_statistic rx_N_pkts equals interface_receive_packets_total",
			func(s []metricSample) error {
				sum, n := queueSum(s, f.netdev, "rx")
				if got := metric(s, "interface_receive_packets_total"); n == 0 || sum != got {
					return fmt.Errorf("%d queues sum %v, receive packets %v", n, sum, got)
				}
				return nil
			}},
		{"corner", "a dropped Tx frame is still completed to the driver", "the sum of ethtool_statistic tx_N_pkts equals transmit packets + dropped",
			func(s []metricSample) error {
				sum, n := queueSum(s, f.netdev, "tx")
				got := metric(s, "interface_transmit_packets_total") + metric(s, "interface_transmit_dropped_total")
				if n == 0 || sum != got {
					return fmt.Errorf("%d queues sum %v, transmit packets + dropped %v", n, sum, got)
				}
				return nil
			}},
		{"boundary", "generic-100g on 8 vCPUs", "interface_channels{kind=combined} 8 and per-queue statistics for queues 0-7",
			func(s []metricSample) error {
				_, rxq := queueSum(s, f.netdev, "rx")
				_, txq := queueSum(s, f.netdev, "tx")
				err := want(s, 8, "interface_channels", "interface", f.netdev, "kind", "combined")
				if rxq != 8 || txq != 8 {
					err = errors.Join(err, fmt.Errorf("rx queues %d, tx queues %d", rxq, txq))
				}
				return err
			}},
	}
	defer func() {
		// The counters the rows were judged on, for the evidence record.
		s, err := scrapeMetrics(t.Context(), f.url)
		if err != nil {
			return
		}
		rxq, _ := queueSum(s, f.netdev, "rx")
		txq, _ := queueSum(s, f.netdev, "tx")
		t.Logf("final: tx packets %v dropped %v, rx packets %v dropped %v over %v, queue sums rx %v tx %v",
			metric(s, "interface_transmit_packets_total"), metric(s, "interface_transmit_dropped_total"),
			metric(s, "interface_receive_packets_total"), metric(s, "interface_receive_dropped_total"),
			metric(s, "interface_receive_over_errors_total"), rxq, txq)
	}()
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			// The LIF block is published each second and the monitor samples
			// every 2s, so wait for a scrape where the row holds.
			waitMetrics(t, f.url, 60*time.Second, row.check)
		})
	}
}

// sysfsEqual compares a monitor interface counter with its sysfs statistic.
func sysfsEqual(s []metricSample, netdev, metric, stat string, nonZero bool) error {
	got, err := value(s, metric, "interface", netdev)
	if err != nil {
		return err
	}
	sys, err := sysfsStat(netdev, stat)
	if err != nil {
		return err
	}
	if got != sys || (nonZero && got == 0) {
		return fmt.Errorf("%s %v, sysfs %s %v", metric, got, stat, sys)
	}
	return nil
}

// everyN checks dropped == floor((passed + dropped) / faultEvery).
func everyN(passed, dropped float64) error {
	if passed < 0 || dropped < 0 {
		return errors.New("counter missing")
	}
	events := uint64(passed + dropped)
	if uint64(dropped) != events/faultEvery {
		return fmt.Errorf("%v dropped of %d events, want %d", dropped, events, events/faultEvery)
	}
	return nil
}
