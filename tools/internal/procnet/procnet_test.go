package procnet

import (
	"slices"
	"strings"
	"testing"
)

const header = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

// go test ./tools/internal/procnet/ -run TestParseStates
func TestParseStates(t *testing.T) {
	type input struct {
		table string
		port  uint16
	}
	type expect struct {
		states []State
		err    string
	}
	tests := []struct {
		name        string
		category    string
		description string
		input       input
		expected    expect
	}{
		{"one_v4", "positive", "a TIME_WAIT socket on the port",
			input{header + "   0: 0100007F:0FA0 0100007F:1F90 06 00000000:00000000 03:00001770 00000000     0        0 0\n", 4000},
			expect{[]State{TimeWait}, ""}},
		{"one_v6", "positive", "a /proc/net/tcp6 line",
			input{header + "   0: 00000000000000000000000001000000:0FA0 00000000000000000000000001000000:1F90 05 00000000:00000000 00:00000000 00000000  1000        0 1\n", 4000},
			expect{[]State{FinWait2}, ""}},
		{"no_match", "negative", "the port has no sockets",
			input{header + "   0: 0100007F:0FA1 0100007F:1F90 01 00000000:00000000 00:00000000 00000000     0        0 0\n", 4000},
			expect{nil, ""}},
		{"malformed", "negative", "a line without a port",
			input{header + "   0: 0100007F 0100007F:1F90 01\n", 4000},
			expect{nil, "local address"}},
		{"bad_state", "negative", "a state that isn't hex",
			input{header + "   0: 0100007F:0FA0 0100007F:1F90 zz\n", 4000},
			expect{nil, "state"}},
		{"header_only", "boundary", "an empty table",
			input{header, 4000},
			expect{nil, ""}},
		{"max_port", "boundary", "port 65535",
			input{header + "   0: 0100007F:FFFF 0100007F:1F90 08 00000000:00000000 00:00000000 00000000     0        0 0\n", 65535},
			expect{[]State{CloseWait}, ""}},
		{"remote_port_ignored", "corner", "only the local port matches",
			input{header + "   0: 0100007F:1F90 0100007F:0FA0 01 00000000:00000000 00:00000000 00000000     0        0 0\n", 4000},
			expect{nil, ""}},
		{"several", "corner", "a listener and its accepted socket share the port",
			input{header +
				"   0: 0100007F:0FA0 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 0\n" +
				"   1: 0100007F:0FA0 0100007F:1F90 01 00000000:00000000 00:00000000 00000000     0        0 0\n", 4000},
			expect{[]State{Established, Listen}, ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseStates(strings.NewReader(tc.input.table), tc.input.port)
			ok := slices.Equal(got, tc.expected.states)
			if tc.expected.err == "" {
				ok = ok && err == nil
			} else {
				ok = err != nil && strings.Contains(err.Error(), tc.expected.err)
			}
			if !ok {
				t.Errorf("%s [%s] %s: port %d: got %v, %v; expected %v, error containing %q",
					tc.name, tc.category, tc.description, tc.input.port, got, err, tc.expected.states, tc.expected.err)
			}
		})
	}
}

// go test ./tools/internal/procnet/ -run TestStateString
func TestStateString(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       State
		expected    string
	}{
		{"established", "positive", "state 1", Established, "ESTABLISHED"},
		{"time_wait", "positive", "state 6", TimeWait, "TIME_WAIT"},
		{"zero", "negative", "0 is not a TCP state", State(0), "STATE_0"},
		{"unknown", "negative", "past the last state", State(0x0d), "STATE_13"},
		{"first", "boundary", "the first state", State(1), "ESTABLISHED"},
		{"last", "boundary", "the last state, TCP_NEW_SYN_RECV", State(0x0c), "NEW_SYN_RECV"},
		{"listen", "corner", "state 10 is LISTEN", Listen, "LISTEN"},
		{"max", "corner", "the largest byte", State(255), "STATE_255"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.input.String(); got != tc.expected {
				t.Errorf("%s [%s] %s: State(%d).String() = %q, expected %q",
					tc.name, tc.category, tc.description, uint8(tc.input), got, tc.expected)
			}
		})
	}
}
