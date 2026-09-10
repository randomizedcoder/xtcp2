package xtcpnl

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestExtract7_1_8_Fixtures turns the three raw nlmon captures under
// testdata/7_1_8/ (produced by `nix run .#capture-netlink-fixtures`) into clean,
// single-record "dump" pcaps the rtnetlink deserialize tests read.
//
// Why a generator: nlmon mirrors EVERY NETLINK_ROUTE datagram in the namespace,
// so a raw capture interleaves our RTM_GET* dump with unrelated route traffic
// (neighbour dumps, per-ifindex link lookups the `ip` tool issues, etc.), and
// nlmsg_seq collides across sockets. Our dump is the set of messages sharing one
// (nlmsg_seq, nlmsg_pid) that forms a run of RTM_NEW* of one family terminated by
// NLMSG_DONE. This test isolates that run and re-emits it as one pcap record
// (cooked header + the concatenated messages), so the fixture is deterministic
// and small while every byte remains real kernel output.
//
// It emits (via writeIfChanged, so `git status` stays clean across runs):
//   - netlink_route_getlink_dump.pcap     RTM_NEWLINK dump   + NLMSG_DONE
//   - netlink_route_getaddr_v4_dump.pcap  RTM_NEWADDR (AF_INET) dump  + DONE
//   - netlink_route_getaddr_v6_dump.pcap  RTM_NEWADDR (AF_INET6) dump + DONE
//   - netlink_route_getroute_dump.pcap    RTM_NEWROUTE dump  + NLMSG_DONE
//
// The test fails if any target dump cannot be reconstructed, so a
// renamed/missing/short bulk capture is caught immediately.
//
// go test ./pkg/xtcpnl/ -run TestExtract7_1_8_Fixtures
func TestExtract7_1_8_Fixtures(t *testing.T) {
	const outDir = "./testdata/7_1_8"

	type spec struct {
		description string
		bulk        string
		out         string
		newType     uint16
		wantFamily  int // -1 = any; else match the RTM_NEW* body's family byte
		minMsgs     int // minimum RTM_NEW* messages expected (sanity floor)
	}
	specs := []spec{
		{
			description: tnGetLinkDump,
			bulk:        tdRouteBulkGetLink_7_1_8,
			out:         tdRouteGetLinkDump_7_1_8,
			newType:     uint16(unix.RTM_NEWLINK),
			wantFamily:  -1,
			minMsgs:     2, // at least lo + one real NIC
		},
		{
			description: tnGetAddrV4Dump,
			bulk:        tdRouteBulkGetAddr_7_1_8,
			out:         tdRouteGetAddrV4Dump_7_1_8,
			newType:     uint16(unix.RTM_NEWADDR),
			wantFamily:  unix.AF_INET,
			minMsgs:     2, // 127.0.0.1 + at least one global v4
		},
		{
			description: tnGetAddrV6Dump,
			bulk:        tdRouteBulkGetAddr_7_1_8,
			out:         tdRouteGetAddrV6Dump_7_1_8,
			newType:     uint16(unix.RTM_NEWADDR),
			wantFamily:  unix.AF_INET6,
			minMsgs:     2, // ::1 + at least one global/ULA v6
		},
		{
			description: tnGetRouteDump,
			bulk:        tdRouteBulkGetRoute_7_1_8,
			out:         tdRouteGetRouteDump_7_1_8,
			newType:     uint16(unix.RTM_NEWROUTE),
			wantFamily:  -1,
			minMsgs:     4,
		},
	}

	for _, sp := range specs {
		t.Run(sp.description, func(t *testing.T) {
			bs, err := os.ReadFile(sp.bulk)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", sp.bulk, err)
			}
			msgs := extractDumpMessages(t, bs)
			if len(msgs) == 0 {
				t.Fatalf("%s: no netlink messages found in bulk capture", sp.bulk)
			}

			full, cooked, count, ok := pickDump(msgs, sp.newType, sp.wantFamily)
			if !ok {
				t.Fatalf("%s: no %s dump (family=%d) terminated by NLMSG_DONE found",
					sp.bulk, rtmName(sp.newType), sp.wantFamily)
			}
			if count < sp.minMsgs {
				t.Fatalf("%s: dump has %d %s messages, want >= %d",
					sp.bulk, count, rtmName(sp.newType), sp.minMsgs)
			}

			pcap := buildSingleRecordPcap(bs[:PcapHeaderSizeCst], cooked, full)

			// Re-read our own emitted fixture the way the deserialize tests do,
			// as a self-check that the round trip is walkable and ends in DONE.
			var sawDone bool
			var newCount int
			walkDumpPayload(pcap[PcapNetlinkOffsetCst:], func(mt uint16, _ []byte) bool {
				if mt == uint16(unix.NLMSG_DONE) {
					sawDone = true
					return false
				}
				if mt == sp.newType {
					newCount++
				}
				return true
			})
			if !sawDone {
				t.Fatalf("%s: emitted fixture is not terminated by NLMSG_DONE", sp.out)
			}
			if newCount != count {
				t.Fatalf("%s: emitted %d %s messages, extracted %d", sp.out, newCount, rtmName(sp.newType), count)
			}

			if werr := writeIfChanged(filepath.Clean(sp.out), pcap); werr != nil {
				t.Fatalf("write %s: %v", sp.out, werr)
			}
			t.Logf("extracted %s: %d %s messages, fixture %d bytes", sp.description, count, rtmName(sp.newType), len(pcap))
		})
	}
}

