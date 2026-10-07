// Package linkmonitor provides reusable network link monitoring interfaces.
//
// Construction validates and copies configuration without I/O. The caller owns
// context cancellation, logging, signal handling and any metrics HTTP server.
// Live Linux collection is not yet wired: Run currently returns
// ErrBackendUnavailable. The lifecycle is exercised with private fake sessions
// while the transport and reconciliation phases are implemented.
package linkmonitor
