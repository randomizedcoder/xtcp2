package xtcpnl

// The ethtool rows' O3 and O4 (tools/nlcapgen matrix_ethtool.go, W5d).
// Each row's namespace holds one netdevsim port, eth0, and the row runs an
// ethtool set or show command over generic netlink.
//
// O3 holds each set command's ETHTOOL_MSG_*_SET to the row's facts: the
// attributes ParseEthtool decodes from it are exactly the ones its
// arguments name (ethFields), with the values given, and the header names
// the device. Going through ParseEthtool's typed fields, not the raw
// attributes, means a decoder that drops or misnumbers a field fails here.
// FEC's modes are a bitset of names, held to fec-names and fec-auto.
//
// O4 holds the readback's *_GET reply to the same facts: every value set
// reads back as set, except the rt.<name> facts, which say what the device
// reads back instead (matrix_ethtool.go gives the reason at each). A show
// row's GET must name its device and carry ETHTOOL_FLAG_STATS exactly when
// -I was given, and a --json row's stdout must print what the reply holds.

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	ethGenlCtrl  = 0x10 // GENL_ID_CTRL
	ethFlagStats = 4    // ETHTOOL_FLAG_STATS
	ethSetFEC    = 30   // ETHTOOL_MSG_FEC_SET
)

// ethFields maps each set command's argument names to the attribute it
// sends (include/uapi/linux/ethtool_netlink_generated.h). The widths are
// ParseEthtool's; a fact for a name missing here is a model error.
var ethFields = map[uint8]map[string]uint16{
	16: { // RINGS_SET
		"rx": 6, "rx-mini": 7, "rx-jumbo": 8, "tx": 9, "rx-buf-len": 10, "tcp-data-split": 11,
		"cqe-size": 12, "tx-push": 13, "rx-push": 14, "tx-push-buf-len": 15, "hds-thresh": 17,
	},
	18: {"rx": 6, "tx": 7, "other": 8, "combined": 9},   // CHANNELS_SET
	22: {"autoneg": 2, "rx": 3, "tx": 4},                // PAUSE_SET
	20: ethCoalesceFields,                               // COALESCE_SET
	30: {"fec-names": 2, "fec-auto": 3, "fec-modes": 2}, // FEC_SET, held apart
}

// ethCoalesceFields are ethtool -C's argument names (netlink/coalesce.c).
var ethCoalesceFields = map[string]uint16{
	"rx-usecs": 2, "rx-frames": 3, "rx-usecs-irq": 4, "rx-frames-irq": 5,
	"tx-usecs": 6, "tx-frames": 7, "tx-usecs-irq": 8, "tx-frames-irq": 9,
	"stats-block-usecs": 10, "adaptive-rx": 11, "adaptive-tx": 12, "pkt-rate-low": 13,
	"rx-usecs-low": 14, "rx-frames-low": 15, "tx-usecs-low": 16, "tx-frames-low": 17,
	"pkt-rate-high": 18, "rx-usecs-high": 19, "rx-frames-high": 20, "tx-usecs-high": 21,
	"tx-frames-high": 22, "sample-interval": 23, "cqe-mode-tx": 24, "cqe-mode-rx": 25,
	"tx-aggr-max-bytes": 26, "tx-aggr-max-frames": 27, "tx-aggr-time-usecs": 28,
	"rx-cqe-frames": 31, "rx-cqe-nsecs": 32,
}

// ethTypedField is one typed field of a decoded message, by attribute.
type ethTypedField struct {
	id  uint16
	u32 *uint32
	u8  *uint8
}

func ethU32(id uint16, p *uint32) ethTypedField { return ethTypedField{id: id, u32: p} }
func ethU8(id uint16, p *uint8) ethTypedField   { return ethTypedField{id: id, u8: p} }

// ethTypedFields lists a message's typed scalar fields by attribute, for
// the kinds the rows set; FEC's bitset is held apart (ethFecProblem).
func ethTypedFields(m *EthtoolMessage) []ethTypedField {
	switch {
	case m.Rings != nil:
		r := m.Rings
		return []ethTypedField{ethU32(2, r.RXMax), ethU32(3, r.RXMiniMax), ethU32(4, r.RXJumboMax), ethU32(5, r.TXMax),
			ethU32(6, r.RX), ethU32(7, r.RXMini), ethU32(8, r.RXJumbo), ethU32(9, r.TX), ethU32(10, r.RXBufLen),
			ethU8(11, r.TCPDataSplit), ethU32(12, r.CQESize), ethU8(13, r.TXPush), ethU8(14, r.RXPush),
			ethU32(15, r.TXPushBufLen), ethU32(16, r.TXPushBufLenMax), ethU32(17, r.HDSThresh), ethU32(18, r.HDSThreshMax)}
	case m.Channels != nil:
		c := m.Channels
		return []ethTypedField{ethU32(2, c.RXMax), ethU32(3, c.TXMax), ethU32(4, c.OtherMax), ethU32(5, c.CombinedMax),
			ethU32(6, c.RX), ethU32(7, c.TX), ethU32(8, c.Other), ethU32(9, c.Combined)}
	case m.Pause != nil:
		return []ethTypedField{ethU8(2, m.Pause.Autoneg), ethU8(3, m.Pause.RX), ethU8(4, m.Pause.TX)}
	case m.Coalesce != nil:
		return ethCoalesceTyped(m.Coalesce)
	}
	return nil
}

