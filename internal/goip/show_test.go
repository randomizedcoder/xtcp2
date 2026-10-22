package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// showRecorder is a Source and TalkSource that answers every dump with no
// replies and every get with ENODEV, recording each request. It is the
// "kernel with no interfaces" the device and read-only rows need.
type showRecorder struct {
	requests [][]byte
}

func (s *showRecorder) Dump(request []byte, _ uint16) ([][]byte, error) {
	s.requests = append(s.requests, slices.Clone(request))
	return nil, nil
}

func (s *showRecorder) Talk(request []byte, _ uint16) ([]byte, error) {
	s.requests = append(s.requests, slices.Clone(request))
	return nil, fmt.Errorf("goip: talk: %w", unix.ENODEV)
}

// openReplay opens pcap the way Run does under GOIP_REPLAY.
func openReplay(t *testing.T, pcap string) Source {
	t.Helper()
	src, err := OpenReplay(pcap)
	if err != nil {
		t.Fatalf("OpenReplay(%s): %v", pcap, err)
	}
	return src
}

// TestShow holds Show to Run: for every row, Show's output must equal what Run
// prints for r.Argv() against the same capture, and the JSON rows must also
// match the sidecar `ip -j` wrote during the capture.
//
// go test ./internal/goip/ -run TestShow$
func TestShow(t *testing.T) {
	tests := []struct {
		name        string
		description string
		pcap        string
		req         ShowRequest
		sidecar     string // a JSON sidecar to compare against, or ""
		wantErr     error  // nil means success and Run parity
	}{
		{
			name: "addr_json", description: "positive: address -j reproduces ip_addr_json",
			pcap: guestDumpsDir + "netlink_route_getaddr.pcap",
			req:  ShowRequest{Object: "address", JSON: true}, sidecar: "ip_addr_json",
		},
		{
			name: "addr_text", description: "positive: address text equals Run's",
			pcap: guestDumpsDir + "netlink_route_getaddr.pcap",
			req:  ShowRequest{Object: "address"},
		},
		{
			name: "addr_dev", description: "positive: address show dev goip0",
			pcap: addrDevPcap,
			req:  ShowRequest{Object: "address", Selectors: []string{"dev", devNameCst}},
		},
		{
			name: "link_json", description: "positive: link -j reproduces ip_link_json",
			pcap: guestDumpsDir + "netlink_route_getlink.pcap",
			req:  ShowRequest{Object: "link", JSON: true}, sidecar: "ip_link_json",
		},
		{
			name: "link_stats", description: "positive: link -s equals Run's",
			pcap: guestDumpsDir + "netlink_route_getlink_stats.pcap",
			req:  ShowRequest{Object: "link", Stats: true},
		},
		{
			name: "route_main_json", description: "positive: route -j table main reproduces ip_route_main_json",
			pcap: guestDumpsDir + "netlink_route_getroute.pcap",
			req:  ShowRequest{Object: "route", JSON: true}, sidecar: "ip_route_main_json",
		},
		{
			name: "route_table_all", description: "positive: route show table all equals Run's",
			pcap: guestDumpsDir + "netlink_route_getroute_table_all.pcap",
			req:  ShowRequest{Object: "route", Selectors: []string{"table", "all"}},
		},
		{
			name: "neigh_proxy_json", description: "positive: neighbor -j proxy reproduces ip_neigh_proxy_json",
			pcap: guestDumpsDir + "netlink_route_getneigh_proxy.pcap",
			req:  ShowRequest{Object: "neighbor", JSON: true, Selectors: []string{"proxy"}}, sidecar: "ip_neigh_proxy_json",
		},
		{
			name: "rule", description: "positive: rule show equals Run's",
			pcap: ruleDumpPcap, req: ShowRequest{Object: "rule", JSON: true},
		},
		{
			name: "nexthop", description: "positive: nexthop show equals Run's",
			pcap: guestDumpsDir + "netlink_route_getnexthop.pcap", req: ShowRequest{Object: "nexthop", JSON: true},
		},
		{
			name: "addrlabel", description: "positive: addrlabel show equals Run's",
			pcap: addrLabelDumpPcap, req: ShowRequest{Object: "addrlabel", JSON: true},
		},
		{
			name: "ntable", description: "positive: ntable show equals Run's",
			pcap: ntableDumpPcap, req: ShowRequest{Object: "ntable", JSON: true},
		},
		{
			name: "netconf", description: "positive: netconf show equals Run's",
			pcap: netconfPcapBase, req: ShowRequest{Object: "netconf", JSON: true},
		},
		{
			name: "vrf", description: "positive: vrf show equals Run's",
			pcap: vrfPcapBase, req: ShowRequest{Object: "vrf"},
		},
		{
			name: "stats_group_link", description: "positive: stats show group link equals Run's",
			pcap: statsPcapBase, req: ShowRequest{Object: "stats", Selectors: []string{"group", "link"}},
		},
		{
			name: "write_verb_as_selector", description: "negative: `set dev lo mtu 1` after show is an unknown selector, never a write",
			pcap: guestDumpsDir + "netlink_route_getlink.pcap",
			req:  ShowRequest{Object: "link", Selectors: []string{"set", "dev", "lo", "mtu", "1"}}, wantErr: ErrNotImplemented,
		},
		{
			name: "unknown_object", description: "negative: tunnel is an object goip does not show",
			pcap: guestDumpsDir + "netlink_route_getlink.pcap",
			req:  ShowRequest{Object: "tunnel"}, wantErr: ErrUnknownObject,
		},
		{
			name: "prefix_object", description: "negative: `a` abbreviates address on a command line, not here",
			pcap: guestDumpsDir + "netlink_route_getlink.pcap",
			req:  ShowRequest{Object: "a"}, wantErr: ErrUnknownObject,
		},
		{
			name: "alias_object", description: "boundary: ntbl is a CLI alias of ntable; Show has one spelling per object",
			pcap: guestDumpsDir + "netlink_route_getlink.pcap",
			req:  ShowRequest{Object: "ntbl"}, wantErr: ErrUnknownObject,
		},
		{
			name: "bad_family", description: "boundary: AF_BRIDGE is not one of -4/-6/-0",
			pcap: guestDumpsDir + "netlink_route_getlink.pcap",
			req:  ShowRequest{Object: "link", Family: unix.AF_BRIDGE}, wantErr: ErrInvalidArgument,
		},
		{
			name: "stats_bare", description: "boundary: a bare stats show needs a group, as on the CLI",
			pcap: statsPcapBase, req: ShowRequest{Object: "stats"}, wantErr: ErrNotImplemented,
		},
		{
			name: "addr_packet", description: "corner: -0 address lists links only and equals Run's",
			pcap: guestDumpsDir + "netlink_route_getaddr.pcap",
			req:  ShowRequest{Object: "address", Family: unix.AF_PACKET},
		},
		{
			name: "addr_v6_stats", description: "corner: -6 -s address equals Run's",
			pcap: guestDumpsDir + "netlink_route_getaddr_v6_stats.pcap",
			req:  ShowRequest{Object: "address", Family: unix.AF_INET6, Stats: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got bytes.Buffer
			err := Show(openReplay(t, tc.pcap), tc.req, &got)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: Show err = %v, want %v", tc.description, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: Show: %v", tc.description, err)
			}

			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.req.Argv(), &stdout, &stderr); code != ExitOK {
				t.Fatalf("%s: Run(%q) = %d, stderr=%s", tc.description, tc.req.Argv(), code, stderr.String())
			}
			if !bytes.Equal(got.Bytes(), stdout.Bytes()) {
				t.Fatalf("%s: Show and Run(%q) differ\nShow:\n%s\nRun:\n%s", tc.description, tc.req.Argv(), got.String(), stdout.String())
			}
			if tc.sidecar != "" {
				want, rerr := os.ReadFile(guestDumpsDir + tc.sidecar)
				if rerr != nil {
					t.Fatal(rerr)
				}
				assertJSONEntriesEqual(t, got.Bytes(), want, false)
			}
		})
	}
}

