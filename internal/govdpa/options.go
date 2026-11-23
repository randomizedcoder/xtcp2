package govdpa

import "strings"

// parseOptions is vdpa.c main's getopt_long loop over "Vjpsh" with GNU
// permutation: options may follow the object, and "--" ends them. It leaves
// the operands in r.args and returns done when main returns before vdpa_init
// (-V, -h, or an option main rejects).
//
// 's' is in the optstring but main's switch has no case for it, so -s lands
// in the default branch like an unknown option, minus getopt's own message.
func (r *runner) parseOptions(args []string) (rc int, done bool) {
	var operands []string
	opt := func(c byte) (int, bool) {
		switch c {
		case 'V':
			r.o.raw(Version + "\n")
			return 0, true
		case 'j':
			r.o.json = true
		case 'p':
			r.o.pretty = true
		case 'h':
			r.help()
			return 0, true
		default:
			r.errf("Unknown option.\n")
			r.help()
			return 1, true
		}
		return 0, false
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			operands = append(operands, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "--"):
			c, ok := longOpt(a[2:])
			if !ok {
				r.errf("vdpa: unrecognized option '%s'\n", a)
			}
			if rc, done := opt(c); done {
				return rc, true
			}
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				c := a[j]
				if !strings.ContainsRune("Vjpsh", rune(c)) {
					r.errf("vdpa: invalid option -- '%c'\n", c)
				}
				if rc, done := opt(c); done {
					return rc, true
				}
			}
		default:
			operands = append(operands, a)
		}
	}
	r.args = operands
	return 0, false
}

// longOpt maps a vdpa long option, or an unambiguous prefix of one, to its
// short character. ok is false for one getopt_long does not recognize; an
// "=value" on a no_argument option is refused by getopt too, which this
// treats the same way.
func longOpt(arg string) (c byte, ok bool) {
	if arg == "" || strings.Contains(arg, "=") {
		return '?', false
	}
	for _, o := range []struct {
		name string
		c    byte
	}{{"Version", 'V'}, {"json", 'j'}, {"pretty", 'p'}, {"help", 'h'}} {
		if strings.HasPrefix(o.name, arg) {
			return o.c, true
		}
	}
	return '?', false
}
