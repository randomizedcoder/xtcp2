package goip

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
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

// Path is r as a path under /ip, each token path-escaped, so a selector
// holding a `/` (a prefix) stays one segment.
func (r Resolved) Path() string {
	var b strings.Builder
	b.WriteString("/ip")
	for _, tok := range r.Argv() {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(tok))
	}
	return b.String()
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
	// ShowRequest has no Brief or Oneline until IA1-T02; refusing them here
	// keeps a dropped option from rendering the full form.
	if o.brief || o.oneline {
		return ShowRequest{}, Resolved{}, fmt.Errorf("options -brief and -oneline: %w", ErrNotImplemented)
	}
	if len(rest) == 0 {
		return ShowRequest{}, Resolved{}, fmt.Errorf("%w: no object", ErrInvalidArgument)
	}

	obj, err := lookupObject(rest[0])
	if err != nil {
		return ShowRequest{}, Resolved{}, err
	}
	res := Resolved{Object: obj.name, Verb: showVerbCst, Selectors: rest[1:]}
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

	canonical := obj.name
	if obj.aliasOf != "" {
		canonical = obj.aliasOf
	}
	r := ShowRequest{
		Object:    canonical,
		Family:    o.family,
		JSON:      o.json,
		Stats:     o.showStats > 0,
		Details:   o.showDetails > 0,
		Selectors: res.Selectors,
	}
	return r, res, nil
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
		if o.run == nil {
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

// Shortest returns the shortest spelling of every object goip implements,
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
