package rdmacapture

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"strings"
)

// Kind is the type of an identifier the capture tool redacts.
type Kind uint8

const (
	KindMAC Kind = iota + 1
	KindGUID
	KindIPv4
	KindIPv6
)

var kindNames = map[Kind]string{
	KindMAC:  "mac",
	KindGUID: "guid",
	KindIPv4: "ipv4",
	KindIPv6: "ipv6",
}

func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

// Value is one real identifier, in network byte order: 6 bytes for a MAC, 8
// for a GUID, 4 for IPv4 and 16 for IPv6 or a GID.
type Value struct {
	Kind  Kind
	Bytes []byte
}

// ErrValue reports a values-file line that does not parse.
var ErrValue = errors.New("rdmacapture: bad value")

// ParseValue parses one identifier. kind is mac, guid, ipv4, ipv6 or gid (an
// alias for ipv6). A GUID is the sysfs form (9803:9b03:0012:3456) or 0x and 16
// hex digits; a GID or IPv6 address is any form net/netip accepts, including
// sysfs's uncompressed one.
func ParseValue(kind, text string) (Value, error) {
	switch kind {
	case "mac":
		hw, err := net.ParseMAC(text)
		if err != nil || len(hw) != 6 {
			return Value{}, fmt.Errorf("%w: mac %q", ErrValue, text)
		}
		return Value{Kind: KindMAC, Bytes: []byte(hw)}, nil
	case "guid":
		b, ok := parseGUID(text)
		if !ok {
			return Value{}, fmt.Errorf("%w: guid %q", ErrValue, text)
		}
		return Value{Kind: KindGUID, Bytes: b}, nil
	case "ipv4":
		a, err := netip.ParseAddr(text)
		if err != nil || !a.Is4() {
			return Value{}, fmt.Errorf("%w: ipv4 %q", ErrValue, text)
		}
		b := a.As4()
		return Value{Kind: KindIPv4, Bytes: b[:]}, nil
	case "ipv6", "gid":
		a, err := netip.ParseAddr(text)
		if err != nil || !a.Is6() || a.Zone() != "" {
			return Value{}, fmt.Errorf("%w: %s %q", ErrValue, kind, text)
		}
		b := a.As16()
		return Value{Kind: KindIPv6, Bytes: b[:]}, nil
	}
	return Value{}, fmt.Errorf("%w: unknown kind %q", ErrValue, kind)
}

func parseGUID(text string) ([]byte, bool) {
	var digits string
	switch {
	case strings.HasPrefix(text, "0x"):
		digits = text[2:]
	case len(text) == 19 && text[4] == ':' && text[9] == ':' && text[14] == ':':
		digits = text[0:4] + text[5:9] + text[10:14] + text[15:19]
	default:
		return nil, false
	}
	if len(digits) != 16 {
		return nil, false
	}
	b, err := hex.DecodeString(digits)
	if err != nil {
		return nil, false
	}
	return b, true
}

// ReadValues reads a values file: one "<kind> <value>" per line, with blank
// lines and #-comments ignored.
func ReadValues(r io.Reader) ([]Value, error) {
	var values []Value
	scanner := bufio.NewScanner(r)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%w: line %d: want \"<kind> <value>\", got %q", ErrValue, line, text)
		}
		v, err := ParseValue(fields[0], fields[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		values = append(values, v)
	}
	return values, scanner.Err()
}

// key orders a kind's values for numbering. It is a sha256 so the order does
// not depend on the values file; it is never written out, because an unsalted
// hash of a MAC, GUID or IPv4 address can be brute-forced back to the value.
func (v Value) key() string {
	sum := sha256.Sum256(append([]byte(v.Kind.String()+":"), v.Bytes...))
	return hex.EncodeToString(sum[:])
}

// Documentation ranges. A real value already inside one is kept as it is,
// which is also what makes redaction idempotent.
var (
	docMACPrefix  = []byte{0x00, 0x00, 0x5e, 0x00, 0x53} // RFC 7042
	docGUIDPrefix = []byte{0x02, 0x00, 0x5e, 0x10, 0x00, 0x00, 0x00}
	docIPv6Prefix = netip.MustParsePrefix("2001:db8::/32") // RFC 3849
	docIPv4       = []netip.Prefix{                        // RFC 5737
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
	}
	linkLocal = netip.MustParsePrefix("fe80::/64")
)

