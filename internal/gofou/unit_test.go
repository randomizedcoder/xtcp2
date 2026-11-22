package gofou

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// runArgs runs gofou with cfg and args, returning stdout, stderr and rc.
func runArgs(cfg config, args ...string) (string, string, int) {
	var out, errb bytes.Buffer
	rc := run(cfg, args, &out, &errb)
	return out.String(), errb.String(), rc
}

// fakeSource answers each Talk with the next scripted reply, for the dispatch
// paths that resolve the family and dump without a real capture.
type fakeSource struct {
	replies [][]Msg
	n       int
}

func (f *fakeSource) Talk(_ []byte) (Reply, error) {
	if f.n >= len(f.replies) {
		return Reply{}, ErrReplay
	}
	r := f.replies[f.n]
	f.n++
	return Reply{Msgs: r}, nil
}

// newFamilyMsg is a CTRL_CMD_NEWFAMILY reply naming "fou" with id 42.
func newFamilyMsg() Msg {
	body := []byte{
		1, 0, 0, 0, // genlmsghdr cmd=NEWFAMILY
		0x06, 0x00, 0x01, 0x00, 42, 0, 0, 0, // CTRL_ATTR_FAMILY_ID u16
		0x08, 0x00, 0x02, 0x00, 'f', 'o', 'u', 0, // CTRL_ATTR_FAMILY_NAME
	}
	return Msg{Type: genlIDCtrl, Body: body}
}

// fouMapMsg is one FOU_CMD_GET mapping: port 5555, ipproto 4 (IPIP), direct,
// AF_INET. The port is big-endian on the wire.
func fouMapMsg() Msg {
	b := []byte{
		3, 1, 0, 0, // genlmsghdr cmd=FOU_CMD_GET
		0x06, 0x00, 0x01, 0x00, 0x15, 0xb3, 0, 0, // FOU_ATTR_PORT be16 5555
		0x05, 0x00, 0x02, 0x00, 2, 0, 0, 0, // FOU_ATTR_AF u8 AF_INET
		0x05, 0x00, 0x03, 0x00, 4, 0, 0, 0, // FOU_ATTR_IPPROTO u8 4
		0x05, 0x00, 0x04, 0x00, 1, 0, 0, 0, // FOU_ATTR_TYPE u8 DIRECT
	}
	return Msg{Type: 42, Body: b}
}

func doneMsg() Msg { return Msg{Type: unix.NLMSG_DONE, Body: []byte{0, 0, 0, 0}} }

// familyThenDump scripts the GETFAMILY reply and then the show dump's
// replies.
func familyThenDump(dump ...Msg) *fakeSource {
	return &fakeSource{replies: [][]Msg{{newFamilyMsg()}, dump}}
}

// TestGofouDispatch covers the dispatch paths that do not need the kernel:
// options, the object, help and the unknown-command refusals.
func TestGofouDispatch(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantRC      int
		stderrHas   string
	}{
		{"positive: help prints usage and exits 255 before any wire", []string{"fou", "help"}, 255, "Usage: ip fou add"},
		{"corner: help by its prefix is still help", []string{"fou", "h"}, 255, "Usage: ip fou add"},
		{"boundary: no object is a usage-style refusal", []string{}, 255, "Object"},
		{"negative: an object that is not fou is refused", []string{"link"}, 255, "Object \"link\" is unknown"},
		{"corner: the fou object by its prefix is accepted (then needs args)", []string{"fo"}, 255, "Usage: ip fou add"},
		{"negative: an unknown option is refused before the object", []string{"-z", "fou", "show"}, 255, "Option \"-z\" is unknown"},
		{"boundary: fou with no sub-command is a usage refusal", []string{"fou"}, 255, "Usage: ip fou add"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, errs, rc := runArgs(config{}, tc.args...)
			if rc != tc.wantRC {
				t.Errorf("rc %d, want %d (stderr %q)", rc, tc.wantRC, errs)
			}
			if !strings.Contains(errs, tc.stderrHas) {
				t.Errorf("stderr %q, want it to contain %q", errs, tc.stderrHas)
			}
		})
	}
}

