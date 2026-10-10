package linkmonitor

import (
	"encoding/binary"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

func productionEnvelope(kind uint16, body []byte) []byte {
	b := make([]byte, 16, 16+len(body))
	binary.LittleEndian.PutUint16(b[4:], kind)
	b = append(b, body...)
	binary.LittleEndian.PutUint32(b, uint32(len(b)))
	return b
}

func productionRouteFixture() []byte {
	body := make([]byte, 16, 24)
	binary.LittleEndian.PutUint32(body[4:], 1)
	body = append(body, 8, 0, unix.IFLA_IFNAME, 0, 'e', 't', 'h', 0)
	return productionEnvelope(unix.RTM_NEWLINK, body)
}

func TestProductionRouteDecode(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		mutate                                       func([]byte) []byte
		accept, ok                                   bool
		count                                        int
		kind                                         model.EventKind
	}{
		{"new", "positive", "valid link record", "one scalar link hint", func(b []byte) []byte { return b }, true, true, 1, model.EventLink},
		{"sender header", "corner", "notification carries origin sequence/pid", "accept kernel-authenticated envelope without header sender assumptions", func(b []byte) []byte { b[8], b[12] = 42, 99; return b }, true, true, 1, model.EventLink},
		{"delete", "positive", "delete only has positive index", "one removal", func(b []byte) []byte { b[4] = unix.RTM_DELLINK; b = b[:32]; b[0] = 32; return b }, true, true, 1, model.EventRemove},
		{"zero index", "negative", "invalid interface identity", "reject before delivery", func(b []byte) []byte { b[20] = 0; return b }, true, false, 0, 0},
		{"truncated", "boundary", "last byte missing", "reject before delivery", func(b []byte) []byte { return b[:len(b)-1] }, true, false, 0, 0},
		{"overflow", "negative", "kernel loss marker", "force stream recovery", func([]byte) []byte { return productionEnvelope(unix.NLMSG_OVERRUN, nil) }, true, false, 0, 0},
		{"full queue", "boundary", "consumer rejects notification", "return ENOBUFS without hidden retry", func(b []byte) []byte { return b }, false, false, 1, model.EventLink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var got []model.Event
			err := decodeRouteEvents(tc.mutate(productionRouteFixture()), 7, model.Stamp{}, func(e model.Event) bool { got = append(got, e); return tc.accept })
			if (err == nil) != tc.ok || len(got) != tc.count {
				t.Fatal(tc.expectedOutcome, got, err)
			}
			if len(got) > 0 && (got[0].Kind != tc.kind || got[0].Observation.Device.Key.Namespace != 7) {
				t.Fatal(tc.expectedOutcome, got)
			}
		})
	}
}

func TestProductionEthtoolDecode(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		command, version                             uint8
		index                                        uint32
		ok                                           bool
		kind                                         model.EventKind
	}{
		{"settings", "positive", "link modes notification", "refresh identified interface", 5, 1, 1, true, model.EventRefresh},
		{"future", "corner", "unknown command", "request authoritative reconciliation", 255, 1, 1, true, model.EventResync},
		{"index", "negative", "zero interface index", "reject consumed notification", 5, 1, 0, false, 0},
		{"reply", "negative", "GET reply on event subscription", "reject unexpected transaction data", 4, 1, 1, false, 0},
		{"version", "boundary", "future protocol version", "conservative reconciliation", 5, 2, 1, true, model.EventResync},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			body := []byte{tc.command, tc.version, 0, 0, 12, 0, 1, 128, 8, 0, 1, 0, 0, 0, 0, 0}
			binary.LittleEndian.PutUint32(body[12:], tc.index)
			var got []model.Event
			err := decodeEthtoolEvents(productionEnvelope(42, body), 42, 7, func(e model.Event) bool { got = append(got, e); return true })
			if (err == nil) != tc.ok || (!tc.ok && len(got) != 0) || (tc.ok && (len(got) != 1 || got[0].Kind != tc.kind)) {
				t.Fatal(tc.expectedOutcome, got, err)
			}
		})
	}
}
