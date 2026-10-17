// Package rdmacapture redacts RDMA captures and writes and verifies their
// manifests.
//
// A capture directory is pkg/linkmonitor/internal/rdmanative/testdata/
// <kernel>/<source>/, laid out as in GO-NATIVE-RDMA-ASSESSMENT.md "Captured
// testdata": nldev/, nlmon/, async/, umad/, sysfs/ and verbs/, one stream per
// top-level directory, plus manifest.json. The capture script
// (nix/tests/rdma-native-capture.sh) writes the files; this package, through
// cmd/rdma-capture-manifest, rewrites the host's identifiers to documentation
// values and records every file's sha256.
//
// Redaction keeps lengths. A MAC, GUID, GID or IP address in a pcap record, an
// strace hex dump or an strace string is replaced byte for byte, so every
// netlink attribute's recorded length and offset still holds. Text files get
// the textual forms replaced, which may change a compressed IPv6 address's
// length; nothing parses text by offset.
//
// This is capture tooling, not a decode path: it uses encoding/json and fmt
// freely, and the rdma-parse lint rules do not apply to it.
package rdmacapture