func ethCoalesceTyped(c *EthtoolCoalesce) []ethTypedField {
	return []ethTypedField{ethU32(2, c.RXUsecs), ethU32(3, c.RXMaxFrames), ethU32(4, c.RXUsecsIRQ),
		ethU32(5, c.RXMaxFramesIRQ), ethU32(6, c.TXUsecs), ethU32(7, c.TXMaxFrames), ethU32(8, c.TXUsecsIRQ),
		ethU32(9, c.TXMaxFramesIRQ), ethU32(10, c.StatsBlockUsecs), ethU8(11, c.UseAdaptiveRX),
		ethU8(12, c.UseAdaptiveTX), ethU32(13, c.PktRateLow), ethU32(14, c.RXUsecsLow), ethU32(15, c.RXMaxFramesLow),
		ethU32(16, c.TXUsecsLow), ethU32(17, c.TXMaxFramesLow), ethU32(18, c.PktRateHigh), ethU32(19, c.RXUsecsHigh),
		ethU32(20, c.RXMaxFramesHigh), ethU32(21, c.TXUsecsHigh), ethU32(22, c.TXMaxFramesHigh),
		ethU32(23, c.RateSampleInterval), ethU8(24, c.UseCQEModeTX), ethU8(25, c.UseCQEModeRX),
		ethU32(26, c.TXAggrMaxBytes), ethU32(27, c.TXAggrMaxFrames), ethU32(28, c.TXAggrTimeUsecs),
		ethU32(31, c.RXCQEFrames), ethU32(32, c.RXCQENsecs)}
}

// ethTyped is a message's present typed fields, attribute to value.
func ethTyped(m *EthtoolMessage) map[uint16]uint64 {
	out := map[uint16]uint64{}
	for _, f := range ethTypedFields(m) {
		switch {
		case f.u32 != nil:
			out[f.id] = uint64(*f.u32)
		case f.u8 != nil:
			out[f.id] = uint64(*f.u8)
		}
	}
	return out
}

// ethWant is what a set row's facts say its request carries, and what the
// readback must show instead where rt.<name> says so: another value, or
// nothing (gone) for rt.<name>=absent.
type ethWant struct {
	set, get uint8
	attrs    map[uint16]uint64
	rt       map[uint16]uint64
	gone     map[uint16]bool
}

func ethFactU8(ax map[string]string, k string) (uint8, error) {
	n, err := strconv.ParseUint(ax[k], 10, 8)
	if err != nil {
		return 0, fmt.Errorf("fact %s: %w", k, err)
	}
	return uint8(n), nil
}

// ethWantFromAxes reads a set row's facts: set and get name the commands,
// rt.<name> a readback value, fec-* the FEC model, and every other key an
// argument of ethFields[set].
func ethWantFromAxes(ax map[string]string) (ethWant, error) {
	var w ethWant
	var err error
	if w.set, err = ethFactU8(ax, "set"); err != nil {
		return w, err
	}
	if w.get, err = ethFactU8(ax, "get"); err != nil {
		return w, err
	}
	fields, ok := ethFields[w.set]
	if !ok {
		return w, fmt.Errorf("no fields for set command %d", w.set)
	}
	w.attrs, w.rt, w.gone = map[uint16]uint64{}, map[uint16]uint64{}, map[uint16]bool{}
	for _, k := range slices.Sorted(maps.Keys(ax)) {
		name, isRT := strings.CutPrefix(k, "rt.")
		if k == "set" || k == "get" || strings.HasPrefix(name, "fec-") {
			continue
		}
		id, ok := fields[name]
		if !ok {
			return w, fmt.Errorf("fact %s names no argument of set command %d", k, w.set)
		}
		if isRT && ax[k] == "absent" {
			w.gone[id] = true
			continue
		}
		n, err := strconv.ParseUint(ax[k], 10, 32)
		if err != nil {
			return w, fmt.Errorf("fact %s: %w", k, err)
		}
		if isRT {
			w.rt[id] = n
		} else {
			w.attrs[id] = n
		}
	}
	for _, id := range slices.Concat(slices.Collect(maps.Keys(w.rt)), slices.Collect(maps.Keys(w.gone))) {
		if _, ok := w.attrs[id]; !ok {
			return w, fmt.Errorf("rt fact for attribute %d, which the row does not set", id)
		}
	}
	return w, nil
}

// ethRequestProblem holds a decoded set request to the want: the device's
// name, and exactly the attributes the facts name, each with its value.
func ethRequestProblem(w ethWant, m *EthtoolMessage, dev string) error {
	var errs []error
	if m.Command != w.set || !m.Request {
		errs = append(errs, fmt.Errorf("command %d request %v, want set %d", m.Command, m.Request, w.set))
	}
	if m.Header.DeviceName != dev {
		errs = append(errs, fmt.Errorf("header device %q, want %q", m.Header.DeviceName, dev))
	}
	if w.set == ethSetFEC {
		return errors.Join(errs...)
	}
	if got := ethTyped(m); !maps.Equal(got, w.attrs) {
		errs = append(errs, fmt.Errorf("attributes %v, want %v", got, w.attrs))
	}
	if n := len(m.Attributes) - 1; n != len(w.attrs) {
		errs = append(errs, fmt.Errorf("%d attributes besides the header, want %d", n, len(w.attrs)))
	}
	return errors.Join(errs...)
}

// ethRoundTripProblem holds a GET reply to the want: each attribute set
// reads back as set, or as its rt fact.
func ethRoundTripProblem(w ethWant, reply *EthtoolMessage) error {
	if reply.Request || reply.Command != w.set {
		return fmt.Errorf("reply command %d request %v, want %d", reply.Command, reply.Request, w.set)
	}
	got := ethTyped(reply)
	var errs []error
	for _, id := range slices.Sorted(maps.Keys(w.attrs)) {
		want := w.attrs[id]
		if w.gone[id] {
			if v, ok := got[id]; ok {
				errs = append(errs, fmt.Errorf("attribute %d reads back %d, want it absent", id, v))
			}
			continue
		}
		if v, ok := w.rt[id]; ok {
			want = v
		}
		if v, ok := got[id]; !ok || v != want {
			errs = append(errs, fmt.Errorf("attribute %d reads back %d (present %v), want %d", id, v, ok, want))
		}
	}
	return errors.Join(errs...)
}

