package render

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// mrouteOffloadCst is RTNH_F_OFFLOAD, which x/sys/unix does not define;
// RTNH_F_UNRESOLVED it does.
const mrouteOffloadCst = 8

// MrouteOptions are the three things print_mroute reads besides the message:
// show_stats, show_details, and whether a `table` filter is set, which
// suppresses the table token.
type MrouteOptions struct {
	Stats       bool
	Details     bool
	TableFilter bool
}

// MrouteView is one line of `ip mroute show`, print_mroute
// (ip/ipmroute.c:49-210), its fields in print order. The optional ones are
// pointers, absent when print_mroute prints nothing for them.
type MrouteView struct {
	Deleted   *bool        `json:"deleted,omitempty"`
	Src       string       `json:"src"`
	Dst       string       `json:"dst"`
	Iif       string       `json:"iif"`
	Multipath *[]MrouteOif `json:"multipath,omitempty"`
	State     string       `json:"state"`
	// Offload is print_null's JSON null, present when RTNH_F_OFFLOAD is.
	Offload json.RawMessage `json:"offload,omitempty"`
	Packets *uint64         `json:"packets,omitempty"`
	Bytes   *uint64         `json:"bytes,omitempty"`
	WrongIf *uint64         `json:"wrong_if,omitempty"`
	// Expires is the age in seconds, print_float's "%g" in JSON.
	Expires json.RawMessage `json:"expires,omitempty"`
	Table   *string         `json:"table,omitempty"`

	iifSet bool
	age    float64
}

// MrouteOif is one rtnexthop of RTA_MULTIPATH: the output interface, and the
// TTL threshold when it is above 1.
type MrouteOif struct {
	Oif string `json:"oif"`
	TTL *uint8 `json:"ttl,omitempty"`
}

// MrouteViewOf builds the view of one RTNL_FAMILY_IPMR or IP6MR route.
// Addresses are rendered in get_real_family's family, AF_INET or AF_INET6.
func MrouteViewOf(ri *xtcpnl.RouteInfo, deleted bool, names NameTab, o MrouteOptions) MrouteView {
	fam := uint8(unix.AF_INET)
	if ri.Family == xtcpnl.RtnlFamilyIp6mrCst {
		fam = unix.AF_INET6
	}
	v := MrouteView{Src: mrouteAddr(ri.Src, fam), Dst: mrouteAddr(ri.Dst, fam), State: "resolved"}
	if deleted {
		t := true
		v.Deleted = &t
	}
	v.Iif = unresolvedCst
	if ri.Iif != 0 {
		v.iifSet = true
		v.Iif = names.IndexToName(int32(ri.Iif))
	}
	if ri.HasMultipath {
		oifs := make([]MrouteOif, 0, len(ri.Multipath))
		for _, nh := range ri.Multipath {
			oif := MrouteOif{Oif: names.IndexToName(nh.Ifindex)}
			if nh.Hops > 1 {
				h := nh.Hops
				oif.TTL = &h
			}
			oifs = append(oifs, oif)
		}
		v.Multipath = &oifs
	}
	if ri.Flags&unix.RTNH_F_UNRESOLVED != 0 {
		v.State = unresolvedCst
	}
	if ri.Flags&mrouteOffloadCst != 0 {
		v.Offload = json.RawMessage("null")
	}
	if o.Stats && ri.MfcStats != nil {
		s := *ri.MfcStats
		v.Packets, v.Bytes = &s.Packets, &s.Bytes
		if s.WrongIf != 0 {
			v.WrongIf = &s.WrongIf
		}
	}
	if o.Stats && ri.HasExpires {
		// __jiffies_to_tv: 10000 microseconds a tick, so ticks/100 seconds.
		us := 10000 * ri.Expires
		v.age = float64(us/1000000) + float64(us%1000000)/1000000
		v.Expires = json.RawMessage(strconv.FormatFloat(v.age, 'g', 6, 64))
	}
	if ri.Table != 0 && (ri.Table != unix.RT_TABLE_MAIN || o.Details) && !o.TableFilter {
		t := routeTableName(ri.Table)
		v.Table = &t
	}
	return v
}

// mrouteAddr is rt_addr_n2a_r, or "unknown" when the attribute is absent.
func mrouteAddr(b []byte, fam uint8) string {
	if b == nil {
		return "unknown"
	}
	return addrString(b, fam)
}

// Text is the line print_mroute writes, newline terminated.
func (v *MrouteView) Text() string {
	var sb strings.Builder
	if v.Deleted != nil {
		sb.WriteString("Deleted ")
	}
	fmt.Fprintf(&sb, "%-32s Iif: ", "("+v.Src+","+v.Dst+")")
	if v.iifSet {
		fmt.Fprintf(&sb, "%-10s ", v.Iif)
	} else {
		sb.WriteString(v.Iif + " ")
	}
	if v.Multipath != nil {
		for i, oif := range *v.Multipath {
			if i == 0 {
				sb.WriteString("Oifs: ")
			}
			sb.WriteString(oif.Oif)
			if oif.TTL != nil {
				fmt.Fprintf(&sb, "(ttl %d) ", *oif.TTL)
			} else {
				sb.WriteString(" ")
			}
		}
	}
	sb.WriteString(" State: " + v.State)
	if v.Offload != nil {
		sb.WriteString(" offload")
	}
	if v.Packets != nil {
		fmt.Fprintf(&sb, "\n  %d packets, %d bytes", *v.Packets, *v.Bytes)
		if v.WrongIf != nil {
			fmt.Fprintf(&sb, ", %d arrived on wrong iif.", *v.WrongIf)
		}
	}
	if v.Expires != nil {
		fmt.Fprintf(&sb, ", Age %.2f", v.age)
	}
	if v.Table != nil {
		sb.WriteString(" Table: " + *v.Table)
	}
	sb.WriteString("\n")
	return sb.String()
}
