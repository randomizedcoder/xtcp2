package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// netnsFDOffCst is NETNSA_FD's value in a by-descriptor GETNSID: the
// nlmsghdr, the rtgenmsg, then the attribute's own header.
const netnsFDOffCst = unix.NLMSG_HDRLEN + 4 + 4

// nsidFake answers the RTM_GETNSIDs of `ip netns list-id`. A by-descriptor
// get is answered by the name of the file the descriptor is open on: its
// ids entry, or the kernel's refusal when it has none. A get by nsid is
// answered from gets, the dump from dump. log is each request in order:
// "fd:NAME", "get:N[:T]", or "dump[:T]".
type nsidFake struct {
	ids     map[string]int32
	gets    map[int32][]byte
	dump    [][]byte
	dumpErr error
	log     []string
	sent    [][]byte
}

func (f *nsidFake) Talk(request []byte, _ uint16) ([]byte, error) {
	f.sent = append(f.sent, zeroSeqPidCopy(request))
	ni, err := xtcpnl.ParseNsid(request[unix.NLMSG_HDRLEN:])
	if err != nil {
		return nil, err
	}
	if ni.HasFD {
		path, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(ni.FD)))
		if err != nil {
			return nil, err
		}
		name := filepath.Base(path)
		f.log = append(f.log, "fd:"+name)
		id, ok := f.ids[name]
		if !ok {
			return nil, unix.EINVAL
		}
		return nsidBody(id, nil), nil
	}
	entry := "get:" + strconv.Itoa(int(ni.NSID))
	if ni.HasTargetNSID {
		entry += ":" + strconv.Itoa(int(ni.TargetNSID))
	}
	f.log = append(f.log, entry)
	b, ok := f.gets[ni.NSID]
	if !ok {
		return nil, unix.EINVAL
	}
	return b, nil
}

func (f *nsidFake) Dump(request []byte, _ uint16) ([][]byte, error) {
	f.sent = append(f.sent, zeroSeqPidCopy(request))
	ni, err := xtcpnl.ParseNsid(request[unix.NLMSG_HDRLEN:])
	if err != nil {
		return nil, err
	}
	entry := "dump"
	if ni.HasTargetNSID {
		entry += ":" + strconv.Itoa(int(ni.TargetNSID))
	}
	f.log = append(f.log, entry)
	return f.dump, f.dumpErr
}

// nsidBody is an RTM_NEWNSID reply: NETNSA_NSID nsid, then
// NETNSA_CURRENT_NSID when current is set.
func nsidBody(nsid int32, current *int32) []byte {
	s32 := func(v int32) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(v)) }
	b := catBytes([]byte{unix.AF_UNSPEC, 0, 0, 0}, nla(unix.NETNSA_NSID, s32(nsid)))
	if current != nil {
		b = catBytes(b, nla(unix.NETNSA_CURRENT_NSID, s32(*current)))
	}
	return b
}

// netnsDir points netnsRunDir at a new directory holding names, each an
// empty file, for the test's life.
func netnsDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := netnsRunDir
	netnsRunDir = dir
	t.Cleanup(func() { netnsRunDir = old })
	return dir
}

// runNetnsCtx runs `ip netns` args on a fresh context over src.
func runNetnsCtx(src Source, json bool, args string) (string, error) {
	var out bytes.Buffer
	c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &out, json: json}
	err := runNetns(c, strings.Fields(args))
	return out.String(), err
}

