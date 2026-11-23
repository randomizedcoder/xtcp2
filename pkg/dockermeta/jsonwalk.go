package dockermeta

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// The Docker bodies carry tenant-chosen data (image labels, names) that we
// never use. A cap on the whole body would let a tenant hide its container
// (or, through /containers/json, freeze the host's index) by padding its
// labels, so instead the walker below keeps only the projected fields and
// skips everything else without buffering it, so memory is bounded by the
// kept fields (about 37 KiB per walk) whatever the body's size. The byte
// limits below bound CPU (the walk reads ~130-200 MB/s) and are set far above
// real bodies; a body over one drops that one container, and is logged.
//
// Accept/reject matches json.NewDecoder(r).Decode into the same structs:
// syntax is validated everywhere (skipped values included), keys match
// case-insensitively and the last duplicate wins, null leaves a field
// unchanged, a wrong type is an error, nesting is limited to 10000 and
// data after the top-level value is ignored. FuzzDockerProjection checks
// this against encoding/json.

// Docker body limits. Measured on a host with 31 containers: inspect bodies
// 6.8-13.7 KB, labels at most 638 B, /containers/json 78 KB (~2.5 KB per
// container). Documented in docs/security-hardening/focused-container-
// hardening.md (C5).
const (
	// maxInspectBody bounds one GET /containers/{id}/json: ~300x the largest
	// measured. Over it, the container is skipped.
	maxInspectBody = 4 << 20
	// maxListEntry bounds one container's entry in GET /containers/json, the
	// same as an inspect body. Over it, that entry is dropped, so a few
	// padded containers cannot fail the whole list.
	maxListEntry = maxInspectBody
	// The list has no total byte limit: a total would let a few padded
	// containers (16 at 4 MiB) fail every re-list. Memory is constant, the
	// tenant-driven CPU is bounded per entry, and requestTimeout stops a
	// body that never ends.
	// maxWalkDepth is encoding/json's nesting limit.
	maxWalkDepth = 10000
	// maxKeyRaw is the longest key kept for matching; the longest wanted key,
	// NetworkSettings, is 90 bytes with every rune \u-escaped.
	maxKeyRaw = 256
	// maxTextRaw caps a kept Name or Image: at least 682 decoded bytes, so
	// strpolicy.Clean's 256-byte result is the same as for the full string.
	maxTextRaw = 4 << 10
	// maxIDRaw caps the daemon-set ID and SandboxKey; longer is an error.
	maxIDRaw = 4 << 10
	// maxNumberRaw caps an int token; a valid int is at most 20 bytes.
	maxNumberRaw = 64
)

var (
	errWalkSyntax   = errors.New("json syntax error")
	errWalkType     = errors.New("json type mismatch")
	errWalkDepth    = errors.New("json nesting too deep")
	errFieldTooLong = errors.New("daemon-set field over its cap")
)

// walker reads one JSON value from r, keeping only what its callers ask for.
type walker struct {
	r     *bufio.Reader
	c     *capReader
	depth int
}

// noLimit is the limit for a body bounded only by requestTimeout.
const noLimit = 0

// capReader counts the bytes read and, with a limit, fails with
// errBodyTooLarge once more than limit are read.
type capReader struct {
	r     io.Reader
	read  int64
	limit int64 // noLimit: count only
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.limit == noLimit {
		k, err := c.r.Read(p)
		c.read += int64(k)
		return k, err
	}
	if c.read > c.limit {
		return 0, errBodyTooLarge
	}
	if room := c.limit - c.read + 1; int64(len(p)) > room {
		p = p[:room]
	}
	k, err := c.r.Read(p)
	c.read += int64(k)
	if c.read > c.limit {
		return k, errBodyTooLarge
	}
	return k, err
}

func newWalker(r io.Reader, limit int64) *walker {
	c := &capReader{r: r, limit: limit}
	return &walker{r: bufio.NewReaderSize(c, 32<<10), c: c}
}

