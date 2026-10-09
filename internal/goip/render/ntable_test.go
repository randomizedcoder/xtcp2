package render

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ntableNow is the base-topology capture instant, recovered from the committed
// `-s` pcap and golden; with the arp_cache deltas below it renders the two
// config dates the ip_ntable_s golden carries.
var ntableNow = time.Date(2026, 10, 9, 16, 1, 41, 0, time.UTC)

var ntableNames = fakeNames{1: {name: "lo"}}

// arpBaseInfo is the arp_cache base message's decoded form, from the committed
// goldens. The config/stats are the `-s` pcap's values (see ndtmsg test).
func arpBaseInfo() xtcpnl.NeighTblInfo {
	return xtcpnl.NeighTblInfo{
		Family:        unix.AF_INET,
		Name:          "arp_cache",
		HasName:       true,
		Thresh1:       128,
		HasThresh1:    true,
		Thresh2:       512,
		HasThresh2:    true,
		Thresh3:       1024,
		HasThresh3:    true,
		GcInterval:    30000,
		HasGcInterval: true,
		Config: xtcpnl.NdtConfig{KeyLen: 4, EntrySize: 432, Entries: 9,
			LastFlush: 40566, LastRand: 4294432082, HashRnd: 3190857053, HashMask: 0x0f},
		HasConfig: true,
		Stats:     xtcpnl.NdtStats{Allocs: 9, HashGrows: 1, Lookups: 15, PeriodicGcRuns: 1},
		HasStats:  true,
		Parms: xtcpnl.NeighTblParms{
			Ifindex: 0,
			Refcnt:  1, HasRefcnt: true,
			ReachableTime: 28789, HasReachableTime: true,
			BaseReachableTime: 30000, HasBaseReachableTime: true,
			RetransTime: 1000, HasRetransTime: true,
			GcStaletime: 60000, HasGcStaletime: true,
			DelayProbeTime: 5000, HasDelayProbeTime: true,
			QueueLen: 101, HasQueueLen: true,
			AppProbes: 0, HasAppProbes: true,
			UcastProbes: 3, HasUcastProbes: true,
			McastProbes: 3, HasMcastProbes: true,
			McastReprobes: 0, HasMcastReprobes: true,
			AnycastDelay: 1000, HasAnycastDelay: true,
			ProxyDelay: 800, HasProxyDelay: true,
			ProxyQlen: 64, HasProxyQlen: true,
			Locktime: 1000, HasLocktime: true,
		},
		HasParms: true,
	}
}

