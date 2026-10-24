package goip

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The C3 corpus's SAs, as xfrm-write-corpus.exp spells them.
const (
	xfrmTestAuthKey = "0x1111111111111111111111111111111111111111111111111111111111111111"
	xfrmTestEncKey  = "0x22222222222222222222222222222222"
	xfrmTestSA1ID   = "src 192.0.2.1 dst 192.0.2.2 proto esp spi 0x100"
	xfrmTestSA1     = "state add " + xfrmTestSA1ID + " reqid 1 mode transport auth sha256 " + xfrmTestAuthKey +
		" enc aes " + xfrmTestEncKey
	xfrmTestSA2 = "state add src 2001:db8::1 dst 2001:db8::2 proto esp spi 0x101 reqid 2 mode tunnel " +
		"auth-trunc sha256 " + xfrmTestAuthKey + " 128 enc aes " + xfrmTestEncKey
	xfrmTestAlgos = " mode transport auth sha256 " + xfrmTestAuthKey + " enc aes " + xfrmTestEncKey
)

// xfrmWriteRun runs `goip xfrm ARGS` over src with GOIP_WRITE_OUT set, and
// returns what it wrote, nil for nothing.
func xfrmWriteRun(t *testing.T, src Source, args string) (xfrmRun, []nlparity.Msg) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "write")
	t.Setenv(writeOutEnvCst, out)
	r := xfrmRunArgs(src, unix.AF_UNSPEC, false, args)
	b, err := os.ReadFile(out)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := nlparity.ParseWriteRequestsProto(b, unix.NETLINK_XFRM, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r, msgs
}