// ethBitIndexes is the set bits of a bitset, compact or verbose.
func ethBitIndexes(b *EthtoolBitset) []uint32 {
	var out []uint32
	if b == nil {
		return out
	}
	var base uint32
	for _, word := range b.Value {
		for i := range uint32(32) {
			if word&(1<<i) != 0 {
				out = append(out, base+i)
			}
		}
		base += 32
	}
	for _, bit := range b.Bits {
		if bit.Index != nil && (bit.Value || b.NoMask) {
			out = append(out, *bit.Index)
		}
	}
	slices.Sort(out)
	return out
}

// ethFecRequestProblem holds a FEC_SET's modes to the facts: a NOMASK
// list of the names ethtool sends (fec-names, in order) and fec-auto.
func ethFecRequestProblem(ax map[string]string, m *EthtoolMessage) error {
	if m.FEC == nil || m.FEC.Modes == nil || m.FEC.Auto == nil {
		return fmt.Errorf("FEC_SET without modes or auto: %+v", m.FEC)
	}
	names := make([]string, 0, len(m.FEC.Modes.Bits))
	for _, b := range m.FEC.Modes.Bits {
		names = append(names, b.Name)
	}
	var errs []error
	if got := strings.Join(names, ","); !m.FEC.Modes.NoMask || got != ax["fec-names"] {
		errs = append(errs, fmt.Errorf("modes %q nomask %v, want %q nomask", got, m.FEC.Modes.NoMask, ax["fec-names"]))
	}
	if got := strconv.Itoa(int(*m.FEC.Auto)); got != ax["fec-auto"] {
		errs = append(errs, fmt.Errorf("auto %s, want %s", got, ax["fec-auto"]))
	}
	return errors.Join(errs...)
}

// ethFecRoundTripProblem holds a FEC GET reply to the facts: the
// configured modes' link-mode bits, auto, and the active mode.
func ethFecRoundTripProblem(ax map[string]string, reply *EthtoolMessage) error {
	if reply.FEC == nil || reply.FEC.Auto == nil || reply.FEC.Active == nil {
		return fmt.Errorf("FEC reply without auto or active: %+v", reply.FEC)
	}
	idx := ethBitIndexes(reply.FEC.Modes)
	bits := make([]string, 0, len(idx))
	for _, i := range idx {
		bits = append(bits, strconv.FormatUint(uint64(i), 10))
	}
	var errs []error
	if got := strings.Join(bits, ","); got != ax["fec-modes"] {
		errs = append(errs, fmt.Errorf("modes %q, want %q", got, ax["fec-modes"]))
	}
	if got := strconv.Itoa(int(*reply.FEC.Auto)); got != ax["fec-auto"] {
		errs = append(errs, fmt.Errorf("auto %s, want %s", got, ax["fec-auto"]))
	}
	if got := strconv.FormatUint(uint64(*reply.FEC.Active), 10); got != ax["fec-active"] {
		errs = append(errs, fmt.Errorf("active %s, want %s", got, ax["fec-active"]))
	}
	return errors.Join(errs...)
}

// ethShowProblem holds a show row's GET request: the command, the
// device's name, and ETHTOOL_FLAG_STATS exactly when -I was given.
func ethShowProblem(get uint8, stats bool, dev string, body []byte) error {
	g, err := ParseGenericNetlink(body)
	if err != nil {
		return err
	}
	var h EthtoolHeader
	if err := decodeEthtoolHeader(&h, g.Attributes); err != nil {
		return err
	}
	var errs []error
	if g.Command != get {
		errs = append(errs, fmt.Errorf("command %d, want %d", g.Command, get))
	}
	if h.DeviceName != dev {
		errs = append(errs, fmt.Errorf("header device %q, want %q", h.DeviceName, dev))
	}
	if got := h.Flags != nil && *h.Flags&ethFlagStats != 0; got != stats {
		errs = append(errs, fmt.Errorf("stats flag %v, want %v", got, stats))
	}
	return errors.Join(errs...)
}

// ethJSONKeys maps each JSON printer's keys to the reply's attributes
// (netlink/rings.c, channels.c, pause.c and coalesce.c). The coalescing
// printer spells four keys rx-frame-low where -C takes rx-frames-low.
var ethJSONKeys = map[EthtoolKind]map[string]uint16{
	EthtoolRingsKind: {"rx-max": 2, "rx-mini-max": 3, "rx-jumbo-max": 4, "tx-max": 5, "rx": 6, "rx-mini": 7,
		"rx-jumbo": 8, "tx": 9, "rx-buf-len": 10, "tcp-data-split": 11, "cqe-size": 12, "tx-push": 13, "rx-push": 14,
		"tx-push-buf-len": 15, "tx-push-buff-max-len": 16, "hds-thresh": 17, "hds-thresh-max": 18},
	EthtoolChannelsKind: {"rx-max": 2, "tx-max": 3, "other-max": 4, "combined-max": 5, "rx": 6, "tx": 7, "other": 8, "combined": 9},
	EthtoolPauseKind:    {"autonegotiate": 2, "rx": 3, "tx": 4},
	EthtoolCoalesceKind: {"rx-usecs": 2, "rx-frames": 3, "rx-usecs-irq": 4, "rx-frames-irq": 5, "tx-usecs": 6,
		"tx-frames": 7, "tx-usecs-irq": 8, "tx-frames-irq": 9, "stats-block-usecs": 10, "adaptive-rx": 11,
		"adaptive-tx": 12, "pkt-rate-low": 13, "rx-usecs-low": 14, "rx-frame-low": 15, "tx-usecs-low": 16,
		"tx-frame-low": 17, "pkt-rate-high": 18, "rx-usecs-high": 19, "rx-frame-high": 20, "tx-usecs-high": 21,
		"tx-frame-high": 22, "sample-interval": 23, "cqe-mode-tx": 24, "cqe-mode-rx": 25, "tx-aggr-max-bytes": 26,
		"tx-aggr-max-frames": 27, "tx-aggr-time-usecs": 28, "rx-cqe-frames": 31, "rx-cqe-nsecs": 32},
}

