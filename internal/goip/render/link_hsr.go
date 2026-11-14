package render

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is hsr_print_opt (ip/iplink_hsr.c) and netkit_print_opt
// (ip/iplink_netkit.c), iproute2 v7.2.0 (KG5-T06,
// docs/netlink/goip-link-kinds.md).
//
//   - hsr prints nothing at all when a slave, the interlink, the sequence
//     number or the supervision address came short.
//   - hsr's slaves print `<none>`, JSON null, when absent; the interlink,
//     sequence, supervision, proto and version only when present.
//   - netkit's mode, policies and scrubs print UNKNOWN past their tables;
//     PRIMARY is `type primary` or `type peer`.

// The kinds this file renders, exported for the `-d` gate.
const (
	HsrKindCst    = "hsr"
	NetkitKindCst = "netkit"
)

// hsrNoneCst is print_null's text for an absent slave.
const hsrNoneCst = "<none>"

// netkitUnknownCst is netkit's name for a value past its table.
const netkitUnknownCst = "UNKNOWN"

// hsrData is hsr_print_opt over the decoded IFLA_INFO_DATA.
func hsrData(h *xtcpnl.HsrInfo, names NameTab) *KindData {
	k := &KindData{}
	if h == nil || h.Short {
		return k
	}
	hsrSlave(k, names, "slave1", h.HasSlave1, h.Slave1)
	hsrSlave(k, names, "slave2", h.HasSlave2, h.Slave2)
	if h.HasInterlink {
		name := kindLinkName(names, h.Interlink)
		k.add("interlink", "interlink "+name+" ", name)
	}
	if h.HasSeqNr {
		k.add("seq_nr", "sequence "+strconv.Itoa(int(h.SeqNr))+" ", h.SeqNr)
	}
	if h.HasSupervisionAddr {
		mac := xtcpnl.LLAddrN2A(h.SupervisionAddr[:], unix.ARPHRD_VOID)
		k.add("supervision_addr", "supervision "+mac+" ", mac)
	}
	if h.HasProtocol {
		k.add("proto", "proto "+strconv.Itoa(int(h.Protocol))+" ", h.Protocol)
	}
	if h.HasVersion {
		k.add("version", "version "+strconv.Itoa(int(h.Version))+" ", h.Version)
	}
	return k
}

// hsrSlave is a slave by name, or print_null's `<none>`.
func hsrSlave(k *KindData, names NameTab, key string, has bool, idx uint32) {
	if !has {
		k.add(key, key+" "+hsrNoneCst+" ", nil)
		return
	}
	name := kindLinkName(names, idx)
	k.add(key, key+" "+name+" ", name)
}

// netkitData is netkit_print_opt over the decoded IFLA_INFO_DATA.
func netkitData(n *xtcpnl.NetkitInfo) *KindData {
	k := &KindData{}
	if n == nil {
		return k
	}
	netkitName(k, "mode", "mode", n.HasMode, n.Mode, xtcpnl.NetkitModeNames)
	if n.HasPrimary {
		t := "peer"
		if n.Primary != 0 {
			t = "primary"
		}
		k.add("type", "type "+t+" ", t)
	}
	netkitName(k, "policy", "policy", n.HasPolicy, n.Policy, xtcpnl.NetkitPolicyNames)
	netkitName(k, "peer_policy", "peer policy", n.HasPeerPolicy, n.PeerPolicy, xtcpnl.NetkitPolicyNames)
	netkitName(k, "scrub", "scrub", n.HasScrub, n.Scrub, xtcpnl.NetkitScrubNames)
	netkitName(k, "peer_scrub", "peer scrub", n.HasPeerScrub, n.PeerScrub, xtcpnl.NetkitScrubNames)
	return k
}

// netkitName is one of netkit's named u32s, UNKNOWN past its table.
func netkitName(k *KindData, key, word string, has bool, v uint32, names map[uint32]string) {
	if !has {
		return
	}
	name, ok := names[v]
	if !ok {
		name = netkitUnknownCst
	}
	k.add(key, word+" "+name+" ", name)
}
