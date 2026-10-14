package linuxio

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func u64(n uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, n)
	return b
}

// statEntry is one STAT_HWCOUNTER_ENTRY, nested as nla_nest_start marks it.
func statEntry(name string, value uint64) []byte {
	return attribute(xtcpnl.RdmaNldevAttrStatHwcounterEntry|unix.NLA_F_NESTED, append(
		attribute(xtcpnl.RdmaNldevAttrStatHwcounterEntryName, []byte(name+"\x00")),
		attribute(xtcpnl.RdmaNldevAttrStatHwcounterEntryValue, u64(value))...))
}

// statBody is a STAT_GET reply for mlx5_0 index 3; entries go in the
// HWCOUNTERS nest and extra after it.
func statBody(index, port uint32, entries [][]byte, extra ...[]byte) []byte {
	body := append(attribute(xtcpnl.RdmaNldevAttrDevIndex, word(int32(index))),
		attribute(xtcpnl.RdmaNldevAttrDevName, []byte("mlx5_0\x00"))...)
	body = append(body, attribute(xtcpnl.RdmaNldevAttrPortIndex, word(int32(port)))...)
	body = append(body, attribute(xtcpnl.RdmaNldevAttrStatHwcounters|unix.NLA_F_NESTED, bytes.Join(entries, nil))...)
	for _, e := range extra {
		body = append(body, e...)
	}
	return body
}

func statEntries(n int) [][]byte {
	entries := make([][]byte, n)
	for i := range entries {
		entries[i] = statEntry("c"+strconv.Itoa(i), uint64(i))
	}
	return entries
}

