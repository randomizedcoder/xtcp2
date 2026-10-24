package goip

import (
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip mrule` is do_multirule (ip/iprule.c:1262-1283): it swaps
// preferred_family for its multicast routing family, RTNL_FAMILY_IPMR for
// none or -4 and RTNL_FAMILY_IP6MR for -6, and hands the arguments to
// do_iprule. So `ip mrule show` is `ip rule show`'s one RTM_GETRULE dump with
// family 128 or 129, and print_rule's render. Nothing in that render depends
// on the family for an ipmr rule: ipmr_rule_fill (net/ipv4/ipmr.c) zeroes
// src_len and dst_len and adds no FRA_SRC or FRA_DST, the only attributes
// print_rule renders through the family.

// ErrMruleFamily is do_multirule's refusal of any other family, which it
// prints and exits -1 for, so 255.
var ErrMruleFamily = errors.New("mrule family")

// mruleFamily is do_multirule's switch. goip has no -M, so the two
// multicast families cannot arrive already set.
func mruleFamily(family uint8) (uint8, error) {
	switch family {
	case unix.AF_UNSPEC, unix.AF_INET:
		return xtcpnl.RtnlFamilyIpmrCst, nil
	case unix.AF_INET6:
		return xtcpnl.RtnlFamilyIp6mrCst, nil
	}
	return 0, fmt.Errorf("%w: Multicast rules are only supported for IPv4/IPv6, was: %d", ErrMruleFamily, family)
}

// runMrule is do_multirule, the family first, as ip checks it before any
// verb. Only do_iprule's listing is implemented; its writes, save, restore
// and flush with the multicast families are not.
func runMrule(c *runCtx, args []string) error {
	family, err := mruleFamily(c.family)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		if !matchesAny(args[0], ruleShowVerbs) {
			return fmt.Errorf("mrule %q: %w", args[0], ErrNotImplemented)
		}
		if err := parseRuleShowArgs(args[1:]); err != nil {
			return err
		}
	}
	c.family = family
	return ruleShow(c)
}
