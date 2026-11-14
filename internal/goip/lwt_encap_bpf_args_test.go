//go:build goip_write

package goip

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestBpfEncapArgs is parse_encap_bpf and bpf_do_parse over the workbench's
// object (bpfTestObjCst), with the kernel's load faked: the items written,
// the index of the last word used, the programs loaded and their types, or
// ip's refusal.
func TestBpfEncapArgs(t *testing.T) {
	const obj = bpfRowObjCst
	long := "/p/" + strings.Repeat("x", 300)
	prog := func(kind uint16, sec string) xtcpnl.BpfEncapItem {
		return xtcpnl.BpfEncapItem{Kind: kind, Prog: xtcpnl.BpfProgRef{FD: bpfFakeFDCst, Name: "lwt_bpf.o:[" + sec + "]"}}
	}
	head := func(v uint32) xtcpnl.BpfEncapItem {
		return xtcpnl.BpfEncapItem{Kind: xtcpnl.LwtBpfXmitHeadroom, Headroom: v}
	}
	in, out, xmit := xtcpnl.LwtBpfIn, xtcpnl.LwtBpfOut, xtcpnl.LwtBpfXmit
	tests := []struct {
		description string
		args        string
		expected    []xtcpnl.BpfEncapItem
		last        int
		loads       []bpfFakeLoad
		err         error
		errText     string
	}{
		{description: "positive: in", args: "in obj " + obj + " sec lwt_in dev d0",
			expected: []xtcpnl.BpfEncapItem{prog(in, "lwt_in")}, last: 4,
			loads: []bpfFakeLoad{{"pass_in", ebpf.LWTIn}}},
		{description: "positive: all three hooks and a headroom, in argv order", args: "xmit obj " + obj + " sec lwt_xmit headroom 16 in obj " +
			obj + " sec lwt_in out obj " + obj + " sec lwt_out",
			expected: []xtcpnl.BpfEncapItem{prog(xmit, "lwt_xmit"), head(16), prog(in, "lwt_in"), prog(out, "lwt_out")}, last: 16,
			loads: []bpfFakeLoad{{"pass_xmit", ebpf.LWTXmit}, {"pass_in", ebpf.LWTIn}, {"pass_out", ebpf.LWTOut}}},
		{description: "positive: object-file's prefix and section's", args: "out o " + obj + " s lwt_out",
			expected: []xtcpnl.BpfEncapItem{prog(out, "lwt_out")}, last: 4,
			loads: []bpfFakeLoad{{"pass_out", ebpf.LWTOut}}},
		{description: "positive: program by name, which keeps the default section in the name", args: "in obj " + obj + " program pass_out",
			expected: []xtcpnl.BpfEncapItem{prog(in, "prog")}, last: 4,
			loads: []bpfFakeLoad{{"pass_out", ebpf.LWTIn}}},
		{description: "positive: a pinned program", args: "in pinned /sys/fs/bpf/p",
			expected: []xtcpnl.BpfEncapItem{{Kind: in, Prog: xtcpnl.BpfProgRef{FD: 9, Name: "p:[*fsobj]"}}}, last: 2},
		{description: "corner: a hook takes another hook's section, loaded as its own type", args: "in obj " + obj + " sec lwt_xmit",
			expected: []xtcpnl.BpfEncapItem{prog(in, "lwt_xmit")}, last: 4,
			loads: []bpfFakeLoad{{"pass_xmit", ebpf.LWTIn}}},
		{description: "corner: a hook twice is written twice", args: "in obj " + obj + " sec lwt_in in obj " + obj + " sec lwt_out",
			expected: []xtcpnl.BpfEncapItem{prog(in, "lwt_in"), prog(in, "lwt_out")}, last: 9,
			loads: []bpfFakeLoad{{"pass_in", ebpf.LWTIn}, {"pass_out", ebpf.LWTIn}}},
		{description: "corner: only the first headroom is written", args: "headroom 8 headroom 9",
			expected: []xtcpnl.BpfEncapItem{head(8)}, last: 3},
		{description: "corner: headroom alone is the kernel's to refuse", args: "headroom 16 dev d0",
			expected: []xtcpnl.BpfEncapItem{head(16)}, last: 1},
		{description: "boundary: headroom's u32 maximum", args: "headroom 0xffffffff",
			expected: []xtcpnl.BpfEncapItem{head(0xffffffff)}, last: 1},
		{description: "boundary: the name is cut to bpf_load_common's 255 bytes", args: "in pinned " + long,
			expected: []xtcpnl.BpfEncapItem{{Kind: in, Prog: xtcpnl.BpfProgRef{FD: 9, Name: (strings.Repeat("x", 300) + ":[*fsobj]")[:255]}}}, last: 2},
		{description: "negative: headroom 0", args: "headroom 0", err: ErrInvalidArgument, errText: "headroom is invalid"},
		{description: "boundary: headroom past u32", args: "headroom 4294967296", err: ErrInvalidArgument, errText: "headroom is invalid"},
		{description: "negative: a section the object doesn't have", args: "in obj " + obj + " sec nosuch", err: ErrBpfLoad,
			errText: "object file doesn't contain sec nosuch\nFailed to parse eBPF program: No such file or directory"},
		{description: "negative: no section is prog, which the object doesn't have", args: "in obj " + obj, err: ErrBpfLoad,
			errText: "object file doesn't contain sec prog\n"},
		{description: "negative: a program the object doesn't have", args: "in obj " + obj + " program nosuch", err: ErrBpfLoad,
			errText: "object file doesn't contain prog nosuch\nFailed to parse eBPF program: No such file or directory"},
		{description: "negative: an object that doesn't open", args: "in obj /nonexistent/lwt.o sec lwt_in", err: ErrBpfLoad,
			errText: "Error opening object /nonexistent/lwt.o: No such file or directory\nCannot initialize ELF context!\n" +
				"Failed to parse eBPF program: Operation not permitted"},
		{description: "negative: a pin that doesn't exist", args: "in pinned /sys/fs/bpf/nosuch", err: ErrBpfLoad,
			errText: "Couldn't retrieve pinned program '/sys/fs/bpf/nosuch': No such file or directory\n" +
				"Failed to parse eBPF program: Operation not permitted"},
		{description: "negative: a cBPF mode, which lwt has no callback for", args: "in bc 1", err: ErrBpfLoad,
			errText: "What mode is \"bc\"?\nFailed to parse eBPF program: Operation not permitted"},
		{description: "negative: a mode with no file", args: "in obj", err: ErrInvalidArgument, errText: "Command line is not complete"},
		{description: "negative: a hook with no mode", args: "in", err: ErrInvalidArgument, errText: "Command line is not complete"},
		{description: "negative: section with no value", args: "in obj " + obj + " sec", err: ErrInvalidArgument,
			errText: "Command line is not complete"},
		{description: "corner: verbose, whose verifier log goip doesn't have", args: "in obj " + obj + " sec lwt_in verbose",
			err: ErrNotImplemented, errText: "bpf verbose"},
		{description: "corner: a pin in bpf_obj_get's work directory", args: "in pinned m:p", err: ErrNotImplemented, errText: "m:p"},
		{description: "corner: help", args: "help", err: ErrNotImplemented, errText: "encap bpf help"},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			loads := useFakeBpf(t, map[string]uint32{"/sys/fs/bpf/p": 9, long: 9})
			got, last, err := bpfEncapArgs(strings.Fields(tt.args), 0)
			if tt.err != nil {
				if !errors.Is(err, tt.err) || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("err = %v, want %v with %q", err, tt.err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Bpf == nil || !reflect.DeepEqual(got.Bpf.Items, tt.expected) {
				t.Errorf("items = %+v, want %+v", got.Bpf, tt.expected)
			}
			if last != tt.last {
				t.Errorf("last = %d, want %d", last, tt.last)
			}
			if !reflect.DeepEqual(*loads, tt.loads) && (len(*loads) > 0 || len(tt.loads) > 0) {
				t.Errorf("loads = %v, want %v", *loads, tt.loads)
			}
		})
	}
}

