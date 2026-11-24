package goldenrec

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// A capture from another host can't be diffed row for row against the
// golden fixture: its workload differs. Its shape can: which columns are
// populated, which enum values occur, which socket families and kinds of
// namespace. ProfileOf reduces a capture to that, and DiffProfiles compares
// two.
//
// A profile is meant to leave the host it was taken on, so it records no
// identity: a column's values are listed only when profileListed names it,
// and every other column is counted.

const (
	// maxDistinct is where a distinct count stops: past it, "64+".
	maxDistinct = 64
	// maxValues is the most values a column's set lists.
	maxValues = 32

	netnsColumn     = "netns"
	containerColumn = "container_name"
	hostNetns       = "default"
)

// profileListed decides, for each column of a valued class, whether a
// profile lists its values (true) or only counts them (false). Listed
// columns say what software and hardware run and how sockets behave; the
// count-only ones could say who runs there or whom they talk to. A valued
// column missing here is a test failure, so each new one gets a decision.
var profileListed = map[string]bool{
	"schema_version":                true,
	"container_runtime":             true,
	"uplink1_nic_driver":            true,
	"uplink1_nic_pci_vendor":        true,
	"uplink1_nic_pci_device":        true,
	"uplink1_nic_speed_mbps":        true,
	"uplink2_nic_driver":            true,
	"uplink2_nic_pci_vendor":        true,
	"uplink2_nic_pci_device":        true,
	"uplink2_nic_speed_mbps":        true,
	"enrich_socket_dest_locality":   true,
	familyColumn:                    true,
	stateColumn:                     true,
	"tcp_info_state":                true,
	"tcp_info_ca_state":             true,
	"tcp_info_options":              true,
	"tcp_info_snd_wscale":           true,
	"tcp_info_rcv_wscale":           true,
	"tcp_info_fastopen_client_fail": true,
	"tcp_info_snd_mss":              true,
	"tcp_info_pmtu":                 true,
	"tcp_info_advmss":               true,
	"tcp_info_reordering":           true,
	"tcp_info_ecn_mode":             true,
	"tcp_info_accecn_fail_mode":     true,
	"tcp_info_options2":             true,
	"inet_diag_cong":                true,
	"inet_diag_cong_enum":           true,
	"inet_diag_tos":                 true,
	"inet_diag_tclass":              true,
	"inet_diag_shutdown":            true,
	"inet_diag_sockopt":             true,

	"location":                         false,
	"label":                            false,
	"tag":                              false,
	"treatment_state_revision":         false,
	"treatment_assignment_id":          false,
	"treatment_generation":             false,
	"treatment_algorithm_exposed":      false,
	"container_name":                   false,
	"container_image":                  false,
	"uplink1_nic_model":                false,
	"uplink2_nic_model":                false,
	"enrich_socket_interface_name":     false,
	"enrich_socket_dest_egress_ifname": false,
	"enrich_socket_dest_asn":           false,
	"enrich_socket_dest_next_hop_asn":  false,
	"enrich_socket_dest_network_owner": false,
	"inet_diag_msg_uid":                false,
	"inet_diag_class_id":               false,
}

// ColumnProfile is one column's shape.
type ColumnProfile struct {
	Class   string `json:"class"`
	Nonzero int    `json:"nonzero"`
	// Distinct is the number of distinct values, "64+" past maxDistinct;
	// empty for a Volatile column.
	Distinct string `json:"distinct,omitempty"`
	// Values is the sorted value set of a column profileListed lists, of a
	// valued class, with at most maxValues distinct values.
	Values []string `json:"values,omitempty"`
}

// Profile is one capture's shape.
type Profile struct {
	Rows    int                      `json:"rows"`
	Columns map[string]ColumnProfile `json:"columns"`
	// Families counts rows per inet_diag_msg_family.
	Families map[string]int `json:"families"`
	// NetnsKinds counts rows per kind of namespace: container (a container
	// name was resolved), host ("default"), docker, anon, named, unknown.
	NetnsKinds map[string]int `json:"netns_kinds"`
}

// valued says whether a class's values are worth listing: the ones a diff
// compares as they are.
func valued(cl Class) bool {
	return cl == Strict
}

