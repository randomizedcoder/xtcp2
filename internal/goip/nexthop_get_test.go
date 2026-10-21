package goip

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestParseNexthopGetArgs is ipnh_get's loop: `id` alone, read by
// get_unsigned in base 0, and usage for anything else.
//
// go test ./internal/goip/ -run TestParseNexthopGetArgs
func TestParseNexthopGetArgs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    uint32
		wantErr     error
		wantErrText string
	}{
		{description: "positive: `id 1`", args: "id 1", expected: 1},
		{description: "positive: base 0 reads `0x14` as 20", args: "id 0x14", expected: 20},
		{description: "boundary: the largest u32", args: "id 4294967295", expected: 0xffffffff},
		{description: "boundary: a second `id` overwrites the first", args: "id 1 id 2", expected: 2},
		{description: "negative: no arguments is usage", args: "", wantErr: ErrNotImplemented},
		{description: "negative: id 0 is no id, so usage", args: "id 0", wantErr: ErrNotImplemented},
		{description: "negative: one past u32 is invarg", args: "id 4294967296",
			wantErr: ErrInvalidArgument, wantErrText: "invalid id value"},
		{description: "negative: a word that is no number is invarg", args: "id one",
			wantErr: ErrInvalidArgument, wantErrText: "invalid id value"},
		{description: "boundary: `id` with no value is not complete", args: "id",
			wantErr: ErrInvalidArgument, wantErrText: "Command line is not complete"},
		{description: "corner: `i` is not `id`, which is strcmp, so usage", args: "i 1", wantErr: ErrNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseNexthopGetArgs(strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.wantErrText) {
					t.Errorf("err = %q, want it to contain %q", err, tc.wantErrText)
				}
				return
			}
			if got != tc.expected {
				t.Errorf("id = %d, want %d", got, tc.expected)
			}
		})
	}
}

