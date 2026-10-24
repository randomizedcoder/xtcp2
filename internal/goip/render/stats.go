package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// IfStatsView is one record of `ip stats show group link`, transcribed from
// ipstats_process_ifsm (ip/ipstats.c:765-779) and the link leaf
// (ipstats_stat_desc_show_link :644-650): the per-interface link stats64 block
// under an `ifindex: ifname: group link` header. goip grounds only the link
// group, so Stats is the one rendered payload. Extended is `show_stats > 1`,
// which adds the RX-errors/TX-errors lines to the block.
type IfStatsView struct {
	Ifindex  uint32
	IfName   string
	Group    string
	Stats    xtcpnl.RtnlLinkStats64
	Extended bool
}

// IfStatsViewOf builds a view from a decoded message. names resolves the ifindex
// via the bundled link dump (ll_index_to_name, :761).
func IfStatsViewOf(info xtcpnl.IfStatsInfo, names NameTab, extended bool) IfStatsView {
	return IfStatsView{
		Ifindex:  info.Ifindex,
		IfName:   names.IndexToName(int32(info.Ifindex)),
		Group:    LinkGroupCst,
		Stats:    info.Link64,
		Extended: extended,
	}
}

// Text reproduces one record's text form: the `%d:` ifindex (:769), the ` %s:`
// ifname (:772), the ` group link` selector (ipstats_show_group :735-736), a
// newline, then the stats64 block. The final newline is the print_nl that
// terminates every record (:778). The dump path emits one further newline per
// message (:862, a blank line between interfaces); the caller adds it — a point
// get has only this one.
func (v IfStatsView) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d:", v.Ifindex)
	b.WriteByte(' ')
	b.WriteString(v.IfName)
	b.WriteByte(':')
	b.WriteString(" group ")
	b.WriteString(v.Group)
	b.WriteByte('\n')
	b.WriteString(linkStatsText(v.Stats, v.Extended))
	b.WriteByte('\n')
	return b.String()
}

// MarshalJSON emits the record object in print order: ifindex (number), ifname
// (string), group (string), then the stats64 object (rx/tx). Only the short
// stats64 keys are grounded this PR; the extended `-s` JSON keys (length_errors,
// …) are a documented follow-up and obj_stats refuses a `-j -s` request rather
// than emit a short object under an extended command.
func (v IfStatsView) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Ifindex uint32        `json:"ifindex"`
		Ifname  string        `json:"ifname"`
		Group   string        `json:"group"`
		Stats64 LinkStatsJSON `json:"stats64"`
	}{
		Ifindex: v.Ifindex,
		Ifname:  v.IfName,
		Group:   v.Group,
		Stats64: linkStatsJSONOf(v.Stats),
	})
}

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
