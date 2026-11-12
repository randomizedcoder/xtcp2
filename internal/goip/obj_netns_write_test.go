//go:build goip_write

package goip

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestNetnsSet is netns_set: its checks in order, then the RTM_NEWNSID's
// NETNSA_NSID, or the refusal.
//
// go test ./internal/goip/ -run TestNetnsSet$
func TestNetnsSet(t *testing.T) {
	type result struct {
		nsid string
		err  error
	}
	tests := []struct {
		description string
		noHook      bool
		args        string
		// errText is a fragment the error must hold, where the error alone
		// does not say which refusal it is.
		errText  string
		expected result
	}{
		{description: "positive: an nsid", args: "set n 7", expected: result{nsid: "7"}},
		{description: "positive: auto is -1", args: "set n auto", expected: result{nsid: "-1"}},
		{description: "positive: hex, get_integer base 0", args: "set n 0x10", expected: result{nsid: "16"}},
		{description: "boundary: 0", args: "set n 0", expected: result{nsid: "0"}},
		{description: "boundary: INT_MAX", args: "set n 2147483647", expected: result{nsid: "2147483647"}},
		{description: "corner: a path with a slash is no name, ahead of the open",
			args: "set PATH 3", expected: result{err: ErrInvalidArgument}},
		{description: "negative: no name", args: "set", errText: "No netns name specified",
			expected: result{err: ErrInvalidArgument}},
		{description: "negative: no nsid", args: "set n", errText: "No nsid specified",
			expected: result{err: ErrInvalidArgument}},
		{description: "negative: `Auto` is not auto", args: "set n Auto", errText: `Invalid "netnsid" value`,
			expected: result{err: ErrInvalidArgument}},
		{description: "boundary: -1 is refused; only auto is -1", args: "set n -1",
			errText: `"netnsid" value should be >= 0`, expected: result{err: ErrInvalidArgument}},
		{description: "boundary: past INT_MAX", args: "set n 2147483648", expected: result{err: ErrInvalidArgument}},
		{description: "negative: no such file", args: "set gone 1", expected: result{err: ErrInvalidArgument}},
		{description: "negative: no GOIP_WRITE_OUT", noHook: true, args: "set n 1",
			expected: result{err: ErrWriteNotApplied}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := netnsDir(t, "n")
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			// PATH is the absolute path of n, for the slash row.
			args := strings.ReplaceAll(tc.args, "PATH", filepath.Join(dir, "n"))
			_, err := runNetnsCtx(&nsidFake{}, false, args)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			if tc.errText != "" && !strings.Contains(err.Error(), tc.errText) {
				t.Errorf("err = %v, want it to hold %s", err, tc.errText)
			}
			got := result{err: tc.expected.err}
			if err == nil {
				writes := restoreWrites(t, out)
				if len(writes) != 1 || writes[0].Hdr.Type != unix.RTM_NEWNSID {
					t.Fatalf("wrote %+v, want one RTM_NEWNSID", writes)
				}
				ni, perr := xtcpnl.ParseNsid(writes[0].Body)
				if perr != nil || !ni.HasFD || !ni.HasNSID {
					t.Fatalf("ParseNsid: %+v, %v", ni, perr)
				}
				got.nsid = fmt.Sprint(ni.NSID)
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestNetnsSetMatchesIp replays link-write/rtnl/netns_set: goip writes ip's
// RTM_NEWNSID, its descriptor aside, and sends nothing.
//
// go test ./internal/goip/ -run TestNetnsSetMatchesIp
func TestNetnsSetMatchesIp(t *testing.T) {
	ipReqs := rtnlRequests(t, "netns_set")
	if len(ipReqs) == 0 {
		t.Fatal("capture holds no requests")
	}
	tests := []struct {
		description string
		args        string
		expected    []byte
	}{
		{"positive: set c1set 7", "set c1set 7", ipReqs[0]},
		{"boundary: `s` is show, not set", "s c1set 7", nil},
		{"positive: `se` is set, and a word after the nsid is ignored", "se c1set 7 extra", ipReqs[0]},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			netnsDir(t, "c1set")
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := &nsidFake{}
			_, err := runNetnsCtx(src, false, tc.args)
			if tc.expected == nil {
				if !errors.Is(err, ErrNotImplemented) {
					t.Fatalf("err = %v, want ErrNotImplemented", err)
				}
				return
			}
			if err != nil || len(src.sent) != 0 {
				t.Fatalf("err %v, sent %d", err, len(src.sent))
			}
			writes := restoreWrites(t, out)
			if len(writes) != 1 {
				t.Fatalf("goip wrote %d messages, want 1", len(writes))
			}
			got, want := capturedRequest(writes[0]), xtcpnl.CopyBytes(tc.expected)
			// NETNSA_FD, the first attribute, is a descriptor of each process.
			copy(got[netnsFDOffCst:netnsFDOffCst+4], []byte{0, 0, 0, 0})
			copy(want[netnsFDOffCst:netnsFDOffCst+4], []byte{0, 0, 0, 0})
			if !bytes.Equal(got, want) || writes[0].Hdr.Flags != unix.NLM_F_REQUEST|unix.NLM_F_ACK {
				t.Errorf("write %x flags %#x\nwant  %x", got, writes[0].Hdr.Flags, want)
			}
		})
	}
}
