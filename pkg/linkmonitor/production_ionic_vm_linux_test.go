//go:build ionic_vm

package linkmonitor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"golang.org/x/sys/unix"
)

// These tests run inside test-linkmonitor-ionic-vm against the in-tree ionic
// and ionic_rdma drivers bound to a rocm-ernic emulated NIC. The guest script
// exports the netdev and RDMA device it found and runs the real
// go-link-monitor binary, whose metrics the tests scrape.

const (
	ionicMetricPrefix = "go_link_monitor_"
	ibPortActive      = 4 // IB_PORT_ACTIVE
	ibPhysicalLinkUp  = 5 // LINK_UP
)

type ionicFixture struct{ netdev, rdma, url string }

func ionicEnv(t *testing.T) ionicFixture {
	t.Helper()
	f := ionicFixture{
		netdev: os.Getenv("LINKMONITOR_IONIC_NETDEV"),
		rdma:   os.Getenv("LINKMONITOR_IONIC_RDMA"),
		url:    os.Getenv("LINKMONITOR_METRICS_URL"),
	}
	if f.netdev == "" || f.rdma == "" || f.url == "" {
		t.Fatal("LINKMONITOR_IONIC_NETDEV, LINKMONITOR_IONIC_RDMA and LINKMONITOR_METRICS_URL are required")
	}
	return f
}

func scrapeMetrics(ctx context.Context, url string) ([]metricSample, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	return parseExposition(string(body))
}

// value returns the single sample of a prefixed metric matching pairs.
func value(samples []metricSample, name string, pairs ...string) (float64, error) {
	got := matching(samples, ionicMetricPrefix+name, pairs...)
	if len(got) != 1 {
		return 0, fmt.Errorf("%s%v: %d samples", name, pairs, len(got))
	}
	return got[0].Value, nil
}

func want(samples []metricSample, expected float64, name string, pairs ...string) error {
	v, err := value(samples, name, pairs...)
	if err == nil && v != expected {
		err = fmt.Errorf("%s%v=%v want %v", name, pairs, v, expected)
	}
	return err
}

func absent(samples []metricSample, name string, pairs ...string) error {
	if got := matching(samples, ionicMetricPrefix+name, pairs...); len(got) != 0 {
		return fmt.Errorf("%s%v still present: %v", name, pairs, got[0].Labels)
	}
	return nil
}

// waitMetrics scrapes until check passes, failing with the last reason.
func waitMetrics(t *testing.T, url string, timeout time.Duration, check func([]metricSample) error) []metricSample {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		samples, err := scrapeMetrics(t.Context(), url)
		if err == nil {
			err = check(samples)
		}
		if err == nil {
			return samples
		}
		if time.Now().After(deadline) {
			t.Fatalf("after %s: %v", timeout, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func rdmaPorts(t *testing.T, device string) (found bool, port linuxio.RDMAPort, get linuxio.RDMAPort) {
	t.Helper()
	c, err := linuxio.NewClient(unix.NETLINK_RDMA)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	devices, err := c.DumpRDMADevices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devices.Values {
		if d.Name != device {
			continue
		}
		ports, err := c.DumpRDMAPorts(t.Context(), d.Index)
		if err != nil || len(ports.Values) != 1 {
			t.Fatal("expected one ionic port", ports, err)
		}
		got, err := c.GetRDMAPort(t.Context(), d.Index, 1)
		if err != nil || len(got.Values) != 1 {
			t.Fatal("single-port GET", got, err)
		}
		return true, ports.Values[0], got.Values[0]
	}
	return false, linuxio.RDMAPort{}, linuxio.RDMAPort{}
}

func TestIonicDiscovery(t *testing.T) {
	f := ionicEnv(t)
	found, port, get := rdmaPorts(t, f.rdma)
	rows := []struct {
		category, description, expected string
		ok                              bool
	}{
		{"positive", "NLDEV dump lists the ionic RDMA device", "device present with one port", found},
		{"positive", "port 1 carries the ionic netdev", "RoCE association to " + f.netdev, port.Port == 1 && port.HasNetdev && port.NetdevName == f.netdev},
		{"corner", "single-port GET, whose reply is typed RDMA_NLDEV_CMD_GET", "ACTIVE and LINK_UP",
			get.HasState && get.State == ibPortActive && get.HasPhysical && get.Physical == ibPhysicalLinkUp},
	}
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			if !row.ok {
				t.Fatalf("dump=%+v get=%+v", port, get)
			}
		})
	}
}

