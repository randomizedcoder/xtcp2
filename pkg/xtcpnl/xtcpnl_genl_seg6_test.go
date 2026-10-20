package xtcpnl

import (
	"encoding/binary"
	"net"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// The SEG6 commands and attributes (linux/seg6_genl.h), which x/sys/unix
// does not define.
const (
	seg6CmdSetHmac      = 1
	seg6CmdDumpHmac     = 2
	seg6CmdSetTunsrc    = 3
	seg6CmdGetTunsrc    = 4
	seg6AttrDst         = 1
	seg6AttrHmacKeyID   = 3
	seg6AttrSecret      = 4
	seg6AttrSecretLen   = 5
	seg6AttrAlgID       = 6
	seg6HmacSecretLen   = 64
	seg6HmacSecretLenIP = 63
)

// seg6Rows is every C2-T02 row, in the corpus's order.
var seg6Rows = []string{
	"sr_hmac_show_empty", "sr_hmac_set", "sr_hmac_set_sha1", "sr_hmac_replace", "sr_hmac_delete",
	"sr_hmac_delete_missing", "sr_hmac_set_long", "sr_hmac_show", "sr_hmac_show_64",
	"sr_tunsrc_show", "sr_tunsrc_set",
}

// seg6Attrs renders a SEG6 message's attributes in the order they came:
// key=, slen=, alg=, secret= (the bytes as text) and dst=.
func seg6Attrs(t *testing.T, attrs []NetlinkAttribute) string {
	t.Helper()
	var out []string
	for _, a := range attrs {
		switch a.ID() {
		case seg6AttrHmacKeyID:
			out = append(out, "key="+u32s(binary.LittleEndian.Uint32(a.Data)))
		case seg6AttrSecretLen:
			out = append(out, "slen="+strconv.Itoa(int(a.Data[0])))
		case seg6AttrAlgID:
			out = append(out, "alg="+strconv.Itoa(int(a.Data[0])))
		case seg6AttrSecret:
			out = append(out, "secret="+string(a.Data))
		case seg6AttrDst:
			out = append(out, "dst="+net.IP(a.Data).String())
		}
	}
	return strings.Join(out, " ")
}

// seg6Msgs describes each SEG6 message in a pcap, requests when request
// is true and the kernel's answers otherwise: the command, "dump" and
// "ack" for those flags on a request, then its attributes. A request's
// attributes keep their order; the answers are sorted, since a dump walks
// a hash table.
func seg6Msgs(t *testing.T, rel string, request bool) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+rel)
	fam := genlFamilyID(t, msgs, "SEG6")
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
			seg6CmdSetHmac: "sethmac", seg6CmdDumpHmac: "dumphmac",
			seg6CmdSetTunsrc: "set_tunsrc", seg6CmdGetTunsrc: "get_tunsrc",
		}[g.Command]}
		if request && m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			words = append(words, "dump")
		}
		if request && m.h.Flags&unix.NLM_F_ACK != 0 {
			words = append(words, "ack")
		}
		if a := seg6Attrs(t, g.Attributes); a != "" {
			words = append(words, a)
		}
		out = append(out, strings.Join(words, " "))
	}
	if !request {
		slices.Sort(out)
	}
	return strings.Join(out, "|")
}

// seg6Errno is the errno the kernel answered a row's SEG6 request with,
// then its ip.rc.
func seg6Errno(t *testing.T, slug string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap")
	fam := genlFamilyID(t, msgs, "SEG6")
	for _, m := range msgs {
		if fam != 0 && m.h.Type == fam && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return strconv.Itoa(int(answerTo(t, msgs, m.h))) + " rc " + tcpmFile(t, slug+"/ip.rc")
		}
	}
	return "no request"
}

