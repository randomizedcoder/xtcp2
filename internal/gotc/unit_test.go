package gotc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"golang.org/x/sys/unix"
)

// fakeSource answers requests from a script, in order, and records them.
type fakeSource struct {
	replies []Reply
	reqs    [][]byte
}

var errScript = errors.New("fake: no reply scripted")

func (f *fakeSource) Talk(req []byte) (Reply, error) {
	f.reqs = append(f.reqs, req)
	if len(f.replies) == 0 {
		return Reply{}, fmt.Errorf("%w: request %d", errScript, len(f.reqs))
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r, nil
}

func rep(m ...Msg) Reply { return Reply{Msgs: m} }

// done is a dump's NLMSG_DONE with error 0.
func done() Msg { return Msg{Type: unix.NLMSG_DONE, Flags: unix.NLM_F_MULTI, Body: make([]byte, 4)} }

// negErrno is -errno as the kernel's int, in its unsigned bits.
func negErrno(e unix.Errno) uint32 {
	v, _ := cint.U32(int(e))
	return -v
}

// nlErr is an NLMSG_ERROR of errno, with the request's header after it.
func nlErr(e unix.Errno) Msg {
	b := binary.LittleEndian.AppendUint32(nil, negErrno(e))
	return Msg{Type: unix.NLMSG_ERROR, Body: append(b, make([]byte, nlHeaderLen)...)}
}

// emptySys is a root with none of tc's files.
func emptySys(t *testing.T) sysFS {
	t.Helper()
	return sysFS{root: t.TempDir(), usrDir: confUsrDirCst}
}

// writeFiles puts each path's content under root.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

const (
	tcUsage = "Usage:\ttc [ OPTIONS ] OBJECT { COMMAND | help }\n" +
		"\ttc [-force] -batch filename\n" +
		"where  OBJECT := { qdisc | class | filter | chain |\n" +
		"\t\t    action | monitor | exec }\n" +
		"       OPTIONS := { -V[ersion] | -s[tatistics] | -d[etails] | -r[aw] |\n" +
		"\t\t    -o[neline] | -j[son] | -p[retty] | -c[olor]\n" +
		"\t\t    -b[atch] [filename] | -n[etns] name | -N[umeric] |\n" +
		"\t\t     -nm | -nam[es] | { -cf | -conf } path\n" +
		"\t\t     -br[ief] | -echo }\n"
	notImpl = "gotc: not implemented: "
)

// TestGotcOptions runs tc.c main's option loop and do_cmd's dispatch:
// the flags each option sets, the words matches() lets through, and what
// each refusal prints and returns.
//
// go test ./internal/gotc/ -run TestGotcOptions
func TestGotcOptions(t *testing.T) {
	t.Parallel()
	type flags struct {
		stats, details, raw, iec, numeric, brief int
		json, pretty, graph, names               bool
		sl                                       string
	}
	type want struct {
		stdout, stderr string
		rc, reqs       int
		flags          *flags // nil: not checked
	}
	missing := filepath.Join(t.TempDir(), "nosuch")
	tests := []struct {
		description string
		args        []string
		replies     []Reply
		expected    want
	}{
		// Positive.
		// A qdisc list with no dev dumps the links (ll_init_map), then the
		// qdiscs; a class list dumps the classes alone.
		{"positive: -s -d -j set the flags and reach qdisc", []string{"-s", "-d", "-j", "qdisc", "show"}, []Reply{rep(done()), rep(done())},
			want{stdout: "[]\n", reqs: 2, flags: &flags{stats: 1, details: 1, json: true, sl: "\n"}}},
		{"positive: -iec", []string{"-iec", "qdisc"}, []Reply{rep(done()), rep(done())},
			want{reqs: 2, flags: &flags{iec: 1, sl: "\n"}}},
		{"positive: -stats and -statistics both count", []string{"-statistics", "-stats", "class", "show"}, []Reply{rep(done())},
			want{reqs: 1, flags: &flags{stats: 2, sl: "\n"}}},
		{"positive: -r -p -g -N -br -o -nm", []string{"-r", "-p", "-g", "-N", "-br", "-o", "-j", "q", "help"}, nil,
			want{stderr: qdiscUsageText, flags: &flags{raw: 1, pretty: true, graph: true, numeric: 1, brief: 1, json: true, sl: "\\"}}},
		{"positive: -Version", []string{"-Version"}, nil,
			want{stdout: "tc utility, iproute2-7.2.0, libbpf 1.8.0\n"}},
		{"positive: -help is usage, rc 0", []string{"-help", "qdisc"}, nil, want{stderr: tcUsage}},
		{"positive: the help object", []string{"help"}, nil, want{stderr: tcUsage}},
		{"positive: class help", []string{"class", "help"}, nil, want{stderr: classUsageText}},
		// Negative.
		{"negative: an unknown option", []string{"-zz", "qdisc"}, nil,
			want{stderr: "Option \"-zz\" is unknown, try \"tc -help\".\n", rc: 255}},
		{"negative: an unknown object", []string{"bogus"}, nil,
			want{stderr: "Object \"bogus\" is unknown, try \"tc help\".\n", rc: 255}},
		{"negative: filter show is refused, no request sent", []string{"filter", "show"}, nil,
			want{stderr: notImpl + "tc filter\n", rc: 255}},
		{"negative: actions, monitor, exec and chain are refused", []string{"mon"}, nil,
			want{stderr: notImpl + "tc monitor\n", rc: 255}},
		{"negative: a qdisc write is refused", []string{"qdisc", "add", "dev", "d0", "root", "fq"}, nil,
			want{stderr: notImpl + "tc qdisc add\n", rc: 255}},
		{"negative: a class write is refused", []string{"class", "change"}, nil,
			want{stderr: notImpl + "tc class change\n", rc: 255}},
		{"negative: class has no link verb", []string{"class", "link"}, nil,
			want{stderr: "Command \"link\" is unknown, try \"tc class help\".\n", rc: 255}},
		{"negative: an unknown qdisc command", []string{"qdisc", "bogus"}, nil,
			want{stderr: "Command \"bogus\" is unknown, try \"tc qdisc help\".\n", rc: 255}},
		{"negative: -nm with a -cf file that is not there", []string{"-cf", missing, "-nm", "qdisc", "show"}, nil,
			want{stderr: "Can't open class names file: " + missing + "\n", rc: 255}},
		// Boundary.
		{"boundary: no arguments is usage, rc 0", nil, nil, want{stderr: tcUsage}},
		{"boundary: options and no object is usage, rc 0", []string{"-s", "-j"}, nil, want{stderr: tcUsage}},
		{"boundary: -batch with no file", []string{"-batch"}, nil,
			want{stderr: "Error: argument \"batch file\" is required\n", rc: 255}},
		{"boundary: -netns with no name opens \"(null)\"", []string{"-netns"}, nil,
			want{stderr: "Cannot open network namespace \"(null)\": No such file or directory\n", rc: 255}},
		{"boundary: an empty object matches nothing", []string{""}, nil,
			want{stderr: "Object \"\" is unknown, try \"tc help\".\n", rc: 255}},
		// Corner.
		{"corner: -c is -cf, so qdisc is the file and show the object (Q24)", []string{"-c", "qdisc", "show"}, nil,
			want{stderr: "Object \"show\" is unknown, try \"tc help\".\n", rc: 255}},
		{"corner: -b is -batch, not -brief", []string{"-b", missing}, []Reply{rep(done())},
			want{stderr: "Cannot open file \"" + missing + "\" for reading: No such file or directory\n", rc: 1, reqs: 1}},
		{"corner: --json is unknown: tc gives -- no meaning", []string{"--json", "qdisc"}, nil,
			want{stderr: "Option \"--json\" is unknown, try \"tc -help\".\n", rc: 255}},
		{"corner: a lone - is -stats", []string{"-", "qdisc", "help"}, nil,
			want{stderr: qdiscUsageText, flags: &flags{stats: 1, sl: "\n"}}},
		{"corner: -color=bogus is no color option, so unknown", []string{"-color=bogus", "qdisc"}, nil,
			want{stderr: "Option \"-color=bogus\" is unknown, try \"tc -help\".\n", rc: 255}},
		{"corner: -c=never is a color option, not -cf", []string{"-c=never", "qdisc", "help"}, nil, want{stderr: qdiscUsageText}},
		{"corner: -echo is strcmp, so -ech is unknown", []string{"-ech"}, nil,
			want{stderr: "Option \"-ech\" is unknown, try \"tc -help\".\n", rc: 255}},
		{"corner: -t and -ts are the timestamps", []string{"-t", "-ts", "-echo", "qdisc", "help"}, nil, want{stderr: qdiscUsageText}},
		{"corner: -cf at the end is NULL, so -nm loads only the defaults", []string{"-nm", "q", "help", "-cf"}, nil,
			want{stderr: qdiscUsageText, flags: &flags{names: true, sl: "\n"}}},
		{"corner: help as a verb takes any prefix of help", []string{"qdisc", "h"}, nil, want{stderr: qdiscUsageText}},
		{"corner: qdisc l and li are link, a write tested before list", []string{"qdisc", "li"}, nil,
			want{stderr: notImpl + "tc qdisc li\n", rc: 255}},
		{"corner: qdisc lis and ls are list", []string{"qdisc", "ls"}, []Reply{rep(done()), rep(done())}, want{reqs: 2}},
		{"corner: class l is list: class has no link", []string{"class", "l"}, []Reply{rep(done())}, want{reqs: 1}},
		{"corner: words after an option-looking object are the object's", []string{"qdisc", "-s"}, nil,
			want{stderr: "Command \"-s\" is unknown, try \"tc qdisc help\".\n", rc: 255}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			cfg := config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)}
			r, rc := runWith(cfg, tc.args, &out, &errb)
			got := want{stdout: out.String(), stderr: errb.String(), rc: rc, reqs: len(src.reqs)}
			if tc.expected.flags != nil {
				got.flags = &flags{stats: r.stats, details: r.details, raw: r.raw, iec: r.iec, numeric: r.numeric,
					brief: r.brief, json: r.o.json, pretty: r.o.pretty, graph: r.graph, names: r.useNames, sl: r.o.sl}
				if *got.flags != *tc.expected.flags {
					t.Errorf("tc %q flags\n got  %+v\n want %+v", tc.args, *got.flags, *tc.expected.flags)
				}
				got.flags = tc.expected.flags
			}
			if got != tc.expected {
				t.Errorf("tc %q\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}

// qdiscUsageText and classUsageText are tc_qdisc.c's and tc_class.c's
// usage, as captured.
const (
	qdiscUsageText = "Usage: tc qdisc [ add | del | replace | change | show ] dev STRING\n" +
		"       [ handle QHANDLE ] [ root | ingress | clsact | parent CLASSID ]\n" +
		"       [ estimator INTERVAL TIME_CONSTANT ]\n" +
		"       [ stab [ help | STAB_OPTIONS] ]\n" +
		"       [ ingress_block BLOCK_INDEX ] [ egress_block BLOCK_INDEX ]\n" +
		"       [ [ QDISC_KIND ] [ help | OPTIONS ] ]\n" +
		"\n" +
		"       tc qdisc { show | list } [ dev STRING ] [ QDISC_ID ] [ invisible ]\n" +
		"Where:\n" +
		"QDISC_KIND := { [p|b]fifo | tbf | prio | red | etc. }\n" +
		"OPTIONS := ... try tc qdisc add <desired QDISC_KIND> help\n" +
		"STAB_OPTIONS := ... try tc qdisc add stab help\n" +
		"QDISC_ID := { root | ingress | handle QHANDLE | parent CLASSID }\n"
	classUsageText = "Usage: tc class [ add | del | change | replace | show ] dev STRING\n" +
		"       [ classid CLASSID ] [ root | parent CLASSID ]\n" +
		"       [ [ QDISC_KIND ] [ help | OPTIONS ] ]\n" +
		"\n" +
		"       tc class show [ dev STRING ] [ root | parent CLASSID ]\n" +
		"Where:\n" +
		"QDISC_KIND := { prio | etc. }\n" +
		"OPTIONS := ... try tc class add <desired QDISC_KIND> help\n"
)

// TestGotcBatch runs batches on stdin over scripted replies: the link
// dump batch() makes before it opens the file, how failures end the
// batch, and how -force goes on.
//
// go test ./internal/gotc/ -run TestGotcBatch
func TestGotcBatch(t *testing.T) {
	t.Parallel()
	links := func() Reply { return rep(done()) }
	missing := filepath.Join(t.TempDir(), "nosuch")
	type want struct {
		stdout, stderr string
		rc, reqs       int
	}
	tests := []struct {
		description string
		args        []string
		stdin       string
		replies     []Reply
		expected    want
	}{
		{"positive: two lines after one ll_init_map", []string{"-batch", "-"}, "qdisc help\nclass help\n",
			[]Reply{links()}, want{stderr: qdiscUsageText + classUsageText, reqs: 1}},
		{"positive: a comment, a blank line and a continuation", []string{"-batch", "-"}, "# x\n\nqdisc \\\nhelp # y\n",
			[]Reply{links()}, want{stderr: qdiscUsageText, reqs: 1}},
		{"negative: a failed line stops the batch", []string{"-batch", "-"}, "bogus\nqdisc help\n", []Reply{links()},
			want{stderr: "Object \"bogus\" is unknown, try \"tc help\".\nCommand failed -:1\n", rc: 1, reqs: 1}},
		{"negative: -force goes on, rc 1", []string{"-force", "-batch", "-"}, "bogus\nqdisc help\n", []Reply{links()},
			want{stderr: "Object \"bogus\" is unknown, try \"tc help\".\nCommand failed -:1\n" + qdiscUsageText, rc: 1, reqs: 1}},
		{"negative: a missing file still costs the link dump", []string{"-batch", missing}, "", []Reply{links()},
			want{stderr: "Cannot open file \"" + missing + "\" for reading: No such file or directory\n", rc: 1, reqs: 1}},
		{"negative: a failed link dump exits 1", []string{"-batch", "-"}, "qdisc help\n", []Reply{rep(nlErr(unix.EPERM))},
			want{stderr: "RTNETLINK answers: Operation not permitted\nDump terminated\n", rc: 1, reqs: 1}},
		{"negative: a refused object ends even a -force batch", []string{"-force", "-batch", "-"}, "filter show\nqdisc help\n",
			[]Reply{links()}, want{stderr: notImpl + "tc filter\n", rc: 255, reqs: 1}},
		{"boundary: an empty batch", []string{"-batch", "-"}, "", []Reply{links()}, want{reqs: 1}},
		{"boundary: Command failed counts physical lines", []string{"-batch", "-"}, "\n# c\nqdisc \\\nhelp\nbogus\n",
			[]Reply{links()}, want{stderr: qdiscUsageText + "Object \"bogus\" is unknown, try \"tc help\".\nCommand failed -:5\n", rc: 1, reqs: 1}},
		{"corner: words after the file are ignored", []string{"-batch", "-", "bogus"}, "qdisc help\n",
			[]Reply{links()}, want{stderr: qdiscUsageText, reqs: 1}},
		{"corner: a missing continuation ends the batch, rc 0", []string{"-batch", "-"}, "qdisc help\nclass \\\n",
			[]Reply{links()}, want{stderr: qdiscUsageText + "Missing continuation line\n", reqs: 1}},
		{"corner: an unterminated quote exits 1", []string{"-batch", "-"}, "qdisc \"help\n", []Reply{links()},
			want{stderr: "Unterminated quoted string\n", rc: 1, reqs: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			rc := run(config{src: src, stdin: strings.NewReader(tc.stdin), sys: emptySys(t)}, tc.args, &out, &errb)
			got := want{stdout: out.String(), stderr: errb.String(), rc: rc, reqs: len(src.reqs)}
			if got != tc.expected {
				t.Errorf("tc %q <<< %q\n got  %#v\n want %#v", tc.args, tc.stdin, got, tc.expected)
			}
		})
	}
}

// TestCoreInit reads /proc/net/psched as tc_core_init's fscanf does.
//
// go test ./internal/gotc/ -run TestCoreInit
func TestCoreInit(t *testing.T) {
	t.Parallel()
	type want struct{ clockFactor, tickInUsec float64 }
	defaults := want{1, 1}
	tests := []struct {
		description string
		psched      *string // nil: no file
		expected    want
	}{
		{"positive: the workbench's four words, the third 1e6", ptr("000003e8 00000040 000f4240 3b9aca00\n"), want{1, 15.625}},
		{"positive: a third word of 1e9 takes the compat hack", ptr("000003e8 00000040 3b9aca00\n"), want{1000, 1000}},
		{"negative: no file keeps the defaults", nil, defaults},
		{"negative: two words keep the defaults", ptr("000003e8 00000040\n"), defaults},
		{"negative: not hex keeps the defaults", ptr("zz\n"), defaults},
		{"boundary: an empty file keeps the defaults", ptr(""), defaults},
		{"boundary: us2t 0 divides by zero to +Inf", ptr("1 0 f4240"), want{1, math.Inf(1)}},
		{"corner: %08x reads eight characters, so unspaced words split", ptr("000003e800000040000f4240"), want{1, 15.625}},
		{"corner: a 0x prefix counts toward the width", ptr("0x3e8 0x40 0xf4240"), want{1, 15.625}},
		{"corner: a directory opens and reads as empty", nil, defaults},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			s := emptySys(t)
			if tc.psched != nil {
				writeFiles(t, s.root, map[string]string{pschedPathCst: *tc.psched})
			}
			if strings.Contains(tc.description, "directory") {
				if err := os.MkdirAll(filepath.Join(s.root, pschedPathCst), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			c := s.coreInit()
			if got := want(c); got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// TestReadIDName parses lines as read_id_name's sscanf chain does.
//
// go test ./internal/gotc/ -run TestReadIDName
func TestReadIDName(t *testing.T) {
	t.Parallel()
	type want struct {
		id      int32
		name    string
		ok      bool
		corrupt bool
	}
	tests := []struct {
		description string
		line        string
		expected    want
	}{
		{"positive: maj:min name", "1:10 web\n", want{0x10010, "web", true, false}},
		{"positive: 0x id", "0x20 bulk\n", want{0x20, "bulk", true, false}},
		{"positive: decimal id", "42 voice\n", want{42, "voice", true, false}},
		{"positive: a trailing comment", "1:1 a # b\n", want{0x10001, "a", true, false}},
		{"negative: no number is corrupt, the line kept", "zz name\n", want{0, "zz name\n", false, true}},
		{"negative: a number with no name is corrupt", "5\n", want{0, "5\n", false, true}},
		{"boundary: a blank line", "\n", want{}},
		{"boundary: a comment after blanks", " \t# c\n", want{}},
		{"boundary: ffff:ffff is -1", "ffff:ffff all\n", want{-1, "all", true, false}},
		{"boundary: 8000:0 wraps negative", "8000:0 neg\n", want{math.MinInt32, "neg", true, false}},
		{"corner: 1:zz falls to %d %s, the name \":zz\"", "1:zz x\n", want{1, ":zz", true, false}},
		{"corner: a NUL ends the line, so %d %s reads 1 and \":1\"", "1:1\x00 x\n", want{1, ":1", true, false}},
		{"corner: a negative decimal parses", "-1 neg\n", want{-1, "neg", true, false}},
		{"corner: leading blanks are skipped", "\t 0x7 seven\n", want{7, "seven", true, false}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			id, name, ok, err := readIDName(tc.line)
			got := want{id, name, ok, errors.Is(err, errNamesCorrupt)}
			if got != tc.expected {
				t.Errorf("readIDName(%q) = %+v, want %+v", tc.line, got, tc.expected)
			}
		})
	}
}

// TestNamesInit loads the -cf file and the default databases as
// cls_names_init does.
//
// go test ./internal/gotc/ -run TestNamesInit
func TestNamesInit(t *testing.T) {
	t.Parallel()
	const etc = confEtcDirCst + "/" + clsNamesDBName
	const usr = confUsrDirCst + "/" + clsNamesDBName
	type want struct {
		stderr string
		rc     int // the returned retCode, 0 for nil
		names  map[uint32]string
	}
	tests := []struct {
		description string
		conf        *string
		files       map[string]string
		expected    want
	}{
		{"positive: -cf and the etc file, etc's line winning", ptr("/cls"),
			map[string]string{"/cls": "1:1 a\n1:2 b\n", etc: "1:2 B\n"},
			want{names: map[uint32]string{0x10001: "a", 0x10002: "B"}}},
		{"positive: no etc file falls back to usr", nil, map[string]string{usr: "0x5 five\n"},
			want{names: map[uint32]string{5: "five"}}},
		{"negative: a -cf file that is not there", ptr("/nosuch"), nil,
			want{stderr: "Can't open class names file: /nosuch\n", rc: -1}},
		{"negative: a corrupt etc file drops the database", nil, map[string]string{etc: "1 a\nbad\n"},
			want{stderr: "Database " + etc + " is corrupted at bad\n\n"}},
		{"boundary: no files at all leaves no database", nil, nil, want{}},
		{"boundary: a negative id is skipped", nil, map[string]string{etc: "-3 x\n4 y\n"},
			want{names: map[uint32]string{4: "y"}}},
		{"corner: the -cf names go when neither default is there (Q33)", ptr("/cls"),
			map[string]string{"/cls": "1:1 a\n"}, want{}},
		{"corner: a corrupt -cf file keeps its lines before and goes on", ptr("/cls"),
			map[string]string{"/cls": "1 a\n?\n2 b\n", etc: "3 c\n"},
			want{stderr: "Database /cls is corrupted at ?\n\n", names: map[uint32]string{1: "a", 3: "c"}}},
		{"corner: a -cf directory reads as empty", ptr("/"), map[string]string{etc: "3 c\n"},
			want{names: map[uint32]string{3: "c"}}},
		{"corner: fgets splits a 600-byte line, the rest corrupt", nil,
			map[string]string{etc: "1 " + strings.Repeat("n", 598) + "\n"},
			want{stderr: "Database " + etc + " is corrupted at " + strings.Repeat("n", 89) + "\n\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			s := emptySys(t)
			writeFiles(t, s.root, tc.files)
			var errb bytes.Buffer
			r := &runner{stderr: &errb, sys: s, confFile: tc.conf}
			err := r.namesInit()
			var ret retCode
			if err != nil && !errors.As(err, &ret) {
				t.Fatal(err)
			}
			var names map[uint32]string
			if r.names != nil {
				names = r.names.byID
			}
			got := want{stderr: errb.String(), rc: int(ret), names: names}
			if got.stderr != tc.expected.stderr || got.rc != tc.expected.rc || fmt.Sprint(got.names) != fmt.Sprint(tc.expected.names) {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
			if n, ok := r.names.lookup(0x10001); ok != (tc.expected.names[0x10001] != "") || n != tc.expected.names[0x10001] {
				t.Errorf("lookup(1:1) = %q, %v", n, ok)
			}
		})
	}
}
