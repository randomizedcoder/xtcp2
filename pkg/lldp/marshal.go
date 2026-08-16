package lldp

import (
	"encoding/binary"
	"fmt"
)

// This file reimplements lldpd's custom pointer-graph deserializer
// (src/marshal.c, function marshal_unserialize_) in pure Go, plus just enough
// of the struct descriptors from src/lldpd-structs.h to walk from an interface
// list or an lldpd_hardware down to the neighbor chassis/port/mgmt data.
//
// Wire format recap (little-endian, LP64):
//
//   Every serialized object is a wrapper:
//       [orig: uint64][size: uint64][ object bytes ][ appended children... ]
//   - orig  = a per-message monotonic dummy id (>=1), used for back-references.
//   - size  = total length of THIS wrapper INCLUDING all appended children
//             (a self-describing subtree length; we use it to skip subtrees).
//   - object bytes = the fixed C struct, verbatim with padding (mi->size bytes);
//             for a string the object bytes are the (NUL-terminated) contents.
//   Children (pointees) are appended after the object, each 8-byte aligned
//   relative to the start of the parent wrapper, in marshal-descriptor order.
//   A pointer field in the object block holds a dummy id: 0 => NULL (no child);
//   an already-seen id => back-reference (no child appended); otherwise the next
//   appended wrapper is the pointee.

// Struct field offsets and sizes, taken from the exact C layouts at git tags
// 1.0.13 / 1.0.18 (amd64, LP64, all ENABLE_* on) and confirmed by compiling the
// structs. chassis/port/mgmt/interface are identical across both versions.
const (
	sizeInterfaceList = 16 // TAILQ_HEAD
	sizeInterface     = 24 // TAILQ_ENTRY(16) + char*(8)
	sizeMgmt          = 56
	sizeChassis       = 144
	sizePort          = 376

	// lldpd_interface: next.tqe_next @0, name @16
	offIfaceNext = 0
	offIfaceName = 16

	// lldpd_mgmt: m_entries.tqe_next @0, m_family @16, m_addr @20, m_addrsize @40
	offMgmtNext     = 0
	offMgmtFamily   = 16
	offMgmtAddr     = 20
	offMgmtAddrsize = 40

	// lldpd_chassis: c_id_subtype @21, c_id @24 (len @32), c_name @40,
	// c_descr @48, c_mgmt.tqh_first @64
	offChassisIDSubtype = 21
	offChassisID        = 24
	offChassisIDLen     = 32
	offChassisName      = 40
	offChassisDescr     = 48
	offChassisMgmt      = 64

	// lldpd_port: p_entries.tqe_next @0, p_chassis @16, p_id_subtype @64,
	// p_id @72 (len @80), p_descr @88
	offPortNext      = 0
	offPortChassis   = 16
	offPortIDSubtype = 64
	offPortID        = 72
	offPortIDLen     = 80
	offPortDescr     = 88
)

// maxDepth bounds recursion so a pathological payload (e.g. a very long forged
// tqe_next chain) cannot exhaust the stack.
const maxDepth = 4096

// mkind is the kind of a marshaled field we care about.
type mkind int

const (
	kPtr  mkind = iota // pointer to another struct (mi set)
	kStr               // pointer to a NUL-terminated string
	kFstr              // pointer to a fixed-length (binary) string; length in lenOff
)

// mfield describes one marshaled pointer/string field of a struct, in
// descriptor order. Only the fields we need are listed; trailing fields that we
// never read (LLDP-MED strings, DOT1/CUSTOM lists, ...) are omitted because we
// advance past whole subtrees using each wrapper's self-describing size.
type mfield struct {
	off    int    // offset of the pointer within the object block
	lenOff int    // for kFstr: offset of the companion int length
	kind   mkind  // kPtr / kStr / kFstr
	mi     *minfo // child descriptor (kPtr only)
}

// minfo is a struct descriptor: its object-block size and its marshaled fields.
type minfo struct {
	name   string
	size   int
	fields []mfield
}

var (
	miInterfaceList = &minfo{name: "lldpd_interface_list", size: sizeInterfaceList}
	miInterface     = &minfo{name: "lldpd_interface", size: sizeInterface}
	miMgmt          = &minfo{name: "lldpd_mgmt", size: sizeMgmt}
	miChassis       = &minfo{name: "lldpd_chassis", size: sizeChassis}
	miPort          = &minfo{name: "lldpd_port", size: sizePort}
)

