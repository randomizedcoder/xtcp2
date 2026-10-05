package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// RtaCacheinfo mirrors the kernel's `struct rta_cacheinfo`, the payload of
// RTA_CACHEINFO on a route message — the dst-entry bookkeeping behind
// `ip route`'s `expires`, `error`, `users`, `used`, `age`, `ipid` and
// `ts`/`tsage` tokens.
//
//	struct rta_cacheinfo {
//		__u32	rta_clntref;
//		__u32	rta_lastuse;
//		__s32	rta_expires;
//		__u32	rta_error;
//		__u32	rta_used;
//
//	#define RTNETLINK_HAVE_PEERINFO 1
//		__u32	rta_id;
//		__u32	rta_ts;
//		__u32	rta_tsage;
//	};
//
// Eight members, 32 bytes, and exactly one of them is signed: rta_expires is
// __s32. The kernel writes a negative value for an already-lapsed expiry
// (`ci.rta_expires = (expires > 0) ? clock : -clock`, net/core/rtnetlink.c
// :1046), and `ip` prints it with %d. Decoding that member as unsigned turns
// "-1" into "4294967295", which is why Expires is int32 below while its seven
// siblings are uint32.
//
// # Which members can ever be non-zero, and on which command
//
// This is the whole reason the type exists, and it is not what reading
// print_rta_cacheinfo alone suggests. Everything below was measured, not
// inferred.
//
// The kernel fills the struct in exactly one place, rtnl_put_cacheinfo
// (net/core/rtnetlink.c:1028-1052), and three of its members are written only
// behind `if (dst)`:
//
//	if (dst) {
//		delta = jiffies - READ_ONCE(dst->lastuse);
//		ci.rta_lastuse = jiffies_delta_to_clock_t(delta);
//		ci.rta_used = dst->__use;
//		ci.rta_clntref = rcuref_read(&dst->__rcuref);
//	}
//
// There are two callers. net/ipv4/route.c:3074 is inside rt_fill_info, which
// serves `ip route get` — a non-dump RTM_GETROUTE — and always passes a real
// dst. net/ipv6/route.c:5944 is inside rt6_fill_node, which serves BOTH the v6
// dump and the v6 get, and passes dst only on the get. The v4 FIB dump
// (fib_dump_info) never calls rtnl_put_cacheinfo at all.
//
// So on a dump, which is every command goip implements:
//
//   - rta_clntref, rta_lastuse, rta_used are always zero. dst is NULL.
//     These are precisely the three members `ip` prints under `-s`.
//   - rta_error is `dst ? dst->error : 0`, so also always zero.
//   - rta_id is passed as the literal 0 at both call sites.
//   - rta_ts and rta_tsage are never written by rtnl_put_cacheinfo at all;
//     the struct is brace-initialized with only rta_error and rta_id, so the
//     remaining six start zeroed and these two are never assigned.
//   - rta_expires is the exception, and the reason this file is not dead
//     code. rt6_fill_node reads `expires = dst ? dst->expires : rt->expires`
//     (net/ipv6/route.c:5931) — the fib6_info's own expiry, available with no
//     dst — whenever RTF_EXPIRES is set.
//
// Measured on the committed corpus: every v4 route fixture carries zero
// RTA_CACHEINFO attributes, every v6 route fixture carries one per route, and
// all 48 of them in testdata/7_1_8 are 32 zero bytes.
//
// # The consequence for `-s`
//
// The three members `-s` gates are the three that a dump can never set. So
// `ip -s route show` is byte-identical to `ip route show`, on both families,
// and goip's acceptance of `-s` here is a no-op by construction rather than by
// luck. Verified against a route that does carry an expiry: with
// `fd99:beef::/64 dev dummy0 expires 600` present, `ip -6 route show` and
// `ip -s -6 route show` produce identical bytes.
//
// What `-s` does not gate is `expires`, and that is where the real divergence
// was. print_rta_cacheinfo (ip/iproute.c:500-532) prints expires, error, ipid
// and ts/tsage OUTSIDE the show_stats guard, so an expiring IPv6 route makes
// plain `ip -6 route show` emit a token goip omitted. That command is already
// in the parity table and already green — green only because no fixture and no
// test host happened to hold a route with a finite lifetime.
//
// `ip route get` is the only command that reaches a non-zero rta_clntref,
// rta_used or rta_lastuse, and goip does not implement it ("object recognized
// but not implemented"). Those three members are decoded here anyway: they are
// five lines, the struct is fixed-width so they cost nothing to skip, and
// leaving holes in a transcribed kernel struct is how an offset error gets in.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/rtnetlink.h
type RtaCacheinfo struct {
	Clntref uint32 // 4 — rta_clntref, dst refcount; `-s` prints it as `users`
	Lastuse uint32 // 4 — rta_lastuse, USER_HZ ticks; `-s` prints it as `age`
	Expires int32  // 4 — rta_expires, SIGNED; USER_HZ ticks, ungated
	Error   uint32 // 4 — rta_error
	Used    uint32 // 4 — rta_used; `-s` prints it as `used`
	ID      uint32 // 4 — rta_id, printed as `ipid`
	Ts      uint32 // 4 — rta_ts
	Tsage   uint32 // 4 = 32 ( 32 / 4 = 8 )
}

