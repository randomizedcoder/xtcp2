package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// unsetWriteOut clears GOIP_WRITE_OUT for one test, restored afterward.
func unsetWriteOut(t *testing.T) {
	t.Helper()
	t.Setenv(writeOutEnvCst, "")
	if err := os.Unsetenv(writeOutEnvCst); err != nil {
		t.Fatal(err)
	}
}

// TestAddrFlushRefusals is where a flush stops before its first dump (design
// table 8, rows 6 and 7), and what it has sent by then: nothing for the two
// refusals ip makes before its keyword loop, the lookup for one made after.
//
// go test ./internal/goip/ -run TestAddrFlushRefusals
func TestAddrFlushRefusals(t *testing.T) {
	type result struct {
		err     string // a fragment of the error
		lookups int
		dumps   int
	}
	tests := []struct {
		description string
		slug        string
		family      uint8
		noHook      bool
		args        string
		expected    result
	}{
		{description: "negative: no arguments, before any request",
			slug: "flush_noargs", expected: result{err: "Flush requires arguments"}},
		{description: "negative: the link family, before any request",
			slug: "flush_packet", family: unix.AF_PACKET, args: "dev d0",
			expected: result{err: "Cannot flush link addresses"}},
		{description: "corner: with -0 and no arguments, no arguments is reported",
			slug: "flush_packet", family: unix.AF_PACKET,
			expected: result{err: "Flush requires arguments"}},
		{description: "negative: without GOIP_WRITE_OUT, after the lookup and before the dump",
			slug: "flush_dev", noHook: true, args: "dev d0",
			expected: result{err: "never applies a write", lookups: 1}},
		{description: "negative: help, before any request",
			slug: "flush_dev", args: "help", expected: result{err: "not implemented"}},
		{description: "negative: a second device is duparg2, before any request",
			slug: "flush_dev", args: "dev d0 d1", expected: result{err: "duplicate"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newFlushReplay(t, filepath.Join(addrFlushCorpusDir, tc.slug, "ip.pcap"))
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			if tc.noHook {
				unsetWriteOut(t)
			}
			c := &runCtx{src: src, lltab: NewLLTab(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, family: tc.family}
			err := addrFlush(c, strings.Fields(tc.args))
			got := result{lookups: len(src.gets), dumps: len(src.dumpReqs)}
			if err != nil && strings.Contains(err.Error(), tc.expected.err) {
				got.err = tc.expected.err
			} else {
				got.err = fmt.Sprint(err)
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
			if _, serr := os.Stat(out); serr == nil {
				t.Error("a write file was written")
			}
		})
	}
}

// TestRunAddressFlush is the CLI end to end through GOIP_REPLAY: the verb
// spellings, the options a flush ignores, the file, and ip's exit codes and
// messages.
//
// go test ./internal/goip/ -run TestRunAddressFlush
func TestRunAddressFlush(t *testing.T) {
	type result struct {
		code   int
		stdout string // the slug's ip.out when "ip.out"
		stderr string // a fragment; "" for empty
		dels   int    // DELs in the file; -1 for no file
	}
	tests := []struct {
		description string
		slug        string
		args        string
		noHook      bool
		expected    result
	}{
		{description: "positive: the four DELs go to GOIP_WRITE_OUT and nothing is printed",
			slug: "flush_dev", args: "addr flush dev d0", expected: result{code: ExitOK, dels: 4}},
		{description: "corner: a fl is address flush",
			slug: "flush_dev", args: "a fl dev d0", expected: result{code: ExitOK, dels: 4}},
		{description: "corner: -j changes nothing on a flush",
			slug: "flush_dev", args: "-j addr flush dev d0", expected: result{code: ExitOK, dels: 4}},
		{description: "corner: -o changes nothing on a flush",
			slug: "flush_dev", args: "-o addr flush dev d0", expected: result{code: ExitOK, dels: 4}},
		{description: "corner: -br changes nothing on a flush",
			slug: "flush_dev", args: "-br addr flush dev d0", expected: result{code: ExitOK, dels: 4}},
		{description: "positive: -s prints ip's round text",
			slug: "flush_stats", args: "-s addr flush dev d0", expected: result{code: ExitOK, stdout: "ip.out", dels: 2}},
		{description: "corner: nothing to flush exits 0 and writes no file",
			slug: "flush_nothing", args: "addr flush dev d0", expected: result{code: ExitOK, dels: -1}},
		{description: "negative: no arguments",
			slug: "flush_noargs", args: "addr flush", expected: result{code: ExitUsage, stderr: "Flush requires arguments", dels: -1}},
		{description: "negative: the link family",
			slug: "flush_packet", args: "-0 addr flush dev d0", expected: result{code: ExitUsage, stderr: "Cannot flush link addresses", dels: -1}},
		{description: "negative: without GOIP_WRITE_OUT",
			slug: "flush_dev", args: "addr flush dev d0", noHook: true,
			expected: result{code: ExitUsage, stderr: "never applies a write", dels: -1}},
		{description: "negative: -s -s is refused, as everywhere",
			slug: "flush_stats", args: "-s -s addr flush dev d0", expected: result{code: ExitUsage, stderr: "-s -s", dels: -1}},
		{description: "corner: -loops 1 stops before round 2, writes what it copied, and exits 255",
			slug: "flush_dev", args: "-loops 1 addr flush dev d0",
			expected: result{code: ExitMinusOne, stderr: "*** Flush remains incomplete after 1 rounds. ***", dels: 4}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", filepath.Join(addrFlushCorpusDir, tc.slug, "ip.pcap"))
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			if tc.noHook {
				unsetWriteOut(t)
			}
			var stdout, stderr bytes.Buffer
			got := result{code: Run(strings.Fields(tc.args), &stdout, &stderr), stdout: stdout.String(), dels: -1}
			if tc.expected.stdout == "ip.out" && got.stdout == addrFlushSlugFile(t, tc.slug, "ip.out") {
				got.stdout = "ip.out"
			}
			got.stderr = stderr.String()
			if tc.expected.stderr != "" && strings.Contains(got.stderr, tc.expected.stderr) {
				got.stderr = tc.expected.stderr
			}
			if b, err := os.ReadFile(out); err == nil {
				msgs, perr := nlparity.ParseWriteRequests(b)
				if perr != nil {
					t.Fatal(perr)
				}
				got.dels = len(msgs)
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// scriptedFlush answers dump k with script[k], the last entry for any dump
// past the end, and the lookup from flush_dev's pcap. It is the kernel a
// test needs for more than one round, which no capture holds.
type scriptedFlush struct {
	*linkReplay
	script [][][]byte
	dumps  int
}

func (s *scriptedFlush) Dump(request []byte, msgType uint16) ([][]byte, error) {
	if msgType != unix.RTM_NEWADDR {
		return s.linkReplay.Dump(request, msgType)
	}
	k := min(s.dumps, len(s.script)-1)
	s.dumps++
	return s.script[k], nil
}

// flushDevRound1 is flush_dev's pre-flush dump: 192.0.2.1, 198.51.100.1,
// 2001:db8::1 and the link-local.
func flushDevRound1(t *testing.T) [][]byte {
	t.Helper()
	_, seg := flushIPOf(t, "flush_dev")
	return flushRound1(seg)
}

// editAddrAttr returns a copy of an RTM_NEWADDR body with every attribute of
// type atype rewritten by edit, which may change the value but not its
// length.
func editAddrAttr(t *testing.T, body []byte, atype uint16, edit func(v []byte)) []byte {
	t.Helper()
	out := xtcpnl.CopyBytes(body)
	for off := 8; off+4 <= len(out); {
		l := int(binary.LittleEndian.Uint16(out[off:]))
		if l < 4 || off+l > len(out) {
			t.Fatalf("bad rtattr at %d", off)
		}
		if binary.LittleEndian.Uint16(out[off+2:])&0x3fff == atype {
			edit(out[off+4 : off+l])
		}
		off += (l + 3) &^ 3
	}
	return out
}

// derivedV4 is flush_dev's 192.0.2.1 moved to 192.0.2.last, local and
// address both: an address the pre-flush dump did not hold.
func derivedV4(t *testing.T, r1 [][]byte, last byte) []byte {
	t.Helper()
	for _, b := range r1 {
		if b[0] != unix.AF_INET {
			continue
		}
		set := func(v []byte) { v[3] = last }
		return editAddrAttr(t, editAddrAttr(t, b, unix.IFA_LOCAL, set), unix.IFA_ADDRESS, set)
	}
	t.Fatal("no IPv4 reply")
	return nil
}

// TestAddrFlushLoops is ipaddr_flush's round loop (ip/ipaddress.c:1972-2014)
// against a kernel that changes between dumps: -loops, the subtraction, the
// single round of a primary flush, and the -s text.
//
// go test ./internal/goip/ -run TestAddrFlushLoops
func TestAddrFlushLoops(t *testing.T) {
	r1 := flushDevRound1(t)
	x := derivedV4(t, r1, 77)
	with := func(extra ...[]byte) [][]byte { return append(append([][]byte(nil), r1...), extra...) }
	aged := make([][]byte, len(r1))
	for i, b := range r1 {
		aged[i] = editAddrAttr(t, b, unix.IFA_CACHEINFO, func(v []byte) { binary.LittleEndian.PutUint32(v, 1) })
	}
	fresh := func(n int) [][][]byte {
		out := make([][][]byte, n)
		for k := range out {
			out[k] = with()
			for j := 0; j <= k; j++ {
				out[k] = append(out[k], derivedV4(t, r1, byte(100+j)))
			}
		}
		return out
	}

	type result struct {
		dumps  int
		dels   int
		stdout string
		err    string // a fragment; "" for none
	}
	const incomplete = "*** Flush remains incomplete after %d rounds. ***"
	tests := []struct {
		description string
		loops       string // "" for the default
		stats       bool
		args        string
		script      [][][]byte
		expected    result
	}{
		{description: "positive: by default an unchanged kernel takes one round",
			stats: true, args: "dev d0", script: [][][]byte{r1},
			expected: result{dumps: 2, dels: 4, stdout: "\n*** Round 1, deleting 4 addresses ***\n*** Flush is complete after 1 round ***\n"}},
		{description: "positive: an address that appears in round 2 makes two rounds",
			stats: true, args: "dev d0", script: [][][]byte{r1, with(x)},
			expected: result{dumps: 3, dels: 5, stdout: "\n*** Round 1, deleting 4 addresses ***\n" +
				"\n*** Round 2, deleting 1 addresses ***\n*** Flush is complete after 2 rounds ***\n"}},
		{description: "corner: a deleted address whose cacheinfo changed is still subtracted",
			args: "dev d0", script: [][][]byte{r1, aged}, expected: result{dumps: 2, dels: 4}},
		{description: "corner: -loops 1 stops before round 2",
			loops: "1", args: "dev d0", script: [][][]byte{r1},
			expected: result{dumps: 1, dels: 4, err: fmt.Sprintf(incomplete, 1)}},
		{description: "corner: -loops 1 with primary completes; all four of d0's are primaries",
			loops: "1", stats: true, args: "dev d0 primary", script: [][][]byte{r1},
			expected: result{dumps: 1, dels: 4, stdout: "\n*** Round 1, deleting 4 addresses ***\n*** Flush is complete after 1 round ***\n"}},
		{description: "corner: -secondary is one round too",
			args: "dev d0 -secondary", script: [][][]byte{r1, with(x)}, expected: result{dumps: 1, dels: 4}},
		{description: "corner: -loops 0 is unlimited",
			loops: "0", args: "dev d0", script: [][][]byte{r1}, expected: result{dumps: 2, dels: 4}},
		{description: "corner: -loops abc is atoi's 0, unlimited",
			loops: "abc", args: "dev d0", script: [][][]byte{r1}, expected: result{dumps: 2, dels: 4}},
		{description: "boundary: -loops -1 runs no round",
			loops: "-1", args: "dev d0", script: [][][]byte{r1},
			expected: result{dumps: 0, dels: 0, err: fmt.Sprintf(incomplete, -1)}},
		{description: "boundary: a fresh address every round hits the limit",
			loops: "3", args: "dev d0", script: fresh(4),
			expected: result{dumps: 3, dels: 7, err: fmt.Sprintf(incomplete, 3)}},
		{description: "boundary: the same, one round more allowed, completes",
			loops: "4", args: "dev d0", script: append(fresh(3), with(derivedV4(t, r1, 100), derivedV4(t, r1, 101), derivedV4(t, r1, 102))),
			expected: result{dumps: 4, dels: 7}},
		{description: "corner: -s with nothing to flush",
			stats: true, args: "dev d0", script: [][][]byte{nil},
			expected: result{dumps: 1, stdout: "Nothing to flush.\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &scriptedFlush{linkReplay: newLinkReplay(t, filepath.Join(addrFlushCorpusDir, "flush_dev", "ip.pcap")), script: tc.script}
			t.Setenv(writeOutEnvCst, filepath.Join(t.TempDir(), "write.bin"))
			var out bytes.Buffer
			c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &bytes.Buffer{}}
			if tc.stats {
				c.showStats = 1
			}
			if tc.loops != "" {
				c.flushLoops, c.haveFlushLoops = cAtoi(tc.loops), true
			}
			r, err := runAddrFlush(c, strings.Fields(tc.args))
			dels, perr := parseFlushFile(r)
			if perr != nil {
				t.Fatal(perr)
			}
			got := result{dumps: src.dumps, dels: len(dels), stdout: out.String()}
			switch {
			case err == nil:
			case errors.Is(err, ErrFlushIncomplete) && strings.Contains(err.Error(), tc.expected.err):
				got.err = tc.expected.err
			default:
				got.err = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestFlushBuf is filter.flushb's packing (ip/ipaddress.c:1558-1567): a
// message goes at the next NLMSG_ALIGN offset, and one that would end past
// 3584 bytes sends what is there first.
//
// go test ./internal/goip/ -run TestFlushBuf
func TestFlushBuf(t *testing.T) {
	sizes := func(n int, l uint32) []uint32 {
		out := make([]uint32, n)
		for i := range out {
			out[i] = l
		}
		return out
	}
	tests := []struct {
		description string
		lengths     []uint32
		expected    string // messages per datagram
	}{
		{description: "positive: 47 IPv4 DELs fit in 3572 bytes", lengths: sizes(47, 76), expected: "[47]"},
		{description: "boundary: the 48th spills into a second datagram", lengths: sizes(48, 76), expected: "[47 1]"},
		{description: "boundary: exactly 3584 bytes stays one datagram", lengths: sizes(7, 512), expected: "[7]"},
		{description: "boundary: 4 bytes over sends the full buffer first", lengths: append(sizes(7, 512), 16), expected: "[7 1]"},
		{description: "corner: the aligned offset counts, not the sum", lengths: sizes(3, 1194), expected: "[2 1]"},
		{description: "positive: flush_many's 120 is 47, 47 and 26", lengths: sizes(120, 76), expected: "[47 47 26]"},
		{description: "corner: no messages, no datagram", lengths: nil, expected: "[]"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b flushBuf
			for _, l := range tc.lengths {
				msg := make([]byte, int(l))
				binary.LittleEndian.PutUint32(msg, l)
				b.add(msg)
			}
			b.end()
			counts := make([]int, 0, len(b.datagrams))
			for _, d := range b.datagrams {
				n := 0
				for off := 0; off < len(d); n++ {
					off += nlmsgAlign(int(binary.LittleEndian.Uint32(d[off:])))
				}
				counts = append(counts, n)
			}
			if got := fmt.Sprint(counts); got != tc.expected {
				t.Errorf("datagrams = %s, want %s", got, tc.expected)
			}
		})
	}
}

// TestMatchFlush is print_addrinfo's filter as a flush applies it, over
// flush_dev's pre-flush dump (d0 is index 3): the address tests apply, and
// the link tests show applies do not.
//
// go test ./internal/goip/ -run TestMatchFlush
func TestMatchFlush(t *testing.T) {
	r1 := flushDevRound1(t)
	addrs := make([]xtcpnl.AddrInfo, len(r1))
	for i, b := range r1 {
		ai, err := xtcpnl.ParseNewAddr(b)
		if err != nil {
			t.Fatal(err)
		}
		addrs[i] = ai
	}
	lk := addrLookups{
		index: func(name string) (int32, error) {
			if name == "d0" {
				return 3, nil
			}
			return 0, nil
		},
		isVRF: func(string) (bool, error) { return false, nil },
	}
	devName := func(idx int32) string {
		if idx == 3 {
			return "d0"
		}
		return "if" + fmt.Sprint(idx)
	}
	tests := []struct {
		description string
		family      uint8
		args        string
		ifindex     int32 // overrides the resolved device when set
		expected    string
	}{
		{description: "positive: dev alone keeps all of d0's", args: "dev d0",
			expected: "192.0.2.1 198.51.100.1 2001:db8::1 fe80::"},
		{description: "positive: scope link keeps the link-local", args: "dev d0 scope link", expected: "fe80::"},
		{description: "positive: -4 keeps the IPv4 addresses", family: unix.AF_INET, args: "dev d0",
			expected: "192.0.2.1 198.51.100.1"},
		{description: "positive: to a prefix", args: "dev d0 to 192.0.2.0/24", expected: "192.0.2.1"},
		{description: "corner: up is parsed and then ignored", args: "dev d0 up",
			expected: "192.0.2.1 198.51.100.1 2001:db8::1 fe80::"},
		{description: "corner: group is parsed and then ignored", args: "dev d0 group 5",
			expected: "192.0.2.1 198.51.100.1 2001:db8::1 fe80::"},
		{description: "corner: label d0 matches IPv6 by the device name", args: "dev d0 label d0",
			expected: "192.0.2.1 198.51.100.1 2001:db8::1 fe80::"},
		{description: "corner: label d0:1 matches nothing", args: "dev d0 label d0:1", expected: ""},
		{description: "negative: another device's addresses", args: "dev d0", ifindex: 4, expected: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f, err := parseAddrShowArgs(strings.Fields(tc.args), tc.family, lk)
			if err != nil {
				t.Fatal(err)
			}
			if tc.ifindex != 0 {
				f.ifindex = tc.ifindex
			}
			var kept []string
			for i := range addrs {
				if f.matchFlush(&addrs[i], devName) {
					kept = append(kept, addrString(addrLocal(&addrs[i])))
				}
			}
			if got := strings.Join(kept, " "); got != tc.expected {
				t.Errorf("kept %q, want %q", got, tc.expected)
			}
		})
	}
}

// addrString prints an address, the link-local by its prefix only.
func addrString(b []byte) string {
	if len(b) == 16 && b[0] == 0xfe && b[1] == 0x80 {
		return "fe80::"
	}
	a, _ := netip.AddrFromSlice(b)
	return a.String()
}
