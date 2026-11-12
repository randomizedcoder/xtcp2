package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// useMaddrProc points the procfs readers at dir for the test.
func useMaddrProc(t *testing.T, dir string) {
	t.Helper()
	old := maddrProcNet
	maddrProcNet = dir
	t.Cleanup(func() { maddrProcNet = old })
}

// runMaddrCtx runs `ip maddress` args on c, as runLinkCtx runs `ip link`.
func runMaddrCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	c.out = &out
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runMaddr(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// maddrReplay is the replay for one maddr row. ReplaySource answers a dump
// with every reply of its type in the capture, here both families'; the
// kernel answers each with its own family's, which this keeps. goip, like
// accept_maddr, does not check the family itself.
type maddrReplay struct{ *restoreReplay }

func (r maddrReplay) Dump(request []byte, msgType uint16) ([][]byte, error) {
	out, err := r.restoreReplay.Dump(request, msgType)
	if err != nil || msgType != unix.RTM_GETMULTICAST {
		return out, err
	}
	fam := request[xtcpnl.NlMsgHdrSizeCst]
	return slices.DeleteFunc(out, func(b []byte) bool { return len(b) == 0 || b[0] != fam }), nil
}

// TestWsV730Maddr replays each v730maddr row at 7.3.0: goip sends ip's
// requests and prints what ip printed. The link-layer groups come from
// testdata/maddr/dev_mcast, which is d0's five groups as dev_mc_seq_show
// prints them, rebuilt from the rows' ip.out: the workbench keeps no copy
// of the guest's procfs.
//
// go test ./internal/goip/ -run TestWsV730Maddr
func TestWsV730Maddr(t *testing.T) {
	useLLMap720(t) // 7.2.0 and 7.3.0 both ask for names alone
	useMaddrProc(t, filepath.Join("testdata", "maddr"))
	dir := filepath.Join(wsCorpusDir731IP730, "v730maddr")
	tests := []struct {
		description string
		row         string
		cmd         string
		requests    int // ip's, which goip's must equal
	}{
		{description: "positive: both families, every device named by a get", row: "all", cmd: "ip maddress show", requests: 5},
		{description: "positive: dev d0, the dumps filtered by the kernel", row: "dev", cmd: "ip maddress show dev d0", requests: 3},
		{description: "positive: -4, no link-layer groups", row: "inet", cmd: "ip -4 maddress show", requests: 4},
		{description: "positive: -6, no link-layer groups", row: "inet6", cmd: "ip -6 maddress show", requests: 4},
		{description: "positive: -j, one object per device", row: "json", cmd: "ip -j maddress show", requests: 5},
		{description: "corner: dev nosuch exits 0, no dump and no output", row: "nosuch", cmd: "ip maddress show dev nosuch", requests: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			rowDir := filepath.Join(dir, tc.row)
			c, _, args, err := wsReadbackArgv(tc.cmd)
			if err != nil {
				t.Fatal(err)
			}
			pcap := filepath.Join(rowDir, "ip.pcap")
			src := &restoreReplay{linkReplay: newLinkReplay(t, pcap)}
			c.src = maddrReplay{src}
			rc, stdout, stderr := runMaddrCtx(c, args)
			if rc != ExitOK {
				t.Fatalf("exit %d: %s", rc, stderr)
			}
			want, err := os.ReadFile(filepath.Join(rowDir, "ip.out"))
			if err != nil {
				t.Fatal(err)
			}
			if stdout != string(want) {
				t.Errorf("stdout\n got  %q\n want %q", stdout, want)
			}
			reqs := slices.Concat(src.dumpReqs, src.gets)
			slices.SortFunc(reqs, bytes.Compare)
			ip := mplsIPRequests(t, pcap)
			if len(ip) != tc.requests {
				t.Errorf("ip sent %d requests, want %d", len(ip), tc.requests)
			}
			if !slices.EqualFunc(reqs, ip, bytes.Equal) {
				t.Errorf("requests\n goip %x\n ip   %x", reqs, ip)
			}
		})
	}
}

