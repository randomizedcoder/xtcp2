package nlparity

// Normalization: zeroing the fields two runs of the same command are
// GUARANTEED to disagree on, and nothing else.
//
// # Why this is not "zero the volatile attributes"
//
// The tempting shortcut is to drop the four cacheinfo attributes entirely, or
// zero their whole payload. Both are wrong in the same way: they discard fields
// that are stable and comparable, and every such field is one a goip bug could
// hide behind. What follows is what each attribute actually carried in the
// guest corpus, measured rather than assumed, and what that implies.
//
//	IFA_CACHEINFO, 16 bytes {preferred, valid, cstamp, tstamp}
//	  Measured, all 10 addresses across both namespaces:
//	    ffffffff ffffffff 85060000 85060000
//	  preferred and valid are INFINITY_LIFE_TIME on every one of them, because
//	  the topology's addresses are static. cstamp == tstamp, both hundredths of
//	  a second since boot. So the lifetimes are COMPARED — free, and they are
//	  what proves goip read the attribute rather than synthesizing "forever" —
//	  and only the two stamps are zeroed.
//
//	NDA_CACHEINFO, 16 bytes {confirmed, used, updated, refcnt}
//	  Measured: e9010000 e9010000 e9010000 00000000, and four more with all
//	  three counters differing. The first three are age counters in USER_HZ and
//	  move between any two captures; refcnt was 0 on every entry. Zero the
//	  three, compare refcnt.
//
//	RTA_CACHEINFO, 32 bytes {clntref, lastuse, expires, error, used, id, ts,
//	                         tsage}
//	  Measured: 32 zero bytes on all 17 routes that carry it, across both guest
//	  namespaces and all three route captures. Nothing here needs
//	  normalizing today, which is exactly why the conservative choice costs
//	  nothing: only the four time-derived fields are zeroed, and clntref, error
//	  and used stay compared. If a future capture has a non-zero refcount, that
//	  is a finding worth seeing rather than a field already blanked.
//
//	IFLA_INET6_CACHEINFO, 16 bytes {max_reasm_len, tstamp, reachable_time,
//	                                retrans_time}
//	  Measured: ffff0000 85060000 94a00000 e8030000 and seven more.
//	  max_reasm_len is 0xffff and retrans_time 1000 on every link; tstamp moves;
//	  reachable_time is the per-interface randomized value and differs per link
//	  (41108, 35660, 18022, ...). Only tstamp is zeroed.
//
//	  reachable_time is deliberately left compared even though it is volatile in
//	  principle — the kernel recomputes it every few minutes, so two adjacent
//	  captures can straddle a recompute. That is precisely the case D_control
//	  exists to catch, and letting it surface as a volatile-fallback entry with
//	  a reason is better than silently blanking a field: the allowlist comment
//	  calls every such entry "a bug report against D_control", and that only
//	  works if the bug reports actually get filed.
//
// This attribute is also the reason SubAttrs is exported. It is two levels
// down: IFLA_AF_SPEC -> the AF_INET6 member -> IFLA_INET6_CACHEINFO, and the
// kernel sets NLA_F_NESTED on none of those three, so the descent is driven by
// attribute type rather than by a flag.
//
// # Ordering: normalize after attribution, never before
//
// Seq and pid are zeroed here too, via Msg.Canonical, and that is safe only
// because a Segmentation already bound every transaction to its socket. Doing
// it earlier destroys the only evidence of who sent what; see the header of
// nlparity_segment.go.

import (
	"golang.org/x/sys/unix"
)

// Offsets and widths of the volatile fields, named so the zeroing reads as the
// struct it is editing rather than as magic numbers. Every one is a __u32.
const (
	// ifaCacheinfoStampsOffCst is cstamp, the third u32 of struct
	// ifa_cacheinfo; tstamp follows it, so eight bytes go at once.
	ifaCacheinfoStampsOffCst = 8
	ifaCacheinfoStampsLenCst = 8

	// ndaCacheinfoAgeOffCst is ndm_confirmed, the first u32 of struct
	// nda_cacheinfo, with ndm_used and ndm_updated after it. refcnt is the
	// fourth and is not included.
	ndaCacheinfoAgeOffCst = 0
	ndaCacheinfoAgeLenCst = 12

	// rtaCacheinfoLastuseOffCst is rta_lastuse, with rta_expires after it.
	// rta_clntref precedes and stays compared.
	rtaCacheinfoLastuseOffCst = 4
	rtaCacheinfoLastuseLenCst = 8
	// rtaCacheinfoTsOffCst is rta_ts, with rta_tsage after it: the last two
	// u32s of the 32-byte struct.
	rtaCacheinfoTsOffCst = 24
	rtaCacheinfoTsLenCst = 8

	// inet6CacheinfoTstampOffCst is tstamp, the second u32 of struct
	// ifla_cacheinfo. max_reasm_len precedes it; reachable_time and
	// retrans_time follow, and both stay compared.
	inet6CacheinfoTstampOffCst = 4
	inet6CacheinfoTstampLenCst = 4
)

