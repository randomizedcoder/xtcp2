package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"golang.org/x/sys/unix"
)

// KP0-T01: the inet devconf codec against the KC1 rows
// (docs/netlink/goip-link-kinds.md, "KP0-T01 The inet devconf codec"). The
// rows are in the 7.3.0 link-write set, which corpusAll skips, so they are
// read directly.

// inetRow is the path of KC1 row slug.
func inetRow(slug string) string {
	return tdLinkWrite_7_3_0 + "/rtnl/" + slug
}

// linkAfSpec returns the IFLA_AF_SPEC payload of an RTM_NEWLINK body, or nil.
func linkAfSpec(t testing.TB, body []byte) []byte {
	t.Helper()
	if len(body) < IfInfomsgSizeCst {
		t.Fatalf("RTM_NEWLINK body of %d bytes", len(body))
	}
	var spec []byte
	if err := WalkRTAttrs(body[IfInfomsgSizeCst:], func(atype uint16, v []byte) {
		if atype == unix.IFLA_AF_SPEC && spec == nil {
			spec = v
		}
	}); err != nil {
		t.Fatal(err)
	}
	return spec
}

// inetRequestAfSpec returns the IFLA_AF_SPEC of the RTM_NEWLINK request in
// row slug's ip.pcap.
func inetRequestAfSpec(t testing.TB, slug string) []byte {
	t.Helper()
	for _, m := range pcapNlMsgs(t, inetRow(slug)+"/ip.pcap") {
		if m.h.Type == unix.RTM_NEWLINK && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return linkAfSpec(t, m.body)
		}
	}
	t.Fatalf("%s: no RTM_NEWLINK request", slug)
	return nil
}

// inetDumpAfSpec returns the IFLA_AF_SPEC of the RTM_NEWLINK answer in row
// slug's `ip -d link show dev d0` readback. The pcap answers two requests:
// ip's name lookup first, whose 24-byte answer has no IFLA_AF_SPEC, then the
// show.
func inetDumpAfSpec(t testing.TB, slug string) []byte {
	t.Helper()
	for _, m := range pcapNlMsgs(t, inetRow(slug)+"/netlink_route_getlink_detail.pcap") {
		if m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if spec := linkAfSpec(t, m.body); spec != nil {
			return spec
		}
	}
	t.Fatalf("%s: no RTM_NEWLINK answer with IFLA_AF_SPEC", slug)
	return nil
}

// inetEvery is what inet_every_param sends: INET_EVERY in
// rtnl-write-corpus.exp, every param of InetDevconfParams in order, on/off
// ones on.
func inetEvery() []InetDevconfSetting {
	numeric := map[string]uint32{
		"rp_filter": 1, "tag": 7, "medium_id": 1, "force_igmp_version": 2,
		"arp_announce": 1, "arp_ignore": 1, "arp_accept": 1,
		"igmpv2_unsolicited_report_interval": 1000,
		"igmpv3_unsolicited_report_interval": 1000,
	}
	out := make([]InetDevconfSetting, 0, len(InetDevconfParams))
	for _, p := range InetDevconfParams {
		v := uint32(1)
		if !p.OnOff {
			v = numeric[p.Name]
		}
		out = append(out, InetDevconfSetting{ID: p.ID, Value: v})
	}
	return out
}

// inetAnswered are the KC1 rows the kernel answered (rc 0 or 2), with the
// settings each sent.
var inetAnswered = []struct {
	slug     string
	settings []InetDevconfSetting
}{
	{"inet_forwarding_on", []InetDevconfSetting{{1, 1}}},
	{"inet_rp_filter_max", []InetDevconfSetting{{8, 2}}},
	{"inet_rp_filter_over", []InetDevconfSetting{{8, 3}}},
	{"inet_tag_hex", []InetDevconfSetting{{12, 0x10}}},
	{"inet_arp_ignore_octal", []InetDevconfSetting{{19, 8}}},
	{"inet_medium_id_neg1", []InetDevconfSetting{{14, 0xffffffff}}},
	{"inet_medium_id_neg2", []InetDevconfSetting{{14, 0xfffffffe}}},
	{"inet_several", []InetDevconfSetting{{1, 1}, {3, 1}, {8, 1}, {19, 2}}},
	{"inet_every_param", inetEvery()},
	{"inet_repeated", []InetDevconfSetting{{1, 1}, {1, 0}}},
	{"inet_then_mtu", []InetDevconfSetting{{1, 1}}},
}

// u32 is v's little-endian bytes.
func u32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func TestInetDevconfParams(t *testing.T) {
	ids := map[uint16]bool{}
	onoff := 0
	for _, p := range InetDevconfParams {
		if p.ID == 0 || p.ID > InetDevconfMax || p.ID == 2 || ids[p.ID] {
			t.Errorf("%s: id %d is 0, too high, mc_forwarding or repeated", p.Name, p.ID)
		}
		ids[p.ID] = true
		if p.OnOff {
			onoff++
		}
	}
	if len(InetDevconfParams) != 32 || onoff != 23 {
		t.Errorf("%d params, %d on/off; want 32 and 23", len(InetDevconfParams), onoff)
	}
}

