package xtcpnl

import (
	"encoding/binary"
)

// This file decodes the bond payload of an xstats group: LINK_XSTATS_TYPE_BOND
// → BOND_XSTATS_3AD, the nine 802.3ad counters `ip stats show group xstats`
// prints for a bond and `group xstats_slave` prints for each of its ports
// (bond_fill_linkxstats, drivers/net/bonding/bond_main.c; the attribute ids
// are include/uapi/linux/if_bonding.h:131-153). x/sys has none of these, so
// they are declared here, the bridge xstats precedent.
//
// Grounded on pkg/xtcpnl/testdata/7_1_4/link-write: bond0 carries the nest in
// the xstats group and la1/la2 in the xstats_slave group, in every LACP state.

const (
	// BondXstats3ad is the one sub-attribute of LINK_XSTATS_TYPE_BOND.
	BondXstats3ad = 1

	// Bond3adStat* index the counters inside BOND_XSTATS_3AD. The first is
	// 0, which is a real attribute type here, not UNSPEC.
	Bond3adStatLacpduRx        = 0
	Bond3adStatLacpduTx        = 1
	Bond3adStatLacpduUnknownRx = 2
	Bond3adStatLacpduIllegalRx = 3
	Bond3adStatMarkerRx        = 4
	Bond3adStatMarkerTx        = 5
	Bond3adStatMarkerRespRx    = 6
	Bond3adStatMarkerRespTx    = 7
	Bond3adStatMarkerUnknownRx = 8
	// Bond3adStatPad is the alignment attribute nla_put_u64_64bit inserts
	// before a counter when the u64 would be misaligned. It carries nothing.
	Bond3adStatPad = 9

	// Bond3adStatCountCst is the number of counters, BOND_3AD_STAT_PAD.
	Bond3adStatCountCst = Bond3adStatPad

	bond3adStatWidthCst = 8
)

// Bond3adStats is BOND_XSTATS_3AD. Each counter is optional on the wire, and
// bond_print_3ad_stats prints only the ones present, so presence is kept per
// counter rather than inferred from zero.
type Bond3adStats struct {
	Values  [Bond3adStatCountCst]uint64
	Present [Bond3adStatCountCst]bool
}

// ParseBond3adStats decodes the inside of a BOND_XSTATS_3AD attribute.
//
// A counter shorter than 8 bytes is skipped rather than read: rta_getattr_u64
// would memcpy past the attribute, which has no faithful transcription. The
// pad and any id past the last counter are ignored, as parse_rtattr does with
// BOND_3AD_STAT_MAX. A malformed nest returns the rtattr error.
func ParseBond3adStats(nest []byte) (*Bond3adStats, error) {
	s := &Bond3adStats{}
	err := WalkRTAttrsNested(nest, func(atype uint16, val []byte) {
		if int(atype) >= Bond3adStatCountCst || len(val) < bond3adStatWidthCst {
			return
		}
		s.Values[atype] = binary.LittleEndian.Uint64(val[:bond3adStatWidthCst])
		s.Present[atype] = true
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}
