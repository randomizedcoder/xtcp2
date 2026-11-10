package render

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// MaddrEntry is ipmaddr.c's struct ma_info: one group a device has joined.
// Family is AF_PACKET for a link-layer group from /proc/net/dev_mcast, else
// AF_INET or AF_INET6.
type MaddrEntry struct {
	Index  int32
	Name   string
	Family uint8
	Addr   []byte
	Users  uint32
	Static bool // dev_mcast's global_use, printed as "static"
}

// MaddrText is print_mlist's text: a "%d:\t%s" header each time the index
// changes, then a tab-led line per group. goip refuses -o for maddress, so
// the header is never repeated per line.
func MaddrText(list []MaddrEntry) string {
	var b strings.Builder
	cur := int32(0)
	for _, m := range list {
		if m.Index != cur {
			fmt.Fprintf(&b, "%d:\t%s\n", m.Index, m.Name)
			cur = m.Index
		}
		b.WriteByte('\t')
		if m.Family == unix.AF_PACKET {
			b.WriteString("link  " + xtcpnl.LLAddrN2A(m.Addr, 0))
		} else {
			fmt.Fprintf(&b, "%-5s %s", familyName(m.Family), addrString(m.Addr, m.Family))
		}
		if m.Users != 1 {
			fmt.Fprintf(&b, " users %d", m.Users)
		}
		if m.Static {
			b.WriteString(" static")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// MaddrJSON is print_mlist under -j: an array of {ifindex, ifname, maddr},
// one per run of an index, with "[]" for no groups at all.
func MaddrJSON(list []MaddrEntry) ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('[')
	for i := 0; i < len(list); {
		j := i
		var groups bytes.Buffer
		for ; j < len(list) && list[j].Index == list[i].Index; j++ {
			b, err := maddrGroupJSON(list[j])
			if err != nil {
				return nil, err
			}
			if j > i {
				groups.WriteByte(',')
			}
			groups.Write(b)
		}
		var w jsonObject
		w.member("ifindex", list[i].Index)
		w.member("ifname", list[i].Name)
		obj, err := w.bytes()
		if err != nil {
			return nil, err
		}
		if i > 0 {
			out.WriteByte(',')
		}
		// The maddr array goes in before the object's closing brace.
		out.Write(obj[:len(obj)-1])
		out.WriteString(`,"maddr":[`)
		out.Write(groups.Bytes())
		out.WriteString("]}")
		i = j
	}
	out.WriteString("]\n")
	return out.Bytes(), nil
}

// maddrGroupJSON is print_maddr's object: "link", or "family" and
// "address", then "users" when not 1 and "features" when static.
func maddrGroupJSON(m MaddrEntry) ([]byte, error) {
	var w jsonObject
	if m.Family == unix.AF_PACKET {
		w.member("link", xtcpnl.LLAddrN2A(m.Addr, 0))
	} else {
		w.member("family", familyName(m.Family))
		w.member("address", addrString(m.Addr, m.Family))
	}
	if m.Users != 1 {
		w.member("users", m.Users)
	}
	if m.Static {
		w.member("features", "static")
	}
	return w.bytes()
}
