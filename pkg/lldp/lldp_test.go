package lldp

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Reference serializer (test-only).
//
// This mirrors lldpd's src/marshal.c marshal_serialize_ so we can hand-build
// realistic on-the-wire fixtures and assert that the production decoder round-
// trips them. The decoder in marshal.go is the actual product; this encoder
// exists only to produce bytes with the exact orig/size/alignment/back-
// reference behavior lldpd emits. Field offsets come from the same C-verified
// constants the decoder uses.
// ---------------------------------------------------------------------------

type sstr struct {
	data  []byte
	fixed bool // true => fixed string (no NUL); false => NUL-terminated
}

type sobj struct {
	mi    *minfo
	body  []byte
	ptr   map[int]*sobj
	str   map[int]*sstr
	extra int // extra aligned trailing bytes, to simulate unmodeled fields
}

type refState struct {
	ids  map[any]uint64
	next uint64
}

func newRefState() *refState { return &refState{ids: map[any]uint64{}, next: 1} }

func (r *refState) idFor(key any) (uint64, bool) {
	if id, ok := r.ids[key]; ok {
		return id, true
	}
	id := r.next
	r.next++
	r.ids[key] = id
	return id, false
}

func putU64(b []byte, off int, v uint64) { binary.LittleEndian.PutUint64(b[off:off+8], v) }
func putU32(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:off+4], v) }

func padAlign(buf []byte) []byte {
	pad := align8(len(buf)) - len(buf)
	if pad > 0 {
		buf = append(buf, make([]byte, pad)...)
	}
	return buf
}

func serializeStr(s *sstr, id uint64) []byte {
	obj := s.data
	if !s.fixed {
		obj = append(append([]byte(nil), s.data...), 0)
	}
	buf := make([]byte, wrapperHeader+len(obj))
	putU64(buf, 0, id)
	putU64(buf, 8, uint64(len(buf)))
	copy(buf[wrapperHeader:], obj)
	return buf
}

func serialize(o *sobj, r *refState) (buf []byte, id uint64, appended bool) {
	id, already := r.idFor(o)
	if already {
		return nil, id, false // back-reference: no bytes
	}
	buf = make([]byte, wrapperHeader+len(o.body))
	copy(buf[wrapperHeader:], o.body)
	for _, f := range o.mi.fields {
		switch f.kind {
		case kPtr:
			t := o.ptr[f.off]
			if t == nil {
				putU64(buf, wrapperHeader+f.off, 0)
				continue
			}
			cb, cid, app := serialize(t, r)
			putU64(buf, wrapperHeader+f.off, cid)
			if !app {
				continue
			}
			buf = padAlign(buf)
			buf = append(buf, cb...)
		case kStr, kFstr:
			s := o.str[f.off]
			if s == nil {
				putU64(buf, wrapperHeader+f.off, 0)
				continue
			}
			sid, already := r.idFor(s)
			putU64(buf, wrapperHeader+f.off, sid)
			if f.kind == kFstr {
				putU32(buf, wrapperHeader+f.lenOff, uint32(len(s.data)))
			}
			if already {
				continue
			}
			buf = padAlign(buf)
			buf = append(buf, serializeStr(s, sid)...)
		}
	}
	if o.extra > 0 {
		buf = padAlign(buf)
		buf = append(buf, make([]byte, o.extra)...)
	}
	putU64(buf, 0, id)
	putU64(buf, 8, uint64(len(buf)))
	return buf, id, true
}

// --- builder graph -> sobj converters ---

type bMgmt struct {
	family   int32
	addr     []byte // up to 16 octets, network order
	addrsize int
	next     *bMgmt
}

type bChassis struct {
	idSubtype byte
	id        []byte
	name      string
	descr     string
	mgmt      *bMgmt
	extra     int
}

type bPort struct {
	idSubtype byte
	id        []byte
	descr     string
	chassis   *bChassis
	next      *bPort
	extra     int
}