// TestGofouShow covers show, its JSON and text forms, and the post-resolve
// refusals, against a scripted source.
func TestGofouShow(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		src         *fakeSource
		wantRC      int
		wantOut     string
		stderrHas   string
	}{
		{
			description: "positive: a one-port text dump",
			args:        []string{"fou", "show"},
			src:         familyThenDump(fouMapMsg(), doneMsg()),
			wantRC:      0,
			wantOut:     "port 5555 ipproto 4\n",
		},
		{
			description: "positive: the same dump as JSON",
			args:        []string{"-j", "fou", "show"},
			src:         familyThenDump(fouMapMsg(), doneMsg()),
			wantRC:      0,
			wantOut:     `[{"port":5555,"ipproto":4,"family":"inet"}]` + "\n",
		},
		{
			description: "boundary: an empty text dump prints nothing",
			args:        []string{"fou", "show"},
			src:         familyThenDump(doneMsg()),
			wantRC:      0,
			wantOut:     "",
		},
		{
			description: "boundary: an empty JSON dump is an empty array",
			args:        []string{"-j", "fou", "show"},
			src:         familyThenDump(doneMsg()),
			wantRC:      0,
			wantOut:     "[]\n",
		},
		{
			description: "negative: a stray argument is refused after the family resolve",
			args:        []string{"fou", "show", "junk"},
			src:         familyThenDump(fouMapMsg(), doneMsg()),
			wantRC:      1,
			stderrHas:   "does not take any arguments",
		},
		{
			description: "negative: an unknown sub-command exits 255 after the family resolve",
			args:        []string{"fou", "badobj"},
			src:         familyThenDump(),
			wantRC:      255,
			stderrHas:   "Command \"badobj\" is unknown",
		},
		{
			description: "negative: a write is refused, read-only",
			args:        []string{"fou", "add", "port", "5555", "gue"},
			src:         familyThenDump(),
			wantRC:      1,
			stderrHas:   "is a write, not cloned",
		},
		{
			description: "corner: a sub-command that is not a prefix of show is unknown",
			args:        []string{"fou", "showX"},
			src:         familyThenDump(),
			wantRC:      255,
			stderrHas:   "Command \"showX\" is unknown",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out, errs, rc := runArgs(config{src: tc.src}, tc.args...)
			if rc != tc.wantRC {
				t.Errorf("rc %d, want %d (stderr %q)", rc, tc.wantRC, errs)
			}
			if out != tc.wantOut {
				t.Errorf("stdout %q, want %q", out, tc.wantOut)
			}
			if tc.stderrHas != "" && !strings.Contains(errs, tc.stderrHas) {
				t.Errorf("stderr %q, want it to contain %q", errs, tc.stderrHas)
			}
		})
	}
}

// TestGofouDumpTerminated pins that an NLMSG_ERROR mid-dump is "Dump
// terminated", rc 1, with the JSON array still closed.
func TestGofouDumpTerminated(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantRC      int
		wantOut     string
		stderrHas   string
	}{
		{"negative: a text dump that errors is terminated", []string{"fou", "show"}, 1, "", "Dump terminated"},
		{"negative: a JSON dump that errors still closes the array", []string{"-j", "fou", "show"}, 1, "[]\n", "Dump terminated"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := familyThenDump(Msg{Type: unix.NLMSG_ERROR, Body: []byte{0, 0, 0, 0}})
			out, errs, rc := runArgs(config{src: src}, tc.args...)
			if rc != tc.wantRC {
				t.Errorf("rc %d, want %d", rc, tc.wantRC)
			}
			if out != tc.wantOut {
				t.Errorf("stdout %q, want %q", out, tc.wantOut)
			}
			if !strings.Contains(errs, tc.stderrHas) {
				t.Errorf("stderr %q, want it to contain %q", errs, tc.stderrHas)
			}
		})
	}
}
