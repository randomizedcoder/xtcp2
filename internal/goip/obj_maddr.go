package goip

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// `ip maddress` is do_multiaddr (ip/ipmaddr.c, iproute2 7.3.0). Its show
// merges three sources, each kept in ifindex order and, within an index, in
// the order read: the link-layer groups from /proc/net/dev_mcast, then the
// RTM_GETMULTICAST dump of AF_INET, then of AF_INET6. A family's dump falls
// back to /proc/net/igmp or igmp6 when it fails or a reply has no
// IFA_MC_USERS (read_maddr_netlink); 7.2.0 read all three from procfs.
// add and delete are SIOCADDMULTI and SIOCDELMULTI ioctls, which goip does
// not send.

// ErrMaddrUsage is a refusal ipmaddr.c ends with exit(-1), 255: usage, an
// unknown command, a second device and NEXT_ARG's incomplete command.
var ErrMaddrUsage = errors.New("maddr")

// maddrVerbs is do_multiaddr (ip/ipmaddr.c:479-487), in its matches() order.
var maddrVerbs = chain([]verb{addVerb, deleteVerb}, listShowLst, []verb{helpVerb})

// maddrProcNet is where the procfs sources are read from; tests point it at
// a fixture.
var maddrProcNet = "/proc/net"

// maddrUsage is usage()'s first line.
const maddrUsage = "Usage: ip maddr [ add | del ] MULTIADDR dev STRING"

// runMaddr is do_multiaddr: a bare maddress is a list.
func runMaddr(c *runCtx, args []string) error {
	if len(args) > 0 {
		v, ok := resolveVerb(maddrVerbs, args[0])
		switch {
		case ok && v.name == helpVerbCst:
			return maddrUsageErr()
		case !ok:
			return fmt.Errorf("%w: Command %q is unknown, try \"ip maddr help\"", ErrMaddrUsage, args[0])
		case v.kind != verbShow:
			return fmt.Errorf("maddress %q: the ioctl: %w", v.name, ErrNotImplemented)
		}
		args = args[1:]
	}
	dev, err := maddrListArgs(args)
	if err != nil {
		return err
	}
	list, err := maddrList(c, dev)
	if err != nil {
		return err
	}
	return maddrPrint(c, list)
}

// maddrUsageErr is usage(), of which goip prints the first line.
func maddrUsageErr() error {
	return fmt.Errorf("%w: %s", ErrMaddrUsage, maddrUsage)
}

// maddrListArgs is multiaddr_list's loop: `dev` is optional before the name,
// `help` is the usage, and a second name is duparg2.
func maddrListArgs(args []string) (string, error) {
	var dev string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == devKeywordCst {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%w: %w", ErrMaddrUsage, incompleteErr())
			}
			i++
			a = args[i]
		} else if matchesPrefix(a, helpVerbCst) {
			return "", maddrUsageErr()
		}
		if dev != "" {
			return "", fmt.Errorf("%w: %w", ErrMaddrUsage, duparg2Err(devKeywordCst, a))
		}
		dev = a
	}
	return dev, nil
}

// maddrList is multiaddr_list after the arguments: a device ll_name_to_index
// cannot find has no groups, and is printed as none without a dump.
func maddrList(c *runCtx, dev string) ([]render.MaddrEntry, error) {
	var index int32
	if dev != "" {
		idx, err := genlNameToIndex(c, dev)
		if err != nil {
			return nil, err
		}
		if idx == 0 {
			return nil, nil
		}
		index = idx
	}
	fam := c.family
	var list []render.MaddrEntry
	if fam == unix.AF_UNSPEC || fam == unix.AF_PACKET {
		list = maddrInsert(list, readDevMcast(dev))
	}
	for _, f := range []uint8{unix.AF_INET, unix.AF_INET6} {
		if fam != unix.AF_UNSPEC && fam != f {
			continue
		}
		got, ok := maddrNetlink(c, f, index)
		if !ok {
			got = readIgmp(f, dev)
		}
		list = maddrInsert(list, got)
	}
	return list, nil
}

// maddrInsert is maddr_ins for each of add in turn: every entry goes after
// all those whose index is not above its own.
func maddrInsert(list, add []render.MaddrEntry) []render.MaddrEntry {
	for _, m := range add {
		i := slices.IndexFunc(list, func(e render.MaddrEntry) bool { return e.Index > m.Index })
		if i < 0 {
			i = len(list)
		}
		list = slices.Insert(list, i, m)
	}
	return list
}

// maddrNetlink is read_maddr_netlink and accept_maddr: one family's dump,
// filtered to index when it is set, each device named by ll_index_to_name.
// ok is false when the dump failed or a group came without IFA_MC_USERS,
// and procfs is read instead.
func maddrNetlink(c *runCtx, family uint8, index int32) ([]render.MaddrEntry, bool) {
	bodies, err := c.src.Dump(xtcpnl.BuildDumpMulticastRequest(family, cint.Bits(index), c.nextSeq()), uint16(unix.RTM_GETMULTICAST))
	if err != nil {
		return nil, false
	}
	var out []render.MaddrEntry
	usersMissing := false
	for _, b := range bodies {
		a, err := xtcpnl.ParseNewAddr(b)
		if err != nil {
			return nil, false
		}
		idx := cint.Int(a.Index)
		if index != 0 && idx != index {
			continue
		}
		if a.Multicast == nil || len(a.Multicast) > maddrDataMaxCst {
			continue
		}
		if !a.HasMcUsers {
			usersMissing = true
			continue
		}
		out = append(out, render.MaddrEntry{Index: idx, Name: genlIndexToName(c, idx),
			Family: a.Family, Addr: a.Multicast, Users: a.McUsers})
	}
	if usersMissing {
		return nil, false
	}
	return out, true
}

