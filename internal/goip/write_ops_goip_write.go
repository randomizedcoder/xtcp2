//go:build goip_write

package goip

// init fills writeOps with the write arms: cmd/goip and goip-parity build
// with goip_write, the daemons without it.
func init() {
	writeOps = writeTable{
		link:    chainOf(lookupLinkWriteVerb, linkWrite),
		addr:    chainOf(lookupAddrWriteVerb, addrWrite),
		route:   chainOf(lookupRouteWriteVerb, routeWrite),
		neigh:   chainOf(lookupNeighWriteVerb, neighModify),
		nexthop: chainOf(lookupNexthopWriteVerb, nexthopModify),

		linkProperty:   linkProperty,
		addrFlush:      addrFlush,
		addrRestore:    addrRestore,
		addrLabelFlush: addrLabelFlush,
		neighFlush:     neighFlush,
		netnsSet:       netnsSet,
		nexthopFlush:   nexthopFlush,
		ntableChange:   ntableChange,
		routeFlush:     routeFlush,
		routeRestore:   routeRestore,
		ruleRestore:    ruleRestore,
		ruleFlush:      ruleFlush,
		statsSet:       statsSet,

		addrLabelModify:  addrLabelModify,
		ruleModify:       ruleModify,
		xfrmStateModify:  xfrmStateModify,
		xfrmPolicyModify: xfrmPolicyModify,

		fouModify:     fouModify,
		ilaModify:     ilaModify,
		l2tpAdd:       l2tpAdd,
		l2tpDel:       l2tpDel,
		macsecModify:  macsecModify,
		macsecOffload: macsecOffload,
		ioamWrite:     ioamWrite,
		srWrite:       srWrite,
		tcpmDelete:    tcpmDelete,
		tcpmFlush:     tcpmFlush,
		mptcpModify:   (*mptcpRun).modify,
		mptcpWrite:    (*mptcpRun).write,
		tokenSet:      tokenSet,

		xfrmStateAllocspi:    xfrmStateAllocspi,
		xfrmStateDelete:      xfrmStateDelete,
		xfrmStateDeleteall:   xfrmStateDeleteall,
		xfrmStateFlush:       xfrmStateFlush,
		xfrmPolicyDelete:     xfrmPolicyDelete,
		xfrmPolicyDeleteall:  xfrmPolicyDeleteallRun,
		xfrmPolicyFlush:      xfrmPolicyFlush,
		xfrmPolicySet:        xfrmPolicySet,
		xfrmPolicySetDefault: xfrmPolicySetDefault,
	}
}

// chainOf is a chain arm from its lookup and its write: the verb the lookup
// finds, run, or not this arm's.
func chainOf[W any](lookup func(string) *W, run func(*runCtx, *W, []string) error) chainWrite {
	return func(c *runCtx, name string, args []string) (bool, error) {
		w := lookup(name)
		if w == nil {
			return false, nil
		}
		return true, run(c, w, args)
	}
}
