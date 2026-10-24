package req

import (
	"bytes"
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

// td730 is the dump corpus `ip` 7.2.0 recorded on the 7.3.0 kernel.
const td730 = "../../../pkg/xtcpnl/testdata/7_3_0/dumps/"

// TestTierALLRequests730 checks each index-cache request goip builds under its
// default, iproute2 7.2.0, against the RTM_GETLINK `ip` 7.2.0 put on the wire
// in the 7_3_0 corpus, byte for byte with seq and pid zeroed. TestMain runs
// the rest of this package as 7.1.0 against 7_1_4; this is where the default
// mask, 0x109, meets a capture that recorded it rather than a constant.
//
// `ip stats show` has no row: it opens with an RTM_GETSTATS dump and then
// gets each link by index, which goip does too, with LinkShowByIndex.
//
// go test ./internal/goip/req/ -run TestTierALLRequests730
func TestTierALLRequests730(t *testing.T) {
	tests := []struct {
		name, category, description, expected string
		pcap                                  string
		// build makes goip's request; idx is the ifindex the captured
		// request asked for, which only the by-index get needs.
		build func(idx int32) ([]byte, error)
	}{
		{"neigh_link_dump", "positive", "ip neigh show's ll_init_map link dump", "byte-identical, mask 0x109",
			"netlink_route_getneigh.pcap", func(int32) ([]byte, error) { return NeighShowLinkDump(1) }},
		{"ntable_link_dump", "positive", "ip ntable show's ll_init_map link dump", "byte-identical, mask 0x109",
			"netlink_route_getneightbl.pcap", func(int32) ([]byte, error) { return NeighTblShowLinkDump(1) }},
		{"netconf_link_dump", "positive", "ip netconf show's ll_init_map link dump", "byte-identical, mask 0x109",
			"netlink_route_getnetconf.pcap", func(int32) ([]byte, error) { return NetconfShowLinkDump(1) }},
		{"link_get_by_name", "corner", "nexthop show dev goip0's ll_link_get by name", "byte-identical: mask 0x109 before IFLA_IFNAME",
			"netlink_route_getnexthop_dev.pcap", func(int32) ([]byte, error) { return LinkShowByName("goip0", 1) }},
		{"link_get_by_index", "boundary", "nexthop show's ll_index_to_name get for nh_oif", "byte-identical: mask 0x109, no name",
			"netlink_route_getnexthop.pcap", func(idx int32) ([]byte, error) { return LinkShowByIndex(idx, 1) }},
	}
	defer func(prev bool) { LLMapNameOnly = prev }(LLMapNameOnly)
	LLMapNameOnly = true
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			var captured []byte
			for _, m := range mustCapture(t, td730+tc.pcap).Msgs() {
				if m.IsRequest() && m.Hdr.Type == unix.RTM_GETLINK {
					captured = canonicalize(m)
					break
				}
			}
			if captured == nil {
				t.Fatalf("%s holds no RTM_GETLINK request", tc.pcap)
			}
			// ifi_index: after the 16-byte nlmsghdr, the ifinfomsg's family,
			// pad and type take 4 bytes.
			idx := int32(binary.LittleEndian.Uint32(captured[20:24]))
			msg, err := tc.build(idx)
			if err != nil {
				t.Fatal(err)
			}
			if got := zeroSeqPid(msg); !bytes.Equal(got, captured) {
				t.Errorf("request differs\n got %x\nwant %x", got, captured)
			}
		})
	}
}
