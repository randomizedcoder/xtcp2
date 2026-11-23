package gobridge

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This is the part of lib/libnetlink.c and lib/ll_map.c bridge's shows
// run through: rtnl_dump_filter's loop and its error reports, the
// suppressed rtnl_talk of ll_link_get, and the link-name cache
// ll_init_map fills.

// llMaskCst is the IFLA_EXT_MASK of ll_init_map's dump and ll_link_get's
// get: RTEXT_FILTER_VF | SKIP_STATS | NAME_ONLY.
const llMaskCst = xtcpnl.RTEXT_FILTER_VF | xtcpnl.RTEXT_FILTER_SKIP_STATS | xtcpnl.RtextFilterNameOnly

// llEntry is one cached link.
type llEntry struct {
	name  string
	flags uint32
	typ   uint16 // ifi_type, an ARPHRD_*: ll_index_to_type
}

// llCache is ll_map.c's cache: by index and by name.
type llCache struct {
	byIndex map[uint32]llEntry
	byName  map[string]uint32
}

func (c *llCache) remember(index uint32, name string, flags uint32, typ uint16) {
	if c.byIndex == nil {
		c.byIndex, c.byName = map[uint32]llEntry{}, map[string]uint32{}
	}
	if old, ok := c.byIndex[index]; ok {
		delete(c.byName, old.name)
	}
	c.byIndex[index] = llEntry{name: name, flags: flags, typ: typ}
	c.byName[name] = index
}

// rememberMsg is ll_remember_index: an RTM_NEWLINK with a name enters the
// cache, an RTM_DELLINK leaves it.
func (c *llCache) rememberMsg(m Msg) {
	if (m.Type != unix.RTM_NEWLINK && m.Type != unix.RTM_DELLINK) || len(m.Body) < xtcpnl.IfInfomsgSizeCst {
		return
	}
	index := binary.LittleEndian.Uint32(m.Body[4:8])
	flags := binary.LittleEndian.Uint32(m.Body[8:12])
	if m.Type == unix.RTM_DELLINK {
		if old, ok := c.byIndex[index]; ok {
			delete(c.byName, old.name)
			delete(c.byIndex, index)
		}
		return
	}
	t, err := xtcpnl.ParseAttrTable(m.Body[xtcpnl.IfInfomsgSizeCst:], xtcpnl.IflaMaxCst)
	if err != nil {
		return
	}
	if name, ok := t.Str(unix.IFLA_IFNAME); ok {
		c.remember(index, name, flags, binary.LittleEndian.Uint16(m.Body[2:4]))
	}
}

// nextSeq stands in for iproute2's time-based sequence numbers; a replay
// ignores them.
func (r *runner) nextSeq() uint32 {
	r.seq++
	return r.seq
}

// talk sends a request and returns its reply; a replay failure is
// returned as is.
func (r *runner) talk(req []byte, err error) (Reply, error) {
	if err != nil {
		return Reply{}, fmt.Errorf("gobridge: building a request: %w", err)
	}
	return r.src.Talk(req)
}

// dump is rtnl_dump_filter: filter sees each message up to NLMSG_DONE; a
// negative return from it, a failed DONE or an NLMSG_ERROR ends the dump
// with a negative result, after the report rtnl_dump_done or
// rtnl_dump_error prints.
func (r *runner) dump(req []byte, berr error, filter func(Msg) int) (int, error) {
	rep, err := r.talk(req, berr)
	if err != nil {
		return 0, err
	}
	for _, m := range rep.Msgs {
		switch m.Type {
		case unix.NLMSG_DONE:
			return r.dumpDone(m), nil
		case unix.NLMSG_ERROR:
			r.dumpError(m)
			return -1, nil
		}
		if rc := filter(m); rc < 0 {
			return rc, nil
		}
	}
	// The capture ended without a DONE: the kernel always sends one, so
	// this is a truncated capture.
	return 0, fmt.Errorf("%w: a dump's reply has no NLMSG_DONE", ErrReplay)
}

// dumpDone is rtnl_dump_done.
func (r *runner) dumpDone(m Msg) int {
	if len(m.Body) < 4 {
		r.errf("DONE truncated\n")
		return -1
	}
	e := cint.Int(binary.LittleEndian.Uint32(m.Body[0:4]))
	if e >= 0 {
		return 0
	}
	// nl_dump_ext_ack_done parses the TLVs after the int whatever the
	// flags say.
	if msg := extackMsg(m.Body[4:]); msg != "" {
		r.extack(true, msg)
		return -1
	}
	switch errno := unix.Errno(-e); errno {
	case unix.ENOENT, unix.EOPNOTSUPP:
	case unix.EMSGSIZE:
		r.errf("Error: Buffer too small for object.\n")
	default:
		r.errf("RTNETLINK answers: %s\n", strerror(errno))
	}
	return -1
}

