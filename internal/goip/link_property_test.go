package goip

import (
	"bytes"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestLinkPropertyMatchesIp replays the three link_altname captures: goip
// sends ip's by-name link get, then writes ip's RTM_NEWLINKPROP or
// RTM_DELLINKPROP, byte for byte with seq and pid zeroed, and prints nothing.
//
// go test ./internal/goip/ -run TestLinkPropertyMatchesIp
func TestLinkPropertyMatchesIp(t *testing.T) {
	addFlags := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_EXCL | unix.NLM_F_CREATE | unix.NLM_F_APPEND)
	tests := []struct {
		description string
		slug        string
		args        string
		expected    uint16 // the write's flags
	}{
		{"positive: add an altname", "link_altname_add", "property add dev d0 altname d0alt", addFlags},
		{"positive: del the altname the Setup added", "link_altname_del", "property del dev d0 altname d0alt",
			unix.NLM_F_REQUEST | unix.NLM_F_ACK},
		{"negative: an altname another link holds is the same request", "link_altname_taken",
			"property add dev d0 altname lo", addFlags},
		{"positive: `p`, `a`, the device before the altname, `alt`", "link_altname_add",
			"p a d0 alt d0alt", addFlags},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ipReqs := rtnlRequests(t, tc.slug)
			if len(ipReqs) != 2 {
				t.Fatalf("capture holds %d requests, want 2", len(ipReqs))
			}
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newRuleReplay(t, tc.slug)
			stdout, err := runLinkWith(t, src, unix.AF_UNSPEC, strings.Fields(tc.args))
			if err != nil || stdout != "" {
				t.Fatalf("err %v, stdout %q", err, stdout)
			}
			if len(src.gets) != 1 || !bytes.Equal(src.gets[0], ipReqs[0]) || len(src.dumpReqs) != 0 {
				t.Fatalf("goip sent gets %x and %d dumps, want %x alone", src.gets, len(src.dumpReqs), ipReqs[0])
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

// TestParseLinkPropArgs is iplink_prop_mod's loop and its check for a
// device. attrs is the IFLA_PROP_LIST nest in hex.
//
// go test ./internal/goip/ -run TestParseLinkPropArgs
func TestParseLinkPropArgs(t *testing.T) {
	type result struct {
		dev   string
		attrs string
		err   error
	}
	long := strings.Repeat("y", 127)
	tests := []struct {
		description string
		args        string
		expected    result
	}{
		{"positive: dev and one altname", "dev d0 altname d0alt",
			result{dev: "d0", attrs: "100034800a0035006430616c74000000"}},
		{"positive: the device without dev, after the altname", "altname d0alt d0",
			result{dev: "d0", attrs: "100034800a0035006430616c74000000"}},
		{"positive: two altnames, in argument order", "dev d0 altname x altname yz",
			result{dev: "d0", attrs: "1400348006003500780000000700350079" + "7a0000"}},
		{"positive: `a` abbreviates altname", "dev d0 a x", result{dev: "d0", attrs: "0c003480060035007800" + "0000"}},
		{"boundary: no altname is an empty nest", "dev d0", result{dev: "d0", attrs: "04003480"}},
		{"boundary: a 127-byte altname, ALTIFNAMSIZ less one", "dev d0 altname " + long, result{dev: "d0",
			attrs: "88003480" + "84003500" + hex.EncodeToString([]byte(long)) + "00"}},
		{"corner: a device name check_ifname refuses but check_altifname takes", "dev a/b altname x",
			result{dev: "a/b", attrs: "0c003480060035007800" + "0000"}},
		{"negative: a 128-byte altname", "dev d0 altname " + long + "y", result{err: ErrInvalidArgument}},
		{"negative: no device", "altname d0alt", result{err: ErrInvalidArgument}},
		{"negative: no arguments", "", result{err: ErrInvalidArgument}},
		{"negative: two devices", "dev d0 dev d1", result{err: ErrInvalidArgument}},
		{"negative: a second bare word is a second device", "d0 altname x d1", result{err: ErrInvalidArgument}},
		{"negative: altname with no value", "dev d0 altname", result{err: ErrInvalidArgument}},
		{"negative: dev with no value", "altname x dev", result{err: ErrInvalidArgument}},
		{"corner: help is not implemented", "dev d0 help", result{err: ErrNotImplemented}},
		{"corner: `h` is help, not a device", "h", result{err: ErrNotImplemented}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w, err := parseLinkPropArgs(strings.Fields(tc.args))
			if tc.expected.err != nil || err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
				return
			}
			got := result{dev: w.dev, attrs: hex.EncodeToString(w.attrs.Bytes())}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestLinkProperty is the verbs, the header and the device lookup against
// a Talk source, which can answer the by-name get with ENODEV.
//
// go test ./internal/goip/ -run TestLinkProperty$
func TestLinkProperty(t *testing.T) {
	type result struct {
		err     error
		talks   string
		msgType uint16
		header  string
	}
	tests := []struct {
		description string
		family      uint8
		noHook      bool
		args        string
		expected    result
	}{
		{description: "positive: add, the ifindex in the header", args: "property add dev d0 altname x",
			expected: result{talks: "d0", msgType: unix.RTM_NEWLINKPROP, header: "00000000030000000000000000000000"}},
		{description: "positive: `d` is del", args: "property d dev d0 altname x",
			expected: result{talks: "d0", msgType: unix.RTM_DELLINKPROP, header: "00000000030000000000000000000000"}},
		{description: "corner: -6 is the header's family", family: unix.AF_INET6, args: "property add dev d0 altname x",
			expected: result{talks: "d0", msgType: unix.RTM_NEWLINKPROP, header: "0a000000030000000000000000000000"}},
		{description: "negative: ENODEV is no such device, nothing written", args: "property add dev nope altname x",
			expected: result{err: ErrInvalidArgument, talks: "nope"}},
		{description: "negative: delete is not del, matches() on a shorter word", args: "property delete dev d0 altname x",
			expected: result{err: ErrInvalidArgument}},
		{description: "negative: no verb", args: "property", expected: result{err: ErrNotImplemented}},
		{description: "corner: help", args: "property help", expected: result{err: ErrNotImplemented}},
		{description: "negative: a bad argument is refused before the get", args: "property add altname x",
			expected: result{err: ErrInvalidArgument}},
		{description: "negative: no GOIP_WRITE_OUT, after the get", noHook: true, args: "property add dev d0 altname x",
			expected: result{err: ErrWriteNotApplied, talks: "d0"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			src := &talkLinks{replies: map[string][]byte{"d0": newLinkBody(3, "d0")}}
			_, err := runLinkWith(t, src, tc.family, strings.Fields(tc.args))
			got := result{talks: strings.Join(src.talks, " ")}
			if tc.expected.err != nil || err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
				got.err = tc.expected.err
			}
			if writtenFile(out) {
				w := restoreWrites(t, out)[0]
				got.msgType = w.Hdr.Type
				got.header = hex.EncodeToString(w.Body[:16])
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}