// ethJSONValue reads one printed value as the attribute's number: a JSON
// number, a boolean (u8 0 or 1), or tcp-data-split's off and on (1, 2).
func ethJSONValue(v any) (uint64, error) {
	switch x := v.(type) {
	case float64:
		if x < 0 || x > 1<<32-1 || x != float64(uint64(x)) {
			return 0, fmt.Errorf("%v is no u32", x)
		}
		return uint64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		switch x {
		case "off":
			return 1, nil
		case "on":
			return 2, nil
		}
	}
	return 0, fmt.Errorf("%v (%T) is no value the printers print", v, v)
}

// ethJSONProblem holds a --json show's stdout to its reply: one object for
// the device, each key a reply attribute with the reply's value. It
// returns how many values it compared.
func ethJSONProblem(out []byte, dev string, reply *EthtoolMessage) (int, error) {
	keys, ok := ethJSONKeys[reply.Kind]
	if !ok {
		return 0, nil
	}
	var objs []map[string]any
	if err := json.Unmarshal(out, &objs); err != nil {
		return 0, err
	}
	if len(objs) != 1 || objs[0]["ifname"] != dev {
		return 0, fmt.Errorf("%d objects, want one for %s", len(objs), dev)
	}
	got := ethTyped(reply)
	var errs []error
	n := 0
	for _, k := range slices.Sorted(maps.Keys(objs[0])) {
		if k == "ifname" {
			continue
		}
		id, ok := keys[k]
		if !ok {
			errs = append(errs, fmt.Errorf("key %q, which the %s printer does not print", k, reply.Kind))
			continue
		}
		v, err := ethJSONValue(objs[0][k])
		if want, present := got[id]; err != nil || !present || v != want {
			errs = append(errs, fmt.Errorf("%s printed %v, reply attribute %d is %d (present %v)", k, objs[0][k], id, want, present))
		}
		n++
	}
	return n, errors.Join(errs...)
}

// ethReplyCmd maps the *_GETs whose JSON printers ethJSONKeys knows to
// the kernel's *_GET_REPLY (the kernel's enum, which runs one ahead of the
// user's for these).
var ethReplyCmd = map[uint8]uint8{15: 16, 17: 18, 19: 20, 21: 22}

// ethGenlID is the ethtool family's id, from the CTRL_CMD_NEWFAMILY reply
// in msgs.
func ethGenlID(msgs []capturedNlMsg) (uint16, bool) {
	for _, m := range msgs {
		if m.h.Type != ethGenlCtrl || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if f, err := ParseGenericNetlinkFamily(m.body); err == nil && f.Name == "ethtool" {
			return f.ID, true
		}
	}
	return 0, false
}

// ethMsgs is msgs' ethtool messages of genl command cmd: requests or
// replies.
func ethMsgs(msgs []capturedNlMsg, id uint16, cmd uint8, request bool) []capturedNlMsg {
	var out []capturedNlMsg
	for _, m := range msgs {
		if m.h.Type == id && len(m.body) > 0 && m.body[0] == cmd && (m.h.Flags&unix.NLM_F_REQUEST != 0) == request {
			out = append(out, m)
		}
	}
	return out
}

// ethCounts is how many requests, round trips and JSON values the ethtool
// rows checked.
type ethCounts struct{ requests, roundTrips, json int }

func TestWsEthtoolCorpus(t *testing.T) {
	var c ethCounts
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "eth") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			if strings.HasPrefix(row.ID, "ethshow/") {
				wsEthShowRow(t, dir, row, &c)
				return
			}
			wsEthSetRow(t, dir, row, &c)
		})
	}
	t.Logf("O3 checked %d ethtool requests, O4 %d round trips and %d JSON values", c.requests, c.roundTrips, c.json)
	if c.requests == 0 || c.roundTrips == 0 || c.json == 0 {
		t.Error("O3 or O4 checked nothing: are there ethtool rows under testdata/*/ws?")
	}
}