// dumpError is rtnl_dump_error on a route socket.
func (r *runner) dumpError(m Msg) {
	if len(m.Body) < 4+nlHeaderLen {
		r.errf("ERROR truncated\n")
		return
	}
	e := cint.Int(binary.LittleEndian.Uint32(m.Body[0:4]))
	r.errf("RTNETLINK answers: %s\n", strerror(unix.Errno(-e)))
}

// extackMsg is the NLMSGERR_ATTR_MSG in a TLV stream, "" for none or a
// malformed stream (mnl_attr_parse failing).
func extackMsg(tlvs []byte) string {
	t, err := xtcpnl.ParseAttrTable(tlvs, nlmsgerrAttrMaxCst)
	if err != nil {
		return ""
	}
	s, _ := t.Str(unix.NLMSGERR_ATTR_MSG)
	return s
}

// extack is print_ext_ack_msg.
func (r *runner) extack(isErr bool, msg string) {
	label := "Warning"
	if isErr {
		label = "Error"
	}
	dot := ""
	if !strings.HasSuffix(msg, ".") {
		dot = "."
	}
	r.errf("%s: %s%s\n", label, msg, dot)
}

// strerror is glibc's strerror: x/sys/unix's strings, which are glibc's
// with the first letter lowered, raised again.
func strerror(errno unix.Errno) string {
	if errno == 0 {
		return "Success"
	}
	s := errno.Error()
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// initMap is ll_init_map: the AF_UNSPEC link dump that fills the cache.
// Its failures exit 1.
func (r *runner) initMap() error {
	if r.llInited {
		return nil // static int initialized: a batch dumps the links once
	}
	req, berr := xtcpnl.BuildDumpLinkRequestExt(unix.AF_UNSPEC, llMaskCst, r.nextSeq())
	rc, err := r.dump(req, berr, func(m Msg) int {
		r.cache.rememberMsg(m)
		return 0
	})
	if err != nil {
		return err
	}
	if rc < 0 {
		r.errf("Dump terminated\n")
		return exitCode(1)
	}
	r.llInited = true
	return nil
}

// linkGet is ll_link_get: an RTM_GETLINK by name or index, errors
// suppressed; the answer enters the cache. It returns the index, 0 on
// failure.
func (r *runner) linkGet(name string, index uint32) (uint32, error) {
	var req []byte
	var berr error
	if name != "" {
		req, berr = xtcpnl.BuildGetLinkByNameRequest(unix.AF_UNSPEC, name, llMaskCst, r.nextSeq())
	} else {
		req, berr = xtcpnl.BuildGetLinkByIndexRequest(unix.AF_UNSPEC, cint.Int(index), llMaskCst, r.nextSeq())
	}
	rep, err := r.talk(req, berr)
	if err != nil {
		return 0, err
	}
	for _, m := range rep.Msgs {
		if m.Type == unix.RTM_NEWLINK && len(m.Body) >= xtcpnl.IfInfomsgSizeCst {
			r.cache.rememberMsg(m)
			return binary.LittleEndian.Uint32(m.Body[4:8]), nil
		}
	}
	return 0, nil
}

// nameToIndex is ll_name_to_index: the cache, ll_link_get, then "ifN".
// if_nametoindex would ask the kernel the same question by ioctl, so a
// replay takes its answer as ll_link_get's.
func (r *runner) nameToIndex(name string) (uint32, error) {
	if i, ok := r.cache.byName[name]; ok {
		return i, nil
	}
	i, err := r.linkGet(name, 0)
	if err != nil || i != 0 {
		return i, err
	}
	return idxA2N(name), nil
}

// idxA2N is ll_idx_a2n: sscanf(name, "if%u").
func idxA2N(name string) uint32 {
	rest, ok := strings.CutPrefix(name, "if")
	if !ok {
		return 0
	}
	n := 0
	for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	if n == 0 {
		return 0
	}
	v, err := strconv.ParseUint(rest[:n], 10, 32)
	if err != nil {
		return 0
	}
	u, _ := cint.U32From64(v)
	return u
}

// indexToName is ll_index_to_name: "*" for 0, the cache, ll_link_get by
// index, then "if%u".
func (r *runner) indexToName(index uint32) (string, error) {
	if index == 0 {
		return "*", nil
	}
	if e, ok := r.cache.byIndex[index]; ok {
		return e.name, nil
	}
	got, err := r.linkGet("", index)
	if err != nil {
		return "", err
	}
	if got == index {
		if e, ok := r.cache.byIndex[index]; ok {
			return e.name, nil
		}
	}
	return "if" + strconv.FormatUint(uint64(index), 10), nil
}

// nodev is lib/utils.c nodev: the message, and -1 to the caller.
func (r *runner) nodev(dev string) error {
	r.errf("Cannot find device \"%s\"\n", dev)
	return retCode(-1)
}

// nlmsgerrAttrMaxCst is NLMSGERR_ATTR_MAX (NLMSGERR_ATTR_MISS_NEST).
const nlmsgerrAttrMaxCst = 6

// nlmsgerrAttrMissTypeCst is NLMSGERR_ATTR_MISS_TYPE, which x/sys/unix lacks.
const nlmsgerrAttrMissTypeCst = 5