// maddrDataMaxCst is sizeof(inet_prefix.data), the longest IFA_MULTICAST
// accept_maddr keeps.
const maddrDataMaxCst = 256

// maddrProcLines is each line of one /proc/net file, or none when it cannot
// be opened, as the readers' fopen failure returns silently.
func maddrProcLines(name string) []string {
	f, err := os.OpenInRoot(maddrProcNet, name)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	return procLines(f)
}

// procLines splits r into lines.
func procLines(r io.Reader) []string {
	var lines []string
	s := bufio.NewScanner(r)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	return lines
}

// readDevMcast is read_dev_mcast: "index name users global_use hex" per
// line, dev_mc_seq_show's format, the name compared to dev's text.
func readDevMcast(dev string) []render.MaddrEntry {
	var out []render.MaddrEntry
	for _, l := range maddrProcLines("dev_mcast") {
		f := strings.Fields(l)
		if len(f) < 5 || (dev != "" && f[1] != dev) {
			continue
		}
		idx, e1 := strconv.ParseInt(f[0], 10, 32)
		users, e2 := strconv.ParseUint(f[2], 10, 32)
		st, e3 := strconv.Atoi(f[3])
		addr, ok := maddrParseHex(f[4])
		if e1 != nil || e2 != nil || e3 != nil || !ok {
			continue
		}
		out = append(out, render.MaddrEntry{Index: cint.LongToInt(idx), Name: f[1], Family: unix.AF_PACKET,
			Addr: addr, Users: cint.Low32(users), Static: st != 0})
	}
	return out
}

// maddrParseHex is parse_hex: two hex digits per byte, an odd length
// refused. The kernel prints lowercase pairs; sscanf's sign and one-digit
// pairs are not reproduced.
func maddrParseHex(s string) ([]byte, bool) {
	if len(s)%2 != 0 {
		return nil, false
	}
	out := make([]byte, 0, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		v, err := strconv.ParseUint(s[i:i+2], 16, 8)
		if err != nil {
			return nil, false
		}
		out = append(out, byte(v))
	}
	return out, true
}

// readIgmp is read_igmp or read_igmp6, the procfs fallback for family.
func readIgmp(family uint8, dev string) []render.MaddrEntry {
	if family == unix.AF_INET6 {
		return readIgmp6(dev)
	}
	var out []render.MaddrEntry
	lines := maddrProcLines("igmp")
	if len(lines) == 0 {
		return nil
	}
	var idx int64
	var name string
	// The first line is the column header.
	for _, l := range lines[1:] {
		f := strings.Fields(l)
		if !strings.HasPrefix(l, "\t") {
			// A device: "index\tname: count querier", the colon on the
			// name when it fills its column.
			if len(f) < 2 {
				continue
			}
			if v, err := strconv.ParseInt(f[0], 10, 32); err == nil {
				idx = v
			}
			name = strings.TrimSuffix(f[1], ":")
			continue
		}
		if (dev != "" && name != dev) || len(f) < 2 {
			continue
		}
		group, e1 := strconv.ParseUint(f[0], 16, 32)
		users, e2 := strconv.ParseUint(f[1], 10, 32)
		if e1 != nil || e2 != nil {
			continue
		}
		// The kernel prints the __be32 as a host-order %08X, and sscanf
		// reads it back into the same bytes.
		addr := binary.NativeEndian.AppendUint32(nil, uint32(group))
		out = append(out, render.MaddrEntry{Index: cint.LongToInt(idx), Name: name, Family: unix.AF_INET,
			Addr: addr, Users: cint.Low32(users)})
	}
	return out
}

// readIgmp6 is read_igmp6: "index name hex users flags timer" per line.
func readIgmp6(dev string) []render.MaddrEntry {
	var out []render.MaddrEntry
	for _, l := range maddrProcLines("igmp6") {
		f := strings.Fields(l)
		if len(f) < 4 || (dev != "" && f[1] != dev) {
			continue
		}
		idx, e1 := strconv.ParseInt(f[0], 10, 32)
		users, e2 := strconv.ParseUint(f[3], 10, 32)
		addr, ok := maddrParseHex(f[2])
		if e1 != nil || e2 != nil || !ok {
			continue
		}
		out = append(out, render.MaddrEntry{Index: cint.LongToInt(idx), Name: f[1], Family: unix.AF_INET6,
			Addr: addr, Users: cint.Low32(users)})
	}
	return out
}

// maddrPrint is print_mlist.
func maddrPrint(c *runCtx, list []render.MaddrEntry) error {
	if c.json {
		b, err := render.MaddrJSON(list)
		if err != nil {
			return err
		}
		_, err = c.out.Write(b)
		return err
	}
	_, err := io.WriteString(c.out, render.MaddrText(list))
	return err
}
