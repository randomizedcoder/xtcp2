//go:build goip_write

package goip

import (
	"bytes"
	"io"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestSrWriteAttrs checks the SETHMAC and SET_TUNSRC attributes for what
// the captures do not hold: SECRETLEN as a u8, and a secret past
// SEG6_REQUEST's buffer, which addattr_l refuses and ip sends without.
//
// go test ./internal/goip/ -run TestSrWriteAttrs
func TestSrWriteAttrs(t *testing.T) {
	tests := []struct {
		description string
		req         srReq
		secretLen   uint8
		secret      int // the SECRET attribute's length, -1 when absent
		stderr      string
	}{
		{description: "positive: a six-byte secret", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2, pass: []byte("s3cret")},
			secretLen: 6, secret: 6},
		{description: "positive: a blank secret has no SECRET", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2},
			secretLen: 0, secret: -1},
		{description: "boundary: 976 bytes, the most the buffer holds", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2,
			pass: bytes.Repeat([]byte{'x'}, 976)}, secretLen: 976 % 256, secret: 976},
		{description: "boundary: 977 bytes is left out, with addattr_l's message", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2,
			pass: bytes.Repeat([]byte{'x'}, 977)}, secretLen: 977 % 256, secret: -1,
			stderr: "addattr_l ERROR: message exceeded bound of 1024\n"},
		{description: "corner: 256 bytes is a SECRETLEN of 0", req: srReq{cmd: xtcpnl.Seg6CmdSetHmac, keyID: 1, algID: 2,
			pass: bytes.Repeat([]byte{'x'}, 256)}, secretLen: 0, secret: 256},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var stderr bytes.Buffer
			c := newRunCtx(nil, io.Discard, &stderr)
			attrs, err := srWriteAttrs(c, &tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if got := stderr.String(); got != tc.stderr {
				t.Errorf("stderr %q, want %q", got, tc.stderr)
			}
			if n := len(attrs) + srReqHdrCst; n > srReqBufCst {
				t.Errorf("request %d bytes, past %d", n, srReqBufCst)
			}
			m, err := xtcpnl.ParseSeg6(append([]byte{tc.req.cmd, xtcpnl.Seg6GenlVersion, 0, 0}, attrs...))
			if err != nil {
				t.Fatal(err)
			}
			if m.SecretLen == nil || *m.SecretLen != tc.secretLen {
				t.Errorf("SECRETLEN %v, want %d", m.SecretLen, tc.secretLen)
			}
			got := -1
			if m.Secret != nil {
				got = len(m.Secret)
			}
			if got != tc.secret {
				t.Errorf("SECRET %d bytes, want %d", got, tc.secret)
			}
		})
	}
}
