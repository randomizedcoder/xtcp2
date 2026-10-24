package xtcpnl

import (
	"bytes"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// TestNsidRequests lays out each RTM_*NSID request as ip sends it. The
// captured rows are dumps/reads/netlink_route_getnsid_list.pcap's and
// link-write/rtnl/netns_set/ip.pcap's requests; the rest are hand-built.
//
// go test ./pkg/xtcpnl/ -run TestNsidRequests
func TestNsidRequests(t *testing.T) {
	hdr := []byte{unix.AF_UNSPEC, 0, 0, 0}
	u32 := func(atype uint16, v int32) []byte { return rtattr(atype, le32(uint32(v))) }
	tests := []struct {
		description string
		build       func() ([]byte, error)
		expected    []byte
	}{
		{"positive: a GETNSID by fd 6 (captured)",
			func() ([]byte, error) { return BuildGetNsidByFDRequest(6, testSeq) },
			nlmsg(uint16(unix.RTM_GETNSID), unix.NLM_F_REQUEST, testSeq, concat(hdr, u32(unix.NETNSA_FD, 6)))},
		{"positive: the list-id dump is the header alone (captured)",
			func() ([]byte, error) { return BuildDumpNsidRequest(0, false, testSeq) },
			nlmsg(uint16(unix.RTM_GETNSID), unix.NLM_F_REQUEST|unix.NLM_F_DUMP, testSeq, hdr)},
		{"positive: netns set's NEWNSID, fd 5 then nsid 7, acked (captured)",
			func() ([]byte, error) { return BuildSetNsidRequest(5, 7, testSeq) },
			nlmsg(uint16(unix.RTM_NEWNSID), unix.NLM_F_REQUEST|unix.NLM_F_ACK, testSeq,
				concat(hdr, u32(unix.NETNSA_FD, 5), u32(unix.NETNSA_NSID, 7)))},
		{"boundary: set auto sends nsid -1",
			func() ([]byte, error) { return BuildSetNsidRequest(5, -1, testSeq) },
			nlmsg(uint16(unix.RTM_NEWNSID), unix.NLM_F_REQUEST|unix.NLM_F_ACK, testSeq,
				concat(hdr, u32(unix.NETNSA_FD, 5), u32(unix.NETNSA_NSID, -1)))},
		{"positive: list-id nsid 2 is a get by NETNSA_NSID",
			func() ([]byte, error) { return BuildGetNsidRequest(2, 0, false, testSeq) },
			nlmsg(uint16(unix.RTM_GETNSID), unix.NLM_F_REQUEST, testSeq, concat(hdr, u32(unix.NETNSA_NSID, 2)))},
		{"corner: a target nsid follows the nsid",
			func() ([]byte, error) { return BuildGetNsidRequest(1, 2, true, testSeq) },
			nlmsg(uint16(unix.RTM_GETNSID), unix.NLM_F_REQUEST, testSeq,
				concat(hdr, u32(unix.NETNSA_NSID, 1), u32(unix.NETNSA_TARGET_NSID, 2)))},
		{"corner: a dump with a target nsid carries it",
			func() ([]byte, error) { return BuildDumpNsidRequest(0, true, testSeq) },
			nlmsg(uint16(unix.RTM_GETNSID), unix.NLM_F_REQUEST|unix.NLM_F_DUMP, testSeq,
				concat(hdr, u32(unix.NETNSA_TARGET_NSID, 0)))},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.build()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("got  % x\nwant % x", got, tc.expected)
			}
		})
	}
}

// TestNsidRequestsCorpus rebuilds every RTM_GETNSID and RTM_NEWNSID request
// ip sent in the committed corpus from its decoded attributes: the builders
// must reproduce each body and flag set exactly.
//
// go test ./pkg/xtcpnl/ -run TestNsidRequestsCorpus
func TestNsidRequestsCorpus(t *testing.T) {
	rebuild := func(typ uint16) string {
		var n, same int
		for _, c := range corpusAll(t) {
			m := c.m
			if m.proto != unix.NETLINK_ROUTE || m.h.Type != typ || m.h.Flags&unix.NLM_F_REQUEST == 0 {
				continue
			}
			n++
			ni, err := ParseNsid(m.body)
			if err != nil {
				continue
			}
			var b []byte
			switch {
			case typ == unix.RTM_NEWNSID:
				b, err = BuildSetNsidRequest(ni.FD, ni.NSID, m.h.Seq)
			case m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP:
				b, err = BuildDumpNsidRequest(ni.TargetNSID, ni.HasTargetNSID, m.h.Seq)
			case ni.HasFD:
				b, err = BuildGetNsidByFDRequest(ni.FD, m.h.Seq)
			default:
				b, err = BuildGetNsidRequest(ni.NSID, ni.TargetNSID, ni.HasTargetNSID, m.h.Seq)
			}
			if err == nil && bytes.Equal(b[NlMsgHdrSizeCst:], m.body) &&
				binaryFlags(b) == m.h.Flags {
				same++
			}
		}
		return strconv.Itoa(same) + " of " + strconv.Itoa(n)
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{"positive: every GETNSID ip sent, by fd or the dump, rebuilds", rebuild(unix.RTM_GETNSID), "202 of 202"},
		{"positive: every NEWNSID ip sent rebuilds", rebuild(unix.RTM_NEWNSID), "4 of 4"},
		{"negative: no DELNSID request exists to rebuild", rebuild(unix.RTM_DELNSID), "0 of 0"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %s, want %s", tc.got, tc.expected)
			}
		})
	}
}

// binaryFlags is a built request's nlmsg_flags.
func binaryFlags(b []byte) uint16 { return uint16(b[6]) | uint16(b[7])<<8 }
