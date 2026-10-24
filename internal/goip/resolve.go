package goip

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// ErrWriteVerb is a verb that changes kernel state: `ip link s` is `ip link
// set`, which a read-only caller refuses. The error names the verb `ip`
// would run, never the abbreviation.
var ErrWriteVerb = errors.New("write verb refused: only the listing is served")

// Resolved is a command line as `ip` reads it: every token at its full
// spelling. It is what a caller logs, and what two spellings of one command
// converge on.
type Resolved struct {
	// Options are the options in force, at their full spelling and in ip's
	// chain order, each once: `-s -br -j` and `-json -stat -brief` are both
	// [-stats -brief -json]. A family is the last one given, as in ip.
	Options []string
	// Object is the object row the token matched: `n` is neighbor, and an
	// alias such as ntbl stays as typed.
	Object string
	// Verb is the show verb the token matched (show, list or lst), or show
	// when there was none.
	Verb string
	// Selectors are the tokens after the verb, untouched; the object's own
	// parser reads them.
	Selectors []string
}

// Argv is r as an `ip` command line, minus the program name.
func (r Resolved) Argv() []string {
	argv := make([]string, 0, len(r.Options)+2+len(r.Selectors))
	argv = append(argv, r.Options...)
	argv = append(argv, r.Object, r.Verb)
	return append(argv, r.Selectors...)
}

// Path is r as a path under /ip, each token escaped as one segment, so a
// selector holding a `/` (a prefix) stays one segment.
func (r Resolved) Path() string {
	var b strings.Builder
	b.WriteString("/ip")
	for _, tok := range r.Argv() {
		b.WriteByte('/')
		escapeSegment(&b, tok)
	}
	return b.String()
}

// escapeSegment writes tok with every byte that is not an RFC 3986 pchar
// percent-escaped, `/` and `%` included. It escapes no more than a request
// needs to, so a path is never longer for being canonical: url.PathEscape
// also escapes `,` and `;`, which tripled a label of commas.
func escapeSegment(b *strings.Builder, tok string) {
	const hex = "0123456789ABCDEF"
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if isPchar(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0xf])
	}
}

// isPchar reports whether c may appear raw in a path segment: unreserved,
// a sub-delim, `:` or `@` (RFC 3986 section 3.3).
func isPchar(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	}
	return strings.IndexByte("-._~!$&'()*+,;=:@", c) >= 0
}

// familyOptions are the -4, -6 and -0 spellings by family.
var familyOptions = map[uint8]string{
	unix.AF_INET:   "-4",
	unix.AF_INET6:  "-6",
	unix.AF_PACKET: "-0",
}

// ResolveShow reads tokens as the arguments of `ip`, the way Run does, and
// returns the read-only request they name.
//
// Options, the object and the verb resolve through ip's ordered chains
// (ipOptions, objects and the object's verbs), first match wins, and only
// then is the result checked: `ip link s` resolves to `set` and is refused
// as ErrWriteVerb, never served as a show. The selectors are not resolved
// here; Show hands them to the object's parser, which abbreviates them as ip
// does. ShowRequest.Selectors and Resolved.Selectors share tokens' backing
// array.
//
// The errors are ErrRefusedOption (-netns, -batch, -force, -rcvbuf, -loops,
// -help), ErrUnknownObject, ErrWriteVerb, ErrNotImplemented (an object,
// option or read verb goip does not serve) and ErrInvalidArgument (an
// unknown option or verb, no object, or `-s -s`).
func ResolveShow(tokens []string) (ShowRequest, Resolved, error) {
	o, rest, err := parseOptions(tokens)
	if err != nil {
		return ShowRequest{}, Resolved{}, resolveOptionError(err)
	}
	for _, row := range o.given {
		if row.refused {
			return ShowRequest{}, Resolved{}, fmt.Errorf("option %s: %w", row.name, ErrRefusedOption)
		}
	}
	if msg := statsCountMsg(o.showStats); msg != "" {
		return ShowRequest{}, Resolved{}, fmt.Errorf("%w: %s", ErrInvalidArgument, msg)
	}
	if len(rest) == 0 {
		return ShowRequest{}, Resolved{}, fmt.Errorf("%w: no object", ErrInvalidArgument)
	}

	obj, err := resolveObject(rest[0])
	if err != nil {
		return ShowRequest{}, Resolved{}, err
	}
	res := Resolved{Object: obj.name, Verb: showVerbOf(obj.canonical()), Selectors: rest[1:]}
	if len(rest) > 1 {
		v, ok := resolveVerb(obj.verbs, rest[1])
		switch {
		case !ok:
			return ShowRequest{}, Resolved{}, fmt.Errorf("%w: command %q is unknown for ip %s",
				ErrInvalidArgument, rest[1], obj.name)
		case v.kind == verbWrite:
			return ShowRequest{}, Resolved{}, fmt.Errorf("ip %s %s: %w", obj.name, v.name, ErrWriteVerb)
		case v.kind != verbShow:
			return ShowRequest{}, Resolved{}, fmt.Errorf("ip %s %s: %w", obj.name, v.name, ErrNotImplemented)
		}
		res.Verb, res.Selectors = v.name, rest[2:]
	}
	res.Options = canonicalOptions(&o)

	r := ShowRequest{
		Object:    obj.canonical(),
		Family:    o.family,
		JSON:      o.json,
		Stats:     o.showStats,
		Details:   o.showDetails > 0,
		Brief:     o.brief,
		Oneline:   o.oneline,
		Selectors: res.Selectors,
	}
	return r, res, nil
}

