// Package goldenrec turns xtcp2's Parquet records into a form two captures
// can be compared in: every column is put in a class that says what a
// difference in it means, and the run-to-run noise (timestamps, cookies,
// ephemeral ports, RTTs) is folded away while the presence of each attribute
// is kept. It also scrubs a capture so it can be committed as a fixture.
//
// The package works on any struct whose fields carry `parquet:"name,..."`
// tags, so it does not import pkg/xtcp; Columns is the class table for
// pkg/xtcp's ParquetRow, and pkg/xtcp's tests check it is complete.
//
// See docs/security-hardening/focused-implementation-plan.md (F0.5).
package goldenrec

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Class says how a column is normalized and compared.
type Class int

const (
	// Strict values are kept and compared exactly.
	Strict Class = iota + 1
	// Presence values become "0" or "nonzero": counters and timings.
	Presence
	// Ordinal values are identities (cookies, inodes, ids): each distinct
	// value becomes "#N", numbered in sorted-row order. A diff compares only
	// whether one is set.
	Ordinal
	// Address values map to documentation addresses (192.0.2.0/24,
	// 2001:db8::/32); loopback and unspecified addresses are kept.
	Address
	// Port values below 32768 are kept; ephemeral ports become "eph".
	Port
	// Volatile columns change every run and are dropped.
	Volatile
	// Host values identify the machine; a set value becomes "<host>".
	Host
	// Netns is a namespace name: a container or named netns is kept, an
	// anonymous "netns:[<inode>]" becomes an ordinal.
	Netns
	// Assigned is a bool column named <ordinal>_assigned that says whether
	// that Ordinal column holds an id, for ids where 0 is a real value. It
	// is kept, and the ordinal is numbered whenever it is true, 0 included,
	// and reads "-" when it is false.
	Assigned
)

// assignedSuffix names an Assigned column after its ordinal.
const assignedSuffix = "_assigned"

var classNames = map[Class]string{
	Strict: "strict", Presence: "presence", Ordinal: "ordinal", Address: "address",
	Port: "port", Volatile: "volatile", Host: "host", Netns: "netns", Assigned: "assigned",
}

func (c Class) String() string {
	if n, ok := classNames[c]; ok {
		return n
	}
	return fmt.Sprintf("class(%d)", int(c))
}

// ClassList is one class and its columns.
type ClassList struct {
	Class   Class
	Columns []string
}

// Table is a class table: every column of the row struct must be in exactly
// one list.
type Table []ClassList

// index maps each column to its class; a column listed twice is an error.
func (t Table) index() (map[string]Class, error) {
	idx := make(map[string]Class)
	var errs []error
	for _, l := range t {
		for _, c := range l.Columns {
			if prev, ok := idx[c]; ok {
				errs = append(errs, fmt.Errorf("column %s is in classes %s and %s", c, prev, l.Class))
				continue
			}
			idx[c] = l.Class
		}
	}
	return idx, errors.Join(errs...)
}

// column is one tagged field of a row struct.
type column struct {
	name  string
	field int
}

// columnsOf lists the parquet-tagged fields of t, in field order. A field
// without a tag, or tagged "-", is not a column.
func columnsOf(t reflect.Type) []column {
	var cols []column
	for i := range t.NumField() {
		tag, ok := t.Field(i).Tag.Lookup("parquet")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		cols = append(cols, column{name: name, field: i})
	}
	return cols
}

// Check reports every way t's columns and the table disagree: a column in no
// class, a column in two, a table entry naming no column, and an Assigned
// column that is not a bool flag on an Ordinal column.
func Check(t reflect.Type, table Table) error {
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("goldenrec: %s is not a struct", t)
	}
	idx, err := table.index()
	errs := []error{err}
	have := make(map[string]reflect.Type)
	cols := columnsOf(t)
	for _, c := range cols {
		have[c.name] = t.Field(c.field).Type
		if _, ok := idx[c.name]; !ok {
			errs = append(errs, fmt.Errorf("unclassified column %s", c.name))
		}
	}
	for _, l := range table {
		for _, c := range l.Columns {
			if _, ok := have[c]; !ok {
				errs = append(errs, fmt.Errorf("class entry %s has no column", c))
			}
		}
	}
	for _, c := range cols {
		if idx[c.name] == Assigned {
			errs = append(errs, checkAssigned(c.name, have, idx))
		}
	}
	return errors.Join(errs...)
}

