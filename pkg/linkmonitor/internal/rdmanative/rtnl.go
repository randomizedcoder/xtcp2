package rdmanative

import (
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

// ErrRtnl is an rtnetlink link or address message too short for its fixed
// header.
var ErrRtnl = errors.New("rdmanative: rtnetlink message shorter than its header")

// Last known link state of a ref.
const (
	stateUnknown uint8 = iota
	stateDown
	stateUp
)

// Translator turns rtnetlink link and address notifications into the events
// the verbs source would have reported: a port's running-and-carrier change
// is a Refresh (PORT_ACTIVE or PORT_ERR), an address change a Topology
// (GID_CHANGE). It is owned by the one goroutine that reads the socket, so it
// needs no lock.
type Translator struct {
	m     *PortMap
	state []uint8 // indexed like m.refs
}

// NewTranslator starts with every port's state unknown, so the first link
// message for a port is reported until Seed records the dumped state.
func NewTranslator(m *PortMap) *Translator {
	return &Translator{m: m, state: make([]uint8, len(m.refs))}
}

// Seed records the state in one RTM_NEWLINK from the link dump without
// reporting it, so the first notification after startup is compared with the
// dumped state rather than taken for a transition. Other types are ignored.
func (t *Translator) Seed(msgType uint16, body []byte) error {
	if msgType != rtmNewLink {
		return nil
	}
	index, state, err := linkState(body)
	if err != nil {
		return err
	}
	lo, hi := t.m.find(index)
	for i := lo; i < hi && i < len(t.state); i++ {
		if t.m.refs[i].kind&refLink != 0 {
			t.state[i] = state
		}
	}
	return nil
}

// Translate writes the events for one rtnetlink message body into out and
// returns how many it wrote. RTM_NEWLINK reports a Refresh for each port
// whose state changed, RTM_DELLINK one for each port, RTM_NEWADDR and
// RTM_DELADDR a Topology for each port the address reaches; anything else,
// or an ifindex the map does not hold, reports nothing. A message too short
// for its header is ErrRtnl and changes no state. It does not allocate.
func (t *Translator) Translate(msgType uint16, body []byte, out *[MaxPorts]rdmaevents.Event) (int, error) {
	switch msgType {
	case rtmNewLink, rtmDelLink:
		index, state, err := linkState(body)
		if err != nil {
			return 0, err
		}
		if msgType == rtmDelLink {
			state = stateDown
		}
		return t.link(index, state, msgType == rtmDelLink, out), nil
	case rtmNewAddr, rtmDelAddr:
		index, err := addrIndex(body)
		if err != nil {
			return 0, err
		}
		return t.emit(index, refAddr, rdmaevents.Topology, out), nil
	}
	return 0, nil
}

func (t *Translator) link(index uint32, state uint8, always bool, out *[MaxPorts]rdmaevents.Event) int {
	n := 0
	lo, hi := t.m.find(index)
	for i := lo; i < hi && i < len(t.state) && n < len(out); i++ {
		r := t.m.refs[i]
		if r.kind&refLink == 0 || (!always && t.state[i] == state) {
			continue
		}
		t.state[i] = state
		out[n] = rdmaevents.Event{Device: t.m.names[r.device], Port: r.port, Kind: rdmaevents.Refresh}
		n++
	}
	return n
}

func (t *Translator) emit(index uint32, kind refKind, event rdmaevents.Kind, out *[MaxPorts]rdmaevents.Event) int {
	n := 0
	lo, hi := t.m.find(index)
	for i := lo; i < hi && n < len(out); i++ {
		r := t.m.refs[i]
		if r.kind&kind == 0 {
			continue
		}
		out[n] = rdmaevents.Event{Device: t.m.names[r.device], Port: r.port, Kind: event}
		n++
	}
	return n
}
