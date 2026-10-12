package linuxio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Recorded contracts: every pcap under testdata/nlrecord/<source>/<kernel>/
// is one linuxio socket recorded by an nlrecord build (see record_linux.go).
// Each recorded request is decoded back into the linuxio call that built it,
// issued through the real API against a socket that checks the built request
// equals the recorded one (sequence and port ID aside) and answers with the
// recorded kernel replies, rewritten only for sequence. The decoded results
// must match the golden file next to the pcap. Reply envelope types and flags
// therefore come from the kernel, never from echoing the request, which is
// the assumption that hid the single-port RDMA GET bug.
//
// LINKMONITOR_NLRECORD_REPLAY=<dir> replays an uncommitted recording instead,
// without golden files. -update rewrites the golden files.
var updateGolden = flag.Bool("update", false, "rewrite recorded-contract golden files")

type exchange struct {
	request []byte
	replies [][]byte
}

type recording struct {
	protocol  int
	exchanges []exchange
}

func loadRecording(path string) (recording, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return recording{}, err
	}
	_, records, err := xtcpnl.ParseNetlinkPcap(raw)
	if err != nil {
		return recording{}, err
	}
	rec := recording{protocol: -1}
	for i, r := range records {
		family, body, err := r.NetlinkPayload()
		if err != nil {
			return recording{}, err
		}
		if rec.protocol >= 0 && int(family) != rec.protocol {
			return recording{}, fmt.Errorf("record %d: protocol %d in a protocol %d recording", i, family, rec.protocol)
		}
		rec.protocol = int(family)
		data := append([]byte(nil), body...)
		switch binary.BigEndian.Uint16(r.Data[0:2]) {
		case 7: // PACKET_KERNEL
			rec.exchanges = append(rec.exchanges, exchange{request: data})
		case 6: // PACKET_USER
			if len(rec.exchanges) == 0 {
				return recording{}, fmt.Errorf("record %d: reply before any request", i)
			}
			last := &rec.exchanges[len(rec.exchanges)-1]
			last.replies = append(last.replies, data)
		default:
			return recording{}, fmt.Errorf("record %d: packet type %d", i, binary.BigEndian.Uint16(r.Data[0:2]))
		}
	}
	if len(rec.exchanges) == 0 {
		return recording{}, errors.New("no requests recorded")
	}
	return rec, nil
}

// eachMessage visits the netlink messages of one datagram.
func eachMessage(datagram []byte, visit func(message []byte)) {
	for len(datagram) >= unix.NLMSG_HDRLEN {
		size := int(binary.LittleEndian.Uint32(datagram))
		if size < unix.NLMSG_HDRLEN || size > len(datagram) {
			return
		}
		visit(datagram[:size])
		aligned := (size + unix.NLMSG_ALIGNTO - 1) &^ (unix.NLMSG_ALIGNTO - 1)
		if aligned > len(datagram) {
			return
		}
		datagram = datagram[aligned:]
	}
}

// withoutAddressing zeroes every message's sequence and port ID.
func withoutAddressing(datagram []byte) []byte {
	out := append([]byte(nil), datagram...)
	eachMessage(out, func(m []byte) { clear(m[8:16]) })
	return out
}

// replaySocket answers each request with its recorded replies, one datagram
// per Receive. It fails a request that differs from the recording.
type replaySocket struct {
	exchanges []exchange
	pending   [][]byte
	failure   error
}

func (s *replaySocket) Send(_ context.Context, b []byte) error {
	switch {
	case len(s.pending) != 0:
		s.failure = fmt.Errorf("request sent with %d recorded replies unconsumed", len(s.pending))
	case len(s.exchanges) == 0:
		s.failure = errors.New("request beyond the recording")
	case !bytes.Equal(withoutAddressing(b), withoutAddressing(s.exchanges[0].request)):
		s.failure = fmt.Errorf("built request differs from the recording:\n got %x\nwant %x", b, s.exchanges[0].request)
	}
	if s.failure != nil {
		return s.failure
	}
	sent := binary.LittleEndian.Uint32(b[8:])
	recorded := binary.LittleEndian.Uint32(s.exchanges[0].request[8:])
	for _, reply := range s.exchanges[0].replies {
		reply = append([]byte(nil), reply...)
		eachMessage(reply, func(m []byte) {
			if binary.LittleEndian.Uint32(m[8:]) == recorded {
				binary.LittleEndian.PutUint32(m[8:], sent)
			}
		})
		s.pending = append(s.pending, reply)
	}
	s.exchanges = s.exchanges[1:]
	return nil
}

func (s *replaySocket) Receive(_ context.Context, visit func([]byte) error) (int, error) {
	if len(s.pending) == 0 {
		return 0, context.DeadlineExceeded
	}
	data := s.pending[0]
	s.pending = s.pending[1:]
	return 1, visit(data)
}

