package render

import (
	"fmt"
	"net/netip"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is the print helpers ip/tunnel.c gives the tunnel print_opts
// (iproute2 v7.2.0): tnl_print_endpoint and tnl_print_encap (KG4-T07,
// docs/netlink/goip-link-kinds.md).

// tnlAnyCst and tnlUnknownCst are tnl_print_endpoint's two words.
const (
	tnlAnyCst     = "any"
	tnlUnknownCst = "unknown"
)

// tnlEndpoint is tnl_print_endpoint: `any` when not sent or all zero,
// `unknown` when the attribute's length isn't 4 or 16 or is the other
// family's, the address otherwise. n is the attribute's length.
func tnlEndpoint(k *KindData, key string, a netip.Addr, has bool, n int, v6 bool) {
	v := tnlAnyCst
	switch {
	case !has:
	case n != 4 && n != 16, (n == 16) != v6:
		v = tnlUnknownCst
	case !a.IsUnspecified():
		v = a.String()
	}
	k.add(key, key+" "+v+" ", v)
}

// tnlEncapPort is tnl_encap_str's port: `auto` for a source port of 0.
func tnlEncapPort(name string, port uint16, auto bool) string {
	if auto && port == 0 {
		return "encap-" + name + " auto "
	}
	return fmt.Sprintf("encap-%s %d ", name, port)
}

// tnlEncapFlags are tnl_print_encap's flag words, in order: the word and
// JSON key, and the bit.
var tnlEncapFlags = [...]struct {
	name string
	bit  uint16
}{
	{"csum", xtcpnl.TunnelEncapFlagCsum},
	{"csum6", xtcpnl.TunnelEncapFlagCsum6},
	{"remcsum", xtcpnl.TunnelEncapFlagRemcsum},
}

// tnlEncap is tnl_print_encap: nothing when no type or type none, else
// the `encap` object. ip reads the flags and ports whether or not sent;
// the kernel sends all four together.
func tnlEncap(k *KindData, e xtcpnl.TunnelEncap) {
	if !e.HasType || e.Type == xtcpnl.TunnelEncapNone {
		return
	}
	o := &KindData{}
	o.addText("encap ")
	switch e.Type {
	case xtcpnl.TunnelEncapFOU:
		o.add("type", "fou ", "fou")
	case xtcpnl.TunnelEncapGUE:
		o.add("type", "gue ", "gue")
	default:
		o.add("type", tnlUnknownCst+" ", nil)
	}
	o.add("sport", tnlEncapPort("sport", e.Sport, true), e.Sport)
	o.add("dport", tnlEncapPort("dport", e.Dport, false), e.Dport)
	for _, f := range tnlEncapFlags {
		on := e.Flags&f.bit != 0
		o.add(f.name, noPrefix(on)+"encap-"+f.name+" ", on)
	}
	k.add("encap", "", o)
}

// tnlTTL is the ttl (or hoplimit) word: `inherit` in text for 0, the
// number in JSON always, under the key ttl.
func tnlTTL(k *KindData, word string, ttl uint8) {
	text := fmt.Sprintf("%s %d ", word, ttl)
	if ttl == 0 {
		text = word + " inherit "
	}
	k.add("ttl", text, ttl)
}

// tnlLink is the `dev` word for a nonzero link, by name.
func tnlLink(k *KindData, names NameTab, has bool, link uint32) {
	if has && link != 0 {
		name := kindLinkName(names, link)
		k.add("link", "dev "+name+" ", name)
	}
}

// tnlFwmark is the fwmark word, printed when not zero.
func tnlFwmark(k *KindData, has bool, mark uint32) {
	if has && mark != 0 {
		h := cHex0x(uint64(mark))
		k.add("fwmark", "fwmark "+h+" ", h)
	}
}
