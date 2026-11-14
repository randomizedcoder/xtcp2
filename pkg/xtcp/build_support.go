package xtcp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

// ErrNotCompiledIn marks a config that asks for a feature this binary was
// built without. The CLI maps it to a usage exit; ConfigService.Set refuses
// it, because Set re-execs the same binary and would hit the same wall.
var ErrNotCompiledIn = errors.New("not compiled into this binary")

// CheckBuildSupport reports whether c asks for a build-tag-gated debug writer
// that is not in this binary: the netlink capture writer (`-tags capture`)
// or a destination such as `file:` (`-tags dest_file`). An unknown scheme is
// not its concern; validateInput reports that.
func CheckBuildSupport(c *xtcp_config.XtcpConfig) error {
	if c.GetWriteFiles() > 0 && !CaptureCompiledIn {
		return fmt.Errorf("-writeFiles %d: the netlink capture writer is %w; "+
			"it is a debugging tool that writes raw netlink packets to disk. "+
			"Rebuild with '-tags capture' (or use the `xtcp2-capture` Nix attribute) to opt in",
			c.GetWriteFiles(), ErrNotCompiledIn)
	}
	scheme, _, _ := strings.Cut(c.GetDest(), ":")
	if _, status := lookupDestinationFactory(scheme); status == destLookupNotCompiledIn {
		return fmt.Errorf("%w: %w", ErrNotCompiledIn, destinationLookupError(scheme, status))
	}
	return nil
}
