package goip

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// requestHeaderOfType returns the 16 bytes of an nlmsghdr with nlmsg_type set
// and everything else zero.
//
// Hand-built rather than taken from a builder in internal/goip/req, because
// what ReplaySource.Dump reads out of the request is exactly two bytes and a
// length. Calling a real builder here would make the test depend on the
// request encoder's whole surface to assert a property of the replay source.
func requestHeaderOfType(typ uint16) []byte {
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst)
	binary.LittleEndian.PutUint32(b[0:4], uint32(len(b)))
	binary.LittleEndian.PutUint16(b[4:6], typ)
	binary.LittleEndian.PutUint16(b[6:8], uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP))
	return b
}

// TestReplaySourceDumpEmptyVsMissing covers the one thing ReplaySource.Dump
// cannot decide from its reply set alone: whether "no replies" means the dump
// came back empty or the fixture is the wrong capture.
//
// The distinction became reachable with the mesh `ip route show dev veth0`
// capture. That device owns no routes, so the dump is answered by NLMSG_DONE
// alone and the real `ip` prints nothing and exits 0. Before this, every empty
// dump was reported as ErrNoReplay, which made the corpus's only empty answer
// unusable as a fixture and would have done the same to any future one.
//
// The discriminator is the REQUEST: if the capture recorded a request of the
// type the caller is sending, the dump happened. See Dump's doc comment for
// why that is used in preference to deriving the GET type from msgType.
//
// go test ./internal/goip/ -run TestReplaySourceDumpEmptyVsMissing
func TestReplaySourceDumpEmptyVsMissing(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		request     []byte
		msgType     uint16
		wantLen     int
		wantErr     error
	}{
		{
			// The ordinary case, kept first so a failure below can be read as
			// a failure of the new rule rather than of the walk itself.
			description: "positive: a dump with replies returns every one of them",
			pcap:        routeDevPcap,
			request:     requestHeaderOfType(unix.RTM_GETROUTE),
			msgType:     unix.RTM_NEWROUTE,
			wantLen:     6,
		},
		{
			// The case this rule exists for. Four datagrams in the capture:
			// the by-name RTM_GETLINK, its reply, the RTM_GETROUTE dump
			// request, and NLMSG_DONE. The dump is real and its answer is
			// nothing.
			description: "boundary: an empty dump whose request IS recorded returns no bodies and no error",
			pcap:        routeMeshDevPcap,
			request:     requestHeaderOfType(unix.RTM_GETROUTE),
			msgType:     unix.RTM_NEWROUTE,
			wantLen:     0,
		},
		{
			// The case the old unconditional rule was right about, and which
			// must keep working: this capture is of `route show dev` and holds
			// no neighbor traffic at all, so asking it for neighbors is asking
			// the wrong file.
			description: "negative: a type the capture never carries, with no matching request, is a missing fixture",
			pcap:        routeDevPcap,
			request:     requestHeaderOfType(unix.RTM_GETNEIGH),
			msgType:     unix.RTM_NEWNEIGH,
			wantErr:     ErrNoReplay,
		},
		{
			// A caller with no request bytes to offer gets the old behavior,
			// because without the evidence there is nothing to distinguish the
			// two cases and "missing fixture" is the safer of the two to
			// report.
			description: "negative: a nil request falls back to treating an empty result as missing",
			pcap:        routeMeshDevPcap,
			request:     nil,
			msgType:     unix.RTM_NEWROUTE,
			wantErr:     ErrNoReplay,
		},
		{
			// One byte short of an nlmsghdr. Indexing [4:6] on this would
			// panic, so the length test is load-bearing and not defensive
			// decoration.
			description: "boundary: a request one byte short of an nlmsghdr is treated as absent, not read",
			pcap:        routeMeshDevPcap,
			request:     make([]byte, xtcpnl.NlMsgHdrSizeCst-1),
			msgType:     unix.RTM_NEWROUTE,
			wantErr:     ErrNoReplay,
		},
		{
			// The deliberate limit of the rule, stated rather than left to be
			// discovered. The discriminator is "was THIS request recorded",
			// not "is msgType the reply type for this request" — so a caller
			// that paired a GETROUTE request with a neighbor reply type gets
			// an empty result rather than ErrNoReplay.
			//
			// Tightening it would mean deriving the GET type from msgType,
			// which is sound (RTM_FAM depends on the NEW/DEL/GET/SET grouping,
			// include/uapi/linux/rtnetlink.h:211) but replaces direct evidence
			// with an assumption about the enum's layout. No production caller
			// mismatches the pair; every one of them builds the request and
			// names the reply type in the same statement.
			description: "corner: a mismatched request/msgType pair reports empty rather than missing, by design",
			pcap:        routeMeshDevPcap,
			request:     requestHeaderOfType(unix.RTM_GETROUTE),
			msgType:     unix.RTM_NEWNEIGH,
			wantLen:     0,
		},
		{
			// The first message of the `dev` captures is a single-get, not a
			// dump, and its reply is an RTM_NEWLINK. Included so the two
			// transactions in one capture are both shown to be reachable
			// through the same method.
			description: "positive: the single-get reply in the same capture is reachable by its own type",
			pcap:        routeDevPcap,
			request:     requestHeaderOfType(unix.RTM_GETLINK),
			msgType:     unix.RTM_NEWLINK,
			wantLen:     1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src, err := OpenReplay(tc.pcap)
			if err != nil {
				t.Fatalf("OpenReplay(%s): %v", tc.pcap, err)
			}
			got, err := src.Dump(tc.request, tc.msgType)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v (got %d bodies)", err, tc.wantErr, len(got))
				}
				if got != nil {
					t.Errorf("bodies = %d, want nil alongside the error", len(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Errorf("bodies = %d, want %d", len(got), tc.wantLen)
			}
		})
	}
}
