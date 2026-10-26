package goip

import (
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is the one path from goip to a live kernel. Every other write
// verb serializes to GOIP_WRITE_OUT (docs/netlink/link-write-path.md,
// decision 5); the treatment actuator in internal/goip/actuate reaches the
// socket only through ApplyRouteControl, which stays here because it needs
// the socket's own descriptor and address.

// ApplyRouteControl is deliberately separate from the read and CLI byte-output
// paths. Only acknowledged route/rule mutations are admitted here.
func (s *NetlinkSource) ApplyRouteControl(request []byte) error {
	if len(request) < 28 {
		return errors.New("short route-control request")
	}
	typ := binary.LittleEndian.Uint16(request[4:6])
	if typ != unix.RTM_NEWROUTE && typ != unix.RTM_DELROUTE && typ != unix.RTM_NEWRULE && typ != unix.RTM_DELRULE {
		return errors.New("not a route-control mutation")
	}
	if binary.LittleEndian.Uint16(request[6:8])&unix.NLM_F_ACK == 0 {
		return errors.New("mutation requires acknowledgement")
	}
	got, _, err := xtcpnl.TalkRtnetlink(s.fd, request, s.sa)
	if err != nil {
		return err
	}
	if got != 0 {
		return errors.New("unexpected non-ACK mutation response")
	}
	return nil
}
