package main

import (
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// Process exit codes. Configuration the operator must change exits with
// exitUsage (sysexits EX_USAGE), so a supervisor can tell "fix the command
// line" from a runtime failure (exitFailure).
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 64
)

// exitCodeFor maps a startup error to a process exit code.
func exitCodeFor(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errUsage),
		errors.Is(err, xtcp.ErrNotCompiledIn),
		errors.Is(err, xtcp.ErrInsecureTransport):
		return exitUsage
	default:
		return exitFailure
	}
}