const (
	RtaCacheinfoSizeCst = 32
	RtaCacheinfoReadCst = RtaCacheinfoSizeCst

	// RtaUserHzCst is iproute2's get_user_hz(), the divisor print_rta_cacheinfo
	// applies to rta_lastuse and rta_expires before printing them as seconds.
	//
	// get_user_hz (include/utils.h:218-223) memoizes __get_user_hz(), which is
	// `return sysconf(_SC_CLK_TCK)` (lib/utils.c:1016-1019). _SC_CLK_TCK is
	// fixed at 100 on Linux for every architecture — it is USER_HZ, an ABI
	// constant glibc reports from AT_CLKTCK, and is deliberately independent
	// of the kernel's internal CONFIG_HZ. `getconf CLK_TCK` returns 100.
	//
	// Hardcoded rather than read, because this package does no syscalls at
	// decode time and a parity renderer wants the same answer on every host.
	//
	// The division is C integer division, so it truncates: 149/100 is 1sec,
	// and 99/100 is 0sec. `ip` prints the truncated integer, never a fraction.
	RtaUserHzCst = 100
)

var (
	ErrRtaCacheinfoSmall = errors.New("data too small for RtaCacheinfo")
)

// DeserializeRtaCacheinfo does a binary read of an RtaCacheinfo with a basic
// length check.
//
// A payload longer than 32 bytes is accepted and the remainder ignored, which
// is what a future kernel appending a member would send and what
// RTA_DATA/struct-cast does in C. A payload shorter than 32 bytes is refused
// rather than partially decoded: unlike the IFLA_INET6_STATS array, where a
// short payload means an older and shorter enum, this is a fixed-width struct
// and a short one means the attribute is malformed.
func DeserializeRtaCacheinfo(data []byte, m *RtaCacheinfo) (n int, err error) {
	if len(data) < RtaCacheinfoSizeCst {
		return 0, ErrRtaCacheinfoSmall
	}

	m.Clntref = binary.LittleEndian.Uint32(data[0:4])
	m.Lastuse = binary.LittleEndian.Uint32(data[4:8])
	m.Expires = int32(binary.LittleEndian.Uint32(data[8:12]))
	m.Error = binary.LittleEndian.Uint32(data[12:16])
	m.Used = binary.LittleEndian.Uint32(data[16:20])
	m.ID = binary.LittleEndian.Uint32(data[20:24])
	m.Ts = binary.LittleEndian.Uint32(data[24:28])
	m.Tsage = binary.LittleEndian.Uint32(data[28:32])

	return RtaCacheinfoReadCst, nil
}
