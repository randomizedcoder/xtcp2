package goip

import "fmt"

// verbKind is what `ip` does with a verb, as far as goip's callers care:
// list, change kernel state, or something else.
type verbKind uint8

const (
	// verbShow is the listing: show, list, lst. It is the only kind Show
	// serves.
	verbShow verbKind = iota + 1
	// verbWrite changes kernel state: add, set, delete, flush, restore, ...
	verbWrite
	// verbOther reads but is not the listing: get, save, showdump, help,
	// xstats, ... goip refuses or handles these per object.
	verbOther
)

// verb is one keyword of an object's verb chain.
type verb struct {
	name string
	kind verbKind
	// exact marks the verbs iproute2 compares with strcmp rather than
	// matches(), such as `chg`; they never abbreviate.
	exact bool
}

// matches is iproute2's test for this verb: strcmp when exact, otherwise
// matches(), which goip implements as matchesPrefix.
func (v *verb) matches(tok string) bool {
	if v.exact {
		return tok == v.name
	}
	return matchesPrefix(tok, v.name)
}

// resolveVerb is an object's do_ip* dispatch: the first verb in chain that
// tok matches, in the chain's order. The order is iproute2's and it decides
// the result — `ip link s` is `set` because do_iplink tests set before show —
// so a chain must never be sorted. ok is false when no verb matches, which
// iproute2 reports as an unknown command; goip's objects report it as
// ErrNotImplemented.
func resolveVerb(chain []verb, tok string) (v verb, ok bool) {
	for i := range chain {
		if chain[i].matches(tok) {
			return chain[i], true
		}
	}
	return verb{}, false
}

// showArgs resolves an object's verb for the listing: it returns the
// selectors after a show verb, or after nothing when args is empty, as every
// object lists when given no verb. Any other verb, or a token no verb
// matches, is ErrNotImplemented naming the resolved verb or the token.
func showArgs(object string, chain []verb, args []string) ([]string, error) {
	if len(args) == 0 {
		return args, nil
	}
	v, ok := resolveVerb(chain, args[0])
	switch {
	case !ok:
		return nil, fmt.Errorf("%s %q: %w", object, args[0], ErrNotImplemented)
	case v.kind != verbShow:
		return nil, fmt.Errorf("%s %q: %w", object, v.name, ErrNotImplemented)
	}
	return args[1:], nil
}

// helpVerbCst and chgVerbCst are verb words many chains share. The other
// shared words reuse address's constants (addr_write.go), which name the same
// words.
const (
	helpVerbCst = "help"
	chgVerbCst  = "chg"
)

// Chain segments shared by many objects.
var (
	showLstList  = []verb{{name: showVerbCst, kind: verbShow}, {name: lstVerbCst, kind: verbShow}, {name: listVerbCst, kind: verbShow}}
	listShowLst  = []verb{{name: listVerbCst, kind: verbShow}, {name: showVerbCst, kind: verbShow}, {name: lstVerbCst, kind: verbShow}}
	listLstShow  = []verb{{name: listVerbCst, kind: verbShow}, {name: lstVerbCst, kind: verbShow}, {name: showVerbCst, kind: verbShow}}
	helpVerb     = verb{name: helpVerbCst, kind: verbOther}
	getVerb      = verb{name: "get", kind: verbOther}
	flushVerb    = verb{name: "flush", kind: verbWrite}
	saveVerb     = verb{name: "save", kind: verbOther}
	restoreVerb  = verb{name: "restore", kind: verbWrite}
	showdumpVerb = verb{name: "showdump", kind: verbOther}
	addVerb      = verb{name: addrVerbAddCst, kind: verbWrite}
	changeVerb   = verb{name: addrVerbChangeCst, kind: verbWrite}
	chgVerb      = verb{name: chgVerbCst, kind: verbWrite, exact: true}
	replaceVerb  = verb{name: addrVerbReplaceCst, kind: verbWrite}
	deleteVerb   = verb{name: addrVerbDeleteCst, kind: verbWrite}
)

// chain concatenates verbs and segments into one chain, in argument order.
func chain(parts ...[]verb) []verb {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]verb, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// writeSegment turns a write table's names into chain entries, so the
// chains of link, address and route take their write verbs from the tables
// the write paths run from instead of a copy.
func writeSegment[T any](table []T, name func(*T) (string, bool)) []verb {
	out := make([]verb, len(table))
	for i := range table {
		n, exact := name(&table[i])
		out[i] = verb{name: n, kind: verbWrite, exact: exact}
	}
	return out
}

