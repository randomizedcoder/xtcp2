// Package io_uring is the xtcp2-internal io_uring helper layer. It owns
// per-Netlinker ring lifecycle, the canonical 64-bit userdata encoding
// used to tag every SQE, and the buffer-ownership map that keeps pool
// buffers alive between submission and completion.
//
// See /home/das/.claude/profiles/runpod/plans/in-this-repo-there-starry-tiger.md
// for the design rationale.
package io_uring

// Operation tags every CQE so the netlinker goroutine can dispatch
// completions back to the right consumer without a side-channel lookup.
//
// Wire layout of the 64-bit userdata stamped on each SQE:
//
//	bits 63..56  Operation     (uint8)
//	bits 55..32  reserved      (24 bits) — must be zero
//	bits 31..0   RequestID     (uint32) — per-ring monotonic counter
//
// NsID is intentionally absent: the ring is per-Netlinker, so the netns
// is already implied by the goroutine that owns the ring.
type Operation uint8

const (
	// OpRead — a recvmsg SQE submitted against the netlink fd.
	OpRead Operation = 0
	// OpSendUDP — a send SQE submitted against the udp dest fd.
	OpSendUDP Operation = 1
	// OpSendUnix — a writev SQE (header + payload iovec) submitted
	// against the unix-stream dest fd.
	OpSendUnix Operation = 2
	// OpSendUnixGram — a send SQE submitted against the unixgram dest fd.
	OpSendUnixGram Operation = 3
)

// EncodedRequest is the in-memory representation of a CQE userdata.
type EncodedRequest struct {
	Operation Operation
	RequestID uint32
}

// serialize packs an EncodedRequest into a 64-bit userdata word.
func serialize(req EncodedRequest) uint64 {
	return uint64(req.Operation)<<56 | uint64(req.RequestID)
}

// deserialize unpacks a 64-bit userdata word back into an EncodedRequest.
func deserialize(data uint64) EncodedRequest {
	return EncodedRequest{
		Operation: Operation(data >> 56),
		RequestID: uint32(data),
	}
}