// ionicCollectors is the per-driver expectation for the ionic netdev (and the
// namespace-scoped netstat collector). Rows other than supported/1 give the
// reason and a cause check that proves it on the guest, so a status that is
// expected for the wrong reason still fails.
// success: 1 succeeded, 0 failed, -1 absent (never attempted).
var ionicCollectors = []struct {
	collector, scope, support string
	success                   float64
	reason                    string
	cause                     func(ionicFixture) error
}{
	{"identity", "netdev", "supported", 1, "", nil},
	{"settings", "netdev", "supported", 1, "", nil},
	{"standard", "netdev", "supported", 1, "", nil},
	{"carrier", "netdev", "supported", 1, "", nil},
	{"driver", "netdev", "supported", 1, "", nil},
	{"phy", "netdev", "supported", 1, "", nil},
	{"channels", "netdev", "supported", 1, "", nil},
	{"rings", "netdev", "supported", 1, "", nil},
	{"rdma_state", "netdev", "supported", 1, "", nil},
	{"rdma_capabilities", "netdev", "not_applicable", 1,
		"the UMAD capability query runs only on native InfiniBand; ionic is RoCE", ionicRoCE},
	{"rdma_counters", "netdev", "supported", 1,
		"ionic_rdma publishes only hw_counters, from the emulator's STATS_HDRS (rocm-ernic #222)", ionicHWCounters},
	{"rdma_events", "netdev", "supported", 1, "", nil},
	{"netstat", "namespace", "supported", 1, "", nil},
}

func ionicPort(f ionicFixture) string {
	return "/sys/class/infiniband/" + f.rdma + "/ports/1"
}

func ionicRoCE(f ionicFixture) error {
	b, err := os.ReadFile(ionicPort(f) + "/link_layer")
	if err != nil {
		return err
	}
	if layer := strings.TrimSpace(string(b)); layer != "Ethernet" {
		return fmt.Errorf("link_layer=%q, want Ethernet", layer)
	}
	return nil
}

// ionicHWCounterNames are the device counters rocm-ernic answers STATS_HDRS
// with, plus the core's lifespan file. Only lifespan is in
// rdmaCounterCatalog; the rest are the emulator's names, so each is a
// catch-all sample.
var ionicHWCounterNames = []string{"commands", "lifespan", "total_bytes_rdma_read", "total_bytes_rdma_write", "total_bytes_received", "total_bytes_sent"}

// ionicRDMAErrorNames are the RDMA error counters a rocm-ernic with error
// statistics adds to STATS_HDRS, under the Pensando ionic_lif_stats names.
// A healthy guest posts no failing work, so each must read zero.
var ionicRDMAErrorNames = []string{
	"req_rx_cqe_err", "req_rx_cqe_flush_err", "req_rx_dup_responses", "req_rx_implied_nak_seq_err", "req_rx_invalid_packets",
	"req_rx_pkt_seq_err", "req_rx_remote_access_err", "req_rx_remote_inv_req_err", "req_rx_remote_oper_err", "req_rx_rnr_retry_err",
	"req_tx_local_access_err", "req_tx_local_oper_err", "req_tx_memory_mgmt_err",
	"resp_rx_cqe_err", "resp_rx_cqe_flush_err", "resp_rx_dup_requests", "resp_rx_inv_request_err", "resp_rx_local_len_err",
	"resp_rx_local_qp_oper_err", "resp_rx_out_of_atomic_res", "resp_rx_out_of_buffer", "resp_rx_out_of_seq_pkts",
	"resp_tx_pkt_seq_err", "resp_tx_remote_access_err", "resp_tx_remote_inv_req_err", "resp_tx_remote_oper_err", "resp_tx_rnr_retry_err",
	"rx_cnp_pkts", "rx_ecn_pkts", "tx_cnp_pkts",
}

// ionicGuestHWCounters lists the guest's hw_counters files, sorted.
func ionicGuestHWCounters(f ionicFixture) ([]string, error) {
	entries, err := os.ReadDir(ionicPort(f) + "/hw_counters")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func ionicHWCounters(f ionicFixture) error {
	for _, group := range [...]string{"counters", "counters_ext"} {
		if _, err := os.Stat(ionicPort(f) + "/" + group); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s present (err=%v): ionic_rdma has no PMA counters", group, err)
		}
	}
	names, err := ionicGuestHWCounters(f)
	if err != nil {
		return err
	}
	withErrors := slices.Sorted(slices.Values(append(slices.Clone(ionicHWCounterNames), ionicRDMAErrorNames...)))
	if !slices.Equal(names, ionicHWCounterNames) && !slices.Equal(names, withErrors) {
		return fmt.Errorf("hw_counters %v, want %v or, with error statistics, %v", names, ionicHWCounterNames, withErrors)
	}
	return nil
}