type bIface struct {
	name string
	next *bIface
}

func mgmtObj(m *bMgmt) *sobj {
	if m == nil {
		return nil
	}
	body := make([]byte, sizeMgmt)
	putU32(body, offMgmtFamily, uint32(m.family))
	copy(body[offMgmtAddr:offMgmtAddr+16], m.addr)
	putU64(body, offMgmtAddrsize, uint64(m.addrsize))
	o := &sobj{mi: miMgmt, body: body, ptr: map[int]*sobj{}, str: map[int]*sstr{}}
	o.ptr[offMgmtNext] = mgmtObj(m.next)
	return o
}

func chassisObj(c *bChassis) *sobj {
	if c == nil {
		return nil
	}
	body := make([]byte, sizeChassis)
	body[offChassisIDSubtype] = c.idSubtype
	o := &sobj{mi: miChassis, body: body, ptr: map[int]*sobj{}, str: map[int]*sstr{}, extra: c.extra}
	if c.id != nil {
		o.str[offChassisID] = &sstr{data: c.id, fixed: true}
	}
	if c.name != "" {
		o.str[offChassisName] = &sstr{data: []byte(c.name)}
	}
	if c.descr != "" {
		o.str[offChassisDescr] = &sstr{data: []byte(c.descr)}
	}
	o.ptr[offChassisMgmt] = mgmtObj(c.mgmt)
	return o
}

// portObj converts a port chain. The shared chassis map ensures ports pointing
// to the same *bChassis share one *sobj (so serialize emits a back-reference).
func portObj(p *bPort, chCache map[*bChassis]*sobj) *sobj {
	if p == nil {
		return nil
	}
	body := make([]byte, sizePort)
	body[offPortIDSubtype] = p.idSubtype
	o := &sobj{mi: miPort, body: body, ptr: map[int]*sobj{}, str: map[int]*sstr{}, extra: p.extra}
	o.ptr[offPortNext] = portObj(p.next, chCache)
	if p.chassis != nil {
		ch, ok := chCache[p.chassis]
		if !ok {
			ch = chassisObj(p.chassis)
			chCache[p.chassis] = ch
		}
		o.ptr[offPortChassis] = ch
	}
	if p.id != nil {
		o.str[offPortID] = &sstr{data: p.id, fixed: true}
	}
	if p.descr != "" {
		o.str[offPortDescr] = &sstr{data: []byte(p.descr)}
	}
	return o
}

func ifaceObj(i *bIface) *sobj {
	if i == nil {
		return nil
	}
	body := make([]byte, sizeInterface)
	o := &sobj{mi: miInterface, body: body, ptr: map[int]*sobj{}, str: map[int]*sstr{}}
	o.ptr[offIfaceNext] = ifaceObj(i.next)
	if i.name != "" {
		o.str[offIfaceName] = &sstr{data: []byte(i.name)}
	}
	return o
}

// buildInterfaceList serializes a full GET_INTERFACES reply payload.
func buildInterfaceList(first *bIface) []byte {
	body := make([]byte, sizeInterfaceList)
	root := &sobj{mi: miInterfaceList, body: body, ptr: map[int]*sobj{}, str: map[int]*sstr{}}
	root.ptr[0] = ifaceObj(first)
	buf, _, _ := serialize(root, newRefState())
	return buf
}

// buildHardware assembles a GET_INTERFACE reply payload: a hardware wrapper
// whose object block is zeroed (the decoder skips it positionally), followed by
// the h_lport substruct wrapper and, if present, the h_rports port chain.
func buildHardware(layout Layout, firstPort *bPort, lportExtra int) []byte {
	hwObj := layout.HardwareSize
	top := make([]byte, wrapperHeader+hwObj) // orig+size + zeroed hardware block

	// h_lport substruct wrapper: no object bytes (they live in the parent),
	// optionally with extra trailing bytes to model a non-trivial local port.
	top = padAlign(top)
	lport := make([]byte, wrapperHeader+lportExtra)
	putU64(lport, 0, 2)
	putU64(lport, 8, uint64(len(lport)))
	top = append(top, lport...)

	// h_rports first element (if any).
	if firstPort != nil {
		portBytes, _, _ := serialize(portObj(firstPort, map[*bChassis]*sobj{}), newRefState())
		top = padAlign(top)
		top = append(top, portBytes...)
	}

	putU64(top, 0, 1)
	putU64(top, 8, uint64(len(top)))
	return top
}

