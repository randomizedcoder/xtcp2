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

// netconfPcap is `ip netconf show`'s capture: the ll_init_map link dump followed
// by the RTM_GETNETCONF AF_UNSPEC dump. Only the RTM_NEWNETCONF bodies are fed to
// the parser here.
const netconfPcap = "testdata/7_1_4/dumps/netlink_route_getnetconf.pcap"

// netconfBodies returns the netconfmsg bodies (nlmsghdr stripped) of every
// RTM_NEWNETCONF message in the pcap, in capture order.
func netconfBodies(t *testing.T, path string) [][]byte {
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
			if binary.LittleEndian.Uint16(body[off+4:off+6]) == uint16(unix.RTM_NEWNETCONF) {
				out = append(out, body[off+NlMsgHdrSizeCst:off+msgLen])
			}
			off += (msgLen + 3) &^ 3 // NLMSG_ALIGN
		}
	}
	return out
}

// TestParseNewNetconfCaptured decodes the real RTM_NEWNETCONF bodies from the
// committed pcap: fourteen records, the inet and inet6 views of five interfaces
// plus the `all`/`default` pseudo-interfaces. The `all` record carries the one
// rp_filter value that is not loose (off, i.e. 0), and the inet6 records carry no
// rp_filter attribute at all — the two shapes the ip_netconf golden shows.
//
// go test ./pkg/xtcpnl/ -run TestParseNewNetconfCaptured
func TestParseNewNetconfCaptured(t *testing.T) {
	bodies := netconfBodies(t, netconfPcap)
	if got := len(bodies); got != 14 {
		t.Fatalf("RTM_NEWNETCONF message count = %d, want 14", got)
	}

	// The first reply is the inet view of the first interface: every rendered
	// setting present (NETCONFA_INPUT is not emitted by this kernel) and rp_filter
	// loose, as the golden's first line shows.
	first, err := ParseNewNetconf(bodies[0])
	if err != nil {
		t.Fatalf("ParseNewNetconf(body 0): %v", err)
	}
	if first.Family != unix.AF_INET {
		t.Errorf("first family = %d, want %d (AF_INET)", first.Family, unix.AF_INET)
	}
	for _, c := range []struct {
		name string
		has  bool
	}{
		{"ifindex", first.HasIfindex},
		{"forwarding", first.HasForwarding},
		{"rp_filter", first.HasRpFilter},
		{"mc_forwarding", first.HasMcForwarding},
		{"proxy_neigh", first.HasProxyNeigh},
		{"ignore_routes_with_linkdown", first.HasIgnoreRoutesWithLinkdown},
	} {
		if !c.has {
			t.Errorf("first record missing %s", c.name)
		}
	}
	if first.HasInput {
		t.Errorf("first record has NETCONFA_INPUT; this kernel does not emit it")
	}
	if first.RpFilter != 2 {
		t.Errorf("first rp_filter = %d, want 2 (loose)", first.RpFilter)
	}

	// Find the inet `all` pseudo-record (ifindex -1): its rp_filter is off (0),
	// the one non-loose value in the capture.
	var all *NetconfInfo
	var anyV6 *NetconfInfo
	for i := range bodies {
		ni, perr := ParseNewNetconf(bodies[i])
		if perr != nil {
			t.Fatalf("ParseNewNetconf(body %d): %v", i, perr)
		}
		if ni.Family == unix.AF_INET && ni.HasIfindex && ni.Ifindex == NetconfIfindexAll {
			n := ni
			all = &n
		}
		if ni.Family == unix.AF_INET6 && anyV6 == nil {
			n := ni
			anyV6 = &n
		}
	}
	if all == nil {
		t.Fatal("no inet `all` (ifindex -1) record found")
	}
	if !all.HasRpFilter || all.RpFilter != 0 {
		t.Errorf("all rp_filter = %d/has=%v, want 0/true (off)", all.RpFilter, all.HasRpFilter)
	}
	if anyV6 == nil {
		t.Fatal("no inet6 record found")
	}
	// rp_filter is an IPv4-only attribute; inet6 records carry none.
	if anyV6.HasRpFilter {
		t.Errorf("inet6 record carries rp_filter; it is IPv4-only")
	}
	if !anyV6.HasForwarding || !anyV6.HasMcForwarding {
		t.Errorf("inet6 record missing forwarding/mc_forwarding")
	}
}

