package render

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tcpmCookieMaxCst is process_msg's cap on the Fast Open cookie it prints.
const tcpmCookieMaxCst = 16

// tcpmMetricNames are metric_name[], the three metrics ip prints by name;
// the RTT four are folded into rtt and rttvar.
var tcpmMetricNames = [xtcpnl.TCPMetricCountCst]string{
	xtcpnl.TCPMetricSsthresh:   "ssthresh",
	xtcpnl.TCPMetricCwnd:       "cwnd",
	xtcpnl.TCPMetricReordering: "reordering",
}

// TCPMetricsView is one entry of `ip tcp_metrics show`, process_msg
// (ip/tcp_metrics.c): the line, and in JSON the object, whose members come
// in the line's order.
type TCPMetricsView struct {
	m xtcpnl.TCPMetrics
}

// TCPMetricsViewOf wraps a decoded entry the caller has filtered as
// process_msg filters it, so it has a destination.
func TCPMetricsViewOf(m *xtcpnl.TCPMetrics) TCPMetricsView {
	return TCPMetricsView{m: *m}
}

// tcpmHost is format_host without -r: inet_ntop of the address.
func tcpmHost(a netip.Addr) string {
	if a.Is4() {
		return a.String()
	}
	return inet6Ntop(a.As16())
}

// rtts is print_tcp_metrics' rtt and rttvar in usecs: the msecs value scaled
// as the kernel stores it, unless the usecs one is present, which wins
// whatever its order. Zero is not printed.
func (v *TCPMetricsView) rtts() (rtt, rttvar uint64) {
	ms := v.m.Metrics
	if p := ms[xtcpnl.TCPMetricRTT]; p != nil {
		rtt = (uint64(*p) * 1000) >> 3
	}
	if p := ms[xtcpnl.TCPMetricRTTVar]; p != nil {
		rttvar = (uint64(*p) * 1000) >> 2
	}
	if p := ms[xtcpnl.TCPMetricRTTUs]; p != nil {
		rtt = uint64(*p) >> 3
	}
	if p := ms[xtcpnl.TCPMetricRTTVarUs]; p != nil {
		rttvar = uint64(*p) >> 2
	}
	return rtt, rttvar
}

// cookie is the Fast Open cookie's first 16 bytes in hex.
func (v *TCPMetricsView) cookie() string {
	c := v.m.FopenCookie
	if len(c) > tcpmCookieMaxCst {
		c = c[:tcpmCookieMaxCst]
	}
	return hex.EncodeToString(c)
}

// twTs is the TIME-WAIT timestamp pair, tsval/stamp, tsval 0 when absent.
func (v *TCPMetricsView) twTs() string {
	var tsval uint32
	if v.m.TwTsval != nil {
		tsval = *v.m.TwTsval
	}
	return fmt.Sprintf("%d/%d", tsval, *v.m.TwTsStamp)
}

// Text is the line process_msg prints, newline terminated.
func (v *TCPMetricsView) Text() string {
	var b strings.Builder
	b.WriteString(tcpmHost(v.m.Addr))
	if v.m.Age != nil {
		fmt.Fprintf(&b, " age %.3fsec", float64(*v.m.Age)/1000)
	}
	if v.m.TwTsStamp != nil {
		b.WriteString(" tw_ts " + v.twTs() + " ago")
	}
	if v.m.HasVals {
		for i, name := range tcpmMetricNames {
			if p := v.m.Metrics[i]; p != nil && name != "" {
				fmt.Fprintf(&b, " %s %d", name, *p)
			}
		}
		rtt, rttvar := v.rtts()
		if rtt != 0 {
			fmt.Fprintf(&b, " rtt %dus", rtt)
		}
		if rttvar != 0 {
			fmt.Fprintf(&b, " rttvar %dus", rttvar)
		}
	}
	if v.m.FopenMSS != nil {
		fmt.Fprintf(&b, " fo_mss %d", *v.m.FopenMSS)
	}
	if v.m.FopenSynDrops != nil {
		fmt.Fprintf(&b, " fo_syn_drops %d/%.3fusec ago", *v.m.FopenSynDrops, v.synDropTS())
	}
	if v.m.FopenCookie != nil {
		b.WriteString(" fo_cookie " + v.cookie())
	}
	if v.m.Saddr.IsValid() {
		b.WriteString(" source " + tcpmHost(v.m.Saddr))
	}
	b.WriteByte('\n')
	return b.String()
}

// synDropTS is the last SYN drop's stamp as process_msg divides it.
func (v *TCPMetricsView) synDropTS() float64 {
	if v.m.FopenSynDropTS == nil {
		return 0
	}
	return float64(*v.m.FopenSynDropTS) / 1000000
}

// jsonFloat is print_float's JSON, jsonw_float's %g.
func jsonFloat(f float64) json.RawMessage {
	return json.RawMessage(strconv.FormatFloat(f, 'g', 6, 64))
}

// MarshalJSON is the entry's object: dst, then each member the line prints,
// in its order. The metrics are numbers under their names, rtt and rttvar
// seconds; the Fast Open MSS is under fopen_miss, ip's spelling.
func (v TCPMetricsView) MarshalJSON() ([]byte, error) {
	var w jsonObject
	w.member("dst", tcpmHost(v.m.Addr))
	if v.m.Age != nil {
		w.member("age", jsonFloat(float64(*v.m.Age)/1000))
	}
	if v.m.TwTsStamp != nil {
		w.member("tw_ts_stamp", v.twTs())
	}
	if v.m.HasVals {
		for i, name := range tcpmMetricNames {
			if p := v.m.Metrics[i]; p != nil && name != "" {
				w.member(name, *p)
			}
		}
		rtt, rttvar := v.rtts()
		if rtt != 0 {
			w.member("rtt", jsonFloat(float64(rtt)/1000000))
		}
		if rttvar != 0 {
			w.member("rttvar", jsonFloat(float64(rttvar)/1000000))
		}
	}
	if v.m.FopenMSS != nil {
		w.member("fopen_miss", *v.m.FopenMSS)
	}
	if v.m.FopenSynDrops != nil {
		w.member("fopen_syn_drops", *v.m.FopenSynDrops)
		w.member("fopen_syn_drop_ts", jsonFloat(v.synDropTS()))
	}
	if v.m.FopenCookie != nil {
		w.member("fo_cookie", v.cookie())
	}
	if v.m.Saddr.IsValid() {
		w.member("source", tcpmHost(v.m.Saddr))
	}
	return w.bytes()
}
