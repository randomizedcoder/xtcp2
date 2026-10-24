package xtcpnl

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// seg6Render shows a decoded SEG6 body as its command, then each field
// present, or the error.
func seg6Render(m Seg6, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	if m.Dst.IsValid() {
		out = append(out, "dst="+m.Dst.String())
	}
	if m.DstLen != nil {
		out = append(out, "dstlen="+strconv.Itoa(int(*m.DstLen)))
	}
	if m.HmacKeyID != nil {
		out = append(out, "key="+strconv.FormatUint(uint64(*m.HmacKeyID), 10))
	}
	if m.SecretLen != nil {
		out = append(out, "slen="+strconv.Itoa(int(*m.SecretLen)))
	}
	if m.Secret != nil {
		out = append(out, "secret="+strconv.Quote(string(m.Secret)))
	}
	if m.AlgID != nil {
		out = append(out, "alg="+strconv.Itoa(int(*m.AlgID)))
	}
	return strings.Join(out, " ")
}

// TestParseSeg6 decodes SEG6 bodies. The positive rows are bodies
// transcribed from link-write/genl/sr_*; the rest are hand-built wire edges
// the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseSeg6$'
func TestParseSeg6(t *testing.T) {
	set := []byte{Seg6CmdSetHmac, 1, 0, 0}
	key := rtattr(Seg6AttrHmacKeyID, le32(7))
	dst := rtattr(Seg6AttrDst, net.ParseIP("2001:db8::1"))
	long := strings.Repeat("y", 64)
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: ip's hmac set, the key, length, algorithm and padded secret (sr_hmac_set)",
			body:        mustHex(t, "010100000800030001000000050005000600000005000600020000000a0004007333637265740000"),
			expected:    `cmd=1 ver=1 key=1 slen=6 secret="s3cret" alg=2`,
		},
		{
			description: "positive: the kernel's dump of that key, the secret before the algorithm (sr_hmac_set)",
			body:        mustHex(t, "02010000080003000100000005000500060000000a00040073336372657400000500060002000000"),
			expected:    `cmd=2 ver=1 key=1 slen=6 secret="s3cret" alg=2`,
		},
		{
			description: "positive: ip's delete is a SETHMAC with SECRETLEN 0 and no SECRET (sr_hmac_delete)",
			body:        mustHex(t, "01010000080003000100000005000500000000000500060002000000"),
			expected:    "cmd=1 ver=1 key=1 slen=0 alg=2",
		},
		{
			description: "boundary: the kernel's dump of a 64-byte secret, the most it keeps (sr_hmac_show_64)",
			body: mustHex(t, "020100000800030004000000050005004000000044000400"+
				strings.Repeat("79", 64)+"0500060002000000"),
			expected: `cmd=2 ver=1 key=4 slen=64 secret="` + long + `" alg=2`,
		},
		{
			description: "positive: ip's hmac set with a 65-byte secret the kernel refuses still decodes (sr_hmac_set_long)",
			body: mustHex(t, "0101000008000300030000000500050041000000050006000200000045000400"+
				strings.Repeat("78", 65)+"000000"),
			expected: `cmd=1 ver=1 key=3 slen=65 secret="` + strings.Repeat("x", 65) + `" alg=2`,
		},
		{
			description: "boundary: ip's hmac show is the genl header alone (sr_hmac_show_empty)",
			body:        mustHex(t, "02010000"),
			expected:    "cmd=2 ver=1",
		},
		{
			description: "positive: the kernel's tunnel source starts as :: (sr_tunsrc_show)",
			body:        mustHex(t, "040100001400010000000000000000000000000000000000"),
			expected:    "cmd=4 ver=1 dst=::",
		},
		{
			description: "positive: ip's tunsrc set (sr_tunsrc_set)",
			body:        mustHex(t, "030100001400010020010db8000000000000000000000001"),
			expected:    "cmd=3 ver=1 dst=2001:db8::1",
		},
		{
			description: "boundary: a body one byte short of the genl header is an error",
			body:        set[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
		{
			description: "corner: a present, empty SECRET is an empty secret, not an absent one",
			body:        cat(set, key, rtattr(Seg6AttrSecret, nil)),
			expected:    `cmd=1 ver=1 key=7 secret=""`,
		},
		{
			description: "corner: a SECRETLEN longer than the SECRET is reported as sent",
			body:        cat(set, key, rtattr(Seg6AttrSecretLen, []byte{9}), rtattr(Seg6AttrSecret, []byte("ab"))),
			expected:    `cmd=1 ver=1 key=7 slen=9 secret="ab"`,
		},
		{
			description: "corner: DSTLEN, which no command uses, decodes signed",
			body:        cat(set, rtattr(Seg6AttrDstLen, le32(0xffffffff))),
			expected:    "cmd=1 ver=1 dstlen=-1",
		},
		{
			description: "corner: HMACINFO, an unknown attribute and an unknown command are not errors",
			body:        cat([]byte{9, 1, 0, 0}, rtattr(Seg6AttrHmacInfo, nil), rtattr(42, le32(1)), dst),
			expected:    "cmd=9 ver=1 dst=2001:db8::1",
		},
		{
			description: "corner: a four-byte SEG6_ATTR_UNSPEC (type 0) is not taken for an address",
			body:        cat(set, rtattr(0, net.ParseIP("192.0.2.1").To4())),
			expected:    "cmd=1 ver=1",
		},
		{
			description: "negative: a DST that is not sixteen bytes is an error, as in the kernel's policy",
			body:        cat(set, rtattr(Seg6AttrDst, net.ParseIP("192.0.2.1").To4())),
			expected:    genlErr + ": attribute 1 needs an unflagged 16-byte scalar",
		},
		{
			description: "negative: a key id narrower than a u32 is an error",
			body:        cat(set, rtattr(Seg6AttrHmacKeyID, []byte{1, 0})),
			expected:    genlErr + ": attribute 3 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: an algorithm wider than a u8 is an error",
			body:        cat(set, rtattr(Seg6AttrAlgID, le32(2))),
			expected:    genlErr + ": attribute 6 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: a SECRETLEN wider than a u8 is an error",
			body:        cat(set, rtattr(Seg6AttrSecretLen, le32(6))),
			expected:    genlErr + ": attribute 5 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: a repeated secret is an error",
			body:        cat(set, rtattr(Seg6AttrSecret, []byte("a")), rtattr(Seg6AttrSecret, []byte("b"))),
			expected:    genlErr + ": duplicate attribute 4",
		},
		{
			description: "negative: a repeated DSTLEN is an error",
			body:        cat(set, rtattr(Seg6AttrDstLen, le32(1)), rtattr(Seg6AttrDstLen, le32(2))),
			expected:    genlErr + ": duplicate attribute 2",
		},
		{
			description: "negative: an attribute longer than the body is an error",
			body:        cat(set, rtattrOverrunning(Seg6AttrSecret, 40)),
			expected:    genlErr + ": attribute length 40",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := seg6Render(ParseSeg6(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseSeg6Corpus decodes every SEG6 message in the committed corpus.
//
// go test ./pkg/xtcpnl/ -run TestParseSeg6Corpus
func TestParseSeg6Corpus(t *testing.T) {
	// tally decodes every SEG6 message of command cmd, ip's requests or
	// the kernel's answers, and counts the fields they carry.
	tally := func(cmd uint8, request bool) string {
		var n, errs, dst, keyID, secret, fit int
		for _, c := range corpusAll(t) {
			m := c.m
			if c.family != Seg6GenlName || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request || len(m.body) == 0 || m.body[0] != cmd {
				continue
			}
			n++
			s, err := ParseSeg6(m.body)
			if err != nil {
				errs++
				continue
			}
			for _, f := range []struct {
				ok bool
				n  *int
			}{{s.Dst.IsValid(), &dst}, {s.HmacKeyID != nil, &keyID}, {s.Secret != nil, &secret},
				{s.Secret != nil && s.SecretLen != nil && len(s.Secret) == int(*s.SecretLen), &fit}} {
				if f.ok {
					*f.n++
				}
			}
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(dst) + " dst, " +
			strconv.Itoa(keyID) + " key, " + strconv.Itoa(secret) + " secret, " + strconv.Itoa(fit) + " of slen"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{
			description: "positive: ip's SETHMAC requests decode, the two deletes with no secret",
			got:         tally(Seg6CmdSetHmac, true),
			expected:    "6 decoded, 0 errors, 0 dst, 6 key, 4 secret, 4 of slen",
		},
		{
			description: "positive: ip's DUMPHMAC requests decode, each the genl header alone",
			got:         tally(Seg6CmdDumpHmac, true),
			expected:    "7 decoded, 0 errors, 0 dst, 0 key, 0 secret, 0 of slen",
		},
		{
			description: "positive: every key the kernel dumped decodes with its id and a secret exactly SECRETLEN long",
			got:         tally(Seg6CmdDumpHmac, false),
			expected:    "7 decoded, 0 errors, 0 dst, 7 key, 7 secret, 7 of slen",
		},
		{
			description: "positive: ip's SET_TUNSRC requests decode with an address",
			got:         tally(Seg6CmdSetTunsrc, true),
			expected:    "1 decoded, 0 errors, 1 dst, 0 key, 0 secret, 0 of slen",
		},
		{
			description: "positive: ip's GET_TUNSRC requests decode with no address",
			got:         tally(Seg6CmdGetTunsrc, true),
			expected:    "2 decoded, 0 errors, 0 dst, 0 key, 0 secret, 0 of slen",
		},
		{
			description: "positive: every tunnel source the kernel answered decodes with an address",
			got:         tally(Seg6CmdGetTunsrc, false),
			expected:    "2 decoded, 0 errors, 2 dst, 0 key, 0 secret, 0 of slen",
		},
		{
			description: "negative: the kernel answers no SETHMAC",
			got:         tally(Seg6CmdSetHmac, false),
			expected:    "0 decoded, 0 errors, 0 dst, 0 key, 0 secret, 0 of slen",
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