func supportStatus(samples []metricSample, iface, collector string) string {
	for _, s := range matching(samples, ionicMetricPrefix+"collector_support", "interface", iface, "collector", collector) {
		if s.Value == 1 {
			return s.Labels["status"]
		}
	}
	return "absent"
}

func successValue(samples []metricSample, iface, collector string) float64 {
	v, err := value(samples, "collector_success", "interface", iface, "collector", collector)
	if err != nil {
		return -1
	}
	return v
}

// observedCollectors renders what the monitor reports, for updating the table.
func observedCollectors(samples []metricSample, netdev string) string {
	var lines []string
	for _, s := range matching(samples, ionicMetricPrefix+"collector_support") {
		iface := s.Labels["interface"]
		if s.Value != 1 || (iface != netdev && iface != "") {
			continue
		}
		c := s.Labels["collector"]
		lines = append(lines, fmt.Sprintf("%s interface=%q support=%s success=%v", c, iface, s.Labels["status"], successValue(samples, iface, c)))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestIonicMonitorHealthy(t *testing.T) {
	f := ionicEnv(t)
	waitMetrics(t, f.url, 120*time.Second, func(s []metricSample) error {
		return errors.Join(want(s, 1, "collection_healthy"), want(s, 1, "baseline_ready"),
			want(s, 1, "baseline_up_links"), want(s, 0, "up_links_delta"))
	})
	// Collectors sample on the stats interval; give every one an attempt.
	samples := waitMetrics(t, f.url, 30*time.Second, func(s []metricSample) error {
		if successValue(s, f.netdev, "rings") < 0 || successValue(s, f.netdev, "rdma_counters") < 0 {
			return errors.New("collectors not yet attempted")
		}
		return nil
	})
	t.Logf("observed collectors:\n%s", observedCollectors(samples, f.netdev))
	listed := map[string]bool{}
	for _, row := range ionicCollectors {
		listed[row.collector] = true
		iface := f.netdev
		if row.scope == "namespace" {
			iface = ""
		}
		t.Run(row.collector, func(t *testing.T) {
			expected := fmt.Sprintf("support=%s success=%v", row.support, row.success)
			if row.reason != "" {
				expected += " (" + row.reason + ")"
			}
			category := "positive"
			if row.support != "supported" {
				category = "corner"
			}
			t.Logf("%s: collector %s on %q; expected: %s", category, row.collector, iface, expected)
			if got := supportStatus(samples, iface, row.collector); got != row.support {
				t.Errorf("support=%s", got)
			}
			if got := successValue(samples, iface, row.collector); got != row.success {
				t.Errorf("success=%v", got)
			}
			if row.cause != nil {
				if err := row.cause(f); err != nil {
					t.Errorf("reason does not hold: %v", err)
				}
			}
		})
	}
	ibPrefix := ionicMetricPrefix + "infiniband_"
	for _, row := range []struct {
		category, description, expected string
		check                           func() error
	}{
		{"positive", "the ionic port's rate from ports/1/rate", "one positive rate gauge for the device",
			func() error {
				v, err := value(samples, "infiniband_rate_bytes_per_second", "device", f.rdma, "port", "1")
				if err == nil && v <= 0 {
					err = fmt.Errorf("rate %v", v)
				}
				return err
			}},
		{"positive", "hw_counters/lifespan, the one cataloged ionic hw counter", "one lifespan gauge for the device",
			func() error {
				_, err := value(samples, "infiniband_lifespan_seconds", "device", f.rdma, "port", "1")
				return err
			}},
		{"corner", "the emulator's own counter names are not in the catalog", "lifespan is the only cataloged counter metric for the device",
			func() error {
				var got []string
				for _, field := range rdmaCounterCatalog {
					if len(matching(samples, ibPrefix+field.metric, "device", f.rdma)) != 0 {
						got = append(got, field.metric)
					}
				}
				if !slices.Equal(got, []string{"lifespan_seconds"}) {
					return fmt.Errorf("cataloged metrics %v, want [lifespan_seconds]", got)
				}
				return nil
			}},
		{"positive", "every uncataloged guest hw_counters file", "exactly one catch-all sample per file, labeled with the file name",
			func() error {
				names, err := ionicGuestHWCounters(f)
				if err != nil {
					return err
				}
				var errs []error
				for _, name := range names {
					if name == "lifespan" {
						continue
					}
					if _, err := value(samples, "rdma_hw_counter", "device", f.rdma, "port", "1", "counter", name); err != nil {
						errs = append(errs, err)
					}
				}
				if got := len(matching(samples, ionicMetricPrefix+"rdma_hw_counter", "device", f.rdma)); got != len(names)-1 {
					errs = append(errs, fmt.Errorf("%d catch-all samples for %d uncataloged files", got, len(names)-1))
				}
				return errors.Join(errs...)
			}},
		{"positive", "the emulator's commands counter", "nonzero: the guest's STATS reads are themselves commands",
			func() error {
				v, err := value(samples, "rdma_hw_counter", "device", f.rdma, "port", "1", "counter", "commands")
				if err == nil && v <= 0 {
					err = fmt.Errorf("commands %v", v)
				}
				return err
			}},
		{"negative", "the RDMA error counters on a healthy guest", "each present one reads exactly zero",
			func() error {
				var errs []error
				for _, name := range ionicRDMAErrorNames {
					for _, s := range matching(samples, ionicMetricPrefix+"rdma_hw_counter", "device", f.rdma, "counter", name) {
						if s.Value != 0 {
							errs = append(errs, fmt.Errorf("%s=%v", name, s.Value))
						}
					}
				}
				return errors.Join(errs...)
			}},
		{"corner", "the cataloged lifespan file", "no catch-all sample; it keeps its reviewed metric",
			func() error {
				return absent(samples, "rdma_hw_counter", "device", f.rdma, "counter", "lifespan")
			}},
	} {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			if err := row.check(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Run("negative/no unlisted collector", func(t *testing.T) {
		t.Log("negative: a collector the table does not list; expected: none, so every status is explained")
		for _, s := range matching(samples, ionicMetricPrefix+"collector_support") {
			iface := s.Labels["interface"]
			if (iface == f.netdev || iface == "") && !listed[s.Labels["collector"]] {
				t.Errorf("unlisted collector %v", s.Labels)
			}
		}
	})
	t.Run("negative/no collector errors", func(t *testing.T) {
		t.Log("negative: collector_errors_total on the ionic interface; expected: all zero")
		for _, s := range matching(samples, ionicMetricPrefix+"collector_errors_total", "interface", f.netdev) {
			if s.Value != 0 {
				t.Errorf("errors %v=%v", s.Labels, s.Value)
			}
		}
	})
	if t.Failed() {
		t.Logf("update ionicCollectors from the observed table above")
	}
}

// currentSection returns ethtool's "Current hardware settings:" block.
func currentSection(out string) string {
	if i := strings.Index(out, "Current hardware settings:"); i >= 0 {
		return out[i:]
	}
	return ""
}

func firstNumber(re *regexp.Regexp, text string) (float64, error) {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return 0, fmt.Errorf("%s not in %q", re, text)
	}
	return strconv.ParseFloat(m[1], 64)
}

func TestIonicCLIDifferential(t *testing.T) {
	f := ionicEnv(t)
	speed := regexp.MustCompile(`Speed: (\d+)Mb/s`)
	driver := regexp.MustCompile(`(?m)^driver: (\S+)`)
	combined := regexp.MustCompile(`(?m)^Combined:\s+(\d+)`)
	rx := regexp.MustCompile(`(?m)^RX:\s+(\d+)`)
	tx := regexp.MustCompile(`(?m)^TX:\s+(\d+)`)
	rows := []struct {
		category, description, expected string
		command                         []string
		check                           func(out string, s []metricSample) error
	}{
		{"positive", "link speed", "interface_speed_bits_per_second equals ethtool Speed in bits/s", []string{"ethtool", f.netdev},
			func(out string, s []metricSample) error {
				n, err := firstNumber(speed, out)
				return errors.Join(err, want(s, n*1e6, "interface_speed_bits_per_second", "interface", f.netdev))
			}},
		{"positive", "duplex", "interface_duplex_info full from ethtool", []string{"ethtool", f.netdev},
			func(out string, s []metricSample) error {
				if !strings.Contains(out, "Duplex: Full") {
					return fmt.Errorf("ethtool duplex: %q", out)
				}
				return want(s, 1, "interface_duplex_info", "interface", f.netdev, "duplex", "full", "source", "ethtool")
			}},
		{"positive", "driver identity", "interface_driver_info carries ethtool -i driver", []string{"ethtool", "-i", f.netdev},
			func(out string, s []metricSample) error {
				m := driver.FindStringSubmatch(out)
				if m == nil || m[1] != "ionic" {
					return fmt.Errorf("ethtool -i driver: %q", out)
				}
				return want(s, 1, "interface_driver_info", "interface", f.netdev, "driver", m[1])
			}},
		{"positive", "combined channels", "interface_channels{kind=combined} equals the current count", []string{"ethtool", "-l", f.netdev},
			func(out string, s []metricSample) error {
				n, err := firstNumber(combined, currentSection(out))
				return errors.Join(err, want(s, n, "interface_channels", "interface", f.netdev, "kind", "combined"))
			}},
		{"positive", "rx ring", "interface_ring_entries{kind=rx} equals the current size", []string{"ethtool", "-g", f.netdev},
			func(out string, s []metricSample) error {
				n, err := firstNumber(rx, currentSection(out))
				return errors.Join(err, want(s, n, "interface_ring_entries", "interface", f.netdev, "kind", "rx"))
			}},
		{"boundary", "tx ring", "interface_ring_entries{kind=tx} equals the current size", []string{"ethtool", "-g", f.netdev},
			func(out string, s []metricSample) error {
				n, err := firstNumber(tx, currentSection(out))
				return errors.Join(err, want(s, n, "interface_ring_entries", "interface", f.netdev, "kind", "tx"))
			}},
		{"corner", "RoCE port state", "rdma_port_up 1 on the netdev label while rdma link shows ACTIVE", []string{"rdma", "link", "show", f.rdma + "/1"},
			func(out string, s []metricSample) error {
				if !strings.Contains(out, "state ACTIVE") {
					return fmt.Errorf("rdma link: %q", out)
				}
				return want(s, 1, "rdma_port_up", "interface", f.netdev, "device", f.rdma, "port", "1")
			}},
	}
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			out, err := exec.CommandContext(t.Context(), row.command[0], row.command[1:]...).CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %v: %s", row.command, err, out)
			}
			waitMetrics(t, f.url, 20*time.Second, func(s []metricSample) error { return row.check(string(out), s) })
		})
	}
}

