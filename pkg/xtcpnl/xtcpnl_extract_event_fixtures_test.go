package xtcpnl

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestExtractEventFixtures slices the bulk nlmon EVENT capture under
// testdata/7_1_4/ (produced by `nix run .#microvm-x86_64-nlmon-capture`) into
// one clean per-family pcap each, which the event tests read.
//
// Why a generator, and why the filtering is not optional: the bulk capture is
// genuinely mixed. `ip` issues an RTM_GET* dump before most of its
// subcommands, and the capture script runs `ip -d link/addr/route/neigh show`
// to write its sidecars, so the file contains solicited dump replies of
// exactly the same nlmsg_type as the notifications we want. Across the 7_1_4
// capture that is 123 dump replies and 33 requests sitting alongside 355
// notifications — a third of the RTM_* traffic is not an event.
//
// The discriminator is IsRtnetlinkNotification — nlmsg_flags, NOT nlmsg_pid /
// nlmsg_seq. See that function for the full reasoning; briefly, the kernel
// echoes the originating pid and seq into a notification it emits on behalf of
// a userspace change, so a pid/seq filter drops every event an operator caused
// while admitting `ip`'s own requests (sent on an unbound socket, so pid 0).
// On this capture that mistake selected 275 messages, some of them requests,
// and threw away the RTM_NEWROUTE for `ip route add 198.51.100.0/24`; the
// flags filter selects 355 genuine notifications.
//
// The test is done here in Go rather than as a BPF filter at capture time,
// because BPF cannot reach past the first netlink message in a datagram.
//
// Unlike the dump generator (xtcpnl_extract_7_1_8_fixtures_test.go), which
// collapses a multipart dump into ONE pcap record, this emits one record per
// event. Events are a time sequence, not a batch, so the fixture has to be a
// real multi-record pcap — which is also what exercises ParsePcap's iterator
// against real bytes.
//
// Emitted via writeIfChanged, so `git status` stays clean across runs.
//
// go test ./pkg/xtcpnl/ -run TestExtractEventFixtures
func TestExtractEventFixtures(t *testing.T) {
	type spec struct {
		description string
		out         string
		newType     uint16
		delType     uint16
		minNew      int // sanity floor: fewer than this means the capture is broken
		minDel      int
	}
	specs := []spec{
		{
			description: tnLinkEvents,
			out:         tdEventsLink_7_1_4,
			newType:     uint16(unix.RTM_NEWLINK),
			delType:     uint16(unix.RTM_DELLINK),
			// The trigger script does veth up/down/carrier-loss plus a dummy
			// add/del, so both senses must be well represented.
			minNew: 20,
			minDel: 2,
		},
		{
			description: tnAddrEvents,
			out:         tdEventsAddr_7_1_4,
			newType:     uint16(unix.RTM_NEWADDR),
			delType:     uint16(unix.RTM_DELADDR),
			minNew:      4, // v4 + v6, added twice (addr phase, then route phase)
			minDel:      2,
		},
		{
			description: tnRouteEvents,
			out:         tdEventsRoute_7_1_4,
			newType:     uint16(unix.RTM_NEWROUTE),
			delType:     uint16(unix.RTM_DELROUTE),
			minNew:      8, // on-link, gateway, table 100, blackhole, v6, + kernel auto-routes
			minDel:      8,
		},
		{
			description: tnNeighEvents,
			out:         tdEventsNeigh_7_1_4,
			newType:     uint16(unix.RTM_NEWNEIGH),
			delType:     uint16(unix.RTM_DELNEIGH),
			// Neighbor notifications only exist because the capture script
			// runs `ip monitor all` — nothing else in the guest joins
			// RTNLGRP_NEIGH, and the kernel does not emit to a group with no
			// subscriber. If this floor ever fails, check that first.
			minNew: 2,
			minDel: 4,
		},
	}

	bs, err := os.ReadFile(tdEventsBulk_7_1_4)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", tdEventsBulk_7_1_4, err)
	}
	global := bs[:PcapHeaderSizeCst]

	all := extractEventRecords(t, bs)
	if len(all) == 0 {
		t.Fatalf("%s: no kernel events found in the bulk capture", tdEventsBulk_7_1_4)
	}

	for _, sp := range specs {
		t.Run(sp.description, func(t *testing.T) {
			var nNew, nDel int
			// Upper bound: this family's slice can hold at most every event.
			recs := make([][]byte, 0, len(all))
			for _, e := range all {
				switch e.mtype {
				case sp.newType:
					nNew++
				case sp.delType:
					nDel++
				default:
					continue
				}
				recs = append(recs, buildEventRecord(e.cooked, e.full, e.ts))
			}

			if nNew < sp.minNew {
				t.Fatalf("%s: %d %s events, want >= %d", sp.out, nNew, rtmEventName(sp.newType), sp.minNew)
			}
			if nDel < sp.minDel {
				t.Fatalf("%s: %d %s events, want >= %d", sp.out, nDel, rtmEventName(sp.delType), sp.minDel)
			}

			pcap := make([]byte, 0, len(global)+len(recs)*256)
			pcap = append(pcap, global...)
			for _, r := range recs {
				pcap = append(pcap, r...)
			}

			// Self-check the round trip the way the event tests will read it:
			// every record must parse, and every message must be an event of
			// this family that ParseRtnetlinkEvent accepts.
			_, back, perr := ParseNetlinkPcap(pcap)
			if perr != nil {
				t.Fatalf("%s: emitted fixture does not re-parse: %v", sp.out, perr)
			}
			if len(back) != len(recs) {
				t.Fatalf("%s: emitted %d records, re-read %d", sp.out, len(recs), len(back))
			}
			for i, r := range back {
				family, body, nerr := r.NetlinkPayload()
				if nerr != nil {
					t.Fatalf("%s: record %d: %v", sp.out, i, nerr)
				}
				if family != unix.NETLINK_ROUTE {
					t.Fatalf("%s: record %d: family %d, want NETLINK_ROUTE", sp.out, i, family)
				}
				var h NlMsgHdr
				if _, herr := DeserializeNlMsgHdr(body, &h); herr != nil {
					t.Fatalf("%s: record %d: %v", sp.out, i, herr)
				}
				if h.Type != sp.newType && h.Type != sp.delType {
					t.Fatalf("%s: record %d: nlmsg_type %d is not in this family", sp.out, i, h.Type)
				}
				if !IsRtnetlinkNotification(h) {
					t.Fatalf("%s: record %d: nlmsg_flags %#x is a request or dump reply, not a notification",
						sp.out, i, h.Flags)
				}
				if _, eerr := ParseRtnetlinkEvent(h.Type, body[NlMsgHdrSizeCst:h.Len]); eerr != nil {
					t.Fatalf("%s: record %d: ParseRtnetlinkEvent: %v", sp.out, i, eerr)
				}
			}

			if werr := writeIfChanged(filepath.Clean(sp.out), pcap); werr != nil {
				t.Fatalf("write %s: %v", sp.out, werr)
			}
			t.Logf("%s: %d add + %d del events, fixture %d bytes",
				sp.description, nNew, nDel, len(pcap))
		})
	}
}

