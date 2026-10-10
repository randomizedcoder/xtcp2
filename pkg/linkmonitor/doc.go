// Package linkmonitor provides reusable network link monitoring interfaces.
//
// Construction validates and copies configuration without I/O. The caller owns
// context cancellation, logging, signal handling and any metrics HTTP server.
// Run connects the Linux poller, collectors and durable baseline lifecycle.
// Full RDMA events and native capabilities require the rdma tag and cgo.
// Explicit io_uring selection returns ErrBackendUnavailable until implemented.
package linkmonitor