func TestInetDevconfEncode(t *testing.T) {
	for _, r := range inetAnswered {
		t.Run("positive: "+r.slug+"'s request", func(t *testing.T) {
			got, err := LinkAttrInetDevconf(r.settings)
			if err != nil {
				t.Fatal(err)
			}
			if want := inetRequestAfSpec(t, r.slug); got.Type != unix.IFLA_AF_SPEC || !bytes.Equal(got.Payload, want) {
				t.Errorf("got type %d % x, want % x", got.Type, got.Payload, want)
			}
		})
	}

	conf := func(attrs ...[]byte) []byte {
		return rtattr(unix.AF_INET, rtattr(unix.IFLA_INET_CONF|unix.NLA_F_NESTED, bytes.Join(attrs, nil)))
	}
	tests := []struct {
		description string
		settings    []InetDevconfSetting
		expected    []byte
		err         error
	}{
		{
			description: "positive: several params, in argument order",
			settings:    []InetDevconfSetting{{1, 1}, {3, 1}, {8, 1}, {19, 2}},
			expected:    conf(rtattr(1, u32(1)), rtattr(3, u32(1)), rtattr(8, u32(1)), rtattr(19, u32(2))),
		},
		{
			description: "negative: no settings",
			settings:    nil,
			err:         ErrInetDevconfEmpty,
		},
		{
			description: "negative: id 0",
			settings:    []InetDevconfSetting{{0, 1}},
			err:         ErrInetDevconfID,
		},
		{
			description: "boundary: the highest id",
			settings:    []InetDevconfSetting{{33, 1}},
			expected:    conf(rtattr(33, u32(1))),
		},
		{
			description: "boundary: one past it",
			settings:    []InetDevconfSetting{{34, 1}},
			err:         ErrInetDevconfID,
		},
		{
			description: "corner: the nest flag only on the innermost",
			settings:    []InetDevconfSetting{{1, 1}},
			expected:    []byte{16, 0, 2, 0, 12, 0, 0x01, 0x80, 8, 0, 1, 0, 1, 0, 0, 0},
		},
		{
			description: "corner: a repeated id is kept",
			settings:    []InetDevconfSetting{{1, 1}, {1, 0}},
			expected:    conf(rtattr(1, u32(1)), rtattr(1, u32(0))),
		},
		{
			description: "corner: medium_id -1",
			settings:    []InetDevconfSetting{{14, 0xffffffff}},
			expected:    conf(rtattr(14, []byte{0xff, 0xff, 0xff, 0xff})),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := LinkAttrInetDevconf(tt.settings)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got.Payload, tt.expected) {
				t.Errorf("got % x, want % x", got.Payload, tt.expected)
			}
		})
	}
}

