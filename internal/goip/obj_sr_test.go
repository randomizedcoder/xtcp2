package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// srNoTTY makes getpass find no terminal, as under setsid.
func srNoTTY(t *testing.T) {
	t.Helper()
	saved := srOpenTTY
	srOpenTTY = func() (*os.File, error) { return nil, unix.ENXIO }
	t.Cleanup(func() { srOpenTTY = saved })
}

// srRun runs `goip [-j] sr ARGS` over src with stdin, with GOIP_WRITE_OUT
// a fresh file and no terminal, and returns stdout, stderr, the file's
// messages and the error.
func srRun(t *testing.T, src Source, json bool, stdin, args string) (string, string, []nlparity.Msg, error) {
	t.Helper()
	srNoTTY(t)
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	var stdout, stderr bytes.Buffer
	c := newRunCtx(strings.NewReader(stdin), &stdout, &stderr)
	c.src, c.json = src, json
	err := runSr(c, strings.Fields(args))
	var writes []nlparity.Msg
	if _, serr := os.Stat(out); serr == nil {
		writes = genlWrites(t, out, xtcpnl.Seg6GenlName)
	}
	return stdout.String(), stderr.String(), writes, err
}

// TestSrMatchesIp replays the 11 C2-T02 sr captures, and other spellings
// over the same captures. A replay answers only a request whose bytes are
// ip's, so a run that succeeds sent ip's GETFAMILY and reads byte for
// byte, seq aside; then every read exchange must have been replayed, no
// write exchange, the write goip made must be the capture's, and stdout
// must be ip.out, or the JSON given.
//
// go test ./internal/goip/ -run TestSrMatchesIp
func TestSrMatchesIp(t *testing.T) {
	const prompt = srHmacPromptCst
	tests := []struct {
		description string
		row         string
		args        string
		stdin       string
		json        bool
		stdout      string // under -j; otherwise ip.out
		stderr      string
	}{
		{description: "boundary: hmac show with no key", row: "sr_hmac_show_empty", args: "hmac show"},
		{description: "positive: hmac show, two keys", row: "sr_hmac_show", args: "hmac show"},
		{description: "boundary: a 64-byte secret prints 63, with the warning", row: "sr_hmac_show_64", args: "hmac show",
			stderr: "HMAC secret length 64 > 63, truncated\n"},
		{description: "positive: tunsrc show, the namespace's ::", row: "sr_tunsrc_show", args: "tunsrc show"},
		{description: "positive: hmac set, sha256", row: "sr_hmac_set", args: "hmac set 1 sha256", stdin: "s3cret\n", stderr: prompt},
		{description: "positive: hmac set, sha1", row: "sr_hmac_set_sha1", args: "hmac set 2 sha1", stdin: "0ther\n", stderr: prompt},
		{description: "positive: a set over a key is the same SETHMAC", row: "sr_hmac_replace", args: "hmac set 1 sha1", stdin: "n3w\n", stderr: prompt},
		{description: "positive: a blank secret, the delete: SECRETLEN 0 and no SECRET", row: "sr_hmac_delete", args: "hmac set 1 sha256",
			stdin: "\n", stderr: prompt},
		{description: "negative: a delete the kernel would refuse is the same request", row: "sr_hmac_delete_missing", args: "hmac set 9 sha256",
			stdin: "\n", stderr: prompt},
		{description: "negative: a 65-byte secret the kernel would refuse is sent whole", row: "sr_hmac_set_long", args: "hmac set 3 sha256",
			stdin: strings.Repeat("x", 65) + "\n", stderr: prompt},
		{description: "positive: tunsrc set", row: "sr_tunsrc_set", args: "tunsrc set 2001:db8::1"},
		{description: "corner: hm and s spell hmac show", row: "sr_hmac_show", args: "hm s"},
		{description: "corner: t and sh spell tunsrc show", row: "sr_tunsrc_show", args: "t sh"},
		{description: "corner: se is set, and the key id is read base 0", row: "sr_hmac_set", args: "hmac se 0x1 sha256", stdin: "s3cret\n", stderr: prompt},
		{description: "corner: a secret with no newline is read to EOF", row: "sr_hmac_set", args: "hmac set 1 sha256", stdin: "s3cret", stderr: prompt},
		{description: "corner: a secret ends at its first NUL, as strlen ends it", row: "sr_hmac_set", args: "hmac set 1 sha256",
			stdin: "s3cret\x00junk\n", stderr: prompt},
		{description: "corner: only the first line is the secret", row: "sr_hmac_set", args: "hmac set 1 sha256", stdin: "s3cret\nmore\n", stderr: prompt},
		{description: "corner: no stdin at all is a blank secret", row: "sr_hmac_delete", args: "hmac set 1 sha256", stderr: prompt},
		{description: "corner: the address spelled long", row: "sr_tunsrc_set", args: "tunsrc set 2001:0db8:0:0::1"},
		{description: "positive: -j hmac show, an array of the two keys", row: "sr_hmac_show", args: "hmac show", json: true,
			stdout: `[{"hmac":1,"algo":"sha256","secret":"s3cret"},{"hmac":2,"algo":"sha1","secret":"0ther"}]` + "\n"},
		{description: "boundary: -j hmac show with no key, an empty array", row: "sr_hmac_show_empty", args: "hmac show", json: true, stdout: "[]\n"},
		{description: "positive: -j tunsrc show", row: "sr_tunsrc_show", args: "tunsrc show", json: true, stdout: `[{"tunsrc":"::"}]` + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(tdGenl, tc.row)
			src, err := OpenReplay(filepath.Join(dir, "ip.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, writes, err := srRun(t, src, tc.json, tc.stdin, tc.args)
			if err != nil {
				t.Fatalf("runSr: %v", err)
			}
			want := tc.stdout
			if !tc.json {
				b, err := os.ReadFile(filepath.Join(dir, "ip.out"))
				if err != nil {
					t.Fatal(err)
				}
				want = string(b)
			}
			if stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", stdout, want)
			}
			if stderr != tc.stderr {
				t.Errorf("stderr %q, want %q", stderr, tc.stderr)
			}
			var ipWrites [][]byte
			for i, txn := range src.gseg.Txns {
				write := nlparity.IsWrite(txn.Request)
				if write {
					ipWrites = append(ipWrites, canonMsg(txn.Request))
				}
				if src.gused[i] == write {
					t.Errorf("exchange %d (%s): replayed %v, a write %v", i, nlparity.MsgName(txn.Request), src.gused[i], write)
				}
			}
			if len(writes) != len(ipWrites) {
				t.Fatalf("goip wrote %d requests, ip sent %d", len(writes), len(ipWrites))
			}
			for i := range writes {
				if got := canonMsg(writes[i]); !bytes.Equal(got, ipWrites[i]) {
					t.Errorf("write %d\n got %x\nwant %x", i, got, ipWrites[i])
				}
			}
		})
	}
}