// overLimit reports that the walk consumed more than the limit. capReader's
// error can trail the last bytes it hands over, which a walk may finish on.
func (w *walker) overLimit() bool {
	return w.c.limit != noLimit && w.pos() > w.c.limit
}

// pos is how many body bytes the walk has consumed.
func (w *walker) pos() int64 {
	return w.c.read - int64(w.r.Buffered())
}

// next skips whitespace and returns the next byte without consuming it.
func (w *walker) next() (byte, error) {
	for {
		b, err := w.r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, err
		}
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return b, w.r.UnreadByte()
	}
}

func (w *walker) expect(want byte) error {
	b, err := w.r.ReadByte()
	if err != nil {
		return eofIsSyntax(err)
	}
	if b != want {
		return fmt.Errorf("%w: %q, expected %q", errWalkSyntax, b, want)
	}
	return nil
}

func eofIsSyntax(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

func (w *walker) push() error {
	w.depth++
	if w.depth > maxWalkDepth {
		return errWalkDepth
	}
	return nil
}

// skip consumes and validates one value of any type.
func (w *walker) skip() error {
	b, err := w.next()
	if err != nil {
		return err
	}
	switch {
	case b == '{':
		return w.object(func(string) error { return w.skip() })
	case b == '[':
		return w.array(w.skip)
	case b == '"':
		_, _, err := w.str(0)
		return err
	case b == '-' || (b >= '0' && b <= '9'):
		_, _, err := w.number(0)
		return err
	case b == 't':
		return w.literal("true")
	case b == 'f':
		return w.literal("false")
	case b == 'n':
		return w.literal("null")
	}
	return fmt.Errorf("%w: %q starts no value", errWalkSyntax, b)
}

func (w *walker) literal(lit string) error {
	for i := 0; i < len(lit); i++ {
		if err := w.expect(lit[i]); err != nil {
			return err
		}
	}
	return nil
}

// object walks {"k":v,...}, calling field for each key with the reader at
// the value; field must consume the value. The opening brace must be next.
func (w *walker) object(field func(key string) error) error {
	if err := w.expect('{'); err != nil {
		return err
	}
	if err := w.push(); err != nil {
		return err
	}
	b, err := w.next()
	if err != nil {
		return err
	}
	if b == '}' {
		w.depth--
		return w.expect('}')
	}
	for {
		if b != '"' {
			return fmt.Errorf("%w: %q, expected a key", errWalkSyntax, b)
		}
		key, over, err := w.str(maxKeyRaw)
		if err != nil {
			return err
		}
		if over {
			key = "" // too long to be a wanted key
		}
		if _, err = w.next(); err != nil {
			return err
		}
		if err := w.expect(':'); err != nil {
			return err
		}
		if err := field(key); err != nil {
			return err
		}
		if b, err = w.next(); err != nil {
			return err
		}
		if b == '}' {
			w.depth--
			return w.expect('}')
		}
		if err := w.expect(','); err != nil {
			return err
		}
		if b, err = w.next(); err != nil {
			return err
		}
	}
}

// array walks [v,...], calling elem for each element. The opening bracket
// must be next.
func (w *walker) array(elem func() error) error {
	if err := w.expect('['); err != nil {
		return err
	}
	if err := w.push(); err != nil {
		return err
	}
	b, err := w.next()
	if err != nil {
		return err
	}
	if b == ']' {
		w.depth--
		return w.expect(']')
	}
	for {
		if err := elem(); err != nil {
			return err
		}
		if b, err = w.next(); err != nil {
			return err
		}
		if b == ']' {
			w.depth--
			return w.expect(']')
		}
		if err := w.expect(','); err != nil {
			return err
		}
	}
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// str consumes a string (the opening quote must be next), validating it, and
// returns its decoded value from at most keep raw bytes; over reports that
// the raw string was longer. The cut falls between escapes, never inside one.
// keep 0 decodes nothing.
func (w *walker) str(keep int) (string, bool, error) {
	if err := w.expect('"'); err != nil {
		return "", false, err
	}
	raw := make([]byte, 0, min(keep, 64)+2)
	raw = append(raw, '"')
	over := false
	for {
		b, err := w.r.ReadByte()
		if err != nil {
			return "", false, eofIsSyntax(err)
		}
		seq := [6]byte{b}
		n := 1
		switch {
		case b == '"':
			if keep == 0 {
				return "", over, nil
			}
			raw = append(raw, '"')
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return "", false, fmt.Errorf("%w: %v", errWalkSyntax, err)
			}
			return s, over, nil
		case b < 0x20:
			return "", false, fmt.Errorf("%w: control byte %#x in string", errWalkSyntax, b)
		case b == '\\':
			e, err := w.r.ReadByte()
			if err != nil {
				return "", false, eofIsSyntax(err)
			}
			seq[1], n = e, 2
			switch e {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				for i := 0; i < 4; i++ {
					h, err := w.r.ReadByte()
					if err != nil {
						return "", false, eofIsSyntax(err)
					}
					if !isHex(h) {
						return "", false, fmt.Errorf("%w: bad \\u escape", errWalkSyntax)
					}
					seq[n] = h
					n++
				}
			default:
				return "", false, fmt.Errorf("%w: bad escape \\%c", errWalkSyntax, e)
			}
		}
		if !over && len(raw)-1+n <= keep {
			raw = append(raw, seq[:n]...)
		} else {
			over = true
		}
	}
}

// Number states: JSON's number grammar as a DFA. numFinal marks the states
// a number may end in.
const (
	numStart = iota
	numMinus
	numZero
	numInt
	numDot
	numFrac
	numExp
	numExpSign
	numExpDigits
)

var numFinal = [...]bool{numZero: true, numInt: true, numFrac: true, numExpDigits: true}

// numNext is the state after byte b in state st, or -1 when b ends the token.
func numNext(st int, b byte) int {
	digit := b >= '0' && b <= '9'
	switch st {
	case numStart:
		switch {
		case b == '-':
			return numMinus
		case b == '0':
			return numZero
		case digit:
			return numInt
		}
	case numMinus:
		switch {
		case b == '0':
			return numZero
		case digit:
			return numInt
		}
	case numZero, numInt:
		switch {
		case st == numInt && digit:
			return numInt
		case b == '.':
			return numDot
		case b == 'e' || b == 'E':
			return numExp
		}
	case numDot, numFrac:
		switch {
		case digit:
			return numFrac
		case st == numFrac && (b == 'e' || b == 'E'):
			return numExp
		}
	case numExp:
		switch {
		case b == '+' || b == '-':
			return numExpSign
		case digit:
			return numExpDigits
		}
	case numExpSign, numExpDigits:
		if digit {
			return numExpDigits
		}
	}
	return -1
}

// number consumes a number token, validating JSON's grammar, and returns up
// to keep bytes of it; over reports a longer token.
func (w *walker) number(keep int) (string, bool, error) {
	var tok []byte
	over := false
	st := numStart
	for {
		b, err := w.r.ReadByte()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", false, err
		}
		next := numNext(st, b)
		if next < 0 {
			if err := w.r.UnreadByte(); err != nil {
				return "", false, err
			}
			break
		}
		st = next
		if len(tok) < keep {
			tok = append(tok, b)
		} else {
			over = true
		}
	}
	if !numFinal[st] {
		return "", false, fmt.Errorf("%w: bad number", errWalkSyntax)
	}
	return string(tok), over, nil
}

