package goip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// macsec0Cst and macsec0IdxCst are the macsec rows' first SecY and its
// ifindex, macsec1 the next.
const (
	macsec0Cst    = "macsec0"
	macsec0IdxCst = 4
)

// macsecRunArgs runs `goip [-j] [-s] macsec ARGS`.
func macsecRunArgs(t *testing.T, src Source, json, stats bool, args string) genlRun {
	t.Helper()
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	var stdout, stderr bytes.Buffer
	c := newRunCtx(nil, &stdout, &stderr)
	c.src, c.json = src, json
	if stats {
		c.showStats = 1
	}
	r := genlRun{err: runMacsec(c, strings.Fields(args))}
	if _, serr := os.Stat(out); serr == nil {
		r.writes = genlWrites(t, out, xtcpnl.MacsecGenlName)
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// The statistics blocks of a SecY, a channel and an SA with every counter
// zero, print_fp_stats' two lines each.
const (
	macsecSecYStats0 = "    stats: OutPktsUntagged InPktsUntagged OutPktsTooLong InPktsNoTag InPktsBadTag " +
		"InPktsUnknownSCI InPktsNoSCI InPktsOverrun\n" +
		"                         0              0              0           0            0                0" +
		"           0             0\n"
	macsecTxscStats0 = "    stats: OutPktsProtected OutPktsEncrypted OutOctetsProtected OutOctetsEncrypted\n" +
		"                          0                0                  0                  0\n"
	macsecTxsaStats0 = "    stats: OutPktsProtected OutPktsEncrypted\n" +
		"                          0                0\n"
	macsecRxscStats0 = "    stats: InOctetsValidated InOctetsDecrypted InPktsUnchecked InPktsDelayed InPktsOK " +
		"InPktsInvalid InPktsLate InPktsNotValid InPktsNotUsingSA InPktsUnusedSA\n" +
		"                           0                 0               0             0        0" +
		"             0          0              0                0              0\n"
	macsecRxsaStats0 = "    stats: InPktsOK InPktsInvalid InPktsNotValid InPktsNotUsingSA InPktsUnusedSA\n" +
		"                  0             0              0                0              0\n"
)

// TestMacsecMatchesIp replays the 32 C2-T08 macsec captures, the readback
// shows the write rows captured after them, and other spellings over the
// same captures. As for l2tp, a run that gets past the lookup sent ip's
// GETFAMILY and dump byte for byte, seq aside; every read exchange must
// have been replayed and no write, the write goip made must be the
// capture's, stdout the capture's output or the text or JSON given, and
// the exit status ip's, but for a write the kernel refused, which goip
// does not send.
//
// go test ./internal/goip/ -run TestMacsecMatchesIp
func TestMacsecMatchesIp(t *testing.T) {
	const (
		rb, rbOut = "netlink_generic_macsec.pcap", "ip_macsec"
		key1      = " key 01000000000000000000000000000000 81818181818181818181818181818181"
		txsa      = "add macsec0 tx sa 0 pn 1 on" + key1
		rxsc      = "add macsec0 rx sci 0a0b0c0d0e0f0001 on"
		rxsa      = "add macsec0 rx sci 0a0b0c0d0e0f0001 sa 0 pn 1 on key 02000000000000000000000000000000 " +
			"82828282828282828282828282828282"
		head = "protect on validate strict sc off sa off encrypt off send_sci on end_station off scb off replay off \n" +
			"    cipher suite: GCM-AES-128, using ICV length 16\n"
		secy0JSON = `{"ifindex":4,"ifname":"macsec0","protect":true,"validate":"strict","sc":false,"sa":false,` +
			`"encrypt":false,"send_sci":true,"end_station":false,"scb":false,"replay":false,` +
			`"cipher_suite":"GCM-AES-128","icv_length":16,"sci":"0x102030405060001","encoding_sa":0,`
		secy1JSON = `{"ifindex":5,"ifname":"macsec1","protect":true,"validate":"strict","sc":false,"sa":false,` +
			`"encrypt":false,"send_sci":true,"end_station":false,"scb":false,"replay":false,` +
			`"cipher_suite":"GCM-AES-128","icv_length":16,"sci":"0x102030405060002","encoding_sa":0,`
		zeroSecY = `"OutPktsUntagged":0,"InPktsUntagged":0,"OutPktsTooLong":0,"InPktsNoTag":0,"InPktsBadTag":0,` +
			`"InPktsUnknownSCI":0,"InPktsNoSCI":0,"InPktsOverrun":0,`
		zeroTxsc = `"OutPktsProtected":0,"OutPktsEncrypted":0,"OutOctetsProtected":0,"OutOctetsEncrypted":0,`
	)
	tests := []struct {
		description string
		row         string
		pcap        string // ip.pcap when empty
		out         string // ip.out when empty
		args        string
		json, stats bool
		stdout      string // when not the capture's
		exit        int
	}{
		{description: "boundary: show with none", row: "macsec_show_empty", args: "show"},
		{description: "positive: show, two SecYs", row: "macsec_show", args: "show"},
		{description: "positive: show of one device, the other filtered out", row: "macsec_show_dev",
			args: "show macsec1"},
		{description: "negative: show of a device that does not exist, exit 1", row: "macsec_show_unknown_dev",
			args: "show nodev0", exit: ExitUsage},
		{description: "positive: add a transmit SA", row: "macsec_txsa_add", args: txsa},
		{description: "positive: an inactive SA, AN 3, PN 100", row: "macsec_txsa_add_off",
			args: "add macsec0 tx sa 3 pn 100 off" + key1},
		{description: "negative: an SA in use, the same request", row: "macsec_txsa_add_dup", args: txsa},
		{description: "negative: an add with no key is check_sa_args', exit 1, nothing sent",
			row: "macsec_txsa_add_no_key", args: "add macsec0 tx sa 1 pn 1", exit: ExitUsage},
		{description: "negative: an add with no PN, exit 1, nothing sent", row: "macsec_txsa_add_no_pn",
			args: "add macsec0 tx sa 1" + key1, exit: ExitUsage},
		{description: "negative: a 32-byte key on a 128-bit suite, the same request", row: "macsec_txsa_add_long_key",
			args: "add macsec0 tx sa 1 pn 1 key 01000000000000000000000000000000 " +
				"8181818181818181818181818181818181818181818181818181818181818181"},
		{description: "boundary: AN 4 is invarg, exit 255, nothing sent", row: "macsec_txsa_add_an4",
			args: "add macsec0 tx sa 4 pn 1" + key1, exit: ExitMinusOne},
		{description: "negative: a device that is not macsec, the same request", row: "macsec_txsa_add_not_macsec",
			args: "add d0 tx sa 0 pn 1" + key1},
		{description: "positive: set an SA's PN and state", row: "macsec_txsa_set", args: "set macsec0 tx sa 0 pn 50 off"},
		{description: "negative: a set with a key, exit 1, nothing sent", row: "macsec_txsa_set_key",
			args: "set macsec0 tx sa 0" + key1, exit: ExitUsage},
		{description: "negative: del of the active SA, the same request", row: "macsec_txsa_del_active",
			args: "del macsec0 tx sa 0"},
		{description: "positive: del an inactive SA", row: "macsec_txsa_del", args: "del macsec0 tx sa 0"},
		{description: "negative: del of a missing SA, the same request", row: "macsec_txsa_del_missing",
			args: "del macsec0 tx sa 2"},
		{description: "positive: add a receive channel by SCI", row: "macsec_rxsc_add", args: rxsc},
		{description: "positive: by port and address, make_sci's", row: "macsec_rxsc_add_port",
			args: "add macsec0 rx port 1 address 02:00:00:00:00:01"},
		{description: "negative: a channel in use, the same request", row: "macsec_rxsc_add_dup", args: rxsc},
		{description: "positive: set a channel off", row: "macsec_rxsc_set", args: "set macsec0 rx sci 0a0b0c0d0e0f0001 off"},
		{description: "positive: del a channel", row: "macsec_rxsc_del", args: "del macsec0 rx sci 0a0b0c0d0e0f0001"},
		{description: "negative: del of a missing channel, the same request", row: "macsec_rxsc_del_missing",
			args: "del macsec0 rx sci 0a0b0c0d0e0f0001"},
		{description: "positive: add a receive SA", row: "macsec_rxsa_add", args: rxsa},
		{description: "negative: a receive SA with no channel, the same request", row: "macsec_rxsa_add_no_sc",
			args: rxsa},
		{description: "positive: set a receive SA", row: "macsec_rxsa_set",
			args: "set macsec0 rx sci 0a0b0c0d0e0f0001 sa 0 pn 60 off"},
		{description: "negative: del of the active receive SA, the same request", row: "macsec_rxsa_del_active",
			args: "del macsec0 rx sci 0a0b0c0d0e0f0001 sa 0"},
		{description: "positive: del an inactive receive SA", row: "macsec_rxsa_del",
			args: "del macsec0 rx sci 0a0b0c0d0e0f0001 sa 0"},
		{description: "positive: an XPN SA, its u64 PN, salt and SSCI", row: "macsec_xpn_txsa_add",
			args: "add macsec0 tx sa 0 xpn 1 salt 0102030405060708090a0b0c ssci 1 on" + key1},
		{description: "negative: pn on an XPN SecY sends neither salt nor SSCI, the same request",
			row: "macsec_xpn_txsa_pn32", args: "add macsec0 tx sa 0 pn 1 salt 0102030405060708090a0b0c ssci 1" + key1},
		{description: "positive: offload off", row: "macsec_offload_off", args: "offload macsec0 off"},
		{description: "negative: offload mac on a device without it, the same request", row: "macsec_offload_mac",
			args: "offload macsec0 mac"},
		{description: "positive: the readback, an SA", row: "macsec_txsa_add", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, AN 3 off", row: "macsec_txsa_add_off", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, the set", row: "macsec_txsa_set", pcap: rb, out: rbOut, args: "show"},
		{description: "boundary: the readback, deleted", row: "macsec_txsa_del", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, a channel", row: "macsec_rxsc_add", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, make_sci's SCI", row: "macsec_rxsc_add_port", pcap: rb, out: rbOut,
			args: "show"},
		{description: "positive: the readback, a channel off", row: "macsec_rxsc_set", pcap: rb, out: rbOut,
			args: "show"},
		{description: "boundary: the readback, the channel deleted", row: "macsec_rxsc_del", pcap: rb, out: rbOut,
			args: "show"},
		{description: "positive: the readback, a receive SA", row: "macsec_rxsa_add", pcap: rb, out: rbOut,
			args: "show"},
		{description: "positive: the readback, a receive SA set", row: "macsec_rxsa_set", pcap: rb, out: rbOut,
			args: "show"},
		{description: "boundary: the readback, the receive SA deleted", row: "macsec_rxsa_del", pcap: rb, out: rbOut,
			args: "show"},
		{description: "corner: the readback, XPN's PN and SSCI with no space", row: "macsec_xpn_txsa_add", pcap: rb,
			out: rbOut, args: "show"},
		{description: "positive: the readback, offload", row: "macsec_offload_off", pcap: rb, out: rbOut, args: "show"},
		{description: "corner: s is show", row: "macsec_show", args: "s"},
		{description: "corner: a is add, d0 by its own index", row: "macsec_txsa_add_not_macsec",
			args: "a d0 tx sa 0 pn 1" + key1},
		{description: "corner: se is set", row: "macsec_txsa_set", args: "se macsec0 tx sa 0 off pn 50"},
		{description: "corner: d is delete, a delete reads no more", row: "macsec_txsa_del",
			args: "d macsec0 tx sa 0 pn 9 bogus"},
		{description: "corner: o is offload, its value by prefix, a word after it unread", row: "macsec_offload_off",
			args: "o macsec0 of trailing"},
		{description: "corner: the words in another order, an AN base 0, a PN in hex", row: "macsec_txsa_add_off",
			args: "add macsec0 tx sa 0x3 key 01000000000000000000000000000000 81818181818181818181818181818181 " +
				"off pn 0x64"},
		{description: "corner: an SCI with 0x, base 16", row: "macsec_rxsc_add",
			args: "add macsec0 rx sci 0x0a0b0c0d0e0f0001 on"},
		{description: "corner: address before port", row: "macsec_rxsc_add_port",
			args: "add macsec0 rx address 02:00:00:00:00:01 port 1"},
		{description: "corner: a short lladdr's parts, a port in hex", row: "macsec_rxsc_add_port",
			args: "add macsec0 rx port 0x1 address 2:0:0:0:0:1"},
		{description: "corner: an lladdr's seventh part is unread", row: "macsec_rxsc_add_port",
			args: "add macsec0 rx port 1 address 02:00:00:00:00:01:zz"},
		{description: "corner: an SCI's hex in capitals", row: "macsec_rxsc_add",
			args: "add macsec0 rx sci 0A0B0C0D0E0F0001 on"},
		{description: "positive: -j show, two SecYs, empty lists", row: "macsec_show", json: true, args: "show",
			stdout: "[" + secy0JSON + `"sa_list":[],"rx_sc":[],"offload":"off"},` + secy1JSON +
				`"sa_list":[],"rx_sc":[],"offload":"off"}]` + "\n"},
		{description: "positive: -j, an SA", row: "macsec_txsa_add", pcap: rb, json: true, args: "show",
			stdout: "[" + secy0JSON + `"sa_list":[{"an":0,"pn":1,"active":true,` +
				`"key":"01000000000000000000000000000000"}],"rx_sc":[],"offload":"off"}]` + "\n"},
		{description: "positive: -j, a channel and its SA", row: "macsec_rxsa_add", pcap: rb, json: true, args: "show",
			stdout: "[" + secy0JSON + `"sa_list":[],"rx_sc":[{"sci":"0xa0b0c0d0e0f0001","active":true,"sa_list":` +
				`[{"an":0,"pn":1,"active":true,"key":"02000000000000000000000000000000"}]}],"offload":"off"}]` + "\n"},
		{description: "corner: -j, XPN's SSCI as %#llx", row: "macsec_xpn_txsa_add", pcap: rb, json: true,
			args: "show",
			stdout: "[" + strings.Replace(secy0JSON, "GCM-AES-128", "GCM-AES-XPN-128", 1) +
				`"sa_list":[{"an":0,"pn":1,"ssci":"0x1","active":true,` +
				`"key":"01000000000000000000000000000000"}],"rx_sc":[],"offload":"off"}]` + "\n"},
		{description: "boundary: -j show with none, an empty array", row: "macsec_show_empty", json: true,
			args: "show", stdout: "[]\n"},
		{description: "negative: -j, a device that does not exist prints nothing", row: "macsec_show_unknown_dev",
			json: true, args: "show nodev0", exit: ExitUsage},
		{description: "positive: -s, the SecY's and the channel's counters", row: "macsec_show_dev", stats: true,
			args: "show macsec1",
			stdout: "5: macsec1: " + head + "    TXSC: 0102030405060002 on SA 0\n" + macsecSecYStats0 +
				macsecTxscStats0 + "    offload: off \n"},
		{description: "positive: -s, an SA's two counters", row: "macsec_txsa_add", pcap: rb, stats: true, args: "show",
			stdout: "4: macsec0: " + head + "    TXSC: 0102030405060001 on SA 0\n" + macsecSecYStats0 +
				macsecTxscStats0 + "        0: PN 1, state on, key 01000000000000000000000000000000\n" +
				macsecTxsaStats0 + "    offload: off \n"},
		{description: "positive: -s, a channel's and its SA's counters", row: "macsec_rxsa_add", pcap: rb,
			stats: true, args: "show",
			stdout: "4: macsec0: " + head + "    TXSC: 0102030405060001 on SA 0\n" + macsecSecYStats0 +
				macsecTxscStats0 + "    RXSC: 0a0b0c0d0e0f0001, state on\n" + macsecRxscStats0 +
				"        0: PN 1, state on, key 02000000000000000000000000000000\n" + macsecRxsaStats0 +
				"    offload: off \n"},
		{description: "positive: -s -j, the counters as members", row: "macsec_show_dev", stats: true, json: true,
			args:   "show macsec1",
			stdout: "[" + secy1JSON + zeroSecY + zeroTxsc + `"sa_list":[],"rx_sc":[],"offload":"off"}]` + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := genlReplay(t, tc.row, tc.pcap)
			r := macsecRunArgs(t, src, tc.json, tc.stats, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			want := tc.stdout
			if want == "" && !tc.json {
				outFile := tc.out
				if outFile == "" {
					outFile = "ip.out"
				}
				want = mustReadFile(t, filepath.Join(tdGenl, tc.row, outFile))
			}
			if r.stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, want)
			}
			ioamCheckExchanges(t, src.ReplaySource, r.writes)
		})
	}
}

