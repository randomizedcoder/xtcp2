package goip

import (
	"errors"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip tuntap` is do_iptuntap (ip/iptuntap.c, iproute2 v7.2.0): the verb by
// prefix, no verb being show. show ignores its arguments (T13), dumps the
// links with an IFLA_INFO_KIND "tun" filter, and prints each ARPHRD_NONE
// or ARPHRD_ETHER link of kind tun whose three sysfs properties read_prop
// parses, the processes holding it under -d. goip reads sysfs and /proc
// through c.sys (internal/tnlsys); add and delete are writes it does not
// make.

const (
	tuntapKindCst = "tun"

	tuntapUsageCst = "Usage: ip tuntap { add | del | show | list | lst | help } [ dev PHYS_DEV ]\n" +
		"       [ mode { tun | tap } ] [ user USER ] [ group GROUP ]\n" +
		"       [ one_queue ] [ pi ] [ vnet_hdr ] [ multi_queue ] [ name NAME ]\n" +
		"\n" +
		"Where: USER  := { STRING | NUMBER }\n" +
		"       GROUP := { STRING | NUMBER }"

	// taskNameBufCst is get_task_name's SPRINT_BUF: fgets keeps 63 bytes.
	taskNameBufCst = 64
)

// runTuntap is do_iptuntap.
func runTuntap(c *runCtx, args []string) error {
	if len(args) == 0 {
		return tuntapShow(c)
	}
	verb := args[0]
	switch {
	case matchesPrefix(verb, addrVerbAddCst), matchesPrefix(verb, addrVerbDeleteCst):
		return fmt.Errorf("tuntap %q: a TUNSETIFF write: %w", verb, ErrNotImplemented)
	case matchesPrefix(verb, showVerbCst), matchesPrefix(verb, lstVerbCst), matchesPrefix(verb, listVerbCst):
		return tuntapShow(c)
	case matchesPrefix(verb, tnlHelpCst):
		return fmt.Errorf("%w: %s", ErrTunnelUsage, tuntapUsageCst)
	}
	return tnlUsageMsg(fmt.Sprintf("Command %q is unknown, try \"ip tuntap help\".", verb))
}

// tuntapShow is do_show: the filtered dump, then print_tuntap for each
// reply, in new_json_obj's array under -j.
func tuntapShow(c *runCtx) error {
	req, err := xtcpnl.BuildDumpLinkRequestKind(unix.AF_UNSPEC, tuntapKindCst, c.nextSeq())
	if err != nil {
		return err
	}
	pr := render.NewPrinter(c.json)
	pr.NewObj()
	err = tuntapWalk(c, pr, req)
	pr.DeleteObj()
	if _, werr := c.out.Write(pr.Bytes()); werr != nil {
		return werr
	}
	return err
}

// tuntapWalk is rtnl_dump_filter over print_tuntap.
func tuntapWalk(c *runCtx, pr *render.Printer, req []byte) error {
	bodies, err := c.src.Dump(req, unix.RTM_NEWLINK)
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrTunnelRefused, err)
	}
	for _, body := range bodies {
		li, err := xtcpnl.ParseNewLink(body)
		if err != nil {
			return fmt.Errorf("%w: Dump terminated: %w", ErrTunnelRefused, err)
		}
		if err := printTuntap(c, pr, &li); err != nil {
			return err
		}
	}
	return nil
}

// printTuntap is print_tuntap after its length check.
func printTuntap(c *runCtx, pr *render.Printer, li *xtcpnl.LinkInfo) error {
	if li.Type != unix.ARPHRD_NONE && li.Type != unix.ARPHRD_ETHER {
		return nil
	}
	if li.Name == "" || li.Kind != tuntapKindCst {
		return nil
	}
	t := render.Tuntap{Name: li.Name, Owner: -1, Group: -1, Details: c.showDetails > 0}
	for _, f := range []struct {
		prop string
		v    *int64
	}{{"tun_flags", &t.Flags}, {"owner", &t.Owner}, {groupKeywordCst, &t.Group}} {
		ok, err := readProp(c, li.Name, f.prop, f.v)
		if err != nil || !ok {
			return err
		}
	}
	if t.Details {
		procs, err := tunProcesses(c, li.Name)
		if err != nil {
			return err
		}
		t.Procs = procs
	}
	render.PrintTuntap(pr, &t, tnlSL(c))
	return nil
}

// readProp is lib/utils.c read_prop: ok with the value, or false after
// its lines on stderr. err is a source error, a replay gap.
func readProp(c *runCtx, dev, prop string, v *int64) (bool, error) {
	fname := "/sys/class/net/" + dev + "/" + prop
	content, err := c.sys.ReadProp(dev, prop)
	if err != nil {
		var e unix.Errno
		if !errors.As(err, &e) {
			return false, err
		}
		_, werr := fmt.Fprintf(c.errOut, "fopen %s: %s\n", fname, cStrerror(e))
		return false, werr
	}
	var msg string
	line, ok := xtcpnl.SysfsPropLine(content)
	n, perr := xtcpnl.ParseSysfsLong(content)
	switch {
	case !ok:
		msg = fmt.Sprintf("property \"%s\" in file %s is currently unknown\n", prop, fname)
	case errors.Is(perr, xtcpnl.ErrSysfsNotNumber):
		msg = fmt.Sprintf("value \"%s\" in file %s is not a number\n", line, fname)
	case errors.Is(perr, xtcpnl.ErrSysfsRange):
		msg = fmt.Sprintf("strtol %s: %s", fname, cStrerror(unix.ERANGE))
	case perr != nil:
		return false, perr
	default:
		*v = n
		return true, nil
	}
	_, werr := fmt.Fprintf(c.errOut, "%sFailed to parse %s\n", msg, fname)
	return false, werr
}

// tunProcesses is show_processes: each /dev/net/tun descriptor whose
// fdinfo has an "iff" naming the device, as the process's comm and pid.
func tunProcesses(c *runCtx, name string) ([]render.TunProc, error) {
	fds, err := c.sys.TunFDs()
	if err != nil {
		return nil, err
	}
	var out []render.TunProc
	for _, fd := range fds {
		for _, iff := range fdinfoIffs(fd.FdInfo) {
			if iff == name {
				out = append(out, render.TunProc{Name: taskName(fd), Pid: fd.Pid})
			}
		}
	}
	return out, nil
}

// taskName is get_task_name's answer as show_processes prints it: comm's
// first line, at most 63 bytes, or "<NULL>" when it cannot be read.
func taskName(fd tnlsys.TunFD) string {
	if fd.NoComm || fd.Pid == 0 || fd.Comm == "" {
		return "<NULL>"
	}
	s := fd.Comm
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i+1]
	}
	if len(s) > taskNameBufCst-1 {
		s = s[:taskNameBufCst-1]
	}
	if i := strings.IndexAny(s, "\n\x00"); i >= 0 {
		s = s[:i]
	}
	return s
}

// fdinfoIffs is show_processes' fscanf("%m[^:]: %ms\n") loop over an
// fdinfo file: the value of each pair whose key is "iff". The scan stops
// where fscanf would return less than two conversions and then meet the
// end of the file; a key fscanf cannot match (a ':' where the key should
// start) would loop forever in ip, and ends the scan here.
func fdinfoIffs(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		colon := strings.IndexByte(s[i:], ':')
		if colon <= 0 {
			break
		}
		key := s[i : i+colon]
		i += colon + 1
		for i < len(s) && cIsSpace(s[i]) {
			i++
		}
		j := i
		for j < len(s) && !cIsSpace(s[j]) {
			j++
		}
		if j == i {
			break
		}
		if key == "iff" {
			out = append(out, s[i:j])
		}
		i = j
		for i < len(s) && cIsSpace(s[i]) {
			i++
		}
	}
	return out
}
