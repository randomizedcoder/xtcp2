package gotc

import (
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_csum.c print_csum (iproute2 v7.2.0).

// csumFlags is print_csum's order of the update flags.
var csumFlags = []struct {
	bit  uint32
	name string
}{
	{xtcpnl.TcaCsumUpdateFlagIPv4Hdr, "iph"},
	{xtcpnl.TcaCsumUpdateFlagICMP, "icmp"},
	{xtcpnl.TcaCsumUpdateFlagIGMP, "igmp"},
	{xtcpnl.TcaCsumUpdateFlagTCP, wordTCP},
	{xtcpnl.TcaCsumUpdateFlagUDP, wordUDP},
	{xtcpnl.TcaCsumUpdateFlagUDPLite, "udplite"},
	{xtcpnl.TcaCsumUpdateFlagSCTP, "sctp"},
}

// printCsum is print_csum: the flags are one comma-joined string, also in
// JSON, "?empty" with none (AC29); the control ends the line with _SL_ and
// index has no space after its tab.
func (r *runner) printCsum(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", "csum ", xtcpnl.TcActKindCsum)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaCsumMaxKnown)
	c, err := xtcpnl.ParseTcCsum(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !c.HasParms {
		r.errf("Missing csum parameters\n")
		return -1, nil
	}
	if c.ParmsShort(xtcpnl.TcCsumSizeCst) {
		return 0, actShort(xtcpnl.TcActKindCsum, c.ParmsLen)
	}
	var names []string
	for _, f := range csumFlags {
		if c.P.V&f.bit != 0 {
			names = append(names, f.name)
		}
	}
	s := strings.Join(names, ", ")
	if s == "" {
		s = "?empty"
	}
	o.str(printAny, colorNone, "csum", "("+s+") ", s)
	r.printActionControl("action ", c.P.Action, o.sl)
	r.actTail(c.P.TcGen, "\tindex ", c.TcActCommon)
	o.nl()
	return 0, nil
}
