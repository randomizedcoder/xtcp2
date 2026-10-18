package linuxio

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ErrIdentity is a character device whose node is not the one GET_CHARDEV
// named: not a char device, or a different dev_t. util/open_cdev.c makes the
// same check before libibverbs and libibumad use a node.
var ErrIdentity = errors.New("linuxio: character device does not match the kernel's dev_t")

var rdmaChardevGet = xtcpnl.RdmaNlGetType(xtcpnl.RdmaNlNldev, xtcpnl.RdmaNldevCmdGetChardev)

// rdmaCdevDir is RDMA_CDEV_DIR, where devtmpfs creates the nodes.
const rdmaCdevDir = "/dev/infiniband"

// rdmaChardevAttrs are the attributes a GET_CHARDEV reply is admitted on.
var rdmaChardevAttrs = []uint16{xtcpnl.RdmaNldevAttrChardevName, xtcpnl.RdmaNldevAttrChardevABI,
	xtcpnl.RdmaNldevAttrChardev, xtcpnl.RdmaNldevAttrUverbsDriverID}

// Chardev asks the kernel which character device serves dev (uverbs) or dev
// and port (umad). The request is built by xtcpnl.BuildRdmaGetChardev, never by
// rdmaRequest, so xtcpnl.IsRdmaGetCmd keeps refusing GET_CHARDEV everywhere
// else. Like libibverbs it asks for no ACK: the unicast reply completes it.
func (c *Client) Chardev(ctx context.Context, dev, port uint32, kind xtcpnl.ChardevKind) (xtcpnl.RdmaChardev, error) {
	if c.protocol != unix.NETLINK_RDMA {
		return xtcpnl.RdmaChardev{}, ErrRequest
	}
	result, err := execute(ctx, c, request[xtcpnl.RdmaChardev]{family: rdmaChardevGet,
		build: func(seq uint32, _ uint64) ([]byte, error) {
			b, err := xtcpnl.BuildRdmaGetChardev(nil, seq, dev, port, kind)
			if err != nil {
				return nil, errors.Join(ErrRequest, err)
			}
			return b, nil
		},
		match: func(e xtcpnl.NetlinkEnvelope) (xtcpnl.RdmaChardev, bool, error) {
			ch, err := decodeRDMAChardev(e.Body, kind)
			return ch, true, err
		}})
	if err != nil {
		return xtcpnl.RdmaChardev{}, err
	}
	if len(result.Values) != 1 {
		return xtcpnl.RdmaChardev{}, ErrReply
	}
	return result.Values[0], nil
}

// decodeRDMAChardev admits each chardev attribute at most once, then decodes.
func decodeRDMAChardev(body []byte, kind xtcpnl.ChardevKind) (xtcpnl.RdmaChardev, error) {
	var seen [xtcpnl.RdmaNldevAttrUverbsDriverID + 1]bool
	if err := admitRDMAAttrs(body, rdmaChardevAttrs, seen[:]); err != nil {
		return xtcpnl.RdmaChardev{}, err
	}
	var ch xtcpnl.RdmaChardev
	if err := xtcpnl.ParseRdmaChardev(body, kind, &ch); err != nil {
		return xtcpnl.RdmaChardev{}, errors.Join(ErrReply, err)
	}
	return ch, nil
}

// OpenChardev opens ch's node under /dev/infiniband with flags plus
// O_CLOEXEC and O_NOCTTY, and returns the descriptor only if fstat shows the
// char device ch names; otherwise it closes it and returns ErrIdentity.
func OpenChardev(ch *xtcpnl.RdmaChardev, flags int) (int, error) {
	return openChardev(rdmaCdevDir, ch, flags)
}

func openChardev(dir string, ch *xtcpnl.RdmaChardev, flags int) (int, error) {
	name := string(ch.NameBytes())
	if !rdmaName(name, xtcpnl.RdmaChardevNameMaxCst+1) || name == "." || name == ".." {
		return -1, ErrRequest
	}
	fd, err := unix.Open(dir+"/"+name, flags|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if err != nil {
		return -1, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return -1, errors.Join(err, unix.Close(fd))
	}
	if st.Mode&unix.S_IFMT != unix.S_IFCHR || st.Rdev != unix.Mkdev(ch.Major(), ch.Minor()) {
		return -1, errors.Join(ErrIdentity, unix.Close(fd))
	}
	return fd, nil
}