// TestXfrmStateWritesMatchIp replays the 20 C3 state write captures, and
// other spellings that send the same bytes. The reads goip sends must be
// ip's, which a replay answers only then, and every read exchange must be
// replayed and no write; the writes goip wrote to GOIP_WRITE_OUT must be
// the capture's, seq aside, in order; stdout must be ip's, but allocspi's,
// whose larval SA goip has no answer to print. A row the kernel refused
// sends the same write, so goip exits 0; one ip refused before sending
// exits as ip did and writes nothing.
//
// go test ./internal/goip/ -run TestXfrmStateWritesMatchIp
func TestXfrmStateWritesMatchIp(t *testing.T) {
	tests := []struct {
		description string
		row         string
		args        string
		exit        int
		noOut       bool // ip printed the kernel's answer
	}{
		{description: "positive: add, SA 1, auth and enc", row: "state_add", args: xfrmTestSA1},
		{description: "positive: add, SA 2, IPv6 tunnel, auth-trunc", row: "state_add_v6_tunnel", args: xfrmTestSA2},
		{description: "positive: add, aead and its ICV length", row: "state_add_aead",
			args: "state add src 192.0.2.1 dst 192.0.2.3 proto esp spi 0x102 reqid 3 mode transport aead " +
				"rfc4106(gcm(aes)) 0x3333333333333333333333333333333333333333 128"},
		{description: "positive: add, enc alone", row: "state_add_enc_only",
			args: "state add src 192.0.2.1 dst 192.0.2.4 proto esp spi 0x103 mode transport enc aes " + xfrmTestEncKey},
		{description: "negative: add of an SA there, the kernel's EEXIST, the same NEWSA", row: "state_add_dup",
			args: xfrmTestSA1},
		{description: "negative: an algorithm the kernel lacks, the same NEWSA", row: "state_add_unknown_alg",
			args: "state add " + xfrmTestSA1ID + " mode transport enc nosuchalg " + xfrmTestEncKey},
		{description: "negative: a key short of aes's, the same NEWSA", row: "state_add_short_key",
			args: "state add " + xfrmTestSA1ID + " mode transport enc aes 0x222222222222222222222222222222"},
		{description: "negative: ESP with no enc or aead, refused before sending, exit 1", row: "state_add_no_algo",
			args: "state add " + xfrmTestSA1ID + " mode transport", exit: ExitUsage},
		{description: "positive: update, with a hard time limit", row: "state_update",
			args: "state update " + xfrmTestSA1ID + " reqid 1" + xfrmTestAlgos + " limit time-hard 3600"},
		{description: "positive: update of a larval SA", row: "state_update_larval",
			args: "state update src 192.0.2.1 dst 192.0.2.2 proto esp spi 0x200" + xfrmTestAlgos},
		{description: "negative: update of a missing SA, the same UPDSA", row: "state_update_missing",
			args: "state update " + xfrmTestSA1ID + " reqid 1" + xfrmTestAlgos},
		{description: "positive: delete, one DELSA", row: "state_delete", args: "state delete " + xfrmTestSA1ID},
		{description: "negative: delete of a missing SA, the same DELSA", row: "state_delete_missing",
			args: "state delete " + xfrmTestSA1ID},
		{description: "positive: deleteall, two DELSAs in one datagram between the dumps", row: "state_deleteall",
			args: "state deleteall"},
		{description: "boundary: deleteall with none, one dump and no write", row: "state_deleteall_empty",
			args: "state deleteall"},
		{description: "positive: flush, every protocol", row: "state_flush", args: "state flush"},
		{description: "positive: flush proto ah", row: "state_flush_proto_ah", args: "state flush proto ah"},
		{description: "positive: allocspi, its larval SA unprinted", row: "state_allocspi",
			args: "state allocspi src 192.0.2.1 dst 192.0.2.2 proto esp min 0x200 max 0x200", noOut: true},
		{description: "negative: allocspi of a taken range, the same ALLOCSPI", row: "state_allocspi_taken",
			args: "state allocspi src 192.0.2.1 dst 192.0.2.2 proto esp min 0x100 max 0x100"},
		{description: "negative: allocspi min above max, refused before sending, exit 1",
			row: "state_allocspi_min_gt_max", exit: ExitUsage,
			args: "state allocspi src 192.0.2.1 dst 192.0.2.2 proto esp min 0x300 max 0x200"},
		{description: "corner: a is add, the ID's words in another order", row: "state_add",
			args: "state a spi 256 proto esp dst 192.0.2.2 src 192.0.2.1 reqid 1" + xfrmTestAlgos},
		{description: "corner: u is update, the limit before the algorithms", row: "state_update",
			args: "state u " + xfrmTestSA1ID + " limit time-hard 3600 reqid 1" + xfrmTestAlgos},
		{description: "corner: d is delete, not deleteall", row: "state_delete", args: "sa d " + xfrmTestSA1ID},
		{description: "corner: delall is deleteall", row: "state_deleteall", args: "state delall"},
		{description: "corner: f is flush, ipsec-any every protocol's 255", row: "state_flush_proto_ah",
			args: "state f proto ah"},
		{description: "corner: al is allocspi, min and max the other way about", row: "state_allocspi_taken",
			args: "state al max 0x100 min 256 src 192.0.2.1 dst 192.0.2.2 proto esp"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(tdXfrm, tc.row)
			src, err := OpenReplay(filepath.Join(dir, "ip.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			r, writes := xfrmWriteRun(t, src, tc.args)
			if code := exitCodeOf(r.err); r.err != nil && code != tc.exit || r.err == nil && tc.exit != 0 {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if want := mustReadFile(t, filepath.Join(dir, "ip.out")); !tc.noOut && r.stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, want)
			}
			if r.stderr != "" {
				t.Errorf("stderr %q, want none", r.stderr)
			}
			var ipWrites [][]byte
			for i, txn := range src.xseg.Txns {
				write := nlparity.IsWrite(txn.Request)
				if write {
					ipWrites = append(ipWrites, canonMsg(txn.Request))
				}
				if src.xused[i] == write {
					t.Errorf("exchange %d (%s): replayed %v, a write %v", i, nlparity.MsgName(txn.Request),
						src.xused[i], write)
				}
			}
			if len(writes) != len(ipWrites) {
				t.Fatalf("goip wrote %d writes, ip sent %d", len(writes), len(ipWrites))
			}
			for i := range writes {
				if got := canonMsg(writes[i]); !bytes.Equal(got, ipWrites[i]) {
					t.Errorf("write %d\n got %x\nwant %x", i, got, ipWrites[i])
				}
			}
		})
	}
}

