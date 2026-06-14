package xtcp

import (
	"strings"
	"sync"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// newValidationFixture returns the minimum XTCP shape validateInput
// needs: a Marshallers sync.Map with one known marshaller and a config
// pointer. The destinations registry is module-global and already
// populated by destinations_{null,udp,unix,unixgram}.go init() funcs,
// so we don't touch it here.
func newValidationFixture(t *testing.T, c *xtcp_config.XtcpConfig) *XTCP {
	t.Helper()
	x := &XTCP{config: c}
	x.Marshallers.Store(MarshallerProtobufSingle, true)
	return x
}

func TestValidateInput_happyPaths(t *testing.T) {
	cases := []struct {
		name string
		cfg  *xtcp_config.XtcpConfig
	}{
		{
			name: "null dest skips dest parsing",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      schemeNull,
				Topic:     "xtcp",
			},
		},
		{
			name: "udp dest with host:port",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      "udp:127.0.0.1:13000",
				Topic:     "xtcp",
			},
		},
		{
			name: "unix dest with absolute path",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      "unix:/var/run/xtcp.sock",
				Topic:     "xtcp",
			},
		},
		{
			name: "unixgram dest with absolute path",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      "unixgram:/var/run/xtcp.sock",
				Topic:     "xtcp",
			},
		},
		{
			name: "unix dest tolerates colon in path",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      "unix:/var/run/weird:path.sock",
				Topic:     "xtcp",
			},
		},
		{
			name: "topic at boundary length=80",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      schemeNull,
				Topic:     strings.Repeat("a", 80),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newValidationFixture(t, tc.cfg)
			if err := x.validateInput(); err != nil {
				t.Fatalf("validateInput unexpected error: %v", err)
			}
		})
	}
}

func TestValidateInput_errorPaths(t *testing.T) {
	cases := []struct {
		name        string
		cfg         *xtcp_config.XtcpConfig
		marshallers []string // additional Marshallers entries (besides the default)
		wantSubstr  string
	}{
		{
			name: "unknown marshaller",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: "notReal",
				Dest:      schemeNull,
				Topic:     "xtcp",
			},
			wantSubstr: "XTCP Marshal must be one of",
		},
		{
			name: "dest missing colon",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      "udp",
				Topic:     "xtcp",
			},
			wantSubstr: "XTCP Dest must contain ':' chars",
		},
		{
			name: "udp dest with too few colons",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      "udp:127.0.0.1",
				Topic:     "xtcp",
			},
			wantSubstr: "must contain x2 ':' chars",
		},
		{
			name: "udp dest with too many colons",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      "udp:127.0.0.1:13000:extra",
				Topic:     "xtcp",
			},
			wantSubstr: "must contain x2 ':' chars",
		},
		{
			name: "unknown scheme",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      "carrier:pigeon:9000",
				Topic:     "xtcp",
			},
			wantSubstr: `unknown destination "carrier"`,
		},
		{
			name: "empty topic",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      schemeNull,
				Topic:     "",
			},
			wantSubstr: "XTCP Topic must not be length",
		},
		{
			name: "topic exceeds 80 chars",
			cfg: &xtcp_config.XtcpConfig{
				MarshalTo: MarshallerProtobufSingle,
				Dest:      schemeNull,
				Topic:     strings.Repeat("a", 81),
			},
			wantSubstr: "XTCP Topic must not be length",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newValidationFixture(t, tc.cfg)
			err := x.validateInput()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("err=%q, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}

// validateInput must be safe to call concurrently — Marshallers is
// sync.Map, the Dest scheme registry is read-only after init(), and
// the function itself doesn't write to x. Race-test verifies that.
func TestValidateInput_concurrent(t *testing.T) {
	cfg := &xtcp_config.XtcpConfig{
		MarshalTo: MarshallerProtobufSingle,
		Dest:      schemeNull,
		Topic:     "xtcp",
	}
	x := newValidationFixture(t, cfg)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := x.validateInput(); err != nil {
				t.Errorf("concurrent validateInput err: %v", err)
			}
		}()
	}
	wg.Wait()
}
