package govdpa

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

// Source is where govdpa's requests go. vdpa opens one generic netlink
// socket for the run (vdpa->nlg), and every request is a request/reply
// exchange, so one Talk is one request and the messages that answer it.
type Source interface {
	Talk(req []byte) (Reply, error)
}

// ErrReplay is a replay that does not match the capture: a request the
// capture did not make, or more requests than it holds. Run exits 2.
var ErrReplay = errors.New("govdpa: replay")

const netlinkHeaderLen = wsreplay.HeaderLen

// ReplaySource answers from an nlmon (ws) pcap of vdpa (internal/wsreplay).
// A request is matched byte for byte once nlmsg_seq and nlmsg_pid are
// zeroed (vdpa's seq is the time it ran). Unlike godevlink's, the genl
// command byte IS matched: vdpa's command enum is append-only, so the
// capture kernel's numbers are upstream's.
type ReplaySource struct {
	*wsreplay.Replay
}

// NewReplaySource reads the ws pcap at path.
func NewReplaySource(path string) (*ReplaySource, error) {
	r, err := wsreplay.Open(path, wsreplay.Options{Err: ErrReplay, Clone: "govdpa", Tool: "vdpa"})
	if err != nil {
		return nil, err
	}
	return &ReplaySource{r}, nil
}
