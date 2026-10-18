package rdmanative

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlstrace"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// chardevGolden is what each capture source's GET_CHARDEV replies must
// decode to, from the kernel rather than from the parser: the driver's
// RDMA_DRIVER_* id (include/uapi/rdma/ib_user_ioctl_verbs.h) and
// uverbs_abi_ver (RXE_UVERBS_ABI_VERSION, MLX5_IB_UVERBS_ABI_VERSION,
// IONIC_ABI_VERSION). Every uverbs node is IB_UVERBS_MAJOR 231 with minor
// IB_UVERBS_BASE_MINOR 192 plus the number in its name.
var chardevGolden = map[string]struct {
	driverID uint32
	abi      uint64
	pairs    int
}{
	"vm-rxe":               {driverID: 14, abi: 2, pairs: 1},
	"vm-ionic":             {driverID: 21, abi: 1, pairs: 1},
	"l-mlx5":               {driverID: 1, abi: 1, pairs: 2},
	"l-mlx5-umad":          {driverID: 1, abi: 1, pairs: 2},
	"l-mlx5-umad-unloaded": {driverID: 1, abi: 1, pairs: 2},
}

const (
	uverbsMajor     = 231
	uverbsBaseMinor = 192
	// umadABI is IB_USER_MAD_ABI_VERSION; umadN is 231:N.
	umadABI = 5
)

var getChardevType = xtcpnl.RdmaNlGetType(xtcpnl.RdmaNlNldev, xtcpnl.RdmaNldevCmdGetChardev)

// chardevPairs returns the GET_CHARDEV requests and replies in one strace
// file, in order.
func chardevPairs(t *testing.T, path string) (reqs, reps []nlstrace.Msg) {
	t.Helper()
	dgs, err := nlstrace.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := nlstrace.Msgs(dgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		switch {
		case m.Type != getChardevType:
		case m.Send:
			reqs = append(reqs, m)
		default:
			reps = append(reps, m)
		}
	}
	return reqs, reps
}

// TestCapturedChardev is the GET_CHARDEV parser table's captured rows: every
// request libibverbs sent in a capture is what BuildRdmaGetChardev builds,
// sequence and sender aside, and every reply decodes to the source's golden
// driver id and ABI with a name that agrees with its dev_t.
func TestCapturedChardev(t *testing.T) {
	for _, dir := range captureDirs(t) {
		source := filepath.Base(dir)
		golden, ok := chardevGolden[source]
		if !ok {
			continue
		}
		t.Run(filepath.ToSlash(dir), func(t *testing.T) {
			t.Logf("positive: captured_uverbs for %s; expected: driver %d, ABI %d, 231:192+N for uverbsN, %d pair(s) per ibv_devinfo run",
				source, golden.driverID, golden.abi, golden.pairs)
			files, err := filepath.Glob(filepath.Join(dir, "nldev", "devinfo*.strace"))
			if err != nil || len(files) == 0 {
				t.Fatalf("no devinfo captures: %v", err)
			}
			for _, f := range files {
				reqs, reps := chardevPairs(t, f)
				if len(reqs) != golden.pairs || len(reps) != golden.pairs {
					t.Fatalf("%s: %d requests, %d replies, want %d each", f, len(reqs), len(reps), golden.pairs)
				}
				for i := range reqs {
					checkChardevPair(t, f, reqs[i], reps[i], golden.driverID, golden.abi)
				}
			}
		})
	}
}

