//go:build goip_write

package goip

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fakeNetns answers netns_get_fd's opens from a set of paths, each a
// descriptor dup'd to netnsFakeFdBase and up, so a row's IFLA_NET_NS_FD is
// known. It lists the paths tried.
type fakeNetns struct {
	exists map[string]bool
	tried  []string
	next   int
}

const (
	netnsFakeFdBase = 900
	netnsFakeRunDir = "/fake/netns"
)

func (f *fakeNetns) open(path string) (int, error) {
	f.tried = append(f.tried, path)
	if !f.exists[path] {
		return -1, unix.ENOENT
	}
	fd, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	target := netnsFakeFdBase + f.next
	f.next++
	err = unix.Dup3(fd, target, unix.O_CLOEXEC)
	if cerr := unix.Close(fd); err != nil || cerr != nil {
		return -1, errors.Join(err, cerr)
	}
	return target, nil
}

// useFakeNetns points netnsOpen and netnsRunDir at f for the test.
func useFakeNetns(t *testing.T, f *fakeNetns) {
	t.Helper()
	prevOpen, prevDir := netnsOpen, netnsRunDir
	netnsOpen, netnsRunDir = f.open, netnsFakeRunDir
	t.Cleanup(func() { netnsOpen, netnsRunDir = prevOpen, prevDir })
}

