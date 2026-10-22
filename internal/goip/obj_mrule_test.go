package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
)

// runMruleCtx runs `ip mrule` args on a fresh context over src.
func runMruleCtx(src Source, family uint8, details int, json bool, args string) (string, error) {
	var out, errOut bytes.Buffer
	c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &errOut, family: family,
		showDetails: details, json: json}
	err := runMrule(c, strings.Fields(args))
	return out.String(), err
}

// TestMruleMatchesIp replays the mrule capture: goip sends ip's one
// RTM_GETRULE dump, family RTNL_FAMILY_IPMR, byte for byte with seq and pid
// zeroed, and prints the sidecar, ipmr's default rule.
//
// go test ./internal/goip/ -run TestMruleMatchesIp
func TestMruleMatchesIp(t *testing.T) {
	const pcap = linkReadsDir + "netlink_route_getrule_mrule.pcap"
	want, err := os.ReadFile(linkReadsDir + "ip_mrule")
	if err != nil {
		t.Fatal(err)
	}
	capt, err := nlparity.ParseRouteCaptureFile(pcap)
	if err != nil {
		t.Fatal(err)
	}
	ip := capt.Requests()
	if len(ip) != 1 {
		t.Fatalf("the capture holds %d requests, want 1", len(ip))
	}
	tests := []struct {
		description string
		family      uint8
		args        string
		expected    string
	}{
		{"positive: show", unix.AF_UNSPEC, "show", string(want)},
		{"positive: no verb is the listing", unix.AF_UNSPEC, "", string(want)},
		{"positive: -4 is the default's request", unix.AF_INET, "list", string(want)},
		{"boundary: `ls` is lst", unix.AF_UNSPEC, "ls", string(want)},
		{"boundary: `s` is show", unix.AF_INET, "s", string(want)},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newStatsReplay(t, pcap)
			stdout, err := runMruleCtx(src, tc.family, 0, false, tc.args)
			if err != nil {
				t.Fatalf("runMrule: %v", err)
			}
			if stdout != tc.expected {
				t.Errorf("stdout %q, want %q", stdout, tc.expected)
			}
			if len(src.sent) != 1 {
				t.Fatalf("goip sent %d requests, want 1", len(src.sent))
			}
			if !bytes.Equal(src.sent[0], capturedRequest(ip[0])) {
				t.Errorf("request differs\ngoip %x\nip   %x", src.sent[0], capturedRequest(ip[0]))
			}
		})
	}
}

// TestMruleShowRender is -d and -j over the captured default rule, whose
// FRA_PROTOCOL is kernel: printed only under -d.
//
// go test ./internal/goip/ -run TestMruleShowRender
func TestMruleShowRender(t *testing.T) {
	tests := []struct {
		description string
		details     int
		json        bool
		expected    string
	}{
		{"positive: -d prints the kernel protocol", 1, false, "32767:\tfrom all lookup default proto kernel\n"},
		{"positive: -j", 0, true, `[{"priority":32767,"src":"all","table":"default"}]` + "\n"},
		{"positive: -j -d", 1, true,
			`[{"priority":32767,"src":"all","table":"default","protocol":"kernel"}]` + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newStatsReplay(t, linkReadsDir+"netlink_route_getrule_mrule.pcap")
			stdout, err := runMruleCtx(src, unix.AF_UNSPEC, tc.details, tc.json, "show")
			if err != nil {
				t.Fatalf("runMrule: %v", err)
			}
			if stdout != tc.expected {
				t.Errorf("stdout %q, want %q", stdout, tc.expected)
			}
		})
	}
}

// mruleFake records each request whole and answers with rules, or fails.
type mruleFake struct {
	rules [][]byte
	fail  bool
	sent  [][]byte
}

func (m *mruleFake) Dump(request []byte, _ uint16) ([][]byte, error) {
	m.sent = append(m.sent, bytes.Clone(request))
	if m.fail {
		return nil, unix.EINVAL
	}
	return m.rules, nil
}

// mruleBody is a fib_rule_hdr of family and table, then FRA_PRIORITY and
// FRA_TABLE.
func mruleBody(family, table uint8, prio uint32) []byte {
	b := make([]byte, 12, 28)
	b[0], b[4], b[7] = family, table, unix.FR_ACT_TO_TBL
	for _, a := range [][2]uint32{{unix.FRA_PRIORITY, prio}, {unix.FRA_TABLE, uint32(table)}} {
		attr := make([]byte, 8)
		binary.LittleEndian.PutUint16(attr[0:2], 8)
		binary.LittleEndian.PutUint16(attr[2:4], uint16(a[0]))
		binary.LittleEndian.PutUint32(attr[4:8], a[1])
		b = append(b, attr...)
	}
	return b
}