func (s *replaySocket) Close() error { return nil }

// replayCall is the linuxio call that builds a recorded request.
type replayCall struct {
	name string
	run  func(context.Context, *Client) (any, int, error)
}

func rdmaAttributes(request []byte) (index, port uint32, err error) {
	attrs, err := xtcpnl.ParseNetlinkAttributes(request[unix.NLMSG_HDRLEN:])
	for _, a := range attrs {
		switch {
		case a.ID() == 1 && len(a.Data) == 4:
			index = binary.LittleEndian.Uint32(a.Data)
		case a.ID() == 3 && len(a.Data) == 4:
			port = binary.LittleEndian.Uint32(a.Data)
		}
	}
	return index, port, err
}

func callFor(protocol int, request []byte, families map[uint16]Family) (replayCall, error) {
	if len(request) < unix.NLMSG_HDRLEN {
		return replayCall{}, errors.New("short request")
	}
	kind := binary.LittleEndian.Uint16(request[4:])
	dump := binary.LittleEndian.Uint16(request[6:])&unix.NLM_F_DUMP == unix.NLM_F_DUMP
	switch {
	case protocol == unix.NETLINK_ROUTE && kind == unix.RTM_GETLINK && dump:
		return replayCall{"DumpLinks", func(ctx context.Context, c *Client) (any, int, error) {
			r, err := c.DumpLinks(ctx)
			return r.Values, len(r.Values), err
		}}, nil
	case protocol == unix.NETLINK_ROUTE && kind == unix.RTM_GETLINK && len(request) >= 24:
		index := int32(binary.LittleEndian.Uint32(request[20:]))
		return replayCall{fmt.Sprintf("GetLink(%d)", index), func(ctx context.Context, c *Client) (any, int, error) {
			r, err := c.GetLink(ctx, index)
			return r.Values, len(r.Values), err
		}}, nil
	case protocol == unix.NETLINK_GENERIC && kind == xtcpnl.GenlControllerID:
		attrs, err := xtcpnl.ParseNetlinkAttributes(request[unix.NLMSG_HDRLEN+4:])
		if err != nil || len(attrs) != 1 || attrs[0].ID() != 2 {
			return replayCall{}, fmt.Errorf("controller request attributes %v: %w", attrs, err)
		}
		name := strings.TrimRight(string(attrs[0].Data), "\x00")
		return replayCall{fmt.Sprintf("DiscoverFamily(%q)", name), func(ctx context.Context, c *Client) (any, int, error) {
			f, err := c.DiscoverFamily(ctx, name)
			if err == nil {
				families[f.ID()] = f
			}
			return map[string]any{"name": f.name, "groups": f.groups}, 1, err
		}}, nil
	case protocol == unix.NETLINK_GENERIC:
		f, ok := families[kind]
		if !ok {
			return replayCall{}, fmt.Errorf("request for undiscovered family %d", kind)
		}
		if f.name == "devlink" {
			return replayCall{"DumpDevlinkPorts", func(ctx context.Context, c *Client) (any, int, error) {
				r, err := c.DumpDevlinkPorts(ctx, f)
				return r.Values, len(r.Values), err
			}}, nil
		}
		if len(request) < 32 {
			return replayCall{}, errors.New("short ethtool request")
		}
		index := binary.LittleEndian.Uint32(request[28:])
		for _, k := range []xtcpnl.EthtoolKind{xtcpnl.EthtoolLinkInfoKind, xtcpnl.EthtoolLinkModesKind, xtcpnl.EthtoolLinkStateKind,
			xtcpnl.EthtoolRingsKind, xtcpnl.EthtoolChannelsKind, xtcpnl.EthtoolPauseKind, xtcpnl.EthtoolFECKind} {
			if built, err := xtcpnl.BuildGetEthtoolRequest(f.id, k, 1, 0); err == nil && built[16] == request[16] {
				return replayCall{fmt.Sprintf("GetEthtool(%s, %d)", k, index), func(ctx context.Context, c *Client) (any, int, error) {
					r, err := c.GetEthtool(ctx, f, k, index)
					return r.Values, len(r.Values), err
				}}, nil
			}
		}
		return replayCall{}, fmt.Errorf("unknown %s command %d", f.name, request[16])
	case protocol == unix.NETLINK_RDMA:
		index, port, err := rdmaAttributes(request)
		if err != nil {
			return replayCall{}, err
		}
		switch {
		case kind == rdmaDeviceGet && dump:
			return replayCall{"DumpRDMADevices", func(ctx context.Context, c *Client) (any, int, error) {
				r, err := c.DumpRDMADevices(ctx)
				return r.Values, len(r.Values), err
			}}, nil
		case kind == rdmaPortGet && dump:
			return replayCall{fmt.Sprintf("DumpRDMAPorts(%d)", index), func(ctx context.Context, c *Client) (any, int, error) {
				r, err := c.DumpRDMAPorts(ctx, index)
				return r.Values, len(r.Values), err
			}}, nil
		case kind == rdmaPortGet:
			return replayCall{fmt.Sprintf("GetRDMAPort(%d, %d)", index, port), func(ctx context.Context, c *Client) (any, int, error) {
				r, err := c.GetRDMAPort(ctx, index, port)
				return r.Values, len(r.Values), err
			}}, nil
		case kind == rdmaSysGet:
			return replayCall{"monitorMode", func(ctx context.Context, c *Client) (any, int, error) {
				err := c.monitorMode(ctx)
				return err == nil, 1, err
			}}, nil
		}
	}
	return replayCall{}, fmt.Errorf("unknown protocol %d request type %d", protocol, kind)
}

