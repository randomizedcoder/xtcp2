package render

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/randomizedcoder/xtcp2/internal/jsonw"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// srSecretMaxCst is print_dumphmac's cap: a 64-byte buffer, its last byte
// the terminating NUL.
const srSecretMaxCst = 63

// SrHmacView is one key of `ip sr hmac show`, print_dumphmac
// (ip/ipseg6.c): the line, and in JSON the object.
type SrHmacView struct {
	keyID  uint32
	algo   string
	secret []byte
	// Truncated is print_dumphmac's stderr line when SECRETLEN is over 63,
	// empty otherwise. It is printed before the key, -j or not.
	Truncated string
}

// SrHmacViewOf is print_dumphmac over a decoded DUMPHMAC reply. An absent
// key id, length or algorithm reads as 0, where rta_getattr_* would read
// through a NULL attribute. The secret is SECRETLEN bytes, at most 63, of
// SECRET, as memcpy copies them, cut at the first NUL as %s prints them;
// where SECRET is shorter than SECRETLEN, only its own bytes are read.
func SrHmacViewOf(m *xtcpnl.Seg6) SrHmacView {
	var v SrHmacView
	if m.HmacKeyID != nil {
		v.keyID = *m.HmacKeyID
	}
	var slen int
	if m.SecretLen != nil {
		slen = int(*m.SecretLen)
	}
	if slen > srSecretMaxCst {
		v.Truncated = fmt.Sprintf("HMAC secret length %d > %d, truncated\n", slen, srSecretMaxCst)
		slen = srSecretMaxCst
	}
	secret := m.Secret
	if len(secret) > slen {
		secret = secret[:slen]
	}
	if i := bytes.IndexByte(secret, 0); i >= 0 {
		secret = secret[:i]
	}
	v.secret = secret
	var alg uint8
	if m.AlgID != nil {
		alg = *m.AlgID
	}
	switch alg {
	case xtcpnl.Seg6HmacAlgoSHA1:
		v.algo = "sha1"
	case xtcpnl.Seg6HmacAlgoSHA256:
		v.algo = "sha256"
	default:
		v.algo = unknownNameCst
	}
	return v
}

// Text is the line print_dumphmac prints.
func (v *SrHmacView) Text() string {
	return fmt.Sprintf("hmac %d algo %s secret \"%s\"\n", v.keyID, v.algo, v.secret)
}

// JSON is the key's object, in the line's order.
func (v *SrHmacView) JSON() []byte {
	b := []byte(`{"hmac":`)
	b = strconv.AppendUint(b, uint64(v.keyID), 10)
	b = append(b, `,"algo":`...)
	b = jsonw.Append(b, v.algo)
	b = append(b, `,"secret":`...)
	b = jsonw.Append(b, v.secret)
	return append(b, '}')
}

// SrTunsrcText is print_tunsrc's line: SEG6_ATTR_DST through rt_addr_n2a,
// inet_ntop's spelling, and all zeros when the reply has none.
func SrTunsrcText(m *xtcpnl.Seg6) string {
	return "tunsrc addr " + srTunsrc(m) + "\n"
}

// SrTunsrcJSON is print_tunsrc's object.
func SrTunsrcJSON(m *xtcpnl.Seg6) []byte {
	b := append([]byte(`{"tunsrc":`), jsonw.Append(nil, srTunsrc(m))...)
	return append(b, '}')
}

// srTunsrc is the source's text.
func srTunsrc(m *xtcpnl.Seg6) string {
	var a [16]byte
	if m.Dst.Is6() {
		a = m.Dst.As16()
	}
	return inet6Ntop(a)
}