func isDoc(v Value) bool {
	switch v.Kind {
	case KindMAC:
		return bytes.HasPrefix(v.Bytes, docMACPrefix)
	case KindGUID:
		if bytes.HasPrefix(v.Bytes, docGUIDPrefix) {
			return true
		}
		mac, ok := macFromEUI64(v.Bytes)
		return ok && bytes.HasPrefix(mac, docMACPrefix)
	case KindIPv4:
		a := netip.AddrFrom4([4]byte(v.Bytes))
		for _, p := range docIPv4 {
			if p.Contains(a) {
				return true
			}
		}
		return a.IsUnspecified() || a.IsLoopback()
	case KindIPv6:
		a := netip.AddrFrom16([16]byte(v.Bytes))
		if docIPv6Prefix.Contains(a) || a.IsUnspecified() || a.IsLoopback() {
			return true
		}
		if linkLocal.Contains(a) {
			return isDoc(Value{Kind: KindGUID, Bytes: v.Bytes[8:]})
		}
		if a.Is4In6() {
			v4 := a.Unmap().As4()
			return isDoc(Value{Kind: KindIPv4, Bytes: v4[:]})
		}
	}
	return false
}

// eui64 is the modified EUI-64 a MAC gives a GUID or IPv6 interface id.
func eui64(mac []byte) []byte {
	return []byte{mac[0] ^ 0x02, mac[1], mac[2], 0xff, 0xfe, mac[3], mac[4], mac[5]}
}

func macFromEUI64(id []byte) ([]byte, bool) {
	if id[3] != 0xff || id[4] != 0xfe {
		return nil, false
	}
	return []byte{id[0] ^ 0x02, id[1], id[2], id[5], id[6], id[7]}, true
}

// mapping assigns each real value its documentation value.
type mapping struct {
	repl  map[string][]byte // kind:hex(real) -> replacement bytes
	kinds map[string]string // replacement text -> kind, for the manifest
	all   []Value           // every value with a replacement, derived ones included
}

func mapKey(v Value) string { return v.Kind.String() + ":" + hex.EncodeToString(v.Bytes) }