// TestParseNexthopBucketListArgs is ipnh_bucket_list's loop: the `id` and
// `nhid` filters, the name selectors goip refuses, and invarg for the rest.
//
// go test ./internal/goip/ -run TestParseNexthopBucketListArgs
func TestParseNexthopBucketListArgs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    xtcpnl.NexthopBucketDumpFilter
		wantErr     error
	}{
		{description: "positive: `id 20` (nexthop_bucket_show)", args: "id 20",
			expected: xtcpnl.NexthopBucketDumpFilter{ID: 20}},
		{description: "positive: `nhid 2`", args: "nhid 2", expected: xtcpnl.NexthopBucketDumpFilter{NhID: 2}},
		{description: "positive: both, in either order", args: "nhid 2 id 20",
			expected: xtcpnl.NexthopBucketDumpFilter{ID: 20, NhID: 2}},
		{description: "boundary: no arguments is the unfiltered dump", args: ""},
		{description: "boundary: `id 0` is no filter, which ip allows on a listing", args: "id 0"},
		{description: "negative: `dev` is refused, not implemented", args: "dev d0", wantErr: ErrNotImplemented},
		{description: "negative: `m`, master by matches(), is refused", args: "m br0", wantErr: ErrNotImplemented},
		{description: "negative: `vrf` is refused", args: "vrf red", wantErr: ErrNotImplemented},
		{description: "negative: an unknown word is invarg", args: "bogus", wantErr: ErrInvalidArgument},
		{description: "negative: a bad id is invarg", args: "id x", wantErr: ErrInvalidArgument},
		{description: "corner: `h` is help, which is usage", args: "h", wantErr: ErrNotImplemented},
		{description: "corner: `nh` is not `nhid`, which is strcmp", args: "nh 2", wantErr: ErrInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseNexthopBucketListArgs(strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && got != tc.expected {
				t.Errorf("filter = %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestParseNexthopBucketGetArgs is ipnh_bucket_get's loop: `id` and `index`,
// both required, and usage for anything else.
//
// go test ./internal/goip/ -run TestParseNexthopBucketGetArgs
func TestParseNexthopBucketGetArgs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    nexthopBucketGetArgs
		wantErr     error
		wantErrText string
	}{
		{description: "positive: `id 20 index 0` (nexthop_bucket_get); index 0 is a real index",
			args: "id 20 index 0", expected: nexthopBucketGetArgs{id: 20}},
		{description: "positive: index first", args: "index 7 id 20", expected: nexthopBucketGetArgs{id: 20, index: 7}},
		{description: "boundary: index 65535", args: "id 20 index 65535",
			expected: nexthopBucketGetArgs{id: 20, index: 0xffff}},
		{description: "negative: index 65536 is invarg", args: "id 20 index 65536",
			wantErr: ErrInvalidArgument, wantErrText: "invalid bucket index value"},
		{description: "negative: no index is usage", args: "id 20", wantErr: ErrNotImplemented},
		{description: "negative: no id is usage", args: "index 0", wantErr: ErrNotImplemented},
		{description: "negative: id 0 is no id, so usage", args: "id 0 index 0", wantErr: ErrNotImplemented},
		{description: "corner: `idx` is not `index`, which is strcmp, so usage", args: "id 20 idx 0",
			wantErr: ErrNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseNexthopBucketGetArgs(strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.wantErrText) {
					t.Errorf("err = %q, want it to contain %q", err, tc.wantErrText)
				}
				return
			}
			if got != tc.expected {
				t.Errorf("args = %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestNexthopGetMatchesIp replays each nexthop get and bucket capture through
// runNexthop and compares what goip sends and prints with what ip sent and
// printed: every request byte for byte and in order, and stdout with ip.out.
//
// go test ./internal/goip/ -run TestNexthopGetMatchesIp
func TestNexthopGetMatchesIp(t *testing.T) {
	const (
		getlink   = 18  // RTM_GETLINK
		getnh     = 106 // RTM_GETNEXTHOP
		getbucket = 118 // RTM_GETNEXTHOPBUCKET
	)
	tests := []struct {
		description string
		slug        string
		args        string
		expected    []uint16 // request types, in order
		wantErr     error
	}{
		{"positive: `nexthop get id 1`, its device named by a get by index",
			"nexthop_get", "get id 1", []uint16{getnh, getlink}, nil},
		{"negative: a missing id is the kernel's ENOENT, exit 2 and nothing on stdout",
			"nexthop_get_missing", "get id 9", []uint16{getnh}, unix.ENOENT},
		{"positive: `nexthop bucket show id 20`, all 1024 buckets of the resilient group",
			"nexthop_bucket_show", "bucket show id 20", []uint16{getbucket}, nil},
		{"positive: `nexthop bucket get id 20 index 0`",
			"nexthop_bucket_get", "bucket get id 20 index 0", []uint16{getbucket}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := routeGetCorpusDir + "/" + tc.slug
			pcap := dir + "/ip.pcap"
			src := routeGetReplay{newStatsReplay(t, pcap)}
			var out bytes.Buffer
			c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard}
			err := runNexthop(c, strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("runNexthop = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && exitCodeOf(err) != ExitFailure {
				t.Errorf("exit %d, want %d (ip.rc)", exitCodeOf(err), ExitFailure)
			}
			want, rerr := os.ReadFile(dir + "/ip.out")
			if rerr != nil {
				t.Fatal(rerr)
			}
			if out.String() != string(want) {
				t.Errorf("stdout differs from ip.out\n got %q\nwant %q", firstLines(out.String(), 3), firstLines(string(want), 3))
			}
			capt, perr := nlparity.ParseRouteCaptureFile(pcap)
			if perr != nil {
				t.Fatal(perr)
			}
			var ip [][]byte
			for _, m := range capt.Requests() {
				ip = append(ip, capturedRequest(m))
			}
			if len(src.sent) != len(tc.expected) || len(ip) != len(tc.expected) {
				t.Fatalf("goip sent %d requests, ip %d; want %d each", len(src.sent), len(ip), len(tc.expected))
			}
			for i := range ip {
				if typ := requestTypeOf(src.sent[i]); typ != tc.expected[i] {
					t.Errorf("request %d type %d, want %d", i+1, typ, tc.expected[i])
				}
				if !bytes.Equal(src.sent[i], ip[i]) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], ip[i])
				}
			}
		})
	}
}

// TestRunNexthopBucketDispatch is do_ipnh's and do_ipnh_bucket's verb
// matching for the two new verbs.
//
// go test ./internal/goip/ -run TestRunNexthopBucketDispatch
func TestRunNexthopBucketDispatch(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		args        string
		wantPrefix  string
		wantErr     error
	}{
		{description: "positive: `g` is get", slug: "nexthop_get", args: "g id 1", wantPrefix: "id 1 via 192.0.2.2"},
		{description: "positive: `b g` is bucket get", slug: "nexthop_bucket_get", args: "b g id 20 index 0",
			wantPrefix: "id 20 index 0 "},
		{description: "positive: `bucket lst` is the listing", slug: "nexthop_bucket_show", args: "bucket lst id 20",
			wantPrefix: "id 20 index 0 "},
		{description: "boundary: `bucket` alone is the unfiltered listing", slug: "nexthop_bucket_show", args: "bucket",
			wantPrefix: "id 20 index 0 "},
		{description: "negative: an unknown bucket verb is refused, exit 1", slug: "nexthop_bucket_show",
			args: "bucket bogus", wantErr: ErrInvalidArgument},
		{description: "corner: `bucket help` is usage", slug: "nexthop_bucket_show", args: "bucket help",
			wantErr: ErrNotImplemented},
		{description: "corner: `flush` stays refused", slug: "nexthop_get", args: "flush", wantErr: ErrNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := routeGetReplay{newStatsReplay(t, routeGetCorpusDir+"/"+tc.slug+"/ip.pcap")}
			var out bytes.Buffer
			c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard}
			err := runNexthop(c, strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("runNexthop = %v, want %v", err, tc.wantErr)
			}
			if err == nil && !strings.HasPrefix(out.String(), tc.wantPrefix) {
				t.Errorf("stdout = %q, want prefix %q", firstLines(out.String(), 3), tc.wantPrefix)
			}
		})
	}
}
