package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ruleShowVerbs are the three spellings do_iprule accepts for the listing
// (ip/iprule.c:1240-1243), in its order, which is what decides the
// abbreviations:
//
//	l    -> list, because no earlier verb starts with 'l'
//	s    -> show, because "save" comes after the list group
//	sa   -> save, which is NOT show, since "sa" is not a prefix of "show"
//	ls   -> lst, the only verb "ls" matches
//
// Note the order differs from do_iproute's: rule tests "lst" SECOND and
// "show" third, where route tests "show" second. Neither order changes what
// any of these four abbreviations resolves to, which is worth knowing because
// it means transcribing the wrong order would not be caught by testing them.
var ruleShowVerbs = []string{"list", "lst", "show"}

func runRule(c *runCtx, args []string) error {
	if len(args) > 0 {
		if !matchesAny(args[0], ruleShowVerbs) {
			return fmt.Errorf("rule %q: %w", args[0], ErrNotImplemented)
		}
		args = args[1:]
	}
	// A bare `ip rule` is a list (ip/iprule.c:1238-1239), like `ip route`.
	if err := parseRuleShowArgs(args); err != nil {
		return err
	}
	return ruleShow(c)
}

// parseRuleShowArgs rejects every selector, and unlike the other objects it
// has none to accept.
//
// That is not an omission: `ip rule show`'s selectors are the one group in
// goip that cannot change a request byte. iprule_list_flush_or_save parses
// them into `filter` and filter_nlmsg applies them to REPLIES
// (ip/iprule.c:98-243), while the dump request stays the same 28 bytes — the
// kernel will not even accept an attribute on it
// (net/core/fib_rules.c:1278-1281). So implementing `from`, `to`, `iif`,
// `oif`, `pref`, `fwmark`, `uidrange` and the rest would add client-side
// filtering and nothing the netlink tier could compare.
//
// They are rejected rather than ignored, which is the policy every object in
// goip follows and matters more here than elsewhere: answering `ip rule show
// pref 100` with all twenty rules is a wrong answer that LOOKS like a
// superset, and the parity harness compares stdout, so it would be reported
// as a rendering divergence on nineteen lines rather than as a missing
// selector.
func parseRuleShowArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("rule show %q: %w", args[0], ErrNotImplemented)
}