// TestBpfCheckELF is bpf_elf_ctx_init's refusals before libbpf opens the
// object: a file that isn't ELF, and an ELF that isn't an eBPF object.
func TestBpfCheckELF(t *testing.T) {
	obj, err := os.ReadFile(bpfTestObjCst)
	if err != nil {
		t.Fatal(err)
	}
	// The workbench's object with one header field changed: e_type (offset
	// 16) or e_machine (offset 18).
	patched := func(off int, v uint16) []byte {
		b := bytes.Clone(obj)
		binary.LittleEndian.PutUint16(b[off:], v)
		return b
	}
	tests := []struct {
		description string
		obj         []byte
		expected    string // "" for no error
	}{
		{description: "positive: the workbench's object", obj: obj},
		{description: "negative: not an ELF file", obj: []byte("not an object\n"),
			expected: "Cannot initialize ELF context!\nFailed to parse eBPF program: Invalid argument"},
		{description: "negative: a relocatable object for another machine", obj: patched(18, uint16(elf.EM_X86_64)),
			expected: "ELF format error, ELF file not for eBPF?\nCannot initialize ELF context!\nFailed to parse eBPF program: Invalid argument"},
		{description: "negative: an executable, not a relocatable object", obj: patched(16, uint16(elf.ET_EXEC)),
			expected: "ELF format error, ELF file not for eBPF?\nCannot initialize ELF context!\nFailed to parse eBPF program: Invalid argument"},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			err := bpfCheckELF(bytes.NewReader(tt.obj))
			switch {
			case tt.expected == "" && err != nil:
				t.Fatalf("err = %v", err)
			case tt.expected != "" && (!errors.Is(err, ErrBpfLoad) || !strings.HasSuffix(err.Error(), tt.expected)):
				t.Fatalf("err = %v, want %q", err, tt.expected)
			}
		})
	}
}