// TestLinkWriteNetns is iplink_parse's netns arm (ip/iplink.c:964) over
// netns_get_fd (lib/namespace.c:105): a name under NETNS_RUN_DIR, a path,
// or a pid; IFLA_NET_NS_FD in the keyword's place; and move_netns, which
// makes index legal on a set as IFLA_NEW_IFINDEX (:1383).
//
// go test ./internal/goip/ -run TestLinkWriteNetns
func TestLinkWriteNetns(t *testing.T) {
	fd := func(n int) xtcpnl.LinkAttr {
		return xtcpnl.LinkAttrU32(unix.IFLA_NET_NS_FD, uint32(netnsFakeFdBase+n))
	}
	set := func(a ...xtcpnl.LinkAttr) xtcpnl.LinkWrite {
		return xtcpnl.LinkWrite{MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, Index: 2, Attrs: a}
	}
	ifname := func(n string) xtcpnl.LinkAttr { return xtcpnl.LinkAttrString(unix.IFLA_IFNAME, n) }
	vethPeer := func(p xtcpnl.VethPeer) []byte {
		b, err := xtcpnl.EncodeVethPeer(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	invalid := func(v string) string { return fmt.Sprintf(`Error: argument %q is wrong: Invalid "netns" value`, v) }
	n1 := netnsFakeRunDir + "/n1"
	tests := []struct {
		description string
		verb        string
		args        []string
		tried       []string // the paths netns_get_fd opened, in order
		expected    xtcpnl.LinkWrite
		errMsg      string // a fragment of ip's refusal, ErrLinkUsage
	}{
		{description: "positive: by name, under NETNS_RUN_DIR", verb: "set", args: []string{"dev", "d0", "netns", "n1"},
			tried: []string{n1}, expected: set(fd(0))},
		{description: "positive: by pid, after the name doesn't open", verb: "set", args: []string{"dev", "d0", "netns", "77"},
			tried: []string{netnsFakeRunDir + "/77", "/proc/77/ns/net"}, expected: set(fd(0))},
		{description: "positive: by path", verb: "set", args: []string{"dev", "d0", "netns", "/proc/self/ns/net"},
			tried: []string{"/proc/self/ns/net"}, expected: set(fd(0))},
		{description: "positive: by a pid's path", verb: "set", args: []string{"dev", "d0", "netns", "/proc/77/ns/net"},
			tried: []string{"/proc/77/ns/net"}, expected: set(fd(0))},
		{description: "positive: in the keyword's place", verb: "set", args: []string{"dev", "d0", "carrier", "on", "netns", "n1", "mtu", "1400"},
			tried: []string{n1}, expected: set(xtcpnl.LinkAttrU8(unix.IFLA_CARRIER, 1), fd(0), xtcpnl.LinkAttrU32(unix.IFLA_MTU, 1400))},
		{description: "positive: index on a set that moves, IFLA_NEW_IFINDEX last", verb: "set", args: []string{"dev", "d0", "index", "100", "netns", "n1"},
			tried: []string{n1}, expected: set(fd(0), xtcpnl.LinkAttrU32(unix.IFLA_NEW_IFINDEX, 100))},
		{description: "positive: index on an add is ifi_index", verb: "add", args: []string{"d9", "netns", "n1", "index", "100", "type", "dummy"},
			tried: []string{n1}, expected: xtcpnl.LinkWrite{
				MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK,
				Index: 100, Kind: "dummy", Attrs: []xtcpnl.LinkAttr{fd(0), ifname("d9")},
			}},
		{description: "positive: a veth peer's netns, in the peer", verb: "add", args: []string{"v9", "type", "veth", "peer", "name", "v10", "netns", "n1"},
			tried: []string{n1}, expected: xtcpnl.LinkWrite{
				MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK,
				Kind: "veth", Attrs: []xtcpnl.LinkAttr{ifname("v9")}, HasData: true,
				Data: vethPeer(xtcpnl.VethPeer{Attrs: []xtcpnl.LinkAttr{fd(0), ifname("v10")}}),
			}},
		{description: "corner: a file that isn't a namespace is sent; the kernel refuses it", verb: "set",
			args: []string{"dev", "d0", "netns", "/proc/self/stat"}, tried: []string{"/proc/self/stat"}, expected: set(fd(0))},
		{description: "corner: with link-netnsid, both sent", verb: "set", args: []string{"dev", "d0", "netns", "n1", "link-netnsid", "5"},
			tried: []string{n1}, expected: set(fd(0), xtcpnl.LinkAttrU32(unix.IFLA_LINK_NETNSID, 5))},
		{description: "negative: pid 0", verb: "set", args: []string{"dev", "d0", "netns", "0"},
			tried: []string{netnsFakeRunDir + "/0", "/proc/0/ns/net"}, errMsg: invalid("0")},
		{description: "negative: pid -1", verb: "set", args: []string{"dev", "d0", "netns", "-1"},
			tried: []string{netnsFakeRunDir + "/-1", "/proc/-1/ns/net"}, errMsg: invalid("-1")},
		{description: "negative: a missing path isn't tried as a pid", verb: "set", args: []string{"dev", "d0", "netns", "/nonexistent"},
			tried: []string{"/nonexistent"}, errMsg: invalid("/nonexistent")},
		{description: "negative: a missing name that isn't a number", verb: "set", args: []string{"dev", "d0", "netns", "nosuch"},
			tried: []string{netnsFakeRunDir + "/nosuch"}, errMsg: invalid("nosuch")},
		{description: "negative: netns twice, refused before the second open", verb: "set", args: []string{"dev", "d0", "netns", "n1", "netns", "n1"},
			tried: []string{n1}, errMsg: `Error: duplicate "netns": "n1" is the second value`},
		{description: "negative: netns as the last word", verb: "set", args: []string{"dev", "d0", "netns"},
			errMsg: "Command line is not complete"},
		{description: "negative: index on a set that doesn't move", verb: "set", args: []string{"dev", "d0", "index", "100"},
			errMsg: "index can be used only when creating devices or when moving device to another netns"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &fakeNetns{exists: map[string]bool{
				n1: true, "/proc/77/ns/net": true, "/proc/self/ns/net": true, "/proc/self/stat": true,
			}}
			useFakeNetns(t, f)
			var fds openFds
			defer fds.close()
			got, err := buildLinkWrite(io.Discard, lookupLinkWriteVerb(tc.verb), unix.AF_UNSPEC, tc.args,
				newFakeLinks().resolve, func(string) (bool, error) { return false, nil }, nil, &fds)
			if !slices.Equal(f.tried, tc.tried) {
				t.Errorf("tried %q, want %q", f.tried, tc.tried)
			}
			if tc.errMsg != "" {
				if !errors.Is(err, ErrLinkUsage) || !strings.Contains(fmt.Sprint(err), tc.errMsg) {
					t.Fatalf("err = %v, want ErrLinkUsage containing %q", err, tc.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("LinkWrite mismatch\n got  %+v\n want %+v", got, tc.expected)
			}
			if len(fds) != f.next {
				t.Errorf("%d descriptors kept, %d opened", len(fds), f.next)
			}
		})
	}
}

// TestOpenFdsClose holds that the descriptors a command kept are closed.
func TestOpenFdsClose(t *testing.T) {
	tests := []struct {
		description string
		n           int
		expected    int // descriptors still open after close
	}{
		{description: "positive: two descriptors", n: 2, expected: 0},
		{description: "boundary: none", n: 0, expected: 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var fds openFds
			var kept []int
			for range tc.n {
				fd, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
				if err != nil {
					t.Fatal(err)
				}
				fds.add(fd)
				kept = append(kept, fd)
			}
			fds.close()
			open := 0
			for _, fd := range kept {
				if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err == nil {
					open++
				}
			}
			if open != tc.expected || len(fds) != 0 {
				t.Errorf("%d still open, %d kept; want %d, 0", open, len(fds), tc.expected)
			}
		})
	}
}