// ---------------------------------------------------------------------------
// Golden fixtures on disk (testdata/), regenerated from the builders above so
// they stay in sync while still being parsed from disk like real captures.
// ---------------------------------------------------------------------------

const tdDir = "./testdata"

func aristaChassis() *bChassis {
	return &bChassis{
		idSubtype: chassisSubtypeLLADDR,
		id:        []byte{0x00, 0x1c, 0x73, 0x11, 0x22, 0x33},
		name:      "spine1",
		descr:     "Arista DCS-7050",
		mgmt:      &bMgmt{family: afIPv4, addr: []byte{10, 0, 0, 1}, addrsize: 4},
	}
}

func fixtures() map[string][]byte {
	arista := aristaChassis()
	twoNeighborChassis := &bChassis{
		idSubtype: chassisSubtypeLLADDR,
		id:        []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		name:      "spine2",
		descr:     "shared",
		mgmt:      &bMgmt{family: afIPv4, addr: []byte{192, 168, 1, 254}, addrsize: 4},
		extra:     32, // simulate unmodeled trailing LLDP-MED strings
	}
	return map[string][]byte{
		"interfaces_one.bin":   buildInterfaceList(&bIface{name: "eth0"}),
		"interfaces_two.bin":   buildInterfaceList(&bIface{name: "eth0", next: &bIface{name: "eth1"}}),
		"interfaces_empty.bin": buildInterfaceList(nil),
		"hw_one_neighbor_18.bin": buildHardware(Layout1_0_18, &bPort{
			idSubtype: portSubtypeIfname,
			id:        []byte("Ethernet1/1"),
			descr:     "uplink to leaf",
			chassis:   arista,
		}, 0),
		"hw_one_neighbor_13.bin": buildHardware(Layout1_0_13, &bPort{
			idSubtype: portSubtypeIfname,
			id:        []byte("Ethernet1/1"),
			descr:     "uplink to leaf",
			chassis:   arista,
		}, 0),
		"hw_one_neighbor_22.bin": buildHardware(Layout1_0_22, &bPort{
			idSubtype: portSubtypeIfname,
			id:        []byte("Ethernet1/1"),
			descr:     "uplink to leaf",
			chassis:   arista,
		}, 0),
		"hw_two_neighbors_18.bin": buildHardware(Layout1_0_18, &bPort{
			idSubtype: portSubtypeLLADDR,
			id:        []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
			descr:     "port-a",
			chassis:   twoNeighborChassis,
			next: &bPort{
				idSubtype: portSubtypeLLADDR,
				id:        []byte{0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc},
				descr:     "port-b",
				chassis:   twoNeighborChassis, // same chassis -> back-reference
			},
		}, 24),
		"hw_empty_18.bin": buildHardware(Layout1_0_18, nil, 0),
	}
}

