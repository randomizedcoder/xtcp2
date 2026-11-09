package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestRuleWriteRealms is iprule_modify's realms arm (ip/iprule.c:1050):
// get_rt_realms_or_raw, the parse route realms uses, into FRA_FLOW u32 where
// the loop reads it, with no duparg. GW4-T01 in goip-write-keywords.md.
//
// go test ./internal/goip/ -run TestRuleWriteRealms
func TestRuleWriteRealms(t *testing.T) {
	flow := func(v uint32) []byte { return fraOf(unix.FRA_FLOW, binary.LittleEndian.AppendUint32(nil, v)...) }
	tests := []struct {
		description string
		args        string
		attrs       []byte
		errMsg      string
	}{
		{description: "positive: a TO realm", args: "realms 10", attrs: flow(10)},
		{description: "positive: FROM/TO", args: "realms 1/2", attrs: flow(0x00010002)},
		{description: "positive: the stock name", args: "realms cosmos", attrs: flow(0)},
		{description: "positive: raw past a half", args: "realms 0x10000", attrs: flow(65536)},
		{description: "positive: the attribute's bytes", args: "realms 10", attrs: []byte{8, 0, 11, 0, 10, 0, 0, 0}},
		{description: "positive: in word order", args: "pref 100 realms 10 lookup main",
			attrs: append(fraOf(unix.FRA_PRIORITY, binary.LittleEndian.AppendUint32(nil, 100)...), flow(10)...)},
		{description: "negative: a word", args: "realms x", errMsg: `Error: argument "x" is wrong: invalid realms`},
		{description: "negative: no value", args: "realms", errMsg: "Command line is not complete"},
		{description: "negative: an unknown name half", args: "realms 1/x", errMsg: `Error: argument "1/x" is wrong: invalid realms`},
		{description: "boundary: both halves at 255", args: "realms 255/255", attrs: flow(0x00ff00ff)},
		{description: "boundary: raw at UINT_MAX", args: "realms 4294967295", attrs: flow(0xffffffff)},
		{description: "boundary: raw one past", args: "realms 4294967296", errMsg: "invalid realms"},
		{description: "corner: r is realms", args: "r 10", attrs: flow(10)},
		{description: "corner: twice is two attributes", args: "realms 1 realms 2", attrs: append(flow(1), flow(2)...)},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			w, err := parseRuleWriteArgs(unix.AF_INET, unix.RTM_NEWRULE, strings.Fields(tc.args))
			if tc.errMsg != "" {
				if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), tc.errMsg) {
					t.Fatalf("err = %v, want ErrInvalidArgument containing %q", err, tc.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := w.attrs.Bytes(); !bytes.Equal(got, tc.attrs) {
				t.Errorf("attrs % x\nwant  % x", got, tc.attrs)
			}
		})
	}
}
