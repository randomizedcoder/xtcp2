package xtcpnl

import (
	"bytes"
	"encoding/binary"
)

// Seg6LocalBPF is SEG6_LOCAL_BPF (include/uapi/linux/seg6_local.h), a nest
// of SEG6_LOCAL_BPF_PROG_*.
const Seg6LocalBPF uint16 = 8

// LWT_BPF_PROG_* (include/uapi/linux/lwtunnel.h), which print_encap_bpf_prog
// also reads a SEG6_LOCAL_BPF nest with: SEG6_LOCAL_BPF_PROG_NAME is 2 too.
const (
	LwtBpfProgFD   uint16 = 1 // u32
	LwtBpfProgName uint16 = 2 // NUL-terminated string
)

// Seg6LocalInfo is an LWTUNNEL_ENCAP_SEG6_LOCAL RTA_ENCAP payload on a
// read, as print_encap_seg6local (ip/iproute_lwtunnel.c) reads it. The first
// of a repeated attribute wins, as in parse_rtattr, and a scalar shorter
// than its width is dropped. ip prints nothing past the type without
// HasAction.
type Seg6LocalInfo struct {
	Action   uint32 // SEG6_LOCAL_ACTION_*
	SRH      []byte // SEG6_LOCAL_SRH as sent
	Table    uint32
	VrfTable uint32
	NH4, NH6 []byte
	IIF, OIF uint32 // ifindexes

	// BPFName is SEG6_LOCAL_BPF's program name; HasBPFName is false when
	// the nest has none, which ip prints as "<unknown>" in JSON.
	BPFName string
	// Counters are SEG6_LOCAL_COUNTERS, which ip prints only under -s; a
	// missing or short member is 0, as ip reads it.
	Counters Seg6LocalCountersInfo
	Flavors  Seg6LocalFlavorsInfo

	// HasSRH, HasNH4 and HasNH6 are the attributes' presence, which is what
	// ip tests: an empty one still prints its keyword.
	HasAction, HasSRH, HasTable, HasVrfTable, HasNH4, HasNH6, HasIIF, HasOIF bool
	HasBPF, HasBPFName, HasCounters, HasFlavors                              bool
}

// Seg6LocalCountersInfo are print_seg6_local_counters' three u64s.
type Seg6LocalCountersInfo struct {
	Packets, Bytes, Errors uint64
}

// Seg6LocalFlavorsInfo is print_seg6_local_flavors' read of SEG6_LOCAL_FLAVORS.
type Seg6LocalFlavorsInfo struct {
	Ops          uint32 // SEG6_LOCAL_FLV_OPERATION, a bit per SEG6_LOCAL_FLV_OP_*
	LcblockBits  uint8  // SEG6_LOCAL_FLV_LCBLOCK_BITS, `lblen`
	LcnodeFnBits uint8  // SEG6_LOCAL_FLV_LCNODE_FN_BITS, `nflen`

	HasOps, HasLcblockBits, HasLcnodeFnBits bool
}

// ParseSeg6LocalEncap decodes an LWTUNNEL_ENCAP_SEG6_LOCAL RTA_ENCAP
// payload. The byte slices are copied.
func ParseSeg6LocalEncap(val []byte) (Seg6LocalInfo, error) {
	var (
		s       Seg6LocalInfo
		nestErr error
		seen    attrSeen
	)
	u32 := func(v []byte, dst *uint32, has *bool) {
		if len(v) >= 4 {
			*dst, *has = binary.LittleEndian.Uint32(v), true
		}
	}
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) {
			return
		}
		var e error
		switch atype {
		case Seg6LocalAction:
			u32(v, &s.Action, &s.HasAction)
		case Seg6LocalSRH:
			s.SRH, s.HasSRH = CopyBytes(v), true
		case Seg6LocalTable:
			u32(v, &s.Table, &s.HasTable)
		case Seg6LocalVrfTable:
			u32(v, &s.VrfTable, &s.HasVrfTable)
		case Seg6LocalNH4:
			s.NH4, s.HasNH4 = CopyBytes(v), true
		case Seg6LocalNH6:
			s.NH6, s.HasNH6 = CopyBytes(v), true
		case Seg6LocalIIF:
			u32(v, &s.IIF, &s.HasIIF)
		case Seg6LocalOIF:
			u32(v, &s.OIF, &s.HasOIF)
		case Seg6LocalBPF:
			s.HasBPF = true
			s.BPFName, s.HasBPFName, e = parseBPFProgName(v)
		case Seg6LocalCounters:
			s.HasCounters = true
			s.Counters, e = parseSeg6LocalCounters(v)
		case Seg6LocalFlavors:
			s.HasFlavors = true
			s.Flavors, e = parseSeg6LocalFlavors(v)
		}
		if nestErr == nil {
			nestErr = e
		}
	})
	if err == nil {
		err = nestErr
	}
	if err != nil {
		return Seg6LocalInfo{}, err
	}
	return s, nil
}

// parseBPFProgName is print_encap_bpf_prog's rta_getattr_str: the name up to
// its NUL, or the whole payload when it has none.
func parseBPFProgName(val []byte) (string, bool, error) {
	var (
		name string
		ok   bool
		seen attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if seen.first(atype) && atype == LwtBpfProgName {
			if i := bytes.IndexByte(v, 0); i >= 0 {
				v = v[:i]
			}
			name, ok = string(v), true
		}
	})
	return name, ok, err
}

func parseSeg6LocalCounters(val []byte) (Seg6LocalCountersInfo, error) {
	var (
		c    Seg6LocalCountersInfo
		seen attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) || len(v) < 8 {
			return
		}
		n := binary.LittleEndian.Uint64(v)
		switch atype {
		case Seg6LocalCntPackets:
			c.Packets = n
		case Seg6LocalCntBytes:
			c.Bytes = n
		case Seg6LocalCntErrors:
			c.Errors = n
		}
	})
	return c, err
}

func parseSeg6LocalFlavors(val []byte) (Seg6LocalFlavorsInfo, error) {
	var (
		f    Seg6LocalFlavorsInfo
		seen attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case Seg6LocalFlvOperation:
			if len(v) >= 4 {
				f.Ops, f.HasOps = binary.LittleEndian.Uint32(v), true
			}
		case Seg6LocalFlvLcblockBits:
			if len(v) >= 1 {
				f.LcblockBits, f.HasLcblockBits = v[0], true
			}
		case Seg6LocalFlvLcnodeFnBits:
			if len(v) >= 1 {
				f.LcnodeFnBits, f.HasLcnodeFnBits = v[0], true
			}
		}
	})
	return f, err
}
