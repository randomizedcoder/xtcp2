package goip

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// linkResolver is ll_name_to_index: the ifindex for a name, 0 for none.
type linkResolver func(name string) (int32, error)

// newLinkNameResolverRemember is newLinkNameResolver that also hands each
// link it fetches to remember. ll_link_get stores its reply in the one ll_map
// that ll_index_to_name reads later (lib/ll_map.c:296), so a command that
// renders the names its lookups resolved, `ip addr show master bond0`, must
// not fetch them a second time by index. remember may be nil.
func newLinkNameResolverRemember(svc *service.Service, remember func(xtcpnl.LinkInfo)) linkResolver {
	cache := map[string]int32{}
	return func(name string) (int32, error) {
		if idx, ok := cache[name]; ok {
			return idx, nil
		}
		if !checkIfname(name) {
			// ll_link_get would send IFLA_ALT_IFNAME; req.LinkShowByName
			// has no captured example to build it from.
			return 0, fmt.Errorf("resolving %q by IFLA_ALT_IFNAME: %w", name, ErrNotImplemented)
		}
		l, err := svc.LinkByName(name)
		var errno syscall.Errno
		switch {
		case err == nil:
			if remember != nil {
				remember(xtcpnl.LinkInfo(l))
			}
			cache[l.Name] = l.Index
			for _, alt := range l.AltNames {
				cache[alt] = l.Index
			}
			return l.Index, nil
		case errors.As(err, &errno):
			return cint.Int(llIdxA2N(name)), nil
		default:
			return 0, err
		}
	}
}
