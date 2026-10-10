package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// statsPcap is `ip stats show group link`'s capture: the ll_init_map link dump
// followed by the RTM_GETSTATS AF_UNSPEC dump. Only the RTM_NEWSTATS bodies are
// fed to the parser here. statsDevPcap is the `dev goip0` point get — a single
// non-dump RTM_NEWSTATS reply.
const (
	statsPcap    = "testdata/7_1_4/dumps/netlink_route_getstats.pcap"
	statsDevPcap = "testdata/7_1_4/dumps/netlink_route_getstats_dev.pcap"
)

// ifStatsBodies returns the if_stats_msg bodies (nlmsghdr stripped) of every
// RTM_NEWSTATS message in the pcap, in capture order.
func ifStatsBodies(t *testing.T, path string) [][]byte {
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
		for off := 0; off+NlMsgHdrSizeCst <= len(body); {
			msgLen := int(binary.LittleEndian.Uint32(body[off : off+4]))
			if msgLen < NlMsgHdrSizeCst || off+msgLen > len(body) {
				break
			}
			if binary.LittleEndian.Uint16(body[off+4:off+6]) == uint16(unix.RTM_NEWSTATS) {
				out = append(out, body[off+NlMsgHdrSizeCst:off+msgLen])
			}
			off += (msgLen + 3) &^ 3 // NLMSG_ALIGN
		}
	}
	return out
}

// ifsmHdr builds the 12-byte if_stats_msg header the test bodies share.
func ifsmHdr(family uint8, ifindex, filterMask uint32) []byte {
	return cat([]byte{family, 0, 0, 0}, le32(ifindex), le32(filterMask))
}

// link64Payload builds a full rtnl_link_stats64 payload with rx_bytes/tx_bytes
// set (fields 2 and 3 of the 25-u64 struct) and everything else zero.
func link64Payload(rxBytes, txBytes uint64) []byte {
	p := make([]byte, RtnlLinkStats64SizeCst)
	binary.LittleEndian.PutUint64(p[16:24], rxBytes) // RxBytes
	binary.LittleEndian.PutUint64(p[24:32], txBytes) // TxBytes
	return p
}

// TestParseNewStatsCaptured decodes the real RTM_NEWSTATS bodies from the
// committed `group link` pcap: five records (ifindex 1..5), each carrying one
// IFLA_STATS_LINK_64 and no other group. The counters are frozen in the pcap, so
// the two that are non-zero (nlmsg0 rx, goip0 tx) are asserted exactly — the
// capture is the ground truth the goldens were rendered from.
//
// go test ./pkg/xtcpnl/ -run TestParseNewStatsCaptured
func TestParseNewStatsCaptured(t *testing.T) {
	bodies := ifStatsBodies(t, statsPcap)
	if got := len(bodies); got != 5 {
		t.Fatalf("RTM_NEWSTATS message count = %d, want 5", got)
	}
	for i, body := range bodies {
		si, err := ParseNewStats(body)
		if err != nil {
			t.Fatalf("ParseNewStats(body %d): %v", i, err)
		}
		if want := uint32(i + 1); si.Ifindex != want {
			t.Errorf("body %d ifindex = %d, want %d", i, si.Ifindex, want)
		}
		if !si.HasLink64 {
			t.Errorf("body %d missing IFLA_STATS_LINK_64", i)
		}
		if si.HasUnsupportedGroup {
			t.Errorf("body %d reports an unsupported group; `group link` must be link-only", i)
		}
	}
	// ifindex 2 (nlmon0) carried rx_bytes 226380; ifindex 3 (goip0) tx_bytes 390.
	rec2, _ := ParseNewStats(bodies[1])
	if rec2.Link64.RxBytes != 226380 {
		t.Errorf("ifindex 2 rx_bytes = %d, want 226380", rec2.Link64.RxBytes)
	}
	rec3, _ := ParseNewStats(bodies[2])
	if rec3.Link64.TxBytes != 390 {
		t.Errorf("ifindex 3 tx_bytes = %d, want 390", rec3.Link64.TxBytes)
	}

	// The point-get pcap carries exactly one reply, the goip0 record.
	devBodies := ifStatsBodies(t, statsDevPcap)
	if got := len(devBodies); got != 1 {
		t.Fatalf("point-get RTM_NEWSTATS count = %d, want 1", got)
	}
	dev, err := ParseNewStats(devBodies[0])
	if err != nil {
		t.Fatalf("ParseNewStats(dev): %v", err)
	}
	if dev.Ifindex != 3 || !dev.HasLink64 || dev.Link64.TxBytes != 390 {
		t.Errorf("dev record = ifindex %d/link64 %v/tx %d, want 3/true/390", dev.Ifindex, dev.HasLink64, dev.Link64.TxBytes)
	}
}

