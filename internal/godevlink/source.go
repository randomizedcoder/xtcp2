package godevlink

import (
	"errors"

	"github.com/randomizedcoder/xtcp2/internal/wsreplay"
)

// Msg is one netlink message of a reply: the fields of its nlmsghdr the
// clone reads, and the body past it.
type Msg = wsreplay.Msg

// Reply is a request's answer: its messages up to and including the
// NLMSG_DONE or NLMSG_ERROR that ends it.
type Reply = wsreplay.Reply

// Source is where godevlink's requests go. devlink opens one generic
// netlink socket for the run (dl->nlg) and, for `resource show`, a fresh
// one per dpipe-table lookup; every request is a request/reply exchange,
// so one Talk is one request and the messages that answer it.
type Source interface {
	Talk(req []byte) (Reply, error)
}

var (
	// ErrSocket is a NETLINK_GENERIC socket that could not be opened.
	ErrSocket = errors.New("godevlink: NETLINK_GENERIC socket")
	// ErrSend is a request that could not be sent.
	ErrSend = errors.New("godevlink: send")
	// ErrReplay is a replay that does not match the capture: a request the
	// capture did not make, or more requests than it holds. Run exits 2.
	ErrReplay = errors.New("godevlink: replay")
)

const netlinkHeaderLen = wsreplay.HeaderLen

// ReplaySource answers from an nlmon (ws) pcap of devlink
// (internal/wsreplay). nlmsg_seq/nlmsg_pid and the genl command/version
// are zeroed before matching (wsreplay.CanonGenlCmd) — the command number
// is kernel-specific (devlink renumbers its enum between kernels), so it is
// not matched here; the 1P corpus test (xtcpnl_ws_devlink_test.go) ties each
// command to its decoded section instead. Everything else — the family
// (nlmsg_type), the dump/ack flags and every selector attribute — must be
// the capture's.
type ReplaySource struct {
	*wsreplay.Replay
}

// NewReplaySource reads the ws pcap at path.
func NewReplaySource(path string) (*ReplaySource, error) {
	r, err := wsreplay.Open(path, wsreplay.Options{
		Err: ErrReplay, Canon: wsreplay.CanonGenlCmd, Clone: "godevlink", Tool: "devlink",
	})
	if err != nil {
		return nil, err
	}
	return &ReplaySource{r}, nil
}