// wsEthSetRow checks a set row: its SET (O3) and, when accepted, the
// readback's GET reply (O4).
func wsEthSetRow(t *testing.T, dir string, row wsRow, c *ethCounts) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	id, ok := ethGenlID(msgs)
	if !ok {
		t.Fatalf("no ethtool family lookup\n  cmd: %s", row.Cmd)
	}
	w, err := ethWantFromAxes(row.Axes)
	if err != nil {
		t.Fatalf("O3 model: %v", err)
	}
	sent := ethMsgs(msgs, id, w.set, true)
	if row.Expect.RefusedBy == wsRefusedAckFail {
		if len(sent) != 0 {
			t.Errorf("O3: ethtool refused its arguments but sent %d SETs\n  cmd: %s", len(sent), row.Cmd)
		}
		return
	}
	if len(sent) != 1 {
		t.Fatalf("O3: %d SETs, want 1\n  cmd: %s", len(sent), row.Cmd)
	}
	m, err := ParseEthtool(sent[0].body, sent[0].h.Flags)
	if err != nil {
		t.Fatalf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	err = ethRequestProblem(w, &m, strings.Fields(row.Cmd)[2])
	if w.set == ethSetFEC {
		err = errors.Join(err, ethFecRequestProblem(row.Axes, &m))
	}
	if err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	c.requests++
	if wsReadRC(t, dir) != 0 {
		return
	}
	rb := pcapNlMsgs(t, filepath.Join(dir, "netlink_generic_show.pcap"))
	replies := ethMsgs(rb, id, w.set, false)
	if len(replies) != 1 {
		t.Fatalf("O4: %d GET replies, want 1\n  cmd: %s", len(replies), row.Cmd)
	}
	reply, err := ParseEthtool(replies[0].body, replies[0].h.Flags)
	if err == nil {
		err = ethRoundTripProblem(w, &reply)
		if w.set == ethSetFEC {
			err = ethFecRoundTripProblem(row.Axes, &reply)
		}
	}
	if err != nil {
		t.Errorf("O4: %v\n  cmd: %s", err, row.Cmd)
	}
	c.roundTrips++
}

// wsEthShowRow checks a show row: its GET (O3) and, for --json, that the
// printed values are the reply's (O4).
func wsEthShowRow(t *testing.T, dir string, row wsRow, c *ethCounts) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	id, ok := ethGenlID(msgs)
	if row.Expect.RefusedBy == wsRefusedIproute2 {
		if ok {
			t.Errorf("O3: ethtool refused its arguments but looked the family up\n  cmd: %s", row.Cmd)
		}
		return
	}
	if !ok {
		t.Fatalf("no ethtool family lookup\n  cmd: %s", row.Cmd)
	}
	get, err := ethFactU8(row.Axes, "get")
	if err != nil {
		t.Fatalf("O3 model: %v", err)
	}
	fields := strings.Fields(row.Cmd)
	dev := fields[len(fields)-1]
	sent := ethMsgs(msgs, id, get, true)
	if len(sent) != 1 {
		t.Fatalf("O3: %d GETs of command %d, want 1\n  cmd: %s", len(sent), get, row.Cmd)
	}
	if err := ethShowProblem(get, row.Axes["stats"] == "1", dev, sent[0].body); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	c.requests++
	reply, ok := ethReplyCmd[get]
	if row.Axes["json"] != "1" || !ok || wsReadRC(t, dir) != 0 {
		return
	}
	replies := ethMsgs(msgs, id, reply, false)
	if len(replies) != 1 {
		t.Fatalf("O4: %d replies, want 1\n  cmd: %s", len(replies), row.Cmd)
	}
	m, err := ParseEthtool(replies[0].body, replies[0].h.Flags)
	if err != nil {
		t.Fatalf("O4: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "ip.out"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := ethJSONProblem(out, dev, &m)
	if err != nil {
		t.Errorf("O4: %v\n  cmd: %s", err, row.Cmd)
	}
	c.json += n
}

// ethTestHdr is an ETHTOOL_A_*_HEADER naming dev, with flags when given.
func ethTestHdr(dev string, flags ...uint32) []byte {
	in := ethtoolTestAttr(2, append([]byte(dev), 0)...)
	for _, fl := range flags {
		in = append(in, ethtoolTestAttr(3, ethtoolTestU32(fl)...)...)
	}
	return ethtoolTestAttr(1|unix.NLA_F_NESTED, in...)
}

// ethTestMsg parses a hand-built ethtool message, failing the test if it
// does not parse.
func ethTestMsg(t *testing.T, flags uint16, cmd byte, attrs ...[]byte) EthtoolMessage {
	t.Helper()
	m, err := ParseEthtool(ethtoolTestBody(cmd, attrs...), flags)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestEthWantFromAxes(t *testing.T) {
	tests := []struct {
		description string
		axes        map[string]string
		expected    string // fmt of attrs, rt, gone, or the error's text
	}{
		{description: "positive: a ring set", axes: map[string]string{"set": "16", "get": "15", "rx": "1024", "tcp-data-split": "0", "rt.tcp-data-split": "2"},
			expected: "map[6:1024 11:0] map[11:2] map[]"},
		{description: "positive: a channel read back absent", axes: map[string]string{"set": "18", "get": "17", "rx": "0", "rt.rx": "absent"},
			expected: "map[6:0] map[] map[6:true]"},
		{description: "positive: FEC's facts are held apart", axes: map[string]string{"set": "30", "get": "29", "fec-names": "RS", "fec-auto": "0"},
			expected: "map[] map[] map[]"},
		{description: "boundary: the largest u32", axes: map[string]string{"set": "20", "get": "19", "rx-usecs": "4294967295"},
			expected: "map[2:4294967295] map[] map[]"},
		{description: "negative: no set fact", axes: map[string]string{"get": "15"}, expected: "fact set"},
		{description: "negative: a set command no table has", axes: map[string]string{"set": "99", "get": "98"}, expected: "no fields for set command 99"},
		{description: "negative: an argument the command lacks", axes: map[string]string{"set": "22", "get": "21", "combined": "1"},
			expected: "fact combined names no argument of set command 22"},
		{description: "negative: an rt fact for an unset attribute", axes: map[string]string{"set": "16", "get": "15", "rt.rx": "1"},
			expected: "rt fact for attribute 6"},
		{description: "corner: a value over the u32 range", axes: map[string]string{"set": "16", "get": "15", "rx": "4294967296"},
			expected: "fact rx"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w, err := ethWantFromAxes(tc.axes)
			got := fmt.Sprint(w.attrs, " ", w.rt, " ", w.gone)
			if err != nil {
				got = err.Error()
			}
			if !strings.HasPrefix(got, tc.expected) {
				t.Errorf("got %q, want prefix %q", got, tc.expected)
			}
		})
	}
}

func TestEthRequestProblem(t *testing.T) {
	ring := ethWant{set: 16, attrs: map[uint16]uint64{6: 1024, 11: 2}}
	tests := []struct {
		description string
		want        ethWant
		attrs       [][]byte
		cmd         byte
		dev         string
		expected    string // "" for no problem, else a fragment
	}{
		{description: "positive: the attributes the facts name", want: ring, cmd: 16, dev: "eth0",
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(6, ethtoolTestU32(1024)...), ethtoolTestAttr(11, 2)}},
		{description: "negative: an attribute the facts lack", want: ring, cmd: 16, dev: "eth0",
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(6, ethtoolTestU32(1024)...), ethtoolTestAttr(11, 2),
				ethtoolTestAttr(9, ethtoolTestU32(1)...)}, expected: "attributes map[6:1024 9:1 11:2]"},
		{description: "negative: a value other than the fact's", want: ring, cmd: 16, dev: "eth0",
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(6, ethtoolTestU32(512)...), ethtoolTestAttr(11, 2)}, expected: "6:512"},
		{description: "negative: another device's name", want: ring, cmd: 16, dev: "eth1",
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(6, ethtoolTestU32(1024)...), ethtoolTestAttr(11, 2)}, expected: `header device "eth0"`},
		{description: "negative: the GET, not the SET", want: ring, cmd: 15, dev: "eth0",
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(6, ethtoolTestU32(1024)...), ethtoolTestAttr(11, 2)}, expected: "command 15"},
		{description: "corner: an attribute no typed field decodes still counts", want: ethWant{set: 16, attrs: map[uint16]uint64{}}, cmd: 16, dev: "eth0",
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(99, 1)}, expected: "1 attributes besides the header"},
		{description: "boundary: FEC's attributes are left to ethFecRequestProblem", want: ethWant{set: 30}, cmd: 30, dev: "eth0",
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(3, 1)}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			m := ethTestMsg(t, unix.NLM_F_REQUEST, tc.cmd, tc.attrs...)
			err := ethRequestProblem(tc.want, &m, tc.dev)
			if (err == nil) != (tc.expected == "") || (err != nil && !strings.Contains(err.Error(), tc.expected)) {
				t.Errorf("got %v, want %q", err, tc.expected)
			}
		})
	}
}