// srFake is a SEG6 family: the lookup answers id 30, a dump its bodies or
// its error, and any other talk its answer.
type srFake struct {
	dump    [][]byte
	dumpErr error
	typ     uint16
	body    []byte
	talkErr error
	sent    int
}

const srFakeIDCst = 30

func (f *srFake) Dump([]byte, uint16) ([][]byte, error) { return nil, nil }

func (f *srFake) GenlDump([]byte) ([][]byte, error) {
	f.sent++
	return f.dump, f.dumpErr
}

func (f *srFake) GenlTalk(req []byte) (uint16, []byte, error) {
	f.sent++
	if binary.LittleEndian.Uint16(req[4:6]) == unix.GENL_ID_CTRL {
		return unix.GENL_ID_CTRL, newFamilyBody(unix.CTRL_CMD_NEWFAMILY, srFakeIDCst, xtcpnl.Seg6GenlName), nil
	}
	return f.typ, f.body, f.talkErr
}

// srBody is a SEG6 reply body: a command, then attributes built by put.
func srBody(cmd uint8, put func(b *xtcpnl.AttrBuilder)) []byte {
	b := xtcpnl.NewAttrBuilder(make([]byte, 256))
	if put != nil {
		put(&b)
	}
	return append([]byte{cmd, xtcpnl.Seg6GenlVersion, 0, 0}, b.Bytes()...)
}

