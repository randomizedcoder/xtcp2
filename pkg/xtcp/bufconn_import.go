package xtcp

// Blank import to force go mod vendor to include the bufconn test helper
// (used by streaming-gRPC unit tests in this package). buildGoModule
// strips test-only sub-packages from the vendored source by default,
// so we keep the import in a regular .go file to anchor it in the
// dependency graph.
import _ "google.golang.org/grpc/test/bufconn"
