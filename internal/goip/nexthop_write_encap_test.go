package goip

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestNexthopWriteEncapIP is ipnh_modify's encap for ip and ip6: NHA_ENCAP
// and NHA_ENCAP_TYPE where the keyword is, and addraw_l's 1024-byte bound
// over the request so far. The expected bytes are built from the uapi
// numbers. TestNexthopWriteEncapMpls keeps mpls's rows. LE2-T02.
//
// go test ./internal/goip/ -run TestNexthopWriteEncapIP
func TestNexthopWriteEncapIP(t *testing.T) {
	cat := func(a ...[]byte) []byte { return bytes.Join(a, nil) }
	nested := func(typ uint16, inner ...[]byte) []byte { return fraOf(typ|unix.NLA_F_NESTED, cat(inner...)...) }
	enc := func(typ byte, inner ...[]byte) []byte {
		return cat(nested(xtcpnl.NhaEncap, inner...), fraOf(xtcpnl.NhaEncapType, typ, 0))
	}
	id := fraOf(xtcpnl.LwtunnelIPID, binary.BigEndian.AppendUint64(nil, 1)...)
	dst4 := fraOf(xtcpnl.LwtunnelIPDst, 10, 0, 0, 1)
	gw4 := fraOf(xtcpnl.NhaGateway, 172, 16, 1, 2)
	v6a := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	gw6 := fraOf(xtcpnl.NhaGateway, 0x20, 0x01, 0x0d, 0xb8, 0, 0x91, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2)
	v4, v6 := nhmOf(unix.AF_INET, 0, 0), nhmOf(unix.AF_INET6, 0, 0)
	// geneve is `encap ip geneve_opts 0:0:<n zero bytes>`, 40 bytes and n.
	geneve := func(n int) []byte {
		return enc(2, nested(xtcpnl.LwtunnelIPOpts, nested(xtcpnl.LwtunnelIPOptsGeneve,
			fraOf(xtcpnl.LwtunnelIPOptGeneveClass, 0, 0), fraOf(xtcpnl.LwtunnelIPOptGeneveType, 0),
			fraOf(xtcpnl.LwtunnelIPOptGeneveData, make([]byte, n)...))))
	}
	geneveArgs := func(n int) string { return "encap ip geneve_opts 0:0:" + strings.Repeat("00", n) }
	const bound = "addraw_l ERROR: message exceeded bound of 1024"
	runNexthopKeywordRows(t, []nexthopKeywordRow{
		{description: "positive: ip's id and dst, then the gateway and dev", args: "id 1 encap ip id 1 dst 10.0.0.1 via 172.16.1.2 dev d0",
			nhm:      v4,
			attrs:    cat(fraOf(xtcpnl.NhaID, 1, 0, 0, 0), enc(2, id, dst4), gw4, fraOf(xtcpnl.NhaOIF, 3, 0, 0, 0)),
			resolved: []string{"d0"}},
		{description: "positive: ip6's tc, hoplimit and key, the flags last", args: "encap ip6 key dst 2001:db8:9::1 tc 0x10 hoplimit 64 via inet6 2001:db8:91::2",
			nhm: v6,
			attrs: cat(enc(4, fraOf(xtcpnl.LwtunnelIPDst, v6a...), fraOf(xtcpnl.LwtunnelIPTos, 0x10),
				fraOf(xtcpnl.LwtunnelIPTTL, 64), fraOf(xtcpnl.LwtunnelIPFlags, 0, 0x04)), gw6)},
		{description: "positive: the encap after the gateway, in argv order", args: "via 172.16.1.2 encap ip dst 10.0.0.1",
			nhm: v4, attrs: cat(gw4, enc(2, dst4))},
		{description: "positive: an erspan option", args: "encap ip id 1 erspan_opts 2:::3 via 172.16.1.2", nhm: v4,
			attrs: cat(enc(2, id, nested(xtcpnl.LwtunnelIPOpts, nested(xtcpnl.LwtunnelIPOptsErspan,
				fraOf(xtcpnl.LwtunnelIPOptErspanVer, 2), fraOf(xtcpnl.LwtunnelIPOptErspanHwid, 3)))), gw4)},
		{description: "negative: tos twice", args: "encap ip tos 1 tos 2 via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: `either "tos" is duplicate, or "2" is garbage`},
		{description: "negative: an IPv4 dst in ip6", args: "encap ip6 dst 10.0.0.1 via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: "inet6 address is expected"},
		{description: "negative: a type this plan leaves out", args: "encap seg6 mode encap segs 2001:db8::1", err: ErrNotImplemented,
			errMsg: "encap seg6"},
		{description: "boundary: an encap that fills addraw_l's 1024 bytes", args: geneveArgs(960) + " via 172.16.1.2",
			nhm: v4, attrs: cat(geneve(960), gw4)},
		{description: "boundary: four bytes more is over the bound", args: geneveArgs(964) + " via 172.16.1.2",
			err: ErrInvalidArgument, errMsg: bound},
		{description: "corner: the attributes before the encap count to its bound", args: "id 1 " + geneveArgs(960),
			err: ErrInvalidArgument, errMsg: bound},
	})
}
