package xtcpnl

import (
	"encoding/binary"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// ILA_ATTR_PAD, which ParseILA skips; the checksum mode and identifier
// type the kernel defaults to; and the width of one of ip's locator words.
const (
	ilaAttrPad         = 6
	ilaCsumNoAction    = 2
	ilaIdentUseFormat  = 32
	ilaLocatorWordSize = 2
)

// ilaRows is every C2-T06 row, in the corpus's order.
var ilaRows = []string{
	"ila_show_empty", "ila_add", "ila_add_dev", "ila_add_opts", "ila_add_auto", "ila_add_dup",
	"ila_add_same_match_dev", "ila_add_no_loc", "ila_add_unknown_word", "ila_del",
	"ila_del_missing", "ila_del_dev", "ila_del_nodev_of_dev", "ila_show", "ila_show_args",
}

// ilaLocator renders a 64-bit locator as `ip` writes one: four 16-bit
// words in hex, each in network order, joined by colons.
func ilaLocator(b []byte) string {
	var words []string
	for i := 0; i+ilaLocatorWordSize <= len(b); i += ilaLocatorWordSize {
		words = append(words, fmt.Sprintf("%x", binary.BigEndian.Uint16(b[i:])))
	}
	return strings.Join(words, ":")
}

// ilaAttrs renders an ILA message's attributes in the order they came:
// loc=, match=, ifindex=, csum=, ident=, pad, and any of identifier=, dir=
// and hook= that appear.
func ilaAttrs(t *testing.T, attrs []NetlinkAttribute) string {
	t.Helper()
	var out []string
	for _, a := range attrs {
		switch a.ID() {
		case ILAAttrLocator:
			out = append(out, "loc="+ilaLocator(a.Data))
		case ILAAttrLocatorMatch:
			out = append(out, "match="+ilaLocator(a.Data))
		case ILAAttrIdentifier:
			out = append(out, "identifier="+ilaLocator(a.Data))
		case ILAAttrIfindex:
			out = append(out, "ifindex="+strconv.Itoa(int(int32(binary.LittleEndian.Uint32(a.Data)))))
		case ILAAttrCsumMode:
			out = append(out, "csum="+strconv.Itoa(int(a.Data[0])))
		case ILAAttrIdentType:
			out = append(out, "ident="+strconv.Itoa(int(a.Data[0])))
		case ILAAttrDir:
			out = append(out, "dir="+u32s(binary.LittleEndian.Uint32(a.Data)))
		case ILAAttrHookType:
			out = append(out, "hook="+strconv.Itoa(int(a.Data[0])))
		case ilaAttrPad:
			out = append(out, "pad")
		}
	}
	return strings.Join(out, " ")
}

// ilaMsgs describes each ILA message in a pcap, requests when request is
// true and the kernel's answers otherwise, as seg6Msgs does.
func ilaMsgs(t *testing.T, rel string, request bool) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+rel)
	fam := genlFamilyID(t, msgs, ILAGenlName)
	var out []string
	for _, m := range msgs {
		if fam == 0 || m.h.Type != fam || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
			continue
		}
		g, err := ParseGenericNetlink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		words := []string{map[uint8]string{
			ILACmdAdd: "add", ILACmdDel: "del", ILACmdGet: "get", ILACmdFlush: "flush",
		}[g.Command]}
		if request && m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			words = append(words, "dump")
		}
		if request && m.h.Flags&unix.NLM_F_ACK != 0 {
			words = append(words, "ack")
		}
		if a := ilaAttrs(t, g.Attributes); a != "" {
			words = append(words, a)
		}
		out = append(out, strings.Join(words, " "))
	}
	if !request {
		slices.Sort(out)
	}
	return strings.Join(out, "|")
}

// ilaErrno is the errno the kernel answered a row's first ILA request
// with, then its ip.rc.
func ilaErrno(t *testing.T, slug string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap")
	fam := genlFamilyID(t, msgs, ILAGenlName)
	for _, m := range msgs {
		if fam != 0 && m.h.Type == fam && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return strconv.Itoa(int(answerTo(t, msgs, m.h))) + " rc " + tcpmFile(t, slug+"/ip.rc")
		}
	}
	return "no request rc " + tcpmFile(t, slug+"/ip.rc")
}

