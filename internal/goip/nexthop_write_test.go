package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

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

// nexthopFlushReplay answers the n-th dump with the replies of the n-th
// RTM_GETNEXTHOP dump in a capture, where ReplaySource answers every dump
// with every reply of the type. Its failDump-th dump fails, and with
// repeatGroups its second dump answers the first one's replies ahead of its
// own, as a kernel that applied no delete would.
type nexthopFlushReplay struct {
	answers      [][][]byte
	dumpReqs     [][]byte
	failDump     int
	repeatGroups bool
}

func newNexthopFlushReplay(t *testing.T, slug string) *nexthopFlushReplay {
	t.Helper()
	capt, err := nlparity.ParseRouteCaptureFile(routeRtnlDir + slug + "/ip.pcap")
	if err != nil {
		t.Fatal(err)
	}
	r := &nexthopFlushReplay{}
	for _, txn := range nlparity.SegmentCapture(capt).Txns {
		if txn.Request.Hdr.Type != unix.RTM_GETNEXTHOP {
			continue
		}
		var bodies [][]byte
		for _, m := range txn.Replies {
			if m.Hdr.Type == unix.RTM_NEWNEXTHOP {
				bodies = append(bodies, m.Body)
			}
		}
		r.answers = append(r.answers, bodies)
	}
	return r
}

func (r *nexthopFlushReplay) Dump(request []byte, _ uint16) ([][]byte, error) {
	r.dumpReqs = append(r.dumpReqs, zeroSeqPidCopy(request))
	n := len(r.dumpReqs)
	if n == r.failDump {
		return nil, errFailSource
	}
	if n > len(r.answers) {
		return nil, ErrNoReplay
	}
	got := r.answers[n-1]
	if n == 2 && r.repeatGroups {
		got = append(append([][]byte(nil), r.answers[0]...), got...)
	}
	return got, nil
}

// TestNexthopModifyMatchesIp replays each C1 nexthop write capture: goip
// sends ip's by-name link get when the command names a device, and writes
// ip's request, byte for byte with seq and pid zeroed, and exits 0 with
// nothing printed. The kernel's EEXIST is ip's, not goip's to know.
//
// go test ./internal/goip/ -run TestNexthopModifyMatchesIp
func TestNexthopModifyMatchesIp(t *testing.T) {
	type result struct {
		gets  int
		flags uint16
	}
	tests := []struct {
		description string
		slug        string
		args        string
		expected    result
	}{
		{"positive: a single nexthop, its device looked up by name", "nexthop_add_single",
			"add id 1 via 192.0.2.2 dev d0", result{1, 0x605}},
		{"positive: a group with a weight", "nexthop_add_group", "add id 10 group 1,3/2", result{0, 0x605}},
		{"positive: a resilient group's nest ahead of its type", "nexthop_add_resilient",
			"add id 20 group 1/2 type resilient buckets 1024", result{0, 0x605}},
		{"positive: replace", "nexthop_replace", "replace id 1 via 192.0.2.4 dev d0", result{1, 0x505}},
		{"positive: an add the kernel refuses is the same request", "nexthop_add_existing",
			"add id 1 blackhole", result{0, 0x605}},
		{"positive: del is RTM_DELNEXTHOP, acked", "nexthop_del", "del id 1", result{0, 0x5}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ipReqs := rtnlRequests(t, tc.slug)
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newRuleReplay(t, tc.slug)
			rc, stdout, stderr := runNexthopCtx(&runCtx{src: src}, strings.Fields(tc.args))
			if rc != 0 || stdout != "" || stderr != "" {
				t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
			}
			if len(src.gets) != tc.expected.gets || len(ipReqs) != tc.expected.gets+1 {
				t.Fatalf("goip sent %d gets, ip %d requests; want %d and one more", len(src.gets),
					len(ipReqs), tc.expected.gets)
			}
			for i, g := range src.gets {
				if !bytes.Equal(g, ipReqs[i]) {
					t.Errorf("get %d:\n got %x\nwant %x", i, g, ipReqs[i])
				}
			}
			writes := restoreWrites(t, out)
			if len(writes) != 1 {
				t.Fatalf("goip wrote %d messages, want 1", len(writes))
			}
			if got, want := capturedRequest(writes[0]), ipReqs[len(ipReqs)-1]; !bytes.Equal(got, want) {
				t.Errorf("write:\n got %x\nwant %x", got, want)
			}
			if writes[0].Hdr.Flags != tc.expected.flags {
				t.Errorf("flags %#x, want %#x", writes[0].Hdr.Flags, tc.expected.flags)
			}
		})
	}
}