// srKey is a DUMPHMAC reply.
func srKey(id uint32, alg uint8, slen uint8, secret []byte) []byte {
	return srBody(xtcpnl.Seg6CmdDumpHmac, func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutU32(xtcpnl.Seg6AttrHmacKeyID, id))
		mustNil(b.PutBytes(xtcpnl.Seg6AttrSecret, secret))
		mustNil(b.PutU8(xtcpnl.Seg6AttrSecretLen, slen))
		mustNil(b.PutU8(xtcpnl.Seg6AttrAlgID, alg))
	})
}

// TestSrShow runs the two reads over a fake family for what the captures
// do not hold: the JSON escapes, a refused dump and talk, a reply of
// another type or command, and a reply that does not decode.
//
// go test ./internal/goip/ -run TestSrShow
func TestSrShow(t *testing.T) {
	tunsrc := srBody(xtcpnl.Seg6CmdGetTunsrc, func(b *xtcpnl.AttrBuilder) {
		a := [16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 1}
		mustNil(b.PutBytes(xtcpnl.Seg6AttrDst, a[:]))
	})
	tests := []struct {
		description string
		src         *srFake
		json        bool
		args        string
		stdout      string
		stderr      string
		exit        int // 0: success
	}{
		{description: "positive: a key whose algorithm is neither, and whose secret needs escapes, under -j",
			src: &srFake{dump: [][]byte{srKey(7, 3, 6, []byte("a\"<&\t\x01"))}}, json: true, args: "hmac show",
			stdout: `[{"hmac":7,"algo":"<unknown>","secret":"a\"<&\t\u0001"}]` + "\n"},
		{description: "positive: the same key's line, the bytes as they are",
			src: &srFake{dump: [][]byte{srKey(7, 3, 6, []byte("a\"<&\t\x01"))}}, args: "hmac show",
			stdout: "hmac 7 algo <unknown> secret \"a\"<&\t\x01\"\n"},
		{description: "boundary: SECRETLEN longer than SECRET reads only SECRET",
			src: &srFake{dump: [][]byte{srKey(1, 2, 9, []byte("abc"))}}, args: "hmac show",
			stdout: "hmac 1 algo sha256 secret \"abc\"\n"},
		{description: "boundary: SECRETLEN shorter than SECRET cuts it",
			src: &srFake{dump: [][]byte{srKey(1, 2, 2, []byte("abc"))}}, args: "hmac show",
			stdout: "hmac 1 algo sha256 secret \"ab\"\n"},
		{description: "corner: a NUL ends the printed secret",
			src: &srFake{dump: [][]byte{srKey(1, 1, 3, []byte("a\x00c"))}}, args: "hmac show",
			stdout: "hmac 1 algo sha1 secret \"a\"\n"},
		{description: "corner: a reply of another command is an empty object under -j",
			src: &srFake{dump: [][]byte{srBody(xtcpnl.Seg6CmdGetTunsrc, nil)}}, json: true, args: "hmac show", stdout: "[{}]\n"},
		{description: "negative: a refused dump under -j leaves the array open, exit 1",
			src: &srFake{dumpErr: unix.EPERM}, json: true, args: "hmac show", stdout: "[", exit: ExitUsage},
		{description: "negative: a refused dump prints nothing, exit 1",
			src: &srFake{dumpErr: unix.EPERM}, args: "hmac show", exit: ExitUsage},
		{description: "negative: a reply that does not decode ends the dump, exit 1",
			src: &srFake{dump: [][]byte{{xtcpnl.Seg6CmdDumpHmac}}}, args: "hmac show", exit: ExitUsage},
		{description: "positive: tunsrc show of a set source", src: &srFake{typ: srFakeIDCst, body: tunsrc},
			args: "tunsrc show", stdout: "tunsrc addr 2001:db8::1\n"},
		{description: "corner: a reply with no DST is ::", src: &srFake{typ: srFakeIDCst, body: srBody(xtcpnl.Seg6CmdGetTunsrc, nil)},
			args: "tunsrc show", stdout: "tunsrc addr ::\n"},
		{description: "corner: a tunsrc reply of another command prints nothing, {} under -j",
			src: &srFake{typ: srFakeIDCst, body: srBody(xtcpnl.Seg6CmdDumpHmac, nil)}, json: true, args: "tunsrc show", stdout: "[{}]\n"},
		{description: "negative: a refused talk is the kernel's errno, exit 2",
			src: &srFake{talkErr: unix.EPERM}, args: "tunsrc show", exit: ExitFailure},
		{description: "negative: an answer of another type is Error parsing reply, exit 1",
			src: &srFake{typ: srFakeIDCst + 1, body: tunsrc}, args: "tunsrc show", exit: ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			stdout, stderr, _, err := srRun(t, tc.src, tc.json, "", tc.args)
			code := 0
			if err != nil {
				code = exitCodeOf(err)
			}
			if code != tc.exit {
				t.Errorf("exit %d (%v), want %d", code, err, tc.exit)
			}
			if stdout != tc.stdout {
				t.Errorf("stdout %q, want %q", stdout, tc.stdout)
			}
			if stderr != tc.stderr {
				t.Errorf("stderr %q, want %q", stderr, tc.stderr)
			}
		})
	}
}