// TestParseNewNetconf exercises the decode contract on hand-built wire shapes: the
// signed ifindex, the width guard on short attributes, the parsed-but-ignored and
// unknown types, attribute order independence, and the error paths — edges the
// static capture does not carry. Every value is chosen to drive one branch.
//
// go test ./pkg/xtcpnl/ -run TestParseNewNetconf$
func TestParseNewNetconf(t *testing.T) {
	hdr := func(family uint8) []byte { return []byte{family, 0, 0, 0} }
	// s32 encodes a signed 32-bit attribute value (the ifindex sentinels are
	// negative); le32 takes a uint32, so the sign conversion happens here.
	s32 := func(v int32) []byte { return le32(uint32(v)) }
	tests := []struct {
		description string
		body        []byte
		wantErr     error
		check       func(t *testing.T, ni NetconfInfo)
	}{
		{
			description: "positive: inet all pseudo-dev (ifindex -1) with forwarding/rp_filter/mc/proxy/input",
			body: cat(hdr(unix.AF_INET),
				rtattr(NetconfaIfindex, s32(NetconfIfindexAll)),
				rtattr(NetconfaForwarding, le32(1)),
				rtattr(NetconfaRpFilter, le32(2)),
				rtattr(NetconfaMcForwarding, le32(0)),
				rtattr(NetconfaProxyNeigh, le32(0)),
				rtattr(NetconfaInput, le32(1))),
			check: func(t *testing.T, ni NetconfInfo) {
				if ni.Family != unix.AF_INET || !ni.HasIfindex || ni.Ifindex != -1 {
					t.Errorf("family/ifindex = %d/%d, want 2/-1", ni.Family, ni.Ifindex)
				}
				if ni.Forwarding != 1 || ni.RpFilter != 2 || !ni.HasInput || ni.Input != 1 {
					t.Errorf("fwd/rp/input = %d/%d/%d", ni.Forwarding, ni.RpFilter, ni.Input)
				}
			},
		},
		{
			description: "positive: inet default pseudo-dev carries ifindex -2",
			body:        cat(hdr(unix.AF_INET), rtattr(NetconfaIfindex, s32(NetconfIfindexDefault))),
			check: func(t *testing.T, ni NetconfInfo) {
				if !ni.HasIfindex || ni.Ifindex != -2 {
					t.Errorf("ifindex = %d/has=%v, want -2/true", ni.Ifindex, ni.HasIfindex)
				}
			},
		},
		{
			description: "positive: inet per-interface (ifindex 1) with all attrs set",
			body: cat(hdr(unix.AF_INET),
				rtattr(NetconfaIfindex, le32(1)),
				rtattr(NetconfaForwarding, le32(1)),
				rtattr(NetconfaRpFilter, le32(1)),
				rtattr(NetconfaMcForwarding, le32(1)),
				rtattr(NetconfaProxyNeigh, le32(1)),
				rtattr(NetconfaIgnoreRoutesWithLinkdown, le32(1)),
				rtattr(NetconfaInput, le32(1))),
			check: func(t *testing.T, ni NetconfInfo) {
				if !ni.HasIfindex || ni.Ifindex != 1 || !ni.HasIgnoreRoutesWithLinkdown {
					t.Errorf("ifindex/ignore = %d/%v", ni.Ifindex, ni.HasIgnoreRoutesWithLinkdown)
				}
			},
		},
		{
			description: "corner: inet6 message carries no rp_filter (the IPv4-only attribute)",
			body: cat(hdr(unix.AF_INET6),
				rtattr(NetconfaIfindex, s32(NetconfIfindexAll)),
				rtattr(NetconfaForwarding, le32(0)),
				rtattr(NetconfaMcForwarding, le32(0)),
				rtattr(NetconfaProxyNeigh, le32(0)),
				rtattr(NetconfaInput, le32(0))),
			check: func(t *testing.T, ni NetconfInfo) {
				if ni.Family != unix.AF_INET6 || ni.HasRpFilter {
					t.Errorf("family/HasRpFilter = %d/%v, want 10/false", ni.Family, ni.HasRpFilter)
				}
				if !ni.HasForwarding || !ni.HasInput {
					t.Error("inet6 record missing forwarding/input")
				}
			},
		},
		{
			description: "boundary: an attribute value of exactly four bytes is decoded",
			body:        cat(hdr(unix.AF_INET), rtattr(NetconfaForwarding, le32(1))),
			check: func(t *testing.T, ni NetconfInfo) {
				if !ni.HasForwarding || ni.Forwarding != 1 {
					t.Errorf("forwarding = %d/has=%v, want 1/true", ni.Forwarding, ni.HasForwarding)
				}
			},
		},
		{
			description: "boundary: an attribute value of three bytes (short) leaves the field absent",
			body:        cat(hdr(unix.AF_INET), rtattr(NetconfaForwarding, []byte{1, 0, 0})),
			check: func(t *testing.T, ni NetconfInfo) {
				if ni.HasForwarding {
					t.Error("short forwarding attribute was decoded; want skipped")
				}
			},
		},
		{
			description: "boundary: a bare four-byte header with no attributes yields family only",
			body:        hdr(unix.AF_INET),
			check: func(t *testing.T, ni NetconfInfo) {
				if ni.Family != unix.AF_INET || ni.HasIfindex || ni.HasForwarding {
					t.Errorf("want family-only, got %+v", ni)
				}
			},
		},
		{
			description: "corner: ifindex 0 on the positive path stores 0 with HasIfindex true",
			body:        cat(hdr(unix.AF_INET), rtattr(NetconfaIfindex, le32(0))),
			check: func(t *testing.T, ni NetconfInfo) {
				if !ni.HasIfindex || ni.Ifindex != 0 {
					t.Errorf("ifindex = %d/has=%v, want 0/true", ni.Ifindex, ni.HasIfindex)
				}
			},
		},
		{
			description: "corner: rp_filter 3 (past {off,strict,loose}) is stored raw",
			body:        cat(hdr(unix.AF_INET), rtattr(NetconfaRpFilter, le32(3))),
			check: func(t *testing.T, ni NetconfInfo) {
				if !ni.HasRpFilter || ni.RpFilter != 3 {
					t.Errorf("rp_filter = %d/has=%v, want 3/true", ni.RpFilter, ni.HasRpFilter)
				}
			},
		},
		{
			description: "corner: NETCONFA_BC_FORWARDING(8) is parsed-but-ignored, siblings unaffected",
			body: cat(hdr(unix.AF_INET),
				rtattr(8, le32(1)),
				rtattr(NetconfaForwarding, le32(1))),
			check: func(t *testing.T, ni NetconfInfo) {
				if !ni.HasForwarding || ni.Forwarding != 1 {
					t.Errorf("forwarding = %d/has=%v, want 1/true", ni.Forwarding, ni.HasForwarding)
				}
			},
		},
		{
			description: "corner: an unknown NETCONFA type (99) is ignored",
			body: cat(hdr(unix.AF_INET),
				rtattr(99, le32(0xdeadbeef)),
				rtattr(NetconfaProxyNeigh, le32(1))),
			check: func(t *testing.T, ni NetconfInfo) {
				if !ni.HasProxyNeigh || ni.ProxyNeigh != 1 {
					t.Errorf("proxy_neigh = %d/has=%v, want 1/true", ni.ProxyNeigh, ni.HasProxyNeigh)
				}
			},
		},
		{
			description: "corner: attributes in reverse order are all decoded",
			body: cat(hdr(unix.AF_INET),
				rtattr(NetconfaInput, le32(1)),
				rtattr(NetconfaForwarding, le32(1)),
				rtattr(NetconfaIfindex, le32(1))),
			check: func(t *testing.T, ni NetconfInfo) {
				if !ni.HasIfindex || !ni.HasForwarding || !ni.HasInput {
					t.Errorf("reverse-order decode missed a field: %+v", ni)
				}
			},
		},
		{
			description: "negative: a zero-length body is an error",
			body:        []byte{},
			wantErr:     ErrNetconfmsgSmall,
		},
		{
			description: "negative: a body shorter than the header is an error",
			body:        []byte{unix.AF_INET, 0, 0},
			wantErr:     ErrNetconfmsgSmall,
		},
		{
			description: "negative: an attribute whose declared length exceeds the body is a parse error",
			body:        cat(hdr(unix.AF_INET), rtattrOverrunning(NetconfaForwarding, 20)),
			wantErr:     ErrRTAttrSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ni, err := ParseNewNetconf(tc.body)
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
				tc.check(t, ni)
			}
		})
	}
}

