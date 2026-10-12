package linuxio

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func rdmaBody() []byte {
	var body []byte
	for _, a := range [][]byte{attribute(1, word(0)), attribute(2, []byte("mlx5_0\x00")), attribute(3, word(1)), attribute(12, []byte{4}), attribute(13, []byte{5})} {
		body = append(body, a...)
	}
	return body
}

// rdmaPortWith is rdmaBody with its device name replaced and extra appended.
func rdmaPortWith(name string, extra ...[]byte) []byte {
	b := rdmaBody()
	body := append(append(b[:8:8], attribute(2, []byte(name+"\x00"))...), b[20:]...)
	for _, e := range extra {
		body = append(body, e...)
	}
	return body
}

func rdmaNetdevAttrs(index int32, name string) []byte {
	return append(attribute(50, word(index)), attribute(51, []byte(name+"\x00"))...)
}

func TestRDMADecoderTable(t *testing.T) {
	mlx := RDMAPort{Name: "mlx5_0", Port: 1, State: 4, HasState: true, Physical: 5, HasPhysical: true}
	with := func(f func(*RDMAPort)) RDMAPort { p := mlx; f(&p); return p }
	for _, tc := range []struct {
		description string
		device      bool
		body        []byte
		expected    RDMAPort
		expectedErr error
	}{
		{description: "positive: complete host port with device index zero", body: rdmaBody(), expected: mlx},
		{description: "positive: the captured link_show_one port reply", body: capturedRDMAPort,
			expected: RDMAPort{Name: "rxe0", Port: 1, State: 4, HasState: true, Physical: 5, HasPhysical: true,
				Netdev: 4, HasNetdev: true, NetdevName: "rx0"}},
		{description: "positive: the captured device dump reply, whose PORT_INDEX is the port count", device: true,
			body: capturedRDMADevice, expected: RDMAPort{Name: "rxe0", Port: 1}},
		{description: "negative: duplicate state, even equal", body: append(rdmaBody(), attribute(12, []byte{4})...), expectedErr: ErrReply},
		{description: "negative: short index", body: append(attribute(1, []byte{0}), rdmaBody()[8:]...), expectedErr: ErrReply},
		{description: "negative: empty body lacks the identifiers", body: nil, expectedErr: ErrReply},
		{description: "negative: a 4-byte CAP_FLAGS, which xtcpnl checks and linkmonitor used to skip",
			body: append(rdmaBody(), attribute(4, word(0))...), expectedErr: ErrReply},
		{description: "negative: a device reply with a 4-byte NODE_GUID", device: true,
			body: append(rdmaBody()[:28:28], attribute(6, word(0))...), expectedErr: ErrReply},
		{description: "negative: NDEV_INDEX zero", body: append(rdmaBody(), rdmaNetdevAttrs(0, "eth0")...), expectedErr: ErrReply},
		{description: "negative: NDEV_INDEX without NDEV_NAME", body: append(rdmaBody(), attribute(50, word(4))...), expectedErr: ErrReply},
		{description: "negative: a device name with a slash", body: rdmaPortWith("mlx/0"), expectedErr: ErrReply},
		{description: "boundary: truncated final attribute", body: rdmaBody()[:len(rdmaBody())-4], expectedErr: ErrReply},
		{description: "boundary: 63-byte RDMA device name, the maximum", body: rdmaPortWith(strings.Repeat("x", 63)),
			expected: with(func(p *RDMAPort) { p.Name = strings.Repeat("x", 63) })},
		{description: "boundary: 64-byte RDMA device name, one over", body: rdmaPortWith(strings.Repeat("x", 64)), expectedErr: ErrReply},
		{description: "boundary: 15-byte netdev name, the IFNAMSIZ maximum",
			body:     append(rdmaBody(), rdmaNetdevAttrs(4, strings.Repeat("n", 15))...),
			expected: with(func(p *RDMAPort) { p.Netdev, p.HasNetdev, p.NetdevName = 4, true, strings.Repeat("n", 15) })},
		{description: "boundary: 16-byte netdev name, one over",
			body: append(rdmaBody(), rdmaNetdevAttrs(4, strings.Repeat("n", 16))...), expectedErr: ErrReply},
		{description: "boundary: NDEV_INDEX 0x7fffffff, the largest ifindex",
			body:     append(rdmaBody(), rdmaNetdevAttrs(0x7fffffff, "eth0")...),
			expected: with(func(p *RDMAPort) { p.Netdev, p.HasNetdev, p.NetdevName = 0x7fffffff, true, "eth0" })},
		{description: "boundary: NDEV_INDEX 0x80000000, one over",
			body: append(rdmaBody(), rdmaNetdevAttrs(-0x80000000, "eth0")...), expectedErr: ErrReply},
		{description: "corner: unknown optional attribute keeps the known evidence",
			body: append(rdmaBody(), attribute(999, []byte{1})...), expected: mlx},
		{description: "corner: NLA_F_NESTED on DEV_INDEX", body: append(attribute(1|unix.NLA_F_NESTED, word(0)), rdmaBody()[8:]...),
			expectedErr: ErrReply},
		{description: "corner: FW_VERSION, which xtcpnl decodes and linkmonitor ignores",
			body: append(rdmaBody(), attribute(5, []byte("1.0\x00"))...), expected: mlx},
		{description: "corner: a device reply without PORT_INDEX", device: true,
			body: append(rdmaBody()[:20:20], attribute(5, []byte("1.0\x00"))...), expectedErr: ErrReply},
	} {
		t.Run(tc.description, func(t *testing.T) {
			decode := decodeRDMAPort
			if tc.device {
				decode = decodeRDMADevice
			}
			body := append([]byte(nil), tc.body...)
			p, err := decode(body)
			if !errors.Is(err, tc.expectedErr) || (err != nil) != (tc.expectedErr != nil) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			clear(body)
			if p != tc.expected {
				t.Fatalf("got %+v\nwant %+v", p, tc.expected)
			}
		})
	}
}

func TestRDMATransactionTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		protocol                                     int
		mode                                         string
		bad                                          bool
	}{
		{"devices", "positive", "device enumeration", "complete owned dump", unix.NETLINK_RDMA, "devices", false},
		{"ports", "positive", "port enumeration", "complete owned dump", unix.NETLINK_RDMA, "ports", false},
		{"single", "positive", "single port query answered as the kernel does, with RDMA_NLDEV_CMD_GET", "one requested port", unix.NETLINK_RDMA, "single", false},
		{"singlePortType", "corner", "single port query answered with the PORT_GET type", "one requested port", unix.NETLINK_RDMA, "singlePortType", false},
		{"singleOtherType", "negative", "single port reply with an unrelated NLDEV type", "reply ignored; no value", unix.NETLINK_RDMA, "singleOtherType", true},
		{"empty", "boundary", "empty completed dump", "successful empty inventory", unix.NETLINK_RDMA, "empty", false},
		{"wrongProtocol", "negative", "generic socket used for RDMA", "reject before send", unix.NETLINK_GENERIC, "ports", true},
		{"interrupted", "negative", "DUMP_INTR after valid data", "no partial values", unix.NETLINK_RDMA, "interrupted", true},
		{"wrongPort", "negative", "single reply identifies another port", "reject reply", unix.NETLINK_RDMA, "wrongPort", true},
		{"wrongSequence", "corner", "only stale sequence received", "no accepted reply", unix.NETLINK_RDMA, "wrongSequence", true},
		{"singleErrno", "negative", "single port query the kernel refuses with ENODEV", "the errno, no value", unix.NETLINK_RDMA, "singleErrno", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			socket := &scriptSocket{}
			socket.onSend = func(data []byte) {
				seq, typ := binary.LittleEndian.Uint32(data[8:]), binary.LittleEndian.Uint16(data[4:])
				if tc.mode == "wrongSequence" {
					seq++
				}
				flags := uint16(unix.NLM_F_MULTI)
				switch tc.mode {
				case "single", "wrongPort":
					flags, typ = 0, rdmaDeviceGet
				case "singlePortType":
					flags = 0
				case "singleOtherType":
					flags, typ = 0, rdmaResGet
				}
				var frames [][]byte
				if tc.mode == "singleErrno" {
					socket.batches = []batch{{data: [][]byte{frame(unix.NLMSG_ERROR, 0, seq, append(word(-int32(unix.ENODEV)), data...))}}}
					return
				}
				if tc.mode != "empty" {
					frames = append(frames, frame(typ, flags, seq, rdmaBody()))
				}
				if flags != 0 {
					if tc.mode == "interrupted" {
						flags = unix.NLM_F_DUMP_INTR
					} else {
						flags = 0
					}
					frames = append(frames, frame(unix.NLMSG_DONE, flags, seq, word(0)))
				}
				socket.batches = []batch{{data: frames}}
			}
			c := scriptedClient(tc.protocol, socket)
			t.Cleanup(func() {
				if err := c.Close(); err != nil {
					t.Error(err)
				}
			})
			var result Result[RDMAPort]
			var err error
			switch tc.mode {
			case "devices":
				result, err = c.DumpRDMADevices(t.Context())
			case "single", "singlePortType", "singleOtherType", "singleErrno":
				result, err = c.GetRDMAPort(t.Context(), 0, 1)
			case "wrongPort":
				result, err = c.GetRDMAPort(t.Context(), 0, 2)
			default:
				result, err = c.DumpRDMAPorts(t.Context(), 0)
			}
			if (err != nil) != tc.bad {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if tc.mode == "singleErrno" && !errors.Is(err, unix.ENODEV) {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if (tc.bad || tc.mode == "empty") && len(result.Values) != 0 {
				t.Fatal("partial result")
			}
			if !tc.bad && tc.mode != "empty" && len(result.Values) != 1 {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

// FuzzRDMAPort checks the admission only narrows xtcpnl's: what linkmonitor
// accepts, xtcpnl decodes too, with a name that is a path component.
func FuzzRDMAPort(f *testing.F) {
	f.Add(rdmaBody())
	f.Add(capturedRDMAPort)
	f.Add(capturedRDMADevice)
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := decodeRDMAPort(b)
		if err != nil {
			return
		}
		if len(p.Name) == 0 || len(p.Name) > 63 || strings.ContainsAny(p.Name, "\x00/:") {
			t.Fatalf("accepted invalid identity %q", p.Name)
		}
		if _, err := xtcpnl.ParseRdmaPort(b); err != nil {
			t.Fatalf("accepted what xtcpnl refuses: %v", err)
		}
	})
}
