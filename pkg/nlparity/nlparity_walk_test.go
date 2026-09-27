package nlparity

import (
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// walkRow is one row of the WalkDatagram table.
//
// Provenance is enforced by the type: filename+record names a real nlmon
// capture, input carries constructed bytes, and exactly one of the two must be
// set. A row whose description begins "positive:" may not use input at all —
// the standing rule in this repo is that positive netlink expectations come
// from captured traffic, and constructed bytes exist for truncation, malformed
// lengths and boundaries. checkRowProvenance fails the row before any decoding
// happens, so a table cannot drift away from that rule quietly.
type walkRow struct {
	description string

	filename string // real capture; mutually exclusive with input
	record   int    // NETLINK_ROUTE record index within filename
	input    []byte // constructed bytes; negative/boundary/corner only

	// sidecar names the evidence an expectation is derived from, so a reader can
	// check the number rather than trust it.
	sidecar string

	wantMsgs        int
	wantFirstLen    uint32   // 0 = not asserted
	wantTypes       []uint16 // nil = not asserted
	wantTail        int
	wantTailAllZero bool
	wantTailFlagged bool
	wantErr         error
}

// classOf returns the leading "positive"/"negative"/"boundary"/"corner" token of
// a description.
func classOf(description string) string {
	for i := 0; i < len(description); i++ {
		if description[i] == ':' {
			return description[:i]
		}
	}
	return ""
}

// checkRowProvenance enforces the two invariants described on walkRow.
func checkRowProvenance(t *testing.T, description, filename string, input []byte) {
	t.Helper()

	switch {
	case filename != "" && input != nil:
		t.Fatalf("row %q sets both filename and input; they are mutually exclusive", description)
	case filename == "" && input == nil:
		t.Fatalf("row %q sets neither filename nor input", description)
	}

	if classOf(description) == "positive" && input != nil {
		t.Fatalf("row %q is positive but uses constructed bytes; positive netlink "+
			"expectations must come from a real capture", description)
	}
}

// TestWalkDatagram is the tolerant walker's table. It is the reason this package
// does not reuse xtcpnl's WalkNlMsgs: rows 4 through 7 are all datagrams that
// WalkNlMsgs rejects with ErrBadMsgLen, and three of iproute2's four dump
// commands produce them on every single run.
//
// go test ./pkg/nlparity/ -run TestWalkDatagram
func TestWalkDatagram(t *testing.T) {
	ifinfo := make([]byte, xtcpnl.IfInfomsgSizeCst)
	ifinfo[0] = unix.AF_PACKET

	// A complete, well-formed 32-byte RTM_GETLINK message, used as the good
	// first message that the later rows hang a broken second message off.
	goodFirst := wellFormed(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), ifinfo)

	// A 16-byte header claiming nlmsg_len 0. Type is non-zero so the tail is not
	// all-zero and is therefore flagged rather than looking like an oversend.
	zeroLenHdr := nlmsg(0, 0x03e7, 0, 1, 0, nil)

	tests := []walkRow{
		{
			description:  "positive: `ip link show` sends exactly nlmsg_len, so there is no tail",
			filename:     tdBulkGetLink,
			record:       0,
			sidecar:      "lib/libnetlink.c:618 rtnl_linkdump_req_filter_fn sends n.nlmsg_len",
			wantMsgs:     1,
			wantFirstLen: 40,
			wantTypes:    []uint16{uint16(unix.RTM_GETLINK)},
			// An empty tail is vacuously all-zero; see Datagram.TailAllZero.
			wantTailAllZero: true,
		},
		{
			description:  "positive: `ip route show table all` oversends a 128-byte zeroed buffer",
			filename:     tdBulkGetRoute,
			record:       0,
			sidecar:      "lib/libnetlink.c rtnl_routedump_req: char buf[128], sends sizeof(req)",
			wantMsgs:     1,
			wantFirstLen: 28,
			wantTypes:    []uint16{uint16(unix.RTM_GETROUTE)},
			// 156 captured bytes, 28 of them the message.
			wantTail:        128,
			wantTailAllZero: true,
		},
		{
			description:     "positive: a multipart link dump is 11 RTM_NEWLINK then NLMSG_DONE",
			filename:        tdGetLinkDump,
			record:          0,
			sidecar:         "pkg/xtcpnl/testdata/7_1_8/ip_link_n lists 11 links",
			wantMsgs:        12,
			wantFirstLen:    796,
			wantTailAllZero: true,
		},
		{
			description: "boundary: 24-byte RTM_GETADDR plus 128 zero bytes is one message and a tail",
			input:       concat(getaddrRequest(unix.AF_INET), zeros(128)),
			sidecar:     "lib/libnetlink.c rtnl_addrdump_req: char buf[128]",
			wantMsgs:    1,
			// NOT an error. xtcpnl's WalkNlMsgs returns ErrBadMsgLen here, which
			// is the entire reason this walker exists.
			wantFirstLen:    24,
			wantTypes:       []uint16{uint16(unix.RTM_GETADDR)},
			wantTail:        128,
			wantTailAllZero: true,
		},
		{
			description:     "boundary: a 256-byte tail, the largest iproute2 oversend",
			input:           concat(goodFirst, zeros(256)),
			sidecar:         "lib/libnetlink.c rtnl_neighdump_req: char buf[256]",
			wantMsgs:        1,
			wantTail:        256,
			wantTailAllZero: true,
		},
		{
			description:     "boundary: a trailing remainder shorter than a header is still a tail",
			input:           concat(goodFirst, zeros(15)),
			wantMsgs:        1,
			wantTail:        15,
			wantTailAllZero: true,
		},
		{
			description: "boundary: the final message's alignment padding may be absent",
			// nlmsg_len 21 is not 4-aligned, and the 3 pad bytes were never sent.
			// Consuming to the end is what the kernel's NLMSG_OK walk does; the
			// alternative would report a 0-byte tail as a 3-byte one.
			input:           nlmsg(21, uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), 1, 0, zeros(5)),
			wantMsgs:        1,
			wantFirstLen:    21,
			wantTailAllZero: true,
		},
		{
			description: "negative: nlmsg_len below a bare header in the FIRST message",
			input:       nlmsg(15, uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), 1, 0, ifinfo),
			wantErr:     ErrBadHead,
		},
		{
			description: "negative: nlmsg_len overrunning the datagram in the FIRST message",
			input:       nlmsg(4096, uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), 1, 0, ifinfo),
			wantErr:     ErrBadHead,
		},
		{
			description: "negative: nlmsg_len overrunning in a LATER message is a flagged tail, not an error",
			input: concat(goodFirst,
				nlmsg(4096, uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), 1, 0, ifinfo)),
			wantMsgs: 1,
			// 32 bytes of good first message consumed, the broken 32-byte
			// second message left as tail.
			wantTail:        32,
			wantTailFlagged: true,
		},
		{
			description: "negative: a datagram shorter than a bare nlmsghdr",
			input:       zeros(15),
			wantErr:     ErrShortDatagram,
		},
		{
			description:     "corner: nlmsg_len 0 in a later message terminates the walk instead of looping",
			input:           concat(goodFirst, zeroLenHdr),
			wantMsgs:        1,
			wantTail:        16,
			wantTailFlagged: true,
		},
		{
			description:     "corner: a non-zero tail is flagged, so truncation is not mistaken for oversend",
			input:           concat(goodFirst, []byte{0xde, 0xad, 0xbe, 0xef}, zeros(124)),
			wantMsgs:        1,
			wantTail:        128,
			wantTailAllZero: false,
			wantTailFlagged: true,
		},
		{
			description: "corner: NLMSG_DONE does not end the walk, so anything after it is still seen",
			input: concat(
				wellFormed(uint16(unix.NLMSG_DONE), uint16(unix.NLM_F_MULTI), zeros(4)),
				wellFormed(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_MULTI), ifinfo),
			),
			wantMsgs: 2,
			wantTypes: []uint16{
				uint16(unix.NLMSG_DONE), uint16(unix.RTM_NEWLINK),
			},
			wantTailAllZero: true,
		},
		{
			description: "corner: NLMSG_ERROR is returned as a message, not as the walk's error",
			input: wellFormed(uint16(unix.NLMSG_ERROR), 0,
				// -ENODEV, as the kernel encodes it.
				[]byte{0xed, 0xff, 0xff, 0xff}),
			wantMsgs:        1,
			wantTypes:       []uint16{uint16(unix.NLMSG_ERROR)},
			wantTailAllZero: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			checkRowProvenance(t, tc.description, tc.filename, tc.input)
			if tc.sidecar != "" {
				// Printed on failure, so a wrong expectation points at the
				// evidence it was derived from rather than at nothing.
				t.Logf("expectation derived from: %s", tc.sidecar)
			}

			data := tc.input
			if tc.filename != "" {
				data = routeDatagram(t, tc.filename, tc.record)
			}

			got, err := WalkDatagram(uint16(unix.NETLINK_ROUTE), data)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			if len(got.Msgs) != tc.wantMsgs {
				t.Errorf("messages = %d, want %d", len(got.Msgs), tc.wantMsgs)
			}
			if got.Len != len(data) {
				t.Errorf("Len = %d, want %d (the captured datagram size)", got.Len, len(data))
			}
			if tc.wantFirstLen != 0 {
				if len(got.Msgs) == 0 {
					t.Fatalf("no messages, cannot check the first nlmsg_len")
				}
				if got.Msgs[0].Hdr.Len != tc.wantFirstLen {
					t.Errorf("first nlmsg_len = %d, want %d", got.Msgs[0].Hdr.Len, tc.wantFirstLen)
				}
			}
			for i, want := range tc.wantTypes {
				if i >= len(got.Msgs) {
					t.Errorf("message[%d] missing, want type %d", i, want)
					continue
				}
				if got.Msgs[i].Hdr.Type != want {
					t.Errorf("message[%d] type = %d, want %d", i, got.Msgs[i].Hdr.Type, want)
				}
			}
			if got.TailBytes != tc.wantTail {
				t.Errorf("TailBytes = %d, want %d", got.TailBytes, tc.wantTail)
			}
			if got.TailAllZero != tc.wantTailAllZero {
				t.Errorf("TailAllZero = %v, want %v", got.TailAllZero, tc.wantTailAllZero)
			}
			if got.TailFlagged != tc.wantTailFlagged {
				t.Errorf("TailFlagged = %v, want %v", got.TailFlagged, tc.wantTailFlagged)
			}
		})
	}
}