func init() {
	// tqh_first -> first interface
	miInterfaceList.fields = []mfield{{off: 0, kind: kPtr, mi: miInterface}}
	// next.tqe_next -> next interface; name -> string
	miInterface.fields = []mfield{
		{off: offIfaceNext, kind: kPtr, mi: miInterface},
		{off: offIfaceName, kind: kStr},
	}
	// m_entries.tqe_next -> next mgmt
	miMgmt.fields = []mfield{{off: offMgmtNext, kind: kPtr, mi: miMgmt}}
	// c_id (fstr), c_name, c_descr, c_mgmt.tqh_first -> first mgmt
	miChassis.fields = []mfield{
		{off: offChassisID, lenOff: offChassisIDLen, kind: kFstr},
		{off: offChassisName, kind: kStr},
		{off: offChassisDescr, kind: kStr},
		{off: offChassisMgmt, kind: kPtr, mi: miMgmt},
	}
	// p_entries.tqe_next -> next port, p_chassis, p_id (fstr), p_descr
	miPort.fields = []mfield{
		{off: offPortNext, kind: kPtr, mi: miPort},
		{off: offPortChassis, kind: kPtr, mi: miChassis},
		{off: offPortID, lenOff: offPortIDLen, kind: kFstr},
		{off: offPortDescr, kind: kStr},
	}
}

// node is a decoded object. body is the fixed struct block (for reading scalar
// fields by offset); ptrs holds decoded pointees keyed by field offset; strs
// holds decoded string/fstring values keyed by field offset.
type node struct {
	mi   *minfo
	body []byte
	ptrs map[int]*node
	strs map[int][]byte
}

func le64(b []byte, off int) (uint64, bool) {
	if off < 0 || off+8 > len(b) {
		return 0, false
	}
	return binary.LittleEndian.Uint64(b[off : off+8]), true
}

func le32(b []byte, off int) (uint32, bool) {
	if off < 0 || off+4 > len(b) {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b[off : off+4]), true
}

func align8(n int) int {
	return (n + alignTo - 1) &^ (alignTo - 1)
}

// wrapperSize reads and validates the self-describing size of the wrapper that
// starts at buf[pos:]. The returned size spans the whole subtree.
func wrapperSize(buf []byte, pos int) (orig uint64, size int, err error) {
	if pos < 0 || pos+wrapperHeader > len(buf) {
		return 0, 0, ErrShortBuffer
	}
	orig = binary.LittleEndian.Uint64(buf[pos : pos+8])
	sz := binary.LittleEndian.Uint64(buf[pos+8 : pos+16])
	if sz < wrapperHeader {
		return 0, 0, fmt.Errorf("%w: wrapper size %d < header", ErrMalformed, sz)
	}
	if sz > hmsgMaxSize {
		return 0, 0, ErrTooLarge
	}
	if pos+int(sz) > len(buf) {
		return 0, 0, fmt.Errorf("%w: wrapper size %d runs past buffer", ErrShortBuffer, sz)
	}
	return orig, int(sz), nil
}