// TestNeighTblViewText pins the text rendering, including the -s gating and the
// wall-clock config dates. The full-record rows are transcribed from the
// ip_ntable / ip_ntable_s goldens (capture); the sentinel and zero-delta rows
// assert the view contract on shapes the capture does not carry.
//
// go test ./internal/goip/render/ -run TestNeighTblViewText
func TestNeighTblViewText(t *testing.T) {
	const plainParms = "    refcnt 1 reachable 28789 base_reachable 30000 retrans 1000 \n" +
		"    gc_stale 60000 delay_probe 5000 queue 101 \n" +
		"    app_probes 0 ucast_probes 3 mcast_probes 3 mcast_reprobes 0 \n" +
		"    anycast_delay 1000 proxy_delay 800 proxy_queue 64 locktime 1000 \n"

	tests := []struct {
		description string
		info        xtcpnl.NeighTblInfo
		showStats   bool
		want        string
	}{
		{
			description: "positive: the base message without -s hides config and stats (capture)",
			info:        arpBaseInfo(),
			showStats:   false,
			want: "inet arp_cache \n" +
				"    thresh1 128 thresh2 512 thresh3 1024 gc_int 30000 \n" +
				plainParms + "\n",
		},
		{
			description: "positive: the base message with -s adds config and stats, dates from now (capture)",
			info:        arpBaseInfo(),
			showStats:   true,
			want: "inet arp_cache \n" +
				"    thresh1 128 thresh2 512 thresh3 1024 gc_int 30000 \n" +
				"    config key_len 4 entry_size 432 entries 9 \n" +
				"        last_flush 2026-10-09 16:01:01 last_rand 2026-08-20 23:07:49 \n" +
				"        hash_rnd 3190857053 hash_mask 0000000f hash_chain_gc 0 proxy_qlen 0 \n" +
				plainParms +
				"    stats allocs 9 destroys 0 hash_grows 1 \n" +
				"    res_failed 0 lookups 15 hits 0 \n" +
				"    rcv_probes_mcast 0 rcv_probes_ucast 0 \n" +
				"    periodic_gc_runs 1 forced_gc_runs 0 \n" +
				"    table_fulls 0 \n\n",
		},
		{
			description: "corner: a device message prints the dev line and no thresholds (capture)",
			info: xtcpnl.NeighTblInfo{
				Family: unix.AF_INET, Name: "arp_cache", HasName: true,
				Parms: xtcpnl.NeighTblParms{
					Ifindex: 1,
					Refcnt:  1, HasRefcnt: true,
					ReachableTime: 28789, HasReachableTime: true,
					BaseReachableTime: 30000, HasBaseReachableTime: true,
					RetransTime: 1000, HasRetransTime: true,
				},
				HasParms: true,
			},
			want: "inet arp_cache \n" +
				"    dev lo \n" +
				"    refcnt 1 reachable 28789 base_reachable 30000 retrans 1000 \n" +
				"    \n    \n    \n\n",
		},
		{
			description: "corner: an uncached device index falls back to if%u (contract)",
			info: xtcpnl.NeighTblInfo{
				Family: unix.AF_INET6, Name: "ndisc_cache", HasName: true,
				Parms:    xtcpnl.NeighTblParms{Ifindex: 42},
				HasParms: true,
			},
			want: "inet6 ndisc_cache \n    dev if42 \n    \n    \n    \n    \n\n",
		},
		{
			description: "corner: an unknown family prints the ??? sentinel (contract)",
			info:        xtcpnl.NeighTblInfo{Family: 99},
			want:        "??? \n\n",
		},
		{
			description: "boundary: a zero last_flush delta renders (error), not a date (contract)",
			info: xtcpnl.NeighTblInfo{
				Family: unix.AF_INET, Name: "arp_cache", HasName: true,
				Config:    xtcpnl.NdtConfig{KeyLen: 4, LastFlush: 0, LastRand: 40566},
				HasConfig: true,
			},
			showStats: true,
			want: "inet arp_cache \n" +
				"    config key_len 4 entry_size 0 entries 0 \n" +
				"        last_flush (error) last_rand 2026-10-09 16:01:01 \n" +
				"        hash_rnd 0 hash_mask 00000000 hash_chain_gc 0 proxy_qlen 0 \n\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := NeighTblViewOf(tc.info, ntableNames, tc.showStats, ntableNow)
			if got := v.Text(); got != tc.want {
				t.Fatalf("Text() mismatch\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestNeighTblViewJSON pins the JSON divergences the text form cannot show: the
// renamed keys (gc_interval vs gc_int, key_length vs key_len), hash_mask as a
// `%#x` string rather than the text's zero-padded token, the -s gating, and the
// device `dev` key. Full structural parity is covered by the object-level
// jsonEquivalent row against the -j -p golden.
//
// go test ./internal/goip/render/ -run TestNeighTblViewJSON
func TestNeighTblViewJSON(t *testing.T) {
	marshal := func(t *testing.T, info xtcpnl.NeighTblInfo, showStats bool) map[string]any {
		t.Helper()
		v := NeighTblViewOf(info, ntableNames, showStats, ntableNow)
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("Unmarshal(%s): %v", raw, err)
		}
		return m
	}

	t.Run("positive: -s base object carries the renamed and string-typed keys", func(t *testing.T) {
		m := marshal(t, arpBaseInfo(), true)
		// gc_interval not gc_int; key_length not key_len.
		if _, ok := m["gc_interval"]; !ok {
			t.Error("missing gc_interval key")
		}
		if _, ok := m["gc_int"]; ok {
			t.Error("gc_int leaked into JSON (text-only token)")
		}
		if _, ok := m["key_length"]; !ok {
			t.Error("missing key_length key")
		}
		// hash_mask is a %#x string, not a number (print_color_0xhex).
		if hm, ok := m["hash_mask"].(string); !ok || hm != "0xf" {
			t.Errorf("hash_mask = %v (%T), want string 0xf", m["hash_mask"], m["hash_mask"])
		}
		// a bare-number field stays a number.
		if _, ok := m["entries"].(float64); !ok {
			t.Errorf("entries = %v (%T), want number", m["entries"], m["entries"])
		}
	})

	t.Run("corner: without -s the config and stats keys are absent", func(t *testing.T) {
		m := marshal(t, arpBaseInfo(), false)
		for _, k := range []string{"key_length", "hash_mask", "allocs", "last_flush"} {
			if _, ok := m[k]; ok {
				t.Errorf("key %q present without -s", k)
			}
		}
		// params (always shown) and the top-level keys are still there.
		if _, ok := m["refcnt"]; !ok {
			t.Error("missing refcnt (params are not -s gated)")
		}
	})

	t.Run("corner: a device message carries the resolved dev key", func(t *testing.T) {
		m := marshal(t, xtcpnl.NeighTblInfo{
			Family: unix.AF_INET, Name: "arp_cache", HasName: true,
			Parms:    xtcpnl.NeighTblParms{Ifindex: 1, Refcnt: 1, HasRefcnt: true},
			HasParms: true,
		}, false)
		if dev, ok := m["dev"].(string); !ok || dev != "lo" {
			t.Errorf("dev = %v, want lo", m["dev"])
		}
	})

	t.Run("corner: a base message has no dev key", func(t *testing.T) {
		m := marshal(t, arpBaseInfo(), false)
		if _, ok := m["dev"]; ok {
			t.Error("base message (ifindex 0) emitted a dev key")
		}
	})
}

// TestNeighTblViewJSONKeyOrder asserts the top-level keys land in print order,
// since map iteration in the test above cannot see order.
//
// go test ./internal/goip/render/ -run TestNeighTblViewJSONKeyOrder
func TestNeighTblViewJSONKeyOrder(t *testing.T) {
	v := NeighTblViewOf(arpBaseInfo(), ntableNames, true, ntableNow)
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	// family precedes name precedes gc_interval precedes config precedes params
	// precedes stats, as print_ntable emits them.
	order := []string{`"family"`, `"name"`, `"thresh1"`, `"gc_interval"`,
		`"key_length"`, `"last_flush"`, `"hash_mask"`, `"refcnt"`, `"allocs"`, `"table_fulls"`}
	prev := -1
	for _, key := range order {
		at := strings.Index(s, key)
		if at < 0 {
			t.Fatalf("key %s absent from %s", key, s)
		}
		if at < prev {
			t.Errorf("key %s at %d is before the previous key (want print order)\n%s", key, at, s)
		}
		prev = at
	}
}
