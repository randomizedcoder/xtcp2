package xtcpnl

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// ndtmsgPcap is `ip ntable show`'s capture: the ll_init_map link dump followed
// by the RTM_GETNEIGHTBL dump. Only the RTM_NEWNEIGHTBL bodies are fed to the
// parser here.
const ndtmsgPcap = "testdata/7_1_4/dumps/netlink_route_getneightbl.pcap"

// le64 encodes a little-endian u64, the sibling of le16/le32.
func le64(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}

// neighTblBodies returns the ndtmsg bodies (nlmsghdr stripped) of every
// RTM_NEWNEIGHTBL message in the pcap, in capture order.
func neighTblBodies(t *testing.T, path string) [][]byte {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	_, recs, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("ParseNetlinkPcap(%s): %v", path, err)
	}
	var out [][]byte
	for _, r := range recs {
		_, body, perr := r.NetlinkPayload()
		if perr != nil {
			continue
		}
		// A multipart reply packs several netlink messages into one datagram, so
		// walk every message in the record, not just the first.
		for off := 0; off+NlMsgHdrSizeCst <= len(body); {
			msgLen := int(binary.LittleEndian.Uint32(body[off : off+4]))
			if msgLen < NlMsgHdrSizeCst || off+msgLen > len(body) {
				break
			}
			if binary.LittleEndian.Uint16(body[off+4:off+6]) == uint16(unix.RTM_NEWNEIGHTBL) {
				out = append(out, body[off+NlMsgHdrSizeCst:off+msgLen])
			}
			off += (msgLen + 3) &^ 3 // NLMSG_ALIGN
		}
	}
	return out
}

// TestParseNewNeighTblCaptured decodes the real RTM_NEWNEIGHTBL bodies from the
// committed pcap. The arp_cache base message (first reply) carries the config,
// stats and base parameter set with NDTPA_IFINDEX absent; the device messages
// that follow carry a parameter set with NDTPA_IFINDEX set and no config/stats,
// exactly as the kernel lays the dump out: only the base message carries
// NDTA_CONFIG, the device messages are identified by NDTPA_IFINDEX.
//
// go test ./pkg/xtcpnl/ -run TestParseNewNeighTblCaptured
func TestParseNewNeighTblCaptured(t *testing.T) {
	bodies := neighTblBodies(t, ndtmsgPcap)
	// Two tables (arp_cache, ndisc_cache), each a base message plus five
	// device-specific sets (goipv, goipvrf, goip0, nlmon0, lo).
	if got := len(bodies); got != 12 {
		t.Fatalf("RTM_NEWNEIGHTBL message count = %d, want 12", got)
	}

	base, err := ParseNewNeighTbl(bodies[0])
	if err != nil {
		t.Fatalf("ParseNewNeighTbl(arp base): %v", err)
	}
	if base.Family != unix.AF_INET || base.Name != "arp_cache" || !base.HasName {
		t.Errorf("base family/name = %d/%q, want 2/arp_cache", base.Family, base.Name)
	}
	if !base.HasThresh1 || base.Thresh1 != 128 || base.Thresh2 != 512 || base.Thresh3 != 1024 {
		t.Errorf("thresh = %d/%d/%d, want 128/512/1024", base.Thresh1, base.Thresh2, base.Thresh3)
	}
	if !base.HasGcInterval || base.GcInterval != 30000 {
		t.Errorf("gc_interval = %d, want 30000", base.GcInterval)
	}
	if !base.HasConfig {
		t.Fatal("base message has no NDTA_CONFIG")
	}
	// The deltas are this pcap's own: the plain and `-s` captures ran a fraction
	// of a second apart, so their ndtc_last_flush/last_rand differ. The plain
	// golden prints no config, so these bytes are never rendered from this pcap.
	wantCfg := NdtConfig{
		KeyLen: 4, EntrySize: 432, Entries: 9,
		LastFlush: 39788, LastRand: 4294431304,
		HashRnd: 3190857053, HashMask: 0x0f, HashChainGc: 0, ProxyQlen: 0,
	}
	if base.Config != wantCfg {
		t.Errorf("config = %+v, want %+v", base.Config, wantCfg)
	}
	if !base.HasStats {
		t.Fatal("base message has no NDTA_STATS")
	}
	wantStats := NdtStats{Allocs: 9, Destroys: 0, HashGrows: 1, ResFailed: 0,
		Lookups: 15, Hits: 0, RcvProbesMcast: 0, RcvProbesUcast: 0,
		PeriodicGcRuns: 1, ForcedGcRuns: 0, TableFulls: 0}
	if base.Stats != wantStats {
		t.Errorf("stats = %+v, want %+v", base.Stats, wantStats)
	}
	if !base.HasParms || base.Parms.Ifindex != 0 {
		t.Fatalf("base parms: HasParms=%v Ifindex=%d, want true/0", base.HasParms, base.Parms.Ifindex)
	}
	p := base.Parms
	for _, c := range []struct {
		name string
		got  uint64
		want uint64
	}{
		{"refcnt", uint64(p.Refcnt), 1},
		{"reachable", p.ReachableTime, 28789},
		{"base_reachable", p.BaseReachableTime, 30000},
		{"retrans", p.RetransTime, 1000},
		{"gc_stale", p.GcStaletime, 60000},
		{"delay_probe", p.DelayProbeTime, 5000},
		{"queue", uint64(p.QueueLen), 101},
		{"app_probes", uint64(p.AppProbes), 0},
		{"ucast_probes", uint64(p.UcastProbes), 3},
		{"mcast_probes", uint64(p.McastProbes), 3},
		{"mcast_reprobes", uint64(p.McastReprobes), 0},
		{"anycast_delay", p.AnycastDelay, 1000},
		{"proxy_delay", p.ProxyDelay, 800},
		{"proxy_queue", uint64(p.ProxyQlen), 64},
		{"locktime", p.Locktime, 1000},
	} {
		if c.got != c.want {
			t.Errorf("base parms %s = %d, want %d", c.name, c.got, c.want)
		}
	}

	// A device-specific set: NDTPA_IFINDEX nonzero, name present, no config/stats.
	var dev *NeighTblInfo
	for i := range bodies {
		ti, err := ParseNewNeighTbl(bodies[i])
		if err != nil {
			t.Fatalf("ParseNewNeighTbl(body %d): %v", i, err)
		}
		if ti.HasParms && ti.Parms.Ifindex != 0 {
			d := ti
			dev = &d
			break
		}
	}
	if dev == nil {
		t.Fatal("no device-specific parameter set found")
	}
	if dev.HasConfig || dev.HasStats {
		t.Errorf("device message carries config/stats: config=%v stats=%v", dev.HasConfig, dev.HasStats)
	}
	if !dev.HasName || dev.HasThresh1 {
		t.Errorf("device message name/thresh = %v/%v, want present/absent", dev.HasName, dev.HasThresh1)
	}
}

