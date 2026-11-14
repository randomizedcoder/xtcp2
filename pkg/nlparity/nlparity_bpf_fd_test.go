package nlparity

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestZeroLwtBpfFDs is the LWT_BPF_PROG_FD normalization: every bpf
// program fd in a route's, a multipath hop's or a nexthop's encap is
// zeroed, and nothing else changes.
func TestZeroLwtBpfFDs(t *testing.T) {
	const name = "lwt_bpf.o:[lwt_in]"
	bpf := func(fd uint32) xtcpnl.LwtEncap {
		return xtcpnl.LwtEncap{Bpf: &xtcpnl.BpfEncap{Items: []xtcpnl.BpfEncapItem{
			{Kind: xtcpnl.LwtBpfIn, Prog: xtcpnl.BpfProgRef{FD: fd, Name: name}},
			{Kind: xtcpnl.LwtBpfXmitHeadroom, Headroom: fd},
			{Kind: xtcpnl.LwtBpfXmit, Prog: xtcpnl.BpfProgRef{FD: fd, Name: name}},
		}}}
	}
	local := func(fd uint32) xtcpnl.LwtEncap {
		return xtcpnl.LwtEncap{Seg6Local: &xtcpnl.Seg6LocalEncap{Items: []xtcpnl.Seg6LocalItem{
			{Kind: xtcpnl.Seg6LocalAction, U32: 15},
			{Kind: xtcpnl.Seg6LocalBPF, Prog: xtcpnl.BpfProgRef{FD: fd, Name: name}},
		}}}
	}
	// An encap of another type, whose attribute 1 is a 4-byte label.
	mpls := xtcpnl.LwtEncap{MPLS: &xtcpnl.MplsEncap{Dst: []byte{0, 0, 0x41, 0}}}
	route := func(t *testing.T, e xtcpnl.LwtEncap) []byte {
		t.Helper()
		return msg(t, unix.RTM_NEWROUTE, unix.SizeofRtMsg, func(ab *xtcpnl.AttrBuilder) error {
			return xtcpnl.PutLwtEncap(ab, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE, e)
		})
	}
	nexthop := func(t *testing.T, e xtcpnl.LwtEncap) []byte {
		t.Helper()
		return msg(t, unix.RTM_NEWNEXTHOP, xtcpnl.NhMsgSizeCst, func(ab *xtcpnl.AttrBuilder) error {
			return xtcpnl.PutLwtEncap(ab, xtcpnl.NhaEncap, xtcpnl.NhaEncapType, e)
		})
	}
	multipath := func(t *testing.T, e xtcpnl.LwtEncap) []byte {
		t.Helper()
		return msg(t, unix.RTM_NEWROUTE, unix.SizeofRtMsg, func(ab *xtcpnl.AttrBuilder) error {
			hop := xtcpnl.NewAttrBuilder(make([]byte, 512))
			if err := xtcpnl.PutLwtEncap(&hop, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE, e); err != nil {
				return err
			}
			// Two hops, the first plain, the second with the encap.
			var mp []byte
			for _, attrs := range [][]byte{nil, hop.Bytes()} {
				rtnh := make([]byte, rtnexthopLenCst)
				binary.LittleEndian.PutUint16(rtnh, uint16(rtnexthopLenCst+len(attrs)))
				mp = append(append(mp, rtnh...), attrs...)
			}
			return ab.PutBytes(unix.RTA_MULTIPATH, mp)
		})
	}
	link := func(t *testing.T, e xtcpnl.LwtEncap) []byte {
		t.Helper()
		return msg(t, unix.RTM_NEWLINK, unix.SizeofIfInfomsg, func(ab *xtcpnl.AttrBuilder) error {
			return xtcpnl.PutLwtEncap(ab, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE, e)
		})
	}
	tests := []struct {
		description string
		build       func(*testing.T, xtcpnl.LwtEncap) []byte
		in          xtcpnl.LwtEncap
		expected    xtcpnl.LwtEncap // the same message built with these fds
	}{
		{description: "positive: a route's bpf hooks, the headroom kept", build: route, in: bpf(7), expected: zeroedBpf(bpf(7))},
		{description: "positive: a nexthop's bpf hooks", build: nexthop, in: bpf(9), expected: zeroedBpf(bpf(9))},
		{description: "positive: a multipath hop's bpf hooks", build: multipath, in: bpf(5), expected: zeroedBpf(bpf(5))},
		{description: "positive: seg6local's End.BPF endpoint", build: route, in: local(6), expected: local(0)},
		{description: "positive: a nexthop's seg6local endpoint", build: nexthop, in: local(6), expected: local(0)},
		{description: "boundary: an fd already 0", build: route, in: bpf(0), expected: bpf(0)},
		{description: "negative: an mpls encap is left alone", build: route, in: mpls, expected: mpls},
		{description: "negative: a link message is left alone", build: link, in: bpf(7), expected: bpf(7)},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			got := ZeroLwtBpfFDs(tt.build(t, tt.in))
			if want := tt.build(t, tt.expected); !bytes.Equal(got, want) {
				t.Errorf("got  %x\nwant %x", got, want)
			}
		})
	}
	t.Run("corner: a truncated message is copied, not a panic", func(t *testing.T) {
		m := route(t, bpf(7))
		for n := range len(m) {
			if got := ZeroLwtBpfFDs(m[:n]); len(got) != n {
				t.Fatalf("len %d: got %d bytes", n, len(got))
			}
		}
	})
	t.Run("negative: a bpf-shaped nest in an encap of another type is left alone", func(t *testing.T) {
		m := msg(t, unix.RTM_NEWROUTE, unix.SizeofRtMsg, func(ab *xtcpnl.AttrBuilder) error {
			enc, err := ab.BeginNest(unix.RTA_ENCAP | unix.NLA_F_NESTED)
			if err != nil {
				return err
			}
			hook, err := ab.BeginNest(xtcpnl.LwtBpfIn | unix.NLA_F_NESTED)
			if err != nil {
				return err
			}
			if err := ab.PutU32(xtcpnl.LwtBpfProgFD, 7); err != nil {
				return err
			}
			if err := ab.EndNest(hook); err != nil {
				return err
			}
			if err := ab.EndNest(enc); err != nil {
				return err
			}
			return ab.PutU16(unix.RTA_ENCAP_TYPE, unix.LWTUNNEL_ENCAP_IP6)
		})
		if got := ZeroLwtBpfFDs(m); !bytes.Equal(got, m) {
			t.Errorf("got  %x\nwant %x", got, m)
		}
	})
	t.Run("positive: NormalizeMsg zeroes the fd", func(t *testing.T) {
		m, want := route(t, bpf(7)), route(t, zeroedBpf(bpf(7)))
		n := xtcpnl.NlMsgHdrSizeCst
		got := NormalizeMsg(Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWROUTE}, Body: m[n:]})
		if !bytes.Equal(got.Body, want[n:]) {
			t.Errorf("got  %x\nwant %x", got.Body, want[n:])
		}
	})
	t.Run("negative: the input is not modified", func(t *testing.T) {
		m := route(t, bpf(7))
		orig := bytes.Clone(m)
		ZeroLwtBpfFDs(m)
		if !bytes.Equal(m, orig) {
			t.Error("ZeroLwtBpfFDs modified its input")
		}
	})
}