func checkChardevPair(t *testing.T, f string, req, rep nlstrace.Msg, driverID uint32, abi uint64) {
	t.Helper()
	if len(req.Body) < 8 {
		t.Fatalf("%s: request body %x", f, req.Body)
	}
	dev := binary.LittleEndian.Uint32(req.Body[4:8])
	want := bytes.Clone(req.Raw)
	clear(want[8:16]) // nlmsg_seq and nlmsg_pid
	got, err := xtcpnl.BuildRdmaGetChardev(nil, 0, dev, 0, xtcpnl.ChardevUverbs)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s: dev %d request\n got %x\nwant %x (%v)", f, dev, got, want, err)
	}
	var c xtcpnl.RdmaChardev
	if err := xtcpnl.ParseRdmaChardev(rep.Body, xtcpnl.ChardevUverbs, &c); err != nil {
		t.Fatalf("%s: reply %x: %v", f, rep.Body, err)
	}
	name := fmt.Sprintf("uverbs%d", c.Minor()-uverbsBaseMinor)
	if c.Major() != uverbsMajor || c.Minor() < uverbsBaseMinor || string(c.NameBytes()) != name ||
		!c.HasDriverID || c.DriverID != driverID || c.ABI != abi {
		t.Fatalf("%s: %q %d:%d driver %d/%v ABI %d", f, c.NameBytes(), c.Major(), c.Minor(), c.DriverID, c.HasDriverID, c.ABI)
	}
}

// TestCapturedUMADChardev reads the umad rows capture-rdma-testdata
// --umad-live recorded on a host: rdmacaps' TestUMADLive run in the native
// and libibumad builds, under strace, with ib_umad unloaded and then loaded.
// A multi-threaded test binary can split a recvmsg across strace lines that
// nlstrace drops, so rows check every message parsed rather than counts.
func TestCapturedUMADChardev(t *testing.T) {
	type row struct {
		name, category, description, expected string
		glob                                  string
		check                                 func(t *testing.T, files []string)
	}
	rows := []row{
		{"live_rows_pass", "positive", "every TestUMADLive run", "exit 0, PASS and no FAIL",
			"*_*loaded-*.out", each(checkLivePass)},
		{"loaded_requests", "positive", "each GET_CHARDEV request with ib_umad loaded", "what BuildRdmaGetChardev builds for umad, port 1 or 2",
			"*_loaded-*.strace", each(checkUMADRequests)},
		{"loaded_replies", "positive", "each GET_CHARDEV reply with ib_umad loaded", "umadN at 231:N, ABI 5, no driver id",
			"*_loaded-*.strace", each(checkUMADReplies)},
		{"absent_port", "boundary", "GET_CHARDEV for port 2 of a one-port device", "an NLMSG_ERROR of -EINVAL in at least one run",
			"*_loaded-*.strace", checkUMADAbsentPort},
		{"unloaded_no_trigger", "negative", "both backends with ib_umad unloaded", "no NETLINK_RDMA socket and no GET_CHARDEV, so nothing autoloads ib_umad",
			"*_unloaded-*.strace", each(checkUMADNoNetlink)},
		{"roce_tools_fail", "corner", "smpquery and perfquery on a RoCE port", "exit 255",
			"*query*.rc", each(checkRC("255"))},
	}
	for _, dir := range captureDirs(t) {
		if _, err := os.Stat(filepath.Join(dir, "umad")); err != nil {
			continue
		}
		for _, tc := range rows {
			files, err := filepath.Glob(filepath.Join(dir, "umad", tc.glob))
			if err != nil {
				t.Fatal(err)
			}
			if len(files) == 0 {
				continue
			}
			t.Run(filepath.ToSlash(dir)+"/"+tc.name, func(t *testing.T) {
				t.Logf("%s: %s; expected: %s (%d files)", tc.category, tc.description, tc.expected, len(files))
				tc.check(t, files)
			})
		}
	}
}

// each applies check to every file.
func each(check func(*testing.T, string)) func(*testing.T, []string) {
	return func(t *testing.T, files []string) {
		t.Helper()
		for _, f := range files {
			check(t, f)
		}
	}
}

func checkLivePass(t *testing.T, f string) {
	t.Helper()
	checkRC("0")(t, strings.TrimSuffix(f, ".out")+".rc")
	out, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("--- PASS: TestUMADLive")) || bytes.Contains(out, []byte("--- FAIL")) {
		t.Fatalf("%s:\n%s", f, out)
	}
}