func TestEthRoundTripProblem(t *testing.T) {
	chans := ethWant{set: 18, attrs: map[uint16]uint64{6: 0, 9: 2}, rt: map[uint16]uint64{}, gone: map[uint16]bool{6: true}}
	coal := ethWant{set: 20, attrs: map[uint16]uint64{25: 1}, rt: map[uint16]uint64{25: 0}, gone: map[uint16]bool{}}
	tests := []struct {
		description string
		want        ethWant
		cmd         byte
		attrs       [][]byte
		expected    string
	}{
		{description: "positive: combined reads back, rx absent", want: chans, cmd: 18,
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(9, ethtoolTestU32(2)...)}},
		{description: "negative: rx present where the device omits it", want: chans, cmd: 18,
			attrs:    [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(6, ethtoolTestU32(0)...), ethtoolTestAttr(9, ethtoolTestU32(2)...)},
			expected: "attribute 6 reads back 0, want it absent"},
		{description: "negative: combined reads back other than set", want: chans, cmd: 18,
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(9, ethtoolTestU32(4)...)}, expected: "attribute 9 reads back 4"},
		{description: "negative: combined missing", want: chans, cmd: 18,
			attrs: [][]byte{ethTestHdr("eth0")}, expected: "present false"},
		{description: "positive: a lost CQE mode reads back as its rt fact", want: coal, cmd: 20,
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(25, 0)}},
		{description: "corner: a lost CQE mode that read back as set is a change", want: coal, cmd: 20,
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(25, 1)}, expected: "attribute 25 reads back 1"},
		{description: "boundary: another command's reply", want: coal, cmd: 18,
			attrs: [][]byte{ethTestHdr("eth0")}, expected: "reply command 18"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			m := ethTestMsg(t, 0, tc.cmd, tc.attrs...)
			err := ethRoundTripProblem(tc.want, &m)
			if (err == nil) != (tc.expected == "") || (err != nil && !strings.Contains(err.Error(), tc.expected)) {
				t.Errorf("got %v, want %q", err, tc.expected)
			}
		})
	}
}

// ethTestBitName is a verbose bitset bit with a name.
func ethTestBitName(name string) []byte {
	return ethtoolTestAttr(1|unix.NLA_F_NESTED, ethtoolTestAttr(2, append([]byte(name), 0)...)...)
}