// TestParseNewNeighTbl exercises the decode contract on hand-built wire shapes:
// the header read, the fixed-struct width guards, the nested NDTPA walk with its
// u64/u32 widths, and the error paths — edges the static capture does not carry.
//
// go test ./pkg/xtcpnl/ -run TestParseNewNeighTbl$
func TestParseNewNeighTbl(t *testing.T) {
	hdr := func(family uint8) []byte { return []byte{family, 0, 0, 0} }

	tests := []struct {
		description string
		body        []byte
		wantErr     error
		check       func(t *testing.T, ti NeighTblInfo)
	}{
		{
			description: "positive: header, name, one thresh and a parms nest decode",
			body: cat(hdr(unix.AF_INET),
				rtattr(NdtaName, []byte("arp_cache\x00")),
				rtattr(NdtaThresh1, le32(128)),
				rtattr(NdtaParms, cat(
					rtattr(NdtpaIfindex, le32(7)),
					rtattr(NdtpaRefcnt, le32(1)),
					rtattr(NdtpaReachableTime, le64(28789)),
				))),
			check: func(t *testing.T, ti NeighTblInfo) {
				if ti.Family != unix.AF_INET || ti.Name != "arp_cache" {
					t.Errorf("family/name = %d/%q", ti.Family, ti.Name)
				}
				if !ti.HasThresh1 || ti.Thresh1 != 128 || ti.HasThresh2 {
					t.Errorf("thresh1 = %d has2=%v", ti.Thresh1, ti.HasThresh2)
				}
				if ti.Parms.Ifindex != 7 || !ti.Parms.HasRefcnt || ti.Parms.Refcnt != 1 {
					t.Errorf("parms ifindex/refcnt = %d/%d", ti.Parms.Ifindex, ti.Parms.Refcnt)
				}
				if !ti.Parms.HasReachableTime || ti.Parms.ReachableTime != 28789 {
					t.Errorf("reachable = %d has=%v", ti.Parms.ReachableTime, ti.Parms.HasReachableTime)
				}
			},
		},
		{
			description: "boundary: a header with no attributes decodes with every Has false",
			body:        hdr(unix.AF_INET6),
			check: func(t *testing.T, ti NeighTblInfo) {
				if ti.Family != unix.AF_INET6 || ti.HasName || ti.HasConfig || ti.HasStats || ti.HasParms {
					t.Errorf("empty body decoded to %+v", ti)
				}
			},
		},
		{
			description: "boundary: a body one byte short of the header is an error",
			body:        make([]byte, NdtMsgSizeCst-1),
			wantErr:     ErrNdtmsgSmall,
		},
		{
			description: "corner: an NDTA_CONFIG shorter than the struct leaves HasConfig false",
			body:        cat(hdr(unix.AF_INET), rtattr(NdtaConfig, make([]byte, NdtConfigSizeCst-1))),
			check: func(t *testing.T, ti NeighTblInfo) {
				if ti.HasConfig {
					t.Error("short NDTA_CONFIG set HasConfig")
				}
			},
		},
		{
			description: "corner: an NDTA_STATS shorter than the struct leaves HasStats false",
			body:        cat(hdr(unix.AF_INET), rtattr(NdtaStats, make([]byte, NdtStatsSizeCst-1))),
			check: func(t *testing.T, ti NeighTblInfo) {
				if ti.HasStats {
					t.Error("short NDTA_STATS set HasStats")
				}
			},
		},
		{
			description: "boundary: an NDTA_CONFIG exactly the struct size decodes",
			body: cat(hdr(unix.AF_INET), rtattr(NdtaConfig, cat(
				le16(4), le16(432), le32(9), le32(40566), le32(4294432082),
				le32(3190857053), le32(0x0f), le32(0), le32(0)))),
			check: func(t *testing.T, ti NeighTblInfo) {
				if !ti.HasConfig || ti.Config.KeyLen != 4 || ti.Config.HashMask != 0x0f {
					t.Errorf("config = %+v has=%v", ti.Config, ti.HasConfig)
				}
			},
		},
		{
			description: "corner: an NDTPA u64 attr at exactly eight bytes decodes, at four is dropped",
			body: cat(hdr(unix.AF_INET), rtattr(NdtaParms, cat(
				rtattr(NdtpaReachableTime, le64(42)),       // 8 bytes: kept
				rtattr(NdtpaBaseReachableTime, le32(99)))), // 4 bytes: dropped
			),
			check: func(t *testing.T, ti NeighTblInfo) {
				if !ti.Parms.HasReachableTime || ti.Parms.ReachableTime != 42 {
					t.Errorf("reachable = %d has=%v, want 42/true", ti.Parms.ReachableTime, ti.Parms.HasReachableTime)
				}
				if ti.Parms.HasBaseReachableTime {
					t.Error("a 4-byte NDTPA_BASE_REACHABLE_TIME was accepted as u64")
				}
			},
		},
		{
			description: "corner: unknown NDTA and NDTPA types are ignored, known fields still decode",
			body: cat(hdr(unix.AF_INET),
				rtattr(99, le32(0xdeadbeef)),
				rtattr(NdtaThresh2, le32(512)),
				rtattr(NdtaParms, cat(rtattr(98, le32(7)), rtattr(NdtpaRefcnt, le32(5))))),
			check: func(t *testing.T, ti NeighTblInfo) {
				if !ti.HasThresh2 || ti.Thresh2 != 512 || !ti.Parms.HasRefcnt || ti.Parms.Refcnt != 5 {
					t.Errorf("known fields lost: thresh2=%d refcnt=%d", ti.Thresh2, ti.Parms.Refcnt)
				}
			},
		},
		{
			description: "negative: an attribute whose declared length exceeds the body is a parse error",
			body:        cat(hdr(unix.AF_INET), rtattrOverrunning(NdtaName, 20)),
			wantErr:     ErrRTAttrSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ti, err := ParseNewNeighTbl(tc.body)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, ti)
			}
		})
	}
}