// replayOutcome is one golden line: the call, its value count, error text and
// a digest of the decoded values.
type replayOutcome struct {
	Call   string `json:"call"`
	Values int    `json:"values"`
	Error  string `json:"error,omitempty"`
	Digest string `json:"digest,omitempty"`
}

// replay issues every recorded request through the real API. A nil error
// means the recording was consumed exactly.
func replay(rec recording) ([]replayOutcome, error) {
	socket := &replaySocket{exchanges: rec.exchanges}
	c, err := NewClient(rec.protocol)
	if err != nil {
		return nil, err
	}
	opened := false
	c.open = func(context.Context, int) (transport, error) {
		if opened {
			return nil, errors.New("recording holds one socket; a second open means a retired epoch")
		}
		opened = true
		return socket, nil
	}
	families := map[uint16]Family{}
	var outcomes []replayOutcome
	for len(socket.exchanges) > 0 {
		call, err := callFor(rec.protocol, socket.exchanges[0].request, families)
		if err != nil {
			return outcomes, err
		}
		remaining := len(socket.exchanges)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		values, n, callErr := call.run(ctx, c)
		cancel()
		if socket.failure != nil {
			return outcomes, socket.failure
		}
		if len(socket.exchanges) == remaining {
			// The call never sent its request (a refused reopen, say), so
			// looping would rebuild the same call forever.
			return outcomes, fmt.Errorf("%s did not send the recorded request: %v", call.name, callErr)
		}
		out := replayOutcome{Call: call.name, Values: n}
		if callErr != nil {
			out.Values, out.Error = 0, callErr.Error()
		} else {
			raw, err := json.Marshal(values)
			if err != nil {
				return outcomes, err
			}
			sum := sha256.Sum256(raw)
			out.Digest = hex.EncodeToString(sum[:8])
		}
		outcomes = append(outcomes, out)
	}
	if len(socket.pending) != 0 {
		return outcomes, fmt.Errorf("%d recorded replies unconsumed", len(socket.pending))
	}
	return outcomes, c.Close()
}

func recordings(t *testing.T) []string {
	t.Helper()
	pattern := filepath.Join("testdata", "nlrecord", "*", "*", "*.pcap")
	if dir := os.Getenv("LINKMONITOR_NLRECORD_REPLAY"); dir != "" {
		pattern = filepath.Join(dir, "*.pcap")
	}
	paths, err := filepath.Glob(pattern)
	if err != nil || len(paths) == 0 {
		t.Fatalf("no recordings match %s: %v", pattern, err)
	}
	return paths
}

func TestRecordedContracts(t *testing.T) {
	adHoc := os.Getenv("LINKMONITOR_NLRECORD_REPLAY") != ""
	for _, path := range recordings(t) {
		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			t.Logf("positive: %s; expected: every request rebuilt exactly and results equal the golden", path)
			rec, err := loadRecording(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := replay(rec)
			for _, o := range got {
				t.Logf("%s values=%d error=%q digest=%s", o.Call, o.Values, o.Error, o.Digest)
			}
			if err != nil {
				t.Fatal(err)
			}
			if adHoc {
				return
			}
			raw, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, '\n')
			golden := strings.TrimSuffix(path, ".pcap") + ".golden.json"
			if *updateGolden {
				if err := os.WriteFile(golden, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(raw, want) {
				t.Fatalf("results differ from %s:\n%s", golden, raw)
			}
		})
	}
}

// findExchange returns a route or RDMA recording cut after the first
// exchange whose call has prefix and whose replies satisfy want.
func findExchange(t *testing.T, prefix string, want func(exchange) bool) (recording, int) {
	t.Helper()
	for _, path := range recordings(t) {
		rec, err := loadRecording(path)
		if err != nil {
			t.Fatal(err)
		}
		if rec.protocol == unix.NETLINK_GENERIC {
			continue
		}
		for i, e := range rec.exchanges {
			call, err := callFor(rec.protocol, e.request, nil)
			if err == nil && strings.HasPrefix(call.name, prefix) && want(e) {
				rec.exchanges = rec.exchanges[:i+1]
				return rec, i
			}
		}
	}
	t.Skipf("no recording holds a matching %s exchange", prefix)
	return recording{}, 0
}