func TestEthFecProblems(t *testing.T) {
	ax := map[string]string{"fec-names": "None,RS", "fec-auto": "1", "fec-modes": "49,50", "fec-active": "50"}
	verbose := func(nomask bool, names ...string) []byte {
		bits := make([]byte, 0, 16*len(names))
		for _, n := range names {
			bits = append(bits, ethTestBitName(n)...)
		}
		in := ethtoolTestAttr(3|unix.NLA_F_NESTED, bits...)
		if nomask {
			in = append(ethtoolTestAttr(1), in...)
		}
		return ethtoolTestAttr(2|unix.NLA_F_NESTED, in...)
	}
	compact := func(word uint32) []byte {
		return ethtoolTestAttr(2|unix.NLA_F_NESTED, ethtoolTestJoin(ethtoolTestAttr(1), ethtoolTestAttr(2, ethtoolTestU32(96)...),
			ethtoolTestAttr(4, ethtoolTestJoin(ethtoolTestU32(0), ethtoolTestU32(word), ethtoolTestU32(0))...))...)
	}
	rs, none := uint32(1)<<(50-32), uint32(1)<<(49-32)
	tests := []struct {
		description string
		request     bool
		attrs       [][]byte
		expected    string
	}{
		{description: "positive: the names ethtool sends, NOMASK, with auto", request: true,
			attrs: [][]byte{ethTestHdr("eth0"), verbose(true, "None", "RS"), ethtoolTestAttr(3, 1)}},
		{description: "negative: names in another order", request: true,
			attrs: [][]byte{ethTestHdr("eth0"), verbose(true, "RS", "None"), ethtoolTestAttr(3, 1)}, expected: `modes "RS,None"`},
		{description: "negative: a list with a mask", request: true,
			attrs: [][]byte{ethTestHdr("eth0"), verbose(false, "None", "RS"), ethtoolTestAttr(3, 1)}, expected: "nomask false"},
		{description: "negative: auto off", request: true,
			attrs: [][]byte{ethTestHdr("eth0"), verbose(true, "None", "RS"), ethtoolTestAttr(3, 0)}, expected: "auto 0"},
		{description: "corner: no modes at all", request: true,
			attrs: [][]byte{ethTestHdr("eth0"), ethtoolTestAttr(3, 1)}, expected: "without modes"},
		{description: "positive: the reply's compact modes, auto and active",
			attrs: [][]byte{ethTestHdr("eth0"), compact(rs | none), ethtoolTestAttr(3, 1), ethtoolTestAttr(4, ethtoolTestU32(50)...)}},
		{description: "negative: another active mode",
			attrs:    [][]byte{ethTestHdr("eth0"), compact(rs | none), ethtoolTestAttr(3, 1), ethtoolTestAttr(4, ethtoolTestU32(49)...)},
			expected: "active 49"},
		{description: "boundary: a configured mode missing",
			attrs:    [][]byte{ethTestHdr("eth0"), compact(rs), ethtoolTestAttr(3, 1), ethtoolTestAttr(4, ethtoolTestU32(50)...)},
			expected: `modes "50"`},
		{description: "corner: a reply without active",
			attrs: [][]byte{ethTestHdr("eth0"), compact(rs | none), ethtoolTestAttr(3, 1)}, expected: "without auto or active"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var err error
			if tc.request {
				m := ethTestMsg(t, unix.NLM_F_REQUEST, 30, tc.attrs...)
				err = ethFecRequestProblem(ax, &m)
			} else {
				m := ethTestMsg(t, 0, 30, tc.attrs...)
				err = ethFecRoundTripProblem(ax, &m)
			}
			if (err == nil) != (tc.expected == "") || (err != nil && !strings.Contains(err.Error(), tc.expected)) {
				t.Errorf("got %v, want %q", err, tc.expected)
			}
		})
	}
}

