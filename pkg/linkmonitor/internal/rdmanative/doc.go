// Package rdmanative derives RDMA port and device events without cgo.
//
// Decoders here read kernel structures at explicit offsets and follow the
// parsing rules in cmd/go-link-monitor/GO-NATIVE-RDMA-DESIGN.md: no
// reflection, no allocation on the hot path, and one length check before
// the field loads.
package rdmanative