// TestShowNoSuchDevice pins ErrNoSuchDevice on every dev-taking object, for
// both ways a device can be missing: the kernel's ENODEV on a by-name get, and
// a miss in the link cache.
//
// go test ./internal/goip/ -run TestShowNoSuchDevice
func TestShowNoSuchDevice(t *testing.T) {
	tests := []struct {
		name        string
		description string
		req         ShowRequest
		want        bool // errors.Is(err, ErrNoSuchDevice)
		alsoIs      error
	}{
		{name: "link_dev", description: "positive: link show dev: kernel ENODEV becomes ErrNoSuchDevice",
			req: ShowRequest{Object: "link", Selectors: []string{"dev", "nosuch0"}}, want: true, alsoIs: unix.ENODEV},
		{name: "addr_dev", description: "positive: address show dev: ll_name_to_index's miss is ip's `Device \"nosuch0\" does not exist`, an invalid argument",
			req: ShowRequest{Object: "address", Selectors: []string{"dev", "nosuch0"}}, want: true, alsoIs: ErrInvalidArgument},
		{name: "route_dev", description: "positive: route show dev",
			req: ShowRequest{Object: "route", Selectors: []string{"dev", "nosuch0"}}, want: true, alsoIs: unix.ENODEV},
		{name: "neigh_dev", description: "positive: neighbor show dev: link cache miss",
			req: ShowRequest{Object: "neighbor", Selectors: []string{"dev", "nosuch0"}}, want: true},
		{name: "nexthop_dev", description: "positive: nexthop show dev: link cache miss",
			req: ShowRequest{Object: "nexthop", Selectors: []string{"dev", "nosuch0"}}, want: true},
		{name: "netconf_dev", description: "positive: netconf show dev keeps its ErrNotImplemented exit code too",
			req: ShowRequest{Object: "netconf", Selectors: []string{"dev", "nosuch0"}}, want: true, alsoIs: ErrNotImplemented},
		{name: "stats_dev", description: "positive: stats show dev",
			req: ShowRequest{Object: "stats", Selectors: []string{"dev", "nosuch0", "group", "link"}}, want: true, alsoIs: unix.ENODEV},
		{name: "no_dev", description: "negative: link show with no dev on an empty kernel is success, not a missing device",
			req: ShowRequest{Object: "link"}, want: false},
		{name: "dev_missing_value", description: "boundary: `dev` with no name is a parse error, not a missing device",
			req: ShowRequest{Object: "netconf", Selectors: []string{"dev"}}, want: false, alsoIs: ErrNotImplemented},
		{name: "dev_15_bytes", description: "boundary: a 15-byte name (IFNAMSIZ-1) is looked up, then missing",
			req: ShowRequest{Object: "neighbor", Selectors: []string{"dev", "abcdefghijklmno"}}, want: true},
		{name: "master_missing", description: "corner: nexthop master NAME misses the same way dev does",
			req: ShowRequest{Object: "nexthop", Selectors: []string{"master", "nosuch0"}}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Show(&showRecorder{}, tc.req, &bytes.Buffer{})
			if got := errors.Is(err, ErrNoSuchDevice); got != tc.want {
				t.Fatalf("%s: errors.Is(%v, ErrNoSuchDevice) = %v, want %v", tc.description, err, got, tc.want)
			}
			if tc.alsoIs != nil && !errors.Is(err, tc.alsoIs) {
				t.Fatalf("%s: errors.Is(%v, %v) = false, want true", tc.description, err, tc.alsoIs)
			}
		})
	}
}