// TestSrArgs checks do_seg6's refusals, which come before the family
// lookup, so the source sees no request; a set's refusals come before the
// prompt too.
//
// go test ./internal/goip/ -run TestSrArgs
func TestSrArgs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    string // a fragment of the error
		exit        int
	}{
		{description: "negative: no verb", args: "", expected: "sr help", exit: ExitUsage},
		{description: "negative: h is help, not hmac", args: "h show", expected: "sr help", exit: ExitUsage},
		{description: "negative: an unknown object", args: "bogus", expected: `argument "bogus" is wrong: unknown`, exit: ExitMinusOne},
		{description: "negative: an unknown hmac verb", args: "hmac bogus", expected: `argument "bogus" is wrong: unknown`, exit: ExitMinusOne},
		{description: "negative: an unknown tunsrc verb", args: "tunsrc del", expected: `argument "del" is wrong: unknown`, exit: ExitMinusOne},
		{description: "negative: key id 0", args: "hmac set 0 sha1", expected: "hmac KEYID value is invalid", exit: ExitMinusOne},
		{description: "negative: a key id that is not a number", args: "hmac set one sha1", expected: "hmac KEYID value is invalid", exit: ExitMinusOne},
		{description: "boundary: key id 2^32 is out of range", args: "hmac set 4294967296 sha1", expected: "hmac KEYID value is invalid", exit: ExitMinusOne},
		{description: "negative: an algorithm by another name", args: "hmac set 1 SHA1", expected: "hmac ALGO value is invalid", exit: ExitMinusOne},
		{description: "negative: an IPv4 tunnel source", args: "tunsrc set 192.0.2.1", expected: `inet6 address is expected rather than "192.0.2.1"`, exit: ExitUsage},
		{description: "boundary: hmac with no verb", args: "hmac", expected: "Command line is not complete", exit: ExitMinusOne},
		{description: "boundary: set with no key id", args: "hmac set", expected: "Command line is not complete", exit: ExitMinusOne},
		{description: "boundary: set with no algorithm", args: "hmac set 1", expected: "Command line is not complete", exit: ExitMinusOne},
		{description: "boundary: tunsrc set with no address", args: "tunsrc set", expected: "Command line is not complete", exit: ExitMinusOne},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &srFake{}
			_, stderr, _, err := srRun(t, f, false, "s3cret\n", tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("err %v, want one containing %q", err, tc.expected)
			}
			if code := exitCodeOf(err); code != tc.exit {
				t.Errorf("exit %d, want %d", code, tc.exit)
			}
			if f.sent != 0 {
				t.Errorf("sent %d requests, want none", f.sent)
			}
			if stderr != "" {
				t.Errorf("stderr %q, want nothing: no prompt", stderr)
			}
		})
	}
}

