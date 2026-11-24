package procnet

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
)

// State is a TCP state as include/net/tcp_states.h numbers it.
type State uint8

// The TCP states (include/net/tcp_states.h).
const (
	Established State = iota + 1
	SynSent
	SynRecv
	FinWait1
	FinWait2
	TimeWait
	Close
	CloseWait
	LastAck
	Listen
	Closing
	NewSynRecv
)

var stateNames = [...]string{
	Established: "ESTABLISHED",
	SynSent:     "SYN_SENT",
	SynRecv:     "SYN_RECV",
	FinWait1:    "FIN_WAIT1",
	FinWait2:    "FIN_WAIT2",
	TimeWait:    "TIME_WAIT",
	Close:       "CLOSE",
	CloseWait:   "CLOSE_WAIT",
	LastAck:     "LAST_ACK",
	Listen:      "LISTEN",
	Closing:     "CLOSING",
	NewSynRecv:  "NEW_SYN_RECV",
}

// String is the state's tcp_states.h name, or STATE_<n> outside it.
func (s State) String() string {
	if int(s) < len(stateNames) && stateNames[s] != "" {
		return stateNames[s]
	}
	return "STATE_" + strconv.Itoa(int(s))
}

// ParseStates returns, sorted, the states of the sockets in a
// /proc/net/tcp or /proc/net/tcp6 table whose local port is port.
func ParseStates(r io.Reader, port uint16) ([]State, error) {
	var out []State
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			return nil, fmt.Errorf("procnet: short line %q", sc.Text())
		}
		i := strings.LastIndexByte(f[1], ':')
		if i < 0 {
			return nil, fmt.Errorf("procnet: local address %q has no port", f[1])
		}
		p, err := strconv.ParseUint(f[1][i+1:], 16, 16)
		if err != nil {
			return nil, fmt.Errorf("procnet: local address %q: %w", f[1], err)
		}
		if uint16(p) != port {
			continue
		}
		st, err := strconv.ParseUint(f[3], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("procnet: state %q: %w", f[3], err)
		}
		out = append(out, State(st))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	slices.Sort(out)
	return out, nil
}

// Lookup returns, sorted, the states of this network namespace's TCP
// sockets, both families, whose local port is port. A missing tcp6 table
// (IPv6 disabled) counts as empty.
func Lookup(port uint16) ([]State, error) {
	var out []State
	for _, name := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		s, err := ParseStates(f, port)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, s...)
	}
	slices.Sort(out)
	return out, nil
}
