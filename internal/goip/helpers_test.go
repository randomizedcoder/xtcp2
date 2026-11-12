package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// skipIfNotCompiled skips a row whose write verb this build left out: only a
// build without goip_write returns ErrWriteNotCompiled, and the goip_write
// leg runs the row.
func skipIfNotCompiled(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, ErrWriteNotCompiled) {
		t.Skip(err)
	}
}

// skipIfNotCompiledText is skipIfNotCompiled for a row that sees only Run's
// stderr.
func skipIfNotCompiledText(t *testing.T, stderr string) {
	t.Helper()
	if strings.Contains(stderr, ErrWriteNotCompiled.Error()) {
		t.Skip(strings.TrimSpace(stderr))
	}
}

// restoreReplay answers restore's link dump from the slug's pcap and records
// each dump request: whole, for its seq, and with seq and pid zeroed.
type restoreReplay struct {
	*linkReplay
	dumpReqs [][]byte
	dumpSeqs []uint32
}

func (r *restoreReplay) Dump(request []byte, msgType uint16) ([][]byte, error) {
	r.dumpReqs = append(r.dumpReqs, zeroSeqPidCopy(request))
	r.dumpSeqs = append(r.dumpSeqs, binary.LittleEndian.Uint32(request[8:12]))
	return r.linkReplay.Dump(request, msgType)
}

// restoreWrites is the messages restore wrote to path, each whole.
func restoreWrites(t *testing.T, path string) []nlparity.Msg {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := nlparity.ParseWriteRequests(b)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

// addrSlugWrite is the write request `ip` sent for a slug, seq and pid zeroed.
func addrSlugWrite(t *testing.T, slug string) []byte {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(filepath.Join(addrWriteCorpusDir, slug, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	reqs := c.Requests()
	if len(reqs) == 0 {
		t.Fatalf("%s: capture holds no requests", slug)
	}
	return wireBytesOf(reqs[len(reqs)-1])
}

// addrWriteCorpusDir holds the A0-T04 address write captures; a row that
// names a slug reads that slug's ip.err.
const addrWriteCorpusDir = "../../pkg/xtcpnl/testdata/7_1_4/link-write/addr"

func nla(atype uint16, payload ...[]byte) []byte {
	var body []byte
	for _, p := range payload {
		body = append(body, p...)
	}
	n, ok := cint.U16(4 + len(body))
	if !ok {
		panic("nla: an attribute past 64 KiB")
	}
	b := make([]byte, 4, 4+len(body)+3)
	binary.LittleEndian.PutUint16(b[0:2], n)
	binary.LittleEndian.PutUint16(b[2:4], atype)
	b = append(b, body...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func le16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func catBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// wireBytesOf rebuilds a captured message as the datagram `ip` sent, with
// nlmsg_seq and nlmsg_pid zero, so it compares with zeroSeqPidCopy output.
func wireBytesOf(m nlparity.Msg) []byte {
	out := make([]byte, xtcpnl.NlMsgHdrSizeCst+len(m.Body))
	n, ok := cint.U32(len(out))
	if !ok {
		panic("a captured message past 4 GiB")
	}
	binary.LittleEndian.PutUint32(out[0:4], n)
	binary.LittleEndian.PutUint16(out[4:6], m.Hdr.Type)
	binary.LittleEndian.PutUint16(out[6:8], m.Hdr.Flags)
	copy(out[xtcpnl.NlMsgHdrSizeCst:], m.Body)
	return out
}

var errFakeResolve = errors.New("fake resolver failure")

func ifinfomsg(index int32, flags, change uint32) []byte {
	return xtcpnl.EncodeIfInfomsg(unix.AF_UNSPEC, index, flags, change)
}

func strz(s string) []byte { return append([]byte(s), 0) }

// newLinkBody is an RTM_NEWLINK body: the ifinfomsg, IFLA_IFNAME and the
// altnames in IFLA_PROP_LIST.
func newLinkBody(index int32, name string, alts ...string) []byte {
	b := catBytes(ifinfomsg(index, 0, 0), nla(unix.IFLA_IFNAME, strz(name)))
	if len(alts) > 0 {
		list := make([][]byte, 0, len(alts))
		for _, a := range alts {
			list = append(list, nla(unix.IFLA_ALT_IFNAME, strz(a)))
		}
		b = append(b, nla(unix.IFLA_PROP_LIST, list...)...)
	}
	return b
}

// runNeighCtx runs `ip neigh` args on c, as runRouteCtx runs `ip route`.
func runNeighCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runNeigh(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// captureRequests is a capture's requests, each with seq and pid zeroed.
func captureRequests(t *testing.T, pcap string) [][]byte {
	t.Helper()
	capt, err := nlparity.ParseRouteCaptureFile(pcap)
	if err != nil {
		t.Fatal(err)
	}
	reqs := capt.Requests()
	out := make([][]byte, 0, len(reqs))
	for _, m := range reqs {
		out = append(out, wireBytesOf(m))
	}
	return out
}

// runNexthopCtx runs `ip nexthop` args on c, as runRuleCtx runs `ip rule`.
func runNexthopCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runNexthop(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// writtenFile reports whether goip wrote GOIP_WRITE_OUT at path.
func writtenFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runRuleCtx runs `ip rule` args on c, as runRouteCtx runs `ip route`.
func runRuleCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runRule(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// fraOf is one FIB rule attribute.
func fraOf(typ uint16, val ...byte) []byte {
	b := make([]byte, 4, 4+len(val)+3)
	binary.LittleEndian.PutUint16(b[0:2], uint16(4+len(val)))
	binary.LittleEndian.PutUint16(b[2:4], typ)
	b = append(b, val...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}