// dumpMsg is one netlink message lifted out of a bulk nlmon capture, tagged with
// the (seq,pid) that identifies its dump and the cooked header of the record it
// came from. full is the whole 4-byte-aligned message (nlmsghdr + body); body is
// the bytes after the 16-byte nlmsghdr.
type dumpMsg struct {
	seq    uint32
	pid    uint32
	mtype  uint16
	full   []byte
	body   []byte
	cooked []byte
}

// extractDumpMessages walks a bulk nlmon pcap and returns every netlink message
// in capture order, walking ALL messages within each record (a dump reply packs
// many multipart messages into one datagram) — unlike the single-message
// sock_diag reader in xtcpnl_extract_7_0_3_fixtures_test.go.
func extractDumpMessages(t *testing.T, bs []byte) []dumpMsg {
	t.Helper()
	if len(bs) < PcapHeaderSizeCst {
		t.Fatalf("pcap too small: %d bytes", len(bs))
	}
	var ph PcapHeader
	if _, err := DeserializePcapHeader(bs[:PcapHeaderSizeCst], &ph); err != nil {
		t.Fatalf("DeserializePcapHeader: %v", err)
	}

	var out []dumpMsg
	off := PcapHeaderSizeCst
	for off+PcapRecordHeaderSizeCst <= len(bs) {
		var prh PcapRecordHeader
		if _, err := DeserializePcapRecordHeader(bs[off:off+PcapRecordHeaderSizeCst], &prh); err != nil {
			t.Fatalf("DeserializePcapRecordHeader at off=%d: %v", off, err)
		}
		dataStart := off + PcapRecordHeaderSizeCst
		dataEnd := dataStart + int(prh.CapLen)
		if dataEnd > len(bs) {
			break
		}
		off = dataEnd

		if int(prh.CapLen) < NetlinkCookedHeaderSizeCst+NlMsgHdrSizeCst {
			continue
		}
		cooked := bs[dataStart : dataStart+NetlinkCookedHeaderSizeCst]
		p := dataStart + NetlinkCookedHeaderSizeCst
		for p+NlMsgHdrSizeCst <= dataEnd {
			var h NlMsgHdr
			if _, err := DeserializeNlMsgHdr(bs[p:p+NlMsgHdrSizeCst], &h); err != nil {
				break
			}
			mlen := int(h.Len)
			if mlen < NlMsgHdrSizeCst || p+mlen > dataEnd {
				break
			}
			out = append(out, dumpMsg{
				seq:    h.Seq,
				pid:    h.Pid,
				mtype:  h.Type,
				full:   append([]byte(nil), bs[p:p+mlen]...),
				body:   append([]byte(nil), bs[p+NlMsgHdrSizeCst:p+mlen]...),
				cooked: append([]byte(nil), cooked...),
			})
			adv := mlen + FourByteAlignPadding(mlen)
			if adv <= 0 || p+adv > dataEnd {
				break
			}
			p += adv
		}
	}
	return out
}