// TestLookupNexthopWriteVerb is do_ipnh's matches() order over the three
// write verbs.
//
// go test ./internal/goip/ -run TestLookupNexthopWriteVerb
func TestLookupNexthopWriteVerb(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		expected    string
	}{
		{"positive: add", "add", nexthopAddVerbCst},
		{"positive: `a` is add", "a", nexthopAddVerbCst},
		{"positive: replace", "replace", addrVerbReplaceCst},
		{"positive: `r` is replace", "r", addrVerbReplaceCst},
		{"positive: `del` is delete", "del", nexthopDelVerbCst},
		{"positive: `d` is delete", "d", nexthopDelVerbCst},
		{"boundary: delete spelled out", "delete", nexthopDelVerbCst},
		{"negative: `addx` is no verb", "addx", ""},
		{"negative: `deletes` is no verb", "deletes", ""},
		{"negative: show is not a write", "show", ""},
		{"negative: flush is not a modify", "flush", ""},
		{"corner: the empty argument matches nothing", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got string
			if v := lookupNexthopWriteVerb(tc.arg); v != nil {
				got = v.name
			}
			if got != tc.expected {
				t.Errorf("lookupNexthopWriteVerb(%q) = %q, want %q", tc.arg, got, tc.expected)
			}
		})
	}
}