// TestMacsecArgs runs do_modify's, do_offload's and do_show's parsing and
// refusals over macsec_show_empty's capture, whose lookup each one makes:
// the exit status, a fragment of the error, or stderr in full for a run
// with none, the command written, "-" for none, and its attributes, when
// given.
//
// go test ./internal/goip/ -run TestMacsecArgs
func TestMacsecArgs(t *testing.T) {
	const (
		dev  = "add macsec0 "
		key  = " key 01 81818181818181818181818181818181"
		key1 = " key 01000000000000000000000000000000 81818181818181818181818181818181"
		// ifx is IFINDEX 4.
		ifx = "08000100" + "04000000"
		// keyAttrs is key's: the 16-byte key id, 01 then zeros, and the
		// 16-byte key.
		keyAttrs = "14000500" + "01000000000000000000000000000000" + "14000400" + "81818181818181818181818181818181"
		// sci is RXSC_CONFIG with SCI 0a0b0c0d0e0f0001 alone.
		sci = "10000200" + "0c000100" + "0a0b0c0d0e0f0001"
	)
	tests := []struct {
		description string
		args        string
		exit        int
		text        string // in the error, or all of stderr when there is none
		cmd         string // the written command, "-" for none
		attrs       string // its attributes in hex, when not empty
	}{
		{"positive: an SA with no state sends no ACTIVE", dev + "tx sa 2 pn 7" + key, 0, "", "4",
			ifx + "3c000300" + "05000100" + "02000000" + "08000300" + "07000000" + keyAttrs},
		{"corner: a short key id is zero-filled to 16 bytes", dev + "tx sa 0 pn 1" + key, 0, "", "4",
			ifx + "3c000300" + "05000100" + "00000000" + "08000300" + "01000000" + keyAttrs},
		{"corner: a second key keeps the first key id's bytes past the second's", dev + "tx sa 0 pn 1 key 0102 81 key 03 " +
			"82", 0, "", "4", ifx + "30000300" + "05000100" + "00000000" + "08000300" + "01000000" + "14000500" +
			"03020000000000000000000000000000" + "05000400" + "82000000"},
		{"corner: a hex pair strtoul reads, +f and -0", dev + "tx sa 0 pn 1 key +f-0 81", 0, "", "4",
			ifx + "30000300" + "05000100" + "00000000" + "08000300" + "01000000" + "14000500" +
				"0f000000000000000000000000000000" + "05000400" + "81000000"},
		{"negative: a key id of quote characters is not hex", dev + "tx sa 0 pn 1 key \"\" 81", ExitMinusOne,
			"expected key id", "-", ""},
		{"boundary: a key id past 16 bytes is cut at 16, the rest unread", dev +
			"tx sa 0 pn 1 key 0102030405060708090a0b0c0d0e0f10zz 81", 0, "", "4", ifx + "30000300" + "05000100" +
			"00000000" + "08000300" + "01000000" + "14000500" + "0102030405060708090a0b0c0d0e0f10" + "05000400" +
			"81000000"},
		{"corner: a key id and key in capitals", dev + "tx sa 0 pn 1 key AB CD", 0, "", "4",
			ifx + "30000300" + "05000100" + "00000000" + "08000300" + "01000000" + "14000500" +
				"ab000000000000000000000000000000" + "05000400" + "cd000000"},
		{"positive: xpn, a u64 PN", dev + "tx sa 0 xpn 4294967296" + key, 0, "", "4",
			ifx + "40000300" + "05000100" + "00000000" + "0c000300" + "0000000001000000" + keyAttrs},
		{"corner: salt and ssci without xpn are not sent", dev + "tx sa 0 pn 1 salt 01 ssci 2" + key, 0, "", "4",
			ifx + "3c000300" + "05000100" + "00000000" + "08000300" + "01000000" + keyAttrs},
		{"corner: a short salt is zero-filled, ssci big-endian base 16", dev + "tx sa 0 xpn 1 salt 01 ssci 1a" + key,
			0, "", "4", ifx + "58000300" + "05000100" + "00000000" + "0c000300" + "0100000000000000" + "10000900" +
				"010000000000000000000000" + "08000800" + "0000001a" + keyAttrs},
		{"positive: a set with only the state", "set macsec0 tx sa 1 on", 0, "", "6",
			ifx + "14000300" + "05000100" + "01000000" + "05000200" + "01000000"},
		{"positive: del of a receive SA, no state", "del macsec0 rx sci 0a0b0c0d0e0f0001 sa 1 on", 0, "", "8",
			ifx + sci + "0c000300" + "05000100" + "01000000"},
		{"positive: a channel with no state sends no ACTIVE", "add macsec0 rx sci 0a0b0c0d0e0f0001", 0, "", "1",
			ifx + sci},
		{"positive: del of a channel reads no word after the SCI", "del macsec0 rx sci 0a0b0c0d0e0f0001 on bogus", 0,
			"", "2", ifx + sci},
		{"positive: offload phy", "offload macsec0 phy", 0, "", "10",
			ifx + "0c000900" + "05000100" + "01000000"},
		{"corner: offload by prefix warns", "offload macsec0 m", 0,
			"WARNING: 'm' matches 'mac' by prefix.\n" +
				"Matching by prefix is deprecated in this context, please use the full string.\n", "10",
			ifx + "0c000900" + "05000100" + "02000000"},
		{"negative: an offload it does not know", "offload macsec0 bogus", ExitMinusOne,
			`must be one of "off", "phy", "mac", not "bogus"`, "-", ""},
		{"negative: offload with no type", "offload macsec0", ExitMinusOne, "Usage: ip macsec", "-", ""},
		{"negative: offload with no device", "offload", ExitMinusOne, "Usage: ip macsec", "-", ""},
		{"negative: offload of a device that does not exist, exit 1", "offload nodev0 off", ExitUsage,
			`Device "nodev0" does not exist`, "-", ""},
		{"negative: a device that does not exist, ahead of the words", "add nodev0 tx sa 9", ExitUsage,
			`Device "nodev0" does not exist`, "-", ""},
		{"negative: add with no device", "add", ExitMinusOne, "Usage: ip macsec", "-", ""},
		{"negative: add with only a device", dev, ExitMinusOne, "Usage: ip macsec", "-", ""},
		{"negative: neither tx nor rx", dev + "both", ExitMinusOne, "Usage: ip macsec", "-", ""},
		{"negative: tx with no sa", dev + "tx", ExitMinusOne, "Usage: ip macsec", "-", ""},
		{"negative: tx with another word", dev + "tx an 0", ExitMinusOne, "Usage: ip macsec", "-", ""},
		{"negative: sa with no AN", dev + "tx sa", ExitMinusOne, "not complete", "-", ""},
		{"negative: an AN not a number", dev + "tx sa x", ExitMinusOne, "expected an { 0..3 }", "-", ""},
		{"negative: a PN not a number", dev + "tx sa 0 pn x", ExitMinusOne, "expected pn", "-", ""},
		{"boundary: a PN of 0", dev + "tx sa 0 pn 0", ExitMinusOne, "expected pn != 0", "-", ""},
		{"boundary: a PN of 2^32", dev + "tx sa 0 pn 4294967296", ExitMinusOne, "expected pn", "-", ""},
		{"boundary: an XPN of 0", dev + "tx sa 0 xpn 0", ExitMinusOne, "expected pn != 0", "-", ""},
		{"negative: pn twice", dev + "tx sa 0 pn 1 pn 2", ExitMinusOne, `either "pn" is duplicate`, "-", ""},
		{"negative: xpn after pn", dev + "tx sa 0 pn 1 xpn 2", ExitMinusOne, `either "xpn" is duplicate`, "-", ""},
		{"negative: on after off", dev + "tx sa 0 off on", ExitMinusOne, `either "on/off" is duplicate, or "on"`,
			"-", ""},
		{"negative: salt twice", dev + "tx sa 0 salt 01 salt 02", ExitMinusOne, `either "salt" is duplicate`, "-", ""},
		{"negative: ssci twice", dev + "tx sa 0 ssci 1 ssci 2", ExitMinusOne, `either "ssci" is duplicate`, "-", ""},
		{"negative: an odd-length salt", dev + "tx sa 0 salt 012", ExitMinusOne, "expected salt", "-", ""},
		{"negative: an ssci not hex", dev + "tx sa 0 ssci xyz", ExitMinusOne, "expected ssci", "-", ""},
		{"negative: a key id not hex", dev + "tx sa 0 key zz 81", ExitMinusOne, "expected key id", "-", ""},
		{"negative: a key of odd length", dev + "tx sa 0 key 01 818", ExitMinusOne, "expected key", "-", ""},
		{"negative: a key with no value", dev + "tx sa 0 key 01", ExitMinusOne, "not complete", "-", ""},
		{"negative: a word it does not know", dev + "tx sa 0 bogus", ExitMinusOne, `unknown command "bogus"?`,
			"-", ""},
		{"negative: no key is check_sa_args', exit 1", dev + "tx sa 0 pn 1", ExitUsage,
			"cannot create SA without key", "-", ""},
		{"corner: no key is checked ahead of no PN", dev + "tx sa 0", ExitUsage, "cannot create SA without key",
			"-", ""},
		{"negative: a set with a key, exit 1", "set macsec0 tx sa 0" + key1, ExitUsage, "cannot change key on SA",
			"-", ""},
		{"negative: rx with nothing after it", dev + "rx", ExitMinusOne, "expected sci", "-", ""},
		{"negative: rx with port alone", dev + "rx port 1", ExitMinusOne, "expected sci", "-", ""},
		{"negative: rx with another word", dev + "rx bogus", ExitMinusOne, "expected sci, port, or address", "-", ""},
		{"negative: sci after port", dev + "rx port 1 sci 1", ExitMinusOne, "expected address", "-", ""},
		{"negative: sci after address", dev + "rx address 02:00:00:00:00:01 sci 1", ExitMinusOne, "expected port",
			"-", ""},
		{"boundary: port 0", dev + "rx port 0 address 02:00:00:00:00:01", ExitMinusOne, "expected port != 0", "-", ""},
		{"boundary: port 65536", dev + "rx port 65536", ExitMinusOne, "expected port", "-", ""},
		{"negative: an lladdr part above 255", dev + "rx port 1 address 100:0", ExitMinusOne, "expected lladdr",
			"-", ""},
		{"negative: an sci not hex", dev + "rx sci 0g", ExitMinusOne, "expected sci", "-", ""},
		{"negative: a channel's word it does not know", dev + "rx sci 1 bogus", ExitMinusOne,
			`unknown command "bogus"?`, "-", ""},
		{"negative: a channel's on twice", dev + "rx sci 1 on on", ExitMinusOne, `either "on/off" is duplicate`,
			"-", ""},
		{"negative: show of a device with a word after it", "show macsec0 bogus", ExitMinusOne, "Usage: ip macsec",
			"-", ""},
		{"negative: a verb it does not know", "flush", ExitMinusOne, `Command "flush" is unknown`, "-", ""},
		{"negative: help", "help", ExitMinusOne, "Usage: ip macsec", "-", ""},
		{"negative: no verb is usage", "", ExitMinusOne, "Usage: ip macsec", "-", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := macsecRunArgs(t, genlReplay(t, "macsec_show_empty", ""), false, false, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if r.err == nil && r.stderr != tc.text {
				t.Errorf("stderr %q, want %q", r.stderr, tc.text)
			}
			if r.err != nil && !strings.Contains(r.err.Error(), tc.text) {
				t.Errorf("%q does not hold %q", r.err.Error(), tc.text)
			}
			got := "-"
			if len(r.writes) == 1 {
				got = fmt.Sprint(r.writes[0].Body[0])
			}
			if got != tc.cmd || len(r.writes) > 1 {
				t.Fatalf("command %q of %d writes, want %q", got, len(r.writes), tc.cmd)
			}
			if tc.attrs != "" {
				if a := hexOf(r.writes[0].Body[4:]); a != tc.attrs {
					t.Errorf("attrs\n got %s\nwant %s", a, tc.attrs)
				}
			}
		})
	}
}