func TestRDMAStatRequestTable(t *testing.T) {
	long := strings.Repeat("n", rdmaNameMax-1)
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		protocol                                     int
		port                                         uint32
		// reply builds the frames answering a request with sequence seq;
		// nil sends nothing, so the receive times out.
		reply   func(seq uint32, request []byte) [][]byte
		wantErr error
		bad     bool
		want    []RDMACounter
		closes  int
		noSend  bool
	}{
		{name: "reply", category: "positive", description: "valid doit reply with 31 counters", expectedOutcome: "31 counters in reply order",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, statEntries(31)))}
			}, want: func() []RDMACounter {
				out := make([]RDMACounter, 31)
				for i := range out {
					out[i] = RDMACounter{Name: "c" + strconv.Itoa(i), Value: uint64(i)}
				}
				return out
			}()},
		{name: "zero-counters", category: "boundary", description: "empty HWCOUNTERS nest", expectedOutcome: "no counters, no error",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, nil))}
			}, want: []RDMACounter{}},
		{name: "one-counter", category: "boundary", description: "a single counter", expectedOutcome: "that counter",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, [][]byte{statEntry("rx_write_requests", 9)}))}
			}, want: []RDMACounter{{"rx_write_requests", 9}}},
		{name: "counter-value-max", category: "boundary", description: "counter value 2^64-1", expectedOutcome: "parsed exactly",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, [][]byte{statEntry("x", ^uint64(0))}))}
			}, want: []RDMACounter{{"x", ^uint64(0)}}},
		{name: "name-63", category: "boundary", description: "counter name of 63 bytes, the most IB_DEVICE_NAME_MAX-sized names allow", expectedOutcome: "accepted",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, [][]byte{statEntry(long, 1)}))}
			}, want: []RDMACounter{{long, 1}}},
		{name: "name-64", category: "boundary", description: "counter name of 64 bytes", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, [][]byte{statEntry(long+"n", 1)}))}
			}, wantErr: ErrReply, closes: 1},
		{name: "ack-only", category: "negative", description: "ACK with no reply message, then nothing", expectedOutcome: "no value; the receive times out",
			reply: func(seq uint32, request []byte) [][]byte {
				return [][]byte{frame(unix.NLMSG_ERROR, 0, seq, append(word(0), request...))}
			}, bad: true, closes: 1},
		{name: "eopnotsupp", category: "negative", description: "error ACK -EOPNOTSUPP", expectedOutcome: "error wraps EOPNOTSUPP",
			reply: func(seq uint32, request []byte) [][]byte {
				return [][]byte{frame(unix.NLMSG_ERROR, 0, seq, append(word(-int32(unix.EOPNOTSUPP)), request...))}
			}, wantErr: unix.EOPNOTSUPP, closes: 1},
		{name: "eperm", category: "negative", description: "error ACK -EPERM", expectedOutcome: "error wraps EPERM",
			reply: func(seq uint32, request []byte) [][]byte {
				return [][]byte{frame(unix.NLMSG_ERROR, 0, seq, append(word(-int32(unix.EPERM)), request...))}
			}, wantErr: unix.EPERM, closes: 1},
		{name: "einval", category: "negative", description: "error ACK -EINVAL, the kernel's answer for a device index it cannot find", expectedOutcome: "error wraps EINVAL; the socket is retired as for any failure",
			reply: func(seq uint32, request []byte) [][]byte {
				return [][]byte{frame(unix.NLMSG_ERROR, 0, seq, append(word(-int32(unix.EINVAL)), request...))}
			}, wantErr: unix.EINVAL, closes: 1},
		{name: "truncated-nest", category: "negative", description: "HWCOUNTERS nest length runs past the message", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				body := statBody(3, 1, [][]byte{statEntry("x", 1)})
				binary.LittleEndian.PutUint16(body[28:], 0x200)
				return [][]byte{frame(rdmaStatGet, 0, seq, body)}
			}, wantErr: ErrReply, closes: 1},
		{name: "duplicate-dev-index", category: "negative", description: "DEV_INDEX twice with the same value", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, nil, attribute(xtcpnl.RdmaNldevAttrDevIndex, word(3))))}
			}, wantErr: ErrReply, closes: 1},
		{name: "duplicate-nest", category: "negative", description: "HWCOUNTERS twice", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, nil, attribute(xtcpnl.RdmaNldevAttrStatHwcounters|unix.NLA_F_NESTED, nil)))}
			}, wantErr: ErrReply, closes: 1},
		{name: "missing-value", category: "negative", description: "counter entry with a name and no value", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				entry := attribute(xtcpnl.RdmaNldevAttrStatHwcounterEntry|unix.NLA_F_NESTED, attribute(xtcpnl.RdmaNldevAttrStatHwcounterEntryName, []byte("x\x00")))
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, [][]byte{entry}))}
			}, wantErr: ErrReply, closes: 1},
		{name: "name-not-terminated", category: "negative", description: "counter name without its NUL", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				entry := attribute(xtcpnl.RdmaNldevAttrStatHwcounterEntry|unix.NLA_F_NESTED, append(
					attribute(xtcpnl.RdmaNldevAttrStatHwcounterEntryName, []byte("x")),
					attribute(xtcpnl.RdmaNldevAttrStatHwcounterEntryValue, u64(1))...))
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, [][]byte{entry}))}
			}, wantErr: ErrReply, closes: 1},
		{name: "slash-name", category: "negative", description: "counter name with '/', which could not be a sysfs file", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, [][]byte{statEntry("../x", 1)}))}
			}, wantErr: ErrReply, closes: 1},
		{name: "no-handle", category: "negative", description: "reply without DEV_NAME", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				body := append(attribute(xtcpnl.RdmaNldevAttrDevIndex, word(3)), attribute(xtcpnl.RdmaNldevAttrPortIndex, word(1))...)
				return [][]byte{frame(rdmaStatGet, 0, seq, body)}
			}, wantErr: ErrReply, closes: 1},
		{name: "wrong-index", category: "negative", description: "reply for device index 4", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(4, 1, nil))}
			}, wantErr: ErrReply, closes: 1},
		{name: "wrong-port", category: "negative", description: "reply for port 2", expectedOutcome: "ErrReply",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 2, nil))}
			}, wantErr: ErrReply, closes: 1},
		{name: "port-zero", category: "negative", description: "request for port 0", expectedOutcome: "ErrRequest before any socket", port: 0, noSend: true, wantErr: ErrRequest},
		{name: "wrong-protocol", category: "negative", description: "STAT_GET on a route socket", expectedOutcome: "ErrRequest before any socket", protocol: unix.NETLINK_ROUTE, noSend: true, wantErr: ErrRequest},
		{name: "wrong-seq", category: "corner", description: "only a reply with a stale sequence", expectedOutcome: "reply ignored; the receive times out",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq+1, statBody(3, 1, nil))}
			}, bad: true, closes: 1},
		{name: "multipart", category: "corner", description: "reply flagged NLM_F_MULTI then DONE", expectedOutcome: "accepted",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, unix.NLM_F_MULTI, seq, statBody(3, 1, [][]byte{statEntry("x", 1)})), frame(unix.NLMSG_DONE, 0, seq, word(0))}
			}, want: []RDMACounter{{"x", 1}}},
		{name: "unknown-attr", category: "corner", description: "top-level attribute the decoder does not read", expectedOutcome: "ignored",
			reply: func(seq uint32, _ []byte) [][]byte {
				return [][]byte{frame(rdmaStatGet, 0, seq, statBody(3, 1, [][]byte{statEntry("x", 1)}, attribute(120, word(1))))}
			}, want: []RDMACounter{{"x", 1}}},
		{name: "timeout", category: "corner", description: "no reply within the budget", expectedOutcome: "deadline error; the socket is retired", bad: true, closes: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			protocol, port := tc.protocol, tc.port
			if protocol == 0 {
				protocol = unix.NETLINK_RDMA
			}
			if port == 0 && !tc.noSend {
				port = 1
			}
			socket := &scriptSocket{}
			socket.onSend = func(data []byte) {
				if tc.reply != nil {
					socket.batches = []batch{{data: tc.reply(binary.LittleEndian.Uint32(data[8:]), data)}}
				}
			}
			c := scriptedClient(protocol, socket)
			got, err := c.GetRDMAStat(t.Context(), 3, port)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: got %v", tc.expectedOutcome, err)
				}
			case tc.bad:
				if err == nil {
					t.Fatalf("%s: no error", tc.expectedOutcome)
				}
			case err != nil:
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if err != nil {
				if len(got.Counters) != 0 {
					t.Fatalf("%s: partial result %v", tc.expectedOutcome, got)
				}
			} else if got.Name != "mlx5_0" || got.Index != 3 || got.Port != 1 || !equalCounters(got.Counters, tc.want) {
				t.Fatalf("%s: got %+v", tc.expectedOutcome, got)
			}
			if tc.noSend != (len(socket.sent) == 0) {
				t.Fatalf("%s: %d requests sent", tc.expectedOutcome, len(socket.sent))
			}
			if !tc.noSend {
				checkStatRequest(t, socket.sent[0], 3, port)
			}
			// Close retires a socket still open, so count only failures.
			if err != nil && socket.closes != tc.closes {
				t.Fatalf("%s: %d closes, want %d", tc.expectedOutcome, socket.closes, tc.closes)
			}
		})
	}
}

