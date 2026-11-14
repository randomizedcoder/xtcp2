package render

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// IfStatsHwSInfoText is ipstats_print's record (ip/ipstats.c), which `ip
// monitor` prints for an RTM_NEWSTATS: `%d:`, ` %s:`, then the offload
// group's hw_s_info body, the one ipstats_print enables, then print_nl. The
// body is print_nl, then `    l3_stats`, ` on` or ` off` for the request and
// ` used on` or ` used off`, each printed only when its byte is there; a
// message with no hw_s_info nest is the header line alone.
//
// ipstats_dissect_01 takes 0 and 1 only. Any other byte is a line on
// stderr, errOut here; a bad USED byte then prints nothing, and a bad
// REQUEST fails the body, so the record stops after the header's newline,
// with no second one.
func IfStatsHwSInfoText(info *xtcpnl.IfStatsInfo, names NameTab) (out, errOut string) {
	var b, e strings.Builder
	fmt.Fprintf(&b, "%d: %s:", info.Ifindex, names.IndexToName(int32(info.Ifindex)))
	if !info.HasHwSInfo {
		b.WriteByte('\n')
		return b.String(), ""
	}
	b.WriteByte('\n')
	h := &info.HwSInfo
	if h.HasL3 {
		request, rok := hwSInfoOnOff(&e, "request", h.L3Request, h.HasL3Request)
		if h.HasL3Request && !rok {
			return b.String(), e.String()
		}
		used, _ := hwSInfoOnOff(&e, "used", h.L3Used, h.HasL3Used)
		b.WriteString("    l3_stats")
		if request != "" {
			b.WriteString(" " + request)
		}
		if used != "" {
			b.WriteString(" used " + used)
		}
	}
	b.WriteByte('\n')
	return b.String(), e.String()
}

// hwSInfoOnOff is ipstats_dissect_01 and print_on_off: "on" for 1, "off"
// for 0, and "" for an absent byte or, with its stderr line, any other.
func hwSInfoOnOff(e *strings.Builder, what string, v uint8, present bool) (string, bool) {
	switch {
	case !present:
		return "", false
	case v <= 1:
		return onOff(v == 1), true
	}
	fmt.Fprintf(e, "Invalid value for %s: expected 0 or 1, got %d.\n", what, v)
	return "", false
}