// TestNetnsListIDMatchesIp replays dumps/reads/netlink_route_getnsid_list:
// the six by-descriptor GETNSIDs of netns_map_init, the descriptor masked,
// then the dump, byte for byte with seq and pid zeroed, and the sidecar.
// The fake answers each name as the capture's VM did: nlcaprp1 is 1,
// nlcaprp2 2, the other four unassigned.
//
// go test ./internal/goip/ -run TestNetnsListIDMatchesIp
func TestNetnsListIDMatchesIp(t *testing.T) {
	const pcap = linkReadsDir + "netlink_route_getnsid_list.pcap"
	capt, err := nlparity.ParseRouteCaptureFile(pcap)
	if err != nil {
		t.Fatal(err)
	}
	var dump [][]byte
	for _, m := range capt.Msgs() {
		if m.Hdr.Type == unix.RTM_NEWNSID && m.IsMulti() {
			dump = append(dump, xtcpnl.CopyBytes(m.Body))
		}
	}
	want, err := os.ReadFile(linkReadsDir + "ip_netns_list_id")
	if err != nil {
		t.Fatal(err)
	}
	masked := func(b []byte) string {
		b = xtcpnl.CopyBytes(b)
		if len(b) == netnsFDOffCst+4 {
			copy(b[netnsFDOffCst:], []byte{0, 0, 0, 0})
		}
		return hex.EncodeToString(b)
	}
	tests := []struct {
		description string
		args        string
		expected    string
	}{
		{"positive: list-id prints the sidecar", "list-id", string(want)},
		{"boundary: `list-` is list-id", "list-", string(want)},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			netnsDir(t, "nlcaprp1", "nlcaprp2", "a", "b", "c", "d")
			src := &nsidFake{ids: map[string]int32{"nlcaprp1": 1, "nlcaprp2": 2, "a": -1, "b": -1, "c": -1, "d": -1},
				dump: dump}
			stdout, err := runNetnsCtx(src, false, tc.args)
			if err != nil {
				t.Fatalf("runNetns: %v", err)
			}
			if stdout != tc.expected {
				t.Errorf("stdout\n got %q\nwant %q", stdout, tc.expected)
			}
			ip := capt.Requests()
			if len(src.sent) != len(ip) {
				t.Fatalf("goip sent %d requests, ip %d", len(src.sent), len(ip))
			}
			for i := range ip {
				if g, w := masked(src.sent[i]), masked(capturedRequest(ip[i])); g != w {
					t.Errorf("request %d differs\ngoip %s\nip   %s", i+1, g, w)
				}
			}
		})
	}
}

