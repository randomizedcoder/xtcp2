package linuxio

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func devlinkBody() []byte {
	body := []byte{7, 1, 0, 0}
	for _, a := range [][]byte{attribute(1, []byte("pci\x00")), attribute(2, []byte("0000:01:00.0\x00")), attribute(3, word(0)), attribute(6, word(1)), attribute(77, []byte{0, 0})} {
		body = append(body, a...)
	}
	return body
}

func TestDevlinkPortDecoding(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       func([]byte) []byte
		wantError                                    bool
	}{
		{"physical", "positive", "complete physical port with zero index", "retain present zero flavor and port index", func(b []byte) []byte { return b }, false},
		{"duplicate", "negative", "duplicate known attribute", "reject conflicting evidence", func(b []byte) []byte { return append(b, attribute(6, word(2))...) }, true},
		{"width", "boundary", "flavor has one byte", "reject short scalar", func(b []byte) []byte { return append(b[:len(b)-8], attribute(77, []byte{1})...) }, true},
		{"unknown", "corner", "future attribute present", "ignore unknown optional attribute", func(b []byte) []byte { return append(b, attribute(999, []byte{1})...) }, false},
		{"version", "negative", "unsupported generic version", "reject response", func(b []byte) []byte { b[1] = 2; return b }, true},
		{"missing", "negative", "required device identity absent", "reject incomplete identity", func(b []byte) []byte { return b[:4] }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			p, matched, err := matchDevlinkPort(xtcpnl.NetlinkEnvelope{Body: tc.change(devlinkBody())})
			if (err != nil) != tc.wantError || matched == tc.wantError {
				t.Fatalf("%s: matched %v err %v", tc.expectedOutcome, matched, err)
			}
			if !tc.wantError && (!p.HasFlavor || p.Flavor != 0 || p.Netdev != 1 || p.Index != 0) {
				t.Fatalf("%s: %+v", tc.expectedOutcome, p)
			}
		})
	}
}

func TestDevlinkDumpTransaction(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		interrupted                                  bool
	}{
		{"complete", "positive", "discovery and complete multipart port dump", "owned port returned after DONE", false},
		{"interrupted", "corner", "dump interrupted after a port", "reject whole candidate", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			socket := &scriptSocket{}
			socket.onSend = func(data []byte) {
				seq := binary.LittleEndian.Uint32(data[8:])
				typ := binary.LittleEndian.Uint16(data[4:])
				if typ == xtcpnl.GenlControllerID {
					socket.batches = []batch{{data: [][]byte{frame(typ, 0, seq, familyBody(30, "devlink", 1))}}}
					return
				}
				if data[16] != 5 || binary.LittleEndian.Uint16(data[6:]) != unix.NLM_F_REQUEST|unix.NLM_F_DUMP {
					t.Fatal("not a read-only port dump")
				}
				flags := uint16(0)
				if tc.interrupted {
					flags = unix.NLM_F_DUMP_INTR
				}
				socket.batches = []batch{{data: [][]byte{frame(typ, unix.NLM_F_MULTI, seq, devlinkBody()), frame(unix.NLMSG_DONE, flags, seq, word(0))}}}
			}
			c := scriptedClient(unix.NETLINK_GENERIC, socket)
			t.Cleanup(func() {
				if err := c.Close(); err != nil {
					t.Error(err)
				}
			})
			family, err := c.DiscoverFamily(t.Context(), "devlink")
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.DumpDevlinkPorts(t.Context(), family)
			if tc.interrupted {
				if !errors.Is(err, ErrInterrupted) || len(result.Values) != 0 {
					t.Fatalf("%s: %v", tc.expectedOutcome, err)
				}
			} else if err != nil || len(result.Values) != 1 {
				t.Fatalf("%s: %+v %v", tc.expectedOutcome, result, err)
			}
		})
	}
}
