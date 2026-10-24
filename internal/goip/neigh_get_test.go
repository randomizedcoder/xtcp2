package goip

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
)

// TestParseNeighGetArgs is ipneigh_get's argument loop and the check after
// it: which arm each word takes, and the errors it stops on before any
// request is sent.
//
// go test ./internal/goip/ -run TestParseNeighGetArgs
func TestParseNeighGetArgs(t *testing.T) {
	v4 := func(s string) inetPrefix {
		a := netip.MustParseAddr(s).As4()
		return inetPrefix{family: unix.AF_INET, data: a[:], bitlen: -1}
	}
	v6 := func(s string) inetPrefix {
		a := netip.MustParseAddr(s).As16()
		return inetPrefix{family: unix.AF_INET6, data: a[:], bitlen: -1}
	}
	tests := []struct {
		description string
		family      uint8
		args        string
		expected    neighGetState
		wantErr     error
		wantErrText string
	}{
		{
			description: "positive: an address and a device, in either order",
			args:        "192.0.2.2 dev d0",
			expected:    neighGetState{dev: "d0", devSet: true, devOK: true, dst: v4("192.0.2.2"), dstOK: true},
		},
		{
			description: "positive: `dev` first and `to` before the address",
			args:        "dev d0 to 192.0.2.2",
			expected:    neighGetState{dev: "d0", devSet: true, devOK: true, dst: v4("192.0.2.2"), dstOK: true},
		},
		{
			description: "positive: an IPv6 address takes AF_INET6",
			args:        "2001:db8::2 dev d0",
			expected:    neighGetState{dev: "d0", devSet: true, devOK: true, dst: v6("2001:db8::2"), dstOK: true},
		},
		{
			description: "positive: `proxy` takes the address and stands for the device",
			args:        "proxy 192.0.2.2",
			expected:    neighGetState{devOK: true, dst: v4("192.0.2.2"), dstOK: true, ndmFlg: unix.NTF_PROXY},
		},
		{
			description: "positive: `p` is proxy, which is matches()",
			args:        "p 192.0.2.2 dev d0",
			expected: neighGetState{dev: "d0", devSet: true, devOK: true, dst: v4("192.0.2.2"), dstOK: true,
				ndmFlg: unix.NTF_PROXY},
		},
		{
			description: "boundary: a second `dev` is no duparg; the last one wins",
			args:        "192.0.2.2 dev d0 dev d1",
			expected:    neighGetState{dev: "d1", devSet: true, devOK: true, dst: v4("192.0.2.2"), dstOK: true},
		},
		{
			description: "boundary: `dev` as the last word has no value; the command line is not complete",
			args:        "192.0.2.2 dev",
			wantErr:     ErrInvalidArgument,
			wantErrText: "Command line is not complete",
		},
		{
			description: "negative: an address without a device",
			args:        "192.0.2.2",
			wantErr:     ErrInvalidArgument,
			wantErrText: "Device and address are required arguments",
		},
		{
			description: "negative: a device without an address",
			args:        "dev d0",
			wantErr:     ErrInvalidArgument,
			wantErrText: "Device and address are required arguments",
		},
		{
			description: "negative: `default` parses with no family, which is no address",
			args:        "default dev d0",
			wantErr:     ErrInvalidArgument,
			wantErrText: "Device and address are required arguments",
		},
		{
			description: "negative: a second address is duparg2 on `to`",
			args:        "192.0.2.2 192.0.2.3 dev d0",
			wantErr:     ErrInvalidArgument,
			wantErrText: `either "to" is duplicate`,
		},
		{
			description: "negative: `proxy` after an address is duparg on `address`",
			args:        "192.0.2.2 proxy 192.0.2.3",
			wantErr:     ErrInvalidArgument,
			wantErrText: `duplicate "address"`,
		},
		{
			description: "negative: -6 refuses an IPv4 address",
			family:      unix.AF_INET6,
			args:        "192.0.2.2 dev d0",
			wantErr:     ErrInvalidArgument,
			wantErrText: "address is expected",
		},
		{
			description: "corner: `d0` is not `dev`, which is strcmp, so it is read as an address",
			args:        "d0 192.0.2.2",
			wantErr:     ErrInvalidArgument,
			wantErrText: "address is expected",
		},
		{
			description: "corner: `help` in the address's place is refused as neigh help",
			args:        "help",
			wantErr:     ErrNotImplemented,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseNeighGetArgs(tc.family, strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.wantErrText) {
					t.Errorf("err = %q, want it to contain %q", err, tc.wantErrText)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("state =\n  %+v\nwant\n  %+v", got, tc.expected)
			}
		})
	}
}