// TestNetnsListID is netns_list_id and print_nsid on constructed replies,
// over a directory of p1 (1), p2 (2) and u (unassigned).
//
// go test ./internal/goip/ -run TestNetnsListID
func TestNetnsListID(t *testing.T) {
	one, two := int32(1), int32(2)
	ids := func() map[string]int32 { return map[string]int32{"p1": 1, "p2": 2, "u": -1} }
	// MAP is netns_map_init's requests, one per name in directory order;
	// WALK is netns_get_name's, the same; WALK:p1 is a walk that stops at
	// p1.
	const mapInit = "MAP"
	type result struct {
		stdout string
		log    string
		err    error
	}
	tests := []struct {
		description string
		src         *nsidFake
		json        bool
		args        string
		// errText is a fragment the error must hold, where the error alone
		// does not say which refusal it is.
		errText  string
		expected result
	}{
		{description: "positive: the dump, each named from the cache",
			src: &nsidFake{dump: [][]byte{nsidBody(1, nil), nsidBody(2, nil)}}, args: "list-id",
			expected: result{stdout: "nsid 1 (iproute2 netns name: p1)\nnsid 2 (iproute2 netns name: p2)\n",
				log: mapInit + ",dump"}},
		{description: "positive: -j, an array of nsid and name",
			src: &nsidFake{dump: [][]byte{nsidBody(1, nil), nsidBody(2, nil)}}, json: true, args: "list-id",
			expected: result{stdout: `[{"nsid":1,"name":"p1"},{"nsid":2,"name":"p2"}]` + "\n", log: mapInit + ",dump"}},
		{description: "boundary: -j over an empty dump is an empty array", src: &nsidFake{}, json: true,
			args: "list-id", expected: result{stdout: "[]\n", log: mapInit + ",dump"}},
		{description: "boundary: an empty dump prints nothing", src: &nsidFake{}, args: "list-id",
			expected: result{log: mapInit + ",dump"}},
		{description: "positive: nsid 2 is a get, named from the cache",
			src: &nsidFake{gets: map[int32][]byte{2: nsidBody(2, nil)}}, args: "list-id nsid 2",
			expected: result{stdout: "nsid 2 (iproute2 netns name: p2)\n", log: mapInit + ",get:2"}},
		{description: "positive: a target names by the current nsid",
			src: &nsidFake{gets: map[int32][]byte{1: nsidBody(-1, &one)}}, args: "list-id nsid 1 target-nsid 2",
			expected: result{stdout: "nsid unassigned current-nsid 1 (iproute2 netns name: p1)\n",
				log: mapInit + ",get:1:2"}},
		{description: "corner: -j leaves an unassigned nsid out",
			src: &nsidFake{gets: map[int32][]byte{1: nsidBody(-1, &one)}}, json: true,
			args:     "list-id target-nsid 2 nsid 1",
			expected: result{stdout: `[{"current-nsid":1,"name":"p1"}]` + "\n", log: mapInit + ",get:1:2"}},
		{description: "positive: target-nsid alone filters the dump",
			src: &nsidFake{dump: [][]byte{nsidBody(2, &two)}}, args: "list-id target-nsid 0x1",
			expected: result{stdout: "nsid 2 current-nsid 2 (iproute2 netns name: p2)\n", log: mapInit + ",dump:1"}},
		{description: "corner: unassigned current-nsid prints so, and names by nothing",
			src: &nsidFake{dump: [][]byte{nsidBody(7, ptrInt32(-1))}}, args: "list-id",
			expected: result{stdout: "nsid 7 current-nsid unassigned \n", log: mapInit + ",dump,WALK"}},
		{description: "corner: a cached name prints once; the second is the directory walk, which caches it again for the third",
			src: &nsidFake{dump: [][]byte{nsidBody(1, nil), nsidBody(1, nil), nsidBody(1, nil)}}, args: "list-id",
			expected: result{stdout: "nsid 1 (iproute2 netns name: p1)\nnsid 1 (iproute2 netns name: p1)\n" +
				"nsid 1 (iproute2 netns name: p1)\n", log: mapInit + ",dump,WALK:p1"}},
		{description: "boundary: an unassigned nsid names by nothing and walks nothing",
			src: &nsidFake{dump: [][]byte{nsidBody(-1, nil)}}, args: "list-id",
			expected: result{stdout: "nsid unassigned \n", log: mapInit + ",dump"}},
		{description: "corner: an nsid in no file walks the whole directory, then prints no name",
			src: &nsidFake{dump: [][]byte{nsidBody(5, nil)}}, args: "list-id",
			expected: result{stdout: "nsid 5 \n", log: mapInit + ",dump,WALK"}},
		{description: "negative: a reply with no NETNSA_NSID ends the lines",
			src:  &nsidFake{dump: [][]byte{nsidBody(1, nil), {unix.AF_UNSPEC, 0, 0, 0}, nsidBody(2, nil)}},
			args: "list-id", expected: result{stdout: "nsid 1 (iproute2 netns name: p1)\n", log: mapInit + ",dump",
				err: ErrDumpTerminated}},
		{description: "negative: the dump fails", src: &nsidFake{dumpErr: unix.EINVAL}, args: "list-id",
			expected: result{log: mapInit + ",dump", err: ErrDumpTerminated}},
		{description: "negative: the get fails, the kernel's error", src: &nsidFake{}, args: "list-id nsid 9",
			expected: result{log: mapInit + ",get:9", err: unix.EINVAL}},
		{description: "negative: an unknown word is usage, after the map's requests", src: &nsidFake{},
			args: "list-id bogus", expected: result{log: mapInit, err: ErrInvalidArgument}},
		{description: "negative: `nsi` is not nsid, strcmp", src: &nsidFake{}, args: "list-id nsi 1",
			expected: result{log: mapInit, err: ErrInvalidArgument}},
		{description: "negative: nsid twice", src: &nsidFake{}, args: "list-id nsid 1 nsid 2",
			expected: result{log: mapInit, err: ErrInvalidArgument}},
		{description: "negative: nsid with no value", src: &nsidFake{}, args: "list-id nsid",
			expected: result{log: mapInit, err: ErrInvalidArgument}},
		{description: "negative: nsid not a number", src: &nsidFake{}, args: "list-id nsid x",
			expected: result{log: mapInit, err: ErrInvalidArgument}},
		{description: "boundary: nsid -1 is refused, invarg naming argv[1], NULL past the last word",
			src: &nsidFake{}, args: "list-id nsid -1", errText: `"(null)" is wrong: "nsid" value should be >= 0`,
			expected: result{log: mapInit, err: ErrInvalidArgument}},
		{description: "corner: invarg names the word after a negative value, not the value",
			src: &nsidFake{}, args: "list-id target-nsid -3 nsid 1",
			errText:  `"nsid" is wrong: "target-nsid" value should be >= 0`,
			expected: result{log: mapInit, err: ErrInvalidArgument}},
		{description: "boundary: nsid 0 is a get", src: &nsidFake{}, args: "list-id nsid 0",
			expected: result{log: mapInit + ",get:0", err: unix.EINVAL}},
		{description: "negative: target-nsid twice", src: &nsidFake{}, args: "list-id target-nsid 1 target-nsid 1",
			expected: result{log: mapInit, err: ErrInvalidArgument}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			netnsDir(t, "p1", "p2", "u")
			names, err := netnsNames()
			if err != nil {
				t.Fatal(err)
			}
			want := tc.expected
			want.log = netnsLog(want.log, names)
			tc.src.ids = ids()
			stdout, err := runNetnsCtx(tc.src, tc.json, tc.args)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			if tc.errText != "" && !strings.Contains(err.Error(), tc.errText) {
				t.Errorf("err = %v, want it to hold %s", err, tc.errText)
			}
			got := result{stdout: stdout, log: strings.Join(tc.src.log, ","), err: tc.expected.err}
			if got != want {
				t.Errorf("got %+v\nwant %+v", got, want)
			}
		})
	}
}