// TestSrWriteAttrs checks the SETHMAC and SET_TUNSRC attributes for what
// the captures do not hold: SECRETLEN as a u8, and a secret past
// SEG6_REQUEST's buffer, which addattr_l refuses and ip sends without.
//
// go test ./internal/goip/ -run TestSrWriteAttrs
func TestSrWriteAttrs(t *testing.T) {
	tests := []struct {
		description string
		req         srReq
		secretLen   uint8
		secret      int // the SECRET attribute's length, -1 when absent
		stderr      string
	}{
		{description: "positive: a six-byte secret", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2, pass: []byte("s3cret")},
			secretLen: 6, secret: 6},
		{description: "positive: a blank secret has no SECRET", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2},
			secretLen: 0, secret: -1},
		{description: "boundary: 976 bytes, the most the buffer holds", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2,
			pass: bytes.Repeat([]byte{'x'}, 976)}, secretLen: 976 % 256, secret: 976},
		{description: "boundary: 977 bytes is left out, with addattr_l's message", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2,
			pass: bytes.Repeat([]byte{'x'}, 977)}, secretLen: 977 % 256, secret: -1,
			stderr: "addattr_l ERROR: message exceeded bound of 1024\n"},
		{description: "corner: 256 bytes is a SECRETLEN of 0", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2,
			pass: bytes.Repeat([]byte{'x'}, 256)}, secretLen: 0, secret: 256},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var stderr bytes.Buffer
			c := newRunCtx(nil, io.Discard, &stderr)
			attrs, err := srWriteAttrs(c, &tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if got := stderr.String(); got != tc.stderr {
				t.Errorf("stderr %q, want %q", got, tc.stderr)
			}
			if n := len(attrs) + srReqHdrCst; n > srReqBufCst {
				t.Errorf("request %d bytes, past %d", n, srReqBufCst)
			}
			m, err := xtcpnl.ParseSeg6(append([]byte{tc.req.cmd, xtcpnl.Seg6GenlVersion, 0, 0}, attrs...))
			if err != nil {
				t.Fatal(err)
			}
			if m.SecretLen == nil || *m.SecretLen != tc.secretLen {
				t.Errorf("SECRETLEN %v, want %d", m.SecretLen, tc.secretLen)
			}
			got := -1
			if m.Secret != nil {
				got = len(m.Secret)
			}
			if got != tc.secret {
				t.Errorf("SECRET %d bytes, want %d", got, tc.secret)
			}
		})
	}
}

// TestSrGetpass checks getpass's two ends: no terminal, stdin and stderr;
// and a terminal that is not a tty, which is prompted and read, with its
// echo left alone and so no newline written.
//
// go test ./internal/goip/ -run TestSrGetpass
func TestSrGetpass(t *testing.T) {
	tests := []struct {
		description string
		tty         bool
		input       string
		expected    string
		prompts     string // on stderr, or on the terminal when tty
	}{
		{description: "positive: no terminal: the prompt on stderr, the line from stdin", input: "s3cret\n", expected: "s3cret",
			prompts: srHmacPromptCst},
		{description: "positive: a terminal: the prompt on it and the line from it", tty: true, input: "s3cret\n", expected: "s3cret",
			prompts: srHmacPromptCst},
		{description: "boundary: an empty input is a blank secret", input: "", expected: "", prompts: srHmacPromptCst},
		{description: "corner: a carriage return is kept, only the newline goes", input: "s3cret\r\n", expected: "s3cret\r",
			prompts: srHmacPromptCst},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var stderr bytes.Buffer
			saved := srOpenTTY
			t.Cleanup(func() { srOpenTTY = saved })
			stdin := tc.input
			var peer *os.File
			if tc.tty {
				stdin = "from stdin\n"
				fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
				if err != nil {
					t.Fatal(err)
				}
				peer = os.NewFile(uintptr(fds[1]), "peer")
				t.Cleanup(func() { mustNil(peer.Close()) })
				if _, err := peer.WriteString(tc.input); err != nil {
					t.Fatal(err)
				}
				srOpenTTY = func() (*os.File, error) { return os.NewFile(uintptr(fds[0]), "tty"), nil }
			} else {
				srOpenTTY = func() (*os.File, error) { return nil, errors.New("no tty") }
			}
			c := newRunCtx(strings.NewReader(stdin), io.Discard, &stderr)
			got, err := srGetpass(c)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expected {
				t.Errorf("secret %q, want %q", got, tc.expected)
			}
			prompts := stderr.String()
			if tc.tty {
				if prompts != "" {
					t.Errorf("stderr %q, want nothing with a terminal", prompts)
				}
				b, err := io.ReadAll(peer)
				if err != nil {
					t.Fatal(err)
				}
				prompts = string(b)
			}
			if prompts != tc.prompts {
				t.Errorf("prompt %q, want %q", prompts, tc.prompts)
			}
		})
	}
}