func equalCounters(a, b []RDMACounter) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// checkStatRequest requires a doit with no ACK whose attributes are the
// ones rdma statistic show sends: DEV_INDEX and PORT_INDEX.
func checkStatRequest(t *testing.T, request []byte, index, port uint32) {
	t.Helper()
	want := xtcpnl.BuildRdmaStatGetRequest(index, port, binary.LittleEndian.Uint32(request[8:]))
	if binary.LittleEndian.Uint16(request[4:]) != rdmaStatGet ||
		binary.LittleEndian.Uint16(request[6:]) != unix.NLM_F_REQUEST ||
		!bytes.Equal(request[unix.NLMSG_HDRLEN:], want[unix.NLMSG_HDRLEN:]) {
		t.Fatalf("request %x, want attributes of %x without NLM_F_ACK", request, want)
	}
}

// TestRDMAStatCapturedFixtures decodes what Linux sent rdma statistic show,
// one row per captured kernel, and checks the counters against rdma's own
// output and the request attributes against rdma's.
func TestRDMAStatCapturedFixtures(t *testing.T) {
	paths, err := filepath.Glob("../../../xtcpnl/testdata/*/rdma/stat_show.strace")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no stat_show captures: %v", err)
	}
	for _, path := range paths {
		kernel := filepath.Base(filepath.Dir(filepath.Dir(path)))
		t.Run(kernel, func(t *testing.T) {
			t.Logf("positive: STAT_GET exchange captured on Linux %s; expected: the counters rdma printed", kernel)
			request, reply := statExchange(t, path)
			var h xtcpnl.NlMsgHdr
			if _, err := xtcpnl.DeserializeNlMsgHdr(request, &h); err != nil {
				t.Fatal(err)
			}
			index, port, err := rdmaAttributes(request)
			if err != nil {
				t.Fatal(err)
			}
			mine, err := rdmaRequest(rdmaStatGet, index, port, h.Seq, false)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(mine[unix.NLMSG_HDRLEN:], request[unix.NLMSG_HDRLEN:h.Len]) {
				t.Fatalf("attributes %x, rdma sent %x", mine[unix.NLMSG_HDRLEN:], request[unix.NLMSG_HDRLEN:h.Len])
			}
			if _, err := xtcpnl.DeserializeNlMsgHdr(reply, &h); err != nil {
				t.Fatal(err)
			}
			got, err := decodeRDMAStat(reply[unix.NLMSG_HDRLEN:h.Len])
			if err != nil {
				t.Fatal(err)
			}
			if got.Index != index || got.Port != port {
				t.Fatalf("reply for %d/%d, asked %d/%d", got.Index, got.Port, index, port)
			}
			want := statOutput(t, strings.TrimSuffix(path, ".strace")+".out")
			if !equalCounters(got.Counters, want) {
				t.Fatalf("decoded %v, rdma printed %v", got.Counters, want)
			}
		})
	}
}