// TestXfrmStateWritesArgs checks the state writes' words, refusals and
// requests without a capture: the exit status, a fragment of the error,
// "" for none, and a fragment of the hex of the write's bytes after its
// nlmsghdr, "-" for no write.
//
// go test ./internal/goip/ -run TestXfrmStateWritesArgs
func TestXfrmStateWritesArgs(t *testing.T) {
	const add = "state add " + xfrmTestSA1ID + " mode transport enc aes 0x22 "
	tests := []struct {
		description string
		args        string
		exit        int
		expected    string // a fragment of the error
		hex         string // a fragment of the write's hex
		absent      string // hex the write must not hold
		stderr      string // a fragment of stderr
	}{
		// Positive: each word's attribute or field.
		{description: "positive: seq, big-endian in the SA, then reqid, the family and the mode",
			args: add + "seq 0x01020304 reqid 0x11", hex: "01020304110000000200"},
		{description: "positive: a mark with only its mask set is sent", args: add + "mark 0 mask 0xff",
			hex: "0c00150000000000ff000000"},
		{description: "positive: mark, XFRMA_MARK", args: add + "mark 7 mask 0xff", hex: "0c00150007000000ff000000"},
		{description: "positive: if_id, XFRMA_IF_ID", args: add + "if_id 5", hex: "08001f0005000000"},
		{description: "positive: tfcpad, XFRMA_TFCPAD", args: add + "tfcpad 9", hex: "0800160009000000"},
		{description: "positive: output-mark and its mask, after the others", args: add + "output-mark 3 mask 4",
			hex: "08001d000300000008001e0004000000"},
		{description: "positive: dir out, XFRMA_SA_DIR", args: add + "dir out", hex: "0500210002"},
		{description: "positive: pcpu-num, XFRMA_SA_PCPU", args: add + "pcpu-num 1", hex: "0800230001000000"},
		{description: "positive: extra-flag words ORed", args: add + "extra-flag dont-encap-dscp oseq-may-wrap",
			hex: "0800180003000000"},
		{description: "positive: extra-flag in hex", args: add + "extra-flag 0x4", hex: "0800180004000000"},
		{description: "positive: replay-seq, XFRMA_REPLAY_VAL", args: add + "replay-seq 2 replay-oseq 3",
			hex: "10000a00030000000200000000000000"},
		{description: "positive: flag esn and a window, XFRMA_REPLAY_ESN_VAL", args: add + "flag esn replay-window 64",
			hex: "1c001700020000000000000000000000000000000000000040000000"},
		{description: "positive: encap espinudp, XFRMA_ENCAP", args: add + "encap espinudp 4500 4500 0.0.0.0",
			hex: "1c000400020011941194"},
		{description: "positive: ctx, XFRMA_SEC_CTX", args: add + "ctx ab", hex: "0f000800 0b0008000101030061620000"},
		{description: "positive: a raw key's bytes", args: "state add " + xfrmTestSA1ID + " enc aes kk",
			hex: "6b6b"},
		{description: "positive: comp under proto comp", args: "state add src 192.0.2.1 dst 192.0.2.2 proto comp " +
			"spi 0x100 comp deflate", hex: "480003006465666c61746500"},
		{description: "positive: sel, the whole selector: addresses, ports and masks, family, prefix lengths",
			args: add + "sel src 10.0.0.0/8 dst 10.1.0.0/16 proto 6 sport 1024 dport 80",
			hex: "0a010000" + strings.Repeat("00", 12) + "0a000000" + strings.Repeat("00", 12) +
				"0050ffff 0400ffff 0200 10 08 06000000 00000000 00000000"},
		{description: "negative: after sel, coa takes the selector's family", args: "state add src 192.0.2.1 " +
			"dst 192.0.2.2 proto route2 mode ro sel src 10.0.0.0/8 coa 2001:db8::9", exit: ExitUsage,
			expected: "inet prefix is expected", hex: "-"},
		{description: "positive: delete with a mark, XFRMA_MARK", args: "state delete " + xfrmTestSA1ID + " mark 1",
			hex: "0c00150001000000ffffffff"},
		{description: "positive: flush proto esp", args: "state flush proto esp", hex: "32"},
		{description: "positive: allocspi of comp, IPComp's max", args: "state allocspi src 192.0.2.1 dst 192.0.2.2 " +
			"proto comp", hex: "00010000ffff0000"},
		{description: "positive: allocspi with dir, mark and pcpu-num", args: "state allocspi src 192.0.2.1 " +
			"dst 192.0.2.2 proto esp dir in mark 2 pcpu-num 3", hex: "05002100010000000c001500020000"},

		// Negative: ip's refusals.
		{description: "negative: no ID", args: "state add mode transport", exit: ExitUsage,
			expected: "ID is required", hex: "-"},
		{description: "negative: AH with enc", args: "state add src 192.0.2.1 dst 192.0.2.2 proto ah spi 1 " +
			"enc aes 0x22", exit: ExitUsage, expected: `"enc", "aead", and "comp" are invalid`, hex: "-"},
		{description: "negative: AH with no auth", args: "state add src 192.0.2.1 dst 192.0.2.2 proto ah spi 1",
			exit: ExitUsage, expected: `"auth" or "auth-trunc" is required`, hex: "-"},
		{description: "negative: comp under ESP", args: add + "comp deflate", exit: ExitUsage,
			expected: `"comp" is invalid with XFRM-PROTO value "esp"`, hex: "-"},
		{description: "negative: comp with enc", args: "state add src 192.0.2.1 dst 192.0.2.2 proto comp spi 1 " +
			"enc aes 0x22", exit: ExitUsage, expected: `"auth-trunc", and "aead" are invalid`, hex: "-"},
		{description: "negative: proto comp with nothing", args: "state add src 192.0.2.1 dst 192.0.2.2 proto comp " +
			"spi 1", exit: ExitUsage, expected: `"comp" is required`, hex: "-"},
		{description: "negative: mode beet under AH", args: "state add src 192.0.2.1 dst 192.0.2.2 proto ah spi 1 " +
			"mode beet auth sha256 0x11", exit: ExitUsage, expected: "MODE value is invalid", hex: "-"},
		{description: "negative: coa under ESP", args: add + "coa 192.0.2.9", exit: ExitUsage,
			expected: `"coa" is invalid`, hex: "-"},
		{description: "negative: route2 without a mode", args: "state add src 192.0.2.1 dst 192.0.2.2 proto route2 " +
			"coa 192.0.2.9", exit: ExitUsage, expected: `"mode" is required`, hex: "-"},
		{description: "negative: route2 without coa", args: "state add src 192.0.2.1 dst 192.0.2.2 proto route2 " +
			"mode ro", exit: ExitUsage, expected: `"coa" is required`, hex: "-"},
		{description: "negative: an algorithm under route2", args: "state add src 192.0.2.1 dst 192.0.2.2 " +
			"proto route2 mode ro coa 192.0.2.9 enc aes 0x22", exit: ExitUsage,
			expected: `ALGO is invalid with XFRM-PROTO value "route2"`, hex: "-"},
		{description: "negative: enc twice", args: add + "enc aes 0x22", exit: ExitMinusOne,
			expected: `duplicate "ALGO-TYPE"`, hex: "-"},
		{description: "negative: enc after aead", args: "state add " + xfrmTestSA1ID + " aead x 0x11 64 " +
			"enc aes 0x22", exit: ExitMinusOne, expected: `duplicate "ALGO-TYPE"`, hex: "-"},
		{description: "negative: no ID, the ID's check ahead of the algorithms'", args: "state add enc aes 0x22",
			exit: ExitUsage, expected: "ID is required", hex: "-"},
		{description: "negative: aead after auth", args: "state add " + xfrmTestSA1ID + " auth sha256 0x11 " +
			"aead x 0x11 64", exit: ExitMinusOne, expected: `duplicate "ALGO-TYPE"`, hex: "-"},
		{description: "negative: a second ID", args: add + "src 192.0.2.5", exit: ExitMinusOne,
			expected: "unknown", hex: "-"},
		{description: "negative: a bad hex key", args: "state add " + xfrmTestSA1ID + " enc aes 0xzz",
			exit: ExitMinusOne, expected: "ALGO-KEYMAT value is invalid", hex: "-"},
		{description: "negative: esn without a window", args: add + "flag esn", exit: ExitMinusOne,
			expected: "esn flag set without replay-window", hex: "-"},
		{description: "negative: a bad dir", args: add + "dir up", exit: ExitMinusOne,
			expected: `DIR value is not "in" or "out"`, hex: "-"},
		{description: "negative: a bad limit word", args: add + "limit bytes 1", exit: ExitMinusOne,
			expected: "LIMIT value is invalid", hex: "-"},
		{description: "negative: a bad encap type", args: add + "encap udp 1 2 0.0.0.0", exit: ExitMinusOne,
			expected: "ENCAP-TYPE value is invalid", hex: "-"},
		{description: "negative: offload without dev", args: add + "offload crypto eth0", exit: ExitMinusOne,
			expected: "Missing dev keyword", hex: "-"},
		{description: "negative: coa twice", args: "state add src 192.0.2.1 dst 192.0.2.2 proto route2 " +
			"mode ro coa 192.0.2.9 coa 192.0.2.8", exit: ExitMinusOne, expected: `duplicate "coa"`, hex: "-"},
		{description: "negative: allocspi with an SPI", args: "state allocspi " + xfrmTestSA1ID, exit: ExitUsage,
			expected: `"spi" is invalid`, hex: "-"},
		{description: "negative: allocspi min without max", args: "state allocspi src 192.0.2.1 dst 192.0.2.2 " +
			"proto esp min 1", exit: ExitUsage, expected: `"max" is missing`, hex: "-"},
		{description: "negative: allocspi max without min", args: "state allocspi src 192.0.2.1 dst 192.0.2.2 " +
			"proto esp max 1", exit: ExitUsage, expected: `"min" is missing`, hex: "-"},
		{description: "negative: allocspi min twice", args: "state allocspi src 192.0.2.1 dst 192.0.2.2 " +
			"proto esp min 1 min 2", exit: ExitMinusOne, expected: `duplicate "min"`, hex: "-"},
		{description: "negative: flush of an unknown protocol", args: "state flush proto tcp", exit: ExitMinusOne,
			expected: "XFRM-PROTO value is invalid", hex: "-"},
		{description: "negative: flush proto twice", args: "state flush proto ah proto esp", exit: ExitMinusOne,
			expected: `duplicate "proto"`, hex: "-"},
		{description: "negative: flush of another word", args: "state flush all", exit: ExitMinusOne,
			expected: "unknown", hex: "-"},

		// Boundary.
		{description: "boundary: a window of 32 stays in the SA", args: add + "reqid 0x11 replay-window 32",
			hex: "1100000002000020", absent: "1c001700"},
		{description: "boundary: a window of 33, XFRMA_REPLAY_ESN_VAL", args: add + "reqid 0x11 replay-window 33",
			hex: "1100000002000000"},
		{description: "boundary: the 2048-byte buffer, each attribute past it refused on stderr and the rest sent",
			args: add + strings.Repeat("encap espinudp 1 2 0.0.0.0 ", 70), stderr: "addattr_l ERROR: message exceeded " +
				"bound of 2048", hex: "1c000400020000010002"},
		{description: "boundary: allocspi min one above max", args: "state allocspi src 192.0.2.1 dst 192.0.2.2 " +
			"proto esp min 6 max 5", exit: ExitUsage, expected: `"min" is larger than value after "max"`, hex: "-"},
		{description: "boundary: a window of 4096", args: add + "replay-window 4096",
			hex: "1c00170080000000"},
		{description: "boundary: a window of 4097", args: add + "replay-window 4097", exit: ExitMinusOne,
			expected: "replay-window (4097) > XFRMA_REPLAY_ESN_MAX (4096)", hex: "-"},
		{description: "boundary: a key of 512 bytes", args: "state add " + xfrmTestSA1ID + " enc aes 0x" +
			strings.Repeat("22", 512), hex: "00100000"},
		{description: "boundary: a key of 513 bytes", args: "state add " + xfrmTestSA1ID + " enc aes 0x" +
			strings.Repeat("22", 513), exit: ExitMinusOne, expected: "makes buffer overflow", hex: "-"},
		{description: "boundary: an ALGO with no name", args: "state add " + xfrmTestSA1ID + " enc",
			exit: ExitMinusOne, expected: `argument "ALGO-NAME" is required`, hex: "-"},
		{description: "boundary: auth-trunc with no length", args: "state add " + xfrmTestSA1ID +
			" auth-trunc sha256 0x11", exit: ExitMinusOne, expected: `argument "ALGO-TRUNC-LEN" is required`,
			hex: "-"},
		{description: "boundary: allocspi min equal to max", args: "state allocspi src 192.0.2.1 dst 192.0.2.2 " +
			"proto esp min 5 max 5", hex: "0500000005000000"},

		// Corner.
		{description: "corner: an odd hex key gets a leading 0", args: "state add " + xfrmTestSA1ID +
			" enc aes 0x123", hex: "100000000123"},
		{description: "corner: 0x alone is a raw key", args: "state add " + xfrmTestSA1ID + " enc aes 0x",
			hex: "100000003078"},
		{description: "corner: extra-flag stops at another word", args: add + "extra-flag oseq-may-wrap if_id 1",
			hex: "08001800020000000800 1f0001000000"},
		{description: "corner: output-mark with no mask leaves the next word, its attribute after tfcpad's",
			args: add + "output-mark 3 tfcpad 2", hex: "080016000200000008001d0003000000"},
		{description: "corner: a selector protocol name is not implemented", args: add + "sel proto tcp",
			exit: ExitUsage, expected: "not implemented", hex: "-"},
		{description: "corner: sport with a protocol ip names is not implemented", args: add + "sel proto 47 sport 1",
			exit: ExitUsage, expected: "not implemented", hex: "-"},
		{description: "corner: a ctx past ip's buffer is not implemented", args: add + "ctx " +
			strings.Repeat("c", 256), exit: ExitUsage, expected: "not implemented", hex: "-"},
		{description: "corner: an ID with no address is AF_INET's 2", args: "state add proto esp spi 1 reqid 0x11 " +
			"enc aes 0x22", hex: "110000000200"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, writes := xfrmWriteRun(t, &xfrmFake{}, tc.args)
			if code := exitCodeOf(r.err); r.err != nil && code != tc.exit || r.err == nil && tc.exit != 0 {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if tc.expected != "" && (r.err == nil || !strings.Contains(r.err.Error(), tc.expected)) {
				t.Errorf("err %v, want one containing %q", r.err, tc.expected)
			}
			if tc.stderr != "" && !strings.Contains(r.stderr, tc.stderr) {
				t.Errorf("stderr %q, want it to hold %q", r.stderr, tc.stderr)
			}
			if tc.hex == "-" {
				if writes != nil {
					t.Errorf("%d writes, want none", len(writes))
				}
				return
			}
			if len(writes) != 1 {
				t.Fatalf("%d writes, want 1", len(writes))
			}
			got := hexOf(writes[0].Body)
			if !strings.Contains(got, strings.ReplaceAll(tc.hex, " ", "")) {
				t.Errorf("write %s, want it to hold %s", got, tc.hex)
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("write %s, want it without %s", got, tc.absent)
			}
			if len(writes[0].Body)+xtcpnl.NlMsgHdrSizeCst > xfrmRtaBufCst {
				t.Errorf("write of %d bytes, past ip's %d", len(writes[0].Body)+xtcpnl.NlMsgHdrSizeCst, xfrmRtaBufCst)
			}
		})
	}
}

