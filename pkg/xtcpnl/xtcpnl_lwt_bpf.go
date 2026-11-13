package xtcpnl

import "errors"

// BpfProgRef is a loaded BPF program as bpf_lwt_cb (ip/iproute_lwtunnel.c)
// writes it into an LWT_BPF_IN, OUT or XMIT nest, or into seg6local's
// SEG6_LOCAL_BPF: LWT_BPF_PROG_FD, then LWT_BPF_PROG_NAME with its NUL.
type BpfProgRef struct {
	FD   uint32
	Name string // bpf_load_common's "<basename>:[<sec>]"
}

// BpfEncapItem is one of parse_encap_bpf's keywords, in the order read.
// Kind is LwtBpfIn, LwtBpfOut or LwtBpfXmit, with Prog, or
// LwtBpfXmitHeadroom, with Headroom. A hook given twice is written twice.
type BpfEncapItem struct {
	Kind     uint16
	Prog     BpfProgRef
	Headroom uint32
}

// BpfEncap is `encap bpf …`: the items as read. parse_encap_bpf writes
// only the first headroom, which is the caller's to drop.
type BpfEncap struct {
	Items []BpfEncapItem
}

// ErrBpfEncap is a bpf encap item of no known kind.
var ErrBpfEncap = errors.New("xtcpnl: a bpf encap has in, out, xmit and headroom items")

func putBpfEncap(ab *AttrBuilder, e *BpfEncap) error {
	for i := range e.Items {
		it := &e.Items[i]
		switch it.Kind {
		case LwtBpfIn, LwtBpfOut, LwtBpfXmit:
			if err := putBpfProg(ab, it.Kind, it.Prog); err != nil {
				return err
			}
		case LwtBpfXmitHeadroom:
			if err := ab.PutU32(LwtBpfXmitHeadroom, it.Headroom); err != nil {
				return err
			}
		default:
			return ErrBpfEncap
		}
	}
	return nil
}

// putBpfProg is lwt_parse_bpf's rta_nest (NLA_F_NESTED) around bpf_lwt_cb.
func putBpfProg(ab *AttrBuilder, kind uint16, p BpfProgRef) error {
	return putNested(ab, kind, func() error {
		if err := ab.PutU32(LwtBpfProgFD, p.FD); err != nil {
			return err
		}
		return ab.PutBytes(LwtBpfProgName, append([]byte(p.Name), 0))
	})
}