// eventMsg is one kernel-originated rtnetlink notification lifted out of the
// bulk capture, with the cooked header and timestamp of the record it arrived
// in so the slice can be re-emitted as a faithful pcap record.
type eventMsg struct {
	mtype  uint16
	full   []byte // whole message: nlmsghdr + body
	cooked []byte // the record's 16-byte Linux SLL header
	ts     PcapRecordHeader
}

// extractEventRecords walks the bulk capture and returns every kernel-
// originated event message, in capture order.
//
// A record can hold several netlink messages, so each is walked individually;
// a record that mixes an event with a dump reply contributes only the event.
func extractEventRecords(t *testing.T, bs []byte) []eventMsg {
	t.Helper()

	_, records, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("ParseNetlinkPcap: %v", err)
	}

	var out []eventMsg
	for _, r := range records {
		family, body, perr := r.NetlinkPayload()
		if perr != nil || family != unix.NETLINK_ROUTE {
			continue
		}
		cooked := r.Data[:NetlinkCookedHeaderSizeCst]

		for len(body) >= NlMsgHdrSizeCst {
			var h NlMsgHdr
			if _, herr := DeserializeNlMsgHdr(body, &h); herr != nil {
				break
			}
			mlen := int(h.Len)
			if mlen < NlMsgHdrSizeCst || mlen > len(body) {
				break
			}
			if IsRtnetlinkNotification(h) {
				out = append(out, eventMsg{
					mtype:  h.Type,
					full:   append([]byte(nil), body[:mlen]...),
					cooked: append([]byte(nil), cooked...),
					ts:     r.Header,
				})
			}
			adv := mlen + FourByteAlignPadding(mlen)
			if adv <= 0 || adv > len(body) {
				break
			}
			body = body[adv:]
		}
	}
	return out
}

// buildEventRecord wraps one event message in a pcap record: a 16-byte record
// header carrying the original capture timestamp, the original cooked header,
// then the message.
//
// The real timestamps are kept rather than zeroed. They are deterministic (the
// input pcap is committed), and they let a reader line the fixture up against
// the ip_monitor_all sidecar, which is timestamped by the same clock.
func buildEventRecord(cooked, msg []byte, ts PcapRecordHeader) []byte {
	payload := make([]byte, 0, len(cooked)+len(msg))
	payload = append(payload, cooked...)
	payload = append(payload, msg...)

	rec := make([]byte, PcapRecordHeaderSizeCst)
	binary.LittleEndian.PutUint32(rec[0:4], ts.TsSec)
	binary.LittleEndian.PutUint32(rec[4:8], ts.TsXsec)
	binary.LittleEndian.PutUint32(rec[8:12], uint32(len(payload)))
	binary.LittleEndian.PutUint32(rec[12:16], uint32(len(payload)))

	out := make([]byte, 0, len(rec)+len(payload))
	out = append(out, rec...)
	out = append(out, payload...)
	return out
}

// rtmEventName labels the eight event types for test diagnostics. It is
// separate from rtmName (xtcpnl_extract_7_1_8_fixtures_test.go), which only
// knows the three RTM_NEW* types the dump generator emits.
func rtmEventName(t uint16) string {
	switch t {
	case uint16(unix.RTM_NEWLINK):
		return "RTM_NEWLINK"
	case uint16(unix.RTM_DELLINK):
		return "RTM_DELLINK"
	case uint16(unix.RTM_NEWADDR):
		return "RTM_NEWADDR"
	case uint16(unix.RTM_DELADDR):
		return "RTM_DELADDR"
	case uint16(unix.RTM_NEWROUTE):
		return "RTM_NEWROUTE"
	case uint16(unix.RTM_DELROUTE):
		return "RTM_DELROUTE"
	case uint16(unix.RTM_NEWNEIGH):
		return "RTM_NEWNEIGH"
	case uint16(unix.RTM_DELNEIGH):
		return "RTM_DELNEIGH"
	default:
		return "RTM_?"
	}
}
