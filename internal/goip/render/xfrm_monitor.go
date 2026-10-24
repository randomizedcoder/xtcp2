package render

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The printers ip/xfrm_monitor.c adds to ipxfrm.c's for `ip xfrm monitor`'s
// events. strxf_proto names a protocol by getprotobynumber, so the caller,
// which knows goip's names, passes the name.

// The XFRM_AE_* flags xfrm_ae_flags_print names.
const (
	xfrmAECRCst = 16 // XFRM_AE_CR, replay update
	xfrmAECECst = 32 // XFRM_AE_CE, timer expired
	xfrmAECUCst = 64 // XFRM_AE_CU, policy updated
)

// XfrmAcquire is xfrm_acquire_print: the protocol, the SPI when set or
// under -s, the selector in its family, else the policy's, else
// preferred_family, then the policy that asked, under "  policy ", and
// under -s its sequence number and algorithm masks.
func XfrmAcquire(acq *xtcpnl.XfrmUserAcquire, a *xtcpnl.XfrmAttrs, o *XfrmOpt) string {
	family := acq.Sel.Family
	if family == unix.AF_UNSPEC {
		family = acq.Policy.Sel.Family
	}
	if family == unix.AF_UNSPEC {
		family = uint16(o.Family)
	}
	var b strings.Builder
	b.WriteString("acquire proto " + XfrmProtoName(acq.ID.Proto) + " ")
	if o.Stats || acq.ID.SPI != 0 {
		fmt.Fprintf(&b, "spi 0x%08x", acq.ID.SPI)
		if o.Stats {
			fmt.Fprintf(&b, "(%d)", acq.ID.SPI)
		}
		b.WriteString(" ")
	}
	b.WriteString("\n")
	o.xfrmSelector(&b, &acq.Sel, family, "  sel ")
	b.WriteString(xfrmPolicyInfo(&acq.Policy, a, o, "    ", "  policy "))
	if o.Stats {
		fmt.Fprintf(&b, "  seq 0x%08d ", acq.Seq)
		fmt.Fprintf(&b, "enc-mask %08x auth-mask %08x comp-mask %08x", acq.EAlgos, acq.AAlgos, acq.CAlgos)
	}
	b.WriteString("\n")
	return b.String()
}

// XfrmFlushSA is xfrm_state_flush_print: the protocol strxf_xfrmproto
// names, or its number.
func XfrmFlushSA(proto uint8) string {
	return "Flushed state proto " + XfrmProtoName(proto) + "\n"
}

// XfrmFlushPolicy is xfrm_policy_flush_print: the policy type, when the
// message has one.
func XfrmFlushPolicy(ptype *uint8) string {
	s := "Flushed policy "
	if ptype != nil {
		s += "ptype " + xfrmPtypeName(*ptype) + " "
	}
	return s + "\n"
}

// xfrmUsersa is xfrm_usersa_print: the destination, the reqid in hex, the
// protocol strxf_proto names, and the SPI in hex.
func xfrmUsersa(b *strings.Builder, id *xtcpnl.XfrmUsersaID, reqid uint32, protoName string) {
	b.WriteString("dst " + xfrmAddrN2a(id.Family, id.Daddr) + " ")
	fmt.Fprintf(b, " reqid 0x%x", reqid)
	b.WriteString(" protocol " + protoName + " ")
	fmt.Fprintf(b, " SPI 0x%x", id.SPI)
}

// XfrmAevent is xfrm_ae_print: the flags in hex and those
// xfrm_ae_flags_print names, then the SA.
func XfrmAevent(ae *xtcpnl.XfrmAeventID, protoName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Async event  (0x%x) ", ae.Flags)
	if ae.Flags&xfrmAECRCst != 0 {
		b.WriteString(" replay update ")
	}
	if ae.Flags&xfrmAECECst != 0 {
		b.WriteString(" timer expired ")
	}
	if ae.Flags&xfrmAECUCst != 0 {
		b.WriteString(" policy updated ")
	}
	b.WriteString("\n\tsrc " + xfrmAddrN2a(ae.SAID.Family, ae.Saddr) + " ")
	xfrmUsersa(&b, &ae.SAID, ae.Reqid, protoName)
	b.WriteString("\n")
	return b.String()
}

// XfrmMapping is xfrm_mapping_print: the peer's old and new address and
// port, then the SA.
func XfrmMapping(m *xtcpnl.XfrmUserMapping, protoName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mapping change %s:%d -> %s:%d\n\t", xfrmAddrN2a(m.ID.Family, m.OldSaddr), m.OldSport,
		xfrmAddrN2a(m.ID.Family, m.NewSaddr), m.NewSport)
	xfrmUsersa(&b, &m.ID, m.Reqid, protoName)
	b.WriteString("\n")
	return b.String()
}