func writeFixtures(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(tdDir, 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	for name, data := range fixtures() {
		p := filepath.Join(tdDir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(tdDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestMain(m *testing.M) {
	// Regenerate golden fixtures before the suite runs.
	_ = os.MkdirAll(tdDir, 0o755)
	for name, data := range fixtures() {
		_ = os.WriteFile(filepath.Join(tdDir, name), data, 0o600)
	}
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestParseInterfaces(t *testing.T) {
	writeFixtures(t)
	tests := []struct {
		name    string
		payload []byte
		want    []string
		wantErr bool
	}{
		{"positive one interface", readFixture(t, "interfaces_one.bin"), []string{"eth0"}, false},
		{"positive two interfaces", readFixture(t, "interfaces_two.bin"), []string{"eth0", "eth1"}, false},
		{"corner empty list", readFixture(t, "interfaces_empty.bin"), nil, false},
		{"negative nil payload", nil, nil, true},
		{"negative short payload", []byte{1, 2, 3, 4}, nil, true},
		{"boundary size past buffer", func() []byte {
			b := make([]byte, wrapperHeader)
			putU64(b, 0, 1)
			putU64(b, 8, 1<<18) // claims a huge subtree, buffer is only 16 bytes
			return b
		}(), nil, true},
		{"boundary oversized len", func() []byte {
			b := make([]byte, wrapperHeader)
			putU64(b, 0, 1)
			putU64(b, 8, hmsgMaxSize+1)
			return b
		}(), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseInterfaces(tt.payload)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseInterfaces() = %v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseInterfaces() error = %v", err)
			}
			if !equalStrings(got, tt.want) {
				t.Fatalf("ParseInterfaces() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseHardware(t *testing.T) {
	writeFixtures(t)
	tests := []struct {
		name    string
		payload []byte
		layout  Layout
		wantN   int
		check   func(t *testing.T, ns []Neighbor)
		wantErr bool
	}{
		{
			name:    "positive one neighbor 1.0.18",
			payload: readFixture(t, "hw_one_neighbor_18.bin"),
			layout:  Layout1_0_18,
			wantN:   1,
			check: func(t *testing.T, ns []Neighbor) {
				n := ns[0]
				if n.ChassisName != "spine1" {
					t.Errorf("ChassisName = %q, want spine1", n.ChassisName)
				}
				if n.ChassisID != "00:1c:73:11:22:33" {
					t.Errorf("ChassisID = %q", n.ChassisID)
				}
				if n.MgmtIP != "10.0.0.1" {
					t.Errorf("MgmtIP = %q", n.MgmtIP)
				}
				if n.PortID != "Ethernet1/1" {
					t.Errorf("PortID = %q", n.PortID)
				}
				if n.PortDescr != "uplink to leaf" {
					t.Errorf("PortDescr = %q", n.PortDescr)
				}
			},
		},
		{
			name:    "positive one neighbor 1.0.13",
			payload: readFixture(t, "hw_one_neighbor_13.bin"),
			layout:  Layout1_0_13,
			wantN:   1,
			check: func(t *testing.T, ns []Neighbor) {
				if ns[0].ChassisName != "spine1" || ns[0].PortID != "Ethernet1/1" {
					t.Errorf("neighbor = %+v", ns[0])
				}
			},
		},
		{
			name:    "positive one neighbor 1.0.22",
			payload: readFixture(t, "hw_one_neighbor_22.bin"),
			layout:  Layout1_0_22,
			wantN:   1,
			check: func(t *testing.T, ns []Neighbor) {
				if ns[0].ChassisName != "spine1" || ns[0].PortID != "Ethernet1/1" {
					t.Errorf("neighbor = %+v", ns[0])
				}
				if ns[0].MgmtIP != "10.0.0.1" {
					t.Errorf("MgmtIP = %q", ns[0].MgmtIP)
				}
			},
		},
		{
			name:    "positive two neighbors shared chassis (dedup)",
			payload: readFixture(t, "hw_two_neighbors_18.bin"),
			layout:  Layout1_0_18,
			wantN:   2,
			check: func(t *testing.T, ns []Neighbor) {
				if ns[0].ChassisName != "spine2" || ns[1].ChassisName != "spine2" {
					t.Errorf("both neighbors should share chassis name spine2: %+v", ns)
				}
				if ns[0].MgmtIP != "192.168.1.254" || ns[1].MgmtIP != ns[0].MgmtIP {
					t.Errorf("both should share mgmt IP: %+v", ns)
				}
				if ns[0].PortID != "11:22:33:44:55:66" || ns[1].PortID != "77:88:99:aa:bb:cc" {
					t.Errorf("port ids = %q, %q", ns[0].PortID, ns[1].PortID)
				}
				if ns[0].PortDescr != "port-a" || ns[1].PortDescr != "port-b" {
					t.Errorf("port descrs = %q, %q", ns[0].PortDescr, ns[1].PortDescr)
				}
			},
		},
		{
			name:    "corner empty neighbor list",
			payload: readFixture(t, "hw_empty_18.bin"),
			layout:  Layout1_0_18,
			wantN:   0,
		},
		{
			name:    "negative wrong layout errors",
			payload: readFixture(t, "hw_one_neighbor_18.bin"),
			layout:  Layout1_0_13,
			wantErr: true,
		},
		{
			name:    "negative nil payload",
			payload: nil,
			layout:  Layout1_0_18,
			wantErr: true,
		},
		{
			name:    "boundary truncated payload",
			payload: readFixture(t, "hw_one_neighbor_18.bin")[:40],
			layout:  Layout1_0_18,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHardware(tt.payload, tt.layout)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseHardware() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHardware() error = %v", err)
			}
			if len(got) != tt.wantN {
				t.Fatalf("ParseHardware() returned %d neighbors, want %d (%+v)", len(got), tt.wantN, got)
			}
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

// TestParseHardwareTolerant verifies the empty-hint auto-detection: a payload
// built for one version is decoded when both layouts are offered.
func TestParseHardwareTolerant(t *testing.T) {
	writeFixtures(t)
	tests := []struct {
		name    string
		payload []byte
	}{
		{"detect 1.0.22", readFixture(t, "hw_one_neighbor_22.bin")},
		{"detect 1.0.18", readFixture(t, "hw_one_neighbor_18.bin")},
		{"detect 1.0.13", readFixture(t, "hw_one_neighbor_13.bin")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHardwareTolerant(tt.payload, layoutsFor(""))
			if err != nil {
				t.Fatalf("parseHardwareTolerant() error = %v", err)
			}
			if len(got) != 1 || got[0].ChassisName != "spine1" {
				t.Fatalf("parseHardwareTolerant() = %+v", got)
			}
		})
	}
}

func TestFormatChassisID(t *testing.T) {
	tests := []struct {
		name    string
		subtype byte
		in      []byte
		want    string
	}{
		{"mac subtype", chassisSubtypeLLADDR, []byte{0x00, 0x1c, 0x73, 0xde, 0xad, 0xbe}, "00:1c:73:de:ad:be"},
		{"ascii non-mac", 7, []byte("switch-a"), "switch-a"},
		{"binary non-mac -> hex", 1, []byte{0x01, 0x02, 0xfe}, "0102fe"},
		{"corner empty", chassisSubtypeLLADDR, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatChassisID(tt.subtype, tt.in); got != tt.want {
				t.Fatalf("formatChassisID(%d,%v) = %q, want %q", tt.subtype, tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatPortID(t *testing.T) {
	tests := []struct {
		name    string
		subtype byte
		in      []byte
		want    string
	}{
		{"mac subtype 3", portSubtypeLLADDR, []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}, "de:ad:be:ef:00:01"},
		{"ifname subtype 5", portSubtypeIfname, []byte("Gi0/1"), "Gi0/1"},
		{"local subtype 7", portSubtypeLocal, []byte("1234"), "1234"},
		{"other printable", 2, []byte("port2"), "port2"},
		{"other binary -> hex", 2, []byte{0x00, 0xff}, "00ff"},
		{"corner empty", portSubtypeLLADDR, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatPortID(tt.subtype, tt.in); got != tt.want {
				t.Fatalf("formatPortID(%d,%v) = %q, want %q", tt.subtype, tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatMgmt(t *testing.T) {
	tests := []struct {
		name   string
		family int32
		octets []byte
		want   string
	}{
		{"ipv4", afIPv4, pad16([]byte{10, 1, 2, 3}), "10.1.2.3"},
		{"ipv6", afIPv6, []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, "2001:db8::1"},
		{"corner unspec", 0, pad16([]byte{1, 2, 3, 4}), ""},
		{"boundary short ipv4", afIPv4, []byte{1, 2}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatMgmt(tt.family, tt.octets); got != tt.want {
				t.Fatalf("formatMgmt(%d,%v) = %q, want %q", tt.family, tt.octets, got, tt.want)
			}
		})
	}
}

func TestLayoutPresets(t *testing.T) {
	if Layout1_0_13.HardwareSize != 624 {
		t.Errorf("Layout1_0_13.HardwareSize = %d, want 624", Layout1_0_13.HardwareSize)
	}
	if Layout1_0_18.HardwareSize != 632 {
		t.Errorf("Layout1_0_18.HardwareSize = %d, want 632", Layout1_0_18.HardwareSize)
	}
	if Layout1_0_22.HardwareSize != 640 {
		t.Errorf("Layout1_0_22.HardwareSize = %d, want 640", Layout1_0_22.HardwareSize)
	}
	if Layout1_0_18.HardwareSize-Layout1_0_13.HardwareSize != 8 {
		t.Errorf("expected 8-byte hardware drift between 1.0.13 and 1.0.18")
	}
	// 1.0.21 added char *h_ifalias, a second 8-byte drift up to 1.0.22.
	if Layout1_0_22.HardwareSize-Layout1_0_18.HardwareSize != 8 {
		t.Errorf("expected 8-byte hardware drift between 1.0.18 and 1.0.22")
	}
}

// TestLayoutsFor checks the version-hint → preset mapping, incl. the aliased
// hints (1.0.19/1.0.20 share the 1.0.18 size; 1.0.21 shares 1.0.22).
func TestLayoutsFor(t *testing.T) {
	tests := []struct {
		hint string
		want []Layout
	}{
		{"1.0.13", []Layout{Layout1_0_13}},
		{"1.0.18", []Layout{Layout1_0_18}},
		{"1.0.19", []Layout{Layout1_0_18}},
		{"1.0.20", []Layout{Layout1_0_18}},
		{"1.0.21", []Layout{Layout1_0_22}},
		{"1.0.22", []Layout{Layout1_0_22}},
		{"", []Layout{Layout1_0_22, Layout1_0_18, Layout1_0_13}},
	}
	for _, tt := range tests {
		t.Run(tt.hint, func(t *testing.T) {
			got := layoutsFor(tt.hint)
			if len(got) != len(tt.want) {
				t.Fatalf("layoutsFor(%q) = %v, want %v", tt.hint, got, tt.want)
			}
			for i := range got {
				if got[i].HardwareSize != tt.want[i].HardwareSize {
					t.Fatalf("layoutsFor(%q)[%d] = %+v, want %+v", tt.hint, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMarshalString(t *testing.T) {
	got := marshalString("eth0")
	// [orig=1][size=16+4+1=21]["eth0"\0]
	if len(got) != 21 {
		t.Fatalf("marshalString len = %d, want 21", len(got))
	}
	if binary.LittleEndian.Uint64(got[0:8]) != 1 {
		t.Errorf("orig = %d, want 1", binary.LittleEndian.Uint64(got[0:8]))
	}
	if binary.LittleEndian.Uint64(got[8:16]) != 21 {
		t.Errorf("size = %d, want 21", binary.LittleEndian.Uint64(got[8:16]))
	}
	if string(got[16:20]) != "eth0" || got[20] != 0 {
		t.Errorf("payload = %q", got[16:])
	}
}

// --- helpers ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pad16(b []byte) []byte {
	out := make([]byte, 16)
	copy(out, b)
	return out
}
