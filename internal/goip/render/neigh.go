package render

import (
	"encoding/hex"
	"net/netip"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// NeighView is the iproute2-compatible typed presentation of one neighbor.
type NeighView struct {
	Dst    string   `json:"dst"`
	Dev    string   `json:"dev"`
	LLAddr string   `json:"lladdr,omitempty"`
	State  []string `json:"state"`
}

func NeighViewOf(n xtcpnl.NeighInfo, names NameTab) NeighView {
	v := NeighView{Dev: names.IndexToName(n.Ifindex), State: neighStates(n.State)}
	if a, ok := netip.AddrFromSlice(n.Dst); ok {
		v.Dst = a.String()
	}
	if len(n.LLAddr) > 0 {
		s := hex.EncodeToString(n.LLAddr)
		var b strings.Builder
		for i := 0; i < len(s); i += 2 {
			if i > 0 {
				b.WriteByte(':')
			}
			b.WriteString(s[i : i+2])
		}
		v.LLAddr = b.String()
	}
	return v
}

func neighStates(state uint16) []string {
	s := xtcpnl.NudStateString(state)
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimPrefix(parts[i], "NUD_")
	}
	return parts
}

func (v NeighView) Text() string {
	var b strings.Builder
	b.WriteString(v.Dst)
	b.WriteString(" dev ")
	b.WriteString(v.Dev)
	b.WriteByte(' ')
	if v.LLAddr != "" {
		b.WriteString("lladdr ")
		b.WriteString(v.LLAddr)
		b.WriteByte(' ')
	}
	b.WriteString(strings.Join(v.State, " "))
	b.WriteString(" \n")
	return b.String()
}
