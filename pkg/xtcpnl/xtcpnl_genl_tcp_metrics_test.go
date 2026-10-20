package xtcpnl

import (
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// The tcp_metrics commands and attributes (linux/tcp_metrics.h), which
// x/sys/unix does not define.
const (
	tcpmCmdGet       = 1
	tcpmCmdDel       = 2
	tcpmAttrAddrIPv4 = 1
	tcpmAttrAddrIPv6 = 2
	tcpmAttrSaddrV4  = 11
	tcpmAttrSaddrV6  = 12
)

// tcpmRows is every C2-T01 row, in the corpus's order.
var tcpmRows = []string{
	"tcpm_show", "tcpm_show_empty", "tcpm_show_v6", "tcpm_get", "tcpm_get_src", "tcpm_get_v6",
	"tcpm_get_missing", "tcpm_delete", "tcpm_delete_src", "tcpm_delete_missing",
	"tcpm_flush_all", "tcpm_flush_prefix", "tcpm_flush_v6", "tcpm_flush_empty",
}

// genlFamilyID returns the id the controller assigned the family called
// name, read from a NEWFAMILY in the same pcap, or 0 when there is none.
// The id is a boot-time value, so a test never names it.
func genlFamilyID(t *testing.T, msgs []capturedNlMsg, name string) uint16 {
	t.Helper()
	for _, m := range msgs {
		if m.h.Type != GenlControllerID || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		f, err := ParseGenericNetlinkFamily(m.body)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == name {
			return f.ID
		}
	}
	return 0
}

// tcpmPcap returns a row's messages and the tcp_metrics family's id.
func tcpmPcap(t *testing.T, rel string) ([]capturedNlMsg, uint16) {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+rel)
	return msgs, genlFamilyID(t, msgs, "tcp_metrics")
}

// tcpmAddrs renders a tcp_metrics message's destination and source
// attributes as "addr=A saddr=S", leaving out whichever is absent.
func tcpmAddrs(t *testing.T, attrs []NetlinkAttribute) string {
	t.Helper()
	var out []string
	for _, a := range attrs {
		switch a.ID() {
		case tcpmAttrAddrIPv4, tcpmAttrAddrIPv6:
			out = append(out, "addr="+net.IP(a.Data).String())
		case tcpmAttrSaddrV4, tcpmAttrSaddrV6:
			out = append(out, "saddr="+net.IP(a.Data).String())
		}
	}
	slices.Sort(out)
	return strings.Join(out, " ")
}

// tcpmRequests describes each tcp_metrics request `ip` sent, in order:
// "get" or "del", then "dump" and "ack" for those flags, then its addresses.
func tcpmRequests(t *testing.T, rel string) []string {
	t.Helper()
	msgs, fam := tcpmPcap(t, rel)
	var out []string
	for _, m := range msgs {
		if fam == 0 || m.h.Type != fam || m.h.Flags&unix.NLM_F_REQUEST == 0 {
			continue
		}
		g, err := ParseGenericNetlink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		words := []string{map[uint8]string{tcpmCmdGet: "get", tcpmCmdDel: "del"}[g.Command]}
		if m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			words = append(words, "dump")
		}
		if m.h.Flags&unix.NLM_F_ACK != 0 {
			words = append(words, "ack")
		}
		if a := tcpmAddrs(t, g.Attributes); a != "" {
			words = append(words, a)
		}
		out = append(out, strings.Join(words, " "))
	}
	return out
}

// tcpmEntries is the entries the kernel answered with, each "addr=D
// saddr=S", sorted and joined by "|".
func tcpmEntries(t *testing.T, rel string) string {
	t.Helper()
	msgs, fam := tcpmPcap(t, rel)
	var out []string
	for _, m := range msgs {
		if fam == 0 || m.h.Type != fam || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		g, err := ParseGenericNetlink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tcpmAddrs(t, g.Attributes))
	}
	slices.Sort(out)
	return strings.Join(out, "|")
}

// tcpmErrno is the errno the kernel answered a row's one tcp_metrics
// request with, then its ip.rc.
func tcpmErrno(t *testing.T, slug string) string {
	t.Helper()
	msgs, fam := tcpmPcap(t, slug+"/ip.pcap")
	for _, m := range msgs {
		if m.h.Type == fam && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return strconv.Itoa(int(answerTo(t, msgs, m.h))) + " rc " + tcpmFile(t, slug+"/ip.rc")
		}
	}
	return "no request"
}