// The verb chains, one per implemented object, each in its do_ip* function's
// order in iproute2 7.1.0.
var (
	// linkVerbs is do_iplink (ip/iplink.c:1515-1546).
	linkVerbs = chain(
		writeSegment(linkWriteVerbs, func(v *linkWriteVerb) (string, bool) { return v.name, false }),
		showLstList,
		[]verb{{name: "xstats", kind: verbOther}, {name: "afstats", kind: verbOther}, {name: "property", kind: verbWrite}, helpVerb},
	)
	// addressVerbs is do_ipaddr (ip/ipaddress.c:2692-2712).
	addressVerbs = chain(
		writeSegment(addrWriteVerbs, func(v *addrWriteVerb) (string, bool) { return v.name, v.exact }),
		listShowLst,
		[]verb{flushVerb, saveVerb, showdumpVerb, restoreVerb, helpVerb},
	)
	// routeVerbs is do_iproute (ip/iproute.c:2422-2456).
	routeVerbs = chain(
		writeSegment(routeWriteVerbs, func(v *routeWriteVerb) (string, bool) { return v.name, v.exact }),
		listShowLst,
		[]verb{getVerb, flushVerb, saveVerb, restoreVerb, showdumpVerb, helpVerb},
	)
	// neighVerbs is do_ipneigh (ip/ipneigh.c:747-764).
	neighVerbs = chain(
		[]verb{addVerb, changeVerb, chgVerb, replaceVerb, deleteVerb, getVerb},
		showLstList,
		[]verb{flushVerb, helpVerb},
	)
	// ruleVerbs is do_iprule (ip/iprule.c:1240-1254). Its order decides
	// the abbreviations:
	//
	//	l    -> list, because no earlier verb starts with 'l'
	//	s    -> show, because "save" comes after the list group
	//	sa   -> save, which is NOT show, since "sa" is not a prefix of "show"
	//	ls   -> lst, the only verb "ls" matches
	//
	// rule tests "lst" second and "show" third, where route tests "show"
	// second. Neither order changes what those four resolve to, so testing
	// abbreviations alone would not catch a wrongly transcribed order;
	// TestVerbChains compares every chain with its transcription instead.
	ruleVerbs = chain(
		listLstShow,
		[]verb{saveVerb, restoreVerb, addVerb, deleteVerb, flushVerb, helpVerb},
	)
	// ntableVerbs is do_ipntable (ip/ipntable.c:688-697). Unlike address,
	// route and neigh, ntable matches `chg` with matches(), not strcmp.
	ntableVerbs = chain(
		[]verb{changeVerb, {name: chgVerbCst, kind: verbWrite}},
		showLstList,
		[]verb{helpVerb},
	)
	// netconfVerbs is do_ipnetconf (ip/ipnetconf.c:236-240).
	netconfVerbs = chain(showLstList, []verb{helpVerb})
	// nexthopVerbs is do_ipnh (ip/ipnexthop.c:1442-1465).
	nexthopVerbs = chain(
		[]verb{addVerb, replaceVerb, deleteVerb},
		listShowLst,
		[]verb{getVerb, flushVerb, {name: "bucket", kind: verbOther}, helpVerb},
	)
	// statsVerbs is do_ipstats (ip/ipstats.c:1321-1330): strcmp throughout,
	// so no stats verb abbreviates.
	statsVerbs = []verb{
		{name: helpVerbCst, kind: verbOther, exact: true},
		{name: showVerbCst, kind: verbShow, exact: true},
		{name: "set", kind: verbWrite, exact: true},
	}
	// addrLabelVerbs is do_ipaddrlabel (ip/ipaddrlabel.c:248-258).
	addrLabelVerbs = chain(listLstShow, []verb{addVerb, deleteVerb, flushVerb, helpVerb})
	// vrfVerbs is do_ipvrf (ip/ipvrf.c:633-647). identify and pids read
	// /proc and exec runs a command; none is a netlink query.
	vrfVerbs = chain(
		[]verb{{name: "identify", kind: verbOther}, {name: "pids", kind: verbOther}, {name: "exec", kind: verbWrite}},
		showLstList,
		[]verb{helpVerb},
	)
)
