package goip

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const netnsAutoCst = "auto"

// netnsSet is netns_set: the name, the nsid or `auto` (-1, the kernel's
// pick), then set_netns_id_from_name's RTM_NEWNSID on the name's file.
// Words after the nsid are ignored. A file that will not open is exit 1
// with nothing printed.
func netnsSet(c *runCtx, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: No netns name specified", ErrInvalidArgument)
	}
	if len(args) < 2 {
		return fmt.Errorf("%w: No nsid specified", ErrInvalidArgument)
	}
	nsid := int32(-1)
	if args[1] != netnsAutoCst {
		v, ok := getInteger(args[1], 0)
		switch {
		case !ok:
			return invargErr("Invalid \"netnsid\" value", args[1])
		case v < 0:
			return invargErr("\"netnsid\" value should be >= 0", args[1])
		}
		nsid = int32(v)
	}
	// set_netns_id_from_name opens a name with a slash as a path, but
	// do_netns's invalid_name has refused any such name already, short of
	// `ip -all`, which goip has not.
	path := filepath.Join(netnsRunDir, args[0])
	fd, err := netnsOpen(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrInvalidArgument, path, err)
	}
	u, ok := cint.U32(fd)
	if !ok {
		return errors.Join(fmt.Errorf("%w: %s: descriptor %d", ErrInvalidArgument, path, fd), unix.Close(fd))
	}
	b, err := xtcpnl.BuildSetNsidRequest(u, nsid, c.nextSeq())
	if cerr := unix.Close(fd); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("goip: build netns set request: %w", err)
	}
	return writeAddrOut(b)
}
