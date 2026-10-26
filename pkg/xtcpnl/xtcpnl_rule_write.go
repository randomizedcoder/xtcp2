package xtcpnl

import (
	"errors"

	"golang.org/x/sys/unix"
)

// This file encodes the request `ip rule add/delete` sends: the fib_rule_hdr
// and the FRA_* attributes. It is RouteWrite's counterpart for the routing
// policy database, so a library caller builds a rule from fields rather than
// from argv (internal/goip/actuate is the first; the CLI path in
// internal/goip/rule_write.go keeps parsing argv in iprule_modify's order).
//
// It encodes; it does not parse argv. iprule_modify (ip/iprule.c) writes each
// attribute as its keyword is read, so the order is argv order and that is
// internal/goip's business. A RuleWrite carries the attributes as an ordered
// list and this file writes them as given, after the one attribute the header
// forces: fib_rule_hdr.table is eight bits, so a table id past 255 travels as
// RT_TABLE_UNSPEC in the header and the real id in FRA_TABLE, which is
// written first. The argv `table N` keyword does the same at its own
// position, so `ip rule add table N …` and a RuleWrite with Table N agree
// byte for byte.
//
// Nothing here sends. BuildRuleWriteRequest goes through BuildWriteRequest,
// and goip writes the result to a file (link-write-path.md, decision 5) or,
// for the one opted-in actuator, to an acknowledged socket.

// ruleWriteBufCst is iprule_modify's char buf[1024], the room after the
// nlmsghdr and fib_rule_hdr that addattr_l is given as maxlen.
const ruleWriteBufCst = 1024

// ErrRuleWriteType is a RuleWrite whose MsgType is not RTM_NEWRULE or
// RTM_DELRULE. BuildWriteRequest would admit route and link types too; a
// rule write must not carry one.
var ErrRuleWriteType = errors.New("xtcpnl: a rule write is RTM_NEWRULE or RTM_DELRULE")

// RuleAttr is one FRA_* attribute of a rule write, payload already encoded.
type RuleAttr struct {
	Type    uint16
	Payload []byte
}

// RuleAttrU8 is addattr8: FRA_IP_PROTO, FRA_PROTOCOL, FRA_L3MDEV.
func RuleAttrU8(atype uint16, v uint8) RuleAttr {
	return RuleAttr{Type: atype, Payload: []byte{v}}
}

// RuleAttrU32 is addattr32: FRA_PRIORITY, FRA_FWMARK, FRA_FWMASK, FRA_GOTO,
// FRA_SUPPRESS_PREFIXLEN. FRA_TABLE is RuleWrite.Table's business.
func RuleAttrU32(atype uint16, v uint32) RuleAttr {
	return RuleAttr{Type: atype, Payload: RouteAttrU32(atype, v).Payload}
}

// RuleAttrName is addattr_l of a NUL-terminated interface name: FRA_IIFNAME
// and FRA_OIFNAME. iprule_modify sends strlen+1, so the NUL is counted in
// rta_len and the padding after it is not.
func RuleAttrName(atype uint16, name string) RuleAttr {
	return RuleAttr{Type: atype, Payload: append([]byte(name), 0)}
}

// RuleAttrBytes is addattr_l of raw bytes: FRA_DST and FRA_SRC (the address
// bytes), FRA_UID_RANGE and the port ranges (their structs).
func RuleAttrBytes(atype uint16, b []byte) RuleAttr {
	return RuleAttr{Type: atype, Payload: b}
}

// RuleWrite is one rule write as iprule_modify assembles it.
type RuleWrite struct {
	// MsgType is RTM_NEWRULE or RTM_DELRULE.
	MsgType uint16
	// Flags are the NLM_F_* bits besides NLM_F_REQUEST: NLM_F_CREATE|
	// NLM_F_EXCL for add, nothing for delete, and NLM_F_ACK, which
	// rtnl_talk adds.
	Flags uint16

	// The struct fib_rule_hdr, field for field, except Table.
	Family uint8
	DstLen uint8
	SrcLen uint8
	Tos    uint8
	// Action is FR_ACT_TO_TBL for an add and FR_ACT_UNSPEC for a delete
	// (iprule_modify sets it from the verb before the loop).
	Action uint8
	// FrhFlags is fib_rule_hdr.flags: FIB_RULE_INVERT for `not`.
	FrhFlags uint32

	// Table is the routing table id. Zero sends none (a delete that does
	// not select on table); 1..255 is the header byte; above that the
	// header carries RT_TABLE_UNSPEC and FRA_TABLE carries the id.
	Table uint32

	// Attrs are the selectors in wire order, after FRA_TABLE when there
	// is one.
	Attrs []RuleAttr
}

// EncodeFibRuleHdr lays out a struct fib_rule_hdr: family, dst_len,
// src_len, tos, table, two reserved bytes, action, flags.
func EncodeFibRuleHdr(w *RuleWrite) []byte {
	table := uint8(unix.RT_TABLE_UNSPEC)
	if w.Table < 256 {
		table = uint8(w.Table)
	}
	b := make([]byte, 0, FibRuleHdrSizeCst)
	b = append(b, w.Family, w.DstLen, w.SrcLen, w.Tos, table, 0, 0, w.Action)
	return append(b, byte(w.FrhFlags), byte(w.FrhFlags>>8), byte(w.FrhFlags>>16), byte(w.FrhFlags>>24))
}

// BuildRuleWriteRequest lays out w: the fib_rule_hdr, FRA_TABLE when the id
// does not fit the header, then w.Attrs in order.
func BuildRuleWriteRequest(w RuleWrite, seq uint32) ([]byte, error) {
	if w.MsgType != uint16(unix.RTM_NEWRULE) && w.MsgType != uint16(unix.RTM_DELRULE) {
		return nil, ErrRuleWriteType
	}
	var raw [ruleWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if w.Table >= 256 {
		if err := ab.PutU32(uint16(unix.FRA_TABLE), w.Table); err != nil {
			return nil, err
		}
	}
	for _, a := range w.Attrs {
		if err := ab.PutBytes(a.Type, a.Payload); err != nil {
			return nil, err
		}
	}
	return BuildWriteRequest(w.MsgType, w.Flags, seq, EncodeFibRuleHdr(&w), ab.Bytes())
}