// macsecDumpBody is a GET_TXSC answer for ifindex idx whose SecY nest
// secy builds, with empty lists and statistics nests, and whatever put
// adds after them.
func macsecDumpBody(cmd uint8, idx uint32, secy func(b *xtcpnl.AttrBuilder), put func(b *xtcpnl.AttrBuilder)) []byte {
	b := xtcpnl.NewAttrBuilder(make([]byte, 512))
	nest := func(typ uint16, fill func()) {
		m, err := b.BeginNest(typ)
		mustNil(err)
		fill()
		mustNil(b.EndNest(m))
	}
	mustNil(b.PutU32(xtcpnl.MacsecAttrIfindex, idx))
	nest(xtcpnl.MacsecAttrSecY, func() { secy(&b) })
	for _, typ := range []uint16{xtcpnl.MacsecAttrTxsaList, xtcpnl.MacsecAttrRxscList, xtcpnl.MacsecAttrTxscStats,
		xtcpnl.MacsecAttrSecYStats} {
		nest(typ, func() {})
	}
	put(&b)
	return append([]byte{cmd, xtcpnl.MacsecGenlVersion, 0, 0}, b.Bytes()...)
}

// macsecSecY puts a complete SecY: SCI 0102030405060001, the suite cid,
// encoding SA enc and every flag but protect off.
func macsecSecY(cid uint64, enc uint8) func(b *xtcpnl.AttrBuilder) {
	return func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutBytes(xtcpnl.MacsecSecYAttrSCI, []byte{1, 2, 3, 4, 5, 6, 0, 1}))
		mustNil(b.PutU8(xtcpnl.MacsecSecYAttrEncodingSA, enc))
		mustNil(b.PutBytes(xtcpnl.MacsecSecYAttrCipherSuite, binary.LittleEndian.AppendUint64(nil, cid)))
		for _, a := range []uint16{xtcpnl.MacsecSecYAttrICVLen, xtcpnl.MacsecSecYAttrProtect,
			xtcpnl.MacsecSecYAttrReplay, xtcpnl.MacsecSecYAttrOper, xtcpnl.MacsecSecYAttrValidate,
			xtcpnl.MacsecSecYAttrEncrypt, xtcpnl.MacsecSecYAttrIncSCI, xtcpnl.MacsecSecYAttrES,
			xtcpnl.MacsecSecYAttrSCB} {
			v := uint8(0)
			switch a {
			case xtcpnl.MacsecSecYAttrICVLen:
				v = 16
			case xtcpnl.MacsecSecYAttrProtect:
				v = 1
			}
			mustNil(b.PutU8(a, v))
		}
	}
}

