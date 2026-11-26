package main

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The monfile tests. The spec path is pinned against hand-built bytes — the
// header fields verbatim, the body zero-padded to the 4-byte boundary — and
// the captured path against the corpus's evnhb-bucket row, whose notifications
// the delivery filter picks out of the whole capture.

// monfileRun runs the subcommand over one spec or pcap, handing back its exit
// and what it printed and wrote.
func monfileRun(t *testing.T, args ...string) (int, string, string, []byte) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	rc := runMonfile(args, &stdout, &stderr)
	var out []byte
	for i, a := range args {
		if a == "-out" && i+1 < len(args) {
			b, err := os.ReadFile(args[i+1])
			if err == nil {
				out = b
			}
		}
	}
	return rc, stdout.String(), stderr.String(), out
}

// monfileWrite writes content under dir and gives its path.
func monfileWrite(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMonfileSpec is the crafted-message path: a spec's bytes, and the
// refusals that keep a spec honest — a message without its why, a body that
// is not hex, a length over rtnl_from_file's buffer, a spec with no messages,
// a field the format does not have.
//
// go test ./tools/nlcapgen/ -run TestMonfileSpec
func TestMonfileSpec(t *testing.T) {
	dir := t.TempDir()
	spec := monfileWrite(t, dir, "row.spec", `# a comment line carries no message
{"type": 16, "flags": 0, "seq": 7, "pid": 42, "body": "", "why": "a bare header, the shape NLMSG_NOOP rides"}

{"type": 20, "flags": 1, "seq": 8, "pid": 43, "body": "0220800003000000", "why": "an ifaddrmsg, already aligned"}
{"type": 24, "flags": 2, "seq": 9, "pid": 44, "body": "0102", "why": "a 2-byte body, zero-padded to the boundary"}
`)
	out := filepath.Join(dir, "row.nl")
	rc, stdout, stderr, got := monfileRun(t, "-spec", spec, "-out", out)
	if rc != 0 || stdout != "3 messages\n" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	want := "1000000010000000070000002a000000" + // bare header
		"1800000014000100080000002b0000000220800003000000" + // aligned body
		"1200000018000200090000002c00000001020000" // padded body
	wantBytes, err := hex.DecodeString(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantBytes) {
		t.Errorf("got %x, want %x", got, wantBytes)
	}

	tests := []struct {
		description, spec, errText string
	}{
		{"negative: a message without its why",
			"{\"type\": 16, \"body\": \"\", \"why\": \"\"}\n",
			"the why is missing"},
		{"negative: a body that is not hex",
			"{\"type\": 16, \"body\": \"zz\", \"why\": \"x\"}\n",
			"body:"},
		{"boundary: a length over the reader's buffer",
			"{\"type\": 16, \"body\": \"" + strings.Repeat("00", 16369) + "\", \"why\": \"x\"}\n",
			"over rtnl_from_file's 16384 buffer"},
		{"negative: no messages at all",
			"# a comment alone\n\n",
			"no messages"},
		{"negative: a field the format does not have",
			"{\"typ\": 16, \"why\": \"x\"}\n",
			`unknown field "typ"`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := monfileWrite(t, t.TempDir(), "row.spec", tc.spec)
			rc, _, stderr, _ := monfileRun(t, "-spec", path, "-out",
				filepath.Join(t.TempDir(), "row.nl"))
			if rc != 1 || !strings.Contains(stderr, tc.errText) {
				t.Errorf("rc %d, stderr %q, want the %q refusal",
					rc, stderr, tc.errText)
			}
		})
	}

	usage := [][]string{
		{"-spec", spec}, // no -out
		{"-spec", spec, "-pcap", "x", "-out", out}, // both inputs
		{"-out", out}, // neither input
	}
	for _, args := range usage {
		if rc, _, _, _ := monfileRun(t, args...); rc != 2 {
			t.Errorf("args %v: rc %d, want 2", args, rc)
		}
	}
}

// evnhbGolden is the evnhb-bucket row's notifications as .nl bytes: the
// monitor's three events out of the capture's sixteen messages.
const evnhbGolden = "" +
	// RTM_NEWMULTICAST (56), flags 0, seq 0, pid 0: ifaddrmsg ifindex 3, the
	// mcast group, its user count, a forever cacheinfo.
	"3c000000380000000000000000000000" +
	"022080000300000008000700e00000fa08000c000100000014000600ffffffffffffffff3128010031280100" +
	// RTM_NEWNEXTHOP (104), flags 0x0505, the requester's seq and port id:
	// nexthop_notify copies the request's REQUEST|ACK|REPLACE|CREATE into the
	// notification, the one notification shape a naive request filter drops.
	"6c00000068000505e60e056b33190000" +
	"0000000000000000080001000a0000000600030001000000140002000100000002000000020000000000000028000c800600010004000000080002007017000008000300640000000c000400000000000000000008000e0000000080" +
	// RTM_NEWNEXTHOPBUCKET (116), flags NLM_F_REPLACE, seq 0, pid 0.
	"40000000740000010000000000000000" +
	"0000000000000000080001000a00000020000d80060001000000000008000300010000000c0002000000000000000000"