// TestNoSuchDeviceKeepsCLIMessage is the guard on the wrapper: adding
// ErrNoSuchDevice must not change one byte of what `ip`'s wording reads like
// on stderr, nor the exit code.
func TestNoSuchDeviceKeepsCLIMessage(t *testing.T) {
	tests := []struct {
		name        string
		description string
		err         error
		wantMsg     string
	}{
		{name: "plain", description: "positive: message is the inner error's",
			err: noSuchDevice(errors.New("neigh show: cannot find device \"x\"")), wantMsg: "neigh show: cannot find device \"x\""},
		{name: "wrapped_invarg", description: "negative: the inner sentinel is still reachable, so the exit code is unchanged",
			err: noSuchDevice(fmt.Errorf("%w: Device %q does not exist", ErrInvalidArgument, "x")), wantMsg: "invalid argument: Device \"x\" does not exist"},
		{name: "empty", description: "boundary: an empty inner message stays empty",
			err: noSuchDevice(errors.New("")), wantMsg: ""},
		{name: "outer_wrap", description: "corner: wrapping again keeps both sentinels reachable",
			err: fmt.Errorf("goip: %w", noSuchDevice(fmt.Errorf("x: %w", ErrNotImplemented))), wantMsg: "goip: x: object recognized but not implemented by goip"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Error() != tc.wantMsg {
				t.Fatalf("%s: message %q, want %q", tc.description, tc.err.Error(), tc.wantMsg)
			}
			if !errors.Is(tc.err, ErrNoSuchDevice) {
				t.Fatalf("%s: lost ErrNoSuchDevice", tc.description)
			}
			if tc.name == "wrapped_invarg" && !errors.Is(tc.err, ErrInvalidArgument) {
				t.Fatalf("%s: lost ErrInvalidArgument", tc.description)
			}
			if tc.name == "outer_wrap" && !errors.Is(tc.err, ErrNotImplemented) {
				t.Fatalf("%s: lost ErrNotImplemented", tc.description)
			}
		})
	}
}