// decode deserializes the struct wrapper at buf[pos:] according to mi, sharing
// seen across the whole message for back-reference resolution. It returns the
// decoded node and the wrapper's total subtree length (its size field), so the
// caller can position the next sibling regardless of any trailing fields we did
// not model. depth bounds recursion.
func decode(buf []byte, pos int, mi *minfo, seen map[uint64]*node, depth int) (*node, int, error) {
	if depth > maxDepth {
		return nil, 0, fmt.Errorf("%w: recursion too deep", ErrMalformed)
	}
	orig, size, err := wrapperSize(buf, pos)
	if err != nil {
		return nil, 0, err
	}
	objStart := pos + wrapperHeader
	objEnd := objStart + mi.size
	if objEnd > pos+size || objEnd > len(buf) {
		return nil, 0, fmt.Errorf("%w: object block of %s past wrapper", ErrShortBuffer, mi.name)
	}
	n := &node{
		mi:   mi,
		body: buf[objStart:objEnd],
		ptrs: make(map[int]*node),
		strs: make(map[int][]byte),
	}
	// Register before walking fields: mirrors marshal_alloc being called for
	// the object before its substructures, so a child can back-reference it.
	seen[orig] = n

	// tl tracks the offset of the next appended child relative to the wrapper
	// start. Children begin right after the object block, 8-aligned.
	tl := wrapperHeader + mi.size
	for _, f := range mi.fields {
		ptr, ok := le64(n.body, f.off)
		if !ok {
			return nil, 0, fmt.Errorf("%w: field @%d of %s", ErrShortBuffer, f.off, mi.name)
		}
		if ptr == 0 {
			continue // NULL pointer: no child appended
		}
		if f.kind == kPtr {
			if existing, seenIt := seen[ptr]; seenIt {
				n.ptrs[f.off] = existing // back-reference
				continue
			}
		}
		// A child wrapper is appended here, 8-aligned from the wrapper start.
		tl = align8(tl)
		childPos := pos + tl
		switch f.kind {
		case kPtr:
			child, childLen, err := decode(buf, childPos, f.mi, seen, depth+1)
			if err != nil {
				return nil, 0, err
			}
			n.ptrs[f.off] = child
			tl += childLen
		case kStr:
			val, childLen, err := decodeString(buf, childPos, -1, seen)
			if err != nil {
				return nil, 0, err
			}
			n.strs[f.off] = val
			tl += childLen
		case kFstr:
			l32, ok := le32(n.body, f.lenOff)
			if !ok {
				return nil, 0, fmt.Errorf("%w: fstr len @%d of %s", ErrShortBuffer, f.lenOff, mi.name)
			}
			val, childLen, err := decodeString(buf, childPos, int(int32(l32)), seen)
			if err != nil {
				return nil, 0, err
			}
			n.strs[f.off] = val
			tl += childLen
		}
		if tl > size {
			return nil, 0, fmt.Errorf("%w: child of %s past wrapper", ErrShortBuffer, mi.name)
		}
	}
	return n, size, nil
}

// decodeString decodes a string wrapper. If flen < 0 it is a NUL-terminated
// ("null string") whose value is the bytes up to the first NUL. Otherwise it is
// a fixed string of exactly flen bytes (may contain arbitrary binary). Returns
// the value and the wrapper's total subtree length.
func decodeString(buf []byte, pos int, flen int, seen map[uint64]*node) ([]byte, int, error) {
	orig, size, err := wrapperSize(buf, pos)
	if err != nil {
		return nil, 0, err
	}
	obj := buf[pos+wrapperHeader : pos+size]
	var val []byte
	if flen < 0 {
		// NUL-terminated: value up to (not including) the first NUL.
		end := len(obj)
		for i, c := range obj {
			if c == 0 {
				end = i
				break
			}
		}
		val = append([]byte(nil), obj[:end]...)
	} else {
		if flen < 0 || flen > len(obj) {
			return nil, 0, fmt.Errorf("%w: fixed string len %d", ErrMalformed, flen)
		}
		val = append([]byte(nil), obj[:flen]...)
	}
	// Register the string so a later pointer with the same id resolves as a
	// back-reference (mirrors marshal_alloc for strings). Strings are leaves.
	seen[orig] = &node{strs: map[int][]byte{0: val}}
	return val, size, nil
}

// ParseInterfaces decodes a GET_INTERFACES reply payload (a marshaled
// lldpd_interface_list) into the list of interface names, in list order.
func ParseInterfaces(payload []byte) ([]string, error) {
	if len(payload) < wrapperHeader {
		return nil, ErrShortBuffer
	}
	seen := make(map[uint64]*node)
	root, _, err := decode(payload, 0, miInterfaceList, seen, 0)
	if err != nil {
		return nil, err
	}
	var names []string
	cur := root.ptrs[0] // tqh_first
	for i := 0; cur != nil && i <= maxDepth; i++ {
		names = append(names, string(cur.strs[offIfaceName]))
		cur = cur.ptrs[offIfaceNext]
	}
	return names, nil
}