// xfrmWithAttrs is an SA's body with attrs after its attributes.
func xfrmWithAttrs(m xfrmMsg, attrs ...byte) xfrmMsg {
	m.body = append(append([]byte(nil), m.body...), attrs...)
	return m
}

// TestXfrmStateDeleteall runs deleteall over a fake source that answers
// every dump as a kernel nothing was deleted from does, as goip's runs are:
// the number of dumps, of DELSAs written, a fragment of their hex, the exit
// status, and a fragment of the error or stderr.
//
// go test ./internal/goip/ -run TestXfrmStateDeleteall
func TestXfrmStateDeleteall(t *testing.T) {
	sas := xfrmCaptureSAs(t)
	ipip := xfrmWithAttrs(sas[1])
	ipip.body[76] = unix.IPPROTO_IPIP
	// XFRMA_MARK 5/0xff, alone and with XFRMA_IF_ID 9 after it.
	mark := []byte{12, 0, 21, 0, 5, 0, 0, 0, 0xff, 0, 0, 0}
	markIfID := []byte{12, 0, 21, 0, 5, 0, 0, 0, 0xff, 0, 0, 0, 8, 0, 31, 0, 9, 0, 0, 0}
	tests := []struct {
		description string
		dump        []xfrmMsg
		dumpErr     error
		args        string
		dumps       int
		writes      int
		hex         string // a fragment of the writes' hex
		exit        int
		text        string // a fragment of the error, or of stderr
	}{
		{description: "positive: two SAs, one round of two, then a round copying none", dump: sas,
			args: "state deleteall", dumps: 2, writes: 2},
		{description: "positive: a filter on reqid copies one", dump: sas, args: "state deleteall reqid 1",
			dumps: 2, writes: 1, hex: "c0000202"},
		{description: "positive: nokeys is read and changes nothing", dump: sas, args: "state deleteall nokeys",
			dumps: 2, writes: 2},
		{description: "positive: a mark copied with the four bytes after it", dump: []xfrmMsg{
			xfrmWithAttrs(sas[1], markIfID...)}, args: "state deleteall", dumps: 2, writes: 1,
			hex: "1000150005000000ff00000008001f00"},
		{description: "boundary: none, one dump and no write", args: "state deleteall", dumps: 1},
		{description: "negative: a dump error is Delete-all terminated, exit 1", dumpErr: unix.EPERM,
			args: "state deleteall", dumps: 1, exit: ExitUsage, text: "Delete-all terminated"},
		{description: "negative: a short SA ends the dump", dump: []xfrmMsg{{typ: xtcpnl.XfrmMsgNewSA,
			body: make([]byte, 100)}}, args: "state deleteall", dumps: 1, exit: ExitUsage, text: "wrong nlmsg len"},
		{description: "negative: a second ID", args: "state deleteall 192.0.2.1 192.0.2.2", exit: ExitMinusOne,
			text: "unknown"},
		{description: "corner: an IPIP SA is skipped", dump: []xfrmMsg{ipip}, args: "state deleteall", dumps: 1},
		{description: "corner: another type is Not a state on stderr", dump: []xfrmMsg{{typ: xtcpnl.XfrmMsgNewPolicy,
			body: make([]byte, 4)}}, args: "state deleteall", dumps: 1, text: "Not a state: 00000014 00000013"},
		{description: "corner: a mark ending its message is not implemented", dump: []xfrmMsg{
			xfrmWithAttrs(sas[1], mark...)}, args: "state deleteall", dumps: 1, exit: ExitUsage,
			text: "not implemented"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &xfrmFake{dump: tc.dump, dumpErr: tc.dumpErr}
			r, writes := xfrmWriteRun(t, f, tc.args)
			if code := exitCodeOf(r.err); r.err != nil && code != tc.exit || r.err == nil && tc.exit != 0 {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if tc.text != "" && !strings.Contains(fmt.Sprint(r.err)+r.stderr, tc.text) {
				t.Errorf("err %v, stderr %q, want one holding %q", r.err, r.stderr, tc.text)
			}
			if len(f.reqs) != tc.dumps {
				t.Errorf("%d dumps, want %d", len(f.reqs), tc.dumps)
			}
			if len(writes) != tc.writes {
				t.Fatalf("%d writes, want %d", len(writes), tc.writes)
			}
			var all []byte
			for _, w := range writes {
				if w.Hdr.Type != xtcpnl.XfrmMsgDelSA || w.Hdr.Flags != unix.NLM_F_REQUEST {
					t.Errorf("write type %d flags %#x, want a DELSA without NLM_F_ACK", w.Hdr.Type, w.Hdr.Flags)
				}
				all = append(all, w.Body...)
			}
			if got := hexOf(all); !strings.Contains(got, tc.hex) {
				t.Errorf("writes %s, want them to hold %s", got, tc.hex)
			}
		})
	}
}