// TestRunMrule is do_multirule's family switch and the verbs goip accepts,
// on constructed replies: the request's rtgenmsg family byte, the stdout,
// and the error. A refusal sends nothing.
//
// go test ./internal/goip/ -run TestRunMrule
func TestRunMrule(t *testing.T) {
	const v6Main = "32767:\tfrom all lookup main\n"
	v6 := [][]byte{mruleBody(129, unix.RT_TABLE_MAIN, 32767)}
	type result struct {
		family  int // the request's family byte, -1 for no request
		stdout  string
		err     error
		errText string
	}
	tests := []struct {
		description string
		family      uint8
		src         *mruleFake
		args        string
		expected    result
	}{
		{"positive: none is RTNL_FAMILY_IPMR", unix.AF_UNSPEC,
			&mruleFake{rules: [][]byte{mruleBody(128, unix.RT_TABLE_DEFAULT, 32767)}}, "show",
			result{family: 128, stdout: "32767:\tfrom all lookup default\n"}},
		{"positive: -4 is RTNL_FAMILY_IPMR", unix.AF_INET, &mruleFake{}, "show", result{family: 128}},
		{"positive: -6 is RTNL_FAMILY_IP6MR, its default rule main", unix.AF_INET6, &mruleFake{rules: v6},
			"show", result{family: 129, stdout: v6Main}},
		{"positive: two rules, in dump order", unix.AF_INET6, &mruleFake{rules: [][]byte{
			mruleBody(129, 7, 100), v6[0]}}, "", result{family: 129, stdout: "100:\tfrom all lookup 7\n" + v6Main}},
		{"boundary: no rules prints nothing", unix.AF_INET6, &mruleFake{}, "lst", result{family: 129}},
		{"negative: -0 is refused, before any request", unix.AF_PACKET, &mruleFake{}, "show",
			result{family: -1, err: ErrMruleFamily,
				errText: "Multicast rules are only supported for IPv4/IPv6, was: 17"}},
		{"corner: -0 refuses the family before reading the verb", unix.AF_PACKET, &mruleFake{}, "bogus",
			result{family: -1, err: ErrMruleFamily}},
		{"negative: help", unix.AF_UNSPEC, &mruleFake{}, "help", result{family: -1, err: ErrNotImplemented}},
		{"negative: an unknown verb", unix.AF_UNSPEC, &mruleFake{}, "foo",
			result{family: -1, err: ErrNotImplemented}},
		{"negative: add is not implemented", unix.AF_UNSPEC, &mruleFake{}, "add table 5",
			result{family: -1, err: ErrNotImplemented}},
		{"negative: flush is not implemented", unix.AF_INET6, &mruleFake{}, "flush",
			result{family: -1, err: ErrNotImplemented}},
		{"corner: `sa` is save, not show", unix.AF_UNSPEC, &mruleFake{}, "sa",
			result{family: -1, err: ErrNotImplemented}},
		{"negative: a selector is refused, as rule show refuses one", unix.AF_UNSPEC, &mruleFake{},
			"show pref 32767", result{family: -1, err: ErrNotImplemented}},
		{"negative: a failed dump is an error", unix.AF_UNSPEC, &mruleFake{fail: true}, "show",
			result{family: 128, err: unix.EINVAL}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			stdout, err := runMruleCtx(tc.src, tc.family, 0, false, tc.args)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			if err != nil && !strings.Contains(err.Error(), tc.expected.errText) {
				t.Errorf("err %q does not hold %q", err, tc.expected.errText)
			}
			got := result{family: -1, stdout: stdout, err: tc.expected.err, errText: tc.expected.errText}
			if len(tc.src.sent) > 1 {
				t.Fatalf("goip sent %d requests, want at most 1", len(tc.src.sent))
			}
			if len(tc.src.sent) == 1 {
				got.family = int(tc.src.sent[0][unix.NLMSG_HDRLEN])
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestMruleFamilyExit is the family refusal through exitCodeOf: ip exits
// do_multirule's -1, 255, and goip does too.
//
// go test ./internal/goip/ -run TestMruleFamilyExit
func TestMruleFamilyExit(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		expected    int
	}{
		{"negative: -0 exits 255", unix.AF_PACKET, ExitMinusOne},
		{"corner: an unnamed family exits 255 too", 42, ExitMinusOne},
		{"positive: -6 is no error", unix.AF_INET6, ExitOK},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := ExitOK
			if _, err := mruleFamily(tc.family); err != nil {
				got = exitCodeOf(err)
			}
			if got != tc.expected {
				t.Errorf("exit %d, want %d", got, tc.expected)
			}
		})
	}
}