// keptString reads a string field into *dst (null leaves it unchanged). A
// raw string over keep is cut, or, when tooLong is non-nil, left out of *dst
// and flagged there; the walk goes on either way.
func (w *walker) keptString(dst *string, keep int, tooLong *bool) error {
	b, err := w.next()
	if err != nil {
		return err
	}
	switch b {
	case 'n':
		return w.literal("null")
	case '"':
		s, over, err := w.str(keep)
		if err != nil {
			return err
		}
		if over && tooLong != nil {
			*tooLong = true
			return nil
		}
		*dst = s
		return nil
	}
	if err := w.skip(); err != nil {
		return err
	}
	return errWalkType
}

// keptInt reads an int field into *dst (null leaves it unchanged).
func (w *walker) keptInt(dst *int) error {
	b, err := w.next()
	if err != nil {
		return err
	}
	if b == 'n' {
		return w.literal("null")
	}
	if b != '-' && (b < '0' || b > '9') {
		if err := w.skip(); err != nil {
			return err
		}
		return errWalkType
	}
	tok, over, err := w.number(maxNumberRaw)
	if err != nil {
		return err
	}
	if over {
		return errWalkType // no int is that long
	}
	var n int
	if err := json.Unmarshal([]byte(tok), &n); err != nil {
		return errWalkType
	}
	*dst = n
	return nil
}

