package goip

import "fmt"

// ErrWriteNotCompiled is a write verb in a build without the goip_write
// tag: xtcp2's and go-link-monitor's goip, which carries no write path at
// all. It is an ErrWriteVerb, so /ip answers it as it answers any write verb.
var ErrWriteNotCompiled = fmt.Errorf("%w (built without goip_write)", ErrWriteVerb)

// The write arms, called through writeOps so that a build without the
// goip_write tag links none of them. Untagged, every field is a stub that
// returns ErrWriteNotCompiled; write_ops_goip_write.go swaps in the real
// arms in its init.
//
// A table that runs an arm (xfrmStateWrites, xfrmPolicyVerbs) calls
// writeOps inside a closure: package variables are initialized before any
// init runs, so a table holding writeOps.x itself would keep the stub.
type writeTable struct {
	// The chain arms: ok reports whether verb, the chain's name for the
	// argument, is one of the arm's write verbs.
	link, addr, route, neigh, nexthop chainWrite

	linkProperty, addrFlush, addrRestore, addrLabelFlush, neighFlush,
	netnsSet, nexthopFlush, ntableChange, routeFlush, routeRestore,
	ruleRestore, ruleFlush, statsSet argsWrite

	addrLabelModify, ruleModify, xfrmStateModify, xfrmPolicyModify func(c *runCtx, msgType uint16, args []string) error

	fouModify, ilaModify func(c *runCtx, id uint16, cmd uint8, args []string) error
	l2tpAdd, l2tpDel     func(c *runCtx, id uint16, args []string) error
	macsecModify         func(c *runCtx, id uint16, v *macsecVerb, args []string) error
	macsecOffload        func(c *runCtx, id uint16, args []string) error
	ioamWrite            func(c *runCtx, id uint16, r *ioamReq) error
	srWrite              func(c *runCtx, id uint16, r *srReq) error
	tcpmDelete           func(c *runCtx, id uint16, f *tcpmFilter) error
	tcpmFlush            func(c *runCtx, id uint16, f *tcpmFilter) error
	mptcpModify          func(m *mptcpRun, cmd uint8, args []string) error
	mptcpWrite           func(m *mptcpRun, cmd uint8, attrs []byte) error
	tokenSet             func(c *runCtx, args []string, del bool) error

	xfrmStateAllocspi, xfrmStateDelete, xfrmStateDeleteall, xfrmStateFlush,
	xfrmPolicyDelete, xfrmPolicyDeleteall, xfrmPolicyFlush, xfrmPolicySet,
	xfrmPolicySetDefault argsWrite
}

// chainWrite is a write arm reached through a verb chain.
type chainWrite func(c *runCtx, verb string, args []string) (ok bool, err error)

// argsWrite is a write arm that takes the arguments after its verb.
type argsWrite func(c *runCtx, args []string) error

var writeOps = untaggedWriteOps()

// notCompiled is the refusal of what, as "ip address flush: ...".
func notCompiled(what string) error {
	return fmt.Errorf("ip %s: %w", what, ErrWriteNotCompiled)
}

// notCompiledChain claims the verbs of segment, and only those, so the
// arm's other verbs (flush, restore, get) still reach their own cases.
func notCompiledChain(object string, segment []verb) chainWrite {
	return func(_ *runCtx, name string, _ []string) (bool, error) {
		for _, v := range segment {
			if v.name == name {
				return true, notCompiled(object + " " + name)
			}
		}
		return false, nil
	}
}

func notCompiledArgs(what string) argsWrite {
	return func(*runCtx, []string) error { return notCompiled(what) }
}

// untaggedWriteOps is writeOps without goip_write: every arm refuses.
func untaggedWriteOps() writeTable {
	msgTypeStub := func(what string) func(*runCtx, uint16, []string) error {
		return func(*runCtx, uint16, []string) error { return notCompiled(what) }
	}
	return writeTable{
		link:    notCompiledChain("link", linkWriteSegment),
		addr:    notCompiledChain("address", addrWriteSegment),
		route:   notCompiledChain("route", routeWriteSegment),
		neigh:   notCompiledChain("neigh", neighWriteSegment),
		nexthop: notCompiledChain("nexthop", nexthopWriteSegment),

		linkProperty:   notCompiledArgs("link property"),
		addrFlush:      notCompiledArgs("address flush"),
		addrRestore:    notCompiledArgs("address restore"),
		addrLabelFlush: notCompiledArgs("addrlabel flush"),
		neighFlush:     notCompiledArgs("neigh flush"),
		netnsSet:       notCompiledArgs("netns set"),
		nexthopFlush:   notCompiledArgs("nexthop flush"),
		ntableChange:   notCompiledArgs("ntable change"),
		routeFlush:     notCompiledArgs("route flush"),
		routeRestore:   notCompiledArgs("route restore"),
		ruleRestore:    notCompiledArgs("rule restore"),
		ruleFlush:      notCompiledArgs("rule flush"),
		statsSet:       notCompiledArgs("stats set"),

		addrLabelModify:  msgTypeStub("addrlabel"),
		ruleModify:       msgTypeStub("rule"),
		xfrmStateModify:  msgTypeStub("xfrm state"),
		xfrmPolicyModify: msgTypeStub("xfrm policy"),

		fouModify: func(*runCtx, uint16, uint8, []string) error { return notCompiled("fou") },
		ilaModify: func(*runCtx, uint16, uint8, []string) error { return notCompiled("ila") },
		l2tpAdd:   func(*runCtx, uint16, []string) error { return notCompiled("l2tp add") },
		l2tpDel:   func(*runCtx, uint16, []string) error { return notCompiled("l2tp delete") },
		macsecModify: func(*runCtx, uint16, *macsecVerb, []string) error {
			return notCompiled("macsec")
		},
		macsecOffload: func(*runCtx, uint16, []string) error { return notCompiled("macsec offload") },
		ioamWrite:     func(*runCtx, uint16, *ioamReq) error { return notCompiled("ioam") },
		srWrite:       func(*runCtx, uint16, *srReq) error { return notCompiled("sr") },
		tcpmDelete:    func(*runCtx, uint16, *tcpmFilter) error { return notCompiled("tcp_metrics delete") },
		tcpmFlush:     func(*runCtx, uint16, *tcpmFilter) error { return notCompiled("tcp_metrics flush") },
		mptcpModify:   func(*mptcpRun, uint8, []string) error { return notCompiled("mptcp") },
		mptcpWrite:    func(*mptcpRun, uint8, []byte) error { return notCompiled("mptcp") },
		tokenSet:      func(*runCtx, []string, bool) error { return notCompiled("token") },

		xfrmStateAllocspi:    notCompiledArgs("xfrm state allocspi"),
		xfrmStateDelete:      notCompiledArgs("xfrm state delete"),
		xfrmStateDeleteall:   notCompiledArgs("xfrm state deleteall"),
		xfrmStateFlush:       notCompiledArgs("xfrm state flush"),
		xfrmPolicyDelete:     notCompiledArgs("xfrm policy delete"),
		xfrmPolicyDeleteall:  notCompiledArgs("xfrm policy deleteall"),
		xfrmPolicyFlush:      notCompiledArgs("xfrm policy flush"),
		xfrmPolicySet:        notCompiledArgs("xfrm policy set"),
		xfrmPolicySetDefault: notCompiledArgs("xfrm policy setdefault"),
	}
}
