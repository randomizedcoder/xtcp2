package rdmanative

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path/filepath"
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
	"vm-rxe":   {driverID: 14, abi: 2, pairs: 1},
	"vm-ionic": {driverID: 21, abi: 1, pairs: 1},
	"l-mlx5":   {driverID: 1, abi: 1, pairs: 2},
}

const (
	uverbsMajor     = 231
	uverbsBaseMinor = 192
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