// TestMacsecShow runs show over a fake family for what the captures do not
// hold: a refused dump, messages process skips or refuses, another
// device's filtered out, and the encoding SA's flags.
//
// go test ./internal/goip/ -run TestMacsecShow
func TestMacsecShow(t *testing.T) {
	none := func(*xtcpnl.AttrBuilder) {}
	full := macsecSecY(xtcpnl.MacsecCipherGCMAES128, 0)
	eth7 := macsecDumpBody(xtcpnl.MacsecCmdGetTxsc, 7, full, none)
	const eth7Text = "7: eth7: protect on validate disabled sc off sa off encrypt off send_sci off end_station off " +
		"scb off replay off \n    cipher suite: GCM-AES-128, using ICV length 16\n    TXSC: 0102030405060001 on SA 0\n"
	tests := []struct {
		description string
		args        string
		dump        [][]byte
		dumpErr     error
		json        bool
		stdout      string
		stderr      string
		exit        int
	}{
		{description: "positive: one SecY, no offload nest, no offload line", args: "show",
			dump: [][]byte{eth7}, stdout: eth7Text},
		{description: "negative: a refused dump prints nothing, exit 1", args: "show", dumpErr: unix.EPERM,
			exit: ExitUsage},
		{description: "negative: under -j it closes the empty array", args: "show", dumpErr: unix.EPERM, json: true,
			stdout: "[]\n", exit: ExitUsage},
		{description: "corner: another command is skipped", args: "show",
			dump:   [][]byte{macsecDumpBody(xtcpnl.MacsecCmdAddRxsc, 7, none, none), eth7},
			stdout: eth7Text},
		{description: "negative: a SecY missing an attribute is incomplete, exit 1, after the ones before",
			args: "show", dump: [][]byte{eth7, macsecDumpBody(xtcpnl.MacsecCmdGetTxsc, 7, none, none)},
			stdout: eth7Text, stderr: "incomplete dump message\n", exit: ExitUsage},
		{description: "negative: an incomplete SecY of another device still ends the dump", args: "show if9",
			dump:   [][]byte{macsecDumpBody(xtcpnl.MacsecCmdGetTxsc, 7, none, none)},
			stderr: "incomplete dump message\n", exit: ExitUsage},
		{description: "negative: a body shorter than its genlmsghdr ends the dump", args: "show",
			dump: [][]byte{{xtcpnl.MacsecCmdGetTxsc}}, exit: ExitUsage},
		{description: "corner: another device's SecY is filtered out", args: "show if9", dump: [][]byte{eth7}},
		{description: "corner: encoding SA 2 prints sc and sa on, XPN-256's name", args: "show",
			dump: [][]byte{macsecDumpBody(xtcpnl.MacsecCmdGetTxsc, 7,
				macsecSecY(xtcpnl.MacsecCipherGCMAESXPN256, 2), none)},
			stdout: "7: eth7: protect on validate disabled sc on sa on encrypt off send_sci off end_station off " +
				"scb off replay off \n    cipher suite: GCM-AES-XPN-256, using ICV length 16\n" +
				"    TXSC: 0102030405060001 on SA 2\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &genlFake{name: xtcpnl.MacsecGenlName, dump: tc.dump, dumpErr: tc.dumpErr}
			r := macsecRunArgs(t, f, tc.json, false, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if r.stdout != tc.stdout {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, tc.stdout)
			}
			if r.stderr != tc.stderr {
				t.Errorf("stderr %q, want %q", r.stderr, tc.stderr)
			}
		})
	}
}

// TestMacsecExitCodes maps each macsec error to ip's exit status.
//
// go test ./internal/goip/ -run TestMacsecExitCodes
func TestMacsecExitCodes(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    int
	}{
		{"positive: usage is exit(-1)", macsecUsage(), ExitMinusOne},
		{"positive: invarg wrapped as usage is 255, not ErrInvalidArgument's 1",
			macsecInvarg(invargErr("expected pn", "x")), ExitMinusOne},
		{"positive: a missing device is -1, exit 1", fmt.Errorf("%w: Device", ErrMacsecRefused), ExitUsage},
		{"positive: a terminated dump is exit(1)", fmt.Errorf("%w: x", ErrMacsecDumpTerminated), ExitUsage},
		{"corner: an unwrapped error is 2", fmt.Errorf("other"), ExitFailure},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := exitCodeOf(tc.err); got != tc.expected {
				t.Errorf("exit %d, want %d", got, tc.expected)
			}
		})
	}
}
