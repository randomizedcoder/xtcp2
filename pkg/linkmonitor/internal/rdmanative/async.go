package rdmanative

import (
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

// ErrAsyncEvent is a uverbs async event record that is not
// sizeof(struct ib_uverbs_async_event_desc) bytes.
var ErrAsyncEvent = errors.New("rdmanative: async event record has the wrong length")

// DecodeAsyncEvent decodes one record read from a uverbs async event fd into
// the Event the cgo source reports for it (monitor_kind and
// monitor_copy_ack, rdmaevents/event_copy.h): PORT_ACTIVE and PORT_ERR are
// Refresh; LID, PKEY, SM, CLIENT_REREGISTER and GID changes are Topology;
// DEVICE_FATAL is Fatal; anything else is Unknown. A port is reported only
// for Refresh and Topology, from element as libibverbs narrows it to an int
// port_num. The R1-T05 differential test uses it for the expected events,
// and R3 reuses it.
func DecodeAsyncEvent(device string, rec []byte) (rdmaevents.Event, error) {
	if len(rec) != asyncEventDescLen {
		return rdmaevents.Event{}, ErrAsyncEvent
	}
	_ = rec[asyncEventDescLen-1]
	element := binary.LittleEndian.Uint64(rec[asyncEventDescOffElement:])
	e := rdmaevents.Event{Device: device, Kind: asyncKind(binary.LittleEndian.Uint32(rec[asyncEventDescOffType:]))}
	if port := int32(uint32(element)); port > 0 && (e.Kind == rdmaevents.Refresh || e.Kind == rdmaevents.Topology) {
		e.Port = uint32(port)
	}
	return e, nil
}

func asyncKind(event uint32) rdmaevents.Kind {
	switch event {
	case eventPortActive, eventPortErr:
		return rdmaevents.Refresh
	case eventLIDChange, eventPKeyChange, eventSMChange, eventClientReregister, eventGIDChange:
		return rdmaevents.Topology
	case eventDeviceFatal:
		return rdmaevents.Fatal
	}
	return rdmaevents.Unknown
}
