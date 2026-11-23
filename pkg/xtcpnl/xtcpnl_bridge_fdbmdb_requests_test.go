package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// fmNlmsg is a request as the builders lay it out: header, then body.
func fmNlmsg(typ, flags uint16, seq uint32, body ...[]byte) []byte {
	b := bytes.Join(body, nil)
	h := make([]byte, NlMsgHdrSizeCst, NlMsgHdrSizeCst+len(b))
	binary.LittleEndian.PutUint32(h[0:4], uint32(NlMsgHdrSizeCst+len(b)))
	binary.LittleEndian.PutUint16(h[4:6], typ)
	binary.LittleEndian.PutUint16(h[6:8], flags|unix.NLM_F_REQUEST)
	binary.LittleEndian.PutUint32(h[8:12], seq)
	return append(h, b...)
}

// fmAttr is one attribute, padded.
func fmAttr(typ uint16, val []byte) []byte {
	a := make([]byte, 4, 4+len(val)+3)
	binary.LittleEndian.PutUint16(a[0:2], uint16(4+len(val)))
	binary.LittleEndian.PutUint16(a[2:4], typ)
	a = append(a, val...)
	for len(a)%4 != 0 {
		a = append(a, 0)
	}
	return a
}

func fmU32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func fmU16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

// fmNdmsg is an AF_BRIDGE ndmsg.
func fmNdmsg(ifindex uint32, flags uint8) []byte {
	b := make([]byte, NdMsgSizeCst)
	b[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	b[NdMsgFlagsOffCst] = flags
	return b
}

// fmBpm is an AF_BRIDGE br_port_msg.
func fmBpm(ifindex uint32) []byte {
	b := make([]byte, BrPortMsgSizeCst)
	b[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	return b
}

// TestFdbMdbBuilders holds each builder to the bytes bridge sends
// (fdb.c fdb_show/fdb_get, mdb.c mdb_show/mdb_get).
//
// go test ./pkg/xtcpnl/ -run TestFdbMdbBuilders
func TestFdbMdbBuilders(t *testing.T) {
	mac := []byte{0x02, 0, 0, 0, 0, 0x01}
	v4 := []byte{239, 1, 1, 1}
	v6 := bytes.Repeat([]byte{0xff}, 16)
	entry := func(vid uint16, grp []byte, proto uint16) []byte {
		e := make([]byte, MdbEntrySizeCst)
		binary.LittleEndian.PutUint16(e[6:8], vid)
		copy(e[8:24], grp)
		binary.BigEndian.PutUint16(e[24:26], proto)
		return e
	}
	tests := []struct {
		description string
		build       func() ([]byte, error)
		want        []byte
		wantErr     error
	}{
		{
			description: "positive: fdb dump, no filter: an AF_BRIDGE ndmsg alone, 28 bytes",
			build:       func() ([]byte, error) { return BuildFdbDumpRequest(0, 0, 7) },
			want:        fmNlmsg(unix.RTM_GETNEIGH, unix.NLM_F_DUMP, 7, fmNdmsg(0, 0)),
		},
		{
			description: "positive: fdb dump dev d0: the ifindex in ndm_ifindex, no NDA_IFINDEX",
			build:       func() ([]byte, error) { return BuildFdbDumpRequest(4, 0, 7) },
			want:        fmNlmsg(unix.RTM_GETNEIGH, unix.NLM_F_DUMP, 7, fmNdmsg(4, 0)),
		},
		{
			description: "positive: fdb dump br br0: NDA_MASTER u32, ifindex 0",
			build:       func() ([]byte, error) { return BuildFdbDumpRequest(0, 3, 7) },
			want:        fmNlmsg(unix.RTM_GETNEIGH, unix.NLM_F_DUMP, 7, fmNdmsg(0, 0), fmAttr(unix.NDA_MASTER, fmU32(3))),
		},
		{
			description: "positive: mdb dump: a br_port_msg naming no device, no attributes",
			build:       func() ([]byte, error) { return BuildMdbDumpRequest(9) },
			want:        fmNlmsg(unix.RTM_GETMDB, unix.NLM_F_DUMP, 9, fmBpm(0)),
		},
		{
			description: "positive: fdb get with vlan, vni and br: LLADDR, VLAN, VNI, MASTER in fdb_get's order",
			build: func() ([]byte, error) {
				return BuildFdbGetRequest(FdbGet{Lladdr: mac, Vlan: 10, HasVlan: true, Vni: 5, HasVni: true, Master: 3}, 1)
			},
			want: fmNlmsg(unix.RTM_GETNEIGH, 0, 1, fmNdmsg(0, 0), fmAttr(unix.NDA_LLADDR, mac),
				fmAttr(unix.NDA_VLAN, fmU16(10)), fmAttr(unix.NDA_VNI, fmU32(5)), fmAttr(unix.NDA_MASTER, fmU32(3))),
		},
		{
			description: "positive: fdb get dev d1 self: ndm_ifindex and NTF_SELF, LLADDR alone",
			build: func() ([]byte, error) {
				return BuildFdbGetRequest(FdbGet{Ifindex: 4, Flags: unix.NTF_SELF, Lladdr: mac}, 1)
			},
			want: fmNlmsg(unix.RTM_GETNEIGH, 0, 1, fmNdmsg(4, unix.NTF_SELF), fmAttr(unix.NDA_LLADDR, mac)),
		},
		{
			description: "positive: mdb get with an IPv4 source: GET_ENTRY 28 bytes, then a NESTED ENTRY_ATTRS with SOURCE",
			build: func() ([]byte, error) {
				return BuildMdbGetRequest(MdbGet{Ifindex: 3, Vid: 10, Group: v4, Source: []byte{10, 0, 0, 1}}, 2)
			},
			want: fmNlmsg(unix.RTM_GETMDB, 0, 2, fmBpm(3), fmAttr(MdbaGetEntry, entry(10, v4, MdbProtoIP)),
				fmAttr(MdbaGetEntryAttrs|unix.NLA_F_NESTED, fmAttr(MdbeAttrSource, []byte{10, 0, 0, 1}))),
		},
		{
			description: "positive: mdb get without source or src_vni: no ENTRY_ATTRS nest",
			build:       func() ([]byte, error) { return BuildMdbGetRequest(MdbGet{Ifindex: 3, Group: v4}, 2) },
			want:        fmNlmsg(unix.RTM_GETMDB, 0, 2, fmBpm(3), fmAttr(MdbaGetEntry, entry(0, v4, MdbProtoIP))),
		},
		{
			description: "negative: fdb get address shorter than ETH_ALEN",
			build:       func() ([]byte, error) { return BuildFdbGetRequest(FdbGet{Lladdr: mac[:5]}, 1) },
			wantErr:     ErrFdbLladdrLen,
		},
		{
			description: "negative: mdb get group of 5 bytes fits no protocol",
			build:       func() ([]byte, error) { return BuildMdbGetRequest(MdbGet{Group: mac[:5]}, 1) },
			wantErr:     ErrMdbGroupLen,
		},
		{
			description: "negative: mdb get source of 6 bytes",
			build:       func() ([]byte, error) { return BuildMdbGetRequest(MdbGet{Group: v4, Source: mac}, 1) },
			wantErr:     ErrMdbSourceLen,
		},
		{
			description: "boundary: fdb get vlan unset (C -1): no NDA_VLAN",
			build:       func() ([]byte, error) { return BuildFdbGetRequest(FdbGet{Lladdr: mac, Master: 3}, 1) },
			want:        fmNlmsg(unix.RTM_GETNEIGH, 0, 1, fmNdmsg(0, 0), fmAttr(unix.NDA_LLADDR, mac), fmAttr(unix.NDA_MASTER, fmU32(3))),
		},
		{
			description: "boundary: fdb get vlan 0 is sent (the kernel refuses it)",
			build:       func() ([]byte, error) { return BuildFdbGetRequest(FdbGet{Lladdr: mac, HasVlan: true}, 1) },
			want:        fmNlmsg(unix.RTM_GETNEIGH, 0, 1, fmNdmsg(0, 0), fmAttr(unix.NDA_LLADDR, mac), fmAttr(unix.NDA_VLAN, fmU16(0))),
		},
		{
			description: "boundary: mdb get IPv6 group and source: proto 0x86dd, SOURCE 16 bytes",
			build: func() ([]byte, error) {
				return BuildMdbGetRequest(MdbGet{Ifindex: 3, Group: v6, Source: v6}, 2)
			},
			want: fmNlmsg(unix.RTM_GETMDB, 0, 2, fmBpm(3), fmAttr(MdbaGetEntry, entry(0, v6, MdbProtoIPv6)),
				fmAttr(MdbaGetEntryAttrs|unix.NLA_F_NESTED, fmAttr(MdbeAttrSource, v6))),
		},
		{
			description: "corner: mdb get L2 group: proto 0, the MAC in addr.u",
			build:       func() ([]byte, error) { return BuildMdbGetRequest(MdbGet{Ifindex: 3, Group: mac}, 2) },
			want:        fmNlmsg(unix.RTM_GETMDB, 0, 2, fmBpm(3), fmAttr(MdbaGetEntry, entry(0, mac, 0))),
		},
		{
			description: "corner: mdb get src_vni only: the nest holds SRC_VNI alone",
			build: func() ([]byte, error) {
				return BuildMdbGetRequest(MdbGet{Ifindex: 3, Group: v4, SrcVni: 0xffffffff, HasSrcVni: true}, 2)
			},
			want: fmNlmsg(unix.RTM_GETMDB, 0, 2, fmBpm(3), fmAttr(MdbaGetEntry, entry(0, v4, MdbProtoIP)),
				fmAttr(MdbaGetEntryAttrs|unix.NLA_F_NESTED, fmAttr(MdbeAttrSrcVni, fmU32(0xffffffff)))),
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.build()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err %v, want %v", err, tc.wantErr)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("\n got  %x\n want %x", got, tc.want)
			}
		})
	}
}

