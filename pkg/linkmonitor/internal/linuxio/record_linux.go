//go:build nlrecord

package linuxio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// nlmon cannot mirror NETLINK_RDMA (netlink_filter_tap() whitelists only a
// few protocols), so nlrecord builds record in process instead. With
// LINKMONITOR_NLRECORD=<dir>, each linuxio socket appends every datagram it
// sends or receives to its own <dir>/<protocol>-<pid>-<serial>.pcap, in the
// DLT_NETLINK form an nlmon capture has, so xtcpnl.ParseNetlinkPcap reads it.
// One file per socket keeps request/reply pairing intact for replay. Like
// nlmon, requests carry PACKET_KERNEL and replies or events PACKET_USER.
// Recording never fails I/O: the first write error stops recording and is
// kept for recordError. Production builds compile record_off_linux.go.
const recordEnv = "LINKMONITOR_NLRECORD"

const (
	packetUser    = 6 // PACKET_USER, to user space
	packetKernel  = 7 // PACKET_KERNEL, to kernel space
	arphrdNetlink = 824
	recordSnapLen = 1 << 18
)

var errRecordTooLarge = errors.New("linuxio: datagram exceeds the record snap length")

type recorder struct {
	dir    string
	serial atomic.Uint64
	mu     sync.Mutex
	err    error
}

var (
	recordOnce   sync.Once
	recordActive *recorder
)

// recorderFromEnv reads the environment once per process, at first open.
func recorderFromEnv() *recorder {
	if dir := os.Getenv(recordEnv); dir != "" {
		return &recorder{dir: dir}
	}
	return nil
}

func recordTransport(protocol int, t transport) transport {
	recordOnce.Do(func() { recordActive = recorderFromEnv() })
	return recordActive.wrap(protocol, t)
}

// recordError returns the first write error of the process recorder.
func recordError() error {
	recordOnce.Do(func() { recordActive = recorderFromEnv() })
	return recordActive.failure()
}

func (r *recorder) failure() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *recorder) wrap(protocol int, t transport) transport {
	if r == nil {
		return t
	}
	name := fmt.Sprintf("%s-%d-%d.pcap", protocolName(protocol), os.Getpid(), r.serial.Add(1))
	return &recordingTransport{inner: t, rec: r, protocol: uint16(protocol), path: filepath.Join(r.dir, name)}
}

func protocolName(protocol int) string {
	switch protocol {
	case unix.NETLINK_ROUTE:
		return "route"
	case unix.NETLINK_GENERIC:
		return "generic"
	case unix.NETLINK_RDMA:
		return "rdma"
	default:
		return fmt.Sprintf("netlink%d", protocol)
	}
}

type recordingTransport struct {
	inner    transport
	rec      *recorder
	protocol uint16
	path     string
	file     *os.File
}

func (t *recordingTransport) Send(ctx context.Context, b []byte) error {
	err := t.inner.Send(ctx, b)
	if err == nil {
		t.record(packetKernel, b)
	}
	return err
}

func (t *recordingTransport) Receive(ctx context.Context, visit func([]byte) error) (int, error) {
	return t.inner.Receive(ctx, func(data []byte) error {
		t.record(packetUser, data)
		return visit(data)
	})
}

func (t *recordingTransport) Close() error {
	err := t.inner.Close()
	t.rec.mu.Lock()
	defer t.rec.mu.Unlock()
	if t.file != nil {
		err = errors.Join(err, t.file.Close())
		t.file = nil
	}
	return err
}

// record appends one pcap record: header, 16-byte SLL cooked header, datagram.
func (t *recordingTransport) record(packetType uint16, data []byte) {
	t.rec.mu.Lock()
	defer t.rec.mu.Unlock()
	if t.rec.err != nil {
		return
	}
	if err := t.appendLocked(packetType, data, time.Now()); err != nil {
		t.rec.err = err
	}
}

func (t *recordingTransport) appendLocked(packetType uint16, data []byte, now time.Time) error {
	if t.file == nil {
		file, err := os.OpenFile(t.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		t.file = file
		var header [xtcpnl.PcapHeaderSizeCst]byte
		binary.LittleEndian.PutUint32(header[0:], xtcpnl.PcapMagicMicrosCst)
		binary.LittleEndian.PutUint16(header[4:], 2)
		binary.LittleEndian.PutUint16(header[6:], 4)
		binary.LittleEndian.PutUint32(header[16:], recordSnapLen)
		binary.LittleEndian.PutUint32(header[20:], xtcpnl.PcapLinkTypeNetlinkCst)
		if _, err := file.Write(header[:]); err != nil {
			return err
		}
	}
	size := xtcpnl.NetlinkCookedHeaderSizeCst + len(data)
	if size > recordSnapLen {
		return errRecordTooLarge
	}
	frame := make([]byte, xtcpnl.PcapRecordHeaderSizeCst+size)
	binary.LittleEndian.PutUint32(frame[0:], uint32(now.Unix()))
	binary.LittleEndian.PutUint32(frame[4:], uint32(now.Nanosecond()/1000))
	binary.LittleEndian.PutUint32(frame[8:], uint32(size))
	binary.LittleEndian.PutUint32(frame[12:], uint32(size))
	cooked := frame[xtcpnl.PcapRecordHeaderSizeCst:]
	binary.BigEndian.PutUint16(cooked[0:], packetType)
	binary.BigEndian.PutUint16(cooked[2:], arphrdNetlink)
	binary.BigEndian.PutUint16(cooked[xtcpnl.SllProtocolOffsetCst:], t.protocol)
	copy(cooked[xtcpnl.NetlinkCookedHeaderSizeCst:], data)
	_, err := t.file.Write(frame)
	return err
}
