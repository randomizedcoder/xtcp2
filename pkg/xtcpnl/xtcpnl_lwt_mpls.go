package xtcpnl

import (
	"errors"

	"golang.org/x/sys/unix"
)

// MplsLabelStackMax is the most labels mpls_pton reads: inet_prefix's data
// is 256 bytes, four per label (lib/mpls_pton.c, include/utils.h).
const MplsLabelStackMax = 64

// ErrMplsLabelStack is a label stack that isn't 1 to MplsLabelStackMax
// four-byte entries.
var ErrMplsLabelStack = errors.New("xtcpnl: an MPLS label stack is 1 to 64 four-byte entries")

// MplsEncap is lwt_parse_encap's `encap mpls LABELS [ttl N]`: Dst is the
// label stack as mpls_pton writes it, big-endian, the bottom-of-stack bit on
// the last entry. HasTTL says whether `ttl` was given.
type MplsEncap struct {
	Dst    []byte
	TTL    uint8
	HasTTL bool
}

// PutMplsEncap appends lwt_parse_encap's two attributes for an MPLS encap:
// encapAttr|NLA_F_NESTED (rta_nest sets the flag) holding MPLS_IPTUNNEL_DST
// and, with a ttl, MPLS_IPTUNNEL_TTL u8, then typeAttr u16
// LWTUNNEL_ENCAP_MPLS. A nexthop's are NHA_ENCAP and NHA_ENCAP_TYPE.
func PutMplsEncap(ab *AttrBuilder, encapAttr, typeAttr uint16, e MplsEncap) error {
	if len(e.Dst) == 0 || len(e.Dst)%4 != 0 || len(e.Dst) > 4*MplsLabelStackMax {
		return ErrMplsLabelStack
	}
	nest, err := ab.BeginNest(encapAttr | unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	if err := ab.PutBytes(unix.MPLS_IPTUNNEL_DST, e.Dst); err != nil {
		return err
	}
	if e.HasTTL {
		if err := ab.PutU8(unix.MPLS_IPTUNNEL_TTL, e.TTL); err != nil {
			return err
		}
	}
	if err := ab.EndNest(nest); err != nil {
		return err
	}
	return ab.PutU16(typeAttr, unix.LWTUNNEL_ENCAP_MPLS)
}
