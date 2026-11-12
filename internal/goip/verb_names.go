package goip

// The write verbs' names. The verb chains and the read paths' refusals
// name them too, so they stay in a build without the write paths.
const (
	// The names below are spelled once here: route_write.go and
	// link_write.go use the same words, and goconst counts across the
	// package.
	addrVerbAddCst     = "add"
	addrVerbChangeCst  = "change"
	addrVerbReplaceCst = "replace"
	addrVerbDeleteCst  = "delete"
	addrVerbChgCst     = "chg"

	addrLabelAddVerbCst   = "add"
	addrLabelDelVerbCst   = "delete"
	addrLabelFlushVerbCst = "flush"

	linkPropertyVerbCst = "property"

	ntableChangeVerbCst = "change"
	ntableChgVerbCst    = "chg"

	routeFlushVerbCst = "flush"

	ruleAddVerbCst    = "add"
	ruleDeleteVerbCst = "delete"

	statsSetVerbCst = "set"

	xfrmAddCst       = "add"
	xfrmUpdateCst    = "update"
	xfrmAllocspiCst  = "allocspi"
	xfrmDeleteallCst = "deleteall"
	xfrmFlushCst     = "flush"
)