func ip(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "ip", args...).CombinedOutput(); err != nil {
		t.Fatalf("ip %v: %v: %s", args, err, out)
	}
}

func transitions(samples []metricSample, iface, direction string) float64 {
	v, err := value(samples, "observed_oper_transitions_total", "interface", iface, "direction", direction)
	if err != nil {
		return 0
	}
	return v
}

func TestIonicEvents(t *testing.T) {
	f := ionicEnv(t)
	renamed := "lmionic01234567" // 15 bytes, the IFNAMSIZ maximum
	up := func(name string) func(before, s []metricSample) error {
		return func(before, s []metricSample) error {
			return errors.Join(want(s, 1, "interface_up", "interface", name),
				want(s, 1, "rdma_port_up", "interface", name, "device", f.rdma, "port", "1"),
				want(s, 1, "collector_success", "interface", name, "collector", "rdma_events"),
				want(s, 1, "collection_healthy"), want(s, 0, "up_links_delta"))
		}
	}
	rows := []struct {
		category, description, expected string
		commands                        [][]string
		check                           func(before, now []metricSample) error
	}{
		{"negative", "administrative down", "interface_up 0, rdma_port_up 0, one more down transition",
			[][]string{{"link", "set", f.netdev, "down"}},
			func(before, s []metricSample) error {
				var err error
				if transitions(s, f.netdev, "down") <= transitions(before, f.netdev, "down") {
					err = errors.New("no down transition observed")
				}
				return errors.Join(err, want(s, 0, "interface_up", "interface", f.netdev),
					want(s, 0, "rdma_port_up", "interface", f.netdev, "device", f.rdma, "port", "1"))
			}},
		{"positive", "administrative up", "back up and healthy, one more up transition",
			[][]string{{"link", "set", f.netdev, "up"}},
			func(before, s []metricSample) error {
				var err error
				if transitions(s, f.netdev, "up") <= transitions(before, f.netdev, "up") {
					err = errors.New("no up transition observed")
				}
				return errors.Join(err, up(f.netdev)(before, s))
			}},
		{"corner", "rename to a 15-byte name", "RoCE port follows the new name; rdma_events keeps running",
			[][]string{{"link", "set", f.netdev, "down"}, {"link", "set", f.netdev, "name", renamed}, {"link", "set", renamed, "up"}},
			func(before, s []metricSample) error {
				return errors.Join(up(renamed)(before, s), absent(s, "interface_up", "interface", f.netdev))
			}},
		{"boundary", "rename back", "original name restored and healthy",
			[][]string{{"link", "set", renamed, "down"}, {"link", "set", renamed, "name", f.netdev}, {"link", "set", f.netdev, "up"}},
			func(before, s []metricSample) error {
				return errors.Join(up(f.netdev)(before, s), absent(s, "interface_up", "interface", renamed))
			}},
	}
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			before, err := scrapeMetrics(t.Context(), f.url)
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range row.commands {
				ip(t, args...)
			}
			waitMetrics(t, f.url, 60*time.Second, func(s []metricSample) error { return row.check(before, s) })
		})
	}
}