// TestParseNewStats exercises the decode contract on hand-built wire shapes: the
// header readback, the tolerant link-stats decode (short and empty payloads still
// count as present), the unsupported-group flag, unknown-type skipping, and the
// error paths — edges the static capture does not carry.
//
// go test ./pkg/xtcpnl/ -run TestParseNewStats$
func TestParseNewStats(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		wantErr     error
		check       func(t *testing.T, si IfStatsInfo)
	}{
		{
			description: "positive: header + full LINK_64 payload decodes ifindex, mask and counters",
			body:        cat(ifsmHdr(unix.AF_UNSPEC, 3, StatsFilterLink64), rtattr(uint16(unix.IFLA_STATS_LINK_64), link64Payload(12345, 6789))),
			check: func(t *testing.T, si IfStatsInfo) {
				if si.Ifindex != 3 || si.FilterMask != StatsFilterLink64 {
					t.Errorf("ifindex/mask = %d/%#x, want 3/0x1", si.Ifindex, si.FilterMask)
				}
				if !si.HasLink64 || si.HasUnsupportedGroup {
					t.Errorf("flags = link64 %v/unsup %v, want true/false", si.HasLink64, si.HasUnsupportedGroup)
				}
				if si.Link64.RxBytes != 12345 || si.Link64.TxBytes != 6789 {
					t.Errorf("rx/tx = %d/%d, want 12345/6789", si.Link64.RxBytes, si.Link64.TxBytes)
				}
			},
		},
		{
			description: "boundary: a bare 12-byte header with no attributes yields neither group",
			body:        ifsmHdr(unix.AF_UNSPEC, 7, StatsFilterLink64),
			check: func(t *testing.T, si IfStatsInfo) {
				if si.HasLink64 || si.HasUnsupportedGroup || si.Ifindex != 7 {
					t.Errorf("want ifindex 7 and no groups, got %+v", si)
				}
			},
		},
		{
			description: "corner: a short LINK_64 payload is still present (DeserializeRtnlLinkStats64 zero-fills)",
			body:        cat(ifsmHdr(unix.AF_UNSPEC, 1, StatsFilterLink64), rtattr(uint16(unix.IFLA_STATS_LINK_64), make([]byte, 8))),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasLink64 {
					t.Error("short LINK_64 was dropped; the decode is tolerant and must set HasLink64")
				}
			},
		},
		{
			description: "corner: an empty LINK_64 payload is still present (tolerant decode never errors)",
			body:        cat(ifsmHdr(unix.AF_UNSPEC, 1, StatsFilterLink64), rtattr(uint16(unix.IFLA_STATS_LINK_64), nil)),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasLink64 || si.Link64.RxBytes != 0 {
					t.Errorf("empty LINK_64 = present %v/rx %d, want true/0", si.HasLink64, si.Link64.RxBytes)
				}
			},
		},
		{
			// IFLA_STATS_LINK_XSTATS(2) is no longer unsupported — it is the bridge
			// group, decoded by parseBridgeXstats (see TestParseNewStatsXstats).
			// LINK_XSTATS_SLAVE(3) is the still-unsupported group this row now guards.
			description: "corner: IFLA_STATS_LINK_XSTATS_SLAVE(3) alongside LINK_64 sets HasUnsupportedGroup, keeps HasLink64",
			body: cat(ifsmHdr(unix.AF_UNSPEC, 6, StatsFilterLink64),
				rtattr(uint16(unix.IFLA_STATS_LINK_64), link64Payload(1, 2)),
				rtattr(uint16(unix.IFLA_STATS_LINK_XSTATS_SLAVE), le32(0))),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasLink64 || !si.HasUnsupportedGroup {
					t.Errorf("flags = link64 %v/unsup %v, want true/true", si.HasLink64, si.HasUnsupportedGroup)
				}
			},
		},
		{
			description: "corner: IFLA_STATS_AF_SPEC(5) alone sets only HasUnsupportedGroup",
			body:        cat(ifsmHdr(unix.AF_UNSPEC, 6, 0x10), rtattr(uint16(unix.IFLA_STATS_AF_SPEC), le32(0))),
			check: func(t *testing.T, si IfStatsInfo) {
				if si.HasLink64 || !si.HasUnsupportedGroup {
					t.Errorf("flags = link64 %v/unsup %v, want false/true", si.HasLink64, si.HasUnsupportedGroup)
				}
			},
		},
		{
			description: "corner: an unknown IFLA_STATS type (99) is ignored, siblings unaffected",
			body: cat(ifsmHdr(unix.AF_UNSPEC, 1, StatsFilterLink64),
				rtattr(99, le32(0xdeadbeef)),
				rtattr(uint16(unix.IFLA_STATS_LINK_64), link64Payload(5, 0))),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasLink64 || si.HasUnsupportedGroup || si.Link64.RxBytes != 5 {
					t.Errorf("want link64 only with rx 5, got %+v", si)
				}
			},
		},
		{
			description: "negative: a zero-length body is an error",
			body:        []byte{},
			wantErr:     ErrIfStatsMsgSmall,
		},
		{
			description: "negative: a body one byte short of the header is an error",
			body:        make([]byte, IfStatsMsgSizeCst-1),
			wantErr:     ErrIfStatsMsgSmall,
		},
		{
			description: "negative: an attribute whose declared length exceeds the body is a parse error",
			body:        cat(ifsmHdr(unix.AF_UNSPEC, 1, StatsFilterLink64), rtattrOverrunning(uint16(unix.IFLA_STATS_LINK_64), 40)),
			wantErr:     ErrRTAttrSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			si, err := ParseNewStats(tc.body)
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
				tc.check(t, si)
			}
		})
	}
}