// ProfileOf profiles rows, a slice of structs classified by table.
func ProfileOf(rows any, table Table) (Profile, error) {
	rv := reflect.ValueOf(rows)
	if rv.Kind() != reflect.Slice || rv.Type().Elem().Kind() != reflect.Struct {
		return Profile{}, fmt.Errorf("goldenrec: rows must be a slice of structs, got %T", rows)
	}
	idx, err := table.index()
	if err != nil {
		return Profile{}, err
	}
	cols := columnsOf(rv.Type().Elem())
	p := Profile{
		Rows:       rv.Len(),
		Columns:    make(map[string]ColumnProfile, len(cols)),
		Families:   map[string]int{},
		NetnsKinds: map[string]int{},
	}
	field := make(map[string]int, len(cols))
	for _, c := range cols {
		cl, ok := idx[c.name]
		if !ok {
			return Profile{}, fmt.Errorf("unclassified column %s", c.name)
		}
		field[c.name] = c.field
		p.Columns[c.name] = profileColumn(rv, c, cl)
	}
	for i := range rv.Len() {
		row := rv.Index(i)
		fam := "0"
		if f, ok := field[familyColumn]; ok {
			fam = strconv.FormatUint(uintOf(row.Field(f)), 10)
		}
		p.Families[fam]++
		p.NetnsKinds[netnsKindOf(row, field)]++
	}
	return p, nil
}

// profileColumn counts one column over every row.
func profileColumn(rv reflect.Value, c column, cl Class) ColumnProfile {
	cp := ColumnProfile{Class: cl.String()}
	seen := make(map[string]bool)
	for i := range rv.Len() {
		v := rv.Index(i).Field(c.field)
		if !isZero(v) {
			cp.Nonzero++
		}
		if cl != Volatile && len(seen) <= maxDistinct {
			seen[format(v)] = true
		}
	}
	if cl == Volatile {
		return cp
	}
	if len(seen) > maxDistinct {
		cp.Distinct = strconv.Itoa(maxDistinct) + "+"
		return cp
	}
	cp.Distinct = strconv.Itoa(len(seen))
	if valued(cl) && profileListed[c.name] && len(seen) > 0 && len(seen) <= maxValues {
		cp.Values = slices.SortedFunc(maps.Keys(seen), shortlex)
	}
	return cp
}

// netnsKindOf is the kind of namespace a row's socket lives in.
func netnsKindOf(row reflect.Value, field map[string]int) string {
	if f, ok := field[containerColumn]; ok && row.Field(f).String() != "" {
		return "container"
	}
	f, ok := field[netnsColumn]
	if !ok {
		return "unknown"
	}
	switch s := row.Field(f).String(); {
	case s == "":
		return "unknown"
	case s == hostNetns:
		return "host"
	case netnsKind(s) != "":
		return netnsKind(s)
	default:
		return "named"
	}
}

// Finding is one way two profiles' shapes differ. Side is "A" or "B", the
// profile that has (or, for family-missing and kind-missing, lacks) Item.
type Finding struct {
	Kind string
	Side string
	Item string
}

func (f Finding) String() string {
	if f.Item == "" {
		return f.Kind + " " + f.Side
	}
	return f.Kind + " " + f.Side + ": " + f.Item
}

// DiffProfiles lists how a's and b's shapes differ, sorted. It ignores row
// counts and how many distinct values a column has, which follow the
// workload.
func DiffProfiles(a, b Profile) []Finding {
	var out []Finding
	sides := [2]struct {
		name string
		p    Profile
	}{{"A", a}, {"B", b}}
	for _, s := range sides {
		if s.p.Rows == 0 {
			out = append(out, Finding{Kind: "no-rows", Side: s.name})
		}
	}
	if len(out) > 0 {
		return out
	}
	for i, s := range sides {
		other := sides[1-i]
		for name, c := range s.p.Columns {
			o, ok := other.p.Columns[name]
			if !ok {
				out = append(out, Finding{"column-only-in", s.name, name})
				continue
			}
			if c.Nonzero > 0 && o.Nonzero == 0 {
				out = append(out, Finding{"populated-only-in", s.name, name})
			}
			if name == familyColumn || c.Values == nil || o.Values == nil {
				continue
			}
			for _, v := range c.Values {
				if !slices.Contains(o.Values, v) {
					out = append(out, Finding{"value-only-in", s.name, name + "=" + v})
				}
			}
		}
		for k := range other.p.Families {
			if s.p.Families[k] == 0 {
				out = append(out, Finding{"family-missing", s.name, k})
			}
		}
		for k := range other.p.NetnsKinds {
			if s.p.NetnsKinds[k] == 0 {
				out = append(out, Finding{"kind-missing", s.name, k})
			}
		}
	}
	slices.SortFunc(out, func(x, y Finding) int {
		if c := strings.Compare(x.Kind, y.Kind); c != 0 {
			return c
		}
		if c := strings.Compare(x.Side, y.Side); c != 0 {
			return c
		}
		return shortlex(x.Item, y.Item)
	})
	return out
}
