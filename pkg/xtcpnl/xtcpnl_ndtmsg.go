package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// This file decodes RTM_NEWNEIGHTBL, the reply to the RTM_GETNEIGHTBL dump that
// `ip ntable show` issues. Each reply is one neighbor table (arp_cache,
// ndisc_cache, …) or one device-specific parameter set. The kernel always
// includes NDTA_CONFIG and NDTA_STATS; iproute2 prints them only under `-s`.
// See internal/goip/obj_ntable.go for the transaction and render/ntable.go for
// the output.

// Ndtmsg mirrors the kernel's `struct ndtmsg`, the fixed header of an
// RTM_*NEIGHTBL message.
//
//	struct ndtmsg {
//		__u8	ndtm_family;
//		__u8	ndtm_pad1;
//		__u16	ndtm_pad2;
//	};
//
// Reference: linux/include/uapi/linux/neighbour.h
type Ndtmsg struct {
	Family uint8
}

const (
	// NdtMsgSizeCst is sizeof(struct ndtmsg): a family byte plus three bytes of
	// padding.
	NdtMsgSizeCst = 4

	// NdtConfigSizeCst is sizeof(struct ndt_config) and NdtStatsSizeCst is
	// sizeof(struct ndt_stats). Declared here because golang.org/x/sys/unix's
	// pinned version carries neither struct nor the NDTA_*/NDTPA_* enums, as
	// xtcpnl_nhmsg.go declares NhaID et al for the same reason.
	NdtConfigSizeCst = 32 // ndt_config: u16,u16,u32×7
	NdtStatsSizeCst  = 88 // ndt_stats: u64×11

	// NDTA_* top-level attributes (include/uapi/linux/neighbour.h). NDTA_PAD=9
	// is a 64-bit alignment pad with no value and is not decoded.
	NdtaName       uint16 = 1 // char*, table name
	NdtaThresh1    uint16 = 2 // u32
	NdtaThresh2    uint16 = 3 // u32
	NdtaThresh3    uint16 = 4 // u32
	NdtaConfig     uint16 = 5 // struct ndt_config
	NdtaParms      uint16 = 6 // nested NDTPA_*
	NdtaStats      uint16 = 7 // struct ndt_stats
	NdtaGcInterval uint16 = 8 // u64

	// NDTPA_* sub-attributes of NDTA_PARMS. Only the ones print_ndtparams
	// renders are decoded (the "decode only what renders" discipline of
	// AddrLabelInfo); NDTPA_QUEUE_LENBYTES, NDTPA_PAD and
	// NDTPA_INTERVAL_PROBE_TIME_MS are not printed and so not decoded.
	NdtpaIfindex           uint16 = 1  // u32
	NdtpaRefcnt            uint16 = 2  // u32
	NdtpaReachableTime     uint16 = 3  // u64
	NdtpaBaseReachableTime uint16 = 4  // u64
	NdtpaRetransTime       uint16 = 5  // u64
	NdtpaGcStaletime       uint16 = 6  // u64
	NdtpaDelayProbeTime    uint16 = 7  // u64
	NdtpaQueueLen          uint16 = 8  // u32
	NdtpaAppProbes         uint16 = 9  // u32
	NdtpaUcastProbes       uint16 = 10 // u32
	NdtpaMcastProbes       uint16 = 11 // u32
	NdtpaAnycastDelay      uint16 = 12 // u64
	NdtpaProxyDelay        uint16 = 13 // u64
	NdtpaProxyQlen         uint16 = 14 // u32
	NdtpaLocktime          uint16 = 15 // u64
	NdtpaMcastReprobes     uint16 = 17 // u32
)

var (
	// ErrNdtmsgSmall is a reply body shorter than the fixed ndtmsg header.
	ErrNdtmsgSmall = errors.New("data too small for Ndtmsg")
)

// NdtConfig is the decoded NDTA_CONFIG fixed struct (ndt_config). LastFlush and
// LastRand are deltas-to-now in msecs; the render layer converts them to the
// absolute dates `ip -s ntable` prints.
type NdtConfig struct {
	KeyLen      uint16
	EntrySize   uint16
	Entries     uint32
	LastFlush   uint32
	LastRand    uint32
	HashRnd     uint32
	HashMask    uint32
	HashChainGc uint32
	ProxyQlen   uint32
}

// NdtStats is the decoded NDTA_STATS fixed struct (ndt_stats): 11 u64 counters,
// field order == print order (ip/ipntable.c:498-528).
type NdtStats struct {
	Allocs         uint64
	Destroys       uint64
	HashGrows      uint64
	ResFailed      uint64
	Lookups        uint64
	Hits           uint64
	RcvProbesMcast uint64
	RcvProbesUcast uint64
	PeriodicGcRuns uint64
	ForcedGcRuns   uint64
	TableFulls     uint64
}

