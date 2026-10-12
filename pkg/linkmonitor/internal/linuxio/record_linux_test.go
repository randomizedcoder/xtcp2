//go:build nlrecord

package linuxio

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

type recordedFrame struct {
	packetType uint16
	data       []byte
}

func readRecording(t *testing.T, path string) []recordedFrame {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, records, err := xtcpnl.ParseNetlinkPcap(raw)
	if err != nil {
		t.Fatal(err)
	}
	frames := make([]recordedFrame, 0, len(records))
	for _, r := range records {
		family, body, err := r.NetlinkPayload()
		if err != nil || family != unix.NETLINK_RDMA {
			t.Fatalf("family=%d error=%v", family, err)
		}
		frames = append(frames, recordedFrame{uint16(r.Data[0])<<8 | uint16(r.Data[1]), body})
	}
	return frames
}

func TestRecorder(t *testing.T) {
	request := frame(rdmaDeviceGet, unix.NLM_F_REQUEST|unix.NLM_F_DUMP, 1, nil)
	reply := frame(rdmaDeviceGet, unix.NLM_F_MULTI, 1, rdmaBody())
	done := frame(unix.NLMSG_DONE, unix.NLM_F_MULTI, 1, nil)
	large := bytes.Repeat([]byte{0xa5}, 32<<10)
	tooLarge := make([]byte, recordSnapLen)
	rows := []struct {
		category, description, expected string
		send                            []byte
		sendErr                         error
		replies                         [][]byte
		unwritable                      bool
		want                            []recordedFrame
		wantErr                         error
	}{
		{"positive", "request and dump reply", "request is PACKET_KERNEL, replies PACKET_USER, in order",
			request, nil, [][]byte{reply, done}, false,
			[]recordedFrame{{packetKernel, request}, {packetUser, reply}, {packetUser, done}}, nil},
		{"corner", "several messages in one datagram", "the datagram is one record, byte for byte",
			request, nil, [][]byte{append(append([]byte(nil), reply...), done...)}, false,
			[]recordedFrame{{packetKernel, request}, {packetUser, append(append([]byte(nil), reply...), done...)}}, nil},
		{"negative", "send fails", "the kernel never saw it, so only the reply is recorded",
			request, unix.EPIPE, [][]byte{done}, false,
			[]recordedFrame{{packetUser, done}}, nil},
		{"negative", "directory is not writable", "I/O still succeeds and the first error is kept",
			request, nil, [][]byte{done}, true, nil, os.ErrNotExist},
		{"boundary", "zero-byte datagram", "an empty record after the cooked header",
			[]byte{}, nil, nil, false, []recordedFrame{{packetKernel, []byte{}}}, nil},
		{"boundary", "32 KiB datagram, the receive buffer size", "recorded whole",
			large, nil, nil, false, []recordedFrame{{packetKernel, large}}, nil},
		{"boundary", "datagram over the snap length", "not recorded; error kept",
			tooLarge, nil, nil, false, nil, errRecordTooLarge},
	}
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			dir := t.TempDir()
			if row.unwritable {
				dir = filepath.Join(dir, "missing")
			}
			rec := &recorder{dir: dir}
			socket := &scriptSocket{sendErr: row.sendErr, batches: []batch{{data: row.replies}}}
			conn := rec.wrap(unix.NETLINK_RDMA, socket)
			if err := conn.Send(context.Background(), row.send); !errors.Is(err, row.sendErr) {
				t.Fatalf("send error=%v", err)
			}
			var seen int
			if row.replies != nil {
				n, err := conn.Receive(context.Background(), func([]byte) error { seen++; return nil })
				if err != nil || n != len(row.replies) || seen != n {
					t.Fatalf("receive n=%d seen=%d error=%v", n, seen, err)
				}
			}
			if err := conn.Close(); err != nil || socket.closes != 1 {
				t.Fatalf("close error=%v closes=%d", err, socket.closes)
			}
			if err := rec.failure(); !errors.Is(err, row.wantErr) {
				t.Fatalf("recorder error=%v want=%v", err, row.wantErr)
			}
			path := conn.(*recordingTransport).path
			if row.want == nil {
				if row.wantErr == errRecordTooLarge {
					if got := readRecording(t, path); len(got) != 0 {
						t.Fatalf("oversized datagram recorded: %d frames", len(got))
					}
				}
				return
			}
			got := readRecording(t, path)
			if len(got) != len(row.want) {
				t.Fatalf("frames=%d want %d", len(got), len(row.want))
			}
			for i := range got {
				if got[i].packetType != row.want[i].packetType || !bytes.Equal(got[i].data, row.want[i].data) {
					t.Fatalf("frame %d type=%d len=%d, want type=%d len=%d", i, got[i].packetType, len(got[i].data), row.want[i].packetType, len(row.want[i].data))
				}
			}
		})
	}
}

func TestRecorderSelection(t *testing.T) {
	rows := []struct {
		category, description, expected string
		env                             string
		sockets                         int
		wantFiles                       int
	}{
		{"corner", "environment unset", "no wrapper; the transport is returned as is", "", 1, 0},
		{"positive", "environment names a directory", "one recording per socket", "dir", 1, 1},
		{"corner", "two sockets on one protocol", "separate files keep request/reply pairing", "dir", 2, 2},
	}
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			dir := t.TempDir()
			value := ""
			if row.env != "" {
				value = dir
			}
			t.Setenv(recordEnv, value)
			rec := recorderFromEnv()
			for range row.sockets {
				socket := &scriptSocket{}
				conn := rec.wrap(unix.NETLINK_RDMA, socket)
				if (rec == nil) != (conn == transport(socket)) {
					t.Fatalf("wrapped=%v with recorder=%v", conn != transport(socket), rec != nil)
				}
				if err := conn.Send(context.Background(), frame(rdmaDeviceGet, unix.NLM_F_REQUEST, 1, nil)); err != nil {
					t.Fatal(err)
				}
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
			}
			files, err := filepath.Glob(filepath.Join(dir, "rdma-*.pcap"))
			if err != nil || len(files) != row.wantFiles {
				t.Fatalf("files=%v error=%v", files, err)
			}
		})
	}
}