// keptObject walks an object field (null leaves it unchanged).
func (w *walker) keptObject(field func(key string) error) error {
	b, err := w.next()
	if err != nil {
		return err
	}
	switch b {
	case 'n':
		return w.literal("null")
	case '{':
		return w.object(field)
	}
	if err := w.skip(); err != nil {
		return err
	}
	return errWalkType
}

// decodeInspect walks a GET /containers/{id}/json body into the inspect
// projection. An ID or SandboxKey over maxIDRaw is errFieldTooLong.
func decodeInspect(r io.Reader, limit int64) (inspect, error) {
	var in inspect
	tooLong := false
	w := newWalker(r, limit)
	err := w.keptObject(func(key string) error {
		switch {
		case strings.EqualFold(key, "Id"):
			return w.keptString(&in.ID, maxIDRaw, &tooLong)
		case strings.EqualFold(key, "Name"):
			return w.keptString(&in.Name, maxTextRaw, nil)
		case strings.EqualFold(key, "Image"):
			return w.keptString(&in.Image, maxTextRaw, nil)
		case strings.EqualFold(key, "Config"):
			return w.keptObject(func(k string) error {
				if strings.EqualFold(k, "Image") {
					return w.keptString(&in.Config.Image, maxTextRaw, nil)
				}
				return w.skip()
			})
		case strings.EqualFold(key, "State"):
			return w.keptObject(func(k string) error {
				if strings.EqualFold(k, "Pid") {
					return w.keptInt(&in.State.Pid)
				}
				return w.skip()
			})
		case strings.EqualFold(key, "NetworkSettings"):
			return w.keptObject(func(k string) error {
				if strings.EqualFold(k, "SandboxKey") {
					return w.keptString(&in.NetworkSettings.SandboxKey, maxIDRaw, &tooLong)
				}
				return w.skip()
			})
		}
		return w.skip()
	})
	switch {
	case err == nil && w.overLimit():
		err = errBodyTooLarge
	case err == nil && tooLong:
		err = errFieldTooLong
	}
	return in, err
}

// decodeIDs walks a GET /containers/json body (limit noLimit: none) and
// returns each container's
// Id, and how many entries it dropped: those over entryLimit bytes, and those
// whose Id is over maxIDRaw. A wrong type anywhere fails the list, as
// encoding/json would.
func decodeIDs(r io.Reader, limit, entryLimit int64) ([]string, int, error) {
	var ids []string
	dropped := 0
	w := newWalker(r, limit)
	b, err := w.next()
	if err != nil {
		return nil, 0, err
	}
	switch b {
	case 'n':
		return nil, 0, w.literal("null")
	case '[':
	default:
		if err := w.skip(); err != nil {
			return nil, 0, err
		}
		return nil, 0, errWalkType
	}
	err = w.array(func() error {
		var id string
		tooLong := false
		if _, err := w.next(); err != nil {
			return err
		}
		start := w.pos()
		err := w.keptObject(func(key string) error {
			if strings.EqualFold(key, "Id") {
				return w.keptString(&id, maxIDRaw, &tooLong)
			}
			return w.skip()
		})
		switch {
		case err != nil:
		case tooLong || w.pos()-start > entryLimit:
			dropped++
		default:
			ids = append(ids, id)
		}
		return err
	})
	return ids, dropped, err
}