// TestMonfilePcap is the captured path over the corpus's evnhb-bucket row:
// the .nl stream is the notifications the monitor was sent — three of
// sixteen, the golden bytes naming which three — and with -all every message,
// walked back to the capture's own, byte for byte.
//
// go test ./tools/nlcapgen/ -run TestMonfilePcap
func TestMonfilePcap(t *testing.T) {
	const pcap = "../../pkg/xtcpnl/testdata/7_3_1/ws/ev/evnhb-bucket/ip.pcap"
	out := filepath.Join(t.TempDir(), "row.nl")

	rc, stdout, stderr, got := monfileRun(t, "-pcap", pcap, "-out", out)
	if rc != 0 || stdout != "3 of 16 messages\n" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	want, err := hex.DecodeString(evnhbGolden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}

	rc, stdout, stderr, all := monfileRun(t, "-pcap", pcap, "-all", "-out", out)
	if rc != 0 || stdout != "16 messages\n" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	capture, err := nlparity.ParseRouteCaptureFile(pcap)
	if err != nil {
		t.Fatal(err)
	}
	msgs := capture.Msgs()
	walked := walkMonfile(t, all)
	if len(walked) != len(msgs) {
		t.Fatalf("walked %d messages, want the capture's %d", len(walked), len(msgs))
	}
	for i := range msgs {
		if walked[i].hdr != msgs[i].Hdr || !bytes.Equal(walked[i].body, msgs[i].Body) {
			t.Errorf("message %d: got %+v/%x, want %+v/%x", i, walked[i].hdr,
				walked[i].body, msgs[i].Hdr, msgs[i].Body)
		}
	}
	// The filtered stream is the -all stream less what the delivery filter
	// drops, so the three golden messages are also the walked ones it keeps.
	idxs := nlparity.NotificationIndexes(msgs)
	kept := make([]monfileMsg, 0, len(idxs))
	for _, i := range idxs {
		kept = append(kept, walked[i])
	}
	wantBytes, err := hex.DecodeString(evnhbGolden)
	if err != nil {
		t.Fatal(err)
	}
	walkedKept := walkMonfile(t, wantBytes)
	if len(walkedKept) != len(kept) {
		t.Fatalf("golden walks to %d messages, the filter keeps %d",
			len(walkedKept), len(kept))
	}
	for i := range kept {
		if walkedKept[i].hdr != kept[i].hdr || !bytes.Equal(walkedKept[i].body, kept[i].body) {
			t.Errorf("notification %d: got %+v/%x, want %+v/%x", i, walkedKept[i],
				walkedKept[i].body, kept[i].hdr, kept[i].body)
		}
	}
}

// TestMonfileCommittedSpecs is the regeneration golden over the committed
// spec/.nl pairs (internal/goip/testdata/monitor): every spec must rewrite
// its .nl byte for byte, so the binary can never drift from its reviewable
// source.
//
// go test ./tools/nlcapgen/ -run TestMonfileCommittedSpecs
func TestMonfileCommittedSpecs(t *testing.T) {
	const dir = "../../internal/goip/testdata/monitor"
	specs, err := filepath.Glob(filepath.Join(dir, "*.spec"))
	if err != nil || len(specs) == 0 {
		t.Fatalf("glob %s: %v", dir, err)
	}
	for _, spec := range specs {
		nl := strings.TrimSuffix(spec, ".spec") + ".nl"
		want, err := os.ReadFile(nl)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(spec), func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "regen.nl")
			rc, stdout, stderr, got := monfileRun(t, "-spec", spec, "-out", out)
			if rc != 0 || stdout != "1 messages\n" || stderr != "" {
				t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s: regenerated %x, committed %x", filepath.Base(nl), got, want)
			}
		})
	}
}

// walkMonfile walks .nl bytes as rtnl_from_file does — each header, then its
// body to the 4-byte boundary — failing unless the walk ends exactly at the
// end of the stream.
func walkMonfile(t *testing.T, data []byte) []monfileMsg {
	t.Helper()
	var out []monfileMsg
	for off := 0; off < len(data); {
		if len(data)-off < xtcpnl.NlMsgHdrSizeCst {
			t.Fatalf("header cut short at %d", off)
		}
		var h xtcpnl.NlMsgHdr
		if _, err := xtcpnl.DeserializeNlMsgHdr(data[off:], &h); err != nil {
			t.Fatal(err)
		}
		l := int(h.Len) - xtcpnl.NlMsgHdrSizeCst
		end := off + xtcpnl.NlMsgHdrSizeCst + l + xtcpnl.FourByteAlignPadding(l)
		if end > len(data) {
			t.Fatalf("body cut short at %d", off)
		}
		out = append(out, monfileMsg{hdr: h,
			body: data[off+xtcpnl.NlMsgHdrSizeCst : off+xtcpnl.NlMsgHdrSizeCst+l]})
		off = end
	}
	return out
}

// TestMonfilePadding is the boundary the reader owes padding on: a body off
// the 4-byte boundary ends where the next message begins, the length the
// header's and the padding zero.
//
// go test ./tools/nlcapgen/ -run TestMonfilePadding
func TestMonfilePadding(t *testing.T) {
	dir := t.TempDir()
	spec := monfileWrite(t, dir, "row.spec",
		"{\"type\": 28, \"flags\": 0, \"seq\": 1, \"pid\": 2, \"body\": \"aabbcc\", \"why\": \"3 bytes\"}\n"+
			"{\"type\": 28, \"flags\": 0, \"seq\": 2, \"pid\": 3, \"body\": \"dd\", \"why\": \"1 byte\"}\n")
	out := filepath.Join(dir, "row.nl")
	rc, stdout, stderr, got := monfileRun(t, "-spec", spec, "-out", out)
	if rc != 0 || stdout != "2 messages\n" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	// 16+3 padded to 20, then 16+1 padded to 20: both messages start on the
	// boundary the reader walks.
	want, err := hex.DecodeString(
		"130000001c0000000100000002000000aabbcc00" +
			"110000001c0000000200000003000000dd000000")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
	if msgs := walkMonfile(t, got); len(msgs) != 2 {
		t.Errorf("walked %d messages, want 2", len(msgs))
	}
}