// ruleShow sends the one request `ip rule show` sends and renders the replies.
//
// # The family substitution, which is the only interesting byte
//
// iprule_list_flush_or_save opens with:
//
//	int af = preferred_family;
//	if (af == AF_UNSPEC)
//		af = AF_INET;
//
// (ip/iprule.c:748-752), so a BARE `ip rule show` asks the kernel for IPv4
// rules, not for every family. That makes `ip rule show` and `ip -4 rule
// show` byte-identical requests as well as identical output, and it is the
// reason there is no `-4` pcap in the corpus — the claim is asserted by a
// parity row instead.
//
// The substitution is here, not in req.RuleShowDump, because this is where
// `ip` does it. rtnl_ruledump_req sends whatever family it is handed.
//
// It is also the one place where getting it wrong is SILENT: the kernel's
// strict-mode validator checks every fib_rule_hdr field except family
// (net/core/fib_rules.c:1271-1276), so an AF_UNSPEC request is accepted and
// answered — with rules from every family at once, which no `ip` invocation
// produces. A one-byte mistake that yields a plausible larger answer is
// exactly what the netlink tier's byte comparison is for.
//
// # -0 is not AF_PACKET's problem here
//
// `-0` sets preferred_family to AF_PACKET, which is not AF_UNSPEC, so it
// survives the substitution and reaches the kernel. The kernel has no
// AF_PACKET rules_ops, so fib_nl_dumprule finds no ops and the dump comes
// back empty rather than erroring. goip reproduces that by passing the family
// through, which costs nothing: there is no branch to write.
func ruleShow(c *runCtx) error {
	family := c.family
	if family == unix.AF_UNSPEC {
		family = unix.AF_INET
	}

	svc := service.New(c.src, c.nextSeq)
	rules, err := svc.Rules(family)
	if err != nil {
		return err
	}

	decoded := make([]xtcpnl.RuleInfo, len(rules))
	for i := range rules {
		decoded[i] = xtcpnl.RuleInfo(rules[i])
	}
	if err := checkRuleRenderable(decoded); err != nil {
		return err
	}

	views := make([]render.RuleView, 0, len(decoded))
	for i := range decoded {
		ri := decoded[i]
		// print_rule's protocol guard (ip/iprule.c:552-557) is
		// `(protocol && protocol != RTPROT_KERNEL) || show_details > 0`.
		// Applied by clearing the presence bit rather than inside
		// RuleViewOf, so the renderer keeps the property every other view in
		// this package has: it is a pure function of decoded bytes, with no
		// CLI state threaded into it.
		//
		// Which makes this the cheapest -d in goip and the most easily got
		// wrong. Every rule the topology adds carries FRA_PROTOCOL with
		// value ZERO, so without -d the attribute is present, decoded, and
		// printed nowhere; a renderer holding a bare uint8 cannot tell those
		// sixteen rules from a rule that sent no attribute, and both goldens
		// agree until -d is asked for.
		if !c.detailed() && (ri.Protocol == 0 || ri.Protocol == unix.RTPROT_KERNEL) {
			ri.HasProtocol = false
		}
		views = append(views, render.RuleViewOf(ri))
	}

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

// checkRuleRenderable refuses a dump carrying an attribute print_rule renders
// through a name table goip does not have.
//
// # The two, and why they are one case rather than two decisions
//
// FRA_IP_PROTO prints ` ipproto %s` through inet_proto_n2a (ip/iprule.c:405-410),
// which is getprotobynumber and therefore /etc/protocols. FRA_DSCP prints
// ` dscp %s` through rtnl_dscp_n2a (:559-581), which is rtnl_dsfield_get_name
// and therefore /etc/iproute2/rt_dsfield. Both resolve a number to a name out
// of a file on whichever machine runs `ip`, and render's package doc has said
// since it was written that goip reads no config files — which is also why
// dsfieldName is pinned to its numeric fallback rather than carrying a table.
//
// Rendering them numerically would produce ` ipproto 6` where `ip` prints
// ` ipproto tcp`: a plausible line, reported by the parity harness as a
// rendering bug on that one rule, with nothing to say a name table is absent.
// Refusing follows the IFLA_INFO_DATA precedent (obj_link.go's
// checkDetailSupported) and the `-s -s` one: an explicit error names the
// missing feature where it was asked for.
//
// # Unlike the -d refusals, this one is unconditional
//
// checkDetailSupported returns early unless -d was given, because
// IFLA_INFO_DATA only prints under -d. Both attributes here print on a BARE
// `ip rule show`, so there is no flag to gate on.
//
// # Measured, which is what makes the refusal cheap
//
// No rule in the captured topology carries either attribute, and that is on
// purpose — netlink-topology.exp's build_clean_rules says so at the two lines
// it does not write. So `ip rule show` is refused nowhere the parity harness
// runs, while a rule added by hand on a real box is refused loudly instead of
// rendered wrong.
func checkRuleRenderable(rules []xtcpnl.RuleInfo) error {
	for i := range rules {
		ri := &rules[i]
		switch {
		case ri.HasIPProto:
			return fmt.Errorf("rule pref %d: iproute2 renders FRA_IP_PROTO through "+
				"/etc/protocols and goip reads no config files: %w", ri.Priority, ErrNotImplemented)
		case ri.HasDscp:
			return fmt.Errorf("rule pref %d: iproute2 renders FRA_DSCP through "+
				"/etc/iproute2/rt_dsfield and goip reads no config files: %w",
				ri.Priority, ErrNotImplemented)
		}
	}
	return nil
}
