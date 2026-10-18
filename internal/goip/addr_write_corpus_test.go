package goip

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
)

// addrWriteBytesRow is one row of TestAddrWriteMatchesIpBytes: a corpus slug
// that `ip` sent a write for, and the argv that builds it.
type addrWriteBytesRow struct {
	description string
	slug        string
	family      uint8  // -4/-6, as the row ran it
	args        string // after `ip addr`, as the row ran it unless the description says otherwise
	expected    int    // name lookups `ip` sent before the write
}

// addrWriteBytesRows is every address write in the corpus, then the rows
// that spell one of them another way.
var addrWriteBytesRows = []addrWriteBytesRow{
	{description: "positive: add, the plain IPv4 case", slug: "add_v4", args: "add 192.0.2.10/24 dev d0", expected: 1},
	{description: "positive: peer is IFA_ADDRESS with no local copy", slug: "add_v4_peer",
		args: "add 10.0.0.1 peer 10.0.0.2/32 dev d0", expected: 1},
	{description: "positive: brd + is computed last", slug: "add_v4_brd_plus", args: "add 192.0.2.10/24 brd + dev d0", expected: 1},
	{description: "positive: brd -", slug: "add_v4_brd_minus", args: "add 192.0.2.10/24 brd - dev d0", expected: 1},
	{description: "boundary: /31 gets no broadcast", slug: "add_v4_brd_31", args: "add 192.0.2.10/31 brd + dev d0", expected: 1},
	{description: "positive: the label goes before the computed broadcast", slug: "add_v4_label",
		args: "add 192.0.2.10/24 brd + label d0:1 dev d0", expected: 1},
	{description: "positive: metric", slug: "add_v4_metric", args: "add 192.0.2.10/24 metric 50 dev d0", expected: 1},
	{description: "corner: pref is metric, not preferred_lft", slug: "add_v4_pref_is_metric",
		args: "add 192.0.2.10/24 pref 7 dev d0", expected: 1},
	{description: "positive: proto by name", slug: "add_v4_proto_name", args: "add 192.0.2.10/24 proto kernel_lo dev d0", expected: 1},
	{description: "positive: lifetimes go in CACHEINFO", slug: "add_v4_lifetimes",
		args: "add 192.0.2.10/24 valid_lft 3600 preferred_lft 1800 dev d0", expected: 1},
	{description: "positive: a secondary is the same request", slug: "add_v4_secondary", args: "add 192.0.2.2/24 dev d0", expected: 1},
	{description: "boundary: 127/8 gets host scope", slug: "add_v4_loopback_scope", args: "add 127.0.0.2/8 dev d0", expected: 1},
	{description: "positive: autojoin spills to IFA_FLAGS", slug: "add_v4_autojoin",
		args: "add 224.0.0.251/32 autojoin dev d0", expected: 1},
	{description: "positive: -6 nodad fits the header", slug: "add_v6_nodad", family: unix.AF_INET6,
		args: "add 2001:db8::5/64 nodad dev d0", expected: 1},
	{description: "boundary: the wide flags, worked example", slug: "add_v6_flags_wide", family: unix.AF_INET6,
		args: "add 2001:db8::5/64 nodad noprefixroute dev d0", expected: 1},
	{description: "corner: a flag before the address warns and is dropped", slug: "add_v6_nodad_first",
		args: "add nodad 2001:db8::5/64 dev d0", expected: 1},
	{description: "negative: mngtmpaddr on /48, refused by the kernel", slug: "add_v6_mngtmpaddr_48", family: unix.AF_INET6,
		args: "add 2001:db8::5/48 mngtmpaddr dev d0", expected: 1},
	{description: "positive: nodad optimistic is both flags; this kernel accepts it", slug: "add_v6_nodad_optimistic", family: unix.AF_INET6,
		args: "add 2001:db8::5/64 nodad optimistic dev d0", expected: 1},
	{description: "negative: an existing address, EEXIST", slug: "add_existing", args: "add 192.0.2.1/24 dev d0", expected: 1},
	{description: "positive: change is REPLACE alone", slug: "change_missing_creates", args: "change 192.0.2.10/24 dev d0", expected: 1},
	{description: "positive: replace", slug: "replace_existing", args: "replace 192.0.2.1/24 metric 9 dev d0", expected: 1},
	{description: "positive: delete keeps scope 0", slug: "del_v4", args: "del 192.0.2.1/24 dev d0", expected: 1},
	{description: "corner: the IPv4 wildcard delete warns and sends no IFA_ADDRESS", slug: "del_v4_wildcard",
		args: "del 192.0.2.1 dev d0", expected: 1},
	{description: "negative: deleting a missing address", slug: "del_missing", args: "del 192.0.2.99/24 dev d0", expected: 1},
	{description: "negative: a secondary with another scope", slug: "scope_conflict",
		args: "add 192.0.2.2/24 scope link dev d0", expected: 1},
	{description: "negative: IPv6 multicast without autojoin", slug: "v6_mcast_noautojoin", family: unix.AF_INET6,
		args: "add ff02::fb/128 dev d0", expected: 1},
	{description: "boundary: a 16-byte label is sent, and the kernel refuses it", slug: "label_16",
		args: "add 192.0.2.10/24 label d0:0123456789abc dev d0", expected: 1},
	{description: "corner: a is add", slug: "add_v4", args: "a 192.0.2.10/24 dev d0", expected: 1},
	{description: "corner: chg is change", slug: "change_missing_creates", args: "chg 192.0.2.10/24 dev d0", expected: 1},
	{description: "corner: delete spelled out is del", slug: "del_v4", args: "delete 192.0.2.1/24 dev d0", expected: 1},
	{description: "corner: the local keyword is optional", slug: "add_v4", args: "add local 192.0.2.10/24 dev d0", expected: 1},
	{description: "corner: priority is metric", slug: "add_v4_metric", args: "add 192.0.2.10/24 priority 50 dev d0", expected: 1},
	{description: "corner: b + is brd +", slug: "add_v4_brd_plus", args: "add 192.0.2.10/24 b + dev d0", expected: 1},
	{description: "corner: v and preferred are the lifetimes", slug: "add_v4_lifetimes",
		args: "add 192.0.2.10/24 v 3600 preferred 1800 dev d0", expected: 1},
}