// TestShowObjectsReadOnly is the guard on the HTTP paths' safety property:
// whatever selectors arrive, Show sends only RTM_GET* requests.
//
// go test ./internal/goip/ -run TestShowObjectsReadOnly
func TestShowObjectsReadOnly(t *testing.T) {
	t.Run("exact_set", func(t *testing.T) {
		// positive: the 12 implemented show objects, sorted, and a copy.
		want := []string{"address", "addrlabel", "link", "mroute", "mrule", "neighbor", "netconf", "nexthop", "ntable", "route", "rule", "stats", "token", "vrf"}
		got := ShowObjects()
		if !slices.Equal(got, want) {
			t.Fatalf("ShowObjects() = %q, want %q", got, want)
		}
		got[0] = "mutated"
		if ShowObjects()[0] != "address" {
			t.Fatal("ShowObjects returned the backing slice")
		}
	})

	t.Run("every_object_implemented", func(t *testing.T) {
		// corner: every ShowObjects name resolves to an object with a run
		// function, so the allowlist cannot name a stub.
		for _, o := range ShowObjects() {
			if _, err := lookupObject(o); err != nil {
				t.Errorf("lookupObject(%q) = %v", o, err)
			}
		}
	})

	t.Run("no_write_capable", func(t *testing.T) {
		// negative: every object × every write verb, as the first selector.
		verbs := []string{"add", "del", "delete", "set", "change", "replace", "flush", "append", "prepend", "save", "restore"}
		for _, o := range ShowObjects() {
			for _, v := range verbs {
				rec := &showRecorder{}
				err := Show(rec, ShowRequest{Object: o, Selectors: []string{v, "dev", "lo"}}, &bytes.Buffer{})
				if err == nil {
					t.Errorf("Show(%s show %s) succeeded; a write verb after show must be rejected", o, v)
				}
				assertReadOnlyRequests(t, o+" "+v, rec.requests)
			}
		}
	})

	argvTests := []struct {
		name        string
		description string
		req         ShowRequest
		want        []string
	}{
		{name: "argv_minimal", description: "boundary: no options, no selectors",
			req: ShowRequest{Object: "address"}, want: []string{"address", "show"}},
		{name: "argv_all_flags", description: "corner: options in Run's order before the object",
			req:  ShowRequest{Object: "route", Family: unix.AF_INET6, JSON: true, Stats: true, Details: true, Selectors: []string{"table", "all"}},
			want: []string{"-6", "-j", "-s", "-d", "route", "show", "table", "all"}},
		{name: "argv_packet", description: "positive: AF_PACKET is -0",
			req: ShowRequest{Object: "address", Family: unix.AF_PACKET}, want: []string{"-0", "address", "show"}},
		{name: "argv_selectors_verbatim", description: "negative: selectors are not reinterpreted, a keyword-like value stays a value",
			req: ShowRequest{Object: "link", Selectors: []string{"dev", "show"}}, want: []string{"link", "show", "dev", "show"}},
	}
	for _, tc := range argvTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.Argv(); !slices.Equal(got, tc.want) {
				t.Fatalf("%s: Argv() = %q, want %q", tc.description, got, tc.want)
			}
		})
	}
}