// TestFdbMdbReadbackRequests rebuilds every fdb and mdb dump request the
// 7_3_0 fdb*/mdb* readbacks (`bridge fdb|mdb show dev X`) sent, byte for
// byte with seq and pid zeroed. The fdb dump's ifindex is the capture's own
// (it must be non-zero: `dev` was given); the mdb dump names none. The
// corner: the fdb datagram is 284 bytes (sizeof(req)) while nlmsg_len is 28.
//
// go test ./pkg/xtcpnl/ -run TestFdbMdbReadbackRequests
func TestFdbMdbReadbackRequests(t *testing.T) {
	wantRows := map[string]int{"fdbbr": 99, "fdbvx": 100, "mdbbr": 90, "mdbvx": 87}
	zero := func(b []byte) []byte {
		b = bytes.Clone(b)
		clear(b[8:16])
		return b
	}
	for base, want := range wantRows {
		dirs, err := filepath.Glob(filepath.Join("testdata", "7_3_0", "ws", base, "*", "netlink_route_dump.pcap"))
		if err != nil {
			t.Fatal(err)
		}
		if len(dirs) != want {
			t.Fatalf("%s: %d readbacks, want %d", base, len(dirs), want)
		}
		fdb := base[:3] == "fdb"
		reqs := 0
		for _, p := range dirs {
			for _, dg := range pcapNlDatagrams(t, p) {
				for _, m := range dg {
					if m.h.Flags&unix.NLM_F_REQUEST == 0 {
						continue
					}
					full := fmNlmsgRaw(m.h, m.body)
					var built []byte
					switch {
					case fdb && m.h.Type == unix.RTM_GETNEIGH:
						if len(m.body) < NdMsgSizeCst {
							t.Fatalf("%s: short ndmsg", p)
						}
						idx := binary.LittleEndian.Uint32(m.body[4:8])
						if idx == 0 {
							t.Fatalf("%s: dev given, ndm_ifindex 0", p)
						}
						built, err = BuildFdbDumpRequest(idx, 0, 0)
					case !fdb && m.h.Type == unix.RTM_GETMDB:
						built, err = BuildMdbDumpRequest(0)
					default:
						continue
					}
					if err != nil {
						t.Fatal(err)
					}
					reqs++
					if !bytes.Equal(zero(full), built) {
						t.Fatalf("%s:\n captured %x\n built    %x", p, zero(full), built)
					}
				}
			}
		}
		// fdb: one dump per readback; mdb: two (entries, then routers).
		if wantReqs := map[bool]int{true: want, false: 2 * want}[fdb]; reqs != wantReqs {
			t.Errorf("%s: %d dump requests, want %d", base, reqs, wantReqs)
		}
	}
}

