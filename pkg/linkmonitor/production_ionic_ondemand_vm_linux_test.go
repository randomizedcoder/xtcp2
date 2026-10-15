//go:build ionic_vm

package linkmonitor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// These tests run after the guest script restarts the monitor with
// -collection.mode=on-demand, -watch.interval=2s, -watch.slow-interval=4s and
// -on-demand.ttl=5s (nix/microvms/linkmonitor-ionic.nix). /metrics/collection
// never refreshes, so it shows what the monitor collected on its own.

const (
	onDemandWatch = 2 * time.Second
	onDemandTTL   = 5 * time.Second
)

type onDemandPaths struct{ summary, collection, netstat, base string }

func onDemandEnv(t *testing.T) (ionicFixture, onDemandPaths) {
	t.Helper()
	f := ionicEnv(t)
	base, ok := strings.CutSuffix(f.url, "/metrics-all")
	if !ok {
		t.Fatalf("LINKMONITOR_METRICS_URL %q does not end in /metrics-all", f.url)
	}
	return f, onDemandPaths{summary: base + "/metrics", collection: base + "/metrics/collection", netstat: base + "/metrics/netstat", base: base}
}

// get returns the status and body of one GET.
func get(ctx context.Context, url string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return resp.StatusCode, string(body), err
}