func checkRC(want string) func(*testing.T, string) {
	return func(t *testing.T, f string) {
		t.Helper()
		rc, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(rc)); got != want {
			t.Fatalf("%s: exit %s, want %s", f, got, want)
		}
	}
}

// umadMsgs returns the GET_CHARDEV messages and NLMSG_ERROR replies in f.
func umadMsgs(t *testing.T, f string) (reqs, reps, errs []nlstrace.Msg) {
	t.Helper()
	dgs, err := nlstrace.ParseFile(f)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := nlstrace.Msgs(dgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		switch {
		case m.Type == syscall.NLMSG_ERROR && !m.Send:
			errs = append(errs, m)
		case m.Type != getChardevType:
		case m.Send:
			reqs = append(reqs, m)
		default:
			reps = append(reps, m)
		}
	}
	return reqs, reps, errs
}

func checkUMADRequests(t *testing.T, f string) {
	t.Helper()
	reqs, _, _ := umadMsgs(t, f)
	if len(reqs) == 0 {
		t.Fatalf("%s: no GET_CHARDEV requests", f)
	}
	for _, req := range reqs {
		if len(req.Body) < 16 {
			t.Fatalf("%s: request body %x", f, req.Body)
		}
		dev, port := binary.LittleEndian.Uint32(req.Body[4:8]), binary.LittleEndian.Uint32(req.Body[12:16])
		want := bytes.Clone(req.Raw)
		clear(want[8:16]) // nlmsg_seq and nlmsg_pid
		got, err := xtcpnl.BuildRdmaGetChardev(nil, 0, dev, port, xtcpnl.ChardevUmad)
		if err != nil || (port != 1 && port != 2) || !bytes.Equal(got, want) {
			t.Fatalf("%s: dev %d port %d request\n got %x\nwant %x (%v)", f, dev, port, got, want, err)
		}
	}
}

func checkUMADReplies(t *testing.T, f string) {
	t.Helper()
	_, reps, _ := umadMsgs(t, f)
	if len(reps) == 0 {
		t.Fatalf("%s: no GET_CHARDEV replies", f)
	}
	for _, rep := range reps {
		var c xtcpnl.RdmaChardev
		if err := xtcpnl.ParseRdmaChardev(rep.Body, xtcpnl.ChardevUmad, &c); err != nil {
			t.Fatalf("%s: reply %x: %v", f, rep.Body, err)
		}
		name := fmt.Sprintf("umad%d", c.Minor())
		if c.Major() != uverbsMajor || string(c.NameBytes()) != name || c.HasDriverID || c.ABI != umadABI {
			t.Fatalf("%s: %q %d:%d driver %d/%v ABI %d", f, c.NameBytes(), c.Major(), c.Minor(), c.DriverID, c.HasDriverID, c.ABI)
		}
	}
}

// checkUMADAbsentPort wants the -EINVAL in some file, not every one: strace
// can split the recvmsg that carries it.
func checkUMADAbsentPort(t *testing.T, files []string) {
	t.Helper()
	for _, f := range files {
		_, _, errs := umadMsgs(t, f)
		for _, e := range errs {
			if len(e.Raw) >= 20 && int32(binary.LittleEndian.Uint32(e.Raw[16:20])) == -int32(syscall.EINVAL) {
				return
			}
		}
	}
	t.Fatalf("no -EINVAL NLMSG_ERROR in %d files", len(files))
}

func checkUMADNoNetlink(t *testing.T, f string) {
	t.Helper()
	raw, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("NETLINK_RDMA")) {
		t.Fatalf("%s: opened a NETLINK_RDMA socket with ib_umad unloaded", f)
	}
	if reqs, reps, _ := umadMsgs(t, f); len(reqs)+len(reps) != 0 {
		t.Fatalf("%s: %d GET_CHARDEV requests, %d replies", f, len(reqs), len(reps))
	}
}
