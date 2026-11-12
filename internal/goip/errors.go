package goip

import "errors"

// ErrFlushIncomplete is a flush that ran out of rounds: `ip -loops N` with
// addresses still matching after N. `ip` exits 255 for it.
var ErrFlushIncomplete = errors.New("flush remains incomplete")

// ErrDumpTerminated is ll_init_map's link dump failing ahead of a restore's
// first write, the address one's or the route one's: `ip` prints "Dump
// terminated" and exits 1.
var ErrDumpTerminated = errors.New("restore")

// ErrAddrLabelFlushTerminated is `Flush terminated`: a failed dump or a short
// reply, after which ipaddrlabel_flush returns -1, so 255.
var ErrAddrLabelFlushTerminated = errors.New("addrlabel flush: Flush terminated")

// ErrInvalidArgument is an argument `ip` refuses before it sends anything:
// invarg, nodev, an unknown keyword, a missing value. The message after it
// is iproute2's.
var ErrInvalidArgument = errors.New("invalid argument")

// ErrWriteNotApplied is a write verb run without GOIP_WRITE_OUT.
var ErrWriteNotApplied = errors.New("goip never applies a write; set " + writeOutEnvCst +
	" to receive the request it would send")

// ErrLinkUsage is a refusal iplink_parse ends with exit(-1), 255: invarg,
// duparg, duparg2 and incomplete_command. Its returned refusals (a bad
// lladdr, an on/off word, nodev) exit 1, as ErrInvalidArgument does.
var ErrLinkUsage = errors.New("link")

// ErrNeighFlushTerminated is a neighbor dump failing inside `ip neigh flush`:
// `ip` prints "Flush terminated" and exits 1 (ip/ipneigh.c:621-623).
var ErrNeighFlushTerminated = errors.New("flush terminated")

// ErrNexthopFlushTerminated is a dump failing inside `nexthop flush`:
// ipnh_flush returns -2, which do_cmd negates, so 2.
var ErrNexthopFlushTerminated = errors.New("nexthop flush: dump terminated")

// writeOutEnvCst names the file a write verb writes its framed request to.
const writeOutEnvCst = "GOIP_WRITE_OUT"