// assertReadOnlyRequests fails if any request is not an RTM_GET*. rtnetlink
// types come in NEW/DEL/GET/SET quadruples from RTM_BASE, so GET is
// (type-RTM_BASE)%4 == 2. Flags are not tested: on a GET, NLM_F_REPLACE and
// NLM_F_EXCL are the bits of NLM_F_DUMP, and the type alone decides whether
// the kernel changes anything.
func assertReadOnlyRequests(t *testing.T, what string, requests [][]byte) {
	t.Helper()
	for _, r := range requests {
		if len(r) < unix.NLMSG_HDRLEN {
			t.Errorf("%s: short request %x", what, r)
			continue
		}
		typ := binary.NativeEndian.Uint16(r[4:6])
		if typ < unix.RTM_BASE || (typ-unix.RTM_BASE)%4 != 2 {
			t.Errorf("%s: request type %d is not an RTM_GET*", what, typ)
		}
	}
}

// TestOpenNetlinkTimeout pins the server-side socket option.
//
// go test ./internal/goip/ -run TestOpenNetlinkTimeout
func TestOpenNetlinkTimeout(t *testing.T) {
	tests := []struct {
		name        string
		description string
		d           time.Duration
		wantErr     error
		// minUs and maxUs bound the readback: the kernel stores the timeout
		// in jiffies, so a sub-jiffy request reads back as one jiffy (1ms at
		// HZ=1000, 4ms at HZ=250). The property is "positive and close",
		// never zero, since zero is block-forever.
		minUs, maxUs int64
	}{
		{name: "five_seconds", description: "positive: 5s lands as SO_RCVTIMEO 5s",
			d: 5 * time.Second, minUs: 5e6, maxUs: 5e6 + 10e3},
		{name: "zero", description: "negative: zero would mean block forever, refused",
			d: 0, wantErr: ErrBadTimeout},
		{name: "one_ms", description: "boundary: 1ms reads back as about one jiffy",
			d: time.Millisecond, minUs: 1000, maxUs: 10e3},
		{name: "one_ns", description: "boundary: sub-microsecond rounds up, never down to zero (forever)",
			d: time.Nanosecond, minUs: 1, maxUs: 10e3},
		{name: "negative", description: "corner: a negative duration is refused",
			d: -time.Second, wantErr: ErrBadTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := OpenNetlinkTimeout(tc.d)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: err = %v, want %v", tc.description, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Skipf("netlink socket unavailable here: %v", err)
			}
			defer func() {
				if cerr := s.Close(); cerr != nil {
					t.Error(cerr)
				}
			}()
			tv, err := unix.GetsockoptTimeval(s.fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO)
			if err != nil {
				t.Fatal(err)
			}
			if us := tv.Nano() / 1e3; us < tc.minUs || us > tc.maxUs {
				t.Fatalf("%s: SO_RCVTIMEO = %dus, want [%d, %d]", tc.description, us, tc.minUs, tc.maxUs)
			}
		})
	}

	t.Run("idle_recv_times_out", func(t *testing.T) {
		// corner: a receive with nothing queued returns EAGAIN, not a hang.
		s, err := OpenNetlinkTimeout(10 * time.Millisecond)
		if err != nil {
			t.Skipf("netlink socket unavailable here: %v", err)
		}
		defer func() { _ = s.Close() }()
		buf := make([]byte, 64)
		start := time.Now()
		_, _, rerr := unix.Recvfrom(s.fd, buf, 0)
		if !errors.Is(rerr, unix.EAGAIN) {
			t.Fatalf("Recvfrom = %v, want EAGAIN", rerr)
		}
		if el := time.Since(start); el > 2*time.Second {
			t.Fatalf("Recvfrom took %v", el)
		}
	})
}
