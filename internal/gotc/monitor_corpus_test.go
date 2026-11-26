package gotc

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// monitorStampRe matches both of print_timestamp's forms: the asctime line
// and the -tshort prefix.
var monitorStampRe = regexp.MustCompile(`(?m)^Timestamp: .* usec$|^\[[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+\] `)

func maskStamps(s string) string {
	return monitorStampRe.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "[") {
			return "[STAMP] "
		}
		return "Timestamp: STAMP"
	})
}

// shellWords splits a command line the way the rows write them: words
// apart on spaces, and '...' quoting.
func shellWords(s string) []string {
	var out []string
	var w strings.Builder
	in, quoted := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted && c == '\'':
			quoted = false
		case quoted:
			w.WriteByte(c)
		case c == '\'':
			quoted, in = true, true
		case c == ' ':
			if in {
				out = append(out, w.String())
				w.Reset()
				in = false
			}
		default:
			w.WriteByte(c)
			in = true
		}
	}
	if in {
		out = append(out, w.String())
	}
	return out
}

// TestShellWords holds the row splitter to the forms the rows use.
func TestShellWords(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    string // words joined by |
	}{
		{"positive: bare words", "tc monitor file m.bin", "tc|monitor|file|m.bin"},
		{"positive: quoted words", "xtcp2-nl-tcmon '-s monitor' 'tc qdisc add dev d0' 'qdisc'", "xtcp2-nl-tcmon|-s monitor|tc qdisc add dev d0|qdisc"},
		{"boundary: empty", "", ""},
		{"corner: an empty quoted word", "a '' b", "a||b"},
		{"corner: doubled spaces", "a  b", "a|b"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := strings.Join(shellWords(tc.in), "|"); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestGotcMonitorCorpus replays every tcmonitor row (gotc 14R): the live
// rows with the monitor's arguments from the helper's first word, the
// offline rows as they are. stdout, stderr and the exit status are held to
// tc's once both stamp forms are masked. An offline row must ask every
// request it recorded; a live row's capture holds the trigger's too.
//
// go test ./internal/gotc/ -run TestGotcMonitorCorpus
func TestGotcMonitorCorpus(t *testing.T) {
	base, err := filepath.Abs("../../pkg/xtcpnl/testdata/7_3_1/ws/tcmonitor")
	if err != nil {
		t.Fatal(err)
	}
	pschedContent = readFile(t, pschedRow)
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	rows, live := 0, 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		c := readCmdJSON(t, dir)
		rows++
		words := shellWords(c.Cmd)
		isLive := len(words) > 0 && words[0] == "xtcp2-nl-tcmon"
		if isLive {
			live++
		}
		t.Run(c.ID, func(t *testing.T) {
			if c.Result.Status != "OK" {
				t.Fatalf("row status %s", c.Result.Status)
			}
			var args []string
			switch {
			case isLive && len(words) >= 3:
				args = strings.Fields(words[1])
			case len(words) > 0 && words[0] == "tc":
				args = words[1:]
			default:
				t.Fatalf("cmd %q is neither tc nor the helper", c.Cmd)
			}
			t.Chdir(dir)
			got := replayIn(t, dir, strings.NewReader(""), args...)
			wantRC, err := strconv.Atoi(strings.TrimSpace(readFile(t, filepath.Join(dir, "ip.rc"))))
			if err != nil {
				t.Fatal(err)
			}
			want := result{
				stdout: maskStamps(readFile(t, filepath.Join(dir, "ip.out"))),
				stderr: maskStamps(readFile(t, filepath.Join(dir, "ip.err"))),
				rc:     wantRC,
			}
			got.stdout, got.stderr = maskStamps(got.stdout), maskStamps(got.stderr)
			if isLive {
				got.pending = 0
			}
			if got != want {
				t.Fatalf("%s\n got  %#v\n want %#v", c.Cmd, got, want)
			}
		})
	}
	if rows != 27 || live != 13 {
		t.Errorf("%d rows, %d live; want 27, 13", rows, live)
	}
}
