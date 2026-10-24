package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip token` is do_iptoken (ip/iptoken.c:191-216): ll_init_map's link dump
// first, before the verb is read, then the listing, an RTM_GETLINK dump of
// AF_INET6 that print_token filters, or the write, one RTM_SETLINK of an
// AF_INET6 ifinfomsg carrying IFLA_AF_SPEC { AF_INET6 { IFLA_INET6_TOKEN } }.
// Both read `dev` from the cache ll_init_map filled, and preferred_family
// reaches neither: the family is always AF_INET6. goip writes the request to
// GOIP_WRITE_OUT.

const (
	iftokSetVerbCst    = "set"
	iftokAddVerbCst    = "add"
	iftokDeleteVerbCst = "delete"
	iftokGetVerbCst    = "get"
	iftokHelpCst       = "help"
	// iftokNoSupportCst is print_token's complaint about a reply with
	// IFLA_PROTINFO but no IFLA_INET6_TOKEN, which ends the dump.
	iftokNoSupportCst  = "Seems there's no support for IPv6 token!"
	iftokDevInvalidCst = "dev is invalid"
	// tokenBufCst is iptoken_set's request buffer after the ifinfomsg.
	tokenBufCst = 512
)

// tokenListVerbs are do_iptoken's listing verbs, matched with matches() in
// its order, ahead of set, so `s` is show.
var tokenListVerbs = []string{listVerbCst, lstVerbCst, showVerbCst}

// runToken is do_iptoken. No verb is the listing; `get` is too.
func runToken(c *runCtx, args []string) error {
	svc := service.New(c.src, c.nextSeq)
	if err := tokenInitMap(c, svc); err != nil {
		return err
	}
	if len(args) == 0 {
		return tokenList(c, svc, nil)
	}
	switch v := args[0]; {
	case matchesAny(v, tokenListVerbs):
		return tokenList(c, svc, args[1:])
	case matchesPrefix(v, iftokSetVerbCst), matchesPrefix(v, iftokAddVerbCst):
		return tokenSet(c, args[1:], false)
	case matchesPrefix(v, iftokDeleteVerbCst):
		return tokenSet(c, args[1:], true)
	case matchesPrefix(v, iftokGetVerbCst):
		return tokenList(c, svc, args[1:])
	case matchesPrefix(v, iftokHelpCst):
		return fmt.Errorf("token help: %w", ErrNotImplemented)
	default:
		return fmt.Errorf("%w: Command %q is unknown, try \"ip token help\"", ErrInvalidArgument, v)
	}
}

// tokenInitMap is ll_init_map, whose failure is "Dump terminated", exit 1.
func tokenInitMap(c *runCtx, svc *service.Service) error {
	links, err := svc.NeighTableLinks()
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
	}
	decoded := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decoded[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decoded)
	return nil
}

// tokenDev is ll_name_to_index on a `dev` value, from the cache alone; a
// miss is invarg's "dev is invalid".
func tokenDev(c *runCtx, name string) (int32, error) {
	idx := c.lltab.NameToIndex(name)
	if idx == 0 {
		return 0, invargErr(iftokDevInvalidCst, name)
	}
	return idx, nil
}

// tokenList is iptoken_list: the loop reads `dev` by strcmp and stops after
// it, skipping every other word; then the dump, filtered as print_token
// filters it. A reply with no IFLA_PROTINFO or no token ends the dump after
// the lines before it, "Dump terminated", exit 1.
func tokenList(c *runCtx, svc *service.Service, args []string) error {
	var ifindex int32
	for i := 0; i < len(args); i++ {
		if args[i] != devKeywordCst {
			continue
		}
		var err error
		if i, err = nextArg(args, i); err != nil {
			return err
		}
		if ifindex, err = tokenDev(c, args[i]); err != nil {
			return err
		}
		break
	}
	tokens, err := svc.LinkTokens()
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
	}
	views := make([]render.TokenView, 0, len(tokens))
	var dumpErr error
	for i := range tokens {
		lt := xtcpnl.LinkToken(tokens[i])
		if !tokenPrinted(&lt, ifindex) {
			continue
		}
		if dumpErr = tokenRenderable(c, &lt); dumpErr != nil {
			break
		}
		views = append(views, render.TokenViewOf(&lt, c.lltab))
	}
	if err := tokenRender(c, views); err != nil {
		return err
	}
	return dumpErr
}

// tokenPrinted is print_token's skips: not AF_INET6, ifindex 0, another
// device than `dev`'s, or a loopback or NOARP device.
func tokenPrinted(lt *xtcpnl.LinkToken, ifindex int32) bool {
	switch {
	case lt.Family != unix.AF_INET6, lt.Index == 0,
		ifindex > 0 && lt.Index != ifindex,
		lt.Flags&(unix.IFF_LOOPBACK|unix.IFF_NOARP) != 0:
		return false
	}
	return true
}

// tokenRenderable is print_token's two refusals, and goip's of a token
// that is not 16 bytes, which inet_ntop would read past.
func tokenRenderable(c *runCtx, lt *xtcpnl.LinkToken) error {
	switch {
	case !lt.HasProtinfo:
		return fmt.Errorf("%w: Dump terminated", ErrDumpTerminated)
	case lt.Token == nil:
		fmt.Fprintln(c.errOut, iftokNoSupportCst)
		return fmt.Errorf("%w: Dump terminated", ErrDumpTerminated)
	case len(lt.Token) != 16:
		return fmt.Errorf("%w: Dump terminated: an IFLA_INET6_TOKEN of %d bytes", ErrDumpTerminated, len(lt.Token))
	}
	return nil
}

// tokenRender prints the lines, or one JSON array of them.
func tokenRender(c *runCtx, views []render.TokenView) error {
	if c.json {
		return json.NewEncoder(c.out).Encode(views)
	}
	for i := range views {
		if _, err := fmt.Fprint(c.out, views[i].Text()); err != nil {
			return err
		}
	}
	return nil
}

// tokenWrite is iptoken_set's request as its loop builds it.
type tokenWrite struct {
	token     []byte
	haveToken bool
	dev       int32
	haveDev   bool
}

// tokenSet is iptoken_set: the loop, the two "Not enough information"
// checks, then the RTM_SETLINK, NLM_F_ACK from rtnl_talk. delete starts with
// the token present and all zero, `::`, so no word is read as one.
func tokenSet(c *runCtx, args []string, del bool) error {
	w := tokenWrite{token: make([]byte, 16), haveToken: del}
	for i := 0; i < len(args); i++ {
		var err error
		if i, err = w.parseArg(c, args, i); err != nil {
			return err
		}
	}
	if !w.haveToken {
		return fmt.Errorf("%w: Not enough information: token is required", ErrInvalidArgument)
	}
	if !w.haveDev {
		return fmt.Errorf("%w: Not enough information: \"dev\" argument is required", ErrInvalidArgument)
	}
	var raw [tokenBufCst]byte
	attrs := xtcpnl.NewAttrBuilder(raw[:])
	afs, err := attrs.BeginNest(unix.IFLA_AF_SPEC)
	if err != nil {
		return err
	}
	afs6, err := attrs.BeginNest(unix.AF_INET6)
	if err != nil {
		return err
	}
	if err = attrs.PutBytes(unix.IFLA_INET6_TOKEN, w.token); err != nil {
		return err
	}
	if err = attrs.EndNest(afs6); err != nil {
		return err
	}
	if err = attrs.EndNest(afs); err != nil {
		return err
	}
	b, err := xtcpnl.BuildWriteRequest(unix.RTM_SETLINK, unix.NLM_F_ACK, c.nextSeq(),
		linkPropHeader(unix.AF_INET6, w.dev), attrs.Bytes())
	if err != nil {
		return fmt.Errorf("goip: build token request: %w", err)
	}
	return writeAddrOut(b)
}

// parseArg is one turn of iptoken_set's loop: `dev` by strcmp, the first
// one resolved and any later one skipped unresolved; any other word is
// checked for `help` with matches(), then the first is the token,
// get_prefix's in AF_INET6, whose prefix length is not sent. It returns the
// index of the last argument it used.
func (w *tokenWrite) parseArg(c *runCtx, args []string, i int) (int, error) {
	var err error
	if args[i] == devKeywordCst {
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
		if !w.haveDev {
			if w.dev, err = tokenDev(c, args[i]); err != nil {
				return i, err
			}
			w.haveDev = true
		}
		return i, nil
	}
	if matchesPrefix(args[i], iftokHelpCst) {
		return i, fmt.Errorf("token help: %w", ErrNotImplemented)
	}
	if !w.haveToken {
		p, err := routeGetPrefix(args[i], unix.AF_INET6)
		if err != nil {
			return i, err
		}
		w.token, w.haveToken = p.data, true
	}
	return i, nil
}
