package xtcpnl

import (
	"bytes"
	"errors"
	"net/netip"
	"testing"

	"golang.org/x/sys/unix"
)

// TestBuildGetNeighRequest compares BuildGetNeighRequest with the RTM_GETNEIGH
// ipneigh_get sent in each link-write/rtnl/neigh_get* capture.
//
// go test ./pkg/xtcpnl/ -run TestBuildGetNeighRequest$
func TestBuildGetNeighRequest(t *testing.T) {
	const rtnl = tdRtnlWrite_7_1_4
	v4 := func(s string) []byte { a := netip.MustParseAddr(s).As4(); return a[:] }
	v6 := netip.MustParseAddr("2001:db8::2").As16()
	hdr := func(family, flags uint8, ifindex byte, dstLen byte) []byte {
		return []byte{
			byte(NlMsgHdrSizeCst + NdMsgSizeCst + 4 + int(dstLen)), 0, 0, 0,
			byte(unix.RTM_GETNEIGH), 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			family, 0, 0, 0, ifindex, 0, 0, 0, 0, 0, flags, 0,
			4 + dstLen, 0, byte(unix.NDA_DST), 0,
		}
	}

	tests := []struct {
		description string
		g           NeighGet
		expected    []byte
	}{
		{
			description: "positive: `neigh get 192.0.2.2 dev d0`, AF_INET, ifindex 4, one NDA_DST",
			g:           NeighGet{Family: unix.AF_INET, Ifindex: 4, Dst: v4("192.0.2.2")},
			expected:    capturedGetRequest(t, rtnl+"/neigh_get/ip.pcap", uint16(unix.RTM_GETNEIGH)),
		},
		{
			description: "positive: the missing entry's get is an ordinary request; ENOENT is the kernel's",
			g:           NeighGet{Family: unix.AF_INET, Ifindex: 4, Dst: v4("192.0.2.9")},
			expected:    capturedGetRequest(t, rtnl+"/neigh_get_missing/ip.pcap", uint16(unix.RTM_GETNEIGH)),
		},
		{
			description: "boundary: an IPv6 destination is a 20-byte NDA_DST",
			g:           NeighGet{Family: unix.AF_INET6, Ifindex: 4, Dst: v6[:]},
			expected:    append(hdr(unix.AF_INET6, 0, 4, 16), v6[:]...),
		},
		{
			description: "corner: proxy sets NTF_PROXY at ndm_flags, offset 10, and may have no ifindex",
			g:           NeighGet{Family: unix.AF_INET, Flags: unix.NTF_PROXY, Dst: v4("192.0.2.2")},
			expected:    append(hdr(unix.AF_INET, unix.NTF_PROXY, 0, 4), v4("192.0.2.2")...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := BuildGetNeighRequest(tt.g, 0)
			if err != nil {
				t.Fatalf("BuildGetNeighRequest: %v", err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("BuildGetNeighRequest =\n  % x\nwant\n  % x", got, tt.expected)
			}
		})
	}

	t.Run("negative: a destination larger than the attribute room is ErrAttrNoSpace", func(t *testing.T) {
		_, err := BuildGetNeighRequest(NeighGet{Family: unix.AF_INET, Dst: make([]byte, reqAttrBufCst)}, 0)
		if !errors.Is(err, ErrAttrNoSpace) {
			t.Errorf("err = %v, want ErrAttrNoSpace", err)
		}
	})
}
