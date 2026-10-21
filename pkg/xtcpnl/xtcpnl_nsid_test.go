package xtcpnl

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// nsidRender shows a decoded RTM_*NSID body as its family then each
// attribute present, or the error.
func nsidRender(ni NsidInfo, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"family=" + strconv.Itoa(int(ni.Family))}
	if ni.HasNSID {
		out = append(out, "nsid="+strconv.Itoa(int(ni.NSID)))
	}
	if ni.HasCurrentNSID {
		out = append(out, "current="+strconv.Itoa(int(ni.CurrentNSID)))
	}
	if ni.HasPID {
		out = append(out, "pid="+strconv.FormatUint(uint64(ni.PID), 10))
	}
	if ni.HasFD {
		out = append(out, "fd="+strconv.FormatUint(uint64(ni.FD), 10))
	}
	if ni.HasTargetNSID {
		out = append(out, "target="+strconv.Itoa(int(ni.TargetNSID)))
	}
	return strings.Join(out, " ")
}

// TestParseNsid decodes RTM_*NSID bodies. The positive rows are bodies
// transcribed from the corpus, each naming its pcap; the rest are
// hand-built wire edges the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseNsid$'
func TestParseNsid(t *testing.T) {
	hdr := []byte{unix.AF_UNSPEC, 0, 0, 0}
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: the kernel's NEWNSID reply to netns set, nsid 7 (link-write/rtnl/netns_set)",
			body:        mustHex(t, "000000000800010007000000"),
			expected:    "family=0 nsid=7",
		},
		{
			description: "positive: an unassigned peer, nsid -1 (link-state matrix raw.pcap)",
			body:        mustHex(t, "0000000008000100ffffffff"),
			expected:    "family=0 nsid=-1",
		},
		{
			description: "positive: a DELNSID event, nsid 9 (link-write/rtnl/monitor_all)",
			body:        mustHex(t, "000000000800010009000000"),
			expected:    "family=0 nsid=9",
		},
		{
			description: "positive: ip's NEWNSID request for netns set, fd 5 then nsid 7 (link-write/rtnl/netns_set)",
			body:        mustHex(t, "0000000008000300050000000800010007000000"),
			expected:    "family=0 nsid=7 fd=5",
		},
		{
			description: "positive: ip's GETNSID request by fd 6 (link-state matrix raw.pcap)",
			body:        mustHex(t, "000000000800030006000000"),
			expected:    "family=0 fd=6",
		},
		{
			description: "boundary: ip's GETNSID dump request is the header alone (dumps/reads/netlink_route_getnsid_list)",
			body:        mustHex(t, "00000000"),
			expected:    "family=0",
		},
		{
			description: "boundary: a body one byte short of the rtgenmsg is an error",
			body:        hdr[:RtGenMsgSizeCst-1],
			expected:    "error: " + ErrNsidMsgSmall.Error(),
		},
		{
			description: "corner: a reply to a target-nsid request carries the current nsid too",
			body:        cat(hdr, rtattr(unix.NETNSA_NSID, le32(2)), rtattr(unix.NETNSA_CURRENT_NSID, le32(0))),
			expected:    "family=0 nsid=2 current=0",
		},
		{
			description: "corner: a list-id request by pid with a target nsid",
			body:        cat(hdr, rtattr(unix.NETNSA_PID, le32(4242)), rtattr(unix.NETNSA_TARGET_NSID, le32(3))),
			expected:    "family=0 pid=4242 target=3",
		},
		{
			description: "corner: a family other than the AF_UNSPEC the kernel and ip both send is reported as sent",
			body:        cat([]byte{unix.AF_INET, 0, 0, 0}, rtattr(unix.NETNSA_NSID, le32(1))),
			expected:    "family=2 nsid=1",
		},
		{
			description: "corner: an nsid that is not four bytes is not decoded",
			body:        cat(hdr, rtattr(unix.NETNSA_NSID, le16(7))),
			expected:    "family=0",
		},
		{
			description: "corner: a repeated nsid keeps the first, as ip's parse_rtattr does",
			body:        cat(hdr, rtattr(unix.NETNSA_NSID, le32(4)), rtattr(unix.NETNSA_NSID, le32(5))),
			expected:    "family=0 nsid=4",
		},
		{
			description: "corner: an unknown attribute is skipped and the nsid after it decodes",
			body:        cat(hdr, rtattr(99, le32(0xdeadbeef)), rtattr(unix.NETNSA_NSID, le32(1))),
			expected:    "family=0 nsid=1",
		},
		{
			description: "negative: an attribute longer than the body is a parse error",
			body:        cat(hdr, rtattrOverrunning(unix.NETNSA_NSID, 20)),
			expected:    "error: " + ErrRTAttrSmall.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := nsidRender(ParseNsid(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseNsidCorpus decodes every RTM_*NSID message in the committed
// corpus, the kernel's and ip's.
//
// go test ./pkg/xtcpnl/ -run TestParseNsidCorpus
func TestParseNsidCorpus(t *testing.T) {
	// tally decodes every message of type typ, the kernel's or ip's
	// requests, and counts how many carry each attribute.
	tally := func(typ uint16, request bool) string {
		var n, errs, nsid, fd int
		for _, c := range corpusAll(t) {
			m := c.m
			if m.proto != unix.NETLINK_ROUTE || m.h.Type != typ || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
				continue
			}
			n++
			ni, err := ParseNsid(m.body)
			if err != nil {
				errs++
				continue
			}
			if ni.HasNSID {
				nsid++
			}
			if ni.HasFD {
				fd++
			}
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " +
			strconv.Itoa(nsid) + " with an nsid, " + strconv.Itoa(fd) + " with an fd"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{
			description: "positive: every NEWNSID the kernel sent decodes with an nsid",
			got:         tally(unix.RTM_NEWNSID, false),
			expected:    "206 decoded, 0 errors, 206 with an nsid, 0 with an fd",
		},
		{
			description: "positive: every DELNSID event decodes with an nsid",
			got:         tally(unix.RTM_DELNSID, false),
			expected:    "3 decoded, 0 errors, 3 with an nsid, 0 with an fd",
		},
		{
			description: "positive: every NEWNSID request ip sent names an fd and an nsid",
			got:         tally(unix.RTM_NEWNSID, true),
			expected:    "4 decoded, 0 errors, 4 with an nsid, 4 with an fd",
		},
		{
			description: "corner: ip's GETNSID asks by fd, except its two header-only dumps",
			got:         tally(unix.RTM_GETNSID, true),
			expected:    "202 decoded, 0 errors, 0 with an nsid, 200 with an fd",
		},
		{
			description: "negative: the kernel never sends a GETNSID",
			got:         tally(unix.RTM_GETNSID, false),
			expected:    "0 decoded, 0 errors, 0 with an nsid, 0 with an fd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
