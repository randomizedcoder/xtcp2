package goip

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func TestLwtEncapArgs(t *testing.T) {
	dst4 := xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPDst, Addr: []byte{203, 0, 113, 5}}
	id1 := xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPID, U64: 1}
	ip4 := func(flags uint16, items ...xtcpnl.IPEncapItem) xtcpnl.LwtEncap {
		return xtcpnl.LwtEncap{IP: &xtcpnl.IPEncap{Family: unix.AF_INET, Items: items, Flags: flags}}
	}
	geneve := func(g ...xtcpnl.GeneveOpt) xtcpnl.IPEncapItem {
		return xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPOpts, Opts: xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsGeneve, Geneve: g}}
	}
	g1 := xtcpnl.GeneveOpt{Class: 0x0102, Type: 0x80, Data: []byte{0, 0x11, 0x22, 0x33}, HasClass: true, HasType: true, HasData: true}
	v6 := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	tests := []struct {
		description string
		args        string // from args[0], the encap
		expected    xtcpnl.LwtEncap
		last        int    // the index of the last word used
		err         error  // with errText, a refusal
		errText     string // in the refusal's message
	}{
		{
			description: "positive: mpls, one label",
			args:        "encap mpls 100 via 192.0.2.1 dev d0",
			expected:    xtcpnl.LwtEncap{MPLS: &xtcpnl.MplsEncap{Dst: []byte{0, 0x06, 0x41, 0}}},
			last:        2,
		},
		{
			description: "positive: mpls, a stack and ttl",
			args:        "encap mpls 100/200 ttl 10 via 192.0.2.1",
			expected:    xtcpnl.LwtEncap{MPLS: &xtcpnl.MplsEncap{Dst: []byte{0, 0x06, 0x40, 0, 0, 0x0c, 0x81, 0}, TTL: 10, HasTTL: true}},
			last:        4,
		},
		{
			description: "positive: ip, id and dst",
			args:        "encap ip id 1 dst 203.0.113.5 dev d0",
			expected:    ip4(0, id1, dst4),
			last:        5,
		},
		{
			description: "positive: ip, every scalar, the flags in any order",
			args:        "encap ip id 1 dst 203.0.113.5 src 192.0.2.10 tos 0x10 ttl 64 seq key csum dev d0",
			expected: ip4(xtcpnl.LwtTunnelKey|xtcpnl.LwtTunnelCsum|xtcpnl.LwtTunnelSeq, id1, dst4,
				xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPSrc, Addr: []byte{192, 0, 2, 10}},
				xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPTos, U8: 0x10},
				xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPTTL, U8: 64}),
			last: 14,
		},
		{
			description: "positive: ip6, tc and hoplimit",
			args:        "encap ip6 dst 2001:db8:9::1 tc 0x10 hoplimit 64 dev d0",
			expected: xtcpnl.LwtEncap{IP: &xtcpnl.IPEncap{Family: unix.AF_INET6, Items: []xtcpnl.IPEncapItem{
				{Kind: xtcpnl.LwtunnelIPDst, Addr: v6},
				{Kind: xtcpnl.LwtunnelIPTos, U8: 0x10},
				{Kind: xtcpnl.LwtunnelIPTTL, U8: 64},
			}}},
			last: 7,
		},
		{
			description: "positive: two geneve options",
			args:        "encap ip id 1 geneve_opts 0x0102:0x80:00112233,0x0102:0x81:44556677 dev d0",
			expected: ip4(0, id1, geneve(g1, xtcpnl.GeneveOpt{Class: 0x0102, Type: 0x81, Data: []byte{0x44, 0x55, 0x66, 0x77},
				HasClass: true, HasType: true, HasData: true})),
			last: 5,
		},
		{
			description: "positive: vxlan and erspan",
			args:        "encap ip erspan_opts 1:0x1234:: dev d0",
			expected: ip4(0, xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPOpts, Opts: xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsErspan,
				Erspan: xtcpnl.ErspanOpt{Ver: 1, Index: 0x1234, HasVer: true, HasIndex: true}}}),
			last: 3,
		},
		{
			description: "positive: vxlan's gbp, base 0",
			args:        "encap ip vxlan_opts 0x800 dev d0",
			expected:    ip4(0, xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPOpts, Opts: xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsVxlan, VxlanGBP: 0x800}}),
			last:        3,
		},
		{
			description: "positive: an unknown word ends the type",
			args:        "encap ip dst 203.0.113.5 via 192.0.2.1",
			expected:    ip4(0, dst4),
			last:        3,
		},
		{
			description: "positive: ip reads its words to the end",
			args:        "encap ip dst 203.0.113.5 key",
			expected:    ip4(xtcpnl.LwtTunnelKey, dst4),
			last:        4,
		},
		{description: "negative: an unknown type", args: "encap foo 1 dev d0", err: ErrInvalidArgument, errText: `"encap type" value is invalid`},
		{description: "negative: a second ttl names its value", args: "encap ip ttl 1 ttl 2 dev d0", err: ErrInvalidArgument, errText: `either "ttl" is duplicate, or "2" is garbage`},
		{description: "negative: a second flag word", args: "encap ip key key dev d0", err: ErrInvalidArgument, errText: `either "key" is duplicate, or "key" is garbage`},
		{
			description: "negative: two option kinds",
			args:        "encap ip geneve_opts 0102:80:00112233 vxlan_opts 1 dev d0",
			err:         ErrInvalidArgument,
			errText:     `either "opts" is duplicate, or "vxlan_opts" is garbage`,
		},
		{description: "negative: an IPv6 address in ip", args: "encap ip dst 2001:db8::1 dev d0", err: ErrInvalidArgument, errText: "inet address is expected"},
		{description: "negative: an id out of range", args: "encap ip id 18446744073709551616 dev d0", err: ErrInvalidArgument, errText: `"id" value is invalid`},
		{description: "negative: hoplimit 256", args: "encap ip6 hoplimit 256 dev d0", err: ErrInvalidArgument, errText: `"hoplimit" value is invalid`},
		{
			description: "negative: a fourth geneve field, the value cut at its first colon",
			args:        "encap ip geneve_opts 0102:80:00:01 dev d0",
			err:         ErrInvalidArgument,
			errText:     `argument "0102" is wrong: "geneve_opts" value is invalid`,
		},
		{description: "negative: geneve data not hex", args: "encap ip geneve_opts 0102:80:0g dev d0", err: ErrInvalidArgument, errText: `"geneve_opts" value is invalid`},
		{description: "negative: a fifth erspan field", args: "encap ip erspan_opts 1:2:3:4:5 dev d0", err: ErrInvalidArgument, errText: `"erspan_opts" value is invalid`},
		{description: "boundary: the type's word last", args: "encap ip key", err: ErrInvalidArgument, errText: `argument "encap type" is required`},
		{description: "boundary: no type", args: "encap", err: ErrInvalidArgument, errText: "Command line is not complete"},
		{description: "boundary: a value missing at the end", args: "encap ip key ttl", err: ErrInvalidArgument, errText: "Command line is not complete"},
		{
			description: "boundary: tos is base 16, so 10 is 0x10",
			args:        "encap ip tos 10 dev d0",
			expected:    ip4(0, xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPTos, U8: 0x10}),
			last:        3,
		},
		{
			description: "boundary: tos by its rt_dsfield name",
			args:        "encap ip tos AF11 dev d0",
			expected:    ip4(0, xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPTos, U8: 0x28}),
			last:        3,
		},
		{
			description: "corner: odd-length geneve data drops its last digit",
			args:        "encap ip geneve_opts 0x0102:0x80:001 dev d0",
			expected:    ip4(0, geneve(xtcpnl.GeneveOpt{Class: 0x0102, Type: 0x80, Data: []byte{0}, HasClass: true, HasType: true, HasData: true})),
			last:        3,
		},
		{
			description: "corner: empty geneve fields are skipped",
			args:        "encap ip geneve_opts :0x80: dev d0",
			expected:    ip4(0, geneve(xtcpnl.GeneveOpt{Type: 0x80, HasType: true})),
			last:        3,
		},
		{
			description: "corner: geneve's class and type are base 0, so 0102 is octal",
			args:        "encap ip geneve_opts 0102:80:00112233 dev d0",
			expected:    ip4(0, geneve(xtcpnl.GeneveOpt{Class: 0o102, Type: 80, Data: g1.Data, HasClass: true, HasType: true, HasData: true})),
			last:        3,
		},
		{
			description: "corner: dst any has no bytes",
			args:        "encap ip6 dst any dev d0",
			expected:    xtcpnl.LwtEncap{IP: &xtcpnl.IPEncap{Family: unix.AF_INET6, Items: []xtcpnl.IPEncapItem{{Kind: xtcpnl.LwtunnelIPDst}}}},
			last:        3,
		},
		{
			description: "corner: a first word ip doesn't know uses nothing past the type",
			args:        "encap ip via 192.0.2.1",
			expected:    ip4(0),
			last:        1,
		},
		{description: "corner: help as the type", args: "encap help me", err: ErrNotImplemented, errText: "encap help"},
		{description: "corner: a type this plan leaves out", args: "encap seg6 mode encap segs 2001:db8::1", err: ErrNotImplemented, errText: "encap seg6"},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			got, last, err := lwtEncapArgs(strings.Fields(tt.args), 0)
			if tt.err != nil {
				if !errors.Is(err, tt.err) || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("err = %v, want %v with %q", err, tt.err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if last != tt.last {
				t.Errorf("last word %d, want %d", last, tt.last)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got  %+v\nwant %+v", lwtString(got), lwtString(tt.expected))
			}
		})
	}
}

// lwtString shows an LwtEncap's pointed-to values in a failure.
func lwtString(e xtcpnl.LwtEncap) any {
	switch {
	case e.MPLS != nil:
		return *e.MPLS
	case e.IP != nil:
		return *e.IP
	}
	return e
}