// TestNetnsMapInit is netns_map_init's cache: each file's nsid, the
// unassigned ones skipped, and netns_map_add's -EEXIST keeping the first
// name, in directory order, for an nsid two files share (two mounts of one
// namespace).
//
// go test ./internal/goip/ -run TestNetnsMapInit
func TestNetnsMapInit(t *testing.T) {
	tests := []struct {
		description string
		ids         map[string]int32
		// expected is the cache, FIRST:N standing for whichever of the files
		// with nsid N the directory lists first.
		expected map[int32]string
	}{
		{description: "positive: each assigned file cached by its nsid",
			ids: map[string]int32{"p1": 1, "p2": 2}, expected: map[int32]string{1: "p1", 2: "p2"}},
		{description: "negative: an unassigned file is not cached",
			ids: map[string]int32{"p1": 1, "u": -1}, expected: map[int32]string{1: "p1"}},
		{description: "boundary: nsid 0 is cached",
			ids: map[string]int32{"z": 0}, expected: map[int32]string{0: "z"}},
		{description: "corner: two files with one nsid keep the first listed",
			ids: map[string]int32{"a": 3, "b": 3, "c": 3}, expected: map[int32]string{3: "FIRST:3"}},
		{description: "corner: a file the kernel does not answer is not cached",
			ids: map[string]int32{"p1": 1, "gone": 9}, expected: map[int32]string{1: "p1"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var files []string
			for n := range tc.ids {
				files = append(files, n)
			}
			netnsDir(t, files...)
			names, err := netnsNames()
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]int32{}
			for n, id := range tc.ids {
				// gone has no ids entry, so the fake refuses it.
				if n != "gone" {
					ids[n] = id
				}
			}
			want := map[int32]string{}
			for id, n := range tc.expected {
				want[id] = n
				if n == fmt.Sprintf("FIRST:%d", id) {
					for _, f := range names {
						if tc.ids[f] == id {
							want[id] = f
							break
						}
					}
				}
			}
			c := &runCtx{src: &nsidFake{ids: ids}}
			got := netnsMapInit(service.New(c.src, c.nextSeq))
			if !maps.Equal(map[int32]string(got), want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// netnsLog expands TestNetnsListID's MAP, WALK and WALK:NAME into the
// "fd:NAME" requests they stand for, names being the directory's order.
func netnsLog(log string, names []string) string {
	walk := func(stop string) string {
		var out []string
		for _, n := range names {
			out = append(out, "fd:"+n)
			if n == stop {
				break
			}
		}
		return strings.Join(out, ",")
	}
	for _, n := range names {
		log = strings.ReplaceAll(log, "WALK:"+n, walk(n))
	}
	log = strings.ReplaceAll(log, "WALK", walk(""))
	return strings.ReplaceAll(log, "MAP", walk(""))
}

func ptrInt32(v int32) *int32 { return &v }

// TestNetnsVerbs is do_netns ahead of the verbs: invalid_name on the word
// after the verb, then matches() in its order.
//
// go test ./internal/goip/ -run TestNetnsVerbs
func TestNetnsVerbs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    error
	}{
		{"negative: no verb is the listing, not netlink", "", ErrNotImplemented},
		{"negative: `l` is list, ahead of list-id", "l", ErrNotImplemented},
		{"negative: `show`", "show", ErrNotImplemented},
		{"negative: `a` is add", "a x", ErrNotImplemented},
		{"negative: `h` is help", "h", ErrNotImplemented},
		{"negative: `d` is delete", "d x", ErrNotImplemented},
		{"negative: exec", "exec x ls", ErrNotImplemented},
		{"negative: an unknown verb", "frob", ErrInvalidArgument},
		{"negative: `list-idx` outruns list-id", "list-idx", ErrInvalidArgument},
		{"negative: a name with a slash, whatever the verb", "frob a/b", ErrInvalidArgument},
		{"boundary: `.` is no name", "set . 1", ErrInvalidArgument},
		{"boundary: `..` is no name", "set .. 1", ErrInvalidArgument},
		{"boundary: 256 bytes is too long", "set " + strings.Repeat("n", 256) + " 1", ErrInvalidArgument},
		{"boundary: 255 bytes is a name, so set writes", "set " + strings.Repeat("n", 255) + " 1", nil},
		{"corner: `...` is a name, so set writes", "set ... 1", nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			netnsDir(t, "...", strings.Repeat("n", 255))
			t.Setenv(writeOutEnvCst, filepath.Join(t.TempDir(), "write.bin"))
			_, err := runNetnsCtx(&nsidFake{}, false, tc.args)
			if !errors.Is(err, tc.expected) {
				t.Errorf("err = %v, want %v", err, tc.expected)
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
