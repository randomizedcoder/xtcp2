//go:build goip_write

package goip

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ErrBpfLoad is a BPF program bpf_parse_and_load_common could not parse or
// load, which lwt_parse_bpf's callers end with exit(-1). Its text is ip's
// stderr: the loader's lines, then "Failed to parse eBPF program: %s"
// with strerror(-err).
var ErrBpfLoad = errors.New("bpf")

const (
	// bpfDefaultSectionCst is bpf_prog_to_default_section for every lwt
	// type: __bpf_prog_meta's ELF_SECTION_PROG.
	bpfDefaultSectionCst = "prog"
	// bpfPinnedSectionCst is bpf_load_common's annotation section for a
	// pinned program.
	bpfPinnedSectionCst = "*fsobj"
	// bpfAnnotationMaxCst is bpf_load_common's char annotation[256]:
	// snprintf keeps 255 bytes and the NUL.
	bpfAnnotationMaxCst = 255
	bpfFailedPrefixCst  = "Failed to parse eBPF program: "
)

// bpfLoadCfg is bpf_cfg_in after bpf_do_parse (lib/bpf_legacy.c:828) for
// an lwt type: an object file or a pinned program, never cBPF, since
// bpf_cb_ops has no cbpf_cb.
type bpfLoadCfg struct {
	progType ebpf.ProgramType // lwt_parse_bpf's BPF_PROG_TYPE_LWT_*
	pinned   bool
	object   string // the file argument, an object or a pinned path
	section  string // `section`, else "prog"
	progName string // `program`, which libbpf matches over the section
}

// annotation is bpf_load_common's "%s:[%s]" of the object's basename (the
// POSIX basename of libgen.h, which path.Base is) and the section, or
// "*fsobj" for a pinned program, in its 256-byte buffer.
func (c *bpfLoadCfg) annotation() string {
	sec := c.section
	if c.pinned {
		sec = bpfPinnedSectionCst
	}
	a := path.Base(c.object) + ":[" + sec + "]"
	if len(a) > bpfAnnotationMaxCst {
		a = a[:bpfAnnotationMaxCst]
	}
	return a
}

// bpfLoad loads cfg's program and returns its descriptor, which stays open
// until goip exits, as ip's does. Tests replace it.
var bpfLoad = bpfLoadProg

// bpfFailed is a refusal ending with lwt_parse_bpf's "Failed to parse eBPF
// program", after the loader's own lines (each with its newline).
func bpfFailed(lines string, errno unix.Errno) error {
	return linkUsage(fmt.Errorf("%w: %s%s%s", ErrBpfLoad, lines, bpfFailedPrefixCst, cStrerror(errno)))
}

// cStrerror is glibc's strerror for the errnos the loader reports: Go's
// texts are glibc's with the first letter lowered.
func cStrerror(e unix.Errno) string {
	s := e.Error()
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// bpfProgArgs is bpf_do_parse from the mode word at args[i] for an lwt
// type, then the load (bpf_load_common): it returns the program and the
// index of the last word it used (PREV_ARG). The mode is `obj` or a prefix
// of `object-file`, else a prefix of `object-pinned`, `pinned` or `fd`;
// then the file, then `section` (any prefix) S, `program` P (strcmp) and
// `verbose` (any prefix), each at most once and in that order.
func bpfProgArgs(args []string, i int, progType ebpf.ProgramType) (xtcpnl.BpfProgRef, int, error) {
	cfg := bpfLoadCfg{progType: progType, section: bpfDefaultSectionCst}
	switch w := args[i]; {
	case matchesPrefix(w, "object-file") || w == "obj":
	case matchesPrefix(w, "object-pinned") || matchesPrefix(w, "pinned") || matchesPrefix(w, "fd"):
		cfg.pinned = true
	default:
		return xtcpnl.BpfProgRef{}, i, bpfFailed(fmt.Sprintf("What mode is %q?\n", w), unix.EPERM)
	}
	i, err := nextArg(args, i)
	if err != nil {
		return xtcpnl.BpfProgRef{}, i, err
	}
	cfg.object = args[i]
	i++
	if i < len(args) && matchesPrefix(args[i], "section") {
		if i, err = nextArg(args, i); err != nil {
			return xtcpnl.BpfProgRef{}, i, err
		}
		cfg.section = args[i]
		i++
	}
	if i < len(args) && args[i] == "program" {
		if i, err = nextArg(args, i); err != nil {
			return xtcpnl.BpfProgRef{}, i, err
		}
		cfg.progName = args[i]
		i++
	}
	if i < len(args) && matchesPrefix(args[i], "verbose") {
		// libbpf's verbose_print and the kernel's verifier log, which goip
		// has no copy of.
		return xtcpnl.BpfProgRef{}, i, fmt.Errorf("bpf verbose: %w", ErrNotImplemented)
	}
	fd, err := bpfLoad(cfg)
	if err != nil {
		return xtcpnl.BpfProgRef{}, i - 1, err
	}
	return xtcpnl.BpfProgRef{FD: fd, Name: cfg.annotation()}, i - 1, nil
}

// bpfHooks are parse_encap_bpf's hook words (strcmp), their nests and
// their program types.
var bpfHooks = map[string]struct {
	attr     uint16
	progType ebpf.ProgramType
}{
	"in":   {xtcpnl.LwtBpfIn, ebpf.LWTIn},
	"out":  {xtcpnl.LwtBpfOut, ebpf.LWTOut},
	"xmit": {xtcpnl.LwtBpfXmit, ebpf.LWTXmit},
}

// bpfEncapArgs is parse_encap_bpf (ip/iproute_lwtunnel.c:2222) from
// args[i]: `in`, `out` and `xmit` each load a program into their nest, as
// often as given, and `headroom` (get_unsigned, nonzero) is written the
// first time only; the first other word ends the encap. It returns the
// index of the last word used.
func bpfEncapArgs(args []string, i int) (xtcpnl.LwtEncap, int, error) {
	var e xtcpnl.BpfEncap
	headroomSet := false
	for ; i < len(args); i++ {
		w := args[i]
		if h, ok := bpfHooks[w]; ok {
			var err error
			if i, err = nextArg(args, i); err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
			p, j, err := bpfProgArgs(args, i, h.progType)
			if err != nil {
				return xtcpnl.LwtEncap{}, j, err
			}
			e.Items = append(e.Items, xtcpnl.BpfEncapItem{Kind: h.attr, Prog: p})
			i = j
			continue
		}
		switch w {
		case "headroom":
			var err error
			if i, err = nextArg(args, i); err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
			v, ok := getU32(args[i], 0)
			if !ok || v == 0 {
				return xtcpnl.LwtEncap{}, i, linkUsage(invargErr("headroom is invalid\n", args[i]))
			}
			if !headroomSet {
				e.Items = append(e.Items, xtcpnl.BpfEncapItem{Kind: xtcpnl.LwtBpfXmitHeadroom, Headroom: v})
			}
			headroomSet = true
		case addrHelpKeywordCst:
			return xtcpnl.LwtEncap{}, i, fmt.Errorf("encap bpf help: %w", ErrNotImplemented)
		default:
			return xtcpnl.LwtEncap{Bpf: &e}, i - 1, nil
		}
	}
	return xtcpnl.LwtEncap{Bpf: &e}, i - 1, nil
}