// TestIonicCoreBackend runs against go-link-monitor-core, the CGO_ENABLED=0
// build, after TestIonicEvents has passed against it: auto resolves to the
// netlink backend there, and rdma_events is healthy on the real driver.
func TestIonicCoreBackend(t *testing.T) {
	f := ionicEnv(t)
	samples := waitMetrics(t, f.url, 60*time.Second, func(s []metricSample) error {
		return want(s, 1, "collector_success", "interface", f.netdev, "collector", "rdma_events")
	})
	for _, row := range []struct {
		name, category, description, expected string
		check                                 func([]metricSample) error
	}{
		{"netlink", "positive", "the core build's resolved backend", "rdma_events_backend_info{backend=netlink} 1",
			func(s []metricSample) error { return want(s, 1, "rdma_events_backend_info", "backend", "netlink") }},
		{"no_verbs", "negative", "the verbs backend, which needs cgo", "absent",
			func(s []metricSample) error { return absent(s, "rdma_events_backend_info", "backend", "verbs") }},
		{"port_up", "boundary", "the RoCE port the events keep current", "rdma_port_up 1",
			func(s []metricSample) error {
				return want(s, 1, "rdma_port_up", "interface", f.netdev, "device", f.rdma, "port", "1")
			}},
		{"healthy", "corner", "the whole collection with no cgo in the process", "collection_healthy 1",
			func(s []metricSample) error { return want(s, 1, "collection_healthy") }},
	} {
		t.Logf("%s: %s: %s; expected: %s", row.category, row.name, row.description, row.expected)
		if err := row.check(samples); err != nil {
			t.Errorf("%s: %v", row.name, err)
		}
	}
}