// TestParseNexthopWriteArgs is ipnh_modify's loop, on the nhmsg and
// attributes it builds. A stub resolver knows d0 as 3; the refusals'
// messages were checked against `ip nexthop` from iproute2-7.1.0 in an
// unprivileged namespace.
//
// go test ./internal/goip/ -run TestParseNexthopWriteArgs
func TestParseNexthopWriteArgs(t *testing.T) {
	type result struct {
		nhm   string // family scope protocol resvd flags
		attrs string
		err   error
	}
	nhm := func(family, protocol uint8, flags uint32) string {
		w := nexthopWrite{family: family, protocol: protocol, flags: flags}
		return string(w.nhm())
	}
	cat := func(a ...[]byte) string { return string(bytes.Join(a, nil)) }
	u32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	u16 := func(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
	id1 := fraOf(xtcpnl.NhaID, u32(1)...)
	grp := func(id uint32, lo, hi byte) []byte { return append(u32(id), lo, hi, 0, 0) }
	resNest := func(inner ...[]byte) []byte {
		return fraOf(xtcpnl.NhaResGroup|unix.NLA_F_NESTED, bytes.Join(inner, nil)...)
	}
	resType := fraOf(xtcpnl.NhaGroupType, u16(xtcpnl.NexthopGrpTypeRes)...)
	tests := []struct {
		description string
		family      uint8
		args        string
		expected    result
	}{
		{"positive: an id alone leaves the family unset", 0, "id 1", result{nhm(0, 0, 0), cat(id1), nil}},
		{"positive: blackhole makes the family AF_INET", 0, "id 1 blackhole",
			result{nhm(unix.AF_INET, 0, 0), cat(id1, fraOf(xtcpnl.NhaBlackhole)), nil}},
		{"positive: -6 blackhole keeps AF_INET6", unix.AF_INET6, "id 1 blackhole",
			result{nhm(unix.AF_INET6, 0, 0), cat(id1, fraOf(xtcpnl.NhaBlackhole)), nil}},
		{"positive: an IPv6 gateway makes the family AF_INET6", 0, "via 2001:db8::1",
			result{nhm(unix.AF_INET6, 0, 0), cat(fraOf(xtcpnl.NhaGateway,
				0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1)), nil}},
		{"positive: via takes a family word", 0, "via inet 192.0.2.1",
			result{nhm(unix.AF_INET, 0, 0), cat(fraOf(xtcpnl.NhaGateway, 192, 0, 2, 1)), nil}},
		{"positive: dev is the resolved ifindex, and AF_INET", 0, "dev d0",
			result{nhm(unix.AF_INET, 0, 0), cat(fraOf(xtcpnl.NhaOIF, u32(3)...)), nil}},
		{"positive: onlink is a header flag", 0, "id 1 onlink",
			result{nhm(0, 0, unix.RTNH_F_ONLINK), cat(id1), nil}},
		{"positive: `proto` abbreviates protocol, a name", 0, "proto static",
			result{nhm(0, unix.RTPROT_STATIC, 0), "", nil}},
		{"positive: fdb is a flag attribute", 0, "id 1 fdb", result{nhm(0, 0, 0), cat(id1, fraOf(xtcpnl.NhaFdb)), nil}},
		{"positive: type mpath", 0, "group 1/2 type mpath",
			result{nhm(0, 0, 0), cat(fraOf(xtcpnl.NhaGroup, append(grp(1, 0, 0), grp(2, 0, 0)...)...),
				fraOf(xtcpnl.NhaGroupType, u16(xtcpnl.NexthopGrpTypeMpath)...)), nil}},
		{"positive: resilient timers in hundredths", 0, "type resilient idle_timer 1 unbalanced_timer 2",
			result{nhm(0, 0, 0), cat(resNest(fraOf(xtcpnl.NhaResGroupIdleTimer, u32(100)...),
				fraOf(xtcpnl.NhaResGroupUnbalancedTimer, u32(200)...)), resType), nil}},
		{"boundary: weights 256 and 65536 split into low and high bytes", 0, "group 1,256/2,65536",
			result{nhm(0, 0, 0), cat(fraOf(xtcpnl.NhaGroup, append(grp(1, 0xff, 0), grp(2, 0xff, 0xff)...)...)), nil}},
		{"boundary: resilient last adds no nest", 0, "type resilient", result{nhm(0, 0, 0), cat(resType), nil}},
		{"boundary: a protocol past a byte keeps its low byte", 0, "protocol 0x1ff", result{nhm(0, 0xff, 0), "", nil}},
		{"boundary: no arguments is an empty request", 0, "", result{nhm(0, 0, 0), "", nil}},
		{"corner: resilient then another keyword leaves an empty nest", 0, "type resilient fdb",
			result{nhm(0, 0, 0), cat(resNest(), resType, fraOf(xtcpnl.NhaFdb)), nil}},
		{"negative: a weight of 0", 0, "group 1,0", result{err: ErrInvalidArgument}},
		{"negative: a weight past 65536", 0, "group 1,65537", result{err: ErrInvalidArgument}},
		{"negative: an empty group", 0, "group 1//2", result{err: ErrInvalidArgument}},
		{"negative: a group id that is no number", 0, "group x", result{err: ErrInvalidArgument}},
		{"negative: an unknown group type", 0, "type bogus", result{err: ErrInvalidArgument}},
		{"negative: buckets past 16 bits", 0, "type resilient buckets 65536", result{err: ErrInvalidArgument}},
		{"negative: an idle timer of UINT32_MAX / 100", 0, "type resilient idle_timer 42949672",
			result{err: ErrInvalidArgument}},
		{"negative: an id with no value", 0, "id", result{err: ErrInvalidArgument}},
		{"negative: an id past 32 bits", 0, "id 4294967296", result{err: ErrInvalidArgument}},
		{"negative: an unknown device", 0, "dev nope", result{err: ErrInvalidArgument}},
		{"negative: an IPv6 gateway after blackhole's AF_INET", 0, "blackhole via 2001:db8::1",
			result{err: ErrInvalidArgument}},
		{"negative: a second gateway of the other family", 0, "via 192.0.2.1 via inet6 2001:db8::1",
			result{err: ErrInvalidArgument}},
		{"negative: an unknown protocol name", 0, "protocol bogus", result{err: ErrInvalidArgument}},
		{"negative: an unknown keyword", 0, "bogus", result{err: ErrInvalidArgument}},
		{"negative: encap is not implemented", 0, "encap mpls 100", result{err: ErrNotImplemented}},
		{"negative: dst_port is not implemented", 0, "dst_port 4789", result{err: ErrNotImplemented}},
		{"negative: hw_stats is not implemented", 0, "hw_stats on", result{err: ErrNotImplemented}},
		{"negative: help is not implemented", 0, "help", result{err: ErrNotImplemented}},
	}
	resolve := func(name string) (int32, error) {
		if name == "d0" {
			return 3, nil
		}
		return 0, nil
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w, err := parseNexthopWriteArgs(tc.family, resolve, strings.Fields(tc.args))
			var got result
			if err != nil {
				for _, e := range []error{ErrInvalidArgument, ErrNotImplemented} {
					if errors.Is(err, e) {
						got.err = e
					}
				}
			} else {
				got = result{nhm: string(w.nhm()), attrs: string(w.attrs.Bytes())}
			}
			if got != tc.expected {
				t.Errorf("got  %x %x %v\nwant %x %x %v (%v)", got.nhm, got.attrs, got.err,
					tc.expected.nhm, tc.expected.attrs, tc.expected.err, err)
			}
		})
	}
}