// ParseHardware decodes a GET_INTERFACE reply payload (a marshaled
// lldpd_hardware) into its neighbors. It uses size-driven positional parsing so
// it tolerates the 1.0.13/1.0.18/1.0.22 hardware-size drift: after the hardware
// fixed block, the appended wrappers are (1) the h_lport substruct subtree
// (local port, skipped), then (2) the first h_rports element if present.
func ParseHardware(payload []byte, layout Layout) ([]Neighbor, error) {
	if len(payload) < wrapperHeader {
		return nil, ErrShortBuffer
	}
	_, topSize, err := wrapperSize(payload, 0)
	if err != nil {
		return nil, err
	}
	hwObj := layout.HardwareSize
	if hwObj <= 0 {
		return nil, fmt.Errorf("%w: bad layout %q", ErrMalformed, layout.Name)
	}
	if wrapperHeader+hwObj > topSize {
		return nil, fmt.Errorf("%w: hardware block past wrapper", ErrShortBuffer)
	}

	// (1) h_lport substruct wrapper: always present, first appended child.
	lportPos := align8(wrapperHeader + hwObj)
	_, lportSize, err := wrapperSize(payload, lportPos)
	if err != nil {
		return nil, err
	}
	if lportPos+lportSize > topSize {
		return nil, fmt.Errorf("%w: h_lport past wrapper", ErrShortBuffer)
	}

	// (2) h_rports first element: appended immediately after the h_lport
	// subtree, 8-aligned. If nothing remains within the top wrapper, there are
	// no neighbors.
	rportPos := align8(lportPos + lportSize)
	if rportPos+wrapperHeader > topSize {
		return nil, nil // no neighbors
	}

	seen := make(map[uint64]*node)
	first, _, err := decode(payload, rportPos, miPort, seen, 0)
	if err != nil {
		return nil, err
	}

	var neighbors []Neighbor
	cur := first
	for i := 0; cur != nil && i <= maxDepth; i++ {
		neighbors = append(neighbors, neighborFromPort(cur))
		cur = cur.ptrs[offPortNext]
	}
	return neighbors, nil
}

// parseHardwareTolerant tries each layout and picks the best decode. Using a
// wrong layout usually fails a bounds/size check (the h_lport size field is read
// from the wrong offset) — but a layout that is merely too LARGE for the payload
// can land past the real h_rports and report zero neighbors without erroring.
// So we can't just take the first error-free result: with the newest-first
// auto-detect order, a 1.0.22 layout would spuriously "succeed" empty on a
// 1.0.13 payload that actually has a neighbor. Instead we prefer the first
// layout that yields NEIGHBORS, falling back to the first error-free (possibly
// empty) result only when none produce any — that still auto-detects across
// 1.0.13 / 1.0.18 / 1.0.22 while honoring a genuinely neighbor-less interface.
func parseHardwareTolerant(payload []byte, layouts []Layout) ([]Neighbor, error) {
	var (
		lastErr    error
		emptyOK    bool
		emptyValue []Neighbor
	)
	for _, l := range layouts {
		n, err := ParseHardware(payload, l)
		if err != nil {
			lastErr = err
			continue
		}
		if len(n) > 0 {
			return n, nil
		}
		if !emptyOK {
			emptyOK = true
			emptyValue = n
		}
	}
	if emptyOK {
		return emptyValue, nil
	}
	return nil, lastErr
}

// neighborFromPort extracts the fields we expose from a decoded lldpd_port and
// its (shared) chassis.
func neighborFromPort(port *node) Neighbor {
	var n Neighbor

	var portSubtype byte
	if len(port.body) > offPortIDSubtype {
		portSubtype = port.body[offPortIDSubtype]
	}
	n.PortID = formatPortID(portSubtype, port.strs[offPortID])
	n.PortDescr = string(port.strs[offPortDescr])

	chassis := port.ptrs[offPortChassis]
	if chassis == nil {
		return n
	}
	var chSubtype byte
	if len(chassis.body) > offChassisIDSubtype {
		chSubtype = chassis.body[offChassisIDSubtype]
	}
	n.ChassisID = formatChassisID(chSubtype, chassis.strs[offChassisID])
	n.ChassisName = string(chassis.strs[offChassisName])
	n.MgmtIP = firstMgmtIP(chassis.ptrs[offChassisMgmt])
	return n
}

// firstMgmtIP walks the chassis management-address list and returns the first
// address, preferring the first IPv4.
func firstMgmtIP(first *node) string {
	var firstAny string
	cur := first
	for i := 0; cur != nil && i <= maxDepth; i++ {
		family, okF := le32(cur.body, offMgmtFamily)
		if okF && cur.mi == miMgmt {
			var octets []byte
			if offMgmtAddr+16 <= len(cur.body) {
				octets = cur.body[offMgmtAddr : offMgmtAddr+16]
			}
			ip := formatMgmt(int32(family), octets)
			if ip != "" {
				if int32(family) == afIPv4 {
					return ip
				}
				if firstAny == "" {
					firstAny = ip
				}
			}
		}
		cur = cur.ptrs[offMgmtNext]
	}
	return firstAny
}
