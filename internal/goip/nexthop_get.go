package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// do_ipnh's `get` and `bucket` (ip/ipnexthop.c:1470-1477), matched with
// matches() after the write and listing verbs. No prefix of either is a
// prefix of add, replace, delete, list, show or lst, so `g` is get and `b`
// is bucket.
const (
	nexthopGetVerbCst    = "get"
	nexthopBucketVerbCst = "bucket"
	nhIDKeywordCst       = "id"
	nhNhIDKeywordCst     = "nhid"
	nhIndexKeywordCst    = "index"
)

// nexthopUsageErr is ipnexthop.c's usage(), which prints the help and exits.
func nexthopUsageErr() error {
	return fmt.Errorf("%w: ip nexthop help is not implemented by goip", ErrNotImplemented)
}

// parseNhID is ipnh_parse_id (ip/ipnexthop.c:1045-1052): get_unsigned in base
// 0, so `0x14` is 20 and `-1` is refused.
func parseNhID(v string) (uint32, error) {
	id, ok := getU32(v, 0)
	if !ok {
		return 0, invargErr("invalid id value", v)
	}
	return id, nil
}

// parseNexthopGetArgs is ipnh_get's loop (ip/ipnexthop.c:1284-1304): `id` is
// strcmp and anything else is usage, as is no id or id 0.
func parseNexthopGetArgs(args []string) (uint32, error) {
	var id uint32
	for i := 0; i < len(args); i++ {
		if args[i] != nhIDKeywordCst {
			return 0, nexthopUsageErr()
		}
		var err error
		if i, err = nextArg(args, i); err != nil {
			return 0, err
		}
		if id, err = parseNhID(args[i]); err != nil {
			return 0, err
		}
	}
	if id == 0 {
		return 0, nexthopUsageErr()
	}
	return id, nil
}

// nexthopGet is `ip nexthop get id ID` (ipnh_get_id, ip/ipnexthop.c:1177):
// the RTM_GETNEXTHOP single get and print_nexthop_nocache, the path `ip
// nexthop show id ID` takes too (ipnh_list_flush_id, :1203-1204).
func nexthopGet(c *runCtx, args []string) error {
	id, err := parseNexthopGetArgs(args)
	if err != nil {
		return err
	}
	return nexthopShowID(c, id)
}

// runNexthopBucket is do_ipnh_bucket (ip/ipnexthop.c:1430-1449): no verb is
// the listing, then list/show/lst and get by matches(), help is usage, and
// anything else is unknown.
func runNexthopBucket(c *runCtx, args []string) error {
	if len(args) == 0 {
		return nexthopBucketList(c, nil)
	}
	switch verb, rest := args[0], args[1:]; {
	case matchesAny(verb, nexthopShowVerbs):
		return nexthopBucketList(c, rest)
	case matchesPrefix(verb, nexthopGetVerbCst):
		return nexthopBucketGet(c, rest)
	case matchesPrefix(verb, routeHelpKeywordCst):
		return nexthopUsageErr()
	default:
		return fmt.Errorf("%w: Command %q is unknown, try \"ip nexthop help\"", ErrInvalidArgument, verb)
	}
}

// parseNexthopBucketListArgs is ipnh_bucket_list's loop (ip/ipnexthop.c:
// 1306-1338). `id` and `nhid` are strcmp and fill the dump filter; a repeat
// overwrites. `dev`, `master` and `vrf` resolve a name before the dump, and no
// capture grounds that traffic, so they are refused by name rather than read
// wrongly. Anything else is invarg with an empty message.
func parseNexthopBucketListArgs(args []string) (xtcpnl.NexthopBucketDumpFilter, error) {
	var f xtcpnl.NexthopBucketDumpFilter
	for i := 0; i < len(args); i++ {
		var err error
		switch {
		case matchesPrefix(args[i], devKeywordCst), matchesPrefix(args[i], "master"),
			matchesPrefix(args[i], vrfKeywordCst):
			return f, fmt.Errorf("nexthop bucket show %q: %w", args[i], ErrNotImplemented)
		case args[i] == nhIDKeywordCst, args[i] == nhNhIDKeywordCst:
			key := args[i]
			if i, err = nextArg(args, i); err != nil {
				return f, err
			}
			var id uint32
			if id, err = parseNhID(args[i]); err != nil {
				return f, err
			}
			if key == nhIDKeywordCst {
				f.ID = id
			} else {
				f.NhID = id
			}
		case matchesPrefix(args[i], routeHelpKeywordCst):
			return f, nexthopUsageErr()
		default:
			return f, invargErr("", args[i])
		}
	}
	return f, nil
}