// srOpenPTY opens a pseudo-terminal pair and returns the master and the
// slave's path, or skips where the machine has none.
func srOpenPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { mustNil(ptmx.Close()) })
	fd := int(ptmx.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	return ptmx, "/dev/pts/" + strconv.Itoa(n)
}

// TestSrGetpassTerminal reads a secret from a real terminal: echo is off
// while it is typed, so the master sees the prompt and, for a line ended
// by Enter, the newline getpass writes in place of the echo, never the
// secret, the terminal's ONLCR making it \r\n; and the terminal's
// settings are restored after.
//
// go test ./internal/goip/ -run TestSrGetpassTerminal
func TestSrGetpassTerminal(t *testing.T) {
	tests := []struct {
		description string
		typed       string
		expected    string
		output      string
	}{
		{description: "positive: a line ended by Enter: the prompt, then a newline, no echo",
			typed: "s3cret\n", expected: "s3cret", output: srHmacPromptCst + "\r\n"},
		{description: "boundary: Enter alone is a blank secret, and the newline",
			typed: "\n", expected: "", output: srHmacPromptCst + "\r\n"},
		{description: "corner: a line ended by two EOFs has no newline to write",
			typed: "s3cret\x04\x04", expected: "s3cret", output: srHmacPromptCst},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ptmx, pts := srOpenPTY(t)
			saved := srOpenTTY
			t.Cleanup(func() { srOpenTTY = saved })
			srOpenTTY = func() (*os.File, error) { return os.OpenFile(pts, os.O_RDWR|unix.O_NOCTTY, 0) }

			// The master types the line once the prompt is out, since
			// TCSAFLUSH discards whatever was typed before echo went off,
			// and reads until getpass closes the slave, so an echo cannot
			// arrive unseen.
			seen := make(chan string, 1)
			go func() {
				var out []byte
				buf := make([]byte, 256)
				typed := false
				for {
					n, err := ptmx.Read(buf)
					out = append(out, buf[:n]...)
					if !typed && bytes.Contains(out, []byte(srHmacPromptCst)) {
						typed = true
						if _, werr := ptmx.WriteString(tc.typed); werr != nil {
							break
						}
					}
					if err != nil {
						break
					}
				}
				seen <- string(out)
			}()

			c := newRunCtx(strings.NewReader("from stdin\n"), io.Discard, io.Discard)
			got, err := srGetpass(c)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expected {
				t.Errorf("secret %q, want %q", got, tc.expected)
			}
			if out := <-seen; out != tc.output {
				t.Errorf("terminal output %q, want %q", out, tc.output)
			}
			f, err := os.OpenFile(pts, os.O_RDWR|unix.O_NOCTTY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { mustNil(f.Close()) }()
			tio, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			if tio.Lflag&unix.ECHO == 0 || tio.Lflag&unix.ISIG == 0 {
				t.Errorf("lflag %#x: echo and signals not restored", tio.Lflag)
			}
		})
	}
}