// TestDeserializeIfStatsMsg covers the fixed 12-byte header read: family,
// ifindex and filter_mask land, the two pad bytes are ignored, and a body short
// of the header is an error.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeIfStatsMsg
func TestDeserializeIfStatsMsg(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		wantErr     error
		wantFamily  uint8
		wantIfindex uint32
		wantMask    uint32
	}{
		{
			description: "positive: family/ifindex/filter_mask all land",
			data:        ifsmHdr(unix.AF_UNSPEC, 3, StatsFilterLink64),
			wantFamily:  unix.AF_UNSPEC,
			wantIfindex: 3,
			wantMask:    StatsFilterLink64,
		},
		{
			description: "positive: pad bytes are ignored, large ifindex round-trips",
			data:        cat([]byte{unix.AF_INET, 0xaa, 0xbb, 0xcc}, le32(0xffffffff), le32(0x1f)),
			wantFamily:  unix.AF_INET,
			wantIfindex: 0xffffffff,
			wantMask:    0x1f,
		},
		{
			description: "negative: a zero-length body is an error",
			data:        []byte{},
			wantErr:     ErrIfStatsMsgSmall,
		},
		{
			description: "boundary: a body one byte short of the header is an error",
			data:        make([]byte, IfStatsMsgSizeCst-1),
			wantErr:     ErrIfStatsMsgSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var h IfStatsMsg
			n, err := DeserializeIfStatsMsg(tc.data, &h)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != IfStatsMsgSizeCst {
				t.Errorf("n = %d, want %d", n, IfStatsMsgSizeCst)
			}
			if h.Family != tc.wantFamily || h.Ifindex != tc.wantIfindex || h.FilterMask != tc.wantMask {
				t.Errorf("header = %+v, want family %d/ifindex %d/mask %#x", h, tc.wantFamily, tc.wantIfindex, tc.wantMask)
			}
		})
	}
}