// NormalizeMsg returns a copy of m with nlmsg_seq, nlmsg_pid and the volatile
// in-payload fields zeroed.
//
// The body is deep-copied, so the returned message does not alias the capture
// buffer and the original is never modified — a differ normalizes both sides
// and then still wants the originals to quote in a report.
//
// A truncated attribute is left alone rather than being an error: a body too
// short for the field being zeroed is itself a divergence the differ will
// report, and panicking on it would replace a finding with a crash.
func NormalizeMsg(m Msg) Msg {
	out := m.Canonical()
	if len(m.Body) == 0 {
		out.Body = nil
		return out
	}

	body := make([]byte, len(m.Body))
	copy(body, m.Body)
	out.Body = body

	// The attributes alias body, so zeroing through Val edits the copy.
	_, attrs, _ := DecodeAttrs(m.Hdr.Type, body)
	normalizeAttrs(m.Hdr.Type, attrs)

	return out
}

// NormalizeMsgs is NormalizeMsg over a slice, for a transaction's replies.
func NormalizeMsgs(msgs []Msg) []Msg {
	if msgs == nil {
		return nil
	}
	out := make([]Msg, len(msgs))
	for i := range msgs {
		out[i] = NormalizeMsg(msgs[i])
	}
	return out
}

// normalizeAttrs zeroes in place, dispatching on the message's attribute
// namespace rather than on the message type, so all three verbs of a family are
// handled by one arm.
func normalizeAttrs(msgType uint16, attrs []Attr) {
	switch AttrFamilyOf(msgType) {
	case AttrFamilyIFA:
		for _, a := range attrs {
			if a.BareType() == uint16(unix.IFA_CACHEINFO) {
				zeroRange(a.Val, ifaCacheinfoStampsOffCst, ifaCacheinfoStampsLenCst)
			}
		}
	case AttrFamilyNDA:
		for _, a := range attrs {
			if a.BareType() == uint16(unix.NDA_CACHEINFO) {
				zeroRange(a.Val, ndaCacheinfoAgeOffCst, ndaCacheinfoAgeLenCst)
			}
		}
	case AttrFamilyRTA:
		for _, a := range attrs {
			if a.BareType() == uint16(unix.RTA_CACHEINFO) {
				zeroRange(a.Val, rtaCacheinfoLastuseOffCst, rtaCacheinfoLastuseLenCst)
				zeroRange(a.Val, rtaCacheinfoTsOffCst, rtaCacheinfoTsLenCst)
			}
		}
	case AttrFamilyIFLA:
		for _, a := range attrs {
			if a.BareType() == uint16(unix.IFLA_AF_SPEC) {
				normalizeAFSpec(a.Val)
			}
		}
	case AttrFamilyNone:
		// NLMSG_DONE, NLMSG_ERROR and anything unmodeled carry no attributes
		// this package can name, so there is nothing to normalize. Their
		// payloads are compared whole.
	}
}

// normalizeAFSpec descends IFLA_AF_SPEC to the AF_INET6 member and zeroes
// IFLA_INET6_CACHEINFO's tstamp.
//
// The AF_INET member is walked past, not into: struct ipv4_devconf is
// configuration, and nothing measured in it moves between runs.
func normalizeAFSpec(val []byte) {
	fams, _ := SubAttrs(val)
	for _, fam := range fams {
		if fam.BareType() != unix.AF_INET6 {
			continue
		}
		inner, _ := SubAttrs(fam.Val)
		for _, in := range inner {
			if in.BareType() == uint16(unix.IFLA_INET6_CACHEINFO) {
				zeroRange(in.Val, inet6CacheinfoTstampOffCst, inet6CacheinfoTstampLenCst)
			}
		}
	}
}

// zeroRange zeroes b[off:off+n] if the whole range is present, and does nothing
// otherwise. The bounds check is the truncation tolerance described above.
func zeroRange(b []byte, off, n int) {
	if off < 0 || n < 0 || off+n > len(b) {
		return
	}
	for i := off; i < off+n; i++ {
		b[i] = 0
	}
}