// NeighTblParms is the decoded NDTA_PARMS nest. Each field carries a Has* bool
// because presence is not value: print_ndtparams guards every token on
// `if (tpb[NDTPA_X])`, so a present-but-zero param prints `refcnt 0` while an
// absent one prints no token at all (the RuleInfo.HasProtocol discipline).
//
// Ifindex uses 0 == absent (index 0 is not a device — the AddrLabelInfo.Index
// precedent); the base table message carries no NDTPA_IFINDEX, device-specific
// messages set it.
type NeighTblParms struct {
	Ifindex int32 // 0 is absent: the base message, not a device

	Refcnt    uint32
	HasRefcnt bool

	ReachableTime    uint64
	HasReachableTime bool

	BaseReachableTime    uint64
	HasBaseReachableTime bool

	RetransTime    uint64
	HasRetransTime bool

	GcStaletime    uint64
	HasGcStaletime bool

	DelayProbeTime    uint64
	HasDelayProbeTime bool

	QueueLen    uint32
	HasQueueLen bool

	AppProbes    uint32
	HasAppProbes bool

	UcastProbes    uint32
	HasUcastProbes bool

	McastProbes    uint32
	HasMcastProbes bool

	McastReprobes    uint32
	HasMcastReprobes bool

	AnycastDelay    uint64
	HasAnycastDelay bool

	ProxyDelay    uint64
	HasProxyDelay bool

	ProxyQlen    uint32
	HasProxyQlen bool

	Locktime    uint64
	HasLocktime bool
}

// NeighTblInfo is a decoded RTM_NEWNEIGHTBL reply: the ndtmsg family plus the
// NDTA_* attributes print_ntable renders. The Has* bools are load-bearing for
// the reason given on NeighTblParms.
type NeighTblInfo struct {
	Family uint8

	Name    string
	HasName bool

	Thresh1    uint32
	HasThresh1 bool
	Thresh2    uint32
	HasThresh2 bool
	Thresh3    uint32
	HasThresh3 bool

	GcInterval    uint64
	HasGcInterval bool

	Config    NdtConfig
	HasConfig bool

	Stats    NdtStats
	HasStats bool

	Parms    NeighTblParms
	HasParms bool
}

// DeserializeNdtmsg does an offset read of the fixed ndtmsg header. The two pad
// bytes and pad u16 are ignored, as the kernel leaves them zero.
func DeserializeNdtmsg(data []byte, h *Ndtmsg) (n int, err error) {
	if len(data) < NdtMsgSizeCst {
		return 0, ErrNdtmsgSmall
	}
	h.Family = data[0]
	return NdtMsgSizeCst, nil
}

// deserializeNdtConfig does an offset read of an ndt_config payload. A payload
// shorter than the struct leaves HasConfig false at the caller, mirroring the
// IFAL_LABEL width guard in ParseNewAddrLabel.
func deserializeNdtConfig(val []byte) (NdtConfig, bool) {
	if len(val) < NdtConfigSizeCst {
		return NdtConfig{}, false
	}
	return NdtConfig{
		KeyLen:      binary.LittleEndian.Uint16(val[0:2]),
		EntrySize:   binary.LittleEndian.Uint16(val[2:4]),
		Entries:     binary.LittleEndian.Uint32(val[4:8]),
		LastFlush:   binary.LittleEndian.Uint32(val[8:12]),
		LastRand:    binary.LittleEndian.Uint32(val[12:16]),
		HashRnd:     binary.LittleEndian.Uint32(val[16:20]),
		HashMask:    binary.LittleEndian.Uint32(val[20:24]),
		HashChainGc: binary.LittleEndian.Uint32(val[24:28]),
		ProxyQlen:   binary.LittleEndian.Uint32(val[28:32]),
	}, true
}

// deserializeNdtStats does an offset read of an ndt_stats payload.
func deserializeNdtStats(val []byte) (NdtStats, bool) {
	if len(val) < NdtStatsSizeCst {
		return NdtStats{}, false
	}
	u := func(off int) uint64 { return binary.LittleEndian.Uint64(val[off : off+8]) }
	return NdtStats{
		Allocs:         u(0),
		Destroys:       u(8),
		HashGrows:      u(16),
		ResFailed:      u(24),
		Lookups:        u(32),
		Hits:           u(40),
		RcvProbesMcast: u(48),
		RcvProbesUcast: u(56),
		PeriodicGcRuns: u(64),
		ForcedGcRuns:   u(72),
		TableFulls:     u(80),
	}, true
}

