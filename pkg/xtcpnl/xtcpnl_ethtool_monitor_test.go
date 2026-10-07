package xtcpnl

import (
	"math"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEthtoolChannelsCommands(t *testing.T) {
	for _, row := range []struct {
		description           string
		command               uint8
		flags                 uint16
		kind                  EthtoolKind
		request, notification bool
	}{
		{"USER channels GET", 17, unix.NLM_F_REQUEST, EthtoolChannelsKind, true, false},
		{"USER channels SET stays decodable", 18, unix.NLM_F_REQUEST, EthtoolChannelsKind, true, false},
		{"KERNEL channels reply", 18, 0, EthtoolChannelsKind, false, false},
		{"KERNEL channels notification", 19, 0, EthtoolChannelsKind, false, true},
		{"KERNEL 17 remains rings notification", 17, 0, EthtoolRingsKind, false, true},
	} {
		t.Run(row.description, func(t *testing.T) {
			m, err := ParseEthtool(ethtoolTestBody(row.command), row.flags)
			if err != nil || m.Kind != row.kind || m.Request != row.request || m.Notification != row.notification {
				t.Fatalf("message=%+v error=%v", m, err)
			}
		})
	}
}

func TestEthtoolMonitorScalars(t *testing.T) {
	// Name/id/width are an explicit UAPI oracle, not derived from the decoder.
	for _, group := range []struct {
		name    string
		command byte
		fields  []struct {
			name  string
			id    uint16
			width int
		}
	}{
		{"rings", 16, []struct {
			name  string
			id    uint16
			width int
		}{
			{"RXBufLen", 10, 4}, {"TCPDataSplit", 11, 1}, {"CQESize", 12, 4},
			{"TXPush", 13, 1}, {"RXPush", 14, 1}, {"TXPushBufLen", 15, 4},
			{"TXPushBufLenMax", 16, 4}, {"HDSThresh", 17, 4}, {"HDSThreshMax", 18, 4},
		}},
		{"channels", 18, []struct {
			name  string
			id    uint16
			width int
		}{
			{"RXMax", 2, 4}, {"TXMax", 3, 4}, {"OtherMax", 4, 4}, {"CombinedMax", 5, 4},
			{"RX", 6, 4}, {"TX", 7, 4}, {"Other", 8, 4}, {"Combined", 9, 4},
		}},
	} {
		for _, field := range group.fields {
			t.Run(group.name+"/"+field.name, func(t *testing.T) {
				for _, row := range []struct {
					description                string
					payload                    []byte
					duplicate, absent, wantErr bool
				}{
					{"absent stays nil", nil, false, true, false},
					{"present zero stays present", make([]byte, field.width), false, false, false},
					{"maximum value retains width", ethtoolTestU32(math.MaxUint32)[:field.width], false, false, false},
					{"short scalar rejected", make([]byte, field.width-1), false, false, true},
					{"oversize scalar rejected", make([]byte, field.width+1), false, false, true},
					{"duplicate scalar rejected", make([]byte, field.width), true, false, true},
				} {
					t.Run(row.description, func(t *testing.T) {
						attr := ethtoolTestAttr(field.id, row.payload...)
						if row.duplicate {
							attr = append(attr, attr...)
						}
						if row.absent {
							attr = nil
						}
						m, err := ParseEthtool(ethtoolTestBody(group.command, attr), 0)
						if (err != nil) != row.wantErr {
							t.Fatalf("error=%v, want error=%v", err, row.wantErr)
						}
						if err != nil {
							return
						}
						v := reflect.ValueOf(m.Rings)
						if group.name == "channels" {
							v = reflect.ValueOf(m.Channels)
						}
						got := v.Elem().FieldByName(field.name)
						if got.IsNil() != row.absent {
							t.Fatal("lost presence")
						}
						if !row.absent {
							want := uint64(0)
							if row.payload[0] == 255 {
								want = 255
								if field.width == 4 {
									want = math.MaxUint32
								}
							}
							if got.Elem().Uint() != want {
								t.Fatalf("got=%d want=%d", got.Elem().Uint(), want)
							}
						}
					})
				}
			})
		}
	}
}

func TestEthtoolMonitorUnknownOwnership(t *testing.T) {
	for _, command := range []byte{16, 18} {
		body := ethtoolTestBody(command, ethtoolTestAttr(999, 1, 2, 3))
		m, err := ParseEthtool(body, 0)
		if err != nil {
			t.Fatal(err)
		}
		clear(body)
		if len(m.Attributes) != 1 || m.Attributes[0].Type != 999 || !reflect.DeepEqual(m.Attributes[0].Data, []byte{1, 2, 3}) {
			t.Fatalf("unknown attr lost/borrowed: %+v", m.Attributes)
		}
	}
}
