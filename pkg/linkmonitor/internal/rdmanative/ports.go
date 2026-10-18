package rdmanative

import (
	"errors"
	"math"
	"slices"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

// MaxPorts bounds the events one rtnetlink message can produce: the RDMA
// ports mapped to a single ifindex. It is the verbs source's batch bound.
const MaxPorts = rdmaevents.Batch

// ErrPortMap is a port map over its bounds: more than MaxPorts ports behind
// one ifindex, or more than rdmaevents.Limit entries in all.
var ErrPortMap = errors.New("rdmanative: port map exceeds its bounds")

// Binding is one RDMA port and the netdev ifindex it is bound to, as the
// monitor's RDMA inventory resolved it. DevIndex is the device's NLDEV index,
// which UNREGISTER notifications name it by, when HasDevIndex: the kernel
// allocates it from 0, so the first device registered has index 0.
type Binding struct {
	Device      string
	Port        uint32
	Netdev      uint32
	DevIndex    uint32
	HasDevIndex bool
}

// Link is the part of one netdev the port map needs from the link dump:
// IFLA_LINK (Lower, for VLAN and other uppers) and IFLA_MASTER (Master, for
// bonds). Zero is none.
type Link struct {
	Index, Lower, Master uint32
}

// refKind says which messages on an ifindex reach a port.
type refKind uint8

const (
	// refLink: the ifindex's running and carrier state is the port's.
	refLink refKind = 1 << iota
	// refAddr: addresses on the ifindex feed the port's RoCE GID table.
	refAddr
)

type portRef struct {
	ifindex uint32
	port    uint32
	device  uint32 // index into PortMap.names
	kind    refKind
}

// PortMap maps an ifindex to the RDMA ports behind it. It is immutable once
// built, sorted by ifindex so a lookup is a binary search, and its device
// names are shared with every Event it produces, so translating allocates
// nothing.
type PortMap struct {
	refs    []portRef
	names   []string
	indices []uint32 // NLDEV index + 1 per name; zero when unknown
}

// NewPortMap builds the map from the inventory's bindings and the link dump.
// A bound netdev maps link state and addresses to its ports. A bond master
// (the bound netdev's Master) does too, as the driver reports a LAG port
// against the bond. An upper (a link whose Lower is a bound netdev or such a
// master) maps addresses only, because its addresses feed the lower's GIDs
// while its state does not change the port's. Bindings without a netdev or a
// port are skipped.
func NewPortMap(bindings []Binding, links []Link) (*PortMap, error) {
	m := &PortMap{}
	for _, b := range bindings {
		if b.Device != "" && !slices.Contains(m.names, b.Device) {
			m.names = append(m.names, b.Device)
		}
	}
	slices.Sort(m.names)
	m.indices = make([]uint32, len(m.names))
	for _, b := range bindings {
		if dev, ok := slices.BinarySearch(m.names, b.Device); ok && b.HasDevIndex && b.DevIndex != math.MaxUint32 {
			m.indices[dev] = b.DevIndex + 1
		}
	}
	byIndex := make(map[uint32][]portRef)
	for _, b := range bindings {
		if b.Device == "" || b.Port == 0 || b.Netdev == 0 {
			continue
		}
		dev, _ := slices.BinarySearch(m.names, b.Device)
		byIndex[b.Netdev] = append(byIndex[b.Netdev], portRef{port: b.Port, device: uint32(dev)})
	}
	add := func(ifindex uint32, kind refKind, from []portRef) {
		for _, r := range from {
			m.refs = append(m.refs, portRef{ifindex: ifindex, port: r.port, device: r.device, kind: kind})
		}
	}
	linked := make(map[uint32][]portRef, len(byIndex))
	for ifindex, refs := range byIndex {
		add(ifindex, refLink|refAddr, refs)
		linked[ifindex] = append(linked[ifindex], refs...)
	}
	for _, l := range links {
		if refs := byIndex[l.Index]; l.Master != 0 && len(refs) > 0 {
			add(l.Master, refLink|refAddr, refs)
			linked[l.Master] = append(linked[l.Master], refs...)
		}
	}
	for _, l := range links {
		if refs := linked[l.Lower]; l.Lower != 0 && l.Lower != l.Index && len(refs) > 0 {
			add(l.Index, refAddr, refs)
		}
	}
	return m, m.normalize()
}

// normalize sorts the refs, merges duplicates and checks the bounds.
func (m *PortMap) normalize() error {
	slices.SortFunc(m.refs, func(a, b portRef) int {
		switch {
		case a.ifindex != b.ifindex:
			return cmpU32(a.ifindex, b.ifindex)
		case a.device != b.device:
			return cmpU32(a.device, b.device)
		}
		return cmpU32(a.port, b.port)
	})
	out := m.refs[:0]
	for _, r := range m.refs {
		if n := len(out); n > 0 && out[n-1].ifindex == r.ifindex && out[n-1].device == r.device && out[n-1].port == r.port {
			out[n-1].kind |= r.kind
			continue
		}
		out = append(out, r)
	}
	m.refs = slices.Clip(out)
	if len(m.refs) > rdmaevents.Limit {
		return ErrPortMap
	}
	for i := 0; i < len(m.refs); {
		lo, hi := m.find(m.refs[i].ifindex)
		if hi-lo > MaxPorts {
			return ErrPortMap
		}
		i = hi
	}
	return nil
}

func cmpU32(a, b uint32) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// find returns the refs for ifindex as the half-open range [lo, hi).
func (m *PortMap) find(ifindex uint32) (lo, hi int) {
	lo, hi = 0, len(m.refs)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if m.refs[mid].ifindex < ifindex {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	for hi = lo; hi < len(m.refs) && m.refs[hi].ifindex == ifindex; hi++ {
	}
	return lo, hi
}

// Devices are the device names the map covers, sorted. The slice is shared;
// callers must not modify it.
func (m *PortMap) Devices() []string { return m.names }

// Device returns the name of the device with NLDEV index devIndex.
func (m *PortMap) Device(devIndex uint32) (string, bool) {
	if devIndex == math.MaxUint32 {
		return "", false
	}
	if i := slices.Index(m.indices, devIndex+1); i >= 0 {
		return m.names[i], true
	}
	return "", false
}

// Equal reports whether m and o map the same ifindexes to the same ports of
// the same devices.
func (m *PortMap) Equal(o *PortMap) bool {
	if m == nil || o == nil {
		return m == o
	}
	return slices.Equal(m.refs, o.refs) && slices.Equal(m.names, o.names) && slices.Equal(m.indices, o.indices)
}

// String renders the map for test failures: "ifindex:device/port[la]".
func (m *PortMap) String() string {
	var b strings.Builder
	for i, r := range m.refs {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(itoa(r.ifindex))
		b.WriteByte(':')
		b.WriteString(m.names[r.device])
		b.WriteByte('/')
		b.WriteString(itoa(r.port))
		if r.kind&refLink != 0 {
			b.WriteByte('l')
		}
		if r.kind&refAddr != 0 {
			b.WriteByte('a')
		}
	}
	return b.String()
}

func itoa(v uint32) string {
	var buf [10]byte
	i := len(buf)
	for {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			return string(buf[i:])
		}
	}
}