// TestGenlIlaCorpus checks the C2-T06 rows (testdata/7_1_4/link-write/
// genl/, scripts/genl-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// `ip ila`'s mappings, each command's request and the kernel's answer.
func TestGenlIlaCorpus(t *testing.T) {
	req := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return ilaMsgs(t, rel, true) }
	}
	replies := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return ilaMsgs(t, rel, false) }
	}
	errno := func(slug string) func(t *testing.T) string {
		return func(t *testing.T) string { return ilaErrno(t, slug) }
	}
	file := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return tcpmFile(t, rel) }
	}
	// get renders one dumped mapping: loc, match and dev, with the
	// kernel's defaults for csum-mode (no-action) and ident-type
	// (use-format) unless given.
	get := func(loc, match string, ifindex, csum, ident int) string {
		return fmt.Sprintf("get loc=%s match=%s ifindex=%d csum=%d ident=%d", loc, match, ifindex, csum, ident)
	}
	m12 := get("2001:db8:3:4", "2001:db8:1:2", 0, ilaCsumNoAction, ilaIdentUseFormat)
	m12dev := get("2001:db8:3:4", "2001:db8:1:2", 3, ilaCsumNoAction, ilaIdentUseFormat)
	line := func(dev, csum string) string {
		return fmt.Sprintf("%-20s%-20s%-16s%suse-format", "2001:db8:1:2", "2001:db8:3:4", dev, csum)
	}
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: every row resolves ila by name inside its own pcap",
			func(t *testing.T) string {
				var missing []string
				for _, slug := range ilaRows {
					if genlFamilyID(t, pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap"), ILAGenlName) == 0 {
						missing = append(missing, slug)
					}
				}
				return strings.Join(missing, " ")
			}, ""},
		{"positive: show is a GET dump", req("ila_show_empty/ip.pcap"), "get dump"},
		{"boundary: with no mapping the dump is empty", replies("ila_show_empty/ip.pcap"), ""},
		{"positive: add is an ADD with the locator to match and the locator", req("ila_add/ip.pcap"), "add ack match=2001:db8:1:2 loc=2001:db8:3:4"},
		{"positive: answered with an ACK", errno("ila_add"), "0 rc 0"},
		{"positive: and the mapping reads back", replies("ila_add/netlink_generic_ila.pcap"), m12},
		{"positive: which ip prints", file("ila_add/ip_ila"), line("*", "no-action")},
		{"positive: dev is IFINDEX", req("ila_add_dev/ip.pcap"), "add ack match=2001:db8:1:2 loc=2001:db8:3:4 ifindex=3"},
		{"positive: and comes back", replies("ila_add_dev/netlink_generic_ila.pcap"), m12dev},
		{"positive: csum-mode and ident-type go in the request", req("ila_add_opts/ip.pcap"), "add ack match=2001:db8:1:2 loc=2001:db8:3:4 csum=0 ident=1"},
		{"positive: and come back", replies("ila_add_opts/netlink_generic_ila.pcap"), get("2001:db8:3:4", "2001:db8:1:2", 0, 0, 1)},
		{"positive: which ip prints by name", file("ila_add_opts/ip_ila"),
			fmt.Sprintf("%-20s%-20s%-16s%s", "2001:db8:1:2", "2001:db8:3:4", "*", "adj-transportluid")},
		{"positive: which ip prints with dev's name", file("ila_add_dev/ip_ila"), line("d0", "no-action")},
		{"corner: ip prints csum-mode and ident-type with nothing between them", file("ila_add_auto/ip_ila"), line("*", "neutral-map-auto")},
		{"positive: neutral-map-auto and use-format", replies("ila_add_auto/netlink_generic_ila.pcap"),
			get("2001:db8:3:4", "2001:db8:1:2", 0, 3, ilaIdentUseFormat)},
		{"negative: a mapping the kernel already has is EEXIST", errno("ila_add_dup"), strconv.Itoa(int(syscall.EEXIST)) + " rc 2"},
		{"corner: the same loc_match with a dev is a second mapping", errno("ila_add_same_match_dev"), "0 rc 0"},
		{"corner: so the dump holds both", replies("ila_add_same_match_dev/netlink_generic_ila.pcap"), m12 + "|" + m12dev},
		{"corner: the one with a dev first, the kernel ordering a loc_match's mappings by how much they name",
			file("ila_add_same_match_dev/ip_ila"), line("d0", "no-action") + "\n" + line("*", "no-action")},
		{"corner: an add with no locator still sends an ADD, with no attribute", req("ila_add_no_loc/ip.pcap"), "add ack"},
		{"corner: which the kernel takes", errno("ila_add_no_loc"), "0 rc 0"},
		{"corner: as a mapping of locator 0 to locator 0", replies("ila_add_no_loc/netlink_generic_ila.pcap"),
			get("0:0:0:0", "0:0:0:0", 0, ilaCsumNoAction, ilaIdentUseFormat)},
		{"corner: after ip says why", file("ila_add_no_loc/ip.err"), "ila: missing locator"},
		{"negative: an unknown word is refused by ip before any request, exit 255", errno("ila_add_unknown_word"), "no request rc 255"},
		{"positive: del is a DEL with the locator to match", req("ila_del/ip.pcap"), "del ack match=2001:db8:1:2"},
		{"positive: answered with an ACK", errno("ila_del"), "0 rc 0"},
		{"positive: and the mapping is gone", replies("ila_del/netlink_generic_ila.pcap"), ""},
		{"corner: a delete that matches nothing is ACKed too", errno("ila_del_missing"), "0 rc 0"},
		{"positive: a delete with a dev removes that mapping alone", replies("ila_del_dev/netlink_generic_ila.pcap"), m12},
		{"corner: a delete without a dev misses a mapping with one", errno("ila_del_nodev_of_dev"), "0 rc 0"},
		{"corner: which stays", replies("ila_del_nodev_of_dev/netlink_generic_ila.pcap"), m12dev},
		{"positive: a dump of two mappings", replies("ila_show/ip.pcap"),
			m12 + "|" + get("2001:db8:7:8", "2001:db8:5:6", 0, 1, ilaIdentUseFormat)},
		{"positive: which ip prints", func(t *testing.T) string {
			lines := strings.Split(tcpmFile(t, "ila_show/ip.out"), "\n")
			slices.Sort(lines)
			return strings.Join(lines, "\n")
		}, line("*", "no-action") + "\n" +
			fmt.Sprintf("%-20s%-20s%-16s%s", "2001:db8:5:6", "2001:db8:7:8", "*", "neutral-mapuse-format")},
		{"negative: show with an argument is refused by ip before the dump; do_list returns -1, so exit 1", errno("ila_show_args"), "no request rc 1"},
		{"negative: and says why", file("ila_show_args/ip.err"), `"ip ila show" does not take any arguments.`},
		{"corner: on x86-64 the kernel puts no ILA_ATTR_PAD before its 64-bit attributes, across every row",
			func(t *testing.T) string {
				var out []string
				for _, slug := range ilaRows {
					for _, rel := range []string{slug + "/ip.pcap", slug + "/netlink_generic_ila.pcap"} {
						if _, err := os.Stat(tdGenlWrite_7_1_4 + "/" + rel); err != nil {
							continue
						}
						if strings.Contains(ilaMsgs(t, rel, false), " pad") {
							out = append(out, rel)
						}
					}
				}
				return strings.Join(out, " ")
			}, ""},
		{"corner: ip never sends a GET that is not a dump, nor a FLUSH, across every row",
			func(t *testing.T) string {
				var out []string
				for _, slug := range ilaRows {
					for _, m := range strings.Split(ilaMsgs(t, slug+"/ip.pcap", true), "|") {
						if (strings.HasPrefix(m, "get") && !strings.HasPrefix(m, "get dump")) ||
							strings.HasPrefix(m, "flush") {
							out = append(out, slug+": "+m)
						}
					}
				}
				return strings.Join(out, " ")
			}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
