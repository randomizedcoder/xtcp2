package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// The replies `bridge mdb show` and `bridge mdb get` print (bridge/mdb.c
// print_mdbs, print_router_ports, mdb_get): gobridge 7P.

// ErrMdbGetReply is an mdb get reply that is not one entry without router
// ports: br_mdb_get_reply_alloc fills exactly one MDBA_MDB_ENTRY_INFO.
var ErrMdbGetReply = errors.New("xtcpnl: mdb get reply is not one entry")

// MdbDump is one RTM_GETMDB dump reply in full: its MDBA_MDB entries and
// its MDBA_ROUTER ports, in wire order.
type MdbDump struct {
	Bridge    uint32 // br_port_msg's ifindex
	Entries   []MdbDumpEntry
	Routers   []MdbRouterPort
	HasRouter bool // an MDBA_ROUTER nest was present, even an empty one
}

// ParseMdbDumpRouters decodes a dump reply as ParseMdbDump does and keeps
// its router ports too, through ParseMdbRouterPorts. mdb_show asks for the
// entries and the router ports in two dumps, and the kernel puts both
// nests in each reply; print_mdbs and print_router_ports pick theirs.
func ParseMdbDumpRouters(body []byte) (MdbDump, error) {
	entries, err := ParseMdbDump(body)
	if err != nil {
		return MdbDump{}, err
	}
	d := MdbDump{Bridge: binary.LittleEndian.Uint32(body[4:8]), Entries: entries}
	var perr error
	err = WalkRTAttrs(body[BrPortMsgSizeCst:], func(atype uint16, val []byte) {
		if atype != MdbaRouter {
			return
		}
		ports, rerr := ParseMdbRouterPorts(val)
		if rerr != nil {
			perr = errors.Join(perr, rerr)
			return
		}
		d.Routers = append(d.Routers, ports...)
		d.HasRouter = true
	})
	if err = errors.Join(err, perr); err != nil {
		return MdbDump{}, err
	}
	return d, nil
}

// ParseMdbGetReply decodes the RTM_NEWMDB an `mdb get` is answered with
// (br_mdb_get_reply_alloc, vxlan_mdb_get_reply_alloc): the dump's layout
// holding one entry and no router ports. Anything else is ErrMdbGetReply.
func ParseMdbGetReply(body []byte) (MdbDumpEntry, error) {
	d, err := ParseMdbDumpRouters(body)
	if err != nil {
		return MdbDumpEntry{}, err
	}
	if len(d.Entries) != 1 || d.HasRouter {
		return MdbDumpEntry{}, ErrMdbGetReply
	}
	return d.Entries[0], nil
}