// addrWriteNodevRow is one row of TestAddrWriteNodevCorpus: a slug `ip`
// refused before writing.
type addrWriteNodevRow struct {
	description string
	slug        string
	family      uint8
	args        string
	expected    int // name lookups `ip` sent before refusing
	// notViaRun, when set, says why the Run half is skipped for this row.
	notViaRun string
}

var addrWriteNodevRows = []addrWriteNodevRow{
	{description: "negative: an unknown device stops after the lookup", slug: "nodev",
		args: "add 192.0.2.10/24 dev nosuch", expected: 1,
		// GOIP_REPLAY's ReplaySource has no Talk, so the lookup falls back
		// to a link dump and reports "not found" instead of the kernel's
		// ENODEV, which only a Talk source returns (NetlinkSource live, or
		// newLinkReplay above).
		notViaRun: "ReplaySource cannot answer a single-get with ENODEV"},
	{description: "negative: valid_lft 0 is refused after the lookup", slug: "valid_zero",
		args: "add 192.0.2.10/24 valid_lft 0 dev d0", expected: 1},
	{description: "negative: autojoin on unicast is refused after the lookup", slug: "autojoin_unicast",
		args: "add 192.0.2.10/24 autojoin dev d0", expected: 1},
	{description: "negative: no dev keyword sends nothing", slug: "no_dev_keyword",
		args: "add 192.0.2.10/24", expected: 0},
	{description: "negative: brd + on IPv6 sends nothing", slug: "brd_plus_v6", family: unix.AF_INET6,
		args: "add 2001:db8::5/64 brd + dev d0", expected: 0},
}

// runAddressWriteWith is runAddressWith with stderr kept, for the warnings.
func runAddressWriteWith(t *testing.T, src Source, family uint8, args []string) (stderr string, err error) {
	t.Helper()
	var errOut bytes.Buffer
	c := &runCtx{
		src:    src,
		lltab:  NewLLTab(),
		out:    io.Discard,
		errOut: &errOut,
		family: family,
	}
	err = runAddress(c, args)
	return errOut.String(), err
}

