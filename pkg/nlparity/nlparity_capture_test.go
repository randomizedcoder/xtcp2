package nlparity

import (
	"errors"
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestParseCapture covers the pcap-file layer: the link-type assertion and the
// family census. The census rows matter more than they look — nlmon captures
// the whole host, so "how many records did you throw away, and why" is the
// difference between a comparator and a random number generator.
//
// go test ./pkg/nlparity/ -run TestParseCapture
func TestParseCapture(t *testing.T) {
	tests := []struct {
		description string

		filename string // real capture; mutually exclusive with input
		input    []byte // constructed bytes

		family uint16

		// sidecarNote records the measurement an expectation came from, so a
		// reader can re-derive the number instead of trusting it.
		sidecarNote string

		wantErr error
		// Asserted only when non-negative, so a row can check one count and
		// stay silent about the others.
		wantDatagrams  int
		wantOtherFam   int
		wantShortRec   int
		wantBadDgram   int
		wantMinRequest int
	}{
		{
			description:   "positive: the `ip link show` capture is clean — one request, four reply datagrams",
			filename:      tdBulkGetLink,
			family:        uint16(unix.NETLINK_ROUTE),
			sidecarNote:   "13 messages across 2 port ids {0:1, 106975:12}",
			wantDatagrams: 5,
			wantOtherFam:  0,
			wantShortRec:  0,
			wantBadDgram:  0,
			// The one RTM_GETLINK dump request.
			wantMinRequest: 1,
		},
		{
			description: "positive: the `ip addr show` capture is heavily polluted and still parses",
			filename:    tdBulkGetAddr,
			family:      uint16(unix.NETLINK_ROUTE),
			sidecarNote: "251 messages, 6 distinct port ids, 17 sequence numbers — " +
				"two ip runs plus an unrelated process",
			wantDatagrams: -1,
			wantOtherFam:  -1,
			wantShortRec:  -1,
			wantBadDgram:  0,
			// Two `ip -4/-6 addr show` runs, each sending a link dump then an
			// addr dump, plus whatever the stray process sent.
			wantMinRequest: 4,
		},
		{
			description:   "corner: a NETLINK_SOCK_DIAG capture read as NETLINK_ROUTE yields nothing, counted",
			filename:      tdSockDiagReply,
			family:        uint16(unix.NETLINK_ROUTE),
			wantDatagrams: 0,
			wantOtherFam:  1,
			wantShortRec:  0,
			wantBadDgram:  0,
		},
		{
			description:   "positive: the same capture read as NETLINK_SOCK_DIAG yields its datagram",
			filename:      tdSockDiagReply,
			family:        uint16(unix.NETLINK_SOCK_DIAG),
			wantDatagrams: 1,
			wantOtherFam:  0,
		},
		{
			description: "corner: a pcap whose link type is not DLT_NETLINK is rejected, not reinterpreted",
			input:       pcapFile(1 /* LINKTYPE_ETHERNET */, nil),
			family:      uint16(unix.NETLINK_ROUTE),
			wantErr:     xtcpnl.ErrPcapNotNetlink,
		},
		{
			description: "negative: a file that is not a pcap at all",
			input:       []byte("this is not a pcap file, not even close, no."),
			family:      uint16(unix.NETLINK_ROUTE),
			wantErr:     xtcpnl.ErrPcapBadMagic,
		},
		{
			description:   "boundary: a DLT_NETLINK pcap with zero records",
			input:         pcapFile(xtcpnl.PcapLinkTypeNetlinkCst, nil),
			family:        uint16(unix.NETLINK_ROUTE),
			wantDatagrams: 0,
			wantOtherFam:  0,
			wantShortRec:  0,
			wantBadDgram:  0,
		},
		{
			description: "boundary: a record too short for the 16-byte SLL cooked header is counted, not fatal",
			input: pcapFile(xtcpnl.PcapLinkTypeNetlinkCst, [][]byte{
				zeros(8),
			}),
			family:        uint16(unix.NETLINK_ROUTE),
			wantDatagrams: 0,
			wantShortRec:  1,
		},
		{
			description: "negative: a NETLINK_ROUTE record whose netlink datagram is malformed is counted as bad",
			input: pcapFile(xtcpnl.PcapLinkTypeNetlinkCst, [][]byte{
				sllRecord(uint16(unix.NETLINK_ROUTE),
					nlmsg(15, uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), 1, 0, zeros(16))),
			}),
			family:        uint16(unix.NETLINK_ROUTE),
			wantDatagrams: 0,
			wantBadDgram:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			checkRowProvenance(t, tc.description, tc.filename, tc.input)
			if tc.sidecarNote != "" {
				t.Logf("expectation derived from: %s", tc.sidecarNote)
			}

			data := tc.input
			if tc.filename != "" {
				var err error
				data, err = os.ReadFile(tc.filename)
				if err != nil {
					t.Fatalf("read %s: %v", tc.filename, err)
				}
			}

			got, err := ParseCapture(data, tc.family)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			checkCount(t, "datagrams", len(got.Datagrams), tc.wantDatagrams)
			checkCount(t, "SkippedOtherFamily", got.SkippedOtherFamily, tc.wantOtherFam)
			checkCount(t, "SkippedShortRecord", got.SkippedShortRecord, tc.wantShortRec)
			checkCount(t, "SkippedBadDatagram", got.SkippedBadDatagram, tc.wantBadDgram)

			if n := len(got.Requests()); n < tc.wantMinRequest {
				t.Errorf("requests = %d, want at least %d", n, tc.wantMinRequest)
			}
		})
	}
}

// checkCount asserts an exact count unless want is negative, which means "this
// row is not about that number".
func checkCount(t *testing.T, name string, got, want int) {
	t.Helper()
	if want < 0 {
		return
	}
	if got != want {
		t.Errorf("%s = %d, want %d", name, got, want)
	}
}
