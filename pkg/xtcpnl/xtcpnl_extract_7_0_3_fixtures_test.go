package xtcpnl

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// TestExtract7_0_3_Fixtures walks the bulk 7_0_3 nlmon capture, finds three
// representative ESTABLISHED sockets from `ss --tcp --info -n`, and writes
// per-socket fixture files into testdata/7_0_3/:
//
//   - <name>.pcap : a complete single-packet pcap (global header + one record).
//   - <name>_info : the INET_DIAG_INFO RTAttr (including its 4-byte header),
//     ready to feed DeserializeTCPInfo after slicing off [RTAttrSizeCst:].
//
// The writes are byte-identical across runs, so `git status` stays clean. The
// test also doubles as an integration check of the pcap -> cooked -> netlink
// -> inet_diag -> RTAttr chain on a real-world capture.
//
// go test --run TestExtract7_0_3_Fixtures
func TestExtract7_0_3_Fixtures(t *testing.T) {
	const (
		bulkPcap = "./testdata/7_0_3/7_0_3_ss_tcp_info.pcap"
		outDir   = "./testdata/7_0_3"
	)

	bs, err := os.ReadFile(bulkPcap)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", bulkPcap, err)
	}

	if len(bs) < PcapHeaderSizeCst {
		t.Fatalf("pcap too small: %d bytes", len(bs))
	}

	var ph PcapHeader
	if _, err := DeserializePcapHeader(bs[:PcapHeaderSizeCst], &ph); err != nil {
		t.Fatalf("DeserializePcapHeader: %v", err)
	}

	// Targets transcribed from testdata/7_0_3/ss_tcp_info_n. Chosen because
	// they (a) are present in the bulk pcap (it's a short capture of only ~5
	// inet_diag responses out of ss's 15 sockets) and (b) cover variety:
	//   line 2  ESTAB 10.0.6.188:26546 -> 140.82.114.25:443  (clean v4, wscale 10,9)
	//   line 13 ESTAB 10.0.6.188:63282 -> 3.140.122.174:443  (v4 with rcv_rtt:612.859, wscale 12,9)
	//   line 28 ESTAB     [::1]:19000  -> [::1]:10156        (IPv6 loopback)
	type target struct {
		name    string
		family  uint8
		sport   uint16
		dport   uint16
		dstAddr [16]byte
	}
	v4 := func(s string) [16]byte {
		var out [16]byte
		copy(out[:4], net.ParseIP(s).To4())
		return out
	}
	v6 := func(s string) [16]byte {
		var out [16]byte
		copy(out[:], net.ParseIP(s).To16())
		return out
	}
	targets := []target{
		{name: "netlink_sock_diag_response_7_0_3_sport26546_dport443", family: 2, sport: 26546, dport: 443, dstAddr: v4("140.82.114.25")},
		{name: "netlink_sock_diag_response_7_0_3_sport63282_dport443_rcvrtt", family: 2, sport: 63282, dport: 443, dstAddr: v4("3.140.122.174")},
		{name: "netlink_sock_diag_response_7_0_3_sport19000_dport10156_v6", family: 10, sport: 19000, dport: 10156, dstAddr: v6("::1")},
	}
	found := make(map[string]bool, len(targets))

	off := PcapHeaderSizeCst
	for off < len(bs) {
		if off+PcapRecordHeaderSizeCst > len(bs) {
			break
		}

		var prh PcapRecordHeader
		if _, err := DeserializePcapRecordHeader(bs[off:off+PcapRecordHeaderSizeCst], &prh); err != nil {
			t.Fatalf("DeserializePcapRecordHeader at off=%d: %v", off, err)
		}

		recStart := off
		dataStart := off + PcapRecordHeaderSizeCst
		dataEnd := dataStart + int(prh.CapLen)
		if dataEnd > len(bs) {
			t.Fatalf("record at off=%d claims caplen=%d but file ends at %d", off, prh.CapLen, len(bs))
		}

		// Each record = NetlinkCookedHeader (16B) | NlMsgHdr | InetDiagMsg | RTAttrs...
		// Iterate to the next record now; continue handles every early exit below.
		off = dataEnd

		if int(prh.CapLen) < NetlinkCookedHeaderSizeCst+NlMsgHdrSizeCst+InetDiagMsgSizeCst {
			continue
		}

		nlStart := dataStart + NetlinkCookedHeaderSizeCst

		var nlh NlMsgHdr
		if _, err := DeserializeNlMsgHdr(bs[nlStart:nlStart+NlMsgHdrSizeCst], &nlh); err != nil {
			continue
		}
		if nlh.Type != NlMsgHdrTypeInetDiagCst {
			continue
		}

		idmStart := nlStart + NlMsgHdrSizeCst
		var idm InetDiagMsg
		var sid InetDiagSockID
		if _, err := DeserializeInetDiagMsg(bs[idmStart:idmStart+InetDiagMsgSizeCst], &idm, &sid); err != nil {
			continue
		}
		if idm.State != 1 { // TCP_ESTABLISHED
			continue
		}

		var match *target
		for i := range targets {
			tg := &targets[i]
			if found[tg.name] {
				continue
			}
			if idm.Family != tg.family || sid.SPort != tg.sport || sid.DPort != tg.dport {
				continue
			}
			if sid.DstIP != tg.dstAddr {
				continue
			}
			match = tg
			break
		}
		if match == nil {
			continue
		}

		// Find the INET_DIAG_INFO RTAttr by walking attributes after the InetDiagMsg.
		// nlh.Len covers NlMsgHdr + payload (InetDiagMsg + RTAttrs), aligned to 4.
		attrStart := idmStart + InetDiagMsgSizeCst
		nlEnd := min(nlStart+int(nlh.Len), dataEnd)

		var infoBlob []byte
		ao := attrStart
		for ao+RTAttrSizeCst <= nlEnd {
			var rta RTAttr
			if _, err := DeserializeRTAttr(bs[ao:ao+RTAttrSizeCst], &rta); err != nil {
				break
			}
			if rta.Len < RTAttrSizeCst || ao+int(rta.Len) > nlEnd {
				break
			}
			if rta.Type == TCPInfoEmumValueCst {
				infoBlob = append([]byte(nil), bs[ao:ao+int(rta.Len)]...)
				break
			}
			// Advance with 4-byte alignment.
			step := (int(rta.Len) + 3) &^ 3
			if step == 0 {
				break
			}
			ao += step
		}
		if infoBlob == nil {
			t.Logf("matched %s but found no INET_DIAG_INFO attribute (sport=%d dport=%d)", match.name, sid.SPort, sid.DPort)
			continue
		}

		// Sanity assertions: blob must be at least kernel 6.5+ TCPInfo (252
		// bytes incl. the 4-byte RTAttr header). Kernel 7.0.3 actually emits
		// 284 bytes — the extra 32 bytes are AccECN fields not yet covered by
		// the Go TCPInfo struct (DeserializeTCPInfo reads the first 248 and
		// ignores the rest). The state byte (offset 4 of the blob, i.e. byte 0
		// of the TCPInfo payload) is TCP_ESTABLISHED == 1.
		const minBlobSize = TCPInfo6_10_3_SizeCst + RTAttrSizeCst
		if len(infoBlob) < minBlobSize {
			t.Errorf("%s INET_DIAG_INFO size=%d want>=%d", match.name, len(infoBlob), minBlobSize)
		}
		if len(infoBlob) > minBlobSize {
			t.Logf("%s INET_DIAG_INFO size=%d (%d bytes beyond TCPInfo6_10_3 — likely AccECN)",
				match.name, len(infoBlob), len(infoBlob)-minBlobSize)
		}
		if infoBlob[RTAttrSizeCst] != 1 {
			t.Errorf("%s state byte=0x%02x want 0x01 (TCP_ESTABLISHED)", match.name, infoBlob[RTAttrSizeCst])
		}

		// Write the single-packet pcap: global pcap header + this record header + record bytes.
		singlePcap := make([]byte, 0, PcapHeaderSizeCst+PcapRecordHeaderSizeCst+int(prh.CapLen))
		singlePcap = append(singlePcap, bs[:PcapHeaderSizeCst]...)
		singlePcap = append(singlePcap, bs[recStart:dataEnd]...)
		pcapPath := filepath.Join(outDir, match.name+".pcap")
		if err := writeIfChanged(pcapPath, singlePcap); err != nil {
			t.Fatalf("write %s: %v", pcapPath, err)
		}

		infoPath := filepath.Join(outDir, match.name+"_info")
		if err := writeIfChanged(infoPath, infoBlob); err != nil {
			t.Fatalf("write %s: %v", infoPath, err)
		}

		found[match.name] = true
		t.Logf("extracted %s (family=%d sport=%d dport=%d caplen=%d)", match.name, idm.Family, sid.SPort, sid.DPort, prh.CapLen)

		if len(found) == len(targets) {
			break
		}
	}

	for _, tg := range targets {
		if !found[tg.name] {
			t.Errorf("target socket not found in pcap: %s (family=%d sport=%d dport=%d)", tg.name, tg.family, tg.sport, tg.dport)
		}
	}
}

// writeIfChanged writes data only when the existing file differs, so repeated
// runs leave mtime untouched and `git status` stays clean.
func writeIfChanged(path string, data []byte) error {
	cur, err := os.ReadFile(path)
	if err == nil && len(cur) == len(data) && bytesEqual(cur, data) {
		return nil
	}
	return os.WriteFile(path, data, 0o600) //nolint:gosec // G703: path is filepath.Join(testdataDir, …) inside the test, not user input
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var _ = binary.LittleEndian
