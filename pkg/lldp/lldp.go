// Package lldp is a pure-Go client for the lldpd control socket.
//
// It speaks lldpd's control protocol (see src/ctl.c in the lldpd source) and
// reimplements lldpd's custom pointer-graph marshaling (src/marshal.c) so it
// can read the LLDP neighbor table per interface without cgo or liblldpctl.
//
// The wire format is little-endian / LP64 (amd64 and arm64). Everything here is
// best-effort: on any dial, protocol, or parse error we return an error and the
// caller is expected to treat LLDP as simply unavailable. Nothing in this
// package panics on malformed input and nothing calls log.Fatal/os.Exit.
package lldp

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

// DefaultSocketPath is lldpd's default control socket.
const DefaultSocketPath = "/run/lldpd.socket"

// Protocol constants verified against src/ctl.h.
const (
	hmsgHeaderSize = 16      // sizeof(struct hmsg_header): int32 + pad + uint64
	hmsgMaxSize    = 1 << 19 // HMSG_MAX_SIZE = 524288
	wrapperHeader  = 16      // sizeof(struct marshal_serialized): orig(8)+size(8)
	alignTo        = 8       // ALIGNOF(struct marshal_serialized)
)

// hmsg_type opcodes (enum order in src/ctl.h).
const (
	opNone          = 0
	opGetConfig     = 1
	opSetConfig     = 2
	opGetInterfaces = 3
	opSetChassis    = 4
	opGetChassis    = 5
	opGetInterface  = 6
	opGetDefault    = 7
	opSetPort       = 8
	opSubscribe     = 9
	opNotification  = 10
)

// LLDP chassis-id subtypes (src/lldp-const.h).
const (
	chassisSubtypeLLADDR = 4 // MAC address
)

// LLDP port-id subtypes (src/lldp-const.h).
const (
	portSubtypeLLADDR = 3 // MAC address
	portSubtypeIfname = 5 // interface name
	portSubtypeLocal  = 7 // locally assigned
)

// lldpd_mgmt m_family values (enum in src/lldpd-structs.h).
const (
	afIPv4 = 1 // LLDPD_AF_IPV4
	afIPv6 = 2 // LLDPD_AF_IPV6
)

// Errors returned by the decoder. Callers generally only need to know that
// something went wrong; these are exported to allow errors.Is checks.
var (
	// ErrShortBuffer indicates a payload/offset that runs past the buffer.
	ErrShortBuffer = errors.New("lldp: buffer too short")
	// ErrTooLarge indicates a declared length exceeding HMSG_MAX_SIZE.
	ErrTooLarge = errors.New("lldp: length exceeds HMSG_MAX_SIZE")
	// ErrProtocol indicates an unexpected reply type or a server error reply.
	ErrProtocol = errors.New("lldp: protocol error")
	// ErrMalformed indicates a structurally invalid marshaled object.
	ErrMalformed = errors.New("lldp: malformed marshaled object")
)

// Neighbor is a single discovered LLDP neighbor on a local interface.
type Neighbor struct {
	Ifname      string // local interface the neighbor was seen on
	ChassisName string // chassis c_name (the switch SysName)
	ChassisID   string // c_id, formatted per c_id_subtype
	MgmtIP      string // first management address (IPv4 preferred)
	PortID      string // p_id, formatted per p_id_subtype
	PortDescr   string // p_descr
}

// Layout captures the version-dependent lldpd_hardware fixed-block size.
//
// Only lldpd_hardware changed size across the versions we support; lldpd_chassis,
// lldpd_port and lldpd_mgmt are identical. Because we locate the appended
// h_lport / h_rports wrappers positionally (using each wrapper's self-describing
// size), the only version-dependent number we need is where the first appended
// wrapper starts, i.e. the hardware fixed-block size. The size drifted as fields
// were added:
//   - 1.0.13 → 1.0.18: gained h_ifindex_changed and h_ifdescr_previous (+8).
//   - 1.0.18 → 1.0.22: gained h_ifalias (a char* added in 1.0.21) (+8). The
//     1.0.20 change that un-#ifdef'd h_tx_fast is size-neutral in the
//     all-ENABLE_* build (the field was already present).
type Layout struct {
	Name         string
	HardwareSize int // sizeof(struct lldpd_hardware) on amd64/LP64, all ENABLE_* on
}

// Version-specific presets. Sizes were computed from the exact C struct
// definitions at git tags 1.0.13, 1.0.18 and 1.0.22 (amd64, LP64, all feature
// macros enabled — the distro default build).
var (
	Layout1_0_13 = Layout{Name: "1.0.13", HardwareSize: 624}
	Layout1_0_18 = Layout{Name: "1.0.18", HardwareSize: 632}
	Layout1_0_22 = Layout{Name: "1.0.22", HardwareSize: 640}
)

// layoutsFor returns the layout(s) to try for a version hint. An empty hint is
// tolerant: try the newest layout first, then progressively older ones. The
// 1.0.19/1.0.20 tags share 1.0.18's size, and 1.0.21 shares 1.0.22's size, so
// those hints map onto the nearest matching preset.
func layoutsFor(hint string) []Layout {
	switch hint {
	case "1.0.13":
		return []Layout{Layout1_0_13}
	case "1.0.18", "1.0.19", "1.0.20":
		return []Layout{Layout1_0_18}
	case "1.0.21", "1.0.22":
		return []Layout{Layout1_0_22}
	default:
		return []Layout{Layout1_0_22, Layout1_0_18, Layout1_0_13}
	}
}

