package gobridge

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

// bridge -batch: bridge.c batch and lib/utils.c do_batch, getcmdline and
// makeargs. One process runs every line, so whatever a command leaves
// behind reaches the next: the filter statics (B28, B29), the link cache
// (ll_init_map dumps once), the vlan port state. A line whose command
// returns non-zero prints "Command failed NAME:LINE" and ends the batch,
// unless -force; a command that calls exit() ends the process (B30).

// maxArgs is MAX_ARGS: makeargs refuses a line's 512th word.
const maxArgs = 512

// batch is batch() and do_batch(name, force, br_batch_cmd): name "-" is
// stdin. Its result is main's return: 1 when any line failed.
func (r *runner) batch(name string, stdin io.Reader, force bool) error {
	in := stdin
	if name != "-" {
		f, err := os.Open(name)
		if err != nil {
			r.errf("Cannot open file \"%s\" for reading: %s\n", name, strerror(errnoOf(err)))
			return retCode(1)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil && r.errOut == nil {
				r.errOut = cerr
			}
		}()
		in = f
	}
	br := bufio.NewReader(in)
	failed := false
	lineno := 0 // cmdlineno
	for {
		line, ok := r.getcmdline(br, &lineno)
		if !ok {
			break
		}
		argv, err := r.makeargs(line)
		if err != nil {
			return err
		}
		if len(argv) == 0 {
			continue // a blank line
		}
		if err := r.doCmd(argv[0], argv[1:]); err != nil {
			var ret retCode
			if !errors.As(err, &ret) {
				return err // exit(), a crash, or gobridge's own refusal
			}
			r.errf("Command failed %s:%d\n", name, lineno)
			failed = true
			if !force {
				break
			}
		}
	}
	if failed {
		return retCode(1)
	}
	return nil
}

// getline is glibc getline: the next line with its '\n', or the last one
// without; false at the end. A read error ends input as EOF does.
func getline(br *bufio.Reader) (string, bool) {
	s, err := br.ReadString('\n')
	if s == "" && err != nil {
		return "", false
	}
	return s, true
}

// cstr is s as the C string functions see it: up to its first NUL.
func cstr(s string) string {
	s, _, _ = strings.Cut(s, "\x00")
	return s
}

// getcmdline is lib/utils.c getcmdline: a line, cut at its first '#',
// with each "\\\n" joined to the next line (itself cut at '#'). A
// continuation with no next line is "Missing continuation line" and ends
// the input, so the batch exits with what it has (B31).
func (r *runner) getcmdline(br *bufio.Reader, lineno *int) (string, bool) {
	line, ok := getline(br)
	if !ok {
		return "", false
	}
	*lineno++
	line = cstr(line)
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	for {
		i := strings.Index(line, "\\\n")
		if i < 0 {
			break
		}
		line1, ok := getline(br)
		if !ok {
			r.errf("Missing continuation line\n")
			return "", false
		}
		*lineno++
		line1 = cstr(line1)
		if j := strings.IndexByte(line1, '#'); j >= 0 {
			line1 = line1[:j]
		}
		line = line[:i] + line1
	}
	return line, true
}

// makeargs is lib/utils.c makeargs: words split on " \t\r\n"; a word that
// begins with a quote runs to the next same quote, which ends it even with
// no space after. An unterminated quote and a 512th word exit 1.
func (r *runner) makeargs(line string) ([]string, error) {
	const ws = " \t\r\n"
	var argv []string
	cp := 0
	for cp < len(line) {
		for cp < len(line) && strings.IndexByte(ws, line[cp]) >= 0 {
			cp++
		}
		if cp == len(line) {
			break
		}
		if len(argv) >= maxArgs-1 {
			r.errf("Too many arguments to command\n")
			return nil, exitCode(1)
		}
		if q := line[cp]; q == '\'' || q == '"' {
			cp++
			j := strings.IndexByte(line[cp:], q)
			if j < 0 {
				r.errf("Unterminated quoted string\n")
				return nil, exitCode(1)
			}
			argv = append(argv, line[cp:cp+j])
			cp += j
		} else {
			j := strings.IndexAny(line[cp:], ws)
			if j < 0 {
				argv = append(argv, line[cp:])
				break
			}
			argv = append(argv, line[cp:cp+j])
			cp += j
		}
		cp++ // *cp++ = 0: the separator or closing quote
	}
	return argv, nil
}