func mustScrape(t *testing.T, url string) []metricSample {
	t.Helper()
	s, err := scrapeMetrics(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// lastSuccess is a collector's last success time, or -1 when absent.
func lastSuccess(s []metricSample, iface, collector string) float64 {
	v, err := value(s, "collector_last_success_timestamp_seconds", "interface", iface, "collector", collector)
	if err != nil {
		return -1
	}
	return v
}

func refreshes(s []metricSample, result string) float64 {
	v, err := value(s, "on_demand_refreshes_total", "result", result)
	if err != nil {
		return 0
	}
	return v
}

func refreshTotal(s []metricSample) float64 {
	return refreshes(s, "cached") + refreshes(s, "collected") + refreshes(s, "timeout")
}

// TestIonicOnDemand checks on-demand collection against the real binary. The
// rows run in order: each relies on the state the previous one left.
func TestIonicOnDemand(t *testing.T) {
	f, p := onDemandEnv(t)
	rows := []struct {
		name, category, description, expected string
		run                                   func(t *testing.T)
	}{
		{"mode", "positive", "monitor restarted on demand", "collection_mode{mode=on-demand} 1; /metrics settles with no problems",
			func(t *testing.T) {
				waitMetrics(t, p.collection, 30*time.Second, func(s []metricSample) error {
					return errors.Join(want(s, 1, "collection_mode", "mode", "on-demand"), absent(s, "collection_mode", "mode", "poll"))
				})
				waitMetrics(t, p.summary, 60*time.Second, func(s []metricSample) error {
					return errors.Join(want(s, 0, "problems"), want(s, 0, "up_links_delta"), want(s, 1, "collection_healthy"))
				})
			}},
		{"detail-idle", "boundary", "three watch intervals with no detail scrape", "standard advances each watch poll; netstat keeps its startup timestamp",
			func(t *testing.T) {
				before := mustScrape(t, p.collection)
				time.Sleep(3*onDemandWatch + time.Second)
				after := mustScrape(t, p.collection)
				if lastSuccess(after, f.netdev, "standard") <= lastSuccess(before, f.netdev, "standard") {
					t.Errorf("standard did not advance: %v then %v", lastSuccess(before, f.netdev, "standard"), lastSuccess(after, f.netdev, "standard"))
				}
				if b, a := lastSuccess(before, "", "netstat"), lastSuccess(after, "", "netstat"); b < 0 || a != b {
					t.Errorf("netstat last success %v then %v; want the startup collection, unchanged", b, a)
				}
				if total := refreshTotal(after); total != 0 {
					t.Errorf("%v refreshes without a detail scrape", total)
				}
			}},
		{"detail-collects", "positive", "GET /metrics/netstat with data older than the TTL", "netstat series served; netstat last success advances; one collected refresh",
			func(t *testing.T) {
				before := mustScrape(t, p.collection)
				if age := time.Since(time.Unix(int64(lastSuccess(before, "", "netstat")), 0)); age <= onDemandTTL {
					time.Sleep(onDemandTTL - age + time.Second)
				}
				s := mustScrape(t, p.netstat)
				netstat := 0
				for _, sample := range s {
					if strings.HasPrefix(sample.Name, ionicMetricPrefix+"netstat_") {
						netstat++
					}
				}
				after := mustScrape(t, p.collection)
				if netstat == 0 {
					t.Error("no netstat series on /metrics/netstat")
				}
				if lastSuccess(after, "", "netstat") <= lastSuccess(before, "", "netstat") {
					t.Errorf("netstat last success did not advance: %v", lastSuccess(after, "", "netstat"))
				}
				if got := refreshes(after, "collected") - refreshes(before, "collected"); got != 1 {
					t.Errorf("collected refreshes +%v, want +1", got)
				}
			}},
		{"repeat-cached", "corner", "the same scrape again within the TTL", "one cached refresh; netstat not collected again",
			func(t *testing.T) {
				before := mustScrape(t, p.collection)
				mustScrape(t, p.netstat)
				after := mustScrape(t, p.collection)
				if got := refreshes(after, "cached") - refreshes(before, "cached"); got != 1 {
					t.Errorf("cached refreshes +%v, want +1", got)
				}
				if a, b := lastSuccess(after, "", "netstat"), lastSuccess(before, "", "netstat"); a != b {
					t.Errorf("netstat collected again: %v then %v", b, a)
				}
			}},
		{"unknown-interface", "negative", "GET /metrics/driver/<absent interface>", "404; no refresh counted",
			func(t *testing.T) {
				before := mustScrape(t, p.collection)
				status, body, err := get(t.Context(), p.base+"/metrics/driver/lmabsent0")
				if err != nil {
					t.Fatal(err)
				}
				after := mustScrape(t, p.collection)
				if status != http.StatusNotFound {
					t.Errorf("status %d (%q), want 404", status, body)
				}
				if got := refreshTotal(after) - refreshTotal(before); got != 0 {
					t.Errorf("refreshes +%v, want 0", got)
				}
			}},
		{"link-down-event", "negative", "administrative down with no detail scrape", "up_links_delta -1 on /metrics within 2s, before the next watch poll could matter",
			func(t *testing.T) {
				ip(t, "link", "set", f.netdev, "down")
				waitMetrics(t, p.summary, 2*time.Second, func(s []metricSample) error { return want(s, -1, "up_links_delta") })
			}},
		{"link-up-event", "positive", "administrative up", "up_links_delta back to 0 and the RDMA port up again",
			func(t *testing.T) {
				ip(t, "link", "set", f.netdev, "up")
				waitMetrics(t, p.summary, 60*time.Second, func(s []metricSample) error { return want(s, 0, "up_links_delta") })
				waitMetrics(t, f.url, 60*time.Second, func(s []metricSample) error {
					return want(s, 1, "rdma_port_up", "interface", f.netdev, "device", f.rdma, "port", "1")
				})
			}},
	}
	for _, row := range rows {
		ok := t.Run(row.category+"/"+row.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			row.run(t)
		})
		if !ok {
			return // later rows depend on this one
		}
	}
}

// TestIonicOnDemandErrors runs in the faults VM after the switch to on demand:
// the injected receive faults must surface as the errors reason on /metrics
// from the watch tier alone, with no detail scrape.
func TestIonicOnDemandErrors(t *testing.T) {
	f, p := onDemandEnv(t)
	peer := os.Getenv("LINKMONITOR_IONIC_PEER")
	if peer == "" {
		t.Fatal("LINKMONITOR_IONIC_PEER is required")
	}
	rows := []struct {
		name, category, description, expected string
		run                                   func(t *testing.T)
	}{
		{"anchored", "boundary", "first watch poll after the restart", "standard has a success to anchor the next increase against; no errors reason yet",
			func(t *testing.T) {
				waitMetrics(t, p.collection, 30*time.Second, func(s []metricSample) error {
					if lastSuccess(s, f.netdev, "standard") < 0 {
						return errors.New("no standard success yet")
					}
					return want(s, 1, "collection_mode", "mode", "on-demand")
				})
				time.Sleep(onDemandWatch) // one more poll, so the anchor is in place
				if err := absent(mustScrape(t, p.summary), "interface_problem", "interface", f.netdev, "reason", "errors"); err != nil {
					t.Error(err)
				}
			}},
		{"errors-from-watch", "positive", "ping through the injected nobuf fault", "interface_problem{reason=errors} on /metrics within three watch intervals",
			func(t *testing.T) {
				if sent, received := pingPeer(t, peer, 100); received == 0 || received >= sent {
					t.Errorf("%d of %d replies; want some loss", received, sent)
				}
				waitMetrics(t, p.summary, 3*onDemandWatch+2*time.Second, func(s []metricSample) error {
					return want(s, 1, "interface_problem", "interface", f.netdev, "reason", "errors")
				})
			}},
		{"no-refresh", "negative", "the errors reason came from the watch tier", "no on-demand refresh was counted",
			func(t *testing.T) {
				if total := refreshTotal(mustScrape(t, p.collection)); total != 0 {
					t.Errorf("%v refreshes; the summary must not need one", total)
				}
			}},
		{"detail-agrees", "corner", "GET /metrics-all after the evidence", "receive over errors equals sysfs rx_over_errors; refreshes counted (cached when the watch poll is inside the TTL)",
			func(t *testing.T) {
				waitMetrics(t, f.url, 30*time.Second, func(s []metricSample) error {
					return sysfsEqual(s, f.netdev, "interface_receive_over_errors_total", "rx_over_errors", true)
				})
				if got := refreshTotal(mustScrape(t, p.collection)); got < 1 {
					t.Errorf("%v refreshes counted, want at least 1", got)
				}
			}},
	}
	for _, row := range rows {
		ok := t.Run(row.category+"/"+row.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			row.run(t)
		})
		if !ok {
			return
		}
	}
}