// pickDump selects the (seq,pid) group that forms our dump: a run of newType
// messages (optionally matching wantFamily on the RTM_NEW* body's first byte)
// terminated by NLMSG_DONE. It returns that group's messages in capture order
// (including the trailing DONE), a representative cooked header, and the newType
// count. When several groups qualify (e.g. the v4 and v6 GETADDR dumps in one
// capture), the one with the most newType messages wins.
func pickDump(msgs []dumpMsg, newType uint16, wantFamily int) (full [][]byte, cooked []byte, count int, ok bool) {
	type key struct {
		seq uint32
		pid uint32
	}
	// Preserve first-seen order of groups for deterministic tie-breaking.
	order := make([]key, 0)
	groups := make(map[key][]dumpMsg)
	for _, m := range msgs {
		k := key{m.seq, m.pid}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], m)
	}

	best := -1
	var bestKey key
	for _, k := range order {
		g := groups[k]
		var hasDone bool
		var n int
		var famOK bool
		for _, m := range g {
			switch m.mtype {
			case uint16(unix.NLMSG_DONE):
				hasDone = true
			case newType:
				n++
				if !famOK && len(m.body) > 0 {
					if wantFamily < 0 || int(m.body[0]) == wantFamily {
						famOK = true
					}
				}
			}
		}
		if !hasDone || n == 0 || !famOK {
			continue
		}
		if n > best {
			best = n
			bestKey = k
		}
	}
	if best < 0 {
		return nil, nil, 0, false
	}

	g := groups[bestKey]
	full = make([][]byte, 0, len(g))
	for _, m := range g {
		// Keep the dump's RTM_NEW* messages and its terminating DONE; drop any
		// stray NOOP/other types that happened to share the (seq,pid).
		if m.mtype == newType || m.mtype == uint16(unix.NLMSG_DONE) {
			full = append(full, m.full)
		}
		if cooked == nil {
			cooked = m.cooked
		}
	}
	return full, cooked, best, true
}

// buildSingleRecordPcap wraps a cooked header + the concatenated (already
// 4-byte-aligned) netlink messages into a one-record pcap: the original 24-byte
// global header, a 16-byte record header with zeroed timestamps (for
// determinism), then the packet payload.
func buildSingleRecordPcap(global, cooked []byte, msgs [][]byte) []byte {
	payload := make([]byte, 0, len(cooked)+64)
	payload = append(payload, cooked...)
	for _, m := range msgs {
		payload = append(payload, m...)
		if pad := FourByteAlignPadding(len(m)); pad > 0 {
			payload = append(payload, make([]byte, pad)...)
		}
	}

	rec := make([]byte, PcapRecordHeaderSizeCst)
	// TsSec/TsXsec left zero.
	binary.LittleEndian.PutUint32(rec[8:12], uint32(len(payload)))  // CapLen
	binary.LittleEndian.PutUint32(rec[12:16], uint32(len(payload))) // Len

	out := make([]byte, 0, len(global)+len(rec)+len(payload))
	out = append(out, global...)
	out = append(out, rec...)
	out = append(out, payload...)
	return out
}

// walkDumpPayload walks the netlink messages in a datagram payload (the bytes
// after the 16-byte cooked header), calling fn with each message type and its
// body (bytes after the nlmsghdr). fn returns false to stop early. It mirrors
// DumpRtnetlink's in-buffer loop and is shared by the extract self-check and the
// deserialize tests.
func walkDumpPayload(data []byte, fn func(mtype uint16, body []byte) bool) {
	for len(data) >= NlMsgHdrSizeCst {
		var h NlMsgHdr
		if _, err := DeserializeNlMsgHdr(data, &h); err != nil {
			return
		}
		mlen := int(h.Len)
		if mlen < NlMsgHdrSizeCst || mlen > len(data) {
			return
		}
		if !fn(h.Type, data[NlMsgHdrSizeCst:mlen]) {
			return
		}
		adv := mlen + FourByteAlignPadding(mlen)
		if adv <= 0 || adv > len(data) {
			return
		}
		data = data[adv:]
	}
}

// rtmName gives a short label for the RTM_NEW* message types used in test
// diagnostics.
func rtmName(t uint16) string {
	switch t {
	case uint16(unix.RTM_NEWLINK):
		return "RTM_NEWLINK"
	case uint16(unix.RTM_NEWADDR):
		return "RTM_NEWADDR"
	case uint16(unix.RTM_NEWROUTE):
		return "RTM_NEWROUTE"
	default:
		return "RTM_?"
	}
}
