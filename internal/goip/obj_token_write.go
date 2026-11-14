//go:build goip_write

package goip

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tokenBufCst is iptoken_set's request buffer after the ifinfomsg.
const tokenBufCst = 512

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