// TestMaddrListArgs is multiaddr_list's argument loop.
//
// go test ./internal/goip/ -run TestMaddrListArgs
func TestMaddrListArgs(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    string
		err         bool // ErrMaddrUsage
	}{
		{description: "positive: no arguments", expected: ""},
		{description: "positive: dev NAME", args: []string{"dev", "d0"}, expected: "d0"},
		{description: "positive: a bare name is the device", args: []string{"d0"}, expected: "d0"},
		{description: "corner: help after dev is a device's name", args: []string{"dev", "help"}, expected: "help"},
		{description: "negative: dev with no name", args: []string{"dev"}, err: true},
		{description: "negative: help", args: []string{"help"}, err: true},
		{description: "boundary: h abbreviates help", args: []string{"h"}, err: true},
		{description: "negative: two names", args: []string{"d0", "d1"}, err: true},
		{description: "negative: dev twice", args: []string{"dev", "d0", "dev", "d1"}, err: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := maddrListArgs(tc.args)
			if tc.err {
				if !errors.Is(err, ErrMaddrUsage) || exitCodeOf(err) != ExitMinusOne {
					t.Fatalf("err %v (exit %d), want ErrMaddrUsage, 255", err, exitCodeOf(err))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestRunMaddrVerbs is do_multiaddr's verbs: the ioctls are refused as not
// implemented, help and an unknown word as ip's usage, before any request.
//
// go test ./internal/goip/ -run TestRunMaddrVerbs
func TestRunMaddrVerbs(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    error
		msg         string
	}{
		{description: "negative: add is an ioctl", args: []string{"add", "33:33:00:00:00:01", "dev", "d0"}, expected: ErrNotImplemented},
		{description: "boundary: d is delete, an ioctl", args: []string{"d"}, expected: ErrNotImplemented},
		{description: "negative: help is the usage", args: []string{"help"}, expected: ErrMaddrUsage, msg: maddrUsage},
		{description: "negative: an unknown command", args: []string{"x"}, expected: ErrMaddrUsage,
			msg: `Command "x" is unknown, try "ip maddr help"`},
		{description: "negative: a show's argument error comes after the verb", args: []string{"show", "dev"}, expected: ErrMaddrUsage,
			msg: "Command line is not complete"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c := &runCtx{lltab: NewLLTab()}
			err := runMaddr(c, tc.args)
			if !errors.Is(err, tc.expected) {
				t.Fatalf("err %v, want %v", err, tc.expected)
			}
			if tc.msg != "" && !bytes.Contains([]byte(err.Error()), []byte(tc.msg)) {
				t.Errorf("err %q, want it to say %q", err, tc.msg)
			}
		})
	}
}

// TestMaddrInsert is maddr_ins: by index, and within an index in the order
// the groups arrive, the earlier sources first.
//
// go test ./internal/goip/ -run TestMaddrInsert
func TestMaddrInsert(t *testing.T) {
	e := func(idx int32, users uint32) render.MaddrEntry { return render.MaddrEntry{Index: idx, Users: users} }
	tests := []struct {
		description string
		list, add   []render.MaddrEntry
		expected    []render.MaddrEntry
	}{
		{description: "positive: into an empty list", add: []render.MaddrEntry{e(2, 1), e(1, 1)},
			expected: []render.MaddrEntry{e(1, 1), e(2, 1)}},
		{description: "positive: the same index goes after what is there", list: []render.MaddrEntry{e(3, 1)},
			add: []render.MaddrEntry{e(3, 2), e(3, 3)}, expected: []render.MaddrEntry{e(3, 1), e(3, 2), e(3, 3)}},
		{description: "positive: a lower index goes before", list: []render.MaddrEntry{e(3, 1)},
			add: []render.MaddrEntry{e(1, 2)}, expected: []render.MaddrEntry{e(1, 2), e(3, 1)}},
		{description: "corner: nothing to add", list: []render.MaddrEntry{e(3, 1)},
			expected: []render.MaddrEntry{e(3, 1)}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := maddrInsert(slices.Clone(tc.list), tc.add); !slices.EqualFunc(got, tc.expected, maddrEqual) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func maddrEqual(a, b render.MaddrEntry) bool {
	return a.Index == b.Index && a.Name == b.Name && a.Family == b.Family && bytes.Equal(a.Addr, b.Addr) &&
		a.Users == b.Users && a.Static == b.Static
}

// The procfs fixtures: the kernel's formats (dev_mc_seq_show,
// igmp_mc_seq_show, igmp6_mc_seq_show) with a device in each.
const (
	maddrDevMcastFixture = "3    d0              1     0     333300000001\n" +
		"3    d0              2     1     01005e000001\n" +
		"4    d1              1     0     333\n" +
		"5    d2              1     0     3333000000zz\n"
	maddrIgmpFixture = "Idx\tDevice    : Count Querier\tGroup    Users Timer\tReporter\n" +
		"1\tlo        :     1      V3\n" +
		"\t\t\t\t010000E0     1 0:00000000\t\t0\n" +
		"3\tverylongname:     2      V3\n" +
		"\t\t\t\t010101EF     3 0:00000000\t\t0\n" +
		"\t\t\t\t010000E0     1 0:00000000\t\t0\n" +
		"9\tabcdefghijklmno:     1      V3\n" +
		"\t\t\t\t010000E0     1 0:00000000\t\t0\n"
	maddrIgmp6Fixture = "1    lo              ff020000000000000000000000000001     1 0000000C 0\n" +
		"3    d0              ff050000000000000000000000001234     2 00000004 0\n"
)

// maddrProcDir writes the fixtures to a directory.
func maddrProcDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"dev_mcast": maddrDevMcastFixture, "igmp": maddrIgmpFixture, "igmp6": maddrIgmp6Fixture} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestMaddrProcfs is read_dev_mcast, read_igmp and read_igmp6 on the
// fixtures, whole and filtered by name.
//
// go test ./internal/goip/ -run TestMaddrProcfs
func TestMaddrProcfs(t *testing.T) {
	useMaddrProc(t, maddrProcDir(t))
	v4 := func(b ...byte) []byte { return b }
	ff02 := []byte{0xff, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	ff05 := []byte{0xff, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x12, 0x34}
	tests := []struct {
		description string
		read        func() []render.MaddrEntry
		expected    []render.MaddrEntry
	}{
		{description: "positive: dev_mcast, global_use as static; odd and bad hex dropped", read: func() []render.MaddrEntry { return readDevMcast("") },
			expected: []render.MaddrEntry{
				{Index: 3, Name: "d0", Family: unix.AF_PACKET, Addr: []byte{0x33, 0x33, 0, 0, 0, 1}, Users: 1},
				{Index: 3, Name: "d0", Family: unix.AF_PACKET, Addr: []byte{1, 0, 0x5e, 0, 0, 1}, Users: 2, Static: true},
			}},
		{description: "positive: dev_mcast filtered by name", read: func() []render.MaddrEntry { return readDevMcast("d1") }},
		{description: "positive: igmp, the address in host order, the colon off a long name", read: func() []render.MaddrEntry { return readIgmp(unix.AF_INET, "") },
			expected: []render.MaddrEntry{
				{Index: 1, Name: "lo", Family: unix.AF_INET, Addr: v4(224, 0, 0, 1), Users: 1},
				{Index: 3, Name: "verylongname", Family: unix.AF_INET, Addr: v4(239, 1, 1, 1), Users: 3},
				{Index: 3, Name: "verylongname", Family: unix.AF_INET, Addr: v4(224, 0, 0, 1), Users: 1},
				{Index: 9, Name: "abcdefghijklmno", Family: unix.AF_INET, Addr: v4(224, 0, 0, 1), Users: 1},
			}},
		{description: "boundary: igmp, a 15-character name and its colon, one token", read: func() []render.MaddrEntry { return readIgmp(unix.AF_INET, "abcdefghijklmno") },
			expected: []render.MaddrEntry{{Index: 9, Name: "abcdefghijklmno", Family: unix.AF_INET, Addr: v4(224, 0, 0, 1), Users: 1}}},
		{description: "positive: igmp filtered by name", read: func() []render.MaddrEntry { return readIgmp(unix.AF_INET, "lo") },
			expected: []render.MaddrEntry{{Index: 1, Name: "lo", Family: unix.AF_INET, Addr: v4(224, 0, 0, 1), Users: 1}}},
		{description: "positive: igmp6", read: func() []render.MaddrEntry { return readIgmp(unix.AF_INET6, "") },
			expected: []render.MaddrEntry{
				{Index: 1, Name: "lo", Family: unix.AF_INET6, Addr: ff02, Users: 1},
				{Index: 3, Name: "d0", Family: unix.AF_INET6, Addr: ff05, Users: 2},
			}},
		{description: "negative: igmp6 for a name it lacks", read: func() []render.MaddrEntry { return readIgmp(unix.AF_INET6, "nosuch") }},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.read(); !slices.EqualFunc(got, tc.expected, maddrEqual) {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestMaddrProcfsMissing is a reader whose file does not open: nothing,
// as fopen's failure returns.
//
// go test ./internal/goip/ -run TestMaddrProcfsMissing
func TestMaddrProcfsMissing(t *testing.T) {
	useMaddrProc(t, t.TempDir())
	tests := []struct {
		description string
		read        func() []render.MaddrEntry
	}{
		{description: "corner: no dev_mcast", read: func() []render.MaddrEntry { return readDevMcast("") }},
		{description: "corner: no igmp", read: func() []render.MaddrEntry { return readIgmp(unix.AF_INET, "") }},
		{description: "corner: no igmp6", read: func() []render.MaddrEntry { return readIgmp(unix.AF_INET6, "") }},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.read(); len(got) != 0 {
				t.Errorf("got %+v, want nothing", got)
			}
		})
	}
}

// maddrSource answers each RTM_GETMULTICAST dump with its family's
// replies, or err.
type maddrSource struct {
	replies map[uint8][][]byte
	err     error
	asked   []uint8
}

func (s *maddrSource) Dump(request []byte, msgType uint16) ([][]byte, error) {
	if msgType != unix.RTM_GETMULTICAST {
		return nil, fmt.Errorf("dump of %d", msgType)
	}
	fam := request[xtcpnl.NlMsgHdrSizeCst]
	s.asked = append(s.asked, fam)
	if s.err != nil {
		return nil, s.err
	}
	return s.replies[fam], nil
}

// maddrReply is one RTM_GETMULTICAST reply body: IFA_MULTICAST unless addr
// is nil, and IFA_MC_USERS for each of users (none or one).
func maddrReply(t *testing.T, family uint8, index uint32, addr []byte, users ...uint32) []byte {
	t.Helper()
	var raw [64]byte
	ab := xtcpnl.NewAttrBuilder(raw[:])
	if addr != nil {
		if err := ab.PutBytes(unix.IFA_MULTICAST, addr); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range users {
		if err := ab.PutU32(xtcpnl.IfaMcUsers, u); err != nil {
			t.Fatal(err)
		}
	}
	attrs := ab.Bytes()
	body := make([]byte, xtcpnl.IfAddrmsgSizeCst, xtcpnl.IfAddrmsgSizeCst+len(attrs))
	body[0] = family
	binary.LittleEndian.PutUint32(body[4:8], index)
	return append(body, attrs...)
}

// TestMaddrList is multiaddr_list on a fake kernel: the sources in their
// order, the filters, and read_maddr_netlink's fallback to procfs.
//
// go test ./internal/goip/ -run TestMaddrList
func TestMaddrList(t *testing.T) {
	useMaddrProc(t, maddrProcDir(t))
	g4 := []byte{239, 1, 1, 1}
	g6 := []byte{0xff, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x12, 0x34}
	both := map[uint8][][]byte{
		unix.AF_INET:  {maddrReply(t, unix.AF_INET, 3, g4, 1), maddrReply(t, unix.AF_INET, 1, []byte{224, 0, 0, 1}, 1)},
		unix.AF_INET6: {maddrReply(t, unix.AF_INET6, 3, g6, 2)},
	}
	tests := []struct {
		description string
		family      uint8
		dev         string
		replies     map[uint8][][]byte
		err         error
		asked       []uint8
		expected    string
	}{
		{description: "positive: link, then inet, then inet6, by index", replies: both,
			asked: []uint8{unix.AF_INET, unix.AF_INET6},
			expected: "1:\tlo\n\tinet  224.0.0.1\n3:\td0\n\tlink  33:33:00:00:00:01\n\tlink  01:00:5e:00:00:01 users 2 static\n" +
				"\tinet  239.1.1.1\n\tinet6 ff05::1234 users 2\n"},
		{description: "positive: -4 dumps inet alone", family: unix.AF_INET, replies: both, asked: []uint8{unix.AF_INET},
			expected: "1:\tlo\n\tinet  224.0.0.1\n3:\td0\n\tinet  239.1.1.1\n"},
		{description: "positive: -0 reads dev_mcast alone", family: unix.AF_PACKET, replies: both,
			expected: "3:\td0\n\tlink  33:33:00:00:00:01\n\tlink  01:00:5e:00:00:01 users 2 static\n"},
		{description: "positive: dev d0 keeps index 3 from the dumps and d0 from dev_mcast", dev: "d0", replies: both,
			asked: []uint8{unix.AF_INET, unix.AF_INET6},
			expected: "3:\td0\n\tlink  33:33:00:00:00:01\n\tlink  01:00:5e:00:00:01 users 2 static\n" +
				"\tinet  239.1.1.1\n\tinet6 ff05::1234 users 2\n"},
		{description: "negative: a failed dump reads igmp and igmp6", family: unix.AF_UNSPEC, err: errors.New("EOPNOTSUPP"),
			asked: []uint8{unix.AF_INET, unix.AF_INET6},
			expected: "1:\tlo\n\tinet  224.0.0.1\n\tinet6 ff02::1\n3:\td0\n\tlink  33:33:00:00:00:01\n" +
				"\tlink  01:00:5e:00:00:01 users 2 static\n\tinet  239.1.1.1 users 3\n\tinet  224.0.0.1\n\tinet6 ff05::1234 users 2\n" +
				"9:\tabcdefghijklmno\n\tinet  224.0.0.1\n"},
		{description: "corner: one reply without IFA_MC_USERS drops its family's dump for igmp", family: unix.AF_INET,
			replies: map[uint8][][]byte{unix.AF_INET: {maddrReply(t, unix.AF_INET, 3, g4, 1), maddrReply(t, unix.AF_INET, 3, g4)}},
			asked:   []uint8{unix.AF_INET},
			expected: "1:\tlo\n\tinet  224.0.0.1\n3:\tverylongname\n\tinet  239.1.1.1 users 3\n\tinet  224.0.0.1\n" +
				"9:\tabcdefghijklmno\n\tinet  224.0.0.1\n"},
		{description: "corner: a reply without IFA_MULTICAST is dropped", family: unix.AF_INET6,
			replies: map[uint8][][]byte{unix.AF_INET6: {maddrReply(t, unix.AF_INET6, 3, nil, 1)}},
			asked:   []uint8{unix.AF_INET6}, expected: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &maddrSource{replies: tc.replies, err: tc.err}
			lt := NewLLTab()
			lt.Fill([]xtcpnl.LinkInfo{{Index: 1, Name: "lo"}, {Index: 3, Name: "d0"}})
			c := &runCtx{src: src, lltab: lt, family: tc.family}
			list, err := maddrList(c, tc.dev)
			if err != nil {
				t.Fatal(err)
			}
			if got := render.MaddrText(list); got != tc.expected {
				t.Errorf("text\n got  %q\n want %q", got, tc.expected)
			}
			if !slices.Equal(src.asked, tc.asked) {
				t.Errorf("dumped %v, want %v", src.asked, tc.asked)
			}
		})
	}
}
