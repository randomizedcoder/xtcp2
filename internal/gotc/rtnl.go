package gotc

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This is the part of lib/libnetlink.c and lib/ll_map.c tc's shows run
// through: rtnl_dump_filter's loop and its error reports, and the link
// cache ll_init_map fills.

// llMaskCst is the IFLA_EXT_MASK of ll_init_map's dump: RTEXT_FILTER_VF |
// SKIP_STATS | NAME_ONLY.
const llMaskCst = xtcpnl.RTEXT_FILTER_VF | xtcpnl.RTEXT_FILTER_SKIP_STATS | xtcpnl.RtextFilterNameOnly

// llEntry is one cached link.
type llEntry struct {
	name  string
	flags uint32
	typ   uint16 // ifi_type, an ARPHRD_*
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
		return Reply{}, fmt.Errorf("gotc: building a request: %w", err)
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
	// The kernel always sends a DONE, so this is a truncated capture.
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

// nlmsgerrAttrMaxCst is NLMSGERR_ATTR_MAX (NLMSGERR_ATTR_MISS_NEST).
const nlmsgerrAttrMaxCst = 6

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
// tc calls it once, at the start of a batch; its failures exit 1.
func (r *runner) initMap() error {
	if r.llInited {
		return nil // static int initialized
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