// addrSlugFile reads one of a corpus slug's files.
func addrSlugFile(t *testing.T, slug, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(addrWriteCorpusDir, slug, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestAddrWriteMatchesIpBytes is A6-T02's exit evidence: for every address
// write in the corpus, goip sends the lookup `ip` sent and builds the same
// write request, byte for byte apart from nlmsg_seq and nlmsg_pid. When
// `ip` succeeded, goip's stderr is `ip`'s too: empty, or the warning. A
// kernel refusal's stderr is the kernel's text, which goip never sees, so it
// is not compared; TestDecodeAddrWriteCorpus checks the answer itself.
//
// go test ./internal/goip/ -run TestAddrWriteMatchesIpBytes
func TestAddrWriteMatchesIpBytes(t *testing.T) {
	for _, tc := range addrWriteBytesRows {
		t.Run(tc.description, func(t *testing.T) {
			pcap := filepath.Join(addrWriteCorpusDir, tc.slug, "ip.pcap")
			c, err := nlparity.ParseRouteCaptureFile(pcap)
			if err != nil {
				t.Fatal(err)
			}
			reqs := c.Requests()
			if len(reqs) != tc.expected+1 {
				t.Fatalf("capture holds %d requests, want %d lookups and the write", len(reqs), tc.expected)
			}

			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newLinkReplay(t, pcap)
			stderr, err := runAddressWriteWith(t, src, tc.family, strings.Fields(tc.args))
			if err != nil {
				t.Fatalf("goip addr %s: %v", tc.args, err)
			}

			if len(src.gets) != tc.expected {
				t.Fatalf("goip sent %d lookups, want %d", len(src.gets), tc.expected)
			}
			for i, g := range src.gets {
				if want := wireBytesOf(reqs[i]); !bytes.Equal(g, want) {
					t.Errorf("lookup %d:\n got %x\nwant %x", i+1, g, want)
				}
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if want := wireBytesOf(reqs[tc.expected]); !bytes.Equal(zeroSeqPidCopy(got), want) {
				t.Errorf("write:\n got %x\nwant %x", zeroSeqPidCopy(got), want)
			}
			if strings.TrimSpace(addrSlugFile(t, tc.slug, "ip.rc")) == "0" {
				if want := addrSlugFile(t, tc.slug, "ip.err"); stderr != want {
					t.Errorf("stderr = %q, want ip.err %q", stderr, want)
				}
			}
		})
	}
}

// TestAddrWriteNodevCorpus is the rows `ip` refused before writing: goip
// must stop at the same step, after the same lookups (none for the two
// refused before the lookup), with no request written, and exit with
// `ip`'s status and message.
//
// go test ./internal/goip/ -run TestAddrWriteNodevCorpus
func TestAddrWriteNodevCorpus(t *testing.T) {
	for _, tc := range addrWriteNodevRows {
		t.Run(tc.description, func(t *testing.T) {
			pcap := filepath.Join(addrWriteCorpusDir, tc.slug, "ip.pcap")
			c, err := nlparity.ParseRouteCaptureFile(pcap)
			if err != nil {
				t.Fatal(err)
			}
			reqs := c.Requests()
			if len(reqs) != tc.expected {
				t.Fatalf("capture holds %d requests, want %d lookups", len(reqs), tc.expected)
			}
			msg := strings.TrimSuffix(addrSlugFile(t, tc.slug, "ip.err"), "\n")

			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newLinkReplay(t, pcap)
			_, err = runAddressWriteWith(t, src, tc.family, strings.Fields(tc.args))
			if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), msg) {
				t.Fatalf("err = %v, want %q", err, msg)
			}
			if len(src.gets) != tc.expected {
				t.Fatalf("goip sent %d lookups, want %d", len(src.gets), tc.expected)
			}
			for i, g := range src.gets {
				if want := wireBytesOf(reqs[i]); !bytes.Equal(g, want) {
					t.Errorf("lookup %d:\n got %x\nwant %x", i+1, g, want)
				}
			}
			if _, err := os.Stat(out); err == nil {
				t.Error("a write file was written")
			}

			// The same command through Run: the exit status and the message.
			if tc.notViaRun != "" {
				return
			}
			t.Setenv("GOIP_REPLAY", pcap)
			args := strings.Fields(tc.args)
			argv := make([]string, 0, len(args)+2)
			if tc.family == unix.AF_INET6 {
				argv = append(argv, "-6")
			}
			argv = append(argv, "addr")
			argv = append(argv, args...)
			var stdout, stderr bytes.Buffer
			code := Run(argv, &stdout, &stderr)
			if want := strings.TrimSpace(addrSlugFile(t, tc.slug, "ip.rc")); want != strconv.Itoa(code) {
				t.Errorf("exit = %d, want ip.rc %s", code, want)
			}
			if !strings.Contains(stderr.String(), msg) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), msg)
			}
		})
	}
}