// checkAssigned checks one Assigned column: a bool named after an Ordinal
// column.
func checkAssigned(name string, have map[string]reflect.Type, idx map[string]Class) error {
	ord, ok := strings.CutSuffix(name, assignedSuffix)
	if !ok {
		return fmt.Errorf("assigned column %s: name does not end in %s", name, assignedSuffix)
	}
	if k := have[name].Kind(); k != reflect.Bool {
		return fmt.Errorf("assigned column %s is %s, not bool", name, k)
	}
	if _, ok := have[ord]; !ok {
		return fmt.Errorf("assigned column %s: no column %s", name, ord)
	}
	if cl := idx[ord]; cl != Ordinal {
		return fmt.Errorf("assigned column %s: %s is %s, not ordinal", name, ord, cl)
	}
	return nil
}

// Columns is the class table for pkg/xtcp's ParquetRow.
//
// When a column is added to the record, TestGoldenClassesComplete (pkg/xtcp)
// fails until it is listed here; the golden fixture then needs recapturing
// (nix run .#capture-golden-records).
var Columns = Table{
	{Strict, []string{
		"schema_version", "location", "label", "tag",
		"treatment_state_revision", "treatment_assignment_id", "treatment_generation", "treatment_algorithm_exposed",
		"container_runtime", "container_name", "container_image",
		"uplink1_nic_driver", "uplink1_nic_model", "uplink1_nic_pci_vendor", "uplink1_nic_pci_device", "uplink1_nic_speed_mbps",
		"uplink2_nic_driver", "uplink2_nic_model", "uplink2_nic_pci_vendor", "uplink2_nic_pci_device", "uplink2_nic_speed_mbps",
		"enrich_socket_interface_name", "enrich_socket_dest_locality", "enrich_socket_dest_egress_ifname",
		"enrich_socket_dest_asn", "enrich_socket_dest_next_hop_asn", "enrich_socket_dest_network_owner",
		familyColumn, stateColumn, "inet_diag_msg_uid",
		"tcp_info_state", "tcp_info_ca_state", "tcp_info_options", "tcp_info_snd_wscale", "tcp_info_rcv_wscale",
		"tcp_info_fastopen_client_fail", "tcp_info_snd_mss", "tcp_info_pmtu", "tcp_info_advmss", "tcp_info_reordering",
		"tcp_info_ecn_mode", "tcp_info_accecn_fail_mode", "tcp_info_options2",
		"inet_diag_cong", "inet_diag_cong_enum", "inet_diag_tos", "inet_diag_tclass", "inet_diag_shutdown",
		"inet_diag_class_id", "inet_diag_sockopt",
	}},
	{Presence, []string{
		"enrich_socket_dest_egress_ifindex",
		"inet_diag_msg_timer", "inet_diag_msg_retrans", "inet_diag_msg_socket_interface",
		"inet_diag_msg_rqueue", "inet_diag_msg_wqueue",
		"mem_info_rmem", "mem_info_wmem", "mem_info_fmem", "mem_info_tmem",
		"tcp_info_retransmits", "tcp_info_probes", "tcp_info_backoff", "tcp_info_delivery_rate_app_limited",
		"tcp_info_rto", "tcp_info_ato", "tcp_info_rcv_mss", "tcp_info_unacked", "tcp_info_sacked", "tcp_info_lost",
		"tcp_info_retrans", "tcp_info_fackets", "tcp_info_last_data_sent", "tcp_info_last_ack_sent",
		"tcp_info_last_data_recv", "tcp_info_last_ack_recv", "tcp_info_rcv_ssthresh", "tcp_info_rtt", "tcp_info_rttvar",
		"tcp_info_snd_ssthresh", "tcp_info_snd_cwnd", "tcp_info_rcv_space", "tcp_info_total_retrans",
		"tcp_info_pacing_rate", "tcp_info_max_pacing_rate", "tcp_info_bytes_acked", "tcp_info_bytes_received",
		"tcp_info_segs_out", "tcp_info_segs_in", "tcp_info_notsent_bytes", "tcp_info_min_rtt",
		"tcp_info_data_segs_in", "tcp_info_data_segs_out", "tcp_info_delivery_rate",
		"tcp_info_rwnd_limited", "tcp_info_sndbuf_limited", "tcp_info_delivered", "tcp_info_delivered_ce",
		"tcp_info_bytes_sent", "tcp_info_bytes_retrans", "tcp_info_dsack_dups", "tcp_info_reord_seen",
		"tcp_info_rcv_ooopack", "tcp_info_snd_wnd", "tcp_info_rcv_wnd", "tcp_info_rehash", "tcp_info_total_rto",
		"tcp_info_total_rto_recoveries", "tcp_info_total_rto_time",
		"tcp_info_received_ce", "tcp_info_delivered_e1_bytes", "tcp_info_delivered_e0_bytes",
		"tcp_info_delivered_ce_bytes", "tcp_info_received_e1_bytes", "tcp_info_received_e0_bytes",
		"tcp_info_received_ce_bytes", "tcp_info_accecn_opt_seen",
		"sk_mem_info_rmem_alloc", "sk_mem_info_rcvbuf", "sk_mem_info_wmem_alloc", "sk_mem_info_sndbuf",
		"sk_mem_info_fwd_alloc", "sk_mem_info_wmem_queued", "sk_mem_info_optmem", "sk_mem_info_backlog",
		"sk_mem_info_drops",
		"vegas_info_enabled", "vegas_info_rttcnt", "vegas_info_rtt", "vegas_info_minrtt",
		"dctcp_info_enabled", "dctcp_info_ce_state", "dctcp_info_alpha", "dctcp_info_ab_ecn", "dctcp_info_ab_tot",
		"bbr_info_bw_lo", "bbr_info_bw_hi", "bbr_info_min_rtt", "bbr_info_pacing_gain", "bbr_info_cwnd_gain",
	}},
	{Ordinal, []string{
		"netns_inode", "nsid", "container_id", "socket_fd",
		"inet_diag_msg_socket_cookie", "inet_diag_msg_inode", "inet_diag_cgroup_id",
	}},
	{Address, []string{
		"inet_diag_msg_socket_source", "inet_diag_msg_socket_destination",
		"uplink1_lldp_mgmt_ip", "uplink2_lldp_mgmt_ip",
	}},
	{Port, []string{
		"inet_diag_msg_socket_source_port", "inet_diag_msg_socket_destination_port",
	}},
	// nsid_assigned says whether nsid holds an id: the kernel's first is 0,
	// and which namespace gets it changes from run to run.
	{Assigned, []string{"nsid_assigned"}},
	// Volatile, though not timestamps, because 0 is a real value that moves
	// between rows from run to run:
	//   - netlinker_id: the decoder that took the packet, 0 or 1 by chance;
	//   - inet_diag_msg_expires: a timer countdown, 0 as it fires
	//     (inet_diag_msg_timer says whether one is set);
	//   - tcp_info_busy_time: counted in jiffies (tcp_get_info_chrono_stats),
	//     so a send shorter than a tick reads 0 or one tick;
	//   - tcp_info_rcv_rtt: measured only once a receive window's worth of
	//     data arrives, which a connection with one small exchange may or
	//     may not have seen by the poll.
	{Volatile, []string{
		"daemon_version", "timestamp_ns", "event_date", "agent_id", "host_boot_id", "collection_id",
		"treatment_applied_timestamp_ns", "record_counter", "netlinker_id", "inet_diag_msg_expires",
		"tcp_info_busy_time", "tcp_info_rcv_rtt",
	}},
	{Host, []string{
		"hostname",
		"uplink1_ifname", "uplink1_nic_bus_info", "uplink1_nic_fw_version",
		"uplink1_lldp_chassis_name", "uplink1_lldp_chassis_id", "uplink1_lldp_port_id", "uplink1_lldp_port_descr",
		"uplink2_ifname", "uplink2_nic_bus_info", "uplink2_nic_fw_version",
		"uplink2_lldp_chassis_name", "uplink2_lldp_chassis_id", "uplink2_lldp_port_id", "uplink2_lldp_port_descr",
	}},
	{Netns, []string{"netns"}},
}