// statExchange returns the first STAT_GET request and the reply buffer from
// an strace -xx -e write=all -e read=all capture.
func statExchange(t *testing.T, path string) (request, reply []byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cur *[]byte
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	for scan.Scan() {
		line := scan.Text()
		switch {
		case strings.Contains(line, "sendto(") && strings.Contains(line, "nlmsg_type=0x1411") && request == nil:
			cur = &request
		case strings.Contains(line, "recvmsg(") && strings.Contains(line, "nlmsg_type=0x1411") && request != nil && reply == nil:
			cur = &reply
		case strings.HasPrefix(strings.TrimSpace(line), "| ") && cur != nil:
			// " | 00000  6c 00 ... 00  ascii |": 16 bytes after the offset.
			fields := strings.TrimSpace(line)[2:]
			if len(fields) < 7+49 {
				fields += strings.Repeat(" ", 7+49-len(fields))
			}
			for _, h := range strings.Fields(fields[7 : 7+49]) {
				b, err := hex.DecodeString(h)
				if err != nil || len(b) != 1 {
					t.Fatalf("hex dump %q", line)
				}
				*cur = append(*cur, b[0])
			}
		case !strings.HasPrefix(strings.TrimSpace(line), "*"):
			cur = nil
		}
	}
	if err := scan.Err(); err != nil || request == nil || reply == nil {
		t.Fatalf("no STAT_GET exchange in %s: %v", path, err)
	}
	return request, reply
}

// statOutput reads rdma's "    name value" lines.
func statOutput(t *testing.T, path string) []RDMACounter {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []RDMACounter
	for _, line := range strings.Split(string(b), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			t.Fatalf("%s: %q", path, line)
		}
		out = append(out, RDMACounter{Name: fields[0], Value: n})
	}
	return out
}

func FuzzRDMAStat(f *testing.F) {
	f.Add(statBody(3, 1, statEntries(3)))
	f.Add(statBody(3, 1, nil))
	f.Fuzz(func(t *testing.T, body []byte) {
		s, err := decodeRDMAStat(body)
		if err != nil {
			return
		}
		for _, c := range s.Counters {
			if !rdmaName(c.Name, rdmaNameMax) {
				t.Fatalf("admitted counter name %q", c.Name)
			}
		}
		if !rdmaName(s.Name, rdmaNameMax) {
			t.Fatalf("admitted device name %q", s.Name)
		}
	})
}

// TestLiveRDMAStat checks STAT_GET against sysfs on the host's RDMA ports:
// every hw_counters file but lifespan is in the reply, and a counter read
// from sysfs afterwards never went backwards. Skipped without RDMA devices.
func TestLiveRDMAStat(t *testing.T) {
	c := liveClient(t, unix.NETLINK_RDMA)
	devices, err := c.DumpRDMADevices(t.Context())
	if err != nil || len(devices.Values) == 0 {
		t.Skipf("no RDMA devices: %v", err)
	}
	for _, d := range devices.Values {
		for port := uint32(1); port <= d.Port; port++ {
			t.Logf("positive: STAT_GET for %s/%d; expected: the hw_counters files' values", d.Name, port)
			got, err := c.GetRDMAStat(t.Context(), d.Index, port)
			if errors.Is(err, unix.EINVAL) {
				continue // a driver without hardware counters
			}
			if err != nil {
				t.Fatal(err)
			}
			values := make(map[string]uint64, len(got.Counters))
			for _, counter := range got.Counters {
				values[counter.Name] = counter.Value
			}
			dir := filepath.Join("/sys/class/infiniband", d.Name, "ports", strconv.FormatUint(uint64(port), 10), "hw_counters")
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() == "lifespan" {
					continue
				}
				value, ok := values[e.Name()]
				if !ok {
					t.Fatalf("%s/%s not in the STAT_GET reply", dir, e.Name())
				}
				b, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
				if err != nil || n < value {
					t.Fatalf("%s/%s: sysfs %d after netlink %d: %v", dir, e.Name(), n, value, err)
				}
			}
		}
	}
}
