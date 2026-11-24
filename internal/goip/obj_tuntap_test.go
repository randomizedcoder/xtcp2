package goip

import (
	"fmt"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
	"golang.org/x/sys/unix"
)

// tunProps is a device's three sysfs properties.
func tunProps(dev, flags, owner, group string) map[string]fakeProp {
	return map[string]fakeProp{dev + "/tun_flags": {v: flags}, dev + "/owner": {v: owner}, dev + "/group": {v: group}}
}

func tunFdinfo(dev string) string {
	return "pos:\t0\nflags:\t0100002\nmnt_id:\t15\nino:\t1033\niff:\t" + dev + "\n"
}

// TestGoipTuntapUnit drives ip tuntap's show over a fake link dump and a
// fake tnlsys.Sys.
//
// go test ./internal/goip/ -run TestGoipTuntapUnit
func TestGoipTuntapUnit(t *testing.T) {
	tap0 := tnlLink(4, unix.ARPHRD_ETHER, "tap0", "tun", false)
	tun1 := tnlLink(5, unix.ARPHRD_NONE, "tun1", "tun", false)
	props := tunProps("tap0", "0x3802", "1000", "100")
	holders := []tnlsys.TunFD{
		{Pid: 11, FD: 3, FdInfo: tunFdinfo("tap0"), Comm: "holder\n"},
		{Pid: 12, FD: 4, FdInfo: tunFdinfo("tun1"), Comm: "other\n"},
		{Pid: 13, FD: 3, FdInfo: tunFdinfo("tap0"), NoComm: true},
	}
	merge := func(ms ...map[string]fakeProp) map[string]fakeProp {
		out := map[string]fakeProp{}
		for _, m := range ms {
			for k, v := range m {
				out[k] = v
			}
		}
		return out
	}
	checkTnlUnit(t, []tnlUnitCase{
		// Positive.
		{description: "positive: tap with one_queue and persist, user and group",
			args: "tuntap show", links: [][]byte{tap0}, props: props,
			expected: "0|tap0: tap one_queue persist user 1000 group 100\n|"},
		{description: "positive: -j: flags as an array, user and group numbers",
			args: "tuntap", opts: "j", links: [][]byte{tap0}, props: props,
			expected: `0|[{"ifname":"tap0","flags":["tap","one_queue","persist"],"user":1000,"group":100}]` + "\n|"},
		{description: "positive: -d: the holders, run together, comm's newline cut (T15)",
			args: "tuntap show", opts: "d", links: [][]byte{tap0}, props: props, fds: holders,
			expected: "0|tap0: tap one_queue persist user 1000 group 100\n\tAttached to processes:holder(11)<NULL>(13)\n|"},
		{description: "positive: -j -d: a processes array of objects",
			args: "tuntap show", opts: "jd", links: [][]byte{tap0}, props: props, fds: holders,
			expected: `0|[{"ifname":"tap0","flags":["tap","one_queue","persist"],"user":1000,"group":100,` +
				`"processes":[{"name":"holder","pid":11},{"name":"<NULL>","pid":13}]}]` + "\n|"},
		// Negative.
		{description: "negative: an unreadable property skips the device after fopen's line",
			args: "tuntap show", links: [][]byte{tap0, tun1},
			props:    merge(map[string]fakeProp{"tap0/tun_flags": {err: unix.ENOENT}}, tunProps("tun1", "0x1001", "-1", "-1")),
			expected: "0|tun1: tun\n|fopen /sys/class/net/tap0/tun_flags: No such file or directory\n"},
		{description: "negative: a value that is not a number, then Failed to parse",
			args: "tuntap show", links: [][]byte{tap0}, props: merge(props, map[string]fakeProp{"tap0/owner": {v: "x1\n"}}),
			expected: "0||value \"x1\" in file /sys/class/net/tap0/owner is not a number\nFailed to parse /sys/class/net/tap0/owner\n"},
		{description: "negative: an unknown command exits -1",
			args: "tuntap bogus", expected: "255||Command \"bogus\" is unknown, try \"ip tuntap help\"."},
		{description: "negative: add is a write goip does not make",
			args: "tuntap add dev tap9 mode tap", expected: "1||not implemented"},
		{description: "boundary: -d with no holders prints the Attached line alone",
			args: "tuntap show", opts: "d", links: [][]byte{tap0}, props: props, expected: "0|tap0: tap one_queue persist user 1000 group 100\n\tAttached to processes:\n|"},
		// Boundary.
		{description: "boundary: no tun devices is an empty array",
			args: "tuntap show", opts: "j", expected: "0|[]\n|"},
		{description: "boundary: an empty file is currently unknown",
			args: "tuntap show", links: [][]byte{tap0}, props: merge(props, map[string]fakeProp{"tap0/group": {v: ""}}),
			expected: "0||property \"group\" in file /sys/class/net/tap0/group is currently unknown\n"},
		{description: "boundary: past a long is strtol's line, with no newline of its own",
			args: "tuntap show", links: [][]byte{tap0}, props: merge(props, map[string]fakeProp{"tap0/owner": {v: "99999999999999999999\n"}}),
			expected: "0||strtol /sys/class/net/tap0/owner: Numerical result out of range" + "Failed to parse /sys/class/net/tap0/owner\n"},
		{description: "boundary: help is usage",
			args: "tuntap help", expected: "255||Usage: ip tuntap { add | del | show | list | lst | help } [ dev PHYS_DEV ]"},
		// Corner.
		{description: "corner: arguments after show are ignored (T13)",
			args: "tuntap show dev tun1 bogus", links: [][]byte{tap0}, props: props,
			expected: "0|tap0: tap one_queue persist user 1000 group 100\n|"},
		{description: "corner: pi and filter print when their bit is clear; unknown bits as hex; -1 owner omitted",
			args: "tuntap show", links: [][]byte{tun1}, props: tunProps("tun1", "0x10011", "-1", "5"),
			expected: "0|tun1: tun pi filter 0x10010 group 5\n|"},
		{description: "corner: -o -d: the Attached line after a backslash, the newline stays (T16)",
			args: "tuntap show", opts: "od", links: [][]byte{tap0}, props: props, fds: holders[:1],
			expected: "0|tap0: tap one_queue persist user 1000 group 100\\\tAttached to processes:holder(11)\n|"},
		{description: "corner: an ARPHRD other than NONE or ETHER is not a tun device (T17)",
			args: "tuntap show", links: [][]byte{tnlLink(6, unix.ARPHRD_TUNNEL, "tap0", "tun", false)}, props: props,
			expected: "0||"},
		{description: "corner: a kind other than tun is skipped before sysfs",
			args: "tuntap show", links: [][]byte{tnlLink(6, unix.ARPHRD_ETHER, "v0", "veth", false)}, expected: "0||"},
	})
}