// resolveObject is lookupObject, naming an object goip does not implement
// as the command `ip` would run: "ip maddress show". An object Show does
// not serve (noShow: a monitor, or one whose listing goip leaves to the
// command line) is refused the same way.
func resolveObject(tok string) (object, error) {
	obj, err := lookupObject(tok)
	if errors.Is(err, ErrNotImplemented) || (err == nil && !obj.served()) {
		return obj, fmt.Errorf("ip %s %s: %w", obj.name, showVerbCst, ErrNotImplemented)
	}
	return obj, err
}

// LookupObject resolves tok as `ip` reads its object argument, first match
// wins, and returns the canonical name ShowRequest.Object takes: `a` is
// address and ntbl is ntable. The errors are ResolveShow's:
// ErrUnknownObject, or ErrNotImplemented naming `ip OBJECT show`.
func LookupObject(tok string) (string, error) {
	obj, err := resolveObject(tok)
	if err != nil {
		return "", err
	}
	return obj.canonical(), nil
}

// IsVerb reports whether tok resolves to any verb of the object canonically
// named object, write verbs included. A caller that reads the token after an
// object either as a verb or as something else (an interface name) uses it
// so that `link s` is never the interface s: it is `link set`, and refused.
func IsVerb(object, tok string) bool {
	for i := range objects {
		if objects[i].name == object {
			_, ok := resolveVerb(objects[i].verbs, tok)
			return ok
		}
	}
	return false
}

// Resolved is r as ResolveShow returns it for r.Argv(): the options at their
// full spelling, the canonical object and the verb show. Two spellings of
// one request share it, so its Path is the canonical location of either.
func (r ShowRequest) Resolved() Resolved {
	o := options{family: r.Family, json: r.JSON, brief: r.Brief, oneline: r.Oneline, showStats: r.Stats}
	if r.Details {
		o.showDetails = 1
	}
	return Resolved{Options: canonicalOptions(&o), Object: r.Object, Verb: showVerbOf(r.Object), Selectors: r.Selectors}
}

// resolveOptionError turns parseOptions' error into ResolveShow's: a
// refused option is refused even when its argument is missing.
func resolveOptionError(err error) error {
	var oe *optionError
	switch {
	case !errors.As(err, &oe):
		return err
	case oe.row == nil:
		return fmt.Errorf("%w: %v", ErrInvalidArgument, oe)
	case oe.row.refused:
		return fmt.Errorf("option %s: %w", oe.row.name, ErrRefusedOption)
	case oe.missing:
		return fmt.Errorf("%w: %v", ErrInvalidArgument, oe)
	}
	return fmt.Errorf("option %s: %w", oe.row.name, ErrNotImplemented)
}

// canonicalOptions is o at full spelling, in ip's chain order.
func canonicalOptions(o *options) []string {
	var out []string
	if f, ok := familyOptions[o.family]; ok {
		out = append(out, f)
	}
	for _, set := range []struct {
		on   bool
		name string
	}{
		{o.showStats > 0, optStatsCst},
		{o.showDetails > 0, optDetailsCst},
		{o.oneline, optOnelineCst},
		{o.brief, optBriefCst},
		{o.json, optJSONCst},
	} {
		if set.on {
			out = append(out, set.name)
		}
	}
	return out
}

// shortest is Shortest's table, built once.
var shortest = sync.OnceValue(func() map[string]string {
	m := make(map[string]string)
	for i := range objects {
		o := &objects[i]
		if !o.served() {
			continue
		}
		m[o.name] = shortestPrefix(o.name, false, func(p string) bool { return objectIndex(p) == i })
		if o.aliasOf != "" {
			continue
		}
		for j := range o.verbs {
			v := &o.verbs[j]
			if v.kind != verbShow {
				continue
			}
			m[o.name+" "+v.name] = shortestPrefix(v.name, v.exact, func(p string) bool { return verbIndex(o.verbs, p) == j })
		}
	}
	return m
})

// Shortest returns the shortest spelling of every object Show serves,
// keyed by name, and of every show verb of the canonical objects, keyed
// "object verb": the shortest prefix whose first match is that keyword
// (address: a, link show: sh). The map is a copy.
func Shortest() map[string]string {
	return maps.Clone(shortest())
}

// shortestPrefix is the shortest prefix of name that first resolves to it;
// an exact keyword has only its full spelling.
func shortestPrefix(name string, exact bool, first func(string) bool) string {
	if exact {
		return name
	}
	for n := 1; n < len(name); n++ {
		if first(name[:n]) {
			return name[:n]
		}
	}
	return name
}

// objectIndex is lookupObject's row index for arg, or -1.
func objectIndex(arg string) int {
	for i := range objects {
		if matchesPrefix(arg, objects[i].name) {
			return i
		}
	}
	return -1
}