func TestEthBitIndexes(t *testing.T) {
	idx := func(v uint32) *uint32 { return &v }
	tests := []struct {
		description string
		b           *EthtoolBitset
		expected    []uint32
	}{
		{description: "positive: compact words, bits counted across words", b: &EthtoolBitset{Value: []uint32{1, 1 << 31}}, expected: []uint32{0, 63}},
		{description: "positive: verbose true bits", b: &EthtoolBitset{Bits: []EthtoolBit{{Index: idx(74), Value: true}, {Index: idx(3)}}},
			expected: []uint32{74}},
		{description: "corner: a NOMASK list's bits are all set", b: &EthtoolBitset{NoMask: true, Bits: []EthtoolBit{{Index: idx(50)}, {Index: idx(49)}}},
			expected: []uint32{49, 50}},
		{description: "boundary: a bit with no index counts for nothing", b: &EthtoolBitset{NoMask: true, Bits: []EthtoolBit{{Name: "RS"}}}},
		{description: "negative: no bitset", b: nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := ethBitIndexes(tc.b); !slices.Equal(got, tc.expected) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestEthShowProblem(t *testing.T) {
	tests := []struct {
		description string
		get         uint8
		stats       bool
		dev         string
		body        []byte
		expected    string
	}{
		{description: "positive: a pause GET with statistics", get: 21, stats: true, dev: "eth0",
			body: ethtoolTestBody(21, ethTestHdr("eth0", ethFlagStats))},
		{description: "positive: a ring GET without", get: 15, dev: "eth0", body: ethtoolTestBody(15, ethTestHdr("eth0"))},
		{description: "negative: the flag on a command that ignores -I", get: 15, dev: "eth0",
			body: ethtoolTestBody(15, ethTestHdr("eth0", ethFlagStats)), expected: "stats flag true"},
		{description: "negative: -I lost", get: 29, stats: true, dev: "eth0",
			body: ethtoolTestBody(29, ethTestHdr("eth0", 1)), expected: "stats flag false"},
		{description: "negative: another command", get: 17, dev: "eth0", body: ethtoolTestBody(15, ethTestHdr("eth0")), expected: "command 15"},
		{description: "negative: another device", get: 15, dev: "lo", body: ethtoolTestBody(15, ethTestHdr("eth0")), expected: `header device "eth0"`},
		{description: "corner: a command ParseEthtool cannot decode still has a header", get: 25, dev: "eth0",
			body: ethtoolTestBody(25, ethTestHdr("eth0"))},
		{description: "boundary: a body too short for genl", get: 15, dev: "eth0", body: []byte{15, 1}, expected: "genl"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := ethShowProblem(tc.get, tc.stats, tc.dev, tc.body)
			if (err == nil) != (tc.expected == "") || (err != nil && !strings.Contains(err.Error(), tc.expected)) {
				t.Errorf("got %v, want %q", err, tc.expected)
			}
		})
	}
}

func TestEthJSONProblem(t *testing.T) {
	tests := []struct {
		description string
		cmd         byte
		attrs       [][]byte
		out         string
		expected    string // "" for no problem
		n           int
	}{
		{description: "positive: rings, a number, a bool and the split mode", cmd: 16,
			attrs: [][]byte{ethtoolTestAttr(6, ethtoolTestU32(512)...), ethtoolTestAttr(13, 0), ethtoolTestAttr(11, 2)},
			out:   `[{"ifname":"eth0","rx":512,"tx-push":false,"tcp-data-split":"on"}]`, n: 3},
		{description: "positive: coalescing's rx-frame-low key", cmd: 20, attrs: [][]byte{ethtoolTestAttr(15, ethtoolTestU32(2)...)},
			out: `[{"ifname":"eth0","rx-frame-low":2}]`, n: 1},
		{description: "negative: a printed value the reply does not hold", cmd: 18, attrs: [][]byte{ethtoolTestAttr(9, ethtoolTestU32(4)...)},
			out: `[{"ifname":"eth0","combined":3}]`, expected: "combined printed 3", n: 1},
		{description: "negative: a key the printer does not print", cmd: 22, attrs: [][]byte{ethtoolTestAttr(3, 1)},
			out: `[{"ifname":"eth0","rx-frames-low":1}]`, expected: `key "rx-frames-low"`},
		{description: "negative: another device's object", cmd: 22, out: `[{"ifname":"lo"}]`, expected: "want one for eth0"},
		{description: "boundary: a printed key whose attribute is absent", cmd: 22, out: `[{"ifname":"eth0","tx":true}]`,
			expected: "present false", n: 1},
		{description: "corner: a split mode the printers never print", cmd: 16, attrs: [][]byte{ethtoolTestAttr(11, 0)},
			out: `[{"ifname":"eth0","tcp-data-split":"n/a"}]`, expected: "tcp-data-split printed", n: 1},
		{description: "corner: no JSON at all", cmd: 16, out: ``, expected: "unexpected end"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			m := ethTestMsg(t, 0, tc.cmd, append([][]byte{ethTestHdr("eth0")}, tc.attrs...)...)
			n, err := ethJSONProblem([]byte(tc.out), "eth0", &m)
			if (err == nil) != (tc.expected == "") || (err != nil && !strings.Contains(err.Error(), tc.expected)) || n != tc.n {
				t.Errorf("got %d, %v, want %d, %q", n, err, tc.n, tc.expected)
			}
		})
	}
}

func TestEthJSONValue(t *testing.T) {
	tests := []struct {
		description string
		in          any
		expected    string
	}{
		{description: "positive: a number", in: 4096.0, expected: "4096"},
		{description: "positive: true", in: true, expected: "1"},
		{description: "positive: off is DISABLED", in: "off", expected: "1"},
		{description: "positive: on is ENABLED", in: "on", expected: "2"},
		{description: "boundary: the largest u32", in: 4294967295.0, expected: "4294967295"},
		{description: "negative: over the u32 range", in: 4294967296.0, expected: "error"},
		{description: "negative: a fraction", in: 1.5, expected: "error"},
		{description: "negative: below zero", in: -1.0, expected: "error"},
		{description: "corner: another string", in: "n/a", expected: "error"},
		{description: "corner: null", in: nil, expected: "error"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, err := ethJSONValue(tc.in)
			got := strconv.FormatUint(v, 10)
			if err != nil {
				got = "error"
			}
			if got != tc.expected {
				t.Errorf("got %s (%v), want %s", got, err, tc.expected)
			}
		})
	}
}

func TestEthGenlID(t *testing.T) {
	family := func(name string, id uint16) []byte {
		return ethtoolTestBody(1, ethtoolTestAttr(1, byte(id), byte(id>>8)), ethtoolTestAttr(2, append([]byte(name), 0)...))
	}
	msg := func(typ, flags uint16, body []byte) capturedNlMsg {
		return capturedNlMsg{h: NlMsgHdr{Type: typ, Flags: flags}, body: body}
	}
	tests := []struct {
		description string
		msgs        []capturedNlMsg
		expected    string
	}{
		{description: "positive: the ethtool reply", msgs: []capturedNlMsg{msg(ethGenlCtrl, 0, family("ethtool", 21))}, expected: "21 true"},
		{description: "negative: another family's reply", msgs: []capturedNlMsg{msg(ethGenlCtrl, 0, family("nlctrl", 16))}, expected: "0 false"},
		{description: "negative: the request, not the reply", msgs: []capturedNlMsg{msg(ethGenlCtrl, unix.NLM_F_REQUEST, family("ethtool", 21))},
			expected: "0 false"},
		{description: "corner: a reply on another type", msgs: []capturedNlMsg{msg(21, 0, family("ethtool", 21))}, expected: "0 false"},
		{description: "boundary: nothing captured", expected: "0 false"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			id, ok := ethGenlID(tc.msgs)
			if got := fmt.Sprint(id, " ", ok); got != tc.expected {
				t.Errorf("got %s, want %s", got, tc.expected)
			}
		})
	}
}