// TestDeserializeNetconfmsg covers the fixed-header read: the family byte lands,
// the pad bytes are ignored, and a body short of the header is an error.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeNetconfmsg
func TestDeserializeNetconfmsg(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		wantErr     error
		wantFamily  uint8
		wantN       int
	}{
		{
			description: "positive: AF_INET family byte lands, n is the header size",
			data:        []byte{unix.AF_INET, 0, 0, 0},
			wantFamily:  unix.AF_INET,
			wantN:       NetconfMsgSizeCst,
		},
		{
			description: "positive: AF_INET6 family byte lands, trailing bytes ignored",
			data:        []byte{unix.AF_INET6, 0xaa, 0xbb, 0xcc, 0xde},
			wantFamily:  unix.AF_INET6,
			wantN:       NetconfMsgSizeCst,
		},
		{
			description: "negative: a zero-length body is an error",
			data:        []byte{},
			wantErr:     ErrNetconfmsgSmall,
		},
		{
			description: "boundary: a body one byte short of the header is an error",
			data:        make([]byte, NetconfMsgSizeCst-1),
			wantErr:     ErrNetconfmsgSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var h Netconfmsg
			n, err := DeserializeNetconfmsg(tc.data, &h)
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
				t.Errorf("family = %d, want %d", h.Family, tc.wantFamily)
			}
		})
	}
}