// errorReply reports whether an exchange ends in an NLMSG_ERROR with errno.
func errorReply(errno unix.Errno) func(exchange) bool {
	return func(e exchange) bool {
		found := false
		for _, r := range e.replies {
			eachMessage(r, func(m []byte) {
				if binary.LittleEndian.Uint16(m[4:]) == unix.NLMSG_ERROR && len(m) >= 20 &&
					int32(binary.LittleEndian.Uint32(m[16:])) == -int32(errno) {
					found = true
				}
			})
		}
		return found
	}
}

func anyExchange(exchange) bool { return true }

func cloneExchanges(in []exchange) []exchange {
	out := make([]exchange, len(in))
	for i, e := range in {
		out[i].request = append([]byte(nil), e.request...)
		for _, r := range e.replies {
			out[i].replies = append(out[i].replies, append([]byte(nil), r...))
		}
	}
	return out
}

// TestRecordedContractMutations proves the replay detects the failures it
// exists for, using real recordings as the starting point.
func TestRecordedContractMutations(t *testing.T) {
	rows := []struct {
		category, description, expected string
		call                            string
		want                            func(exchange) bool
		mutate                          func(*exchange) error
		check                           func(replayOutcome, error) error
	}{
		{"negative", "recorded request has one more flag bit (NLM_F_ACK)", "replay rejects the built request",
			"GetRDMAPort", anyExchange, func(e *exchange) error { e.request[6] ^= unix.NLM_F_ACK; return nil },
			func(_ replayOutcome, err error) error {
				if err == nil || !strings.Contains(err.Error(), "differs from the recording") {
					return fmt.Errorf("error=%v", err)
				}
				return nil
			}},
		{"negative", "single-port GET answered with an unrelated reply type (#192 shape)", "the request times out",
			"GetRDMAPort", anyExchange, func(e *exchange) error {
				for _, r := range e.replies {
					eachMessage(r, func(m []byte) {
						if binary.LittleEndian.Uint16(m[4:]) == rdmaDeviceGet {
							binary.LittleEndian.PutUint16(m[4:], rdmaResGet)
						}
					})
				}
				return nil
			},
			func(o replayOutcome, err error) error {
				if err != nil || !strings.Contains(o.Error, context.DeadlineExceeded.Error()) {
					return fmt.Errorf("outcome=%+v error=%v", o, err)
				}
				return nil
			}},
		{"corner", "single-port GET reply as the kernel sends it", "typed RDMA_NLDEV_CMD_GET, accepted as an alias",
			"GetRDMAPort", anyExchange, func(e *exchange) error {
				var err error
				eachMessage(e.replies[0], func(m []byte) {
					if got := binary.LittleEndian.Uint16(m[4:]); got != rdmaDeviceGet {
						err = fmt.Errorf("recorded GET reply type %#x", got)
					}
				})
				return err
			},
			func(o replayOutcome, err error) error {
				if err != nil || o.Error != "" || o.Values != 1 {
					return fmt.Errorf("outcome=%+v error=%v", o, err)
				}
				return nil
			}},
		{"boundary", "link dump with only its DONE message", "zero links and no error",
			"DumpLinks", anyExchange, func(e *exchange) error {
				var done []byte
				for _, r := range e.replies {
					eachMessage(r, func(m []byte) {
						if binary.LittleEndian.Uint16(m[4:]) == unix.NLMSG_DONE {
							done = append([]byte(nil), m...)
						}
					})
				}
				if done == nil {
					return errors.New("dump has no DONE message")
				}
				e.replies = [][]byte{done}
				return nil
			},
			func(o replayOutcome, err error) error {
				if err != nil || o.Error != "" || o.Values != 0 {
					return fmt.Errorf("outcome=%+v error=%v", o, err)
				}
				return nil
			}},
		{"corner", "kernel error reply for an unused link index", "ENODEV with no values",
			"GetLink", errorReply(unix.ENODEV), func(*exchange) error { return nil },
			func(o replayOutcome, err error) error {
				if err != nil || o.Error != unix.ENODEV.Error() || o.Values != 0 {
					return fmt.Errorf("outcome=%+v error=%v", o, err)
				}
				return nil
			}},
	}
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			rec, i := findExchange(t, row.call, row.want)
			rec.exchanges = cloneExchanges(rec.exchanges)
			if err := row.mutate(&rec.exchanges[i]); err != nil {
				t.Fatal(err)
			}
			got, err := replay(rec)
			var last replayOutcome
			if len(got) == i+1 {
				last = got[i]
			}
			if err := row.check(last, err); err != nil {
				t.Fatal(err)
			}
		})
	}
}
