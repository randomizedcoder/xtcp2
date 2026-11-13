package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// ErrIPEncapOpts is an LWTUNNEL_IP_OPTS nest that print_encap_ip's
// lwtunnel_print_opts can't read: a geneve option without its class, type
// or data, a stride past the end, or a vxlan or erspan member missing or
// short. ip would dereference the missing attribute; the kernel always
// sends all of them (ip_tun_fill_encap_opts_*).
var ErrIPEncapOpts = errors.New("xtcpnl: an ip encap's opts lack a member ip reads")

// geneveOptStrideCst is lwtunnel_print_geneve_opts' step past one option
// less its data: the class (be16) and the type (u8) are 8 aligned bytes
// each, and the data's header is 4.
const geneveOptStrideCst = 20

// IPEncapInfo is an LWTUNNEL_ENCAP_IP or LWTUNNEL_ENCAP_IP6 RTA_ENCAP
// payload on a read, as print_encap_ip and print_encap_ip6
// (ip/iproute_lwtunnel.c) read it. The two share the attribute numbers, so
// TTL is IPv6's hoplimit and Tos its tc. The first of a repeated attribute
// wins, as in parse_rtattr, and a scalar shorter than its width is dropped.
type IPEncapInfo struct {
	ID       uint64 // LWTUNNEL_IP_ID, sent big-endian
	Src, Dst []byte // nil when absent
	TTL      uint8
	Tos      uint8
	Flags    uint16 // the TUNNEL_* bits, sent big-endian
	// Opts are the options lwtunnel_print_opts prints, nil without
	// LWTUNNEL_IP_OPTS. Opts.Kind is 0 when the nest has none of the three.
	Opts *IPEncapOpts

	HasID, HasTTL, HasTos, HasFlags bool
}

// ParseIPEncap decodes an LWTUNNEL_ENCAP_IP or LWTUNNEL_ENCAP_IP6 RTA_ENCAP
// payload. The byte slices are copied.
func ParseIPEncap(val []byte) (IPEncapInfo, error) {
	var (
		e       IPEncapInfo
		optsErr error
		seen    attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case LwtunnelIPID:
			if len(v) >= 8 {
				e.ID, e.HasID = binary.BigEndian.Uint64(v), true
			}
		case LwtunnelIPSrc:
			e.Src = CopyBytes(v)
		case LwtunnelIPDst:
			e.Dst = CopyBytes(v)
		case LwtunnelIPTTL:
			if len(v) >= 1 {
				e.TTL, e.HasTTL = v[0], true
			}
		case LwtunnelIPTos:
			if len(v) >= 1 {
				e.Tos, e.HasTos = v[0], true
			}
		case LwtunnelIPFlags:
			if len(v) >= 2 {
				e.Flags, e.HasFlags = binary.BigEndian.Uint16(v), true
			}
		case LwtunnelIPOpts:
			var o IPEncapOpts
			o, optsErr = parseIPEncapOpts(v)
			e.Opts = &o
		}
	})
	if err = errors.Join(err, optsErr); err != nil {
		return IPEncapInfo{}, err
	}
	return e, nil
}

// parseIPEncapOpts is lwtunnel_print_opts' choice: geneve if present, else
// vxlan, else erspan, whatever the order sent.
func parseIPEncapOpts(val []byte) (IPEncapOpts, error) {
	var kinds [LwtunnelIPOptsErspan + 1][]byte
	var seen attrSeen
	if err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if seen.first(atype) && atype <= LwtunnelIPOptsErspan {
			kinds[atype] = v
		}
	}); err != nil {
		return IPEncapOpts{}, err
	}
	var (
		o   IPEncapOpts
		err error
	)
	switch {
	case kinds[LwtunnelIPOptsGeneve] != nil:
		o.Kind = LwtunnelIPOptsGeneve
		o.Geneve, err = parseGeneveOpts(kinds[LwtunnelIPOptsGeneve])
	case kinds[LwtunnelIPOptsVxlan] != nil:
		o.Kind = LwtunnelIPOptsVxlan
		o.VxlanGBP, err = parseVxlanOpts(kinds[LwtunnelIPOptsVxlan])
	case kinds[LwtunnelIPOptsErspan] != nil:
		o.Kind = LwtunnelIPOptsErspan
		o.Erspan, err = parseErspanOpts(kinds[LwtunnelIPOptsErspan])
	}
	if err != nil {
		return IPEncapOpts{}, err
	}
	return o, nil
}

// parseGeneveOpts is lwtunnel_print_geneve_opts' loop: from each offset the
// first class, type and data after it make one option, and the next starts
// the data's length plus 20 bytes on.
func parseGeneveOpts(val []byte) ([]GeneveOpt, error) {
	var opts []GeneveOpt
	for off := 0; off < len(val); {
		var (
			g    GeneveOpt
			seen attrSeen
		)
		err := WalkRTAttrsNested(val[off:], func(atype uint16, v []byte) {
			if !seen.first(atype) {
				return
			}
			switch atype {
			case LwtunnelIPOptGeneveClass:
				if len(v) >= 2 {
					g.Class, g.HasClass = binary.BigEndian.Uint16(v), true
				}
			case LwtunnelIPOptGeneveType:
				if len(v) >= 1 {
					g.Type, g.HasType = v[0], true
				}
			case LwtunnelIPOptGeneveData:
				g.Data, g.HasData = CopyBytes(v), true
			}
		})
		if err != nil {
			return nil, err
		}
		if !g.HasClass || !g.HasType || !g.HasData {
			return nil, ErrIPEncapOpts
		}
		opts = append(opts, g)
		off += len(g.Data) + geneveOptStrideCst
		if off > len(val) {
			return nil, ErrIPEncapOpts
		}
	}
	return opts, nil
}

// parseVxlanOpts is lwtunnel_print_vxlan_opts' gbp, a host-order u32.
func parseVxlanOpts(val []byte) (uint32, error) {
	var (
		gbp  uint32
		ok   bool
		seen attrSeen
	)
	if err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if seen.first(atype) && atype == LwtunnelIPOptVxlanGBP && len(v) >= 4 {
			gbp, ok = binary.LittleEndian.Uint32(v), true
		}
	}); err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrIPEncapOpts
	}
	return gbp, nil
}

// parseErspanOpts is lwtunnel_print_erspan_opts' read: the version, then
// the index for version 1 or the direction and hardware id otherwise. Those
// are the members ip reads, so only they must be present.
func parseErspanOpts(val []byte) (ErspanOpt, error) {
	var (
		s    ErspanOpt
		seen attrSeen
	)
	if err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case LwtunnelIPOptErspanVer:
			if len(v) >= 1 {
				s.Ver, s.HasVer = v[0], true
			}
		case LwtunnelIPOptErspanIndex:
			if len(v) >= 4 {
				s.Index, s.HasIndex = binary.BigEndian.Uint32(v), true
			}
		case LwtunnelIPOptErspanDir:
			if len(v) >= 1 {
				s.Dir, s.HasDir = v[0], true
			}
		case LwtunnelIPOptErspanHwid:
			if len(v) >= 1 {
				s.Hwid, s.HasHwid = v[0], true
			}
		}
	}); err != nil {
		return ErspanOpt{}, err
	}
	switch {
	case !s.HasVer,
		s.Ver == 1 && !s.HasIndex,
		s.Ver != 1 && (!s.HasDir || !s.HasHwid):
		return ErspanOpt{}, ErrIPEncapOpts
	}
	return s, nil
}
