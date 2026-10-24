package goip

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip netns` is do_netns (ip/ipnetns.c). goip implements its two netlink
// verbs: list-id, an RTM_GETNSID dump or get that print_nsid renders, and
// set, one RTM_NEWNSID. The rest (add, delete, exec and the others) are
// mounts, setns and /proc reads, not netlink, and are ErrNotImplemented.
//
// Both verbs name namespaces by their files under /run/netns, and the
// kernel by a descriptor of one: NETNSA_FD. list-id's netns_map_init first
// asks, file by file in directory order, for each one's nsid, so it can name
// the dump's peers. The descriptor is a slot in the asking process's table,
// so goip's is not ip's; the matrix accepts that one value (the allowlist's
// NETNSA_FD entries).

const (
	netnsListIDVerbCst = "list-id"
	netnsHelpVerbCst   = "help"
	netnsAddVerbCst    = "add"
	netnsSetVerbCst    = "set"
	netnsAutoCst       = "auto"
	netnsTargetKeyCst  = "target-nsid"
	netnsNsidKeyCst    = "nsid"
	// netnsNameMaxCst is NAME_MAX, invalid_name's length bound.
	netnsNameMaxCst = 255
	// netnsNullArgCst is how glibc prints the NULL argv[1] that list-id's
	// invarg passes when the negative value was the last word.
	netnsNullArgCst = "(null)"
)

// netnsRunDir is NETNS_RUN_DIR, a variable so the tests can point it at a
// directory of their own.
var netnsRunDir = "/run/netns"

var (
	// netnsListVerbs are do_netns's first arm, ahead of list-id, so `l` is
	// list.
	netnsListVerbs = []string{listVerbCst, showVerbCst, lstVerbCst}
	// netnsOtherVerbs are the verbs after set, none of them netlink.
	netnsOtherVerbs = []string{"delete", identifyVerbCst, pidsVerbCst, execVerbCst, monitorVerbCst, "attach"}
)

// runNetns is do_netns, its verbs matched with matches() in its order.
func runNetns(c *runCtx, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("netns list: %w", ErrNotImplemented)
	}
	if len(args) > 1 && netnsInvalidName(args[1]) {
		return fmt.Errorf("%w: Invalid netns name %q", ErrInvalidArgument, args[1])
	}
	switch v := args[0]; {
	case matchesAny(v, netnsListVerbs):
		return fmt.Errorf("netns %s: %w", v, ErrNotImplemented)
	case matchesPrefix(v, netnsListIDVerbCst):
		return netnsListID(c, service.New(c.src, c.nextSeq), args[1:])
	case matchesPrefix(v, netnsHelpVerbCst), matchesPrefix(v, netnsAddVerbCst):
		return fmt.Errorf("netns %s: %w", v, ErrNotImplemented)
	case matchesPrefix(v, netnsSetVerbCst):
		return netnsSet(c, args[1:])
	case matchesAny(v, netnsOtherVerbs):
		return fmt.Errorf("netns %s: %w", v, ErrNotImplemented)
	default:
		return fmt.Errorf("%w: Command %q is unknown, try \"ip netns help\"", ErrInvalidArgument, v)
	}
}

// netnsInvalidName is invalid_name, which do_netns applies to the word after
// the verb, whatever the verb.
func netnsInvalidName(name string) bool {
	return name == "" || len(name) > netnsNameMaxCst || strings.Contains(name, "/") ||
		name == "." || name == ".."
}

// netnsMap is netns_map_init's cache: nsid to name.
type netnsMap map[int32]string

// netnsNames is NETNS_RUN_DIR's entries in directory order, as readdir
// returns them. A directory that cannot be read is no names, as ip's
// opendir failing is.
func netnsNames() ([]string, error) {
	f, err := os.Open(netnsRunDir)
	if err != nil {
		return nil, err
	}
	names, rerr := f.Readdirnames(-1)
	if cerr := f.Close(); cerr != nil || rerr != nil {
		return nil, errors.Join(rerr, cerr)
	}
	return names, nil
}

// netnsNsid is netns_id_from_name: the file's nsid in this namespace, asked
// by descriptor. ok is false when the file will not open (no request), the
// kernel refuses, or the reply has no NETNSA_NSID; an unassigned nsid is
// negative and ok.
func netnsNsid(svc *service.Service, path string) (nsid int32, ok bool) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, false
	}
	ni, err := svc.NsidByFD(uint32(fd))
	// A failed close is EBADF, a bug in this file; the reply is not trusted
	// then.
	if cerr := unix.Close(fd); cerr != nil || err != nil || !ni.HasNSID {
		return 0, false
	}
	return ni.NSID, true
}

// netnsMapInit is netns_map_init: each name's nsid, the assigned ones
// cached, the first name for an nsid kept.
func netnsMapInit(svc *service.Service) netnsMap {
	m := netnsMap{}
	names, err := netnsNames()
	if err != nil {
		return m
	}
	for _, name := range names {
		nsid, ok := netnsNsid(svc, filepath.Join(netnsRunDir, name))
		if _, dup := m[nsid]; ok && nsid >= 0 && !dup {
			m[nsid] = name
		}
	}
	return m
}

// netnsGetName is netns_get_name: the directory again, name by name, until
// one's nsid is nsid. Each name asked is one more request.
func netnsGetName(svc *service.Service, nsid int32) (string, bool) {
	if nsid < 0 {
		return "", false
	}
	names, err := netnsNames()
	if err != nil {
		return "", false
	}
	for _, name := range names {
		if id, ok := netnsNsid(svc, filepath.Join(netnsRunDir, name)); ok && id >= 0 && id == nsid {
			return name, true
		}
	}
	return "", false
}

// netnsListIDArgs is netns_list_id's loop: `target-nsid` and `nsid` by
// strcmp, each once; any other word is usage.
type netnsListIDArgs struct {
	target, nsid int32
}

func (a *netnsListIDArgs) parse(args []string) error {
	a.target, a.nsid = -1, -1
	for i := 0; i < len(args); i++ {
		var dst *int32
		switch args[i] {
		case netnsTargetKeyCst:
			dst = &a.target
		case netnsNsidKeyCst:
			dst = &a.nsid
		default:
			return fmt.Errorf("%w: ip netns list-id [target-nsid POSITIVE-INT] [nsid POSITIVE-INT]", ErrInvalidArgument)
		}
		key := args[i]
		if *dst >= 0 {
			return dupargErr(key, key)
		}
		var err error
		if i, err = nextArg(args, i); err != nil {
			return err
		}
		v, ok := getInteger(args[i], 0)
		if !ok {
			return invargErr(fmt.Sprintf("%q value is invalid", key), args[i])
		}
		if v < 0 {
			// invarg is handed argv[1], the word after the value.
			next := netnsNullArgCst
			if i+1 < len(args) {
				next = args[i+1]
			}
			return invargErr(fmt.Sprintf("%q value should be >= 0", key), next)
		}
		*dst = int32(v)
	}
	return nil
}

// netnsListID is netns_list_id: netns_map_init's requests, then the get
// by nsid, or the dump.
func netnsListID(c *runCtx, svc *service.Service, args []string) error {
	m := netnsMapInit(svc)
	var a netnsListIDArgs
	if err := a.parse(args); err != nil {
		return err
	}
	hasTarget := a.target >= 0
	if a.nsid >= 0 {
		ni, err := svc.NsidGet(a.nsid, a.target, hasTarget)
		if err != nil {
			return err
		}
		views, perr := netnsViews(svc, m, []xtcpnl.NsidInfo{xtcpnl.NsidInfo(ni)})
		if err := netnsRender(c, views); err != nil {
			return err
		}
		return perr
	}
	dump, err := svc.Nsids(a.target, hasTarget)
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
	}
	nis := make([]xtcpnl.NsidInfo, len(dump))
	for i := range dump {
		nis[i] = xtcpnl.NsidInfo(dump[i])
	}
	views, perr := netnsViews(svc, m, nis)
	if err := netnsRender(c, views); err != nil {
		return err
	}
	return perr
}

// netnsViews is print_nsid over the replies: a reply with no NETNSA_NSID
// ends them, "BUG: NETNSA_NSID is missing", exit 1, after the lines before
// it. Each name is the cache's for the current nsid when the reply has one,
// else the nsid, taken out of the cache once printed; failing that the
// directory is walked again for the nsid, and a name found is cached.
func netnsViews(svc *service.Service, m netnsMap, nis []xtcpnl.NsidInfo) ([]render.NsidView, error) {
	views := make([]render.NsidView, 0, len(nis))
	for i := range nis {
		ni := &nis[i]
		if !ni.HasNSID {
			return views, fmt.Errorf("%w: BUG: NETNSA_NSID is missing", ErrDumpTerminated)
		}
		key := ni.NSID
		if ni.HasCurrentNSID {
			key = ni.CurrentNSID
		}
		// netnsMapInit caches no negative nsid, so an unassigned key misses.
		name, ok := m[key]
		if ok {
			delete(m, key)
		} else if name, ok = netnsGetName(svc, ni.NSID); ok {
			if _, dup := m[ni.NSID]; !dup {
				m[ni.NSID] = name
			}
		}
		views = append(views, render.NsidViewOf(ni, false, name))
	}
	return views, nil
}

// netnsRender prints the lines, or one JSON array of them.
func netnsRender(c *runCtx, views []render.NsidView) error {
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

// netnsSet is netns_set: the name, the nsid or `auto` (-1, the kernel's
// pick), then set_netns_id_from_name's RTM_NEWNSID on the name's file.
// Words after the nsid are ignored. A file that will not open is exit 1
// with nothing printed.
func netnsSet(c *runCtx, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: No netns name specified", ErrInvalidArgument)
	}
	if len(args) < 2 {
		return fmt.Errorf("%w: No nsid specified", ErrInvalidArgument)
	}
	nsid := int32(-1)
	if args[1] != netnsAutoCst {
		v, ok := getInteger(args[1], 0)
		switch {
		case !ok:
			return invargErr("Invalid \"netnsid\" value", args[1])
		case v < 0:
			return invargErr("\"netnsid\" value should be >= 0", args[1])
		}
		nsid = int32(v)
	}
	// set_netns_id_from_name opens a name with a slash as a path, but
	// do_netns's invalid_name has refused any such name already, short of
	// `ip -all`, which goip has not.
	path := filepath.Join(netnsRunDir, args[0])
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrInvalidArgument, path, err)
	}
	b, err := xtcpnl.BuildSetNsidRequest(uint32(fd), nsid, c.nextSeq())
	if cerr := unix.Close(fd); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("goip: build netns set request: %w", err)
	}
	return writeAddrOut(b)
}