// fmNlmsgRaw is a captured message's wire bytes.
func fmNlmsgRaw(h NlMsgHdr, body []byte) []byte {
	b := make([]byte, NlMsgHdrSizeCst, NlMsgHdrSizeCst+len(body))
	binary.LittleEndian.PutUint32(b[0:4], h.Len)
	binary.LittleEndian.PutUint16(b[4:6], h.Type)
	binary.LittleEndian.PutUint16(b[6:8], h.Flags)
	binary.LittleEndian.PutUint32(b[8:12], h.Seq)
	binary.LittleEndian.PutUint32(b[12:16], h.Pid)
	return append(b, body...)
}

// TestFdbDumpDatagramPadding pins the corner the replay relies on: bridge's
// fdb dump goes out as sizeof(req), a 284-byte datagram whose first message
// is 28 bytes long, the rest zero; the mdb dump's datagram is its 24 bytes.
//
// go test ./pkg/xtcpnl/ -run TestFdbDumpDatagramPadding
func TestFdbDumpDatagramPadding(t *testing.T) {
	tests := []struct {
		description  string
		row          string
		typ          uint16
		wantDgram    int
		wantNlmsgLen uint32
	}{
		{"corner: fdb dump datagram is sizeof(req)", "fdbbr/bd-001", unix.RTM_GETNEIGH, 284, 28},
		{"positive: mdb dump datagram is nlmsg_len", "mdbbr/" + firstDir(t, "mdbbr"), unix.RTM_GETMDB, 24, 24},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			bs, err := os.ReadFile(filepath.Join("testdata", "7_3_0", "ws", tc.row, "netlink_route_dump.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			_, recs, err := ParseNetlinkPcap(bs)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range recs {
				_, dg, err := r.NetlinkPayload()
				if err != nil {
					t.Fatal(err)
				}
				if len(dg) < NlMsgHdrSizeCst || binary.LittleEndian.Uint16(dg[4:6]) != tc.typ ||
					binary.LittleEndian.Uint16(dg[6:8])&unix.NLM_F_REQUEST == 0 {
					continue
				}
				if l := binary.LittleEndian.Uint32(dg[0:4]); len(dg) != tc.wantDgram || l != tc.wantNlmsgLen {
					t.Fatalf("datagram %d nlmsg_len %d, want %d and %d", len(dg), l, tc.wantDgram, tc.wantNlmsgLen)
				}
				if !bytes.Equal(dg[tc.wantNlmsgLen:], make([]byte, len(dg)-int(tc.wantNlmsgLen))) {
					t.Fatal("trailer is not zero")
				}
				return
			}
			t.Fatal("no dump request")
		})
	}
}

func firstDir(t *testing.T, base string) string {
	t.Helper()
	es, err := os.ReadDir(filepath.Join("testdata", "7_3_0", "ws", base))
	if err != nil || len(es) == 0 {
		t.Fatalf("%s: %v", base, err)
	}
	return es[0].Name()
}