// TestAddrWriteCorpusComplete keeps the two tables above in step with the
// corpus directory, so a new capture cannot be forgotten.
//
// go test ./internal/goip/ -run TestAddrWriteCorpusComplete
func TestAddrWriteCorpusComplete(t *testing.T) {
	entries, err := os.ReadDir(addrWriteCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	var writes, refused []string
	for _, r := range addrWriteBytesRows {
		if !slices.Contains(writes, r.slug) {
			writes = append(writes, r.slug)
		}
	}
	for _, r := range addrWriteNodevRows {
		if !slices.Contains(refused, r.slug) {
			refused = append(refused, r.slug)
		}
	}
	type counts struct{ writes, refused, dirs int }
	tests := []struct {
		description string
		got         func() any
		expected    any
	}{
		{description: "positive: every slug directory is a row of exactly one table",
			got: func() any {
				var bad []string
				for _, d := range dirs {
					if slices.Contains(writes, d) == slices.Contains(refused, d) {
						bad = append(bad, d)
					}
				}
				return strings.Join(bad, " ")
			}, expected: ""},
		{description: "boundary: the tables hold 27 and 5 distinct slugs, the 32 directories",
			got:      func() any { return counts{len(writes), len(refused), len(dirs)} },
			expected: counts{27, 5, 32}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// addrSlugWrite is the write request `ip` sent for a slug, seq and pid zeroed.
func addrSlugWrite(t *testing.T, slug string) []byte {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(filepath.Join(addrWriteCorpusDir, slug, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	reqs := c.Requests()
	if len(reqs) == 0 {
		t.Fatalf("%s: capture holds no requests", slug)
	}
	return wireBytesOf(reqs[len(reqs)-1])
}

// TestRunAddressWrite is the CLI end to end: the GOIP_WRITE_OUT hook, the
// refusal without it, the exit codes, and a warning reaching stderr. Each
// row replays its slug's capture, which answers the `dev d0` lookup.
//
// go test ./internal/goip/ -run TestRunAddressWrite
func TestRunAddressWrite(t *testing.T) {
	type hook uint8
	const (
		hookUnset hook = iota
		hookEmpty
		hookSet
	)
	type result struct {
		code      int
		stderr    string // a fragment; "" for empty
		writeSlug string // the slug whose write the file must hold; "" for no file
	}
	tests := []struct {
		description string
		slug        string // the capture GOIP_REPLAY reads
		args        string
		hook        hook
		expected    result
	}{
		{description: "positive: the request goes to GOIP_WRITE_OUT and nothing is printed",
			slug: "add_v4", args: "addr add 192.0.2.10/24 dev d0", hook: hookSet,
			expected: result{code: ExitOK, writeSlug: "add_v4"}},
		{description: "corner: a a is address add",
			slug: "add_v4", args: "a a 192.0.2.10/24 dev d0", hook: hookSet,
			expected: result{code: ExitOK, writeSlug: "add_v4"}},
		{description: "negative: without GOIP_WRITE_OUT the write is refused",
			slug: "add_v4", args: "addr add 192.0.2.10/24 dev d0",
			expected: result{code: ExitUsage, stderr: "never applies a write"}},
		{description: "corner: an empty GOIP_WRITE_OUT is no hook",
			slug: "add_v4", args: "addr add 192.0.2.10/24 dev d0", hook: hookEmpty,
			expected: result{code: ExitUsage, stderr: "never applies a write"}},
		{description: "negative: an argv error wins over the missing hook",
			slug: "add_v4", args: "addr add 192.0.2.10/24 valid_lft 3600sec dev d0",
			expected: result{code: ExitUsage, stderr: "valid_lft value"}},
		{description: "negative: an argv error writes no file",
			slug: "add_v4", args: "addr add 192.0.2.10/24 valid_lft 3600sec dev d0", hook: hookSet,
			expected: result{code: ExitUsage, stderr: "valid_lft value"}},
		{description: "boundary: help is not implemented and writes no file",
			slug: "add_v4", args: "addr add help", hook: hookSet,
			expected: result{code: ExitUsage, stderr: "not implemented"}},
		{description: "corner: a warning goes to stderr and the write still lands",
			slug: "add_v6_nodad_first", args: "addr add nodad 2001:db8::5/64 dev d0", hook: hookSet,
			expected: result{code: ExitOK, stderr: "Warning: nodad option can be set only for IPv6 addresses",
				writeSlug: "add_v6_nodad_first"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", filepath.Join(addrWriteCorpusDir, tc.slug, "ip.pcap"))
			out := filepath.Join(t.TempDir(), "write.bin")
			switch tc.hook {
			case hookSet:
				t.Setenv(writeOutEnvCst, out)
			case hookEmpty:
				t.Setenv(writeOutEnvCst, "")
			case hookUnset:
				t.Setenv(writeOutEnvCst, "")
				if err := os.Unsetenv(writeOutEnvCst); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			code := Run(strings.Fields(tc.args), &stdout, &stderr)
			if code != tc.expected.code {
				t.Errorf("code = %d, want %d; stderr %q", code, tc.expected.code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.expected.stderr) || (tc.expected.stderr == "") != (stderr.Len() == 0) {
				t.Errorf("stderr = %q, want %q", stderr.String(), tc.expected.stderr)
			}
			got, rerr := os.ReadFile(out)
			switch {
			case tc.expected.writeSlug == "" && rerr == nil:
				t.Errorf("wrote %x, want no file", got)
			case tc.expected.writeSlug != "":
				if want := addrSlugWrite(t, tc.expected.writeSlug); !bytes.Equal(zeroSeqPidCopy(got), want) {
					t.Errorf("wrote %x (%v)\nwant  %x", zeroSeqPidCopy(got), rerr, want)
				}
			}
		})
	}
}