// TestDeserializeNdtmsg covers the fixed-header read: the family byte lands, the
// pad bytes are ignored, and a body short of the header is an error.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeNdtmsg
func TestDeserializeNdtmsg(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		wantErr     error
		wantFamily  uint8
		wantN       int
	}{
		{
			description: "positive: the family byte lands and the three pad bytes are ignored",
			data:        []byte{unix.AF_INET6, 0xaa, 0xbb, 0xcc},
			wantFamily:  unix.AF_INET6,
			wantN:       NdtMsgSizeCst,
		},
		{
			description: "boundary: trailing bytes past the header are ignored and n is the header size",
			data:        []byte{unix.AF_INET, 0, 0, 0, 0xde, 0xad},
			wantFamily:  unix.AF_INET,
			wantN:       NdtMsgSizeCst,
		},
		{
			description: "boundary: a body one byte short of the header is an error",
			data:        make([]byte, NdtMsgSizeCst-1),
			wantErr:     ErrNdtmsgSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var h Ndtmsg
			n, err := DeserializeNdtmsg(tc.data, &h)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tc.wantN {
				t.Errorf("n = %d, want %d", n, tc.wantN)
			}
			if h.Family != tc.wantFamily {
				t.Errorf("Family = %d, want %d", h.Family, tc.wantFamily)
			}
		})
	}
}