// tcpmFile is one of the genl fixtures' files, trimmed.
func tcpmFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(tdGenlWrite_7_1_4 + "/" + rel)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// TestGenlTcpMetricsCorpus checks the C2-T01 rows (testdata/7_1_4/
// link-write/genl/, scripts/genl-write-corpus.exp; docs/netlink/
// goip-parity-gaps.md): what `ip tcp_metrics` asks the kernel for each
// verb, and what the kernel answers, over two connections per family.
func TestGenlTcpMetricsCorpus(t *testing.T) {
	const (
		v4   = "addr=192.0.2.1 saddr=192.0.2.2|addr=192.0.2.2 saddr=192.0.2.1"
		v6   = "addr=2001:db8::1 saddr=2001:db8::2|addr=2001:db8::2 saddr=2001:db8::1"
		both = v4 + "|" + v6
	)
	esrch := strconv.Itoa(int(syscall.ESRCH)) + " rc 2"
	reqs := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return strings.Join(tcpmRequests(t, rel), "|") }
	}
	// sortedReqs is reqs for a flush, whose DELs follow the dump's order.
	sortedReqs := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string {
			r := tcpmRequests(t, rel)
			slices.Sort(r)
			return strings.Join(r, "|")
		}
	}
	entries := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return tcpmEntries(t, rel) }
	}
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: every row resolves tcp_metrics by name inside its own pcap",
			func(t *testing.T) string {
				var missing []string
				for _, slug := range tcpmRows {
					if _, fam := tcpmPcap(t, slug+"/ip.pcap"); fam == 0 {
						missing = append(missing, slug)
					}
				}
				return strings.Join(missing, " ")
			}, ""},
		{"negative: a family the pcap never looked up resolves to nothing",
			func(t *testing.T) string {
				msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/tcpm_show/ip.pcap")
				return strconv.Itoa(int(genlFamilyID(t, msgs, "SEG6")))
			}, "0"},
		{"corner: the id is assigned at boot, one per family, above the controller's",
			func(t *testing.T) string {
				var ids []uint16
				for _, slug := range tcpmRows {
					if _, fam := tcpmPcap(t, slug+"/ip.pcap"); !slices.Contains(ids, fam) {
						ids = append(ids, fam)
					}
				}
				return strconv.Itoa(len(ids)) + " " + strconv.FormatBool(ids[0] > GenlControllerID)
			}, "1 true"},
		{"corner: every record is NETLINK_GENERIC, the family lookup included",
			func(t *testing.T) string {
				var other []string
				for _, slug := range tcpmRows {
					bs, err := os.ReadFile(tdGenlWrite_7_1_4 + "/" + slug + "/ip.pcap")
					if err != nil {
						t.Fatal(err)
					}
					_, recs, err := ParseNetlinkPcap(bs)
					if err != nil {
						t.Fatal(err)
					}
					for _, r := range recs {
						if proto, _, _ := r.NetlinkPayload(); proto != unix.NETLINK_GENERIC {
							other = append(other, slug)
							break
						}
					}
				}
				return strings.Join(other, " ")
			}, ""},
		{"positive: show is one dump with no address", reqs("tcpm_show/ip.pcap"), "get dump"},
		{"positive: and finds both ends of both connections", entries("tcpm_show/ip.pcap"), both},
		{"boundary: with no connection the dump is empty", entries("tcpm_show_empty/ip.pcap"), ""},
		{"corner: -6 sends the same dump, so gets every entry back", reqs("tcpm_show_v6/ip.pcap"), "get dump"},
		{"corner: and ip drops the IPv4 ones itself",
			func(t *testing.T) string {
				return strconv.FormatBool(!strings.Contains(tcpmFile(t, "tcpm_show_v6/ip.out"), "192.0.2.") &&
					tcpmEntries(t, "tcpm_show_v6/ip.pcap") == both)
			}, "true"},
		{"positive: show address is a GET with the address", reqs("tcpm_get/ip.pcap"), "get addr=192.0.2.1"},
		{"positive: answered with the one entry", entries("tcpm_get/ip.pcap"), "addr=192.0.2.1 saddr=192.0.2.2"},
		{"positive: src adds the source", reqs("tcpm_get_src/ip.pcap"), "get addr=192.0.2.1 saddr=192.0.2.2"},
		{"positive: an IPv6 address is ADDR_IPV6", reqs("tcpm_get_v6/ip.pcap"), "get addr=2001:db8::1"},
		{"negative: a GET of an address with no entry is ESRCH, and ip exits 2",
			func(t *testing.T) string { return tcpmErrno(t, "tcpm_get_missing") }, esrch},
		{"positive: delete is a DEL that asks for an ACK", reqs("tcpm_delete/ip.pcap"), "del ack addr=192.0.2.1"},
		{"positive: and gets one",
			func(t *testing.T) string { return tcpmErrno(t, "tcpm_delete") }, "0 rc 0"},
		{"positive: leaving the other three entries",
			entries("tcpm_delete/netlink_generic_tcpm.pcap"), "addr=192.0.2.2 saddr=192.0.2.1|" + v6},
		{"positive: delete with src carries both addresses",
			reqs("tcpm_delete_src/ip.pcap"), "del ack addr=192.0.2.1 saddr=192.0.2.2"},
		{"negative: a DEL of an address with no entry is ESRCH, and ip exits 2",
			func(t *testing.T) string { return tcpmErrno(t, "tcpm_delete_missing") }, esrch},
		{"positive: flush all is one DEL with no address", reqs("tcpm_flush_all/ip.pcap"), "del ack"},
		{"positive: which empties the table", entries("tcpm_flush_all/netlink_generic_tcpm.pcap"), ""},
		{"positive: a prefix flush dumps, deletes each match without an ACK, and dumps again",
			sortedReqs("tcpm_flush_prefix/ip.pcap"),
			"del addr=192.0.2.1 saddr=192.0.2.2|del addr=192.0.2.2 saddr=192.0.2.1|get dump|get dump"},
		{"positive: leaving the IPv6 entries", entries("tcpm_flush_prefix/netlink_generic_tcpm.pcap"), v6},
		{"corner: -6 flush is the same walk over the IPv6 entries",
			sortedReqs("tcpm_flush_v6/ip.pcap"),
			"del addr=2001:db8::1 saddr=2001:db8::2|del addr=2001:db8::2 saddr=2001:db8::1|get dump|get dump"},
		{"positive: leaving the IPv4 entries", entries("tcpm_flush_v6/netlink_generic_tcpm.pcap"), v4},
		{"boundary: a flush that matches nothing is its dump alone", reqs("tcpm_flush_empty/ip.pcap"), "get dump"},
		{"boundary: and says so, on stderr",
			func(t *testing.T) string { return tcpmFile(t, "tcpm_flush_empty/ip.err") }, "Nothing to flush."},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
