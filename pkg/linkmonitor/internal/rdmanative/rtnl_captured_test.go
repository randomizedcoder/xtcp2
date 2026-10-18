package rdmanative

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/nlstrace"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// capturedPort is one port in a capture's `rdma link show` NLDEV replies,
// taken before the triggers.
type capturedPort struct {
	Binding
	active bool
}

// capturedBindings reads each port's netdev, which is what the inventory
// binds, and whether it was ACTIVE, from the capture's `rdma link show`
// NLDEV replies.
func capturedBindings(t *testing.T, dir string) []capturedPort {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "nldev", "link_show-*.strace"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no link_show captures in %s: %v", dir, err)
	}
	var out []capturedPort
	for _, f := range files {
		dgs, err := nlstrace.ParseFile(f)
		if err != nil {
			t.Fatal(err)
		}
		msgs, err := nlstrace.Msgs(dgs)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range msgs {
			if m.Send || xtcpnl.RdmaNlGetClient(m.Type) != xtcpnl.RdmaNlNldev {
				continue
			}
			v, err := xtcpnl.ParseRdmaMsg(m.Type, m.Body)
			if err != nil {
				continue
			}
			if p, ok := v.(xtcpnl.RdmaPort); ok && p.HasNdevIndex && p.HasPortIndex {
				out = append(out, capturedPort{Binding{Device: p.DevName, Port: p.PortIndex, Netdev: p.NdevIndex},
					p.HasState && p.State == portActive})
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: no port with a netdev", dir)
	}
	return out
}

var asyncRead = regexp.MustCompile(`read\(\d+<[^>]*>, "((?:\\x[0-9a-f]{2})+)", 16\) = 16`)

// capturedAsync decodes every uverbs async record in one strace.
func capturedAsync(t *testing.T, device, path string) []rdmaevents.Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reads := asyncRead.FindAllStringSubmatch(string(data), -1)
	out := make([]rdmaevents.Event, 0, len(reads))
	for _, m := range reads {
		rec, err := hex.DecodeString(strings.ReplaceAll(m[1], `\x`, ""))
		if err != nil {
			t.Fatal(err)
		}
		e, err := DecodeAsyncEvent(device, rec)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out = append(out, e)
	}
	return out
}

// portActive is IB_PORT_ACTIVE.
const portActive = 4

// seedBody is an RTM_NEWLINK body for ifindex with the given flags.
func seedBody(ifindex, flags uint32) []byte {
	b := make([]byte, ifinfomsgLen)
	binary.LittleEndian.PutUint32(b[ifinfomsgOffIndex:], ifindex)
	binary.LittleEndian.PutUint32(b[ifinfomsgOffFlags:], flags)
	return b
}

// translateCapture runs the rtnetlink notifications in an nlmon pcap through
// a translator seeded as the source seeds it from its link dump: a port the
// capture saw ACTIVE before the trigger has a running netdev with carrier.
//
// nlmon records the whole host, requests and dump replies included, so only
// notifications are translated, as only they reach a subscribed socket
// outside a dump. It also records a multicast notification once per socket
// it is delivered to (netlink_deliver_tap runs per recipient), so the copies
// after the first are dropped: one subscriber receives it once.
func translateCapture(t *testing.T, ports []capturedPort, m *PortMap, path string) []rdmaevents.Event {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tr := NewTranslator(m)
	for _, p := range ports {
		flags := uint32(0)
		if p.active {
			flags = iffRunning | iffLowerUp
		}
		if err := tr.Seed(rtmNewLink, seedBody(p.Netdev, flags)); err != nil {
			t.Fatal(err)
		}
	}
	var out []rdmaevents.Event
	var batch [MaxPorts]rdmaevents.Event
	var last nlparity.Msg
	for _, msg := range c.Msgs() {
		if !xtcpnl.IsRtnetlinkNotification(msg.Hdr) {
			continue
		}
		if msg.Hdr == last.Hdr && bytes.Equal(msg.Body, last.Body) {
			continue
		}
		last = msg
		n, err := tr.Translate(msg.Hdr.Type, msg.Body, &batch)
		if err != nil {
			t.Fatalf("%s: type %d: %v", path, msg.Hdr.Type, err)
		}
		out = append(out, batch[:n]...)
	}
	return out
}

// eventCounts renders events as "device/port:kind count" in sorted order,
// with kinds as R(efresh), T(opology), F(atal) and U(nknown).
func eventCounts(events []rdmaevents.Event) string {
	counts := make(map[string]int)
	for _, e := range events {
		counts[eventKey(e)]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
	}
	return strings.Join(parts, ", ")
}

func eventKey(e rdmaevents.Event) string {
	return fmt.Sprintf("%s/%d:%c", e.Device, e.Port, "RTFU"[min(int(e.Kind), 3)])
}

