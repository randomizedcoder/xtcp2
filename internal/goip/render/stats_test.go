package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestIfStatsViewText pins the per-record text framing of `ip stats show group
// link` against the committed ip_stats golden's lo record: the `ifindex: ifname:
// group link` header, the short stats64 block, and the single trailing newline
// that terminates a record (the dump's blank separator is the caller's, not
// this). The name resolution follows the lltab rules: a cached index renders its
// name, a miss renders `if%u`, and index 0 renders `*`.
//
// go test ./internal/goip/render/ -run TestIfStatsViewText
func TestIfStatsViewText(t *testing.T) {
	zero := func(w int) string { return sp(w-1) + "0 " }
	block := strings.Join([]string{
		"    RX:  bytes packets errors dropped  missed   mcast" + sp(11),
		"    " + zero(10) + zero(7) + zero(6) + zero(7) + zero(7) + zero(7),
		"    TX:  bytes packets errors dropped carrier collsns" + sp(11),
		"    " + zero(10) + zero(7) + zero(6) + zero(7) + zero(7) + zero(7),
	}, "\n")

	names := fakeNames{1: {name: "lo"}}

	t.Run("short lo record, byte-exact vs the golden", func(t *testing.T) {
		v := IfStatsViewOf(xtcpnl.IfStatsInfo{Ifindex: 1}, names, false)
		want := "1: lo: group link\n" + block + "\n"
		if got := v.Text(); got != want {
			t.Errorf("text mismatch\n got %q\nwant %q", got, want)
		}
	})

	t.Run("uncached ifindex renders if%u", func(t *testing.T) {
		v := IfStatsViewOf(xtcpnl.IfStatsInfo{Ifindex: 42}, names, false)
		if got := v.Text(); !strings.HasPrefix(got, "42: if42: group link\n") {
			t.Errorf("want `42: if42:` header, got %q", got[:min(len(got), 30)])
		}
	})

	t.Run("ifindex 0 renders *", func(t *testing.T) {
		v := IfStatsViewOf(xtcpnl.IfStatsInfo{Ifindex: 0}, names, false)
		if got := v.Text(); !strings.HasPrefix(got, "0: *: group link\n") {
			t.Errorf("want `0: *:` header, got %q", got[:min(len(got), 30)])
		}
	})

	t.Run("extended record ends with the TX errors line", func(t *testing.T) {
		v := IfStatsViewOf(xtcpnl.IfStatsInfo{Ifindex: 1}, names, true)
		got := v.Text()
		ls := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		// header + 8 block lines (RX/RXerr/TX/TXerr pairs).
		if len(ls) != 9 {
			t.Fatalf("extended record = %d lines, want 9:\n%q", len(ls), got)
		}
		if !strings.HasPrefix(ls[7], "    TX errors:") {
			t.Errorf("line 8 is not the TX errors header: %q", ls[7])
		}
		if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
			t.Errorf("record must end in exactly one newline: %q", got[len(got)-3:])
		}
	})
}

// TestIfStatsViewJSON pins the JSON object: key order (ifindex, ifname, group,
// stats64{rx,tx}), the text-missed vs JSON-over_errors divergence, and the
// compressed omitempty guard. Grounded on the ip_stats_json golden's lo record.
//
// go test ./internal/goip/render/ -run TestIfStatsViewJSON
func TestIfStatsViewJSON(t *testing.T) {
	names := fakeNames{1: {name: "lo"}}

	t.Run("lo record, compact JSON matches the golden shape", func(t *testing.T) {
		v := IfStatsViewOf(xtcpnl.IfStatsInfo{Ifindex: 1}, names, false)
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		want := `{"ifindex":1,"ifname":"lo","group":"link",` +
			`"stats64":{"rx":{"bytes":0,"packets":0,"errors":0,"dropped":0,"over_errors":0,"multicast":0},` +
			`"tx":{"bytes":0,"packets":0,"errors":0,"dropped":0,"carrier_errors":0,"collisions":0}}}`
		if string(b) != want {
			t.Errorf("json mismatch\n got %s\nwant %s", b, want)
		}
	})

	t.Run("JSON rx.over_errors is rx_over_errors, not the text missed column", func(t *testing.T) {
		v := IfStatsViewOf(xtcpnl.IfStatsInfo{
			Ifindex: 1,
			Link64:  xtcpnl.RtnlLinkStats64{RxMissedErrors: 5, RxOverErrors: 7},
		}, names, false)
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), `"over_errors":7`) {
			t.Errorf("over_errors should be rx_over_errors (7), got %s", b)
		}
		if strings.Contains(string(b), `:5`) {
			t.Errorf("the text missed counter (5) leaked into JSON: %s", b)
		}
	})

	t.Run("compressed is omitted at zero and present when non-zero", func(t *testing.T) {
		vZero := IfStatsViewOf(xtcpnl.IfStatsInfo{Ifindex: 1}, names, false)
		if bz, _ := json.Marshal(vZero); strings.Contains(string(bz), "compressed") {
			t.Errorf("compressed must be omitted at zero: %s", bz)
		}
		vNon := IfStatsViewOf(xtcpnl.IfStatsInfo{
			Ifindex: 1,
			Link64:  xtcpnl.RtnlLinkStats64{RxCompressed: 3, TxCompressed: 4},
		}, names, false)
		b, _ := json.Marshal(vNon)
		if !strings.Contains(string(b), `"compressed":3`) || !strings.Contains(string(b), `"compressed":4`) {
			t.Errorf("non-zero compressed must appear on both rx and tx: %s", b)
		}
	})
}