// TestNexthopModify is the write verbs' paths around the request, over
// nexthop_add_single's capture.
//
// go test ./internal/goip/ -run TestNexthopModify$
func TestNexthopModify(t *testing.T) {
	type result struct {
		rc, gets int
	}
	tests := []struct {
		description string
		noHook      bool
		args        string
		expected    result
	}{
		{"positive: `a` is add", false, "a id 1 via 192.0.2.2 dev d0", result{ExitOK, 1}},
		{"negative: no GOIP_WRITE_OUT, after the get", true, "add id 1 via 192.0.2.2 dev d0", result{ExitUsage, 1}},
		{"negative: a bad argument is refused before the get", false, "add bogus dev d0", result{ExitUsage, 0}},
		{"corner: a refusal after dev comes after the get", false, "add dev d0 bogus", result{ExitUsage, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, filepath.Join(t.TempDir(), "write.bin"))
			}
			src := newRuleReplay(t, "nexthop_add_single")
			rc, _, _ := runNexthopCtx(&runCtx{src: src}, strings.Fields(tc.args))
			if got := (result{rc, len(src.gets)}); got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestNexthopFlushMatchesIp replays nexthop_flush's capture: goip sends ip's
// groups dump and its full dump, byte for byte with seq and pid zeroed, and
// writes ip's three deletes, the group's first, each NLM_F_REQUEST|NLM_F_ACK,
// and prints ip's line.
//
// go test ./internal/goip/ -run TestNexthopFlushMatchesIp
func TestNexthopFlushMatchesIp(t *testing.T) {
	ipReqs := rtnlRequests(t, "nexthop_flush")
	if len(ipReqs) != 5 {
		t.Fatalf("capture holds %d requests, want 5", len(ipReqs))
	}
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := newNexthopFlushReplay(t, "nexthop_flush")
	rc, stdout, stderr := runNexthopCtx(&runCtx{src: src}, []string{"flush"})
	if rc != 0 || stdout != "Flushed 3 nexthops\n" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	// ip's order: the groups dump, the group's delete, the full dump, then
	// the members' deletes.
	dumps, dels := [][]byte{ipReqs[0], ipReqs[2]}, [][]byte{ipReqs[1], ipReqs[3], ipReqs[4]}
	if len(src.dumpReqs) != 2 {
		t.Fatalf("goip sent %d dumps, want 2", len(src.dumpReqs))
	}
	for i := range dumps {
		if !bytes.Equal(src.dumpReqs[i], dumps[i]) {
			t.Errorf("dump %d:\n got %x\nwant %x", i, src.dumpReqs[i], dumps[i])
		}
	}
	writes := restoreWrites(t, out)
	if len(writes) != len(dels) {
		t.Fatalf("goip wrote %d messages, want %d", len(writes), len(dels))
	}
	for i, w := range writes {
		if got := capturedRequest(w); !bytes.Equal(got, dels[i]) {
			t.Errorf("write %d:\n got %x\nwant %x", i, got, dels[i])
		}
		if w.Hdr.Flags != unix.NLM_F_REQUEST|unix.NLM_F_ACK {
			t.Errorf("write %d flags %#x, want 0x5", i, w.Hdr.Flags)
		}
	}
}

// TestNexthopFlush is the flush's paths over nexthop_flush's capture, whose
// groups dump holds resilient group 20 and whose full dump holds its members
// 1 and 2, and nexthop_flush_empty's.
//
// go test ./internal/goip/ -run TestNexthopFlush$
func TestNexthopFlush(t *testing.T) {
	type result struct {
		rc, dumps int
		ids       string
		stdout    string
	}
	tests := []struct {
		description  string
		slug         string
		noHook       bool
		stats        int
		failDump     int
		repeatGroups bool
		args         string
		expected     result
	}{
		{description: "positive: `f` abbreviates flush", slug: "nexthop_flush", args: "f",
			expected: result{ExitOK, 2, "20 1 2", "Flushed 3 nexthops\n"}},
		{description: "positive: a group the second dump repeats is deleted once", slug: "nexthop_flush",
			repeatGroups: true, args: "flush", expected: result{ExitOK, 2, "20 1 2", "Flushed 3 nexthops\n"}},
		{description: "boundary: nothing to flush needs no hook", slug: "nexthop_flush_empty", noHook: true,
			args: "flush", expected: result{ExitOK, 2, "", "Nothing to flush\n"}},
		{description: "negative: deletes and no GOIP_WRITE_OUT, after both dumps", slug: "nexthop_flush",
			noHook: true, args: "flush", expected: result{ExitUsage, 2, "", ""}},
		{description: "negative: a failed groups dump prints ip's line, exit 2", slug: "nexthop_flush",
			failDump: 1, args: "flush", expected: result{ExitFailure, 1, "", "Nothing to flush\n"}},
		{description: "negative: a selector is refused before any request", slug: "nexthop_flush",
			args: "flush dev d0", expected: result{ExitUsage, 0, "", ""}},
		{description: "negative: -s is refused before any request", slug: "nexthop_flush", stats: 1,
			args: "flush", expected: result{ExitUsage, 0, "", ""}},
		{description: "corner: a failed full dump still deletes the group, exit 2", slug: "nexthop_flush",
			failDump: 2, args: "flush", expected: result{ExitFailure, 2, "20", "Flushed 1 nexthops\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			src := newNexthopFlushReplay(t, tc.slug)
			src.failDump, src.repeatGroups = tc.failDump, tc.repeatGroups
			rc, stdout, _ := runNexthopCtx(&runCtx{src: src, showStats: tc.stats}, strings.Fields(tc.args))
			got := result{rc: rc, dumps: len(src.dumpReqs), stdout: stdout}
			if b, err := os.ReadFile(out); err == nil {
				got.ids = nexthopDelIDs(t, b)
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// nexthopDelIDs is the NHA_ID of each delete in a GOIP_WRITE_OUT file, in
// order.
func nexthopDelIDs(t *testing.T, b []byte) string {
	t.Helper()
	var ids []string
	for len(b) >= xtcpnl.NlMsgHdrSizeCst {
		n := int(binary.LittleEndian.Uint32(b[0:4]))
		body := b[xtcpnl.NlMsgHdrSizeCst+xtcpnl.NhMsgSizeCst : n]
		if err := xtcpnl.WalkRTAttrs(body, func(atype uint16, v []byte) {
			if atype == xtcpnl.NhaID {
				ids = append(ids, strconv.FormatUint(uint64(binary.LittleEndian.Uint32(v)), 10))
			}
		}); err != nil {
			t.Fatal(err)
		}
		b = b[nlmsgAlign(n):]
	}
	return strings.Join(ids, " ")
}
