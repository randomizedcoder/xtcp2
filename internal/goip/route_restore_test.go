package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// rtaOf is one route attribute with a 4-byte value, 8 bytes in all.
func rtaOf(typ uint16, val [4]byte) []byte {
	b := []byte{8, 0, 0, 0, val[0], val[1], val[2], val[3]}
	binary.LittleEndian.PutUint16(b[2:4], typ)
	return b
}

// routeRecordOf is a saved record of type typ: table 100's rtmsg, then
// attrs, NLM_F_MULTI as a dump's reply carries it.
func routeRecordOf(t *testing.T, typ uint16, attrs ...[]byte) []byte {
	t.Helper()
	rtmsg := []byte{unix.AF_INET, 24, 0, 0, 100, unix.RTPROT_BOOT, 0, unix.RTN_UNICAST, 0, 0, 0, 0}
	b, err := xtcpnl.BuildWriteRequest(typ, unix.NLM_F_MULTI, 0, rtmsg, bytes.Join(attrs, nil))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var (
	rtaDst192   = rtaOf(unix.RTA_DST, [4]byte{192, 0, 2, 0})
	rtaDst198   = rtaOf(unix.RTA_DST, [4]byte{198, 51, 100, 0})
	rtaPref192  = rtaOf(unix.RTA_PREFSRC, [4]byte{192, 0, 2, 0})
	rtaPref1921 = rtaOf(unix.RTA_PREFSRC, [4]byte{192, 0, 2, 1})
	rtaGw       = rtaOf(unix.RTA_GATEWAY, [4]byte{192, 0, 2, 2})
	rtaOif3     = rtaOf(unix.RTA_OIF, [4]byte{3, 0, 0, 0})
)

// TestRouteRestorePrio is restore_handler's three classes, and what goip
// refuses to classify.
//
// go test ./internal/goip/ -run TestRouteRestorePrio
func TestRouteRestorePrio(t *testing.T) {
	tests := []struct {
		description string
		record      []byte
		expected    int
		wantErr     error
	}{
		{"positive: a route on a device, no gateway or source, is class 0",
			routeRecordOf(t, unix.RTM_NEWROUTE, rtaDst192, rtaOif3), 0, nil},
		{"positive: a source equal to the destination is class 0",
			routeRecordOf(t, unix.RTM_NEWROUTE, rtaDst192, rtaPref192), 0, nil},
		{"positive: another source is class 1",
			routeRecordOf(t, unix.RTM_NEWROUTE, rtaDst192, rtaPref1921), 1, nil},
		{"positive: a gateway is class 2", routeRecordOf(t, unix.RTM_NEWROUTE, rtaDst198, rtaGw, rtaOif3), 2, nil},
		{"corner: a gateway wins over a source", routeRecordOf(t, unix.RTM_NEWROUTE, rtaDst198, rtaPref192, rtaGw), 2, nil},
		{"corner: a source with no destination is class 1, rtattr_cmp's missing operand",
			routeRecordOf(t, unix.RTM_NEWROUTE, rtaPref192), 1, nil},
		{"corner: parse_rtattr keeps the first source, which equals the destination",
			routeRecordOf(t, unix.RTM_NEWROUTE, rtaDst192, rtaPref192, rtaPref1921), 0, nil},
		{"boundary: the rtmsg alone is class 0", routeRecordOf(t, unix.RTM_NEWROUTE), 0, nil},
		{"negative: an address record is not modeled", cat(routeHdr(24, unix.RTM_NEWADDR), dumpIfa(3)), 0, ErrNotImplemented},
		{"negative: an RTM_DELROUTE record is not modeled", routeRecordOf(t, unix.RTM_DELROUTE, rtaDst192), 0, ErrNotImplemented},
		{"negative: a body shorter than struct rtmsg", append(routeHdr(20, unix.RTM_NEWROUTE), 2, 24, 0, 0), 0, ErrNotImplemented},
		{"negative: an attribute longer than the record", routeRecordOf(t, unix.RTM_NEWROUTE, []byte{40, 0, 1, 0}), 0, ErrNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var h xtcpnl.NlMsgHdr
			if _, err := xtcpnl.DeserializeNlMsgHdr(tc.record, &h); err != nil {
				t.Fatal(err)
			}
			got, err := routeRestorePrio(h, tc.record[16:])
			if !errors.Is(err, tc.wantErr) || got != tc.expected {
				t.Errorf("prio %d, err %v; want %d, %v", got, err, tc.expected, tc.wantErr)
			}
		})
	}
}

