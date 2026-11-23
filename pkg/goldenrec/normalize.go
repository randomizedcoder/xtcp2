package goldenrec

import (
	"cmp"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Row is one normalized record: column name to normalized value. Volatile
// columns are absent.
type Row map[string]string

const (
	// ephemeralPortMin is where Linux's default ephemeral range
	// (net.ipv4.ip_local_port_range) begins.
	ephemeralPortMin = 32768
	// maxDocV4 is the number of host addresses in 192.0.2.0/24 (TEST-NET-1).
	maxDocV4 = 254
	// maxDocV6 is the number of addresses 2001:db8::1 to 2001:db8::ffff.
	maxDocV6 = 0xffff
	// familyColumn names the column whose value says how to read the
	// 16-byte address columns (2 = AF_INET, 10 = AF_INET6).
	familyColumn = "inet_diag_msg_family"
	afInet       = 2
	afInet6      = 10
)

// errTooManyAddresses is returned when a capture has more distinct addresses
// than the documentation range holds; mapping two to one would hide a
// difference.
var errTooManyAddresses = errors.New("goldenrec: too many distinct addresses")

// addrKind is how an address cell reads before numbering.
type addrKind int

const (
	addrEmpty addrKind = iota
	addrKept           // loopback or unspecified: kept as is
	addrV4
	addrV6
	addrInvalid
)

// addrCell is one decoded address.
type addrCell struct {
	kind addrKind
	addr netip.Addr
	n    int // the original byte length, for addrInvalid and for writing back
}

// cell is one column of one row before ordinals and addresses are numbered.
type cell struct {
	sortForm string // what the row sort uses: never an identity or address
	raw      string // the identity an ordinal or netns cell is numbered by
	addr     addrCell
	pending  bool // set: numbered in the second pass
}

// plan is one row set, classified and sorted; Normalize and Scrub share it,
// so the scrubbed fixture numbers everything the way Normalize does.
type plan struct {
	cols    []column
	classes []Class
	rows    reflect.Value
	cells   [][]cell
	order   []int // row indices in sorted order

	ordinals map[string]map[string]int // column -> raw identity -> N
	netns    map[string]int
	v4, v6   map[netip.Addr]int
}

// newPlan classifies every cell of rows (a slice of structs) and sorts the
// rows by everything that is not an identity, so the order does not depend
// on cookies, inodes or addresses.
func newPlan(rows any, table Table) (*plan, error) {
	rv := reflect.ValueOf(rows)
	if rv.Kind() != reflect.Slice || rv.Type().Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("goldenrec: rows must be a slice of structs, got %T", rows)
	}
	idx, err := table.index()
	if err != nil {
		return nil, err
	}
	p := &plan{rows: rv, cols: columnsOf(rv.Type().Elem())}
	famField := -1
	var errs []error
	for _, c := range p.cols {
		cl, ok := idx[c.name]
		if !ok {
			errs = append(errs, fmt.Errorf("unclassified column %s", c.name))
		}
		p.classes = append(p.classes, cl)
		if c.name == familyColumn {
			famField = c.field
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	p.cells = make([][]cell, rv.Len())
	for i := range rv.Len() {
		row := rv.Index(i)
		family := uint64(0)
		if famField >= 0 {
			family = uintOf(row.Field(famField))
		}
		p.cells[i] = make([]cell, len(p.cols))
		for j, c := range p.cols {
			p.cells[i][j] = classify(p.classes[j], row.Field(c.field), family)
		}
	}

	p.order = make([]int, rv.Len())
	keys := make([]string, rv.Len())
	ties := make([][]string, rv.Len())
	for i := range p.order {
		p.order[i] = i
		var k strings.Builder
		for _, c := range p.cells[i] {
			k.WriteString(c.sortForm)
			k.WriteByte(0)
			if c.pending {
				ties[i] = append(ties[i], c.raw)
			}
		}
		keys[i] = k.String()
	}
	sort.SliceStable(p.order, func(a, b int) bool {
		ia, ib := p.order[a], p.order[b]
		if keys[ia] != keys[ib] {
			return keys[ia] < keys[ib]
		}
		return compareSeq(ties[ia], ties[ib], shortlex) < 0
	})

	// Rows tied on every non-identity column are put in the order of their
	// numbers and renumbered, until the order holds. At that point a scrubbed
	// copy, whose identities are those numbers, sorts and numbers the same
	// way, so Normalize(Scrub(r)) == Normalize(r) and Scrub is idempotent.
	for range maxRenumber {
		if err := p.number(); err != nil {
			return nil, err
		}
		nums := p.numbers()
		prev := slices.Clone(p.order)
		sort.SliceStable(p.order, func(a, b int) bool {
			ia, ib := p.order[a], p.order[b]
			if keys[ia] != keys[ib] {
				return keys[ia] < keys[ib]
			}
			return compareSeq(nums[ia], nums[ib], cmp.Compare[int]) < 0
		})
		if slices.Equal(prev, p.order) {
			return p, nil
		}
	}
	return p, p.number()
}

// maxRenumber bounds the sort-and-number passes; real captures settle in two.
const maxRenumber = 8

// numbers is each row's assigned numbers, in column order.
func (p *plan) numbers() [][]int {
	out := make([][]int, len(p.cells))
	for i, row := range p.cells {
		for j, c := range row {
			if c.pending {
				out[i] = append(out[i], p.number1(j, c))
			}
		}
	}
	return out
}

// number1 is the number assigned to one pending cell.
func (p *plan) number1(j int, c cell) int {
	switch p.classes[j] {
	case Ordinal:
		return p.ordinals[p.cols[j].name][c.raw]
	case Netns:
		return p.netns[c.raw]
	default: // Address
		if c.addr.kind == addrV4 {
			return p.v4[c.addr.addr]
		}
		return p.v6[c.addr.addr]
	}
}

// shortlex orders shorter strings first, then bytewise: the numeric order
// of decimal numbers, "netns:[N]", 192.0.2.N and 2001:db8::N, which is what
// Scrub writes, so a scrubbed capture sorts by its numbers.
func shortlex(a, b string) int {
	if len(a) != len(b) {
		return cmp.Compare(len(a), len(b))
	}
	return strings.Compare(a, b)
}

func compareSeq[E any](a, b []E, f func(E, E) int) int {
	for i := range min(len(a), len(b)) {
		if c := f(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

// number assigns ordinals and documentation addresses in sorted-row order.
// Addresses share one numbering across columns, so a peer's address reads
// the same as a source and as a destination.
func (p *plan) number() error {
	p.ordinals = make(map[string]map[string]int)
	p.netns = make(map[string]int)
	p.v4 = make(map[netip.Addr]int)
	p.v6 = make(map[netip.Addr]int)
	for _, i := range p.order {
		for j, c := range p.cells[i] {
			if !c.pending {
				continue
			}
			switch p.classes[j] {
			case Ordinal:
				m := p.ordinals[p.cols[j].name]
				if m == nil {
					m = make(map[string]int)
					p.ordinals[p.cols[j].name] = m
				}
				if _, ok := m[c.raw]; !ok {
					m[c.raw] = len(m) + 1
				}
			case Netns:
				if _, ok := p.netns[c.raw]; !ok {
					p.netns[c.raw] = len(p.netns) + 1
				}
			case Address:
				m := p.v6
				if c.addr.kind == addrV4 {
					m = p.v4
				}
				if _, ok := m[c.addr.addr]; !ok {
					m[c.addr.addr] = len(m) + 1
				}
			default:
			}
		}
	}
	if len(p.v4) > maxDocV4 {
		return fmt.Errorf("%w: more than %d distinct v4 addresses (%d)", errTooManyAddresses, maxDocV4, len(p.v4))
	}
	if len(p.v6) > maxDocV6 {
		return fmt.Errorf("%w: more than %d distinct v6 addresses (%d)", errTooManyAddresses, maxDocV6, len(p.v6))
	}
	return nil
}

// docAddr is the documentation address a numbered address maps to.
func (p *plan) docAddr(a addrCell) netip.Addr {
	if a.kind == addrV4 {
		n := p.v4[a.addr]
		return netip.AddrFrom4([4]byte{192, 0, 2, byte(n)})
	}
	n := p.v6[a.addr]
	return netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, 14: byte(n >> 8), 15: byte(n)})
}

// final is a cell's normalized value.
func (p *plan) final(j int, c cell) string {
	if !c.pending {
		if p.classes[j] == Address {
			return addrString(c.addr)
		}
		return c.sortForm
	}
	switch p.classes[j] {
	case Ordinal:
		return "#" + strconv.Itoa(p.number1(j, c))
	case Netns:
		return c.sortForm + ":#" + strconv.Itoa(p.netns[c.raw])
	default: // Address
		return p.docAddr(c.addr).String()
	}
}

// Normalize returns rows (a slice of structs) in normalized, sorted form.
// Two captures of the same workload normalize to the same rows except where
// an attribute really differs.
func Normalize(rows any, table Table) ([]Row, error) {
	p, err := newPlan(rows, table)
	if err != nil {
		return nil, err
	}
	out := make([]Row, 0, len(p.order))
	for _, i := range p.order {
		r := make(Row, len(p.cols))
		for j, c := range p.cells[i] {
			if p.classes[j] == Volatile {
				continue
			}
			r[p.cols[j].name] = p.final(j, c)
		}
		out = append(out, r)
	}
	return out, nil
}

// classify reads one value by its column's class.
func classify(cl Class, v reflect.Value, family uint64) cell {
	switch cl {
	case Strict:
		return cell{sortForm: format(v)}
	case Presence:
		if isZero(v) {
			return cell{sortForm: "0"}
		}
		return cell{sortForm: "nonzero"}
	case Port:
		n := uintOf(v)
		if n >= ephemeralPortMin {
			return cell{sortForm: "eph"}
		}
		return cell{sortForm: strconv.FormatUint(n, 10)}
	case Volatile:
		return cell{}
	case Host:
		if isZero(v) {
			return cell{}
		}
		return cell{sortForm: "<host>"}
	case Netns:
		s := v.String()
		if k := netnsKind(s); k != "" {
			return cell{sortForm: "netns:" + k, raw: s, pending: true}
		}
		return cell{sortForm: s}
	case Ordinal:
		if isZero(v) {
			return cell{sortForm: "0"}
		}
		return cell{sortForm: "#", raw: format(v), pending: true}
	default: // Address
		a := decodeAddr(v, family)
		switch a.kind {
		case addrV4:
			return cell{sortForm: "v4", raw: a.addr.String(), addr: a, pending: true}
		case addrV6:
			return cell{sortForm: "v6", raw: a.addr.String(), addr: a, pending: true}
		default:
			return cell{sortForm: addrString(a), addr: a}
		}
	}
}

// netnsKind classifies a netns name whose value changes from run to run:
// "anon" for a namespace xtcp2 knows only by inode ("netns:[N]"), "docker"
// for a Docker sandbox (/run/docker/netns/<12 hex>), and "" for a stable
// name such as "default" or a named netns. A named netns of exactly 12 hex
// digits reads as Docker's.
func netnsKind(s string) string {
	switch {
	case strings.HasPrefix(s, "netns:[") && strings.HasSuffix(s, "]"):
		return netnsAnon
	case len(s) == dockerNetnsLen && strings.Trim(s, "0123456789abcdef") == "":
		return netnsDocker
	default:
		return ""
	}
}

const (
	netnsAnon      = "anon"
	netnsDocker    = "docker"
	dockerNetnsLen = 12
)

// netnsName is the name Scrub writes for the Nth numbered namespace, one
// netnsKind reads back as the same kind.
func netnsName(kind string, n int) string {
	if kind == netnsDocker {
		return fmt.Sprintf("%0*x", dockerNetnsLen, n)
	}
	return "netns:[" + strconv.Itoa(n) + "]"
}

// decodeAddr reads an address column: a 4- or 16-byte slice (v4 in the first
// 4 bytes of 16 when the row's family is AF_INET), or a string.
func decodeAddr(v reflect.Value, family uint64) addrCell {
	if v.Kind() == reflect.String {
		s := v.String()
		if s == "" {
			return addrCell{}
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return addrCell{kind: addrInvalid, n: len(s)}
		}
		return kindOf(a, len(s))
	}
	b := v.Bytes()
	switch {
	case len(b) == 0:
		return addrCell{}
	case len(b) == 4:
		return kindOf(netip.AddrFrom4([4]byte(b)), 4)
	case len(b) == 16 && (family == afInet || family != afInet6 && allZero(b[4:]) && !allZero(b[:4])):
		return kindOf(netip.AddrFrom4([4]byte(b[:4])), 16)
	case len(b) == 16:
		return kindOf(netip.AddrFrom16([16]byte(b)), 16)
	default:
		return addrCell{kind: addrInvalid, n: len(b)}
	}
}

func kindOf(a netip.Addr, n int) addrCell {
	switch {
	case a.IsLoopback() || a.IsUnspecified():
		return addrCell{kind: addrKept, addr: a, n: n}
	case a.Is4():
		return addrCell{kind: addrV4, addr: a, n: n}
	default:
		return addrCell{kind: addrV6, addr: a, n: n}
	}
}

// addrString is an unnumbered address cell's normalized value.
func addrString(a addrCell) string {
	switch a.kind {
	case addrEmpty:
		return ""
	case addrInvalid:
		return "invalid:" + strconv.Itoa(a.n)
	default:
		return a.addr.String()
	}
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

func isZero(v reflect.Value) bool {
	if v.Kind() == reflect.Slice {
		return v.Len() == 0
	}
	return v.IsZero()
}

func uintOf(v reflect.Value) uint64 {
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() < 0 {
			return 0
		}
		return uint64(v.Int())
	default:
		return 0
	}
}

// format is a value's exact text: strings as is, bytes as hex, numbers in
// decimal.
func format(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return hex.EncodeToString(v.Bytes())
		}
	default:
	}
	return fmt.Sprint(v.Interface())
}