// zeroedBpf is e with its hooks' fds zeroed and its headroom kept.
func zeroedBpf(e xtcpnl.LwtEncap) xtcpnl.LwtEncap {
	items := make([]xtcpnl.BpfEncapItem, len(e.Bpf.Items))
	copy(items, e.Bpf.Items)
	for i := range items {
		items[i].Prog.FD = 0
	}
	return xtcpnl.LwtEncap{Bpf: &xtcpnl.BpfEncap{Items: items}}
}

// msg is a request of type typ: the nlmsghdr, a zeroed family header of
// hdrLen bytes, and the attributes put writes.
func msg(t *testing.T, typ uint16, hdrLen int, put func(*xtcpnl.AttrBuilder) error) []byte {
	t.Helper()
	ab := xtcpnl.NewAttrBuilder(make([]byte, 1024))
	if err := put(&ab); err != nil {
		t.Fatal(err)
	}
	body := append(make([]byte, hdrLen), ab.Bytes()...)
	h := make([]byte, xtcpnl.NlMsgHdrSizeCst, xtcpnl.NlMsgHdrSizeCst+len(body))
	binary.LittleEndian.PutUint32(h, uint32(len(h)+len(body)))
	binary.LittleEndian.PutUint16(h[4:], typ)
	return append(h, body...)
}
