package goip

// Keywords the read and write parsers share.
const (
	addrHelpKeywordCst  = "help"
	addrLocalKeywordCst = "local"
	addrScopeKeywordCst = "scope"

	// mplsKeywordCst is "mpls": read_family's word for AF_MPLS, and an encap
	// type.
	mplsKeywordCst = "mpls"

	// The route priority keywords, spelled the same in `ip address`
	// (ipaddr_modify) and `ip route` (iproute_modify).
	routeMetricKeywordCst     = "metric"
	routePriorityKeywordCst   = "priority"
	routePreferenceKeywordCst = "preference"

	// routeAsKeywordCst is RTA_NEWDST's word, on the route and in a hop.
	routeAsKeywordCst   = "as"
	routeToKeywordCst   = "to"
	routeHelpKeywordCst = "help"
)

// Link kinds the show paths name too.
const (
	kindBondCst   = "bond"
	kindVethCst   = "veth"
	kindVxcanCst  = "vxcan"
	kindNetkitCst = "netkit"
)

// Keywords the tunnel read path (obj_tunnel.go, obj_tunnel6.go) shares
// with the link kinds' write parsers.
const (
	kindKeyTTL      = "ttl"
	kindKeyHoplimit = "hoplimit"
	tnlKeyKey       = "key"
	tnlKeyIkey      = "ikey"
	tnlKeyOkey      = "okey"
	tnlKeyHlim      = "hlim"
	kindKeyRemote   = "remote"
	gre6Flowlabel   = "flowlabel"
	gre6Dscp        = "dscp"
	kindVti6Cst     = "vti6"
	kindIP6GreCst   = "ip6gre"
	lwtKeyCsum      = "csum"
)

// groupKeywordCst is the `group` keyword, which vxlan and nexthop share
// with `ip stats show`'s first level (ipstatsLevels) and ip tuntap's
// group property.
const groupKeywordCst = "group"