// TestFdinfoIffs is show_processes' fscanf loop over an fdinfo file.
//
// go test ./internal/goip/ -run TestFdinfoIffs
func TestFdinfoIffs(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    string
	}{
		{"positive: a tun descriptor's fdinfo", tunFdinfo("tap0"), "[tap0]"},
		{"negative: no iff line", "pos:\t0\nflags:\t02\n", "[]"},
		{"boundary: empty", "", "[]"},
		{"boundary: a key at the end with no value", "pos:\t0\niff:", "[]"},
		{"corner: a value is one word; the next word is the next key", "iff: a b: c\n", "[a]"},
		{"corner: a line starting with ':' stops the scan (ip loops forever)", ":x\niff: a\n", "[]"},
		{"corner: two iff lines", "iff:\ta\niff:\tb\n", "[a b]"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := fmt.Sprint(fdinfoIffs(tc.in)); got != tc.expected {
				t.Errorf("fdinfoIffs(%q) = %s, want %s", tc.in, got, tc.expected)
			}
		})
	}
}

// TestTaskName is get_task_name as show_processes prints it.
//
// go test ./internal/goip/ -run TestTaskName
func TestTaskName(t *testing.T) {
	tests := []struct {
		description string
		fd          tnlsys.TunFD
		expected    string
	}{
		{"positive: comm's newline cut", tnlsys.TunFD{Pid: 1, Comm: "qemu\n"}, "qemu"},
		{"negative: an unreadable comm", tnlsys.TunFD{Pid: 1, NoComm: true}, "<NULL>"},
		{"boundary: an empty comm is fgets' NULL", tnlsys.TunFD{Pid: 1, Comm: ""}, "<NULL>"},
		{"boundary: 63 bytes kept", tnlsys.TunFD{Pid: 1, Comm: strings.Repeat("a", 70)}, strings.Repeat("a", 63)},
		{"corner: pid 0 is refused", tnlsys.TunFD{Comm: "x\n"}, "<NULL>"},
		{"corner: a NUL ends the string", tnlsys.TunFD{Pid: 1, Comm: "a\x00b\n"}, "a"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := taskName(tc.fd); got != tc.expected {
				t.Errorf("taskName = %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestTnlTOS is parse_args' tos value.
//
// go test ./internal/goip/ -run TestTnlTOS
func TestTnlTOS(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    string
	}{
		{"positive: hex", "0x10", "0x10"},
		{"positive: inherit/hex sets bit 0", "inherit/0x10", "0x11"},
		{"negative: not a value", "zz", "error"},
		{"boundary: inherit alone is 1", "inherit", "0x01"},
		{"boundary: 0x100 is past a byte", "100", "error"},
		{"corner: what follows a slash after a value is dropped", "0x10/zz", "0x10"},
		{"corner: inherit/ with nothing after it reads an empty word", "inherit/", "error"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, err := tnlTOS(tc.in)
			got := fmt.Sprintf("0x%02x", v)
			if err != nil {
				got = "error"
			}
			if got != tc.expected {
				t.Errorf("tnlTOS(%q) = %s (%v), want %s", tc.in, got, err, tc.expected)
			}
		})
	}
}