// TestBuildStatsRequests pins the two request builders byte-for-byte. The dump
// and point-get hex are transcribed from the recorded requests in the committed
// pcaps (capture seq, goip0 = ifindex 3). The point get carries NLM_F_REQUEST
// alone — ipstats_show_one sets only REQUEST and rtnl_talk adds no ACK, so the
// captured flag word is 0x0001, unlike netconf's REQUEST|ACK point get. A large
// ifindex is a contract row exercising the attribute-free header.
//
// go test ./pkg/xtcpnl/ -run TestBuildStatsRequests
func TestBuildStatsRequests(t *testing.T) {
	// Captured AF_UNSPEC dump: len 28, RTM_GETSTATS, DUMP|REQUEST, seq, ifindex 0,
	// filter_mask 0x1 (the link group).
	wantDump := mustHex(t, "1c0000005e000103c87cc96a00000000000000000000000001000000")
	gotDump := BuildDumpStatsRequest(unix.AF_UNSPEC, 0, StatsFilterLink64, 1791589576)
	if !bytes.Equal(gotDump, wantDump) {
		t.Errorf("dump request mismatch\ngot  %s\nwant %s", hex.EncodeToString(gotDump), hex.EncodeToString(wantDump))
	}
	if flags := binary.LittleEndian.Uint16(gotDump[6:8]); flags&uint16(unix.NLM_F_DUMP) == 0 {
		t.Errorf("dump flags %#x lack NLM_F_DUMP", flags)
	}
	if len(gotDump) != NlMsgHdrSizeCst+IfStatsMsgSizeCst {
		t.Errorf("dump request len = %d, want %d (no attributes)", len(gotDump), NlMsgHdrSizeCst+IfStatsMsgSizeCst)
	}

	// Captured whole-group xstats dump: identical to the link dump but filter_mask
	// 0x2, and crucially NO IFLA_STATS_GET_FILTERS nest (the attribute-free header
	// is `group xstats`'s request — the nest appears only for partial selection).
	wantXstats := mustHex(t, "1c0000005e0001031eb5c96a00000000000000000000000002000000")
	gotXstats := BuildDumpStatsRequest(unix.AF_UNSPEC, 0, StatsFilterXstats, 1791603998)
	if !bytes.Equal(gotXstats, wantXstats) {
		t.Errorf("xstats dump request mismatch\ngot  %s\nwant %s", hex.EncodeToString(gotXstats), hex.EncodeToString(wantXstats))
	}
	if len(gotXstats) != NlMsgHdrSizeCst+IfStatsMsgSizeCst {
		t.Errorf("xstats dump len = %d, want %d (no GET_FILTERS nest)", len(gotXstats), NlMsgHdrSizeCst+IfStatsMsgSizeCst)
	}

	// Captured point get: len 28, RTM_GETSTATS, REQUEST only (no DUMP, no ACK),
	// ifindex 3, filter_mask 0x1.
	wantGet := mustHex(t, "1c0000005e000100c97cc96a00000000000000000300000001000000")
	gotGet, err := BuildGetStatsByIndexRequest(3, StatsFilterLink64, 1791589577)
	if err != nil {
		t.Fatalf("BuildGetStatsByIndexRequest: %v", err)
	}
	if !bytes.Equal(gotGet, wantGet) {
		t.Errorf("point get mismatch\ngot  %s\nwant %s", hex.EncodeToString(gotGet), hex.EncodeToString(wantGet))
	}
	flags := binary.LittleEndian.Uint16(gotGet[6:8])
	if flags != uint16(unix.NLM_F_REQUEST) {
		t.Errorf("point get flags %#x, want exactly NLM_F_REQUEST (0x1): no DUMP, no ACK", flags)
	}

	// Contract: a large ifindex travels as 0xffffffff in the header, mask unchanged.
	wantGetBig := mustHex(t, "1c0000005e000100010000000000000000000000ffffffff01000000")
	gotGetBig, err := BuildGetStatsByIndexRequest(0xffffffff, StatsFilterLink64, 1)
	if err != nil {
		t.Fatalf("BuildGetStatsByIndexRequest(big): %v", err)
	}
	if !bytes.Equal(gotGetBig, wantGetBig) {
		t.Errorf("point get (big ifindex) mismatch\ngot  %s\nwant %s", hex.EncodeToString(gotGetBig), hex.EncodeToString(wantGetBig))
	}
}