// TestSeg6LocalEndpoint is seg6local's `endpoint`: lwt_parse_bpf into
// SEG6_LOCAL_BPF as BPF_PROG_TYPE_LWT_SEG6LOCAL, and duparg2 on a second.
func TestSeg6LocalEndpoint(t *testing.T) {
	obj := bpfRowObjCst
	tests := []struct {
		description string
		args        string
		expected    []xtcpnl.Seg6LocalItem
		last        int
		loads       []bpfFakeLoad
		err         error
		errText     string
	}{
		{description: "positive: End.BPF's endpoint", args: "encap seg6local action End.BPF endpoint obj " + obj + " sec lwt_in dev d0",
			expected: []xtcpnl.Seg6LocalItem{{Kind: xtcpnl.Seg6LocalAction, U32: seg6LocalActionEndBPFCst},
				{Kind: xtcpnl.Seg6LocalBPF, Prog: xtcpnl.BpfProgRef{FD: bpfFakeFDCst, Name: "lwt_bpf.o:[lwt_in]"}}},
			last: 8, loads: []bpfFakeLoad{{"pass_in", ebpf.LWTSeg6Local}}},
		{description: "negative: endpoint twice", args: "encap seg6local action End.BPF endpoint obj " + obj + " sec lwt_in endpoint obj x",
			err: ErrInvalidArgument, errText: `either "endpoint" is duplicate, or "obj" is garbage`},
		{description: "negative: endpoint with nothing after it", args: "encap seg6local action End.BPF endpoint",
			err: ErrInvalidArgument, errText: "Command line is not complete"},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			loads := useFakeBpf(t, nil)
			got, last, err := lwtEncapArgs(strings.Fields(tt.args), 0, nil)
			if tt.err != nil {
				if !errors.Is(err, tt.err) || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("err = %v, want %v with %q", err, tt.err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Seg6Local == nil || !reflect.DeepEqual(got.Seg6Local.Items, tt.expected) {
				t.Errorf("items = %+v, want %+v", got.Seg6Local, tt.expected)
			}
			if last != tt.last {
				t.Errorf("last = %d, want %d", last, tt.last)
			}
			if !reflect.DeepEqual(*loads, tt.loads) {
				t.Errorf("loads = %v, want %v", *loads, tt.loads)
			}
		})
	}
}