// nexthopBucketList is `ip nexthop bucket show`: the RTM_GETNEXTHOPBUCKET dump
// and print_nexthop_bucket per reply, in the kernel's order. A bucket line
// names no device, so nothing follows the dump.
func nexthopBucketList(c *runCtx, args []string) error {
	f, err := parseNexthopBucketListArgs(args)
	if err != nil {
		return err
	}
	svc := service.New(c.src, c.nextSeq)
	bs, err := svc.NexthopBuckets(c.family, f)
	if err != nil {
		return err
	}
	views := make([]render.NexthopBucketView, len(bs))
	for i := range bs {
		views[i] = render.NexthopBucketViewOf(xtcpnl.NexthopBucketInfo(bs[i]))
	}
	return writeNexthopBuckets(c, views)
}

// nexthopBucketGetArgs is ipnh_bucket_get's result: the group and the index,
// both required.
type nexthopBucketGetArgs struct {
	id    uint32
	index uint16
}

// parseNexthopBucketGetArgs is ipnh_bucket_get's loop (ip/ipnexthop.c:
// 1401-1428): `id` and `index` are strcmp, the index get_u16 in base 0, and
// anything else is usage, as is a missing id or index. Index 0 is a real
// index; id 0 is no id.
func parseNexthopBucketGetArgs(args []string) (nexthopBucketGetArgs, error) {
	var (
		a        nexthopBucketGetArgs
		indexSet bool
	)
	for i := 0; i < len(args); i++ {
		var err error
		switch args[i] {
		case nhIDKeywordCst:
			if i, err = nextArg(args, i); err != nil {
				return a, err
			}
			if a.id, err = parseNhID(args[i]); err != nil {
				return a, err
			}
		case nhIndexKeywordCst:
			if i, err = nextArg(args, i); err != nil {
				return a, err
			}
			v, ok := getU16(args[i], 0)
			if !ok {
				return a, invargErr("invalid bucket index value", args[i])
			}
			a.index, indexSet = v, true
		default:
			return a, nexthopUsageErr()
		}
	}
	if a.id == 0 || !indexSet {
		return a, nexthopUsageErr()
	}
	return a, nil
}

// nexthopBucketGet is `ip nexthop bucket get id ID index INDEX`
// (ipnh_bucket_get_id, ip/ipnexthop.c:1359-1399): one RTM_GETNEXTHOPBUCKET
// single get and print_nexthop_bucket over its reply. A missing group or
// index is the kernel's refusal: exit 2, nothing on stdout.
func nexthopBucketGet(c *runCtx, args []string) error {
	a, err := parseNexthopBucketGetArgs(args)
	if err != nil {
		return err
	}
	svc := service.New(c.src, c.nextSeq)
	nb, err := svc.NexthopBucketGet(c.family, a.id, a.index)
	if err != nil {
		return err
	}
	return writeNexthopBuckets(c, []render.NexthopBucketView{
		render.NexthopBucketViewOf(xtcpnl.NexthopBucketInfo(nb)),
	})
}

// writeNexthopBuckets prints bucket lines, or under -j the array new_json_obj
// opens around them, for the get's one object too.
func writeNexthopBuckets(c *runCtx, views []render.NexthopBucketView) error {
	if c.json {
		return json.NewEncoder(c.out).Encode(views)
	}
	for _, v := range views {
		if _, err := fmt.Fprint(c.out, v.Text()); err != nil {
			return err
		}
	}
	return nil
}
