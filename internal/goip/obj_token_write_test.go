//go:build goip_write

package goip

import (
	"bytes"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestTokenSet is iptoken_set on d0 (3) from the fake's link dump: the
// written RTM_SETLINK's ifinfomsg and attributes in hex, or the refusal.
//
// go test ./internal/goip/ -run TestTokenSet
func TestTokenSet(t *testing.T) {
	const (
		hdr     = "0a00000003000000000000000000000000" // AF_INET6, ifindex 3
		nest    = "1c001a0018000a00140007" + "00"      // AF_SPEC { AF_INET6 { TOKEN, 16 bytes } }
		tok1234 = "00000000000000000001000200030004"
		zero    = "00000000000000000000000000000000"
	)
	set := func(tok string) string { return hdr[:32] + nest + tok }
	type result struct {
		write string
		err   error
	}
	tests := []struct {
		description string
		noHook      bool
		args        string
		expected    result
	}{
		{description: "positive: set, the prefix length not sent", args: "set ::1:2:3:4/64 dev d0",
			expected: result{write: set(tok1234)}},
		{description: "positive: `se`, the device first", args: "se dev d0 ::1:2:3:4", expected: result{write: set(tok1234)}},
		{description: "positive: add is set", args: "add ::1:2:3:4 dev d0", expected: result{write: set(tok1234)}},
		{description: "positive: delete is ::", args: "delete dev d0", expected: result{write: set(zero)}},
		{description: "corner: delete reads no word as the token", args: "d ::5 dev d0", expected: result{write: set(zero)}},
		{description: "corner: the first token wins", args: "set ::1:2:3:4 ::9 dev d0", expected: result{write: set(tok1234)}},
		{description: "corner: a second dev is not even looked up", args: "set ::1:2:3:4 dev d0 dev nope",
			expected: result{write: set(tok1234)}},
		{description: "boundary: default is sixteen zero bytes", args: "set default dev d0", expected: result{write: set(zero)}},
		{description: "boundary: any is an empty token", args: "set any dev d0",
			expected: result{write: hdr[:32] + "0c001a0008000a0004000700"}},
		{description: "negative: no token", args: "set dev d0", expected: result{err: ErrInvalidArgument}},
		{description: "negative: `de` is not dev, strcmp, so no dev", args: "set ::1 de d0",
			expected: result{err: ErrInvalidArgument}},
		{description: "negative: no dev", args: "set ::1", expected: result{err: ErrInvalidArgument}},
		{description: "negative: dev of no device", args: "set ::1 dev nope", expected: result{err: ErrInvalidArgument}},
		{description: "negative: dev with no name", args: "set ::1 dev", expected: result{err: ErrInvalidArgument}},
		{description: "negative: a v4 token", args: "set 1.2.3.4 dev d0", expected: result{err: ErrInvalidArgument}},
		{description: "negative: a length past 128", args: "set ::1/129 dev d0", expected: result{err: ErrInvalidArgument}},
		{description: "negative: `h` after the token is still help", args: "set ::1 dev d0 h",
			expected: result{err: ErrNotImplemented}},
		{description: "negative: help on delete too", args: "del help", expected: result{err: ErrNotImplemented}},
		{description: "negative: no GOIP_WRITE_OUT", noHook: true, args: "set ::1 dev d0",
			expected: result{err: ErrWriteNotApplied}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			_, _, err := runTokenCtx(&tokenFake{}, false, tc.args)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			got := result{err: tc.expected.err}
			if err == nil {
				writes := restoreWrites(t, out)
				if len(writes) != 1 || writes[0].Hdr.Type != unix.RTM_SETLINK ||
					writes[0].Hdr.Flags != unix.NLM_F_REQUEST|unix.NLM_F_ACK {
					t.Fatalf("wrote %+v, want one RTM_SETLINK, REQUEST|ACK", writes)
				}
				got.write = hex.EncodeToString(writes[0].Body)
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestTokenSetMatchesIp replays the two C1-T02 token writes: goip sends
// ll_init_map's link dump, then writes ip's RTM_SETLINK byte for byte. The
// noarp capture's kernel refused it; the request is the same.
//
// go test ./internal/goip/ -run TestTokenSetMatchesIp
func TestTokenSetMatchesIp(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		args        string
		expected    uint16 // the write's flags
	}{
		{"positive: set on a veth", "token_set", "set ::1:2:3:4/64 dev d0", unix.NLM_F_REQUEST | unix.NLM_F_ACK},
		{"negative: set on a dummy, which the kernel refuses, is the same request", "token_set_noarp",
			"set ::1:2:3:4/64 dev d0", unix.NLM_F_REQUEST | unix.NLM_F_ACK},
		{"positive: `a`, the device first, no prefix length", "token_set", "a dev d0 ::1:2:3:4",
			unix.NLM_F_REQUEST | unix.NLM_F_ACK},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ipReqs := rtnlRequests(t, tc.slug)
			if len(ipReqs) != 2 {
				t.Fatalf("capture holds %d requests, want 2", len(ipReqs))
			}
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newStatsReplay(t, routeRtnlDir+tc.slug+"/ip.pcap")
			stdout, _, err := runTokenCtx(src, false, tc.args)
			if err != nil || stdout != "" {
				t.Fatalf("err %v, stdout %q", err, stdout)
			}
			if len(src.sent) != 1 || !bytes.Equal(src.sent[0], ipReqs[0]) {
				t.Fatalf("goip sent %x, want %x alone", src.sent, ipReqs[0])
			}
			writes := restoreWrites(t, out)
			if len(writes) != 1 {
				t.Fatalf("goip wrote %d messages, want 1", len(writes))
			}
			if got := capturedRequest(writes[0]); !bytes.Equal(got, ipReqs[1]) {
				t.Errorf("write:\n got %x\nwant %x", got, ipReqs[1])
			}
			if writes[0].Hdr.Flags != tc.expected {
				t.Errorf("flags %#x, want %#x", writes[0].Hdr.Flags, tc.expected)
			}
		})
	}
}