// TestIonicRemoved runs after a guest PCI remove: the kernel must finish removal
// while the monitor holds verbs and netlink handles, and the monitor must
// report the lost link against its baseline.
func TestIonicRemoved(t *testing.T) {
	f := ionicEnv(t)
	t.Logf("negative: ionic NIC hot-unplugged; expected: no RDMA device, link metrics gone, up_links_delta -1")
	if found, _, _ := rdmaPorts(t, f.rdma); found {
		t.Fatal("RDMA device still registered")
	}
	waitMetrics(t, f.url, 60*time.Second, func(s []metricSample) error {
		return errors.Join(want(s, -1, "up_links_delta"), absent(s, "interface_up", "interface", f.netdev),
			absent(s, "rdma_port_up", "device", f.rdma))
	})
}

// TestIonicRecovered runs after QMP device_add with the rediscovered names.
func TestIonicRecovered(t *testing.T) {
	f := ionicEnv(t)
	t.Logf("positive: ionic NIC replugged; expected: rediscovered, back to baseline and healthy, RDMA event coverage restored")
	if found, _, get := rdmaPorts(t, f.rdma); !found || get.State != ibPortActive {
		t.Fatalf("RDMA device found=%v port=%+v", found, get)
	}
	waitMetrics(t, f.url, 120*time.Second, func(s []metricSample) error {
		return errors.Join(want(s, 0, "up_links_delta"), want(s, 1, "collection_healthy"),
			want(s, 1, "interface_up", "interface", f.netdev),
			want(s, 1, "rdma_port_up", "interface", f.netdev, "device", f.rdma, "port", "1"),
			want(s, 1, "collector_success", "interface", f.netdev, "collector", "rdma_state"),
			want(s, 1, "collector_success", "interface", f.netdev, "collector", "rdma_events"),
			want(s, 1, "collector_support", "interface", f.netdev, "collector", "rdma_events", "status", "supported"))
	})
}
