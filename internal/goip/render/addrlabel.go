package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// ErrAddrLabelPerDev marks a per-device addrlabel entry (ifal_index != 0) goip
// declines to render: print_addrlabel resolves the index through
// ll_index_to_name, which is not reproducible from the dump bytes, and no capture
// carries such an entry. The object handler maps this to ErrNotImplemented, as
// obj_nexthop does for ErrNexthopGroup.
var ErrAddrLabelPerDev = errors.New("per-device addrlabel entry not implemented by goip")

// AddrLabelView is one line of `ip addrlabel show`, transcribed from
// print_addrlabel (ip/ipaddrlabel.c:44-97).
//
// # Like RuleView, this object carries no NameTab
//
// print_addrlabel resolves an index to a name only for a per-device entry
// (ifal_index != 0), which the kernel's built-in label table never produces —
// every default entry has index 0. goip declines the nonzero-index case in
// AddrLabelViewOf rather than carry a NameTab no grounded reply would use; see
// the refusal there.
//
// # Field order is print order, twice over
//
// Text: `prefix ADDR/LEN label N ` with a trailing space before the newline,
// each token emitted by its own print_* call. JSON: keys `address`, `prefixlen`,
// `label` in that order — the two paths diverge, because the text token is glued
// (`ADDR/LEN`) while JSON splits it into `address` + `prefixlen`, and MarshalJSON
// writes the keys itself rather than leaning on struct tags.
type AddrLabelView struct {
	// Address is the formatted IFAL_ADDRESS. HasAddress distinguishes an absent
	// attribute from the all-zero `::` of the default route entry, which is a
	// real, printed address.
	Address    string
	HasAddress bool
	PrefixLen  uint8

	// Label is IFAL_LABEL; HasLabel gates it because label 0 is a real value
	// (the ::1/128 entry) that omitempty would wrongly drop.
	Label    uint32
	HasLabel bool
}

// AddrLabelViewOf derives the view from a decoded reply.
//
// It declines a per-device entry (ifal_index != 0): print_addrlabel renders it
// through ll_index_to_name, which resolves against the running machine's
// interface list and so is not reproducible from the netlink bytes alone. No
// capture carries such an entry, so rendering one would be ungrounded. This
// follows the checkRuleRenderable precedent (obj_rule.go) and the nexthop
// unknown-group refusal (NexthopViewOf).
func AddrLabelViewOf(ai xtcpnl.AddrLabelInfo) (AddrLabelView, error) {
	if ai.Index != 0 {
		return AddrLabelView{}, fmt.Errorf("addrlabel dev index %d: %w", ai.Index, ErrAddrLabelPerDev)
	}
	v := AddrLabelView{PrefixLen: ai.Prefixlen, Label: ai.Label, HasLabel: ai.HasLabel}
	if ai.Address != nil {
		v.Address = addrString(ai.Address, ai.Family)
		v.HasAddress = true
	}
	return v, nil
}

// Text renders one `ip addrlabel show` line, newline terminated, reproducing the
// trailing space each print_* token carries.
func (v AddrLabelView) Text() string {
	var b bytes.Buffer
	if v.HasAddress {
		b.WriteString("prefix ")
		b.WriteString(v.Address)
		b.WriteByte('/')
		b.WriteString(strconv.FormatUint(uint64(v.PrefixLen), 10))
		b.WriteByte(' ')
	}
	if v.HasLabel {
		b.WriteString("label ")
		b.WriteString(strconv.FormatUint(uint64(v.Label), 10))
		b.WriteByte(' ')
	}
	b.WriteByte('\n')
	return b.String()
}

// MarshalJSON writes the keys in print_addrlabel's emission order: address,
// prefixlen, label. prefixlen is emitted only alongside address (print_addrlabel
// prints both inside the same `if (tb[IFAL_ADDRESS])` block), and label only
// when present — both as bare JSON numbers (print_uint).
func (v AddrLabelView) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	write := func(key, val string) {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteByte('"')
		b.WriteString(key)
		b.WriteString(`":`)
		b.WriteString(val)
	}
	if v.HasAddress {
		addr, err := json.Marshal(v.Address)
		if err != nil {
			return nil, err
		}
		write("address", string(addr))
		write("prefixlen", strconv.FormatUint(uint64(v.PrefixLen), 10))
	}
	if v.HasLabel {
		write("label", strconv.FormatUint(uint64(v.Label), 10))
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