// translationGolden is what each capture's notifications translate to, per
// trigger. The verbs side is in the capture's async straces. Against it:
//   - gid matches exactly on rxe and ionic; mlx5 reports GID_CHANGE twice per
//     address change, so verbs has 4 where netlink has 2.
//   - flap adds one or two Topology on rxe: the IPv6 link-local address goes
//     with the link and comes back with it. The GID cache already holds it as
//     the default GID, so verbs is silent, but the header does not say so.
//     The extra costs a resync and never hides a change.
//   - Some rxe kernels report PORT_ACTIVE twice on the way up; the link
//     state changes once.
//   - ionic's flap capture holds the down notification only: nlmon records a
//     notification only when some socket receives it, and the guest's one
//     listener was gone before the link came up. The source, subscribed
//     throughout, would receive it.
var translationGolden = map[string]map[string]string{
	"6_1_177/vm-rxe":   {"flap": "rxe0/1:R 2, rxe0/1:T 1", "gid": "rxe0/1:T 2"},
	"6_6_144/vm-rxe":   {"flap": "rxe0/1:R 2, rxe0/1:T 2", "gid": "rxe0/1:T 2"},
	"6_12_96/vm-rxe":   {"flap": "rxe0/1:R 2, rxe0/1:T 1", "gid": "rxe0/1:T 2"},
	"6_18_39/vm-rxe":   {"flap": "rxe0/1:R 2, rxe0/1:T 1", "gid": "rxe0/1:T 2"},
	"7_1_4/vm-rxe":     {"flap": "rxe0/1:R 2, rxe0/1:T 1", "gid": "rxe0/1:T 2"},
	"7_2_0_rc3/vm-rxe": {"flap": "rxe0/1:R 2, rxe0/1:T 1", "gid": "rxe0/1:T 2"},
	"7_1_4/vm-ionic":   {"flap": "ionic_0/1:R 1", "gid": "ionic_0/1:T 2"},
	"7_2_9/l-mlx5":     {"gid": "mlx5_0/1:T 2"},
}

// checkAgainstVerbs is the differential rule: every device, port and kind
// verbs reported is translated, and anything more is a Topology on a port
// verbs reported on.
func checkAgainstVerbs(t *testing.T, trigger string, got, verbs []rdmaevents.Event) {
	t.Helper()
	translated := make(map[string]bool)
	for _, e := range got {
		translated[eventKey(e)] = true
	}
	reported := make(map[string]bool)
	ports := make(map[string]bool)
	for _, e := range verbs {
		reported[eventKey(e)] = true
		ports[fmt.Sprintf("%s/%d", e.Device, e.Port)] = true
		if !translated[eventKey(e)] {
			t.Errorf("%s: verbs reported %s, translation did not", trigger, eventKey(e))
		}
	}
	for _, e := range got {
		if !reported[eventKey(e)] && (e.Kind != rdmaevents.Topology || !ports[fmt.Sprintf("%s/%d", e.Device, e.Port)]) {
			t.Errorf("%s: translated %s, which verbs did not report", trigger, eventKey(e))
		}
	}
}

// TestCapturedTranslation is the translator table's captured rows
// (carrier_down, addr_add and their reverses): for each trigger (flap takes
// the link down and up; gid adds and removes an IPv6 address), the rtnetlink
// notifications nlmon recorded translate to the golden events and agree with
// what the verbs async fd reported for the same trigger.
func TestCapturedTranslation(t *testing.T) {
	for _, dir := range captureDirs(t) {
		source := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(dir)), filepath.Base(dir)))
		golden, ok := translationGolden[source]
		if !ok {
			t.Errorf("%s: no golden translation", source)
			continue
		}
		t.Run(source, func(t *testing.T) {
			ports := capturedBindings(t, dir)
			bindings := make([]Binding, 0, len(ports))
			for _, p := range ports {
				bindings = append(bindings, p.Binding)
			}
			m, err := NewPortMap(bindings, nil)
			if err != nil {
				t.Fatal(err)
			}
			pcaps, _ := filepath.Glob(filepath.Join(dir, "nlmon", "*.pcap"))
			if len(pcaps) != len(golden) {
				t.Fatalf("%d pcaps, %d golden triggers", len(pcaps), len(golden))
			}
			for _, pcap := range pcaps {
				trigger := strings.TrimSuffix(filepath.Base(pcap), ".pcap")
				t.Logf("positive: captured %s; expected: %s", trigger, golden[trigger])
				got := translateCapture(t, ports, m, pcap)
				verbs := make([]rdmaevents.Event, 0, len(got))
				for _, dev := range m.Devices() {
					verbs = append(verbs, capturedAsync(t, dev, filepath.Join(dir, "async", dev+"-"+trigger+".strace"))...)
				}
				if s := eventCounts(got); s != golden[trigger] {
					t.Errorf("%s: translated %q, want %q (verbs %q)", trigger, s, golden[trigger], eventCounts(verbs))
				}
				if source == "7_1_4/vm-ionic" && trigger == "flap" {
					continue // the capture misses the up notification; see translationGolden
				}
				checkAgainstVerbs(t, trigger, got, verbs)
			}
		})
	}
}