func TestInetDevconfRequestParse(t *testing.T) {
	for _, r := range inetAnswered {
		t.Run("positive: "+r.slug+"'s request decodes to what was sent", func(t *testing.T) {
			got, err := ParseInetDevconfRequest(inetRequestAfSpec(t, r.slug))
			if err != nil || !slices.Equal(got, r.settings) {
				t.Errorf("got %v, %v; want %v", got, err, r.settings)
			}
		})
		t.Run("positive: "+r.slug+"'s settings round trip", func(t *testing.T) {
			a, err := LinkAttrInetDevconf(r.settings)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseInetDevconfRequest(a.Payload)
			if err != nil || !slices.Equal(got, r.settings) {
				t.Errorf("got %v, %v; want %v", got, err, r.settings)
			}
		})
	}

	inner := func(b []byte) []byte { return rtattr(unix.IFLA_INET_CONF|unix.NLA_F_NESTED, b) }
	tests := []struct {
		description string
		afSpec      []byte
		expected    []InetDevconfSetting
		err         error
	}{
		{
			description: "negative: a 3-byte inner attribute",
			afSpec:      rtattr(unix.AF_INET, inner(rtattr(1, []byte{1, 0, 0}))),
			err:         ErrInetDevconfShort,
		},
		{
			description: "negative: a truncated nest",
			afSpec:      rtattr(unix.AF_INET, inner(append(rtattr(1, u32(1)), rtattr(3, u32(1))...)))[:10],
			err:         ErrRTAttrSmall,
		},
		{
			description: "boundary: an AF_INET entry with an empty IFLA_INET_CONF",
			afSpec:      rtattr(unix.AF_INET, inner(nil)),
			expected:    nil,
		},
		{
			description: "corner: only an AF_INET6 entry",
			afSpec:      rtattr(unix.AF_INET6, inner(rtattr(1, u32(1)))),
			expected:    nil,
		},
		{
			description: "corner: AF_INET6 before AF_INET",
			afSpec:      append(rtattr(unix.AF_INET6, inner(rtattr(3, u32(1)))), rtattr(unix.AF_INET, inner(rtattr(1, u32(1))))...),
			expected:    []InetDevconfSetting{{1, 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := ParseInetDevconfRequest(tt.afSpec)
			if !errors.Is(err, tt.err) || !slices.Equal(got, tt.expected) {
				t.Errorf("got %v, %v; want %v, %v", got, err, tt.expected, tt.err)
			}
		})
	}
}

func TestInetDevconfValues(t *testing.T) {
	// value returns devconf id's value in row slug's readback.
	value := func(t *testing.T, slug string, id uint16) uint32 {
		t.Helper()
		v, ok, err := ParseInetDevconfValues(inetDumpAfSpec(t, slug))
		if err != nil || !ok || int(id) > len(v) {
			t.Fatalf("%s: %d values, %v, %v", slug, len(v), ok, err)
		}
		return v[id-1]
	}

	t.Run("positive: every devconf id reads back what inet_every_param sent", func(t *testing.T) {
		want := make([]uint32, InetDevconfMax)
		for _, s := range inetEvery() {
			want[s.ID-1] = s.Value
		}
		got, ok, err := ParseInetDevconfValues(inetDumpAfSpec(t, "inet_every_param"))
		if err != nil || !ok || !slices.Equal(got, want) {
			t.Errorf("got %v, %v, %v; want %v", got, ok, err, want)
		}
	})
	t.Run("positive: the last of a repeated param wins", func(t *testing.T) {
		if got := value(t, "inet_repeated", 1); got != 0 {
			t.Errorf("forwarding %d, want 0", got)
		}
	})
	t.Run("negative: a refused row changes nothing", func(t *testing.T) {
		got, want := value(t, "inet_rp_filter_over", 8), value(t, "inet_forwarding_on", 8)
		if got != want {
			t.Errorf("rp_filter %d, want the default %d", got, want)
		}
	})
	t.Run("corner: medium_id -1 reads back as 0xffffffff", func(t *testing.T) {
		if got := value(t, "inet_medium_id_neg1", 14); got != 0xffffffff {
			t.Errorf("medium_id %#x, want 0xffffffff", got)
		}
	})
	t.Run("corner: an AF_INET6 entry beside it", func(t *testing.T) {
		spec := inetDumpAfSpec(t, "inet_every_param")
		families := 0
		if err := WalkRTAttrs(spec, func(family uint16, _ []byte) {
			if family == unix.AF_INET6 {
				families++
			}
		}); err != nil || families != 1 {
			t.Fatalf("the readback has %d AF_INET6 entries (%v); the row is meant to carry one", families, err)
		}
		got, _, err := ParseInetDevconfValues(spec)
		if err != nil || len(got) != int(InetDevconfMax) {
			t.Errorf("%d values, %v; want %d, the AF_INET array only", len(got), err, InetDevconfMax)
		}
	})

	// seq is the values 1 to n.
	seq := func(n uint32) []uint32 {
		var out []uint32
		for v := uint32(1); v <= n; v++ {
			out = append(out, v)
		}
		return out
	}
	// array is a dump's AF_INET entry holding values.
	array := func(values []uint32) []byte {
		var b []byte
		for _, v := range values {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		return rtattr(unix.AF_INET, rtattr(unix.IFLA_INET_CONF, b))
	}
	tests := []struct {
		description string
		afSpec      []byte
		expected    []uint32
		ok          bool
		err         error
	}{
		{
			description: "negative: a 131-byte array",
			afSpec:      rtattr(unix.AF_INET, rtattr(unix.IFLA_INET_CONF, make([]byte, 131))),
			err:         ErrInetDevconfShort,
		},
		{
			description: "boundary: no IFLA_AF_SPEC",
			afSpec:      nil,
			ok:          false,
		},
		{
			description: "boundary: an array of 34",
			afSpec:      array(seq(34)),
			expected:    seq(34),
			ok:          true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, ok, err := ParseInetDevconfValues(tt.afSpec)
			if !errors.Is(err, tt.err) || ok != tt.ok || !slices.Equal(got, tt.expected) {
				t.Errorf("got %v, %v, %v; want %v, %v, %v", got, ok, err, tt.expected, tt.ok, tt.err)
			}
		})
	}
}

func BenchmarkParseInetDevconf(b *testing.B) {
	request := inetRequestAfSpec(b, "inet_every_param")
	dump := inetDumpAfSpec(b, "inet_every_param")
	b.Run("request", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := ParseInetDevconfRequest(request); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("values", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := ParseInetDevconfValues(dump); err != nil {
				b.Fatal(err)
			}
		}
	})
}