// TestGenlSeg6Corpus checks the C2-T02 rows (testdata/7_1_4/link-write/
// genl/, scripts/genl-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// `ip sr`'s HMAC keys and tunnel source, each command's request and the
// kernel's answer.
func TestGenlSeg6Corpus(t *testing.T) {
	long := strings.Repeat("x", seg6HmacSecretLen+1)
	maxSecret := strings.Repeat("y", seg6HmacSecretLen)
	sha1, sha256 := "alg=1", "alg=2"
	req := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return seg6Msgs(t, rel, true) }
	}
	replies := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return seg6Msgs(t, rel, false) }
	}
	errno := func(slug string) func(t *testing.T) string {
		return func(t *testing.T) string { return seg6Errno(t, slug) }
	}
	key1 := "dumphmac key=1 slen=6 secret=s3cret " + sha256
	key2 := "dumphmac key=2 slen=5 secret=0ther " + sha1
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: every row resolves SEG6 by name inside its own pcap",
			func(t *testing.T) string {
				var missing []string
				for _, slug := range seg6Rows {
					if genlFamilyID(t, pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap"), "SEG6") == 0 {
						missing = append(missing, slug)
					}
				}
				return strings.Join(missing, " ")
			}, ""},
		{"positive: hmac show is a DUMPHMAC dump", req("sr_hmac_show_empty/ip.pcap"), "dumphmac dump"},
		{"boundary: with no key the dump is empty", replies("sr_hmac_show_empty/ip.pcap"), ""},
		{"positive: hmac set is a SETHMAC with the key, length, algorithm and secret",
			req("sr_hmac_set/ip.pcap"), "sethmac ack key=1 slen=6 " + sha256 + " secret=s3cret"},
		{"positive: answered with an ACK", errno("sr_hmac_set"), "0 rc 0"},
		{"positive: and the key reads back", replies("sr_hmac_set/netlink_generic_hmac.pcap"), key1},
		{"positive: sha1 is algorithm 1", req("sr_hmac_set_sha1/ip.pcap"), "sethmac ack key=2 slen=5 " + sha1 + " secret=0ther"},
		{"positive: a set of an existing key replaces it",
			replies("sr_hmac_replace/netlink_generic_hmac.pcap"), "dumphmac key=1 slen=3 secret=n3w " + sha1},
		{"positive: a blank secret is SECRETLEN 0 and no SECRET", req("sr_hmac_delete/ip.pcap"), "sethmac ack key=1 slen=0 " + sha256},
		{"positive: which deletes the key", replies("sr_hmac_delete/netlink_generic_hmac.pcap"), key2},
		{"negative: deleting a key that does not exist is ENOENT, and ip exits 1", errno("sr_hmac_delete_missing"),
			strconv.Itoa(int(syscall.ENOENT)) + " rc 1"},
		{"negative: a 65-byte secret is EINVAL, and ip exits 1", errno("sr_hmac_set_long"),
			strconv.Itoa(int(syscall.EINVAL)) + " rc 1"},
		{"negative: the refused request still carries the whole secret",
			req("sr_hmac_set_long/ip.pcap"), "sethmac ack key=3 slen=65 " + sha256 + " secret=" + long},
		{"positive: a dump of two keys", replies("sr_hmac_show/ip.pcap"), key1 + "|" + key2},
		{"boundary: the kernel keeps and returns a 64-byte secret",
			replies("sr_hmac_show_64/ip.pcap"), "dumphmac key=4 slen=64 secret=" + maxSecret + " " + sha256},
		{"boundary: ip prints 63 bytes of it",
			func(t *testing.T) string { return tcpmFile(t, "sr_hmac_show_64/ip.out") },
			"hmac 4 algo sha256 secret \"" + maxSecret[:seg6HmacSecretLenIP] + "\""},
		{"boundary: and says so on stderr",
			func(t *testing.T) string { return tcpmFile(t, "sr_hmac_show_64/ip.err") },
			"HMAC secret length 64 > 63, truncated"},
		{"positive: tunsrc show is a GET_TUNSRC, not a dump", req("sr_tunsrc_show/ip.pcap"), "get_tunsrc"},
		{"boundary: a namespace's tunnel source starts as ::", replies("sr_tunsrc_show/ip.pcap"), "get_tunsrc dst=::"},
		{"positive: tunsrc set is a SET_TUNSRC with the address", req("sr_tunsrc_set/ip.pcap"), "set_tunsrc ack dst=2001:db8::1"},
		{"positive: which reads back", replies("sr_tunsrc_set/netlink_generic_tunsrc.pcap"), "get_tunsrc dst=2001:db8::1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
