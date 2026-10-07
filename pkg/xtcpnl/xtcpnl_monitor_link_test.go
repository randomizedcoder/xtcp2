package xtcpnl

import (
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func monitorTestLink(attrs ...[]byte) []byte {
	body := make([]byte, 16)
	binary.LittleEndian.PutUint32(body[4:], 7)
	for _, a := range attrs {
		body = append(body, a...)
	}
	return body
}

func TestMonitorLinkValidation(t *testing.T) {
	name := ethtoolTestAttr(unix.IFLA_IFNAME, 'e', '0', 0)
	carrier := ethtoolTestAttr(unix.IFLA_CARRIER, 0)
	stats := ethtoolTestAttr(unix.IFLA_STATS64, make([]byte, 23*8)...)
	for _, row := range []struct {
		description string
		body        []byte
		required    MonitorLinkRequirements
		wantErr     bool
	}{
		{"inventory with oldest complete stats accepted", monitorTestLink(name, stats), MonitorLinkRequirements{true, true}, false},
		{"deletion can omit name and counters", monitorTestLink(), MonitorLinkRequirements{}, false},
		{"inventory cannot omit name", monitorTestLink(), MonitorLinkRequirements{Name: true}, true},
		{"traffic cannot omit direct counters", monitorTestLink(name), MonitorLinkRequirements{Stats: true}, true},
		{"short header rejected", make([]byte, 15), MonitorLinkRequirements{}, true},
		{"zero index rejected", make([]byte, 16), MonitorLinkRequirements{}, true},
		{"duplicate identity rejected", monitorTestLink(name, name), MonitorLinkRequirements{}, true},
		{"unterminated name rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_IFNAME, 'e', '0')), MonitorLinkRequirements{}, true},
		{"interior NUL rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_IFNAME, 'e', 0, '0', 0)), MonitorLinkRequirements{}, true},
		{"invalid kernel name rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_IFNAME, '.', 0)), MonitorLinkRequirements{}, true},
		{"absent carrier allowed", monitorTestLink(name), MonitorLinkRequirements{}, false},
		{"present zero carrier allowed", monitorTestLink(name, carrier), MonitorLinkRequirements{}, false},
		{"invalid carrier rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_CARRIER, 2)), MonitorLinkRequirements{}, true},
		{"duplicate carrier rejected", monitorTestLink(carrier, carrier), MonitorLinkRequirements{}, true},
		{"short carrier rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_CARRIER)), MonitorLinkRequirements{}, true},
		{"long carrier rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_CARRIER, 0, 0)), MonitorLinkRequirements{}, true},
		{"network endian scalar rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_MTU|unix.NLA_F_NET_BYTEORDER, 0, 0, 0, 0)), MonitorLinkRequirements{}, true},
		{"nested scalar rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_MTU|unix.NLA_F_NESTED, 0, 0, 0, 0)), MonitorLinkRequirements{}, true},
		{"future operstate retained for policy", monitorTestLink(ethtoolTestAttr(unix.IFLA_OPERSTATE, 255)), MonitorLinkRequirements{}, false},
		{"empty stats rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_STATS64)), MonitorLinkRequirements{}, true},
		{"partial counter rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_STATS64, make([]byte, 23*8+1)...)), MonitorLinkRequirements{}, true},
		{"future whole counters accepted", monitorTestLink(ethtoolTestAttr(unix.IFLA_STATS64, make([]byte, 26*8)...)), MonitorLinkRequirements{Stats: true}, false},
		{"short attr tail rejected", append(monitorTestLink(name), 1), MonitorLinkRequirements{}, true},
		{"unknown repeated attrs tolerated", monitorTestLink(ethtoolTestAttr(999, 1), ethtoolTestAttr(999, 2)), MonitorLinkRequirements{}, false},
		{"malformed nested kind rejected", monitorTestLink(ethtoolTestAttr(unix.IFLA_LINKINFO, 1)), MonitorLinkRequirements{}, true},
	} {
		t.Run(row.description, func(t *testing.T) {
			got, err := ParseMonitorLink(row.body, row.required)
			if (err != nil) != row.wantErr || (err != nil && !errors.Is(err, ErrMonitorLink)) {
				t.Fatalf("error=%v, want error=%v", err, row.wantErr)
			}
			if !row.wantErr {
				want, parseErr := ParseNewLink(row.body)
				if parseErr != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("strict and tolerant outputs differ: %v", parseErr)
				}
			}
		})
	}
}

func TestMonitorLinkCounters(t *testing.T) {
	for _, row := range []struct {
		description  string
		typ          uint16
		width, count int
		wantFields   uint8
		is64         bool
	}{
		{"old 64-bit prefix", unix.IFLA_STATS64, 8, 23, 23, true},
		{"all 64-bit fields", unix.IFLA_STATS64, 8, 25, 25, true},
		{"future 64-bit suffix ignored", unix.IFLA_STATS64, 8, 26, 25, true},
		{"old 32-bit prefix", unix.IFLA_STATS, 4, 23, 23, false},
		{"all 32-bit fields exclude otherhost", unix.IFLA_STATS, 4, 24, 24, false},
		{"future 32-bit suffix ignored", unix.IFLA_STATS, 4, 25, 24, false},
	} {
		t.Run(row.description, func(t *testing.T) {
			payload := make([]byte, row.width*row.count)
			for i := range payload {
				payload[i] = 255
			}
			body := monitorTestLink(ethtoolTestAttr(row.typ, payload...),
				ethtoolTestAttr(unix.IFLA_CARRIER_CHANGES, ethtoolTestU32(0)...),
				ethtoolTestAttr(unix.IFLA_CARRIER_UP_COUNT, ethtoolTestU32(math.MaxUint32)...))
			m, err := ParseMonitorLink(body, MonitorLinkRequirements{Stats: true})
			if err != nil {
				t.Fatal(err)
			}
			want := uint64(math.MaxUint32)
			if row.is64 {
				want = math.MaxUint64
			}
			if m.Stats == nil || m.Stats.RxPackets != want || m.StatsFields != row.wantFields || m.StatsIs64 != row.is64 {
				t.Fatalf("stats=%+v fields=%d is64=%v", m.Stats, m.StatsFields, m.StatsIs64)
			}
			if m.CarrierChanges != (U32Attr{0, true}) || m.CarrierUpCount != (U32Attr{math.MaxUint32, true}) || m.CarrierDownCount.Present {
				t.Fatalf("carrier presence lost: %+v", m)
			}
			clear(body)
			if m.Stats.RxPackets != want {
				t.Fatal("stats alias input")
			}
		})
	}
}

func TestMonitorLinkTolerantCompatibility(t *testing.T) {
	for _, row := range []struct {
		description string
		attrs       [][]byte
		wantFields  uint8
		wantCounter uint64
	}{
		{"64-bit selection wins over 32-bit", [][]byte{ethtoolTestAttr(unix.IFLA_STATS, ethtoolTestU32(9)...), ethtoolTestAttr(unix.IFLA_STATS64, 1, 0, 0, 0, 0, 0, 0, 0)}, 1, 1},
		{"partial scalar remains absent and zero", [][]byte{ethtoolTestAttr(unix.IFLA_STATS64, 1)}, 0, 0},
		{"first duplicate stats wins", [][]byte{ethtoolTestAttr(unix.IFLA_STATS, ethtoolTestU32(9)...), ethtoolTestAttr(unix.IFLA_STATS, ethtoolTestU32(1)...)}, 1, 9},
	} {
		t.Run(row.description, func(t *testing.T) {
			m, err := ParseNewLink(monitorTestLink(row.attrs...))
			if err != nil || m.Stats == nil || m.Stats.RxPackets != row.wantCounter || m.StatsFields != row.wantFields {
				t.Fatalf("stats=%+v fields=%d error=%v", m.Stats, m.StatsFields, err)
			}
			if _, err := ParseMonitorLink(monitorTestLink(row.attrs...), MonitorLinkRequirements{}); !errors.Is(err, ErrMonitorLink) {
				t.Fatalf("strict accepted short/duplicate counters: %v", err)
			}
		})
	}
	for _, typ := range []uint16{unix.IFLA_CARRIER_CHANGES, unix.IFLA_CARRIER_UP_COUNT, unix.IFLA_CARRIER_DOWN_COUNT} {
		body := monitorTestLink(ethtoolTestAttr(typ, 1), ethtoolTestAttr(typ, ethtoolTestU32(9)...))
		m, err := ParseNewLink(body)
		if err != nil || m.CarrierChanges.Present || m.CarrierUpCount.Present || m.CarrierDownCount.Present {
			t.Fatalf("first short attr no longer wins: %+v %v", m, err)
		}
		if _, err := ParseMonitorLink(body, MonitorLinkRequirements{}); err == nil {
			t.Fatal("strict accepted short/duplicate carrier")
		}
	}
}

func FuzzParseMonitorLink(f *testing.F) {
	f.Add(monitorTestLink(ethtoolTestAttr(unix.IFLA_IFNAME, 'e', '0', 0)))
	f.Add(monitorTestLink(ethtoolTestAttr(unix.IFLA_STATS64, make([]byte, 200)...)))
	f.Fuzz(func(t *testing.T, body []byte) {
		got, err := ParseMonitorLink(body, MonitorLinkRequirements{})
		if err == nil {
			want, parseErr := ParseNewLink(body)
			if parseErr != nil || !reflect.DeepEqual(got, want) || got.Index <= 0 || got.StatsFields > 25 {
				t.Fatal("strict acceptance violates tolerant/presence contract")
			}
		}
	})
}

func TestMonitorLinkNestsAndWidths(t *testing.T) {
	kind := ethtoolTestAttr(unix.IFLA_INFO_KIND, 'v', 'e', 't', 'h', 0)
	for _, row := range []struct {
		description string
		attrs       []byte
		wantErr     bool
	}{
		{"kind without historical nested flag", ethtoolTestAttr(unix.IFLA_LINKINFO, kind...), false},
		{"kind with nested flag", ethtoolTestAttr(unix.IFLA_LINKINFO|unix.NLA_F_NESTED, kind...), false},
		{"duplicate kind", ethtoolTestAttr(unix.IFLA_LINKINFO, ethtoolTestJoin(kind, kind)...), true},
		{"unterminated kind", ethtoolTestAttr(unix.IFLA_LINKINFO, ethtoolTestAttr(unix.IFLA_INFO_KIND, 'x')...), true},
		{"empty kind", ethtoolTestAttr(unix.IFLA_LINKINFO, ethtoolTestAttr(unix.IFLA_INFO_KIND, 0)...), true},
		{"unknown nested data retained", ethtoolTestAttr(unix.IFLA_LINKINFO, ethtoolTestJoin(kind, ethtoolTestAttr(99, 1))...), false},
		{"truncated u32", ethtoolTestAttr(unix.IFLA_CARRIER_DOWN_COUNT, 0, 0, 0), true},
		{"oversized u32", ethtoolTestAttr(unix.IFLA_CARRIER_UP_COUNT, 0, 0, 0, 0, 0), true},
		{"native IB address", ethtoolTestAttr(unix.IFLA_ADDRESS, make([]byte, 20)...), false},
	} {
		t.Run(row.description, func(t *testing.T) {
			_, err := ParseMonitorLink(monitorTestLink(row.attrs), MonitorLinkRequirements{})
			if (err != nil) != row.wantErr {
				t.Fatalf("error=%v, want error=%v", err, row.wantErr)
			}
		})
	}
}

func TestMonitorLinkKernelFixtures(t *testing.T) {
	for _, kernel := range []string{"6_8_12", "7_1_4"} {
		for _, scenario := range []string{"route-lifecycle", "route-move-source", "route-move-target"} {
			t.Run(kernel+"/"+scenario, func(t *testing.T) {
				t.Log("existing capture: strict NEW/DEL decode equals tolerant result, with observed counter presence")
				links := 0
				for _, packet := range linkCaptureBundle(t, kernel, scenario) {
					if err := WalkNetlinkEnvelopes(packet.wire, nil); err != nil {
						t.Fatal(err)
					}
					if packet.protocol != unix.NETLINK_ROUTE || packet.header.Flags&unix.NLM_F_REQUEST != 0 || (packet.header.Type != unix.RTM_NEWLINK && packet.header.Type != unix.RTM_DELLINK) {
						continue
					}
					links++
					got, err := ParseMonitorLink(packet.body, MonitorLinkRequirements{Name: true})
					if err != nil {
						t.Fatal(err)
					}
					want, err := ParseNewLink(packet.body)
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("strict result changed: %v", err)
					}
				}
				if links == 0 {
					t.Fatal("no link messages tested")
				}
			})
		}
	}
}