// Fetch dials the lldpd control socket, requests all interfaces and each
// interface's neighbors, and returns a map of ifname -> first neighbor. LLDP is
// typically one upstream switch per physical uplink, so the first neighbor is
// the interesting one. It is best-effort: a dial or top-level protocol error is
// returned (the caller should treat LLDP as disabled); a per-interface decode
// error is skipped so one bad interface does not hide the others.
func Fetch(ctx context.Context, socketPath, versionHint string) (map[string]Neighbor, error) {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close() //nolint:errcheck // best-effort close of a read-only control-socket conn
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl) //nolint:errcheck,gosec // best-effort deadline; a failure just means no timeout
	}

	if err := writeMsg(conn, opGetInterfaces, nil); err != nil {
		return nil, err
	}
	ifPayload, err := readMsg(conn, opGetInterfaces)
	if err != nil {
		return nil, err
	}
	ifnames, err := ParseInterfaces(ifPayload)
	if err != nil {
		return nil, err
	}

	layouts := layoutsFor(versionHint)
	out := make(map[string]Neighbor, len(ifnames))
	for _, ifn := range ifnames {
		if err := writeMsg(conn, opGetInterface, marshalString(ifn)); err != nil {
			return nil, err
		}
		hwPayload, err := readMsg(conn, opGetInterface)
		if err != nil {
			// A per-interface failure is non-fatal; keep going.
			continue
		}
		neighbors, err := parseHardwareTolerant(hwPayload, layouts)
		if err != nil || len(neighbors) == 0 {
			continue
		}
		n := neighbors[0]
		n.Ifname = ifn
		out[ifn] = n
	}
	return out, nil
}

// writeMsg frames and sends a single control message. A nil payload sends a
// header-only request (len == 0), matching ctl_msg_send_unserialized.
func writeMsg(w io.Writer, opcode int32, payload []byte) error {
	var hdr [hmsgHeaderSize]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(opcode))
	binary.LittleEndian.PutUint64(hdr[8:16], uint64(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// readMsg reads one reply and returns its payload. It validates the reply type
// and rejects lengths larger than HMSG_MAX_SIZE. A NONE/len==0 reply is the
// lldpd error reply and is reported as a protocol error.
func readMsg(r io.Reader, want int32) ([]byte, error) {
	var hdr [hmsgHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	typ := int32(binary.LittleEndian.Uint32(hdr[0:4]))
	length := binary.LittleEndian.Uint64(hdr[8:16])
	if length > hmsgMaxSize {
		return nil, ErrTooLarge
	}
	if typ != want {
		if typ == opNone && length == 0 {
			return nil, fmt.Errorf("%w: server returned error reply", ErrProtocol)
		}
		return nil, fmt.Errorf("%w: reply type %d, want %d", ErrProtocol, typ, want)
	}
	if length == 0 {
		return nil, nil
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// marshalString serializes a bare C string the way lldpd does for the
// GET_INTERFACE request argument: a single wrapper [orig=1][size][bytes+NUL].
func marshalString(s string) []byte {
	size := wrapperHeader + len(s) + 1
	buf := make([]byte, size)
	binary.LittleEndian.PutUint64(buf[0:8], 1) // dummy orig id
	binary.LittleEndian.PutUint64(buf[8:16], uint64(size))
	copy(buf[16:], s)
	// trailing NUL already zero
	return buf
}

// --- formatting helpers ---

// formatChassisID renders a chassis id per its subtype: MAC as colon-hex,
// otherwise printable ASCII as-is, otherwise plain hex.
func formatChassisID(subtype byte, b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if subtype == chassisSubtypeLLADDR {
		return macString(b)
	}
	if isPrintableASCII(b) {
		return string(b)
	}
	return hex.EncodeToString(b)
}

// formatPortID renders a port id per its subtype: MAC as colon-hex, ifname or
// locally-assigned as ASCII, otherwise printable ASCII or plain hex.
func formatPortID(subtype byte, b []byte) string {
	if len(b) == 0 {
		return ""
	}
	switch subtype {
	case portSubtypeLLADDR:
		return macString(b)
	case portSubtypeIfname, portSubtypeLocal:
		return string(b)
	default:
		if isPrintableASCII(b) {
			return string(b)
		}
		return hex.EncodeToString(b)
	}
}

// macString formats bytes as lowercase colon-separated hex (xx:xx:...).
func macString(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(parts, ":")
}

func isPrintableASCII(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// formatMgmt renders an lldpd_mgmt address given its family and 16 octets
// (network byte order). Returns "" for unknown families.
func formatMgmt(family int32, octets []byte) string {
	switch family {
	case afIPv4:
		if len(octets) < 4 {
			return ""
		}
		return net.IP(octets[0:4]).String()
	case afIPv6:
		if len(octets) < 16 {
			return ""
		}
		return net.IP(octets[0:16]).String()
	default:
		return ""
	}
}