// TestBuildNetconfRequests pins the two request builders byte-for-byte. The dump
// and point-get hex are transcribed from the recorded requests in the committed
// pcaps (capture seq, goip0 = ifindex 3); the AF_INET dump and the ifindex -1
// point get are contract rows exercising the family byte and the signed attribute.
//
// go test ./pkg/xtcpnl/ -run TestBuildNetconfRequests
func TestBuildNetconfRequests(t *testing.T) {
	// Captured AF_UNSPEC dump: len 20, RTM_GETNETCONF, DUMP|REQUEST, seq, ncm_family 0.
	wantDump := mustHex(t, "1400000052000103d460c96a0000000000000000")
	gotDump := BuildDumpNetconfRequest(unix.AF_UNSPEC, 1791582420)
	if !bytes.Equal(gotDump, wantDump) {
		t.Errorf("dump request mismatch\ngot  %s\nwant %s", hex.EncodeToString(gotDump), hex.EncodeToString(wantDump))
	}
	// The dump flags are NLM_F_DUMP|NLM_F_REQUEST and carry no attributes.
	if flags := binary.LittleEndian.Uint16(gotDump[6:8]); flags&uint16(unix.NLM_F_DUMP) == 0 {
		t.Errorf("dump flags %#x lack NLM_F_DUMP", flags)
	}
	if len(gotDump) != NlMsgHdrSizeCst+NetconfMsgSizeCst {
		t.Errorf("dump request len = %d, want %d (no attributes)", len(gotDump), NlMsgHdrSizeCst+NetconfMsgSizeCst)
	}

	// Contract: an AF_INET dump differs only in the ncm_family byte.
	wantInet := mustHex(t, "14000000520001030100000000000000"+"02000000")
	if gotInet := BuildDumpNetconfRequest(unix.AF_INET, 1); !bytes.Equal(gotInet, wantInet) {
		t.Errorf("AF_INET dump mismatch\ngot  %s\nwant %s", hex.EncodeToString(gotInet), hex.EncodeToString(wantInet))
	}

	// Captured point get: len 28, RTM_GETNETCONF, REQUEST|ACK (no DUMP), ncm_family
	// AF_INET, one NETCONFA_IFINDEX=3 attribute.
	wantGet := mustHex(t, "1c00000052000500d560c96a00000000020000000800010003000000")
	gotGet, err := BuildGetNetconfByIndexRequest(unix.AF_INET, 3, 1791582421)
	if err != nil {
		t.Fatalf("BuildGetNetconfByIndexRequest: %v", err)
	}
	if !bytes.Equal(gotGet, wantGet) {
		t.Errorf("point get mismatch\ngot  %s\nwant %s", hex.EncodeToString(gotGet), hex.EncodeToString(wantGet))
	}
	flags := binary.LittleEndian.Uint16(gotGet[6:8])
	if flags&uint16(unix.NLM_F_REQUEST) == 0 || flags&uint16(unix.NLM_F_ACK) == 0 {
		t.Errorf("point get flags %#x lack REQUEST|ACK", flags)
	}
	if flags&uint16(unix.NLM_F_DUMP) != 0 {
		t.Errorf("point get flags %#x set NLM_F_DUMP; it is not a dump", flags)
	}

	// Contract boundary: a -1 ifindex travels as 0xffffffff in the attribute.
	wantGetNeg := mustHex(t, "1c0000005200050001000000000000000200000008000100ffffffff")
	gotGetNeg, err := BuildGetNetconfByIndexRequest(unix.AF_INET, -1, 1)
	if err != nil {
		t.Fatalf("BuildGetNetconfByIndexRequest(-1): %v", err)
	}
	if !bytes.Equal(gotGetNeg, wantGetNeg) {
		t.Errorf("point get (-1) mismatch\ngot  %s\nwant %s", hex.EncodeToString(gotGetNeg), hex.EncodeToString(wantGetNeg))
	}
}