// parseNdtParms walks the NDTA_PARMS nest. u64 attributes are taken at >= 8
// bytes and u32 at >= 4 (the decodeResGroup width discipline in xtcpnl_nhmsg.go);
// a short payload leaves the field's Has bit clear, like a missing attribute.
//
// The per-type field and width live in a table rather than a switch so adding a
// param is one row, and so the walk stays a single assignment per attribute.
func parseNdtParms(val []byte) (NeighTblParms, error) {
	var p NeighTblParms
	u32 := func(dst *uint32, has *bool, b []byte) {
		if len(b) >= 4 {
			*dst, *has = binary.LittleEndian.Uint32(b[0:4]), true
		}
	}
	u64 := func(dst *uint64, has *bool, b []byte) {
		if len(b) >= 8 {
			*dst, *has = binary.LittleEndian.Uint64(b[0:8]), true
		}
	}
	setters := map[uint16]func(b []byte){
		NdtpaIfindex: func(b []byte) {
			if len(b) >= 4 {
				p.Ifindex = int32(binary.LittleEndian.Uint32(b[0:4]))
			}
		},
		NdtpaRefcnt:            func(b []byte) { u32(&p.Refcnt, &p.HasRefcnt, b) },
		NdtpaReachableTime:     func(b []byte) { u64(&p.ReachableTime, &p.HasReachableTime, b) },
		NdtpaBaseReachableTime: func(b []byte) { u64(&p.BaseReachableTime, &p.HasBaseReachableTime, b) },
		NdtpaRetransTime:       func(b []byte) { u64(&p.RetransTime, &p.HasRetransTime, b) },
		NdtpaGcStaletime:       func(b []byte) { u64(&p.GcStaletime, &p.HasGcStaletime, b) },
		NdtpaDelayProbeTime:    func(b []byte) { u64(&p.DelayProbeTime, &p.HasDelayProbeTime, b) },
		NdtpaQueueLen:          func(b []byte) { u32(&p.QueueLen, &p.HasQueueLen, b) },
		NdtpaAppProbes:         func(b []byte) { u32(&p.AppProbes, &p.HasAppProbes, b) },
		NdtpaUcastProbes:       func(b []byte) { u32(&p.UcastProbes, &p.HasUcastProbes, b) },
		NdtpaMcastProbes:       func(b []byte) { u32(&p.McastProbes, &p.HasMcastProbes, b) },
		NdtpaMcastReprobes:     func(b []byte) { u32(&p.McastReprobes, &p.HasMcastReprobes, b) },
		NdtpaAnycastDelay:      func(b []byte) { u64(&p.AnycastDelay, &p.HasAnycastDelay, b) },
		NdtpaProxyDelay:        func(b []byte) { u64(&p.ProxyDelay, &p.HasProxyDelay, b) },
		NdtpaProxyQlen:         func(b []byte) { u32(&p.ProxyQlen, &p.HasProxyQlen, b) },
		NdtpaLocktime:          func(b []byte) { u64(&p.Locktime, &p.HasLocktime, b) },
	}
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if set, ok := setters[atype]; ok {
			set(v)
		}
	})
	return p, err
}

// ParseNewNeighTbl decodes an RTM_NEWNEIGHTBL reply body: the ndtmsg header then
// its NDTA_* attributes. NDTA_CONFIG and NDTA_STATS are fixed C structs read by
// offset; NDTA_PARMS is a nested TLV stream.
func ParseNewNeighTbl(body []byte) (NeighTblInfo, error) {
	var ti NeighTblInfo
	var h Ndtmsg
	if _, err := DeserializeNdtmsg(body, &h); err != nil {
		return ti, err
	}
	ti.Family = h.Family

	var parmsRaw []byte // decoded after the walk so a short attr cannot abort it
	err := WalkRTAttrs(body[NdtMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case NdtaName:
			ti.Name = string(bytes.TrimRight(val, "\x00"))
			ti.HasName = true
		case NdtaThresh1:
			if len(val) >= 4 {
				ti.Thresh1, ti.HasThresh1 = binary.LittleEndian.Uint32(val[0:4]), true
			}
		case NdtaThresh2:
			if len(val) >= 4 {
				ti.Thresh2, ti.HasThresh2 = binary.LittleEndian.Uint32(val[0:4]), true
			}
		case NdtaThresh3:
			if len(val) >= 4 {
				ti.Thresh3, ti.HasThresh3 = binary.LittleEndian.Uint32(val[0:4]), true
			}
		case NdtaGcInterval:
			if len(val) >= 8 {
				ti.GcInterval, ti.HasGcInterval = binary.LittleEndian.Uint64(val[0:8]), true
			}
		case NdtaConfig:
			ti.Config, ti.HasConfig = deserializeNdtConfig(val)
		case NdtaStats:
			ti.Stats, ti.HasStats = deserializeNdtStats(val)
		case NdtaParms:
			ti.HasParms = true
			parmsRaw = CopyBytes(val)
		}
	})
	if err != nil {
		return ti, err
	}
	if ti.HasParms {
		p, perr := parseNdtParms(parmsRaw)
		if perr != nil {
			return ti, perr
		}
		ti.Parms = p
	}
	return ti, nil
}
