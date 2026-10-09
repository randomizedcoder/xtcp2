package linuxio

import (
	"encoding/binary"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func rdmaBody() []byte {
	var body []byte
	for _, a := range [][]byte{attribute(1, word(0)), attribute(2, []byte("mlx5_0\x00")), attribute(3, word(1)), attribute(12, []byte{4}), attribute(13, []byte{5})} {
		body = append(body, a...)
	}
	return body
}

func TestRDMADecoderTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       func([]byte) []byte
		bad                                          bool
	}{
		{"valid", "positive", "complete host port with device index zero", "preserve zero index and ACTIVE/LINK_UP", func(b []byte) []byte { return b }, false},
		{"future", "corner", "unknown optional attribute", "known evidence retained", func(b []byte) []byte { return append(b, attribute(999, []byte{1})...) }, false},
		{"duplicate", "negative", "duplicate state", "reject even equal duplicate", func(b []byte) []byte { return append(b, attribute(12, []byte{4})...) }, true},
		{"width", "negative", "short index", "reject invalid scalar", func(b []byte) []byte { return append(attribute(1, []byte{0}), b[8:]...) }, true},
		{"truncated", "boundary", "truncated final attribute", "reject framing", func(b []byte) []byte { return b[:len(b)-4] }, true},
		{"missing", "negative", "empty body", "reject missing identifiers", func([]byte) []byte { return nil }, true},
		{"name63", "boundary", "63-byte RDMA device name", "accept maximum name", func(b []byte) []byte {
			return append(append(b[:8:8], attribute(2, []byte(strings.Repeat("x", 63)+"\x00"))...), b[20:]...)
		}, false},
		{"name64", "boundary", "64-byte RDMA device name", "reject oversized name", func(b []byte) []byte {
			return append(append(b[:8:8], attribute(2, []byte(strings.Repeat("x", 64)+"\x00"))...), b[20:]...)
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			body := tc.change(rdmaBody())
			p, err := decodeRDMAPort(body)
			if (err != nil) != tc.bad {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if !tc.bad {
				clear(body)
				if p.Name == "" || p.Index != 0 || p.Port != 1 || p.State != 4 || p.Physical != 5 {
					t.Fatalf("%s: %+v", tc.expectedOutcome, p)
				}
			}
		})
	}
}

func TestRDMATransactionTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		protocol                                     int
		mode                                         string
		bad                                          bool
	}{
		{"devices", "positive", "device enumeration", "complete owned dump", unix.NETLINK_RDMA, "devices", false},
		{"ports", "positive", "port enumeration", "complete owned dump", unix.NETLINK_RDMA, "ports", false},
		{"single", "positive", "single port query", "one requested port", unix.NETLINK_RDMA, "single", false},
		{"empty", "boundary", "empty completed dump", "successful empty inventory", unix.NETLINK_RDMA, "empty", false},
		{"wrongProtocol", "negative", "generic socket used for RDMA", "reject before send", unix.NETLINK_GENERIC, "ports", true},
		{"interrupted", "negative", "DUMP_INTR after valid data", "no partial values", unix.NETLINK_RDMA, "interrupted", true},
		{"wrongPort", "negative", "single reply identifies another port", "reject reply", unix.NETLINK_RDMA, "wrongPort", true},
		{"wrongSequence", "corner", "only stale sequence received", "no accepted reply", unix.NETLINK_RDMA, "wrongSequence", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			socket := &scriptSocket{}
			socket.onSend = func(data []byte) {
				seq, typ := binary.LittleEndian.Uint32(data[8:]), binary.LittleEndian.Uint16(data[4:])
				if tc.mode == "wrongSequence" {
					seq++
				}
				flags := uint16(unix.NLM_F_MULTI)
				if tc.mode == "single" || tc.mode == "wrongPort" {
					flags = 0
				}
				var frames [][]byte
				if tc.mode != "empty" {
					frames = append(frames, frame(typ, flags, seq, rdmaBody()))
				}
				if flags != 0 {
					if tc.mode == "interrupted" {
						flags = unix.NLM_F_DUMP_INTR
					} else {
						flags = 0
					}
					frames = append(frames, frame(unix.NLMSG_DONE, flags, seq, word(0)))
				}
				socket.batches = []batch{{data: frames}}
			}
			c := scriptedClient(tc.protocol, socket)
			t.Cleanup(func() {
				if err := c.Close(); err != nil {
					t.Error(err)
				}
			})
			var result Result[RDMAPort]
			var err error
			switch tc.mode {
			case "devices":
				result, err = c.DumpRDMADevices(t.Context())
			case "single":
				result, err = c.GetRDMAPort(t.Context(), 0, 1)
			case "wrongPort":
				result, err = c.GetRDMAPort(t.Context(), 0, 2)
			default:
				result, err = c.DumpRDMAPorts(t.Context(), 0)
			}
			if (err != nil) != tc.bad {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if (tc.bad || tc.mode == "empty") && len(result.Values) != 0 {
				t.Fatal("partial result")
			}
			if !tc.bad && tc.mode != "empty" && len(result.Values) != 1 {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func FuzzRDMAPort(f *testing.F) {
	f.Add(rdmaBody())
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := decodeRDMAPort(b)
		if err == nil && (len(p.Name) == 0 || len(p.Name) > 63) {
			t.Fatal("accepted invalid identity")
		}
	})
}
