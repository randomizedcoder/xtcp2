package xtcp

import (
	"fmt"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// InputValidation wraps validateInput in a log.Fatalf-on-error envelope.
// Tests substitute x.fatalf with a capture so we can hit both branches
// (happy path → no call; error path → captured args).

func TestInputValidation_happy(t *testing.T) {
	x := newValidationFixture(t, &xtcp_config.XtcpConfig{
		Topic: "x", Dest: schemeNull, MarshalTo: MarshallerProtoJSON,
	})
	called := false
	x.fatalf = func(string, ...any) { called = true }
	x.InputValidation()
	if called {
		t.Error("fatalf should not be called on happy path")
	}
}

func TestInputValidation_errorTriggersFatalf(t *testing.T) {
	x := newValidationFixture(t, &xtcp_config.XtcpConfig{
		Topic: "x", Dest: schemeNull, MarshalTo: "no-such-marshaller",
	})
	var captured string
	x.fatalf = func(format string, args ...any) {
		captured = fmt.Sprintf(format, args...)
	}
	x.InputValidation()
	if captured == "" {
		t.Error("fatalf should have been called with a message")
	}
}