// newMapping derives the full value set and assigns replacements. Derived
// values are the GUID inside a link-local GID and the IPv4 address inside an
// IPv4-mapped GID; a GUID that is a MAC's EUI-64 gets the EUI-64 of that MAC's
// replacement, so a GID, a GUID and a MAC from the same port stay consistent
// after redaction. Within a kind, values are numbered in the order of their
// sha256, so the assignment does not depend on the order of the values file.
func newMapping(values []Value) (*mapping, error) {
	byKind := map[Kind]map[string]Value{}
	add := func(v Value) {
		if byKind[v.Kind] == nil {
			byKind[v.Kind] = map[string]Value{}
		}
		byKind[v.Kind][mapKey(v)] = v
	}
	for _, v := range values {
		add(v)
		if v.Kind == KindIPv6 {
			a := netip.AddrFrom16([16]byte(v.Bytes))
			switch {
			case linkLocal.Contains(a):
				add(Value{Kind: KindGUID, Bytes: append([]byte(nil), v.Bytes[8:]...)})
			case a.Is4In6():
				v4 := a.Unmap().As4()
				add(Value{Kind: KindIPv4, Bytes: v4[:]})
			}
		}
	}
	m := &mapping{repl: map[string][]byte{}, kinds: map[string]string{}}
	assign := func(kind Kind, next func(n int) []byte) error {
		set := byKind[kind]
		used := map[string]bool{}
		for _, v := range set {
			if isDoc(v) {
				used[hex.EncodeToString(v.Bytes)] = true
			}
		}
		ordered := make([]Value, 0, len(set))
		for _, v := range set {
			ordered = append(ordered, v)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].key() < ordered[j].key() })
		n := 0
		for _, v := range ordered {
			var r []byte
			switch {
			case isDoc(v):
				r = v.Bytes
			case kind == KindGUID:
				r = m.guidFromMAC(v)
			case kind == KindIPv6:
				r = m.ipv6(v)
			}
			for r == nil {
				n++
				if n > 255 {
					return fmt.Errorf("%w: more than 255 %s values", ErrValue, kind)
				}
				if c := next(n); !used[hex.EncodeToString(c)] {
					r = c
				}
			}
			used[hex.EncodeToString(r)] = true
			m.repl[mapKey(v)] = r
			m.kinds[formatValue(Value{Kind: kind, Bytes: r})] = kind.String()
			m.all = append(m.all, v)
		}
		return nil
	}
	steps := []struct {
		kind Kind
		next func(n int) []byte
	}{
		{KindMAC, func(n int) []byte { return append(append([]byte(nil), docMACPrefix...), byte(n)) }},
		{KindIPv4, func(n int) []byte { return []byte{192, 0, 2, byte(n)} }},
		{KindGUID, func(n int) []byte { return append(append([]byte(nil), docGUIDPrefix...), byte(n)) }},
		{KindIPv6, func(n int) []byte {
			b := docIPv6Prefix.Addr().As16()
			b[15] = byte(n)
			return b[:]
		}},
	}
	for _, s := range steps {
		if err := assign(s.kind, s.next); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// guidFromMAC returns the EUI-64 of a MAC's replacement if v is that MAC's
// EUI-64, or nil.
func (m *mapping) guidFromMAC(v Value) []byte {
	mac, ok := macFromEUI64(v.Bytes)
	if !ok {
		return nil
	}
	r, ok := m.repl[mapKey(Value{Kind: KindMAC, Bytes: mac})]
	if !ok {
		return nil
	}
	return eui64(r)
}

// ipv6 keeps a link-local or IPv4-mapped address's form and replaces only the
// part that identifies the host; anything else is numbered in 2001:db8::/32.
func (m *mapping) ipv6(v Value) []byte {
	a := netip.AddrFrom16([16]byte(v.Bytes))
	out := append([]byte(nil), v.Bytes...)
	switch {
	case linkLocal.Contains(a):
		copy(out[8:], m.repl[mapKey(Value{Kind: KindGUID, Bytes: v.Bytes[8:]})])
		return out
	case a.Is4In6():
		v4 := a.Unmap().As4()
		copy(out[12:], m.repl[mapKey(Value{Kind: KindIPv4, Bytes: v4[:]})])
		return out
	}
	return nil
}

// formatValue is a value's canonical text, the form the manifest records.
func formatValue(v Value) string {
	switch v.Kind {
	case KindMAC:
		return net.HardwareAddr(v.Bytes).String()
	case KindGUID:
		return guidColon(v.Bytes)
	case KindIPv4:
		return netip.AddrFrom4([4]byte(v.Bytes)).String()
	case KindIPv6:
		return netip.AddrFrom16([16]byte(v.Bytes)).String()
	}
	return hex.EncodeToString(v.Bytes)
}

func guidColon(b []byte) string {
	h := hex.EncodeToString(b)
	return h[0:4] + ":" + h[4:8] + ":" + h[8:12] + ":" + h[12:16]
}

func ipv6Full(b []byte) string {
	h := hex.EncodeToString(b)
	parts := make([]string, 0, 8)
	for i := 0; i < 32; i += 4 {
		parts = append(parts, h[i:i+4])
	}
	return strings.Join(parts, ":")
}

// textForms lists the ways a value is printed by sysfs, iproute2, rdma-core
// and infiniband-diags. Each form pairs with the same form of the
// replacement.
func textForms(kind Kind, b []byte) []string {
	switch kind {
	case KindMAC:
		// Bare hex is how systemd's predictable altname (enx<mac>) and sysfs
		// phys_port_id carry a MAC; netlink sends the altname in
		// IFLA_PROP_LIST. The enx form is listed because text matching does
		// not start inside a run of hex digits, and x counts as one.
		s := net.HardwareAddr(b).String()
		h := hex.EncodeToString(b)
		return []string{s, strings.ToUpper(s), "enx" + h, h, strings.ToUpper(h)}
	case KindGUID:
		h := hex.EncodeToString(b)
		return []string{guidColon(b), "0x" + h, "0x" + strings.ToUpper(h), h, strings.ToUpper(h)}
	case KindIPv4:
		return []string{netip.AddrFrom4([4]byte(b)).String()}
	case KindIPv6:
		a := netip.AddrFrom16([16]byte(b))
		return []string{ipv6Full(b), a.String()}
	}
	return nil
}

// binaryForms lists the byte sequences a value appears as on the wire: its
// network-order bytes and, for a GUID, the byte-reversed u64 that NLDEV's
// NODE_GUID and SYS_IMAGE_GUID attributes carry on a little-endian host.
func binaryForms(kind Kind, b []byte) [][]byte {
	forms := [][]byte{b}
	if kind == KindGUID {
		rev := make([]byte, len(b))
		for i := range b {
			rev[len(b)-1-i] = b[i]
		}
		forms = append(forms, rev)
	}
	return forms
}
