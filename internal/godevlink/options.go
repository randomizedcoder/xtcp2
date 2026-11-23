package godevlink

import "strings"

// parseOptions is devlink.c main's getopt_long loop over "Vfb:njpvsN:ix" with
// GNU permutation: options may follow the object, and "--" ends them. It
// leaves the operands in r.args and returns done when main would return before
// dl_init (here, only -V).
//
// The read paths this clone covers depend on -j (JSON) and -p (pretty); the
// other flags are accepted where devlink accepts them, and the ones that take
// an argument but drive a path with no capture (-b batch, -N netns) are
// refused.
func (r *runner) parseOptions(args []string) (rc int, done bool) {
	var operands []string
	opt := func(c byte) (int, bool) {
		switch c {
		case 'V':
			r.d.raw(Version + "\n")
			return 0, true
		case 'j':
			r.d.json = true
		case 'p':
			r.d.pretty = true
		case 'f', 'n', 'v', 's', 'i', 'x':
			// Accepted; no effect on the captured read paths.
		case 'b':
			r.errf("godevlink: -b (batch) is not implemented\n")
			return 1, true
		case 'N':
			r.errf("godevlink: -N (netns) is not implemented\n")
			return 1, true
		default:
			r.errf("devlink: unknown option.\n")
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
			c, takesNext := longOpt(a[2:])
			if takesNext {
				i++ // consume the option's argument (unused: only -b/-N take one)
			}
			if rc, done := opt(c); done {
				return rc, true
			}
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				c := a[j]
				if c == 'b' || c == 'N' {
					// Takes an argument: rest of the cluster or the next word.
					if j+1 == len(a) {
						i++
					}
					j = len(a)
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

// longOpt maps a devlink long option to its short character and whether it
// takes the next word. An unknown one is '?'.
func longOpt(name string) (byte, bool) {
	name, _, _ = strings.Cut(name, "=")
	switch {
	case match(name, "Version"):
		return 'V', false
	case match(name, "force"):
		return 'f', false
	case match(name, "batch"):
		return 'b', true
	case match(name, "no-nice-names"):
		return 'n', false
	case match(name, "json"):
		return 'j', false
	case match(name, "pretty"):
		return 'p', false
	case match(name, "verbose"):
		return 'v', false
	case match(name, "statistics"):
		return 's', false
	case match(name, "Netns"):
		return 'N', true
	case match(name, "iec"):
		return 'i', false
	case match(name, "hex"):
		return 'x', false
	}
	return '?', false
}

func match(arg, full string) bool {
	return arg != "" && len(arg) <= len(full) && full[:len(arg)] == arg
}
