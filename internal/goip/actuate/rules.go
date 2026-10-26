package actuate

import (
	"context"
	"errors"
	"reflect"

	"github.com/randomizedcoder/xtcp2/internal/goip"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// rule is the request for the one policy rule this backend owns, as
// xtcpnl.RuleWrite lays it out: `ip rule add table T priority P ipproto tcp
// iif lo protocol PROTO`. Only locally originated TCP (iif lo is the kernel's
// marker for output lookups) consults the private table; FRA_PROTOCOL marks
// the rule as this actuator's so readback can tell it from an operator's.
// del is the matching RTM_DELRULE: the same selectors, no action and no
// create flags. The returned bytes are the whole request, nlmsghdr included.
func (b *Backend) rule(family uint8, del bool) ([]byte, error) {
	if err := b.reserved(); err != nil {
		return nil, err
	}
	w := xtcpnl.RuleWrite{
		MsgType: unix.RTM_NEWRULE, Flags: unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL,
		Family: family, Action: unix.FR_ACT_TO_TBL, Table: b.Table,
		Attrs: []xtcpnl.RuleAttr{
			xtcpnl.RuleAttrU32(unix.FRA_PRIORITY, b.Priority),
			xtcpnl.RuleAttrU8(unix.FRA_IP_PROTO, unix.IPPROTO_TCP),
			xtcpnl.RuleAttrName(unix.FRA_IIFNAME, "lo"),
			xtcpnl.RuleAttrU8(unix.FRA_PROTOCOL, b.Protocol),
		},
	}
	if del {
		w.MsgType, w.Flags, w.Action = unix.RTM_DELRULE, unix.NLM_F_ACK, unix.FR_ACT_UNSPEC
	}
	return xtcpnl.BuildRuleWriteRequest(w, 1)
}

func (b *Backend) ownRule(r xtcpnl.RuleInfo) bool {
	raw, err := b.rule(r.Family, false)
	if err != nil {
		return false
	}
	want, err := xtcpnl.ParseRule(raw[xtcpnl.NlMsgHdrSizeCst:])
	clearDefaultSuppressors(&r)
	clearDefaultSuppressors(&want)
	// FRA_TABLE is authoritative; Linux emits RT_TABLE_COMPAT (252) in
	// the legacy byte for a table ID that cannot fit there.
	r.RawTable = 0
	want.RawTable = 0
	return err == nil && reflect.DeepEqual(r, want)
}

func clearDefaultSuppressors(r *xtcpnl.RuleInfo) {
	if r.HasSuppressPrefixlen && r.SuppressPrefixlen == ^uint32(0) {
		r.HasSuppressPrefixlen = false
		r.SuppressPrefixlen = 0
	}
	if r.HasSuppressIfgroup && r.SuppressIfgroup == ^uint32(0) {
		r.HasSuppressIfgroup = false
		r.SuppressIfgroup = 0
	}
}

func normalRule(r xtcpnl.RuleInfo) bool {
	if !(r.Priority == 0 && r.Table == unix.RT_TABLE_LOCAL || r.Priority == 32766 && r.Table == unix.RT_TABLE_MAIN || r.Priority == 32767 && r.Table == unix.RT_TABLE_DEFAULT) {
		return false
	}
	if r.Action != unix.FR_ACT_TO_TBL {
		return false
	}
	clearDefaultSuppressors(&r)
	// Kernel-generated default rules may carry FRA_PROTOCOL=kernel.
	r.Family = 0
	r.Table = 0
	r.RawTable = 0
	r.Priority = 0
	r.HasPriority = false
	r.Action = 0
	r.Protocol = 0
	r.HasProtocol = false
	return reflect.DeepEqual(r, xtcpnl.RuleInfo{})
}

func (b *Backend) checkRules(svc *service.Service, family uint8) error {
	if e, ok := b.ruleCache[family]; ok {
		return e
	}
	rs, err := svc.Rules(family)
	if err != nil {
		return err
	}
	local, main := false, false
	owned := 0
	for _, v := range rs {
		r := xtcpnl.RuleInfo(v)
		if b.ownRule(r) {
			owned++
			continue
		}
		if !normalRule(r) {
			return errors.New("unsupported policy routing or reserved rule collision")
		}
		local = local || r.Table == unix.RT_TABLE_LOCAL
		main = main || r.Table == unix.RT_TABLE_MAIN
	}
	if !local || !main || owned > 1 {
		return errors.New("missing/duplicate baseline routing rules")
	}
	if b.ruleCache != nil {
		b.ruleCache[family] = nil
	}
	return nil
}

func (b *Backend) rules(ctx context.Context, enable bool) error {
	if err := b.valid(); err != nil {
		return err
	}
	return withSource(ctx, func(s *goip.NetlinkSource, svc *service.Service) error {
		for _, f := range []uint8{unix.AF_INET, unix.AF_INET6} {
			if enable {
				if err := b.checkRules(svc, f); err != nil {
					return err
				}
			}
			rs, err := svc.Rules(f)
			if err != nil {
				return err
			}
			owned := 0
			for _, r := range rs {
				if b.ownRule(xtcpnl.RuleInfo(r)) {
					owned++
				} else if enable && (r.Priority == b.Priority || r.Table == b.Table) {
					return errors.New("reserved routing rule collision")
				}
			}
			if enable {
				routes, e := b.routeBodies(svc, f, b.Table)
				if e != nil {
					return e
				}
				for _, raw := range routes {
					r, e := xtcpnl.ParseNewRoute(raw)
					if e != nil {
						return e
					}
					if r.Protocol != b.Protocol {
						return errors.New("foreign route in reserved treatment table")
					}
				}
			}
			if enable && owned == 0 || !enable && owned > 0 {
				msg, err := b.rule(f, !enable)
				if err != nil {
					return err
				}
				for range max(1, owned) {
					if err = s.ApplyRouteControl(msg); err != nil {
						return err
					}
				}
			}
			rs, err = svc.Rules(f)
			if err != nil {
				return err
			}
			owned = 0
			for _, r := range rs {
				if b.ownRule(xtcpnl.RuleInfo(r)) {
					owned++
				}
			}
			if enable && owned != 1 || !enable && owned != 0 {
				return errors.New("rule readback mismatch")
			}
		}
		return nil
	})
}

func (b *Backend) Enable(ctx context.Context) error { return b.rules(ctx, true) }

func (b *Backend) Disable(ctx context.Context) error { return b.rules(ctx, false) }