// TestNeighGetMatchesIp replays each `neigh get` capture through neighGet and
// compares what goip sends and prints with what ip sent and printed: the get
// by name and the get, byte for byte and in order, and stdout with ip.out.
//
// go test ./internal/goip/ -run TestNeighGetMatchesIp
func TestNeighGetMatchesIp(t *testing.T) {
	const (
		getlink  = 18 // RTM_GETLINK
		getneigh = 30 // RTM_GETNEIGH
	)
	tests := []struct {
		description string
		slug        string
		args        string
		expected    []uint16 // request types, in order
		wantErr     error
	}{
		{"positive: `neigh get 192.0.2.2 dev d0`, a permanent entry, its device cached by the lookup",
			"neigh_get", "192.0.2.2 dev d0", []uint16{getlink, getneigh}, nil},
		{"negative: a missing entry is the kernel's ENOENT, exit 2 and nothing on stdout",
			"neigh_get_missing", "192.0.2.9 dev d0", []uint16{getlink, getneigh}, unix.ENOENT},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := routeGetCorpusDir + "/" + tc.slug
			pcap := dir + "/ip.pcap"
			src := routeGetReplay{newStatsReplay(t, pcap)}
			var out bytes.Buffer
			c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard}
			err := neighGet(c, strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("neighGet = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && exitCodeOf(err) != ExitFailure {
				t.Errorf("exit %d, want %d (ip.rc)", exitCodeOf(err), ExitFailure)
			}
			want, rerr := os.ReadFile(dir + "/ip.out")
			if rerr != nil {
				t.Fatal(rerr)
			}
			if out.String() != string(want) {
				t.Errorf("stdout differs from ip.out\n got %q\nwant %q", out.String(), want)
			}
			capt, perr := nlparity.ParseRouteCaptureFile(pcap)
			if perr != nil {
				t.Fatal(perr)
			}
			var ip [][]byte
			for _, m := range capt.Requests() {
				ip = append(ip, capturedRequest(m))
			}
			if len(src.sent) != len(tc.expected) || len(ip) != len(tc.expected) {
				t.Fatalf("goip sent %d requests, ip %d; want %d each", len(src.sent), len(ip), len(tc.expected))
			}
			for i := range ip {
				if typ := requestTypeOf(src.sent[i]); typ != tc.expected[i] {
					t.Errorf("request %d type %d, want %d", i+1, typ, tc.expected[i])
				}
				if !bytes.Equal(src.sent[i], ip[i]) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], ip[i])
				}
			}
		})
	}

	t.Run("negative: a device the kernel does not know stops after the get by name", func(t *testing.T) {
		src := routeGetReplay{newStatsReplay(t, routeGetCorpusDir+"/neigh_get/ip.pcap")}
		c := &runCtx{src: src, lltab: NewLLTab(), out: io.Discard, errOut: io.Discard}
		err := neighGet(c, []string{"192.0.2.2", "dev", "nosuch0"})
		if err == nil {
			t.Fatal("neighGet(nosuch0) succeeded")
		}
		if len(src.sent) != 1 || requestTypeOf(src.sent[0]) != getlink {
			t.Errorf("sent %d requests; want only the get by name", len(src.sent))
		}
	})

	t.Run("corner: `neigh g` reaches get through runNeigh", func(t *testing.T) {
		src := routeGetReplay{newStatsReplay(t, routeGetCorpusDir+"/neigh_get/ip.pcap")}
		var out bytes.Buffer
		c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard}
		if err := runNeigh(c, []string{"g", "192.0.2.2", "dev", "d0"}); err != nil {
			t.Fatalf("runNeigh: %v", err)
		}
		if !strings.HasPrefix(out.String(), "192.0.2.2 dev d0 ") {
			t.Errorf("stdout = %q", out.String())
		}
	})
}
