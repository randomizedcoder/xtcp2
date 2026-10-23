package goip

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"golang.org/x/sys/unix"
)

// ErrNoSuchDevice marks a `dev NAME` (or master/vrf NAME) that names no
// interface, whether the kernel said so (ENODEV on the by-name get) or the
// link cache did (a NameToIndex miss).
//
// The CLI never needed it: `ip` prints a message and exits, and goip's
// messages for this case already match. A caller that maps errors to
// something other than an exit code — an HTTP 404 — does need it, and the
// sites spell the failure five different ways, two of them wrapping
// ErrNotImplemented or ErrInvalidArgument for their exit code.
var ErrNoSuchDevice = errors.New("no such device")

// noSuchDeviceError adds ErrNoSuchDevice to an error's chain without touching
// its message, so `ip`'s wording on stderr is exactly what it was.
type noSuchDeviceError struct{ err error }

func (e noSuchDeviceError) Error() string   { return e.err.Error() }
func (e noSuchDeviceError) Unwrap() []error { return []error{e.err, ErrNoSuchDevice} }

func noSuchDevice(err error) error { return noSuchDeviceError{err: err} }

// ShowRequest is one read-only `ip [OPTIONS] OBJECT show [SELECTORS]`.
//
// It is the typed sibling of Run's argv for callers that are not a shell —
// the HTTP paths in goiphttp. The options are fields rather than flags so
// there is nothing to prefix-match, and the verb is not a field at all: Show
// always runs `show`, which is what makes it read-only by construction.
type ShowRequest struct {
	// Object is a canonical object name from ShowObjects. It is matched
	// exactly; `a` is not `address` here, because abbreviations are a
	// property of a command line and this is not one.
	Object string
	// Family is AF_UNSPEC, AF_INET, AF_INET6 or AF_PACKET — Run's -4/-6/-0.
	Family uint8
	// JSON, Stats and Details are -j, -s and -d.
	JSON, Stats, Details bool
	// Selectors are the arguments after `show`, exactly as `ip` would get
	// them (`dev eth0`, `table all`). The caller builds them; Show passes
	// them to the object's own selector parser unchanged, so the two entry
	// points cannot accept different things.
	Selectors []string
}

// showObjects is the exact set Show serves, sorted: every object whose show
// path goip implements, by its canonical name in the objects table. It is
// derived from that table rather than spelled out again, so the two cannot
// drift. Aliases (the British spelling of neighbor, ntbl) are left out so
// each object has one spelling, and so is monitor, which never ends.
var showObjects = implementedObjects()

func implementedObjects() []string {
	names := make([]string, 0, len(objects))
	for _, o := range objects {
		if o.run != nil && !o.alias && !o.noShow {
			names = append(names, o.name)
		}
	}
	slices.Sort(names)
	return names
}

// ShowObjects returns the objects Show accepts, sorted. The slice is a copy.
func ShowObjects() []string {
	return slices.Clone(showObjects)
}

// Argv is the `ip` command line equivalent to r, minus the program name, with
// the options in the order Run documents them. Run(r.Argv(), …) and Show(r)
// produce the same output; TestShow holds them to it.
func (r ShowRequest) Argv() []string {
	argv := make([]string, 0, 6+len(r.Selectors))
	switch r.Family {
	case unix.AF_INET:
		argv = append(argv, "-4")
	case unix.AF_INET6:
		argv = append(argv, "-6")
	case unix.AF_PACKET:
		argv = append(argv, "-0")
	}
	if r.JSON {
		argv = append(argv, "-j")
	}
	if r.Stats {
		argv = append(argv, "-s")
	}
	if r.Details {
		argv = append(argv, "-d")
	}
	argv = append(argv, r.Object, showVerbOf(r.Object))
	return append(argv, r.Selectors...)
}

// showVerbOf is the verb Show runs for the object named name: its
// showVerb, or show.
func showVerbOf(name string) string {
	for i := range objects {
		if objects[i].name == name && objects[i].showVerb != "" {
			return objects[i].showVerb
		}
	}
	return showVerbCst
}

// Show runs r against src and writes what `ip` would print to out.
//
// The output is written as the object renders it, so a caller that must not
// send half a document on error (an HTTP handler) buffers out itself.
func Show(src Source, r ShowRequest, out io.Writer) error {
	if !slices.Contains(showObjects, r.Object) {
		return fmt.Errorf("%q: %w", r.Object, ErrUnknownObject)
	}
	switch r.Family {
	case unix.AF_UNSPEC, unix.AF_INET, unix.AF_INET6, unix.AF_PACKET:
	default:
		return fmt.Errorf("%w: family %d", ErrInvalidArgument, r.Family)
	}
	obj, err := lookupObject(r.Object)
	if err != nil {
		return err
	}

	c := newRunCtx(nil, out, io.Discard)
	c.src = src
	c.family = r.Family
	c.json = r.JSON
	if r.Stats {
		c.showStats = 1
	}
	if r.Details {
		c.showDetails = 1
	}

	args := append([]string{showVerbOf(r.Object)}, r.Selectors...)
	if err := obj.run(c, args); err != nil {
		if errors.Is(err, unix.ENODEV) && !errors.Is(err, ErrNoSuchDevice) {
			return noSuchDevice(err)
		}
		return err
	}
	return nil
}

// newRunCtx is the part of a runCtx that Run and Show build identically. The
// source, family and options are the caller's. Show has no stdin, so passes
// nil, which the showdump and restore verbs read and refuse.
func newRunCtx(stdin io.Reader, stdout, stderr io.Writer) *runCtx {
	return &runCtx{
		in:     stdin,
		out:    stdout,
		errOut: stderr,
		family: unix.AF_UNSPEC,
		lltab:  NewLLTab(),
		now:    nowFromEnv(),
	}
}