// TestRouteRestoreMatchesIp replays route_restore's capture: goip sends ip's
// one link dump, then writes ip's two writes, the gateway route's after the
// device route's though the file holds them the other way round, message by
// message with seq and pid zeroed, each 0x427 and the link dump's seq plus
// one and two, and exits 0 with nothing printed, as ip did.
//
// go test ./internal/goip/ -run TestRouteRestoreMatchesIp
func TestRouteRestoreMatchesIp(t *testing.T) {
	pcap := routeRtnlDir + "route_restore/ip.pcap"
	capt, err := nlparity.ParseRouteCaptureFile(pcap)
	if err != nil {
		t.Fatal(err)
	}
	reqs := capt.Requests()
	ipReqs := make([][]byte, 0, len(reqs))
	for _, m := range reqs {
		ipReqs = append(ipReqs, wireBytesOf(m))
	}
	if len(ipReqs) != 3 {
		t.Fatalf("capture holds %d requests, want 3", len(ipReqs))
	}

	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := &restoreReplay{linkReplay: newLinkReplay(t, pcap)}
	in := bytes.NewReader(readRtnlFile(t, "route_restore", "ip.save"))
	rc, stdout, stderr := runRouteCtx(&runCtx{src: src, in: in}, []string{"restore"})
	if rc != 0 || stdout != "" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	if len(src.dumpReqs) != 1 || len(src.gets) != 0 || !bytes.Equal(src.dumpReqs[0], ipReqs[0]) {
		t.Fatalf("goip sent %d dumps, %d gets; link dump %x, want %x", len(src.dumpReqs), len(src.gets), src.dumpReqs, ipReqs[0])
	}
	writes := restoreWrites(t, out)
	if len(writes) != 2 {
		t.Fatalf("goip wrote %d messages, want 2", len(writes))
	}
	seq := src.dumpSeqs[0]
	for i, w := range writes {
		seq++
		if got, want := wireBytesOf(w), ipReqs[1+i]; !bytes.Equal(got, want) {
			t.Errorf("write %d:\n got %x\nwant %x", i, got, want)
		}
		if w.Hdr.Flags != 0x427 || w.Hdr.Seq != seq {
			t.Errorf("write %d flags %#x seq %d, want 0x427 and %d", i, w.Hdr.Flags, w.Hdr.Seq, seq)
		}
	}
}

// TestRouteRestore is the pass order and the refusals, over synthetic files.
//
// go test ./internal/goip/ -run TestRouteRestore$
func TestRouteRestore(t *testing.T) {
	magic := []byte{0x24, 0x12, 0x31, 0x45}
	class0 := routeRecordOf(t, unix.RTM_NEWROUTE, rtaDst192, rtaOif3)
	class1 := routeRecordOf(t, unix.RTM_NEWROUTE, rtaDst192, rtaPref1921, rtaOif3)
	class2 := routeRecordOf(t, unix.RTM_NEWROUTE, rtaDst198, rtaGw, rtaOif3)
	type result struct {
		rc     int
		dumps  int
		writes string // each write's class, in order
	}
	classOf := map[string]string{string(class0[16:]): "0", string(class1[16:]): "1", string(class2[16:]): "2"}
	tests := []struct {
		description string
		in          []byte
		noHook      bool
		expected    result
	}{
		{"positive: the classes go in 0, 1, 2, whatever the file's order",
			cat(magic, class2, class1, class0), false, result{dumps: 1, writes: "012"}},
		{"positive: two of a class keep the file's order", cat(magic, class2, class0, class2, class0), false,
			result{dumps: 1, writes: "0022"}},
		{"boundary: the magic alone sends nothing and needs no hook", magic, true, result{}},
		{"negative: records with no GOIP_WRITE_OUT send nothing", cat(magic, class0), true, result{rc: 1}},
		{"negative: a bad magic sends nothing, exit 1", cat([]byte{0, 0, 0, 0}, class0), false, result{rc: 1}},
		{"negative: a truncated record ends the first pass: class 0 before it goes, exit 2",
			cat(magic, class2, class0, class1[:20]), false, result{rc: 2, dumps: 1, writes: "0"}},
		{"corner: a truncated first record sends nothing, exit 2", cat(magic, class0[:20]), false, result{rc: 2}},
		{"negative: an address record is refused before anything is sent", cat(magic, class0, cat(routeHdr(24, unix.RTM_NEWADDR), dumpIfa(3))), false,
			result{rc: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			src := &restoreReplay{linkReplay: newLinkReplay(t, routeRtnlDir+"route_restore/ip.pcap")}
			rc, _, stderr := runRouteCtx(&runCtx{src: src, in: bytes.NewReader(tc.in)}, []string{"restore"})
			got := result{rc: rc, dumps: len(src.dumpReqs)}
			if got.dumps > 0 {
				for _, w := range restoreWrites(t, out) {
					got.writes += classOf[string(w.Body)]
				}
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v; stderr %q", got, tc.expected, stderr)
			}
		})
	}

	t.Run("negative: a failed link dump is ll_init_map's Dump terminated, exit 1, nothing written", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "write.bin")
		t.Setenv(writeOutEnvCst, out)
		src := &failSource{}
		rc, _, stderr := runRouteCtx(&runCtx{src: src, in: bytes.NewReader(cat(magic, class0))}, []string{"restore"})
		if rc != ExitUsage || src.requests != 1 || !strings.Contains(stderr, "Dump terminated") || writtenFile(out) {
			t.Errorf("rc %d, %d requests, stderr %q", rc, src.requests, stderr)
		}
	})
}

// writtenFile reports whether goip wrote GOIP_WRITE_OUT at path.
func writtenFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
