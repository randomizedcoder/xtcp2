package xtcpnl

import (
	"net"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// isNotification reports whether a message is a notification. Most carry
// neither NLM_F_REQUEST nor NLM_F_MULTI, but nexthop_notify copies the
// request's nlmsg_flags into its notification (net/ipv4/nexthop.c:1091), so
// a NEWNEXTHOP event has NLM_F_REQUEST set; it is told from the request by
// its nlmsg_pid, the requester's port id, where `ip` sends 0.
func isNotification(h NlMsgHdr) bool {
	return h.Flags&unix.NLM_F_MULTI == 0 && (h.Flags&unix.NLM_F_REQUEST == 0 || h.Pid != 0)
}

// notifications counts a C1-T04 row's notifications by type.
func notifications(t *testing.T, slug string) map[uint16]int {
	t.Helper()
	out := map[uint16]int{}
	for _, m := range pcapNlMsgs(t, tdRtnlWrite_7_1_4+"/"+slug+"/ip.pcap") {
		if isNotification(m.h) {
			out[m.h.Type]++
		}
	}
	return out
}

// newPrefix decodes a row's one RTM_NEWPREFIX notification with ParsePrefix.
func newPrefix(t *testing.T, slug string) string {
	t.Helper()
	var body []byte
	for _, m := range pcapNlMsgs(t, tdRtnlWrite_7_1_4+"/"+slug+"/ip.pcap") {
		if m.h.Type == unix.RTM_NEWPREFIX && isNotification(m.h) {
			body = m.body
		}
	}
	pi, err := ParsePrefix(body)
	if err != nil {
		t.Fatal(err)
	}
	addr, cache := "none", "none"
	if pi.Address != nil {
		addr = net.IP(pi.Address).String()
	}
	if pi.HasCacheinfo {
		cache = u32s(pi.PreferredTime) + "/" + u32s(pi.ValidTime)
	}
	return strings.Join([]string{
		strconv.Itoa(int(pi.Family)), strconv.Itoa(int(pi.Type)), strconv.Itoa(int(pi.Len)),
		"0x" + strconv.FormatUint(uint64(pi.Flags), 16), addr, cache,
	}, " ")
}

// TestRtnlMonitorCorpus checks the C1-T04 event rows (testdata/7_1_4/
// link-write/rtnl/, scripts/rtnl-write-corpus.exp; docs/netlink/
// goip-parity-gaps.md): the router advertisement notifies NEWPREFIX only to
// a subscriber, and `ip monitor all` sees every type the trigger provokes.
func TestRtnlMonitorCorpus(t *testing.T) {
	// missing is the types of want that slug has no notification of.
	missing := func(t *testing.T, slug string, want ...uint16) string {
		got := notifications(t, slug)
		var out []string
		for _, w := range want {
			if got[w] == 0 {
				out = append(out, strconv.Itoa(int(w)))
			}
		}
		return strings.Join(out, " ")
	}
	allTypes := []uint16{
		unix.RTM_NEWLINK, unix.RTM_NEWADDR, unix.RTM_NEWROUTE, unix.RTM_NEWNETCONF,
		unix.RTM_NEWRULE, unix.RTM_NEWNEXTHOP, unix.RTM_NEWSTATS, unix.RTM_NEWNSID,
		unix.RTM_DELNSID, unix.RTM_NEWANYCAST, unix.RTM_NEWMULTICAST, unix.RTM_NEWPREFIX,
	}
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: the advertisement notifies NEWPREFIX once, for r0 alone",
			func(t *testing.T) string { return strconv.Itoa(notifications(t, "prefix_event")[unix.RTM_NEWPREFIX]) }, "1"},
		{"negative: with no subscriber there is no NEWPREFIX",
			func(t *testing.T) string {
				return strconv.Itoa(notifications(t, "prefix_no_monitor")[unix.RTM_NEWPREFIX])
			}, "0"},
		{"positive: yet the advertisement was received: r0 has the prefix route",
			func(t *testing.T) string {
				return strconv.FormatBool(strings.HasPrefix(readSidecar(t, "prefix_no_monitor/ip_getroute6"), "2001:db8:5::/64 proto kernel"))
			}, "true"},
		{"positive: ip monitor all sees every type the trigger provokes",
			func(t *testing.T) string { return missing(t, "monitor_all", allTypes...) }, ""},
		{"positive: and so does ip -t monitor all",
			func(t *testing.T) string { return missing(t, "monitor_all_timestamp", allTypes...) }, ""},
		{"positive: the NEWPREFIX is AF_INET6, type 3, /64, the wire's L flag, the prefix and its lifetimes",
			func(t *testing.T) string { return newPrefix(t, "prefix_event") }, "10 3 64 0x80 2001:db8:5:: 300/600"},
		{"corner: ip tests IF_PREFIX_ONLINK (0x01), so prints no onlink for the L flag (0x80)",
			func(t *testing.T) string {
				return prefixLines(readSidecar(t, "prefix_event/ip.out"))
			}, "prefix 2001:db8:5::/64dev r0 valid 600 preferred 300 "},
		{"corner: the NEWNEXTHOP notification carries the request's flags, NLM_F_REQUEST among them",
			func(t *testing.T) string {
				for _, m := range pcapNlMsgs(t, tdRtnlWrite_7_1_4+"/monitor_all/ip.pcap") {
					if m.h.Type == unix.RTM_NEWNEXTHOP && m.h.Pid != 0 {
						return "0x" + strconv.FormatUint(uint64(m.h.Flags), 16)
					}
				}
				return "none"
			}, "0x" + strconv.FormatUint(unix.NLM_F_REQUEST|unix.NLM_F_ACK|unix.NLM_F_EXCL|unix.NLM_F_CREATE, 16)},
		{"corner: monitor all labels the same line [PREFIX]",
			func(t *testing.T) string {
				return strconv.FormatBool(strings.Contains(readSidecar(t, "monitor_all/ip.out"),
					"\n[PREFIX]prefix 2001:db8:5::/64dev r0 valid 600 preferred 300 \n"))
			}, "true"},
		{"corner: -t puts one Timestamp line before every message",
			func(t *testing.T) string {
				out := readSidecar(t, "monitor_all_timestamp/ip.out")
				ts := regexp.MustCompile(`(?m)^Timestamp: .* [0-9]+ usec$`).FindAllString(out, -1)
				labels := regexp.MustCompile(`(?m)^\[[A-Z]+\]`).FindAllString(out, -1)
				return strconv.FormatBool(len(ts) > 0 && len(ts) == len(labels))
			}, "true"},
		{"corner: and with them taken out, the labels are monitor all's",
			func(t *testing.T) string {
				return strconv.FormatBool(slices.Equal(labelSet(readSidecar(t, "monitor_all_timestamp/ip.out")),
					labelSet(readSidecar(t, "monitor_all/ip.out"))))
			}, "true"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// readSidecar reads one of a row's files under the rtnl fixtures.
func readSidecar(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(tdRtnlWrite_7_1_4 + "/" + rel)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// labelSet is the distinct `[LABEL]` banners of an ip monitor output, sorted.
func labelSet(out string) []string {
	var set []string
	for _, l := range regexp.MustCompile(`(?m)^\[[A-Z]+\]`).FindAllString(out, -1) {
		if !slices.Contains(set, l) {
			set = append(set, l)
		}
	}
	slices.Sort(set)
	return set
}

// prefixLines is the `prefix` lines of an ip monitor output, joined by "|".
func prefixLines(out string) string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "prefix ") {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "|")
}