// TestWalkDatagramAdvances is the loop-termination property stated as a
// property rather than as a row: no input, however malformed, may make
// WalkDatagram fail to return. The corner row above pins one specific case;
// this pins the class.
//
// go test ./pkg/nlparity/ -run TestWalkDatagramAdvances
func TestWalkDatagramAdvances(t *testing.T) {
	tests := []struct {
		description string
		input       []byte
	}{
		{"boundary: exactly one bare header, nlmsg_len 16, no body", nlmsg(16, 0, 0, 0, 0, nil)},
		{"corner: every nlmsg_len 0, sixteen headers deep", func() []byte {
			var out []byte
			for i := 0; i < 16; i++ {
				out = append(out, nlmsg(0, 0x03e7, 0, 0, 0, nil)...)
			}
			return out
		}()},
		{"corner: 1 KiB of 0xff, so every length field is 0xffffffff", func() []byte {
			b := make([]byte, 1024)
			for i := range b {
				b[i] = 0xff
			}
			return b
		}()},
		{"corner: 1 KiB of zeros, so every length field is 0", zeros(1024)},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			// A hang fails via the go test timeout; the assertion here is that
			// the walker returns one of exactly two outcomes and never panics.
			d, err := WalkDatagram(uint16(unix.NETLINK_ROUTE), tc.input)
			if err != nil {
				if !IsBadCapture(err) {
					t.Fatalf("err = %v, want ErrBadHead or ErrShortDatagram", err)
				}
				return
			}
			if len(d.Msgs) == 0 && d.TailBytes != len(tc.input) {
				t.Errorf("no messages but TailBytes = %d, want %d: bytes went missing",
					d.TailBytes, len(tc.input))
			}
		})
	}
}
